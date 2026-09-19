package auth

// The session half of sign-in (ticket C3).
//
// Callback ends at "this is a verified human with this verified email
// address". This file turns that into something the next request can read: a
// sealed cookie naming the person row the identity resolved to.
//
// Three decisions are worth stating, because all three are the kind that only
// show up in production.
//
//   - The session is self-contained. Everything needed to recognise the
//     visitor is inside the sealed cookie, so there is no server-side session
//     table to keep, to expire, or to lose: a deploy, a container restart and
//     a second replica all leave existing sessions working. The price is that
//     a session cannot be revoked by deleting a row, which is why the cookie
//     carries an identifier and nothing else -- authority is re-read from the
//     database on every request (see internal/signin), so removing someone
//     from a season's whitelist takes effect on their next click rather than
//     in thirty days.
//   - Secure is derived from the configured ORIGIN's scheme, never from
//     r.TLS. Behind Nginx Proxy Manager r.TLS is always nil even though the
//     browser is on HTTPS, so a session cookie keyed off it would be sent in
//     clear text on the next plain-HTTP request to the same host.
//     X-Forwarded-Proto would be the other candidate and is worse: it is a
//     request header, which is to say it is whatever the client last said it
//     was. Ticket C3's own text says "behind a proxy: trust X-Forwarded-
//     Proto"; that instruction is not followed, and the reason is written out
//     at secureCookies in auth.go.
//   - The key is derived under its own HKDF label and every value binds its
//     own AAD, both distinct from the flow cookie's. See sessionCookieLabel.
//
// Rotation, which nap-hjj asked C3 to decide before shipping rather than
// after: bumping the :v1 in either constant below invalidates every
// outstanding session, i.e. it signs everybody out. So does changing
// COOKIE_SECRET. Neither is destructive -- nothing is stored server-side, so
// the next sign-in rebuilds the session from Google -- but both are a visible
// event for every user at once, and neither is something to do casually. There
// is deliberately no key-overlap window: for a thirty-person site, "everyone
// signs in again" is a cheaper answer than carrying two live keys and the code
// that decides between them.

