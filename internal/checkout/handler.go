package checkout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"
)

var sidPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Placer places an order (implemented by Service).
type Placer interface {
	Place(ctx context.Context, sid string, items []Item) (orderID string, totalCents int, err error)
}

// CartSource reads and clears a session's cart.
type CartSource interface {
	Get(ctx context.Context, sid string) ([]Item, error)
	Clear(ctx context.Context, sid string) error
}

// Handler serves POST /checkout.
type Handler struct {
	Placer Placer
	Cart   CartSource
}

// Routes mounts the checkout API.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/checkout", func(w http.ResponseWriter, req *http.Request) {
		var in struct {
			SessionID string `json:"session_id"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 1<<12)).Decode(&in); err != nil || !sidPattern.MatchString(in.SessionID) {
			http.Error(w, "invalid session_id", http.StatusBadRequest)
			return
		}
		items, err := h.Cart.Get(req.Context(), in.SessionID)
		if err != nil {
			slog.Error("cart lookup failed", "error", err)
			http.Error(w, "cart unavailable", http.StatusBadGateway)
			return
		}
		if len(items) == 0 {
			http.Error(w, "cart is empty", http.StatusBadRequest)
			return
		}
		orderID, total, err := h.Placer.Place(req.Context(), in.SessionID, items)
		switch {
		case errors.Is(err, ErrInsufficientStock), errors.Is(err, ErrUnknownProduct):
			http.Error(w, err.Error(), http.StatusConflict)
			return
		case errors.Is(err, ErrPayment):
			slog.Warn("payment failed", "error", err)
			http.Error(w, "payment failed", http.StatusBadGateway)
			return
		case err != nil:
			slog.Error("checkout failed", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if err := h.Cart.Clear(req.Context(), in.SessionID); err != nil {
			slog.Warn("clearing cart failed", "error", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"order_id": orderID, "total_cents": total})
	})
}

// HTTPCart talks to the cart service over HTTP.
type HTTPCart struct {
	BaseURL string
	HTTP    *http.Client
}

func (c *HTTPCart) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return cartHTTP
}

// cartHTTP opens a new connection per call so each request is routed afresh
// by the cart service's load balancer.
var cartHTTP = &http.Client{
	Timeout:   5 * time.Second,
	Transport: &http.Transport{DisableKeepAlives: true, Proxy: http.ProxyFromEnvironment},
}

// Get returns the session's items (empty when the cart is empty or unknown).
func (c *HTTPCart) Get(ctx context.Context, sid string) ([]Item, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/carts/"+sid, nil) // #nosec G704 -- base URL is operator config; sid is validated upstream
	if err != nil {
		return nil, err
	}
	resp, err := c.client().Do(req) // #nosec G704 -- request built from operator-configured base URL
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cart: status %d", resp.StatusCode)
	}
	var out struct {
		Items []Item `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// Clear deletes the session's cart.
func (c *HTTPCart) Clear(ctx context.Context, sid string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.BaseURL+"/carts/"+sid, nil) // #nosec G704 -- base URL is operator config; sid is validated upstream
	if err != nil {
		return err
	}
	resp, err := c.client().Do(req) // #nosec G704 -- request built from operator-configured base URL
	if err != nil {
		return err
	}
	return resp.Body.Close()
}
