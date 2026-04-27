package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/service"
)

const (
	defaultListenAddr            = ":9098"
	defaultWebhookPath           = "/alertmanager/feishu"
	defaultHealthPath            = "/healthz"
	defaultMessagePrefix         = "[new-api 监控告警]"
	defaultMinIntervalSeconds    = 120
	defaultMaxAlertsPerMessage   = 10
	defaultRequestTimeoutSeconds = 10
)

func main() {
	adapterConfig, listenAddr, webhookPath := loadConfigFromEnv()

	adapter, err := service.NewAlertmanagerFeishuAdapter(adapterConfig)
	if err != nil {
		log.Fatalf("build feishu alert adapter failed: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle(webhookPath, adapter)
	mux.HandleFunc(defaultHealthPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("starting feishu alert adapter listen=%s webhook_path=%s health_path=%s", listenAddr, webhookPath, defaultHealthPath)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("feishu alert adapter exited with error: %v", err)
	}
}

func loadConfigFromEnv() (service.AlertmanagerFeishuAdapterConfig, string, string) {
	listenAddr := envString("ALERTMANAGER_FEISHU_ADAPTER_LISTEN", defaultListenAddr)
	webhookPath := normalizePath(envString("ALERTMANAGER_FEISHU_ADAPTER_PATH", defaultWebhookPath))
	webhookURL := strings.TrimSpace(os.Getenv("ALERTMANAGER_FEISHU_WEBHOOK_URL"))
	if webhookURL == "" {
		log.Fatal("ALERTMANAGER_FEISHU_WEBHOOK_URL is required")
	}

	return service.AlertmanagerFeishuAdapterConfig{
		WebhookURL:          webhookURL,
		BearerToken:         envString("ALERTMANAGER_FEISHU_ADAPTER_BEARER_TOKEN", ""),
		MessagePrefix:       envString("ALERTMANAGER_FEISHU_MESSAGE_PREFIX", defaultMessagePrefix),
		MinInterval:         time.Duration(envInt("ALERTMANAGER_FEISHU_MIN_INTERVAL_SECONDS", defaultMinIntervalSeconds)) * time.Second,
		MaxAlertsPerMessage: envInt("ALERTMANAGER_FEISHU_MAX_ALERTS_PER_MESSAGE", defaultMaxAlertsPerMessage),
		RequestTimeout:      time.Duration(envInt("ALERTMANAGER_FEISHU_REQUEST_TIMEOUT_SECONDS", defaultRequestTimeoutSeconds)) * time.Second,
	}, listenAddr, webhookPath
}

func envString(key string, defaultValue string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	return value
}

func envInt(key string, defaultValue int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		log.Fatalf("invalid integer env %s=%q: %v", key, value, err)
	}
	return parsed
}

func normalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return defaultWebhookPath
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}
