package storefront

// StorefrontConfig is the storefront's environment.
type StorefrontConfig struct {
	Port        int    `env:"PORT" default:"8080" doc:"Listen port."`
	CatalogURL  string `env:"CATALOG_URL" default:"http://catalog:8081" doc:"Base URL of the catalog service."`
	CartURL     string `env:"CART_URL" default:"http://cart:8082" doc:"Base URL of the cart service."`
	CheckoutURL string `env:"CHECKOUT_URL" default:"http://checkout:8083" doc:"Base URL of the checkout service."`
	RedisURL    string `env:"REDIS_URL" default:"" doc:"Redis URL used to share runtime toggles between replicas (optional)."`
	AdminToken  string `env:"ADMIN_TOKEN" default:"" doc:"Enables /admin routes when set; they return 404 otherwise."`
	PodName     string `env:"POD_NAME" default:"local" doc:"Pod name, reported in logs and the X-Served-By header."`
	DrainSecs   int    `env:"SHUTDOWN_DRAIN_SECONDS" default:"0" doc:"Seconds to keep serving after SIGTERM before shutting down."`
}

// Config is the storefront's settings.
type Config = StorefrontConfig
