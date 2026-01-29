package cloud

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/libp2p/go-libp2p/core/peer"

	"p2papp/internal/core"
)

// FileHash represents a content-addressed file hash
type FileHash string

// FileInfo contains metadata about a stored file
type FileInfo struct {
	Hash       FileHash `json:"hash"`
	Name       string   `json:"name"`
	Size       int64    `json:"size"`
	ChunkCount int      `json:"chunk_count"`
	LocalPath  string   `json:"local_path,omitempty"`
}

// CloudService provides cloud storage operations
type CloudService interface {
	Upload(filePath string) (*FileInfo, error)
	Download(hash FileHash, destPath string) error
	List() ([]FileInfo, error)
	Share(hash FileHash, peerID peer.ID) error
	Delete(hash FileHash) error
	GetInfo(hash FileHash) (*FileInfo, error)
	Close() error
}

type cloudService struct {
	host     *core.P2PHost
	storage  *FileStorage
	transfer *FileTransfer
	index    *FileIndex
	cloudDir string
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewCloudService creates a new cloud storage service
func NewCloudService(ctx context.Context, host *core.P2PHost, cloudDir string) (CloudService, error) {
	// Ensure cloud directory exists
	if err := os.MkdirAll(cloudDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cloud directory: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)

	storage := NewFileStorage(cloudDir)
	index := NewFileIndex(host)
	transfer := NewFileTransfer(host, storage)

	// Register transfer protocol handler
	transfer.RegisterHandler()

	// Start DHT advertising for local files
	go index.AdvertiseLoop(ctx)

	return &cloudService{
		host:     host,
		storage:  storage,
		transfer: transfer,
		index:    index,
		cloudDir: cloudDir,
		ctx:      ctx,
		cancel:   cancel,
	}, nil
}

// Upload chunks and stores a file, returning its hash
func (s *cloudService) Upload(filePath string) (*FileInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Store the file
	info, err := s.storage.Store(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to store file: %w", err)
	}

	// Advertise file to DHT
	if err := s.index.Advertise(s.ctx, info.Hash); err != nil {
		fmt.Printf("Warning: failed to advertise file to DHT: %v\n", err)
	}

	return info, nil
}

// Download retrieves a file by its hash
func (s *cloudService) Download(hash FileHash, destPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if file exists locally
	if s.storage.Exists(hash) {
		return s.storage.Retrieve(hash, destPath)
	}

	// Find providers in DHT
	providers, err := s.index.FindProviders(s.ctx, hash)
	if err != nil {
		return fmt.Errorf("failed to find providers: %w", err)
	}

	if len(providers) == 0 {
		return fmt.Errorf("no providers found for hash: %s", hash)
	}

	// Try to download from a provider
	for _, provider := range providers {
		if provider == s.host.ID() {
			continue // Skip self
		}

		err := s.transfer.Download(s.ctx, provider, hash, destPath)
		if err == nil {
			return nil
		}
		fmt.Printf("Failed to download from %s: %v\n", provider.String()[:8], err)
	}

	return fmt.Errorf("failed to download file from any provider")
}

// List returns all locally stored files
func (s *cloudService) List() ([]FileInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.storage.List()
}

// Share makes a file available to a specific peer
func (s *cloudService) Share(hash FileHash, peerID peer.ID) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.storage.Exists(hash) {
		return fmt.Errorf("file not found: %s", hash)
	}

	// Notify peer about the file
	return s.transfer.NotifyPeer(s.ctx, peerID, hash)
}

// Delete removes a file from local storage
func (s *cloudService) Delete(hash FileHash) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.storage.Delete(hash)
}

// GetInfo returns information about a stored file
func (s *cloudService) GetInfo(hash FileHash) (*FileInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.storage.GetInfo(hash)
}

// Close shuts down the cloud service
func (s *cloudService) Close() error {
	s.cancel()
	s.transfer.Close()
	return nil
}

// GetCloudDir returns the cloud storage directory
func GetCloudDir(dataDir string) string {
	return filepath.Join(dataDir, "cloud")
}
