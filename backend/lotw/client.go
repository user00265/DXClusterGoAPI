package lotw

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	// using ticker-based updates instead of gocron

	"github.com/user00265/dxclustergoapi/config"
	"github.com/user00265/dxclustergoapi/db"
	"github.com/user00265/dxclustergoapi/logging"
	"github.com/user00265/dxclustergoapi/version" // For User-Agent
)

const (
	DBFileName        = "lotw.db" // Made public for use in main and tests
	dbTableName       = "lotw_users"
	metadataTableName = "lotw_metadata"
)

// DBTableName is exported for tests.
const DBTableName = dbTableName

// User-Agent info set at build time via ldflags

// UserActivity represents a single entry from the LoTW user activity CSV.
type UserActivity struct {
	Callsign      string
	LastUploadUTC time.Time
}

// Client manages LoTW user activity data.
type Client struct {
	cfg        config.Config
	dbClient   db.DBClient
	httpClient *http.Client
	// Exported testing hooks
	HTTPClient HTTPDoer
	DbClient   db.DBClient
	updateStop chan struct{}
	updateDone chan struct{}
}

// HTTPDoer is a minimal interface for http clients used in tests.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// NewClient creates and returns a new LoTW client.
func NewClient(ctx context.Context, cfg config.Config, dbClient db.DBClient) (*Client, error) {
	if dbClient == nil {
		return nil, fmt.Errorf("dbClient cannot be nil")
	}

	client := &Client{
		cfg:      cfg,
		dbClient: dbClient,
		httpClient: &http.Client{
			Timeout: 30 * time.Second, // Configurable API timeout for HTTP requests
		},
	}

	// Expose testing hooks
	client.HTTPClient = client.httpClient
	client.DbClient = client.dbClient

	// Initialize the LoTW table
	if err := client.createTable(); err != nil {
		return nil, fmt.Errorf("failed to create LoTW users table: %w", err)
	}

	// updater will be started with StartUpdater (ticker-based)
	client.updateStop = nil
	client.updateDone = nil

	return client, nil
}

// createTable creates the lotw_users table if it doesn't exist, and handles schema migrations.
func (c *Client) createTable() error {
	queries := []string{
		fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %s (
				callsign TEXT PRIMARY KEY,
				last_upload_utc TEXT NOT NULL
			);
		`, dbTableName),
		fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %s (
				data_type TEXT PRIMARY KEY,
				last_updated TEXT NOT NULL,
				file_size INTEGER,
				source_url TEXT
			);
		`, metadataTableName),
	}

	db := c.dbClient.GetDB()
	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("failed to execute query: %w\nQuery: %s", err, query)
		}
	}

	// Handle schema migration: check if old schema with lotw_member column exists
	if err := c.migrateSchema(db); err != nil {
		return fmt.Errorf("failed to migrate schema: %w", err)
	}

	return nil
}

