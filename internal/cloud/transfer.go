package cloud

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	"p2papp/internal/core"
)

const (
	TransferProtocol = protocol.ID("/p2papp/transfer/1.0.0")
	NotifyProtocol   = protocol.ID("/p2papp/notify/1.0.0")
)

// TransferRequest represents a file transfer request
type TransferRequest struct {
	Type      string   `json:"type"` // "metadata" or "chunk"
	FileHash  FileHash `json:"file_hash,omitempty"`
	ChunkHash string   `json:"chunk_hash,omitempty"`
}

// TransferResponse represents a file transfer response
type TransferResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Data    []byte `json:"data,omitempty"`
}

// FileNotification notifies a peer about a shared file
type FileNotification struct {
	FileHash FileHash `json:"file_hash"`
	FileName string   `json:"file_name"`
	FileSize int64    `json:"file_size"`
	From     peer.ID  `json:"from"`
}

// FileTransfer handles P2P file transfers
type FileTransfer struct {
	host    *core.P2PHost
	storage *FileStorage
}

// NewFileTransfer creates a new file transfer handler
func NewFileTransfer(host *core.P2PHost, storage *FileStorage) *FileTransfer {
	return &FileTransfer{
		host:    host,
		storage: storage,
	}
}

// RegisterHandler registers the transfer protocol handlers
func (t *FileTransfer) RegisterHandler() {
	t.host.Host.SetStreamHandler(TransferProtocol, t.handleTransfer)
	t.host.Host.SetStreamHandler(NotifyProtocol, t.handleNotify)
}

// handleTransfer handles incoming file transfer requests
func (t *FileTransfer) handleTransfer(stream network.Stream) {
	defer stream.Close()

	reader := bufio.NewReader(stream)
	data, err := reader.ReadBytes('\n')
	if err != nil {
		return
	}

	var req TransferRequest
	if err := json.Unmarshal(data, &req); err != nil {
		t.sendResponse(stream, TransferResponse{Success: false, Error: "invalid request"})
		return
	}

	switch req.Type {
	case "metadata":
		meta, err := t.storage.GetMetadata(req.FileHash)
		if err != nil {
			t.sendResponse(stream, TransferResponse{Success: false, Error: "file not found"})
			return
		}
		metaBytes, _ := json.Marshal(meta)
		t.sendResponse(stream, TransferResponse{Success: true, Data: metaBytes})

	case "chunk":
		data, err := t.storage.GetChunk(req.ChunkHash)
		if err != nil {
			t.sendResponse(stream, TransferResponse{Success: false, Error: "chunk not found"})
			return
		}
		t.sendResponse(stream, TransferResponse{Success: true, Data: data})

	default:
		t.sendResponse(stream, TransferResponse{Success: false, Error: "unknown request type"})
	}
}

// handleNotify handles file share notifications
func (t *FileTransfer) handleNotify(stream network.Stream) {
	defer stream.Close()

	reader := bufio.NewReader(stream)
	data, err := reader.ReadBytes('\n')
	if err != nil {
		return
	}

	var notification FileNotification
	if err := json.Unmarshal(data, &notification); err != nil {
		return
	}

	notification.From = stream.Conn().RemotePeer()
	fmt.Printf("File shared by %s: %s (%d bytes)\n",
		notification.From.String()[:8],
		notification.FileName,
		notification.FileSize)
}

// sendResponse sends a response over the stream
func (t *FileTransfer) sendResponse(stream network.Stream, resp TransferResponse) {
	data, _ := json.Marshal(resp)
	data = append(data, '\n')
	stream.Write(data)
}

// Download downloads a file from a peer
func (t *FileTransfer) Download(ctx context.Context, peerID peer.ID, hash FileHash, destPath string) error {
	// Request metadata
	meta, err := t.requestMetadata(ctx, peerID, hash)
	if err != nil {
		return fmt.Errorf("failed to get metadata: %w", err)
	}

	// Create temp file
	tmpPath := destPath + ".tmp"
	outFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer outFile.Close()

	// Download chunks
	for _, chunk := range meta.Chunks {
		data, err := t.requestChunk(ctx, peerID, chunk.Hash)
		if err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("failed to download chunk %d: %w", chunk.Index, err)
		}

		if _, err := outFile.Write(data); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("failed to write chunk %d: %w", chunk.Index, err)
		}

		// Also save chunk locally
		t.storage.GetChunk(chunk.Hash) // Trigger local storage
	}

	// Rename temp file
	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("failed to finalize file: %w", err)
	}

	return nil
}

// requestMetadata requests file metadata from a peer
func (t *FileTransfer) requestMetadata(ctx context.Context, peerID peer.ID, hash FileHash) (*FileMetadata, error) {
	stream, err := t.host.Host.NewStream(ctx, peerID, TransferProtocol)
	if err != nil {
		return nil, fmt.Errorf("failed to open stream: %w", err)
	}
	defer stream.Close()

	req := TransferRequest{Type: "metadata", FileHash: hash}
	reqData, _ := json.Marshal(req)
	reqData = append(reqData, '\n')

	if _, err := stream.Write(reqData); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	reader := bufio.NewReader(stream)
	respData, err := reader.ReadBytes('\n')
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var resp TransferResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("transfer failed: %s", resp.Error)
	}

	var meta FileMetadata
	if err := json.Unmarshal(resp.Data, &meta); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	return &meta, nil
}

// requestChunk requests a chunk from a peer
func (t *FileTransfer) requestChunk(ctx context.Context, peerID peer.ID, chunkHash string) ([]byte, error) {
	stream, err := t.host.Host.NewStream(ctx, peerID, TransferProtocol)
	if err != nil {
		return nil, fmt.Errorf("failed to open stream: %w", err)
	}
	defer stream.Close()

	req := TransferRequest{Type: "chunk", ChunkHash: chunkHash}
	reqData, _ := json.Marshal(req)
	reqData = append(reqData, '\n')

	if _, err := stream.Write(reqData); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	reader := bufio.NewReader(stream)
	respData, err := reader.ReadBytes('\n')
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var resp TransferResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("transfer failed: %s", resp.Error)
	}

	return resp.Data, nil
}

// NotifyPeer notifies a peer about a shared file
func (t *FileTransfer) NotifyPeer(ctx context.Context, peerID peer.ID, hash FileHash) error {
	info, err := t.storage.GetInfo(hash)
	if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}

	stream, err := t.host.Host.NewStream(ctx, peerID, NotifyProtocol)
	if err != nil {
		return fmt.Errorf("failed to open stream: %w", err)
	}
	defer stream.Close()

	notification := FileNotification{
		FileHash: hash,
		FileName: info.Name,
		FileSize: info.Size,
		From:     t.host.ID(),
	}

	data, _ := json.Marshal(notification)
	data = append(data, '\n')

	_, err = stream.Write(data)
	return err
}

// Close closes the file transfer handler
func (t *FileTransfer) Close() {
	t.host.Host.RemoveStreamHandler(TransferProtocol)
	t.host.Host.RemoveStreamHandler(NotifyProtocol)
}
