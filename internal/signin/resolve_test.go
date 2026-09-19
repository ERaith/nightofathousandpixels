package signin

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ERaith/nightofathousandpixels/internal/auth"
	"github.com/ERaith/nightofathousandpixels/internal/store"
)

// TestCollisionIsNotReadAsPersonNotFound is the hazard this whole file exists
// for.
//
// UpsertPersonOnSignIn reports an account-takeover attempt by returning NO
// ROWS, which is the same signal pgx uses for "nothing matched" everywhere
// else. Read as "person not found", the obvious next move is to create the
// person -- and creating the person is the takeover. So this asserts on the
// distinct error, not merely on "an error".
func TestCollisionIsNotReadAsPersonNotFound(t *testing.T) {
	t.Parallel()

	f := &fakeStore{
		personBySub: map[string]store.Person{},
		upsertErr:   pgx.ErrNoRows,
	}

	_, err := resolvePerson(context.Background(), f, &auth.Identity{
		Subject:       "a-different-google-account",
		Email:         "alice@example.test",
		EmailVerified: true,
	})

	if !errors.Is(err, ErrIdentityCollision) {
		t.Fatalf("error = %v, want ErrIdentityCollision", err)
	}
	// And it must NOT have been mistaken for a not-found that a caller would
	// then "fix" by inserting.
	if errors.Is(err, pgx.ErrNoRows) {
		t.Error("the collision still reads as pgx.ErrNoRows: a caller that handles " +
			"not-found by creating the person would perform the takeover this guards against")
	}
}

// TestReturningVisitorIsFoundBySubjectNotEmail proves an email change does not
// make somebody a new person: the subject lookup comes first, and the row it
// finds is refreshed with the new address rather than a second row appearing.
func TestReturningVisitorIsFoundBySubjectNotEmail(t *testing.T) {
	t.Parallel()

	existing := newPerson(t, "alice.old@example.test", "sub-alice")
	f := &fakeStore{
		personBySub: map[string]store.Person{"sub-alice": existing},
		personByID:  map[uuid.UUID]store.Person{existing.ID: existing},
	}

	got, err := resolvePerson(context.Background(), f, &auth.Identity{
		Subject:       "sub-alice",
		Email:         "alice.new@example.test",
		EmailVerified: true,
		Name:          "Alice Renamed",
	})
	if err != nil {
		t.Fatalf("resolvePerson: %v", err)
	}

	if got.ID != existing.ID {
		t.Errorf("person ID changed from %v to %v: an email change made a new person", existing.ID, got.ID)
	}
	if got.Email != "alice.new@example.test" {
		t.Errorf("Email = %q, want the new address - the row was read but not refreshed", got.Email)
	}
	if f.upsertCalled {
		t.Error("UpsertPersonOnSignIn was called for a known subject: the email path must not run when the subject matched")
	}
	if !f.updateCalled {
		t.Error("UpdatePersonIdentity was not called: an email change would never be recorded")
	}
}

// TestFirstSignInClaimsTheWhitelistedRow covers the path an admin sets up:
// the row exists with google_sub NULL, and the first sign-in claims it.
func TestFirstSignInClaimsTheWhitelistedRow(t *testing.T) {
	t.Parallel()

	claimed := newPerson(t, "bob@example.test", "sub-bob")
	f := &fakeStore{
		personBySub:  map[string]store.Person{},
		upsertResult: claimed,
	}

	got, err := resolvePerson(context.Background(), f, &auth.Identity{
		Subject:       "sub-bob",
		Email:         "Bob@Example.TEST",
		EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("resolvePerson: %v", err)
	}
	if got.ID != claimed.ID {
		t.Errorf("got person %v, want %v", got.ID, claimed.ID)
	}

	// The normalized address is what the whitelist row was written under, so
	// a mismatch here is a whitelisted person being told they are not on the
	// list. The mixed-case input above is the realistic version of that.
	if f.lastUpsertArg.EmailNormalized != "bob@example.test" {
		t.Errorf("EmailNormalized = %q, want bob@example.test", f.lastUpsertArg.EmailNormalized)
	}
	// The address as typed is preserved separately, so the admin screen shows
	// what the person actually uses.
	if f.lastUpsertArg.Email != "Bob@Example.TEST" {
		t.Errorf("Email = %q, want the address as the provider sent it", f.lastUpsertArg.Email)
	}
}

func TestNormalizeEmail(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"Alice@Example.Test":   "alice@example.test",
		"  bob@example.test  ": "bob@example.test",
		"ALLCAPS@EXAMPLE.TEST": "allcaps@example.test",
		// Deliberately NOT folded - see the comment on NormalizeEmail.
		"alice+films@example.test": "alice+films@example.test",
		"a.l.i.c.e@example.test":   "a.l.i.c.e@example.test",
	} {
		if got := NormalizeEmail(in); got != want {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", in, got, want)
		}
	}
}
