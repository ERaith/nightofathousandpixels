// Command mockoidcd is the OpenID Connect provider that local development and
// the end-to-end suite sign in against.
//
// It exists because neither a developer nor Playwright can sign in to Google:
// headless browsers get flagged, and driving somebody's real account would be
// fragile even if they were not. The usual answer to that is a test-only
// bypass route in the application - a handler that mints a session for whoever
// asks. This is not that, on purpose: a bypass route is a hole in production
// that happens to be switched off, and the failure mode of leaving it switched
// on is silent and total.
//
// What runs here instead is a real OIDC provider. OAUTH_ISSUER_URL is already
// configuration rather than a constant (see internal/config), so pointing it
// at this process changes the issuer and nothing else: the application still
// runs discovery, still fetches the JWKS, still sends a PKCE S256 challenge,
// still exchanges the code, and still verifies an RS256 signature over the
// issuer, audience, expiry and nonce. The bytes on the wire are the shape
// Google puts there, because oauth2-proxy/mockoidc produces them.
//
// The consequence worth stating plainly: there is no skipAuth() anywhere to
// leave enabled. The worst case of a misconfigured OAUTH_ISSUER_URL in
// production is that sign-in stops working, not that it is bypassed.
//
// Two ways to choose who signs in:
//
//   - a person clicks one of the identities on the authorize page (picker.go);
//   - a test POSTs one to /control/user before driving the flow.
//
// Both do the same thing -- push a user onto mockoidc's queue, which its
// authorize endpoint pops. Neither grants anything: they decide which address
// the provider will sign a token for, and the application still has to accept
// that token and still has to decide whether that address is on the season's
// whitelist.
//
// This command is never built into the shipped image. It is its own Dockerfile
// stage, `--target final` does not reach it, and `go list -deps ./cmd/server`
// does not mention mockoidc.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/oauth2-proxy/mockoidc"
)

const (
	// issuerSuffix is fixed inside mockoidc (its IssuerBase), so the issuer
	// this process can serve is always <public base>/oidc. Deriving the public
	// base by stripping it off OAUTH_ISSUER_URL - rather than taking the base
	// as a second setting - means the provider and the application cannot
	// disagree about the issuer string, which is the one value an OIDC client
	// compares byte for byte.
	issuerSuffix = "/oidc"

	defaultListen = ":9000"

	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 5 * time.Second
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("mockoidcd: %v", err)
	}
}

func run() error {
	issuer := strings.TrimSpace(os.Getenv("OAUTH_ISSUER_URL"))
	clientID := strings.TrimSpace(os.Getenv("OAUTH_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("OAUTH_CLIENT_SECRET"))
	if issuer == "" || clientID == "" || clientSecret == "" {
		return errors.New("OAUTH_ISSUER_URL, OAUTH_CLIENT_ID and OAUTH_CLIENT_SECRET must all be set, and must match what the application is given")
	}

	users, err := loadDevUsers()
	if err != nil {
		return err
	}

	publicHost, err := publicHostFrom(issuer)
	if err != nil {
		return err
	}

	m, err := mockoidc.NewServer(nil)
	if err != nil {
		return fmt.Errorf("new mock provider: %w", err)
	}
	m.ClientID = clientID
	m.ClientSecret = clientSecret
	// S256 only, matching what internal/auth actually sends. Advertising
	// "plain" would let a regression to plain PKCE pass unnoticed here even
	// though Google would reject it.
	m.CodeChallengeMethodsSupported = []string{"S256"}

	// mockoidc derives every URL it advertises - issuer, authorize, token,
	// userinfo, jwks - from m.Server.Addr. Setting it by hand is what lets the
	// address the provider ADVERTISES differ from the address it LISTENS on.
	// Under `make dev` the two are the same, because the application runs on
	// the host and reaches this container through its published port, exactly
	// as the browser does. The end-to-end stack is where they differ, and
	// there the application is put in this process's network namespace so
	// "localhost:<port>" means one socket for both.
	//
	// This server is not started by mockoidc.Start: the mux below is ours, so
	// that the picker and the control endpoint can sit next to the protocol
	// endpoints. Every protocol handler on it is mockoidc's own exported
	// method, unmodified.
	m.Server = &http.Server{Addr: publicHost, ReadHeaderTimeout: readHeaderTimeout}

	if got := m.Issuer(); got != issuer {
		return fmt.Errorf("OAUTH_ISSUER_URL is %q but this provider can only serve %q - the issuer must be the provider's public base URL plus %q", issuer, got, issuerSuffix)
	}

	mux := http.NewServeMux()
	mux.HandleFunc(mockoidc.DiscoveryEndpoint, m.Discovery)
	// The picker wraps authorize; everything else is mockoidc's handler
	// mounted directly.
	mux.Handle(mockoidc.AuthorizationEndpoint, pickUser(m, users))
	mux.HandleFunc(mockoidc.TokenEndpoint, m.Token)
	mux.HandleFunc(mockoidc.UserinfoEndpoint, m.Userinfo)
	mux.HandleFunc(mockoidc.JWKSEndpoint, m.JWKS)
	mux.HandleFunc("/control/user", queueUser(m))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})

	listen := strings.TrimSpace(os.Getenv("MOCKOIDC_LISTEN"))
	if listen == "" {
		listen = defaultListen
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("listen on %q: %w", listen, err)
	}

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: readHeaderTimeout}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("mockoidcd: listening on %s, advertising issuer %s, client_id %s", ln.Addr(), m.Issuer(), m.ClientID)
		for _, u := range users {
			log.Printf("mockoidcd: offering %s (%s) sub=%s", u.Email, u.Note, u.Subject())
		}
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return <-serveErr
}

