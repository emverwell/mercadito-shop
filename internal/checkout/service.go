package checkout

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// Item is one product line to order.
type Item struct {
	ProductID int `json:"product_id"`
	Qty       int `json:"qty"`
}

// Errors reported by Place.
var (
	ErrInsufficientStock = errors.New("insufficient stock")
	ErrUnknownProduct    = errors.New("unknown product")
	ErrPayment           = errors.New("payment failed")
)

// Charger takes a payment.
type Charger interface {
	Charge(ctx context.Context, amountCents int, ref string) error
}

// Service places orders.
type Service struct {
	Pool     *pgxpool.Pool
	Payments Charger
}

// Place reserves stock, charges payment and records the order in one
// transaction, which stays open while the payment is in flight. Any failure
// rolls everything back.
func (s *Service) Place(ctx context.Context, sid string, items []Item) (string, int, error) {
	sorted := append([]Item(nil), items...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ProductID < sorted[j].ProductID })

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	orderID := newOrderID()
	total := 0
	prices := make([]int, len(sorted))
	for i, it := range sorted {
		var price int
		err := tx.QueryRow(ctx,
			"UPDATE products SET stock = stock - $2 WHERE id = $1 AND stock >= $2 RETURNING price_cents",
			it.ProductID, it.Qty).Scan(&price)
		if errors.Is(err, pgx.ErrNoRows) {
			var exists bool
			if qerr := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM products WHERE id=$1)", it.ProductID).Scan(&exists); qerr != nil {
				return "", 0, qerr
			}
			if !exists {
				return "", 0, ErrUnknownProduct
			}
			return "", 0, ErrInsufficientStock
		}
		if err != nil {
			return "", 0, err
		}
		prices[i] = price
		total += price * it.Qty
	}
	if _, err := tx.Exec(ctx, "INSERT INTO orders (id, session_id, total_cents) VALUES ($1,$2,$3)", orderID, sid, total); err != nil {
		return "", 0, err
	}
	for i, it := range sorted {
		if _, err := tx.Exec(ctx, "INSERT INTO order_items (order_id, product_id, qty, price_cents) VALUES ($1,$2,$3,$4)",
			orderID, it.ProductID, it.Qty, prices[i]); err != nil {
			return "", 0, err
		}
	}
	if err := s.Payments.Charge(ctx, total, orderID); err != nil {
		return "", 0, fmt.Errorf("%w: %v", ErrPayment, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", 0, err
	}
	return orderID, total, nil
}

func newOrderID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "ord-" + hex.EncodeToString(b)
}

// RegisterPoolMetrics exports connection pool saturation gauges.
func RegisterPoolMetrics(reg prometheus.Registerer, pool *pgxpool.Pool) {
	cl := prometheus.Labels{"service": "checkout"}
	reg.MustRegister(
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "db_pool_acquired", Help: "Connections currently in use.", ConstLabels: cl},
			func() float64 { return float64(pool.Stat().AcquiredConns()) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "db_pool_wait_seconds", Help: "Cumulative time callers waited for a free connection.", ConstLabels: cl},
			func() float64 { return pool.Stat().EmptyAcquireWaitTime().Seconds() }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "db_pool_max", Help: "Configured maximum connections.", ConstLabels: cl},
			func() float64 { return float64(pool.Stat().MaxConns()) }),
	)
}
