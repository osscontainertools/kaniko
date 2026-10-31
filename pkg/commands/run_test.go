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
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	kConfig "github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/dockerfile"
	"github.com/osscontainertools/kaniko/pkg/util"
	"github.com/osscontainertools/kaniko/testutil"
)

func TestRunCrossStageBindMountDoesNotRetainMountedFiles(t *testing.T) {
	originalFlag := kConfig.FF.RunCrossStageBindMount
	originalDepsDir := kConfig.KanikoInterStageDepsDir
	originalSwapDir := kConfig.KanikoSwapDir
	kConfig.FF.RunCrossStageBindMount = true
	kConfig.KanikoInterStageDepsDir = t.TempDir()
	kConfig.KanikoSwapDir = t.TempDir()
	defer func() {
		kConfig.FF.RunCrossStageBindMount = originalFlag
		kConfig.KanikoInterStageDepsDir = originalDepsDir
		kConfig.KanikoSwapDir = originalSwapDir
	}()

	source := filepath.Join(kConfig.KanikoInterStageDepsDir, "0", "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "mounted"), []byte("source"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name          string
		target        func() string
		prepare       func(string) error
		mountOptions  string
		command       func(string) string
		wantError     bool
		verifyCleanup func(*testing.T, string)
	}{
		{
			name:   "restores pre-existing target",
			target: func() string { return filepath.Join(t.TempDir(), "target") },
			prepare: func(target string) error {
				if err := os.Mkdir(target, 0o755); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(target, "original"), []byte("original"), 0o644)
			},
			verifyCleanup: func(t *testing.T, target string) {
				t.Helper()
				if _, err := os.Stat(filepath.Join(target, "original")); err != nil {
					t.Errorf("original target file was not restored: %v", err)
				}
				if _, err := os.Stat(filepath.Join(target, "mounted")); !os.IsNotExist(err) {
					t.Errorf("mounted file remained at target: %v", err)
				}
			},
		},
		{
			name:   "removes newly created target",
			target: func() string { return filepath.Join(t.TempDir(), "target") },
			verifyCleanup: func(t *testing.T, target string) {
				t.Helper()
				if _, err := os.Lstat(target); !os.IsNotExist(err) {
					t.Errorf("new target remained after RUN: %v", err)
				}
			},
		},
		{
			name:      "rejects writes to cross-stage bind mount",
			target:    func() string { return filepath.Join(t.TempDir(), "target") },
			command:   func(target string) string { return "printf changed > " + target + "/mounted" },
			wantError: true,
			verifyCleanup: func(t *testing.T, target string) {
				t.Helper()
				if _, err := os.Lstat(target); !os.IsNotExist(err) {
					t.Errorf("new target remained after RUN: %v", err)
				}
			},
		},
		{
			name:         "discards writes to read-write cross-stage bind mount",
			target:       func() string { return filepath.Join(t.TempDir(), "target") },
			mountOptions: ",rw",
			command:      func(target string) string { return "printf changed > " + target + "/mounted" },
			verifyCleanup: func(t *testing.T, target string) {
				t.Helper()
				if _, err := os.Lstat(target); !os.IsNotExist(err) {
					t.Errorf("new target remained after RUN: %v", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target := test.target()
			if test.prepare != nil {
				if err := test.prepare(target); err != nil {
					t.Fatal(err)
				}
			}
			command := "test -f " + target + "/mounted"
			if test.command != nil {
				command = test.command(target)
			}
			dockerfileText := fmt.Sprintf("FROM scratch\nRUN --mount=type=bind,from=0,source=source,target=%s%s %s", target, test.mountOptions, command)
			stages, _, err := dockerfile.Parse([]byte(dockerfileText))
			if err != nil {
				t.Fatal(err)
			}
			cmdRun, ok := stages[0].Commands[0].(*instructions.RunCommand)
			if !ok {
				t.Fatalf("expected RUN command, got %T", stages[0].Commands[0])
			}
			err = runCommandWithFlags(&v1.Config{}, dockerfile.NewBuildArgs(nil), cmdRun, util.FileContext{Root: t.TempDir()}, nil)
			if test.wantError {
				if err == nil {
					t.Fatal("expected read-only bind mount write to fail")
				}
			} else if err != nil {
				t.Fatalf("running bind-mounted command: %v", err)
			}
			test.verifyCleanup(t, target)

			content, err := os.ReadFile(filepath.Join(source, "mounted"))
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != "source" {
				t.Errorf("source stage file was modified: %q", content)
			}
		})
	}
}

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
