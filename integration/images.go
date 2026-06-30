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
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/osscontainertools/kaniko/pkg/util"
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

	ExecutorImageMoved   = "executor-image-moved"
	ExecutorImageTainted = "executor-image-tainted"
	AlpineImage          = "alpine-image"
)

var coverageDir string

func addCoverageFlags(flags []string) []string {
	if coverageDir == "" {
		return flags
	}
	return append(flags, "-v", coverageDir+":/covdata", "-e", "GOCOVERDIR=/covdata")
}

// addKanikoEnvFlags passes the suite's feature-flag matrix (KanikoEnv) to the executor
// container. Every executor invocation must use it so all tests exercise the same flags.
func addKanikoEnvFlags(flags []string) []string {
	for _, envVariable := range KanikoEnv {
		flags = append(flags, "-e", envVariable)
	}
	return flags
}

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
	"Dockerfile_test_multistage":  {"file=/foo2"},
	"Dockerfile_test_issue_mz655": {"BASE_TAG=1.37.0"},
}

var argsMapVersion1 = map[string][]string{
	"Dockerfile_test_issue_mz655": {"BASE_TAG=1.36.1"},
}

// Environment to build Dockerfiles with, used for both docker and kaniko builds
var envsMap = map[string][]string{
	"Dockerfile_test_arg_secret":  {"SSH_PRIVATE_KEY=ThEPriv4t3Key"},
	"Dockerfile_test_issue_519":   {"DOCKER_BUILDKIT=0"},
	"Dockerfile_test_issue_cg188": {"SECRET=blubb"},
	"Dockerfile_test_issue_mz774": {"FF_KANIKO_SKIP_WRITE_WHITEOUTS=1"},
	"Dockerfile_test_issue_mz775": {"FF_KANIKO_CACHE_LOOKAHEAD=0", "FF_KANIKO_SKIP_RELABEL_RECOMPRESS=1"},
	"Dockerfile_test_issue_mz793": {"FF_KANIKO_VOLUME_SKIP_MKDIR=0"},
	"Dockerfile_test_issue_mz473": {"KANIKO_DIR=/kaniko2"},
	"Dockerfile_test_issue_mz661": {"KANIKO_DIR=/kaniko2"},
	"Dockerfile_test_stopsignal":  {"FF_KANIKO_OCI_SCRATCH_BASE=0"},
	"Dockerfile_test_healthcheck": {"FF_KANIKO_OCI_SCRATCH_BASE=0"},
}

var KanikoEnv = []string{
	"FF_KANIKO_COPY_AS_ROOT=1",
	"FF_KANIKO_RUN_VIA_TINI=1",
	"FF_KANIKO_COPY_CHMOD_ON_IMPLICIT_DIRS=1",
	"FF_KANIKO_OCI_SCRATCH_BASE=1",
	"FF_KANIKO_INFER_CROSS_STAGE_CACHE_KEY=1",
	"FF_KANIKO_CACHE_LOOKAHEAD=1",
	"FF_KANIKO_SCOPED_DOCKERIGNORE=1",
	"FF_KANIKO_RESOLVE_CACHE_KEY=1",
	"FF_KANIKO_ROLLING_CACHE_KEY=1",
	"FF_KANIKO_UNTAR_SKIP_ROOT=1",
	"FF_KANIKO_REPRODUCIBLE_PRESERVE_BASE_LAYERS=1",
	"FF_KANIKO_RUN_HONOR_GROUP=1",
	"FF_KANIKO_PRECOMPILE_DOCKERIGNORE=1",
	"FF_KANIKO_EXPAND_HEREDOC=1",
	"KANIKO_PRINT_PLAN=1",
}

var WarmerEnv = []string{}

// Arguments to build Dockerfiles with when building with docker
var additionalDockerFlagsMap = map[string][]string{
	"Dockerfile_test_target":      {"--target=second"},
	"Dockerfile_test_issue_cg188": {"--secret=id=netrc,env=SECRET"},
	"Dockerfile_test_issue_mz511": {"--secret=id=netrc,src=context/foo"},
	"Dockerfile_test_issue_mz661": {"--secret=id=kaniko,src=context/foo"},
	// provenance forces ociv1 on buildkit but for these images we emit dockerv2 in kaniko
	"Dockerfile_test_cross_compile":                {"--platform=linux/" + crossCompileArch},
	"Dockerfile_test_snapshotter_ignorelist":       {"--provenance=false"},
	"Dockerfile_test_whitelist":                    {"--provenance=false"},
	"Dockerfile_test_volume_4":                     {"--provenance=false"},
	"Dockerfile_test_volume_3":                     {"--provenance=false"},
	"Dockerfile_test_meta_arg":                     {"--provenance=false"},
	"Dockerfile_test_replaced_symlinks":            {"--provenance=false"},
	"Dockerfile_test_pre_defined_build_args":       {"--provenance=false"},
	"Dockerfile_test_replaced_hardlinks":           {"--provenance=false"},
	"Dockerfile_test_issue_647":                    {"--provenance=false"},
	"Dockerfile_test_copy_root_multistage":         {"--provenance=false"},
	"Dockerfile_test_issue_1837":                   {"--provenance=false"},
	"Dockerfile_test_issue_2049":                   {"--provenance=false"},
	"Dockerfile_test_issue_1039":                   {"--provenance=false"},
	"Dockerfile_test_copyadd_chmod":                {"--provenance=false"},
	"Dockerfile_test_copy_reproducible":            {"--provenance=false"},
	"Dockerfile_test_copy_chown_intermediate_dirs": {"--provenance=false"},
	"Dockerfile_test_copy":                         {"--provenance=false"},
	"Dockerfile_test_copy_bucket":                  {"--provenance=false"},
	"Dockerfile_test_cache_copy_oci":               {"--provenance=false"},
	"Dockerfile_test_add_url_with_arg":             {"--provenance=false"},
	"Dockerfile_test_add_dest_symlink_dir":         {"--provenance=false"},
	"Dockerfile_test_add_chown_intermediate_dirs":  {"--provenance=false"},
	"Dockerfile_test_arg_two_level":                {"--provenance=false"},
	"Dockerfile_test_arg_multi_empty_val":          {"--provenance=false"},
	"issue-1020":                                   {"--provenance=false"},
	"issue-774":                                    {"--provenance=false"},
	"issue-1315":                                   {"--provenance=false"},
	"dockerfiles/Dockerfile_relative_copy":         {"--provenance=false"},
}

