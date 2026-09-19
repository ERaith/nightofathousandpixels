package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/oauth2-proxy/mockoidc"
)

// TestFlowEndToEnd runs the whole thing the way a browser does: a real HTTP
// client with a real cookie jar follows a real redirect chain through a real
// OIDC provider, and nothing in the test hand-feeds the implementation a
// value. If PKCE, state, the cookie or the redirect URI were wrong, this would
// not reach the success handler.
func TestFlowEndToEnd(t *testing.T) {
	t.Parallel()

	m := startMock(t)
	m.QueueUser(claimUser{
		subject:       "sub-12345",
		email:         "friend@example.test",
		emailVerified: true,
		name:          "A Friend",
	})

	mux := http.NewServeMux()
	app := httptest.NewServer(mux)
	t.Cleanup(app.Close)

	// The origin is the app's own URL here, so the provider's redirect lands
	// back on a server that is really listening.
	a := newAuthenticator(t, m, app.URL)

	var (
		gotIdentity *Identity
		gotErr      error
	)
	mux.HandleFunc(a.LoginPath(), a.Login)
	mux.HandleFunc(a.CallbackPath(), a.CallbackHandler(
		func(w http.ResponseWriter, _ *http.Request, id *Identity) {
			gotIdentity = id
			w.WriteHeader(http.StatusOK)
		},
		func(w http.ResponseWriter, _ *http.Request, err error) {
			gotErr = err
			w.WriteHeader(http.StatusUnauthorized)
		},
	))

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}

	resp, err := client.Get(app.URL + a.LoginPath())
	if err != nil {
		t.Fatalf("GET login: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("final status = %d, want %d (callback error: %v)", resp.StatusCode, http.StatusOK, gotErr)
	}
	if gotErr != nil {
		t.Fatalf("callback returned error: %v", gotErr)
	}
	if gotIdentity == nil {
		t.Fatal("callback produced no identity")
	}

	want := Identity{
		Subject:       "sub-12345",
		Email:         "friend@example.test",
		EmailVerified: true,
		Name:          "A Friend",
	}
	if *gotIdentity != want {
		t.Errorf("identity = %+v, want %+v", *gotIdentity, want)
	}

	// The flow cookie is single-use; the callback must have cleared it.
	appURL, err := url.Parse(app.URL)
	if err != nil {
		t.Fatalf("parse app URL: %v", err)
	}
	if c := findCookie(jar.Cookies(appURL.JoinPath(a.CallbackPath())), flowCookieName); c != nil {
		t.Errorf("flow cookie survived the callback: %q", c.Value)
	}
}

// TestRedirectURIComesFromOriginNotRequest is the reverse-proxy requirement.
// The request below is plain HTTP with an attacker-chosen Host, exactly what
// the Go process sees behind a TLS-terminating proxy. The redirect URI must
// still be the configured public origin.
func TestRedirectURIComesFromOriginNotRequest(t *testing.T) {
	t.Parallel()

	m := startMock(t)
	a := newAuthenticator(t, m, testOrigin)

	req := httptest.NewRequest(http.MethodGet, a.LoginPath(), nil)
	req.Host = "evil.example.com"
	req.TLS = nil

	rec := httptest.NewRecorder()
	a.Login(rec, req)

	loc, err := rec.Result().Location()
	if err != nil {
		t.Fatalf("Location: %v", err)
	}

	const wantRedirect = testOrigin + "/auth/callback"
	if got := loc.Query().Get("redirect_uri"); got != wantRedirect {
		t.Errorf("redirect_uri = %q, want %q", got, wantRedirect)
	}
	if got := a.RedirectURL(); got != wantRedirect {
		t.Errorf("RedirectURL() = %q, want %q", got, wantRedirect)
	}
	if strings.Contains(loc.String(), "evil.example.com") {
		t.Errorf("authorize URL leaked the request Host: %s", loc)
	}
}

