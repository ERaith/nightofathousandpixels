package signin

import "strings"

// NormalizeEmail is the application's email-matching policy, in one place.
//
// person.email_normalized is a plain column rather than a generated one
// precisely so that this policy lives in Go (see migration 00001), and there
// are exactly two callers that matter: sign-in, which looks a person up by it,
// and the seed/admin path, which writes the whitelist row before that person
// has ever signed in. If those two ever disagree, a whitelisted person signs
// in and is told they are not on the list -- so they share this function
// rather than each calling strings.ToLower.
//
// The policy is deliberately only case folding and trimming. Stripping
// plus-tags and gmail dots is the obvious next step and is NOT done, because
// it is not reversible and it changes which rows collide: person_email_
// normalized_key is UNIQUE, so widening the policy later can merge two
// existing people into one row, and narrowing it can split one person in two.
// That is a migration with a data decision in it, not a one-line change, and
// it is not needed to run a season.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
