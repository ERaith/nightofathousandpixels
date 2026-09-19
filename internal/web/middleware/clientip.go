package middleware

import (
	"net/http"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// ClientIPPolicy returns the middleware that decides what "the client's IP"
// means for this deployment, given the number of reverse proxies in front of
// the server.
//
// This exists instead of chi's middleware.RealIP, which is DEPRECATED in chi
// v5.3.2 for an IP-spoofing vulnerability (GHSA-3fxj-6jh8-hvhx and two
// siblings): it rewrites r.RemoteAddr from X-Forwarded-For unconditionally,
// so on a server reachable without a proxy any client can name its own IP and
// every log line, rate limit and audit record downstream believes it.
//
// Doing nothing at all is not the alternative either. In production this app
// sits behind Nginx Proxy Manager in Docker, where r.RemoteAddr is the bridge
// gateway for every request that has ever arrived -- one address, forever,
// which is the same as having no address at all.
//
// So the policy is chosen from configuration rather than compiled in:
//
//	trustedProxyCount == 0  direct exposure. The peer address is the client;
//	                        X-Forwarded-For is attacker-controlled input and
//	                        is ignored completely.
//	trustedProxyCount >= 1  behind N proxies. The client is the Nth entry from
//	                        the right of X-Forwarded-For, the last one that N
//	                        trusted hops had the chance to append.
//
// The count is configuration and not a constant because both wrong answers
// are silent. Too low and a client can spoof its IP past the proxy's own
// entry; too high and the address is simply absent. It is 1 behind Nginx
// Proxy Manager and 0 when running the binary on a laptop.
//
// Note that ClientIPFromXFFTrustedProxies panics on a count below 1, which is
// exactly why zero is routed to ClientIPFromRemoteAddr rather than passed
// through: "no proxy" is a real deployment, not a degenerate argument.
func ClientIPPolicy(trustedProxyCount int) func(http.Handler) http.Handler {
	if trustedProxyCount < 1 {
		return chimw.ClientIPFromRemoteAddr
	}
	return chimw.ClientIPFromXFFTrustedProxies(trustedProxyCount)
}

// ClientIP returns the client IP for r, as established by ClientIPPolicy.
//
// It falls back to the peer address when the policy produced nothing, which
// happens when the server is configured for N proxies and a request arrives
// with a shorter X-Forwarded-For chain than that -- chi fails closed there and
// sets no IP. The fallback is safe precisely because r.RemoteAddr is the one
// value on the request that the client cannot choose: the worst case is that
// a log line names the proxy instead of the client, never that it names an
// address the client made up.
func ClientIP(r *http.Request) string {
	if ip := chimw.GetClientIP(r.Context()); ip != "" {
		return ip
	}
	return remoteHost(r)
}
