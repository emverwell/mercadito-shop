package catalog

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgStore struct{ pool *pgxpool.Pool }

// NewPGStore reads products from Postgres.
func NewPGStore(pool *pgxpool.Pool) Store { return &pgStore{pool: pool} }

const selectCols = "id, name, category, price_cents, stock, features"

func scan(row pgx.Row) (Product, error) {
	var p Product
	err := row.Scan(&p.ID, &p.Name, &p.Category, &p.PriceCents, &p.Stock, &p.Features)
	return p, err
}

func (s *pgStore) Get(ctx context.Context, id int) (Product, error) {
	p, err := scan(s.pool.QueryRow(ctx, "SELECT "+selectCols+" FROM products WHERE id=$1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, ErrNotFound
	}
	return p, err
}

func (s *pgStore) All(ctx context.Context) ([]Product, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+selectCols+" FROM products ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Product
	for rows.Next() {
		p, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
