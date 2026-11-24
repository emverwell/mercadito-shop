// Package cart implements the shopping cart service with a pluggable store.
package cart

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Item is one product line in a cart.
type Item struct {
	ProductID int `json:"product_id"`
	Qty       int `json:"qty"`
}

// Cart is a session's contents, ordered by product id.
type Cart struct {
	Items []Item `json:"items"`
}

// ErrCartNotFound means the store holds nothing for the session id.
var ErrCartNotFound = errors.New("cart not found")

// Store persists carts per session.
type Store interface {
	Add(ctx context.Context, sid string, productID, qty int) error
	Get(ctx context.Context, sid string) (Cart, error)
	Clear(ctx context.Context, sid string) error
	Ping(ctx context.Context) error
}

type memoryStore struct {
	mu    sync.Mutex
	carts map[string]map[int]int
}

// NewMemoryStore keeps carts in this process only.
func NewMemoryStore() Store { return &memoryStore{carts: map[string]map[int]int{}} }

func (m *memoryStore) Add(_ context.Context, sid string, productID, qty int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.carts[sid] == nil {
		m.carts[sid] = map[int]int{}
	}
	m.carts[sid][productID] += qty
	return nil
}

func (m *memoryStore) Get(_ context.Context, sid string) (Cart, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lines, ok := m.carts[sid]
	if !ok {
		return Cart{Items: []Item{}}, ErrCartNotFound
	}
	return toCart(lines), nil
}

func (m *memoryStore) Clear(_ context.Context, sid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.carts, sid)
	return nil
}

func (m *memoryStore) Ping(context.Context) error { return nil }

type redisStore struct {
	c   *redis.Client
	ttl time.Duration
}

// NewRedisStore shares carts between replicas through Redis.
func NewRedisStore(c *redis.Client, ttl time.Duration) Store { return &redisStore{c: c, ttl: ttl} }

func key(sid string) string { return "cart:" + sid }

func (r *redisStore) Add(ctx context.Context, sid string, productID, qty int) error {
	pipe := r.c.TxPipeline()
	pipe.HIncrBy(ctx, key(sid), strconv.Itoa(productID), int64(qty))
	pipe.Expire(ctx, key(sid), r.ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (r *redisStore) Get(ctx context.Context, sid string) (Cart, error) {
	raw, err := r.c.HGetAll(ctx, key(sid)).Result()
	if err != nil {
		return Cart{}, err
	}
	if len(raw) == 0 {
		return Cart{Items: []Item{}}, ErrCartNotFound
	}
	lines := make(map[int]int, len(raw))
	for k, v := range raw {
		id, err1 := strconv.Atoi(k)
		qty, err2 := strconv.Atoi(v)
		if err1 != nil || err2 != nil {
			continue
		}
		lines[id] = qty
	}
	return toCart(lines), nil
}

func (r *redisStore) Clear(ctx context.Context, sid string) error {
	return r.c.Del(ctx, key(sid)).Err()
}

func (r *redisStore) Ping(ctx context.Context) error { return r.c.Ping(ctx).Err() }

func toCart(lines map[int]int) Cart {
	items := make([]Item, 0, len(lines))
	for id, qty := range lines {
		items = append(items, Item{ProductID: id, Qty: qty})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ProductID < items[j].ProductID })
	return Cart{Items: items}
}
