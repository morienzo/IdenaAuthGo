package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type IndexerConfig struct {
	RPCURL          string `json:"rpc_url"`
	RPCKey          string `json:"rpc_key"`
	IntervalMinutes int    `json:"interval_minutes"`
	DBPath          string `json:"db_path"`
}

type IdenaIdentity struct {
	Address string  `json:"address"`
	State   string  `json:"state"`
	Stake   float64 `json:"stake"`
}

type RPCRequest struct {
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
	ID     int           `json:"id"`
}

type RPCResponse struct {
	Result []IdenaIdentity `json:"result"`
	Error  *RPCError       `json:"error"`
	ID     int             `json:"id"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Indexer struct {
	config IndexerConfig
	db     *sql.DB
}

func main() {
	config := loadConfig()
	
	indexer, err := NewIndexer(config)
	if err != nil {
		log.Fatalf("Erreur de création de l'indexeur: %v", err)
	}
	defer indexer.Close()

	log.Println("Indexeur démarré...")
	indexer.Run()
}

func loadConfig() IndexerConfig {
	// Priorité aux variables d'environnement
	config := IndexerConfig{
		RPCURL:          getEnv("RPC_URL", "http://localhost:9009"),
		RPCKey:          getEnv("RPC_KEY", ""),
		IntervalMinutes: getEnvInt("FETCH_INTERVAL_MINUTES", 10),
		DBPath:          getEnv("DB_PATH", "identities.db"),
	}

	// Essayer de charger config.json si il existe
	if configFile, err := ioutil.ReadFile("config.json"); err == nil {
		var fileConfig IndexerConfig
		if json.Unmarshal(configFile, &fileConfig) == nil {
			// Les variables d'environnement ont priorité
			if config.RPCURL == "http://localhost:9009" && fileConfig.RPCURL != "" {
				config.RPCURL = fileConfig.RPCURL
			}
			if config.RPCKey == "" && fileConfig.RPCKey != "" {
				config.RPCKey = fileConfig.RPCKey
			}
			if config.IntervalMinutes == 10 && fileConfig.IntervalMinutes != 0 {
				config.IntervalMinutes = fileConfig.IntervalMinutes
			}
			if config.DBPath == "identities.db" && fileConfig.DBPath != "" {
				config.DBPath = fileConfig.DBPath
			}
		}
	}

	return config
}

func NewIndexer(config IndexerConfig) (*Indexer, error) {
	db, err := sql.Open("sqlite3", config.DBPath)
	if err != nil {
		return nil, err
	}

	// Créer les tables
	createTables := `
	CREATE TABLE IF NOT EXISTS identities (
		address TEXT PRIMARY KEY,
		state TEXT NOT NULL,
		stake REAL NOT NULL,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_state ON identities(state);
	CREATE INDEX IF NOT EXISTS idx_eligible ON identities(state, stake);
	`

	if _, err := db.Exec(createTables); err != nil {
		return nil, err
	}

	indexer := &Indexer{
		config: config,
		db:     db,
	}

	// Démarrer le serveur HTTP
	go indexer.startHTTPServer()

	return indexer, nil
}

func (i *Indexer) Run() {
	ticker := time.NewTicker(time.Duration(i.config.IntervalMinutes) * time.Minute)
	defer ticker.Stop()

	// Premier fetch immédiat
	i.fetchIdentities()

	// Puis fetch périodique
	for range ticker.C {
		i.fetchIdentities()
	}
}

func (i *Indexer) fetchIdentities() {
	log.Println("Récupération des identités...")

	// Préparer la requête RPC
	request := RPCRequest{
		Method: "dna_identities",
		Params: []interface{}{},
		ID:     1,
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		log.Printf("Erreur de sérialisation: %v", err)
		return
	}

	// Créer la requête HTTP
	req, err := http.NewRequest("POST", i.config.RPCURL, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Erreur de création de requête: %v", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	if i.config.RPCKey != "" {
		req.Header.Set("Authorization", "Bearer "+i.config.RPCKey)
	}

	// Envoyer la requête
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Erreur de requête RPC: %v", err)
		return
	}
	defer resp.Body.Close()

	// Lire la réponse
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Erreur de lecture de réponse: %v", err)
		return
	}

	// Parser la réponse
	var rpcResponse RPCResponse
	if err := json.Unmarshal(body, &rpcResponse); err != nil {
		log.Printf("Erreur de parsing de réponse: %v", err)
		return
	}

	if rpcResponse.Error != nil {
		log.Printf("Erreur RPC: %s", rpcResponse.Error.Message)
		return
	}

	// Mettre à jour la base de données
	i.updateDatabase(rpcResponse.Result)
}

func (i *Indexer) updateDatabase(identities []IdenaIdentity) {
	tx, err := i.db.Begin()
	if err != nil {
		log.Printf("Erreur de transaction: %v", err)
		return
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO identities (address, state, stake, updated_at) 
		VALUES (?, ?, ?, ?)
	`)
	if err != nil {
		log.Printf("Erreur de préparation: %v", err)
		return
	}
	defer stmt.Close()

	updated := 0
	for _, identity := range identities {
		_, err := stmt.Exec(
			identity.Address,
			identity.State,
			identity.Stake,
			time.Now(),
		)
		if err != nil {
			log.Printf("Erreur d'insertion pour %s: %v", identity.Address, err)
			continue
		}
		updated++
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Erreur de commit: %v", err)
		return
	}

	log.Printf("Identités mises à jour: %d/%d", updated, len(identities))
}

