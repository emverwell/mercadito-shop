package cart

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/redis/go-redis/v9"

	"github.com/emverwell/mercadito-shop/internal/httpx"
)

func stores(t *testing.T) map[string]Store {
	t.Helper()
	mr := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = c.Close() })
	return map[string]Store{"memory": NewMemoryStore(), "redis": NewRedisStore(c, time.Hour)}
}

func TestStoreContract(t *testing.T) {
	for name, s := range stores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			if _, err := s.Get(ctx, "nobody"); !errors.Is(err, ErrCartNotFound) {
				t.Fatalf("unknown session err = %v, want ErrCartNotFound", err)
			}
			if err := s.Add(ctx, "s1", 7, 2); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(ctx, "s1", 7, 3); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(ctx, "s1", 9, 1); err != nil {
				t.Fatal(err)
			}
			c, err := s.Get(ctx, "s1")
			if err != nil || len(c.Items) != 2 {
				t.Fatalf("cart = %+v err=%v", c, err)
			}
			if c.Items[0] != (Item{ProductID: 7, Qty: 5}) || c.Items[1] != (Item{ProductID: 9, Qty: 1}) {
				t.Fatalf("items = %+v (want 7x5, 9x1 sorted by product)", c.Items)
			}
			if err := s.Clear(ctx, "s1"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Get(ctx, "s1"); !errors.Is(err, ErrCartNotFound) {
				t.Fatalf("after clear err = %v", err)
			}
			if err := s.Ping(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func handler(t *testing.T, s Store) (http.Handler, *Handler) {
	t.Helper()
	h := &Handler{Store: s}
	r, reg, _ := httpx.NewRouter("cart", "pod-1", s.Ping, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	h.Register(reg, "memory")
	h.Routes(r)
	return r, h
}

func call(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func TestUnknownSessionCountsMiss(t *testing.T) {
	h, hd := handler(t, NewMemoryStore())
	rec := call(h, "GET", "/carts/ghost", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	if v := testutil.ToFloat64(hd.misses); v != 1 {
		t.Fatalf("cart_misses_total = %v, want 1", v)
	}
}

func TestAddThenGetOverHTTP(t *testing.T) {
	h, hd := handler(t, NewMemoryStore())
	if rec := call(h, "POST", "/carts/abc/items", `{"product_id":3,"qty":2}`); rec.Code != 204 {
		t.Fatalf("add = %d %s", rec.Code, rec.Body)
	}
	rec := call(h, "GET", "/carts/abc", "")
	if !strings.Contains(rec.Body.String(), `"product_id":3`) || !strings.Contains(rec.Body.String(), `"qty":2`) {
		t.Fatalf("body = %s", rec.Body)
	}
	if testutil.ToFloat64(hd.misses) != 0 {
		t.Fatal("known session counted as miss")
	}
	if rec := call(h, "DELETE", "/carts/abc", ""); rec.Code != 204 {
		t.Fatalf("delete = %d", rec.Code)
	}
}

func TestInvalidInputRejected(t *testing.T) {
	h, _ := handler(t, NewMemoryStore())
	for _, body := range []string{`{"product_id":3,"qty":0}`, `{"product_id":3,"qty":-1}`, `{"product_id":3,"qty":1000}`, `{"product_id":0,"qty":1}`, `{bad`} {
		if rec := call(h, "POST", "/carts/abc/items", body); rec.Code != 400 {
			t.Fatalf("%s = %d, want 400", body, rec.Code)
		}
	}
	for _, sid := range []string{"has%20space", strings.Repeat("a", 65), "a$b"} {
		if rec := call(h, "GET", "/carts/"+sid, ""); rec.Code != 400 {
			t.Fatalf("sid %q = %d, want 400", sid, rec.Code)
		}
	}
}

func TestRedisDownReadyz503NoPanic(t *testing.T) {
	mr := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1, DialTimeout: 200 * time.Millisecond})
	t.Cleanup(func() { _ = c.Close() })
	h, _ := handler(t, NewRedisStore(c, time.Hour))
	mr.Close()
	if rec := call(h, "GET", "/readyz", ""); rec.Code != 503 {
		t.Fatalf("readyz = %d, want 503", rec.Code)
	}
	if rec := call(h, "POST", "/carts/abc/items", `{"product_id":1,"qty":1}`); rec.Code != 503 {
		t.Fatalf("add = %d, want 503", rec.Code)
	}
	if rec := call(h, "GET", "/carts/abc", ""); rec.Code != 503 {
		t.Fatalf("get = %d, want 503", rec.Code)
	}
}

func TestBackendGaugeExported(t *testing.T) {
	reg := prometheus.NewRegistry()
	(&Handler{Store: NewMemoryStore()}).Register(reg, "redis")
	if n, err := testutil.GatherAndCount(reg, "cart_store_backend"); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}
