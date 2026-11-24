package httpx

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func newRouter(t *testing.T, ready func(context.Context) error) (chi.Router, *prometheus.Registry, *Readiness) {
	t.Helper()
	reg := prometheus.NewRegistry()
	m := NewMetrics("test", reg)
	rd := &Readiness{}
	r := chi.NewRouter()
	r.Use(Middleware("test", "pod-1", m, slog.New(slog.NewJSONHandler(io.Discard, nil))))
	MountBase(r, reg, ready, rd)
	r.Get("/products/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	return r, reg, rd
}

func TestMiddlewareSetsServedByAndRequestID(t *testing.T) {
	r, _, _ := newRouter(t, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/products/1", nil))
	if got := rec.Header().Get("X-Served-By"); got != "pod-1" {
		t.Fatalf("X-Served-By = %q", got)
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("no request id generated")
	}
	req := httptest.NewRequest("GET", "/products/1", nil)
	req.Header.Set("X-Request-Id", "abc")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Header().Get("X-Request-Id") != "abc" {
		t.Fatal("incoming request id not preserved")
	}
}

func TestMetricsUseRoutePatternNotRawPath(t *testing.T) {
	r, reg, _ := newRouter(t, nil)
	for _, p := range []string{"/products/1", "/products/2"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", p, nil))
	}
	n, err := testutil.GatherAndCount(reg, "http_requests_total")
	if err != nil || n != 1 {
		t.Fatalf("series = %d err=%v, want 1 series for /products/{id}", n, err)
	}
}

func TestReadyzReflectsCheckAndDraining(t *testing.T) {
	var failing bool
	r, _, rd := newRouter(t, func(context.Context) error {
		if failing {
			return errors.New("db down")
		}
		return nil
	})
	code := func() int {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest("GET", "/readyz", nil))
		return rec.Code
	}
	if code() != 200 {
		t.Fatal("want 200 when ready")
	}
	failing = true
	if code() != 503 {
		t.Fatal("want 503 when check fails")
	}
	failing = false
	rd.SetDraining()
	if code() != 503 {
		t.Fatal("want 503 when draining")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 200 {
		t.Fatal("healthz must stay 200 while draining")
	}
}

func startServer(t *testing.T, h http.Handler) (*http.Server, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: time.Second}, "http://" + addr
}

// runAsync starts Run and waits until the server accepts connections.
func runAsync(t *testing.T, ctx context.Context, srv *http.Server, drain time.Duration, rd *Readiness) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, srv, drain, rd, slog.New(slog.NewJSONHandler(io.Discard, nil))) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.Dial("tcp", srv.Addr); err == nil {
			_ = c.Close()
			return done
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server did not start")
	return nil
}

func TestRunDrainZeroCutsInFlight(t *testing.T) {
	started := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(200)
	})
	srv, url := startServer(t, h)
	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(t, ctx, srv, 0, &Readiness{})
	errc := make(chan error, 1)
	go func() {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
		}
		errc <- err
	}()
	<-started
	cancel()
	if err := <-errc; err == nil {
		t.Fatal("in-flight request should have been cut")
	}
	<-done
}

func TestRunDrainKeepsInFlightAndFlipsReadyz(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	rd := &Readiness{}
	mux := http.NewServeMux()
	mux.HandleFunc("/slow", func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(started) })
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(200)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if rd.Check(r.Context()) != nil {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
	})
	srv, url := startServer(t, mux)
	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(t, ctx, srv, 500*time.Millisecond, rd)
	codec := make(chan int, 1)
	go func() {
		resp, err := http.Get(url + "/slow")
		if err != nil {
			codec <- -1
			return
		}
		_ = resp.Body.Close()
		codec <- resp.StatusCode
	}()
	<-started
	cancel()
	time.Sleep(100 * time.Millisecond)
	resp, err := http.Get(url + "/readyz")
	if err != nil {
		t.Fatalf("server should still accept during drain: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Fatalf("readyz during drain = %d, want 503", resp.StatusCode)
	}
	if c := <-codec; c != 200 {
		t.Fatalf("in-flight request = %d, want 200", c)
	}
	<-done
}

func TestProbeSelf(t *testing.T) {
	srv, url := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(t, ctx, srv, 0, &Readiness{})
	defer func() { cancel(); <-done }()
	port := url[strings.LastIndex(url, ":")+1:]
	if handled, code := ProbeSelf([]string{"app", "healthcheck"}, port); !handled || code != 0 {
		t.Fatalf("handled=%v code=%d", handled, code)
	}
	if handled, _ := ProbeSelf([]string{"app"}, port); handled {
		t.Fatal("must not handle without subcommand")
	}
	if _, code := ProbeSelf([]string{"app", "healthcheck"}, "1"); code == 0 {
		t.Fatal("unreachable port must exit non-zero")
	}
}

func TestNewRouterWiresMiddlewareProbesAndMetrics(t *testing.T) {
	r, reg, _ := NewRouter("svc", "pod-9", nil, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	r.Get("/x", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != 204 || rec.Header().Get("X-Served-By") != "pod-9" {
		t.Fatalf("code=%d served-by=%q", rec.Code, rec.Header().Get("X-Served-By"))
	}
	for _, p := range []string{"/healthz", "/readyz", "/metrics"} {
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code != 200 {
			t.Fatalf("%s = %d", p, rec.Code)
		}
	}
	if n, err := testutil.GatherAndCount(reg, "http_requests_total"); err != nil || n == 0 {
		t.Fatalf("no request metrics: n=%d err=%v", n, err)
	}
}

func TestNewServerHasHeaderTimeoutAndAddr(t *testing.T) {
	s := NewServer("8099", http.NewServeMux())
	if s.Addr != ":8099" || s.ReadHeaderTimeout <= 0 {
		t.Fatalf("addr=%q readHeaderTimeout=%v", s.Addr, s.ReadHeaderTimeout)
	}
}
