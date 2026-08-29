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

			bakefile, err := bake.Parse(filepath.Join(ctxDir, "kaniko-bake.hcl"))
			if err != nil {
				t.Fatal(err)
			}
			targets, err := bakefile.Resolve(nil)
			if err != nil {
				t.Fatal(err)
			}

			kanikoImages := map[string]string{}
			dockerImages := map[string]string{}
			kanikoFlags := []string{"run", "--rm", "--net=host", "-v", ctxDir + ":/ctx"}
			kanikoFlags = addAuthFlags(kanikoFlags)
			kanikoFlags = addCoverageFlags(kanikoFlags)
			kanikoFlags = addKanikoEnvFlags(kanikoFlags)
			kanikoFlags = append(kanikoFlags, ExecutorImage, "bake", "/ctx/kaniko-bake.hcl", "-c", "/ctx")
			dockerFlags := []string{"buildx", "bake", "-f", "docker-bake.hcl"}
			var dockerTargets []string
			for _, target := range targets {
				imageName := name + "-" + target.ID
				kanikoImages[target.ID] = GetKanikoImage(config.imageRepo, imageName)
				dockerImages[target.ID] = GetDockerImage(config.imageRepo, imageName)
				kanikoFlags = append(kanikoFlags, "--set", target.ID+".destination="+kanikoImages[target.ID])
				dockerFlags = append(dockerFlags, "--set", target.ID+".tags="+dockerImages[target.ID])
				// kaniko emits dockerv2 here, so the oracle has to as well. This is the
				// bake spelling of dockerV2Flags, and it pushes, so --push is not needed.
				dockerFlags = append(dockerFlags,
					"--set", target.ID+".attest=type=provenance,disabled=true",
					"--set", target.ID+".output=type=registry,oci-mediatypes=false")
				dockerTargets = append(dockerTargets, target.ID)
			}
			// buildx bake builds the "default" group when no target is named, and the
			// fixtures define no such group.
			dockerFlags = append(dockerFlags, dockerTargets...)

			kanikoCmd := exec.Command("docker", kanikoFlags...)
			if out, err := RunCommandWithoutTest(kanikoCmd); err != nil {
				t.Fatalf("%v: %v\n%s", kanikoCmd.Args, err, string(out))
			}

			dockerCmd := exec.Command("docker", dockerFlags...)
			dockerCmd.Dir = ctxDir
			if out, err := RunCommandWithoutTest(dockerCmd); err != nil {
				t.Fatalf("%v: %v\n%s", dockerCmd.Args, err, string(out))
			}

			for _, target := range targets {
				t.Run(target.ID, func(t *testing.T) {
					containerDiff(t, dockerImages[target.ID], kanikoImages[target.ID], "--semantic", "--extra-ignore-file-content")
				})
			}
		})
	}
}
