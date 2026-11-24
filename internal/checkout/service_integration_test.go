//go:build integration

package checkout

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/emverwell/mercadito-shop/internal/catalog"
)

type slowCharger struct {
	delay time.Duration
	fail  error
}

func (s slowCharger) Charge(ctx context.Context, _ int, _ string) error {
	select {
	case <-time.After(s.delay):
		return s.fail
	case <-ctx.Done():
		return ctx.Err()
	}
}

func testPool(t *testing.T, maxConns int32) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = maxConns
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "DROP TABLE IF EXISTS order_items, orders, products"); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Migrate(ctx, pool, 20); err != nil {
		t.Fatal(err)
	}
	return pool
}

func stock(t *testing.T, pool *pgxpool.Pool, id int) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), "SELECT stock FROM products WHERE id=$1", id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPlaceDecrementsStockAndRecordsOrder(t *testing.T) {
	pool := testPool(t, 4)
	before := stock(t, pool, 1)
	svc := &Service{Pool: pool, Payments: slowCharger{}}
	id, total, err := svc.Place(context.Background(), "s1", []Item{{ProductID: 1, Qty: 2}})
	if err != nil || id == "" || total <= 0 {
		t.Fatalf("place: id=%q total=%d err=%v", id, total, err)
	}
	if got := stock(t, pool, 1); got != before-2 {
		t.Fatalf("stock = %d, want %d", got, before-2)
	}
}

func TestInsufficientStockRollsBack(t *testing.T) {
	pool := testPool(t, 4)
	before1, before2 := stock(t, pool, 1), stock(t, pool, 2)
	svc := &Service{Pool: pool, Payments: slowCharger{}}
	_, _, err := svc.Place(context.Background(), "s1", []Item{{ProductID: 1, Qty: 1}, {ProductID: 2, Qty: 100000}})
	if !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("err = %v, want ErrInsufficientStock", err)
	}
	if stock(t, pool, 1) != before1 || stock(t, pool, 2) != before2 {
		t.Fatal("stock changed although the order failed")
	}
}

func TestUnknownProduct(t *testing.T) {
	pool := testPool(t, 4)
	_, _, err := (&Service{Pool: pool, Payments: slowCharger{}}).Place(context.Background(), "s1", []Item{{ProductID: 99999, Qty: 1}})
	if !errors.Is(err, ErrUnknownProduct) {
		t.Fatalf("err = %v", err)
	}
}

func TestPaymentFailureRollsBackStock(t *testing.T) {
	pool := testPool(t, 4)
	before := stock(t, pool, 3)
	svc := &Service{Pool: pool, Payments: slowCharger{fail: errors.New("declined")}}
	if _, _, err := svc.Place(context.Background(), "s1", []Item{{ProductID: 3, Qty: 1}}); !errors.Is(err, ErrPayment) {
		t.Fatalf("err = %v, want ErrPayment", err)
	}
	if stock(t, pool, 3) != before {
		t.Fatal("stock kept after payment failure")
	}
}

func TestTimeoutReleasesConn(t *testing.T) {
	pool := testPool(t, 1)
	svc := &Service{Pool: pool, Payments: slowCharger{delay: 5 * time.Second}}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, _, err := svc.Place(ctx, "s1", []Item{{ProductID: 1, Qty: 1}}); err == nil {
		t.Fatal("want error from the hung payment")
	}
	start := time.Now()
	fast := &Service{Pool: pool, Payments: slowCharger{}}
	if _, _, err := fast.Place(context.Background(), "s2", []Item{{ProductID: 1, Qty: 1}}); err != nil {
		t.Fatalf("second order: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("second order waited %v for a connection", time.Since(start))
	}
}

func TestPoolSaturationVisibleInGauge(t *testing.T) {
	pool := testPool(t, 2)
	reg := prometheus.NewRegistry()
	RegisterPoolMetrics(reg, pool)
	svc := &Service{Pool: pool, Payments: slowCharger{delay: 400 * time.Millisecond}}
	var wg sync.WaitGroup
	var maxSeen atomic.Int64
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = svc.Place(context.Background(), "s", []Item{{ProductID: 5 + i%3, Qty: 1}})
		}()
	}
	for end := time.Now().Add(300 * time.Millisecond); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if v := int64(pool.Stat().AcquiredConns()); v > maxSeen.Load() {
			maxSeen.Store(v)
		}
	}
	wg.Wait()
	if maxSeen.Load() != 2 {
		t.Fatalf("max acquired = %d, want 2 (pool saturated)", maxSeen.Load())
	}
	if n, err := testutil.GatherAndCount(reg, "db_pool_acquired", "db_pool_wait_seconds", "db_pool_max"); err != nil || n != 3 {
		t.Fatalf("pool gauges: n=%d err=%v", n, err)
	}
}
