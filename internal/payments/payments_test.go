package payments

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/emverwell/mercadito-shop/internal/admin"
)

func newHandler(h *Handler) http.Handler {
	r := chi.NewRouter()
	h.Routes(r)
	return r
}

func charge(h http.Handler, ctx context.Context, body string) (*httptest.ResponseRecorder, time.Duration) {
	req := httptest.NewRequest("POST", "/charge", strings.NewReader(body)).WithContext(ctx)
	rec := httptest.NewRecorder()
	start := time.Now()
	h.ServeHTTP(rec, req)
	return rec, time.Since(start)
}

const okBody = `{"amount_cents":1250,"order_ref":"o-1"}`

func noFaults() admin.Snapshot { return admin.Snapshot{} }

func TestFixedLatency(t *testing.T) {
	h := newHandler(&Handler{LatencyMS: 50, Faults: noFaults})
	rec, d := charge(h, context.Background(), okBody)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"approved"`) {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	if d < 45*time.Millisecond || d > 150*time.Millisecond {
		t.Fatalf("took %v, want ~50ms", d)
	}
}

func TestFaultOverridesEnv(t *testing.T) {
	ten := 10
	h := newHandler(&Handler{LatencyMS: 500, Faults: func() admin.Snapshot { return admin.Snapshot{PaymentLatencyMS: &ten} }})
	_, d := charge(h, context.Background(), okBody)
	if d > 200*time.Millisecond {
		t.Fatalf("took %v; fault latency of 10ms should win over 500ms", d)
	}
}

func TestJitterStaysWithinBound(t *testing.T) {
	h := newHandler(&Handler{LatencyMS: 0, JitterMS: 60, Faults: noFaults})
	for i := 0; i < 5; i++ {
		if _, d := charge(h, context.Background(), okBody); d > 200*time.Millisecond {
			t.Fatalf("took %v with jitter 60ms", d)
		}
	}
}

func TestFailureRateOneAlways502(t *testing.T) {
	h := newHandler(&Handler{FailureRate: 1, Faults: noFaults})
	for i := 0; i < 5; i++ {
		if rec, _ := charge(h, context.Background(), okBody); rec.Code != 502 {
			t.Fatalf("code = %d, want 502", rec.Code)
		}
	}
}

func TestFailureRateFaultOverrides(t *testing.T) {
	one := 1.0
	h := newHandler(&Handler{Faults: func() admin.Snapshot { return admin.Snapshot{PaymentFailureRate: &one} }})
	if rec, _ := charge(h, context.Background(), okBody); rec.Code != 502 {
		t.Fatalf("code = %d, want 502", rec.Code)
	}
}

func TestClientCancelStopsSleep(t *testing.T) {
	h := newHandler(&Handler{LatencyMS: 5000, Faults: noFaults})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, d := charge(h, ctx, okBody)
	if d > time.Second {
		t.Fatalf("handler kept sleeping for %v after the client gave up", d)
	}
}

func TestBadBody400(t *testing.T) {
	h := newHandler(&Handler{Faults: noFaults})
	for _, b := range []string{`{bad`, `{"amount_cents":0,"order_ref":"x"}`, `{"amount_cents":5,"order_ref":""}`, `{"amount_cents":-5,"order_ref":"x"}`} {
		if rec, _ := charge(h, context.Background(), b); rec.Code != 400 {
			t.Fatalf("%s = %d, want 400", b, rec.Code)
		}
	}
}
