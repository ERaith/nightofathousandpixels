// Package config loads the server's settings from the process environment.
//
// Everything the server needs is read here and nowhere else. In particular the
// OIDC issuer is a plain configuration value with no default: tests point it at
// a mock OIDC server, so no provider hostname is baked into the binary.
package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const (
	// defaultPort is used when PORT is unset.
	defaultPort = 8080

	// minCookieSecretLen is the shortest signing key we accept. A short key is
	// a silent security bug, so it fails at startup instead.
	minCookieSecretLen = 32

	// defaultLogLevel is used when LOG_LEVEL is unset.
	defaultLogLevel = slog.LevelInfo

	// defaultTrustedProxyCount is used when TRUSTED_PROXY_COUNT is unset. Zero
	// -- trust nothing -- is the only safe default: it is right for a binary
	// run on a laptop, and where it is wrong the failure is a log line naming
	// the proxy, not a log line naming whatever address the client asked for.
	defaultTrustedProxyCount = 0

	// maxTrustedProxyCount bounds the setting. Nobody has ten reverse proxies;
	// a number this large is a typo, and a typo here means every request
	// arrives with an X-Forwarded-For chain too short to satisfy the count, so
	// no client IP is recorded at all. Failing at startup is louder.
	maxTrustedProxyCount = 10
)

// Config holds every setting the server reads at startup.
type Config struct {
	DatabaseURL       string
	Port              int
	OAuthIssuerURL    string
	OAuthClientID     string
	OAuthClientSecret string
	CookieSecret      string
	Origin            string
	LogLevel          slog.Level

	// TrustedProxyCount is how many reverse proxies stand between a client and
	// this server. It decides whether X-Forwarded-For is believed at all; see
	// internal/web/middleware.ClientIPPolicy for what each value means and why
	// this is configuration rather than a constant.
	TrustedProxyCount int

	// BootstrapAdminEmails is the answer to the chicken-and-egg problem in
	// admin (ticket D3, nap-06p).
	//
	// Admin is per season: season_member.is_admin. So before the first season
	// exists there is no membership row, and therefore nobody is an admin, and
	// therefore nobody can create the season that would make somebody one. A
	// site with no seed -- which production deliberately is -- cannot start its
	// first year at all.
	//
	// These addresses are admins of the admin screens regardless of any
	// membership row. That is the whole of the privilege: they can create a
	// season and re-date one. Whoever creates a season is written into it as
	// an admin in the same transaction, so this variable is needed exactly
	// once, in the life of the deployment, and can then be removed.
	//
	// Empty is the normal steady state and is not an error. It is a list
	// rather than one address because the person setting the variable and the
	// person who will run the season are not always the same, and a list of
	// two costs nothing.
	//
	// The values are normalized with signin.NormalizeEmail's policy -- lower
	// case and trimmed -- because they are compared against
	// person.email_normalized, and a capital letter in an environment variable
	// silently granting nobody anything is exactly the failure this is meant
	// to avoid.
	BootstrapAdminEmails []string
}

// IsBootstrapAdmin reports whether a normalized email address is on the
// bootstrap list.
//
// It takes an already-normalized address rather than normalizing here, so that
// there is one normalization policy in the application and this is not a
// second one that could drift from it. The caller passes
// person.email_normalized, which is the column the policy wrote.
//
// A blank address is never a match, even if the list somehow contains one:
// person.email_normalized has a not-blank CHECK, so a blank here means a
// caller with no person, and that must not be an admin.
func (c *Config) IsBootstrapAdmin(emailNormalized string) bool {
	if emailNormalized == "" {
		return false
	}
	for _, e := range c.BootstrapAdminEmails {
		if e == emailNormalized {
			return true
		}
	}

	return false
}

