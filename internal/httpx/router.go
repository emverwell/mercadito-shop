package httpx

import (
	"context"
	"log/slog"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// NewLogger returns the JSON logger every service uses.
func NewLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(os.Stdout, nil)) }

// NewRouter builds a router with the shared middleware and the base routes
// (/healthz, /readyz, /metrics) already mounted. The registry also carries Go
// runtime and process collectors.
func NewRouter(service, pod string, ready func(context.Context) error, log *slog.Logger) (chi.Router, *prometheus.Registry, *Readiness) {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m := NewMetrics(service, reg)
	rd := &Readiness{}
	r := chi.NewRouter()
	r.Use(Middleware(service, pod, m, log))
	MountBase(r, reg, ready, rd)
	return r, reg, rd
}
