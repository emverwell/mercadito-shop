package cart

// CartConfig is the cart service's environment.
type CartConfig struct {
	Port       int    `env:"PORT" default:"8082" doc:"Listen port."`
	CartStore  string `env:"CART_STORE" default:"memory" doc:"Where carts are kept: memory or redis."`
	RedisURL   string `env:"REDIS_URL" default:"" doc:"Redis URL. Used for carts when CART_STORE=redis, and for sharing runtime toggles."`
	CartTTL    string `env:"CART_TTL" default:"24h" doc:"How long an idle cart is kept in Redis (Go duration)."`
	AdminToken string `env:"ADMIN_TOKEN" default:"" doc:"Enables /admin routes when set; they return 404 otherwise."`
	PodName    string `env:"POD_NAME" default:"local" doc:"Pod name, reported in logs and the X-Served-By header."`
	DrainSecs  int    `env:"SHUTDOWN_DRAIN_SECONDS" default:"0" doc:"Seconds to keep serving after SIGTERM before shutting down."`
}
