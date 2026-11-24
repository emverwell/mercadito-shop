// Command catalog serves the product catalog. `catalog migrate` creates the
// schema and seed data; `catalog healthcheck` probes the local /readyz.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/emverwell/mercadito-shop/internal/admin"
	"github.com/emverwell/mercadito-shop/internal/catalog"
	"github.com/emverwell/mercadito-shop/internal/config"
	"github.com/emverwell/mercadito-shop/internal/httpx"
)

func main() { os.Exit(run()) }

func run() int {
	var cfg catalog.CatalogConfig
	if err := config.Load(&cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if handled, code := httpx.ProbeSelf(os.Args, strconv.Itoa(cfg.Port)); handled {
		return code
	}
	log := httpx.NewLogger()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	pool, err := connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("database unavailable", "error", err)
		return 1
	}
	defer pool.Close()

	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		if err := catalog.Migrate(ctx, pool, cfg.SeedProducts); err != nil {
			log.Error("migrate failed", "error", err)
			return 1
		}
		log.Info("migrate complete")
		return 0
	}

	store := catalog.NewPGStore(pool)
	products, err := store.All(ctx)
	if err != nil {
		log.Error("loading products failed", "error", err)
		return 1
	}
	d := &catalog.Deps{
		Store:    store,
		Index:    catalog.NewIndex(products),
		Products: products,
		Ping:     pool.Ping,
	}
	r, reg, rd := httpx.NewRouter("catalog", cfg.PodName, d.Ready, log)
	faults, err := admin.Setup(ctx, r, cfg.AdminToken, cfg.RedisURL, os.Getenv)
	if err != nil {
		log.Error("admin setup failed", "error", err)
		return 2
	}
	d.Thumbs = catalog.NewThumbs(cfg.ThumbCacheMB, func() bool { return faults.Get().ThumbBurst })
	d.Register(reg)
	d.Routes(r)

	go d.Index.Warm(ctx, time.Duration(cfg.WarmupSecs)*time.Second)

	srv := httpx.NewServer(strconv.Itoa(cfg.Port), r)
	if err := httpx.Run(ctx, srv, time.Duration(cfg.DrainSecs)*time.Second, rd, log); err != nil {
		log.Error("server stopped", "error", err)
		return 1
	}
	return 0
}

// connect retries for up to a minute so start-up order does not matter.
func connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	var lastErr error
	for i := 0; i < 30; i++ {
		pool, err := pgxpool.New(ctx, url)
		if err == nil {
			if err = pool.Ping(ctx); err == nil {
				return pool, nil
			}
			pool.Close()
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return nil, lastErr
}
