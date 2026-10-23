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
	"errors"
	"fmt"
	"os"

	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

func LoadImage(path string) (*Output, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return loadFromOCILayout(path)
	}
	img, err := tarball.ImageFromPath(path, nil)
	if err != nil {
		return nil, err
	}
	return &Output{Image: img}, nil
}

func loadFromOCILayout(path string) (*Output, error) {
	idx, err := layout.ImageIndexFromPath(path)
	if err != nil {
		return nil, fmt.Errorf("reading OCI layout: %w", err)
	}
	manifest, err := idx.IndexManifest()
	if err != nil {
		return nil, fmt.Errorf("reading OCI index manifest: %w", err)
	}
	if len(manifest.Manifests) != 1 {
		return nil, fmt.Errorf("OCI layout must contain exactly one image, found %d", len(manifest.Manifests))
	}
	desc := manifest.Manifests[0]
	if !desc.MediaType.IsIndex() {
		img, err := idx.Image(desc.Digest)
		if err != nil {
			return nil, err
		}
		return &Output{Image: img}, nil
	}
	pushed, err := idx.ImageIndex(desc.Digest)
	if err != nil {
		return nil, err
	}
	children, err := pushed.IndexManifest()
	if err != nil {
		return nil, err
	}
	for _, child := range children.Manifests {
		if child.Annotations["vnd.docker.reference.type"] != "attestation-manifest" {
			img, err := pushed.Image(child.Digest)
			if err != nil {
				return nil, err
			}
			return &Output{Image: img, Index: pushed}, nil
		}
	}
	return nil, errors.New("OCI layout index holds no image")
}
