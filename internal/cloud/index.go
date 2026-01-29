package cloud

import (
	"context"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"

	"p2papp/internal/core"
)

const (
	ProviderPrefix    = "/p2papp/file/"
	AdvertiseInterval = 10 * time.Minute
)

// FileIndex handles DHT-based file indexing
type FileIndex struct {
	host           *core.P2PHost
	advertisedHash map[FileHash]bool
}

// NewFileIndex creates a new file index
func NewFileIndex(host *core.P2PHost) *FileIndex {
	return &FileIndex{
		host:           host,
		advertisedHash: make(map[FileHash]bool),
	}
}

// Advertise advertises a file to the DHT
func (i *FileIndex) Advertise(ctx context.Context, hash FileHash) error {
	if i.host.DHT == nil {
		return fmt.Errorf("DHT not enabled")
	}

	routingDiscovery := drouting.NewRoutingDiscovery(i.host.DHT)
	rendezvous := ProviderPrefix + string(hash)

	_, err := routingDiscovery.Advertise(ctx, rendezvous)
	if err != nil {
		return fmt.Errorf("failed to advertise: %w", err)
	}

	i.advertisedHash[hash] = true
	return nil
}

// FindProviders finds peers that have a file
func (i *FileIndex) FindProviders(ctx context.Context, hash FileHash) ([]peer.ID, error) {
	if i.host.DHT == nil {
		return nil, fmt.Errorf("DHT not enabled")
	}

	routingDiscovery := drouting.NewRoutingDiscovery(i.host.DHT)
	rendezvous := ProviderPrefix + string(hash)

	peerChan, err := routingDiscovery.FindPeers(ctx, rendezvous)
	if err != nil {
		return nil, fmt.Errorf("failed to find peers: %w", err)
	}

	var providers []peer.ID
	timeout := time.After(30 * time.Second)

	for {
		select {
		case peerInfo, ok := <-peerChan:
			if !ok {
				return providers, nil
			}
			if peerInfo.ID == "" || peerInfo.ID == i.host.ID() {
				continue
			}
			providers = append(providers, peerInfo.ID)
		case <-timeout:
			return providers, nil
		case <-ctx.Done():
			return providers, ctx.Err()
		}
	}
}

// AdvertiseLoop periodically re-advertises all local files
func (i *FileIndex) AdvertiseLoop(ctx context.Context) {
	ticker := time.NewTicker(AdvertiseInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			for hash := range i.advertisedHash {
				i.Advertise(ctx, hash)
			}
		case <-ctx.Done():
			return
		}
	}
}

// ProvideFile uses DHT content routing to provide a file
func (i *FileIndex) ProvideFile(ctx context.Context, hash FileHash) error {
	if i.host.DHT == nil {
		return fmt.Errorf("DHT not enabled")
	}

	// Create CID from hash (simplified)
	rendezvous := ProviderPrefix + string(hash)

	routingDiscovery := drouting.NewRoutingDiscovery(i.host.DHT)
	dutil.Advertise(ctx, routingDiscovery, rendezvous)

	i.advertisedHash[hash] = true
	return nil
}

// StopProviding stops providing a file
func (i *FileIndex) StopProviding(hash FileHash) {
	delete(i.advertisedHash, hash)
}
