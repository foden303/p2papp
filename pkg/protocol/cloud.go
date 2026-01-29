package protocol

import (
	"github.com/libp2p/go-libp2p/core/peer"
)

// CloudProtocol constants
const (
	CloudProtocolVersion = 1
	TransferProtocol     = "/p2papp/transfer/1.0.0"
	NotifyProtocol       = "/p2papp/notify/1.0.0"
	ChunkSize            = 256 * 1024 // 256KB
)

// TransferRequest is the wire format for transfer requests
type TransferRequest struct {
	Version   int    `json:"version"`
	Type      string `json:"type"` // "metadata" or "chunk"
	FileHash  string `json:"file_hash,omitempty"`
	ChunkHash string `json:"chunk_hash,omitempty"`
}

// TransferResponse is the wire format for transfer responses
type TransferResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Data    []byte `json:"data,omitempty"`
}

// FileNotification notifies about shared files
type FileNotification struct {
	Version  int     `json:"version"`
	FileHash string  `json:"file_hash"`
	FileName string  `json:"file_name"`
	FileSize int64   `json:"file_size"`
	From     peer.ID `json:"from"`
}

// Request types
const (
	RequestMetadata = "metadata"
	RequestChunk    = "chunk"
)
