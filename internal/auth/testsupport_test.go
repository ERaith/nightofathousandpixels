package auth

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/oauth2-proxy/mockoidc"
)

// testCookieSecret is 32 bytes, the minimum config enforces.
const testCookieSecret = "0123456789abcdef0123456789abcdef"

// testOrigin is deliberately not the address anything actually listens on.
// The redirect URI must be built from it and from nothing else, so if the
// implementation ever reaches for r.Host these tests stop matching.
const testOrigin = "https://pixels.example.test"

// startMock runs an in-process OIDC provider that serves real discovery,
// authorize, token, userinfo and JWKS endpoints and signs real RS256 tokens.
func startMock(t *testing.T, middleware ...func(http.Handler) http.Handler) *mockoidc.MockOIDC {
	t.Helper()

	m, err := mockoidc.NewServer(nil)
	if err != nil {
		t.Fatalf("mockoidc.NewServer: %v", err)
	}
	for _, mw := range middleware {
		if err := m.AddMiddleware(mw); err != nil {
			t.Fatalf("mockoidc.AddMiddleware: %v", err)
		}
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	if err := m.Start(ln, nil); err != nil {
		t.Fatalf("mockoidc.Start: %v", err)
	}
	t.Cleanup(func() { _ = m.Shutdown() })

	return m
}

// newAuthenticator wires an Authenticator to a mock provider. Note that the
// issuer comes from the mock: nothing in the package knows about Google.
func newAuthenticator(t *testing.T, m *mockoidc.MockOIDC, origin string) *Authenticator {
	t.Helper()

	a, err := New(context.Background(), Options{
		IssuerURL:    m.Issuer(),
		ClientID:     m.ClientID,
		ClientSecret: m.ClientSecret,
		Origin:       origin,
		CookieSecret: testCookieSecret,
	})
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	return a
}

// noRedirect is a client that hands back the 302 instead of following it, so
// tests can inspect and tamper with what the provider sent.
func noRedirect() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// begin runs the login handler and returns the authorize URL the browser would
// be sent to along with the flow cookie that was set alongside it.
func begin(t *testing.T, a *Authenticator) (*url.URL, *http.Cookie) {
	t.Helper()

	rec := httptest.NewRecorder()
	a.Login(rec, httptest.NewRequest(http.MethodGet, a.LoginPath(), nil))

	resp := rec.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	loc, err := resp.Location()
	if err != nil {
		t.Fatalf("login Location: %v", err)
	}

	cookie := findCookie(resp.Cookies(), flowCookieName)
	if cookie == nil {
		t.Fatal("login did not set a flow cookie")
	}
	return loc, cookie
}

// authorize drives the provider's authorization endpoint and returns the
// callback URL it redirects the browser to.
func authorize(t *testing.T, authURL *url.URL) *url.URL {
	t.Helper()

	resp, err := noRedirect().Get(authURL.String())
	if err != nil {
		t.Fatalf("GET authorize: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("authorize status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	loc, err := resp.Location()
	if err != nil {
		t.Fatalf("authorize Location: %v", err)
	}
	return loc
}

// callback invokes the callback with a hand-built request, which is how the
// tampering tests get to change exactly one thing at a time.
func callback(t *testing.T, a *Authenticator, cbURL *url.URL, cookies ...*http.Cookie) (*Identity, error) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, cbURL.String(), nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	return a.Callback(httptest.NewRecorder(), req)
}

// openFlowCookie decrypts a flow cookie so a test can see, and then change,
// the state and verifier the server minted.
func openFlowCookie(t *testing.T, a *Authenticator, c *http.Cookie) flowState {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, a.CallbackPath(), nil)
	req.AddCookie(c)

	state, err := a.cookies.get(req)
	if err != nil {
		t.Fatalf("open flow cookie: %v", err)
	}
	return state
}

// sealFlowCookie produces a cookie the server will accept, carrying whatever
// the test wants inside it. This is how a "tampered verifier" is simulated
// without also breaking the cookie's own integrity check - otherwise the
// cookie would be rejected before the verifier ever reached the provider.
func sealFlowCookie(t *testing.T, a *Authenticator, state flowState) *http.Cookie {
	t.Helper()

	rec := httptest.NewRecorder()
	if err := a.cookies.set(rec, state); err != nil {
		t.Fatalf("seal flow cookie: %v", err)
	}
	c := findCookie(rec.Result().Cookies(), flowCookieName)
	if c == nil {
		t.Fatal("seal flow cookie: no cookie written")
	}
	return c
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// withQuery returns a copy of u with one query parameter replaced.
func withQuery(u *url.URL, key, value string) *url.URL {
	out := *u
	q := out.Query()
	q.Set(key, value)
	out.RawQuery = q.Encode()
	return &out
}

// claimUser is a mock provider user whose ID token claims are written out
// literally.
//
// mockoidc's own MockUser tags email_verified with `omitempty`, so setting it
// to false omits the claim entirely. "Absent" and "present and false" are
// different bugs, and the gate has to reject both, so the tests need a user
// that can produce either shape - and a string-valued one, which some real
// providers emit.
type claimUser struct {
	subject string
	email   string
	// emailVerified is written verbatim when non-nil and omitted when nil.
	emailVerified any
	name          string
}

func (u claimUser) ID() string { return u.subject }

func (u claimUser) Userinfo([]string) ([]byte, error) {
	return json.Marshal(map[string]any{"email": u.email, "name": u.name})
}

func (u claimUser) Claims(_ []string, base *mockoidc.IDTokenClaims) (jwt.Claims, error) {
	// Round-trip the registered claims so the token keeps the iss, aud, exp,
	// jti and nonce the provider expects; only the profile claims are ours.
	raw, err := json.Marshal(base)
	if err != nil {
		return nil, err
	}
	claims := jwt.MapClaims{}
	if err := json.Unmarshal(raw, &claims); err != nil {
		return nil, err
	}

	claims["email"] = u.email
	if u.emailVerified != nil {
		claims["email_verified"] = u.emailVerified
	}
	if u.name != "" {
		claims["name"] = u.name
	}
	return claims, nil
}

// forgeIDTokenSignature rewrites the token endpoint's id_token with one
// carrying byte-identical claims signed by a key the issuer never published.
// Everything else about the response - and about the flow - stays honest, so a
// test that passes here can only be passing because the signature was checked.
func forgeIDTokenSignature(t *testing.T, evil *mockoidc.Keypair) func(http.Handler) http.Handler {
	t.Helper()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != mockoidc.TokenEndpoint {
				next.ServeHTTP(w, r)
				return
			}

			rec := httptest.NewRecorder()
			next.ServeHTTP(rec, r)

			body := map[string]any{}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err == nil {
				if raw, _ := body["id_token"].(string); raw != "" {
					claims := jwt.MapClaims{}
					if _, _, err := jwt.NewParser().ParseUnverified(raw, claims); err != nil {
						t.Errorf("parse honest id_token: %v", err)
					}
					forged, err := evil.SignJWT(claims)
					if err != nil {
						t.Errorf("sign forged id_token: %v", err)
					}
					body["id_token"] = forged
				}
			}

			out, err := json.Marshal(body)
			if err != nil {
				t.Errorf("marshal forged token response: %v", err)
			}
			for k, values := range rec.Header() {
				for _, v := range values {
					w.Header().Add(k, v)
				}
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(out)))
			w.WriteHeader(rec.Code)
			if _, err := w.Write(out); err != nil {
				t.Errorf("write forged token response: %v", err)
			}
		})
	}
}
