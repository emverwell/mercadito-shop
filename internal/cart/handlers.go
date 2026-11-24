package cart

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
)

const maxQty = 99

var sidPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Handler serves the cart HTTP API.
type Handler struct {
	Store  Store
	misses prometheus.Counter
}

// Register adds the cart metrics; backend names the active store.
func (h *Handler) Register(reg prometheus.Registerer, backend string) {
	cl := prometheus.Labels{"service": "cart"}
	h.misses = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cart_misses_total", Help: "Cart lookups for a session the store does not hold.", ConstLabels: cl,
	})
	info := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "cart_store_backend", Help: "Active cart storage backend.", ConstLabels: cl,
	}, []string{"backend"})
	info.WithLabelValues(backend).Set(1)
	reg.MustRegister(h.misses, info)
}

// Routes mounts the cart API.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/carts/{sid}/items", func(w http.ResponseWriter, req *http.Request) {
		sid, ok := sessionID(w, req)
		if !ok {
			return
		}
		var in Item
		if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 1<<12)).Decode(&in); err != nil ||
			in.ProductID < 1 || in.Qty < 1 || in.Qty > maxQty {
			http.Error(w, "invalid item", http.StatusBadRequest)
			return
		}
		if err := h.Store.Add(req.Context(), sid, in.ProductID, in.Qty); err != nil {
			storeDown(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	r.Get("/carts/{sid}", func(w http.ResponseWriter, req *http.Request) {
		sid, ok := sessionID(w, req)
		if !ok {
			return
		}
		c, err := h.Store.Get(req.Context(), sid)
		if errors.Is(err, ErrCartNotFound) {
			h.misses.Inc()
			slog.Info("cart not found", "session", sid)
		} else if err != nil {
			storeDown(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(c)
	})
	r.Delete("/carts/{sid}", func(w http.ResponseWriter, req *http.Request) {
		sid, ok := sessionID(w, req)
		if !ok {
			return
		}
		if err := h.Store.Clear(req.Context(), sid); err != nil {
			storeDown(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func sessionID(w http.ResponseWriter, req *http.Request) (string, bool) {
	sid := chi.URLParam(req, "sid")
	if !sidPattern.MatchString(sid) {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return "", false
	}
	return sid, true
}

func storeDown(w http.ResponseWriter, err error) {
	slog.Error("cart store error", "error", err)
	http.Error(w, "cart store unavailable", http.StatusServiceUnavailable)
}
