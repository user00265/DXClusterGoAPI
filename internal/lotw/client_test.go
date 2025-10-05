package lotw_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/user00265/dxclustergoapi/internal/config"
	"github.com/user00265/dxclustergoapi/internal/db"
	"github.com/user00265/dxclustergoapi/internal/lotw"
)

// mockHTTPClient for testing external API calls
type mockHTTPClient struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.DoFunc != nil {
		return m.DoFunc(req)
	}
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
}

// setupTestClient creates a new LoTW client with a temporary SQLite database.
func setupTestClient(t *testing.T, cfg *config.Config) (*lotw.Client, func()) {
	t.Helper()

	tempDir := t.TempDir()
	cfg.DataDir = tempDir // Override data directory for tests

	lotwDBClient, err := db.NewSQLiteClient(cfg.DataDir, lotw.DBFileName)
	if err != nil {
		t.Fatalf("Failed to create LoTW DB client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	client, err := lotw.NewClient(ctx, *cfg, lotwDBClient)
	if err != nil {
		t.Fatalf("Failed to create LoTW client: %v", err)
	}

	cleanup := func() {
		client.StopUpdater() // Stop scheduler gracefully
		cancel()             // Cancel context
		lotwDBClient.Close()
		os.RemoveAll(tempDir) // Clean up temp directory
	}
	return client, cleanup
}

func TestNewClient_Success(t *testing.T) {
	cfg := &config.Config{LoTWUpdateInterval: 1 * time.Minute}
	client, cleanup := setupTestClient(t, cfg)
	defer cleanup()

	if client == nil {
		t.Fatal("NewClient returned nil client")
	}
	// Check if table was created by trying to query
	_, err := client.GetLoTWUserActivity(context.Background(), "NOEXIST")
	// Expected sql.ErrNoRows for a valid query with no results,
	// not "no such table".
	if err != nil && err != sql.ErrNoRows { // `sql` here is database/sql
		t.Fatalf("Expected sql.ErrNoRows or nil for non-existent callsign, got: %v", err)
	}
}

func TestNewClient_ErrorNoDBClient(t *testing.T) {
	cfg := &config.Config{}
	ctx := context.Background()
	_, err := lotw.NewClient(ctx, *cfg, nil)
	if err == nil {
		t.Error("Expected error when dbClient is nil, got nil")
	}
	if !strings.Contains(err.Error(), "dbClient cannot be nil") {
		t.Errorf("Expected 'dbClient cannot be nil' error, got: %v", err)
	}
}

func TestFetchAndStoreUsers_Success(t *testing.T) {
	// Mock HTTP server to serve a test CSV
	mockCSV := `
CALL1,2023-01-01,10:00:00
CALL2,2023-02-15,11:30:45
CALL3,2024-03-20,23:59:59
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/lotw-user-activity.csv" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(strings.TrimSpace(mockCSV)))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Override config to use mock server URL
	cfg := &config.Config{
		LoTWUpdateInterval: 1 * time.Minute,
	}
	// Temporarily modify the global LoTWActivityURL for testing
	originalURL := config.LoTWActivityURL
	defer func() { config.LoTWActivityURL = originalURL }()
	config.LoTWActivityURL = server.URL + "/lotw-user-activity.csv"

	client, cleanup := setupTestClient(t, cfg)
	defer cleanup()

	// Replace the internal httpClient with the test server's client to hit our test server
	client.SetHTTPClient(server.Client())

	ctx := context.Background()
	// Manually trigger the fetch and store, as scheduler is async
	err := client.FetchAndStoreUsers(ctx)
	if err != nil {
		t.Fatalf("FetchAndStoreUsers failed: %v", err)
	}

	// Verify data in DB
	ua1, err := client.GetLoTWUserActivity(ctx, "CALL1")
	if err != nil {
		t.Fatalf("Failed to get CALL1: %v", err)
	}
	if ua1 == nil || ua1.Callsign != "CALL1" || ua1.LastUploadUTC.Format(time.RFC3339) != "2023-01-01T10:00:00Z" {
		t.Errorf("CALL1 data mismatch: %+v", ua1)
	}

	ua2, err := client.GetLoTWUserActivity(ctx, "CALL2")
	if err != nil {
		t.Fatalf("Failed to get CALL2: %v", err)
	}
	if ua2 == nil || ua2.Callsign != "CALL2" || ua2.LastUploadUTC.Format(time.RFC3339) != "2023-02-15T11:30:45Z" {
		t.Errorf("CALL2 data mismatch: %+v", ua2)
	}

	ua3, err := client.GetLoTWUserActivity(ctx, "CALL3")
	if err != nil {
		t.Fatalf("Failed to get CALL3: %v", err)
	}
	if ua3 == nil || ua3.Callsign != "CALL3" || ua3.LastUploadUTC.Format(time.RFC3339) != "2024-03-20T23:59:59Z" {
		t.Errorf("CALL3 data mismatch: %+v", ua3)
	}

	isUser, err := client.IsLoTWUser(ctx, "CALLX")
	if err != nil {
		t.Fatalf("IsLoTWUser failed: %v", err)
	}
	if isUser {
		t.Error("Expected CALLX not to be an LoTW user")
	}
}

func TestFetchAndStoreUsers_HTTPError(t *testing.T) {
	cfg := &config.Config{LoTWUpdateInterval: 1 * time.Minute}
	client, cleanup := setupTestClient(t, cfg)
	defer cleanup()

	// Mock HTTP client to return an error
	client.HTTPClient = &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("simulated network error")
		},
	}

	ctx := context.Background()
	err := client.FetchAndStoreUsers(ctx)
	if err == nil {
		t.Error("Expected error from FetchAndStoreUsers, got nil")
	}
	if !strings.Contains(err.Error(), "simulated network error") {
		t.Errorf("Expected 'simulated network error', got: %v", err)
	}
}

func TestFetchAndStoreUsers_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := &config.Config{LoTWUpdateInterval: 1 * time.Minute}
	originalURL := config.LoTWActivityURL
	defer func() { config.LoTWActivityURL = originalURL }()
	config.LoTWActivityURL = server.URL + "/lotw-user-activity.csv"

	client, cleanup := setupTestClient(t, cfg)
	defer cleanup()

	client.HTTPClient = &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return http.DefaultClient.Do(req)
		},
	}

	ctx := context.Background()
	err := client.FetchAndStoreUsers(ctx)
	if err == nil {
		t.Error("Expected error from FetchAndStoreUsers, got nil")
	}
	if !strings.Contains(err.Error(), "non-OK status: 500") {
		t.Errorf("Expected non-OK status error, got: %v", err)
	}
}

func TestParseLoTWCSV_MalformedLine(t *testing.T) {
	mockCSV := strings.NewReader(`
CALL1,2023-01-01,10:00:00
MALFORMED_LINE_HERE
CALL2,2023-02-15,11:30:45
`)
	users, err := lotw.ParseLoTWCSV(mockCSV)
	if err != nil {
		t.Fatalf("ParseLoTWCSV failed: %v", err)
	}

	if len(users) != 2 {
		t.Errorf("Expected 2 users, got %d. Malformed line should be skipped.", len(users))
	}
	if users[0].Callsign != "CALL1" || users[1].Callsign != "CALL2" {
		t.Errorf("Parsed users mismatch: %+v", users)
	}
}

func TestParseLoTWCSV_InvalidTimestamp(t *testing.T) {
	mockCSV := strings.NewReader(`
CALL1,2023-01-01,INVALID_TIME
CALL2,2023-02-15,11:30:45
`)
	users, err := lotw.ParseLoTWCSV(mockCSV)
	if err != nil {
		t.Fatalf("ParseLoTWCSV failed: %v", err)
	}

	if len(users) != 1 {
		t.Errorf("Expected 1 user, got %d. Invalid timestamp line should be skipped.", len(users))
	}
	if users[0].Callsign != "CALL2" {
		t.Errorf("Parsed users mismatch: %+v", users)
	}
}

func TestReplaceUsersInDB_Concurrency(t *testing.T) {
	cfg := &config.Config{LoTWUpdateInterval: 1 * time.Minute}
	client, cleanup := setupTestClient(t, cfg)
	defer cleanup()

	numConcurrentUpdates := 5
	var wg sync.WaitGroup

	for i := 0; i < numConcurrentUpdates; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			users := []lotw.UserActivity{
				{Callsign: fmt.Sprintf("CONCURRENT%d_1", idx), LastUploadUTC: time.Now().UTC()},
				{Callsign: fmt.Sprintf("CONCURRENT%d_2", idx), LastUploadUTC: time.Now().UTC()},
			}
			err := client.ReplaceUsersInDB(users)
			if err != nil {
				t.Errorf("Concurrent ReplaceUsersInDB failed for index %d: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	// Only the last update should be present due to truncate-and-replace strategy
	dbConn := client.DbClient.GetDB() // Access the *sql.DB for direct query
	rows, err := dbConn.QueryContext(context.Background(), fmt.Sprintf("SELECT callsign FROM %s", lotw.DBTableName))
	if err != nil {
		t.Fatalf("Failed to query all users: %v", err)
	}
	defer rows.Close()

	var finalUsers []string
	for rows.Next() {
		var callsign string
		if err := rows.Scan(&callsign); err != nil {
			t.Fatalf("Failed to scan callsign: %v", err)
		}
		finalUsers = append(finalUsers, callsign)
	}

	if len(finalUsers) != 2 { // Only 2 users should exist (truncate-and-replace)
		t.Errorf("Expected 2 users in DB after concurrent updates, got %d", len(finalUsers))
	}
}

// For tests we directly access unexported fields via provided accessors on the package.
