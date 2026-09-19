package health_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/ERaith/nightofathousandpixels/internal/web/health"
)

// fakeDB counts probes so that the caching behaviour can be measured rather
// than assumed, and can be switched between healthy and broken mid-test.
type fakeDB struct {
	mu    sync.Mutex
	err   error
	calls int
}

func (f *fakeDB) QueryRow(context.Context, string, ...any) pgx.Row {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++
	return fakeRow{err: f.err}
}

func (f *fakeDB) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.err = err
}

func (f *fakeDB) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

type fakeRow struct{ err error }

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) == 1 {
		if p, ok := dest[0].(*int); ok {
			*p = 1
		}
	}
	return nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{
		AddSource: false, Level: slog.LevelDebug, ReplaceAttr: nil,
	}))
}

func get(t *testing.T, h http.Handler) *httptest.ResponseRecorder {
	t.Helper()

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	return w
}

func TestHealthzOKWhenDatabaseAnswers(t *testing.T) {
	t.Parallel()

	w := get(t, health.NewHandler(&fakeDB{}, discardLogger()))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if got := strings.TrimSpace(w.Body.String()); got != `{"status":"ok"}` {
		t.Errorf("body = %s", got)
	}
	// A monitor that is served a cached 200 is watching nothing.
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", got, "no-store")
	}
}

// TestHealthzFailsWhenDatabaseIsDown is the whole point of the endpoint: the
// static 200 it replaced could not report an outage.
func TestHealthzFailsWhenDatabaseIsDown(t *testing.T) {
	t.Parallel()

	db := &fakeDB{}
	db.setErr(errors.New(`failed to connect to "user=nap database=nap" at 10.0.0.5:5432`))
	w := get(t, health.NewHandler(db, discardLogger()))

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	// The body says only that it is down. The connection error names the
	// host, port and database user, and this endpoint is unauthenticated.
	body := w.Body.String()
	for _, forbidden := range []string{"10.0.0.5", "user=nap", "database=nap"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("response body leaks %q: %s", forbidden, body)
		}
	}
}

// TestHealthzCachesProbes pins the property that keeps a public, cheap-looking
// endpoint from becoming a way to aim traffic at Postgres.
func TestHealthzCachesProbes(t *testing.T) {
	t.Parallel()

	db := &fakeDB{}
	h := health.NewHandler(db, discardLogger())

	for range 100 {
		if w := get(t, h); w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	}

	if got := db.callCount(); got != 1 {
		t.Errorf("%d database probes for 100 requests, want 1", got)
	}
}

func TestHealthzIsConcurrencySafe(t *testing.T) {
	t.Parallel()

	db := &fakeDB{}
	h := health.NewHandler(db, discardLogger())

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()

			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
			}
		}()
	}
	wg.Wait()

	if got := db.callCount(); got != 1 {
		t.Errorf("%d database probes for 50 concurrent requests, want 1", got)
	}
}

// TestHealthzSurvivesClientDisconnect: the probe runs while the handler holds
// the lock, so it must not inherit the cancellation of whichever request
// happened to trigger it -- otherwise one client hanging up fails the check
// for every request queued behind it.
func TestHealthzSurvivesClientDisconnect(t *testing.T) {
	t.Parallel()

	h := health.NewHandler(&fakeDB{}, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil).WithContext(ctx))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d: a cancelled request must not report the database as down", w.Code, http.StatusOK)
	}
}
