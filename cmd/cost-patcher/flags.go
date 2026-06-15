package main

import (
	"flag"
	"log/slog"
	"os"
	"time"

	"github.com/m2khosravi/kubefisher/internal/costpatcher"
)

func parseFlags() (costpatcher.Config, *slog.Logger) {
	var (
		promURL        = flag.String("prometheus-url", envOrDefault("PROMETHEUS_URL", "http://localhost:9090"), "Prometheus base URL (no trailing slash)")
		pricingNS      = flag.String("pricing-namespace", envOrDefault("PRICING_CONFIGMAP_NAMESPACE", "kubefisher-system"), "Namespace of gpu-pricing ConfigMap")
		pricingName    = flag.String("pricing-configmap", envOrDefault("PRICING_CONFIGMAP_NAME", "gpu-pricing"), "Name of gpu-pricing ConfigMap")
		pricingKey     = flag.String("pricing-key", envOrDefault("PRICING_CONFIGMAP_KEY", "pricing.yaml"), "ConfigMap data key containing pricing YAML")
		reconcileEvery = flag.Duration("reconcile-interval", envOrDefaultDuration("RECONCILE_INTERVAL", 30*time.Second), "How often to reconcile annotations")
		pricingEvery   = flag.Duration("pricing-refresh-interval", envOrDefaultDuration("PRICING_REFRESH_INTERVAL", 60*time.Second), "How often to refresh pricing ConfigMap")
		httpAddr       = flag.String("http-addr", envOrDefault("HTTP_ADDR", ":8080"), "Listen address for /metrics and health endpoints")
		logFormat      = flag.String("log-format", envOrDefault("LOG_FORMAT", ""), "Log format: json|text (default uses LOG_FORMAT env; json when empty)")
	)
	flag.Parse()

	cfg := costpatcher.Config{
		PrometheusURL:       *promURL,
		PricingNamespace:    *pricingNS,
		PricingConfigMap:    *pricingName,
		PricingDataKey:      *pricingKey,
		ReconcileInterval:   *reconcileEvery,
		PricingPollInterval: *pricingEvery,
		HTTPAddr:            *httpAddr,
		LogFormat:           *logFormat,
	}
	log := costpatcher.NewLogger(cfg.LogFormat)
	return cfg, log
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envOrDefaultDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			return d
		}
	}
	return def
}
