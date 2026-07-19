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

package executor

import (
	"io"
	"sync/atomic"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// downloadTimer accumulates the time spent reading (fetching plus decompressing)
// base-layer bytes. A remote layer is streamed on demand while it is being
// extracted, so read time and disk-write time interleave and cannot be two
// separate spans. Measuring read time lets the FS-unpack span report how much of
// it was network plus decompress versus local disk.
type downloadTimer struct{ ns atomic.Int64 }

func (d *downloadTimer) add(t time.Duration)    { d.ns.Add(int64(t)) }
func (d *downloadTimer) elapsed() time.Duration { return time.Duration(d.ns.Load()) }

// timedImage wraps an image so reads of its layers feed a downloadTimer.
type timedImage struct {
	v1.Image
	dl *downloadTimer
}

func (i timedImage) Layers() ([]v1.Layer, error) {
	layers, err := i.Image.Layers()
	if err != nil {
		return nil, err
	}
	wrapped := make([]v1.Layer, len(layers))
	for idx, l := range layers {
		wrapped[idx] = timedLayer{Layer: l, dl: i.dl}
	}
	return wrapped, nil
}

type timedLayer struct {
	v1.Layer
	dl *downloadTimer
}

func (l timedLayer) Uncompressed() (io.ReadCloser, error) {
	rc, err := l.Layer.Uncompressed()
	if err != nil {
		return nil, err
	}
	return timedReadCloser{ReadCloser: rc, dl: l.dl}, nil
}

type timedReadCloser struct {
	io.ReadCloser
	dl *downloadTimer
}

func (r timedReadCloser) Read(p []byte) (int, error) {
	start := time.Now()
	n, err := r.ReadCloser.Read(p)
	r.dl.add(time.Since(start))
	return n, err
}
