// Package objectstore wraps Cloudflare R2 (S3-compatible) access: only
// presigned PUT/GET URL generation and a HEAD check, since the API
// server itself never proxies file bytes (see PACKS_PLAN.md §1) - the
// CLI uploads/downloads directly against R2 using the URLs this package
// hands back.
package objectstore

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Client is a thin wrapper the api package depends on through its own
// narrow ObjectStore interface (see internal/api/api.go) - mirrors how
// internal/store.Store is a concrete type consumed through a
// per-consumer interface, not an exported interface of its own.
type Client struct {
	presign *s3.PresignClient
	s3      *s3.Client
	bucket  string
}

// New builds a Client against R2's S3-compatible endpoint for
// accountID/bucket, authenticated with an R2 API token's access/secret
// key pair (Cloudflare dashboard: R2 -> Manage API Tokens).
func New(ctx context.Context, accountID, accessKeyID, secretAccessKey, bucket string) (*Client, error) {
	endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", accountID)

	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("auto"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS SDK config: %w", err)
	}

	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
	})

	return &Client{
		presign: s3.NewPresignClient(s3Client),
		s3:      s3Client,
		bucket:  bucket,
	}, nil
}

// PresignPut returns a URL the CLI can PUT a tarball's bytes to directly,
// valid for expires.
func (c *Client) PresignPut(ctx context.Context, key string, expires time.Duration) (string, error) {
	req, err := c.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expires))
	if err != nil {
		return "", fmt.Errorf("failed to presign PUT for %s: %w", key, err)
	}
	return req.URL, nil
}

// PresignGet returns a URL the CLI can download a tarball from directly,
// valid for expires.
func (c *Client) PresignGet(ctx context.Context, key string, expires time.Duration) (string, error) {
	req, err := c.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expires))
	if err != nil {
		return "", fmt.Errorf("failed to presign GET for %s: %w", key, err)
	}
	return req.URL, nil
}

// HeadObject returns key's size in bytes, confirming it actually exists
// in the bucket - used by the /complete endpoint to verify a publish's
// PUT really landed before flipping the version to published.
func (c *Client) HeadObject(ctx context.Context, key string) (int64, error) {
	out, err := c.s3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, fmt.Errorf("object %s not found in bucket: %w", key, err)
	}
	if out.ContentLength == nil {
		return 0, fmt.Errorf("object %s has no reported size", key)
	}
	return *out.ContentLength, nil
}
