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
	"context"
	"maps"
	"testing"

	"github.com/osscontainertools/kaniko/pkg/config"
)

func env(vars map[string]string) getenv {
	return func(key string) string { return vars[key] }
}

// A GitLab job as the runner presents it, trimmed to what is read.
func gitlabJob() map[string]string {
	return map[string]string{
		"GITLAB_CI":            "true",
		"CI_PROJECT_PATH":      "acme/app",
		"CI_PROJECT_URL":       "https://gitlab.com/acme/app",
		"CI_PIPELINE_ID":       "9001",
		"CI_PIPELINE_URL":      "https://gitlab.com/acme/app/-/pipelines/9001",
		"CI_JOB_ID":            "77",
		"CI_JOB_NAME":          "build-image",
		"CI_JOB_URL":           "https://gitlab.com/acme/app/-/jobs/77",
		"CI_COMMIT_SHA":        "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		"CI_COMMIT_REF_NAME":   "main",
		"CI_MERGE_REQUEST_IID": "",
	}
}

// A GitHub Actions job on a push.
func githubJob() map[string]string {
	return map[string]string{
		"GITHUB_ACTIONS":     "true",
		"GITHUB_REPOSITORY":  "acme/app",
		"GITHUB_SERVER_URL":  "https://github.com",
		"GITHUB_RUN_ID":      "12345",
		"GITHUB_RUN_ATTEMPT": "1",
		"GITHUB_WORKFLOW":    "build",
		"GITHUB_JOB":         "image",
		"GITHUB_SHA":         "cafebabecafebabecafebabecafebabecafebabe",
		"GITHUB_REF_NAME":    "main",
		"GITHUB_REF_TYPE":    "branch",
	}
}

func attrsOf(vars map[string]string) map[string]string {
	got := map[string]string{}
	for _, kv := range ciAttrs(env(vars)) {
		got[string(kv.Key)] = kv.Value.AsString()
	}
	return got
}

func TestGitLabAttributes(t *testing.T) {
	got := attrsOf(gitlabJob())
	for key, want := range map[string]string{
		"kaniko.ci":                       "gitlab",
		"repo":                            "acme/app",
		"ci.pipeline":                     "9001",
		"git.sha":                         "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		"git.ref":                         "main",
		"vcs.repository.url.full":         "https://gitlab.com/acme/app",
		"vcs.ref.head.name":               "main",
		"vcs.ref.head.revision":           "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		"cicd.pipeline.run.id":            "9001",
		"cicd.pipeline.run.url.full":      "https://gitlab.com/acme/app/-/pipelines/9001",
		"cicd.pipeline.task.name":         "build-image",
		"cicd.pipeline.task.run.id":       "77",
		"cicd.pipeline.task.run.url.full": "https://gitlab.com/acme/app/-/jobs/77",
	} {
		if got[key] != want {
			t.Errorf("%s = %q, want %q", key, got[key], want)
		}
	}
	// An unset variable is absent, not empty: an empty git.ref reads as a build
	// with no branch.
	if _, ok := got["vcs.change.id"]; ok {
		t.Errorf("vcs.change.id emitted for a non-MR pipeline: %q", got["vcs.change.id"])
	}
	if _, ok := got["cicd.pipeline.name"]; ok {
		t.Error("cicd.pipeline.name emitted although CI_PIPELINE_NAME is unset")
	}
}

func TestGitLabMergeRequest(t *testing.T) {
	vars := gitlabJob()
	vars["CI_MERGE_REQUEST_IID"] = "42"
	if got := attrsOf(vars)["vcs.change.id"]; got != "42" {
		t.Errorf("vcs.change.id = %q, want 42", got)
	}
}

func TestGitHubAttributes(t *testing.T) {
	got := attrsOf(githubJob())
	for key, want := range map[string]string{
		"kaniko.ci":                  "github",
		"repo":                       "acme/app",
		"ci.pipeline":                "12345",
		"git.sha":                    "cafebabecafebabecafebabecafebabecafebabe",
		"git.ref":                    "main",
		"vcs.repository.url.full":    "https://github.com/acme/app",
		"cicd.pipeline.name":         "build",
		"cicd.pipeline.run.id":       "12345",
		"cicd.pipeline.run.url.full": "https://github.com/acme/app/actions/runs/12345",
		"cicd.pipeline.task.name":    "image",
		"kaniko.ci.run_attempt":      "1",
	} {
		if got[key] != want {
			t.Errorf("%s = %q, want %q", key, got[key], want)
		}
	}
}

// On a pull request GITHUB_REF_NAME is "N/merge" and GITHUB_SHA is the merge
// commit, so the branch has to come from GITHUB_HEAD_REF.
func TestGitHubPullRequest(t *testing.T) {
	vars := githubJob()
	vars["GITHUB_EVENT_NAME"] = "pull_request"
	vars["GITHUB_REF_NAME"] = "42/merge"
	vars["GITHUB_HEAD_REF"] = "fix/cache-key"

	got := attrsOf(vars)
	if got["git.ref"] != "fix/cache-key" {
		t.Errorf("git.ref = %q, want the head branch", got["git.ref"])
	}
	if got["vcs.ref.head.name"] != "fix/cache-key" {
		t.Errorf("vcs.ref.head.name = %q, want the head branch", got["vcs.ref.head.name"])
	}
	if got["vcs.change.id"] != "42" {
		t.Errorf("vcs.change.id = %q, want 42", got["vcs.change.id"])
	}
}

