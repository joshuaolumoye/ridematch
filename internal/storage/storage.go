// Package storage saves uploaded files (a driver's vehicle photo, ID
// document) and returns a URL the rest of the app can store as a plain
// string — internal/dto's RegisterDriverRequest has always expected these
// as URLs, uploaded "directly to object storage... or via a future
// /uploads endpoint" (see its doc comment); this package is that endpoint's
// backing implementation.
package storage

import (
	"context"
	"fmt"
	"io"

	"ridematch-backend/internal/config"
)

// Store saves file content and returns a publicly reachable URL for it.
type Store interface {
	Save(ctx context.Context, filename string, data io.Reader, size int64, contentType string) (url string, err error)
}

// New builds the configured Store. "local" (the default, and everything
// this app runs on out of the box) writes to ./uploads and serves it back
// via the API's own /uploads static route. "s3" talks to any
// S3-compatible object store — this app is configured for IDrive e2, but
// the same client works unmodified against R2, Backblaze B2, MinIO, or
// real AWS S3. It needs real credentials in .env — if they're missing,
// New fails loudly at startup rather than silently falling back to local
// (a driver's real documents ending up on local disk when the operator
// believed they'd configured object storage would be a much worse
// surprise than a startup error).
func New(cfg *config.Config) (Store, error) {
	switch cfg.StorageDriver {
	case "", "local":
		return newLocalStore(cfg.AppPublicURL)
	case "s3":
		if cfg.S3Endpoint == "" || cfg.S3AccessKey == "" || cfg.S3SecretKey == "" || cfg.S3Bucket == "" {
			return nil, fmt.Errorf("storage: STORAGE_DRIVER=s3 requires S3_ENDPOINT, S3_ACCESS_KEY, S3_SECRET_KEY, and S3_BUCKET to be set")
		}
		return newS3Store(cfg)
	default:
		return nil, fmt.Errorf("storage: unknown STORAGE_DRIVER %q (expected \"local\" or \"s3\")", cfg.StorageDriver)
	}
}
