package checkout

// CheckoutConfig is the checkout service's environment.
type CheckoutConfig struct {
	Port             int    `env:"PORT" default:"8083" doc:"Listen port."`
	DatabaseURL      string `env:"DATABASE_URL" default:"" doc:"Postgres connection URL."`
	DBMaxConns       int    `env:"DB_MAX_CONNS" default:"2" doc:"Maximum Postgres connections held by each replica."`
	CartURL          string `env:"CART_URL" default:"http://cart:8082" doc:"Base URL of the cart service."`
	PaymentsURL      string `env:"PAYMENTS_URL" default:"http://payments-mock:8084" doc:"Base URL of the payment provider."`
	PaymentTimeoutMS int    `env:"PAYMENT_TIMEOUT_MS" default:"30000" doc:"Time allowed for one payment attempt, in milliseconds."`
	PaymentRetries   int    `env:"PAYMENT_RETRIES" default:"0" doc:"Extra payment attempts after a failure or timeout."`
	PaymentBackoffMS int    `env:"PAYMENT_BACKOFF_MS" default:"100" doc:"Base delay between payment retries, in milliseconds; doubles each retry with full jitter."`
	RedisURL         string `env:"REDIS_URL" default:"" doc:"Redis URL used to share runtime toggles between replicas (optional)."`
	AdminToken       string `env:"ADMIN_TOKEN" default:"" doc:"Enables /admin routes when set; they return 404 otherwise."`
	PodName          string `env:"POD_NAME" default:"local" doc:"Pod name, reported in logs and the X-Served-By header."`
	DrainSecs        int    `env:"SHUTDOWN_DRAIN_SECONDS" default:"0" doc:"Seconds to keep serving after SIGTERM before shutting down."`
}
