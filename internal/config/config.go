// Package config loads the server's settings from the process environment.
//
// Everything the server needs is read here and nowhere else. In particular the
// OIDC issuer is a plain configuration value with no default: tests point it at
// a mock OIDC server, so no provider hostname is baked into the binary.
package config

import (
	"fmt"
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
