// Command checkout places orders.
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
	"github.com/emverwell/mercadito-shop/internal/checkout"
	"github.com/emverwell/mercadito-shop/internal/config"
	"github.com/emverwell/mercadito-shop/internal/httpx"
)

func main() { os.Exit(run()) }

func run() int {
	var cfg checkout.CheckoutConfig
	if err := config.Load(&cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if handled, code := httpx.ProbeSelf(os.Args, strconv.Itoa(cfg.Port)); handled {
		return code
	}
	if cfg.DBMaxConns < 1 {
		fmt.Fprintln(os.Stderr, "DB_MAX_CONNS must be at least 1")
		return 2
	}
	log := httpx.NewLogger()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	pcfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		log.Error("invalid DATABASE_URL", "error", err)
		return 2
	}
	pcfg.MaxConns = int32(cfg.DBMaxConns) // #nosec G115 -- validated positive, small
	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		log.Error("database config failed", "error", err)
		return 1
	}
	defer pool.Close()

	payments := &checkout.PaymentsClient{
		BaseURL: cfg.PaymentsURL,
		Timeout: time.Duration(cfg.PaymentTimeoutMS) * time.Millisecond,
		Retries: cfg.PaymentRetries,
		Backoff: time.Duration(cfg.PaymentBackoffMS) * time.Millisecond,
	}
	svc := &checkout.Service{Pool: pool, Payments: payments}

	r, reg, rd := httpx.NewRouter("checkout", cfg.PodName, pool.Ping, log)
	if _, err := admin.Setup(ctx, r, cfg.AdminToken, cfg.RedisURL, os.Getenv); err != nil {
		log.Error("admin setup failed", "error", err)
		return 2
	}
	payments.Register(reg)
	checkout.RegisterPoolMetrics(reg, pool)
	(&checkout.Handler{Placer: svc, Cart: &checkout.HTTPCart{BaseURL: cfg.CartURL}}).Routes(r)

	srv := httpx.NewServer(strconv.Itoa(cfg.Port), r)
	if err := httpx.Run(ctx, srv, time.Duration(cfg.DrainSecs)*time.Second, rd, log); err != nil {
		log.Error("server stopped", "error", err)
		return 1
	}
	return 0
}
