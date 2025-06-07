// main.go - Fixed main backend
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
)

type Config struct {
	BaseURL    string
	IdenaRPCKey string
	Port       string
}

type Identity struct {
	Address   string    `json:"address"`
	State     string    `json:"state"`
	Stake     float64   `json:"stake"`
	Timestamp time.Time `json:"timestamp"`
}

type WhitelistResponse struct {
	Addresses []string `json:"addresses"`
	Count     int      `json:"count"`
}

type EligibilityCheck struct {
	Address  string `json:"address"`
	Eligible bool   `json:"eligible"`
	Reason   string `json:"reason,omitempty"`
}

type Server struct {
	db     *sql.DB
	config Config
}

func main() {
	// Load environment variables
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	config := Config{
		BaseURL:     getEnv("BASE_URL", "http://localhost:3030"),
		IdenaRPCKey: getEnv("IDENA_RPC_KEY", ""),
		Port:        getEnv("PORT", "3030"),
	}

	// Initialize database
	db, err := initDB()
	if err != nil {
		log.Fatalf("Database initialization error: %v", err)
	}
	defer db.Close()

	server := &Server{
		db:     db,
		config: config,
	}

	// Configure routes
	router := mux.NewRouter()
	
	// Authentication routes
	router.HandleFunc("/signin", server.handleSignIn).Methods("GET")
	router.HandleFunc("/callback", server.handleCallback).Methods("GET")
	
	// Whitelist routes
	router.HandleFunc("/whitelist", server.handleWhitelist).Methods("GET")
	router.HandleFunc("/whitelist/check", server.handleWhitelistCheck).Methods("GET")
	
	// Merkle root route (implemented)
	router.HandleFunc("/merkle_root", server.handleMerkleRoot).Methods("GET")
	
	// Status routes
	router.HandleFunc("/health", server.handleHealth).Methods("GET")

	log.Printf("Server started on port %s", config.Port)
	log.Fatal(http.ListenAndServe(":"+config.Port, router))
}

func initDB() (*sql.DB, error) {
	db, err := sql.Open("sqlite3", "./identities.db")
	if err != nil {
		return nil, err
	}

	// Create tables
	createTables := `
	CREATE TABLE IF NOT EXISTS identities (
		address TEXT PRIMARY KEY,
		state TEXT NOT NULL,
		stake REAL NOT NULL,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_state ON identities(state);
	CREATE INDEX IF NOT EXISTS idx_stake ON identities(stake);
	CREATE INDEX IF NOT EXISTS idx_timestamp ON identities(timestamp);
	`

	_, err = db.Exec(createTables)
	return db, err
}

func (s *Server) handleSignIn(w http.ResponseWriter, r *http.Request) {
	// Generate unique session token
	sessionToken := generateSessionToken()
	
	// Build callback URL
	callbackURL := fmt.Sprintf("%s/callback?token=%s", s.config.BaseURL, sessionToken)
	
	// Build Idena deep-link URL
	idenaURL := fmt.Sprintf("idena://signin?callback_url=%s&token=%s", 
		url.QueryEscape(callbackURL), sessionToken)

	response := map[string]string{
		"signin_url": idenaURL,
		"token":      sessionToken,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	address := r.URL.Query().Get("address")
	signature := r.URL.Query().Get("signature")

	if token == "" || address == "" || signature == "" {
		http.Error(w, "Missing parameters", http.StatusBadRequest)
		return
	}

	// Verify signature (simplified for example)
	if !verifySignature(address, token, signature) {
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	// Check eligibility
	eligible, reason := s.checkEligibility(address)

	response := map[string]interface{}{
		"success":  true,
		"address":  address,
		"eligible": eligible,
		"reason":   reason,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleWhitelist(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT address FROM identities 
		WHERE state IN ('Human', 'Verified', 'Newbie') AND stake >= 10000
		ORDER BY address
	`)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var addresses []string
	for rows.Next() {
		var address string
		if err := rows.Scan(&address); err != nil {
			continue
		}
		addresses = append(addresses, address)
	}

	response := WhitelistResponse{
		Addresses: addresses,
		Count:     len(addresses),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleWhitelistCheck(w http.ResponseWriter, r *http.Request) {
	address := r.URL.Query().Get("address")
	if address == "" {
		http.Error(w, "Missing address", http.StatusBadRequest)
		return
	}

	eligible, reason := s.checkEligibility(address)

	response := EligibilityCheck{
		Address:  address,
		Eligible: eligible,
		Reason:   reason,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleMerkleRoot(w http.ResponseWriter, r *http.Request) {
	// Get all eligible addresses
	rows, err := s.db.Query(`
		SELECT address FROM identities 
		WHERE state IN ('Human', 'Verified', 'Newbie') AND stake >= 10000
		ORDER BY address
	`)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var addresses []string
	for rows.Next() {
		var address string
		if err := rows.Scan(&address); err != nil {
			continue
		}
		addresses = append(addresses, address)
	}

	// Calculate merkle root
	merkleRoot := calculateMerkleRoot(addresses)

	response := map[string]interface{}{
		"merkle_root":    merkleRoot,
		"addresses_count": len(addresses),
		"timestamp":      time.Now().Unix(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	// Check database connection
	err := s.db.Ping()
	if err != nil {
		http.Error(w, "Database unavailable", http.StatusServiceUnavailable)
		return
	}

	response := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
		"version":   "1.0.0",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) checkEligibility(address string) (bool, string) {
	var state string
	var stake float64

	err := s.db.QueryRow(
		"SELECT state, stake FROM identities WHERE address = ?", 
		address,
	).Scan(&state, &stake)

	if err != nil {
		if err == sql.ErrNoRows {
			return false, "Address not found in database"
		}
		return false, "Database error"
	}

	// Check eligibility criteria
	validStates := []string{"Human", "Verified", "Newbie"}
	isValidState := false
	for _, validState := range validStates {
		if state == validState {
			isValidState = true
			break
		}
	}

	if !isValidState {
		return false, fmt.Sprintf("Ineligible state: %s", state)
	}

	if stake < 10000 {
		return false, fmt.Sprintf("Insufficient stake: %.2f iDNA (minimum 10,000)", stake)
	}

	return true, "Eligible"
}

// Utility functions
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func generateSessionToken() string {
	return fmt.Sprintf("token_%d", time.Now().UnixNano())
}

func verifySignature(address, token, signature string) bool {
	// Simplified implementation - in production, verify cryptographic signature
	return len(signature) > 0 && len(address) > 0
}

func calculateMerkleRoot(addresses []string) string {
	if len(addresses) == 0 {
		return ""
	}
	
	// Simplified merkle tree implementation
	// In production, use complete implementation with SHA256 hashing
	hash := ""
	for _, addr := range addresses {
		hash += addr
	}
	
	return fmt.Sprintf("merkle_%x", len(hash))
}
