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

package golden

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	ggcrtypes "github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/osscontainertools/kaniko/cmd/executor/cmd"
	warmercmd "github.com/osscontainertools/kaniko/cmd/warmer/cmd"
	testbake "github.com/osscontainertools/kaniko/golden/testdata/test_bake"
	testissuemz1128 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz1128"
	testissuemz1133 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz1133"
	testissuemz195 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz195"
	testissuemz333 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz333"
	testissuemz334 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz334"
	testissuemz338 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz338"
	testissuemz480 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz480"
	testissuemz487 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz487"
	testissuemz703 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz703"
	testissuemz791 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz791"
	testissuemz813 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz813"
	testissuemz822 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz822"
	testissuemz936 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz936"
	testissuemz989 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz989"
	testunittests "github.com/osscontainertools/kaniko/golden/testdata/test_unittests"
	testwarmer "github.com/osscontainertools/kaniko/golden/testdata/test_warmer"
	"github.com/osscontainertools/kaniko/golden/types"
	"github.com/osscontainertools/kaniko/pkg/cache"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/executor"
	kremote "github.com/osscontainertools/kaniko/pkg/image/remote"
	"github.com/osscontainertools/kaniko/pkg/mounts"
	"github.com/osscontainertools/kaniko/pkg/warmer"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// mirrors pkg/executor/push.go
const cachePointerLabel = "kaniko.cache.pointer-target"

type fakeLayerCache struct {
	opts       *config.KanikoOptions
	cachedKeys []string
	mediaType  ggcrtypes.MediaType
}

// fakeLayer stands in for a cache entry's layer. Its content is the key that found it, so its
// digest is derived rather than fixed by hand and stays stable across runs.
type fakeLayer struct {
	key       string
	mediaType ggcrtypes.MediaType
}

func (l *fakeLayer) Digest() (v1.Hash, error) { return l.hash() }
func (l *fakeLayer) DiffID() (v1.Hash, error) { return l.hash() }
func (l *fakeLayer) Size() (int64, error)     { return int64(len(l.key)), nil }

func (l *fakeLayer) MediaType() (ggcrtypes.MediaType, error) { return l.mediaType, nil }
func (l *fakeLayer) Compressed() (io.ReadCloser, error)      { return l.reader(), nil }

func (l *fakeLayer) Uncompressed() (io.ReadCloser, error) { return l.reader(), nil }

func (l *fakeLayer) hash() (v1.Hash, error) {
	return mounts.PlannedDigest(l.key), nil
}

func (l *fakeLayer) reader() io.ReadCloser {
	return io.NopCloser(strings.NewReader(l.key))
}

func (f *fakeLayerCache) RetrieveLayer(key string) (v1.Image, error) {
	if !slices.Contains(f.cachedKeys, key) {
		return nil, errors.New("could not find layer")
	}
	cf := &v1.ConfigFile{}
	cf.Config.Labels = map[string]string{cachePointerLabel: key}
	img, err := mutate.ConfigFile(empty.Image, cf)
	if err != nil {
		return nil, err
	}
	var layer v1.Layer = &fakeLayer{key: key, mediaType: f.mediaType}
	var tag name.Tag
	dest, err := cache.Destination(f.opts, key)
	remoteCache := err == nil && !strings.HasPrefix(dest, "oci:")
	if remoteCache {
		tag, err = name.NewTag(dest, name.WeakValidation)
		remoteCache = err == nil
	}
	// Layers read through remote.Image are mountable, layers read from a layout are not.
	if remoteCache {
		layer = &remote.MountableLayer{Layer: layer, Reference: tag}
	}
	img, err = mutate.AppendLayers(img, layer)
	if err != nil {
		return nil, err
	}
	// The real registry cache records where it read from, and the plan reads that back.
	if config.FF.CrossRepoMount && remoteCache {
		mounts.RecordImage(img, tag.Context())
	}
	return img, nil
}

func renderCommand(env map[string]string, args []string) string {
	var parts []string

	if len(env) > 0 {
		keys := make([]string, 0, len(env))
		for k := range env {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			parts = append(parts, k+"="+env[k])
		}
	}

	parts = append(parts, strings.Join(args, " "))

	return strings.Join(parts, " ")
}

