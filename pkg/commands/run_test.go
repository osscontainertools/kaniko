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

package commands

import (
	"bytes"
	"os"
	"os/user"
	"path/filepath"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
	kConfig "github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/dockerfile"
	"github.com/osscontainertools/kaniko/pkg/util"
	"github.com/osscontainertools/kaniko/testutil"
)

func Test_addDefaultHOME(t *testing.T) {
	tests := []struct {
		name        string
		user        string
		mockUser    *user.User
		lookupError error
		initial     []string
		expected    []string
	}{
		{
			name: "HOME already set",
			user: "",
			initial: []string{
				"HOME=/something",
				"PATH=/something/else",
			},
			expected: []string{
				"HOME=/something",
				"PATH=/something/else",
			},
		},
		{
			name: "HOME not set and user not set",
			user: "",
			initial: []string{
				"PATH=/something/else",
			},
			expected: []string{
				"PATH=/something/else",
				"HOME=/root",
			},
		},
		{
			name: "HOME not set and user and homedir for the user set",
			user: "www-add",
			mockUser: &user.User{
				Username: "www-add",
				HomeDir:  "/home/some-other",
			},
			initial: []string{
				"PATH=/something/else",
			},
			expected: []string{
				"PATH=/something/else",
				"HOME=/home/some-other",
			},
		},
		{
			name: "USER is set using the UID",
			user: "1000",
			mockUser: &user.User{
				Username: "1000",
				HomeDir:  "/",
			},
			initial: []string{
				"PATH=/something/else",
			},
			expected: []string{
				"PATH=/something/else",
				"HOME=/",
			},
		},
		{
			name: "HOME not set and user is set to root",
			user: "root",
			mockUser: &user.User{
				Username: "root",
			},
			initial: []string{
				"PATH=/something/else",
			},
			expected: []string{
				"PATH=/something/else",
				"HOME=/root",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original := userLookup
			userLookup = func(_ string) (*user.User, error) { return test.mockUser, test.lookupError }
			defer func() {
				userLookup = original
			}()
			actual, err := addDefaultHOME(test.user, test.initial)
			testutil.CheckErrorAndDeepEqual(t, false, err, test.expected, actual)
		})
	}
}

func TestSetWorkDirIfExists(t *testing.T) {
	testDir := t.TempDir()
	testutil.CheckDeepEqual(t, testDir, setWorkDirIfExists(testDir))
	testutil.CheckDeepEqual(t, "", setWorkDirIfExists("doesnot-exists"))
}

func TestRunCommandBindMountRelativeTarget(t *testing.T) {
	parseRun := func(t *testing.T, line string) *instructions.RunCommand {
		t.Helper()
		p, err := parser.Parse(bytes.NewReader([]byte(line)))
		if err != nil {
			t.Fatalf("parsing dockerfile line: %v", err)
		}
		res, err := instructions.ParseInstruction(p.AST.Children[0])
		if err != nil {
			t.Fatalf("parsing instruction: %v", err)
		}
		cmd, ok := res.(*instructions.RunCommand)
		if !ok {
			t.Fatalf("expected *instructions.RunCommand, got %T", res)
		}
		return cmd
	}

	testCases := []struct {
		name          string
		flagEnabled   bool
		workdir       string
		target        string
		expectedCheck string // what the shell command should test
	}{
		{
			name:          "flag enabled with relative target in workdir",
			flagEnabled:   true,
			workdir:       "/tmp/test-workdir",
			target:        "mounted-req.txt",
			expectedCheck: "test -f /tmp/test-workdir/mounted-req.txt",
		},
		{
			name:          "flag enabled with absolute target",
			flagEnabled:   true,
			workdir:       "/tmp/test-workdir",
			target:        "/tmp/abs-mounted.txt",
			expectedCheck: "test -f /tmp/abs-mounted.txt && test ! -f /tmp/test-workdir/tmp/abs-mounted.txt",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			oldFF := kConfig.FF.RunMountBindRelative
			kConfig.FF.RunMountBindRelative = tc.flagEnabled
			defer func() {
				kConfig.FF.RunMountBindRelative = oldFF
			}()

			contextDir := t.TempDir()
			srcFile := filepath.Join(contextDir, "source.txt")
			if err := os.WriteFile(srcFile, []byte("hello"), 0o644); err != nil {
				t.Fatalf("writing source file: %v", err)
			}

			// Ensure workdir exists
			if err := os.MkdirAll(tc.workdir, 0o755); err != nil {
				t.Fatalf("creating workdir: %v", err)
			}
			defer os.RemoveAll(tc.workdir)

			dockerfileLine := "RUN --mount=type=bind,source=source.txt,target=" + tc.target + " " + tc.expectedCheck
			cmdRun := parseRun(t, dockerfileLine)

			cfg := &v1.Config{
				WorkingDir: tc.workdir,
			}
			buildArgs := dockerfile.NewBuildArgs([]string{})
			fileContext := util.FileContext{Root: contextDir}

			err := runCommandWithFlags(cfg, buildArgs, cmdRun, fileContext, nil)
			if err != nil {
				t.Fatalf("expected runCommandWithFlags to succeed, got: %v", err)
			}
		})
	}

	t.Run("flag disabled does not resolve relative target to workdir", func(t *testing.T) {
		oldFF := kConfig.FF.RunMountBindRelative
		kConfig.FF.RunMountBindRelative = false
		defer func() {
			kConfig.FF.RunMountBindRelative = oldFF
		}()

		contextDir := t.TempDir()
		srcFile := filepath.Join(contextDir, "source.txt")
		if err := os.WriteFile(srcFile, []byte("hello"), 0o644); err != nil {
			t.Fatalf("writing source file: %v", err)
		}

		workdir := filepath.Join(t.TempDir(), "workdir")
		if err := os.MkdirAll(workdir, 0o755); err != nil {
			t.Fatalf("creating workdir: %v", err)
		}

		// When flag is disabled, target="rel.txt" will be placed in rootfs cwd (where kaniko runs, repo root)
		// and NOT in workdir/rel.txt.
		dockerfileLine := "RUN --mount=type=bind,source=source.txt,target=rel.txt test ! -f " + filepath.Join(workdir, "rel.txt")
		cmdRun := parseRun(t, dockerfileLine)

		cfg := &v1.Config{
			WorkingDir: workdir,
		}
		buildArgs := dockerfile.NewBuildArgs([]string{})
		fileContext := util.FileContext{Root: contextDir}

		err := runCommandWithFlags(cfg, buildArgs, cmdRun, fileContext, nil)
		if err != nil {
			t.Fatalf("expected runCommandWithFlags to succeed, got: %v", err)
		}
	})
}
