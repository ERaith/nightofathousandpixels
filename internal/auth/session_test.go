package auth

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

const testOriginHTTPS = "https://pixels.example.com"

func testSessions(t *testing.T, origin string) *Sessions {
	t.Helper()
	s, err := NewSessions(testCookieSecret, origin, 0)
	if err != nil {
		t.Fatalf("NewSessions: %v", err)
	}
	return s
}

// issueAndRead mints a session and reads it back through a fresh request, the
// way a browser would.
func issueAndRead(t *testing.T, s *Sessions, id uuid.UUID, email string) (Session, *http.Cookie) {
	t.Helper()

	rec := newRecorder()
	if err := s.Issue(rec, id, email); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	cookie := findCookie(rec.Result().Cookies(), sessionCookieName)
	if cookie == nil {
		t.Fatal("Issue set no session cookie")
	}

	req := newRequest()
	req.AddCookie(cookie)
	got, err := s.Read(req)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return got, cookie
}

func TestSessionRoundTrip(t *testing.T) {
	t.Parallel()

	s := testSessions(t, testOriginHTTPS)
	id := uuid.New()

	got, _ := issueAndRead(t, s, id, "alice@example.test")

	if got.PersonID != id {
		t.Errorf("PersonID = %v, want %v", got.PersonID, id)
	}
	if got.Email != "alice@example.test" {
		t.Errorf("Email = %q, want alice@example.test", got.Email)
	}
	if got.ExpiresAt-got.IssuedAt != int64(DefaultSessionTTL.Seconds()) {
		t.Errorf("session lasts %ds, want %ds", got.ExpiresAt-got.IssuedAt, int64(DefaultSessionTTL.Seconds()))
	}
}

// TestSessionCookieAttributes covers the four attributes the ticket names. The
// Secure case is the one with history behind it, so it gets both directions.
func TestSessionCookieAttributes(t *testing.T) {
	t.Parallel()

	_, cookie := issueAndRead(t, testSessions(t, testOriginHTTPS), uuid.New(), "a@b.test")

	if !cookie.HttpOnly {
		t.Error("session cookie is not HttpOnly: script-readable sessions are stealable by any injected script")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", cookie.SameSite)
	}
	if cookie.Path != "/" {
		t.Errorf("Path = %q, want /: a session scoped below the root is not sent to most pages", cookie.Path)
	}
	if cookie.MaxAge != int(DefaultSessionTTL.Seconds()) {
		t.Errorf("MaxAge = %d, want %d", cookie.MaxAge, int(DefaultSessionTTL.Seconds()))
	}
}

// TestSessionSecureFollowsOriginNotRequest is the regression this application
// has a proxy-shaped reason to care about. Behind Nginx Proxy Manager r.TLS is
// nil on every request, so a Secure derived from the request would be false in
// production and the session cookie would travel in clear text. The requests
// below therefore carry no TLS at all, exactly like the deployed ones.
func TestSessionSecureFollowsOriginNotRequest(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		origin string
		want   bool
	}{
		{"https://pixels.example.com", true},
		{"https://pixels.example.com/under/a/path", true},
		{"http://localhost:7351", false},
	} {
		s := testSessions(t, tc.origin)
		if s.Secure() != tc.want {
			t.Errorf("origin %q: Secure() = %v, want %v", tc.origin, s.Secure(), tc.want)
		}

		rec := newRecorder()
		if err := s.Issue(rec, uuid.New(), "a@b.test"); err != nil {
			t.Fatalf("Issue: %v", err)
		}
		// newRequest builds a plain http request: r.TLS is nil here, as it is
		// behind the proxy.
		if got := findCookie(rec.Result().Cookies(), sessionCookieName).Secure; got != tc.want {
			t.Errorf("origin %q: cookie Secure = %v, want %v", tc.origin, got, tc.want)
		}
	}
}

// TestSessionCookieDoesNotShareTheFlowCookiesKey is ticket nap-hjj.
//
// builder-1's TestCookieKeyDerivationIsLabelled proves that two different
// labels give different keys, which stays true however these constants are
// set -- so it cannot catch C3 reusing the flow cookie's label. This test can:
// it asserts on the constants themselves, and then proves the consequence.
func TestSessionCookieDoesNotShareTheFlowCookiesKey(t *testing.T) {
	t.Parallel()

	if sessionCookieLabel == flowCookieLabel {
		t.Fatalf("the session cookie and the flow cookie share the HKDF label %q: "+
			"a thirty-day session is now sealed under the same key as a ten-minute "+
			"single-use flow cookie, which is what nap-dh7 removed", sessionCookieLabel)
	}
	if sessionCookieAAD == flowCookieAAD {
		t.Fatalf("the session cookie and the flow cookie share the AAD %q", sessionCookieAAD)
	}

	// The consequence, proven rather than asserted: a session cookie's bytes
	// must be meaningless to the flow cookie's reader.
	sessions := testSessions(t, testOriginHTTPS)
	rec := newRecorder()
	if err := sessions.Issue(rec, uuid.New(), "a@b.test"); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	sealed := findCookie(rec.Result().Cookies(), sessionCookieName).Value

	flow, err := newFlowCookie(testCookieSecret, true, "/auth")
	if err != nil {
		t.Fatalf("newFlowCookie: %v", err)
	}
	req := newRequest()
	req.AddCookie(&http.Cookie{Name: flowCookieName, Value: sealed})
	if _, err := flow.get(req); err == nil {
		t.Error("the flow cookie reader opened a session cookie's value")
	}
}

