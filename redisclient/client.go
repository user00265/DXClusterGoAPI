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

// Client holds the Redis client instance.
type Client struct {
	*redis.Client
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
		Addr:     addr,
		Username: cfg.User,
		Password: cfg.Password,
		DB:       cfg.DB,
		// Adjust pool sizes and timeouts as needed for your application's load
		PoolSize:     10,              // Number of concurrent connections to Redis
		MinIdleConns: 5,               // Minimum number of idle connections
		ReadTimeout:  3 * time.Second, // Read timeout
		WriteTimeout: 3 * time.Second, // Write timeout
		DialTimeout:  5 * time.Second, // Connection timeout
	}

	if cfg.UseTLS {
		options.TLSConfig = &tls.Config{
			InsecureSkipVerify: cfg.InsecureSkipVerify,
			// For production, you'd typically want to specify RootCAs, Certificates, etc.
			// e.g., RootCAs: certpool, Certificates: []tls.Certificate{clientCert}
		}
	}

	// Allow tests to override redis.NewClient by setting NewRedisClient.
	rdb := NewRedisClient(options)

	// Ping to verify connection
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Redis at %s: %w", addr, err)
	}

	return &Client{rdb}, nil
}

// NewRedisClient is a variable wrapper around redis.NewClient so tests can override it.
var NewRedisClient = func(opt *redis.Options) *redis.Client {
	return redis.NewClient(opt)
}
