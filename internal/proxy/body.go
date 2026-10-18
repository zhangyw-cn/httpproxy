package proxy

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/andybalholm/brotli"
)

// bodyCapture is written by the upstream transport goroutine while the
// handler may read results, so access is serialized.
type bodyCapture struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	max   int64
	total int64
}

func newBodyCapture(max int64) *bodyCapture {
	if max < 0 {
		max = 0
	}
	return &bodyCapture{max: max}
}

func (c *bodyCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := len(p)
	c.total += int64(n)

	remaining := c.max - int64(c.buf.Len())
	if remaining > 0 {
		toStore := int64(n)
		if toStore > remaining {
			toStore = remaining
		}
		_, _ = c.buf.Write(p[:int(toStore)])
	}
	return n, nil
}

func (c *bodyCapture) result() (data []byte, total int64, truncated bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.buf.Bytes()...), c.total, c.total > c.max
}

func readLimited(r io.Reader, max int64) (data []byte, total int64, truncated bool, err error) {
	if r == nil {
		return nil, 0, false, nil
	}
	capture := newBodyCapture(max)
	_, err = io.Copy(capture, r)
	data, total, truncated = capture.result()
	return data, total, truncated, err
}

type teeReadCloser struct {
	io.Reader
	io.Closer
}

// streamRequestBody tees the client body into a capture while forwarding
// upstream immediately (no prefix buffering). ReqBody is filled as bytes pass
// through; OnCreated may see an empty body until OnUpdated.
func streamRequestBody(req *http.Request, max int64) *bodyCapture {
	capture := newBodyCapture(max)
	if req.Body == nil || req.Body == http.NoBody {
		return capture
	}
	body := req.Body
	req.Body = &teeReadCloser{
		Reader: io.TeeReader(body, capture),
		Closer: body,
	}
	return capture
}

// decodeForDisplay returns a decompressed copy of a captured body for the UI,
// capped at max bytes. Nested encodings are applied with a per-layer output
// limit so a compression bomb cannot expand past max.
func decodeForDisplay(body []byte, headers http.Header, max int64) []byte {
	if len(body) == 0 || max <= 0 {
		return nil
	}
	encodings := strings.Split(headers.Get("Content-Encoding"), ",")
	var r io.Reader = bytes.NewReader(body)
	decoded := false
	for i := len(encodings) - 1; i >= 0; i-- {
		switch strings.ToLower(strings.TrimSpace(encodings[i])) {
		case "", "identity":
			continue
		case "gzip", "x-gzip":
			gz, err := gzip.NewReader(r)
			if err != nil {
				return nil
			}
			// Cap each layer's decompressed output to stop nested bombs.
			r = io.LimitReader(gz, max)
		case "deflate":
			dr, err := newDeflateReader(r)
			if err != nil {
				return nil
			}
			r = io.LimitReader(dr, max)
		case "br":
			r = io.LimitReader(brotli.NewReader(r), max)
		default:
			return nil
		}
		decoded = true
	}
	if !decoded {
		return nil
	}
	out, _ := io.ReadAll(io.LimitReader(r, max))
	if len(out) == 0 {
		return nil
	}
	return out
}

// newDeflateReader accepts both zlib-wrapped (RFC 1950) and raw deflate data.
// It peeks at most 2 bytes and never buffers the whole stream.
func newDeflateReader(r io.Reader) (io.ReadCloser, error) {
	br := bufio.NewReader(r)
	hdr, err := br.Peek(2)
	if err != nil && err != io.EOF && len(hdr) < 2 {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	if len(hdr) >= 2 && hdr[0]&0x0f == 8 && (uint16(hdr[0])<<8|uint16(hdr[1]))%31 == 0 {
		_, _ = br.Discard(2)
		return flate.NewReader(br), nil
	}
	return flate.NewReader(br), nil
}
