package graph

import (
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"

	"github.com/99designs/gqlgen/graphql"
	"github.com/google/uuid"
)

// This file maps object-store keys to GraphQL models. There is no relational
// store: a Media's identity is encoded in its object key's file stem
// (`{uuid}.{ext}`) and recovered by parsing. Media.Path is intentionally left
// unset here — it is resolved lazily by the Media.path field resolver, which
// performs the live S3 list + pre-sign only when a client selects `path`.

// mediaFromKey builds a Media from an object key, mirroring the Rust
// `Media::try_from(&str)`:
//
//   - Treat the key as a filesystem path and take its file stem (the base
//     name minus the final extension). If the key has no name → error
//     "File in bucket does not have a name."
//   - Parse the stem as a UUID; a parse failure propagates as the query error.
//
// Examples: "f47ac10b-…-d479.png" → stem "f47ac10b-…-d479" → Media{id}.
// A key with multiple dots, e.g. "a.b.png", has stem "a.b" → UUID parse fails →
// the whole query errors (matches the original; foreign keys only).
func mediaFromKey(key string) (*Media, error) {
	stem, ok := fileStem(key)
	if !ok {
		return nil, fmt.Errorf("File in bucket does not have a name.")
	}
	id, err := uuid.Parse(stem)
	if err != nil {
		return nil, err
	}
	return &Media{ID: id}, nil
}

// fileStem returns the file stem of a key the way Rust's Path::file_stem does:
// the base name (component after the last '/') with the final extension
// removed. A dotfile with no other extension (e.g. ".bashrc") keeps its whole
// name as the stem. It reports ok=false when the key has no file name
// component (empty, or "."/"..") — mapping to the "does not have a name" error.
func fileStem(key string) (string, bool) {
	base := filepath.Base(key)
	if base == "" || base == "." || base == ".." || base == string(filepath.Separator) {
		return "", false
	}
	ext := filepath.Ext(base)
	// filepath.Ext(".bashrc") == ".bashrc": a leading-dot name has no
	// extension, so its stem is the whole name (matches Path::file_stem).
	if ext == base {
		return base, true
	}
	return strings.TrimSuffix(base, ext), true
}

// toMediaConnection assembles a MediaConnection from a page of keys mapped to
// Media nodes plus the offset-pagination bookkeeping the caller computed.
func toMediaConnection(nodes []Media, hasNextPage bool, totalCount int) *MediaConnection {
	return &MediaConnection{
		Nodes:       nodes,
		HasNextPage: hasNextPage,
		TotalCount:  totalCount,
	}
}

// mimeSubtype parses a Content-Type value as a MIME type and returns its
// SUBTYPE, used verbatim as the uploaded object's file extension (mirrors the
// Rust `content_type.parse::<mime::Mime>()?.subtype()`).
//
// The subtype is the part after '/', truncated at a '+' suffix — exactly what
// `mime` 0.3.17's `Mime::subtype()` returns (its slice ends at the '+' index):
//
//	image/png       → png
//	image/jpeg      → jpeg   (NOT jpg — it's the MIME subtype, not the filename ext)
//	application/pdf → pdf
//	text/plain      → plain
//	image/svg+xml   → svg    (subtype stops at '+'; the "+xml" is the suffix())
//
// A value that fails to parse as a MIME type is an error (as in Rust).
func mimeSubtype(contentType string) (string, error) {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", err
	}
	slash := strings.IndexByte(mediaType, '/')
	if slash < 0 {
		return "", fmt.Errorf("invalid media type: %q", contentType)
	}
	subtype := mediaType[slash+1:]
	// mime::subtype() truncates the subtype at the '+' suffix boundary.
	if plus := strings.IndexByte(subtype, '+'); plus >= 0 {
		subtype = subtype[:plus]
	}
	return subtype, nil
}

// readAll reads the entire uploaded file into an in-memory buffer, mirroring
// the Rust `read_to_end` (the whole file is buffered in memory; nothing is
// streamed to S3).
func readAll(upload graphql.Upload) ([]byte, error) {
	return io.ReadAll(upload.File)
}