// Override which kaniko executor image to use for a specific test
var executorImages = map[string]string{
	"Dockerfile_test_issue_mz444": ExecutorImageMoved,
	"Dockerfile_test_issue_mz455": ExecutorImageTainted,
	"Dockerfile_test_issue_mz595": AlpineImage,
}

// Arguments to build Dockerfiles with when building with kaniko
var additionalKanikoFlagsMap = map[string][]string{
	"Dockerfile_test_issue_mz822":                {"--cache=true", "--cache-copy-layers=true"},
	"Dockerfile_test_issue_519":                  {"--target=final_stage,nosquash1,nosquash2"},
	"Dockerfile_test_multistage_args_issue_1911": {"--target=base-custom2,nosquash1,nosquash2,nosquash3"},
	"Dockerfile_test_cmd":                        {"--target=final,nosquash"},
	"Dockerfile_test_issue_mz247":                {"--target=final,nosquash"},
	"Dockerfile_test_issue_mz276":                {"--target=second,nosquash"},
	"Dockerfile_test_pre_defined_build_args":     {"--target=final,nosquash"},
	"Dockerfile_test_issue_1039":                 {"--target=final,nosquash"},
	"Dockerfile_test_issue_1837":                 {"--target=final,nosquash"},
	"Dockerfile_test_issue_2066":                 {"--target=b,nosquash"},
	"Dockerfile_test_issue_mz782":                {"--target=final,nosquash"},
	"Dockerfile_test_issue_mz775":                {"--compressed-caching=false", "--target=final,nosquash"},
	"Dockerfile_test_add":                        {"--single-snapshot"},
	"Dockerfile_test_issue_mz621":                {"--single-snapshot"},
	"Dockerfile_test_run_new":                    {"--use-new-run=true"},
	"Dockerfile_test_run_redo":                   {"--snapshot-mode=redo"},
	"Dockerfile_test_scratch":                    {"--single-snapshot"},
	"Dockerfile_test_maintainer":                 {"--single-snapshot"},
	"Dockerfile_test_target":                     {"--target=second"},
	"Dockerfile_test_snapshotter_ignorelist":     {"--use-new-run=true", "-v=trace"},
	"Dockerfile_test_issue_mz334":                {"--cache-copy-layers=true"},
	"Dockerfile_test_issue_mz879":                {"--cache-copy-layers=true", "--use-new-run"},
	"Dockerfile_test_issue_mz896":                {"--cache-copy-layers=true", "--cache-run-layers=false"},
	"Dockerfile_test_issue_mz787":                {"--cache=true"},
	"Dockerfile_test_issue_mz789":                {"--cache=true", "--target=final,nosquash"},
	"Dockerfile_test_cache":                      {"--cache-copy-layers=true"},
	"Dockerfile_test_cache_oci":                  {"--cache-copy-layers=true"},
	"Dockerfile_test_cache_install":              {"--cache-copy-layers=true"},
	"Dockerfile_test_cache_install_oci":          {"--cache-copy-layers=true"},
	"Dockerfile_test_cache_copy":                 {"--cache-copy-layers=true"},
	"Dockerfile_test_cache_copy_oci":             {"--cache-copy-layers=true"},
	"Dockerfile_test_issue_add":                  {"--cache-copy-layers=true"},
	"Dockerfile_test_issue_mz655":                {"--cache-copy-layers=true"},
	"Dockerfile_test_issue_mz873":                {"--reproducible"},
	"Dockerfile_test_issue_mz774":                {"--cache-copy-layers=true"},
	"Dockerfile_test_volume_3":                   {"--skip-unused-stages=false"},
	"Dockerfile_test_multistage":                 {"--skip-unused-stages=false"},
	"Dockerfile_test_copy_root_multistage":       {"--skip-unused-stages=false"},
	"Dockerfile_test_issue_cg188":                {"--secret=id=netrc,env=SECRET"},
	// mz511: we're using /etc/nsswitch.conf because it pre-exists
	// in the kaniko image and can therefore safely be deleted.
	"Dockerfile_test_issue_mz511":   {"--secret=id=netrc,src=/etc/nsswitch.conf", "--target=second,nosquash"},
	"Dockerfile_test_ignore_path":   {"--ignore-path=/kaniko-extra-file", "--ignore-path=/kaniko-extra-dir"},
	"Dockerfile_test_cross_compile": {"--custom-platform=linux/" + crossCompileArch},
	"Dockerfile_test_issue_mz529":   {"--cleanup", "--target=final,nosquash"},
	"Dockerfile_test_issue_mz595":   {"--cleanup"},
	"Dockerfile_test_issue_mz661":   {"--secret=id=kaniko,src=/kaniko/executor"},
}

