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
