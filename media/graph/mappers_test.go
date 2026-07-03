package graph

import "testing"

// TestMimeSubtype locks in the exact behavior of Rust `mime` 0.3.17's
// Mime::subtype(): the subtype is the part after '/', TRUNCATED at a '+'
// suffix. In particular image/svg+xml → "svg" (NOT "svg+xml"): the media spec
// §3.5 text is wrong on this point; the mime-crate source (subtype() slices
// [slash+1 .. plus]) is authoritative and reproduced here.
func TestMimeSubtype(t *testing.T) {
	cases := []struct {
		contentType string
		want        string
		wantErr     bool
	}{
		{"image/png", "png", false},
		{"image/jpeg", "jpeg", false}, // MIME subtype, not the "jpg" filename ext
		{"application/pdf", "pdf", false},
		{"text/plain", "plain", false},
		{"image/svg+xml", "svg", false}, // subtype stops at '+'
		{"application/vnd.api+json", "vnd.api", false},
		{"text/plain; charset=utf-8", "plain", false}, // parameters stripped
		{"not-a-mime", "", true},
		{"", "", true},
	}
	for _, c := range cases {
		got, err := mimeSubtype(c.contentType)
		if c.wantErr {
			if err == nil {
				t.Errorf("mimeSubtype(%q): expected error, got subtype %q", c.contentType, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("mimeSubtype(%q): unexpected error %v", c.contentType, err)
			continue
		}
		if got != c.want {
			t.Errorf("mimeSubtype(%q) = %q, want %q", c.contentType, got, c.want)
		}
	}
}

// TestMediaFromKey verifies the key → Media mapping mirrors the Rust
// `Media::try_from(&str)`: take the file stem (base name minus final
// extension), parse it as a UUID; a multi-dot key or a nameless key errors.
func TestMediaFromKey(t *testing.T) {
	const id = "f47ac10b-58cc-4372-a567-0e02b2c3d479"

	m, err := mediaFromKey(id + ".png")
	if err != nil {
		t.Fatalf("mediaFromKey(%q.png): unexpected error %v", id, err)
	}
	if m.ID.String() != id {
		t.Errorf("mediaFromKey stem parse = %s, want %s", m.ID, id)
	}

	// A multi-dot key has stem "<id>.png" (only the last ext is stripped) → the
	// stem is not a valid UUID → error (matches the Rust edge case).
	if _, err := mediaFromKey(id + ".png.bak"); err == nil {
		t.Errorf("mediaFromKey(multi-dot): expected UUID parse error, got nil")
	}

	// A key that is only a directory / has no file name → "does not have a name".
	if _, err := mediaFromKey(""); err == nil {
		t.Errorf("mediaFromKey(empty): expected error, got nil")
	}
	if _, err := mediaFromKey("."); err == nil {
		t.Errorf("mediaFromKey(dot): expected error, got nil")
	}

	// A non-UUID stem errors.
	if _, err := mediaFromKey("hello.png"); err == nil {
		t.Errorf("mediaFromKey(non-uuid): expected UUID parse error, got nil")
	}
}
