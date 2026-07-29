package prometheusmetrics

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/netip"
	"os"
	"strings"
)

const (
	envEnabled                 = "PROMETHEUS_ENABLED"
	envBearerToken             = "PROMETHEUS_BEARER_TOKEN"
	envAllowedIPs              = "PROMETHEUS_ALLOWED_IPS"
	envAllowPublic             = "PROMETHEUS_ALLOW_PUBLIC"
	envDisableChannelHistogram = "PROMETHEUS_DISABLE_CHANNEL_HISTOGRAM"
)

type Config struct {
	Enabled                 bool
	AllowPublic             bool
	DisableChannelHistogram bool

	bearerToken     string
	allowedPrefixes []netip.Prefix
}

func LoadConfig() (Config, error) {
	return parseConfig(os.Getenv)
}

func parseConfig(getenv func(string) string) (Config, error) {
	enabled, err := parseStrictBool(envEnabled, getenv(envEnabled))
	if err != nil {
		return Config{}, err
	}
	if !enabled {
		return Config{}, nil
	}

	allowPublic, err := parseStrictBool(envAllowPublic, getenv(envAllowPublic))
	if err != nil {
		return Config{}, err
	}
	disableChannelHistogram, err := parseStrictBool(envDisableChannelHistogram, getenv(envDisableChannelHistogram))
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Enabled:                 true,
		AllowPublic:             allowPublic,
		DisableChannelHistogram: disableChannelHistogram,
		bearerToken:             strings.TrimSpace(getenv(envBearerToken)),
	}

	allowedIPs := strings.TrimSpace(getenv(envAllowedIPs))
	if allowedIPs != "" {
		for _, item := range strings.Split(allowedIPs, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			prefix, err := parseAllowedPrefix(item)
			if err != nil {
				return Config{}, fmt.Errorf("%s contains an invalid IP address or CIDR", envAllowedIPs)
			}
			cfg.allowedPrefixes = append(cfg.allowedPrefixes, prefix)
		}
	}

	if !cfg.AllowPublic && cfg.bearerToken == "" && len(cfg.allowedPrefixes) == 0 {
		return Config{}, fmt.Errorf("%s=true requires a bearer token, an IP allowlist, or explicit public access", envEnabled)
	}
	return cfg, nil
}

func parseStrictBool(name, value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, fmt.Errorf("%s must be true or false", name)
	}
}

func parseAllowedPrefix(value string) (netip.Prefix, error) {
	if strings.Contains(value, "/") {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return netip.Prefix{}, err
		}
		return prefix.Masked(), nil
	}
	address, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(address, address.BitLen()), nil
}

func (c Config) Authorize(clientIP, authorization string) bool {
	if !c.Enabled {
		return false
	}
	if c.AllowPublic {
		return true
	}
	if c.authorizeBearer(authorization) {
		return true
	}

	address, err := netip.ParseAddr(strings.TrimSpace(clientIP))
	if err != nil {
		return false
	}
	for _, prefix := range c.allowedPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func (c Config) authorizeBearer(authorization string) bool {
	if c.bearerToken == "" {
		return false
	}
	parts := strings.Fields(authorization)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return false
	}
	expected := sha256.Sum256([]byte(c.bearerToken))
	actual := sha256.Sum256([]byte(parts[1]))
	return subtle.ConstantTimeCompare(expected[:], actual[:]) == 1
}
