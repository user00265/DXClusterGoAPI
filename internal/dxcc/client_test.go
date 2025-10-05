package dxcc_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/user00265/dxclustergoapi/internal/config"
	"github.com/user00265/dxclustergoapi/internal/db"
	"github.com/user00265/dxclustergoapi/internal/dxcc"
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

// createGzippedXML creates a gzipped byte slice from an XML string.
func createGzippedXML(xmlContent string) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(xmlContent)); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// setupTestClient creates a new DXCC client with a temporary SQLite database.
func setupTestClient(t *testing.T, cfg *config.Config) (*dxcc.Client, func()) {
	t.Helper()

	tempDir := t.TempDir()
	cfg.DataDir = tempDir // Override data directory for tests

	dxccDBClient, err := db.NewSQLiteClient(cfg.DataDir, dxcc.DBFileName)
	if err != nil {
		t.Fatalf("Failed to create DXCC DB client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	client, err := dxcc.NewClient(ctx, *cfg, dxccDBClient)
	if err != nil {
		t.Fatalf("Failed to create DXCC client: %v", err)
	}

	cleanup := func() {
		client.Close() // Stop scheduler gracefully
		cancel()       // Cancel context
		dxccDBClient.Close()
		os.RemoveAll(tempDir) // Clean up temp directory
	}
	return client, cleanup
}

const testXMLContent = `<?xml version="1.0" encoding="ISO-8859-1"?>
<cty>
  <prefixes>
    <prefix record="1">
      <call>K</call>
      <entity>United States</entity>
      <adif>291</adif>
      <cqz>5</cqz>
      <cont>NA</cont>
      <long>-98.0</long>
      <lat>39.0</lat>
    </prefix>
    <prefix record="2">
      <call>DX1</call>
      <entity>Test DX1 Entity</entity>
      <adif>901</adif>
      <cqz>1</cqz>
      <cont>AS</cont>
      <long>1.0</long>
      <lat>1.0</lat>
    </prefix>
    <prefix record="3">
      <call>3A</call>
      <entity>Monaco</entity>
      <adif>260</adif>
      <cqz>14</cqz>
      <cont>EU</cont>
      <long>7.4</long>
      <lat>43.7</lat>
      <start>2000-01-01 00:00:00</start>
      <end>2030-12-31 23:59:59</end>
    </prefix>
    <prefix record="4">
      <call>KG4</call>
      <entity>Guantanamo Bay</entity>
      <adif>105</adif>
      <cqz>8</cqz>
      <cont>NA</cont>
      <long>-75.1</long>
      <lat>19.9</lat>
    </prefix>
    <prefix record="5">
      <call>OH</call>
      <entity>Finland</entity>
      <adif>224</adif>
      <cqz>15</cqz>
      <cont>EU</cont>
      <long>25.0</long>
      <lat>64.0</lat>
    </prefix>
    <prefix record="6">
      <call>CX</call>
      <entity>Uruguay</entity>
      <adif>144</adif>
      <cqz>13</cqz>
      <cont>SA</cont>
      <long>-56.0</long>
      <lat>-33.0</lat>
    </prefix>
    <prefix record="7">
      <call>3D2/R</call>
      <entity>Rotuma I.</entity>
      <adif>460</adif>
      <cqz>32</cqz>
      <cont>OC</cont>
      <long>177.0</long>
      <lat>-12.5</lat>
    </prefix>
    <prefix record="8">
      <call>3D2/C</call>
      <entity>Conway Reef</entity>
      <adif>489</adif>
      <cqz>32</cqz>
      <cont>OC</cont>
      <long>174.4</long>
      <lat>-21.8</lat>
    </prefix>
    <prefix record="9">
      <call>LZ</call>
      <entity>Bulgaria</entity>
      <adif>212</adif>
      <cqz>20</cqz>
      <cont>EU</cont>
      <long>25.0</long>
      <lat>42.6</lat>
    </prefix>
  </prefixes>
  <exceptions>
    <exception record="1">
      <call>LZ0</call>
      <entity>Antarctica</entity>
      <adif>13</adif>
      <cqz>13</cqz>
      <cont>AN</cont>
      <long>0.0</long>
      <lat>-90.0</lat>
    </exception>
  </exceptions>
  <entities>
    <entity adif="0">
      <name>- NONE - (e.g. /MM, /AM)</name>
      <prefix></prefix>
      <ituz>0</ituz>
      <cqz>0</cqz>
      <cont></cont>
      <long>0.0</long>
      <lat>0.0</lat>
    </entity>
    <entity adif="291">
      <name>United States</name>
      <prefix>K</prefix>
      <ituz>8</ituz>
      <cqz>5</cqz>
      <cont>NA</cont>
      <long>-98.0</long>
      <lat>39.0</lat>
    </entity>
    <entity adif="901">
      <name>Test DX1 Entity</name>
      <prefix>DX1</prefix>
      <ituz>1</ituz>
      <cqz>1</cqz>
      <cont>AS</cont>
      <long>1.0</long>
      <lat>1.0</lat>
    </entity>
    <entity adif="260">
      <name>Monaco</name>
      <prefix>3A</prefix>
      <ituz>27</ituz>
      <cqz>14</cqz>
      <cont>EU</cont>
      <long>7.4</long>
      <lat>43.7</lat>
    </entity>
    <entity adif="105">
      <name>Guantanamo Bay</name>
      <prefix>KG4</prefix>
      <ituz>11</ituz>
      <cqz>8</cqz>
      <cont>NA</cont>
      <long>-75.1</long>
      <lat>19.9</lat>
    </entity>
    <entity adif="224">
      <name>Finland</name>
      <prefix>OH</prefix>
      <ituz>18</ituz>
      <cqz>15</cqz>
      <cont>EU</cont>
      <long>25.0</long>
      <lat>64.0</lat>
    </entity>
    <entity adif="144">
      <name>Uruguay</name>
      <prefix>CX</prefix>
      <ituz>14</ituz>
      <cqz>13</cqz>
      <cont>SA</cont>
      <long>-56.0</long>
      <lat>-33.0</lat>
    </entity>
    <entity adif="460">
      <name>Rotuma I.</name>
      <prefix>3D2/R</prefix>
      <ituz>56</ituz>
      <cqz>32</cqz>
      <cont>OC</cont>
      <long>177.0</long>
      <lat>-12.5</lat>
    </entity>
    <entity adif="489">
      <name>Conway Reef</name>
      <prefix>3D2/C</prefix>
      <ituz>56</ituz>
      <cqz>32</cqz>
      <cont>OC</cont>
      <long>174.4</long>
      <lat>-21.8</lat>
    </entity>
    <entity adif="212">
      <name>Bulgaria</name>
      <prefix>LZ</prefix>
      <ituz>28</ituz>
      <cqz>20</cqz>
      <cont>EU</cont>
      <long>25.0</long>
      <lat>42.6</lat>
    </entity>
    <entity adif="13">
      <name>Antarctica</name>
      <prefix>LA/LH</prefix>
      <ituz>72</ituz>
      <cqz>12</cqz>
      <cont>AN</cont>
      <long>0.0</long>
      <lat>-90.0</lat>
    </entity>
  </entities>
</cty>`

func TestNewClient_ErrorNoDBClient(t *testing.T) {
	cfg := &config.Config{}
	ctx := context.Background()
	_, err := dxcc.NewClient(ctx, *cfg, nil)
	if err == nil {
		t.Error("Expected error when dbClient is nil, got nil")
	}
	if !strings.Contains(err.Error(), "dbClient cannot be nil") {
		t.Errorf("Expected 'dbClient cannot be nil' error, got: %v", err)
	}
}

func TestFetchAndStoreData_Success(t *testing.T) {
	gzippedXML, err := createGzippedXML(testXMLContent)
	if err != nil {
		t.Fatalf("Failed to gzip test XML: %v", err)
	}

	// Mock HTTP server to serve gzipped XML
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/cty.php") || strings.HasSuffix(r.URL.Path, "cty.xml.gz") {
			w.Header().Set("Content-Encoding", "gzip")
			w.WriteHeader(http.StatusOK)
			w.Write(gzippedXML)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		ClubLogAPIKey:      "dummykey",
		ClubLogAPIURL:      server.URL + "/cty.php?api=%s",
		FallbackGitHubURL:  server.URL + "/cty.xml.gz",
		DXCCUpdateInterval: 1 * time.Minute,
	}

	client, cleanup := setupTestClient(t, cfg)
	defer cleanup()

	// Inject mock HTTP client via SetHTTPDoer
	client.SetHTTPDoer(&mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return http.DefaultClient.Do(req)
		},
	})

	ctx := context.Background()
	// Manually trigger the fetch and store, as scheduler is async
	client.FetchAndStoreData(ctx) // This calls loadMapsFromDB internally

	// Verify data loaded into in-memory maps
	if len(client.PrefixesMap) != 9 {
		t.Errorf("Expected 9 prefixes, got %d", len(client.PrefixesMap))
	}
	if len(client.ExceptionsMap) != 1 {
		t.Errorf("Expected 1 exception, got %d", len(client.ExceptionsMap))
	}
	if len(client.EntitiesMap) != 11 { // 10 from XML + 1 for ADIF 0
		t.Errorf("Expected 11 entities, got %d", len(client.EntitiesMap))
	}

	// Basic check on a prefix
	pfx := client.PrefixesMap["K"]
	if pfx.Entity != "United States" || pfx.ADIF != 291 || pfx.CQZ != 5 || pfx.Cont != "NA" {
		t.Errorf("Prefix 'K' mismatch: %+v", pfx)
	}
	// Basic check on an entity
	entity := client.EntitiesMap[291]
	if entity.Name != "United States" || entity.ITUZ != 8 || entity.ADIF != 291 {
		t.Errorf("Entity '291' mismatch: %+v", entity)
	}
	// Basic check on an exception
	exc := client.ExceptionsMap["LZ0"]
	if exc.Entity != "Antarctica" || exc.ADIF != 13 {
		t.Errorf("Exception 'LZ0' mismatch: %+v", exc)
	}
}