func (i *Indexer) startHTTPServer() {
	http.HandleFunc("/identities/latest", i.handleLatestIdentities)
	http.HandleFunc("/identities/eligible", i.handleEligibleIdentities)
	http.HandleFunc("/identity/", i.handleSingleIdentity)
	http.HandleFunc("/state/", i.handleStateFilter)

	log.Println("Serveur HTTP démarré sur :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func (i *Indexer) handleLatestIdentities(w http.ResponseWriter, r *http.Request) {
	rows, err := i.db.Query("SELECT address, state, stake, updated_at FROM identities ORDER BY updated_at DESC")
	if err != nil {
		http.Error(w, "Erreur de base de données", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var identities []map[string]interface{}
	for rows.Next() {
		var address, state string
		var stake float64
		var updatedAt time.Time

		if err := rows.Scan(&address, &state, &stake, &updatedAt); err != nil {
			continue
		}

		identities = append(identities, map[string]interface{}{
			"address":    address,
			"state":      state,
			"stake":      stake,
			"updated_at": updatedAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(identities)
}

func (i *Indexer) handleEligibleIdentities(w http.ResponseWriter, r *http.Request) {
	rows, err := i.db.Query(`
		SELECT address, state, stake FROM identities 
		WHERE state IN ('Human', 'Verified', 'Newbie') AND stake >= 10000
		ORDER BY address
	`)
	if err != nil {
		http.Error(w, "Erreur de base de données", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var addresses []string
	for rows.Next() {
		var address, state string
		var stake float64

		if err := rows.Scan(&address, &state, &stake); err != nil {
			continue
		}
		addresses = append(addresses, address)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"addresses": addresses,
		"count":     len(addresses),
	})
}

func (i *Indexer) handleSingleIdentity(w http.ResponseWriter, r *http.Request) {
	address := strings.TrimPrefix(r.URL.Path, "/identity/")
	if address == "" {
		http.Error(w, "Adresse manquante", http.StatusBadRequest)
		return
	}

	var state string
	var stake float64
	var updatedAt time.Time

	err := i.db.QueryRow(
		"SELECT state, stake, updated_at FROM identities WHERE address = ?",
		address,
	).Scan(&state, &stake, &updatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Identité non trouvée", http.StatusNotFound)
		} else {
			http.Error(w, "Erreur de base de données", http.StatusInternalServerError)
		}
		return
	}

	response := map[string]interface{}{
		"address":    address,
		"state":      state,
		"stake":      stake,
		"updated_at": updatedAt,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (i *Indexer) handleStateFilter(w http.ResponseWriter, r *http.Request) {
	state := strings.TrimPrefix(r.URL.Path, "/state/")
	if state == "" {
		http.Error(w, "État manquant", http.StatusBadRequest)
		return
	}

	rows, err := i.db.Query(
		"SELECT address, stake FROM identities WHERE state = ? ORDER BY address",
		state,
	)
	if err != nil {
		http.Error(w, "Erreur de base de données", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var identities []map[string]interface{}
	for rows.Next() {
		var address string
		var stake float64

		if err := rows.Scan(&address, &stake); err != nil {
			continue
		}

		identities = append(identities, map[string]interface{}{
			"address": address,
			"stake":   stake,
			"state":   state,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(identities)
}

func (i *Indexer) Close() {
	if i.db != nil {
		i.db.Close()
	}
}

func getEnvInt(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	
	intValue, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	
	return intValue
}
