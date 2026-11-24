package catalog

import (
	"context"
	"strings"
	"sync/atomic"
	"time"
)

// Index is the in-memory search index. It is usable immediately but only
// reports Ready once Warm has finished building it.
type Index struct {
	products []Product
	haystack atomic.Pointer[[]string] // lowercase "name category", set when warm
	ready    atomic.Bool
}

// NewIndex wraps the product list; call Warm to build the fast path.
func NewIndex(products []Product) *Index { return &Index{products: products} }

// Ready reports whether warm-up has completed.
func (ix *Index) Ready() bool { return ix.ready.Load() }

// Warm builds the lookup table, spending CPU for about d before marking the
// index ready. It returns early if ctx is cancelled.
func (ix *Index) Warm(ctx context.Context, d time.Duration) {
	deadline := time.Now().Add(d)
	var built []string
	for {
		built = make([]string, len(ix.products))
		for i, p := range ix.products {
			built[i] = strings.ToLower(p.Name + " " + p.Category)
		}
		if ctx.Err() != nil || !time.Now().Before(deadline) {
			break
		}
	}
	if ctx.Err() != nil {
		return
	}
	ix.haystack.Store(&built)
	ix.ready.Store(true)
}

// Search returns up to limit products whose name or category contains q
// (case-insensitive), in id order. An empty q returns the first limit products.
func (ix *Index) Search(q string, limit int) []Product {
	q = strings.ToLower(strings.TrimSpace(q))
	hay := ix.haystack.Load()
	out := make([]Product, 0, limit)
	for i, p := range ix.products {
		if len(out) >= limit {
			break
		}
		var h string
		if hay != nil {
			h = (*hay)[i]
		} else {
			h = slowHaystack(p)
		}
		if q == "" || strings.Contains(h, q) {
			out = append(out, p)
		}
	}
	return out
}

// slowHaystack is the cold path: rebuilds the lowercase text per product on
// every query, repeating the work so cold searches cost noticeably more.
func slowHaystack(p Product) string {
	var h string
	for i := 0; i < 200; i++ {
		h = strings.ToLower(p.Name + " " + p.Category)
	}
	return h
}
