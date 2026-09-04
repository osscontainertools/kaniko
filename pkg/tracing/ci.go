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
	"strings"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// getenv is a parameter rather than a package call, so a forge's variables map
// to attributes as a pure function of the environment.
type getenv func(string) string

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

// forge is a CI system whose predefined variables kaniko reads. marker is the
// variable that says a build is running there; identity is what makes two runs
// the same build.
type forge struct {
	name     string
	marker   string
	attrs    func(getenv) []attribute.KeyValue
	identity func(getenv) []string
}

var forges = []forge{
	{name: "gitlab", marker: "GITLAB_CI", attrs: gitlabAttrs, identity: gitlabIdentity},
	{name: "github", marker: "GITHUB_ACTIONS", attrs: githubAttrs, identity: githubIdentity},
}

func detectForge(env getenv) (forge, bool) {
	for _, f := range forges {
		if env(f.marker) != "" {
			return f, true
		}
	}
	return forge{}, false
}

// ciAttrs is what the CI system already knows, so a pipeline does not have to
// repeat it in OTEL_RESOURCE_ATTRIBUTES.
func ciAttrs(env getenv) []attribute.KeyValue {
	f, ok := detectForge(env)
	if !ok {
		return nil
	}
	return append([]attribute.KeyValue{forgeKey.String(f.name)}, f.attrs(env)...)
}

// ciBuildID identifies the build *definition* — which job of which project this
// is — so runs group across commits and editing the Dockerfile does not start a
// new history. Empty outside CI, and empty when the forge's own identity is
// incomplete, which leaves the content-addressed fallback in place.
func ciBuildID(env getenv, target string) string {
	f, ok := detectForge(env)
	if !ok {
		return ""
	}
	parts := f.identity(env)
	for _, p := range parts {
		if p == "" {
			return ""
		}
	}
	preimage := strings.Join(append(append([]string{f.name}, parts...), target), "|")
	sum := sha256.Sum256([]byte(preimage))
	return hex.EncodeToString(sum[:])[:16]
}

func gitlabIdentity(env getenv) []string {
	return []string{env("CI_PROJECT_PATH"), env("CI_JOB_NAME")}
}

func gitlabAttrs(env getenv) []attribute.KeyValue {
	var a []attribute.KeyValue
	a = add(a, repoKey, env("CI_PROJECT_PATH"))
	a = add(a, pipelineKey, env("CI_PIPELINE_ID"))
	a = add(a, gitSHAKey, env("CI_COMMIT_SHA"))
	a = add(a, gitRefKey, env("CI_COMMIT_REF_NAME"))

	a = add(a, semconv.VCSRepositoryURLFullKey, env("CI_PROJECT_URL"))
	a = add(a, semconv.VCSRepositoryNameKey, env("CI_PROJECT_PATH"))
	a = add(a, semconv.VCSRefHeadNameKey, env("CI_COMMIT_REF_NAME"))
	a = add(a, semconv.VCSRefHeadRevisionKey, env("CI_COMMIT_SHA"))
	a = add(a, semconv.VCSChangeIDKey, env("CI_MERGE_REQUEST_IID"))

	// CI_PIPELINE_NAME is only set when workflow:name is, so it is usually absent.
	a = add(a, semconv.CICDPipelineNameKey, env("CI_PIPELINE_NAME"))
	a = add(a, semconv.CICDPipelineRunIDKey, env("CI_PIPELINE_ID"))
	a = add(a, semconv.CICDPipelineRunURLFullKey, env("CI_PIPELINE_URL"))
	a = add(a, semconv.CICDPipelineTaskNameKey, env("CI_JOB_NAME"))
	a = add(a, semconv.CICDPipelineTaskRunIDKey, env("CI_JOB_ID"))
	a = add(a, semconv.CICDPipelineTaskRunURLFullKey, env("CI_JOB_URL"))
	return a
}

// GITHUB_JOB is the job's key in the workflow file, so a matrix shares it; the
// workflow is folded in because two workflows may use the same job key.
func githubIdentity(env getenv) []string {
	return []string{env("GITHUB_REPOSITORY"), env("GITHUB_WORKFLOW"), env("GITHUB_JOB")}
}

func githubAttrs(env getenv) []attribute.KeyValue {
	repo, server := env("GITHUB_REPOSITORY"), env("GITHUB_SERVER_URL")

	var a []attribute.KeyValue
	a = add(a, repoKey, repo)
	a = add(a, pipelineKey, env("GITHUB_RUN_ID"))
	a = add(a, gitSHAKey, env("GITHUB_SHA"))
	// On a pull_request the ref is refs/pull/N/merge and the sha is the merge
	// commit, neither of which names the branch someone pushed.
	a = add(a, gitRefKey, firstOf(env("GITHUB_HEAD_REF"), env("GITHUB_REF_NAME")))

	a = add(a, semconv.VCSRepositoryURLFullKey, repoURL(server, repo))
	a = add(a, semconv.VCSRepositoryNameKey, repo)
	a = add(a, semconv.VCSRefHeadNameKey, firstOf(env("GITHUB_HEAD_REF"), env("GITHUB_REF_NAME")))
	a = add(a, semconv.VCSRefHeadRevisionKey, env("GITHUB_SHA"))
	a = add(a, semconv.VCSChangeIDKey, pullRequestID(env))

	a = add(a, semconv.CICDPipelineNameKey, env("GITHUB_WORKFLOW"))
	a = add(a, semconv.CICDPipelineRunIDKey, env("GITHUB_RUN_ID"))
	a = add(a, semconv.CICDPipelineRunURLFullKey, runURL(server, repo, env("GITHUB_RUN_ID"), env("GITHUB_RUN_ATTEMPT")))
	a = add(a, semconv.CICDPipelineTaskNameKey, env("GITHUB_JOB"))
	// A re-run keeps GITHUB_RUN_ID and increments this, so without it two
	// attempts are one run.
	a = add(a, attemptKey, env("GITHUB_RUN_ATTEMPT"))
	return a
}

// pullRequestID reads N out of GITHUB_REF_NAME's "N/merge", GitHub having no
// variable for the number itself.
func pullRequestID(env getenv) string {
	if env("GITHUB_HEAD_REF") == "" {
		return ""
	}
	number, _, found := strings.Cut(env("GITHUB_REF_NAME"), "/")
	if !found {
		return ""
	}
	return number
}

func repoURL(server, repo string) string {
	if server == "" || repo == "" {
		return ""
	}
	return server + "/" + repo
}

func runURL(server, repo, run, attempt string) string {
	if server == "" || repo == "" || run == "" {
		return ""
	}
	url := server + "/" + repo + "/actions/runs/" + run
	if attempt != "" && attempt != "1" {
		url += "/attempts/" + attempt
	}
	return url
}

// add skips an absent variable rather than emitting it empty, which would read
// as "this build has no branch" instead of "this forge does not say".
func add(attrs []attribute.KeyValue, key attribute.Key, value string) []attribute.KeyValue {
	if value == "" {
		return attrs
	}
	return append(attrs, key.String(value))
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
