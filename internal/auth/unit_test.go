package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

func TestCallbackURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		origin  string
		path    string
		want    string
		wantErr bool
	}{
		{name: "plain origin", origin: "https://pixels.example.test", path: "/auth/callback", want: "https://pixels.example.test/auth/callback"},
		{name: "trailing slash", origin: "https://pixels.example.test/", path: "/auth/callback", want: "https://pixels.example.test/auth/callback"},
		{name: "origin with sub-path", origin: "https://example.test/nap", path: "/auth/callback", want: "https://example.test/nap/auth/callback"},
		{name: "local http origin", origin: "http://localhost:8080", path: "/auth/callback", want: "http://localhost:8080/auth/callback"},
		{name: "query and fragment dropped", origin: "https://example.test/?a=b#frag", path: "/auth/callback", want: "https://example.test/auth/callback"},
		{name: "non-default port kept", origin: "https://example.test:8443", path: "/auth/callback", want: "https://example.test:8443/auth/callback"},
		{name: "scheme required", origin: "pixels.example.test", path: "/auth/callback", wantErr: true},
		{name: "host required", origin: "https://", path: "/auth/callback", wantErr: true},
		{name: "non-http scheme rejected", origin: "ftp://example.test", path: "/auth/callback", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := callbackURL(tc.origin, tc.path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("callbackURL(%q) = %q, want an error", tc.origin, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("callbackURL(%q): %v", tc.origin, err)
			}
			if got != tc.want {
				t.Errorf("callbackURL(%q) = %q, want %q", tc.origin, got, tc.want)
			}
		})
	}
}

func TestSecureCookies(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"https://pixels.example.test": true,
		"http://localhost:8080":       false,
		"http://127.0.0.1:3000":       false,
		// An unparseable origin should not silently downgrade to a cookie
		// that will travel in clear text.
		"://nonsense": true,
	}
	for origin, want := range cases {
		if got := secureCookies(origin); got != want {
			t.Errorf("secureCookies(%q) = %v, want %v", origin, got, want)
		}
	}
}

func TestCookiePath(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"https://example.test/auth/callback":     "/auth",
		"https://example.test/nap/auth/callback": "/nap/auth",
		"https://example.test/cb":                "/",
		"https://example.test":                   "/",
	}
	for in, want := range cases {
		if got := cookiePath(in); got != want {
			t.Errorf("cookiePath(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestBoolClaim pins the decoding of email_verified, which is the one claim
// whose misreading turns a hard gate into a no-op.
func TestBoolClaim(t *testing.T) {
	t.Parallel()

	cases := []struct {
		json    string
		want    bool
		wantErr bool
	}{
		{json: `{"email_verified": true}`, want: true},
		{json: `{"email_verified": false}`, want: false},
		{json: `{"email_verified": "true"}`, want: true},
		{json: `{"email_verified": "false"}`, want: false},
		{json: `{"email_verified": null}`, want: false},
		{json: `{}`, want: false},
		{json: `{"email_verified": "yes please"}`, wantErr: true},
		{json: `{"email_verified": 1}`, wantErr: true},
		{json: `{"email_verified": {"nested": true}}`, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.json, func(t *testing.T) {
			t.Parallel()

			var claims idTokenClaims
			err := json.Unmarshal([]byte(tc.json), &claims)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Unmarshal(%s) succeeded with %v, want an error", tc.json, bool(claims.EmailVerified))
				}
				// Whatever happens, a decode failure must not leave the gate
				// open: the zero value is false.
				if bool(claims.EmailVerified) {
					t.Error("failed decode left EmailVerified true")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unmarshal(%s): %v", tc.json, err)
			}
			if bool(claims.EmailVerified) != tc.want {
				t.Errorf("EmailVerified = %v, want %v", bool(claims.EmailVerified), tc.want)
			}
		})
	}
}

// TestSanitizeExchangeError proves the token endpoint's response body cannot
// carry an authorization code into our logs.
func TestSanitizeExchangeError(t *testing.T) {
	t.Parallel()

	const secretCode = "4/0AeanS0abcdefSECRETCODE"

	retrieve := &oauth2.RetrieveError{
		Response:         &http.Response{StatusCode: http.StatusUnauthorized},
		Body:             []byte(`{"error":"invalid_grant","error_description":"Invalid code: ` + secretCode + `"}`),
		ErrorCode:        "invalid_grant",
		ErrorDescription: "Invalid code: " + secretCode,
	}

	got := sanitizeExchangeError(retrieve).Error()
	if strings.Contains(got, secretCode) {
		t.Fatalf("sanitized error leaked the authorization code: %q", got)
	}
	if !strings.Contains(got, "invalid_grant") {
		t.Errorf("sanitized error dropped the useful part: %q", got)
	}

	// A transport-level failure has no OAuth error code to report.
	plain := sanitizeExchangeError(errors.New("dial tcp: connection refused")).Error()
	if strings.Contains(plain, "connection refused") {
		t.Errorf("sanitized error echoed an arbitrary error string: %q", plain)
	}
}

func TestSanitizeCode(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"access_denied":         "access_denied",
		"invalid-grant":         "invalid-grant",
		"weird\nvalue with sp":  "weird.value.with.sp",
		"<script>alert(1)</s>":  ".script.alert.1...s.",
		strings.Repeat("a", 90): strings.Repeat("a", 64),
	}
	for in, want := range cases {
		if got := sanitizeCode(in); got != want {
			t.Errorf("sanitizeCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFlowCookieRoundTrip(t *testing.T) {
	t.Parallel()

	c, err := newFlowCookie(testCookieSecret, true, "/auth")
	if err != nil {
		t.Fatalf("newFlowCookie: %v", err)
	}

	// The verifier is long and distinctive on purpose: a two-character needle
	// turns up in random base64 often enough to make the confidentiality
	// assertion below meaningless.
	const verifier = "verifier-QmVhcmVyLW9mLXRoZS1zZWNyZXQtdmVyaWZpZXI"
	want := flowState{State: "st", Verifier: verifier, Nonce: "nc", ExpiresAt: 1 << 40}

	rec := newRecorder()
	if err := c.set(rec, want); err != nil {
		t.Fatalf("set: %v", err)
	}
	cookie := findCookie(rec.Result().Cookies(), flowCookieName)
	if cookie == nil {
		t.Fatal("no cookie written")
	}
	if strings.Contains(cookie.Value, verifier) {
		t.Error("cookie value contains the verifier in clear text")
	}

	req := newRequest()
	req.AddCookie(cookie)
	got, err := c.get(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != want {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

// TestFlowCookieRejectsIncompleteState guards against a cookie that
// authenticates but carries nothing usable.
func TestFlowCookieRejectsIncompleteState(t *testing.T) {
	t.Parallel()

	c, err := newFlowCookie(testCookieSecret, true, "/auth")
	if err != nil {
		t.Fatalf("newFlowCookie: %v", err)
	}

	cases := map[string]flowState{
		"no state":    {Verifier: "vf", Nonce: "nc", ExpiresAt: 1 << 40},
		"no verifier": {State: "st", Nonce: "nc", ExpiresAt: 1 << 40},
		"no nonce":    {State: "st", Verifier: "vf", ExpiresAt: 1 << 40},
	}
	for name, state := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := newRecorder()
			if err := c.set(rec, state); err != nil {
				t.Fatalf("set: %v", err)
			}
			req := newRequest()
			req.AddCookie(findCookie(rec.Result().Cookies(), flowCookieName))
			if _, err := c.get(req); err == nil {
				t.Error("incomplete flow cookie was accepted")
			}
		})
	}
}
