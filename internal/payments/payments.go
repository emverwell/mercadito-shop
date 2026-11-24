// Package payments implements a fake payment provider with configurable
// latency and failure rate.
package payments

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"math/big"
	mrand "math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/emverwell/mercadito-shop/internal/admin"
)

// PaymentsConfig is the payments-mock service's environment.
type PaymentsConfig struct {
	Port        int     `env:"PORT" default:"8084" doc:"Listen port."`
	LatencyMS   int     `env:"PAYMENT_LATENCY_MS" default:"50" doc:"Base time a charge takes, in milliseconds."`
	JitterMS    int     `env:"PAYMENT_JITTER_MS" default:"0" doc:"Random extra delay of up to this many milliseconds."`
	FailureRate float64 `env:"PAYMENT_FAILURE_RATE" default:"0" doc:"Fraction of charges (0 to 1) that fail with 502."`
	RedisURL    string  `env:"REDIS_URL" default:"" doc:"Redis URL used to share runtime toggles between replicas (optional)."`
	AdminToken  string  `env:"ADMIN_TOKEN" default:"" doc:"Enables /admin routes when set; they return 404 otherwise."`
	PodName     string  `env:"POD_NAME" default:"local" doc:"Pod name, reported in logs and the X-Served-By header."`
	DrainSecs   int     `env:"SHUTDOWN_DRAIN_SECONDS" default:"0" doc:"Seconds to keep serving after SIGTERM before shutting down."`
}

// Handler serves POST /charge. Runtime toggles from Faults win over the
// configured values.
type Handler struct {
	LatencyMS   int
	JitterMS    int
	FailureRate float64
	Faults      func() admin.Snapshot
}

type chargeRequest struct {
	AmountCents int    `json:"amount_cents"`
	OrderRef    string `json:"order_ref"`
}

// Routes mounts the payments API.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/charge", func(w http.ResponseWriter, req *http.Request) {
		var in chargeRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 1<<12)).Decode(&in); err != nil ||
			in.AmountCents < 1 || strings.TrimSpace(in.OrderRef) == "" {
			http.Error(w, "invalid charge", http.StatusBadRequest)
			return
		}
		latency, jitter, failure := h.LatencyMS, h.JitterMS, h.FailureRate
		f := h.Faults()
		if f.PaymentLatencyMS != nil {
			latency = *f.PaymentLatencyMS
		}
		if f.PaymentJitterMS != nil {
			jitter = *f.PaymentJitterMS
		}
		if f.PaymentFailureRate != nil {
			failure = *f.PaymentFailureRate
		}
		delay := time.Duration(latency) * time.Millisecond
		if jitter > 0 {
			delay += time.Duration(mrand.Intn(jitter+1)) * time.Millisecond // #nosec G404 -- simulated jitter
		}
		select {
		case <-time.After(delay):
		case <-req.Context().Done():
			return
		}
		if failure > 0 && mrand.Float64() < failure { // #nosec G404 -- simulated failures
			http.Error(w, "payment provider error", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "approved", "id": "pay_" + randomHex()})
	})
}

func randomHex() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return "0"
	}
	return hex.EncodeToString(n.Bytes())
}