// TestSessionRejectsTamperedAndForeignCookies covers what Read must refuse. An
// expired session is in here because Max-Age is only advice: a client is free
// to keep sending a cookie forever, so the expiry inside the sealed payload is
// the one that counts.
func TestSessionRejectsTamperedAndForeignCookies(t *testing.T) {
	t.Parallel()

	s := testSessions(t, testOriginHTTPS)

	rec := newRecorder()
	if err := s.Issue(rec, uuid.New(), "a@b.test"); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	good := findCookie(rec.Result().Cookies(), sessionCookieName).Value

	other, err := NewSessions("ffffffffffffffffffffffffffffffff", testOriginHTTPS, 0)
	if err != nil {
		t.Fatalf("NewSessions(other): %v", err)
	}
	otherRec := newRecorder()
	if err := other.Issue(otherRec, uuid.New(), "a@b.test"); err != nil {
		t.Fatalf("Issue(other): %v", err)
	}

	// A session that has already expired inside its own payload.
	expired, err := NewSessions(testCookieSecret, testOriginHTTPS, time.Nanosecond)
	if err != nil {
		t.Fatalf("NewSessions(expired): %v", err)
	}
	expiredRec := newRecorder()
	if err := expired.Issue(expiredRec, uuid.New(), "a@b.test"); err != nil {
		t.Fatalf("Issue(expired): %v", err)
	}

	for _, tc := range []struct {
		name  string
		value string
		want  error
	}{
		{"flipped byte", good[:len(good)-1] + flipLast(good), ErrInvalidSession},
		{"truncated", good[:10], ErrInvalidSession},
		{"not base64", "!!!not base64!!!", ErrInvalidSession},
		{"empty", "", ErrInvalidSession},
		{"sealed under another secret", findCookie(otherRec.Result().Cookies(), sessionCookieName).Value, ErrInvalidSession},
		{"expired", findCookie(expiredRec.Result().Cookies(), sessionCookieName).Value, ErrInvalidSession},
	} {
		req := newRequest()
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tc.value})
		if _, err := s.Read(req); err != tc.want {
			t.Errorf("%s: Read error = %v, want %v", tc.name, err, tc.want)
		}
	}

	// And no cookie at all is the ordinary signed-out case, not an invalid one.
	if _, err := s.Read(newRequest()); err != ErrNoSession {
		t.Errorf("no cookie: Read error = %v, want %v", err, ErrNoSession)
	}
}

// flipLast returns the last character of s changed to a different one, so that
// the result is still valid base64 but is not the same ciphertext.
func flipLast(s string) string {
	last := s[len(s)-1]
	if last == 'A' {
		return "B"
	}
	return "A"
}

// TestSessionCarriesNoAuthority is a design guard rather than a behaviour
// test. The session deliberately holds an identifier and nothing else:
// authority is re-read per request, so that removing someone from a season
// takes effect on their next click rather than when their cookie expires. A
// field added here for convenience -- is_admin is the tempting one -- would
// silently undo that, and nothing else in the suite would notice.
func TestSessionCarriesNoAuthority(t *testing.T) {
	t.Parallel()

	s := testSessions(t, testOriginHTTPS)
	rec := newRecorder()
	if err := s.Issue(rec, uuid.New(), "a@b.test"); err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Open the payload with the session's own key and look at the raw JSON.
	aead, err := newCookieAEAD(testCookieSecret, sessionCookieLabel)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	var claims map[string]any
	value := findCookie(rec.Result().Cookies(), sessionCookieName).Value
	if err := openJSON(aead, []byte(sessionCookieAAD), value, &claims); err != nil {
		t.Fatalf("open: %v", err)
	}

	for _, key := range []string{"p", "e", "i", "x"} {
		if _, ok := claims[key]; !ok {
			t.Errorf("session payload is missing %q", key)
		}
		delete(claims, key)
	}
	if len(claims) != 0 {
		t.Errorf("session payload carries unexpected claims %v: authority belongs in the database, "+
			"re-read per request, not in a cookie that outlives the decision", claims)
	}
}

// TestClearMatchesIssuedAttributes: a browser treats a Set-Cookie whose Path,
// Secure or SameSite differ as a DIFFERENT cookie, so a mismatched Clear
// leaves the real session in place and signing out silently does nothing.
func TestClearMatchesIssuedAttributes(t *testing.T) {
	t.Parallel()

	s := testSessions(t, testOriginHTTPS)

	issued := newRecorder()
	if err := s.Issue(issued, uuid.New(), "a@b.test"); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	set := findCookie(issued.Result().Cookies(), sessionCookieName)

	cleared := newRecorder()
	s.Clear(cleared)
	clr := findCookie(cleared.Result().Cookies(), sessionCookieName)
	if clr == nil {
		t.Fatal("Clear set no cookie")
	}

	if clr.Path != set.Path || clr.Secure != set.Secure || clr.SameSite != set.SameSite || clr.HttpOnly != set.HttpOnly {
		t.Errorf("Clear attributes %+v do not match Issue's %+v: the browser will keep the original cookie", clr, set)
	}
	if clr.MaxAge >= 0 {
		t.Errorf("Clear MaxAge = %d, want negative", clr.MaxAge)
	}
	if clr.Value != "" {
		t.Errorf("Clear Value = %q, want empty", clr.Value)
	}
}
