# P2P Chat & Cloud Application

A decentralized peer-to-peer chat and cloud storage application built with **go-libp2p**.

## Features

- 🔐 **Wallet**: Ed25519 keypair generation with encrypted storage
- 💬 **Chat**: GossipSub-based room messaging and direct P2P messaging
- ☁️ **Cloud**: Distributed file storage with DHT-based discovery

## Quick Start

### Build
```bash
go build -o p2papp ./cmd/p2papp
```

### Create Wallet
```bash
./p2papp wallet create
```

### Join Chat Room
```bash
./p2papp chat join myroom
```

### Upload File
```bash
./p2papp cloud upload /path/to/file
```

### Download File
```bash
./p2papp cloud download <hash> /destination/path
```

### List Files
```bash
./p2papp cloud list
```

### Run Interactive Node
```bash
./p2papp run
```

## Project Structure

```
p2papp/
├── cmd/p2papp/main.go     # CLI application
├── internal/
│   ├── core/              # P2P host, config
│   ├── wallet/            # Identity, keypair management
│   ├── chat/              # GossipSub messaging, rooms, DM
│   └── cloud/             # File storage, P2P transfer, DHT indexing
├── pkg/protocol/          # Protocol definitions
└── docs/                  # Documentation
```

## Technologies

| Technology | Purpose |
|------------|---------|
| go-libp2p | Core P2P networking stack |
| GossipSub | Pub-sub messaging protocol |
| Kademlia DHT | Distributed hash table for peer/content discovery |
| mDNS | Local network peer discovery |
| Ed25519 | Asymmetric cryptography for identity |
| AES-GCM | Symmetric encryption for keystore |

## Documentation

- [Architecture](docs/ARCHITECTURE.md) - Detailed system design
- [Task Breakdown](docs/TASK.md) - Development roadmap

## License

MIT
