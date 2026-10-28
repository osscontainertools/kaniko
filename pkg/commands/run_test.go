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
	"os"
	"os/user"
	"path/filepath"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"strings"

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

func TestRunCommandWithFlags_TmpfsMount(t *testing.T) {
	tmpDir := t.TempDir()
	originalSwapDir := kConfig.KanikoSwapDir
	kConfig.KanikoSwapDir = filepath.Join(tmpDir, "swap")
	defer func() {
		kConfig.KanikoSwapDir = originalSwapDir
	}()

	targetPath := filepath.Join(tmpDir, "tmpfs_target")

	// Pre-create target directory with a file to test existing target swap & restoration
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}
	existingFilePath := filepath.Join(targetPath, "existing.txt")
	if err := os.WriteFile(existingFilePath, []byte("pre-existing content"), 0o644); err != nil {
		t.Fatalf("failed to write existing file: %v", err)
	}

	dockerfileSnippet := "RUN --mount=type=tmpfs,target=" + targetPath + " echo 'hello tmpfs' > " + filepath.Join(targetPath, "newfile.txt")
	res, err := parser.Parse(strings.NewReader(dockerfileSnippet))
	if err != nil {
		t.Fatalf("failed to parse Dockerfile snippet: %v", err)
	}
	inst, err := instructions.ParseInstruction(res.AST.Children[0])
	if err != nil {
		t.Fatalf("failed to parse instruction: %v", err)
	}
	cmdRun, ok := inst.(*instructions.RunCommand)
	if !ok {
		t.Fatalf("expected *instructions.RunCommand, got %T", inst)
	}

	cfg := &v1.Config{}
	buildArgs := dockerfile.NewBuildArgs([]string{})
	fileContext := util.FileContext{Root: tmpDir}

	err = runCommandWithFlags(cfg, buildArgs, cmdRun, fileContext, nil)
	if err != nil {
		t.Fatalf("runCommandWithFlags returned error: %v", err)
	}

	// Verify that the pre-existing file content is restored after RUN execution
	content, err := os.ReadFile(existingFilePath)
	if err != nil {
		t.Fatalf("failed to read restored existing file: %v", err)
	}
	if string(content) != "pre-existing content" {
		t.Errorf("expected pre-existing content %q, got %q", "pre-existing content", string(content))
	}

	// Verify that newfile.txt created inside tmpfs mount was cleaned up / discarded
	newFilePath := filepath.Join(targetPath, "newfile.txt")
	if _, err := os.Stat(newFilePath); !os.IsNotExist(err) {
		t.Errorf("expected newfile.txt to be discarded after RUN command, but it exists")
	}
}

func TestRunCommandWithFlags_TmpfsMount_NonExistentTarget(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "new_dir", "tmpfs_target")

	dockerfileSnippet := "RUN --mount=type=tmpfs,target=" + targetPath + " echo 'hello tmpfs' > " + filepath.Join(targetPath, "newfile.txt")
	res, err := parser.Parse(strings.NewReader(dockerfileSnippet))
	if err != nil {
		t.Fatalf("failed to parse Dockerfile snippet: %v", err)
	}
	inst, err := instructions.ParseInstruction(res.AST.Children[0])
	if err != nil {
		t.Fatalf("failed to parse instruction: %v", err)
	}
	cmdRun, ok := inst.(*instructions.RunCommand)
	if !ok {
		t.Fatalf("expected *instructions.RunCommand, got %T", inst)
	}

	cfg := &v1.Config{}
	buildArgs := dockerfile.NewBuildArgs([]string{})
	fileContext := util.FileContext{Root: tmpDir}

	err = runCommandWithFlags(cfg, buildArgs, cmdRun, fileContext, nil)
	if err != nil {
		t.Fatalf("runCommandWithFlags returned error: %v", err)
	}

	// Verify target path created by ensureDir was cleaned up after RUN
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Errorf("expected targetPath %s to be cleaned up, but it exists", targetPath)
	}
}