// migrateSchema handles schema migrations for existing databases.
// If the old schema with lotw_member column is detected, it will be removed
// since we now calculate days since upload dynamically.
func (c *Client) migrateSchema(db *sql.DB) error {
	// Check if the lotw_member column exists in the table
	query := fmt.Sprintf("PRAGMA table_info(%s);", dbTableName)
	rows, err := db.Query(query)
	if err != nil {
		return fmt.Errorf("failed to check table schema: %w", err)
	}
	defer rows.Close()

	hasLoTWMemberColumn := false
	for rows.Next() {
		var cid int
		var name string
		var type_ string
		var notnull int
		var dfltValue interface{}
		var pk int

		if err := rows.Scan(&cid, &name, &type_, &notnull, &dfltValue, &pk); err != nil {
			return fmt.Errorf("failed to scan column info: %w", err)
		}

		if name == "lotw_member" {
			hasLoTWMemberColumn = true
			break
		}
	}

	// If old schema exists, recreate the table without the lotw_member column
	if hasLoTWMemberColumn {
		logging.Info("Detected old LoTW schema with lotw_member column. Migrating to new schema...")

		// Create a temporary table with the new schema
		tempTableName := dbTableName + "_new"
		createTempQuery := fmt.Sprintf(`
			CREATE TABLE %s (
				callsign TEXT PRIMARY KEY,
				last_upload_utc TEXT NOT NULL
			);
		`, tempTableName)

		if _, err := db.Exec(createTempQuery); err != nil {
			return fmt.Errorf("failed to create temporary table: %w", err)
		}

		// Copy data from old table to new table (only the columns that exist in new schema)
		copyQuery := fmt.Sprintf(`
			INSERT INTO %s (callsign, last_upload_utc)
			SELECT callsign, last_upload_utc FROM %s;
		`, tempTableName, dbTableName)

		if _, err := db.Exec(copyQuery); err != nil {
			// Clean up temporary table
			db.Exec(fmt.Sprintf("DROP TABLE %s;", tempTableName))
			return fmt.Errorf("failed to copy data to temporary table: %w", err)
		}

		// Drop old table
		dropQuery := fmt.Sprintf("DROP TABLE %s;", dbTableName)
		if _, err := db.Exec(dropQuery); err != nil {
			// Clean up temporary table
			db.Exec(fmt.Sprintf("DROP TABLE %s;", tempTableName))
			return fmt.Errorf("failed to drop old table: %w", err)
		}

		// Rename temporary table to original name
		renameQuery := fmt.Sprintf("ALTER TABLE %s RENAME TO %s;", tempTableName, dbTableName)
		if _, err := db.Exec(renameQuery); err != nil {
			return fmt.Errorf("failed to rename table: %w", err)
		}

		logging.Info("Successfully migrated LoTW schema to new version")

		// Clear the download metadata so the data will be re-fetched with the new schema
		clearMetadataQuery := fmt.Sprintf("DELETE FROM %s WHERE data_type = 'lotw';", metadataTableName)
		if _, err := db.Exec(clearMetadataQuery); err != nil {
			logging.Warn("Failed to clear LoTW metadata during migration: %v", err)
		}
	}

	return nil
}

// updateLastDownloadTime records when LoTW data was last downloaded.
func (c *Client) updateLastDownloadTime(ctx context.Context, sourceURL string, fileSize int) error {
	db := c.dbClient.GetDB()
	query := fmt.Sprintf(`
		INSERT OR REPLACE INTO %s (data_type, last_updated, file_size, source_url)
		VALUES ('lotw', ?, ?, ?)
	`, metadataTableName)

	_, err := db.ExecContext(ctx, query, time.Now().UTC().Format(time.RFC3339), fileSize, sourceURL)
	if err != nil {
		return fmt.Errorf("failed to update LoTW download metadata: %w", err)
	}
	return nil
}

// GetLastDownloadTime retrieves when LoTW data was last downloaded.
func (c *Client) GetLastDownloadTime(ctx context.Context) (time.Time, error) {
	db := c.dbClient.GetDB()
	query := fmt.Sprintf(`
		SELECT last_updated FROM %s WHERE data_type = 'lotw'
	`, metadataTableName)

	var lastUpdated string
	err := db.QueryRowContext(ctx, query).Scan(&lastUpdated)
	if err != nil {
		if err == sql.ErrNoRows {
			// No record found, return zero time to indicate never downloaded
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("failed to query LoTW download metadata: %w", err)
	}

	t, err := time.Parse(time.RFC3339, lastUpdated)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse last download time: %w", err)
	}
	return t, nil
}

// needsUpdate checks if LoTW data needs to be updated based on the configured interval.
func (c *Client) needsUpdate(ctx context.Context) (bool, error) {
	lastUpdate, err := c.GetLastDownloadTime(ctx)
	if err != nil {
		return false, err
	}

	// If never downloaded (zero time), needs update
	if lastUpdate.IsZero() {
		return true, nil
	}

	// Check if data is older than the update interval
	if time.Since(lastUpdate) >= c.cfg.LoTWUpdateInterval {
		return true, nil
	}

	// If last update was recent, also check if database actually has data
	// This prevents the case where metadata says "updated recently" but database is empty
	recordCount, err := c.getTableRecordCount(ctx, dbTableName)
	if err != nil {
		logging.Warn("Failed to count LoTW records, assuming update needed: %v", err)
		return true, nil
	}

	// If table is empty despite recent update, force re-download
	if recordCount == 0 {
		logging.Info("LoTW database appears empty (%d records) despite recent update - forcing re-download", recordCount)
		return true, nil
	}

	return false, nil
}

