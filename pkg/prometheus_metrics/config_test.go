package prometheusmetrics

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseConfigSecurityPolicy(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		enabled   bool
		wantError string
	}{
		{name: "disabled by default"},
		{
			name:      "enabled without protection fails closed",
			env:       map[string]string{"PROMETHEUS_ENABLED": "true"},
			wantError: "requires a bearer token, an IP allowlist, or explicit public access",
		},
		{
			name:    "bearer token protects endpoint",
			env:     map[string]string{"PROMETHEUS_ENABLED": "true", "PROMETHEUS_BEARER_TOKEN": "secret"},
			enabled: true,
		},
		{
			name:    "IP allowlist protects endpoint",
			env:     map[string]string{"PROMETHEUS_ENABLED": "true", "PROMETHEUS_ALLOWED_IPS": "127.0.0.1,10.0.0.0/8"},
			enabled: true,
		},
		{
			name:    "public access must be explicit",
			env:     map[string]string{"PROMETHEUS_ENABLED": "true", "PROMETHEUS_ALLOW_PUBLIC": "true"},
			enabled: true,
		},
		{
			name:      "invalid boolean is rejected",
			env:       map[string]string{"PROMETHEUS_ENABLED": "yes"},
			wantError: "PROMETHEUS_ENABLED",
		},
		{
			name:      "invalid network is rejected",
			env:       map[string]string{"PROMETHEUS_ENABLED": "true", "PROMETHEUS_ALLOWED_IPS": "10.0.0.0/99"},
			wantError: "PROMETHEUS_ALLOWED_IPS",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg, err := parseConfig(func(key string) string { return test.env[key] })
			if test.wantError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), test.wantError)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, test.enabled, cfg.Enabled)
		})
	}
}

func TestConfigAuthorizeUsesBearerOrTrustedClientIP(t *testing.T) {
	cfg, err := parseConfig(func(key string) string {
		return map[string]string{
			"PROMETHEUS_ENABLED":      "true",
			"PROMETHEUS_BEARER_TOKEN": "secret",
			"PROMETHEUS_ALLOWED_IPS":  "127.0.0.1,10.0.0.0/8",
		}[key]
	})
	require.NoError(t, err)

	tests := []struct {
		name       string
		clientIP   string
		authority  string
		authorized bool
	}{
		{name: "exact IP", clientIP: "127.0.0.1", authorized: true},
		{name: "CIDR IP", clientIP: "10.23.4.5", authorized: true},
		{name: "valid bearer", clientIP: "203.0.113.10", authority: "Bearer secret", authorized: true},
		{name: "bearer scheme is case insensitive", clientIP: "203.0.113.10", authority: "bearer secret", authorized: true},
		{name: "wrong bearer", clientIP: "203.0.113.10", authority: "Bearer wrong", authorized: false},
		{name: "malformed IP", clientIP: "not-an-ip", authority: "", authorized: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.authorized, cfg.Authorize(test.clientIP, test.authority))
		})
	}
}

func TestParseConfigErrorsDoNotExposeBearerToken(t *testing.T) {
	const secret = "do-not-print-this-secret"
	_, err := parseConfig(func(key string) string {
		return map[string]string{
			"PROMETHEUS_ENABLED":      "true",
			"PROMETHEUS_BEARER_TOKEN": secret,
			"PROMETHEUS_ALLOWED_IPS":  "invalid-network",
		}[key]
	})
	require.Error(t, err)
	assert.False(t, strings.Contains(err.Error(), secret))
}

func TestPublicConfigAuthorizesEveryClient(t *testing.T) {
	cfg, err := parseConfig(func(key string) string {
		return map[string]string{
			"PROMETHEUS_ENABLED":      "true",
			"PROMETHEUS_ALLOW_PUBLIC": "true",
		}[key]
	})
	require.NoError(t, err)

	assert.True(t, cfg.Authorize("not-an-ip", ""))
}

func TestParseConfigChannelHistogramPolicy(t *testing.T) {
	tests := []struct {
		name         string
		value        string
		wantDisabled bool
		wantError    bool
	}{
		{name: "enabled by default"},
		{name: "explicitly disabled", value: "true", wantDisabled: true},
		{name: "explicitly retained", value: "false"},
		{name: "invalid value", value: "yes", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg, err := parseConfig(func(key string) string {
				return map[string]string{
					"PROMETHEUS_ENABLED":                   "true",
					"PROMETHEUS_ALLOW_PUBLIC":              "true",
					"PROMETHEUS_DISABLE_CHANNEL_HISTOGRAM": test.value,
				}[key]
			})
			if test.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "PROMETHEUS_DISABLE_CHANNEL_HISTOGRAM")
				return
			}

			require.NoError(t, err)
			assert.Equal(t, test.wantDisabled, cfg.DisableChannelHistogram)
		})
	}
}
