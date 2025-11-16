package redisclient

import (
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.com/go-redis/redis/v8" // Using v8 as it's common in go.mod references
	"golang.org/x/net/context"

	"github.com/user00265/dxclustergoapi/config"
)

// RedisCmdable defines the subset of redis client methods we need.
// Keep it minimal so tests can provide simple mocks.
type RedisCmdable interface {
	Ping(ctx context.Context) *redis.StatusCmd
	Get(ctx context.Context, key string) *redis.StringCmd
	SetEX(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	Close() error
}

// Client holds the Redis client instance (now interface for testability)
type Client struct {
	Redis RedisCmdable
}

// NewClient initializes and returns a new Redis client based on configuration.
func NewClient(ctx context.Context, cfg config.RedisConfig) (*Client, error) {
	if !cfg.Enabled {
		return nil, nil // Redis is disabled
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("redis host must be specified when Redis is enabled")
	}

	addr := net.JoinHostPort(cfg.Host, cfg.Port)

	options := &redis.Options{
		Addr:         addr,
		Username:     cfg.User,
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     10,
		MinIdleConns: 5,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		DialTimeout:  5 * time.Second,
	}

	if cfg.UseTLS {
		options.TLSConfig = &tls.Config{
			InsecureSkipVerify: cfg.InsecureSkipVerify,
		}
	}

	rdb := NewRedisClient(options)

	// Ping to verify connection
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Redis at %s: %w", addr, err)
	}

	return &Client{Redis: rdb}, nil
}

// NewRedisClient is a variable wrapper around redis.NewClient so tests can override it.
var NewRedisClient = func(opt *redis.Options) *redis.Client {
	return redis.NewClient(opt)
}

// IsConnected checks if the Redis client is connected and responsive.
func (c *Client) IsConnected(ctx context.Context) bool {
	if c == nil || c.Redis == nil {
		return false
	}
	_, err := c.Redis.Ping(ctx).Result()
	return err == nil
}

// HealthStatus returns a string describing the current health of the Redis connection.
func (c *Client) HealthStatus(ctx context.Context) string {
	if c == nil || c.Redis == nil {
		return "unavailable (not initialized)"
	}
	if c.IsConnected(ctx) {
		return "connected"
	}
	return "disconnected"
}

// Close closes the Redis connection.
func (c *Client) Close() error {
	if c == nil || c.Redis == nil {
		return nil
	}
	return c.Redis.Close()
}

// Get wraps the underlying Redis GET command and returns a *redis.StringCmd.
func (c *Client) Get(ctx context.Context, key string) *redis.StringCmd {
	if c == nil || c.Redis == nil {
		// Return a zero-value StringCmd by calling redis.NewStringResult on nil
		return redis.NewStringResult("", fmt.Errorf("redis client not initialized"))
	}
	return c.Redis.Get(ctx, key)
}

// SetEX wraps the underlying Redis SETEX command (Set with EXPIRE).
func (c *Client) SetEX(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	if c == nil || c.Redis == nil {
		return redis.NewStatusResult("", fmt.Errorf("redis client not initialized"))
	}
	return c.Redis.SetEX(ctx, key, value, expiration)
}
