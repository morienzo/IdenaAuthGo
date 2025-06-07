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