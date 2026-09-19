// Package auth implements the OpenID Connect authorization-code flow with
// PKCE.
//
// The package's job ends at "this is a verified human with this verified email
// address". It deliberately does not create sessions (C3) and does not decide
// who is allowed in (C4): Callback returns an Identity and the caller decides
// what it is worth.
//
// Two structural choices are worth stating up front, because both are easy to
// get wrong in a way that only shows up in production:
//
//   - The issuer is configuration, never a constant. Discovery runs against
//     whatever OAUTH_ISSUER_URL says, which is what lets the tests point the
//     whole flow at an in-process mock provider.
//   - The redirect URI is built from the configured ORIGIN, never from the
//     inbound request. Behind a TLS-terminating reverse proxy r.TLS is nil and
//     r.Host is attacker-controlled, so a request-derived redirect URI would
//     both mismatch the URI registered with the provider and hand an attacker
//     a redirect primitive.
package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Errors returned by Callback. They are sentinels so that C3/C4 can tell a
// hostile callback from a merely unlucky one without string matching.
var (
	// ErrNoFlowCookie means the browser presented no flow cookie, or one that
	// failed authentication or had expired. A stale bookmark looks like this.
	ErrNoFlowCookie = errors.New("auth: missing or invalid flow cookie")

	// ErrStateMismatch means the state in the callback did not match the state
	// in the cookie. This is the CSRF gate.
	ErrStateMismatch = errors.New("auth: state mismatch")

	// ErrNonceMismatch means the ID token's nonce did not match the one minted
	// for this flow, i.e. the token was not issued for this login attempt.
	ErrNonceMismatch = errors.New("auth: nonce mismatch")

	// ErrProviderDenied means the provider redirected back with an error,
	// typically because the user declined consent.
	ErrProviderDenied = errors.New("auth: provider returned an error")

	// ErrNoIDToken means the token response carried no id_token.
	ErrNoIDToken = errors.New("auth: token response contained no id_token")

	// ErrEmailNotVerified means the provider did not assert a verified email.
	// The whitelist is keyed on email address, so an unverified address is not
	// an identity at all.
	ErrEmailNotVerified = errors.New("auth: email address is not verified")

	// ErrNoEmail means the ID token carried no email claim.
	ErrNoEmail = errors.New("auth: id token contained no email claim")

	// ErrNoSubject means the ID token carried no sub claim. OIDC requires one,
	// but go-oidc does not enforce it: Verify copies whatever is there, so an
	// absent sub yields an empty string rather than an error. An empty subject
	// must never reach the database - person.google_sub is UNIQUE, so the
	// first empty-subject row would silently become the account that every
	// later subject-less sign-in matches.
	ErrNoSubject = errors.New("auth: id token contained no sub claim")
)

const (
	// defaultCallbackPath is where the provider sends the browser back.
	defaultCallbackPath = "/auth/callback"

	// defaultLoginPath starts the flow.
	defaultLoginPath = "/auth/login"

	// flowTTL bounds how long a login may sit half-finished. Long enough for a
	// consent screen and a password manager, short enough that a cookie
	// captured from a shared machine is worthless.
	flowTTL = 10 * time.Minute

	// defaultHTTPTimeout bounds every outbound call to the provider:
	// discovery, JWKS fetches and the token exchange.
	//
	// It is not optional. http.DefaultClient has no timeout at all, so with a
	// nil Options.HTTPClient a provider that accepts a connection and then
	// stalls parks the calling goroutine indefinitely -- during Callback that
	// is a request goroutine, holding its connection, with nothing to reap it.
	// Sign-in is on the critical path for every user, so it fails fast
	// instead.
	defaultHTTPTimeout = 10 * time.Second
)

// defaultScopes requests the claims this application needs and nothing else.
// "openid" must come first: some providers key the presence of an ID token off
// the first scope.
var defaultScopes = []string{oidc.ScopeOpenID, "email", "profile"}

// Identity is a verified end user. Every field comes from an ID token whose
// signature, issuer, audience and expiry have already been checked.
type Identity struct {
	// Subject is the provider's stable, opaque user ID (the "sub" claim). It
	// is the only identifier guaranteed not to change; email addresses can be
	// reassigned.
	Subject string

	// Email is the verified email address. EmailVerified is always true on an
	// Identity returned by Callback - the field is kept so callers storing an
	// Identity cannot mistake "we never checked" for "we checked".
	Email         string
	EmailVerified bool

	// Name is the provider's display name, and may be empty.
	Name string
}

