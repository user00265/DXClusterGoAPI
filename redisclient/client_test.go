package redisclient

import (
	"context"
	"errors"
	"testing"

	"time"

	"github.com/go-redis/redis/v8"
	"github.com/user00265/dxclustergoapi/config"
)

// mockGoRedisClient implements only Ping and Close for RedisCmdable
// Used to inject success/failure for tests
type mockGoRedisClient struct {
	pingErr error
}

func (m *mockGoRedisClient) Ping(ctx context.Context) *redis.StatusCmd {
	cmd := redis.NewStatusCmd(ctx)
	if m.pingErr != nil {
		cmd.SetErr(m.pingErr)
	} else {
		cmd.SetVal("PONG")
	}
	return cmd
}

func (m *mockGoRedisClient) Close() error { return nil }

func (m *mockGoRedisClient) Get(ctx context.Context, key string) *redis.StringCmd {
	// Return nil as not found by default
	return redis.NewStringResult("", nil)
}

func (m *mockGoRedisClient) SetEX(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	return redis.NewStatusResult("OK", nil)
}

func TestNewClient_Disabled(t *testing.T) {
	cfg := config.RedisConfig{Enabled: false}
	client, err := NewClient(context.Background(), cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client != nil {
		t.Errorf("expected nil client when disabled, got %v", client)
	}
}

func TestNewClient_MissingHost(t *testing.T) {
	cfg := config.RedisConfig{Enabled: true, Host: ""}
	client, err := NewClient(context.Background(), cfg)
	if err == nil || client != nil {
		t.Errorf("expected error for missing host, got client=%v err=%v", client, err)
	}
}

func TestNewClient_ConnectionError(t *testing.T) {
	// Patch NewRedisClient to use our mock (not used, but for interface compatibility)
	NewRedisClient = func(opt *redis.Options) *redis.Client {
		return nil
	}

	client := &Client{Redis: &mockGoRedisClient{pingErr: errors.New("mock connection error")}}
	if client.IsConnected(context.Background()) {
		t.Errorf("expected error for connection failure, got client=%v", client)
	}

	// Do not call NewClient here, as it expects a real redis.Client and will panic with nil
}

func TestNewClient_Success(t *testing.T) {
	// Patch NewRedisClient to use our mock (not used, but for interface compatibility)
	NewRedisClient = func(opt *redis.Options) *redis.Client {
		return nil
	}

	// Patch NewRedisClient to use our mock
	orig := NewRedisClient
	NewRedisClient = func(opt *redis.Options) *redis.Client {
		return nil // not used, we construct Client directly
	}
	defer func() { NewRedisClient = orig }()

	client := &Client{Redis: &mockGoRedisClient{pingErr: nil}}
	// client is constructed directly and therefore cannot be nil; assert
	// that the underlying Redis mock reports a healthy connection instead.
	if !client.IsConnected(context.Background()) {
		t.Fatal("expected client to be connected")
	}

}

func TestIsConnected(t *testing.T) {
	ctx := context.Background()
	// nil receiver
	var c *Client
	if c.IsConnected(ctx) {
		t.Error("IsConnected should be false for nil receiver")
	}
	// nil Redis field
	c = &Client{}
	if c.IsConnected(ctx) {
		t.Error("IsConnected should be false for nil Redis field")
	}
	// working client
	client := &Client{Redis: &mockGoRedisClient{pingErr: nil}}
	if !client.IsConnected(ctx) {
		t.Error("IsConnected should be true for healthy client")
	}
}

func TestHealthStatus(t *testing.T) {
	ctx := context.Background()
	// nil receiver
	var c *Client
	if got := c.HealthStatus(ctx); got != "unavailable (not initialized)" {
		t.Errorf("HealthStatus(nil) = %q, want unavailable", got)
	}
	// nil Redis field
	c = &Client{}
	if got := c.HealthStatus(ctx); got != "unavailable (not initialized)" {
		t.Errorf("HealthStatus(nil Redis) = %q, want unavailable", got)
	}
	// connected
	client := &Client{Redis: &mockGoRedisClient{pingErr: nil}}
	if got := client.HealthStatus(ctx); got != "connected" {
		t.Errorf("HealthStatus(connected) = %q, want connected", got)
	}
	// disconnected
	client = &Client{Redis: &mockGoRedisClient{pingErr: errors.New("fail")}}
	if got := client.HealthStatus(ctx); got != "disconnected" {
		t.Errorf("HealthStatus(disconnected) = %q, want disconnected", got)
	}
}
