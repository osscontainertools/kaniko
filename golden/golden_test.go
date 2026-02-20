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
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/osscontainertools/kaniko/cmd/executor/cmd"
	testissuemz195 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz195"
	testissuemz333 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz333"
	testissuemz338 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz338"
	testissuemz487 "github.com/osscontainertools/kaniko/golden/testdata/test_issue_mz487"
	testunittests "github.com/osscontainertools/kaniko/golden/testdata/test_unittests"
	"github.com/osscontainertools/kaniko/golden/types"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/executor"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

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
	"test_issue_mz338": {testissuemz338.Tests},
	"test_issue_mz487": {testissuemz487.Tests},
	"test_unittests":   testunittests.Tests,
}
var update bool

func TestMain(m *testing.M) {
	flag.BoolVar(&update, "update", false, "Whether to update expected output instead of testing it")
	flag.Parse()
	exitCode := m.Run()
	os.Exit(exitCode)
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

							var buf bytes.Buffer
							executor.Out = &buf
							_, err = executor.DoBuild(&opts)
							if err != nil {
								t.Error(err)
							}

							planPath := filepath.Join(testDir, "plans", test.Plan)
							if update {
								err = os.WriteFile(planPath, buf.Bytes(), 0644)
								if err != nil {
									t.Fatal(err)
								}
							} else {
								output := strings.Trim(buf.String(), "\n")
								expectedPlan, err := os.ReadFile(planPath)
								if err != nil {
									t.Fatal(err)
								}
								expected := strings.Trim(string(expectedPlan), "\n")

								if diff := cmp.Diff(expected, output); diff != "" {
									t.Errorf("plan mismatch (-expected +got):\n%s", diff)
								}
							}
						})
					}
				})
			}
		})
	}
}
