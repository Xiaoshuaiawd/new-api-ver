package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfigDefaults(t *testing.T) {
	var readPath string
	config, err := loadConfig(
		func(string) string { return "" },
		func(path string) ([]byte, error) {
			readPath = path
			return []byte("  https://open.feishu.cn/open-apis/bot/v2/hook/test-token\n"), nil
		},
	)

	require.NoError(t, err)
	assert.Equal(t, defaultWebhookURLFile, readPath)
	assert.Equal(t, ":8080", config.ListenAddress)
	assert.Equal(t, "https://open.feishu.cn/open-apis/bot/v2/hook/test-token", config.WebhookURL)
	assert.Equal(t, "Asia/Shanghai", config.Location.String())
}

func TestLoadConfigUsesEnvironment(t *testing.T) {
	environment := map[string]string{
		"FEISHU_WEBHOOK_URL_FILE":  "/secure/feishu-url",
		"FEISHU_ALERT_LISTEN_ADDR": "127.0.0.1:18080",
		"FEISHU_ALERT_TIMEZONE":    "UTC",
	}
	config, err := loadConfig(
		func(key string) string { return environment[key] },
		func(path string) ([]byte, error) {
			require.Equal(t, "/secure/feishu-url", path)
			return []byte("https://open.feishu.cn/open-apis/bot/v2/hook/test-token"), nil
		},
	)

	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:18080", config.ListenAddress)
	assert.Equal(t, "UTC", config.Location.String())
}

func TestLoadConfigRejectsMissingOrInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name        string
		environment map[string]string
		readFile    func(string) ([]byte, error)
		secret      string
	}{
		{
			name:     "missing file",
			readFile: func(string) ([]byte, error) { return nil, errors.New("permission denied") },
		},
		{
			name:     "empty file",
			readFile: func(string) ([]byte, error) { return []byte(" \n"), nil },
		},
		{
			name:        "timezone",
			environment: map[string]string{"FEISHU_ALERT_TIMEZONE": "Mars/Olympus"},
			readFile: func(string) ([]byte, error) {
				return []byte("https://open.feishu.cn/open-apis/bot/v2/hook/never-log-this"), nil
			},
			secret: "never-log-this",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := loadConfig(
				func(key string) string { return test.environment[key] },
				test.readFile,
			)
			require.Error(t, err)
			if test.secret != "" {
				assert.NotContains(t, err.Error(), test.secret)
			}
		})
	}
}

func TestCheckReady(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantError  bool
	}{
		{name: "ready", statusCode: http.StatusOK},
		{name: "not ready", statusCode: http.StatusServiceUnavailable, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				assert.Equal(t, "/readyz", request.URL.Path)
				response.WriteHeader(test.statusCode)
			}))
			defer server.Close()
			address := strings.TrimPrefix(server.URL, "http://")

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := checkReady(ctx, server.Client(), address)
			if test.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestReadyURLUsesLoopbackForWildcardListener(t *testing.T) {
	url, err := readyURL(":8080")
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:8080/readyz", url)
}