// getTableRecordCount returns the number of records in the specified table.
func (c *Client) getTableRecordCount(ctx context.Context, tableName string) (int, error) {
	var count int
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)
	err := c.dbClient.GetDB().QueryRowContext(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count records in %s: %w", tableName, err)
	}
	return count, nil
} // StartUpdater starts the periodic update job for LoTW data.
func (c *Client) StartUpdater(ctx context.Context) {
	if c.updateStop != nil {
		return // already running
	}
	c.updateStop = make(chan struct{})
	c.updateDone = make(chan struct{})

	// Check if initial update is needed
	go func() {
		needsUpdate, err := c.needsUpdate(ctx)
		if err != nil {
			logging.Error("Failed to check if LoTW data needs update: %v", err)
			// Fallback to update on error
			needsUpdate = true
		}

		if needsUpdate {
			logging.Info("LoTW data needs update, fetching...")
			c.fetchAndStoreUsers(ctx)
		} else {
			logging.Notice("LoTW data is up to date, skipping initial download")
		}
	}()

	go func() {
		ticker := time.NewTicker(c.cfg.LoTWUpdateInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.fetchAndStoreUsers(ctx)
			case <-c.updateStop:
				close(c.updateDone)
				return
			case <-ctx.Done():
				close(c.updateDone)
				return
			}
		}
	}()
	logging.Info("LoTW updater started. Will check for updates every %s.", c.cfg.LoTWUpdateInterval)
}

// StopUpdater halts the periodic update and releases resources.
func (c *Client) StopUpdater() {
	if c.updateStop != nil {
		close(c.updateStop)
		<-c.updateDone
		c.updateStop = nil
		c.updateDone = nil
	}
	logging.Info("LoTW updater stopped.")
}

// fetchAndStoreUsers downloads the CSV, parses it, and replaces data in the database.
func (c *Client) fetchAndStoreUsers(ctx context.Context) error {
	logging.Info("Fetching LoTW user activity data")
	req, err := http.NewRequestWithContext(ctx, "GET", config.LoTWActivityURL, nil)
	if err != nil {
		logging.Error("Failed to create HTTP request for LoTW CSV: %v", err)
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", version.UserAgent)

	// Allow tests to inject a custom HTTP client via the HTTPClient interface
	var resp *http.Response
	var doErr error
	if c.HTTPClient != nil {
		resp, doErr = c.HTTPClient.Do(req)
	} else {
		resp, doErr = c.httpClient.Do(req)
	}
	if doErr != nil {
		logging.Error("HTTP request to LoTW CSV failed: %v", doErr)
		return fmt.Errorf("failed to fetch LoTW CSV: %w", doErr)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logging.Error("LoTW CSV download returned non-OK status: %s", resp.Status)
		return fmt.Errorf("non-OK status: %s", resp.Status)
	}

	// Read the response body to get file size for metadata tracking
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		logging.Error("Failed to read LoTW CSV response: %v", err)
		return fmt.Errorf("failed to read LoTW CSV: %w", err)
	}

	parsedUsers, err := parseLoTWCSV(strings.NewReader(string(data)))
	if err != nil {
		logging.Error("Failed to parse LoTW CSV: %v", err)
		return fmt.Errorf("failed to parse LoTW CSV: %w", err)
	}
	logging.Info("Parsed %d LoTW user entries.", len(parsedUsers))

	if err := c.replaceUsersInDB(parsedUsers); err != nil {
		logging.Error("Failed to store LoTW users in DB: %v", err)
		return fmt.Errorf("failed to store LoTW users: %w", err)
	}

	// Record the successful download
	if err := c.updateLastDownloadTime(ctx, config.LoTWActivityURL, len(data)); err != nil {
		logging.Warn("Failed to update LoTW download metadata: %v", err)
	}

	logging.Info("LoTW user activity update completed.")
	return nil
}

// parseLoTWCSV parses the LoTW CSV data from an io.Reader.
func parseLoTWCSV(r io.Reader) ([]UserActivity, error) {
	scanner := bufio.NewScanner(r)
	var users []UserActivity
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ",")
		if len(parts) != 3 {
			logging.Warn("Skipping malformed LoTW CSV line: %s", line)
			continue
		}

		callsign := strings.ToUpper(strings.TrimSpace(parts[0]))
		dateStr := strings.TrimSpace(parts[1])
		timeStr := strings.TrimSpace(parts[2])

		// LoTW dates are UTC YYYY-MM-DD, times are UTC HH:MM:SS
		// We'll combine them and parse as UTC.
		combinedDateTimeStr := fmt.Sprintf("%s %s UTC", dateStr, timeStr)
		lastUpload, err := time.Parse("2006-01-02 15:04:05 MST", combinedDateTimeStr)
		if err != nil {
			logging.Warn("Failed to parse LoTW timestamp '%s': %v. Skipping line: %s", combinedDateTimeStr, err, line)
			continue
		}

		users = append(users, UserActivity{
			Callsign:      callsign,
			LastUploadUTC: lastUpload,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading LoTW CSV: %w", err)
	}
	return users, nil
}

