package storefront

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/emverwell/mercadito-shop/internal/admin"
)

// burnIterations counts spin batches; tests watch it to see the loop run.
var burnIterations atomic.Uint64

const (
	burnSlice = 100 * time.Millisecond
	burnIdle  = 100 * time.Millisecond
)

// StartBurn starts workers goroutines that spin the CPU while the cpu_burn
// toggle is on and idle otherwise. They stop when ctx ends.
func StartBurn(ctx context.Context, f *admin.Faults, workers int) {
	for i := 0; i < workers; i++ {
		go func() {
			for ctx.Err() == nil {
				if !f.Get().CPUBurn {
					select {
					case <-time.After(burnIdle):
					case <-ctx.Done():
						return
					}
					continue
				}
				deadline := time.Now().Add(burnSlice)
				for time.Now().Before(deadline) {
					x := 0
					for j := 0; j < 100000; j++ {
						x += j
					}
					_ = x
					burnIterations.Add(1)
				}
			}
		}()
	}
}
