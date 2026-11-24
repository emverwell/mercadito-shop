// Package storefront renders the shop's HTML pages by calling the catalog,
// cart and checkout services.
package storefront

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/emverwell/mercadito-shop/web"
)

const (
	sidCookie     = "sid"
	hasCartCookie = "has_cart"
	pageSize      = 24
)

var (
	sidPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	orderPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,40}$`)
	errNotFound  = errors.New("not found")
)

// Product mirrors the catalog's JSON.
type Product struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Category   string `json:"category"`
	PriceCents int    `json:"price_cents"`
	Stock      int    `json:"stock"`
}

type cartItem struct {
	ProductID int `json:"product_id"`
	Qty       int `json:"qty"`
}

type cartLine struct {
	Product       Product
	Qty           int
	SubtotalCents int
}

// Server renders pages.
type Server struct {
	cfg   Config
	pages map[string]*template.Template
	http  *http.Client
}

// New parses the embedded templates.
func New(cfg Config) (*Server, error) {
	funcs := template.FuncMap{"price": price}
	layout, err := fs.ReadFile(web.FS, "templates/layout.html")
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, pages: map[string]*template.Template{}, http: &http.Client{Timeout: 10 * time.Second, Transport: oneShot()}}
	for _, name := range []string{"index", "product", "cart", "confirmation", "error"} {
		body, err := fs.ReadFile(web.FS, "templates/"+name+".html")
		if err != nil {
			return nil, err
		}
		t, err := template.New(name).Funcs(funcs).Parse(string(layout))
		if err != nil {
			return nil, err
		}
		if _, err := t.Parse(string(body)); err != nil {
			return nil, err
		}
		s.pages[name] = t
	}
	return s, nil
}

// oneShot returns a transport that opens a new connection for every request,
// so each call is routed afresh by the service's load balancer.
func oneShot() *http.Transport {
	return &http.Transport{DisableKeepAlives: true, Proxy: http.ProxyFromEnvironment}
}

func price(cents int) string { return fmt.Sprintf("€%d.%02d", cents/100, cents%100) }

// Routes mounts the storefront pages.
func (s *Server) Routes(r chi.Router) {
	static, _ := fs.Sub(web.FS, "static")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	r.Get("/", s.index)
	r.Get("/search", s.search)
	r.Get("/product/{id}", s.product)
	r.Get("/cart", s.cart)
	r.Post("/cart/add", s.addToCart)
	r.Post("/checkout", s.checkout)
	r.Get("/confirmation", s.confirmation)
	r.Get("/img/{id}", s.image)
	r.Get("/api/cart-count", s.cartCount)
}

// ---- pages ----

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	s.session(w, r)
	var ps []Product
	if err := s.getJSON(r.Context(), s.cfg.CatalogURL+"/products?limit="+strconv.Itoa(pageSize), &ps); err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, http.StatusOK, "index", map[string]any{"Heading": "Today at the market", "Products": ps})
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	s.session(w, r)
	q := r.URL.Query().Get("q")
	var ps []Product
	if err := s.getJSON(r.Context(), s.cfg.CatalogURL+"/search?q="+url.QueryEscape(q)+"&limit="+strconv.Itoa(pageSize), &ps); err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, http.StatusOK, "index", map[string]any{"Heading": "Results", "Query": q, "Products": ps})
}

func (s *Server) product(w http.ResponseWriter, r *http.Request) {
	s.session(w, r)
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || id < 1 {
		s.fail(w, errNotFound)
		return
	}
	var p Product
	if err := s.getJSON(r.Context(), fmt.Sprintf("%s/products/%d", s.cfg.CatalogURL, id), &p); err != nil {
		s.fail(w, err)
		return
	}
	var recs []Product
	if err := s.getJSON(r.Context(), fmt.Sprintf("%s/products/%d/recommendations", s.cfg.CatalogURL, id), &recs); err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, http.StatusOK, "product", map[string]any{"Product": p, "Recommendations": recs, "Quantities": []int{1, 2, 3, 4, 5}})
}

func (s *Server) cart(w http.ResponseWriter, r *http.Request) {
	sid := s.session(w, r)
	s.renderCart(w, r, sid, http.StatusOK, "")
}

func (s *Server) renderCart(w http.ResponseWriter, r *http.Request, sid string, status int, msg string) {
	lines, total, err := s.loadCart(r, sid)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, status, "cart", map[string]any{"Lines": lines, "TotalCents": total, "Message": msg})
}

func (s *Server) addToCart(w http.ResponseWriter, r *http.Request) {
	sid := s.session(w, r)
	if err := r.ParseForm(); err != nil {
		s.renderError(w, http.StatusBadRequest, "Bad request", "That form could not be read.")
		return
	}
	pid, err1 := strconv.Atoi(r.PostFormValue("product_id"))
	qty, err2 := strconv.Atoi(r.PostFormValue("qty"))
	if err1 != nil || err2 != nil || pid < 1 || qty < 1 || qty > 99 {
		s.renderError(w, http.StatusBadRequest, "Bad request", "Choose a product and a quantity between 1 and 99.")
		return
	}
	body, _ := json.Marshal(cartItem{ProductID: pid, Qty: qty})
	resp, err := s.do(r.Context(), http.MethodPost, s.cfg.CartURL+"/carts/"+sid+"/items", body)
	if err != nil || resp != http.StatusNoContent {
		s.fail(w, fmt.Errorf("cart add: status %d err %v", resp, err))
		return
	}
	http.SetCookie(w, &http.Cookie{Name: hasCartCookie, Value: "1", Path: "/", MaxAge: 86400, HttpOnly: true, SameSite: http.SameSiteLaxMode}) // #nosec G124 -- plain-HTTP sandbox
	http.Redirect(w, r, "/cart", http.StatusSeeOther)
}

func (s *Server) checkout(w http.ResponseWriter, r *http.Request) {
	sid := s.session(w, r)
	body, _ := json.Marshal(map[string]string{"session_id": sid})
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.cfg.CheckoutURL+"/checkout", bytes.NewReader(body))
	if err != nil {
		s.fail(w, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	// Checkout may legitimately take long when payments are slow; the
	// request context bounds it, not the shared client timeout.
	resp, err := (&http.Client{Transport: oneShot()}).Do(req) // #nosec G704 -- URL comes from operator config
	if err != nil {
		s.fail(w, err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusCreated:
		var out struct {
			OrderID    string `json:"order_id"`
			TotalCents int    `json:"total_cents"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			s.fail(w, err)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: hasCartCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode}) // #nosec G124 -- plain-HTTP sandbox
		q := url.Values{"order": {out.OrderID}, "total": {strconv.Itoa(out.TotalCents)}}
		http.Redirect(w, r, "/confirmation?"+q.Encode(), http.StatusSeeOther)
	case http.StatusConflict:
		s.renderCart(w, r, sid, http.StatusConflict, "Some items in your cart are no longer available. Please review your cart.")
	case http.StatusBadRequest:
		http.Redirect(w, r, "/cart", http.StatusSeeOther)
	default:
		s.fail(w, fmt.Errorf("checkout: status %d", resp.StatusCode))
	}
}

