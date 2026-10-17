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

package provenance

import (
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

const (
	ModeMin = "min"

	StatementType    = "https://in-toto.io/Statement/v1"
	PredicateType    = "https://slsa.dev/provenance/v1"
	BuildType        = "https://github.com/osscontainertools/kaniko/blob/main/docs/provenance.md"
	DefaultBuilderID = "https://github.com/osscontainertools/kaniko"

	// BuildKit's layout, which docker buildx imagetools and Docker Scout read
	inTotoMediaType           = "application/vnd.in-toto+json"
	annotationPredicateType   = "in-toto.io/predicate-type"
	annotationReferenceType   = "vnd.docker.reference.type"
	annotationReferenceDigest = "vnd.docker.reference.digest"
	referenceTypeAttestation  = "attestation-manifest"
)

type Statement struct {
	Type          string               `json:"_type"`
	Subject       []ResourceDescriptor `json:"subject"`
	PredicateType string               `json:"predicateType"`
	Predicate     Predicate            `json:"predicate"`
}

type ResourceDescriptor struct {
	URI    string            `json:"uri,omitempty"`
	Name   string            `json:"name,omitempty"`
	Digest map[string]string `json:"digest,omitempty"`
}

type Predicate struct {
	BuildDefinition BuildDefinition `json:"buildDefinition"`
	RunDetails      RunDetails      `json:"runDetails"`
}

type BuildDefinition struct {
	BuildType            string               `json:"buildType"`
	ExternalParameters   ExternalParameters   `json:"externalParameters"`
	InternalParameters   InternalParameters   `json:"internalParameters"`
	ResolvedDependencies []ResourceDescriptor `json:"resolvedDependencies,omitempty"`
}

type ExternalParameters struct {
	Context    string   `json:"context,omitempty"`
	Dockerfile string   `json:"dockerfile"`
	Target     []string `json:"target,omitempty"`
	Platform   string   `json:"platform"`
}

type InternalParameters struct {
	KanikoVersion string            `json:"kanikoVersion"`
	FeatureFlags  map[string]string `json:"featureFlags,omitempty"`
}

type RunDetails struct {
	Builder  Builder   `json:"builder"`
	Metadata *Metadata `json:"metadata,omitempty"`
}

type Builder struct {
	ID string `json:"id"`
}

type Metadata struct {
	InvocationID string     `json:"invocationId,omitempty"`
	StartedOn    *time.Time `json:"startedOn,omitempty"`
	FinishedOn   *time.Time `json:"finishedOn,omitempty"`
}

// PURL names an image the way BuildKit does, which is the form Docker Scout reads
// for base image detection: pkg:docker/alpine@3.20?platform=linux%2Famd64
func PURL(ref name.Reference, platform string) string {
	path := ref.Context().RepositoryStr()
	if ref.Context().RegistryStr() == name.DefaultRegistry {
		path = strings.TrimPrefix(path, "library/")
	} else {
		path = ref.Context().RegistryStr() + "/" + path
	}
	return "pkg:docker/" + path + "@" + ref.Identifier() + "?platform=" + url.QueryEscape(platform)
}

// Digest turns sha256:abc into the in-toto digest set {"sha256": "abc"}.
func Digest(digest string) map[string]string {
	algorithm, hex, found := strings.Cut(digest, ":")
	if !found {
		return nil
	}
	return map[string]string{algorithm: hex}
}

func Attestation(statement Statement) (v1.Image, error) {
	data, err := json.Marshal(statement)
	if err != nil {
		return nil, err
	}
	layer := static.NewLayer(data, inTotoMediaType)
	diffID, err := layer.DiffID()
	if err != nil {
		return nil, err
	}
	img := mutate.MediaType(empty.Image, types.OCIManifestSchema1)
	img = mutate.ConfigMediaType(img, types.OCIConfigJSON)
	img, err = mutate.Append(img, mutate.Addendum{
		Layer:       layer,
		Annotations: map[string]string{annotationPredicateType: statement.PredicateType},
	})
	if err != nil {
		return nil, err
	}
	// replaces the config Append wrote, which carries an empty history entry
	return mutate.ConfigFile(img, &v1.ConfigFile{
		Architecture: "unknown",
		OS:           "unknown",
		RootFS:       v1.RootFS{Type: "layers", DiffIDs: []v1.Hash{diffID}},
	})
}

// Index always is an OCI index, a Docker manifest list has no descriptor
// annotations to link the attestation to its image.
func Index(image, attestation v1.Image, annotations map[string]string) (v1.ImageIndex, error) {
	cf, err := image.ConfigFile()
	if err != nil {
		return nil, err
	}
	digest, err := image.Digest()
	if err != nil {
		return nil, err
	}
	idx := mutate.IndexMediaType(empty.Index, types.OCIImageIndex)
	idx = mutate.AppendManifests(idx,
		mutate.IndexAddendum{
			Add:        image,
			Descriptor: v1.Descriptor{Platform: cf.Platform()},
		},
		mutate.IndexAddendum{
			Add: attestation,
			Descriptor: v1.Descriptor{
				Platform: &v1.Platform{OS: "unknown", Architecture: "unknown"},
				Annotations: map[string]string{
					annotationReferenceType:   referenceTypeAttestation,
					annotationReferenceDigest: digest.String(),
				},
			},
		},
	)
	if len(annotations) > 0 {
		idx = mutate.Annotations(idx, annotations).(v1.ImageIndex)
	}
	return idx, nil
}
