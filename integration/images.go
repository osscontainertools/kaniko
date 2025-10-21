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
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"github.com/osscontainertools/kaniko/pkg/timing"
	"github.com/osscontainertools/kaniko/pkg/util"
	"github.com/osscontainertools/kaniko/pkg/util/bucket"
)

const (
	// ExecutorImage is the name of the kaniko executor image
	ExecutorImage = "executor-image"
	// WarmerImage is the name of the kaniko cache warmer image
	WarmerImage = "warmer-image"

	dockerPrefix     = "docker-"
	kanikoPrefix     = "kaniko-"
	buildContextPath = "/workspace"
	cacheDir         = "/workspace/cache"
	baseImageToCache = "debian:12.10@sha256:264982ff4d18000fa74540837e2c43ca5137a53a83f8f62c7b3803c0f0bdcd56"
)

// Arguments to build Dockerfiles with, used for both docker and kaniko builds
var argsMap = map[string][]string{
	"Dockerfile_test_run":        {"file=/file"},
	"Dockerfile_test_run_new":    {"file=/file"},
	"Dockerfile_test_run_redo":   {"file=/file"},
	"Dockerfile_test_workdir":    {"workdir=/arg/workdir"},
	"Dockerfile_test_add":        {"file=context/foo"},
	"Dockerfile_test_arg_secret": {"SSH_PRIVATE_KEY", "SSH_PUBLIC_KEY=Pµbl1cK€Y"},
	"Dockerfile_test_onbuild":    {"file=/tmp/onbuild"},
	"Dockerfile_test_scratch": {
		"image=scratch",
		"hello=hello-value",
		"file=context/foo",
		"file3=context/b*",
	},
	"Dockerfile_test_multistage": {"file=/foo2"},
}

// Environment to build Dockerfiles with, used for both docker and kaniko builds
var envsMap = map[string][]string{
	"Dockerfile_test_arg_secret":                 {"SSH_PRIVATE_KEY=ThEPriv4t3Key"},
	"Dockerfile_test_issue_519":                  {"DOCKER_BUILDKIT=0", "FF_KANIKO_SQUASH_STAGES=0"},
	"Dockerfile_test_cmd":                        {"FF_KANIKO_SQUASH_STAGES=0"},
	"Dockerfile_test_issue_mz247":                {"FF_KANIKO_SQUASH_STAGES=0"},
	"Dockerfile_test_multistage_args_issue_1911": {"FF_KANIKO_SQUASH_STAGES=0"},
	"Dockerfile_test_issue_mz276":                {"FF_KANIKO_SQUASH_STAGES=0"},
	"Dockerfile_test_pre_defined_build_args":     {"FF_KANIKO_SQUASH_STAGES=0"},
	"Dockerfile_test_issue_1039":                 {"FF_KANIKO_SQUASH_STAGES=0"},
	"Dockerfile_test_issue_2066":                 {"FF_KANIKO_SQUASH_STAGES=0"},
	"Dockerfile_test_issue_1837":                 {"FF_KANIKO_SQUASH_STAGES=0"},
}

var KanikoEnv = []string{
	"FF_KANIKO_COPY_AS_ROOT=1",
	"FF_KANIKO_OCI_STAGES=1",
	"FF_KANIKO_IGNORE_CACHED_MANIFEST=1",
}

// Arguments to build Dockerfiles with when building with docker
var additionalDockerFlagsMap = map[string][]string{
	"Dockerfile_test_target": {"--target=second"},
}

