package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"time"
)

// FileStorage implements Storage using the local filesystem.
// Each cache entry is stored as a file with a SHA256-hashed name,
// distributed across 256 subdirectories for fan-out.
type FileStorage struct {
	basePath string
}

// NewFileStorage creates a new file-based storage backend.
func NewFileStorage(basePath string) (*FileStorage, error) {
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, err
	}
	return &FileStorage{basePath: basePath}, nil
}

func (f *FileStorage) keyToPath(key string) string {
	h := sha256.Sum256([]byte(key))
	hexStr := hex.EncodeToString(h[:])
	return filepath.Join(f.basePath, hexStr[:2], hexStr[2:])
}

func (f *FileStorage) Get(_ context.Context, key string) ([]byte, bool) {
	data, err := os.ReadFile(f.keyToPath(key))
	if err != nil {
		return nil, false
	}
	return data, true
}

func (f *FileStorage) Set(_ context.Context, key string, data []byte, _ time.Duration) error {
	path := f.keyToPath(key)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (f *FileStorage) Delete(_ context.Context, key string) error {
	err := os.Remove(f.keyToPath(key))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (f *FileStorage) Purge(_ context.Context) error {
	if err := os.RemoveAll(f.basePath); err != nil {
		return err
	}
	return os.MkdirAll(f.basePath, 0755)
}

func (f *FileStorage) Close() error {
	return nil
}
