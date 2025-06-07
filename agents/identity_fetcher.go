// agents/identity_fetcher.go - Fixed agent
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type FetcherConfig struct {
	RPCURL          string `json:"rpc_url"`
	RPCKey          string `json:"rpc_key"`
	OutputFile      string `json:"output_file"`
	AddressListFile string `json:"address_list_file"`
	BatchSize       int    `json:"batch_size"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
}

type RPCRequest struct {
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
	ID     int           `json:"id"`
}

type RPCResponse struct {
	Result *IdentityInfo `json:"result"`
	Error  *RPCError     `json:"error"`
	ID     int           `json:"id"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type IdentityInfo struct {
	Address string  `json:"address"`
	State   string  `json:"state"`
	Stake   float64 `json:"stake"`
}

type Snapshot struct {
	Timestamp  time.Time       `json:"timestamp"`
	Identities []IdentityInfo  `json:"identities"`
	Total      int             `json:"total"`
	Successful int             `json:"successful"`
	Failed     []string        `json:"failed"`
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: go run identity_fetcher.go <config_file>")
	}

	configFile := os.Args[1]
	config, err := loadConfig(configFile)
	if err != nil {
		log.Fatalf("Erreur de chargement de config: %v", err)
	}

	addresses, err := loadAddresses(config.AddressListFile)
	if err != nil {
		log.Fatalf("Error loading addresses: %v", err)
	}

	log.Printf("Fetching information for %d addresses...", len(addresses))

	fetcher := NewIdentityFetcher(config)
	snapshot := fetcher.FetchIdentities(addresses)

	if err := saveSnapshot(snapshot, config.OutputFile); err != nil {
		log.Fatalf("Error saving snapshot: %v", err)
	}

	log.Printf("Completed! %d/%d identities fetched successfully", 
		snapshot.Successful, snapshot.Total)
	
	if len(snapshot.Failed) > 0 {
		log.Printf("Failed addresses: %v", snapshot.Failed)
	}
}

func loadConfig(filename string) (*FetcherConfig, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var config FetcherConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	// Default values
	if config.BatchSize == 0 {
		config.BatchSize = 100
	}
	if config.TimeoutSeconds == 0 {
		config.TimeoutSeconds = 30
	}
	if config.OutputFile == "" {
		config.OutputFile = "snapshot.json"
	}

	return &config, nil
}

func loadAddresses(filename string) ([]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var addresses []string
	scanner := bufio.NewScanner(file)
	
	for scanner.Scan() {
		address := strings.TrimSpace(scanner.Text())
		if address != "" && !strings.HasPrefix(address, "#") {
			addresses = append(addresses, address)
		}
	}

	return addresses, scanner.Err()
}

type IdentityFetcher struct {
	config *FetcherConfig
	client *http.Client
}

func NewIdentityFetcher(config *FetcherConfig) *IdentityFetcher {
	return &IdentityFetcher{
		config: config,
		client: &http.Client{
			Timeout: time.Duration(config.TimeoutSeconds) * time.Second,
		},
	}
}

func (f *IdentityFetcher) FetchIdentities(addresses []string) *Snapshot {
	snapshot := &Snapshot{
		Timestamp:  time.Now(),
		Identities: make([]IdentityInfo, 0),
		Total:      len(addresses),
		Failed:     make([]string, 0),
	}

	// Process in batches to avoid server overload
	for i := 0; i < len(addresses); i += f.config.BatchSize {
		end := i + f.config.BatchSize
		if end > len(addresses) {
			end = len(addresses)
		}

		batch := addresses[i:end]
		log.Printf("Processing batch %d-%d/%d", i+1, end, len(addresses))

		for _, address := range batch {
			identity, err := f.fetchIdentity(address)
			if err != nil {
				log.Printf("Error for %s: %v", address, err)
				snapshot.Failed = append(snapshot.Failed, address)
				continue
			}

			snapshot.Identities = append(snapshot.Identities, *identity)
			snapshot.Successful++
		}

		// Small pause between batches
		if end < len(addresses) {
			time.Sleep(100 * time.Millisecond)
		}
	}

	return snapshot
}