// Arguments to build Dockerfiles with when building with kaniko
var additionalKanikoFlagsMap = map[string][]string{
	"Dockerfile_test_add":                    {"--single-snapshot"},
	"Dockerfile_test_run_new":                {"--use-new-run=true"},
	"Dockerfile_test_run_redo":               {"--snapshot-mode=redo"},
	"Dockerfile_test_scratch":                {"--single-snapshot"},
	"Dockerfile_test_maintainer":             {"--single-snapshot"},
	"Dockerfile_test_target":                 {"--target=second"},
	"Dockerfile_test_snapshotter_ignorelist": {"--use-new-run=true", "-v=trace"},
	"Dockerfile_test_cache":                  {"--cache-copy-layers=true"},
	"Dockerfile_test_cache_oci":              {"--cache-copy-layers=true"},
	"Dockerfile_test_cache_install":          {"--cache-copy-layers=true"},
	"Dockerfile_test_cache_install_oci":      {"--cache-copy-layers=true"},
	"Dockerfile_test_cache_copy":             {"--cache-copy-layers=true"},
	"Dockerfile_test_cache_copy_oci":         {"--cache-copy-layers=true"},
	"Dockerfile_test_issue_add":              {"--cache-copy-layers=true"},
	"Dockerfile_test_volume_3":               {"--skip-unused-stages=false"},
	"Dockerfile_test_multistage":             {"--skip-unused-stages=false"},
	"Dockerfile_test_copy_root_multistage":   {"--skip-unused-stages=false"},
}

// Arguments to diffoci when comparing dockerfiles
var diffArgsMap = map[string][]string{
	// /root/.config 0x1c0 0x1ed
	// I suspect the issue is that /root/.config pre-exists,
	// it's where we store the docker credentials.
	"TestWithContext/test_with_context_issue-1020": {"--extra-ignore-files=root/.config/"},
	// docker is wrong. we do copy the symlink correctly.
	"TestRun/test_Dockerfile_test_copy_symlink": {"--extra-ignore-files=workdirAnother/relative_link"},
	"TestRun/test_Dockerfile_test_multistage":   {"--extra-ignore-files=new"},
	// when we untar we overwrite the parent directory, buildkit doesnt
	"TestRun/test_Dockerfile_test_add": {"--extra-ignore-file-permissions"},
	// Verify we don't store root directory
	"TestRun/test_Dockerfile_test_root": {"--extra-ignore-layer-length-mismatch=false"},
	// FROM scratch we start with root, buildkit doesnt
	"TestRun/test_Dockerfile_test_workdir_with_user": {"--extra-ignore-file-permissions"},
	// We don't handle user nobody=-1 nogroup=-1 correctly
	// if group is not set, buildkit defaults to 0
	"TestRun/test_Dockerfile_test_user_nonexisting": {"--extra-ignore-file-permissions"},
	// #mz155: `COPY --from` does not copy the timestamps from the source but touches new files with new timestamps.
	// To test this we have to deactivate `--ignore-file-timestamps`. This is achieved here by deactivating `--semantic` comparison,
	// which we pass by default, and then activating all the necessary ignores except file-timestamps.
	// We do ignore /tmp directory as the timestamp on that directory will be altered if we create a new file inside.
	// for some reason buildkit switches to USTAR instead of PAX format and we don't
	"TestRun/test_Dockerfile_test_issue_mz155": {"--semantic=false", "--ignore-history", "--ignore-file-meta-format", "--ignore-file-atime", "--ignore-file-ctime", "--extra-ignore-files=tmp/"},
	// We enforce predefined ARGs are identical by dumping them to a file
	"TestRun/test_Dockerfile_test_pre_defined_build_args": {"--extra-ignore-file-content=false"},
}

// output check to do when building with kaniko
var outputChecks = map[string]func(string, []byte) error{
	"Dockerfile_test_arg_secret": checkArgsNotPrinted,
	"Dockerfile_test_snapshotter_ignorelist": func(_ string, out []byte) error {
		for _, s := range []string{
			"Adding whiteout for /dev",
		} {
			if strings.Contains(string(out), s) {
				return fmt.Errorf("output must not contain %s", s)
			}
		}

		for _, s := range []string{
			"Resolved symlink /hello to /dev/null",
			"Path /dev/null is ignored, ignoring it",
		} {
			if !strings.Contains(string(out), s) {
				return fmt.Errorf("output must contain %s", s)
			}
		}

		return nil
	},
}