import (
	"crypto/cipher"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// sessionCookieName is the signed-in cookie. The nap_ prefix keeps it
// distinguishable from anything a reverse proxy or an analytics script sets on
// the same host.
const sessionCookieName = "nap_session"

// sessionCookieLabel and sessionCookieAAD separate this cookie from the flow
// cookie, which is derived from the same COOKIE_SECRET.
//
// These MUST differ from flowCookieLabel and flowCookieAAD, and that is the
// whole of ticket nap-hjj. Reusing the flow label would put a thirty-day
// session cookie under the same key as a ten-minute single-use flow cookie --
// the exact property nap-dh7 was filed to remove -- and no existing test would
// catch it: TestCookieKeyDerivationIsLabelled proves only that two DIFFERENT
// labels yield different keys, which stays true no matter what this constant
// says. TestSessionCookieDoesNotShareTheFlowCookiesKey in session_test.go is
// the test that fails if these two lines are changed to match, and it asserts
// on the constants rather than on a literal so that it cannot drift.
const (
	sessionCookieLabel = "nap:session-cookie:v1"
	sessionCookieAAD   = "nap:session-cookie:aad:v1"
)

// DefaultSessionTTL is how long a sign-in lasts. Thirty days is the ticket's
// number and the right one for this application: the site is used in bursts a
// few weeks apart, and being asked to sign in again every visit is the kind of
// friction that stops people voting at all.
const DefaultSessionTTL = 30 * 24 * time.Hour

// Errors returned by Sessions.Read.
var (
	// ErrNoSession means the request carried no session cookie at all. This is
	// the ordinary signed-out case and is not worth logging.
	ErrNoSession = errors.New("auth: no session cookie")

	// ErrInvalidSession means a session cookie was present but unusable:
	// tampered with, sealed under a previous COOKIE_SECRET, sealed as a
	// different kind of cookie, or expired. The caller should clear it,
	// because a browser that keeps presenting it will keep being told to sign
	// in.
	ErrInvalidSession = errors.New("auth: session cookie is invalid")
)

// Session is what the session cookie carries.
//
// It deliberately holds no authority. There is no is_admin flag and no
// membership here: those are read from the database per request, so a cookie
// minted before someone was made an admin - or after they were removed from a
// season - can never be the reason they are treated as one.
type Session struct {
	// PersonID is the person row this session belongs to.
	PersonID uuid.UUID `json:"p"`

	// Email is the address the session was minted for. It is carried for
	// logging and for the "signed in as" line only; the person row is the
	// truth, and it is looked up by PersonID.
	Email string `json:"e"`

	// IssuedAt and ExpiresAt are unix seconds. ExpiresAt is inside the sealed
	// payload as well as in the cookie's Max-Age, because Max-Age is only
	// advice to a cooperating browser - a client that keeps sending an expired
	// cookie must still be told it has expired.
	IssuedAt  int64 `json:"i"`
	ExpiresAt int64 `json:"x"`
}

// Expired reports whether the session has passed its own expiry.
func (s Session) Expired(now time.Time) bool { return s.ExpiresAt <= now.Unix() }

// Sessions mints, reads and clears the session cookie.
type Sessions struct {
	aead   cipher.AEAD
	aad    []byte
	secure bool
	ttl    time.Duration
}

// NewSessions returns the session cookie manager.
//
// secret is COOKIE_SECRET, which config has already checked is at least 32
// characters. origin is ORIGIN, and only its scheme is used - see the file
// comment for why the request is not consulted. A ttl of zero means
// DefaultSessionTTL.
func NewSessions(secret, origin string, ttl time.Duration) (*Sessions, error) {
	aead, err := newCookieAEAD(secret, sessionCookieLabel)
	if err != nil {
		return nil, err
	}
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	return &Sessions{
		aead:   aead,
		aad:    []byte(sessionCookieAAD),
		secure: secureCookies(origin),
		ttl:    ttl,
	}, nil
}

// Secure reports whether the cookies this manager sets carry the Secure
// attribute. It exists so that a caller can log the value it derived, which is
// the only cheap way to catch an ORIGIN typo in a deployed environment.
func (s *Sessions) Secure() bool { return s.secure }

// TTL is how long an issued session lasts.
func (s *Sessions) TTL() time.Duration { return s.ttl }

// Issue mints a session for personID and writes the cookie.
func (s *Sessions) Issue(w http.ResponseWriter, personID uuid.UUID, email string) error {
	now := time.Now()
	value, err := sealJSON(s.aead, s.aad, Session{
		PersonID:  personID,
		Email:     email,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(s.ttl).Unix(),
	})
	if err != nil {
		return fmt.Errorf("auth: seal session: %w", err)
	}

	http.SetCookie(w, s.cookie(value, int(s.ttl.Seconds())))
	return nil
}

// Read returns the session on r, or ErrNoSession / ErrInvalidSession.
func (s *Sessions) Read(r *http.Request) (Session, error) {
	var zero Session

	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return zero, ErrNoSession
	}

	var session Session
	if err := openJSON(s.aead, s.aad, cookie.Value, &session); err != nil {
		return zero, ErrInvalidSession
	}
	// A sealed payload that decodes to the zero UUID is not a session. JSON
	// makes that reachable in a way the crypto does not: an authentic envelope
	// carrying `{}` unmarshals happily into an all-zero Session, and a caller
	// that trusted it would look up the nil person.
	if session.PersonID == uuid.Nil {
		return zero, ErrInvalidSession
	}
	if session.Expired(time.Now()) {
		return zero, ErrInvalidSession
	}
	return session, nil
}

// Clear expires the session cookie. Every attribute except Max-Age has to
// match the one that was set, or the browser treats it as a different cookie
// and leaves the original in place.
func (s *Sessions) Clear(w http.ResponseWriter) {
	http.SetCookie(w, s.cookie("", -1))
}

func (s *Sessions) cookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:  sessionCookieName,
		Value: value,
		// Root path: unlike the flow cookie, which only has business on the
		// callback, this one has to reach every page.
		Path:   "/",
		MaxAge: maxAge,
		// No JavaScript on this site needs the session, and the one that does
		// not exist cannot leak it to an injected script.
		HttpOnly: true,
		Secure:   s.secure,
		// Lax rather than Strict so that following a link into the site from
		// the group chat arrives signed in. Strict is the stronger setting,
		// but it would mean every shared link looks signed-out on arrival,
		// which trains people to sign in again and defeats the point.
		SameSite: http.SameSiteLaxMode,
	}
}
