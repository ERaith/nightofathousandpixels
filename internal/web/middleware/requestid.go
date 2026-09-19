package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// RequestIDHeader is the header read on the way in and written on the way out.
const RequestIDHeader = "X-Request-Id"

const (
	// requestIDBytes is the length of a generated ID before hex encoding. 16
	// bytes is 128 bits, which is collision-free for any traffic this site
	// will ever see and still short enough to paste into a chat message.
	requestIDBytes = 16

	// maxInboundRequestIDLen caps an ID we did not generate. Without a cap a
	// client could push a megabyte of text into every log line.
	maxInboundRequestIDLen = 64
)

// requestIDKey is the context key for the request ID. It is a private type so
// that nothing outside this package can collide with it.
type requestIDKey struct{}

// RequestID gives every request an identifier and puts it on the response.
//
// An inbound X-Request-Id is honoured so that a trace started by Nginx Proxy
// Manager, or by a client debugging a report, survives into these logs. It is
// sanitised first: the value is untrusted, and it is about to be echoed in a
// response header and copied into every log line for the request.
//
// chi ships middleware.RequestID, but it ignores any inbound value and never
// writes the ID back to the client, which are the two things that make an ID
// useful for correlating a user's report with a log line. The generated ID is
// also stored under chi's own context key, so middleware.GetReqID and
// anything built on it agree with this package rather than inventing a second
// identifier for the same request.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := sanitiseRequestID(r.Header.Get(RequestIDHeader))
		if id == "" {
			id = newRequestID()
		}

		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		ctx = context.WithValue(ctx, chimw.RequestIDKey, id)

		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetRequestID returns the request ID stored by RequestID, or "" if the
// middleware did not run.
func GetRequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// sanitiseRequestID reduces an inbound ID to something safe to echo and to
// log: at most maxInboundRequestIDLen characters drawn from an alphabet with
// no CR, LF, quote or control character in it. Anything else in the value is
// dropped rather than replaced, and a value that is left empty by that is
// treated as absent so a fresh ID is generated.
func sanitiseRequestID(raw string) string {
	if len(raw) > maxInboundRequestIDLen {
		raw = raw[:maxInboundRequestIDLen]
	}

	out := make([]byte, 0, len(raw))
	for i := range len(raw) {
		c := raw[i]
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '-', c == '_', c == '.':
			out = append(out, c)
		}
	}
	return string(out)
}

// newRequestID returns a fresh random ID. crypto/rand.Read does not fail on
// any platform Go supports since 1.24 -- it panics rather than returning an
// error it cannot recover from -- so there is no error path to handle here.
func newRequestID() string {
	b := make([]byte, requestIDBytes)
	rand.Read(b) //nolint:errcheck // crypto/rand.Read never returns an error; it panics instead.
	return hex.EncodeToString(b)
}
