// Command cart serves shopping carts.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/emverwell/mercadito-shop/internal/admin"
	"github.com/emverwell/mercadito-shop/internal/cart"
	"github.com/emverwell/mercadito-shop/internal/config"
	"github.com/emverwell/mercadito-shop/internal/httpx"
)

func main() { os.Exit(run()) }

func run() int {
	var cfg cart.CartConfig
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

	var store cart.Store
	switch cfg.CartStore {
	case "memory":
		store = cart.NewMemoryStore()
	case "redis":
		ttl, err := time.ParseDuration(cfg.CartTTL)
		if err != nil {
			log.Error("invalid CART_TTL", "error", err)
			return 2
		}
		opts, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			log.Error("CART_STORE=redis needs a valid REDIS_URL", "error", err)
			return 2
		}
		store = cart.NewRedisStore(redis.NewClient(opts), ttl)
	default:
		log.Error("CART_STORE must be memory or redis", "value", cfg.CartStore)
		return 2
	}

	h := &cart.Handler{Store: store}
	r, reg, rd := httpx.NewRouter("cart", cfg.PodName, store.Ping, log)
	if _, err := admin.Setup(ctx, r, cfg.AdminToken, cfg.RedisURL, os.Getenv); err != nil {
		log.Error("admin setup failed", "error", err)
		return 2
	}
	h.Register(reg, cfg.CartStore)
	h.Routes(r)

	srv := httpx.NewServer(strconv.Itoa(cfg.Port), r)
	if err := httpx.Run(ctx, srv, time.Duration(cfg.DrainSecs)*time.Second, rd, log); err != nil {
		log.Error("server stopped", "error", err)
		return 1
	}
	return 0
}