var allTests = map[string][]types.GoldenTests{
	"test_issue_mz195":  {testissuemz195.Tests},
	"test_issue_mz333":  {testissuemz333.Tests},
	"test_issue_mz334":  {testissuemz334.Tests},
	"test_issue_mz338":  {testissuemz338.Tests},
	"test_issue_mz487":  {testissuemz487.Tests},
	"test_issue_mz480":  {testissuemz480.Tests},
	"test_issue_mz703":  {testissuemz703.Tests},
	"test_issue_mz791":  {testissuemz791.Tests},
	"test_issue_mz813":  {testissuemz813.Tests},
	"test_issue_mz822":  {testissuemz822.Tests},
	"test_issue_mz936":  {testissuemz936.Tests},
	"test_issue_mz989":  {testissuemz989.Tests},
	"test_issue_mz1128": {testissuemz1128.Tests},
	"test_issue_mz1133": {testissuemz1133.Tests},
	"test_unittests":    testunittests.Tests,
}
var update bool

// sync with the Preview profile in README.md
var previewEnv = map[string]string{
	"FF_KANIKO_CACHE_IMAGE_INDEX": "1",
	"FF_KANIKO_COPY_LINK":         "1",
	"FF_KANIKO_DEFER_CACHE_PUSH":  "1",
	"FF_KANIKO_WARMER_BUILD_ARGS": "1",
}

func TestMain(m *testing.M) {
	flag.BoolVar(&update, "update", false, "Whether to update expected output instead of testing it")
	flag.Parse()
	exitCode := m.Run()
	os.Exit(exitCode)
}

func renderPlan(t *testing.T, opts *config.KanikoOptions, test types.GoldenTest) string {
	t.Helper()
	cacheMediaType := ggcrtypes.MediaType(test.CacheMediaType)
	if cacheMediaType == "" {
		cacheMediaType = ggcrtypes.DockerLayer
	}
	origNewLayerCache := executor.NewLayerCache
	executor.NewLayerCache = func(opts *config.KanikoOptions) cache.LayerCache {
		return &fakeLayerCache{opts: opts, cachedKeys: test.CachedKeys, mediaType: cacheMediaType}
	}
	t.Cleanup(func() { executor.NewLayerCache = origNewLayerCache })

	var buf bytes.Buffer
	executor.Out = &buf
	if _, err := executor.DoBuild(opts); err != nil {
		t.Error(err)
	}
	return buf.String()
}

