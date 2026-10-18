package common

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const DefaultSessionCookieName = "session"
const DefaultRefreshCookieName = "new_api_refresh"

func BuildSessionCookieName(secret string, configuredName string) string {
	configuredName = strings.TrimSpace(configuredName)
	if configuredName != "" {
		return configuredName
	}

	secret = strings.TrimSpace(secret)
	if secret == "" {
		return DefaultSessionCookieName
	}

	sum := sha256.Sum256([]byte(secret))
	return "session_" + hex.EncodeToString(sum[:6])
}
