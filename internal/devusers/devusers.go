// Package devusers is the development cast: the four identities the mock OIDC
// provider will sign tokens for, and the whitelist rows the seed writes for
// them.
//
// It exists so that there is exactly one list. The provider and the seed are
// two separate binaries with no other reason to know about each other, and the
// failure when they disagree is silent and misleading: a test user who is
// supposed to be whitelisted signs in successfully and is told they are not on
// the list, which looks like a bug in the whitelist gate rather than a typo in
// a fixture.
//
// Nothing in the shipped server imports this package. It carries no mock
// provider dependency and no bypass of any kind -- it is a list of names and
// addresses, and both of its consumers (cmd/seed, devtools/mockoidcd) are
// development-only.
package devusers

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// User is one development identity.
type User struct {
	Email       string
	DisplayName string

	// Note is the one-line explanation shown on the provider's picker page and
	// in the README table.
	Note string

	// Member is whether the seed puts this person on the season's whitelist.
	// One of them is deliberately false.
	Member bool

	// Admin sets season_member.is_admin. Admin is per season, so it lives on
	// the membership row rather than on the person.
	Admin bool
}

// Subject is the OIDC "sub" claim the mock provider signs for this user.
//
// It is derived from the address rather than stored or randomised, so it is
// stable across restarts of the provider and across `make compose-nuke`. That
// matters more than it looks: sub is what person.google_sub holds once someone
// has signed in, so a subject that changed between runs would make the next
// sign-in look like a brand new Google account arriving with an address that
// is already claimed -- which is the identity-collision refusal, triggered for
// no reason and very hard to read as a fixture problem.
//
// The "dev-" prefix makes it obvious in the database that a row came from the
// mock provider rather than from Google.
func (u User) Subject() string {
	sum := sha256.Sum256([]byte("nap-dev-oidc|" + strings.ToLower(u.Email)))
	return "dev-" + hex.EncodeToString(sum[:8])
}

// All is the development cast, in the order the picker shows them.
//
// The .test TLD is reserved by RFC 6761 for exactly this: it can never be
// registered, so none of these addresses can ever reach a real inbox, and a
// seed that escaped into a real database could not mail anybody.
var All = []User{
	{
		Email:       "admin@example.test",
		DisplayName: "Ada Admin",
		Note:        "admin for the 2026 season",
		Member:      true,
		Admin:       true,
	},
	{
		Email:       "alice@example.test",
		DisplayName: "Alice Voter",
		Note:        "ordinary member",
		Member:      true,
	},
	{
		Email:       "bob@example.test",
		DisplayName: "Bob Voter",
		Note:        "ordinary member",
		Member:      true,
	},
	{
		// On purpose, and the reason this one exists: without somebody who is
		// NOT on the list, the "ask Eraith to add you" page is a thing you
		// have to take on trust instead of a thing you can click.
		Email:       "stranger@example.test",
		DisplayName: "Sam Stranger",
		Note:        "NOT on the whitelist - shows the refusal page",
		Member:      false,
	},
}