var expectErr = map[string]int{
	"Dockerfile_test_issue_cg326_1": 1,
	"Dockerfile_test_add_404":       1,
}

var crossCompileArch = func() string {
	if runtime.GOARCH == "amd64" {
		return "arm64"
	}
	return "amd64"
}()

// Platform overrides for docker pull and diffoci, keyed by test name.
var platformMap = map[string]v1.Platform{
	"TestRun/test_Dockerfile_test_cross_compile":          {OS: "linux", Architecture: crossCompileArch},
	"TestLayers/test_layer_Dockerfile_test_cross_compile": {OS: "linux", Architecture: crossCompileArch},
}

// Arguments to diffoci when comparing dockerfiles
var diffArgsMap = map[string][]string{
	// /root/.config 0x1c0 0x1ed
	// I suspect the issue is that /root/.config pre-exists,
	// it's where we store the docker credentials.
	"TestWithContext/test_with_context_issue-1020": {"--extra-ignore-files=root/.config/", "--extra-ignore-layer-length-mismatch"},
	// docker is wrong. we do copy the symlink correctly.
	"TestRun/test_Dockerfile_test_copy_symlink":  {"--extra-ignore-files=workdirAnother/relative_link"},
	"TestRun/test_Dockerfile_test_multistage":    {"--extra-ignore-files=new", "--extra-ignore-layer-length-mismatch"},
	"TestRun/test_Dockerfile_test_cross_compile": {"--platform=linux/" + crossCompileArch},
	// kaniko adds parent directories of changed paths to the full-filesystem snapshot
	"TestRun/test_Dockerfile_test_volume":   {"--extra-ignore-layer-length-mismatch"},
	"TestRun/test_Dockerfile_test_volume_2": {"--extra-ignore-layer-length-mismatch"},
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
	// mz511: We delete the builtin file /etc/nsswitch.conf to verify that secrets are persisted
	// But we discovered a new issue with this. For builtins, buildkit will emit "whiteout" files,
	// to remember that it was removed, we don't. So we end up with a diff in the resulting image.
	"TestRun/test_Dockerfile_test_issue_mz511": {"--extra-ignore-files=etc/.wh.nsswitch.conf", "--extra-ignore-layer-length-mismatch"},
	// mz793: with FF_KANIKO_VOLUME_SKIP_MKDIR off, VOLUME creates the directory fresh on
	// each build, so its mtime differs between the two cached builds. That divergence is the
	// known volume non-determinism the flag fixes, here we only assert the build no longer panics.
	"TestCache/test_cache_Dockerfile_test_issue_mz793": {"--extra-ignore-files=data/"},
	// Layer-length divergences from buildkit, enforced per-test instead of globally
	"TestRun/test_Dockerfile_test_add":                       {"--extra-ignore-layer-length-mismatch"},
	"TestRun/test_Dockerfile_test_arg_blank_with_quotes":     {"--extra-ignore-layer-length-mismatch"},
	"TestRun/test_Dockerfile_test_arg_multi":                 {"--extra-ignore-layer-length-mismatch"},
	"TestRun/test_Dockerfile_test_arg_multi_with_quotes":     {"--extra-ignore-layer-length-mismatch"},
	"TestRun/test_Dockerfile_test_cache_install_oci":         {"--extra-ignore-layer-length-mismatch"},
	"TestRun/test_Dockerfile_test_copy_same_file_many_times": {"--extra-ignore-layer-length-mismatch"},
	"TestRun/test_Dockerfile_test_meta_arg":                  {"--extra-ignore-layer-length-mismatch"},
	"TestRun/test_Dockerfile_test_scratch":                   {"--extra-ignore-layer-length-mismatch"},
	"TestRun/test_Dockerfile_test_issue_969":                 {"--extra-ignore-layer-length-mismatch"},
	"TestRun/test_Dockerfile_test_issue_1007":                {"--extra-ignore-layer-length-mismatch"},
	"TestRun/test_Dockerfile_test_issue_1039":                {"--extra-ignore-layer-length-mismatch"},
	"TestRun/test_Dockerfile_test_issue_1568":                {"--extra-ignore-layer-length-mismatch"},
	"TestRun/test_Dockerfile_test_issue_1837":                {"--extra-ignore-layer-length-mismatch"},
	"TestRun/test_Dockerfile_test_issue_2049":                {"--extra-ignore-layer-length-mismatch"},
	"TestRun/test_Dockerfile_test_issue_2066":                {"--extra-ignore-layer-length-mismatch"},
	"TestRun/test_Dockerfile_test_issue_3393":                {"--extra-ignore-layer-length-mismatch"},
	"TestRun/test_Dockerfile_test_issue_cg73":                {"--extra-ignore-layer-length-mismatch"},
	"TestRun/test_Dockerfile_test_issue_cg188":               {"--extra-ignore-layer-length-mismatch"},
	"TestRun/test_Dockerfile_test_issue_mz247":               {"--extra-ignore-layer-length-mismatch"},
	"TestRun/test_Dockerfile_test_issue_mz332":               {"--extra-ignore-layer-length-mismatch"},
	"TestRun/test_Dockerfile_test_issue_mz455":               {"--extra-ignore-layer-length-mismatch"},
	"TestRun/test_Dockerfile_test_issue_mz560":               {"--extra-ignore-layer-length-mismatch"},
	"TestRun/test_Dockerfile_test_issue_mz725":               {"--extra-ignore-layer-length-mismatch"},
	"TestWithContext/test_with_context_issue-57":             {"--extra-ignore-layer-length-mismatch"},
	"TestWithContext/test_with_context_issue-1568":           {"--extra-ignore-layer-length-mismatch"},
	"TestK8s/test_k8s_with_context_issue-57":                 {"--extra-ignore-layer-length-mismatch"},
	"TestK8s/test_k8s_with_context_issue-1020":               {"--extra-ignore-layer-length-mismatch"},
	"TestK8s/test_k8s_with_context_issue-1568":               {"--extra-ignore-layer-length-mismatch"},
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

var cacheHitOutputChecks = map[string]func(string, []byte) error{
	"Dockerfile_test_issue_mz334": func(_ string, out []byte) error {
		for _, cmd := range []string{
			"COPY --from=first /blubb /blubb",
			"COPY --from=third /bli /bli",
			"COPY --from=debian:12.10 /etc/os-release /external-os-release",
		} {
			if !strings.Contains(string(out), "Cache hit via inferred cross-stage key for cmd: "+cmd) {
				return fmt.Errorf("expected inferred-key cache hit for %q but found none in output", cmd)
			}
		}
		return nil
	},
}

// can be removed once buildkit releases this fix
// https://github.com/moby/buildkit/issues/6712
var imageChecks = map[string]func(*testing.T, string){
	"Dockerfile_test_issue_mz334": func(t *testing.T, kanikoImage string) {
		t.Helper()
		out, err := exec.Command("docker", "inspect", "--format", `{{index .Config.Labels "from"}}`, kanikoImage).Output()
		if err != nil {
			t.Errorf("docker inspect: %v", err)
			return
		}
		// final stage is based on first; if second's LABEL mutated first's shared map the value is "second"
		if got, want := strings.TrimSpace(string(out)), "first"; got != want {
			t.Errorf("final stage label 'from': got %q, want %q (shallow-copy corruption from second stage)", got, want)
		}
	},
}

// Digest for debian:12.10 (see baseImageToCache)
const debian1210Digest = "6bc30d909583f38600edd6609e29eb3fb284ab8affce8d0389f332fc91c2dd91"

var warmerOutputChecks = map[string]func(string, []byte) error{
	"Dockerfile_test_issue_mz320": func(_ string, out []byte) error {
		s := fmt.Sprintf("Found sha256:%s in local cache", debian1210Digest)
		if !strings.Contains(string(out), s) {
			return fmt.Errorf("output must contain %s", s)
		}
		return nil
	},
}

// expectedWarnings maps a Dockerfile name to a warning substring that must appear in its output.
// Dockerfiles listed here are required to emit that warning; all others must emit no warnings.
var expectedWarnings = map[string]string{
	// mz640: COPY to /kaniko (ignored path) must warn rather than silently skip.
	"Dockerfile_test_issue_mz560": "Skipping copy targeting kaniko directory",
	// mz793: the test disables FF_KANIKO_VOLUME_SKIP_MKDIR, which the flag registry warns about.
	"Dockerfile_test_issue_mz793": "feature flags explicitly disabled, please create an issue for your use-case: FF_KANIKO_VOLUME_SKIP_MKDIR",
}

func checkNoWarnings(dockerfile string, out []byte) error {
	expected, hasExpected := expectedWarnings[dockerfile]
	found := false
	for line := range strings.SplitSeq(string(out), "\n") {
		if !strings.Contains(line, "WARN") {
			continue
		}
		if hasExpected && strings.Contains(line, expected) {
			found = true
			continue
		}
		return fmt.Errorf("unexpected WARN in output: %s", line)
	}
	if hasExpected && !found {
		return fmt.Errorf("expected WARN %q not found in output", expected)
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
				return fmt.Errorf("argument value %s for argument %s displayed in output", argSplitted[1], argSplitted[0])
			}
		} else if len(argSplitted) == 1 {
			if envs, ok := envsMap[dockerfile]; ok {
				for _, env := range envs {
					envSplitted := strings.Split(env, "=")
					if len(envSplitted) == 2 {
						if idx := bytes.Index(out, []byte(envSplitted[1])); idx >= 0 {
							return fmt.Errorf("argument value %s for argument %s displayed in output", envSplitted[1], argSplitted[0])
						}
					}
				}
			}
		}
	}
	return nil
}

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
		return []string{}, fmt.Errorf("failed to find docker files with pattern %s: %w", dockerfilesPattern, err)
	}

	var dockerfiles []string
	for _, dockerfile := range allDockerfiles {
		// Remove the leading directory from the path
		dockerfile = dockerfile[len("dockerfiles/"):]
		dockerfiles = append(dockerfiles, dockerfile)
	}
	return dockerfiles, err
}