var warmerOutputChecks = map[string]func(string, []byte) error{
	"Dockerfile_test_issue_mz320": func(_ string, out []byte) error {
		s := "Found sha256:6bc30d909583f38600edd6609e29eb3fb284ab8affce8d0389f332fc91c2dd91 in local cache"
		if !strings.Contains(string(out), s) {
			return fmt.Errorf("output must contain %s", s)
		}
		return nil
	},
}

func checkNoWarnings(_ string, out []byte) error {
	if strings.Contains(string(out), "WARN") {
		return fmt.Errorf("output must not contain WARN")
	}
	return nil
}

// Checks if argument are not printed in output.
// Argument may be passed through --build-arg key=value manner or --build-arg key with key in environment
func checkArgsNotPrinted(dockerfile string, out []byte) error {
	for _, arg := range argsMap[dockerfile] {
		argSplitted := strings.Split(arg, "=")
		if len(argSplitted) == 2 {
			if idx := bytes.Index(out, []byte(argSplitted[1])); idx >= 0 {
				return fmt.Errorf("Argument value %s for argument %s displayed in output", argSplitted[1], argSplitted[0])
			}
		} else if len(argSplitted) == 1 {
			if envs, ok := envsMap[dockerfile]; ok {
				for _, env := range envs {
					envSplitted := strings.Split(env, "=")
					if len(envSplitted) == 2 {
						if idx := bytes.Index(out, []byte(envSplitted[1])); idx >= 0 {
							return fmt.Errorf("Argument value %s for argument %s displayed in output", envSplitted[1], argSplitted[0])
						}
					}
				}
			}
		}
	}
	return nil
}

var (
	bucketContextTests = []string{"Dockerfile_test_copy_bucket"}
	reproducibleTests  = []string{"Dockerfile_test_reproducible"}
)

// GetDockerImage constructs the name of the docker image that would be built with
// dockerfile if it was tagged with imageRepo.
func GetDockerImage(imageRepo, dockerfile string) string {
	return strings.ToLower(imageRepo + dockerPrefix + dockerfile)
}

// GetKanikoImage constructs the name of the kaniko image that would be built with
// dockerfile if it was tagged with imageRepo.
func GetKanikoImage(imageRepo, dockerfile string) string {
	return strings.ToLower(imageRepo + kanikoPrefix + dockerfile)
}

// GetVersionedKanikoImage versions constructs the name of the kaniko image that would be built
// with the dockerfile and versions it for cache testing
func GetVersionedKanikoImage(imageRepo, dockerfile string, version int) string {
	return strings.ToLower(imageRepo + kanikoPrefix + dockerfile + strconv.Itoa(version))
}

// FindDockerFiles will look for test docker files in the directory dir
// and match the files against dockerfilesPattern.
// If the file is one we are intentionally
// skipping, it will not be included in the returned list.
func FindDockerFiles(dir, dockerfilesPattern string) ([]string, error) {
	pattern := filepath.Join(dir, dockerfilesPattern)
	fmt.Printf("finding docker images with pattern %v\n", pattern)
	allDockerfiles, err := filepath.Glob(pattern)
	if err != nil {
		return []string{}, fmt.Errorf("Failed to find docker files with pattern %s: %w", dockerfilesPattern, err)
	}

	var dockerfiles []string
	for _, dockerfile := range allDockerfiles {
		// Remove the leading directory from the path
		dockerfile = dockerfile[len("dockerfiles/"):]
		dockerfiles = append(dockerfiles, dockerfile)

	}
	return dockerfiles, err
}

// DockerFileBuilder knows how to build docker files using both Kaniko and Docker and
// keeps track of which files have been built.
type DockerFileBuilder struct {
	// Holds all available docker files and whether or not they've been built
	filesBuilt              map[string]struct{}
	DockerfilesToIgnore     map[string]struct{}
	TestCacheDockerfiles    map[string]struct{}
	TestOCICacheDockerfiles map[string]struct{}
}

