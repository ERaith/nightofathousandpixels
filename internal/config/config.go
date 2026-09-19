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
