//go:build integration

package catalog

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(context.Background(), "DROP TABLE IF EXISTS order_items, orders, products"); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestMigrateIdempotent(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := Migrate(ctx, pool, 500); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM products").Scan(&n); err != nil || n != 500 {
		t.Fatalf("rows = %d err=%v, want 500", n, err)
	}
}

func TestPGStoreGetAndNotFound(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool, 50); err != nil {
		t.Fatal(err)
	}
	s := NewPGStore(pool)
	p, err := s.Get(ctx, 7)
	if err != nil || p.ID != 7 || len(p.Features) != FeatureDims {
		t.Fatalf("get = %+v err=%v", p, err)
	}
	if _, err := s.Get(ctx, 99999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	all, err := s.All(ctx)
	if err != nil || len(all) != 50 {
		t.Fatalf("all = %d err=%v", len(all), err)
	}
}
