// Command server runs the Night of a Thousand Pixels HTTP server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ERaith/nightofathousandpixels/internal/auth"
	"github.com/ERaith/nightofathousandpixels/internal/config"
	"github.com/ERaith/nightofathousandpixels/internal/signin"
	"github.com/ERaith/nightofathousandpixels/internal/store"
	"github.com/ERaith/nightofathousandpixels/internal/theme"
	"github.com/ERaith/nightofathousandpixels/internal/web"
	"github.com/ERaith/nightofathousandpixels/internal/web/board"
	"github.com/ERaith/nightofathousandpixels/internal/web/health"
	"github.com/ERaith/nightofathousandpixels/internal/web/middleware"
	"github.com/ERaith/nightofathousandpixels/internal/web/templates"
)

// Server-side timeouts. Without these an http.Server has none at all, so a
// slow or stalled peer parks a goroutine and its connection indefinitely.
// ReadHeaderTimeout alone is not enough: it bounds the headers and then stops
// caring, so a handler that blocks on an outbound call still has nothing to
// reap it. These are the backstop underneath auth's own client timeout.
const (
	// readTimeout bounds headers plus body.
	readTimeout = 15 * time.Second

	// readHeaderTimeout bounds the headers on their own, which is what stops a
	// slowloris client from holding a connection open cheaply.
	readHeaderTimeout = 10 * time.Second

	// writeTimeout bounds how long a handler may take to write its response.
	// It must stay comfortably above auth's defaultHTTPTimeout: the OIDC
	// callback makes outbound calls while the client waits, and cutting the
	// response short at exactly that boundary would turn a slow provider into
	// an unexplained truncated response.
	writeTimeout = 30 * time.Second

	// idleTimeout bounds a kept-alive connection between requests.
	idleTimeout = 60 * time.Second

	shutdownTimeout = 15 * time.Second

	// discoveryTimeout bounds how long startup will wait for the OIDC provider
	// to answer discovery before giving up.
	//
	// auth.New fails when the provider is unreachable, and that is the right
	// behaviour: a server that boots with a broken auth configuration would
	// otherwise only find out when the first person tried to sign in. But
	// "unreachable" and "not up yet" look identical over one attempt, and in
	// both the dev stack and the compose stack this process and the provider
	// start at the same moment. So discovery is retried for this long before
	// the failure is taken at face value. There is no fallback at the end of
	// it: if discovery never succeeds the process exits, because the
	// alternative is a running server on which nobody can sign in.
	discoveryTimeout = 30 * time.Second

	// discoveryRetryInterval is how often to retry within that window.
	discoveryRetryInterval = time.Second
)

