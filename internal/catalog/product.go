// Package catalog implements the product catalog service: storage, search,
// recommendations and generated thumbnails.
package catalog

import (
	"context"
	"errors"
)

// FeatureDims is the length of every product's feature vector.
const FeatureDims = 16

// Product is one catalog entry.
type Product struct {
	ID         int       `json:"id"`
	Name       string    `json:"name"`
	Category   string    `json:"category"`
	PriceCents int       `json:"price_cents"`
	Stock      int       `json:"stock"`
	Features   []float32 `json:"-"`
}

// ErrNotFound is returned when a product id does not exist.
var ErrNotFound = errors.New("product not found")

// Store reads products.
type Store interface {
	All(ctx context.Context) ([]Product, error)
	Get(ctx context.Context, id int) (Product, error)
}
