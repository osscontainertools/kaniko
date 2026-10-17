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
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/constants"
	"github.com/osscontainertools/kaniko/pkg/provenance"
	"github.com/osscontainertools/kaniko/pkg/version"
)

// Dependencies come from the plan, so a stage skipped on a cache hit still lists its base.
func provenanceStatement(image v1.Image, opts *config.KanikoOptions, stages []config.KanikoStage, externalImageDigests map[string]string, started, finished time.Time) (provenance.Statement, error) {
	digest, err := image.Digest()
	if err != nil {
		return provenance.Statement{}, err
	}
	subject := []provenance.ResourceDescriptor{}
	for _, destination := range opts.Destinations {
		ref, err := name.ParseReference(destination, name.WeakValidation)
		if err != nil {
			return provenance.Statement{}, err
		}
		subject = append(subject, provenance.ResourceDescriptor{Name: provenance.PURL(ref, opts.CustomPlatform), Digest: provenance.Digest(digest.String())})
	}
	if len(subject) == 0 {
		subject = append(subject, provenance.ResourceDescriptor{Digest: provenance.Digest(digest.String())})
	}

	images := map[string]string{}
	for _, s := range stages {
		if !s.BaseImageStoredLocally && s.BaseName != constants.NoBaseImage {
			images[s.BaseName] = s.BaseImageDigest
		}
	}
	maps.Copy(images, externalImageDigests)
	dependencies := []provenance.ResourceDescriptor{}
	src := opts.ContextSource
	if src.URL != "" && src.Revision != "" {
		uri := "git+" + src.URL
		if src.Version != "" {
			uri += "@" + src.Version
		}
		dependencies = append(dependencies, provenance.ResourceDescriptor{URI: uri, Digest: map[string]string{"gitCommit": src.Revision}})
	}
	for _, image := range slices.Sorted(maps.Keys(images)) {
		ref, err := name.ParseReference(image, name.WeakValidation)
		if err != nil {
			return provenance.Statement{}, err
		}
		dependencies = append(dependencies, provenance.ResourceDescriptor{URI: provenance.PURL(ref, opts.CustomPlatform), Digest: provenance.Digest(images[image])})
	}

	dockerfile, err := filepath.Rel(opts.SrcContext, opts.DockerfilePath)
	if err != nil || strings.HasPrefix(dockerfile, "..") {
		dockerfile = opts.DockerfilePath
	}

	featureFlags := map[string]string{}
	for _, kv := range os.Environ() {
		key, value, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(key, "FF_KANIKO_") {
			featureFlags[key] = value
		}
	}

	builderID := provenance.DefaultBuilderID
	switch {
	case opts.CI.JobURL != "":
		builderID = opts.CI.JobURL
	case opts.CI.PipelineURL != "":
		builderID = opts.CI.PipelineURL
	}

	var metadata *provenance.Metadata
	if !opts.Reproducible {
		var ids []string
		for _, id := range []string{opts.CI.PipelineID, opts.CI.JobID, opts.CI.RunAttempt} {
			if id != "" {
				ids = append(ids, id)
			}
		}
		started, finished := started.UTC(), finished.UTC()
		metadata = &provenance.Metadata{InvocationID: strings.Join(ids, "/"), StartedOn: &started, FinishedOn: &finished}
	}

	return provenance.Statement{
		Type:          provenance.StatementType,
		Subject:       subject,
		PredicateType: provenance.PredicateType,
		Predicate: provenance.Predicate{
			BuildDefinition: provenance.BuildDefinition{
				BuildType: provenance.BuildType,
				ExternalParameters: provenance.ExternalParameters{
					Context:    src.URL,
					Dockerfile: dockerfile,
					Target:     opts.Target,
					Platform:   opts.CustomPlatform,
				},
				InternalParameters: provenance.InternalParameters{
					KanikoVersion: version.Version(),
					FeatureFlags:  featureFlags,
				},
				ResolvedDependencies: dependencies,
			},
			RunDetails: provenance.RunDetails{
				Builder:  provenance.Builder{ID: builderID},
				Metadata: metadata,
			},
		},
	}, nil
}
