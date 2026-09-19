package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// flowCookieName holds the per-login secrets between the authorize redirect
// and the callback.
const flowCookieName = "nap_oauth_flow"

// flowCookieLabel and flowCookieAAD separate this cookie from every other
// cookie derived from the same COOKIE_SECRET.
//
// The label is HKDF's info parameter, so the flow cookie's key is
// cryptographically independent of any other labelled key: C3's session cookie
// derives from the same secret under its own label and cannot decrypt this
// one, nor this one it. The AAD binds each sealed value to the cookie it was
// sealed as, so a value cannot be lifted from one cookie and presented as
// another even if the two ever shared a key.
//
// Both carry a version suffix. Changing either invalidates every outstanding
// cookie of that kind, which for a 10-minute flow cookie costs an interrupted
// sign-in and nothing else -- that is the cheap moment to rotate, and it is
// why the version is here from the start rather than added once it hurts.
//
// C3 MUST choose its own label and AAD. Reusing these would put the session
// cookie under the same key as the flow cookie.
const (
	flowCookieLabel = "nap:flow-cookie:v1"
	flowCookieAAD   = "nap:flow-cookie:aad:v1"
)

// tokenBytes is the entropy behind state and nonce. 32 bytes is well past the
// point where guessing is the attacker's best option.
const tokenBytes = 32

// flowState is everything a login needs to remember about itself. It lives in
// the browser rather than in server memory so that the flow survives a deploy
// and works across replicas.
type flowState struct {
	State     string `json:"s"`
	Verifier  string `json:"v"`
	Nonce     string `json:"n"`
	ExpiresAt int64  `json:"e"`
}

// flowCookie seals and opens flowState.
//
// The cookie is authenticated *and* encrypted rather than merely signed. The
// ticket asks for signed, and AES-GCM is a strict superset: it gives the same
// tamper detection plus confidentiality, which matters because the payload
// contains the PKCE code_verifier. A signed-but-readable verifier would still
// be safe in theory - it is bound to this browser's own flow - but there is no
// reason to publish it, and one primitive doing both jobs is less code than a
// separate HMAC.
type flowCookie struct {
	aead   cipher.AEAD
	aad    []byte
	secure bool
	path   string
}

func newFlowCookie(secret string, secure bool, path string) (*flowCookie, error) {
	aead, err := newCookieAEAD(secret, flowCookieLabel)
	if err != nil {
		return nil, err
	}
	if path == "" {
		path = "/"
	}
	return &flowCookie{
		aead:   aead,
		aad:    []byte(flowCookieAAD),
		secure: secure,
		path:   path,
	}, nil
}

// newCookieAEAD derives an AES-256-GCM AEAD for one labelled purpose from the
// shared cookie secret.
//
// HKDF rather than a bare SHA-256 of the secret. The previous version hashed
// the secret directly, which meant every cookie the application ever seals
// would share one key: any two cookie types would be interchangeable to the
// cipher, and the only thing keeping them apart would be the payload's shape.
// HKDF's info parameter makes each purpose a separate key, so adding C3's
// session cookie cannot weaken this one.
//
// The salt is nil, which HKDF defines as a zero salt. A salt's job is to add
// entropy when the input keying material is low-entropy or non-uniform; here
// the domain separation is carried by the label, and a fixed non-secret salt
// would add nothing a constant info string does not already provide. What
// matters is that COOKIE_SECRET is long -- config enforces at least 32
// characters at startup.
func newCookieAEAD(secret, label string) (cipher.AEAD, error) {
	key, err := deriveCookieKey(secret, label)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("auth: cookie cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("auth: cookie aead: %w", err)
	}
	return aead, nil
}

func (c *flowCookie) set(w http.ResponseWriter, state flowState) error {
	payload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("auth: encode flow state: %w", err)
	}

	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("auth: cookie nonce: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, payload, c.aad)

	http.SetCookie(w, &http.Cookie{
		Name:     flowCookieName,
		Value:    base64.RawURLEncoding.EncodeToString(sealed),
		Path:     c.path,
		MaxAge:   int(flowTTL.Seconds()),
		HttpOnly: true,
		Secure:   c.secure,
		// Lax, not Strict: the callback arrives as a top-level navigation from
		// the provider, which is cross-site. Strict would drop the cookie and
		// break every sign-in.
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (c *flowCookie) get(r *http.Request) (flowState, error) {
	var zero flowState

	cookie, err := r.Cookie(flowCookieName)
	if err != nil {
		return zero, fmt.Errorf("auth: read flow cookie: %w", err)
	}

	sealed, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return zero, errors.New("auth: flow cookie is not valid base64")
	}
	if len(sealed) < c.aead.NonceSize() {
		return zero, errors.New("auth: flow cookie is truncated")
	}

	nonce, ciphertext := sealed[:c.aead.NonceSize()], sealed[c.aead.NonceSize():]
	payload, err := c.aead.Open(nil, nonce, ciphertext, c.aad)
	if err != nil {
		// Tampered with, sealed under a different secret, or sealed as a
		// different kind of cookie -- the AAD makes that last one fail here
		// rather than one layer further in. All of them are "start again".
		return zero, errors.New("auth: flow cookie failed authentication")
	}

	var state flowState
	if err := json.Unmarshal(payload, &state); err != nil {
		return zero, errors.New("auth: flow cookie payload is malformed")
	}
	// The expiry is inside the sealed payload as well as in Max-Age, because
	// Max-Age is only advice to a cooperating browser.
	if state.ExpiresAt <= time.Now().Unix() {
		return zero, errors.New("auth: flow cookie expired")
	}
	if state.State == "" || state.Verifier == "" || state.Nonce == "" {
		return zero, errors.New("auth: flow cookie is incomplete")
	}
	return state, nil
}

func (c *flowCookie) clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     flowCookieName,
		Value:    "",
		Path:     c.path,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   c.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// randomToken returns a URL-safe random string for use as state or nonce.
func randomToken() (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// deriveCookieKey is the HKDF step on its own, so a test can assert on the key
// material rather than only on the behaviour of the cipher built from it.
func deriveCookieKey(secret, label string) ([]byte, error) {
	key, err := hkdf.Key(sha256.New, []byte(secret), nil, label, 32)
	if err != nil {
		return nil, fmt.Errorf("auth: derive cookie key: %w", err)
	}
	return key, nil
}
