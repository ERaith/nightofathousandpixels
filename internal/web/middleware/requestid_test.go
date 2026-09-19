package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/ERaith/nightofathousandpixels/internal/web/middleware"
)

func TestRequestIDRoundTrip(t *testing.T) {
	t.Parallel()

	var seen string
	h := middleware.RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = middleware.GetRequestID(r.Context())
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set(middleware.RequestIDHeader, "test123")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if seen != "test123" {
		t.Errorf("context request ID = %q, want %q", seen, "test123")
	}
	if got := w.Header().Get(middleware.RequestIDHeader); got != "test123" {
		t.Errorf("response header = %q, want %q", got, "test123")
	}
}

func TestRequestIDGeneratedWhenAbsent(t *testing.T) {
	t.Parallel()

	ids := make(map[string]bool, 2)
	h := middleware.RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		ids[middleware.GetRequestID(r.Context())] = true
	}))

	for range 2 {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Header().Get(middleware.RequestIDHeader) == "" {
			t.Fatal("no request ID on the response")
		}
	}

	if len(ids) != 2 {
		t.Errorf("got %d distinct generated IDs across 2 requests, want 2", len(ids))
	}
	delete(ids, "")
	if len(ids) != 2 {
		t.Error("a generated request ID was empty")
	}
}

// TestRequestIDSanitisesInbound covers the fact that this value is chosen by
// the caller and then echoed in a header and copied into every log line for
// the request.
func TestRequestIDSanitisesInbound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain value passes through", in: "abc-123_x.y", want: "abc-123_x.y"},
		{name: "quotes and spaces are dropped", in: `evil" id`, want: "evilid"},
		{name: "slashes are dropped", in: "../../etc/passwd", want: "....etcpasswd"},
		{name: "control characters are dropped", in: "a\r\nb", want: "ab"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var seen string
			h := middleware.RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				seen = middleware.GetRequestID(r.Context())
			}))

			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set(middleware.RequestIDHeader, tc.in)
			h.ServeHTTP(httptest.NewRecorder(), r)

			if seen != tc.want {
				t.Errorf("request ID = %q, want %q", seen, tc.want)
			}
		})
	}
}

func TestRequestIDCapsInboundLength(t *testing.T) {
	t.Parallel()

	var seen string
	h := middleware.RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = middleware.GetRequestID(r.Context())
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set(middleware.RequestIDHeader, strings.Repeat("a", 4096))
	h.ServeHTTP(httptest.NewRecorder(), r)

	if len(seen) != 64 {
		t.Errorf("request ID length = %d, want 64", len(seen))
	}
}

// TestRequestIDVisibleToChi pins the interop: anything built on chi's
// GetReqID must see the same identifier this package logs, not a second one.
func TestRequestIDVisibleToChi(t *testing.T) {
	t.Parallel()

	var ours, theirs string
	h := middleware.RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		ours = middleware.GetRequestID(r.Context())
		theirs = chimw.GetReqID(r.Context())
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if ours == "" || ours != theirs {
		t.Errorf("chi GetReqID = %q, this package = %q; want the same non-empty value", theirs, ours)
	}
}
