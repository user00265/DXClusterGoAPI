package redisclient_test

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8" // Using v8 as it's common in go.mod references

	"github.com/user00265/dxclustergoapi/internal/config"
	"github.com/user00265/dxclustergoapi/internal/redisclient"
)

func TestNewClient_Disabled(t *testing.T) {
	ctx := context.Background()
	cfg := config.RedisConfig{Enabled: false}

	client, err := redisclient.NewClient(ctx, cfg)
	if err != nil {
		t.Fatalf("NewClient unexpectedly returned an error for disabled Redis: %v", err)
	}
	if client != nil {
		t.Error("NewClient unexpectedly returned a client for disabled Redis")
	}
}

func TestNewClient_NoHostWhenEnabled(t *testing.T) {
	ctx := context.Background()
	cfg := config.RedisConfig{Enabled: true, Host: ""} // Missing host

	_, err := redisclient.NewClient(ctx, cfg)
	if err == nil {
		t.Error("NewClient unexpectedly succeeded without a host when enabled")
	}
	if !strings.Contains(err.Error(), "redis host must be specified") {
		t.Errorf("Expected 'host must be specified' error, got: %v", err)
	}
}

func TestNewClient_SuccessfulConnection(t *testing.T) {
	s := miniredis.NewMiniRedis()
	s.Start()
	defer s.Close()

	ctx := context.Background()
	cfg := config.RedisConfig{
		Enabled:  true,
		Host:     s.Host(),
		Port:     s.Port(),
		Password: "", // Miniredis doesn't require password by default
		DB:       0,
	}

	client, err := redisclient.NewClient(ctx, cfg)
	if err != nil {
		t.Fatalf("NewClient failed to connect to miniredis: %v", err)
	}
	defer client.Close()

	if client.Client == nil {
		t.Fatal("Returned client.Client is nil")
	}

	// Test a basic Redis operation
	testKey := "testkey"
	testValue := "testvalue"
	err = client.Set(ctx, testKey, testValue, 0).Err()
	if err != nil {
		t.Fatalf("Failed to set key in Redis: %v", err)
	}
	val, err := client.Get(ctx, testKey).Result()
	if err != nil {
		t.Fatalf("Failed to get key from Redis: %v", err)
	}
	if val != testValue {
		t.Errorf("Expected value %s, got %s", testValue, val)
	}
}

func TestNewClient_WithPassword(t *testing.T) {
	s := miniredis.NewMiniRedis()
	s.RequireAuth("testpass") // Configure miniredis with a password
	s.Start()
	defer s.Close()

	ctx := context.Background()
	cfg := config.RedisConfig{
		Enabled:  true,
		Host:     s.Host(),
		Port:     s.Port(),
		Password: "testpass", // Provide the correct password
		DB:       0,
	}

	client, err := redisclient.NewClient(ctx, cfg)
	if err != nil {
		t.Fatalf("NewClient failed with password: %v", err)
	}
	defer client.Close()

	// Verify connection by pinging
	_, err = client.Ping(ctx).Result()
	if err != nil {
		t.Fatalf("Ping failed with password: %v", err)
	}
}

func TestNewClient_InvalidPassword(t *testing.T) {
	s := miniredis.NewMiniRedis()
	s.RequireAuth("correctpass")
	s.Start()
	defer s.Close()

	ctx := context.Background()
	cfg := config.RedisConfig{
		Enabled:  true,
		Host:     s.Host(),
		Port:     s.Port(),
		Password: "wrongpass", // Provide an incorrect password
		DB:       0,
	}

	_, err := redisclient.NewClient(ctx, cfg)
	if err == nil {
		t.Error("NewClient unexpectedly succeeded with invalid password")
	}
	// miniredis doesn't directly return AUTH error on Ping, but connection fails
	if !strings.Contains(err.Error(), "NOAUTH") && !strings.Contains(err.Error(), "connection refused") && !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "WRONGPASS") {
		t.Errorf("Expected authentication/connection error, got: %v", err)
	}
}

func TestNewClient_WithTLS_InsecureSkipVerify(t *testing.T) {
	// miniredis does not support TLS directly.
	// This test primarily checks if the tls.Config struct is correctly set up.
	// A real integration test would require a TLS-enabled Redis instance.

	ctx := context.Background()
	cfg := config.RedisConfig{
		Enabled:            true,
		Host:               "localhost", // Dummy host for setup, won't connect
		Port:               "6379",
		UseTLS:             true,
		InsecureSkipVerify: true,
	}

	// Temporarily replace NewRedisClient to inspect options
	originalNewClient := redisclient.NewRedisClient
	defer func() { redisclient.NewRedisClient = originalNewClient }()

	var capturedOptions *redis.Options
	redisclient.NewRedisClient = func(opt *redis.Options) *redis.Client {
		capturedOptions = opt
		// Return a mock client that will immediately error on Ping by wrapping Dialer
		return redis.NewClient(&redis.Options{
			Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return nil, fmt.Errorf("mock connection error for TLS test")
			},
		})
	}

	_, err := redisclient.NewClient(ctx, cfg)
	if err == nil {
		t.Error("Expected error during mock TLS connection, got nil")
	}

	if capturedOptions == nil {
		t.Fatal("Redis options were not captured")
	}
	if capturedOptions.TLSConfig == nil {
		t.Fatal("TLSConfig was not set in Redis options")
	}
	if !capturedOptions.TLSConfig.InsecureSkipVerify {
		t.Error("Expected InsecureSkipVerify to be true in TLSConfig, got false")
	}
}

func TestNewClient_WithDB(t *testing.T) {
	s := miniredis.NewMiniRedis()
	s.Start()
	defer s.Close()

	ctx := context.Background()
	cfg := config.RedisConfig{
		Enabled: true,
		Host:    s.Host(),
		Port:    s.Port(),
		DB:      1, // Use database 1
	}

	client, err := redisclient.NewClient(ctx, cfg)
	if err != nil {
		t.Fatalf("NewClient failed with specified DB: %v", err)
	}
	defer client.Close()

	// Set a key in DB 1
	err = client.Set(ctx, "dbtestkey", "dbtestvalue", 0).Err()
	if err != nil {
		t.Fatalf("Failed to set key in specified DB: %v", err)
	}

	// Verify that the key is in DB 1 by querying miniredis directly
	if !s.DB(1).Exists("dbtestkey") {
		t.Error("Key 'dbtestkey' not found in miniredis DB 1")
	}
	if s.DB(0).Exists("dbtestkey") {
		t.Error("Key 'dbtestkey' unexpectedly found in miniredis DB 0")
	}
}
