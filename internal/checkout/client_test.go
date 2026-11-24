package checkout

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func flaky(t *testing.T, failures int32, failCode int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) <= failures {
			w.WriteHeader(failCode)
			return
		}
		_, _ = w.Write([]byte(`{"status":"approved","id":"pay_1"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func client(url string, timeout time.Duration, retries int) *PaymentsClient {
	return &PaymentsClient{BaseURL: url, Timeout: timeout, Retries: retries, Backoff: time.Millisecond}
}

func TestRetriesWithBackoffThenSucceeds(t *testing.T) {
	srv, calls := flaky(t, 2, 502)
	if err := client(srv.URL, time.Second, 2).Charge(context.Background(), 500, "o-1"); err != nil {
		t.Fatalf("charge: %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
}

func TestNoRetriesByDefault(t *testing.T) {
	srv, calls := flaky(t, 5, 502)
	if err := client(srv.URL, time.Second, 0).Charge(context.Background(), 500, "o-1"); err == nil {
		t.Fatal("want error")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestRetriesExhaustedReturnsError(t *testing.T) {
	srv, calls := flaky(t, 10, 503)
	if err := client(srv.URL, time.Second, 2).Charge(context.Background(), 500, "o-1"); err == nil {
		t.Fatal("want error")
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3 (1 + 2 retries)", calls.Load())
	}
}

func TestClientErrorNotRetried(t *testing.T) {
	srv, calls := flaky(t, 10, 400)
	if err := client(srv.URL, time.Second, 3).Charge(context.Background(), 500, "o-1"); err == nil {
		t.Fatal("want error")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestTimeoutReturnsPromptly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body) // reading the body lets the server notice the client hanging up
		<-r.Context().Done()
	}))
	defer srv.Close()
	start := time.Now()
	if err := client(srv.URL, 100*time.Millisecond, 0).Charge(context.Background(), 500, "o-1"); err == nil {
		t.Fatal("want timeout error")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
}

func TestCallerContextCancelStopsRetries(t *testing.T) {
	srv, calls := flaky(t, 100, 502)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	c := &PaymentsClient{BaseURL: srv.URL, Timeout: time.Second, Retries: 50, Backoff: 20 * time.Millisecond}
	_ = c.Charge(ctx, 500, "o-1")
	if calls.Load() > 10 {
		t.Fatalf("kept retrying after context ended: %d calls", calls.Load())
	}
}

func TestCartClientUsesFreshConnections(t *testing.T) {
	var conns atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}
	srv.Start()
	defer srv.Close()
	c := &HTTPCart{BaseURL: srv.URL}
	for i := 0; i < 3; i++ {
		if _, err := c.Get(context.Background(), "abc"); err != nil {
			t.Fatal(err)
		}
	}
	if conns.Load() != 3 {
		t.Fatalf("opened %d connections for 3 calls, want 3", conns.Load())
	}
}
