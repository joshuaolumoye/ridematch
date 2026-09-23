package storage

import (
	"fmt"

	"ridematch-backend/internal/config"
)

// newR2Store is a placeholder: Cloudflare R2 upload support (the AWS S3
// SDK client, credentials, multipart handling) hasn't been built yet.
// New() only reaches this once STORAGE_DRIVER=r2 is explicitly set with
// all four R2_* vars filled in, so this error is a real "not built yet"
// rather than something a default config could ever hit — the app runs
// on the local driver out of the box.
func newR2Store(_ *config.Config) (Store, error) {
	return nil, fmt.Errorf("storage: STORAGE_DRIVER=r2 is not implemented yet — use STORAGE_DRIVER=local, or implement internal/storage/r2.go")
}