// Options configures an Authenticator. Every string field maps to a
// configuration value; nothing here has a provider-specific default.
type Options struct {
	// IssuerURL is the OIDC issuer, e.g. https://accounts.google.com. It is
	// read from OAUTH_ISSUER_URL and is never defaulted.
	IssuerURL string

	ClientID     string
	ClientSecret string

	// Origin is the public, browser-visible origin of this application, e.g.
	// https://pixels.example.com. The redirect URI is derived from it.
	Origin string

	// CookieSecret keys the flow cookie. It must be at least 32 bytes; config
	// already enforces that at startup.
	CookieSecret string

	// LoginPath and CallbackPath default to /auth/login and /auth/callback.
	// CallbackPath must match the redirect URI registered with the provider.
	LoginPath    string
	CallbackPath string

	// Scopes defaults to openid, email and profile.
	Scopes []string

	// HTTPClient, when set, is used for discovery, JWKS fetches and the token
	// exchange. Leaving it nil is fine and is the normal production case: New
	// substitutes a client with defaultHTTPTimeout. It is never
	// http.DefaultClient, which has no timeout.
	HTTPClient *http.Client
}

// Authenticator runs the authorization-code flow for one provider.
type Authenticator struct {
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
	cookies  *flowCookie

	loginPath    string
	callbackPath string
	httpClient   *http.Client
}

// New performs OIDC discovery against opts.IssuerURL and returns a ready
// Authenticator.
//
// Discovery is a network call, so New fails if the provider is unreachable at
// startup. That is deliberate: a server that boots with a broken auth
// configuration only discovers it when the first user tries to sign in.
func New(ctx context.Context, opts Options) (*Authenticator, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}

	// Always a bounded client. Tests pin their own; production gets this one
	// rather than the unbounded http.DefaultClient.
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	ctx = oidc.ClientContext(ctx, httpClient)

	provider, err := oidc.NewProvider(ctx, opts.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("auth: oidc discovery for %q: %w", opts.IssuerURL, err)
	}

	loginPath := orDefault(opts.LoginPath, defaultLoginPath)
	callbackPath := orDefault(opts.CallbackPath, defaultCallbackPath)

	redirectURL, err := callbackURL(opts.Origin, callbackPath)
	if err != nil {
		return nil, err
	}

	scopes := opts.Scopes
	if len(scopes) == 0 {
		scopes = defaultScopes
	}

	// Scope the flow cookie to the directory containing the callback, derived
	// from the redirect URI rather than from CallbackPath, so an ORIGIN that
	// carries a sub-path still gets a path the browser will match.
	cookies, err := newFlowCookie(opts.CookieSecret, secureCookies(opts.Origin), cookiePath(redirectURL))
	if err != nil {
		return nil, err
	}

	return &Authenticator{
		oauth: &oauth2.Config{
			ClientID:     opts.ClientID,
			ClientSecret: opts.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  redirectURL,
			Scopes:       scopes,
		},
		// Verifier checks the signature against the issuer's JWKS (refetching
		// on key rotation), and checks iss, aud and expiry.
		verifier:     provider.Verifier(&oidc.Config{ClientID: opts.ClientID}),
		cookies:      cookies,
		loginPath:    loginPath,
		callbackPath: callbackPath,
		httpClient:   httpClient,
	}, nil
}

