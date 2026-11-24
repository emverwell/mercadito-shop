package admin

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Mount registers /admin/faults routes guarded by X-Admin-Token. With an
// empty (or whitespace-only) token nothing is registered, so the paths 404.
func Mount(r chi.Router, token string, f *Faults) {
	if strings.TrimSpace(token) == "" {
		return
	}
	guard := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, req *http.Request) {
			got := req.Header.Get("X-Admin-Token")
			if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next(w, req)
		}
	}
	write := func(w http.ResponseWriter, s Snapshot) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s)
	}
	r.Get("/admin/faults", guard(func(w http.ResponseWriter, _ *http.Request) { write(w, f.Get()) }))
	r.Post("/admin/faults", guard(func(w http.ResponseWriter, req *http.Request) {
		s := f.Get()
		if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 1<<16)).Decode(&s); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if err := f.Set(req.Context(), s); err != nil {
			slog.Warn("toggle publish failed", "error", err)
		}
		write(w, f.Get())
	}))
	r.Post("/admin/faults/reset", guard(func(w http.ResponseWriter, req *http.Request) {
		if err := f.Reset(req.Context()); err != nil {
			slog.Warn("toggle publish failed", "error", err)
		}
		write(w, f.Get())
	}))
}