type logger func(string, ...interface{})

// NewDockerFileBuilder will create a DockerFileBuilder initialized with dockerfiles, which
// it will assume are all as yet unbuilt.
func NewDockerFileBuilder() *DockerFileBuilder {
	d := DockerFileBuilder{
		filesBuilt: map[string]struct{}{},
	}
	d.DockerfilesToIgnore = map[string]struct{}{
		"Dockerfile_test_add_404": {},
		// TODO: remove test_user_run from this when https://github.com/GoogleContainerTools/container-diff/issues/237 is fixed
		"Dockerfile_test_user_run": {},
		// TODO: All the below tests are fialing with errro
		// You don't have the needed permissions to perform this operation, and you may have invalid credentials.
		// To authenticate your request, follow the steps in: https://cloud.google.com/container-registry/docs/advanced-authentication
		"Dockerfile_test_onbuild":    {},
		"Dockerfile_test_extraction": {},
		"Dockerfile_test_hardlink":   {},
	}
	d.TestCacheDockerfiles = map[string]struct{}{
		"Dockerfile_test_cache":         {},
		"Dockerfile_test_cache_install": {},
		"Dockerfile_test_cache_perm":    {},
		"Dockerfile_test_cache_copy":    {},
		"Dockerfile_test_issue_3429":    {},
		"Dockerfile_test_issue_workdir": {},
		"Dockerfile_test_issue_add":     {},
		"Dockerfile_test_issue_empty":   {},
		"Dockerfile_test_issue_mz320":   {},
	}
	d.TestOCICacheDockerfiles = map[string]struct{}{
		"Dockerfile_test_cache_oci":         {},
		"Dockerfile_test_cache_install_oci": {},
		"Dockerfile_test_cache_perm_oci":    {},
		"Dockerfile_test_cache_copy_oci":    {},
	}
	return &d
}

func addServiceAccountFlags(flags []string, serviceAccount string) []string {
	if len(serviceAccount) > 0 {
		flags = append(flags, "-e",
			"GOOGLE_APPLICATION_CREDENTIALS=/secret/"+filepath.Base(serviceAccount),
			"-v", filepath.Dir(serviceAccount)+":/secret/")
	} else {
		gcloudConfig := os.Getenv("HOME") + "/.config/gcloud"
		if util.FilepathExists(gcloudConfig) {
			flags = append(flags, "-v", gcloudConfig+":/root/.config/gcloud")
		}

		dockerConfig := os.Getenv("HOME") + "/.docker/config.json"
		if util.FilepathExists(dockerConfig) {
			flags = append(flags, "-v", dockerConfig+":/root/.docker/config.json", "-e", "DOCKER_CONFIG=/root/.docker")
		}
	}
	return flags
}

func (d *DockerFileBuilder) BuildDockerImage(t *testing.T, imageRepo, dockerfilesPath, dockerfile, contextDir string) error {
	t.Logf("Building image for Dockerfile %s\n", dockerfile)

	var buildArgs []string
	buildArgFlag := "--build-arg"
	for _, arg := range argsMap[dockerfile] {
		buildArgs = append(buildArgs, buildArgFlag, arg)
	}

	// build docker image
	additionalFlags := append(buildArgs, additionalDockerFlagsMap[dockerfile]...)
	dockerImage := strings.ToLower(imageRepo + dockerPrefix + dockerfile)

	dockerArgs := []string{
		"build",
		"--no-cache",
		"-t", dockerImage,
	}

	if dockerfilesPath != "" {
		dockerArgs = append(dockerArgs, "-f", path.Join(dockerfilesPath, dockerfile))
	}

	dockerArgs = append(dockerArgs, contextDir)
	dockerArgs = append(dockerArgs, additionalFlags...)

	dockerCmd := exec.Command("docker", dockerArgs...)
	if env, ok := envsMap[dockerfile]; ok {
		dockerCmd.Env = append(dockerCmd.Env, env...)
	}

	out, err := RunCommandWithoutTest(dockerCmd)
	if err != nil {
		return fmt.Errorf("Failed to build image %s with docker command \"%s\": %w %s", dockerImage, dockerCmd.Args, err, string(out))
	}
	t.Logf("Build image for Dockerfile %s as %s. docker build output: %s \n", dockerfile, dockerImage, out)
	return nil
}

