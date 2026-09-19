package auth

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

// TestCookieKeyDerivationIsLabelled proves the two properties the flow
// cookie's key derivation is supposed to have, because both are invisible in
// normal operation: a cookie sealed by this code is opened by this same code,
// so nothing in the happy path would notice if either were lost.
//
// This matters now rather than later because C3 mints the session cookie from
// the same COOKIE_SECRET. Under the previous derivation -- a bare
// sha256.Sum256 of the secret -- every cookie in the application would have
// shared one key, and the only thing keeping a session cookie from being
// opened as a flow cookie would have been the shape of the JSON inside.
func TestCookieKeyDerivationIsLabelled(t *testing.T) {
	t.Parallel()

	const secret = testCookieSecret

	// 1. A different label is a different key. This is what lets C3 derive its
	//    own cookie key from the same secret without weakening this one.
	flow, err := newCookieAEAD(secret, flowCookieLabel)
	if err != nil {
		t.Fatalf("derive flow key: %v", err)
	}
	session, err := newCookieAEAD(secret, "nap:session-cookie:v1")
	if err != nil {
		t.Fatalf("derive session key: %v", err)
	}

	nonce := make([]byte, flow.NonceSize())
	sealed := flow.Seal(nil, nonce, []byte("flow payload"), []byte(flowCookieAAD))

	if _, err := session.Open(nil, nonce, sealed, []byte(flowCookieAAD)); err == nil {
		t.Error("a cookie sealed under the flow label opened under the session label: " +
			"the labels are not separating keys, so every cookie shares one key")
	}

	// 2. The derived key is not the old bare hash of the secret. A refactor
	//    that quietly reinstated sha256.Sum256(secret) would still pass every
	//    other test in this package.
	legacy := sha256.Sum256([]byte(secret))
	derived, err := deriveCookieKey(secret, flowCookieLabel)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if bytes.Equal(derived, legacy[:]) {
		t.Error("cookie key equals sha256(secret): the HKDF derivation has been lost")
	}
}

// TestCookieAADIsBound proves the sealed value is tied to the cookie it was
// sealed as. Without the AAD, a value lifted from one cookie and presented as
// another would decrypt cleanly and be rejected — if at all — only by whatever
// happened to be checked further in.
func TestCookieAADIsBound(t *testing.T) {
	t.Parallel()

	aead, err := newCookieAEAD(testCookieSecret, flowCookieLabel)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}

	nonce := make([]byte, aead.NonceSize())
	sealed := aead.Seal(nil, nonce, []byte("flow payload"), []byte(flowCookieAAD))

	if _, err := aead.Open(nil, nonce, sealed, []byte(flowCookieAAD)); err != nil {
		t.Fatalf("matching AAD failed to open: %v", err)
	}
	if _, err := aead.Open(nil, nonce, sealed, []byte("nap:session-cookie:aad:v1")); err == nil {
		t.Error("opened with a different AAD: the sealed value is not bound to its cookie")
	}
	if _, err := aead.Open(nil, nonce, sealed, nil); err == nil {
		t.Error("opened with no AAD: the binding is not actually applied")
	}
}

// TestFlowCookieRejectsForeignSecret covers the one case
// TestFlowCookieRoundTrip in unit_test.go does not: a well-formed cookie
// sealed under a different COOKIE_SECRET. The round trip and the
// confidentiality of the verifier are already proven there, so this does not
// repeat them.
func TestFlowCookieRejectsForeignSecret(t *testing.T) {
	t.Parallel()

	mine, err := newFlowCookie(testCookieSecret, true, "/auth")
	if err != nil {
		t.Fatalf("newFlowCookie: %v", err)
	}
	theirs, err := newFlowCookie("ffffffffffffffffffffffffffffffff", true, "/auth")
	if err != nil {
		t.Fatalf("newFlowCookie(theirs): %v", err)
	}

	rec := newRecorder()
	if err := theirs.set(rec, flowState{State: "st", Verifier: "vf", Nonce: "nc", ExpiresAt: 1 << 40}); err != nil {
		t.Fatalf("set: %v", err)
	}
	req := newRequest()
	req.AddCookie(findCookie(rec.Result().Cookies(), flowCookieName))

	if _, err := mine.get(req); err == nil {
		t.Error("a cookie sealed under a different secret opened")
	}
}
