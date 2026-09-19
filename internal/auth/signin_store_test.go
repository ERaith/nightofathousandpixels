//go:build integration

// Live-database tests. They are behind the `integration` build tag, which is
// the repository's one contract for "this test needs Postgres":
//
//     go test ./...                  compiles nothing in this file
//     make test                      the same, and says so
//     make test-integration          starts a database, migrates it, runs this
//
// The tag exists because the alternative did not work. This file used to be
// untagged and to skip when TEST_DATABASE_URL was empty, so a bare
// `go test ./...` printed `ok  internal/store` having run no database test at
// all -- for months, including the runs used to justify merges (nap-gn1). A
// skip that fires by default is indistinguishable from a pass in every summary
// anyone actually reads.
//
// With the tag, the untagged run does not compile these tests, `make test`
// prints what it left out, and `make test-integration` fails outright if the
// tag selects nothing. A missing TEST_DATABASE_URL is therefore a FATAL here
// rather than a skip: under this tag the database is the point, so its absence
// is a broken invocation, not a reason to go quietly green.

package auth

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ERaith/nightofathousandpixels/internal/store"
)

// This file exists because two commits in different packages are load-bearing
// on each other and nothing failed when that coupling was broken.
//
// internal/store's UpsertPersonOnSignIn takes google_sub as a NON-NULL
// parameter (sqlc.arg(google_sub)::text). That tightening is only safe while
// internal/auth guarantees a non-empty Subject. The guarantee is ErrNoSubject,
// and until now it was recorded in a SQL comment and a ticket -- so removing
// it would have turned nothing red, in either package.
//
// What breaks if it goes: an ID token with no usable sub yields an Identity
// whose Subject is "", the sign-in path writes google_sub = '' onto a
// whitelisted row, and person_google_sub_key UNIQUE then makes that row the
// one every later subject-less sign-in matches. Not a failed lookup -- a
// shared account, reached through the front door, reported to the real owner
// as someone else's identity collision.
//
// The test drives the real flow against a real database and asserts the empty
// subject never arrives.

// connectDB opens a pool against TEST_DATABASE_URL. Under the integration tag
// a missing DSN is fatal, not a skip -- see the note at the top of this file.
// It mirrors internal/store's own harness, which lives in that package's test
// binary and cannot be imported.
func connectDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL is not set.\n" +
			"These tests are built with -tags=integration, which means they need a database.\n" +
			"Run them with `make test-integration`, which starts and migrates one.")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// beginTx wraps the scenario in a transaction that is always rolled back.
func beginTx(t *testing.T, pool *pgxpool.Pool) pgx.Tx {
	t.Helper()

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("rollback: %v", err)
		}
	})
	return tx
}

// signInWithIdentity is the sign-in path as C3/C4 will write it: look the
// person up by subject, and fall back to the email path for a first sign-in
// against a whitelisted row. It is deliberately trusting -- it does no
// validation of its own, because the entire question is whether the auth
// package let something through that this code would then act on.
func signInWithIdentity(ctx context.Context, q *store.Queries, id *Identity) (store.Person, error) {
	if p, err := q.GetPersonByGoogleSub(ctx, id.Subject); err == nil {
		return p, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return store.Person{}, err
	}
	return q.UpsertPersonOnSignIn(ctx, store.UpsertPersonOnSignInParams{
		Email:           id.Email,
		EmailNormalized: id.Email,
		GoogleSub:       id.Subject,
		DisplayName:     id.Name,
	})
}

// TestSubjectlessSignInNeverReachesTheStore is the enforcement of the
// auth/store coupling. Deleting the ErrNoSubject guard in identityFrom turns
// this red.
func TestSubjectlessSignInNeverReachesTheStore(t *testing.T) {
	pool := connectDB(t)
	tx := beginTx(t, pool)
	ctx := context.Background()
	q := store.New(tx)

	// Two whitelisted people: added by an admin, neither has ever signed in,
	// so both carry google_sub IS NULL. These are the rows an empty subject
	// would collide on.
	const victimEmail = "victim@example.test"
	const otherEmail = "other@example.test"
	for _, email := range []string{victimEmail, otherEmail} {
		if _, err := tx.Exec(ctx,
			`INSERT INTO person (email, email_normalized, display_name) VALUES ($1, $1, 'Whitelisted')`,
			email); err != nil {
			t.Fatalf("whitelist %s: %v", email, err)
		}
	}

	// A provider that issues a token with no usable sub. The claim is present
	// and empty, which is the shape go-oidc copies through as "".
	m := startMock(t)
	m.QueueUser(claimUser{
		subject:       "sub-present",
		subjectClaim:  "",
		email:         victimEmail,
		emailVerified: true,
	})
	a := newAuthenticator(t, m, testOrigin)

	// The store call is wired behind the real CallbackHandler, so nothing here
	// depends on remembering to check an error: if Callback returns an
	// identity, the database is written, exactly as it would be in production.
	var reached bool
	var storeErr error
	handler := a.CallbackHandler(
		func(_ http.ResponseWriter, r *http.Request, id *Identity) {
			reached = true
			t.Errorf("auth handed the store an identity with Subject=%q -- the ErrNoSubject guard is gone, "+
				"and UpsertPersonOnSignIn's non-null google_sub parameter is no longer safe", id.Subject)
			_, storeErr = signInWithIdentity(r.Context(), q, id)
		},
		func(_ http.ResponseWriter, _ *http.Request, err error) {
			if !errors.Is(err, ErrNoSubject) {
				t.Errorf("sign-in failed with %v, want ErrNoSubject", err)
			}
		},
	)

	authURL, cookie := begin(t, a)
	cbURL := authorize(t, authURL)
	req := newRequest()
	req.URL = cbURL
	req.AddCookie(cookie)
	handler.ServeHTTP(newRecorder(), req)

	if reached {
		t.Logf("store call returned: %v", storeErr)
	}

	// The assertions that matter, and the ones that survive someone deleting
	// the guard without reading this file.

	// 1. No person row anywhere carries an empty subject. This is the state
	//    that person_google_sub_key turns into a shared account.
	var empties int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM person WHERE google_sub = ''`).Scan(&empties); err != nil {
		t.Fatalf("count empty subjects: %v", err)
	}
	if empties != 0 {
		t.Errorf("%d person row(s) have google_sub = ''; the next subject-less sign-in will match one of them", empties)
	}

	// 2. The whitelisted row was not claimed. An admin added this person and
	//    they have still never signed in.
	var sub *string
	if err := tx.QueryRow(ctx,
		`SELECT google_sub FROM person WHERE email_normalized = $1`, victimEmail).Scan(&sub); err != nil {
		t.Fatalf("read victim row: %v", err)
	}
	if sub != nil {
		t.Errorf("whitelisted row was claimed with google_sub = %q", *sub)
	}

	// 3. And the lookup that would do the damage finds nothing to confuse.
	if _, err := q.GetPersonByGoogleSub(ctx, ""); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("GetPersonByGoogleSub(\"\") = %v, want pgx.ErrNoRows: an empty subject resolves to a person", err)
	}

	// Guarded, because on failure the sentence above is the opposite of what
	// just happened and a green-looking log line in a red test is how a wrong
	// claim gets believed.
	if !t.Failed() {
		t.Logf("subject-less sign-in rejected at the auth boundary; %s still unclaimed and no row holds google_sub = ''", victimEmail)
	}
}
