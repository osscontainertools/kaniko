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
// Only repositories this process read from or wrote to belong here. remote.Write fails a push
// outright when the token request for a mount source is refused, so an unproven entry is not
// a missed optimisation but a broken build.
package mounts

import (
	"slices"
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
			_, known := find(sources[digest], func(r name.Repository) bool { return r.String() == repo.String() })
			if !known {
				sources[digest] = append(sources[digest], repo)
			}
		}
	}
}

func find(repos []name.Repository, match func(name.Repository) bool) (name.Repository, bool) {
	i := slices.IndexFunc(repos, match)
	if i < 0 {
		return name.Repository{}, false
	}
	return repos[i], true
}

func Mountable(img v1.Image, registry string) v1.Image {
	return &mountableImage{Image: img, registry: registry}
}

type mountableImage struct {
	v1.Image

	registry string
}

// Cross-registry origins are not honoured in practice, so a source off the registry being
// pushed to is no source at all.
func mountable(l v1.Layer, digest v1.Hash, candidates []name.Repository, registry string) v1.Layer {
	repo, ok := find(candidates, func(r name.Repository) bool { return r.RegistryStr() == registry })
	if !ok {
		return l
	}
	return &remote.MountableLayer{Layer: l, Reference: repo.Digest(digest.String())}
}

// Layers is the only accessor remote.Write reads to decide what it sends, so LayerByDigest
// and LayerByDiffID are deliberately left untagged.
func (m *mountableImage) Layers() ([]v1.Layer, error) {
	layers, err := m.Image.Layers()
	if err != nil {
		return nil, err
	}
	tagged := make([]v1.Layer, 0, len(layers))
	mu.Lock()
	defer mu.Unlock()
	for _, l := range layers {
		digest, err := l.Digest()
		if err == nil {
			tagged = append(tagged, mountable(l, digest, sources[digest], m.registry))
		} else {
			tagged = append(tagged, l)
		}
	}
	return tagged, nil
}
