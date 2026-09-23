package storage

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"

	"ridematch-backend/internal/config"
)

// s3Store saves uploads to any S3-compatible object store — this app runs
// it against IDrive e2 (cheap, S3-compatible, no egress-fee surprises),
// but nothing here is IDrive-specific: the same client, pointed at a
// different S3_ENDPOINT, works against Cloudflare R2, Backblaze B2,
// MinIO, or real AWS S3 without a code change.
type s3Store struct {
	client        *s3.Client
	bucket        string
	publicBaseURL string
}

// newS3Store builds an s3Store from config. Credentials are supplied
// directly (S3_ACCESS_KEY/S3_SECRET_KEY) rather than via the AWS SDK's
// usual environment/instance-profile discovery chain — this app has one
// object-storage account, not an AWS execution role to assume.
func newS3Store(cfg *config.Config) (Store, error) {
	awsCfg := aws.Config{
		Region: cfg.S3Region,
		Credentials: credentials.NewStaticCredentialsProvider(
			cfg.S3AccessKey, cfg.S3SecretKey, "",
		),
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.S3Endpoint)
		// IDrive e2 (like most non-AWS S3-compatible providers) is
		// addressed as endpoint/bucket/key rather than
		// bucket.endpoint/key — path-style must be forced since the SDK
		// defaults to virtual-hosted style, which real AWS S3 expects.
		o.UsePathStyle = cfg.S3ForcePathStyle
	})

	publicBaseURL := strings.TrimSuffix(cfg.S3PublicBaseURL, "/")
	if publicBaseURL == "" {
		// Fall back to constructing it from the endpoint — correct for a
		// publicly-readable bucket with no CDN/custom domain in front of
		// it, which is the common case for IDrive e2.
		style := "/" + cfg.S3Bucket
		if !cfg.S3ForcePathStyle {
			style = ""
		}
		publicBaseURL = strings.TrimSuffix(cfg.S3Endpoint, "/") + style
	}

	return &s3Store{client: client, bucket: cfg.S3Bucket, publicBaseURL: publicBaseURL}, nil
}

func (s *s3Store) Save(ctx context.Context, filename string, data io.Reader, size int64, contentType string) (string, error) {
	// Same treatment as the local store: only the extension of the
	// caller-supplied filename is trusted, the object key is always a
	// fresh UUID.
	ext := filepath.Ext(filename)
	key := uuid.NewString() + ext

	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          data,
		ContentLength: aws.Int64(size),
		ContentType:   aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("storage: failed to upload to S3-compatible store: %w", err)
	}

	return fmt.Sprintf("%s/%s", s.publicBaseURL, key), nil
}
