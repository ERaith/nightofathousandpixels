package config_test

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/ERaith/nightofathousandpixels/internal/config"
	"github.com/ERaith/nightofathousandpixels/internal/signin"
)

// setRequired fills in everything Load insists on, so that each test can vary
// the one variable it is about.
func setRequired(t *testing.T) {
	t.Helper()

	t.Setenv("DATABASE_URL", "postgres://nap:nap@localhost:5432/nap?sslmode=disable")
	t.Setenv("OAUTH_ISSUER_URL", "http://localhost:9000")
	t.Setenv("OAUTH_CLIENT_ID", "nap-local-dev")
	t.Setenv("OAUTH_CLIENT_SECRET", "not-a-real-secret")
	t.Setenv("COOKIE_SECRET", strings.Repeat("k", 32))
	t.Setenv("ORIGIN", "http://localhost:7331")
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("TRUSTED_PROXY_COUNT", "")
	t.Setenv("BOOTSTRAP_ADMIN_EMAILS", "")
	// Not required, but cleared so that a key in the developer's own
	// environment cannot make TestTMDBIsOptional pass for the wrong reason.
	t.Setenv("TMDB_API_KEY", "")
	t.Setenv("TMDB_BASE_URL", "")
}

// TestTMDBIsOptional: the submit page's search is configuration the server
// starts without.
//
// This is not leniency. Every agent's dev stack and every fresh clone is in
// this state, and a required key here would mean nobody could run the server
// without one -- so the submit form falls back to the four typed boxes it has
// always had. See internal/web/board.Options.TMDB.
func TestTMDBIsOptional(t *testing.T) {
	setRequired(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load refused to start with no TMDB key: %v", err)
	}
	if cfg.TMDBAPIKey != "" || cfg.TMDBBaseURL != "" {
		t.Errorf("TMDB settings = %q / %q, want both empty", cfg.TMDBAPIKey, cfg.TMDBBaseURL)
	}
}

func TestTMDBSettingsAreRead(t *testing.T) {
	setRequired(t)
	t.Setenv("TMDB_API_KEY", "  a-key  ")
	t.Setenv("TMDB_BASE_URL", "  http://localhost:9330  ")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Trimmed, because a key with a trailing newline from a copy-paste is a
	// 401 from TMDB and an afternoon of confusion.
	if cfg.TMDBAPIKey != "a-key" {
		t.Errorf("TMDBAPIKey = %q, want it trimmed", cfg.TMDBAPIKey)
	}
	if cfg.TMDBBaseURL != "http://localhost:9330" {
		t.Errorf("TMDBBaseURL = %q, want it trimmed", cfg.TMDBBaseURL)
	}
}

// A typo in TMDB_BASE_URL is a server that cannot search. Finding that out at
// startup beats finding it out from somebody mid-submission.
func TestTMDBBaseURLMustBeAbsolute(t *testing.T) {
	for _, raw := range []string{"localhost:9330", "/api", "ftp://example.com", "http://"} {
		t.Run(raw, func(t *testing.T) {
			setRequired(t)
			t.Setenv("TMDB_BASE_URL", raw)

			if _, err := config.Load(); err == nil {
				t.Errorf("Load accepted TMDB_BASE_URL=%q", raw)
			}
		})
	}
}

func TestLoadLogLevel(t *testing.T) {
	tests := []struct {
		raw     string
		want    slog.Level
		wantErr bool
	}{
		{raw: "", want: slog.LevelInfo},
		{raw: "debug", want: slog.LevelDebug},
		{raw: "DEBUG", want: slog.LevelDebug},
		{raw: " warn ", want: slog.LevelWarn},
		{raw: "warning", want: slog.LevelWarn},
		{raw: "error", want: slog.LevelError},
		{raw: "verbose", wantErr: true},
	}

	for _, tc := range tests {
		t.Run("LOG_LEVEL="+tc.raw, func(t *testing.T) {
			setRequired(t)
			t.Setenv("LOG_LEVEL", tc.raw)

			cfg, err := config.Load()
			switch {
			case tc.wantErr && err == nil:
				t.Fatal("want an error for an unrecognised level, got none")
			case tc.wantErr:
				return
			case err != nil:
				t.Fatalf("Load: %v", err)
			case cfg.LogLevel != tc.want:
				t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, tc.want)
			}
		})
	}
}

// TestLoadTrustedProxyCount guards the setting whose wrong values are both
// silent: too low lets a client forge its own IP, too high erases it.
func TestLoadTrustedProxyCount(t *testing.T) {
	tests := []struct {
		raw     string
		want    int
		wantErr bool
	}{
		// Trust nothing is the default, because it is the one that cannot be
		// exploited when it is wrong.
		{raw: "", want: 0},
		{raw: "0", want: 0},
		{raw: "1", want: 1},
		{raw: "2", want: 2},
		{raw: "-1", wantErr: true},
		{raw: "11", wantErr: true},
		{raw: "one", wantErr: true},
		{raw: "1.5", wantErr: true},
	}

	for _, tc := range tests {
		t.Run("TRUSTED_PROXY_COUNT="+tc.raw, func(t *testing.T) {
			setRequired(t)
			t.Setenv("TRUSTED_PROXY_COUNT", tc.raw)

			cfg, err := config.Load()
			switch {
			case tc.wantErr && err == nil:
				t.Fatal("want an error, got none")
			case tc.wantErr:
				return
			case err != nil:
				t.Fatalf("Load: %v", err)
			case cfg.TrustedProxyCount != tc.want:
				t.Errorf("TrustedProxyCount = %d, want %d", cfg.TrustedProxyCount, tc.want)
			}
		})
	}
}

