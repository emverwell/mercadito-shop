package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/emverwell/mercadito-shop/internal/httpx"
)

type fakeStore struct{ products []Product }

func (f fakeStore) All(context.Context) ([]Product, error) { return f.products, nil }
func (f fakeStore) Get(_ context.Context, id int) (Product, error) {
	for _, p := range f.products {
		if p.ID == id {
			return p, nil
		}
	}
	return Product{}, ErrNotFound
}

func newTestHandler(t *testing.T, warm bool) (http.Handler, *Deps, *prometheus.Registry) {
	t.Helper()
	products := GenerateProducts(60)
	ix := NewIndex(products)
	if warm {
		ix.Warm(context.Background(), 0)
	}
	th := NewThumbs(0, func() bool { return false })
	d := &Deps{Store: fakeStore{products}, Index: ix, Thumbs: th, Products: products}
	r, reg, _ := httpx.NewRouter("catalog", "pod-1", d.Ready, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	d.Register(reg)
	d.Routes(r)
	return r, d, reg
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func TestRecommendationsEndpointObservesHistogram(t *testing.T) {
	h, d, reg := newTestHandler(t, true)
	rec := get(h, "/products/3/recommendations")
	if rec.Code != 200 {
		t.Fatalf("code = %d", rec.Code)
	}
	var got []Product
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got) != 6 {
		t.Fatalf("body: %v len=%d", err, len(got))
	}
	if n, _ := testutil.GatherAndCount(reg, "catalog_recommendation_seconds"); n == 0 {
		t.Fatal("catalog_recommendation_seconds not exported")
	}
	if c := testutil.CollectAndCount(d.recs); c == 0 {
		t.Fatal("histogram never observed")
	}
}

func TestSearchWhileWarmingStillAnswers(t *testing.T) {
	h, _, _ := newTestHandler(t, false)
	rec := get(h, "/search?q=fruit")
	if rec.Code != 200 {
		t.Fatalf("code = %d", rec.Code)
	}
	var got []Product
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got) == 0 {
		t.Fatal("no results while warming")
	}
}

func TestReadyzUnavailableUntilWarm(t *testing.T) {
	cold, _, _ := newTestHandler(t, false)
	if rec := get(cold, "/readyz"); rec.Code != 503 {
		t.Fatalf("cold readyz = %d, want 503", rec.Code)
	}
	if rec := get(cold, "/healthz"); rec.Code != 200 {
		t.Fatalf("healthz = %d, want 200 while warming", rec.Code)
	}
	warm, _, _ := newTestHandler(t, true)
	if rec := get(warm, "/readyz"); rec.Code != 200 {
		t.Fatalf("warm readyz = %d", rec.Code)
	}
}

func TestImagesWidthValidation(t *testing.T) {
	h, _, _ := newTestHandler(t, true)
	for _, q := range []string{"w=0", "w=-3", "w=abc", "w=99999"} {
		if rec := get(h, "/images/1?"+q); rec.Code != 400 {
			t.Fatalf("%s = %d, want 400", q, rec.Code)
		}
	}
	rec := get(h, "/images/1?w=64")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("code=%d type=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := get(h, "/images/1"); rec.Code != 200 {
		t.Fatalf("missing w should use the default width, got %d", rec.Code)
	}
}

func TestImageForUnknownProduct404(t *testing.T) {
	h, d, _ := newTestHandler(t, true)
	if rec := get(h, "/images/999999?w=64"); rec.Code != 404 {
		t.Fatalf("got %d, want 404", rec.Code)
	}
	if d.Thumbs.Bytes() != 0 {
		t.Fatal("unknown product must not populate the cache")
	}
}

func TestProductNotFoundAndBadID(t *testing.T) {
	h, _, _ := newTestHandler(t, true)
	if rec := get(h, "/products/9999"); rec.Code != 404 {
		t.Fatalf("unknown = %d", rec.Code)
	}
	if rec := get(h, "/products/abc"); rec.Code != 400 {
		t.Fatalf("bad id = %d", rec.Code)
	}
	if rec := get(h, "/products/9999/recommendations"); rec.Code != 404 {
		t.Fatalf("recs unknown = %d", rec.Code)
	}
	if rec := get(h, "/products/2"); rec.Code != 200 {
		t.Fatalf("known = %d", rec.Code)
	}
}

func TestListLimitClamped(t *testing.T) {
	h, _, _ := newTestHandler(t, true)
	var got []Product
	_ = json.Unmarshal(get(h, "/products?limit=100000").Body.Bytes(), &got)
	if len(got) != 60 {
		t.Fatalf("len = %d", len(got))
	}
	if rec := get(h, "/products?limit=-1"); rec.Code != 400 {
		t.Fatalf("negative limit = %d", rec.Code)
	}
}

func TestStoreErrorIs500(t *testing.T) {
	h, d, _ := newTestHandler(t, true)
	d.Store = errStore{}
	if rec := get(h, "/products/1"); rec.Code != 500 {
		t.Fatalf("got %d", rec.Code)
	}
}

type errStore struct{}

func (errStore) All(context.Context) ([]Product, error)  { return nil, errors.New("boom") }
func (errStore) Get(context.Context, int) (Product, error) { return Product{}, errors.New("boom") }