// TestAuthorizeRequestUsesS256PKCE proves the challenge is actually derived
// from the verifier in the cookie with SHA-256, rather than being a "plain"
// challenge or an unrelated random string.
func TestAuthorizeRequestUsesS256PKCE(t *testing.T) {
	t.Parallel()

	m := startMock(t)
	a := newAuthenticator(t, m, testOrigin)

	authURL, cookie := begin(t, a)
	flow := openFlowCookie(t, a, cookie)

	q := authURL.Query()
	if got := q.Get("code_challenge_method"); got != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", got)
	}
	sum := sha256.Sum256([]byte(flow.Verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if got := q.Get("code_challenge"); got != want {
		t.Errorf("code_challenge = %q, want S256 of the cookie's verifier (%q)", got, want)
	}
	if got := q.Get("state"); got != flow.State {
		t.Errorf("state = %q, want the cookie's state %q", got, flow.State)
	}
	if q.Get("nonce") != flow.Nonce {
		t.Error("nonce in authorize URL does not match the cookie")
	}
	// The verifier itself must never leave the server in the authorize request.
	if strings.Contains(authURL.RawQuery, flow.Verifier) {
		t.Error("authorize URL contains the raw code_verifier")
	}

	if !cookie.HttpOnly {
		t.Error("flow cookie is not HttpOnly")
	}
	if !cookie.Secure {
		t.Error("flow cookie is not Secure for an https origin")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("flow cookie SameSite = %v, want Lax", cookie.SameSite)
	}
	if cookie.MaxAge <= 0 || cookie.MaxAge > int(flowTTL.Seconds()) {
		t.Errorf("flow cookie MaxAge = %d, want a short positive TTL", cookie.MaxAge)
	}
}

// TestStateMismatchRejected is the CSRF gate.
//
// Each subtest gets its own provider. mockoidc's SessionStore is a plain map
// with no mutex, so two parallel subtests sharing one server race inside the
// library - not in our code, but it fails the build all the same.
func TestStateMismatchRejected(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"different state": "not-the-state-we-minted",
		"empty state":     "",
		// A state that is a prefix of the real one, in case anything ever
		// reaches for strings.HasPrefix.
		"truncated state": "",
	}

	for name, tamperedState := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			m := startMock(t)
			a := newAuthenticator(t, m, testOrigin)

			authURL, cookie := begin(t, a)
			cbURL := authorize(t, authURL)

			state := tamperedState
			if name == "truncated state" {
				real := openFlowCookie(t, a, cookie).State
				state = real[:len(real)-1]
			}

			id, err := callback(t, a, withQuery(cbURL, "state", state), cookie)
			if !errors.Is(err, ErrStateMismatch) {
				t.Fatalf("err = %v, want ErrStateMismatch", err)
			}
			if id != nil {
				t.Errorf("identity returned despite state mismatch: %+v", id)
			}
		})
	}
}

// TestUntamperedCallbackSucceeds is the control for the tampering tests above
// and below: the same setup, with nothing changed, must reach an identity.
// Without it, a rejection test could be passing for an unrelated reason.
func TestUntamperedCallbackSucceeds(t *testing.T) {
	t.Parallel()

	m := startMock(t)
	m.QueueUser(claimUser{subject: "sub-control", email: "control@example.test", emailVerified: true})
	a := newAuthenticator(t, m, testOrigin)

	authURL, cookie := begin(t, a)
	id, err := callback(t, a, authorize(t, authURL), cookie)
	if err != nil {
		t.Fatalf("control run failed: %v", err)
	}
	if id.Subject != "sub-control" || id.Email != "control@example.test" {
		t.Errorf("identity = %+v", id)
	}
}

