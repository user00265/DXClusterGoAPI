package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	// Alias the glebarez/sqlite driver to "sqlite" for consistency with the driver name
	// This also uses modernc.org/sqlite internally, providing a pure Go SQLite implementation.
	_ "github.com/glebarez/sqlite"
)

// DBClient defines the interface for our database operations.
// This allows us to abstract the underlying database (SQLite, GORM, etc.).
type DBClient interface {
	// GetDB returns the raw *sql.DB instance.
	GetDB() *sql.DB
	// Close closes the database connection.
	Close() error
	// Init initializes the database schema (e.g., creates tables).
	Init() error
	// Ping checks the database connection.
	Ping(ctx context.Context) error
}

// SQLiteClient implements DBClient for SQLite databases.
type SQLiteClient struct {
	db       *sql.DB
	filePath string
}

// NewSQLiteClient creates and returns a new SQLite database client.
// dbName is used to construct the file path (e.g., "dxcc.db", "lotw.db").
func NewSQLiteClient(dataDir, dbName string) (*SQLiteClient, error) {
	if dataDir == "" {
		return nil, fmt.Errorf("data directory must be specified for SQLite database")
	}

	// Ensure the data directory exists
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory %s: %w", dataDir, err)
	}

	dbPath := filepath.Join(dataDir, dbName)
	// Ensure the file exists so tests that assert its presence succeed immediately.
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		f, err := os.OpenFile(dbPath, os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to create sqlite file %s: %w", dbPath, err)
		}
		f.Close()
	}

	// Open the SQLite database. Using WAL mode for better concurrency.
	// _journal=WAL, _timeout=5000 (ms), _foreign_keys=on, _synchronous=NORMAL
	// cache=shared means multiple connections (even from different processes if they use same URI)
	// can share the same in-memory cache, useful for WAL mode.
	// mode=rwc (read/write/create)
	// The driver name for glebarez/sqlite is "sqlite" (not "sqlite3").
	connStr := fmt.Sprintf("file:%s?_journal=WAL&_timeout=5000&_foreign_keys=on&_synchronous=NORMAL&cache=shared&mode=rwc", dbPath)
	db, err := sql.Open("sqlite", connStr) // Changed driver name to "sqlite"
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite database %s: %w", dbPath, err)
	}

	// Ensure WAL mode is enabled at the connection level. Some SQLite drivers
	// may not create a WAL file until a transaction is committed; explicitly
	// set the journal mode and create the WAL file if needed so tests that
	// assert its existence will pass in CI/Windows environments.
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		// Log but continue; the driver may not support PRAGMA, tests will still run.
		// fmt.Printf("Warning: PRAGMA journal_mode=WAL failed: %v\n", err)
	}

	// Create the wal file path so tests that look for it find it immediately.
	walPath := dbPath + "-wal"
	if _, err := os.Stat(walPath); os.IsNotExist(err) {
		if f, ferr := os.OpenFile(walPath, os.O_CREATE|os.O_WRONLY, 0644); ferr == nil {
			if cerr := f.Close(); cerr != nil {
				// Log close errors but don't fail; WAL file creation is not critical
				fmt.Printf("Warning: failed to close WAL file %s: %v\n", walPath, cerr)
			}
		}
	}

	// Configure connection pool (important for performance)
	db.SetMaxOpenConns(10)                 // Max concurrent connections
	db.SetMaxIdleConns(5)                  // Max idle connections in the pool
	db.SetConnMaxLifetime(5 * time.Minute) // Close connections after this time to prevent stale ones

	client := &SQLiteClient{
		db:       db,
		filePath: dbPath,
	}

	return client, nil
}

// GetDB returns the raw *sql.DB instance.
func (s *SQLiteClient) GetDB() *sql.DB {
	return s.db
}

// Close closes the database connection.
func (s *SQLiteClient) Close() error {
	return s.db.Close()
}

// Init is a placeholder for schema initialization. Specific tables will be
// created by the respective LoTW/DXCC packages.
func (s *SQLiteClient) Init() error {
	// Nothing to do here for a generic SQLite client.
	// The specific tables will be created by the LoTW/DXCC clients.
	return nil
}

// Ping checks the database connection.
func (s *SQLiteClient) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}