// publicHostFrom turns the configured issuer into the host:port mockoidc
// should advertise.
func publicHostFrom(issuer string) (string, error) {
	u, err := url.Parse(issuer)
	if err != nil {
		return "", fmt.Errorf("OAUTH_ISSUER_URL is not a valid URL: %w", err)
	}
	if u.Scheme != "http" {
		return "", fmt.Errorf("OAUTH_ISSUER_URL must be an http URL for the mock provider, got %q (this process serves no TLS)", issuer)
	}
	if u.Host == "" {
		return "", fmt.Errorf("OAUTH_ISSUER_URL must include a host, got %q", issuer)
	}
	if !strings.HasSuffix(u.Path, issuerSuffix) {
		return "", fmt.Errorf("OAUTH_ISSUER_URL must end in %q - mockoidc serves its endpoints under that path and cannot be told otherwise, got %q", issuerSuffix, issuer)
	}
	return u.Host, nil
}

// userRequest is the body of POST /control/user.
type userRequest struct {
	Subject string `json:"subject"`
	Email   string `json:"email"`
	Name    string `json:"name"`

	// EmailVerified is a pointer so that omitting it means "verified", which
	// is the case every test wants, while sending false is still a way to ask
	// for the unverified-address rejection path.
	EmailVerified *bool `json:"email_verified"`
}

// queueUser lets a test say who the next sign-in is, without a browser.
//
// This endpoint is on the PROVIDER, not on the application. It grants nothing:
// pushing a user here only decides which email address the provider will sign
// a token for. It is the mock equivalent of choosing which Google account to
// click.
func queueUser(m *mockoidc.MockOIDC) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req userRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("bad request body: %v", err), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Email) == "" || strings.TrimSpace(req.Subject) == "" {
			http.Error(w, "subject and email are both required", http.StatusBadRequest)
			return
		}

		verified := true
		if req.EmailVerified != nil {
			verified = *req.EmailVerified
		}

		m.QueueUser(&namedUser{
			MockUser: &mockoidc.MockUser{
				Subject:           req.Subject,
				Email:             req.Email,
				EmailVerified:     verified,
				PreferredUsername: req.Email,
			},
			Name: req.Name,
		})
		w.WriteHeader(http.StatusNoContent)
	}
}

// namedUser adds the "name" claim to mockoidc's user. Google sends it and
// internal/auth reads it into Identity.Name; without it the mock would be
// quietly missing a claim the application uses, and a page that greets someone
// by name would pass its test with an empty greeting.
type namedUser struct {
	*mockoidc.MockUser
	Name string
}

func (u *namedUser) Claims(scope []string, base *mockoidc.IDTokenClaims) (jwt.Claims, error) {
	inner, err := u.MockUser.Claims(scope, base)
	if err != nil {
		return nil, err
	}
	if u.Name == "" || !hasScope(scope, "profile") {
		return inner, nil
	}
	return &namedClaims{Claims: inner, name: u.Name}, nil
}

func hasScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want {
			return true
		}
	}
	return false
}

// namedClaims is the wrapped claim set. jwt only marshals it, but the
// interface it has to satisfy is about validation, so the accessors are
// delegated verbatim by embedding.
type namedClaims struct {
	jwt.Claims
	name string
}

func (c *namedClaims) MarshalJSON() ([]byte, error) {
	raw, err := json.Marshal(c.Claims)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	m["name"] = c.name
	return json.Marshal(m)
}
