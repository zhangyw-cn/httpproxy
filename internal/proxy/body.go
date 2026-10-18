package proxy

import (
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

// prepareRequestBody buffers at most max+1 bytes of the client body so the
// created Flow can show it, then streams the remainder to upstream while the
// capture keeps counting. On success it replaces req.Body (and fixes
// ContentLength when the whole body fit in the prefix).
func prepareRequestBody(req *http.Request, max int64) (*bodyCapture, error) {
	capture := newBodyCapture(max)
	if req.Body == nil || req.Body == http.NoBody {
		return capture, nil
	}
	prefix, err := io.ReadAll(io.LimitReader(req.Body, max+1))
	_, _ = capture.Write(prefix)
	if err != nil {
		return capture, err
	}
	if int64(len(prefix)) <= max {
		req.ContentLength = int64(len(prefix))
		if len(prefix) == 0 {
			req.Body = http.NoBody
		} else {
			req.Body = io.NopCloser(bytes.NewReader(prefix))
		}
		return capture, nil
	}
	rest := io.TeeReader(req.Body, capture)
	req.Body = io.NopCloser(io.MultiReader(bytes.NewReader(prefix), rest))
	return capture, nil
}

// decodeForDisplay returns a decompressed copy of a captured body for the UI,
// capped at max bytes. A truncated capture yields a best-effort partial decode.
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
			r = gz
		case "deflate":
			r = newDeflateReader(r)
		case "br":
			r = brotli.NewReader(r)
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

// newDeflateReader accepts both zlib-wrapped (RFC 1950) and raw deflate data,
// since servers send either for "Content-Encoding: deflate".
func newDeflateReader(r io.Reader) io.Reader {
	data, err := io.ReadAll(r)
	if err != nil && len(data) == 0 {
		return bytes.NewReader(nil)
	}
	if len(data) >= 2 && data[0]&0x0f == 8 && (uint16(data[0])<<8|uint16(data[1]))%31 == 0 {
		return flate.NewReader(bytes.NewReader(data[2:]))
	}
	return flate.NewReader(bytes.NewReader(data))
}
