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

package integration

import (
	"fmt"
	"os/exec"
	"testing"
)

// testGitBuildcontextRawHelper builds the same Dockerfile with docker and
// kaniko, using ref strings constructed by the caller. Unlike the existing
// testGitBuildcontextHelper, it does not transform the ref (no implicit
// refs/heads/ prefix), so callers can exercise the plain-branch, tag, and
// flag-driven paths in pkg/buildcontext/git.go.
func testGitBuildcontextRawHelper(t *testing.T, dockerRef, kanikoRef, imageName string, kanikoExtraFlags ...string) {
	t.Helper()
	dockerfile := fmt.Sprintf("%s/%s/Dockerfile_test_run_2", integrationPath, dockerfilesPath)

	dockerImage := GetDockerImage(config.imageRepo, imageName)
	dockerCmd := exec.Command("docker", "build", "--push",
		"-t", dockerImage, "-f", dockerfile, dockerRef)
	out, err := RunCommandWithoutTest(dockerCmd)
	if err != nil {
		t.Fatalf("docker build failed: %v\n%s", err, out)
	}

	kanikoImage := GetKanikoImage(config.imageRepo, imageName)
	dockerRunFlags := []string{"run", "--net=host"}
	dockerRunFlags = addServiceAccountFlags(dockerRunFlags, config.serviceAccount)
	dockerRunFlags = addCoverageFlags(dockerRunFlags)
	dockerRunFlags = append(dockerRunFlags, ExecutorImage,
		"-f", dockerfile, "-d", kanikoImage, "-c", kanikoRef)
	dockerRunFlags = append(dockerRunFlags, kanikoExtraFlags...)
	kanikoCmd := exec.Command("docker", dockerRunFlags...)
	out, err = RunCommandWithoutTest(kanikoCmd)
	if err != nil {
		t.Fatalf("kaniko build failed: %v\n%s", err, out)
	}

	containerDiff(t, dockerImage, kanikoImage, "--semantic",
		"--extra-ignore-file-content", "--extra-ignore-layer-length-mismatch")
}

// TestGitBuildcontextPlainBranch exercises a bare branch name with no
// refs/heads/ prefix (else branch in UnpackTarFromBuildContext around line 88).
func TestGitBuildcontextPlainBranch(t *testing.T) {
	t.Parallel()
	branch, _, url := getBranchCommitAndURL()
	testGitBuildcontextRawHelper(t,
		fmt.Sprintf("https://%s.git#%s", url, branch),
		fmt.Sprintf("git://%s.git#%s", url, branch),
		"Dockerfile_test_git_plain_branch",
	)
}

// TestGitBuildcontextTag exercises a refs/tags/<tag> ref, which goes through
// the post-clone fetch flow (lines 84-87 and 108-124) instead of the direct
// clone-by-ref path used for refs/heads/.
func TestGitBuildcontextTag(t *testing.T) {
	t.Parallel()
	_, _, url := getBranchCommitAndURL()
	tag := "refs/tags/v1.27.5"
	testGitBuildcontextRawHelper(t,
		fmt.Sprintf("https://%s.git#%s", url, tag),
		fmt.Sprintf("git://%s.git#%s", url, tag),
		"Dockerfile_test_git_tag",
	)
}

// TestGitBuildcontextBranchFlag exercises the --git=branch=<name> flag, which
// triggers getGitReferenceName (lines 94-100 and 152-179). Docker has no
// equivalent flag, so the docker side puts the branch in the URL fragment.
func TestGitBuildcontextBranchFlag(t *testing.T) {
	t.Parallel()
	branch, _, url := getBranchCommitAndURL()
	testGitBuildcontextRawHelper(t,
		fmt.Sprintf("https://%s.git#%s", url, branch),
		fmt.Sprintf("git://%s.git", url),
		"Dockerfile_test_git_branch_flag",
		"--git=branch="+branch,
	)
}
