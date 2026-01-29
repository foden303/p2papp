package cloud

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	ChunkSize = 256 * 1024 // 256KB chunks
)

// FileStorage handles local file storage and chunking
type FileStorage struct {
	baseDir   string
	chunksDir string
	metaDir   string
}

// Chunk represents a file chunk
type Chunk struct {
	Index int    `json:"index"`
	Hash  string `json:"hash"`
	Size  int64  `json:"size"`
}

// FileMetadata contains file metadata
type FileMetadata struct {
	Hash       FileHash `json:"hash"`
	Name       string   `json:"name"`
	Size       int64    `json:"size"`
	ChunkCount int      `json:"chunk_count"`
	Chunks     []Chunk  `json:"chunks"`
}

// NewFileStorage creates a new file storage
func NewFileStorage(baseDir string) *FileStorage {
	chunksDir := filepath.Join(baseDir, "chunks")
	metaDir := filepath.Join(baseDir, "meta")

	os.MkdirAll(chunksDir, 0755)
	os.MkdirAll(metaDir, 0755)

	return &FileStorage{
		baseDir:   baseDir,
		chunksDir: chunksDir,
		metaDir:   metaDir,
	}
}

// Store chunks a file and stores it
func (s *FileStorage) Store(filePath string) (*FileInfo, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	// Hash the entire file for content addressing
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return nil, fmt.Errorf("failed to hash file: %w", err)
	}
	fileHash := FileHash(hex.EncodeToString(hasher.Sum(nil)))

	// Reset file position
	file.Seek(0, 0)

	// Chunk the file
	var chunks []Chunk
	buffer := make([]byte, ChunkSize)
	index := 0

	for {
		n, err := file.Read(buffer)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read file: %w", err)
		}

		chunkData := buffer[:n]
		chunkHash := sha256.Sum256(chunkData)
		chunkHashStr := hex.EncodeToString(chunkHash[:])

		// Save chunk
		chunkPath := filepath.Join(s.chunksDir, chunkHashStr)
		if err := os.WriteFile(chunkPath, chunkData, 0644); err != nil {
			return nil, fmt.Errorf("failed to write chunk: %w", err)
		}

		chunks = append(chunks, Chunk{
			Index: index,
			Hash:  chunkHashStr,
			Size:  int64(n),
		})
		index++
	}

	// Save metadata
	meta := FileMetadata{
		Hash:       fileHash,
		Name:       filepath.Base(filePath),
		Size:       stat.Size(),
		ChunkCount: len(chunks),
		Chunks:     chunks,
	}

	metaPath := filepath.Join(s.metaDir, string(fileHash)+".json")
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		return nil, fmt.Errorf("failed to write metadata: %w", err)
	}

	return &FileInfo{
		Hash:       fileHash,
		Name:       meta.Name,
		Size:       meta.Size,
		ChunkCount: meta.ChunkCount,
		LocalPath:  filePath,
	}, nil
}

// Retrieve reconstructs a file from chunks
func (s *FileStorage) Retrieve(hash FileHash, destPath string) error {
	meta, err := s.loadMetadata(hash)
	if err != nil {
		return fmt.Errorf("failed to load metadata: %w", err)
	}

	outFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	for _, chunk := range meta.Chunks {
		chunkPath := filepath.Join(s.chunksDir, chunk.Hash)
		chunkData, err := os.ReadFile(chunkPath)
		if err != nil {
			return fmt.Errorf("failed to read chunk %d: %w", chunk.Index, err)
		}

		if _, err := outFile.Write(chunkData); err != nil {
			return fmt.Errorf("failed to write chunk %d: %w", chunk.Index, err)
		}
	}

	return nil
}

// Exists checks if a file exists in storage
func (s *FileStorage) Exists(hash FileHash) bool {
	metaPath := filepath.Join(s.metaDir, string(hash)+".json")
	_, err := os.Stat(metaPath)
	return err == nil
}

// GetInfo returns file information
func (s *FileStorage) GetInfo(hash FileHash) (*FileInfo, error) {
	meta, err := s.loadMetadata(hash)
	if err != nil {
		return nil, err
	}

	return &FileInfo{
		Hash:       meta.Hash,
		Name:       meta.Name,
		Size:       meta.Size,
		ChunkCount: meta.ChunkCount,
	}, nil
}

// List returns all stored files
func (s *FileStorage) List() ([]FileInfo, error) {
	files, err := os.ReadDir(s.metaDir)
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}

	var result []FileInfo
	for _, file := range files {
		if filepath.Ext(file.Name()) != ".json" {
			continue
		}

		hash := FileHash(file.Name()[:len(file.Name())-5]) // Remove .json
		info, err := s.GetInfo(hash)
		if err != nil {
			continue
		}
		result = append(result, *info)
	}

	return result, nil
}

// Delete removes a file from storage
func (s *FileStorage) Delete(hash FileHash) error {
	meta, err := s.loadMetadata(hash)
	if err != nil {
		return fmt.Errorf("file not found: %w", err)
	}

	// Delete chunks
	for _, chunk := range meta.Chunks {
		chunkPath := filepath.Join(s.chunksDir, chunk.Hash)
		os.Remove(chunkPath) // Ignore errors
	}

	// Delete metadata
	metaPath := filepath.Join(s.metaDir, string(hash)+".json")
	return os.Remove(metaPath)
}

// GetChunk returns a specific chunk
func (s *FileStorage) GetChunk(chunkHash string) ([]byte, error) {
	chunkPath := filepath.Join(s.chunksDir, chunkHash)
	return os.ReadFile(chunkPath)
}

// GetMetadata returns file metadata
func (s *FileStorage) GetMetadata(hash FileHash) (*FileMetadata, error) {
	return s.loadMetadata(hash)
}

func (s *FileStorage) loadMetadata(hash FileHash) (*FileMetadata, error) {
	metaPath := filepath.Join(s.metaDir, string(hash)+".json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, err
	}

	var meta FileMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}

	return &meta, nil
}
