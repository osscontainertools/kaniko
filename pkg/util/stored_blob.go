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
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
)

// StoredBlobLayer marks a layer whose Compressed() reads the stored blob.
type StoredBlobLayer struct {
	v1.Layer
}

// Descriptor keeps the URLs and annotations of the stored descriptor, which
// embedding alone would hide from ggcr.
func (l *StoredBlobLayer) Descriptor() (*v1.Descriptor, error) {
	return partial.Descriptor(l.Layer)
}

// WithStoredBlobs marks every layer of img as a StoredBlobLayer. img must be
// read from an OCI layout.
func WithStoredBlobs(img v1.Image) v1.Image {
	return &storedBlobImage{Image: img}
}

type storedBlobImage struct {
	v1.Image
}

func (i *storedBlobImage) Layers() ([]v1.Layer, error) {
	layers, err := i.Image.Layers()
	if err != nil {
		return nil, err
	}
	out := make([]v1.Layer, len(layers))
	for j, l := range layers {
		out[j] = &StoredBlobLayer{Layer: l}
	}
	return out, nil
}

func (i *storedBlobImage) LayerByDigest(h v1.Hash) (v1.Layer, error) {
	l, err := i.Image.LayerByDigest(h)
	if err != nil {
		return nil, err
	}
	return &StoredBlobLayer{Layer: l}, nil
}

func (i *storedBlobImage) LayerByDiffID(h v1.Hash) (v1.Layer, error) {
	l, err := i.Image.LayerByDiffID(h)
	if err != nil {
		return nil, err
	}
	return &StoredBlobLayer{Layer: l}, nil
}
