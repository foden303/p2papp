# Decentralized P2P Chat & Cloud App

## Objective
Build a decentralized chat and cloud file management application based on go-libp2p with 3 separate modules:
- **Chat Module**: Decentralized P2P messaging
- **Cloud Module**: Distributed file management and sharing
- **Wallet Module**: Identity and keypair management

## Task Breakdown

### Phase 1: Core Infrastructure
- [x] Initialize Go project structure with go modules
- [x] Create folder structure for modules (chat, cloud, wallet)
- [x] Install dependencies (go-libp2p, go-libp2p-kad-dht, go-libp2p-pubsub)

### Phase 2: Wallet Module
- [x] Create keypair generation (Ed25519)
- [x] Create peer identity management
- [x] Implement save/load wallet from file
- [x] Create wallet service interface

### Phase 3: Chat Module
- [x] Setup libp2p host with peer discovery
- [x] Implement pub-sub messaging (GossipSub)
- [x] Create room/channel management
- [x] Build direct message (P2P streams)
- [ ] Implement message persistence (local storage)

### Phase 4: Cloud Module
- [x] Implement file chunking & hashing
- [x] Create DHT-based file indexing
- [x] Build file upload/download with P2P streams
- [x] Implement file sharing via peer discovery
- [x] Create local file metadata storage

### Phase 5: CLI Application
- [x] Create command-line interface for all modules
- [x] Implement interactive mode
- [x] Add configuration management

### Phase 6: Testing & Documentation
- [ ] Write unit tests for modules
- [ ] Create integration tests with multiple peers
- [ ] Write documentation and usage examples
