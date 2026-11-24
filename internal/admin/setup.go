package admin

import (
	"context"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

const syncEvery = 2 * time.Second

// Setup builds the Faults for a service and mounts the admin routes when a
// token is set. With a non-empty redisURL toggles are shared between replicas
// and polled every 2 s; an unreachable Redis does not stop startup.
func Setup(ctx context.Context, r chi.Router, token, redisURL string, env func(string) string) (*Faults, error) {
	store := NewMemoryStore()
	if redisURL != "" {
		opts, err := redis.ParseURL(redisURL)
		if err != nil {
			return nil, err
		}
		store = NewRedisStore(redis.NewClient(opts))
	}
	f := NewFaults(env, store)
	if redisURL != "" && token != "" {
		go f.Sync(ctx, syncEvery)
	}
	Mount(r, token, f)
	return f, nil
}
