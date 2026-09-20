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
	//
	// Sign in used to be a fourth entry here, and that was nap-1j5: a nav item
	// is a constant, so the header offered it to people who were already
	// signed in, on every page, and never once said whose session it was. It
	// is now the header's account control instead, which is built per request
	// from LayoutData.CurrentUser / SignInHref / SignOutHref. Every service
	// that renders a page therefore has to be given the sign-in path
	// separately -- hence SignInHref below on all three.
	nav := []templates.NavItem{
		{Label: "Home", Href: "/"},
		{Label: "The slate", Href: board.SlatePath},
	}

	queries := store.New(pool)

	accounts := signin.New(signin.Options{
		Auth:       authenticator,
		Sessions:   sessions,
		Store:      queries,
		Logger:     logger,
		Origin:     cfg.Origin,
		Nav:        nav,
		SignInHref: authenticator.LoginPath(),
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
	}).Routes(r)

	// The HTML pages and /static/. Origin is only used to build absolute URLs
	// for link previews; nothing here reads the database.
	web.New(web.Options{
		Origin:     cfg.Origin,
		Nav:        nav,
		SignInHref: authenticator.LoginPath(),
	}).Routes(r)

	return r
}