func (o Options) validate() error {
	var missing []string
	for _, f := range []struct {
		name  string
		value string
	}{
		{"IssuerURL", o.IssuerURL},
		{"ClientID", o.ClientID},
		{"ClientSecret", o.ClientSecret},
		{"Origin", o.Origin},
		{"CookieSecret", o.CookieSecret},
	} {
		if strings.TrimSpace(f.value) == "" {
			missing = append(missing, f.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("auth: missing required option(s): %s", strings.Join(missing, ", "))
	}
	return nil
}

// LoginPath is where Login should be mounted.
func (a *Authenticator) LoginPath() string { return a.loginPath }

// CallbackPath is where Callback should be mounted. It is the path component
// of RedirectURL, and both must match what the provider has registered.
func (a *Authenticator) CallbackPath() string { return a.callbackPath }

// RedirectURL is the absolute redirect URI sent to the provider, built from
// the configured origin. This exact string must be registered with the
// provider; a mismatch is the single most common cause of a sign-in that works
// locally and fails in production.
func (a *Authenticator) RedirectURL() string { return a.oauth.RedirectURL }

// Login starts the flow: it mints state, a PKCE verifier and a nonce, stores
// them in a short-lived authenticated cookie, and redirects to the provider.
//
// The per-flow secrets live in the cookie rather than in a server-side map
// keyed by state, so the flow survives a restart and works across replicas.
func (a *Authenticator) Login(w http.ResponseWriter, r *http.Request) {
	state, err := randomToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	nonce, err := randomToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	verifier := oauth2.GenerateVerifier()

	if err := a.cookies.set(w, flowState{
		State:     state,
		Verifier:  verifier,
		Nonce:     nonce,
		ExpiresAt: time.Now().Add(flowTTL).Unix(),
	}); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	authURL := a.oauth.AuthCodeURL(
		state,
		oidc.Nonce(nonce),
		// S256 only. "plain" is a no-op against an attacker who can read the
		// authorize request.
		oauth2.S256ChallengeOption(verifier),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// Callback completes the flow and returns a verified identity.
//
// It clears the flow cookie unconditionally: the cookie is single-use, and
// leaving it in place after a failure invites a replay.
func (a *Authenticator) Callback(w http.ResponseWriter, r *http.Request) (*Identity, error) {
	flow, cookieErr := a.cookies.get(r)
	a.cookies.clear(w)
	if cookieErr != nil {
		return nil, ErrNoFlowCookie
	}

	query := r.URL.Query()
	if provErr := query.Get("error"); provErr != "" {
		// Only the machine-readable code is reported. error_description is
		// provider-controlled text and has no business in our logs.
		return nil, fmt.Errorf("%w: %s", ErrProviderDenied, sanitizeCode(provErr))
	}

	// State is compared in constant time and before anything else touches the
	// network: a mismatched callback should cost us nothing.
	if subtle.ConstantTimeCompare([]byte(flow.State), []byte(query.Get("state"))) != 1 {
		return nil, ErrStateMismatch
	}

	code := query.Get("code")
	if code == "" {
		return nil, fmt.Errorf("%w: no code and no error", ErrProviderDenied)
	}

	// oidc.ClientContext sets the same context key oauth2 reads, so this one
	// call bounds both the token exchange below and any JWKS refetch that
	// Verify triggers on key rotation.
	ctx := oidc.ClientContext(r.Context(), a.httpClient)

	token, err := a.oauth.Exchange(ctx, code, oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		// The upstream error body can quote the code back at us, so it is
		// reduced to the OAuth error code before it can reach a log line.
		return nil, fmt.Errorf("auth: code exchange failed: %w", sanitizeExchangeError(err))
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, ErrNoIDToken
	}

	// Verify checks the RS256 signature against the issuer's JWKS and the iss,
	// aud and exp claims. An unverified ID token is just a string.
	idToken, err := a.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("auth: id token verification failed: %w", err)
	}

	if subtle.ConstantTimeCompare([]byte(flow.Nonce), []byte(idToken.Nonce)) != 1 {
		return nil, ErrNonceMismatch
	}

	return identityFrom(idToken)
}

// IdentityFunc receives a verified identity. C3 turns it into a session; C4
// decides whether this person is on the list.
type IdentityFunc func(w http.ResponseWriter, r *http.Request, id *Identity)

// ErrorFunc handles a failed sign-in.
type ErrorFunc func(w http.ResponseWriter, r *http.Request, err error)

// CallbackHandler wraps Callback as an http.Handler. onError may be nil, in
// which case a failure renders a bare 401 with no detail.
func (a *Authenticator) CallbackHandler(onIdentity IdentityFunc, onError ErrorFunc) http.HandlerFunc {
	if onError == nil {
		onError = func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "sign-in failed", http.StatusUnauthorized)
		}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := a.Callback(w, r)
		if err != nil {
			onError(w, r, err)
			return
		}
		onIdentity(w, r, id)
	}
}

// idTokenClaims is the subset of the ID token this application reads.
type idTokenClaims struct {
	Email string `json:"email"`
	// EmailVerified is absent from the struct's zero value for a reason: a
	// provider that omits the claim entirely (mock providers and several real
	// ones do, when the value is false) must read as "not verified".
	EmailVerified boolClaim `json:"email_verified"`
	Name          string    `json:"name"`
}

func identityFrom(idToken *oidc.IDToken) (*Identity, error) {
	var claims idTokenClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("auth: decode id token claims: %w", err)
	}

	subject := strings.TrimSpace(idToken.Subject)
	if subject == "" {
		return nil, ErrNoSubject
	}

	email := strings.TrimSpace(claims.Email)
	if email == "" {
		return nil, ErrNoEmail
	}

	// The hard gate. The whitelist is keyed on email address, so an address the
	// provider will not vouch for cannot be used to look anyone up.
	if !bool(claims.EmailVerified) {
		return nil, ErrEmailNotVerified
	}

	return &Identity{
		Subject:       subject,
		Email:         email,
		EmailVerified: true,
		Name:          strings.TrimSpace(claims.Name),
	}, nil
}

