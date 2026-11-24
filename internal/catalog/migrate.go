package catalog

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const schema = `
CREATE TABLE IF NOT EXISTS products (
  id int PRIMARY KEY,
  name text NOT NULL,
  category text NOT NULL,
  price_cents int NOT NULL CHECK (price_cents > 0),
  stock int NOT NULL CHECK (stock >= 0),
  features real[] NOT NULL
);
CREATE TABLE IF NOT EXISTS orders (
  id text PRIMARY KEY,
  session_id text NOT NULL,
  total_cents int NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS order_items (
  order_id text NOT NULL REFERENCES orders(id),
  product_id int NOT NULL REFERENCES products(id),
  qty int NOT NULL,
  price_cents int NOT NULL
);`

// Migrate creates the schema and seeds count products when the table is empty.
// Running it again is a no-op.
func Migrate(ctx context.Context, pool *pgxpool.Pool, count int) error {
	if _, err := pool.Exec(ctx, schema); err != nil {
		return err
	}
	var have int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM products").Scan(&have); err != nil {
		return err
	}
	if have > 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, p := range GenerateProducts(count) {
		batch.Queue("INSERT INTO products (id, name, category, price_cents, stock, features) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING",
			p.ID, p.Name, p.Category, p.PriceCents, p.Stock, p.Features)
	}
	return pool.SendBatch(ctx, batch).Close()
}
