// Package health serves the readiness endpoint that the homelab's uptime
// monitor polls.
//
// It lives beside the middleware rather than inside it because it is a
// handler, not a wrapper: nothing else in the request chain depends on it.
package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	// checkTimeout bounds one database probe. A monitor wants an answer, and
	// a health check that hangs until the client gives up is reported as a
	// timeout anyway -- better to answer 503 quickly and say why in the log.
	checkTimeout = 2 * time.Second

	// cacheTTL is how long a probe result is reused.
	//
	// /healthz is unauthenticated and public, so its cost per request is
	// whatever an anonymous caller can multiply by however many requests they
	// care to send. Reusing the last answer for a second bounds the database
	// side of that at one round trip per second no matter how hard the
	// endpoint is hit, while staying far below any sane monitor's poll
	// interval, so nothing is slower to notice a real outage.
	cacheTTL = time.Second
)

// DB is the part of the database handle this package needs. It is the same
// shape as internal/store's DBTX, so a *pgxpool.Pool, a pgx.Tx or a store
// query set all satisfy it without this package importing any of them.
type DB interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Handler answers /healthz.
type Handler struct {
	db     DB
	logger *slog.Logger
	now    func() time.Time

	mu        sync.Mutex
	cachedErr error
	cachedAt  time.Time
}

// NewHandler returns a health handler that probes db.
func NewHandler(db DB, logger *slog.Logger) *Handler {
	return &Handler{
		db:        db,
		logger:    logger,
		now:       time.Now,
		mu:        sync.Mutex{},
		cachedErr: nil,
		cachedAt:  time.Time{},
	}
}

// ServeHTTP reports whether the process can reach its database: 200 when it
// can, 503 when it cannot.
//
// Before this it was a static 200, which is a check that cannot fail and
// therefore reports nothing. A process that is running but cannot reach
// Postgres serves errors on every page, and the monitor should say so.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	err := h.check(r.Context())

	status := http.StatusOK
	// The reason for a failure goes to the log, not to the body: a connection
	// error from pgx names the host, port and user it tried, and this
	// endpoint is public.
	body := map[string]string{"status": "ok"}
	if err != nil {
		status = http.StatusServiceUnavailable
		body = map[string]string{"status": "unavailable"}
	}

	w.Header().Set("Content-Type", "application/json")
	// A cached 200 from a monitor or a proxy would defeat the whole endpoint.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		h.logger.LogAttrs(r.Context(), slog.LevelWarn, "healthz: write response",
			slog.String("error", err.Error()))
	}
}

// check returns the database's last known state, probing it again if the last
// answer is older than cacheTTL.
func (h *Handler) check(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.cachedAt.IsZero() && h.now().Sub(h.cachedAt) < cacheTTL {
		return h.cachedErr
	}

	// The probe is deliberately detached from the request context except for
	// its deadline: the lock is held across it, so a client that disconnects
	// mid-probe must not cancel the answer every other waiter is queued for.
	probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), checkTimeout)
	defer cancel()

	previous := h.cachedErr
	first := h.cachedAt.IsZero()
	h.cachedErr = h.probe(probeCtx)
	h.cachedAt = h.now()

	// Logging belongs here rather than in the response path, so that the log
	// records probes and not requests. Reporting every 503 would hand an
	// anonymous caller an unbounded log-volume amplifier on a public
	// endpoint, and would bury the transition -- the moment the database went
	// away -- under thousands of identical lines saying it is still away.
	switch {
	case h.cachedErr != nil && (first || previous == nil):
		h.logger.LogAttrs(probeCtx, slog.LevelError, "healthz: database unreachable",
			slog.String("error", h.cachedErr.Error()))
	case h.cachedErr == nil && previous != nil:
		h.logger.LogAttrs(probeCtx, slog.LevelInfo, "healthz: database reachable again")
	case h.cachedErr != nil:
		// Still down. Debug level keeps the detail available when someone is
		// actively looking without spending a log line a second on it.
		h.logger.LogAttrs(probeCtx, slog.LevelDebug, "healthz: database still unreachable",
			slog.String("error", h.cachedErr.Error()))
	}

	return h.cachedErr
}

// probe runs the cheapest statement Postgres has. It is not a query against
// any table: this check must stay O(1) and must not depend on a migration
// having run.
func (h *Handler) probe(ctx context.Context) error {
	var one int
	if err := h.db.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
		return err
	}
	return nil
}