func TestFetchAndStoreData_ClubLogFailsFallbackToGitHub(t *testing.T) {
	gzippedXML, err := createGzippedXML(testXMLContent)
	if err != nil {
		t.Fatalf("Failed to gzip test XML: %v", err)
	}

	// Mock server for Club Log (always fails) and GitHub (succeeds)
	clubLogServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError) // Simulate failure
	}))
	defer clubLogServer.Close()

	gitHubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "cty.xml.gz") {
			w.Header().Set("Content-Encoding", "gzip")
			w.WriteHeader(http.StatusOK)
			w.Write(gzippedXML)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer gitHubServer.Close()

	cfg := &config.Config{
		ClubLogAPIKey:      "dummykey",
		ClubLogAPIURL:      clubLogServer.URL + "/cty.php?api=%s", // This will fail
		FallbackGitHubURL:  gitHubServer.URL + "/cty.xml.gz",      // This will succeed
		DXCCUpdateInterval: 1 * time.Minute,
	}

	client, cleanup := setupTestClient(t, cfg)
	defer cleanup()

	client.SetHTTPDoer(&mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.String(), "cty.php") {
				return clubLogServer.Client().Do(req)
			}
			return gitHubServer.Client().Do(req)
		},
	})

	ctx := context.Background()
	client.FetchAndStoreData(ctx)

	// Verify data from GitHub fallback is loaded
	if len(client.PrefixesMap) != 9 {
		t.Errorf("Expected 9 prefixes from fallback, got %d", len(client.PrefixesMap))
	}
	if client.PrefixesMap["K"].Entity != "United States" {
		t.Errorf("Fallback data not loaded correctly.")
	}
}

