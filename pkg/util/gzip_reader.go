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
	"bytes"
	"errors"
	"io"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/klauspost/pgzip"
)

type GzipReader func(io.Reader) (io.ReadCloser, error)

func NewParallelGzipReader(r io.Reader) (io.ReadCloser, error) {
	return pgzip.NewReader(r)
}

// WithGzipReader decompresses the gzip layers of img with newReader. The
// Compressed() of every layer of img must read the stored blob, as it does for
// images read from a registry or an OCI layout.
func WithGzipReader(img v1.Image, newReader GzipReader) v1.Image {
	return &gzipReaderImage{Image: img, newReader: newReader}
}

type gzipReaderImage struct {
	v1.Image
	newReader GzipReader
}

// Descriptor keeps the platform and annotations of the stored descriptor,
// which embedding alone would hide from ggcr.
func (i *gzipReaderImage) Descriptor() (*v1.Descriptor, error) {
	return partial.Descriptor(i.Image)
}

func (i *gzipReaderImage) wrap(l v1.Layer) v1.Layer {
	ml, ok := l.(*remote.MountableLayer)
	if ok {
		return &remote.MountableLayer{Layer: &gzipReaderLayer{Layer: ml.Layer, newReader: i.newReader}, Reference: ml.Reference}
	}
	return &gzipReaderLayer{Layer: l, newReader: i.newReader}
}

func (i *gzipReaderImage) Layers() ([]v1.Layer, error) {
	layers, err := i.Image.Layers()
	if err != nil {
		return nil, err
	}
	out := make([]v1.Layer, len(layers))
	for j, l := range layers {
		out[j] = i.wrap(l)
	}
	return out, nil
}

func (i *gzipReaderImage) LayerByDigest(h v1.Hash) (v1.Layer, error) {
	l, err := i.Image.LayerByDigest(h)
	if err != nil {
		return nil, err
	}
	return i.wrap(l), nil
}

func (i *gzipReaderImage) LayerByDiffID(h v1.Hash) (v1.Layer, error) {
	l, err := i.Image.LayerByDiffID(h)
	if err != nil {
		return nil, err
	}
	return i.wrap(l), nil
}

type gzipReaderLayer struct {
	v1.Layer
	newReader GzipReader
}

// Descriptor keeps the URLs and annotations of the stored descriptor, which
// embedding alone would hide from ggcr.
func (l *gzipReaderLayer) Descriptor() (*v1.Descriptor, error) {
	return partial.Descriptor(l.Layer)
}

var gzipMagic = []byte{0x1f, 0x8b}

func (l *gzipReaderLayer) Uncompressed() (io.ReadCloser, error) {
	rc, err := l.Compressed()
	if err != nil {
		return nil, err
	}
	br := bufio.NewReader(rc)
	magic, err := br.Peek(len(gzipMagic))
	switch {
	case err != nil && !errors.Is(err, io.EOF):
		rc.Close()
		return nil, err
	case !bytes.Equal(magic, gzipMagic):
		// ggcr picks the decompressor from the content, not the media type.
		rc.Close()
		return l.Layer.Uncompressed()
	}
	gz, err := l.newReader(br)
	if err != nil {
		rc.Close()
		return nil, err
	}
	return &gzipReadCloser{Reader: gz, gz: gz, src: rc}, nil
}

type gzipReadCloser struct {
	io.Reader
	gz  io.Closer
	src io.Closer
}

// Close closes src first, since pgzip's Close waits for its read-ahead, which
// can be blocked reading src.
func (r *gzipReadCloser) Close() error {
	err := r.src.Close()
	// the decompressor reports the read error of the src closed above
	_ = r.gz.Close()
	return err
}
