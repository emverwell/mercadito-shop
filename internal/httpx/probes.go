package httpx

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Readiness tracks whether the process is shutting down.
type Readiness struct{ draining atomic.Bool }

// SetDraining makes /readyz report 503 from now on.
func (r *Readiness) SetDraining() { r.draining.Store(true) }

// Check returns an error once draining has started.
func (r *Readiness) Check(context.Context) error {
	if r != nil && r.draining.Load() {
		return errors.New("draining")
	}
	return nil
}

// MountBase mounts /healthz (always 200), /readyz and /metrics.
// ready may be nil; it reports dependency and warm-up state.
func MountBase(r chi.Router, g prometheus.Gatherer, ready func(context.Context) error, rd *Readiness) {
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.Get("/readyz", func(w http.ResponseWriter, req *http.Request) {
		err := rd.Check(req.Context())
		if err == nil && ready != nil {
			err = ready(req.Context())
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	r.Method(http.MethodGet, "/metrics", promhttp.HandlerFor(g, promhttp.HandlerOpts{}))
}

// ProbeSelf implements the `healthcheck` subcommand used by container
// healthchecks in images that ship no shell or curl.
func ProbeSelf(args []string, port string) (handled bool, exit int) {
	if len(args) < 2 || args[1] != "healthcheck" {
		return false, 0
	}
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get("http://127.0.0.1:" + port + "/readyz")
	if err != nil {
		return true, 1
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return true, 1
	}
	return true, 0
}
