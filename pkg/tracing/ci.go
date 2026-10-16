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

package tracing

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	"github.com/osscontainertools/kaniko/pkg/buildenv"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

// The keys the backend's rollups order on. Kept alongside the semconv names
// rather than replaced by them: renaming these invalidates existing history.
const (
	repoKey     = attribute.Key("repo")
	pipelineKey = attribute.Key("ci.pipeline")
	gitSHAKey   = attribute.Key("git.sha")
	gitRefKey   = attribute.Key("git.ref")
	forgeKey    = attribute.Key("kaniko.ci")
	attemptKey  = attribute.Key("kaniko.ci.run_attempt")
)

func ciAttrs() []attribute.KeyValue {
	ci := buildenv.DetectCI()
	if ci.Forge == "" {
		return nil
	}
	attrs := []attribute.KeyValue{forgeKey.String(ci.Forge)}
	for _, kv := range []attribute.KeyValue{
		repoKey.String(ci.RepositoryPath),
		pipelineKey.String(ci.PipelineID),
		gitSHAKey.String(ci.Revision),
		gitRefKey.String(ci.Ref),

		semconv.VCSRepositoryURLFullKey.String(ci.RepositoryURL),
		semconv.VCSRepositoryNameKey.String(ci.RepositoryName),
		semconv.VCSRefHeadNameKey.String(ci.Ref),
		semconv.VCSRefHeadRevisionKey.String(ci.Revision),
		semconv.VCSChangeIDKey.String(ci.ChangeID),

		semconv.CICDPipelineNameKey.String(ci.PipelineName),
		semconv.CICDPipelineRunIDKey.String(ci.PipelineID),
		semconv.CICDPipelineRunURLFullKey.String(ci.PipelineURL),
		semconv.CICDPipelineTaskNameKey.String(ci.JobName),
		semconv.CICDPipelineTaskRunIDKey.String(ci.JobID),
		semconv.CICDPipelineTaskRunURLFullKey.String(ci.JobURL),
		attemptKey.String(ci.RunAttempt),
	} {
		if kv.Value.AsString() != "" {
			attrs = append(attrs, kv)
		}
	}
	return attrs
}

// ciBuildID identifies the build *definition* — which job of which project this
// is — so runs group across commits and editing the Dockerfile does not start a
// new history. Empty outside CI, and empty when the forge's own identity is
// incomplete, which leaves the content-addressed fallback in place.
func ciBuildID(path, target string) string {
	ci := buildenv.DetectCI()
	if ci.Forge == "" {
		return ""
	}
	if slices.Contains(ci.Identity, "") {
		return ""
	}
	// a matrix over Dockerfiles is one job, so the path is what tells its legs apart
	preimage := strings.Join(append(append([]string{ci.Forge}, ci.Identity...), path, target), "|")
	sum := sha256.Sum256([]byte(preimage))
	return hex.EncodeToString(sum[:])[:16]
}