type syncMap[K comparable, V any] struct{ m sync.Map }

func (s *syncMap[K, V]) LoadOrStore(key K, val V) (V, bool) {
	v, ok := s.m.LoadOrStore(key, val)
	return v.(V), ok
}

// DockerFileBuilder knows how to build docker files using both Kaniko and Docker and
// keeps track of which files have been built.
type DockerFileBuilder struct {
	// Holds all available docker files and whether or not they've been built
	filesBuilt                  syncMap[string, func() error]
	DockerfilesToIgnore         map[string]struct{}
	TestCacheDockerfiles        map[string]struct{}
	TestOCICacheDockerfiles     map[string]struct{}
	TestWarmerDockerfiles       map[string]struct{}
	TestReproducibleDockerfiles map[string]struct{}
}

type logger func(string, ...any)

// NewDockerFileBuilder will create a DockerFileBuilder initialized with dockerfiles, which
// it will assume are all as yet unbuilt.
func NewDockerFileBuilder() *DockerFileBuilder {
	d := DockerFileBuilder{}
	d.DockerfilesToIgnore = map[string]struct{}{
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
		"Dockerfile_test_issue_mz637":   {},
		"Dockerfile_test_issue_mz334":   {},
		"Dockerfile_test_issue_mz655":   {},
		"Dockerfile_test_issue_mz774":   {},
		"Dockerfile_test_issue_mz775":   {},
		"Dockerfile_test_issue_mz782":   {},
		"Dockerfile_test_issue_mz873":   {},
		"Dockerfile_test_issue_mz793":   {},
		"Dockerfile_test_issue_mz879":   {},
		"Dockerfile_test_issue_mz896":   {},
	}
	d.TestOCICacheDockerfiles = map[string]struct{}{
		"Dockerfile_test_cache_oci":         {},
		"Dockerfile_test_cache_install_oci": {},
		"Dockerfile_test_cache_perm_oci":    {},
		"Dockerfile_test_cache_copy_oci":    {},
	}
	d.TestWarmerDockerfiles = map[string]struct{}{
		"Dockerfile_test_issue_mz320": {},
	}
	d.TestReproducibleDockerfiles = map[string]struct{}{
		"Dockerfile_test_copy_reproducible": {},
		"Dockerfile_test_issue_mz731":       {},
		"Dockerfile_test_issue_mz851":       {},
	}
	return &d
}