// The bootstrap admin list (ticket D3, nap-06p).
//
// It exists because admin is per season and the first season has no membership
// row, so on a fresh production database -- which is never seeded -- nobody is
// an admin and nobody can create the season that would make somebody one.

// TestBootstrapAdminsAreOptional: unset is the normal case and the right
// steady state. Once a season exists, admin comes from season_member and this
// variable has nothing left to do.
func TestBootstrapAdminsAreOptional(t *testing.T) {
	setRequired(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.BootstrapAdminEmails) != 0 {
		t.Errorf("BootstrapAdminEmails = %v, want empty", cfg.BootstrapAdminEmails)
	}
	if cfg.IsBootstrapAdmin("anybody@example.test") {
		t.Error("an empty list made somebody an admin")
	}
}

func TestBootstrapAdminsAreParsedAndNormalized(t *testing.T) {
	setRequired(t)
	// Deliberately messy: mixed case, spaces, and a trailing comma. A typo in
	// a deployment's environment must not stop the process from starting.
	t.Setenv("BOOTSTRAP_ADMIN_EMAILS", " Ada@Example.Test , bootstrap@example.test ,, ")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := []string{"ada@example.test", "bootstrap@example.test"}
	if len(cfg.BootstrapAdminEmails) != len(want) {
		t.Fatalf("BootstrapAdminEmails = %v, want %v", cfg.BootstrapAdminEmails, want)
	}
	for i, w := range want {
		if cfg.BootstrapAdminEmails[i] != w {
			t.Errorf("entry %d = %q, want %q", i, cfg.BootstrapAdminEmails[i], w)
		}
	}

	if !cfg.IsBootstrapAdmin("ada@example.test") {
		t.Error("a capitalised address in the environment did not match its normalized form")
	}
	if cfg.IsBootstrapAdmin("someone.else@example.test") {
		t.Error("an address not on the list matched")
	}
	if cfg.IsBootstrapAdmin("") {
		t.Error("a blank address matched; person.email_normalized is never blank, so this means a caller with no person")
	}
}

// TestBootstrapAdminMatchingUsesTheStoredForm is the trap this pair exists to
// avoid.
//
// IsBootstrapAdmin compares against person.email_normalized, which
// signin.NormalizeEmail wrote. If the two policies ever disagree, the variable
// silently grants nobody anything -- which looks exactly like "the deployment
// forgot to set it" on the one day it matters.
//
// The comparison is deliberately against the address AS STORED, not against a
// re-normalization here, because a second call to a normalizer in the test
// would pass even if both sides were wrong in the same way.
func TestBootstrapAdminMatchingUsesTheStoredForm(t *testing.T) {
	setRequired(t)
	t.Setenv("BOOTSTRAP_ADMIN_EMAILS", "ADA@EXAMPLE.TEST")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// What migration 00001 would hold for that person: signin.NormalizeEmail
	// is lower case and trimmed, so this is the literal stored value.
	const stored = "ada@example.test"

	if !cfg.IsBootstrapAdmin(stored) {
		t.Errorf("the environment's %q did not match the stored %q; the two normalization policies have drifted",
			"ADA@EXAMPLE.TEST", stored)
	}
	if cfg.IsBootstrapAdmin("ADA@EXAMPLE.TEST") {
		t.Error("IsBootstrapAdmin matched an un-normalized address; it takes person.email_normalized and must not normalize again")
	}
}

// TestBootstrapAdminNormalizationMatchesSignIn holds the two policies
// together.
//
// internal/config deliberately does not import internal/signin -- a package
// that reads environment variables must not depend on one that talks to the
// database -- so the normalization is written out twice. This test is the
// thing that makes that safe: it is an external test package, so it may import
// both, and it fails the moment they disagree.
//
// If NormalizeEmail ever gains a rule (plus-tags, gmail dots -- see its
// comment for why it has not), this fails and loadBootstrapAdminEmails has to
// gain the same one.
func TestBootstrapAdminNormalizationMatchesSignIn(t *testing.T) {
	addresses := []string{
		"Ada@Example.Test",
		"  spaced@example.test  ",
		"ALLCAPS@EXAMPLE.TEST",
		"mixed.Case+tag@Example.test",
	}

	setRequired(t)
	t.Setenv("BOOTSTRAP_ADMIN_EMAILS", strings.Join(addresses, ","))

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.BootstrapAdminEmails) != len(addresses) {
		t.Fatalf("parsed %d addresses, want %d", len(cfg.BootstrapAdminEmails), len(addresses))
	}

	for i, raw := range addresses {
		// What internal/signin would have written into
		// person.email_normalized for this address.
		stored := signin.NormalizeEmail(raw)

		if cfg.BootstrapAdminEmails[i] != stored {
			t.Errorf("config normalized %q to %q; signin.NormalizeEmail gives %q. "+
				"The two policies have drifted, and the effect is that the bootstrap list silently matches nobody.",
				raw, cfg.BootstrapAdminEmails[i], stored)
		}
		if !cfg.IsBootstrapAdmin(stored) {
			t.Errorf("the stored form of %q does not match the bootstrap list", raw)
		}
	}
}
