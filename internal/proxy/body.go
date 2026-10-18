package proxy

import (
	"bytes"
	"io"
)

type bodyCapture struct {
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
