package embeddings

import (
	"container/list"
	"context"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"math"
	"sync"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/circuitbreaker"
	"github.com/go-redis/redis/v8"
)

// EmbeddingCache defines cache operations
type EmbeddingCache interface {
	Get(ctx context.Context, key string) ([]float32, bool)
	Set(ctx context.Context, key string, v []float32, ttl time.Duration)
}

// LocalLRU is a simple in-process LRU with TTL
type LocalLRU struct {
	mu   sync.Mutex
	cap  int
	list *list.List               // front = most recent
	m    map[string]*list.Element // key -> element
}

type lruEntry struct {
	key string
	vec []float32
	exp time.Time
}

func NewLocalLRU(capacity int) *LocalLRU {
	if capacity <= 0 {
		capacity = 1024
	}
	return &LocalLRU{cap: capacity, list: list.New(), m: make(map[string]*list.Element, capacity)}
}

func (l *LocalLRU) Get(_ context.Context, key string) ([]float32, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if el, ok := l.m[key]; ok {
		ent := el.Value.(lruEntry)
		if ent.exp.After(time.Now()) {
			l.list.MoveToFront(el)
			return ent.vec, true
		}
		// expired: remove
		l.list.Remove(el)
		delete(l.m, key)
	}
	return nil, false
}

func (l *LocalLRU) Set(_ context.Context, key string, v []float32, ttl time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if el, ok := l.m[key]; ok {
		el.Value = lruEntry{key: key, vec: v, exp: time.Now().Add(ttl)}
		l.list.MoveToFront(el)
		return
	}
	el := l.list.PushFront(lruEntry{key: key, vec: v, exp: time.Now().Add(ttl)})
	l.m[key] = el
	if l.list.Len() > l.cap {
		lru := l.list.Back()
		if lru != nil {
			ent := lru.Value.(lruEntry)
			delete(l.m, ent.key)
			l.list.Remove(lru)
		}
	}
}

// RedisCache uses circuit-breaker wrapped Redis
type RedisCache struct {
	cli *circuitbreaker.RedisWrapper
}

func NewRedisCache(addr string) (*RedisCache, error) {
	rc := redis.NewClient(&redis.Options{Addr: addr})
	// Wrap with circuit breaker
	wrapper := circuitbreaker.NewRedisWrapper(rc, nil)
	// Ping once
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := wrapper.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return &RedisCache{cli: wrapper}, nil
}

func (r *RedisCache) Get(ctx context.Context, key string) ([]float32, bool) {
	b, err := r.cli.Get(ctx, key).Bytes()
	if err != nil {
		return nil, false
	}
	// decode bytes as float32 slice (naive 4-byte chunks)
	if len(b)%4 != 0 {
		return nil, false
	}
	out := make([]float32, len(b)/4)
	for i := 0; i < len(out); i++ {
		u := binary.LittleEndian.Uint32(b[i*4:])
		out[i] = math.Float32frombits(u)
	}
	return out, true
}

func (r *RedisCache) Set(ctx context.Context, key string, v []float32, ttl time.Duration) {
	// encode float32 slice into bytes
	b := make([]byte, len(v)*4)
	for i, f := range v {
		u := math.Float32bits(f)
		binary.LittleEndian.PutUint32(b[i*4:], u)
	}
	_ = r.cli.Set(ctx, key, b, ttl).Err()
}

// Key helpers
func MakeKey(model, text string) string {
	h := md5.Sum([]byte(model + "|" + text))
	return "emb:" + hex.EncodeToString(h[:])
}

// unsafePointer is a local alias to avoid importing unsafe at top-level API points
// We keep the unsafe usage contained inside the cache serialization only.
// Note: unsafe conversions are limited to cache serialization/deserialization.