// BuildImage will build dockerfile (located at dockerfilesPath) using both kaniko and docker.
// The resulting image will be tagged with imageRepo. If the dockerfile will be built with
// context (i.e. it is in `buildContextTests`) the context will be pulled from gcsBucket.
func (d *DockerFileBuilder) BuildImage(t *testing.T, config *integrationTestConfig, dockerfilesPath, dockerfile string) error {
	_, ex, _, _ := runtime.Caller(0)
	cwd := filepath.Dir(ex)

	return d.BuildImageWithContext(t, config, dockerfilesPath, dockerfile, cwd)
}

func (d *DockerFileBuilder) BuildImageWithContext(t *testing.T, config *integrationTestConfig, dockerfilesPath, dockerfile, contextDir string) error {
	if _, present := d.filesBuilt[dockerfile]; present {
		return nil
	}
	gcsBucket, gcsClient, serviceAccount, imageRepo := config.gcsBucket, config.gcsClient, config.serviceAccount, config.imageRepo

	var buildArgs []string
	buildArgFlag := "--build-arg"
	for _, arg := range argsMap[dockerfile] {
		buildArgs = append(buildArgs, buildArgFlag, arg)
	}

	timer := timing.Start(dockerfile + "_docker")
	if err := d.BuildDockerImage(t, imageRepo, dockerfilesPath, dockerfile, contextDir); err != nil {
		return err
	}

	timing.DefaultRun.Stop(timer)

	contextFlag := "-c"
	contextPath := buildContextPath
	for _, d := range bucketContextTests {
		if d == dockerfile {
			contextFlag = "-b"
			contextPath = gcsBucket
		}
	}

	additionalKanikoFlags := additionalKanikoFlagsMap[dockerfile]
	additionalKanikoFlags = append(additionalKanikoFlags, contextFlag, contextPath)
	for _, d := range reproducibleTests {
		if d == dockerfile {
			additionalKanikoFlags = append(additionalKanikoFlags, "--reproducible")
			break
		}
	}

	kanikoImage := GetKanikoImage(imageRepo, dockerfile)
	timer = timing.Start(dockerfile + "_kaniko")
	if _, err := buildKanikoImage(t.Logf, dockerfilesPath, dockerfile, buildArgs, additionalKanikoFlags, kanikoImage,
		contextDir, gcsBucket, gcsClient, serviceAccount, true); err != nil {
		return err
	}
	timing.DefaultRun.Stop(timer)

	d.filesBuilt[dockerfile] = struct{}{}

	return nil
}

func populateVolumeCache() error {
	_, ex, _, _ := runtime.Caller(0)
	cwd := filepath.Dir(ex)
	warmerCmd := exec.Command("docker",
		append([]string{
			"run", "--net=host",
			"-v", os.Getenv("HOME") + "/.config/gcloud:/root/.config/gcloud",
			"-v", cwd + ":/workspace",
			WarmerImage,
			"-c", cacheDir,
			"-i", baseImageToCache,
		},
		)...,
	)

	if _, err := RunCommandWithoutTest(warmerCmd); err != nil {
		return fmt.Errorf("Failed to warm kaniko cache: %w", err)
	}

	return nil
}

