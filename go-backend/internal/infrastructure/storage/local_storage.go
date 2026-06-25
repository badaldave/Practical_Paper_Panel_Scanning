package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"university-result-processing/backend/internal/domain"
)

type LocalStorageProvider struct {
	baseDir string
}

func NewLocalStorageProvider(baseDir string) (*LocalStorageProvider, error) {
	// Ensure base directory exists
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create upload base directory: %w", err)
	}
	return &LocalStorageProvider{baseDir: baseDir}, nil
}

func (p *LocalStorageProvider) SaveFile(ctx context.Context, filename string, reader io.Reader) (string, error) {
	// Create destination path
	destPath := filepath.Join(p.baseDir, filename)
	
	// Create subdirectories if filename has them (e.g. tenant_id/doc_id/...)
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create target directories: %w", err)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("failed to create local file: %w", err)
	}
	defer out.Close()

	_, err = io.Copy(out, reader)
	if err != nil {
		return "", fmt.Errorf("failed to write file content: %w", err)
	}

	return destPath, nil
}

func (p *LocalStorageProvider) GetFile(ctx context.Context, filePath string) (io.ReadCloser, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open local file: %w", err)
	}
	return file, nil
}

func (p *LocalStorageProvider) DeleteFile(ctx context.Context, filePath string) error {
	err := os.Remove(filePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete local file: %w", err)
	}
	return nil
}

// Compile-time check to verify LocalStorageProvider implements domain.StorageProvider
var _ domain.StorageProvider = (*LocalStorageProvider)(nil)