// TestStateFromADifferentFlowRejected is the realistic version of the attack:
// a valid, provider-issued callback from one login pointed at another login's
// cookie. Both halves are individually genuine; only the pairing is wrong.
func TestStateFromADifferentFlowRejected(t *testing.T) {
	t.Parallel()

	m := startMock(t)
	a := newAuthenticator(t, m, testOrigin)

	_, victimCookie := begin(t, a)
	attackerAuth, _ := begin(t, a)
	attackerCallback := authorize(t, attackerAuth)

	id, err := callback(t, a, attackerCallback, victimCookie)
	if !errors.Is(err, ErrStateMismatch) {
		t.Fatalf("err = %v, want ErrStateMismatch", err)
	}
	if id != nil {
		t.Errorf("identity returned for a cross-flow callback: %+v", id)
	}
}

// TestTamperedCodeVerifierRejected swaps the verifier for a different, valid
// one after the challenge has already been sent to the provider. This is the
// PKCE gate: without it, an intercepted authorization code would be redeemable
// by whoever holds it.
func TestTamperedCodeVerifierRejected(t *testing.T) {
	t.Parallel()

	m := startMock(t)
	a := newAuthenticator(t, m, testOrigin)

	authURL, cookie := begin(t, a)
	flow := openFlowCookie(t, a, cookie)
	cbURL := authorize(t, authURL)

	// Keep the state so the CSRF check passes and the failure can only come
	// from PKCE. Everything else is untouched.
	swapped := flow
	swapped.Verifier = "a-different-but-perfectly-well-formed-code-verifier-0123456789"
	if swapped.Verifier == flow.Verifier {
		t.Fatal("test bug: verifier was not changed")
	}

	id, err := callback(t, a, cbURL, sealFlowCookie(t, a, swapped))
	if err == nil {
		t.Fatalf("tampered code_verifier was accepted, identity = %+v", id)
	}
	if id != nil {
		t.Errorf("identity returned despite PKCE failure: %+v", id)
	}
	if !strings.Contains(err.Error(), "invalid_grant") {
		t.Errorf("err = %v, want the provider's invalid_grant rejection", err)
	}
	t.Logf("provider rejected the swapped verifier: %v", err)
}

// TestReplayedCodeRejected proves an authorization code cannot be spent twice,
// even by a caller holding the correct verifier. This is what makes a code
// leaked through a Referer header or a proxy log worthless after the fact.
func TestReplayedCodeRejected(t *testing.T) {
	t.Parallel()

	m := startMock(t)
	m.QueueUser(claimUser{subject: "sub-replay", email: "replay@example.test", emailVerified: true})

	a := newAuthenticator(t, m, testOrigin)

	authURL, cookie := begin(t, a)
	flow := openFlowCookie(t, a, cookie)
	cbURL := authorize(t, authURL)

	if _, err := callback(t, a, cbURL, cookie); err != nil {
		t.Fatalf("first use of the code failed: %v", err)
	}

	// Reseal an identical cookie: the browser's copy was cleared, but an
	// attacker replaying the callback would supply their own.
	replayed, err := callback(t, a, cbURL, sealFlowCookie(t, a, flow))
	if err == nil {
		t.Fatalf("replayed code was accepted, identity = %+v", replayed)
	}
	if !strings.Contains(err.Error(), "invalid_grant") {
		t.Errorf("err = %v, want the provider's invalid_grant rejection", err)
	}
	t.Logf("provider rejected the replayed code: %v", err)
}

