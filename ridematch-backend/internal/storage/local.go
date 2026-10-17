package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const uploadsDir = "uploads"

type localStore struct {
	publicBaseURL string
}

func newLocalStore(publicBaseURL string) (Store, error) {
	if err := os.MkdirAll(uploadsDir, 0o755); err != nil {
		return nil, fmt.Errorf("storage: failed to create %s directory: %w", uploadsDir, err)
	}
	return &localStore{publicBaseURL: strings.TrimSuffix(publicBaseURL, "/")}, nil
}

func (s *localStore) Save(_ context.Context, filename string, data io.Reader, _ int64, _ string) (string, error) {
	// The caller-supplied filename is only ever used for its extension —
	// the on-disk name is always a fresh UUID, so nothing about the
	// original filename (including any path segments in it) ever reaches
	// the filesystem path.
	ext := filepath.Ext(filename)
	safeName := uuid.NewString() + ext
	path := filepath.Join(uploadsDir, safeName)

	out, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("storage: failed to create file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, data); err != nil {
		return "", fmt.Errorf("storage: failed to write file: %w", err)
	}

	return fmt.Sprintf("%s/%s/%s", s.publicBaseURL, uploadsDir, safeName), nil
}

// SignedURL is a no-op for the local driver — /uploads is already served
// directly and publicly by the API itself, nothing to sign.
func (s *localStore) SignedURL(_ context.Context, url string) (string, error) {
	return url, nil
}
