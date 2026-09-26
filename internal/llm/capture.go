package llm

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/anthropics/anthropic-sdk-go/option"
)

const CaptureDir = "results/requests"

type Capture struct {
	dir string
	seq atomic.Int64
}

func NewCapture(dir string) *Capture {
	if dir == "" {
		dir = CaptureDir
	}
	os.MkdirAll(dir, 0o755)
	c := &Capture{dir: dir}
	c.seq.Store(scanCaptureSeq(dir))
	return c
}

func scanCaptureSeq(dir string) int64 {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var max int64
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		digits := ""
		for _, r := range name {
			if r < '0' || r > '9' {
				break
			}
			digits += string(r)
		}
		if digits == "" {
			continue
		}
		n, err := strconv.ParseInt(digits, 10, 64)
		if err == nil && n > max {
			max = n
		}
	}
	return max
}

func (c *Capture) Dir() string {
	return c.dir
}

func (c *Capture) Save(body []byte) (string, error) {
	n := c.seq.Add(1)
	path := filepath.Join(c.dir, fmt.Sprintf("%04d.json", n))
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func (c *Capture) Middleware() option.RequestOption {
	return option.WithMiddleware(func(r *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		if r.Body != nil {
			data, err := io.ReadAll(r.Body)
			r.Body.Close()
			if err == nil {
				c.Save(data)
				r.Body = io.NopCloser(bytes.NewReader(data))
				r.ContentLength = int64(len(data))
				r.GetBody = func() (io.ReadCloser, error) {
					return io.NopCloser(bytes.NewReader(data)), nil
				}
			} else {
				r.Body = io.NopCloser(bytes.NewReader(nil))
			}
		}
		return next(r)
	})
}