// TestEmailVerifiedGate is the hard gate. The whitelist is keyed on email
// address, so anything short of an explicit true must fail - including the
// claim being absent, which is how several providers (and mockoidc's own
// MockUser) represent false.
func TestEmailVerifiedGate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		emailVerified any
		wantAccepted  bool
	}{
		{"explicit false", false, false},
		{"claim absent entirely", nil, false},
		{"string \"false\"", "false", false},
		{"explicit true", true, true},
		{"string \"true\"", "true", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := startMock(t)
			m.QueueUser(claimUser{
				subject:       "sub-gate",
				email:         "unverified@example.test",
				emailVerified: tc.emailVerified,
				name:          "Gate Test",
			})
			a := newAuthenticator(t, m, testOrigin)

			authURL, cookie := begin(t, a)
			id, err := callback(t, a, authorize(t, authURL), cookie)

			if tc.wantAccepted {
				if err != nil {
					t.Fatalf("sign-in rejected: %v", err)
				}
				if !id.EmailVerified {
					t.Error("accepted identity does not report EmailVerified")
				}
				return
			}

			if !errors.Is(err, ErrEmailNotVerified) {
				t.Fatalf("err = %v, want ErrEmailNotVerified", err)
			}
			if id != nil {
				t.Errorf("identity returned for an unverified email: %+v", id)
			}
			t.Logf("rejected as expected: %v", err)
		})
	}
}

// TestMockUserWithEmailVerifiedFalse covers the provider shape the ticket
// warned about from the other direction: mockoidc's stock MockUser with
// EmailVerified false, which omits the claim rather than sending false. A gate
// written as a plain struct field with no thought would pass the explicit-false
// case and silently admit this one.
func TestMockUserWithEmailVerifiedFalse(t *testing.T) {
	t.Parallel()

	m := startMock(t)
	user := mockoidc.DefaultUser()
	user.EmailVerified = false
	m.QueueUser(user)

	a := newAuthenticator(t, m, testOrigin)

	authURL, cookie := begin(t, a)
	id, err := callback(t, a, authorize(t, authURL), cookie)
	if !errors.Is(err, ErrEmailNotVerified) {
		t.Fatalf("err = %v, want ErrEmailNotVerified", err)
	}
	if id != nil {
		t.Errorf("identity returned: %+v", id)
	}
	t.Logf("rejected as expected: %v", err)
}

// TestWrongSigningKeyRejected replaces the ID token with one carrying
// identical claims signed by a key the issuer never published. Accepting it
// would be a complete authentication bypass: anyone able to reach the callback
// could mint any identity they liked.
func TestWrongSigningKeyRejected(t *testing.T) {
	t.Parallel()

	evil, err := mockoidc.RandomKeypair(2048)
	if err != nil {
		t.Fatalf("RandomKeypair: %v", err)
	}

	t.Run("unknown key id", func(t *testing.T) {
		t.Parallel()
		assertForgedTokenRejected(t, evil)
	})

	t.Run("correct key id, wrong signature", func(t *testing.T) {
		t.Parallel()

		// Borrow the honest key's kid so the verifier looks up the right
		// public key and has to fail on the RSA check itself, rather than
		// bailing out early because no key matched.
		honest, err := mockoidc.NewKeypair(nil)
		if err != nil {
			t.Fatalf("NewKeypair: %v", err)
		}
		kid, err := honest.KeyID()
		if err != nil {
			t.Fatalf("KeyID: %v", err)
		}

		impostor, err := mockoidc.RandomKeypair(2048)
		if err != nil {
			t.Fatalf("RandomKeypair: %v", err)
		}
		impostor.Kid = kid

		assertForgedTokenRejected(t, impostor)
	})
}

func assertForgedTokenRejected(t *testing.T, signer *mockoidc.Keypair) {
	t.Helper()

	m := startMock(t, forgeIDTokenSignature(t, signer))
	m.QueueUser(claimUser{subject: "sub-forged", email: "forged@example.test", emailVerified: true})

	a := newAuthenticator(t, m, testOrigin)

	authURL, cookie := begin(t, a)
	id, err := callback(t, a, authorize(t, authURL), cookie)
	if err == nil {
		t.Fatalf("forged id_token was accepted, identity = %+v", id)
	}
	if id != nil {
		t.Errorf("identity returned for a forged token: %+v", id)
	}
	if !strings.Contains(err.Error(), "id token verification failed") {
		t.Errorf("err = %v, want an id token verification failure", err)
	}
	t.Logf("rejected as expected: %v", err)
}