func TestGetDxccInfo_Basic(t *testing.T) {
	cfg := &config.Config{DXCCUpdateInterval: 1 * time.Minute}
	client, cleanup := setupTestClient(t, cfg)
	defer cleanup()
	client.LoadMapsFromDB(context.Background()) // Load the test data if not already (should be empty from setup)

	// Manually populate maps for this test
	client.ExceptionsMap = map[string]dxcc.DxccException{
		"LZ0": {Call: "LZ0", Entity: "Antarctica", ADIF: 13, CQZ: 13, Cont: "AN"},
	}
	client.PrefixesMap = map[string]dxcc.DxccPrefix{
		"K":     {Call: "K", Entity: "United States", ADIF: 291, CQZ: 5, Cont: "NA"},
		"DX1":   {Call: "DX1", Entity: "Test DX1 Entity", ADIF: 901, CQZ: 1, Cont: "AS"},
		"3A":    {Call: "3A", Entity: "Monaco", ADIF: 260, CQZ: 14, Cont: "EU"},
		"3D2/R": {Call: "3D2/R", Entity: "Rotuma I.", ADIF: 460, CQZ: 32, Cont: "OC"},
		"KG4":   {Call: "KG4", Entity: "Guantanamo Bay", ADIF: 105, CQZ: 8, Cont: "NA"},
		"OH":    {Call: "OH", Entity: "Finland", ADIF: 224, CQZ: 15, Cont: "EU"},
		"CX":    {Call: "CX", Entity: "Uruguay", ADIF: 144, CQZ: 13, Cont: "SA"},
		"LZ":    {Call: "LZ", Entity: "Bulgaria", ADIF: 212, CQZ: 20, Cont: "EU"},
	}
	client.EntitiesMap = map[int]dxcc.DxccEntity{
		0:   {ADIF: 0, Name: "- NONE -", ITUZ: 0, CQZ: 0, Cont: "", Lat: 0.0, Long: 0.0},
		291: {ADIF: 291, Name: "United States", Prefix: "K", ITUZ: 8, CQZ: 5, Cont: "NA", Lat: 39.0, Long: -98.0},
		901: {ADIF: 901, Name: "Test DX1 Entity", Prefix: "DX1", ITUZ: 1, CQZ: 1, Cont: "AS", Lat: 1.0, Long: 1.0},
		260: {ADIF: 260, Name: "Monaco", Prefix: "3A", ITUZ: 27, CQZ: 14, Cont: "EU", Lat: 43.7, Long: 7.4},
		13:  {ADIF: 13, Name: "Antarctica", Prefix: "LA/LH", ITUZ: 72, CQZ: 12, Cont: "AN", Lat: -90.0, Long: 0.0},
		460: {ADIF: 460, Name: "Rotuma I.", Prefix: "3D2/R", ITUZ: 56, CQZ: 32, Cont: "OC", Lat: -12.5, Long: 177.0},
		105: {ADIF: 105, Name: "Guantanamo Bay", Prefix: "KG4", ITUZ: 11, CQZ: 8, Cont: "NA", Lat: 19.9, Long: -75.1},
		224: {ADIF: 224, Name: "Finland", Prefix: "OH", ITUZ: 18, CQZ: 15, Cont: "EU", Lat: 64.0, Long: 25.0},
		144: {ADIF: 144, Name: "Uruguay", Prefix: "CX", ITUZ: 14, CQZ: 13, Cont: "SA", Lat: -33.0, Long: -56.0},
		212: {ADIF: 212, Name: "Bulgaria", Prefix: "LZ", ITUZ: 28, CQZ: 20, Cont: "EU", Lat: 42.6, Long: 25.0},
	}

	tests := []struct {
		callsign string
		expected *dxcc.DxccInfo
		err      bool
	}{
		{
			callsign: "K6ABC",
			expected: &dxcc.DxccInfo{
				Cont: "NA", Entity: "United States", Flag: "\U0001F1FA\U0001F1F8", DXCCID: 291, CQZ: 5, ITUZ: 8, Latitude: 39.0, Longitude: -98.0,
			},
		},
		{
			callsign: "W1AW", // Should also resolve to K
			expected: &dxcc.DxccInfo{
				Cont: "NA", Entity: "United States", Flag: "\U0001F1FA\U0001F1F8", DXCCID: 291, CQZ: 5, ITUZ: 8, Latitude: 39.0, Longitude: -98.0,
			},
		},
		{
			callsign: "DX1ABC",
			expected: &dxcc.DxccInfo{
				Cont: "AS", Entity: "Test Dx1 Entity", Flag: "", DXCCID: 901, CQZ: 1, ITUZ: 1, Latitude: 1.0, Longitude: 1.0,
			},
		},
		{
			callsign: "3A2ABC",
			expected: &dxcc.DxccInfo{
				Cont: "EU", Entity: "Monaco", Flag: "\U0001F1F2\U0001F1E8", DXCCID: 260, CQZ: 14, ITUZ: 27, Latitude: 43.7, Longitude: 7.4,
			},
		},
		{
			callsign: "MM/G0AAA", // Mobile Marine, should resolve to -NONE- via wpx logic
			expected: &dxcc.DxccInfo{
				Cont: "", Entity: "- None - (e.g. /MM, /AM)", Flag: "", DXCCID: 0, CQZ: 0, ITUZ: 0, Latitude: 0.0, Longitude: 0.0,
			},
		},
		{
			callsign: "3D2R/VK2ABC", // Specific exception handling should trigger 3D2/R from PHP original logic
			expected: &dxcc.DxccInfo{
				Cont: "OC", Entity: "Rotuma I.", Flag: "", DXCCID: 460, CQZ: 32, ITUZ: 56, Latitude: -12.5, Longitude: 177.0,
			},
		},
		{
			callsign: "CX/VE3AAA", // CX/ as non-Antarctica is Uruguay (no exception match from specific PHP rule)
			expected: &dxcc.DxccInfo{
				Cont: "SA", Entity: "Uruguay", Flag: "\U0001F1FA\U0001F1FE", DXCCID: 144, CQZ: 13, ITUZ: 14, Latitude: -33.0, Longitude: -56.0,
			},
		},
		{
			callsign: "TF/DL2NWK/MM", // Explicitly handled in wpx (A/B/MM)
			expected: &dxcc.DxccInfo{
				Cont: "", Entity: "- None - (e.g. /MM, /AM)", Flag: "", DXCCID: 0, CQZ: 0, ITUZ: 0, Latitude: 0.0, Longitude: 0.0,
			},
		},
		{
			callsign: "LZ/VE3AAA", // PHP's original logic would find LZ0 exception. Our Go wpx needs to correctly identify 'LZ' as prefix.
			expected: &dxcc.DxccInfo{ // Should resolve to Bulgaria (LZ), not Antarctica (LZ0 exception) if `LZ` itself is a prefix
				Cont: "EU", Entity: "Bulgaria", Flag: "\U0001F1E7\U0001F1EC", DXCCID: 212, CQZ: 20, ITUZ: 28, Latitude: 42.6, Longitude: 25.0,
			},
		},
		{
			callsign: "INVALID",
			expected: &dxcc.DxccInfo{
				Cont: "", Entity: "- None - (e.g. /MM, /AM)", Flag: "", DXCCID: 0, CQZ: 0, ITUZ: 0, Latitude: 0.0, Longitude: 0.0,
			},
		},
		{
			callsign: "K/QRP", // QRP is a lid addition
			expected: &dxcc.DxccInfo{
				Cont: "NA", Entity: "United States", Flag: "\U0001F1FA\U0001F1F8", DXCCID: 291, CQZ: 5, ITUZ: 8, Latitude: 39.0, Longitude: -98.0,
			},
		},
		{
			callsign: "KG4AAA", // KG4 with 3 chars = Guantanamo Bay
			expected: &dxcc.DxccInfo{
				Cont: "NA", Entity: "Guantanamo Bay", Flag: "", DXCCID: 105, CQZ: 8, ITUZ: 11, Latitude: 19.9, Longitude: -75.1,
			},
		},
		{
			callsign: "OH/N4AAA", // OH/ is non-Aland, so Finland
			expected: &dxcc.DxccInfo{
				Cont: "EU", Entity: "Finland", Flag: "\U0001F1EB\U0001F1EE", DXCCID: 224, CQZ: 15, ITUZ: 18, Latitude: 64.0, Longitude: 25.0,
			},
		},
		{
			callsign: "LZ0XXX", // Should resolve to Antarctica via exception
			expected: &dxcc.DxccInfo{
				Cont: "AN", Entity: "Antarctica", Flag: "\U0001F1E6\U0001F1F6", DXCCID: 13, CQZ: 12, ITUZ: 72, Latitude: -90.0, Longitude: 0.0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.callsign, func(t *testing.T) {
			info, err := client.GetDxccInfo(context.Background(), tt.callsign, nil)
			if (err != nil) != tt.err {
				t.Fatalf("GetDxccInfo(%q) error status mismatch. Expected error: %t, got: %v", tt.callsign, tt.err, err)
			}
			if !tt.err {
				if info == nil {
					t.Fatalf("GetDxccInfo(%q) returned nil info, expected %+v", tt.callsign, tt.expected)
				}
				// Compare individual fields to avoid issues with float/nil pointers
				if info.Cont != tt.expected.Cont {
					t.Errorf("Cont mismatch for %s. Expected %s, got %s", tt.callsign, tt.expected.Cont, info.Cont)
				}
				if info.Entity != tt.expected.Entity {
					t.Errorf("Entity mismatch for %s. Expected %s, got %s", tt.callsign, tt.expected.Entity, info.Entity)
				}
				if info.Flag != tt.expected.Flag {
					t.Errorf("Flag mismatch for %s. Expected %s, got %s", tt.callsign, tt.expected.Flag, info.Flag)
				}
				if info.DXCCID != tt.expected.DXCCID {
					t.Errorf("DXCCID mismatch for %s. Expected %d, got %d", tt.callsign, tt.expected.DXCCID, info.DXCCID)
				}
				if info.CQZ != tt.expected.CQZ {
					t.Errorf("CQZ mismatch for %s. Expected %d, got %d", tt.callsign, tt.expected.CQZ, info.CQZ)
				}
				if info.ITUZ != tt.expected.ITUZ {
					t.Errorf("ITUZ mismatch for %s. Expected %d, got %d", tt.callsign, tt.expected.ITUZ, info.ITUZ)
				}
				if info.Latitude != tt.expected.Latitude {
					t.Errorf("Latitude mismatch for %s. Expected %f, got %f", tt.callsign, tt.expected.Latitude, info.Latitude)
				}
				if info.Longitude != tt.expected.Longitude {
					t.Errorf("Longitude mismatch for %s. Expected %f, got %f", tt.callsign, tt.expected.Longitude, info.Longitude)
				}
			}
		})
	}
}

func TestGetDxccInfo_DateValidity(t *testing.T) {
	cfg := &config.Config{DXCCUpdateInterval: 1 * time.Minute}
	client, cleanup := setupTestClient(t, cfg)
	defer cleanup()

	// Manually populate maps for this test
	client.PrefixesMap = map[string]dxcc.DxccPrefix{
		"3A": {
			Call: "3A", Entity: "Monaco", ADIF: 260, CQZ: 14, Cont: "EU",
			Start: time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2010, time.December, 31, 23, 59, 59, 0, time.UTC),
		},
	}
	client.ExceptionsMap = map[string]dxcc.DxccException{}
	client.EntitiesMap = map[int]dxcc.DxccEntity{
		0:   {ADIF: 0, Name: "- NONE -", ITUZ: 0, CQZ: 0, Cont: "", Lat: 0.0, Long: 0.0},
		260: {ADIF: 260, Name: "Monaco", Prefix: "3A", ITUZ: 27, CQZ: 14, Cont: "EU", Lat: 43.7, Long: 7.4},
	}

	tests := []struct {
		callsign    string
		lookupDate  time.Time
		expectedID  int
		description string
	}{
		{
			callsign:    "3A1ABC",
			lookupDate:  time.Date(2005, time.June, 15, 12, 0, 0, 0, time.UTC),
			expectedID:  260, // Within valid period
			description: "Within valid period",
		},
		{
			callsign:    "3A1ABC",
			lookupDate:  time.Date(1999, time.December, 31, 23, 59, 59, 0, time.UTC),
			expectedID:  0, // Before start date
			description: "Before start date",
		},
		{
			callsign:    "3A1ABC",
			lookupDate:  time.Date(2031, time.January, 1, 0, 0, 0, 0, time.UTC),
			expectedID:  0, // After end date
			description: "After end date",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			info, err := client.GetDxccInfo(context.Background(), tt.callsign, &tt.lookupDate)
			if err != nil {
				t.Fatalf("GetDxccInfo failed: %v", err)
			}
			if info == nil {
				t.Fatalf("GetDxccInfo returned nil info")
			}
			if info.DXCCID != tt.expectedID {
				t.Errorf("For callsign %s and date %s, expected DXCCID %d, got %d", tt.callsign, tt.lookupDate.Format(time.RFC3339), tt.expectedID, info.DXCCID)
			}
		})
	}
}

func TestToUcWord(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"united states", "United States"},
		{"MONACO", "Monaco"},
		{"test dx1 entity", "Test Dx1 Entity"},
		{"a single word", "A Single Word"},
		{"  leading and trailing spaces  ", "Leading And Trailing Spaces"},
		{"multiple   spaces", "Multiple Spaces"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := dxcc.ToUcWord(tt.input)
			if result != tt.expected {
				t.Errorf("ToUcWord(%q) expected %q, got %q", tt.input, tt.expected, result)
			}
		})
	}
}

// Use the exported accessors on the dxcc.Client (SetHTTPDoer, GetHTTPClient, LoadMapsFromDB, FetchAndStoreData, GetPrefixes, GetExceptions, GetEntities) provided by the package.
