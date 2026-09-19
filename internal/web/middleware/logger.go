// Package middleware holds the HTTP middleware shared by every route: request
// IDs, structured request logging, panic recovery, and the client-IP policy
// that the first three depend on.
//
// Ordering matters and is not arbitrary. The chain is:
//
//	ClientIP -> RequestID -> Recoverer -> RequestLogger -> ...routes
//
// ClientIP first so that everything downstream can read it; RequestID before
// Recoverer and RequestLogger so both can name the request in their output;
// Recoverer outside RequestLogger so that a panic is still logged as a 500
// with a duration rather than vanishing.
package middleware

import (
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// NewLogger returns the process logger: JSON, one object per line, on stdout.
//
// Stdout rather than stderr and JSON rather than text are both deliberate.
// The server runs as a container behind Docker, where stdout is the log
// stream, and Dozzle parses JSON lines into browsable fields; a text handler
// would render as an undifferentiated string.
func NewLogger(level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource:   false,
		Level:       level,
		ReplaceAttr: nil,
	}))
}

// RequestLogger logs one line per completed request.
//
// The fields are chosen to be the ones worth having at 3am and nothing more.
// In particular the log records r.URL.Path and never RawQuery: the OAuth
// callback carries `code` and `state` in the query string, and a log file is
// not a place to keep credentials. Headers are never logged for the same
// reason -- Cookie and Authorization live there.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// WrapResponseWriter is chi's, and is used rather than a local
			// type because it preserves Flush, Hijack and ReadFrom, which a
			// naive wrapper silently drops.
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()

			defer func() {
				logger.LogAttrs(r.Context(), slog.LevelInfo, "http request",
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Int("status", ww.Status()),
					slog.Int64("duration_ms", time.Since(start).Milliseconds()),
					slog.Int("bytes", ww.BytesWritten()),
					slog.String("request_id", GetRequestID(r.Context())),
					slog.String("client_ip", ClientIP(r)),
					slog.String("user_agent", r.UserAgent()),
				)
			}()

			next.ServeHTTP(ww, r)
		})
	}
}

// Recoverer turns a panic into a 500 and a log line.
//
// chi ships one, but it writes a plain-text stack trace to stderr, which in a
// container means the one event you most want to find is the one line Dozzle
// cannot parse. This one emits the same information as JSON on the same
// stream as every other log line.
func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				// http.ErrAbortHandler is the documented way for a handler to
				// abandon a response; it is not an error and re-panicking is
				// what net/http expects.
				if rec == http.ErrAbortHandler { //nolint:errorlint // sentinel is panicked as a value, not wrapped
					panic(rec)
				}

				logger.LogAttrs(r.Context(), slog.LevelError, "panic recovered",
					slog.Any("panic", rec),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.String("request_id", GetRequestID(r.Context())),
					slog.String("client_ip", ClientIP(r)),
					slog.String("stack", string(debug.Stack())),
				)
				w.WriteHeader(http.StatusInternalServerError)
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// remoteHost is the IP half of r.RemoteAddr, or the whole value if it does not
// carry a port (which happens in httptest).
func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