// A re-run keeps GITHUB_RUN_ID, so the attempt is what separates the two.
func TestGitHubRerunURL(t *testing.T) {
	vars := githubJob()
	vars["GITHUB_RUN_ATTEMPT"] = "3"
	got := attrsOf(vars)
	if want := "https://github.com/acme/app/actions/runs/12345/attempts/3"; got["cicd.pipeline.run.url.full"] != want {
		t.Errorf("cicd.pipeline.run.url.full = %q, want %q", got["cicd.pipeline.run.url.full"], want)
	}
}

func TestNoCIMeansNoAttributes(t *testing.T) {
	if got := ciAttrs(env(map[string]string{"CI_PROJECT_PATH": "acme/app"})); got != nil {
		t.Errorf("ciAttrs() = %v outside CI, want none", got)
	}
}

// The point of the CI build id: the job is the build, so a commit or a
// Dockerfile edit does not start a new history, and two jobs of one project
// stay apart.
func TestGitLabBuildIDIsProjectAndJob(t *testing.T) {
	first := gitlabJob()
	second := gitlabJob()
	second["CI_PIPELINE_ID"] = "9002"
	second["CI_COMMIT_SHA"] = "0000000000000000000000000000000000000000"
	second["CI_COMMIT_REF_NAME"] = "topic"

	if ciBuildID(env(first), "") != ciBuildID(env(second), "") {
		t.Error("build id changed between two runs of the same job")
	}

	other := gitlabJob()
	other["CI_JOB_NAME"] = "build-image-arm64"
	if ciBuildID(env(first), "") == ciBuildID(env(other), "") {
		t.Error("two jobs of the same project share a build id")
	}

	elsewhere := gitlabJob()
	elsewhere["CI_PROJECT_PATH"] = "globex/app"
	if ciBuildID(env(first), "") == ciBuildID(env(elsewhere), "") {
		t.Error("the same job name in two projects shares a build id")
	}

	if ciBuildID(env(first), "") == ciBuildID(env(first), "builder") {
		t.Error("two targets of one job share a build id")
	}
}

func TestGitHubBuildIDIsRepoWorkflowAndJob(t *testing.T) {
	first := githubJob()
	rerun := githubJob()
	rerun["GITHUB_RUN_ID"] = "999"
	rerun["GITHUB_SHA"] = "0000000000000000000000000000000000000000"

	if ciBuildID(env(first), "") != ciBuildID(env(rerun), "") {
		t.Error("build id changed between two runs of the same job")
	}

	otherWorkflow := githubJob()
	otherWorkflow["GITHUB_WORKFLOW"] = "release"
	if ciBuildID(env(first), "") == ciBuildID(env(otherWorkflow), "") {
		t.Error("the same job key in two workflows shares a build id")
	}
}

// An incomplete forge identity falls back rather than hashing a blank.
func TestIncompleteCIIdentityFallsBack(t *testing.T) {
	vars := gitlabJob()
	vars["CI_JOB_NAME"] = ""
	if got := ciBuildID(env(vars), ""); got != "" {
		t.Errorf("ciBuildID() = %q, want empty so the content hash is used", got)
	}

	opts := &config.KanikoOptions{DockerfilePath: "/workspace/Dockerfile"}
	content := []byte("FROM scratch\n")
	if buildID(env(vars), opts.DockerfilePath, "", content) != buildID(noEnv, opts.DockerfilePath, "", content) {
		t.Error("an incomplete CI identity did not fall back to the content hash")
	}
}

func TestBuildIDEnvStillWins(t *testing.T) {
	vars := gitlabJob()
	vars[BuildIDEnv] = "chosen-by-hand"
	if got := buildID(env(vars), "/workspace/Dockerfile", "", []byte("FROM scratch\n")); got != "chosen-by-hand" {
		t.Errorf("buildID() = %q, want the explicit id", got)
	}
}

// OTEL_RESOURCE_ATTRIBUTES is the last word: a pipeline that sets repo itself
// keeps its value.
func TestResourceAttributesOverrideCI(t *testing.T) {
	vars := gitlabJob()
	vars["OTEL_RESOURCE_ATTRIBUTES"] = "repo=acme/monorepo,team=platform"
	// resource.WithFromEnv reads the process environment, not our map.
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", vars["OTEL_RESOURCE_ATTRIBUTES"])

	res, err := buildResource(context.Background(), env(vars),
		&config.KanikoOptions{DockerfilePath: "/workspace/Dockerfile"}, []byte("FROM scratch\n"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, kv := range res.Attributes() {
		got[string(kv.Key)] = kv.Value.AsString()
	}
	if got["repo"] != "acme/monorepo" {
		t.Errorf("repo = %q, want the pipeline's own value", got["repo"])
	}
	if got["team"] != "platform" {
		t.Errorf("team = %q, want platform", got["team"])
	}
	// Everything it did not name still arrives.
	if got["cicd.pipeline.task.name"] != "build-image" {
		t.Errorf("cicd.pipeline.task.name = %q, want build-image", got["cicd.pipeline.task.name"])
	}
	if got["kaniko.build_id"] == "" {
		t.Error("kaniko.build_id missing from the resource")
	}
}

func TestCIAttributesAreStable(t *testing.T) {
	// Two calls, same environment: nothing here may depend on map order.
	first, second := attrsOf(gitlabJob()), attrsOf(gitlabJob())
	if !maps.Equal(first, second) {
		t.Errorf("ciAttrs() is not deterministic:\n%v\n%v", first, second)
	}
}
