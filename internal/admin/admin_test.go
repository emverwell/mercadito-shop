package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

func noEnv(string) string { return "" }

func do(r http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("X-Admin-Token", token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func router(token string, f *Faults) http.Handler {
	r := chi.NewRouter()
	Mount(r, token, f)
	return r
}

func TestNoRoutesWhenTokenEmpty(t *testing.T) {
	r := router("", NewFaults(noEnv, NewMemoryStore()))
	for _, c := range [][2]string{{"GET", "/admin/faults"}, {"POST", "/admin/faults"}, {"POST", "/admin/faults/reset"}} {
		if rec := do(r, c[0], c[1], "anything", "{}"); rec.Code != 404 {
			t.Fatalf("%s %s = %d, want 404", c[0], c[1], rec.Code)
		}
	}
}

func TestWhitespaceOnlyTokenCountsAsUnset(t *testing.T) {
	r := router("   ", NewFaults(noEnv, NewMemoryStore()))
	if rec := do(r, "GET", "/admin/faults", "   ", ""); rec.Code != 404 {
		t.Fatalf("got %d, want 404", rec.Code)
	}
}

func TestWrongOrMissingHeader401(t *testing.T) {
	r := router("s3cret", NewFaults(noEnv, NewMemoryStore()))
	if rec := do(r, "GET", "/admin/faults", "", ""); rec.Code != 401 {
		t.Fatalf("missing header = %d, want 401", rec.Code)
	}
	if rec := do(r, "GET", "/admin/faults", "nope", ""); rec.Code != 401 {
		t.Fatalf("wrong header = %d, want 401", rec.Code)
	}
}

func TestPostThenGet(t *testing.T) {
	f := NewFaults(noEnv, NewMemoryStore())
	r := router("tok", f)
	if rec := do(r, "POST", "/admin/faults", "tok", `{"thumb_burst":true,"payment_latency_ms":2000}`); rec.Code != 200 {
		t.Fatalf("post = %d %s", rec.Code, rec.Body)
	}
	s := f.Get()
	if !s.ThumbBurst || s.PaymentLatencyMS == nil || *s.PaymentLatencyMS != 2000 {
		t.Fatalf("snapshot = %+v", s)
	}
	rec := do(r, "GET", "/admin/faults", "tok", "")
	if !strings.Contains(rec.Body.String(), `"payment_latency_ms":2000`) {
		t.Fatalf("get body = %s", rec.Body)
	}
	// a second partial post keeps earlier values
	do(r, "POST", "/admin/faults", "tok", `{"cpu_burn":true}`)
	if s := f.Get(); !s.ThumbBurst || !s.CPUBurn {
		t.Fatalf("partial post lost state: %+v", s)
	}
}

func TestBadJSON400(t *testing.T) {
	r := router("tok", NewFaults(noEnv, NewMemoryStore()))
	if rec := do(r, "POST", "/admin/faults", "tok", `{not json`); rec.Code != 400 {
		t.Fatalf("got %d, want 400", rec.Code)
	}
}

func TestResetClears(t *testing.T) {
	f := NewFaults(noEnv, NewMemoryStore())
	r := router("tok", f)
	do(r, "POST", "/admin/faults", "tok", `{"cpu_burn":true}`)
	if rec := do(r, "POST", "/admin/faults/reset", "tok", ""); rec.Code != 200 {
		t.Fatalf("reset = %d", rec.Code)
	}
	if s := f.Get(); s.CPUBurn {
		t.Fatalf("reset left state: %+v", s)
	}
}

func TestEnvInitialValues(t *testing.T) {
	env := map[string]string{"FAULT_THUMB_BURST": "true", "FAULT_PAYMENT_LATENCY_MS": "750", "FAULT_PAYMENT_FAILURE_RATE": "0.25", "FAULT_CPU_BURN": "garbage"}
	f := NewFaults(func(k string) string { return env[k] }, NewMemoryStore())
	s := f.Get()
	if !s.ThumbBurst || s.CPUBurn || s.PaymentLatencyMS == nil || *s.PaymentLatencyMS != 750 || s.PaymentFailureRate == nil || *s.PaymentFailureRate != 0.25 {
		t.Fatalf("snapshot = %+v", s)
	}
}

func redisFor(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = c.Close() })
	return mr, c
}

func TestSyncPropagatesBetweenTwoInstances(t *testing.T) {
	_, c := redisFor(t)
	a := NewFaults(noEnv, NewRedisStore(c))
	b := NewFaults(noEnv, NewRedisStore(c))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.Sync(ctx, 20*time.Millisecond)
	if err := a.Set(ctx, Snapshot{CPUBurn: true}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(60 * time.Millisecond)
	for !b.Get().CPUBurn && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !b.Get().CPUBurn {
		t.Fatal("second instance never saw the toggle")
	}
	if err := a.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(200 * time.Millisecond)
	for b.Get().CPUBurn && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if b.Get().CPUBurn {
		t.Fatal("reset did not propagate")
	}
}

func TestSyncSurvivesRedisDown(t *testing.T) {
	mr, c := redisFor(t)
	f := NewFaults(noEnv, NewRedisStore(c))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = f.Set(ctx, Snapshot{ThumbBurst: true})
	go f.Sync(ctx, 10*time.Millisecond)
	mr.Close()
	time.Sleep(60 * time.Millisecond)
	if !f.Get().ThumbBurst {
		t.Fatal("state lost when redis went away")
	}
}

func TestSetupWithoutRedisUsesMemoryAndMountsWhenTokenSet(t *testing.T) {
	r := chi.NewRouter()
	f, err := Setup(context.Background(), r, "tok", "", noEnv)
	if err != nil || f == nil {
		t.Fatalf("setup: %v", err)
	}
	if rec := do(r, "GET", "/admin/faults", "tok", ""); rec.Code != 200 {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestSetupNoTokenNoRoutes(t *testing.T) {
	r := chi.NewRouter()
	if _, err := Setup(context.Background(), r, "", "", noEnv); err != nil {
		t.Fatal(err)
	}
	if rec := do(r, "GET", "/admin/faults", "x", ""); rec.Code != 404 {
		t.Fatalf("got %d, want 404", rec.Code)
	}
}

func TestSetupBadRedisURLErrors(t *testing.T) {
	if _, err := Setup(context.Background(), chi.NewRouter(), "tok", "not a url", noEnv); err == nil {
		t.Fatal("want error for malformed REDIS_URL")
	}
}

func TestSetupUnreachableRedisStillStarts(t *testing.T) {
	if _, err := Setup(context.Background(), chi.NewRouter(), "tok", "redis://127.0.0.1:1", noEnv); err != nil {
		t.Fatalf("unreachable redis must not stop startup: %v", err)
	}
}
