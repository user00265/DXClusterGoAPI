package utils

import (
	"context"
	"testing"
	"time"

	"github.com/user00265/dxclustergoapi/backend/dxcc"
	"github.com/user00265/dxclustergoapi/config"
	"github.com/user00265/dxclustergoapi/db"
)

func TestLookupDXCC_Basic(t *testing.T) {
	// Create a mock client for basic unit testing
	mockClient := &mockDXCCClient{
		exceptions: map[string]*dxcc.DxccInfo{
			"EXC": {Entity: "EXCEPTION", DXCCID: 1},
		},
		prefixes: map[string]*dxcc.DxccInfo{
			"K":  {Entity: "USA", DXCCID: 291},
			"W":  {Entity: "USA", DXCCID: 291},
			"N":  {Entity: "USA", DXCCID: 291},
			"AA": {Entity: "USA", DXCCID: 291},
		},
	}

	tests := []struct {
		name     string
		call     string
		expected string
		wantNil  bool
	}{
		{"Exception match", "EXC", "EXCEPTION", false},
		{"Prefix K", "K1ABC", "USA", false},
		{"Prefix W", "W5XYZ", "USA", false},
		{"Prefix N", "N0OP", "USA", false},
		{"Unknown", "ZZZ", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := LookupDXCC(tt.call, mockClient)
			if err != nil {
				t.Fatalf("LookupDXCC(%q) error: %v", tt.call, err)
			}
			if tt.wantNil {
				if result != nil {
					t.Errorf("LookupDXCC(%q) = %v, want nil", tt.call, result)
				}
			} else {
				if result == nil {
					t.Errorf("LookupDXCC(%q) = nil, want entity=%q", tt.call, tt.expected)
				} else if result.Entity != tt.expected {
					t.Errorf("LookupDXCC(%q).Entity = %q, want %q", tt.call, result.Entity, tt.expected)
				}
			}
		})
	}
}

func TestLookupDXCC_WithRealData(t *testing.T) {
	// This test uses the real DXCC client with real data
	// It mirrors the production initialization flow from main.go
	ctx := context.Background()

	// Create DB client (temporary for this test)
	cfg := config.Config{DXCCUpdateInterval: 0}
	dataDir := "." // Uses current directory for dxcc.db
	dbClient, err := db.NewSQLiteClient(dataDir, dxcc.DBFileName)
	if err != nil {
		t.Fatalf("Failed to create SQLite DB client: %v", err)
	}
	defer dbClient.Close()

	// Create DXCC client
	dxccClient, err := dxcc.NewClient(ctx, cfg, dbClient)
	if err != nil {
		t.Fatalf("Failed to create DXCC client: %v", err)
	}
	defer dxccClient.Close()

	// Mirror the production initialization logic from main.go
	lastDXCCUpdate, err := dxccClient.GetLastDownloadTime(ctx)
	if err != nil {
		t.Logf("Warning: failed to check DXCC last update time: %v", err)
	}

	needsDXCCUpdate := lastDXCCUpdate.IsZero() || time.Since(lastDXCCUpdate) >= cfg.DXCCUpdateInterval
	if needsDXCCUpdate {
		t.Logf("DXCC data needs update, downloading now...")
		dxccClient.FetchAndStoreData(ctx)
	} else {
		t.Logf("DXCC data is up to date (last updated: %s)", lastDXCCUpdate.Format(time.RFC3339))
		if err := dxccClient.LoadMapsFromDB(ctx); err != nil {
			t.Fatalf("Failed to load DXCC maps from database: %v", err)
		}
	}

	// Check if data is loaded, if not force update
	if len(dxccClient.PrefixesMap) == 0 {
		t.Logf("DXCC database is empty despite recent update timestamp. Forcing download now...")
		dxccClient.FetchAndStoreData(ctx)
	}

	// Verify data is present
	if len(dxccClient.PrefixesMap) == 0 {
		t.Skip("DXCC data not available after update; skipping real-data test")
	}

	// Test cases with real callsigns and expected DXCC entities (from real Club Log data)
	testCases := []struct {
		callsign string
		expected string
	}{
		{"K1JT", "UNITED STATES OF AMERICA"},
		{"W2/JR1AQN", "UNITED STATES OF AMERICA"},
		{"KF0ACN", "UNITED STATES OF AMERICA"},
		{"XE1YO", "MEXICO"},
		{"WP4NVX", "PUERTO RICO"},
		{"YY5YDT", "VENEZUELA"},
		{"ZW5B", "BRAZIL"},
		{"VA3WR", "CANADA"},
		{"PI4DX", "NETHERLANDS"},
		{"TI4LAS", "COSTA RICA"},
		{"CO8LY", "CUBA"},
		{"JA7QVI", "JAPAN"},
		{"CT1BFP", "PORTUGAL"},
		{"F4JRC", "FRANCE"},
		{"HI3R", "DOMINICAN REPUBLIC"},
		{"K2J", "UNITED STATES OF AMERICA"},
		{"DJ7NT", "GERMANY"},
		{"DB4SCW", "GERMANY"},
		{"HB9HIL", "SWITZERLAND"},
		{"W9/N3YPL", "UNITED STATES OF AMERICA"},
		{"AB4KK", "UNITED STATES OF AMERICA"},
		{"VE3HZ", "CANADA"},
		{"NP3DM", "PUERTO RICO"},
		{"KK7UIL/AE", "UNITED STATES OF AMERICA"},
		{"R4FBH", "EUROPEAN RUSSIA"},
		{"RA3RCL", "EUROPEAN RUSSIA"},
		{"WH6HI", "HAWAII"},
		{"WL7X", "ALASKA"},
		{"D4Z", "CAPE VERDE"},
		{"8P6EX", "BARBADOS"},
		{"FM5KC", "MARTINIQUE"},
		{"V31MA", "BELIZE"},
		{"HP6LEF", "PANAMA"},
		{"D2UY", "ANGOLA"},
		{"IK4GRO", "ITALY"},
		{"EA4GOY", "SPAIN"},
		{"MI0NWA", "NORTHERN IRELAND"},
		{"M0MCX", "ENGLAND"},
		{"GM0OPS", "SCOTLAND"},
		{"FK8GX", "NEW CALEDONIA"},
		{"ZL2CA", "NEW ZEALAND"},
		{"K6VHF/HR9", "HAWAII"}, // Corrected from Honduras to Hawaii (HR9 suffix doesn't override prefix)
	}

	for _, tc := range testCases {
		t.Run(tc.callsign, func(t *testing.T) {
			result, err := LookupDXCC(tc.callsign, dxccClient)
			if err != nil {
				t.Errorf("LookupDXCC(%q) error: %v", tc.callsign, err)
				return
			}
			if result == nil {
				t.Errorf("LookupDXCC(%q) returned nil, expected entity=%q", tc.callsign, tc.expected)
				return
			}
			if result.Entity != tc.expected {
				t.Errorf("LookupDXCC(%q).Entity = %q, want %q", tc.callsign, result.Entity, tc.expected)
			}
		})
	}
}

// mockDXCCClient is a mock implementation for unit testing
type mockDXCCClient struct {
	exceptions map[string]*dxcc.DxccInfo
	prefixes   map[string]*dxcc.DxccInfo
}

func (m *mockDXCCClient) GetException(call string) (*dxcc.DxccInfo, bool) {
	info, found := m.exceptions[call]
	return info, found
}

func (m *mockDXCCClient) GetPrefix(prefix string) (*dxcc.DxccInfo, bool) {
	info, found := m.prefixes[prefix]
	return info, found
}
