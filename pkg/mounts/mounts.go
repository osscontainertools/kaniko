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

// Package mounts remembers which repositories hold a layer, so a push can have the
// destination registry mount a blob instead of taking the bytes.
//
// remote.Write only offers a mount for layers still carrying the reference they were pulled
// from, which every local copy kaniko makes throws away. Keyed by digest rather than held on
// the layer, so a copy that keeps the bytes keeps the entry and one that rewrites them misses.
//
// Only repositories this process read from or wrote to belong here.
// A mount candidate is best-effort: if its authorization or the mount attempt fails,
// the executor retries the push with a plain blob upload.
package mounts

import (
	"maps"
	"sync"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

var (
	mu      sync.Mutex
	sources = map[v1.Hash][]name.Repository{}
)

func RecordImage(img v1.Image, repo name.Repository) {
	layers, err := img.Layers()
	if err != nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	for _, l := range layers {
		digest, err := l.Digest()
		if err == nil {
			// One repository per registry is all a push can ever use.
			_, known := Mountable(sources[digest], repo.RegistryStr())
			if !known {
				sources[digest] = append(sources[digest], repo)
			}
		}
	}
}

// PlannedDigest names a layer that does not exist yet by the cache key that decides where it
// will end up. Cache keys are sha256 hex like a digest, and never collide with one in practice.
func PlannedDigest(cacheKey string) v1.Hash {
	return v1.Hash{Algorithm: "sha256", Hex: cacheKey}
}

// Snapshot copies the map, not the slices in it. Those are shared and read-only: extend an
// entry by replacing it, never by appending into the one that is there.
func Snapshot() map[v1.Hash][]name.Repository {
	mu.Lock()
	defer mu.Unlock()
	return maps.Clone(sources)
}

// Mountable returns one of the repositories known to hold a layer that sits on registry.
// Cross-registry origins are not honoured in practice, so a source elsewhere is no source.
func Mountable(repos []name.Repository, registry string) (name.Repository, bool) {
	for _, repo := range repos {
		if repo.RegistryStr() == registry {
			return repo, true
		}
	}
	return name.Repository{}, false
}

// MountableImage returns an image whose layers carry stable mount candidates recorded for registry.
// The source snapshot is taken at construction time,
// so preflight and the subsequent remote.Write observe the same mount plan.
func MountableImage(img v1.Image, registry string) v1.Image {
	return &mountableImage{
		Image:    img,
		registry: registry,
		sources:  Snapshot(),
	}
}

type mountableImage struct {
	v1.Image

	registry string
	sources  map[v1.Hash][]name.Repository
}

// Layers is the only accessor remote.Write reads to decide what it sends, so LayerByDigest
// and LayerByDiffID are deliberately left untagged.
func (m *mountableImage) Layers() ([]v1.Layer, error) {
	layers, err := m.Image.Layers()
	if err != nil {
		return nil, err
	}
	tagged := make([]v1.Layer, 0, len(layers))
	for _, l := range layers {
		layer := l
		digest, err := l.Digest()
		if err == nil {
			// Use the frozen plan rather than the global registry of sources.
			// A later cache read or push must not change this push's candidates.
			repo, ok := Mountable(m.sources[digest], m.registry)
			if ok {
				layer = &remote.MountableLayer{Layer: l, Reference: repo.Digest(digest.String())}
			}
		}
		tagged = append(tagged, layer)
	}
	return tagged, nil
}
