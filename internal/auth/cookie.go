package auth

import (
	"crypto/aes"
	"crypto/cipher"
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
	secure bool
	path   string
}

func newFlowCookie(secret string, secure bool, path string) (*flowCookie, error) {
	// The secret is a configuration string of arbitrary length; SHA-256 turns
	// it into the fixed 32-byte key AES-256 needs. config already rejects
	// secrets shorter than 32 characters, so this is a widening, not a
	// strengthening.
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("auth: cookie cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("auth: cookie aead: %w", err)
	}
	if path == "" {
		path = "/"
	}
	return &flowCookie{aead: aead, secure: secure, path: path}, nil
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
	sealed := c.aead.Seal(nonce, nonce, payload, nil)

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
	payload, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		// Either the cookie was tampered with or it was sealed under a
		// different secret. Both are "start again".
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
