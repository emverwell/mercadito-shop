package admin

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/redis/go-redis/v9"
)

// Store shares a Snapshot between instances.
type Store interface {
	Load(ctx context.Context) (Snapshot, bool, error)
	Save(ctx context.Context, s Snapshot) error
}

type memoryStore struct {
	mu  sync.Mutex
	s   Snapshot
	set bool
}

// NewMemoryStore keeps state in-process (single instance / local runs).
func NewMemoryStore() Store { return &memoryStore{} }

func (m *memoryStore) Load(context.Context) (Snapshot, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.s, m.set, nil
}

func (m *memoryStore) Save(_ context.Context, s Snapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.s, m.set = s, true
	return nil
}

const redisKey = "mercadito:faults"

type redisStore struct{ c *redis.Client }

// NewRedisStore shares state between replicas through Redis.
func NewRedisStore(c *redis.Client) Store { return &redisStore{c: c} }

func (r *redisStore) Load(ctx context.Context) (Snapshot, bool, error) {
	raw, err := r.c.Get(ctx, redisKey).Bytes()
	if errors.Is(err, redis.Nil) {
		return Snapshot{}, false, nil
	}
	if err != nil {
		return Snapshot{}, false, err
	}
	var s Snapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		return Snapshot{}, false, err
	}
	return s, true, nil
}

func (r *redisStore) Save(ctx context.Context, s Snapshot) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return r.c.Set(ctx, redisKey, raw, 0).Err()
}
