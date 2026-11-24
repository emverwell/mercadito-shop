package storefront

import (
	"fmt"
	"io"
	"net"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/emverwell/mercadito-shop/internal/httpx"
)

type fakes struct {
	catalog, cart, checkout *httptest.Server
	cartCalls               atomic.Int32
	checkoutStatus          int
	catalogFail             bool
}

func newFakes(t *testing.T) *fakes {
	t.Helper()
	f := &fakes{checkoutStatus: 201}
	product := func(id int) string {
		return fmt.Sprintf(`{"id":%d,"name":"Fresh Fruit Basket #%d","category":"Fruit","price_cents":1250,"stock":9}`, id, id)
	}
	cm := http.NewServeMux()
	cm.HandleFunc("GET /products", func(w http.ResponseWriter, _ *http.Request) {
		if f.catalogFail {
			http.Error(w, "boom", 500)
			return
		}
		_, _ = io.WriteString(w, "["+product(1)+","+product(2)+"]")
	})
	cm.HandleFunc("GET /products/{id}", func(w http.ResponseWriter, r *http.Request) {
		if f.catalogFail {
			http.Error(w, "boom", 500)
			return
		}
		if r.PathValue("id") == "404" {
			http.NotFound(w, r)
			return
		}
		var id int
		_, _ = fmt.Sscan(r.PathValue("id"), &id)
		_, _ = io.WriteString(w, product(id))
	})
	cm.HandleFunc("GET /products/{id}/recommendations", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `[{"id":77,"name":"Rustic Bakery Loaf #77","category":"Bakery","price_cents":400,"stock":3}]`)
	})
	cm.HandleFunc("GET /search", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "[]") })
	cm.HandleFunc("GET /images/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = io.WriteString(w, "PNG:"+r.URL.Query().Get("w"))
	})
	f.catalog = httptest.NewServer(cm)

	km := http.NewServeMux()
	km.HandleFunc("/carts/", func(w http.ResponseWriter, r *http.Request) {
		f.cartCalls.Add(1)
		switch r.Method {
		case http.MethodGet:
			_, _ = io.WriteString(w, `{"items":[{"product_id":1,"qty":2}]}`)
		default:
			w.WriteHeader(204)
		}
	})
	f.cart = httptest.NewServer(km)

	om := http.NewServeMux()
	om.HandleFunc("POST /checkout", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(f.checkoutStatus)
		if f.checkoutStatus == 201 {
			_, _ = io.WriteString(w, `{"order_id":"ord-1","total_cents":2500}`)
		}
	})
	f.checkout = httptest.NewServer(om)
	t.Cleanup(func() { f.catalog.Close(); f.cart.Close(); f.checkout.Close() })
	return f
}

func (f *fakes) handler(t *testing.T) http.Handler {
	t.Helper()
	s, err := New(Config{CatalogURL: f.catalog.URL, CartURL: f.cart.URL, CheckoutURL: f.checkout.URL, PodName: "pod-7"})
	if err != nil {
		t.Fatal(err)
	}
	r, _, _ := httpx.NewRouter("storefront", "pod-7", nil, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	s.Routes(r)
	return r
}

func do(h http.Handler, method, path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func cookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestSessionCookieIssuedWhenMissing(t *testing.T) {
	h := newFakes(t).handler(t)
	rec := do(h, "GET", "/", nil)
	c := cookie(rec, "sid")
	if rec.Code != 200 || c == nil || !c.HttpOnly || c.Value == "" || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("code=%d cookie=%+v", rec.Code, c)
	}
	rec = do(h, "GET", "/", nil, &http.Cookie{Name: "sid", Value: "abc123"})
	if cookie(rec, "sid") != nil {
		t.Fatal("existing session must not be replaced")
	}
}

func TestIndexListsProductsAndServedBy(t *testing.T) {
	rec := do(newFakes(t).handler(t), "GET", "/", nil)
	body := rec.Body.String()
	if !strings.Contains(body, "Fresh Fruit Basket #1") || !strings.Contains(body, "pod-7") {
		t.Fatalf("body missing product or pod name:\n%s", body)
	}
}

func TestProductPageRendersRecommendations(t *testing.T) {
	rec := do(newFakes(t).handler(t), "GET", "/product/1", nil)
	body := rec.Body.String()
	for _, want := range []string{"Fresh Fruit Basket #1", "Rustic Bakery Loaf #77", `name="product_id"`, "€12.50"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
}

func TestProductNotFoundRenders404(t *testing.T) {
	rec := do(newFakes(t).handler(t), "GET", "/product/404", nil)
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), "Not found") {
		t.Fatalf("code=%d", rec.Code)
	}
	if rec := do(newFakes(t).handler(t), "GET", "/product/abc", nil); rec.Code != 404 {
		t.Fatalf("non-numeric id = %d, want 404", rec.Code)
	}
}

