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

package image

import (
	"encoding/json"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/osscontainertools/kaniko/pkg/util"
)

// ReplaceBase returns img with its first len(base.Layers()) layers and first
// len(base.ConfigFile().History) history entries replaced by base's. img's
// full top-level ConfigFile (Architecture/OS, Created, Container, the inner
// Config.Cmd/Env/Entrypoint/Labels/…) is carried through unchanged.
//
// Unlike mutate.Rebase, this has no digest-prefix verifier and no os/arch
// swap — it's a pure layer/history splice that trusts the caller's invariant
// that img.Layers()[:len(base.Layers())] are the recipient of base.
func ReplaceBase(img, base v1.Image) (v1.Image, error) {
	imgCfg, err := img.ConfigFile()
	if err != nil {
		return nil, err
	}
	imgLayers, err := img.Layers()
	if err != nil {
		return nil, err
	}
	baseCfg, err := base.ConfigFile()
	if err != nil {
		return nil, err
	}
	baseLayers, err := base.Layers()
	if err != nil {
		return nil, err
	}

	// img must have at least as many layers as base; otherwise base can't be a prefix of img.
	util.Assert("image.replacebase.base-layer-fit", len(baseLayers) <= len(imgLayers),
		"base has %d layers, img has %d", len(baseLayers), len(imgLayers))
	// Same invariant in history terms — base's history must fit within img's prefix.
	util.Assert("image.replacebase.base-history-fit", len(baseCfg.History) <= len(imgCfg.History),
		"base has %d history entries, img has %d", len(baseCfg.History), len(imgCfg.History))
	// EmptyLayer flags must agree across the base prefix; if they diverge, the splice would
	// either attach a layer to an EmptyLayer history entry or leave a non-empty entry without
	// a layer, producing a corrupt manifest.
	for i := range baseCfg.History {
		util.Assert("image.replacebase.base-history-alignment",
			imgCfg.History[i].EmptyLayer == baseCfg.History[i].EmptyLayer,
			"history[%d] EmptyLayer differs: img=%v base=%v",
			i, imgCfg.History[i].EmptyLayer, baseCfg.History[i].EmptyLayer)
	}

	addendums := make([]mutate.Addendum, 0, len(imgCfg.History))
	li := 0
	for i, h := range imgCfg.History {
		a := mutate.Addendum{History: h}
		if i < len(baseCfg.History) {
			a.History = baseCfg.History[i]
		}
		if !h.EmptyLayer {
			if li < len(baseLayers) {
				a.Layer = baseLayers[li]
			} else {
				a.Layer = imgLayers[li]
			}
			li++
		}
		addendums = append(addendums, a)
	}

	stacked, err := mutate.Append(empty.Image, addendums...)
	if err != nil {
		return nil, err
	}
	stackedCfg, err := stacked.ConfigFile()
	if err != nil {
		return nil, err
	}
	finalCfg := imgCfg.DeepCopy()
	finalCfg.RootFS = stackedCfg.RootFS
	finalCfg.History = stackedCfg.History
	return mutate.ConfigFile(stacked, finalCfg)
}

func AssertConsistentMediaType(img v1.Image) error {
	man, err := img.Manifest()
	if err != nil {
		return err
	}
	oci, docker := false, false
	classify := func(mt types.MediaType) {
		s := string(mt)
		switch {
		case strings.Contains(s, types.OCIVendorPrefix):
			oci = true
		case strings.Contains(s, types.DockerVendorPrefix):
			docker = true
		}
	}
	classify(man.MediaType)
	classify(man.Config.MediaType)
	for _, l := range man.Layers {
		classify(l.MediaType)
	}
	util.Assert("image.consistent-media-type", !(oci && docker), "manifest mixes OCI and docker media types")
	return nil
}

// WithoutAnnotations returns an image whose manifest has no annotations.
// mutate.Annotations only merges into existing annotations and cannot delete
// keys, so we wrap the image to intercept Manifest and RawManifest instead.
func WithoutAnnotations(img v1.Image) v1.Image {
	return &noAnnotationsImage{Image: img}
}

type noAnnotationsImage struct {
	v1.Image
}

func (n *noAnnotationsImage) Manifest() (*v1.Manifest, error) {
	m, err := n.Image.Manifest()
	if err != nil {
		return nil, err
	}
	stripped := *m
	stripped.Annotations = nil
	return &stripped, nil
}

func (n *noAnnotationsImage) RawManifest() ([]byte, error) {
	m, err := n.Manifest()
	if err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

func (n *noAnnotationsImage) Digest() (v1.Hash, error) {
	return partial.Digest(n)
}
