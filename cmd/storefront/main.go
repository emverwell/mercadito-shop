// Command storefront serves the shop's HTML pages.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/emverwell/mercadito-shop/internal/admin"
	"github.com/emverwell/mercadito-shop/internal/config"
	"github.com/emverwell/mercadito-shop/internal/httpx"
	"github.com/emverwell/mercadito-shop/internal/storefront"
)

func main() { os.Exit(run()) }

func run() int {
	var cfg storefront.StorefrontConfig
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

	s, err := storefront.New(cfg)
	if err != nil {
		log.Error("templates failed to load", "error", err)
		return 1
	}
	r, _, rd := httpx.NewRouter("storefront", cfg.PodName, nil, log)
	faults, err := admin.Setup(ctx, r, cfg.AdminToken, cfg.RedisURL, os.Getenv)
	if err != nil {
		log.Error("admin setup failed", "error", err)
		return 2
	}
	storefront.StartBurn(ctx, faults, runtime.NumCPU())
	s.Routes(r)

	srv := httpx.NewServer(strconv.Itoa(cfg.Port), r)
	if err := httpx.Run(ctx, srv, time.Duration(cfg.DrainSecs)*time.Second, rd, log); err != nil {
		log.Error("server stopped", "error", err)
		return 1
	}
	return 0
}
