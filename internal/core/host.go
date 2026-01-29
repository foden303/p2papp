package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
)

// P2PHost wraps a libp2p host with DHT and PubSub
type P2PHost struct {
	Host    host.Host
	DHT     *dht.IpfsDHT
	PubSub  *pubsub.PubSub
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.RWMutex
	started bool
}

// Config holds P2P host configuration
type Config struct {
	ListenAddrs    []string
	PrivKey        crypto.PrivKey
	BootstrapPeers []peer.AddrInfo
	EnableMDNS     bool
	EnableDHT      bool
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		ListenAddrs: []string{
			"/ip4/0.0.0.0/tcp/0",
			"/ip4/0.0.0.0/udp/0/quic-v1",
		},
		BootstrapPeers: dht.GetDefaultBootstrapPeerAddrInfos(),
		EnableMDNS:     true,
		EnableDHT:      true,
	}
}

// NewP2PHost creates a new P2P host with the given configuration
func NewP2PHost(ctx context.Context, cfg *Config) (*P2PHost, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	ctx, cancel := context.WithCancel(ctx)

	// Build host options
	opts := []libp2p.Option{
		libp2p.ListenAddrStrings(cfg.ListenAddrs...),
		libp2p.NATPortMap(),
		libp2p.EnableNATService(),
		libp2p.EnableRelay(),
		libp2p.EnableHolePunching(),
	}

	// Use provided private key if available
	if cfg.PrivKey != nil {
		opts = append(opts, libp2p.Identity(cfg.PrivKey))
	}

	// Setup DHT for routing
	var kadDHT *dht.IpfsDHT
	if cfg.EnableDHT {
		opts = append(opts, libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			var err error
			kadDHT, err = dht.New(ctx, h, dht.Mode(dht.ModeAutoServer))
			return kadDHT, err
		}))
	}

	// Create the host
	h, err := libp2p.New(opts...)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
	}

	// Create PubSub
	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		h.Close()
		cancel()
		return nil, fmt.Errorf("failed to create pubsub: %w", err)
	}

	p2pHost := &P2PHost{
		Host:   h,
		DHT:    kadDHT,
		PubSub: ps,
		ctx:    ctx,
		cancel: cancel,
	}

	// Setup mDNS discovery for local network
	if cfg.EnableMDNS {
		if err := p2pHost.setupMDNS(); err != nil {
			fmt.Printf("Warning: mDNS setup failed: %v\n", err)
		}
	}

	// Bootstrap DHT
	if cfg.EnableDHT && kadDHT != nil {
		if err := p2pHost.bootstrapDHT(cfg.BootstrapPeers); err != nil {
			fmt.Printf("Warning: DHT bootstrap failed: %v\n", err)
		}
	}

	p2pHost.started = true
	return p2pHost, nil
}

// setupMDNS sets up mDNS discovery
func (p *P2PHost) setupMDNS() error {
	service := mdns.NewMdnsService(p.Host, "p2papp-discovery", &mdnsNotifee{host: p.Host})
	return service.Start()
}

// bootstrapDHT bootstraps the DHT with known peers
func (p *P2PHost) bootstrapDHT(bootstrapPeers []peer.AddrInfo) error {
	if p.DHT == nil {
		return nil
	}

	// Bootstrap the DHT
	if err := p.DHT.Bootstrap(p.ctx); err != nil {
		return fmt.Errorf("failed to bootstrap DHT: %w", err)
	}

	// Connect to bootstrap peers
	var wg sync.WaitGroup
	for _, peerInfo := range bootstrapPeers {
		wg.Add(1)
		go func(pi peer.AddrInfo) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
			defer cancel()
			if err := p.Host.Connect(ctx, pi); err != nil {
				fmt.Printf("Failed to connect to bootstrap peer %s: %v\n", pi.ID.String()[:8], err)
			}
		}(peerInfo)
	}
	wg.Wait()

	return nil
}

// DiscoverPeers discovers peers using DHT routing discovery
func (p *P2PHost) DiscoverPeers(ctx context.Context, rendezvous string) (<-chan peer.AddrInfo, error) {
	if p.DHT == nil {
		return nil, fmt.Errorf("DHT not enabled")
	}

	routingDiscovery := drouting.NewRoutingDiscovery(p.DHT)
	dutil.Advertise(ctx, routingDiscovery, rendezvous)

	peerChan, err := routingDiscovery.FindPeers(ctx, rendezvous)
	if err != nil {
		return nil, fmt.Errorf("failed to find peers: %w", err)
	}

	return peerChan, nil
}

// ID returns the peer ID of the host
func (p *P2PHost) ID() peer.ID {
	return p.Host.ID()
}

// Addrs returns the multiaddresses the host is listening on
func (p *P2PHost) Addrs() []string {
	addrs := p.Host.Addrs()
	result := make([]string, len(addrs))
	for i, addr := range addrs {
		result[i] = fmt.Sprintf("%s/p2p/%s", addr, p.Host.ID())
	}
	return result
}

// Close shuts down the P2P host
func (p *P2PHost) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.started {
		return nil
	}

	p.cancel()
	if p.DHT != nil {
		p.DHT.Close()
	}
	p.started = false
	return p.Host.Close()
}

// mdnsNotifee handles mDNS peer discovery
type mdnsNotifee struct {
	host host.Host
}

func (n *mdnsNotifee) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == n.host.ID() {
		return
	}
	fmt.Printf("Discovered peer via mDNS: %s\n", pi.ID.String()[:8])
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := n.host.Connect(ctx, pi); err != nil {
		fmt.Printf("Failed to connect to discovered peer: %v\n", err)
	}
}
