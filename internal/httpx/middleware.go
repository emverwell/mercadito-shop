// Package httpx holds the HTTP plumbing shared by every service: request
// middleware, standard probe/metrics endpoints and the server lifecycle.
package httpx

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics are the request metrics every service exposes.
type Metrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inflight prometheus.Gauge
}

// Buckets cover fast reads up to the 30 s default payment timeout.
var buckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}

// NewMetrics registers the shared request metrics, labelled with the service name.
func NewMetrics(service string, reg prometheus.Registerer) *Metrics {
	cl := prometheus.Labels{"service": service}
	m := &Metrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total", Help: "HTTP requests by route, method and status code.", ConstLabels: cl,
		}, []string{"route", "method", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "http_request_duration_seconds", Help: "HTTP request duration.", ConstLabels: cl, Buckets: buckets,
		}, []string{"route", "method"}),
		inflight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "http_in_flight_requests", Help: "Requests currently being served.", ConstLabels: cl,
		}),
	}
	reg.MustRegister(m.requests, m.duration, m.inflight)
	return m
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

// Middleware tags each response with X-Served-By and a request id, records
// metrics keyed by route pattern (never the raw path) and writes one JSON log
// line per request.
func Middleware(service, pod string, m *Metrics, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			id := r.Header.Get("X-Request-Id")
			if id == "" {
				id = newID()
			}
			w.Header().Set("X-Request-Id", id)
			w.Header().Set("X-Served-By", pod)
			sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
			m.inflight.Inc()
			defer m.inflight.Dec()

			next.ServeHTTP(sw, r)

			route := "unmatched"
			if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
				route = rc.RoutePattern()
			}
			d := time.Since(start)
			m.requests.WithLabelValues(route, r.Method, strconv.Itoa(sw.code)).Inc()
			m.duration.WithLabelValues(route, r.Method).Observe(d.Seconds())
			log.Info("request", "service", service, "pod", pod, "request_id", id,
				"method", r.Method, "route", route, "status", sw.code, "duration_ms", d.Milliseconds())
		})
	}
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
