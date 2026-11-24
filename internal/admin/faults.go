// Package admin holds the runtime toggles a service can be asked to apply and
// the token-guarded routes that change them.
package admin

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Snapshot is the full set of runtime toggles. Nil pointers mean "use the
// service's configured value".
type Snapshot struct {
	ThumbBurst         bool     `json:"thumb_burst"`
	CPUBurn            bool     `json:"cpu_burn"`
	PaymentLatencyMS   *int     `json:"payment_latency_ms"`
	PaymentJitterMS    *int     `json:"payment_jitter_ms"`
	PaymentFailureRate *float64 `json:"payment_failure_rate"`
}

// Faults holds the current snapshot and keeps it in step with a shared Store.
type Faults struct {
	cur   atomic.Pointer[Snapshot]
	store Store
}

// NewFaults reads initial values from FAULT_* variables via env.
func NewFaults(env func(string) string, store Store) *Faults {
	f := &Faults{store: store}
	s := Snapshot{}
	if b, err := strconv.ParseBool(env("FAULT_THUMB_BURST")); err == nil {
		s.ThumbBurst = b
	}
	if b, err := strconv.ParseBool(env("FAULT_CPU_BURN")); err == nil {
		s.CPUBurn = b
	}
	if n, err := strconv.Atoi(strings.TrimSpace(env("FAULT_PAYMENT_LATENCY_MS"))); err == nil {
		s.PaymentLatencyMS = &n
	}
	if n, err := strconv.Atoi(strings.TrimSpace(env("FAULT_PAYMENT_JITTER_MS"))); err == nil {
		s.PaymentJitterMS = &n
	}
	if x, err := strconv.ParseFloat(strings.TrimSpace(env("FAULT_PAYMENT_FAILURE_RATE")), 64); err == nil {
		s.PaymentFailureRate = &x
	}
	f.cur.Store(&s)
	return f
}

// Get returns the current snapshot without locking.
func (f *Faults) Get() Snapshot { return *f.cur.Load() }

// Set applies s locally and publishes it to the store.
func (f *Faults) Set(ctx context.Context, s Snapshot) error {
	f.cur.Store(&s)
	return f.store.Save(ctx, s)
}

// Reset clears every toggle locally and in the store.
func (f *Faults) Reset(ctx context.Context) error { return f.Set(ctx, Snapshot{}) }

// Sync polls the store and applies whatever it holds until ctx ends.
// Store errors are logged and the last known state is kept.
func (f *Faults) Sync(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			lctx, cancel := context.WithTimeout(ctx, time.Second)
			s, ok, err := f.store.Load(lctx)
			cancel()
			if err != nil {
				slog.Warn("toggle sync failed", "error", err)
				continue
			}
			if ok {
				f.cur.Store(&s)
			}
		}
	}
}
