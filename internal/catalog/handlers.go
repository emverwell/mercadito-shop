package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	defaultWidth = 200
	maxList      = 500
)

// Deps is everything the HTTP handlers need.
type Deps struct {
	Store    Store
	Index    *Index
	Thumbs   *Thumbs
	Products []Product                       // full catalog loaded at start-up
	Ping     func(ctx context.Context) error // optional dependency check

	recs prometheus.Histogram
}

// Register adds the catalog-specific metrics to reg.
func (d *Deps) Register(reg prometheus.Registerer) {
	d.recs = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:        "catalog_recommendation_seconds",
		Help:        "Time spent computing recommendations.",
		ConstLabels: prometheus.Labels{"service": "catalog"},
		Buckets:     []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
	})
	thumbBytes := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name:        "thumb_cache_bytes",
		Help:        "Bytes of thumbnail data held in memory.",
		ConstLabels: prometheus.Labels{"service": "catalog"},
	}, func() float64 { return float64(d.Thumbs.Bytes()) })
	reg.MustRegister(d.recs, thumbBytes)
}

// Ready reports dependency and warm-up state for /readyz.
func (d *Deps) Ready(ctx context.Context) error {
	if d.Ping != nil {
		if err := d.Ping(ctx); err != nil {
			return err
		}
	}
	if !d.Index.Ready() {
		return errors.New("warming up")
	}
	return nil
}

// Routes mounts the catalog API.
func (d *Deps) Routes(r chi.Router) {
	known := make(map[int]bool, len(d.Products))
	for _, p := range d.Products {
		known[p.ID] = true
	}

	r.Get("/products", func(w http.ResponseWriter, req *http.Request) {
		limit, ok := intParam(w, req, "limit", 50)
		if !ok {
			return
		}
		list := d.Products
		if limit < len(list) {
			list = list[:limit]
		}
		writeJSON(w, list)
	})
	r.Get("/products/{id}", func(w http.ResponseWriter, req *http.Request) {
		id, ok := pathID(w, req)
		if !ok {
			return
		}
		p, err := d.Store.Get(req.Context(), id)
		if err != nil {
			storeError(w, err)
			return
		}
		writeJSON(w, p)
	})
	r.Get("/products/{id}/recommendations", func(w http.ResponseWriter, req *http.Request) {
		id, ok := pathID(w, req)
		if !ok {
			return
		}
		start := time.Now()
		recs := Recommend(d.Products, id, 6)
		d.recs.Observe(time.Since(start).Seconds())
		if recs == nil {
			http.Error(w, "product not found", http.StatusNotFound)
			return
		}
		writeJSON(w, recs)
	})
	r.Get("/search", func(w http.ResponseWriter, req *http.Request) {
		limit, ok := intParam(w, req, "limit", 20)
		if !ok {
			return
		}
		writeJSON(w, d.Index.Search(req.URL.Query().Get("q"), limit))
	})
	r.Get("/images/{id}", func(w http.ResponseWriter, req *http.Request) {
		id, ok := pathID(w, req)
		if !ok {
			return
		}
		if !known[id] {
			http.Error(w, "product not found", http.StatusNotFound)
			return
		}
		width, ok := intParam(w, req, "w", defaultWidth)
		if !ok {
			return
		}
		data, err := d.Thumbs.Get(id, width)
		if errors.Is(err, ErrBadWidth) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(data) // #nosec G705 -- PNG bytes generated here, served as image/png
	})
}

func pathID(w http.ResponseWriter, req *http.Request) (int, bool) {
	id, err := strconv.Atoi(chi.URLParam(req, "id"))
	if err != nil || id < 1 {
		http.Error(w, "bad id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

// intParam reads an integer query parameter, answering 400 when it is present
// but invalid (non-numeric or negative). Large values are clamped to maxList
// for "limit"; every other parameter is returned as given.
func intParam(w http.ResponseWriter, req *http.Request, name string, def int) (int, bool) {
	raw := req.URL.Query().Get(name)
	if raw == "" {
		return def, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		http.Error(w, "bad "+name, http.StatusBadRequest)
		return 0, false
	}
	if name == "limit" && n > maxList {
		n = maxList
	}
	if name == "limit" && n < 0 {
		http.Error(w, "bad "+name, http.StatusBadRequest)
		return 0, false
	}
	return n, true
}

func storeError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		http.Error(w, "product not found", http.StatusNotFound)
		return
	}
	http.Error(w, "internal error", http.StatusInternalServerError)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
