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
// bakefiles/ it builds the bakefile's target with kaniko and the equivalent
// docker bake HCL with buildx, then checks the two images match. The push
// destinations are injected with --set, so the fixtures stay registry-agnostic.
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
			target := targets[0]

			kanikoImage := GetKanikoImage(config.imageRepo, name)
			dockerImage := GetDockerImage(config.imageRepo, name)

			kanikoFlags := []string{"run", "--rm", "--net=host", "-v", ctxDir + ":/ctx"}
			kanikoFlags = addServiceAccountFlags(kanikoFlags, config.serviceAccount)
			kanikoFlags = addCoverageFlags(kanikoFlags)
			kanikoFlags = append(kanikoFlags, ExecutorImage,
				"bake", "/ctx/bake.json", "-c", "/ctx",
				"--set", target.ID+".destination="+kanikoImage)
			kanikoCmd := exec.Command("docker", kanikoFlags...)
			if out, err := RunCommandWithoutTest(kanikoCmd); err != nil {
				t.Fatalf("%v: %v\n%s", kanikoCmd.Args, err, string(out))
			}

			dockerCmd := exec.Command("docker", "buildx", "bake",
				"-f", "docker-bake.hcl",
				"--set", target.ID+".tags="+dockerImage,
				"--push")
			dockerCmd.Dir = ctxDir
			if out, err := RunCommandWithoutTest(dockerCmd); err != nil {
				t.Fatalf("%v: %v\n%s", dockerCmd.Args, err, string(out))
			}

			containerDiff(t, dockerImage, kanikoImage, "--ignore-history")
		})
	}
}
