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
	"os"
	"slices"
	"strings"

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
	ci := DetectCI()
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
	ci := DetectCI()
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

type CI struct {
	Forge          string
	RepositoryPath string
	RepositoryURL  string
	RepositoryName string
	Revision       string
	Ref            string
	ChangeID       string
	PipelineName   string
	PipelineID     string
	PipelineURL    string
	JobName        string
	JobID          string
	JobURL         string
	RunAttempt     string
	// Identity is what makes two runs the same build.
	Identity []string
}

type forge struct {
	marker string
	detect func() CI
}

var forges = []forge{
	{marker: "GITLAB_CI", detect: gitlab},
	{marker: "GITHUB_ACTIONS", detect: github},
}

func DetectCI() CI {
	for _, f := range forges {
		if os.Getenv(f.marker) != "" {
			return f.detect()
		}
	}
	return CI{}
}

func gitlab() CI {
	return CI{
		Forge:          "gitlab",
		RepositoryPath: os.Getenv("CI_PROJECT_PATH"),
		RepositoryURL:  os.Getenv("CI_PROJECT_URL"),
		RepositoryName: os.Getenv("CI_PROJECT_NAME"),
		Revision:       os.Getenv("CI_COMMIT_SHA"),
		Ref:            os.Getenv("CI_COMMIT_REF_NAME"),
		ChangeID:       os.Getenv("CI_MERGE_REQUEST_IID"),
		// CI_PIPELINE_NAME is only set when workflow:name is, so it is usually absent.
		PipelineName: os.Getenv("CI_PIPELINE_NAME"),
		PipelineID:   os.Getenv("CI_PIPELINE_ID"),
		PipelineURL:  os.Getenv("CI_PIPELINE_URL"),
		JobName:      os.Getenv("CI_JOB_NAME"),
		JobID:        os.Getenv("CI_JOB_ID"),
		JobURL:       os.Getenv("CI_JOB_URL"),
		Identity:     []string{os.Getenv("CI_PROJECT_PATH"), os.Getenv("CI_JOB_NAME")},
	}
}

// On a pull_request GITHUB_REF_NAME is N/merge and GITHUB_SHA is the merge commit,
// neither of which names the branch someone pushed.
func github() CI {
	ref := os.Getenv("GITHUB_HEAD_REF")
	if ref == "" {
		ref = os.Getenv("GITHUB_REF_NAME")
	}
	return CI{
		Forge:          "github",
		RepositoryPath: os.Getenv("GITHUB_REPOSITORY"),
		RepositoryURL:  repoURL(),
		RepositoryName: repoName(),
		Revision:       os.Getenv("GITHUB_SHA"),
		Ref:            ref,
		ChangeID:       pullRequestID(),
		PipelineName:   os.Getenv("GITHUB_WORKFLOW"),
		PipelineID:     os.Getenv("GITHUB_RUN_ID"),
		PipelineURL:    runURL(),
		JobName:        os.Getenv("GITHUB_JOB"),
		// A re-run keeps GITHUB_RUN_ID and increments this, so without it two
		// attempts are one run.
		RunAttempt: os.Getenv("GITHUB_RUN_ATTEMPT"),
		Identity:   githubIdentity(),
	}
}

// GITHUB_JOB is the job's key in the workflow file, so a matrix shares it, and the
// workflow is folded in because two workflows may use the same job key. It goes in by
// ref, GITHUB_WORKFLOW being a display name two files can share, minus the ref's
// @refs/... suffix, which would start a new history per branch.
func githubIdentity() []string {
	workflow, _, found := strings.Cut(os.Getenv("GITHUB_WORKFLOW_REF"), "@")
	if !found {
		workflow = os.Getenv("GITHUB_WORKFLOW")
	}
	return []string{os.Getenv("GITHUB_REPOSITORY"), workflow, os.Getenv("GITHUB_JOB")}
}

// pullRequestID reads N out of GITHUB_REF_NAME's "N/merge", GitHub having no
// variable for the number itself.
func pullRequestID() string {
	if os.Getenv("GITHUB_HEAD_REF") == "" {
		return ""
	}
	number, _, found := strings.Cut(os.Getenv("GITHUB_REF_NAME"), "/")
	if !found {
		return ""
	}
	return number
}

// semconv wants the name without the organization, GITHUB_REPOSITORY is org/name.
func repoName() string {
	_, name, found := strings.Cut(os.Getenv("GITHUB_REPOSITORY"), "/")
	if !found {
		return ""
	}
	return name
}

func repoURL() string {
	server, repo := os.Getenv("GITHUB_SERVER_URL"), os.Getenv("GITHUB_REPOSITORY")
	if server == "" || repo == "" {
		return ""
	}
	return server + "/" + repo
}

func runURL() string {
	server, repo, run := os.Getenv("GITHUB_SERVER_URL"), os.Getenv("GITHUB_REPOSITORY"), os.Getenv("GITHUB_RUN_ID")
	if server == "" || repo == "" || run == "" {
		return ""
	}
	url := server + "/" + repo + "/actions/runs/" + run
	attempt := os.Getenv("GITHUB_RUN_ATTEMPT")
	if attempt != "" && attempt != "1" {
		url += "/attempts/" + attempt
	}
	return url
}
