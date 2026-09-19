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

// identity is what makes two runs the same build.
type forge struct {
	name     string
	marker   string
	mappings []mapping
	identity func(getenv) []string
}

var forges = []forge{
	{name: "gitlab", marker: "GITLAB_CI", mappings: gitlabMappings, identity: gitlabIdentity},
	{name: "github", marker: "GITHUB_ACTIONS", mappings: githubMappings, identity: githubIdentity},
}

type mapping struct {
	key     attribute.Key
	vars    []string
	compute func(getenv) string
}

func mapAttrs(env getenv, mappings []mapping) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	for _, m := range mappings {
		value := ""
		if m.compute != nil {
			value = m.compute(env)
		} else {
			for _, name := range m.vars {
				value = env(name)
				if value != "" {
					break
				}
			}
		}
		if value != "" {
			attrs = append(attrs, m.key.String(value))
		}
	}
	return attrs
}

func detectForge(env getenv) (forge, bool) {
	for _, f := range forges {
		if env(f.marker) != "" {
			return f, true
		}
	}
	return forge{}, false
}

func ciAttrs(env getenv) []attribute.KeyValue {
	f, ok := detectForge(env)
	if !ok {
		return nil
	}
	return append([]attribute.KeyValue{forgeKey.String(f.name)}, mapAttrs(env, f.mappings)...)
}

// ciBuildID identifies the build *definition* — which job of which project this
// is — so runs group across commits and editing the Dockerfile does not start a
// new history. Empty outside CI, and empty when the forge's own identity is
// incomplete, which leaves the content-addressed fallback in place.
func ciBuildID(env getenv, path, target string) string {
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
	// a matrix over Dockerfiles is one job, so the path is what tells its legs apart
	preimage := strings.Join(append(append([]string{f.name}, parts...), path, target), "|")
	sum := sha256.Sum256([]byte(preimage))
	return hex.EncodeToString(sum[:])[:16]
}

func gitlabIdentity(env getenv) []string {
	return []string{env("CI_PROJECT_PATH"), env("CI_JOB_NAME")}
}

var gitlabMappings = []mapping{
	{key: repoKey, vars: []string{"CI_PROJECT_PATH"}},
	{key: pipelineKey, vars: []string{"CI_PIPELINE_ID"}},
	{key: gitSHAKey, vars: []string{"CI_COMMIT_SHA"}},
	{key: gitRefKey, vars: []string{"CI_COMMIT_REF_NAME"}},

	{key: semconv.VCSRepositoryURLFullKey, vars: []string{"CI_PROJECT_URL"}},
	{key: semconv.VCSRepositoryNameKey, vars: []string{"CI_PROJECT_NAME"}},
	{key: semconv.VCSRefHeadNameKey, vars: []string{"CI_COMMIT_REF_NAME"}},
	{key: semconv.VCSRefHeadRevisionKey, vars: []string{"CI_COMMIT_SHA"}},
	{key: semconv.VCSChangeIDKey, vars: []string{"CI_MERGE_REQUEST_IID"}},

	// CI_PIPELINE_NAME is only set when workflow:name is, so it is usually absent.
	{key: semconv.CICDPipelineNameKey, vars: []string{"CI_PIPELINE_NAME"}},
	{key: semconv.CICDPipelineRunIDKey, vars: []string{"CI_PIPELINE_ID"}},
	{key: semconv.CICDPipelineRunURLFullKey, vars: []string{"CI_PIPELINE_URL"}},
	{key: semconv.CICDPipelineTaskNameKey, vars: []string{"CI_JOB_NAME"}},
	{key: semconv.CICDPipelineTaskRunIDKey, vars: []string{"CI_JOB_ID"}},
	{key: semconv.CICDPipelineTaskRunURLFullKey, vars: []string{"CI_JOB_URL"}},
}

// GITHUB_JOB is the job's key in the workflow file, so a matrix shares it, and the
// workflow is folded in because two workflows may use the same job key. It goes in by
// ref, GITHUB_WORKFLOW being a display name two files can share, minus the ref's
// @refs/... suffix, which would start a new history per branch.
func githubIdentity(env getenv) []string {
	workflow, _, found := strings.Cut(env("GITHUB_WORKFLOW_REF"), "@")
	if !found {
		workflow = env("GITHUB_WORKFLOW")
	}
	return []string{env("GITHUB_REPOSITORY"), workflow, env("GITHUB_JOB")}
}

// On a pull_request GITHUB_REF_NAME is N/merge and GITHUB_SHA is the merge commit,
// neither of which names the branch someone pushed.
var githubMappings = []mapping{
	{key: repoKey, vars: []string{"GITHUB_REPOSITORY"}},
	{key: pipelineKey, vars: []string{"GITHUB_RUN_ID"}},
	{key: gitSHAKey, vars: []string{"GITHUB_SHA"}},
	{key: gitRefKey, vars: []string{"GITHUB_HEAD_REF", "GITHUB_REF_NAME"}},

	{key: semconv.VCSRepositoryURLFullKey, compute: repoURL},
	{key: semconv.VCSRepositoryNameKey, compute: repoName},
	{key: semconv.VCSRefHeadNameKey, vars: []string{"GITHUB_HEAD_REF", "GITHUB_REF_NAME"}},
	{key: semconv.VCSRefHeadRevisionKey, vars: []string{"GITHUB_SHA"}},
	{key: semconv.VCSChangeIDKey, compute: pullRequestID},

	{key: semconv.CICDPipelineNameKey, vars: []string{"GITHUB_WORKFLOW"}},
	{key: semconv.CICDPipelineRunIDKey, vars: []string{"GITHUB_RUN_ID"}},
	{key: semconv.CICDPipelineRunURLFullKey, compute: runURL},
	{key: semconv.CICDPipelineTaskNameKey, vars: []string{"GITHUB_JOB"}},
	// A re-run keeps GITHUB_RUN_ID and increments this, so without it two
	// attempts are one run.
	{key: attemptKey, vars: []string{"GITHUB_RUN_ATTEMPT"}},
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

// semconv wants the name without the organization, GITHUB_REPOSITORY is org/name.
func repoName(env getenv) string {
	_, name, found := strings.Cut(env("GITHUB_REPOSITORY"), "/")
	if !found {
		return ""
	}
	return name
}

func repoURL(env getenv) string {
	server, repo := env("GITHUB_SERVER_URL"), env("GITHUB_REPOSITORY")
	if server == "" || repo == "" {
		return ""
	}
	return server + "/" + repo
}

func runURL(env getenv) string {
	server, repo, run := env("GITHUB_SERVER_URL"), env("GITHUB_REPOSITORY"), env("GITHUB_RUN_ID")
	if server == "" || repo == "" || run == "" {
		return ""
	}
	url := server + "/" + repo + "/actions/runs/" + run
	attempt := env("GITHUB_RUN_ATTEMPT")
	if attempt != "" && attempt != "1" {
		url += "/attempts/" + attempt
	}
	return url
}
