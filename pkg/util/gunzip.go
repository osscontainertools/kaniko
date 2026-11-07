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
	"bufio"
	"io"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/klauspost/compress/gzip"
)

const (
	readAheadChunkSize = 1 << 20
	readAheadChunks    = 16
)

// gunzipReadAhead returns the uncompressed stream of l. Registry gzip layers
// are decompressed with klauspost on a separate goroutine, every other layer
// goes through l.Uncompressed().
func gunzipReadAhead(l v1.Layer) (io.ReadCloser, error) {
	mediaType, err := l.MediaType()
	if err != nil {
		return nil, err
	}
	// Compressed() of a layer backed by an uncompressed tarball, like a
	// snapshot of an earlier stage, gzips it first. Only registry layers are
	// known to serve the stored blob.
	_, fromRegistry := l.(*remote.MountableLayer)
	switch {
	case !fromRegistry:
		return l.Uncompressed()
	case mediaType != types.DockerLayer && mediaType != types.OCILayer && mediaType != types.DockerForeignLayer && mediaType != types.OCIRestrictedLayer:
		return l.Uncompressed()
	}

	rc, err := l.Compressed()
	if err != nil {
		return nil, err
	}
	br := bufio.NewReaderSize(rc, readAheadChunkSize)
	magic, err := br.Peek(2)
	if err != nil {
		rc.Close()
		return nil, err
	}
	// ggcr picks the decompressor from the content, not the media type.
	if magic[0] != 0x1f || magic[1] != 0x8b {
		rc.Close()
		return l.Uncompressed()
	}
	gz, err := gzip.NewReader(br)
	if err != nil {
		rc.Close()
		return nil, err
	}
	ra := &readAhead{
		chunks: make(chan []byte, readAheadChunks),
		free:   make(chan []byte, readAheadChunks+2),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
		src:    rc,
	}
	go ra.fill(gz)
	return ra, nil
}

// readAhead reads its source in chunks on a separate goroutine so that the
// consumer does not wait on decompression.
type readAhead struct {
	chunks chan []byte
	free   chan []byte
	stop   chan struct{}
	done   chan struct{}
	src    io.Closer
	buf    []byte
	cur    []byte
	// err is written by fill before chunks is closed.
	err error
}

func (ra *readAhead) fill(r io.Reader) {
	defer close(ra.done)
	defer close(ra.chunks)
	for {
		var buf []byte
		select {
		case buf = <-ra.free:
		default:
			buf = make([]byte, readAheadChunkSize)
		}
		n := 0
		var err error
		for n < len(buf) && err == nil {
			var m int
			m, err = r.Read(buf[n:])
			n += m
		}
		if n > 0 {
			select {
			case ra.chunks <- buf[:n]:
			case <-ra.stop:
				return
			}
		}
		if err != nil {
			ra.err = err
			return
		}
	}
}

func (ra *readAhead) Read(p []byte) (int, error) {
	if len(ra.cur) == 0 {
		if ra.buf != nil {
			select {
			case ra.free <- ra.buf[:cap(ra.buf)]:
			default:
			}
			ra.buf = nil
		}
		chunk, ok := <-ra.chunks
		if !ok {
			return 0, ra.err
		}
		ra.buf = chunk
		ra.cur = chunk
	}
	n := copy(p, ra.cur)
	ra.cur = ra.cur[n:]
	return n, nil
}

// Close closes the source first, so that a fill blocked on a read returns.
func (ra *readAhead) Close() error {
	close(ra.stop)
	err := ra.src.Close()
	<-ra.done
	return err
}
