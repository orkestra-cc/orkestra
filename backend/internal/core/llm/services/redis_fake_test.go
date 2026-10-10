package services

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/orkestra/backend/pkg/sdk/module"
)

var _ module.RedisClient = (*memRedis)(nil)

// memRedis is an in-memory module.RedisClient: counters, TTLs and
// injectable failures, enough for the rate limiter.
type memRedis struct {
	mu        sync.Mutex
	vals      map[string]string
	ttl       map[string]time.Duration
	incrErr   error
	expireErr error
}

func newMemRedis() *memRedis {
	return &memRedis{vals: map[string]string{}, ttl: map[string]time.Duration{}}
}

func (r *memRedis) Get(_ context.Context, key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.vals[key]
	if !ok {
		return "", errors.New("redis: nil")
	}
	return v, nil
}

func (r *memRedis) Set(_ context.Context, key string, value interface{}, exp time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch v := value.(type) {
	case string:
		r.vals[key] = v
	default:
		r.vals[key] = ""
	}
	if exp > 0 {
		r.ttl[key] = exp
	}
	return nil
}

func (r *memRedis) Del(_ context.Context, keys ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, k := range keys {
		delete(r.vals, k)
		delete(r.ttl, k)
	}
	return nil
}

func (r *memRedis) Keys(_ context.Context, pattern string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	prefix := strings.TrimSuffix(pattern, "*")
	var out []string
	for k := range r.vals {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out, nil
}

func (r *memRedis) Incr(_ context.Context, key string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.incrErr != nil {
		return 0, r.incrErr
	}
	n, _ := strconv.ParseInt(r.vals[key], 10, 64)
	n++
	r.vals[key] = strconv.FormatInt(n, 10)
	return n, nil
}

func (r *memRedis) Expire(_ context.Context, key string, exp time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.expireErr != nil {
		return r.expireErr
	}
	if _, ok := r.vals[key]; ok {
		r.ttl[key] = exp
	}
	return nil
}

// keysWithoutTTL lists the counters that would never expire.
func (r *memRedis) keysWithoutTTL() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for k := range r.vals {
		if _, ok := r.ttl[k]; !ok {
			out = append(out, k)
		}
	}
	return out
}
