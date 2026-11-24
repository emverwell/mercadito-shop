// Package checkout places orders: it reserves stock in Postgres, charges the
// payment provider and records the order.
package checkout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// PaymentsClient calls the payment provider.
type PaymentsClient struct {
	BaseURL string
	Timeout time.Duration // per attempt
	Retries int           // extra attempts after the first
	Backoff time.Duration // base delay; doubles each retry, full jitter
	HTTP    *http.Client

	seconds prometheus.Histogram
}

// Register adds payments_client_seconds to reg.
func (c *PaymentsClient) Register(reg prometheus.Registerer) {
	c.seconds = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:        "payments_client_seconds",
		Help:        "Time checkout spent in a payment charge, including retries.",
		ConstLabels: prometheus.Labels{"service": "checkout"},
		Buckets:     []float64{0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
	})
	reg.MustRegister(c.seconds)
}

type retryable struct{ error }

// Charge asks the provider to charge amountCents for order ref.
func (c *PaymentsClient) Charge(ctx context.Context, amountCents int, ref string) error {
	if c.seconds != nil {
		start := time.Now()
		defer func() { c.seconds.Observe(time.Since(start).Seconds()) }()
	}
	var err error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			ceiling := c.Backoff << (attempt - 1)
			if ceiling <= 0 {
				ceiling = time.Millisecond
			}
			select {
			case <-time.After(time.Duration(rand.Int63n(int64(ceiling) + 1))): // #nosec G404 -- backoff jitter
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		err = c.once(ctx, amountCents, ref)
		var r retryable
		if err == nil || !errors.As(err, &r) {
			return err
		}
	}
	return err
}

func (c *PaymentsClient) once(ctx context.Context, amountCents int, ref string) error {
	actx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	body, _ := json.Marshal(map[string]any{"amount_cents": amountCents, "order_ref": ref})
	req, err := http.NewRequestWithContext(actx, http.MethodPost, c.BaseURL+"/charge", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return retryable{err}
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusOK:
		return nil
	case resp.StatusCode >= 500:
		return retryable{fmt.Errorf("payments: status %d", resp.StatusCode)}
	default:
		return fmt.Errorf("payments: status %d", resp.StatusCode)
	}
}