func addAuthFlags(flags []string) []string {
	dockerConfig := os.Getenv("HOME") + "/.docker/config.json"
	if util.FilepathExists(dockerConfig) {
		flags = append(flags, "-v", dockerConfig+":/root/.docker/config.json:ro", "-e", "DOCKER_CONFIG=/root/.docker")
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
	buildArgs = append(buildArgs, buildArgFlag, "IMAGE_REPO="+imageRepo)

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
		return fmt.Errorf("failed to build image %s with docker command \"%s\": %w %s", dockerImage, dockerCmd.Args, err, string(out))
	}
	t.Logf("Build image for Dockerfile %s as %s. docker build output: %s \n", dockerfile, dockerImage, out)
	// mz507: push is kept as a separate step because Dockerfile_test_issue_519
	// still uses legacy builder and not buildkit
	pushCmd := exec.Command("docker", "push", dockerImage)
	out, err = RunCommandWithoutTest(pushCmd)
	if err != nil {
		return fmt.Errorf("failed to push image %s with docker command \"%s\": %w %s", dockerImage, pushCmd.Args, err, string(out))
	}
	return nil
}

// BuildImage will build dockerfile (located at dockerfilesPath) using both kaniko and docker.
// The resulting image will be tagged with imageRepo.
func (d *DockerFileBuilder) BuildImage(t *testing.T, config *integrationTestConfig, dockerfilesPath, dockerfile string) error {
	_, ex, _, _ := runtime.Caller(0)
	cwd := filepath.Dir(ex)

	return d.BuildImageWithContext(t, config, dockerfilesPath, dockerfile, cwd)
}

// BuildKanikoImage builds dockerfile using only kaniko (no docker build).
func (d *DockerFileBuilder) BuildKanikoImage(t *testing.T, config *integrationTestConfig, dockerfilesPath, dockerfile string) error {
	_, ex, _, _ := runtime.Caller(0)
	cwd := filepath.Dir(ex)

	var buildArgs []string
	for _, arg := range argsMap[dockerfile] {
		buildArgs = append(buildArgs, "--build-arg", arg)
	}
	buildArgs = append(buildArgs, "--build-arg", "IMAGE_REPO="+config.imageRepo)

	additionalKanikoFlags := additionalKanikoFlagsMap[dockerfile]
	additionalKanikoFlags = append(additionalKanikoFlags, "-c", buildContextPath)

	kanikoImage := GetKanikoImage(config.imageRepo, dockerfile)
	_, err := buildKanikoImage(t.Logf, dockerfilesPath, dockerfile, buildArgs, additionalKanikoFlags, kanikoImage,
		cwd, "", "")
	return err
}