func main() {
	if err := run(); err != nil {
		// Configuration can fail before there is a logger to fail into, so
		// this one path stays on the standard logger.
		log.Fatalf("server: %v", err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := middleware.NewLogger(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// pgxpool.New does not dial here: connections are opened on first use. A
	// database that is down at boot must not stop the process from starting,
	// because a process that refuses to start cannot serve the 503 that tells
	// the monitor what is wrong.
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	// Discovery is a network call, so this is where a broken OAUTH_ISSUER_URL
	// stops the process.
	authenticator, err := discoverProvider(ctx, cfg, logger)
	if err != nil {
		return err
	}

	sessions, err := auth.NewSessions(cfg.CookieSecret, cfg.Origin, 0)
	if err != nil {
		return err
	}
	// The one cheap way to catch an ORIGIN typo in a deployed environment: a
	// session cookie without Secure behind HTTPS is invisible until somebody
	// looks for it.
	logger.Info("session cookies configured",
		slog.Bool("secure", sessions.Secure()),
		slog.Duration("ttl", sessions.TTL()),
		slog.String("origin", cfg.Origin),
	)

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           newRouter(cfg, logger, pool, authenticator, sessions),
		ReadTimeout:       readTimeout,
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	// ListenAndServe always returns a non-nil error; ErrServerClosed is the
	// expected one after Shutdown, so it is normalised to nil here.
	serveErr := make(chan error, 1)
	go func() {
		logger.Info("listening",
			slog.String("addr", srv.Addr),
			slog.Int("trusted_proxy_count", cfg.TrustedProxyCount),
			slog.String("log_level", cfg.LogLevel.String()),
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		// Restore default signal handling so a second SIGINT kills immediately
		// if the drain below hangs.
		stop()
	}

	logger.Info("shutdown signal received", slog.Duration("drain_timeout", shutdownTimeout))
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	logger.Info("shutdown complete")
	return <-serveErr
}

// discoverProvider runs OIDC discovery, retrying until discoveryTimeout.
func discoverProvider(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*auth.Authenticator, error) {
	opts := auth.Options{
		IssuerURL:    cfg.OAuthIssuerURL,
		ClientID:     cfg.OAuthClientID,
		ClientSecret: cfg.OAuthClientSecret,
		Origin:       cfg.Origin,
		CookieSecret: cfg.CookieSecret,
	}

	deadline := time.Now().Add(discoveryTimeout)
	for attempt := 1; ; attempt++ {
		authenticator, err := auth.New(ctx, opts)
		if err == nil {
			logger.Info("oidc provider discovered",
				slog.String("issuer", cfg.OAuthIssuerURL),
				slog.String("redirect_uri", authenticator.RedirectURL()),
			)
			return authenticator, nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return nil, err
		}
		logger.Warn("oidc discovery failed, retrying",
			slog.Int("attempt", attempt),
			slog.String("issuer", cfg.OAuthIssuerURL),
			slog.Any("error", err),
		)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(discoveryRetryInterval):
		}
	}
}

// themesDir is where the theme packs live, relative to the working directory
// the server starts from — the same convention staticDir already uses. It is
// read once at startup and never again: a theme is decided in September and
// then left alone, and a pack changing under a running server is a thing nobody
// wants to debug while thirty people are on the site.
const themesDir = "themes"

func newRouter(
	cfg *config.Config,
	logger *slog.Logger,
	pool *pgxpool.Pool,
	authenticator *auth.Authenticator,
	sessions *auth.Sessions,
) http.Handler {
	r := chi.NewRouter()
	// Order is load-bearing; see the package comment on internal/web/middleware.
	r.Use(middleware.ClientIPPolicy(cfg.TrustedProxyCount))
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer(logger))
	r.Use(middleware.RequestLogger(logger))

	r.Method(http.MethodGet, "/healthz", health.NewHandler(pool, logger))

	// The nav lists only routes that exist and that anybody may follow. The
	// slate is on it because it is public; Submit deliberately is not, even
	// though it now exists -- a header link that bounces a signed-out visitor
	// into an OAuth flow they did not ask for is worse than no link, and the
	// slate already offers "Put a movie up" to the people who can use it and
	// "Sign in to add yours" to the people who cannot.
	nav := []templates.NavItem{
		{Label: "Home", Href: "/"},
		{Label: "The slate", Href: board.SlatePath},
		{Label: "Sign in", Href: authenticator.LoginPath()},
	}

	queries := store.New(pool)

	// The theme packs. A pack that does not load is logged and skipped inside
	// theme.New, and the pages it would have themed render in base.css's own
	// contrast-checked palette instead — a plain, readable site rather than a
	// server that will not start because of a trailing comma in a JSON file.
	packs, err := theme.New(themesDir, logger)
	if err != nil {
		// Only an unreadable themes directory reaches here, and that is still
		// not worth refusing to serve over.
		logger.Error("theme packs could not be read; running unthemed",
			slog.String("dir", themesDir), slog.Any("error", err))
		packs = nil
	}
	pack := currentThemePack(queries, logger)
	logger.Info("theme",
		slog.String("pack", pack),
		slog.Any("available", packs.Names()),
	)

	// The sign-in flow's own pages go through templates.Layout too, so they
	// take the same pack. Its theme is fixed rather than per-request: ?theme=
	// is an admin's tool for judging a pack against the slate, and the pages
	// somebody sees while they are still proving who they are are not where
	// that judgement is made.
	accounts := signin.New(signin.Options{
		Auth:     authenticator,
		Sessions: sessions,
		Store:    queries,
		Logger:   logger,
		Origin:   cfg.Origin,
		Nav:      nav,
		Theme:    packs.Theme(pack),
	})
	accounts.Routes(r)

	// The slate and the submit form: the two pages that read and write the
	// season. The gate is handed over rather than reached for, so that the
	// board depends on the shape of the whitelist check and not on the
	// service that owns it.
	board.New(board.Options{
		Store:         queries,
		DB:            pool,
		Sessions:      sessions,
		RequireMember: accounts.RequireMember,
		SignInHref:    authenticator.LoginPath(),
		Logger:        logger,
		Origin:        cfg.Origin,
		Nav:           nav,
		Theme:         packs.Theme(pack),
	}).Routes(r)

	// The HTML pages, /static/ and /theme/. Origin is only used to build
	// absolute URLs for link previews.
	web.New(web.Options{
		Origin:       cfg.Origin,
		Nav:          nav,
		Themes:       packs,
		Pack:         pack,
		ThemePreview: adminThemePreview(sessions, queries, logger),
	}).Routes(r)

	return r
}

// currentThemePack reads season.theme_pack for the open season.
//
// A season nobody has opened yet, an unreachable database at startup, or a
// column still holding its 'default' all mean the same thing: the unthemed
// site. None of them is a reason not to serve, because the pages that matter
// most in those states are the ones explaining what is going on.
func currentThemePack(q *store.Queries, logger *slog.Logger) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	season, err := q.GetCurrentSeason(ctx)
	if err != nil {
		logger.Warn("no current season to take a theme from; running unthemed",
			slog.Any("error", err))

		return theme.DefaultPackName
	}

	return season.ThemePack
}

// adminThemePreview is the gate on ?theme=.
//
// Previewing is how the 2026 theme gets chosen in late September: two people
// argue about Portal versus Elvira and settle it by looking at the real slate
// on a real phone, thirty seconds apart, instead of one deploy per opinion.
// That makes it an admin's tool, and season_member.is_admin is what an admin
// is here — there is no global admin in this project.
//
// The lookup only runs when ?theme= is present, which is close to never, so
// two queries on those requests is not a cost worth designing around. Any
// failure at all is a no: a preview that cannot be authorised is simply not a
// preview, and the visitor gets the season's real theme rather than an error
// about a feature they did not ask for.
func adminThemePreview(sessions *auth.Sessions, q *store.Queries, logger *slog.Logger) theme.Authorizer {
	return func(r *http.Request) bool {
		session, err := sessions.Read(r)
		if err != nil {
			return false
		}

		ctx := r.Context()
		season, err := q.GetCurrentSeason(ctx)
		if err != nil {
			return false
		}

		member, err := q.GetSeasonMember(ctx, store.GetSeasonMemberParams{
			SeasonID: season.ID,
			PersonID: session.PersonID,
		})
		if err != nil {
			return false
		}
		if !member.IsAdmin {
			return false
		}

		logger.Info("theme preview",
			slog.String("email", session.Email),
			slog.String("pack", r.URL.Query().Get(theme.PreviewParam)),
		)

		return true
	}
}