// TestNonceMismatchRejected: an ID token minted for a different login attempt
// must not satisfy this one, even with a matching state.
func TestNonceMismatchRejected(t *testing.T) {
	t.Parallel()

	m := startMock(t)
	a := newAuthenticator(t, m, testOrigin)

	authURL, cookie := begin(t, a)
	flow := openFlowCookie(t, a, cookie)
	cbURL := authorize(t, authURL)

	swapped := flow
	swapped.Nonce = "a-nonce-from-some-other-login"

	id, err := callback(t, a, cbURL, sealFlowCookie(t, a, swapped))
	if !errors.Is(err, ErrNonceMismatch) {
		t.Fatalf("err = %v, want ErrNonceMismatch", err)
	}
	if id != nil {
		t.Errorf("identity returned despite nonce mismatch: %+v", id)
	}
}

func TestCallbackCookieFailures(t *testing.T) {
	t.Parallel()

	m := startMock(t)
	a := newAuthenticator(t, m, testOrigin)

	authURL, good := begin(t, a)
	flow := openFlowCookie(t, a, good)
	cbURL := authorize(t, authURL)

	expired := flow
	expired.ExpiresAt = time.Now().Add(-time.Minute).Unix()

	tampered := *good
	tampered.Value = tamperCiphertext(t, good.Value)

	foreign, err := newFlowCookie("a-completely-different-cookie-sec", true, "/auth")
	if err != nil {
		t.Fatalf("newFlowCookie: %v", err)
	}
	rec := httptest.NewRecorder()
	if err := foreign.set(rec, flow); err != nil {
		t.Fatalf("seal under foreign secret: %v", err)
	}
	wrongSecret := findCookie(rec.Result().Cookies(), flowCookieName)

	cases := map[string][]*http.Cookie{
		"no cookie at all":         nil,
		"expired cookie":           {sealFlowCookie(t, a, expired)},
		"tampered ciphertext":      {&tampered},
		"sealed by another secret": {wrongSecret},
	}
	for name, cookies := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			id, err := callback(t, a, cbURL, cookies...)
			if !errors.Is(err, ErrNoFlowCookie) {
				t.Fatalf("err = %v, want ErrNoFlowCookie", err)
			}
			if id != nil {
				t.Errorf("identity returned: %+v", id)
			}
		})
	}
}

// TestCallbackClearsCookieOnFailure: a failed attempt must not leave a usable
// cookie behind for a second try.
func TestCallbackClearsCookieOnFailure(t *testing.T) {
	t.Parallel()

	m := startMock(t)
	a := newAuthenticator(t, m, testOrigin)

	authURL, cookie := begin(t, a)
	cbURL := withQuery(authorize(t, authURL), "state", "wrong")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, cbURL.String(), nil)
	req.AddCookie(cookie)

	if _, err := a.Callback(rec, req); !errors.Is(err, ErrStateMismatch) {
		t.Fatalf("err = %v, want ErrStateMismatch", err)
	}

	cleared := findCookie(rec.Result().Cookies(), flowCookieName)
	if cleared == nil {
		t.Fatal("failed callback did not clear the flow cookie")
	}
	if cleared.MaxAge >= 0 || cleared.Value != "" {
		t.Errorf("flow cookie not expired: value=%q MaxAge=%d", cleared.Value, cleared.MaxAge)
	}
}

func TestProviderErrorRejected(t *testing.T) {
	t.Parallel()

	m := startMock(t)
	a := newAuthenticator(t, m, testOrigin)

	_, cookie := begin(t, a)
	flow := openFlowCookie(t, a, cookie)

	denied, err := url.Parse(testOrigin + "/auth/callback?error=access_denied&error_description=user+said+no&state=" + url.QueryEscape(flow.State))
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}

	id, err := callback(t, a, denied, cookie)
	if !errors.Is(err, ErrProviderDenied) {
		t.Fatalf("err = %v, want ErrProviderDenied", err)
	}
	if id != nil {
		t.Errorf("identity returned: %+v", id)
	}
	// The provider's free-text description is not repeated into our errors.
	if strings.Contains(err.Error(), "user said no") {
		t.Errorf("error echoed the provider's error_description: %v", err)
	}
}