// boolClaim decodes a claim that is a JSON boolean for most providers but a
// string for some. Left as a plain bool, a provider sending "true" would make
// json.Unmarshal fail, and a caller that ignored that error would silently
// treat every user as unverified - or, worse, a caller that ignored it in the
// other direction would treat everyone as verified. Being explicit here keeps
// the gate honest.
type boolClaim bool

func (b *boolClaim) UnmarshalJSON(data []byte) error {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("email_verified: %w", err)
	}
	switch v := raw.(type) {
	case bool:
		*b = boolClaim(v)
	case string:
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("email_verified: %q is not a boolean", v)
		}
		*b = boolClaim(parsed)
	case nil:
		*b = false
	default:
		return fmt.Errorf("email_verified: unexpected JSON type %T", raw)
	}
	return nil
}

// callbackURL builds the redirect URI from the configured origin. It never
// looks at the inbound request; see the package comment.
func callbackURL(origin, path string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(origin))
	if err != nil {
		return "", fmt.Errorf("auth: ORIGIN is not a valid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("auth: ORIGIN must be an absolute http or https URL, got %q", origin)
	}
	if u.Host == "" {
		return "", fmt.Errorf("auth: ORIGIN must include a host, got %q", origin)
	}
	// JoinPath so an origin that already carries a sub-path keeps it.
	joined := u.JoinPath(path)
	joined.RawQuery = ""
	joined.Fragment = ""
	return joined.String(), nil
}

// secureCookies mirrors the scheme the browser sees, which is the origin's
// scheme. It is deliberately not derived from r.TLS: behind a TLS-terminating
// proxy that is always nil even though the browser is on HTTPS, and a flow
// cookie sent without Secure would travel in clear text on the next plain-HTTP
// request to the same host.
func secureCookies(origin string) bool {
	u, err := url.Parse(strings.TrimSpace(origin))
	if err != nil {
		return true
	}
	return u.Scheme != "http"
}

// sanitizeExchangeError reduces a token-endpoint failure to its OAuth error
// code. The provider's error_description can quote the authorization code
// back at us, and this error is destined for a log.
func sanitizeExchangeError(err error) error {
	var retrieve *oauth2.RetrieveError
	if !errors.As(err, &retrieve) {
		return errors.New("token endpoint request failed")
	}
	if retrieve.ErrorCode != "" {
		return fmt.Errorf("token endpoint returned %s", sanitizeCode(retrieve.ErrorCode))
	}
	status := 0
	if retrieve.Response != nil {
		status = retrieve.Response.StatusCode
	}
	return fmt.Errorf("token endpoint returned HTTP %d", status)
}

// sanitizeCode keeps a provider-supplied identifier to the character set an
// OAuth error code is allowed to use, so nothing provider-controlled reaches a
// log line unfiltered.
func sanitizeCode(s string) string {
	const maxLen = 64
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			return r
		default:
			return '.'
		}
	}, s)
}

// cookiePath returns the directory of the redirect URI's path, e.g.
// "/auth" for "https://host/auth/callback". Scoping the flow cookie there
// keeps it off every other request in the application.
func cookiePath(redirectURL string) string {
	u, err := url.Parse(redirectURL)
	if err != nil {
		return "/"
	}
	dir := path.Dir(u.EscapedPath())
	if dir == "" || dir == "." {
		return "/"
	}
	return dir
}

func orDefault(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
