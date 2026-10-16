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

package buildenv

import (
	"os"
	"strings"
)

type CI struct {
	Forge          string
	RepositoryPath string
	RepositoryURL  string
	RepositoryName string
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
	// Checkout is what the job expects the build context to be, which a git checkout
	// can contradict. The other fields describe the job and hold regardless.
	Checkout Source
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
	// a merge request pipeline runs in the parent project, but only a detached one builds the fork's
	// commit, merged results and merge trains build a merge commit in the parent project
	source := os.Getenv("CI_PROJECT_URL")
	if os.Getenv("CI_MERGE_REQUEST_EVENT_TYPE") == "detached" {
		source = os.Getenv("CI_MERGE_REQUEST_SOURCE_PROJECT_URL")
	}
	return CI{
		Forge:          "gitlab",
		RepositoryPath: os.Getenv("CI_PROJECT_PATH"),
		RepositoryURL:  os.Getenv("CI_PROJECT_URL"),
		RepositoryName: os.Getenv("CI_PROJECT_NAME"),
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
		Checkout:     Source{URL: source, Revision: os.Getenv("CI_COMMIT_SHA"), Version: os.Getenv("CI_COMMIT_TAG")},
	}
}

// On a pull_request GITHUB_REF_NAME is N/merge and GITHUB_SHA is the merge commit,
// neither of which names the branch someone pushed.
func github() CI {
	ref := os.Getenv("GITHUB_HEAD_REF")
	if ref == "" {
		ref = os.Getenv("GITHUB_REF_NAME")
	}
	tag := ""
	if os.Getenv("GITHUB_REF_TYPE") == "tag" {
		tag = os.Getenv("GITHUB_REF_NAME")
	}
	return CI{
		Forge:          "github",
		RepositoryPath: os.Getenv("GITHUB_REPOSITORY"),
		RepositoryURL:  repoURL(),
		RepositoryName: repoName(),
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
		Checkout:   Source{URL: repoURL(), Revision: os.Getenv("GITHUB_SHA"), Version: tag},
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
