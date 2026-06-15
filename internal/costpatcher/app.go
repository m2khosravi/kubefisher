package costpatcher

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"

	"github.com/m2khosravi/kubefisher/internal/adapters"
	"github.com/m2khosravi/kubefisher/internal/costpatcher/pricing"
	"github.com/m2khosravi/kubefisher/internal/kubeclient"
	"github.com/m2khosravi/kubefisher/pkg/promclient"
)

// Config holds runtime settings for the cost patcher process.
type Config struct {
	PrometheusURL       string
	PricingNamespace    string
	PricingConfigMap    string
	PricingDataKey      string
	ReconcileInterval   time.Duration
	PricingPollInterval time.Duration
	HTTPAddr            string
	LogFormat           string // json|text (empty uses env LOG_FORMAT)
}

// App wires Kubernetes, Prometheus, pricing export, HTTP, and reconciliation.
type App struct {
	log *slog.Logger
	cfg Config
}

func NewApp(log *slog.Logger, cfg Config) *App {
	return &App{log: log, cfg: cfg}
}

func (a *App) Run(ctx context.Context) error {
	k8s, _, err := kubeclient.NewCached(ctx)
	if err != nil {
		return err
	}

	pc := pricing.NewGaugeCollector()
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	reg.MustRegister(pc)

	promc, err := promclient.NewClient(a.cfg.PrometheusURL)
	if err != nil {
		return err
	}

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		t := time.NewTicker(a.cfg.PricingPollInterval)
		defer t.Stop()

		refresh := func() {
			doc, err := pricing.FetchConfigMap(ctx, k8s, a.cfg.PricingNamespace, a.cfg.PricingConfigMap, a.cfg.PricingDataKey)
			if err != nil {
				a.log.Error("refresh pricing failed", "err", err)
				return
			}
			pc.SetDocument(doc)

			// Compute per-node prices for joins without kube_node_labels.
			var nodes corev1.NodeList
			if err := k8s.List(ctx, &nodes); err != nil {
				a.log.Error("list nodes failed", "err", err)
				return
			}
			nodePrice := map[string]float64{}
			for i := range nodes.Items {
				n := nodes.Items[i]
				if p, ok := pricing.PriceForNode(doc, &n); ok {
					nodePrice[n.Name] = p
				}
			}
			pc.SetNodePrices(nodePrice)

			a.log.Info("refreshed gpu pricing", "rules", len(doc.GPUs), "currency", doc.Currency, "nodes_priced", len(nodePrice))
		}
		refresh()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-t.C:
				refresh()
			}
		}
	})

	g.Go(func() error {
		r := &Reconciler{
			K8s:  k8s,
			Prom: promc,
			Adapters: adapters.Registry,
			Log: a.log,
		}
		return r.Run(ctx, a.cfg.ReconcileInterval)
	})

	g.Go(func() error { return runHTTPServer(ctx, a.log, reg, promc, a.cfg.HTTPAddr) })

	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func runHTTPServer(ctx context.Context, log *slog.Logger, reg *prometheus.Registry, promc *promclient.Client, addr string) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := promc.QueryInstant(r.Context(), "vector(1)"); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	log.Info("http listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// NewLogger builds a slog logger that follows repo rules:
// JSON handler in production, Text handler for local dev via LOG_FORMAT=text.
func NewLogger(logFormat string) *slog.Logger {
	format := logFormat
	if format == "" {
		format = os.Getenv("LOG_FORMAT")
	}
	var h slog.Handler
	switch format {
	case "text":
		h = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{})
	default:
		h = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{})
	}
	return slog.New(h)
}
