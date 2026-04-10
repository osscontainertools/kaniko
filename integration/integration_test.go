/*
Copyright 2018 Google LLC

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
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/osscontainertools/kaniko/pkg/timing"
	"github.com/osscontainertools/kaniko/pkg/util/bucket"
	"github.com/osscontainertools/kaniko/testutil"
	"google.golang.org/api/option"
)

var (
	config         *integrationTestConfig
	imageBuilder   *DockerFileBuilder
	allDockerfiles []string
)

const (
	daemonPrefix    = "docker://"
	integrationPath = "integration"
	dockerfilesPath = "dockerfiles"
)

func getDockerMajorVersion() int {
	out, err := exec.Command("docker", "version", "--format", "{{.Server.Version}}").Output()
	if err != nil {
		log.Fatal("Error getting docker version of server:", err)
	}
	versionArr := strings.Split(string(out), ".")

	ver, err := strconv.Atoi(versionArr[0])
	if err != nil {
		log.Fatal("Error getting docker version of server during parsing version string:", err)
	}
	return ver
}

func launchTests(m *testing.M) (int, error) {
	if config.isGcrRepository() {
		contextFilePath, err := CreateIntegrationTarball()
		if err != nil {
			return 1, fmt.Errorf("Failed to create tarball of integration files for build context: %w", err)
		}

		bucketName, item, err := bucket.GetNameAndFilepathFromURI(config.gcsBucket)
		if err != nil {
			return 1, fmt.Errorf("failed to get bucket name from uri: %w", err)
		}
		contextFile, err := os.Open(contextFilePath)
		if err != nil {
			return 1, fmt.Errorf("failed to read file at path %v: %w", contextFilePath, err)
		}
		err = bucket.Upload(context.Background(), bucketName, item, contextFile, config.gcsClient)
		if err != nil {
			return 1, fmt.Errorf("Failed to upload build context: %w", err)
		}

		if err = os.Remove(contextFilePath); err != nil {
			return 1, fmt.Errorf("Failed to remove tarball at %s: %w", contextFilePath, err)
		}

		deleteFunc := func() {
			bucket.Delete(context.Background(), bucketName, item, config.gcsClient)
		}
		RunOnInterrupt(deleteFunc)
		defer deleteFunc()
	}
	if err := buildRequiredImages(); err != nil {
		return 1, fmt.Errorf("Error while building images: %w", err)
	}

	imageBuilder = NewDockerFileBuilder()

	return m.Run(), nil
}

func TestMain(m *testing.M) {
	var err error
	if !meetsRequirements() {
		fmt.Println("Missing required tools")
		os.Exit(1)
	}

	config = initIntegrationTestConfig()
	if allDockerfiles, err = FindDockerFiles(dockerfilesPath, config.dockerfilesPattern); err != nil {
		fmt.Println("Coudn't create map of dockerfiles", err)
		os.Exit(1)
	}

	exitCode, err := launchTests(m)
	if err != nil {
		fmt.Println(err)
	}
	os.Exit(exitCode)
}

func buildRequiredImages() error {
	setupCommands := []struct {
		name    string
		command []string
	}{{
		name:    "Building kaniko image",
		command: []string{"docker", "build", "-t", ExecutorImage, "-f", "../deploy/Dockerfile", "--target", "kaniko-executor", ".."},
	}, {
		name:    "Building cache warmer image",
		command: []string{"docker", "build", "-t", WarmerImage, "-f", "../deploy/Dockerfile", "--target", "kaniko-warmer", ".."},
	}, {
		name:    "Building onbuild base image",
		command: []string{"docker", "build", "--push", "-t", config.onbuildBaseImage, "-f", dockerfilesPath + "/Dockerfile_onbuild_base", "."},
	}, {
		name:    "Building onbuild copy image",
		command: []string{"docker", "build", "--push", "-t", config.onbuildCopyImage, "-f", dockerfilesPath + "/Dockerfile_onbuild_copy", "."},
	}, {
		name:    "Building hardlink base image",
		command: []string{"docker", "build", "--push", "-t", config.hardlinkBaseImage, "-f", dockerfilesPath + "/Dockerfile_hardlink_base", "."},
	}, {
		name:    "Building kaniko image with moved kaniko dir",
		command: []string{"docker", "build", "-t", ExecutorImageMoved, "-f", dockerfilesPath + "/Dockerfile_test_issue_mz444", "--target", "kaniko", "."},
	}, {
		name:    "Building kaniko image with leftover stuff in the filesystem",
		command: []string{"docker", "build", "-t", ExecutorImageTainted, "-f", dockerfilesPath + "/Dockerfile_test_issue_mz455", "--target", "kaniko", "."},
	}, {
		name:    "Building hijack base image",
		command: []string{"docker", "build", "--push", "-t", config.hijackBaseImage, "-f", dockerfilesPath + "/Dockerfile_test_issue_mz560", "--target", "base", "."},
	}}

	for _, setupCmd := range setupCommands {
		fmt.Println(setupCmd.name)
		cmd := exec.Command(setupCmd.command[0], setupCmd.command[1:]...)
		if out, err := RunCommandWithoutTest(cmd); err != nil {
			return fmt.Errorf("%s failed: %s: %w", setupCmd.name, string(out), err)
		}
	}

	maliciousRef := strings.ToLower(config.imageRepo + "path-traversal-malicious:latest")
	err := pushMaliciousPathTraversalImage(maliciousRef)
	if err != nil {
		return err
	}

	return nil
}

func TestRun(t *testing.T) {
	t.Parallel()
	for _, dockerfile := range allDockerfiles {
		t.Run("test_"+dockerfile, func(t *testing.T) {
			dockerfile := dockerfile
			t.Parallel()
			if _, ok := imageBuilder.DockerfilesToIgnore[dockerfile]; ok {
				t.SkipNow()
			}
			if _, ok := imageBuilder.TestCacheDockerfiles[dockerfile]; ok {
				t.SkipNow()
			}
			if _, ok := imageBuilder.TestWarmerDockerfiles[dockerfile]; ok {
				t.SkipNow()
			}

			buildImage(t, dockerfile, imageBuilder)

			if _, ok := expectErr[dockerfile]; !ok {
				dockerImage := GetDockerImage(config.imageRepo, dockerfile)
				kanikoImage := GetKanikoImage(config.imageRepo, dockerfile)
				containerDiff(t, dockerImage, kanikoImage, "--semantic", "--extra-ignore-file-content", "--extra-ignore-layer-length-mismatch")
			}
		})
	}

	err := logBenchmarks("benchmark")
	if err != nil {
		t.Logf("Failed to create benchmark file: %v", err)
	}
}

func getBranchCommitAndURL() (branch, commit, url string) {
	repo := os.Getenv("GITHUB_REPOSITORY")
	commit = os.Getenv("GITHUB_SHA")
	if _, isPR := os.LookupEnv("GITHUB_HEAD_REF"); isPR {
		branch = "main"
	} else {
		branch = os.Getenv("GITHUB_REF")
		log.Printf("GITHUB_HEAD_REF is unset (not a PR); using GITHUB_REF=%q", branch)
		branch = strings.TrimPrefix(branch, "refs/heads/")
	}
	if repo == "" {
		repo = "osscontainertools/kaniko"
	}
	if branch == "" {
		branch = "main"
	}
	log.Printf("repo=%q / commit=%q / branch=%q", repo, commit, branch)
	url = "github.com/" + repo
	return
}

func DockerGitRepo(url string, commit string, branch string) string {
	ref := ""
	if commit != "" {
		ref = "#" + commit
	} else if branch != "" {
		ref = "#" + branch
	}
	return fmt.Sprintf("https://%s.git%s", url, ref)
}

func KanikoGitRepo(url string, commit string, branch string) string {
	ref := ""
	if commit != "" {
		ref = "#" + commit
	} else if branch != "" {
		ref = "#refs/heads/" + branch
	}
	return fmt.Sprintf("git://%s.git%s", url, ref)
}

func testGitBuildcontextHelper(t *testing.T, url string, commit string, branch string, imageName string) {
	t.Log("testGitBuildcontextHelper repo", url)
	dockerfile := fmt.Sprintf("%s/%s/Dockerfile_test_run_2", integrationPath, dockerfilesPath)

	// Build with docker
	dockerImage := GetDockerImage(config.imageRepo, imageName)
	dockerCmd := exec.Command("docker",
		[]string{
			"build",
			"--push",
			"-t", dockerImage,
			"-f", dockerfile,
			DockerGitRepo(url, commit, branch),
		}...)
	out, err := RunCommandWithoutTest(dockerCmd)
	if err != nil {
		t.Errorf("Failed to build image %s with docker command %q: %s %s", dockerImage, dockerCmd.Args, err, string(out))
	}

	// Build with kaniko
	kanikoImage := GetKanikoImage(config.imageRepo, imageName)
	dockerRunFlags := []string{"run", "--net=host"}
	dockerRunFlags = addServiceAccountFlags(dockerRunFlags, config.serviceAccount)
	dockerRunFlags = append(dockerRunFlags, ExecutorImage,
		"-f", dockerfile,
		"-d", kanikoImage,
		"-c", KanikoGitRepo(url, commit, branch))

	kanikoCmd := exec.Command("docker", dockerRunFlags...)

	out, err = RunCommandWithoutTest(kanikoCmd)
	if err != nil {
		t.Errorf("Failed to build image %s with kaniko command %q: %v %s", dockerImage, kanikoCmd.Args, err, string(out))
	}

	containerDiff(t, dockerImage, kanikoImage, "--semantic", "--extra-ignore-file-content", "--extra-ignore-layer-length-mismatch")
}

// TestGitBuildcontext explicitly names the main branch
// Example:
//
//	git://github.com/myuser/repo#refs/heads/main
func TestGitBuildcontext(t *testing.T) {
	t.Parallel()
	branch, _, url := getBranchCommitAndURL()
	testGitBuildcontextHelper(t, url, "", branch, "Dockerfile_test_git_branch")
}

// TestGitBuildcontextNoRef builds without any commit / branch reference
// Example:
//
//	git://github.com/myuser/repo
func TestGitBuildcontextNoRef(t *testing.T) {
	t.Skip("Docker's behavior is to assume a 'master' branch, which the Kaniko repo doesn't have")
	t.Parallel()
	_, _, url := getBranchCommitAndURL()
	testGitBuildcontextHelper(t, url, "", "", "Dockerfile_test_git_noref")
}

// TestGitBuildcontextExplicitCommit uses an explicit commit hash instead of named reference
// Example:
//
//	git://github.com/myuser/repo#b873088c4a7b60bb7e216289c58da945d0d771b6
func TestGitBuildcontextExplicitCommit(t *testing.T) {
	t.Parallel()
	_, commit, url := getBranchCommitAndURL()
	testGitBuildcontextHelper(t, url, commit, "", "Dockerfile_test_git_commit")
}

func TestGitBuildcontextSubPath(t *testing.T) {
	t.Parallel()
	branch, _, url := getBranchCommitAndURL()
	dockerfile := "Dockerfile_test_run_2"

	// Build with docker
	dockerImage := GetDockerImage(config.imageRepo, "Dockerfile_test_git_subpath")
	dockerCmd := exec.Command("docker",
		[]string{
			"build",
			"--push",
			"-t", dockerImage,
			"-f", filepath.Join(integrationPath, dockerfilesPath, dockerfile),
			DockerGitRepo(url, "", branch),
		}...)
	out, err := RunCommandWithoutTest(dockerCmd)
	if err != nil {
		t.Errorf("Failed to build image %s with docker command %q: %s %s", dockerImage, dockerCmd.Args, err, string(out))
	}

	// Build with kaniko
	kanikoImage := GetKanikoImage(config.imageRepo, "Dockerfile_test_git_subpath")
	dockerRunFlags := []string{"run", "--net=host"}
	dockerRunFlags = addServiceAccountFlags(dockerRunFlags, config.serviceAccount)
	dockerRunFlags = append(
		dockerRunFlags,
		ExecutorImage,
		"-f", dockerfile,
		"-d", kanikoImage,
		"-c", KanikoGitRepo(url, "", branch),
		"--context-sub-path", filepath.Join(integrationPath, dockerfilesPath),
	)

	kanikoCmd := exec.Command("docker", dockerRunFlags...)

	out, err = RunCommandWithoutTest(kanikoCmd)
	if err != nil {
		t.Errorf("Failed to build image %s with kaniko command %q: %v %s", dockerImage, kanikoCmd.Args, err, string(out))
	}

	containerDiff(t, dockerImage, kanikoImage, "--semantic", "--extra-ignore-file-content", "--extra-ignore-layer-length-mismatch")
}

func TestBuildViaRegistryMirrors(t *testing.T) {
	t.Parallel()
	branch, _, url := getBranchCommitAndURL()
	dockerfile := fmt.Sprintf("%s/%s/Dockerfile_registry_mirror", integrationPath, dockerfilesPath)

	// Build with docker
	dockerImage := GetDockerImage(config.imageRepo, "Dockerfile_registry_mirror")
	dockerCmd := exec.Command("docker",
		[]string{
			"build",
			"--push",
			"-t", dockerImage,
			"-f", dockerfile,
			DockerGitRepo(url, "", branch),
		}...)
	out, err := RunCommandWithoutTest(dockerCmd)
	if err != nil {
		t.Errorf("Failed to build image %s with docker command %q: %s %s", dockerImage, dockerCmd.Args, err, string(out))
	}

	// Build with kaniko
	kanikoImage := GetKanikoImage(config.imageRepo, "Dockerfile_registry_mirror")
	dockerRunFlags := []string{"run", "--net=host"}
	dockerRunFlags = addServiceAccountFlags(dockerRunFlags, config.serviceAccount)
	dockerRunFlags = append(dockerRunFlags, ExecutorImage,
		"-f", dockerfile,
		"-d", kanikoImage,
		"--registry-mirror", "doesnotexist.example.com",
		"--registry-mirror", "us-mirror.gcr.io",
		"-c", KanikoGitRepo(url, "", branch))

	kanikoCmd := exec.Command("docker", dockerRunFlags...)

	out, err = RunCommandWithoutTest(kanikoCmd)
	if err != nil {
		t.Errorf("Failed to build image %s with kaniko command %q: %v %s", dockerImage, kanikoCmd.Args, err, string(out))
	}

	containerDiff(t, dockerImage, kanikoImage, "--semantic", "--extra-ignore-file-content", "--extra-ignore-layer-length-mismatch")
}

func TestBuildViaRegistryMap(t *testing.T) {
	t.Parallel()
	branch, _, url := getBranchCommitAndURL()
	dockerfile := fmt.Sprintf("%s/%s/Dockerfile_registry_mirror", integrationPath, dockerfilesPath)

	// Build with docker
	dockerImage := GetDockerImage(config.imageRepo, "Dockerfile_registry_map")
	dockerCmd := exec.Command("docker",
		[]string{
			"build",
			"-t", dockerImage,
			"-f", dockerfile,
			"--push",
			DockerGitRepo(url, "", branch),
		}...)
	out, err := RunCommandWithoutTest(dockerCmd)
	if err != nil {
		t.Errorf("Failed to build image %s with docker command %q: %s %s", dockerImage, dockerCmd.Args, err, string(out))
	}

	// Build with kaniko
	kanikoImage := GetKanikoImage(config.imageRepo, "Dockerfile_registry_map")
	dockerRunFlags := []string{"run", "--net=host"}
	dockerRunFlags = addServiceAccountFlags(dockerRunFlags, config.serviceAccount)
	dockerRunFlags = append(dockerRunFlags, ExecutorImage,
		"-f", dockerfile,
		"-d", kanikoImage,
		"--registry-map", "index.docker.io=doesnotexist.example.com",
		"--registry-map", "index.docker.io=us-mirror.gcr.io",
		"-c", KanikoGitRepo(url, "", branch))

	kanikoCmd := exec.Command("docker", dockerRunFlags...)

	out, err = RunCommandWithoutTest(kanikoCmd)
	if err != nil {
		t.Errorf("Failed to build image %s with kaniko command %q: %v %s", dockerImage, kanikoCmd.Args, err, string(out))
	}

	containerDiff(t, dockerImage, kanikoImage, "--semantic", "--extra-ignore-file-content", "--extra-ignore-layer-length-mismatch")
}

func TestBuildSkipFallback(t *testing.T) {
	t.Parallel()
	branch, _, url := getBranchCommitAndURL()
	dockerfile := fmt.Sprintf("%s/%s/Dockerfile_registry_mirror", integrationPath, dockerfilesPath)

	// Build with kaniko
	kanikoImage := GetKanikoImage(config.imageRepo, "Dockerfile_registry_skip_fallback")
	dockerRunFlags := []string{"run", "--net=host"}
	dockerRunFlags = addServiceAccountFlags(dockerRunFlags, config.serviceAccount)
	dockerRunFlags = append(dockerRunFlags, ExecutorImage,
		"-f", dockerfile,
		"-d", kanikoImage,
		"--registry-mirror", "doesnotexist.example.com",
		"--skip-default-registry-fallback",
		"-c", KanikoGitRepo(url, "", branch))

	kanikoCmd := exec.Command("docker", dockerRunFlags...)

	_, err := RunCommandWithoutTest(kanikoCmd)
	if err == nil {
		t.Errorf("Build should fail after using skip-default-registry-fallback and registry-mirror fail to pull")
	}
}

// TestKanikoDir tests that a build that sets --kaniko-dir produces the same output as the equivalent docker build.
func TestKanikoDir(t *testing.T) {
	t.Parallel()
	branch, _, url := getBranchCommitAndURL()
	dockerfile := fmt.Sprintf("%s/%s/Dockerfile_registry_mirror", integrationPath, dockerfilesPath)

	// Build with docker
	dockerImage := GetDockerImage(config.imageRepo, "Dockerfile_kaniko_dir")
	dockerCmd := exec.Command("docker",
		[]string{
			"build",
			"-t", dockerImage,
			"-f", dockerfile,
			"--push",
			DockerGitRepo(url, "", branch),
		}...)
	out, err := RunCommandWithoutTest(dockerCmd)
	if err != nil {
		t.Errorf("Failed to build image %s with docker command %q: %s %s", dockerImage, dockerCmd.Args, err, string(out))
	}

	// Build with kaniko
	kanikoImage := GetKanikoImage(config.imageRepo, "Dockerfile_kaniko_dir")
	dockerRunFlags := []string{"run", "--net=host"}
	dockerRunFlags = addServiceAccountFlags(dockerRunFlags, config.serviceAccount)
	dockerRunFlags = append(dockerRunFlags, ExecutorImage,
		"-f", dockerfile,
		"-d", kanikoImage,
		"--kaniko-dir", "/not-kaniko",
		"-c", KanikoGitRepo(url, "", branch))

	kanikoCmd := exec.Command("docker", dockerRunFlags...)

	out, err = RunCommandWithoutTest(kanikoCmd)
	if err != nil {
		t.Errorf("Failed to build image %s with kaniko command %q: %v %s", dockerImage, kanikoCmd.Args, err, string(out))
	}

	containerDiff(t, dockerImage, kanikoImage, "--semantic", "--extra-ignore-file-content", "--extra-ignore-layer-length-mismatch")
}

func TestBuildWithLabels(t *testing.T) {
	t.Parallel()
	branch, _, url := getBranchCommitAndURL()
	dockerfile := fmt.Sprintf("%s/%s/Dockerfile_test_label", integrationPath, dockerfilesPath)

	testLabel := "mylabel=myvalue"

	// Build with docker
	dockerImage := GetDockerImage(config.imageRepo, "Dockerfile_test_label")
	dockerCmd := exec.Command("docker",
		[]string{
			"build",
			"--push",
			"-t", dockerImage,
			"-f", dockerfile,
			"--label", testLabel,
			DockerGitRepo(url, "", branch),
		}...)
	out, err := RunCommandWithoutTest(dockerCmd)
	if err != nil {
		t.Errorf("Failed to build image %s with docker command %q: %s %s", dockerImage, dockerCmd.Args, err, string(out))
	}

	// Build with kaniko
	kanikoImage := GetKanikoImage(config.imageRepo, "Dockerfile_test_label")
	dockerRunFlags := []string{"run", "--net=host"}
	dockerRunFlags = addServiceAccountFlags(dockerRunFlags, config.serviceAccount)
	dockerRunFlags = append(dockerRunFlags, ExecutorImage,
		"-f", dockerfile,
		"-d", kanikoImage,
		"--label", testLabel,
		"-c", KanikoGitRepo(url, "", branch),
	)

	kanikoCmd := exec.Command("docker", dockerRunFlags...)

	out, err = RunCommandWithoutTest(kanikoCmd)
	if err != nil {
		t.Errorf("Failed to build image %s with kaniko command %q: %v %s", dockerImage, kanikoCmd.Args, err, string(out))
	}

	containerDiff(t, dockerImage, kanikoImage, "--semantic", "--extra-ignore-file-content", "--extra-ignore-layer-length-mismatch")
}

func TestBuildWithHTTPError(t *testing.T) {
	t.Parallel()
	branch, _, url := getBranchCommitAndURL()
	dockerfile := fmt.Sprintf("%s/%s/Dockerfile_test_add_404", integrationPath, dockerfilesPath)

	// Build with docker
	dockerImage := GetDockerImage(config.imageRepo, "Dockerfile_test_add_404")
	dockerCmd := exec.Command("docker",
		[]string{
			"build",
			"-t", dockerImage,
			"-f", dockerfile,
			DockerGitRepo(url, "", branch),
		}...)
	out, err := RunCommandWithoutTest(dockerCmd)
	if err == nil {
		t.Errorf("an error was expected, got %s", string(out))
	}

	// Build with kaniko
	kanikoImage := GetKanikoImage(config.imageRepo, "Dockerfile_test_add_404")
	dockerRunFlags := []string{"run", "--net=host"}
	dockerRunFlags = addServiceAccountFlags(dockerRunFlags, config.serviceAccount)
	dockerRunFlags = append(dockerRunFlags, ExecutorImage,
		"-f", dockerfile,
		"-d", kanikoImage,
		"-c", KanikoGitRepo(url, "", branch),
	)

	kanikoCmd := exec.Command("docker", dockerRunFlags...)

	out, err = RunCommandWithoutTest(kanikoCmd)
	if err == nil {
		t.Errorf("an error was expected, got %s", string(out))
	}
}

func TestLayers(t *testing.T) {
	t.Parallel()
	// offset is caused because for those three files we use
	// --single-snapshot option, compressing all layers into one
	offset := map[string]int{
		"Dockerfile_test_add":        12,
		"Dockerfile_test_scratch":    3,
		"Dockerfile_test_maintainer": 0,
	}

	for _, dockerfile := range allDockerfiles {
		t.Run("test_layer_"+dockerfile, func(t *testing.T) {
			dockerfileTest := dockerfile

			t.Parallel()
			if _, ok := imageBuilder.DockerfilesToIgnore[dockerfileTest]; ok {
				t.SkipNow()
			}
			if _, ok := expectErr[dockerfile]; ok {
				t.SkipNow()
			}

			buildImage(t, dockerfileTest, imageBuilder)

			dockerImage := GetDockerImage(config.imageRepo, dockerfileTest)
			kanikoImage := GetKanikoImage(config.imageRepo, dockerfileTest)
			pushCmd := exec.Command("docker", "push", dockerImage)
			RunCommand(pushCmd, t)
			checkLayers(t, dockerImage, kanikoImage, offset[dockerfileTest])
			onBuildDiff(t, dockerImage, kanikoImage)
		})
	}

	err := logBenchmarks("benchmark_layers")
	if err != nil {
		t.Logf("Failed to create benchmark file: %v", err)
	}
}

// pushMaliciousPathTraversalImage creates and pushes a minimal OCI image whose
// single layer contains a tar entry with a "../" path-traversal sequence.
// Docker cannot produce such layers, so the image is crafted programmatically.
func pushMaliciousPathTraversalImage(imageRef string) error {
	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		if err := tw.WriteHeader(&tar.Header{
			Name:     "blubb",
			Typeflag: tar.TypeReg,
			Size:     0,
			Mode:     0o644,
		}); err != nil {
			return nil, err
		}
		payload := "#!/bin/sh\necho WARN HIJACKED"
		if err := tw.WriteHeader(&tar.Header{
			Name:     "../../../tini",
			Typeflag: tar.TypeReg,
			Size:     int64(len(payload)),
			Mode:     0o755,
		}); err != nil {
			return nil, err
		}
		if _, err := tw.Write([]byte(payload)); err != nil {
			return nil, err
		}
		tw.Close()
		return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
	})
	if err != nil {
		return fmt.Errorf("creating path-traversal layer: %v", err)
	}

	img, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		return fmt.Errorf("appending layer to empty image: %v", err)
	}

	ref, err := name.ParseReference(imageRef, name.WeakValidation)
	if err != nil {
		return fmt.Errorf("parsing image ref %s: %v", imageRef, err)
	}
	if err := remote.Write(ref, img, remote.WithAuthFromKeychain(authn.DefaultKeychain)); err != nil {
		return fmt.Errorf("pushing malicious image to %s: %v", imageRef, err)
	}
	return nil
}

func TestReplaceFolderWithFileOrLink(t *testing.T) {
	t.Parallel()
	dockerfiles := []string{"TestReplaceFolderWithFile", "TestReplaceFolderWithLink"}
	for _, dockerfile := range dockerfiles {
		t.Run(dockerfile, func(t *testing.T) {
			t.Parallel()
			buildImage(t, dockerfile, imageBuilder)
			kanikoImage := GetKanikoImage(config.imageRepo, dockerfile)

			kanikoFiles, err := getLastLayerFiles(kanikoImage)
			if err != nil {
				t.Fatal(err)
			}
			fmt.Println(kanikoFiles)

			for _, file := range kanikoFiles {
				if strings.HasPrefix(file, "a/.wh.") {
					t.Errorf("Last layer should not add whiteout files to deleted directory but found %s", file)
				}
			}
		})
	}
}

func buildImage(t *testing.T, dockerfile string, imageBuilder *DockerFileBuilder) {
	t.Logf("Building image '%v'...", dockerfile)

	if err := imageBuilder.BuildImage(t, config, dockerfilesPath, dockerfile); err != nil {
		t.Errorf("Error building image: %s", err)
		t.FailNow()
	}
}

// Build each image with kaniko twice, and then make sure they're exactly the same
func TestCache(t *testing.T) {
	t.Parallel()
	// Build dockerfiles with registry cache
	for dockerfile := range imageBuilder.TestCacheDockerfiles {
		t.Run("test_cache_"+dockerfile, func(t *testing.T) {
			dockerfile := dockerfile
			cache := filepath.Join(config.imageRepo, "cache", strconv.FormatInt(time.Now().UnixNano(), 10))
			t.Parallel()
			verifyBuildWith(t, cache, dockerfile)
		})
	}

	// Build dockerfiles with layout cache
	for dockerfile := range imageBuilder.TestOCICacheDockerfiles {
		t.Run("test_oci_cache_"+dockerfile, func(t *testing.T) {
			dockerfile := dockerfile
			cache := filepath.Join("oci:", cacheDir, "cached", strconv.FormatInt(time.Now().UnixNano(), 10))
			t.Parallel()
			verifyBuildWith(t, cache, dockerfile)
		})
	}

	if err := logBenchmarks("benchmark_cache"); err != nil {
		t.Logf("Failed to create benchmark file: %v", err)
	}
}

func TestWarmer(t *testing.T) {
	t.Parallel()
	populateVolumeCache(t.Logf, config.serviceAccount)

	for dockerfile := range imageBuilder.TestWarmerDockerfiles {
		t.Run("test_warmer_"+dockerfile, func(t *testing.T) {
			t.Parallel()
			args, ok := additionalKanikoFlagsMap[dockerfile]
			imageRepo := config.imageRepo
			if !ok {
				args = []string{}
			}

			// Build the initial without warmer
			if err := imageBuilder.buildWarmerImage(t.Logf, config, dockerfilesPath, dockerfile, 0, args, false); err != nil {
				t.Fatalf("error building cached image for the first time: %v", err)
			}

			// Build the second with warmer
			if err := imageBuilder.buildWarmerImage(t.Logf, config, dockerfilesPath, dockerfile, 1, args, true); err != nil {
				t.Fatalf("error building cached image for the second time: %v", err)
			}

			// Make sure both images are the same
			kanikoVersion0 := GetKanikoImage(imageRepo, "test_warmer_"+dockerfile) + strconv.Itoa(0)
			kanikoVersion1 := GetKanikoImage(imageRepo, "test_warmer_"+dockerfile) + strconv.Itoa(1)

			containerDiff(t, kanikoVersion0, kanikoVersion1)
		})
	}
}

// Attempt to warm an image two times : first time should populate the cache, second time should find the image in the cache.
func TestWarmerTwice(t *testing.T) {
	t.Parallel()
	dockerfiles := map[string]bool{
		"debian:trixie-slim": true,
		"debian:12.10@sha256:264982ff4d18000fa74540837e2c43ca5137a53a83f8f62c7b3803c0f0bdcd56": true,  // image-index requires remote lookup
		"debian:12.10@sha256:6bc30d909583f38600edd6609e29eb3fb284ab8affce8d0389f332fc91c2dd91": false, // image-manifest can skip lookup
	}
	for dockerfile, remoteLookup := range dockerfiles {
		t.Run("test_warmer_twice_"+dockerfile, func(t *testing.T) {
			t.Parallel()
			tmpDir, err := os.MkdirTemp("", "")
			if err != nil {
				t.Fatal("failed to create tmpdir")
			}
			defer os.RemoveAll(tmpDir)

			// Start a sleeping warmer container
			dockerRunFlags := []string{"run", "--net=host"}
			dockerRunFlags = addServiceAccountFlags(dockerRunFlags, config.serviceAccount)
			for _, envVariable := range WarmerEnv {
				dockerRunFlags = append(dockerRunFlags, "-e", envVariable)
			}
			dockerRunFlags = append(dockerRunFlags,
				"-v", tmpDir+":/cache",
				WarmerImage,
				"--cache-dir=/cache",
				"-i", dockerfile)

			warmCmd := exec.Command("docker", dockerRunFlags...)
			out, err := RunCommandWithoutTest(warmCmd)
			t.Logf("First warm output:\n%s", out)
			if err != nil {
				t.Fatalf("Unable to perform first warming: %s", err)
			}

			warmCmd = exec.Command("docker", dockerRunFlags...)
			out, err = RunCommandWithoutTest(warmCmd)
			t.Logf("Second warm output:\n%s", out)
			if err != nil {
				t.Fatalf("Unable to perform second warming: %s", err)
			}

			s := "Image already in cache: " + dockerfile
			if !strings.Contains(string(out), s) {
				t.Fatalf("output must contain %s", s)
			}
			s = fmt.Sprintf("Retrieving image %s from registry index.docker.io", dockerfile)
			if remoteLookup && !strings.Contains(string(out), s) {
				t.Fatalf("output must contain %s", s)
			} else if !remoteLookup && strings.Contains(string(out), s) {
				t.Fatalf("output must not contain %s", s)
			}
		})
	}
}

func verifyBuildWith(t *testing.T, cache, dockerfile string) {
	args, ok := additionalKanikoFlagsMap[dockerfile]
	if !ok {
		args = []string{}
	}

	// Build the initial image which will cache layers
	if err := imageBuilder.buildCachedImage(t.Logf, config, cache, dockerfilesPath, dockerfile, 0, args); err != nil {
		t.Fatalf("error building cached image for the first time: %v", err)
	}
	// Build the second image which should pull from the cache
	if err := imageBuilder.buildCachedImage(t.Logf, config, cache, dockerfilesPath, dockerfile, 1, args); err != nil {
		t.Fatalf("error building cached image for the second time: %v", err)
	}
	// Make sure both images are the same
	kanikoVersion0 := GetVersionedKanikoImage(config.imageRepo, dockerfile, 0)
	kanikoVersion1 := GetVersionedKanikoImage(config.imageRepo, dockerfile, 1)

	containerDiff(t, kanikoVersion0, kanikoVersion1)
}

func TestRelativePaths(t *testing.T) {
	t.Parallel()
	dockerfile := "Dockerfile_relative_copy"

	t.Run("test_relative_"+dockerfile, func(t *testing.T) {
		t.Parallel()

		dockerfile = filepath.Join("./dockerfiles", dockerfile)

		contextPath := "./context"

		err := imageBuilder.buildRelativePathsImage(
			t.Logf,
			config.imageRepo,
			dockerfile,
			config.serviceAccount,
			contextPath,
		)
		if err != nil {
			t.Fatal(err)
		}

		dockerImage := GetDockerImage(config.imageRepo, "test_relative_"+dockerfile)
		kanikoImage := GetKanikoImage(config.imageRepo, "test_relative_"+dockerfile)

		containerDiff(t, dockerImage, kanikoImage, "--semantic", "--extra-ignore-file-content", "--extra-ignore-layer-length-mismatch")
	})
}

func TestExitCodePropagation(t *testing.T) {
	t.Parallel()
	currentDir, err := os.Getwd()
	if err != nil {
		t.Fatal("Could not get working dir")
	}

	context := currentDir + "/testdata/exit-code-propagation"
	dockerfile := context + "/Dockerfile_exit_code_propagation"

	t.Run("test error code propagation", func(t *testing.T) {
		t.Parallel()
		// building the image with docker should fail with exit code 42
		dockerImage := GetDockerImage(config.imageRepo, "Dockerfile_exit_code_propagation")
		dockerFlags := []string{
			"build",
			"-t", dockerImage,
			"-f", dockerfile,
		}
		dockerCmd := exec.Command("docker", append(dockerFlags, context)...)
		dockerCmd.Env = append(dockerCmd.Env, "DOCKER_BUILDKIT=0")

		out, kanikoErr := RunCommandWithoutTest(dockerCmd)
		if kanikoErr == nil {
			t.Fatalf("docker build did not produce an error:\n%s", out)
		}
		var dockerCmdExitErr *exec.ExitError
		var dockerExitCode int

		if errors.As(kanikoErr, &dockerCmdExitErr) {
			dockerExitCode = dockerCmdExitErr.ExitCode()
			testutil.CheckDeepEqual(t, 42, dockerExitCode)
			if t.Failed() {
				t.Fatalf("Output was:\n%s", out)
			}
		} else {
			t.Fatalf("did not produce the expected error:\n%s", out)
		}

		// try to build the same image with kaniko the error code should match with the one from the plain docker build
		contextVolume := context + ":/workspace"

		dockerFlags = []string{
			"run",
			"-v", contextVolume,
		}
		dockerFlags = addServiceAccountFlags(dockerFlags, "")
		dockerFlags = append(dockerFlags, ExecutorImage,
			"-c", "dir:///workspace/",
			"-f", "./Dockerfile_exit_code_propagation",
			"--no-push",
		)

		dockerCmdWithKaniko := exec.Command("docker", dockerFlags...)

		out, kanikoErr = RunCommandWithoutTest(dockerCmdWithKaniko)
		if kanikoErr == nil {
			t.Fatalf("the kaniko build did not produce the expected error:\n%s", out)
		}

		var kanikoExitErr *exec.ExitError
		if errors.As(kanikoErr, &kanikoExitErr) {
			testutil.CheckDeepEqual(t, dockerExitCode, kanikoExitErr.ExitCode())
			if t.Failed() {
				t.Fatalf("Output was:\n%s", out)
			}
		} else {
			t.Fatalf("did not produce the expected error:\n%s", out)
		}
	})
}

func TestBuildWithAnnotations(t *testing.T) {
	t.Parallel()
	branch, _, url := getBranchCommitAndURL()

	dockerfile := integrationPath + "/testdata/Dockerfile.trivial"
	annotationKey := "myannotation"
	annotationValue := "myvalue"

	// Build with docker
	dockerImage := GetDockerImage(config.imageRepo, "Dockerfile_test_annotation")
	dockerCmd := exec.Command("docker",
		"build",
		"--push",
		"-t", dockerImage,
		"-f", dockerfile,
		"--annotation", fmt.Sprintf("%s=%s", annotationKey, annotationValue),
		DockerGitRepo(url, "", branch),
	)
	out, err := RunCommandWithoutTest(dockerCmd)
	if err != nil {
		t.Errorf("Failed to build image %s with docker command %q: %s %s", dockerImage, dockerCmd.Args, err, string(out))
	}

	// Build with kaniko
	kanikoImage := GetKanikoImage(config.imageRepo, "Dockerfile_test_annotation")
	dockerRunFlags := []string{"run", "--net=host"}
	dockerRunFlags = addServiceAccountFlags(dockerRunFlags, config.serviceAccount)
	dockerRunFlags = append(dockerRunFlags, ExecutorImage,
		"-f", dockerfile,
		"-d", kanikoImage,
		"--annotation", fmt.Sprintf("%s=%s", annotationKey, annotationValue),
		"-c", KanikoGitRepo(url, "", branch),
	)
	kanikoCmd := exec.Command("docker", dockerRunFlags...)
	out, err = RunCommandWithoutTest(kanikoCmd)
	if err != nil {
		t.Errorf("Failed to build image %s with kaniko command %q: %v %s", dockerImage, kanikoCmd.Args, err, string(out))
	}
	containerDiff(t, dockerImage, kanikoImage, "--ignore-history")
}

func onBuildDiff(t *testing.T, image1, image2 string) {
	img1, err := getImageConfig(image1)
	if err != nil {
		t.Fatalf("Failed to get image config for (%s): %s", image1, err)
	}
	img2, err := getImageConfig(image2)
	if err != nil {
		t.Fatalf("Failed to get image config for (%s): %s", image2, err)
	}
	testutil.CheckDeepEqual(t, img1.Config.OnBuild, img2.Config.OnBuild)
}

func checkLayers(t *testing.T, image1, image2 string, offset int) {
	t.Helper()
	img1, err := getImageDetails(image1)
	if err != nil {
		t.Fatalf("Couldn't get details from image reference for (%s): %s", image1, err)
	}

	img2, err := getImageDetails(image2)
	if err != nil {
		t.Fatalf("Couldn't get details from image reference for (%s): %s", image2, err)
	}

	actualOffset := int(math.Abs(float64(img1.numLayers - img2.numLayers)))
	if actualOffset != offset {
		t.Fatalf("Difference in number of layers in each image is %d but should be %d. Image 1: %s, Image 2: %s", actualOffset, offset, img1, img2)
	}
}

func getImageConfig(image string) (*v1.ConfigFile, error) {
	ref, err := name.ParseReference(image, name.WeakValidation)
	if err != nil {
		return nil, fmt.Errorf("Couldn't parse reference to image %s: %w", image, err)
	}
	imgRef, err := remote.Image(ref)
	if err != nil {
		return nil, fmt.Errorf("Couldn't get reference to image %s from remote: %w", image, err)
	}
	cfg, err := imgRef.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("Couldn't get Config for image %s: %w", image, err)
	}
	return cfg, nil
}

func getImage(image string) (v1.Image, error) {
	ref, err := name.ParseReference(image, name.WeakValidation)
	if err != nil {
		return nil, fmt.Errorf("Couldn't parse reference to image %s: %w", image, err)
	}
	return remote.Image(ref)
}

func getImageDetails(image string) (*imageDetails, error) {
	imgRef, err := getImage(image)
	if err != nil {
		return nil, fmt.Errorf("Couldn't get reference to image %s from remote: %w", image, err)
	}
	layers, err := imgRef.Layers()
	if err != nil {
		return nil, fmt.Errorf("Error getting layers for image %s: %w", image, err)
	}
	digest, err := imgRef.Digest()
	if err != nil {
		return nil, fmt.Errorf("Error getting digest for image %s: %w", image, err)
	}
	return &imageDetails{
		name:      image,
		numLayers: len(layers),
		digest:    digest.Hex,
	}, nil
}

func getLastLayerFiles(image string) ([]string, error) {
	imgRef, err := getImage(image)
	if err != nil {
		return nil, fmt.Errorf("Couldn't get reference to image %s from daemon: %w", image, err)
	}
	layers, err := imgRef.Layers()
	if err != nil {
		return nil, fmt.Errorf("Error getting layers for image %s: %w", image, err)
	}
	readCloser, err := layers[len(layers)-1].Uncompressed()
	if err != nil {
		return nil, err
	}

	tr := tar.NewReader(readCloser)
	var files []string
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		files = append(files, hdr.Name)
	}
	return files, nil
}

func logBenchmarks(benchmark string) error {
	if b, err := strconv.ParseBool(os.Getenv("BENCHMARK")); err == nil && b {
		f, err := os.Create(benchmark)
		if err != nil {
			return err
		}
		f.WriteString(timing.Summary())
		defer f.Close()
	}
	return nil
}

type imageDetails struct {
	name      string
	numLayers int
	digest    string
}

func (i imageDetails) String() string {
	return fmt.Sprintf("Image: [%s] Digest: [%s] Number of Layers: [%d]", i.name, i.digest, i.numLayers)
}

func initIntegrationTestConfig() *integrationTestConfig {
	var c integrationTestConfig

	var gcsEndpoint string
	var disableGcsAuth bool
	flag.StringVar(&c.gcsBucket, "bucket", "gs://kaniko-test-bucket", "The gcs bucket argument to uploaded the tar-ed contents of the `integration` dir to.")
	flag.StringVar(&c.imageRepo, "repo", "gcr.io/kaniko-test", "The (docker) image repo to build and push images to during the test. `gcloud` must be authenticated with this repo or serviceAccount must be set.")
	flag.StringVar(&c.serviceAccount, "serviceAccount", "", "The path to the service account push images to GCR and upload/download files to GCS.")
	flag.StringVar(&gcsEndpoint, "gcs-endpoint", "", "Custom endpoint for GCS. Used for local integration tests")
	flag.BoolVar(&disableGcsAuth, "disable-gcs-auth", false, "Disable GCS Authentication. Used for local integration tests")
	// adds the possibility to run a single dockerfile. This is useful since running all images can exhaust the dockerhub pull limit
	flag.StringVar(&c.dockerfilesPattern, "dockerfiles-pattern", "Dockerfile_test*", "The pattern to match dockerfiles with")
	flag.Parse()

	if len(c.serviceAccount) > 0 {
		absPath, err := filepath.Abs("../" + c.serviceAccount)
		if err != nil {
			log.Fatalf("Error getting absolute path for service account: %s\n", c.serviceAccount)
		}
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			log.Fatalf("Service account does not exist: %s\n", absPath)
		}
		c.serviceAccount = absPath
		os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", absPath)
	}

	if c.imageRepo == "" {
		log.Fatal("You must provide a image repository")
	}

	if c.isGcrRepository() && c.gcsBucket == "" {
		log.Fatalf("You must provide a gcs bucket when using a Google Container Registry (\"%s\" was provided)", c.imageRepo)
	}
	if !strings.HasSuffix(c.imageRepo, "/") {
		c.imageRepo = c.imageRepo + "/"
	}

	if c.gcsBucket != "" {
		var opts []option.ClientOption
		if gcsEndpoint != "" {
			opts = append(opts, option.WithEndpoint(gcsEndpoint))
		}
		if disableGcsAuth {
			opts = append(opts, option.WithoutAuthentication())
		}

		gcsClient, err := bucket.NewClient(context.Background(), opts...)
		if err != nil {
			log.Fatalf("Could not create a new Google Storage Client: %s", err)
		}
		c.gcsClient = gcsClient
	}

	c.dockerMajorVersion = getDockerMajorVersion()
	c.onbuildBaseImage = c.imageRepo + "onbuild-base:latest"
	c.onbuildCopyImage = c.imageRepo + "onbuild-copy:latest"
	c.hardlinkBaseImage = c.imageRepo + "hardlink-base:latest"
	c.hijackBaseImage = c.imageRepo + "hijack:latest"
	return &c
}

func meetsRequirements() bool {
	requiredTools := []string{"diffoci"}
	hasRequirements := true
	for _, tool := range requiredTools {
		_, err := exec.LookPath(tool)
		if err != nil {
			fmt.Printf("You must have %s installed and on your PATH\n", tool)
			hasRequirements = false
		}
	}
	return hasRequirements
}

// containerDiff compares the container images image1 and image2.
func containerDiff(t *testing.T, image1, image2 string, flags ...string) {
	// workaround for container-diff OCI issue https://github.com/GoogleContainerTools/container-diff/issues/389
	dockerPullCmd := exec.Command("docker", "pull", image1)
	out := RunCommand(dockerPullCmd, t)
	t.Logf("docker pull cmd output for image1 = %s", string(out))
	image1 = daemonPrefix + image1

	dockerPullCmd = exec.Command("docker", "pull", image2)
	out = RunCommand(dockerPullCmd, t)
	t.Logf("docker pull cmd output for image2 = %s", string(out))
	image2 = daemonPrefix + image2

	flags = append([]string{"diff"}, flags...)
	flags = append(flags, image1, image2, "--ignore-image-name", "--ignore-image-timestamps")
	flags = append(flags, diffArgsMap[t.Name()]...)

	containerdiffCmd := exec.Command("diffoci", flags...)
	diff := RunCommand(containerdiffCmd, t)
	t.Logf("diff = %s", string(diff))
}
