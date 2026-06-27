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
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/osscontainertools/kaniko/cmd/executor/cmd"
	testbake "github.com/osscontainertools/kaniko/golden/testdata/test_bake"
	testissuemz195 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz195"
	testissuemz333 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz333"
	testissuemz334 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz334"
	testissuemz338 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz338"
	testissuemz480 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz480"
	testissuemz487 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz487"
	testissuemz703 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz703"
	testissuemz791 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz791"
	testissuemz813 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz813"
	testunittests "github.com/osscontainertools/kaniko/golden/testdata/test_unittests"
	"github.com/osscontainertools/kaniko/golden/types"
	"github.com/osscontainertools/kaniko/pkg/cache"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/executor"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// mirrors pkg/executor/push.go
const cachePointerLabel = "kaniko.cache.pointer-target"

type fakeLayerCache struct {
	cachedKeys []string
}

func (f *fakeLayerCache) RetrieveLayer(key string) (v1.Image, error) {
	if !slices.Contains(f.cachedKeys, key) {
		return nil, errors.New("could not find layer")
	}
	cf := &v1.ConfigFile{}
	cf.Config.Labels = map[string]string{cachePointerLabel: key}
	return mutate.ConfigFile(empty.Image, cf)
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
	"test_issue_mz195": {testissuemz195.Tests},
	"test_issue_mz333": {testissuemz333.Tests},
	"test_issue_mz334": {testissuemz334.Tests},
	"test_issue_mz338": {testissuemz338.Tests},
	"test_issue_mz487": {testissuemz487.Tests},
	"test_issue_mz480": {testissuemz480.Tests},
	"test_issue_mz703": {testissuemz703.Tests},
	"test_issue_mz791": {testissuemz791.Tests},
	"test_issue_mz813": {testissuemz813.Tests},
	"test_unittests":   testunittests.Tests,
}
var update bool

func TestMain(m *testing.M) {
	flag.BoolVar(&update, "update", false, "Whether to update expected output instead of testing it")
	flag.Parse()
	exitCode := m.Run()
	os.Exit(exitCode)
}

func renderPlan(t *testing.T, opts *config.KanikoOptions, cachedKeys []string) string {
	t.Helper()
	origNewLayerCache := executor.NewLayerCache
	executor.NewLayerCache = func(_ *config.KanikoOptions) cache.LayerCache {
		return &fakeLayerCache{cachedKeys: cachedKeys}
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
							for k, v := range test.Env {
								t.Setenv(k, v)
							}

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

							output := renderPlan(t, &opts, test.CachedKeys)
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
							for k, v := range test.Env {
								t.Setenv(k, v)
							}

							opts := config.KanikoOptions{}
							var set []string
							exec := &cobra.Command{Use: "bake"}
							cmd.AddBakeFlags(exec, &opts, &set)
							args := []string{
								filepath.Join(testDir, "bake.json"),
								"--dryrun",
								"--dockerfile=" + filepath.Join(testDir, testSuite.Dockerfile),
							}
							args = append(args, test.Args...)
							if err := exec.ParseFlags(args); err != nil {
								t.Fatal(err)
							}
							cmd.ValidateFlags(&opts)

							rest := exec.Flags().Args()
							if err := cmd.ConfigureFromBakefile(&opts, rest[0], rest[1:], set); err != nil {
								t.Fatal(err)
							}

							output := renderPlan(t, &opts, test.CachedKeys)
							comparePlan(t, filepath.Join(testDir, "plans", test.Plan), output)
						})
					}
				})
			}
		})
	}
}
