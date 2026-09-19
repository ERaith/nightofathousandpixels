package config_test

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/ERaith/nightofathousandpixels/internal/config"
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
