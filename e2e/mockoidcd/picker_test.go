package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/oauth2-proxy/mockoidc"

	"github.com/ERaith/nightofathousandpixels/internal/devusers"
)

const (
	testClientID = "nap-test-client"
	testRedirect = "http://localhost:8080/auth/callback"
)

func testProvider(t *testing.T) *mockoidc.MockOIDC {
	t.Helper()

	m, err := mockoidc.NewServer(nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	m.ClientID = testClientID
	m.ClientSecret = "test-client-secret"
	m.CodeChallengeMethodsSupported = []string{"S256"}
	// Only so Issuer()/endpoint helpers have an Addr to derive from; this
	// server is never served.
	m.Server = &http.Server{Addr: "localhost:9000"}
	return m
}

// authorizeRequest builds a well-formed authorize request, which individual
// tests then break in one specific way.
func authorizeRequest(overrides map[string]string) *http.Request {
	q := url.Values{
		"client_id":             {testClientID},
		"response_type":         {"code"},
		"scope":                 {"openid email profile"},
		"state":                 {"test-state"},
		"redirect_uri":          {testRedirect},
		"nonce":                 {"test-nonce"},
		"code_challenge":        {"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"},
		"code_challenge_method": {"S256"},
	}
	for k, v := range overrides {
		if v == "" {
			q.Del(k)
			continue
		}
		q.Set(k, v)
	}
	return httptest.NewRequest(http.MethodGet, mockoidc.AuthorizationEndpoint+"?"+q.Encode(), nil)
}

// TestInvalidAuthorizeRequestIsRejectedNotOfferedThePicker is the regression
// builder-5's e2e suite caught.
//
// The picker used to render as soon as it saw no chosen identity, so
// m.Authorize never ran and nothing about the request was validated. A bogus
// client_id got a 200 and a list of real people to click. In the dev stack
// that means a mistyped OAUTH_CLIENT_ID in .env presents a working-looking
// sign-in screen and the failure surfaces somewhere else entirely.
func TestInvalidAuthorizeRequestIsRejectedNotOfferedThePicker(t *testing.T) {
	t.Parallel()

	for name, override := range map[string]map[string]string{
		"unknown client_id":          {"client_id": "somebody-elses-app"},
		"missing client_id":          {"client_id": ""},
		"wrong response_type":        {"response_type": "token"},
		"missing state":              {"state": ""},
		"missing redirect_uri":       {"redirect_uri": ""},
		"unsupported scope":          {"scope": "openid wiretap"},
		"unsupported code challenge": {"code_challenge_method": "plain"},
		"missing scope":              {"scope": ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			m := testProvider(t)
			rec := httptest.NewRecorder()
			pickUser(m, devusers.All).ServeHTTP(rec, authorizeRequest(override))

			if rec.Code < http.StatusBadRequest {
				t.Errorf("status = %d, want >= 400: an invalid authorize request was accepted", rec.Code)
			}
			if strings.Contains(rec.Body.String(), "Mock sign-in") {
				t.Error("the picker was rendered for an invalid authorize request: " +
					"the identity list is being offered to a client the provider should have refused")
			}
			// The refusal must name no real people either.
			for _, u := range devusers.All {
				if strings.Contains(rec.Body.String(), u.Email) {
					t.Errorf("the refusal leaks the dev identity %s", u.Email)
				}
			}
		})
	}
}

// TestValidAuthorizeRequestGetsThePicker is the other half: the rejection
// above must not be so eager that it refuses good requests too.
func TestValidAuthorizeRequestGetsThePicker(t *testing.T) {
	t.Parallel()

	m := testProvider(t)
	rec := httptest.NewRecorder()
	pickUser(m, devusers.All).ServeHTTP(rec, authorizeRequest(nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Mock sign-in") {
		t.Fatal("a valid authorize request did not get the picker")
	}
	for _, u := range devusers.All {
		if !strings.Contains(body, u.Email) {
			t.Errorf("the picker does not offer %s", u.Email)
		}
	}
	// The OIDC parameters have to survive the extra hop, or the flow the
	// application started cannot be completed.
	for _, param := range []string{"test-state", "test-nonce", "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"} {
		if !strings.Contains(body, param) {
			t.Errorf("the picker links drop %q: state, nonce and the PKCE challenge must be carried through", param)
		}
	}
}

// TestChosenIdentityIsSignedIn covers the click.
func TestChosenIdentityIsSignedIn(t *testing.T) {
	t.Parallel()

	m := testProvider(t)
	rec := httptest.NewRecorder()
	pickUser(m, devusers.All).ServeHTTP(rec, authorizeRequest(map[string]string{userParam: "alice@example.test"}))

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 to the callback", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, testRedirect) || !strings.Contains(loc, "code=") {
		t.Errorf("Location = %q, want a redirect to the callback carrying a code", loc)
	}
}

// TestUnknownChosenIdentityIsAnErrorNotADefaultUser: falling through to
// m.Authorize with an empty queue would sign the visitor in as mockoidc's own
// DefaultUser (jane.doe@example.com), and the whitelist refusal that followed
// would name an address nobody asked for.
func TestUnknownChosenIdentityIsAnErrorNotADefaultUser(t *testing.T) {
	t.Parallel()

	m := testProvider(t)
	rec := httptest.NewRecorder()
	pickUser(m, devusers.All).ServeHTTP(rec, authorizeRequest(map[string]string{userParam: "nobody@example.test"}))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if strings.Contains(rec.Header().Get("Location"), "code=") {
		t.Error("an unknown identity was signed in anyway, as mockoidc's DefaultUser")
	}
}

// TestQueuedIdentityBypassesThePicker is builder-5's Playwright path:
// /control/user pushes an identity, and the next authorize must consume it
// rather than rendering HTML at something that is not a browser.
func TestQueuedIdentityBypassesThePicker(t *testing.T) {
	t.Parallel()

	m := testProvider(t)
	m.QueueUser(&namedUser{
		MockUser: &mockoidc.MockUser{
			Subject:       "queued-subject",
			Email:         "queued@example.test",
			EmailVerified: true,
		},
		Name: "Queued Person",
	})

	rec := httptest.NewRecorder()
	pickUser(m, devusers.All).ServeHTTP(rec, authorizeRequest(nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302: a queued identity must not get the picker", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "Mock sign-in") {
		t.Error("the picker was rendered even though an identity was queued")
	}
}

// TestRejectedRequestDoesNotConsumeAQueuedIdentity: the validation probe runs
// m.Authorize for real, so if mockoidc ever validated AFTER popping, a bad
// request from anywhere would silently eat the identity a test had lined up.
func TestRejectedRequestDoesNotConsumeAQueuedIdentity(t *testing.T) {
	t.Parallel()

	m := testProvider(t)
	m.QueueUser(&namedUser{
		MockUser: &mockoidc.MockUser{Subject: "queued-subject", Email: "queued@example.test", EmailVerified: true},
	})

	// An invalid request arrives first.
	pickUser(m, devusers.All).ServeHTTP(httptest.NewRecorder(), authorizeRequest(map[string]string{"client_id": "wrong"}))

	if !userQueued(m) {
		t.Fatal("a rejected authorize request consumed the queued identity")
	}
}