func (f *IdentityFetcher) fetchIdentity(address string) (*IdentityInfo, error) {
	request := RPCRequest{
		Method: "dna_identity",
		Params: []interface{}{address},
		ID:     1,
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", f.config.RPCURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if f.config.RPCKey != "" {
		req.Header.Set("Authorization", "Bearer "+f.config.RPCKey)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rpcResponse RPCResponse
	if err := json.Unmarshal(body, &rpcResponse); err != nil {
		return nil, err
	}

	if rpcResponse.Error != nil {
		return nil, fmt.Errorf("RPC error: %s", rpcResponse.Error.Message)
	}

	if rpcResponse.Result == nil {
		return nil, fmt.Errorf("no result for address %s", address)
	}

	// Ensure address is set
	rpcResponse.Result.Address = address

	return rpcResponse.Result, nil
}

func saveSnapshot(snapshot *Snapshot, filename string) error {
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}

	return ioutil.WriteFile(filename, data, 0644)
}

// ============================================================================
// main_test.go - Unit tests
// ============================================================================

/*
package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestMain(m *testing.M) {
	// Setup
	os.Setenv("BASE_URL", "http://localhost:3030")
	os.Setenv("IDENA_RPC_KEY", "test_key")
	
	// Run tests
	code := m.Run()
	
	// Teardown
	os.Remove("test_identities.db")
	
	os.Exit(code)
}

func setupTestDB() (*sql.DB, error) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		return nil, err
	}

	createTables := `
	CREATE TABLE identities (
		address TEXT PRIMARY KEY,
		state TEXT NOT NULL,
		stake REAL NOT NULL,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`

	_, err = db.Exec(createTables)
	return db, err
}

func insertTestData(db *sql.DB) error {
	testData := []struct {
		address string
		state   string
		stake   float64
	}{
		{"0x1234567890abcdef1234567890abcdef12345678", "Human", 15000},
		{"0xabcdef1234567890abcdef1234567890abcdef12", "Verified", 25000},
		{"0x9876543210fedcba9876543210fedcba98765432", "Newbie", 5000},
		{"0xfedcba0987654321fedcba0987654321fedcba09", "Candidate", 12000},
	}

	for _, data := range testData {
		_, err := db.Exec(
			"INSERT INTO identities (address, state, stake) VALUES (?, ?, ?)",
			data.address, data.state, data.stake,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

func TestCheckEligibility(t *testing.T) {
	db, err := setupTestDB()
	if err != nil {
		t.Fatalf("DB setup error: %v", err)
	}
	defer db.Close()

	if err := insertTestData(db); err != nil {
		t.Fatalf("Data insertion error: %v", err)
	}

	server := &Server{db: db}

	tests := []struct {
		address  string
		eligible bool
		reason   string
	}{
		{
			address:  "0x1234567890abcdef1234567890abcdef12345678",
			eligible: true,
			reason:   "Eligible",
		},
		{
			address:  "0x9876543210fedcba9876543210fedcba98765432",
			eligible: false,
			reason:   "Insufficient stake: 5000.00 iDNA (minimum 10,000)",
		},
		{
			address:  "0xfedcba0987654321fedcba0987654321fedcba09",
			eligible: false,
			reason:   "Ineligible state: Candidate",
		},
		{
			address:  "0xinexistant",
			eligible: false,
			reason:   "Address not found in database",
		},
	}

	for _, test := range tests {
		t.Run(test.address, func(t *testing.T) {
			eligible, reason := server.checkEligibility(test.address)
			
			if eligible != test.eligible {
				t.Errorf("Expected eligible=%v, got=%v", test.eligible, eligible)
			}
			
			if reason != test.reason {
				t.Errorf("Expected reason=%q, got=%q", test.reason, reason)
			}
		})
	}
}

func TestWhitelistEndpoint(t *testing.T) {
	db, err := setupTestDB()
	if err != nil {
		t.Fatalf("Erreur de setup DB: %v", err)
	}
	defer db.Close()

	if err := insertTestData(db); err != nil {
		t.Fatalf("Erreur d'insertion de données: %v", err)
	}

	server := &Server{db: db}

	req, err := http.NewRequest("GET", "/whitelist", nil)
	if err != nil {
		t.Fatalf("Erreur de création de requête: %v", err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.handleWhitelist)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Wrong status code: got %v, expected %v", status, http.StatusOK)
	}

	var response WhitelistResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Response parsing error: %v", err)
	}

	// Should have 2 eligible addresses (Human with 15000 and Verified with 25000)
	expectedCount := 2
	if response.Count != expectedCount {
		t.Errorf("Expected count=%d, got=%d", expectedCount, response.Count)
	}

	if len(response.Addresses) != expectedCount {
		t.Errorf("Expected %d addresses, got %d", expectedCount, len(response.Addresses))
	}, len(response.Addresses))
	}
}

func TestWhitelistCheckEndpoint(t *testing.T) {
	db, err := setupTestDB()
	if err != nil {
		t.Fatalf("DB setup error: %v", err)
	}
	defer db.Close()

	if err := insertTestData(db); err != nil {
		t.Fatalf("Data insertion error: %v", err)
	}

	server := &Server{db: db}

	// Test with eligible address
	req, err := http.NewRequest("GET", "/whitelist/check?address=0x1234567890abcdef1234567890abcdef12345678", nil)
	if err != nil {
		t.Fatalf("Request creation error: %v", err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.handleWhitelistCheck)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Wrong status code: got %v, expected %v", status, http.StatusOK)
	}

	var response EligibilityCheck
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Response parsing error: %v", err)
	}

	if !response.Eligible {
		t.Errorf("Address should be eligible")
	}
}

func TestMerkleRootEndpoint(t *testing.T) {
	db, err := setupTestDB()
	if err != nil {
		t.Fatalf("Erreur de setup DB: %v", err)
	}
	defer db.Close()

	if err := insertTestData(db); err != nil {
		t.Fatalf("Erreur d'insertion de données: %v", err)
	}

	server := &Server{db: db}

	req, err := http.NewRequest("GET", "/merkle_root", nil)
	if err != nil {
		t.Fatalf("Erreur de création de requête: %v", err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.handleMerkleRoot)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Mauvais status code: obtenu %v, attendu %v", status, http.StatusOK)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Response parsing error: %v", err)
	}

	if response["merkle_root"] == nil {
		t.Error("merkle_root missing in response")
	}

	if response["addresses_count"] == nil {
		t.Error("addresses_count missing in response")
	}
}

func TestHealthEndpoint(t *testing.T) {
	db, err := setupTestDB()
	if err != nil {
		t.Fatalf("Erreur de setup DB: %v", err)
	}
	defer db.Close()

	server := &Server{db: db}

	req, err := http.NewRequest("GET", "/health", nil)
	if err != nil {
		t.Fatalf("Erreur de création de requête: %v", err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.handleHealth)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Wrong status code: got %v, expected %v", status, http.StatusOK)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Response parsing error: %v", err)
	}

	if response["status"] != "healthy" {
		t.Errorf("Expected status=healthy, got=%v", response["status"])
	}
}

// Benchmark for performance
func BenchmarkCheckEligibility(b *testing.B) {
	db, err := setupTestDB()
	if err != nil {
		b.Fatalf("DB setup error: %v", err)
	}
	defer db.Close()

	if err := insertTestData(db); err != nil {
		b.Fatalf("Data insertion error: %v", err)
	}

	server := &Server{db: db}
	address := "0x1234567890abcdef1234567890abcdef12345678"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		server.checkEligibility(address)
	}
}
*/

// ============================================================================
// schema.sql - Database schema
// ============================================================================

/*
-- Database schema creation for SQLite

CREATE TABLE IF NOT EXISTS identities (
    address TEXT PRIMARY KEY,
    state TEXT NOT NULL,
    stake REAL NOT NULL,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for performance improvement
CREATE INDEX IF NOT EXISTS idx_state ON identities(state);
CREATE INDEX IF NOT EXISTS idx_stake ON identities(stake);
CREATE INDEX IF NOT EXISTS idx_eligible ON identities(state, stake);
CREATE INDEX IF NOT EXISTS idx_timestamp ON identities(timestamp);
CREATE INDEX IF NOT EXISTS idx_updated_at ON identities(updated_at);

-- View for eligible identities
CREATE VIEW IF NOT EXISTS eligible_identities AS
SELECT address, state, stake, updated_at
FROM identities 
WHERE state IN ('Human', 'Verified', 'Newbie') 
  AND stake >= 10000;

-- Trigger to automatically update updated_at
CREATE TRIGGER IF NOT EXISTS update_timestamp 
    AFTER UPDATE ON identities
BEGIN
    UPDATE identities SET updated_at = CURRENT_TIMESTAMP 
    WHERE address = NEW.address;
END;
*/
