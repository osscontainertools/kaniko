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

package mounts

import (
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

func TestMountableImageFreezesSources(t *testing.T) {
	oldSources := sources
	defer func() { sources = oldSources }()
	sources = map[v1.Hash][]name.Repository{}

	img, err := random.Image(1024, 1)
	if err != nil {
		t.Fatalf("random.Image: %v", err)
	}
	wrapper := MountableImage(img, "registry.example")

	repo, err := name.NewRepository("registry.example/source", name.StrictValidation)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	RecordImage(img, repo)
	layers, err := wrapper.Layers()
	if err != nil {
		t.Fatalf("Layers: %v", err)
	}
	if _, ok := layers[0].(*remote.MountableLayer); ok {
		t.Fatal("wrapper observed a source recorded after construction")
	}
}
