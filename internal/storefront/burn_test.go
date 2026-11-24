package storefront

import (
	"context"
	"testing"
	"time"

	"github.com/emverwell/mercadito-shop/internal/admin"
)

func newFaults() *admin.Faults {
	return admin.NewFaults(func(string) string { return "" }, admin.NewMemoryStore())
}

func TestBurnStartsAndStopsWithToggle(t *testing.T) {
	f := newFaults()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	StartBurn(ctx, f, 2)

	_ = f.Set(ctx, admin.Snapshot{CPUBurn: true})
	time.Sleep(300 * time.Millisecond)
	a := burnIterations.Load()
	time.Sleep(200 * time.Millisecond)
	if burnIterations.Load() <= a {
		t.Fatal("burn counter did not advance while the toggle was on")
	}

	_ = f.Reset(ctx)
	time.Sleep(1200 * time.Millisecond) // within the 1 s stop bound plus slack
	b := burnIterations.Load()
	time.Sleep(300 * time.Millisecond)
	if burnIterations.Load() != b {
		t.Fatal("burn kept running after the toggle was turned off")
	}
}

func TestBurnIdleWhenOff(t *testing.T) {
	f := newFaults()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	before := burnIterations.Load()
	StartBurn(ctx, f, 2)
	time.Sleep(300 * time.Millisecond)
	if burnIterations.Load() != before {
		t.Fatal("burn ran with the toggle off")
	}
}

func TestBurnStopsOnContextCancel(t *testing.T) {
	f := newFaults()
	ctx, cancel := context.WithCancel(context.Background())
	_ = f.Set(ctx, admin.Snapshot{CPUBurn: true})
	StartBurn(ctx, f, 1)
	time.Sleep(200 * time.Millisecond)
	cancel()
	time.Sleep(400 * time.Millisecond)
	a := burnIterations.Load()
	time.Sleep(200 * time.Millisecond)
	if burnIterations.Load() != a {
		t.Fatal("burn kept running after context cancel")
	}
}