func TestDownstreamErrorRenders502Page(t *testing.T) {
	f := newFakes(t)
	f.catalogFail = true
	rec := do(f.handler(t), "GET", "/", nil)
	if rec.Code != 502 || !strings.Contains(rec.Body.String(), "try again") {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestSearchEscapesQuery(t *testing.T) {
	rec := do(newFakes(t).handler(t), "GET", "/search?q="+url.QueryEscape("<script>alert(1)</script>"), nil)
	if strings.Contains(rec.Body.String(), "<script>alert(1)") {
		t.Fatal("query reflected unescaped")
	}
}

func TestAddToCartSetsHasCartCookieAndRedirects(t *testing.T) {
	h := newFakes(t).handler(t)
	rec := do(h, "POST", "/cart/add", url.Values{"product_id": {"1"}, "qty": {"2"}}, &http.Cookie{Name: "sid", Value: "abc"})
	if rec.Code != 303 || rec.Header().Get("Location") != "/cart" || cookie(rec, "has_cart") == nil {
		t.Fatalf("code=%d loc=%q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := do(h, "POST", "/cart/add", url.Values{"product_id": {"x"}, "qty": {"2"}}, &http.Cookie{Name: "sid", Value: "abc"}); rec.Code != 400 {
		t.Fatalf("bad product_id = %d", rec.Code)
	}
}

func TestCartCountSkipsCartServiceForNewVisitors(t *testing.T) {
	f := newFakes(t)
	h := f.handler(t)
	rec := do(h, "GET", "/api/cart-count", nil)
	if !strings.Contains(rec.Body.String(), `"count":0`) || f.cartCalls.Load() != 0 {
		t.Fatalf("body=%s cartCalls=%d", rec.Body, f.cartCalls.Load())
	}
	rec = do(h, "GET", "/api/cart-count", nil, &http.Cookie{Name: "sid", Value: "abc"}, &http.Cookie{Name: "has_cart", Value: "1"})
	if !strings.Contains(rec.Body.String(), `"count":2`) {
		t.Fatalf("body=%s", rec.Body)
	}
}

func TestCartPageShowsTotals(t *testing.T) {
	rec := do(newFakes(t).handler(t), "GET", "/cart", nil, &http.Cookie{Name: "sid", Value: "abc"}, &http.Cookie{Name: "has_cart", Value: "1"})
	if !strings.Contains(rec.Body.String(), "€25.00") {
		t.Fatalf("total missing:\n%s", rec.Body)
	}
}

func TestCheckoutRedirectsToConfirmation(t *testing.T) {
	h := newFakes(t).handler(t)
	rec := do(h, "POST", "/checkout", url.Values{}, &http.Cookie{Name: "sid", Value: "abc"})
	if rec.Code != 303 || !strings.HasPrefix(rec.Header().Get("Location"), "/confirmation?order=ord-1") {
		t.Fatalf("code=%d loc=%q", rec.Code, rec.Header().Get("Location"))
	}
	if c := cookie(rec, "has_cart"); c == nil || c.MaxAge >= 0 {
		t.Fatalf("has_cart not cleared: %+v", c)
	}
	rec = do(h, "GET", "/confirmation?order=ord-1&total=2500", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "ord-1") || !strings.Contains(rec.Body.String(), "€25.00") {
		t.Fatalf("confirmation page wrong: %d", rec.Code)
	}
}

func TestCheckoutConflictShowsMessage(t *testing.T) {
	f := newFakes(t)
	f.checkoutStatus = 409
	rec := do(f.handler(t), "POST", "/checkout", url.Values{}, &http.Cookie{Name: "sid", Value: "abc"})
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "no longer available") {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestImageProxyPassesWidth(t *testing.T) {
	rec := do(newFakes(t).handler(t), "GET", "/img/3?w=120", nil)
	if rec.Code != 200 || rec.Body.String() != "PNG:120" || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("code=%d body=%q type=%q", rec.Code, rec.Body, rec.Header().Get("Content-Type"))
	}
}

func TestStaticAssetsServed(t *testing.T) {
	h := newFakes(t).handler(t)
	for _, p := range []string{"/static/app.js", "/static/style.css"} {
		if rec := do(h, "GET", p, nil); rec.Code != 200 {
			t.Fatalf("%s = %d", p, rec.Code)
		}
	}
}

func TestDownstreamCallsUseFreshConnections(t *testing.T) {
	var conns atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"items":[{"product_id":1,"qty":1}]}`)
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}
	srv.Start()
	defer srv.Close()
	f := newFakes(t)
	s, err := New(Config{CatalogURL: f.catalog.URL, CartURL: srv.URL, CheckoutURL: f.checkout.URL, PodName: "p"})
	if err != nil {
		t.Fatal(err)
	}
	r, _, _ := httpx.NewRouter("storefront", "p", nil, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	s.Routes(r)
	for i := 0; i < 4; i++ {
		do(r, "GET", "/api/cart-count", nil, &http.Cookie{Name: "sid", Value: "abc"}, &http.Cookie{Name: "has_cart", Value: "1"})
	}
	if conns.Load() != 4 {
		t.Fatalf("opened %d connections for 4 calls, want 4", conns.Load())
	}
}
