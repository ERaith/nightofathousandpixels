package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/ERaith/nightofathousandpixels/internal/devusers"
)

// loadDevUsers returns the identities the picker offers.
//
// The default is internal/devusers.All, which is the same list cmd/seed writes
// whitelist rows from -- one list, so the provider and the seed cannot drift
// apart and leave a "whitelisted" test user being told they are not on the
// list.
//
// MOCKOIDC_USERS overrides it, as a comma-separated list of
// `email|Display Name|note`. That exists so a test can offer an identity the
// seed does not know about -- an address that is on nobody's whitelist, say --
// without rebuilding this image.
func loadDevUsers() ([]devusers.User, error) {
	raw := strings.TrimSpace(os.Getenv("MOCKOIDC_USERS"))
	if raw == "" {
		return devusers.All, nil
	}

	var users []devusers.User
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.Split(entry, "|")
		email := strings.TrimSpace(parts[0])
		if email == "" || !strings.Contains(email, "@") {
			return nil, fmt.Errorf("MOCKOIDC_USERS entry %q has no email address", entry)
		}
		u := devusers.User{Email: email}
		if len(parts) > 1 {
			u.DisplayName = strings.TrimSpace(parts[1])
		}
		if len(parts) > 2 {
			u.Note = strings.TrimSpace(parts[2])
		}
		users = append(users, u)
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("MOCKOIDC_USERS is set to %q but lists no users", raw)
	}
	return users, nil
}
