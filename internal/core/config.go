package core

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// AppConfig holds application-wide configuration
type AppConfig struct {
	// Network settings
	ListenPort int    `json:"listen_port"`
	ListenAddr string `json:"listen_addr"`

	// Storage paths
	DataDir    string `json:"data_dir"`
	WalletPath string `json:"wallet_path"`
	CloudDir   string `json:"cloud_dir"`

	// Feature flags
	EnableMDNS bool `json:"enable_mdns"`
	EnableDHT  bool `json:"enable_dht"`

	// Bootstrap peers for DHT
	BootstrapPeers []string `json:"bootstrap_peers"`
}

// DefaultAppConfig returns the default application configuration
func DefaultAppConfig() *AppConfig {
	homeDir, _ := os.UserHomeDir()
	dataDir := filepath.Join(homeDir, ".p2papp")

	return &AppConfig{
		ListenPort:     0, // Random port
		ListenAddr:     "0.0.0.0",
		DataDir:        dataDir,
		WalletPath:     filepath.Join(dataDir, "wallet.json"),
		CloudDir:       filepath.Join(dataDir, "cloud"),
		EnableMDNS:     true,
		EnableDHT:      true,
		BootstrapPeers: []string{},
	}
}

// Load loads configuration from a JSON file
func (c *AppConfig) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Use defaults
		}
		return err
	}
	return json.Unmarshal(data, c)
}

// Save saves configuration to a JSON file
func (c *AppConfig) Save(path string) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// EnsureDirectories creates all necessary directories
func (c *AppConfig) EnsureDirectories() error {
	dirs := []string{
		c.DataDir,
		c.CloudDir,
		filepath.Dir(c.WalletPath),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}