// buildCachedImage builds the image for testing caching via kaniko where version is the nth time this image has been built
func (d *DockerFileBuilder) buildCachedImage(logf logger, config *integrationTestConfig, cacheRepo, dockerfilesPath, dockerfile string, version int, args []string) error {
	imageRepo, serviceAccount := config.imageRepo, config.serviceAccount
	_, ex, _, _ := runtime.Caller(0)
	cwd := filepath.Dir(ex)

	cacheFlag := "--cache=true"

	benchmarkEnv := "BENCHMARK_FILE=false"
	if b, err := strconv.ParseBool(os.Getenv("BENCHMARK")); err == nil && b {
		os.Mkdir("benchmarks", 0o755)
		benchmarkEnv = "BENCHMARK_FILE=/workspace/benchmarks/" + dockerfile
	}
	kanikoImage := GetVersionedKanikoImage(imageRepo, dockerfile, version)

	dockerRunFlags := []string{
		"run", "--net=host",
		"-v", cwd + ":/workspace",
		"-e", benchmarkEnv,
	}
	for _, envVariable := range KanikoEnv {
		dockerRunFlags = append(dockerRunFlags, "-e", envVariable)
	}
	dockerRunFlags = addServiceAccountFlags(dockerRunFlags, serviceAccount)
	dockerRunFlags = append(dockerRunFlags, ExecutorImage,
		"-f", path.Join(buildContextPath, dockerfilesPath, dockerfile),
		"-d", kanikoImage,
		"-c", buildContextPath,
		cacheFlag,
		"--cache-repo", cacheRepo,
		"--cache-dir", cacheDir)
	for _, v := range args {
		dockerRunFlags = append(dockerRunFlags, v)
	}
	kanikoCmd := exec.Command("docker", dockerRunFlags...)

	out, err := RunCommandWithoutTest(kanikoCmd)
	logf(string(out))

	if err != nil {
		return fmt.Errorf("Failed to build cached image %s with kaniko command \"%s\": %w", kanikoImage, kanikoCmd.Args, err)
	}
	if outputCheck := outputChecks[dockerfile]; outputCheck != nil {
		if err := outputCheck(dockerfile, out); err != nil {
			return fmt.Errorf("Output check failed for image %s with kaniko command : %w", kanikoImage, err)
		}
	}
	if outputCheck := warmerOutputChecks[dockerfile]; outputCheck != nil {
		if err := outputCheck(dockerfile, out); err != nil {
			return fmt.Errorf("Output check failed for image %s with kaniko command : %w", kanikoImage, err)
		}
	}
	if err := checkNoWarnings(dockerfile, out); err != nil {
		return err
	}
	return nil
}

// buildRelativePathsImage builds the images for testing passing relatives paths to Kaniko
func (d *DockerFileBuilder) buildRelativePathsImage(logf logger, imageRepo, dockerfile, serviceAccount, buildContextPath string) error {
	_, ex, _, _ := runtime.Caller(0)
	cwd := filepath.Dir(ex)

	dockerImage := GetDockerImage(imageRepo, "test_relative_"+dockerfile)
	kanikoImage := GetKanikoImage(imageRepo, "test_relative_"+dockerfile)

	dockerCmd := exec.Command("docker",
		append([]string{
			"build",
			"-t", dockerImage,
			"-f", dockerfile,
			"./context",
		},
		)...,
	)

	timer := timing.Start(dockerfile + "_docker")
	out, err := RunCommandWithoutTest(dockerCmd)
	timing.DefaultRun.Stop(timer)
	if err != nil {
		return fmt.Errorf("Failed to build image %s with docker command \"%s\": %w %s", dockerImage, dockerCmd.Args, err, string(out))
	}

	dockerRunFlags := []string{"run", "--net=host", "-v", cwd + ":/workspace"}
	for _, envVariable := range KanikoEnv {
		dockerRunFlags = append(dockerRunFlags, "-e", envVariable)
	}
	dockerRunFlags = addServiceAccountFlags(dockerRunFlags, serviceAccount)
	dockerRunFlags = append(dockerRunFlags, ExecutorImage,
		"-f", dockerfile,
		"-d", kanikoImage,
		"-c", buildContextPath)

	kanikoCmd := exec.Command("docker", dockerRunFlags...)

	timer = timing.Start(dockerfile + "_kaniko_relative_paths")
	out, err = RunCommandWithoutTest(kanikoCmd)
	timing.DefaultRun.Stop(timer)
	logf(string(out))

	if err != nil {
		return fmt.Errorf(
			"Failed to build relative path image %s with kaniko command \"%s\": %w",
			kanikoImage, kanikoCmd.Args, err)
	}
	if outputCheck := outputChecks[dockerfile]; outputCheck != nil {
		if err := outputCheck(dockerfile, out); err != nil {
			return fmt.Errorf("Output check failed for image %s with kaniko command : %w", kanikoImage, err)
		}
	}
	if err := checkNoWarnings(dockerfile, out); err != nil {
		return err
	}
	return nil
}