// ParseLoTWCSV is exported for tests to parse CSV input.
func ParseLoTWCSV(r io.Reader) ([]UserActivity, error) {
	return parseLoTWCSV(r)
}

// GetHTTPClient exposes the internal http client for tests.
func (c *Client) GetHTTPClient() *http.Client {
	return c.httpClient
}

// SetHTTPClient allows tests to inject a custom *http.Client (including custom Transport).
func (c *Client) SetHTTPClient(h *http.Client) {
	if h == nil {
		return
	}
	c.httpClient = h
	// Also update the HTTPClient interface for tests that use it
	c.HTTPClient = h
}

// GetDbClient exposes the db client for tests.
func (c *Client) GetDbClient() db.DBClient {
	return c.dbClient
}

// FetchAndStoreUsers calls the internal fetch and store.
func (c *Client) FetchAndStoreUsers(ctx context.Context) error {
	return c.fetchAndStoreUsers(ctx)
}

// ReplaceUsersInDB exposes the replace operation for tests.
func (c *Client) ReplaceUsersInDB(users []UserActivity) error {
	return c.replaceUsersInDB(users)
}

// replaceUsersInDB truncates the table and inserts new users in a single transaction.
func (c *Client) replaceUsersInDB(users []UserActivity) (err error) {
	db := c.dbClient.GetDB()
	tx, err := db.BeginTx(context.Background(), nil) // Use background context for transaction
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// Truncate table
	if _, err = tx.Exec(fmt.Sprintf("DELETE FROM %s", dbTableName)); err != nil {
		return fmt.Errorf("failed to truncate %s table: %w", dbTableName, err)
	}

	// Prepare statement for insertion
	stmt, err := tx.Prepare(fmt.Sprintf("INSERT INTO %s (callsign, last_upload_utc) VALUES (?, ?)", dbTableName))
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close() // Close the statement after loop, before commit

	for _, user := range users {
		// Store as ISO 8601 string for consistency and easy parsing back to time.Time
		_, err = stmt.Exec(user.Callsign, user.LastUploadUTC.Format(time.RFC3339))
		if err != nil {
			return fmt.Errorf("failed to insert LoTW user %s: %w", user.Callsign, err)
		}
	}

	err = tx.Commit()
	return err
}

// IsLoTWUser checks if a callsign is in the LoTW database.
// Returns true if found, false otherwise, and an error if the lookup fails.
func (c *Client) IsLoTWUser(ctx context.Context, callsign string) (bool, error) {
	db := c.dbClient.GetDB()
	var count int
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE callsign = ?", dbTableName)
	err := db.QueryRowContext(ctx, query, strings.ToUpper(strings.TrimSpace(callsign))).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to query LoTW user: %w", err)
	}
	return count > 0, nil
}

// GetLoTWUserActivity retrieves a user's activity details.
// Returns nil, nil if not found.
func (c *Client) GetLoTWUserActivity(ctx context.Context, callsign string) (*UserActivity, error) {
	db := c.dbClient.GetDB()
	var ua UserActivity
	var lastUploadStr string
	query := fmt.Sprintf("SELECT callsign, last_upload_utc FROM %s WHERE callsign = ?", dbTableName)
	err := db.QueryRowContext(ctx, query, strings.ToUpper(strings.TrimSpace(callsign))).Scan(&ua.Callsign, &lastUploadStr)
	if err == sql.ErrNoRows {
		return nil, nil // Not found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve LoTW user activity for %s: %w", callsign, err)
	}

	// Parse the stored ISO 8601 string back to time.Time
	ua.LastUploadUTC, err = time.Parse(time.RFC3339, lastUploadStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse stored last_upload_utc for %s: %w", callsign, err)
	}

	return &ua, nil
}
