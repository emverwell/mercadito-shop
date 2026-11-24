package catalog

// CatalogConfig is the catalog service's environment.
type CatalogConfig struct {
	Port         int    `env:"PORT" default:"8081" doc:"Listen port."`
	DatabaseURL  string `env:"DATABASE_URL" default:"" doc:"Postgres connection URL."`
	RedisURL     string `env:"REDIS_URL" default:"" doc:"Redis URL used to share runtime toggles between replicas (optional)."`
	WarmupSecs   int    `env:"WARMUP_SECONDS" default:"45" doc:"Seconds spent building the search index at start-up; /readyz returns 503 until it finishes."`
	ThumbCacheMB int    `env:"THUMB_CACHE_MAX_MB" default:"0" doc:"Maximum size of the in-memory thumbnail cache in MB; 0 means no limit."`
	SeedProducts int    `env:"SEED_PRODUCTS" default:"500" doc:"Number of products created by the migrate command."`
	AdminToken   string `env:"ADMIN_TOKEN" default:"" doc:"Enables /admin routes when set; they return 404 otherwise."`
	PodName      string `env:"POD_NAME" default:"local" doc:"Pod name, reported in logs and the X-Served-By header."`
	DrainSecs    int    `env:"SHUTDOWN_DRAIN_SECONDS" default:"0" doc:"Seconds to keep serving after SIGTERM before shutting down."`
}