// TestNoCodeAndNoError guards the shape where a callback is hit directly with
// a valid-looking state but nothing to exchange.
func TestNoCodeAndNoError(t *testing.T) {
	t.Parallel()

	m := startMock(t)
	a := newAuthenticator(t, m, testOrigin)

	_, cookie := begin(t, a)
	flow := openFlowCookie(t, a, cookie)

	bare, err := url.Parse(testOrigin + "/auth/callback?state=" + url.QueryEscape(flow.State))
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}

	if _, err := callback(t, a, bare, cookie); !errors.Is(err, ErrProviderDenied) {
		t.Fatalf("err = %v, want ErrProviderDenied", err)
	}
}

// TestNewRequiresIssuerReachable documents that a misconfigured issuer fails
// at construction rather than at the first user's sign-in.
func TestNewRequiresIssuerReachable(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := New(ctx, Options{
		IssuerURL:    "http://127.0.0.1:1/nowhere",
		ClientID:     "id",
		ClientSecret: "secret",
		Origin:       testOrigin,
		CookieSecret: testCookieSecret,
	})
	if err == nil {
		t.Fatal("New succeeded against an unreachable issuer")
	}
	if !strings.Contains(err.Error(), "oidc discovery") {
		t.Errorf("err = %v, want an oidc discovery failure", err)
	}
}

func TestNewRejectsMissingOptions(t *testing.T) {
	t.Parallel()

	_, err := New(context.Background(), Options{Origin: testOrigin})
	if err == nil {
		t.Fatal("New succeeded with no issuer, client id, client secret or cookie secret")
	}
	for _, want := range []string{"IssuerURL", "ClientID", "ClientSecret", "CookieSecret"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to name %s", err, want)
		}
	}
}

// tamperCiphertext flips one bit in the middle of the sealed cookie.
//
// It deliberately works on the decoded bytes rather than on the base64 text.
// Flipping the final base64 character can land entirely in that character's
// unused padding bits, which decodes back to the identical byte string - a
// "tampered" cookie that is in fact untouched, and a test that proves nothing.
func tamperCiphertext(t *testing.T, value string) string {
	t.Helper()

	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		t.Fatalf("decode cookie: %v", err)
	}
	if len(raw) < 2 {
		t.Fatalf("cookie is implausibly short: %d bytes", len(raw))
	}
	raw[len(raw)/2] ^= 0x01

	out := base64.RawURLEncoding.EncodeToString(raw)
	if out == value {
		t.Fatal("test bug: tampering did not change the cookie")
	}
	return out
}

// TestMissingSubjectRejected: OIDC requires a sub claim, but go-oidc does not
// enforce it - Verify copies an absent sub through as an empty string. Since
// person.google_sub is UNIQUE, letting an empty subject out of this package
// would mean the first subject-less sign-in creates a row that every later
// subject-less sign-in then matches.
func TestMissingSubjectRejected(t *testing.T) {
	t.Parallel()

	m := startMock(t)
	m.QueueUser(claimUser{subject: "", email: "nosub@example.test", emailVerified: true})
	a := newAuthenticator(t, m, testOrigin)

	authURL, cookie := begin(t, a)
	id, err := callback(t, a, authorize(t, authURL), cookie)
	if !errors.Is(err, ErrNoSubject) {
		t.Fatalf("err = %v, want ErrNoSubject", err)
	}
	if id != nil {
		t.Errorf("identity returned with no subject: %+v", id)
	}
	t.Logf("rejected as expected: %v", err)
}