func (d *DockerFileBuilder) BuildImageWithContext(t *testing.T, config *integrationTestConfig, dockerfilesPath, dockerfile, contextDir string) error {
	fn, _ := d.filesBuilt.LoadOrStore(dockerfile, sync.OnceValue(func() error {
		return d.buildImage(t, config, dockerfilesPath, dockerfile, contextDir)
	}))
	return fn()
}

func (d *DockerFileBuilder) buildImage(t *testing.T, config *integrationTestConfig, dockerfilesPath, dockerfile, contextDir string) error {
	imageRepo := config.imageRepo

	var buildArgs []string
	buildArgFlag := "--build-arg"
	for _, arg := range argsMap[dockerfile] {
		buildArgs = append(buildArgs, buildArgFlag, arg)
	}
	buildArgs = append(buildArgs, buildArgFlag, "IMAGE_REPO="+config.imageRepo)

	if err := d.BuildDockerImage(t, imageRepo, dockerfilesPath, dockerfile, contextDir); err != nil {
		return err
	}

	additionalKanikoFlags := additionalKanikoFlagsMap[dockerfile]
	additionalKanikoFlags = append(additionalKanikoFlags, "-c", buildContextPath)
	if _, ok := d.TestReproducibleDockerfiles[dockerfile]; ok {
		additionalKanikoFlags = append(additionalKanikoFlags, "--reproducible")
	}

	kanikoImage := GetKanikoImage(imageRepo, dockerfile)
	if _, err := buildKanikoImage(t.Logf, dockerfilesPath, dockerfile, buildArgs, additionalKanikoFlags, kanikoImage,
		contextDir, "", ""); err != nil {
		return err
	}

	return nil
}

