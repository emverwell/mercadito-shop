package catalog

import (
	"math"
	"sort"
)

// recsWorkPasses is how many times the full similarity sweep is evaluated per
// call. Calibrated with BenchmarkRecommend500 so one call over the 500-product
// seed costs 40-80 ms of CPU on one core.
const recsWorkPasses = 18000

// Recommend returns the n products most similar to product id (cosine
// similarity over feature vectors), best first, never including id itself.
// It returns nil if id is unknown.
func Recommend(all []Product, id, n int) []Product {
	var target *Product
	for i := range all {
		if all[i].ID == id {
			target = &all[i]
			break
		}
	}
	if target == nil {
		return nil
	}
	scores := make([]float64, len(all))
	for pass := 0; pass < recsWorkPasses; pass++ {
		for i := range all {
			scores[i] = cosine(target.Features, all[i].Features)
		}
	}
	idx := make([]int, 0, len(all)-1)
	for i := range all {
		if all[i].ID != id {
			idx = append(idx, i)
		}
	}
	sort.Slice(idx, func(a, b int) bool {
		if scores[idx[a]] != scores[idx[b]] {
			return scores[idx[a]] > scores[idx[b]]
		}
		return all[idx[a]].ID < all[idx[b]].ID
	})
	if n > len(idx) {
		n = len(idx)
	}
	out := make([]Product, n)
	for i := 0; i < n; i++ {
		out[i] = all[idx[i]]
	}
	return out
}

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