func (s *Server) confirmation(w http.ResponseWriter, r *http.Request) {
	s.session(w, r)
	order := r.URL.Query().Get("order")
	total, err := strconv.Atoi(r.URL.Query().Get("total"))
	if !orderPattern.MatchString(order) || err != nil || total < 0 {
		s.fail(w, errNotFound)
		return
	}
	s.render(w, http.StatusOK, "confirmation", map[string]any{"Order": order, "TotalCents": total})
}

func (s *Server) image(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	target := fmt.Sprintf("%s/images/%d", s.cfg.CatalogURL, id)
	if wq := r.URL.Query().Get("w"); wq != "" {
		target += "?w=" + url.QueryEscape(wq)
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil) // #nosec G704 -- URL built from operator-configured base and validated parts
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	resp, err := s.http.Do(req) // #nosec G704 -- URL comes from operator config
	if err != nil {
		http.Error(w, "image unavailable", http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		http.Error(w, "image unavailable", resp.StatusCode)
		return
	}
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = io.Copy(w, resp.Body) // #nosec G705 -- proxied image bytes
}

func (s *Server) cartCount(w http.ResponseWriter, r *http.Request) {
	sid := s.session(w, r)
	count := 0
	status := http.StatusOK
	if c, err := r.Cookie(hasCartCookie); err == nil && c.Value == "1" {
		var cart struct {
			Items []cartItem `json:"items"`
		}
		if err := s.getJSON(r.Context(), s.cfg.CartURL+"/carts/"+sid, &cart); err != nil {
			status = http.StatusServiceUnavailable
		}
		for _, it := range cart.Items {
			count += it.Qty
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]int{"count": count})
}

// ---- helpers ----

// session returns the visitor's session id, issuing a cookie when missing.
func (s *Server) session(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(sidCookie); err == nil && sidPattern.MatchString(c.Value) {
		return c.Value
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	sid := hex.EncodeToString(b)
	http.SetCookie(w, &http.Cookie{Name: sidCookie, Value: sid, Path: "/", MaxAge: 7 * 86400, HttpOnly: true, SameSite: http.SameSiteLaxMode}) // #nosec G124 -- plain-HTTP sandbox
	return sid
}

func (s *Server) loadCart(r *http.Request, sid string) ([]cartLine, int, error) {
	if c, err := r.Cookie(hasCartCookie); err != nil || c.Value != "1" {
		return nil, 0, nil // nothing was ever added, so there is nothing to look up
	}
	var cart struct {
		Items []cartItem `json:"items"`
	}
	if err := s.getJSON(r.Context(), s.cfg.CartURL+"/carts/"+sid, &cart); err != nil {
		return nil, 0, err
	}
	lines := make([]cartLine, 0, len(cart.Items))
	total := 0
	for _, it := range cart.Items {
		var p Product
		if err := s.getJSON(r.Context(), fmt.Sprintf("%s/products/%d", s.cfg.CatalogURL, it.ProductID), &p); err != nil {
			return nil, 0, err
		}
		sub := p.PriceCents * it.Qty
		lines = append(lines, cartLine{Product: p, Qty: it.Qty, SubtotalCents: sub})
		total += sub
	}
	return lines, total, nil
}

func (s *Server) getJSON(ctx context.Context, target string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil) // #nosec G704 -- URL built from operator-configured base and validated parts
	if err != nil {
		return err
	}
	resp, err := s.http.Do(req) // #nosec G704 -- URL comes from operator config
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return errNotFound
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("%s: status %d", target, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (s *Server) do(ctx context.Context, method, target string, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body)) // #nosec G704 -- URL built from operator-configured base and validated parts
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req) // #nosec G704 -- URL comes from operator config
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, nil
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	if errors.Is(err, errNotFound) {
		s.renderError(w, http.StatusNotFound, "Not found", "We could not find that page.")
		return
	}
	slog.Error("downstream error", "error", err)
	s.renderError(w, http.StatusBadGateway, "Something went wrong", "Please try again in a moment.")
}

func (s *Server) renderError(w http.ResponseWriter, status int, heading, detail string) {
	s.render(w, status, "error", map[string]any{"Heading": heading, "Detail": detail})
}

func (s *Server) render(w http.ResponseWriter, status int, page string, data map[string]any) {
	data["PodName"] = s.cfg.PodName
	if _, ok := data["Query"]; !ok {
		data["Query"] = ""
	}
	var buf bytes.Buffer
	if err := s.pages[page].ExecuteTemplate(&buf, "layout", data); err != nil {
		slog.Error("render failed", "page", page, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w) // #nosec G705 -- html/template output
}

