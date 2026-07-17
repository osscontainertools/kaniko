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
	"fmt"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/osscontainertools/kaniko/pkg/assert"
	"github.com/sirupsen/logrus"
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
	assert.Assert("image.replacebase.base-layer-fit", len(baseLayers) <= len(imgLayers),
		"base has %d layers, img has %d", len(baseLayers), len(imgLayers))
	// Same invariant in history terms — base's history must fit within img's prefix.
	assert.Assert("image.replacebase.base-history-fit", len(baseCfg.History) <= len(imgCfg.History),
		"base has %d history entries, img has %d", len(baseCfg.History), len(imgCfg.History))
	// EmptyLayer flags must agree across the base prefix; if they diverge, the splice would
	// either attach a layer to an EmptyLayer history entry or leave a non-empty entry without
	// a layer, producing a corrupt manifest.
	for i := range baseCfg.History {
		assert.Assert("image.replacebase.base-history-alignment",
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
				mt, err := baseLayers[li].MediaType()
				if err != nil {
					return nil, err
				}
				switch mt {
				case types.DockerLayer, types.DockerUncompressedLayer, types.DockerForeignLayer:
					a.Layer = baseLayers[li]
				case types.OCILayer:
					a.Layer = &mediaTypeLayer{Layer: baseLayers[li], mediaType: types.DockerLayer}
				case types.OCIUncompressedLayer:
					a.Layer = &mediaTypeLayer{Layer: baseLayers[li], mediaType: types.DockerUncompressedLayer}
				default:
					logrus.Warnf("not preserving base layers: base image has %s layers, incompatible with a reproducible dockerv2 image", mt)
					return img, nil
				}
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

type mediaTypeLayer struct {
	v1.Layer
	mediaType types.MediaType
}

func (l *mediaTypeLayer) MediaType() (types.MediaType, error) {
	return l.mediaType, nil
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
	assert.Assert("image.consistent-media-type", !(oci && docker), "manifest mixes OCI and docker media types")
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

func WithMediaType(img v1.Image, manifestMT types.MediaType) (v1.Image, error) {
	vendor := mediaTypeVendor(manifestMT)
	configMT := types.DockerConfigJSON
	if vendor == types.OCIVendorPrefix {
		configMT = types.OCIConfigJSON
	}
	relabeled, err := relabelLayers(img, vendor)
	if err != nil {
		return nil, err
	}
	return mutate.ConfigMediaType(mutate.MediaType(relabeled, manifestMT), configMT), nil
}

func relabelLayers(img v1.Image, vendor string) (v1.Image, error) {
	man, err := img.Manifest()
	if err != nil {
		return nil, err
	}
	relabeled := man.DeepCopy()
	for i := range relabeled.Layers {
		mt, err := relabelLayerMediaType(relabeled.Layers[i].MediaType, vendor)
		if err != nil {
			return nil, err
		}
		relabeled.Layers[i].MediaType = mt
	}
	return &layerRelabeledImage{Image: img, manifest: relabeled, vendor: vendor}, nil
}

type layerRelabeledImage struct {
	v1.Image
	manifest *v1.Manifest
	vendor   string
}

func (i *layerRelabeledImage) Manifest() (*v1.Manifest, error) {
	return i.manifest.DeepCopy(), nil
}

func (i *layerRelabeledImage) RawManifest() ([]byte, error) {
	return json.Marshal(i.manifest)
}

func (i *layerRelabeledImage) relabel(l v1.Layer) (v1.Layer, error) {
	mt, err := l.MediaType()
	if err != nil {
		return nil, err
	}
	relabeled, err := relabelLayerMediaType(mt, i.vendor)
	if err != nil {
		return nil, err
	}
	return &mediaTypeLayer{Layer: l, mediaType: relabeled}, nil
}

func (i *layerRelabeledImage) Layers() ([]v1.Layer, error) {
	layers, err := i.Image.Layers()
	if err != nil {
		return nil, err
	}
	out := make([]v1.Layer, len(layers))
	for j, l := range layers {
		if out[j], err = i.relabel(l); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (i *layerRelabeledImage) LayerByDigest(h v1.Hash) (v1.Layer, error) {
	l, err := i.Image.LayerByDigest(h)
	if err != nil {
		return nil, err
	}
	return i.relabel(l)
}

func (i *layerRelabeledImage) LayerByDiffID(h v1.Hash) (v1.Layer, error) {
	l, err := i.Image.LayerByDiffID(h)
	if err != nil {
		return nil, err
	}
	return i.relabel(l)
}

func mediaTypeVendor(mt types.MediaType) string {
	if strings.Contains(string(mt), types.OCIVendorPrefix) {
		return types.OCIVendorPrefix
	}
	return types.DockerVendorPrefix
}

func relabelLayerMediaType(mt types.MediaType, vendor string) (types.MediaType, error) {
	if mediaTypeVendor(mt) == vendor {
		return mt, nil
	}
	switch vendor {
	case types.OCIVendorPrefix:
		switch mt {
		case types.DockerLayer:
			return types.OCILayer, nil
		case types.DockerUncompressedLayer:
			return types.OCIUncompressedLayer, nil
		case types.DockerForeignLayer:
			return types.OCIRestrictedLayer, nil
		}
	case types.DockerVendorPrefix:
		switch mt {
		case types.OCILayer:
			return types.DockerLayer, nil
		case types.OCIUncompressedLayer:
			return types.DockerUncompressedLayer, nil
		case types.OCIRestrictedLayer:
			return types.DockerForeignLayer, nil
		case types.OCILayerZStd:
			return "", fmt.Errorf("cannot relabel zstd layer %q to docker schema2, which has no zstd media type, use --image-format=oci", mt)
		}
	}
	return "", fmt.Errorf("cannot relabel layer media type %q to %s", mt, vendor)
}
