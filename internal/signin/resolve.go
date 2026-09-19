package signin

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/ERaith/nightofathousandpixels/internal/auth"
	"github.com/ERaith/nightofathousandpixels/internal/store"
)

// ErrIdentityCollision means the verified email address already belongs to a
// DIFFERENT Google account.
//
// This is the single most important error in this package to get right,
// because the query it comes from reports it the same way it reports "nothing
// happened": UpsertPersonOnSignIn's DO UPDATE carries a WHERE that only
// matches a row which is unclaimed or already this subject's, so a genuine
// collision updates nothing, RETURNING yields no row, and :one surfaces
// pgx.ErrNoRows. Read as "person not found" -- which is what pgx.ErrNoRows
// means everywhere else in this codebase -- the natural next move is to create
// the person, and creating the person is precisely the account takeover the
// guard exists to stop. See the comment on the query in
// internal/store/queries/person.sql.
//
// It is not a bug and not an attack on its own. Google ties sub to the account
// object rather than to the address, so a deleted-and-recreated account, or a
// Workspace address reassigned to a new person, produces it honestly. It needs
// a human: an admin has to decide whether this is the same person with a new
// account or a different person who now holds the address.
var ErrIdentityCollision = errors.New("signin: email address belongs to a different Google account")

// personStore is the slice of the query set that resolving an identity needs.
type personStore interface {
	GetPersonByGoogleSub(ctx context.Context, googleSub string) (store.Person, error)
	UpdatePersonIdentity(ctx context.Context, arg store.UpdatePersonIdentityParams) (store.Person, error)
	UpsertPersonOnSignIn(ctx context.Context, arg store.UpsertPersonOnSignInParams) (store.Person, error)
}

// resolvePerson turns a verified identity into the person row it belongs to,
// creating or claiming that row as needed.
//
// Two paths, in this order, which is the order person.sql was written for:
//
//  1. The subject is already known. That is the returning visitor, and it is
//     tried first because sub is the only identifier Google guarantees is
//     stable -- an email change must not make someone a new person.
//  2. The subject is new. Either a whitelisted row is waiting for this address
//     with no sub on it yet (first sign-in), or nobody here has this address
//     (a genuinely new person, who will be created and then almost certainly
//     turned away by the whitelist gate, which is a separate decision).
func resolvePerson(ctx context.Context, q personStore, id *auth.Identity) (store.Person, error) {
	var zero store.Person

	normalized := NormalizeEmail(id.Email)
	if normalized == "" {
		// identityFrom already rejects an empty email, so this is only
		// reachable via an address that is entirely whitespace.
		return zero, errors.New("signin: identity carries no usable email address")
	}

	person, err := q.GetPersonByGoogleSub(ctx, id.Subject)
	switch {
	case err == nil:
		// Known subject. Google may have handed us a new address or display
		// name since last time, so the row is refreshed rather than just read
		// -- otherwise an email change would never be recorded and the admin's
		// whitelist would still show the old address.
		updated, err := q.UpdatePersonIdentity(ctx, store.UpdatePersonIdentityParams{
			ID:              person.ID,
			Email:           id.Email,
			EmailNormalized: normalized,
			DisplayName:     id.Name,
		})
		if err != nil {
			return zero, fmt.Errorf("signin: refresh person %s: %w", person.ID, err)
		}
		return updated, nil

	case !errors.Is(err, pgx.ErrNoRows):
		return zero, fmt.Errorf("signin: look up person by subject: %w", err)
	}

	// New subject: claim a waiting whitelist row, or create the person.
	claimed, err := q.UpsertPersonOnSignIn(ctx, store.UpsertPersonOnSignInParams{
		Email:           id.Email,
		EmailNormalized: normalized,
		GoogleSub:       id.Subject,
		DisplayName:     id.Name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// NOT "person not found". The row exists and is owned by another
			// subject; see ErrIdentityCollision.
			return zero, ErrIdentityCollision
		}
		return zero, fmt.Errorf("signin: claim person row: %w", err)
	}
	return claimed, nil
}
