package cache

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type Client interface {
	SetLastSeen(agentID string, tsMs int64, ttlSeconds int) error
	GetLastSeen(agentID string) (int64, error)
	SetStatus(agentID string, status string) error
	GetStatus(agentID string) (string, error)
	SubscribeExpired() (*redis.PubSub, error)
	Close() error
}

type RedisCache struct {
	rdb *redis.Client
}

func NewRedisClient() (*RedisCache, error) {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		return nil, fmt.Errorf("REDIS_URL is required")
	}

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}

	if dbStr := os.Getenv("REDIS_DB"); dbStr != "" {
		if db, err := strconv.Atoi(dbStr); err == nil {
			opts.DB = db
		}
	}

	rdb := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	return &RedisCache{rdb: rdb}, nil
}

func (c *RedisCache) SetLastSeen(agentID string, tsMs int64, ttlSeconds int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	key := fmt.Sprintf("ops:agent:last_seen:%s", agentID)
	return c.rdb.Set(ctx, key, tsMs, time.Duration(ttlSeconds)*time.Second).Err()
}

func (c *RedisCache) GetLastSeen(agentID string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	key := fmt.Sprintf("ops:agent:last_seen:%s", agentID)
	val, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(val, 10, 64)
}

func (c *RedisCache) SetStatus(agentID string, status string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	key := fmt.Sprintf("ops:agent:status:%s", agentID)
	return c.rdb.Set(ctx, key, status, 0).Err()
}

func (c *RedisCache) GetStatus(agentID string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	key := fmt.Sprintf("ops:agent:status:%s", agentID)
	return c.rdb.Get(ctx, key).Result()
}

func (c *RedisCache) SubscribeExpired() (*redis.PubSub, error) {
	channel := fmt.Sprintf("__keyevent@%d__:expired", c.rdb.Options().DB)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pubsub := c.rdb.Subscribe(ctx, channel)
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return nil, err
	}
	return pubsub, nil
}

func (c *RedisCache) Close() error {
	return c.rdb.Close()
}
