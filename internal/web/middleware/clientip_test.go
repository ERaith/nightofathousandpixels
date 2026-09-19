package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ERaith/nightofathousandpixels/internal/web/middleware"
)

// TestClientIPPolicy is the regression test for the reason this package
// exists. The two failures it guards against are opposites and both silent:
// believing X-Forwarded-For when nothing trustworthy sets it, and ignoring it
// when a proxy does.
func TestClientIPPolicy(t *testing.T) {
	t.Parallel()

	const peer = "203.0.113.9:54321"

	tests := []struct {
		name         string
		trustedCount int
		xff          []string
		xRealIP      string
		want         string
	}{{
		name:         "no proxies ignores a forged single XFF",
		trustedCount: 0,
		xff:          []string{"1.2.3.4"},
		want:         "203.0.113.9",
	}, {
		name:         "no proxies ignores a forged XFF chain",
		trustedCount: 0,
		xff:          []string{"1.2.3.4, 5.6.7.8"},
		want:         "203.0.113.9",
	}, {
		name:         "no proxies ignores X-Real-IP",
		trustedCount: 0,
		xRealIP:      "1.2.3.4",
		want:         "203.0.113.9",
	}, {
		name:         "no proxies and no headers uses the peer",
		trustedCount: 0,
		want:         "203.0.113.9",
	}, {
		name:         "one proxy takes the value the proxy appended",
		trustedCount: 1,
		xff:          []string{"1.2.3.4"},
		want:         "1.2.3.4",
	}, {
		// The client controls everything left of the rightmost entry, so with
		// one proxy the rightmost is the only believable one.
		name:         "one proxy takes the rightmost entry, not the client's",
		trustedCount: 1,
		xff:          []string{"1.2.3.4, 5.6.7.8"},
		want:         "5.6.7.8",
	}, {
		// A client cannot escape the count by splitting the chain over
		// several headers; chi merges them before walking.
		name:         "one proxy is not fooled by a split header",
		trustedCount: 1,
		xff:          []string{"1.2.3.4", "5.6.7.8"},
		want:         "5.6.7.8",
	}, {
		// Fail-closed in chi means no IP at all; the fallback to the peer is
		// what keeps the log line useful without making it forgeable.
		name:         "one proxy with no XFF falls back to the peer",
		trustedCount: 1,
		want:         "203.0.113.9",
	}, {
		name:         "two proxies take the second entry from the right",
		trustedCount: 2,
		xff:          []string{"1.2.3.4, 5.6.7.8, 9.9.9.9"},
		want:         "5.6.7.8",
	}, {
		// Chain shorter than the count: chi sets nothing rather than
		// guessing, and the peer is the honest answer.
		name:         "two proxies with a one-entry chain falls back to the peer",
		trustedCount: 2,
		xff:          []string{"1.2.3.4"},
		want:         "203.0.113.9",
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got string
			h := middleware.ClientIPPolicy(tc.trustedCount)(http.HandlerFunc(
				func(_ http.ResponseWriter, r *http.Request) {
					got = middleware.ClientIP(r)
				}))

			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = peer
			for _, v := range tc.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if tc.xRealIP != "" {
				r.Header.Set("X-Real-IP", tc.xRealIP)
			}
			h.ServeHTTP(httptest.NewRecorder(), r)

			if got != tc.want {
				t.Errorf("client IP = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestClientIPPolicyZeroDoesNotPanic pins the reason zero is special-cased:
// chi's ClientIPFromXFFTrustedProxies panics on a count below one, so "no
// proxy" has to be routed to a different middleware entirely.
func TestClientIPPolicyZeroDoesNotPanic(t *testing.T) {
	t.Parallel()

	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("ClientIPPolicy(0) panicked: %v", rec)
		}
	}()
	_ = middleware.ClientIPPolicy(0)
}
