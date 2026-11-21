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

	"github.com/sirupsen/logrus"
)

// maxStalledResumes bounds the resumes in a row that deliver no bytes.
const maxStalledResumes = 5

// resumeTransport hands out blob bodies that pick up a cut download with a Range request.
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

// blobPath returns the registry blob path req fetches, following the redirects that
// led to it, since registries hand blob downloads to storage under another URL.
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
		switch {
		case err == nil, errors.Is(err, io.EOF):
			return n, err
		case b.req.Context().Err() != nil, b.stalled >= maxStalledResumes:
			return n, fmt.Errorf("blob %s cut after %d bytes: %w", b.blob, b.offset, err)
		}
		logrus.Warnf("resuming blob %s at byte %d: %v", b.blob, b.offset, err)
		b.stalled++
		b.body.Close()
		body, resumeErr := RetryWithResult(b.resume, maxStalledResumes-1, 1000)
		if resumeErr != nil {
			return n, fmt.Errorf("blob %s cut after %d bytes: %w, resuming: %w", b.blob, b.offset, err, resumeErr)
		}
		b.body = body
		if n > 0 {
			return n, nil
		}
	}
}

func (b *resumingBody) resume() (io.ReadCloser, error) {
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
