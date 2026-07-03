// Package store is the media service persistence layer. Unlike the other
// MiSArch services it is NOT backed by a relational or document database: the
// "database" is an S3-compatible object store (MinIO), bucket "media-data".
// Object keys are the single source of truth — there is no projection, cache,
// or metadata store. This package wraps the minio-go client with the exact
// list / put / presign behavior the original Rust service (rust-s3) exhibited.
package store

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"misarch/media/config"
)

// Store provides object-store operations for media files.
type Store struct {
	client     *minio.Client
	bucket     string
	region     string
	expiration time.Duration
	proxyPath  string
}

// New builds a Store from the media configuration. It constructs a MinIO
// client with path-style addressing (bucket name in the URL path), region
// eu-central-1, and static admin/password credentials — matching the Rust
// service's `Bucket::create_with_path_style` / `Region::Custom` setup — then
// bootstraps the "media-data" bucket.
//
// Bucket bootstrap mirrors the Rust create-then-fallback: attempt to create
// the bucket; if it already exists (or is already owned), proceed with a
// handle to the existing bucket rather than crashing.
func New(ctx context.Context, cfg config.Config) (*Store, error) {
	// minio-go builds the endpoint URL as scheme+"://"+endpoint, so the
	// endpoint passed to New must be a bare host:port. MINIO_ENDPOINT is a full
	// URL (e.g. http://media-minio:9000); split it into host (endpoint) and
	// scheme (Secure flag).
	endpoint, secure, err := parseEndpoint(cfg.MinioEndpoint)
	if err != nil {
		return nil, err
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(config.AccessKey, config.SecretKey, ""),
		Secure: secure,
		Region: config.Region,
		// Path-style addressing: http://host/media-data/{key} rather than
		// virtual-hosted http://media-data.host/{key}. Matches rust-s3's
		// with_path_style() and keeps the presigned URL path in the form the
		// PROXY_PATH rewrite and the Nginx location block expect.
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return nil, fmt.Errorf("media: create minio client: %w", err)
	}

	st := &Store{
		client:     client,
		bucket:     config.BucketName,
		region:     config.Region,
		expiration: time.Duration(cfg.PathExpirationTime) * time.Second,
		proxyPath:  cfg.ProxyPath,
	}
	if err := st.ensureBucket(ctx); err != nil {
		return nil, err
	}
	return st, nil
}

// ensureBucket creates the media-data bucket if absent. An "already exists /
// already owned by you" outcome is not an error — the Rust service falls back
// to opening the existing bucket in that case.
func (s *Store) ensureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err == nil && exists {
		return nil
	}
	makeErr := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: s.region})
	if makeErr == nil {
		return nil
	}
	// Tolerate the create-race / already-exists conditions (matches the Rust
	// create-then-fallback-to-open behavior); surface any other error.
	code := minio.ToErrorResponse(makeErr).Code
	if code == "BucketAlreadyOwnedByYou" || code == "BucketAlreadyExists" {
		return nil
	}
	// Re-check existence: BucketExists may have failed transiently above.
	if exists2, err2 := s.client.BucketExists(ctx, s.bucket); err2 == nil && exists2 {
		return nil
	}
	return fmt.Errorf("media: bootstrap bucket %q: %w", s.bucket, makeErr)
}

// ListAll lists every object in the bucket (prefix "") and returns their keys
// in the order S3 yields them (lexical). Mirrors `bucket.list("", None)` — a
// flat/recursive listing (no delimiter grouping) — followed by taking the
// single result page's contents. An error from the listing stream is returned.
func (s *Store) ListAll(ctx context.Context) ([]string, error) {
	return s.listKeys(ctx, "")
}

// ListByPrefix lists objects whose key starts with prefix, in lexical order.
// Used by Media.path with prefix = the media UUID string.
func (s *Store) ListByPrefix(ctx context.Context, prefix string) ([]string, error) {
	return s.listKeys(ctx, prefix)
}

// listKeys performs the underlying ListObjects call. Recursive:true disables
// the '/' delimiter grouping, matching rust-s3's delimiter=None (a flat listing
// of full keys). minio-go streams objects in lexical order, so the resulting
// slice is the equivalent of the original's single ListBucketResult page
// contents.
func (s *Store) listKeys(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	for object := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if object.Err != nil {
			return nil, object.Err
		}
		keys = append(keys, object.Key)
	}
	return keys, nil
}

// PutObject stores buffer under key. It returns the HTTP status code of the
// underlying PutObject request so the caller can reproduce the Rust service's
// status-code check (200 → success, otherwise error) — including the ordering
// where the created-event is published before this status is evaluated.
//
// The object's stored Content-Type is application/octet-stream, matching
// rust-s3's put_object default; download correctness relies on the presigned
// URL + reverse proxy + the extension baked into the key, not this header.
func (s *Store) PutObject(ctx context.Context, key string, buffer []byte) (int, error) {
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(buffer), int64(len(buffer)), minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	if err != nil {
		// minio-go returns a nil error only on a 2xx response. A non-2xx S3
		// response surfaces as an ErrorResponse carrying the HTTP StatusCode;
		// the Rust code treats that as a completed request whose status is then
		// checked (event already published), so return the status with no
		// transport error. A zero StatusCode means a genuine transport failure
		// (the Rust `?` on put_object propagates it before publishing).
		if status := minio.ToErrorResponse(err).StatusCode; status != 0 {
			return status, nil
		}
		return 0, err
	}
	return 200, nil
}

// PresignedPath builds the rewritten, reverse-proxied media path for key.
// It pre-signs a GET URL with the configured expiration, then strips the
// scheme+host and prepends PROXY_PATH, keeping the URL path (which includes
// /media-data/{key}) and the full query string:
//
//	{PROXY_PATH}{url.Path}?{url.RawQuery}
//
// This matches the Rust `format!("{}{}?{}", PROXY_PATH, url.path(), url.query())`.
// If the presigned URL somehow has no query, the "?" is still appended
// (trailing "?"), mirroring `url.query().unwrap_or("")`.
func (s *Store) PresignedPath(ctx context.Context, key string) (string, error) {
	presigned, err := s.client.PresignedGetObject(ctx, s.bucket, key, s.expiration, url.Values{})
	if err != nil {
		return "", err
	}
	return s.proxyPath + presigned.Path + "?" + presigned.RawQuery, nil
}

// parseEndpoint splits a MINIO_ENDPOINT URL into the host:port minio-go wants
// and a Secure flag. A bare host:port (no scheme) is treated as insecure http,
// matching the dev/prod composes which set MINIO_ENDPOINT=http://media-minio:9000.
func parseEndpoint(raw string) (endpoint string, secure bool, err error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false, fmt.Errorf("media: parse MINIO_ENDPOINT %q: %w", raw, err)
	}
	switch u.Scheme {
	case "https":
		return u.Host, true, nil
	case "http":
		return u.Host, false, nil
	case "":
		// No scheme: the whole value is host:port (url.Parse puts it in Path).
		host := u.Host
		if host == "" {
			host = u.Path
		}
		return host, false, nil
	default:
		return "", false, fmt.Errorf("media: unsupported MINIO_ENDPOINT scheme %q", u.Scheme)
	}
}