// comparePlan diffs output against the golden plan at planPath, or rewrites it
// when -update is set.
func comparePlan(t *testing.T, planPath, output string) {
	t.Helper()
	if update {
		if err := os.WriteFile(planPath, []byte(output), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	expectedPlan, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	expected := strings.Trim(string(expectedPlan), "\n")
	if diff := cmp.Diff(expected, strings.Trim(output, "\n")); diff != "" {
		t.Errorf("plan mismatch (-expected +got):\n%s", diff)
	}
}

func TestRun(t *testing.T) {
	logrus.SetLevel(logrus.WarnLevel)

	for testName, testSuites := range allTests {
		t.Run(testName, func(t *testing.T) {
			testDir := filepath.Join("testdata", testName)
			for _, testSuite := range testSuites {
				t.Run(testSuite.Name, func(t *testing.T) {
					dockerfilePath := filepath.Join(testDir, testSuite.Dockerfile)
					for _, test := range testSuite.Tests {
						t.Run(renderCommand(test.Env, test.Args), func(t *testing.T) {
							t.Cleanup(config.InitFeatureFlags)
							for k, v := range previewEnv {
								t.Setenv(k, v)
							}
							for k, v := range test.Env {
								t.Setenv(k, v)
							}
							// Feature flags resolve once at startup; re-resolve so
							// this subtest's env takes effect, and restore afterwards.
							config.InitFeatureFlags()
							mounts.Reset()
							kremote.ResetManifestCache()

							opts := config.KanikoOptions{}
							exec := &cobra.Command{
								Use: "kaniko",
							}
							cmd.AddKanikoOptionsFlags(exec, &opts)

							args := []string{
								"--dryrun",
								"--dockerfile=" + dockerfilePath,
							}
							err := exec.ParseFlags(append(args, test.Args...))
							if err != nil {
								t.Fatal(err)
							}
							cmd.ValidateFlags(&opts)

							output := renderPlan(t, &opts, test)
							comparePlan(t, filepath.Join(testDir, "plans", test.Plan), output)
						})
					}
				})
			}
		})
	}
}

var bakeTests = map[string][]types.GoldenTests{
	"test_bake": {testbake.Tests},
}

func TestBake(t *testing.T) {
	logrus.SetLevel(logrus.WarnLevel)

	for testName, testSuites := range bakeTests {
		t.Run(testName, func(t *testing.T) {
			testDir := filepath.Join("testdata", testName)
			for _, testSuite := range testSuites {
				t.Run(testSuite.Name, func(t *testing.T) {
					for _, test := range testSuite.Tests {
						t.Run(renderCommand(test.Env, test.Args), func(t *testing.T) {
							t.Cleanup(config.InitFeatureFlags)
							for k, v := range previewEnv {
								t.Setenv(k, v)
							}
							for k, v := range test.Env {
								t.Setenv(k, v)
							}
							// Feature flags resolve once at startup; re-resolve so
							// this subtest's env takes effect, and restore afterwards.
							config.InitFeatureFlags()
							mounts.Reset()
							kremote.ResetManifestCache()

							opts := config.KanikoOptions{}
							var set []string
							exec := &cobra.Command{Use: "bake"}
							cmd.AddBakeFlags(exec, &opts, &set)
							args := []string{
								filepath.Join(testDir, "kaniko-bake.hcl"),
								"--dryrun",
								"--dockerfile=" + filepath.Join(testDir, testSuite.Dockerfile),
							}
							args = append(args, test.Args...)
							if err := exec.ParseFlags(args); err != nil {
								t.Fatal(err)
							}
							cmd.ValidateFlags(&opts)

							rest := exec.Flags().Args()
							targets, err := cmd.ConfigureFromBakefile(&opts, rest[0], rest[1:], set)
							if err != nil {
								t.Fatal(err)
							}

							var output string
							for _, target := range targets {
								cmd.ApplyTarget(&opts, target)
								output += renderPlan(t, &opts, test)
							}
							comparePlan(t, filepath.Join(testDir, "plans", test.Plan), output)
						})
					}
				})
			}
		})
	}
}

var warmerTests = map[string][]types.GoldenTests{
	"test_warmer": {testwarmer.Tests},
}

// fakeRemoteImage derives the image and its index from its reference, so their digests are stable across runs.
func fakeRemoteImage(image string, _ config.RegistryOptions, _ string) (v1.Image, []byte, error) {
	cf := &v1.ConfigFile{}
	cf.Config.Labels = map[string]string{"ref": image}
	img, err := mutate.ConfigFile(empty.Image, cf)
	if err != nil {
		return nil, nil, err
	}
	index, err := mutate.Annotations(empty.Index, map[string]string{"ref": image}).(v1.ImageIndex).RawManifest()
	return img, index, err
}

func TestWarmer(t *testing.T) {
	logrus.SetLevel(logrus.ErrorLevel)

	for testName, testSuites := range warmerTests {
		t.Run(testName, func(t *testing.T) {
			testDir := filepath.Join("testdata", testName)
			for _, testSuite := range testSuites {
				t.Run(testSuite.Name, func(t *testing.T) {
					for _, test := range testSuite.Tests {
						t.Run(renderCommand(test.Env, test.Args), func(t *testing.T) {
							t.Cleanup(config.InitFeatureFlags)
							for k, v := range previewEnv {
								t.Setenv(k, v)
							}
							for k, v := range test.Env {
								t.Setenv(k, v)
							}
							// Feature flags resolve once at startup; re-resolve so
							// this subtest's env takes effect, and restore afterwards.
							config.InitFeatureFlags()

							origRemote, origLocal := warmer.RemoteSource, warmer.LocalSource
							warmer.RemoteSource = fakeRemoteImage
							warmer.LocalSource = func(_ *config.CacheOptions, key, _ string) (v1.Image, error) {
								if !slices.Contains(test.CachedKeys, key) {
									return nil, cache.NotFoundErr{}
								}
								return empty.Image, nil
							}
							t.Cleanup(func() { warmer.RemoteSource, warmer.LocalSource = origRemote, origLocal })

							opts := config.WarmerOptions{}
							exec := &cobra.Command{Use: "warmer"}
							warmercmd.AddWarmerFlags(exec, &opts)
							args := []string{
								"--dryrun",
								"--dockerfile=" + filepath.Join(testDir, testSuite.Dockerfile),
							}
							err := exec.ParseFlags(append(args, test.Args...))
							if err != nil {
								t.Fatal(err)
							}

							var buf bytes.Buffer
							warmer.Out = &buf
							if err := warmer.WarmCache(&opts); err != nil {
								t.Error(err)
							}
							comparePlan(t, filepath.Join(testDir, "plans", test.Plan), buf.String())
						})
					}
				})
			}
		})
	}
}
