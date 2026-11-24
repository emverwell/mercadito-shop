package catalog

import (
	"fmt"
	"math/rand"
)

var categories = []string{
	"Fruit", "Vegetables", "Bakery", "Dairy", "Meat", "Fish",
	"Pantry", "Drinks", "Snacks", "Frozen", "Household", "Garden",
}

var adjectives = []string{"Fresh", "Organic", "Local", "Rustic", "Golden", "Classic", "Spiced", "Smoked", "Sweet", "Wild", "Crispy", "Hearty"}
var nouns = []string{"Basket", "Selection", "Harvest", "Blend", "Reserve", "Mix", "Cut", "Pack", "Batch", "Bundle", "Crate", "Jar"}

// GenerateProducts returns n deterministic products with ids 1..n.
func GenerateProducts(n int) []Product {
	rng := rand.New(rand.NewSource(42)) // #nosec G404 -- seed data, not security
	out := make([]Product, n)
	for i := range out {
		cat := i % len(categories)
		f := make([]float32, FeatureDims)
		for d := range f {
			f[d] = rng.Float32()
		}
		f[cat%FeatureDims] += 1.5 // keep categories loosely clustered
		out[i] = Product{
			ID:         i + 1,
			Name:       fmt.Sprintf("%s %s %s #%d", adjectives[rng.Intn(len(adjectives))], categories[cat], nouns[rng.Intn(len(nouns))], i+1),
			Category:   categories[cat],
			PriceCents: 199 + rng.Intn(19800),
			Stock:      50 + rng.Intn(150),
			Features:   f,
		}
	}
	return out
}
