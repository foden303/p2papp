package consensus

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb/v2"
)

const (
	RetainSnapshotCount = 2
	RaftTimeout         = 10 * time.Second
)

// Command represents a command to be applied to the FSM
type Command struct {
	Op    string      `json:"op"`
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
}

// CommandOp defines command operations
const (
	OpSet    = "set"
	OpDelete = "delete"
)

// RaftNode wraps the Raft consensus implementation
type RaftNode struct {
	raft          *raft.Raft
	fsm           *FSM
	transport     *raft.NetworkTransport
	logStore      raft.LogStore
	stableStore   raft.StableStore
	snapshotStore raft.SnapshotStore

	nodeID   string
	raftDir  string
	raftBind string

	mu sync.RWMutex
}

// Config holds Raft node configuration
type Config struct {
	NodeID    string
	RaftDir   string
	RaftBind  string // e.g., "127.0.0.1:7000"
	Bootstrap bool
	JoinAddr  string // Leader address to join
}

// NewRaftNode creates a new Raft node
func NewRaftNode(cfg Config) (*RaftNode, error) {
	// Ensure raft directory exists
	if err := os.MkdirAll(cfg.RaftDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create raft dir: %w", err)
	}

	node := &RaftNode{
		nodeID:   cfg.NodeID,
		raftDir:  cfg.RaftDir,
		raftBind: cfg.RaftBind,
	}

	// Create FSM
	node.fsm = NewFSM()

	// Setup Raft configuration
	raftConfig := raft.DefaultConfig()
	raftConfig.LocalID = raft.ServerID(cfg.NodeID)
	raftConfig.SnapshotThreshold = 1024
	raftConfig.SnapshotInterval = 30 * time.Second

	// Setup TCP transport
	addr, err := net.ResolveTCPAddr("tcp", cfg.RaftBind)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve bind address: %w", err)
	}

	transport, err := raft.NewTCPTransport(cfg.RaftBind, addr, 3, RaftTimeout, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport: %w", err)
	}
	node.transport = transport

	// Create BoltDB store for logs and stable storage
	boltDB, err := raftboltdb.NewBoltStore(filepath.Join(cfg.RaftDir, "raft.db"))
	if err != nil {
		return nil, fmt.Errorf("failed to create bolt store: %w", err)
	}
	node.logStore = boltDB
	node.stableStore = boltDB

	// Create snapshot store
	snapshotStore, err := raft.NewFileSnapshotStore(cfg.RaftDir, RetainSnapshotCount, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot store: %w", err)
	}
	node.snapshotStore = snapshotStore

	// Create Raft instance
	r, err := raft.NewRaft(raftConfig, node.fsm, node.logStore, node.stableStore, node.snapshotStore, transport)
	if err != nil {
		return nil, fmt.Errorf("failed to create raft: %w", err)
	}
	node.raft = r

	// Bootstrap if this is the first node
	if cfg.Bootstrap {
		configuration := raft.Configuration{
			Servers: []raft.Server{
				{
					ID:      raft.ServerID(cfg.NodeID),
					Address: raft.ServerAddress(cfg.RaftBind),
				},
			},
		}
		r.BootstrapCluster(configuration)
	}

	return node, nil
}

// Join adds a node to the cluster
func (n *RaftNode) Join(nodeID, addr string) error {
	if n.raft.State() != raft.Leader {
		return fmt.Errorf("not the leader")
	}

	configFuture := n.raft.GetConfiguration()
	if err := configFuture.Error(); err != nil {
		return fmt.Errorf("failed to get configuration: %w", err)
	}

	for _, srv := range configFuture.Configuration().Servers {
		if srv.ID == raft.ServerID(nodeID) {
			if srv.Address == raft.ServerAddress(addr) {
				return nil // Already a member
			}
			// Remove existing with same ID but different address
			future := n.raft.RemoveServer(srv.ID, 0, 0)
			if err := future.Error(); err != nil {
				return fmt.Errorf("failed to remove existing server: %w", err)
			}
		}
	}

	future := n.raft.AddVoter(raft.ServerID(nodeID), raft.ServerAddress(addr), 0, 0)
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to add voter: %w", err)
	}

	return nil
}

// Leave removes a node from the cluster
func (n *RaftNode) Leave(nodeID string) error {
	if n.raft.State() != raft.Leader {
		return fmt.Errorf("not the leader")
	}

	future := n.raft.RemoveServer(raft.ServerID(nodeID), 0, 0)
	return future.Error()
}

