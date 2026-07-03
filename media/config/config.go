// Package config reads the media service configuration from the environment
// once at startup, mirroring the Rust service's env handling.
//
// The Rust service reads MINIO_ENDPOINT eagerly (unwrap → panic if unset) and
// PATH_EXPIRATION_TIME / PROXY_PATH lazily via once_cell::Lazy (parsed on first
// use, then cached for process lifetime). Reading them all once here reproduces
// that "read once, cache for the process" semantics. MONGODB_URI is dead config
// (read by nothing) and is intentionally ignored.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Defaults mirror the Rust once_cell fallbacks.
const (
	// DefaultPathExpirationTime is the pre-signed GET URL validity (seconds)
	// when PATH_EXPIRATION_TIME is unset or unparseable: 1 day.
	DefaultPathExpirationTime uint32 = 86400
	// DefaultProxyPath is the reverse-proxy prefix injected into the returned
	// media path when PROXY_PATH is unset.
	DefaultProxyPath = "/api/media"
	// BucketName is the hardcoded MinIO bucket, matching the Rust service.
	BucketName = "media-data"
	// Region is the hardcoded MinIO region, matching the Rust service and the
	// MINIO_REGION set on the MinIO container.
	Region = "eu-central-1"
	// AccessKey / SecretKey are the hardcoded MinIO credentials from the Rust
	// service (matching the container's MINIO_ROOT_USER / MINIO_ROOT_PASSWORD).
	AccessKey = "admin"
	SecretKey = "password"
)

// Config holds the media service's runtime configuration.
type Config struct {
	// MinioEndpoint is the MinIO/S3 endpoint URL (e.g. http://media-minio:9000).
	// Required — the Rust service unwrap()s it, panicking if unset.
	MinioEndpoint string
	// PathExpirationTime is the pre-signed GET URL validity in seconds
	// (X-Amz-Expires). Parsed from PATH_EXPIRATION_TIME as u32; unset or
	// unparseable → DefaultPathExpirationTime.
	PathExpirationTime uint32
	// ProxyPath is the path prefix injected into the returned media path,
	// replacing the presigned URL's scheme+host. Unset → DefaultProxyPath.
	ProxyPath string
}

// Load reads the media configuration from the environment. It returns an error
// if MINIO_ENDPOINT is unset (the Rust service panics in that case; a Go port
// should fail fast with a clear message instead of dereferencing a nil handle).
func Load() (Config, error) {
	endpoint, ok := os.LookupEnv("MINIO_ENDPOINT")
	if !ok || endpoint == "" {
		return Config{}, fmt.Errorf("MINIO_ENDPOINT is not set")
	}

	// PATH_EXPIRATION_TIME: parse as u32; on missing/parse error → default.
	// Matches the Rust `.ok().and_then(parse::<u32>().ok()).unwrap_or(86400)`.
	expiration := DefaultPathExpirationTime
	if raw, ok := os.LookupEnv("PATH_EXPIRATION_TIME"); ok {
		if v, err := strconv.ParseUint(raw, 10, 32); err == nil {
			expiration = uint32(v)
		}
	}

	proxyPath := DefaultProxyPath
	if raw, ok := os.LookupEnv("PROXY_PATH"); ok {
		proxyPath = raw
	}

	return Config{
		MinioEndpoint:      endpoint,
		PathExpirationTime: expiration,
		ProxyPath:          proxyPath,
	}, nil
}
