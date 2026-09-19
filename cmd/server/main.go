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

	"github.com/ERaith/nightofathousandpixels/internal/config"
	"github.com/ERaith/nightofathousandpixels/internal/web"
	"github.com/ERaith/nightofathousandpixels/internal/web/health"
	"github.com/ERaith/nightofathousandpixels/internal/web/middleware"
)

const (
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 15 * time.Second
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

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           newRouter(cfg, logger, pool),
		ReadHeaderTimeout: readHeaderTimeout,
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

func newRouter(cfg *config.Config, logger *slog.Logger, pool *pgxpool.Pool) http.Handler {
	r := chi.NewRouter()
	// Order is load-bearing; see the package comment on internal/web/middleware.
	r.Use(middleware.ClientIPPolicy(cfg.TrustedProxyCount))
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer(logger))
	r.Use(middleware.RequestLogger(logger))

	r.Method(http.MethodGet, "/healthz", health.NewHandler(pool, logger))

	// The HTML pages and /static/. Origin is only used to build absolute URLs
	// for link previews; nothing here reads the database.
	web.New(web.Options{Origin: cfg.Origin}).Routes(r)

	return r
}