func buildKanikoImage(
	logf logger,
	dockerfilesPath string,
	dockerfile string,
	buildArgs []string,
	kanikoArgs []string,
	kanikoImage string,
	contextDir string,
	gcsBucket string,
	gcsClient *storage.Client,
	serviceAccount string,
	shdUpload bool,
) (string, error) {
	benchmarkEnv := "BENCHMARK_FILE=false"
	benchmarkDir, err := os.MkdirTemp("", "")
	if err != nil {
		return "", err
	}
	if b, err := strconv.ParseBool(os.Getenv("BENCHMARK")); err == nil && b {
		benchmarkEnv = "BENCHMARK_FILE=/kaniko/benchmarks/" + dockerfile
		if shdUpload {
			benchmarkFile := path.Join(benchmarkDir, dockerfile)
			fileName := fmt.Sprintf("run_%s_%s", time.Now().Format("2006-01-02-15:04"), dockerfile)
			dst := path.Join("benchmarks", fileName)
			file, err := os.Open(benchmarkFile)
			if err != nil {
				return "", err
			}
			defer bucket.Upload(context.Background(), gcsBucket, dst, file, gcsClient)
		}
	}

	// build kaniko image
	additionalFlags := append(buildArgs, kanikoArgs...)
	logf("Going to build image with kaniko: %s, flags: %s \n", kanikoImage, additionalFlags)

	dockerRunFlags := []string{
		"run", "--net=host",
		"-e", benchmarkEnv,
		"-v", contextDir + ":/workspace:ro",
		"-v", benchmarkDir + ":/kaniko/benchmarks",
	}

	for _, envVariable := range KanikoEnv {
		dockerRunFlags = append(dockerRunFlags, "-e", envVariable)
	}
	if env, ok := envsMap[dockerfile]; ok {
		for _, envVariable := range env {
			dockerRunFlags = append(dockerRunFlags, "-e", envVariable)
		}
	}

	dockerRunFlags = addServiceAccountFlags(dockerRunFlags, serviceAccount)

	kanikoDockerfilePath := path.Join(buildContextPath, dockerfilesPath, dockerfile)
	if dockerfilesPath == "" {
		kanikoDockerfilePath = path.Join(buildContextPath, "Dockerfile")
	}

	dockerRunFlags = append(dockerRunFlags, ExecutorImage,
		"-f", kanikoDockerfilePath,
		"-d", kanikoImage,
	)
	dockerRunFlags = append(dockerRunFlags, additionalFlags...)

	kanikoCmd := exec.Command("docker", dockerRunFlags...)

	out, err := RunCommandWithoutTest(kanikoCmd)
	logf(string(out))

	if err != nil {
		return "", fmt.Errorf("Failed to build image %s with kaniko command \"%s\": %w", kanikoImage, kanikoCmd.Args, err)
	}
	if outputCheck := outputChecks[dockerfile]; outputCheck != nil {
		if err := outputCheck(dockerfile, out); err != nil {
			return "", fmt.Errorf("Output check failed for image %s with kaniko command : %w", kanikoImage, err)
		}
	}
	if err := checkNoWarnings(dockerfile, out); err != nil {
		return "", err
	}
	return benchmarkDir, nil
}
