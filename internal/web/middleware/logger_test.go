package middleware_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ERaith/nightofathousandpixels/internal/web/middleware"
)

// logRecorder captures what the middleware writes, as the parsed JSON objects
// a log viewer would see rather than as text.
func logRecorder() (*slog.Logger, func(t *testing.T) []map[string]any) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		AddSource: false, Level: slog.LevelDebug, ReplaceAttr: nil,
	}))

	return logger, func(t *testing.T) []map[string]any {
		t.Helper()

		var out []map[string]any
		for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
			if line == "" {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				t.Fatalf("log line is not JSON: %v\nline: %s", err, line)
			}
			out = append(out, m)
		}
		return out
	}
}

func TestRequestLoggerFields(t *testing.T) {
	t.Parallel()

	logger, records := logRecorder()
	h := middleware.ClientIPPolicy(0)(middleware.RequestID(
		middleware.RequestLogger(logger)(http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusTeapot)
				_, _ = w.Write([]byte("hi"))
			}))))

	r := httptest.NewRequest(http.MethodPost, "/ballot?code=super-secret-oauth-code", nil)
	r.RemoteAddr = "203.0.113.9:1234"
	r.Header.Set("User-Agent", "uptime-kuma")
	r.Header.Set("Cookie", "session=do-not-log-me")
	h.ServeHTTP(httptest.NewRecorder(), r)

	got := records(t)
	if len(got) != 1 {
		t.Fatalf("got %d log records, want 1", len(got))
	}
	rec := got[0]

	for field, want := range map[string]any{
		"msg":       "http request",
		"method":    http.MethodPost,
		"path":      "/ballot",
		"status":    float64(http.StatusTeapot),
		"bytes":     float64(2),
		"client_ip": "203.0.113.9",
	} {
		if rec[field] != want {
			t.Errorf("%s = %v, want %v", field, rec[field], want)
		}
	}
	if _, ok := rec["duration_ms"]; !ok {
		t.Error("duration_ms is missing")
	}
	if id, _ := rec["request_id"].(string); id == "" {
		t.Error("request_id is missing")
	}

	// The two things that must never reach a log file on a public site: the
	// query string, which carries the OAuth code, and anything from a header
	// that holds a credential.
	line := mustMarshal(t, rec)
	for _, forbidden := range []string{"super-secret-oauth-code", "do-not-log-me", "code="} {
		if strings.Contains(line, forbidden) {
			t.Errorf("log record leaks %q:\n%s", forbidden, line)
		}
	}
}

func TestRecovererLogsAndReturns500(t *testing.T) {
	t.Parallel()

	logger, records := logRecorder()
	h := middleware.RequestID(middleware.Recoverer(logger)(http.HandlerFunc(
		func(http.ResponseWriter, *http.Request) {
			panic("boom")
		})))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if body := w.Body.String(); strings.Contains(body, "boom") {
		t.Errorf("panic value leaked into the response body: %q", body)
	}

	got := records(t)
	if len(got) != 1 {
		t.Fatalf("got %d log records, want 1", len(got))
	}
	if got[0]["level"] != "ERROR" || got[0]["panic"] != "boom" {
		t.Errorf("log record = %v, want an ERROR naming the panic", got[0])
	}
	if stack, _ := got[0]["stack"].(string); stack == "" {
		t.Error("stack is missing")
	}
}

// TestRecovererRepanicsOnAbortHandler: ErrAbortHandler is how a handler tells
// net/http it has given up on a response on purpose. Swallowing it would turn
// a deliberate abort into a spurious 500 and an error log every time.
func TestRecovererRepanicsOnAbortHandler(t *testing.T) {
	t.Parallel()

	logger, _ := logRecorder()
	h := middleware.Recoverer(logger)(http.HandlerFunc(
		func(http.ResponseWriter, *http.Request) {
			panic(http.ErrAbortHandler)
		}))

	defer func() {
		if rec := recover(); rec != http.ErrAbortHandler { //nolint:errorlint // sentinel is panicked as a value
			t.Errorf("recovered %v, want it to re-panic with http.ErrAbortHandler", rec)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func mustMarshal(t *testing.T, v any) string {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}
