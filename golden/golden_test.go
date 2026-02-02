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

package plan

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/osscontainertools/kaniko/cmd/executor/cmd"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/executor"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v4"
)

type GoldenTest struct {
	Args []string          `yaml:"args"`
	Env  map[string]string `yaml:"env,omitempty"`
	Plan string            `yaml:"plan"`
}

type GoldenTests struct {
	Dockerfile string       `yaml:"dockerfile"`
	Tests      []GoldenTest `yaml:"tests"`
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

var dockerfilesPattern string

func TestMain(m *testing.M) {
	// adds the possibility to run a single dockerfile.
	flag.StringVar(&dockerfilesPattern, "dockerfiles-pattern", "Dockerfile_test*", "The pattern to match dockerfiles with")
	flag.Parse()
	exitCode := m.Run()
	os.Exit(exitCode)
}

func TestRun(t *testing.T) {
	logrus.SetLevel(logrus.WarnLevel)

	pattern := fmt.Sprintf("dockerfiles/%s.yaml", dockerfilesPattern)
	allDockerfiles, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	for _, dockerfile := range allDockerfiles {
		t.Run(filepath.Base(dockerfile), func(t *testing.T) {
			data, err := os.ReadFile(dockerfile)
			if err != nil {
				t.Fatal(err)
			}
			dec := yaml.NewDecoder(bytes.NewReader(data))
			var allDocs []GoldenTests
			for {
				var doc GoldenTests
				err := dec.Decode(&doc)
				if err != nil {
					if errors.Is(err, io.EOF) {
						break
					}
					t.Fatal(err)
				}
				allDocs = append(allDocs, doc)
			}
			for idx, doc := range allDocs {
				t.Run(strconv.Itoa(idx), func(t *testing.T) {
					tmpDir := t.TempDir()
					dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
					err = os.WriteFile(dockerfilePath, []byte(doc.Dockerfile), 0644)
					if err != nil {
						t.Fatal(err)
					}

					for _, test := range doc.Tests {
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
							err = exec.ParseFlags(append(args, test.Args...))
							if err != nil {
								t.Error(err)
							}
							cmd.ValidateFlags(&opts)

							var buf bytes.Buffer
							oldStdout := os.Stdout
							r, w, _ := os.Pipe()
							os.Stdout = w
							_, err = executor.DoBuild(&opts)
							if err != nil {
								t.Error(err)
							}
							w.Close()
							os.Stdout = oldStdout
							_, _ = io.Copy(&buf, r)
							output := strings.Trim(buf.String(), "\n")
							plan := strings.Trim(test.Plan, "\n")
							if diff := cmp.Diff(output, plan); diff != "" {
								t.Errorf("plan mismatch (-expected +got):\n%s", diff)
							}
						})
					}
				})
			}
		})
	}
}
