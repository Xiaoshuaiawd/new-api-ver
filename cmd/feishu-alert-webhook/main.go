package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/QuantumNous/new-api/pkg/feishualert"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	defaultWebhookURLFile = "/run/secrets/feishu_webhook_url"
	defaultListenAddress  = ":8080"
	defaultTimezone       = "Asia/Shanghai"
)

type appConfig struct {
	ListenAddress string
	WebhookURL    string
	Location      *time.Location
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := checkReady(ctx, &http.Client{Timeout: 3 * time.Second}, environmentOrDefault(os.Getenv, "FEISHU_ALERT_LISTEN_ADDR", defaultListenAddress)); err != nil {
			logger.Error("飞书告警服务健康检查失败", "error_type", "not_ready")
			os.Exit(1)
		}
		return
	}

	config, err := loadConfig(os.Getenv, os.ReadFile)
	if err != nil {
		logger.Error("飞书告警服务配置加载失败", "error", err.Error())
		os.Exit(1)
	}
	if err := runServer(config, logger); err != nil {
		logger.Error("飞书告警服务退出", "error", err.Error())
		os.Exit(1)
	}
}

func loadConfig(getenv func(string) string, readFile func(string) ([]byte, error)) (appConfig, error) {
	webhookFile := environmentOrDefault(getenv, "FEISHU_WEBHOOK_URL_FILE", defaultWebhookURLFile)
	webhookBytes, err := readFile(webhookFile)
	if err != nil {
		return appConfig{}, fmt.Errorf("读取飞书 Webhook secret %s 失败", webhookFile)
	}
	webhookURL := strings.TrimSpace(string(webhookBytes))
	if webhookURL == "" {
		return appConfig{}, fmt.Errorf("飞书 Webhook secret %s 为空", webhookFile)
	}
	timezone := environmentOrDefault(getenv, "FEISHU_ALERT_TIMEZONE", defaultTimezone)
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return appConfig{}, fmt.Errorf("飞书告警时区配置无效")
	}
	listenAddress := environmentOrDefault(getenv, "FEISHU_ALERT_LISTEN_ADDR", defaultListenAddress)
	if _, err := readyURL(listenAddress); err != nil {
		return appConfig{}, fmt.Errorf("飞书告警监听地址无效")
	}
	return appConfig{
		ListenAddress: listenAddress,
		WebhookURL:    webhookURL,
		Location:      location,
	}, nil
}

func runServer(config appConfig, logger *slog.Logger) error {
	service, err := feishualert.NewService(feishualert.ServiceConfig{
		WebhookURL: config.WebhookURL,
		Registry:   prometheus.NewRegistry(),
		Logger:     logger,
		Location:   config.Location,
	})
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              config.ListenAddress,
		Handler:           service.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("飞书告警服务已启动", "listen_address", config.ListenAddress)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("关闭飞书告警服务失败: %w", err)
		}
		return nil
	}
}

func checkReady(ctx context.Context, client *http.Client, listenAddress string) error {
	endpoint, err := readyURL(listenAddress)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("飞书告警服务未就绪")
	}
	return nil
}

func readyURL(listenAddress string) (string, error) {
	host, port, err := net.SplitHostPort(listenAddress)
	if err != nil || port == "" {
		return "", fmt.Errorf("监听地址格式无效")
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/readyz", nil
}

func environmentOrDefault(getenv func(string) string, key, fallback string) string {
	if value := strings.TrimSpace(getenv(key)); value != "" {
		return value
	}
	return fallback
}
