/*
Copyright 2026 OSS Container Tools

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package util

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const maxStalledResumes = 5

type resumeTransport struct {
	inner http.RoundTripper
}

func (t resumeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.inner.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	blob := blobPath(req)
	if req.Method == http.MethodGet && resp.StatusCode == http.StatusOK && req.Header.Get("Range") == "" && blob != "" {
		resp.Body = &resumingBody{
			inner: t.inner,
			req:   req,
			body:  resp.Body,
			blob:  path.Base(blob),
		}
	}
	return resp, nil
}

// registries redirect blob downloads to storage under another URL
func blobPath(req *http.Request) string {
	for r := req; r != nil; {
		if strings.Contains(r.URL.Path, "/blobs/") {
			return r.URL.Path
		}
		if r.Response == nil {
			return ""
		}
		r = r.Response.Request
	}
	return ""
}

type resumingBody struct {
	inner   http.RoundTripper
	req     *http.Request
	body    io.ReadCloser
	blob    string
	offset  int64
	stalled int
}

func (b *resumingBody) Read(p []byte) (int, error) {
	for {
		n, err := b.body.Read(p)
		b.offset += int64(n)
		if n > 0 {
			b.stalled = 0
		}
		if err == nil || errors.Is(err, io.EOF) {
			return n, err
		}
		b.body.Close()
		err = b.resume(err)
		if err != nil {
			return n, err
		} else if n > 0 {
			return n, nil
		}
	}
}

func (b *resumingBody) resume(cut error) error {
	var openErr error
	for {
		b.stalled++
		if b.stalled > maxStalledResumes && openErr != nil {
			return fmt.Errorf("blob %s cut after %d bytes: %w, resuming: %w", b.blob, b.offset, cut, openErr)
		} else if b.stalled > maxStalledResumes {
			return fmt.Errorf("blob %s cut after %d bytes: %w", b.blob, b.offset, cut)
		}
		delay := time.Duration(1<<(b.stalled-1)) * time.Second
		logrus.Warnf("resuming blob %s at byte %d in %s: %v", b.blob, b.offset, delay, cut)
		select {
		case <-b.req.Context().Done():
			return fmt.Errorf("blob %s cut after %d bytes: %w", b.blob, b.offset, cut)
		case <-time.After(delay):
		}
		body, err := b.open()
		if err != nil {
			openErr = err
		} else {
			b.body = body
			return nil
		}
	}
}

func (b *resumingBody) open() (io.ReadCloser, error) {
	req := b.req.Clone(b.req.Context())
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-", b.offset))
	resp, err := b.inner.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == http.StatusPartialContent && strings.HasPrefix(resp.Header.Get("Content-Range"), fmt.Sprintf("bytes %d-", b.offset)):
		return resp.Body, nil
	case resp.StatusCode == http.StatusOK:
		// the server ignored Range and sends the blob from its first byte
		_, err = io.CopyN(io.Discard, resp.Body, b.offset)
		if err != nil {
			resp.Body.Close()
			return nil, err
		}
		return resp.Body, nil
	default:
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected %s, Content-Range %q", resp.Status, resp.Header.Get("Content-Range"))
	}
}

func (b *resumingBody) Close() error {
	return b.body.Close()
}