// Load reads and validates the configuration from the environment. Every
// required variable that is missing is reported in a single error rather than
// one per run.
func Load() (*Config, error) {
	var missing []string
	required := func(key string) string {
		v := strings.TrimSpace(os.Getenv(key))
		if v == "" {
			missing = append(missing, key)
		}
		return v
	}

	cfg := &Config{
		DatabaseURL:       required("DATABASE_URL"),
		OAuthIssuerURL:    required("OAUTH_ISSUER_URL"),
		OAuthClientID:     required("OAUTH_CLIENT_ID"),
		OAuthClientSecret: required("OAUTH_CLIENT_SECRET"),
		CookieSecret:      required("COOKIE_SECRET"),
		Origin:            required("ORIGIN"),
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("config: missing required environment variable(s): %s", strings.Join(missing, ", "))
	}

	port, err := loadPort()
	if err != nil {
		return nil, err
	}
	cfg.Port = port

	level, err := loadLogLevel()
	if err != nil {
		return nil, err
	}
	cfg.LogLevel = level

	proxies, err := loadTrustedProxyCount()
	if err != nil {
		return nil, err
	}
	cfg.TrustedProxyCount = proxies

	cfg.BootstrapAdminEmails = loadBootstrapAdminEmails()

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Addr is the listen address for net/http, e.g. ":8080".
func (c *Config) Addr() string {
	return fmt.Sprintf(":%d", c.Port)
}

func (c *Config) validate() error {
	if err := requireAbsoluteURL("OAUTH_ISSUER_URL", c.OAuthIssuerURL); err != nil {
		return err
	}
	if err := requireAbsoluteURL("ORIGIN", c.Origin); err != nil {
		return err
	}
	if len(c.CookieSecret) < minCookieSecretLen {
		return fmt.Errorf("config: COOKIE_SECRET must be at least %d characters, got %d", minCookieSecretLen, len(c.CookieSecret))
	}
	return nil
}

func requireAbsoluteURL(key, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("config: %s is not a valid URL: %w", key, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("config: %s must be an absolute http or https URL, got %q", key, raw)
	}
	if u.Host == "" {
		return fmt.Errorf("config: %s must include a host, got %q", key, raw)
	}
	return nil
}

func loadPort() (int, error) {
	raw := strings.TrimSpace(os.Getenv("PORT"))
	if raw == "" {
		return defaultPort, nil
	}
	port, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("config: PORT must be a number, got %q", raw)
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("config: PORT must be between 1 and 65535, got %d", port)
	}
	return port, nil
}

// loadLogLevel reads LOG_LEVEL. An unrecognised value is an error rather than
// a fall back to the default: silently serving info logs to someone who asked
// for debug wastes an afternoon.
func loadLogLevel() (slog.Level, error) {
	raw := strings.TrimSpace(os.Getenv("LOG_LEVEL"))
	if raw == "" {
		return defaultLogLevel, nil
	}

	switch strings.ToLower(raw) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("config: LOG_LEVEL must be one of debug, info, warn, error, got %q", raw)
	}
}

// loadTrustedProxyCount reads TRUSTED_PROXY_COUNT.
func loadTrustedProxyCount() (int, error) {
	raw := strings.TrimSpace(os.Getenv("TRUSTED_PROXY_COUNT"))
	if raw == "" {
		return defaultTrustedProxyCount, nil
	}

	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("config: TRUSTED_PROXY_COUNT must be a number, got %q", raw)
	}
	if n < 0 || n > maxTrustedProxyCount {
		return 0, fmt.Errorf("config: TRUSTED_PROXY_COUNT must be between 0 and %d, got %d", maxTrustedProxyCount, n)
	}
	return n, nil
}

// loadBootstrapAdminEmails reads BOOTSTRAP_ADMIN_EMAILS, a comma-separated
// list.
//
// Unset is the normal case and yields nil rather than an error: once a season
// exists, admin comes from season_member and this variable has nothing left to
// do. Blank entries and stray whitespace are dropped rather than refused, so
// that a trailing comma in a deployment's environment does not stop the
// process from starting -- the failure mode of a typo here is that somebody is
// not an admin, which they can see, not that the site is down, which they
// cannot fix.
//
// The normalization matches signin.NormalizeEmail exactly (lower case, trim).
// It is repeated rather than imported because internal/config must not depend
// on a package that depends on the database; the two are held together by
// TestBootstrapAdminNormalizationMatchesSignIn.
func loadBootstrapAdminEmails() []string {
	raw := strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_EMAILS"))
	if raw == "" {
		return nil
	}

	var emails []string
	for _, part := range strings.Split(raw, ",") {
		if e := strings.ToLower(strings.TrimSpace(part)); e != "" {
			emails = append(emails, e)
		}
	}

	return emails
}
