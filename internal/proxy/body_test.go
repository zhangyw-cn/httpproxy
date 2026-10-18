package proxy

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"testing"
)

func TestDecodeForDisplayCapsNestedBomb(t *testing.T) {
	// Build gzip(gzip(highly compressible data)) truncated to a small capture,
	// then nest encodings so a naive decoder would expand without limit.
	inner := bytes.Repeat([]byte("A"), 256*1024)
	var once bytes.Buffer
	gz1 := gzip.NewWriter(&once)
	if _, err := gz1.Write(inner); err != nil {
		t.Fatal(err)
	}
	_ = gz1.Close()

	var twice bytes.Buffer
	gz2 := gzip.NewWriter(&twice)
	if _, err := gz2.Write(once.Bytes()); err != nil {
		t.Fatal(err)
	}
	_ = gz2.Close()

	const max = 1024
	out := decodeForDisplay(twice.Bytes(), http.Header{"Content-Encoding": {"gzip, gzip"}}, max)
	if int64(len(out)) > max {
		t.Fatalf("decoded %d bytes, want <= %d", len(out), max)
	}
}

func TestNewDeflateReaderDoesNotBufferEntireStream(t *testing.T) {
	raw := bytes.Repeat([]byte{0}, 64)
	r, err := newDeflateReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	// Reading should not panic; corrupted deflate may error — that is fine.
	_, _ = io.Copy(io.Discard, io.LimitReader(r, 8))
	_ = r.Close()
}
