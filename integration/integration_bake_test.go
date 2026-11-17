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

// Each image ships its fixture at /ctx, a mount would hide it.
var bakeExecutorImages = map[string]string{
	"test_bake_unmounted_context": ExecutorImageBakeContext,
}

var bakeSelectedTargets = map[string][]string{
	"test_context_dockerfile": {"only_context", "tools"},
}

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
			selected := bakeSelectedTargets[name]
			targets, err := bakefile.Resolve(selected)
			if err != nil {
				t.Fatal(err)
			}

			kanikoImages := map[string]string{}
			dockerImages := map[string]string{}
			kanikoFlags := []string{"run", "--rm", "--net=host"}
			executorImage, ok := bakeExecutorImages[name]
			if !ok {
				executorImage = ExecutorImage
				kanikoFlags = append(kanikoFlags, "-v", ctxDir+":/ctx")
			}
			kanikoFlags = addAuthFlags(kanikoFlags)
			kanikoFlags = addCoverageFlags(kanikoFlags)
			kanikoFlags = addKanikoEnvFlags(kanikoFlags, t.Name())
			// Match buildx's OCI registry output; Kaniko otherwise inherits the base image format.
			kanikoFlags = append(kanikoFlags, executorImage, "bake", "/ctx/kaniko-bake.hcl", "-c", "/ctx", "--image-format=oci")
			dockerFlags := []string{"buildx", "bake", "-f", "docker-bake.hcl"}
			var dockerTargets []string
			for _, target := range targets {
				imageName := name + "-" + target.ID
				kanikoImages[target.ID] = GetKanikoImage(config.imageRepo, imageName)
				dockerImages[target.ID] = GetDockerImage(config.imageRepo, imageName)
				kanikoFlags = append(kanikoFlags, "--set", target.ID+".destination="+kanikoImages[target.ID])
				dockerFlags = append(dockerFlags, "--set", target.ID+".tags="+dockerImages[target.ID])
				// type=registry pushes, so --push is not needed.
				dockerFlags = append(dockerFlags,
					"--set", target.ID+".attest=type=provenance,disabled=true",
					"--set", target.ID+".output=type=registry")
				dockerTargets = append(dockerTargets, target.ID)
			}
			// buildx bake builds the "default" group when no target is named, and the
			// fixtures define no such group.
			dockerFlags = append(dockerFlags, dockerTargets...)
			kanikoFlags = append(kanikoFlags, selected...)

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
					diffoci(t, dockerImages[target.ID], kanikoImages[target.ID], "--semantic", "--extra-ignore-file-content")
				})
			}
		})
	}
}
