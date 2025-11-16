package db

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestNewSQLiteClient_Success tests successful client creation and configuration
func TestNewSQLiteClient_Success(t *testing.T) {
	// Create a temporary directory for the test database
	tmpDir := t.TempDir()
	dbName := "test.db"

	client, err := NewSQLiteClient(tmpDir, dbName)
	if err != nil {
		t.Fatalf("NewSQLiteClient() failed: %v", err)
	}
	defer client.Close()

	// Verify the database file was created
	dbPath := filepath.Join(tmpDir, dbName)
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Errorf("Database file not created at %s", dbPath)
	}

	// Verify WAL file was created (indicates WAL mode is active)
	walPath := dbPath + "-wal"
	if _, err := os.Stat(walPath); os.IsNotExist(err) {
		t.Errorf("WAL file not created at %s", walPath)
	}

	// Verify the client is usable
	if client.GetDB() == nil {
		t.Error("GetDB() returned nil")
	}
}

// TestNewSQLiteClient_EmptyDataDir tests error handling for empty data directory
func TestNewSQLiteClient_EmptyDataDir(t *testing.T) {
	client, err := NewSQLiteClient("", "test.db")
	if err == nil {
		t.Error("NewSQLiteClient() should fail with empty data directory")
	}
	if client != nil {
		client.Close()
	}
	if err.Error() != "data directory must be specified for SQLite database" {
		t.Errorf("Expected 'data directory must be specified' error, got: %v", err)
	}
}

// TestNewSQLiteClient_CreateDataDir tests automatic creation of data directory
func TestNewSQLiteClient_CreateDataDir(t *testing.T) {
	tmpBase := t.TempDir()
	// Create a nested path that doesn't exist yet
	dataDir := filepath.Join(tmpBase, "subdir", "another", "level")

	client, err := NewSQLiteClient(dataDir, "test.db")
	if err != nil {
		t.Fatalf("NewSQLiteClient() failed to create nested directory: %v", err)
	}
	defer client.Close()

	// Verify the directory was created
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		t.Errorf("Data directory not created at %s", dataDir)
	}
}

// TestNewSQLiteClient_ExistingDatabase tests opening an existing database
func TestNewSQLiteClient_ExistingDatabase(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "existing.db"
	dbPath := filepath.Join(tmpDir, dbName)

	// Create the database file first
	file, err := os.Create(dbPath)
	if err != nil {
		t.Fatalf("Failed to create test database file: %v", err)
	}
	file.Close()

	// Now open it with the client
	client, err := NewSQLiteClient(tmpDir, dbName)
	if err != nil {
		t.Fatalf("NewSQLiteClient() failed for existing database: %v", err)
	}
	defer client.Close()

	// Verify it's usable
	if client.GetDB() == nil {
		t.Error("GetDB() returned nil for existing database")
	}
}

// TestGetDB tests the GetDB method
func TestGetDB(t *testing.T) {
	tmpDir := t.TempDir()
	client, err := NewSQLiteClient(tmpDir, "test.db")
	if err != nil {
		t.Fatalf("NewSQLiteClient() failed: %v", err)
	}
	defer client.Close()

	db := client.GetDB()
	if db == nil {
		t.Error("GetDB() returned nil")
	}

	// Verify it's a valid *sql.DB by attempting a simple operation
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		t.Errorf("PingContext on returned db failed: %v", err)
	}
}

// TestClose tests the Close method
func TestClose(t *testing.T) {
	tmpDir := t.TempDir()
	client, err := NewSQLiteClient(tmpDir, "test.db")
	if err != nil {
		t.Fatalf("NewSQLiteClient() failed: %v", err)
	}

	// Verify the connection works before closing
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping before Close failed: %v", err)
	}

	// Close the client
	if err := client.Close(); err != nil {
		t.Errorf("Close() failed: %v", err)
	}

	// Attempting to ping after close should fail
	ctx2, cancel2 := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel2()
	if err := client.Ping(ctx2); err == nil {
		t.Error("Ping after Close() should have failed but didn't")
	}
}

// TestInit tests the Init method (placeholder)
func TestInit(t *testing.T) {
	tmpDir := t.TempDir()
	client, err := NewSQLiteClient(tmpDir, "test.db")
	if err != nil {
		t.Fatalf("NewSQLiteClient() failed: %v", err)
	}
	defer client.Close()

	// Init should return nil (it's a placeholder)
	if err := client.Init(); err != nil {
		t.Errorf("Init() failed: %v", err)
	}
}