// Apply applies a command to the Raft cluster
func (n *RaftNode) Apply(cmd Command) error {
	if n.raft.State() != raft.Leader {
		return fmt.Errorf("not the leader")
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	future := n.raft.Apply(data, RaftTimeout)
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to apply command: %w", err)
	}

	return nil
}

// Set stores a key-value pair
func (n *RaftNode) Set(key string, value interface{}) error {
	return n.Apply(Command{Op: OpSet, Key: key, Value: value})
}

// Delete removes a key
func (n *RaftNode) Delete(key string) error {
	return n.Apply(Command{Op: OpDelete, Key: key})
}

// Get retrieves a value by key (local read)
func (n *RaftNode) Get(key string) (interface{}, bool) {
	return n.fsm.Get(key)
}

// GetAll returns all stored data
func (n *RaftNode) GetAll() map[string]interface{} {
	return n.fsm.GetAll()
}

// IsLeader returns true if this node is the leader
func (n *RaftNode) IsLeader() bool {
	return n.raft.State() == raft.Leader
}

// LeaderAddr returns the address of the current leader
func (n *RaftNode) LeaderAddr() string {
	addr, _ := n.raft.LeaderWithID()
	return string(addr)
}

// State returns the current Raft state
func (n *RaftNode) State() raft.RaftState {
	return n.raft.State()
}

// Stats returns Raft statistics
func (n *RaftNode) Stats() map[string]string {
	return n.raft.Stats()
}

// Shutdown gracefully shuts down the Raft node
func (n *RaftNode) Shutdown() error {
	future := n.raft.Shutdown()
	return future.Error()
}

// WaitForLeader blocks until a leader is elected
func (n *RaftNode) WaitForLeader(timeout time.Duration) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-ticker.C:
			if leader, _ := n.raft.LeaderWithID(); leader != "" {
				return nil
			}
		case <-timer.C:
			return fmt.Errorf("timeout waiting for leader")
		}
	}
}

// Snapshot triggers a manual snapshot
func (n *RaftNode) Snapshot() error {
	future := n.raft.Snapshot()
	return future.Error()
}

// FSM implements the raft.FSM interface
type FSM struct {
	data map[string]interface{}
	mu   sync.RWMutex
}

// NewFSM creates a new FSM
func NewFSM() *FSM {
	return &FSM{
		data: make(map[string]interface{}),
	}
}

// Apply applies a Raft log entry to the FSM
func (f *FSM) Apply(log *raft.Log) interface{} {
	var cmd Command
	if err := json.Unmarshal(log.Data, &cmd); err != nil {
		return fmt.Errorf("failed to unmarshal command: %w", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	switch cmd.Op {
	case OpSet:
		f.data[cmd.Key] = cmd.Value
	case OpDelete:
		delete(f.data, cmd.Key)
	}

	return nil
}

// Snapshot returns a snapshot of the FSM
func (f *FSM) Snapshot() (raft.FSMSnapshot, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Deep copy the data
	data := make(map[string]interface{})
	for k, v := range f.data {
		data[k] = v
	}

	return &fsmSnapshot{data: data}, nil
}

// Restore restores the FSM from a snapshot
func (f *FSM) Restore(rc io.ReadCloser) error {
	defer rc.Close()

	var data map[string]interface{}
	if err := json.NewDecoder(rc).Decode(&data); err != nil {
		return fmt.Errorf("failed to decode snapshot: %w", err)
	}

	f.mu.Lock()
	f.data = data
	f.mu.Unlock()

	return nil
}

// Get retrieves a value from the FSM
func (f *FSM) Get(key string) (interface{}, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	val, ok := f.data[key]
	return val, ok
}

// GetAll returns all data
func (f *FSM) GetAll() map[string]interface{} {
	f.mu.RLock()
	defer f.mu.RUnlock()

	data := make(map[string]interface{})
	for k, v := range f.data {
		data[k] = v
	}
	return data
}

// fsmSnapshot implements raft.FSMSnapshot
type fsmSnapshot struct {
	data map[string]interface{}
}

func (s *fsmSnapshot) Persist(sink raft.SnapshotSink) error {
	err := func() error {
		data, err := json.Marshal(s.data)
		if err != nil {
			return fmt.Errorf("failed to marshal snapshot: %w", err)
		}

		if _, err := sink.Write(data); err != nil {
			return fmt.Errorf("failed to write snapshot: %w", err)
		}

		return nil
	}()

	if err != nil {
		sink.Cancel()
		return err
	}

	return sink.Close()
}

func (s *fsmSnapshot) Release() {}