// buildCachedImage builds the image for testing caching via kaniko where version is the nth time this image has been built
func (d *DockerFileBuilder) buildCachedImage(logf logger, config *integrationTestConfig, cacheRepo, dockerfilesPath, dockerfile string, version int, args []string) error {
	imageRepo := config.imageRepo
	_, ex, _, _ := runtime.Caller(0)
	cwd := filepath.Dir(ex)

	cacheFlag := "--cache=true"

	benchmarkEnv := "BENCHMARK_FILE=false"
	if b, err := strconv.ParseBool(os.Getenv("BENCHMARK")); err == nil && b {
		err := os.Mkdir("benchmarks", 0o755)
		if err != nil {
			return err
		}
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
	for _, envVariable := range envsMap[dockerfile] {
		dockerRunFlags = append(dockerRunFlags, "-e", envVariable)
	}
	executorImage := ExecutorImage
	if exec, ok := executorImages[dockerfile]; ok {
		executorImage = exec
	}

	rawBuildArgs := argsMap[dockerfile]
	if override, ok := argsMapVersion1[dockerfile]; version == 1 && ok {
		rawBuildArgs = override
	}
	var buildArgs []string
	for _, arg := range rawBuildArgs {
		buildArgs = append(buildArgs, "--build-arg", arg)
	}
	buildArgs = append(buildArgs, "--build-arg", "IMAGE_REPO="+config.imageRepo)

	dockerRunFlags = addAuthFlags(dockerRunFlags)
	dockerRunFlags = addCoverageFlags(dockerRunFlags)
	dockerRunFlags = append(dockerRunFlags, executorImage,
		"-f", path.Join(buildContextPath, dockerfilesPath, dockerfile),
		"-d", kanikoImage,
		"-c", buildContextPath,
		cacheFlag,
		"--cache-repo", cacheRepo,
		"--cache-dir", cacheDir)
	dockerRunFlags = append(dockerRunFlags, args...)
	dockerRunFlags = append(dockerRunFlags, buildArgs...)
	kanikoCmd := exec.Command("docker", dockerRunFlags...)

	out, err := RunCommandWithoutTest(kanikoCmd)
	logf(string(out))

	if err != nil {
		return fmt.Errorf("failed to build cached image %s with kaniko command \"%s\": %w", kanikoImage, kanikoCmd.Args, err)
	}
	if outputCheck := outputChecks[dockerfile]; outputCheck != nil {
		if err := outputCheck(dockerfile, out); err != nil {
			return fmt.Errorf("output check failed for image %s with kaniko command : %w", kanikoImage, err)
		}
	}
	if version > 0 {
		if outputCheck := cacheHitOutputChecks[dockerfile]; outputCheck != nil {
			if err := outputCheck(dockerfile, out); err != nil {
				return fmt.Errorf("cache hit check failed for image %s: %w", kanikoImage, err)
			}
		}
	}
	if outputCheck := warmerOutputChecks[dockerfile]; outputCheck != nil {
		if err := outputCheck(dockerfile, out); err != nil {
			return fmt.Errorf("output check failed for image %s with kaniko command : %w", kanikoImage, err)
		}
	}
	if err := checkNoWarnings(dockerfile, out); err != nil {
		return err
	}
	return nil
}

func (d *DockerFileBuilder) buildCachedImageInContext(logf logger, config *integrationTestConfig, cacheRepo, dockerfile, contextDir string, version int) error {
	_, ex, _, _ := runtime.Caller(0)
	cwd := filepath.Dir(ex)

	kanikoImage := GetVersionedKanikoImage(config.imageRepo, filepath.Base(contextDir), version)

	dockerRunFlags := []string{"run", "--net=host", "-v", cwd + ":/workspace"}
	for _, envVariable := range KanikoEnv {
		dockerRunFlags = append(dockerRunFlags, "-e", envVariable)
	}
	dockerRunFlags = addAuthFlags(dockerRunFlags)
	dockerRunFlags = addCoverageFlags(dockerRunFlags)
	dockerRunFlags = append(dockerRunFlags, ExecutorImage,
		"-f", path.Join(buildContextPath, dockerfile),
		"-c", path.Join(buildContextPath, contextDir),
		"-d", kanikoImage,
		"--cache=true",
		"--cache-copy-layers",
		"--cache-repo", cacheRepo,
		"--cache-dir", cacheDir)

	out, err := RunCommandWithoutTest(exec.Command("docker", dockerRunFlags...))
	logf(string(out))
	return err
}

func populateVolumeCache(logf logger) error {
	fmt.Println("Populating warmer cache")
	_, ex, _, _ := runtime.Caller(0)
	cwd := filepath.Dir(ex)
	cmd := []string{
		"run", "--net=host",
		"-v", cwd + ":/workspace",
	}
	for _, envVariable := range WarmerEnv {
		cmd = append(cmd, "-e", envVariable)
	}
	cmd = addAuthFlags(cmd)
	cmd = addCoverageFlags(cmd)
	cmd = append(cmd,
		WarmerImage,
		"-c", cacheDir,
		"-i", baseImageToCache,
	)

	warmerCmd := exec.Command("docker", cmd...)
	out, err := RunCommandWithoutTest(warmerCmd)
	logf(string(out))
	if err != nil {
		return fmt.Errorf("failed to warm kaniko cache: %w", err)
	}
	return nil
}

// buildCachedImage builds the image for testing caching via kaniko warmer cache where version is the nth time this image has been built
func (d *DockerFileBuilder) buildWarmerImage(logf logger, config *integrationTestConfig, dockerfilesPath, dockerfile string, version int, args []string, cache bool) error {
	imageRepo := config.imageRepo
	_, ex, _, _ := runtime.Caller(0)
	cwd := filepath.Dir(ex)

	kanikoImage := GetKanikoImage(imageRepo, "test_warmer_"+dockerfile) + strconv.Itoa(version)

	dockerRunFlags := []string{
		"run", "--net=host",
		"-v", cwd + ":/workspace:ro",
	}
	for _, envVariable := range KanikoEnv {
		dockerRunFlags = append(dockerRunFlags, "-e", envVariable)
	}
	executorImage := ExecutorImage
	if exec, ok := executorImages[dockerfile]; ok {
		executorImage = exec
	}
	dockerRunFlags = addAuthFlags(dockerRunFlags)
	dockerRunFlags = addCoverageFlags(dockerRunFlags)
	dockerRunFlags = append(dockerRunFlags, executorImage,
		"-f", path.Join(buildContextPath, dockerfilesPath, dockerfile),
		"-d", kanikoImage,
		"-c", buildContextPath,
		fmt.Sprintf("--cache=%t", cache),
		"--cache-dir", cacheDir,
		"--cache-run-layers=false",
		"--no-push-cache",
	)
	dockerRunFlags = append(dockerRunFlags, args...)
	kanikoCmd := exec.Command("docker", dockerRunFlags...)

	out, err := RunCommandWithoutTest(kanikoCmd)
	logf(string(out))

	if err != nil {
		return fmt.Errorf("failed to build image %s with kaniko command \"%s\": %w", kanikoImage, kanikoCmd.Args, err)
	}
	if outputCheck := outputChecks[dockerfile]; outputCheck != nil {
		if err := outputCheck(dockerfile, out); err != nil {
			return fmt.Errorf("output check failed for image %s with kaniko command : %w", kanikoImage, err)
		}
	}
	if cache {
		if outputCheck := warmerOutputChecks[dockerfile]; outputCheck != nil {
			if err := outputCheck(dockerfile, out); err != nil {
				return fmt.Errorf("output check failed for image %s with kaniko command : %w", kanikoImage, err)
			}
		}
	}
	if err := checkNoWarnings(dockerfile, out); err != nil {
		return err
	}
	return nil
}

// buildRelativePathsImage builds the images for testing passing relatives paths to Kaniko
func (d *DockerFileBuilder) buildRelativePathsImage(logf logger, imageRepo, dockerfile, buildContextPath string) error {
	_, ex, _, _ := runtime.Caller(0)
	cwd := filepath.Dir(ex)

	dockerImage := GetDockerImage(imageRepo, "test_relative_"+dockerfile)
	kanikoImage := GetKanikoImage(imageRepo, "test_relative_"+dockerfile)

	dockerArgs := []string{
		"build",
		"--push",
		"-t", dockerImage,
		"-f", dockerfile,
		"./context",
	}
	dockerArgs = append(dockerArgs, additionalDockerFlagsMap[dockerfile]...)
	dockerCmd := exec.Command("docker", dockerArgs...)

	out, err := RunCommandWithoutTest(dockerCmd)
	if err != nil {
		return fmt.Errorf("failed to build image %s with docker command \"%s\": %w %s", dockerImage, dockerCmd.Args, err, string(out))
	}

	dockerRunFlags := []string{"run", "--net=host", "-v", cwd + ":/workspace"}
	for _, envVariable := range KanikoEnv {
		dockerRunFlags = append(dockerRunFlags, "-e", envVariable)
	}
	executorImage := ExecutorImage
	if exec, ok := executorImages[dockerfile]; ok {
		executorImage = exec
	}
	dockerRunFlags = addAuthFlags(dockerRunFlags)
	dockerRunFlags = addCoverageFlags(dockerRunFlags)
	dockerRunFlags = append(dockerRunFlags, executorImage,
		"-f", dockerfile,
		"-d", kanikoImage,
		"-c", buildContextPath)

	kanikoCmd := exec.Command("docker", dockerRunFlags...)

	out, err = RunCommandWithoutTest(kanikoCmd)
	logf(string(out))

	if err != nil {
		return fmt.Errorf(
			"failed to build relative path image %s with kaniko command \"%s\": %w",
			kanikoImage, kanikoCmd.Args, err)
	}
	if outputCheck := outputChecks[dockerfile]; outputCheck != nil {
		if err := outputCheck(dockerfile, out); err != nil {
			return fmt.Errorf("output check failed for image %s with kaniko command : %w", kanikoImage, err)
		}
	}
	if err := checkNoWarnings(dockerfile, out); err != nil {
		return err
	}
	return nil
}

var extraDockerRunFlags = map[string]func(contextDir string) []string{
	"Dockerfile_test_issue_mz753": func(ctx string) []string {
		return []string{"-v", filepath.Join(ctx, "testdata/Dockerfile.trivial") + ":/opt/driver/lib.so:ro"}
	},
}

func buildKanikoImage(
	logf logger,
	dockerfilesPath string,
	dockerfile string,
	buildArgs []string,
	kanikoArgs []string,
	kanikoImage string,
	contextDir string,
	tlsCACert string,
	dockerConfig string,
) (string, error) {
	benchmarkEnv := "BENCHMARK_FILE=false"
	benchmarkDir, err := os.MkdirTemp("", "")
	if err != nil {
		return "", err
	}

	if b, err := strconv.ParseBool(os.Getenv("BENCHMARK")); err == nil && b {
		benchmarkEnv = "BENCHMARK_FILE=/benchmarks/" + dockerfile
	}

	// build kaniko image
	additionalFlags := append(buildArgs, kanikoArgs...)
	additionalFlags = append(additionalFlags,
		"--digest-file=/dev/stdout",
		"--image-name-with-digest-file=/dev/stdout",
		"--image-name-tag-with-digest-file=/dev/stdout",
	)
	logf("Going to build image with kaniko: %s, flags: %s \n", kanikoImage, additionalFlags)

	dockerRunFlags := []string{
		"run", "--net=host",
		"-e", benchmarkEnv,
		"-v", contextDir + ":/workspace:ro",
		"-v", benchmarkDir + ":" + "/benchmarks",
	}

	for _, envVariable := range KanikoEnv {
		dockerRunFlags = append(dockerRunFlags, "-e", envVariable)
	}
	if env, ok := envsMap[dockerfile]; ok {
		for _, envVariable := range env {
			dockerRunFlags = append(dockerRunFlags, "-e", envVariable)
		}
	}

	if dockerConfig != "" {
		dockerRunFlags = append(dockerRunFlags, "-v", dockerConfig+":/kaniko/.docker/config.json:ro")
	} else {
		dockerRunFlags = addAuthFlags(dockerRunFlags)
	}

	kanikoDockerfilePath := path.Join(buildContextPath, dockerfilesPath, dockerfile)
	if dockerfilesPath == "" {
		kanikoDockerfilePath = path.Join(buildContextPath, "Dockerfile")
	}

	executorImage := ExecutorImage
	if exec, ok := executorImages[dockerfile]; ok {
		executorImage = exec
	}

	if tlsCACert != "" {
		dockerRunFlags = append(dockerRunFlags,
			"-v", tlsCACert+":/kaniko/ssl/certs/test-registry-ca.crt:ro")
	}

	if fn, ok := extraDockerRunFlags[dockerfile]; ok {
		dockerRunFlags = append(dockerRunFlags, fn(contextDir)...)
	}

	dockerRunFlags = addCoverageFlags(dockerRunFlags)
	dockerRunFlags = append(dockerRunFlags, executorImage,
		"-f", kanikoDockerfilePath,
		"-d", kanikoImage,
	)
	dockerRunFlags = append(dockerRunFlags, additionalFlags...)

	kanikoCmd := exec.Command("docker", dockerRunFlags...)

	out, err := RunCommandWithoutTest(kanikoCmd)
	logf(string(out))

	if err != nil {
		return "", fmt.Errorf("failed to build image %s with kaniko command \"%s\": %w", kanikoImage, kanikoCmd.Args, err)
	}
	if outputCheck := outputChecks[dockerfile]; outputCheck != nil {
		if err := outputCheck(dockerfile, out); err != nil {
			return "", fmt.Errorf("output check failed for image %s with kaniko command : %w", kanikoImage, err)
		}
	}
	if err := checkNoWarnings(dockerfile, out); err != nil {
		return "", err
	}
	return benchmarkDir, nil
}