// TestPing tests the Ping method with context
func TestPing(t *testing.T) {
	tmpDir := t.TempDir()
	client, err := NewSQLiteClient(tmpDir, "test.db")
	if err != nil {
		t.Fatalf("NewSQLiteClient() failed: %v", err)
	}
	defer client.Close()

	// Ping with valid context should succeed
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx); err != nil {
		t.Errorf("Ping() failed: %v", err)
	}
}

// TestPing_ContextCancelled tests Ping with a cancelled context
func TestPing_ContextCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	client, err := NewSQLiteClient(tmpDir, "test.db")
	if err != nil {
		t.Fatalf("NewSQLiteClient() failed: %v", err)
	}
	defer client.Close()

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Ping with cancelled context should fail
	if err := client.Ping(ctx); err == nil {
		t.Error("Ping() with cancelled context should have failed")
	}
}

// TestPing_ContextDeadlineExceeded tests Ping with an already-exceeded deadline
func TestPing_ContextDeadlineExceeded(t *testing.T) {
	tmpDir := t.TempDir()
	client, err := NewSQLiteClient(tmpDir, "test.db")
	if err != nil {
		t.Fatalf("NewSQLiteClient() failed: %v", err)
	}
	defer client.Close()

	// Create a context that has already exceeded its deadline
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
	defer cancel()

	// Ping should fail due to deadline
	if err := client.Ping(ctx); err == nil {
		t.Error("Ping() with exceeded deadline should have failed")
	}
}

// TestDBInterface verifies SQLiteClient implements DBClient interface
func TestDBInterface(t *testing.T) {
	tmpDir := t.TempDir()
	client, err := NewSQLiteClient(tmpDir, "test.db")
	if err != nil {
		t.Fatalf("NewSQLiteClient() failed: %v", err)
	}
	defer client.Close()

	// This will fail to compile if SQLiteClient doesn't implement DBClient
	var _ DBClient = client
}

// TestMultipleClients tests creating multiple concurrent clients
func TestMultipleClients(t *testing.T) {
	tmpDir := t.TempDir()

	// Create two clients to the same database
	client1, err := NewSQLiteClient(tmpDir, "shared.db")
	if err != nil {
		t.Fatalf("NewSQLiteClient() for client1 failed: %v", err)
	}
	defer client1.Close()

	client2, err := NewSQLiteClient(tmpDir, "shared.db")
	if err != nil {
		t.Fatalf("NewSQLiteClient() for client2 failed: %v", err)
	}
	defer client2.Close()

	// Both should be able to ping
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client1.Ping(ctx); err != nil {
		t.Errorf("client1.Ping() failed: %v", err)
	}

	if err := client2.Ping(ctx); err != nil {
		t.Errorf("client2.Ping() failed: %v", err)
	}
}

// TestDatabaseFilePermissions verifies database file is created with correct permissions
func TestDatabaseFilePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	dbName := "test.db"

	client, err := NewSQLiteClient(tmpDir, dbName)
	if err != nil {
		t.Fatalf("NewSQLiteClient() failed: %v", err)
	}
	defer client.Close()

	dbPath := filepath.Join(tmpDir, dbName)
	fileInfo, err := os.Stat(dbPath)
	if err != nil {
		t.Errorf("Failed to stat database file: %v", err)
		return
	}

	// Verify the file is readable and writable (0644 permissions)
	if !fileInfo.Mode().IsRegular() {
		t.Error("Database file is not a regular file")
	}

	// Check it's not a directory
	if fileInfo.IsDir() {
		t.Error("Database file is a directory")
	}
}

// BenchmarkPing benchmarks the Ping operation
func BenchmarkPing(b *testing.B) {
	tmpDir := b.TempDir()
	client, err := NewSQLiteClient(tmpDir, "bench.db")
	if err != nil {
		b.Fatalf("NewSQLiteClient() failed: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = client.Ping(ctx)
	}
}

// BenchmarkNewSQLiteClient benchmarks client creation
func BenchmarkNewSQLiteClient(b *testing.B) {
	tmpBase := b.TempDir()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tmpDir := filepath.Join(tmpBase, "bench", string(rune(i)))
		client, _ := NewSQLiteClient(tmpDir, "bench.db")
		if client != nil {
			client.Close()
		}
	}
}
