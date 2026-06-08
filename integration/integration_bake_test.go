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

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/osscontainertools/kaniko/pkg/bake"
)

// TestBake is a smoke test for the bake subcommand. For each folder under
// bakefiles/ it builds the bakefile's target and checks the result matches the
// same stage built directly with --target. The build goes via an artifact and
// a separate push because the test registry cannot be baked into the fixture.
func TestBake(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(cwd, "bakefiles")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		ctxDir := filepath.Join(dir, name)

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			bakefile, err := bake.Parse(filepath.Join(ctxDir, "bake.json"))
			if err != nil {
				t.Fatal(err)
			}
			targets, err := bakefile.Resolve(nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(targets) != 1 {
				t.Fatalf("want a single target, got %d", len(targets))
			}

			outDir := t.TempDir()
			directImage := GetKanikoImage(config.imageRepo, name+"_direct")
			bakeImage := GetKanikoImage(config.imageRepo, name)

			runExecutor := func(args ...string) {
				t.Helper()
				flags := []string{"run", "--rm", "--net=host", "-v", ctxDir + ":/ctx", "-v", outDir + ":/out"}
				flags = addServiceAccountFlags(flags, config.serviceAccount)
				flags = addCoverageFlags(flags)
				flags = append(flags, ExecutorImage)
				flags = append(flags, args...)
				cmd := exec.Command("docker", flags...)
				if out, err := RunCommandWithoutTest(cmd); err != nil {
					t.Fatalf("%v: %v\n%s", cmd.Args, err, string(out))
				}
			}

			runExecutor("bake", "/ctx/bake.json", "-c", "/ctx", "--no-push", "--tar-path", "/out/image")
			runExecutor("push", "/out/image", "-d", bakeImage)
			runExecutor("-c", "/ctx", "--target", targets[0].Stage, "-d", directImage)

			containerDiff(t, directImage, bakeImage)
		})
	}
}
