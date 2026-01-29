package wallet

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

// Wallet represents a user's identity in the P2P network
type Wallet struct {
	PeerID  peer.ID        `json:"peer_id"`
	PrivKey crypto.PrivKey `json:"-"`
	PubKey  crypto.PubKey  `json:"-"`
}

// WalletData is used for JSON serialization
type WalletData struct {
	PeerID     string `json:"peer_id"`
	PrivKeyHex string `json:"private_key"`
}

// WalletService provides wallet management operations
type WalletService interface {
	Create() (*Wallet, error)
	Load(path string) (*Wallet, error)
	Save(w *Wallet, path string) error
	GetPeerID() peer.ID
	GetPrivKey() crypto.PrivKey
}

type walletService struct {
	wallet *Wallet
}

// NewWalletService creates a new wallet service
func NewWalletService() WalletService {
	return &walletService{}
}

// Create generates a new wallet with Ed25519 keypair
func (s *walletService) Create() (*Wallet, error) {
	// Generate Ed25519 keypair
	privKey, pubKey, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate keypair: %w", err)
	}

	// Derive peer ID from public key
	peerID, err := peer.IDFromPublicKey(pubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive peer ID: %w", err)
	}

	s.wallet = &Wallet{
		PeerID:  peerID,
		PrivKey: privKey,
		PubKey:  pubKey,
	}

	return s.wallet, nil
}

// Load loads a wallet from a JSON file
func (s *walletService) Load(path string) (*Wallet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read wallet file: %w", err)
	}

	var walletData WalletData
	if err := json.Unmarshal(data, &walletData); err != nil {
		return nil, fmt.Errorf("failed to parse wallet data: %w", err)
	}

	// Decode private key
	privKeyBytes, err := crypto.ConfigDecodeKey(walletData.PrivKeyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to decode private key: %w", err)
	}

	privKey, err := crypto.UnmarshalPrivateKey(privKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal private key: %w", err)
	}

	pubKey := privKey.GetPublic()
	peerID, err := peer.IDFromPublicKey(pubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive peer ID: %w", err)
	}

	s.wallet = &Wallet{
		PeerID:  peerID,
		PrivKey: privKey,
		PubKey:  pubKey,
	}

	return s.wallet, nil
}

// Save saves the wallet to a JSON file
func (s *walletService) Save(w *Wallet, path string) error {
	if w == nil {
		return fmt.Errorf("wallet is nil")
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Marshal private key
	privKeyBytes, err := crypto.MarshalPrivateKey(w.PrivKey)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}

	walletData := WalletData{
		PeerID:     w.PeerID.String(),
		PrivKeyHex: crypto.ConfigEncodeKey(privKeyBytes),
	}

	data, err := json.MarshalIndent(walletData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal wallet data: %w", err)
	}

	// Write with restricted permissions
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write wallet file: %w", err)
	}

	return nil
}

// GetPeerID returns the peer ID of the loaded wallet
func (s *walletService) GetPeerID() peer.ID {
	if s.wallet == nil {
		return ""
	}
	return s.wallet.PeerID
}

// GetPrivKey returns the private key of the loaded wallet
func (s *walletService) GetPrivKey() crypto.PrivKey {
	if s.wallet == nil {
		return nil
	}
	return s.wallet.PrivKey
}
