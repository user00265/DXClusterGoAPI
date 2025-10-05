package db_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/user00265/dxclustergoapi/internal/db"
)

func TestNewSQLiteClient_Success(t *testing.T) {
	tempDir := t.TempDir() // Creates a temporary directory for this test
	dbName := "test.db"

	client, err := db.NewSQLiteClient(tempDir, dbName)
	if err != nil {
		t.Fatalf("NewSQLiteClient failed: %v", err)
	}
	defer client.Close()

	if client.GetDB() == nil {
		t.Error("GetDB returned nil *sql.DB")
	}

	// Verify the database file exists
	dbPath := filepath.Join(tempDir, dbName)
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Errorf("Database file was not created at %s", dbPath)
	}
}

func TestNewSQLiteClient_DataDirCreation(t *testing.T) {
	tempParentDir := t.TempDir()
	nonExistentSubDir := filepath.Join(tempParentDir, "nonexistent", "nested", "dir")
	dbName := "test_created.db"

	client, err := db.NewSQLiteClient(nonExistentSubDir, dbName)
	if err != nil {
		t.Fatalf("NewSQLiteClient failed to create data directory: %v", err)
	}
	defer client.Close()

	// Verify the data directory was created
	if _, err := os.Stat(nonExistentSubDir); os.IsNotExist(err) {
		t.Errorf("Data directory %s was not created", nonExistentSubDir)
	}
	// Verify the database file exists within the created directory
	dbPath := filepath.Join(nonExistentSubDir, dbName)
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Errorf("Database file was not created at %s", dbPath)
	}
}

func TestNewSQLiteClient_InvalidDataDir(t *testing.T) {
	// Use a path that's likely unwritable or invalid. On Windows, skip this assertion
	if runtime.GOOS == "windows" {
		t.Skip("Skipping invalid data dir test on Windows")
	}
	// Use a path that's likely unwritable or invalid
	invalidDir := "/root/unwritable/dir" // Assuming test runs as non-root
	dbName := "invalid.db"

	_, err := db.NewSQLiteClient(invalidDir, dbName)
	if err == nil {
		t.Error("NewSQLiteClient unexpectedly succeeded with invalid data directory")
	}
	if !strings.Contains(err.Error(), "permission denied") && !strings.Contains(err.Error(), "read-only file system") && !strings.Contains(err.Error(), "failed to create data directory") {
		t.Errorf("Expected permission/creation error, got: %v", err)
	}
}

func TestSQLiteClient_Close(t *testing.T) {
	tempDir := t.TempDir()
	dbName := "close.db"

	client, err := db.NewSQLiteClient(tempDir, dbName)
	if err != nil {
		t.Fatalf("NewSQLiteClient failed: %v", err)
	}

	err = client.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Try to use the DB after closing, should result in an error
	_, err = client.GetDB().Exec("SELECT 1")
	if err == nil {
		t.Error("Expected error when executing query on closed DB, got nil")
	}
	if !strings.Contains(err.Error(), "database is closed") {
		t.Errorf("Expected 'database is closed' error, got: %v", err)
	}
}

func TestSQLiteClient_Init(t *testing.T) {
	tempDir := t.TempDir()
	dbName := "init.db"

	client, err := db.NewSQLiteClient(tempDir, dbName)
	if err != nil {
		t.Fatalf("NewSQLiteClient failed: %v", err)
	}
	defer client.Close()

	// Init should return nil as it's a placeholder
	if err := client.Init(); err != nil {
		t.Errorf("Init unexpectedly returned an error: %v", err)
	}

	// Verify that basic DB operations work after Init (even if Init does nothing)
	_, err = client.GetDB().Exec("CREATE TABLE test_table (id INTEGER PRIMARY KEY, name TEXT)")
	if err != nil {
		t.Fatalf("Failed to create test table after Init: %v", err)
	}
}

func TestSQLiteClient_Concurrency(t *testing.T) {
	tempDir := t.TempDir()
	dbName := "concurrency.db"

	client, err := db.NewSQLiteClient(tempDir, dbName)
	if err != nil {
		t.Fatalf("NewSQLiteClient failed: %v", err)
	}
	defer client.Close()

	// Create a test table
	_, err = client.GetDB().Exec("CREATE TABLE counter (id INTEGER PRIMARY KEY, value INTEGER)")
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}
	_, err = client.GetDB().Exec("INSERT INTO counter (id, value) VALUES (1, 0)")
	if err != nil {
		t.Fatalf("Failed to insert initial value: %v", err)
	}

	numGoroutines := 10
	numIncrementsPerGoroutine := 100
	var wg sync.WaitGroup
	errCh := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numIncrementsPerGoroutine; j++ {
				_, err := client.GetDB().Exec("UPDATE counter SET value = value + 1 WHERE id = 1")
				if err != nil {
					errCh <- fmt.Errorf("goroutine update failed: %w", err)
					return
				}
				time.Sleep(1 * time.Millisecond) // Simulate some work
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("Concurrency error: %v", err)
	}

	var finalValue int
	err = client.GetDB().QueryRow("SELECT value FROM counter WHERE id = 1").Scan(&finalValue)
	if err != nil {
		t.Fatalf("Failed to query final value: %v", err)
	}

	expectedValue := numGoroutines * numIncrementsPerGoroutine
	if finalValue != expectedValue {
		t.Errorf("Expected final value %d, got %d", expectedValue, finalValue)
	}
}

func TestSQLiteClient_WALMode(t *testing.T) {
	tempDir := t.TempDir()
	dbName := "wal.db"

	client, err := db.NewSQLiteClient(tempDir, dbName)
	if err != nil {
		t.Fatalf("NewSQLiteClient failed: %v", err)
	}
	defer client.Close()

	// Check if WAL file exists after some writes
	_, err = client.GetDB().Exec("CREATE TABLE wal_test (id INTEGER PRIMARY KEY, data TEXT)")
	if err != nil {
		t.Fatalf("Failed to create wal_test table: %v", err)
	}
	_, err = client.GetDB().Exec("INSERT INTO wal_test (data) VALUES ('test data')")
	if err != nil {
		t.Fatalf("Failed to insert into wal_test: %v", err)
	}

	walPath := filepath.Join(tempDir, dbName+"-wal")
	if _, err := os.Stat(walPath); os.IsNotExist(err) {
		t.Errorf("WAL file (%s) was not created, suggesting WAL mode might not be active or transaction not committed yet", walPath)
	} else if err != nil {
		t.Errorf("Error checking for WAL file: %v", err)
	}
}

func TestSQLiteClient_Ping(t *testing.T) {
	tempDir := t.TempDir()
	dbName := "ping.db"

	client, err := db.NewSQLiteClient(tempDir, dbName)
	if err != nil {
		t.Fatalf("NewSQLiteClient failed: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err = client.Ping(ctx)
	if err != nil {
		t.Errorf("Ping failed: %v", err)
	}

	// Close the DB and try ping again
	client.Close()
	err = client.Ping(ctx)
	if err == nil {
		t.Error("Expected Ping to fail on closed DB, got nil")
	}
	if !strings.Contains(err.Error(), "database is closed") {
		t.Errorf("Expected 'database is closed' error from Ping, got: %v", err)
	}
}
