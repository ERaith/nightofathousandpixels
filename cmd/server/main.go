// Command server runs the Night of a Thousand Pixels HTTP server.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/ERaith/nightofathousandpixels/internal/config"
)

const (
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 15 * time.Second
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           newRouter(),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	// ListenAndServe always returns a non-nil error; ErrServerClosed is the
	// expected one after Shutdown, so it is normalised to nil here.
	serveErr := make(chan error, 1)
	go func() {
		log.Printf("listening on %s", srv.Addr)
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

	log.Printf("shutdown signal received, draining for up to %s", shutdownTimeout)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	log.Print("shutdown complete")
	return <-serveErr
}

func newRouter() http.Handler {
	r := chi.NewRouter()
	// Structured request logging replaces chi's stdlib logger in A5.
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", handleHealthz)

	return r
}

// handleHealthz reports process liveness. It deliberately checks nothing else:
// the database-aware readiness check lands in A5.
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		log.Printf("healthz: write response: %v", err)
	}
}
