/*
Copyright 2019 Google LLC

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

package warmer

import (
	"os"
	"testing"

	"github.com/osscontainertools/kaniko/pkg/config"
)

func TestParseDockerfile_SingleStageDockerfile(t *testing.T) {
	dockerfile := `FROM alpine:latest
LABEL maintainer="alexezio"
`
	tmpfile, err := os.CreateTemp("", "example")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.WriteString(dockerfile); err != nil {
		t.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	opts := &config.WarmerOptions{DockerfilePath: tmpfile.Name()}
	baseNames, err := ParseDockerfile(opts)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(baseNames) != 1 {
		t.Fatalf("expected 1 base name, got %d", len(baseNames))
	}
	if baseNames[0] != "alpine:latest" {
		t.Fatalf("expected 'alpine:latest', got '%s'", baseNames[0])
	}
}

func TestParseDockerfile_MultiStageDockerfile(t *testing.T) {
	dockerfile := `FROM golang:1.20 as BUILDER
LABEL maintainer="alexezio"

FROM alpine:latest as RUNNER
LABEL maintainer="alexezio"
`
	tmpfile, err := os.CreateTemp("", "example")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(dockerfile)); err != nil {
		t.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	opts := &config.WarmerOptions{DockerfilePath: tmpfile.Name()}
	baseNames, err := ParseDockerfile(opts)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(baseNames) != 2 {
		t.Fatalf("expected 2 base name, got %d", len(baseNames))
	}
	if baseNames[0] != "golang:1.20" {
		t.Fatalf("expected 'golang:1.20', got '%s'", baseNames[0])
	}

	if baseNames[1] != "alpine:latest" {
		t.Fatalf("expected 'alpine:latest', got '%s'", baseNames[0])
	}
}

func TestParseDockerfile_ArgsDockerfile(t *testing.T) {
	dockerfile := `ARG NGINX_VERSION=1.29.1
FROM nginx:$NGINX_VERSION-alpine-slim
`
	tmpfile, err := os.CreateTemp("", "example")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(dockerfile)); err != nil {
		t.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	opts := &config.WarmerOptions{DockerfilePath: tmpfile.Name(), BuildArgs: []string{"version=1.20"}}
	baseNames, err := ParseDockerfile(opts)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(baseNames) != 1 {
		t.Fatalf("expected 1 base name, got %d", len(baseNames))
	}
	if baseNames[0] != "nginx:1.29.1-alpine-slim" {
		t.Fatalf("expected 'nginx:1.29.1-alpine-slim', got '%s'", baseNames[0])
	}
}

func TestParseDockerfile_MissingsDockerfile(t *testing.T) {
	opts := &config.WarmerOptions{DockerfilePath: "dummy-nowhere"}
	baseNames, err := ParseDockerfile(opts)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if len(baseNames) != 0 {
		t.Fatalf("expected no base names, got %d", len(baseNames))
	}
}

func TestParseDockerfile_InvalidsDockerfile(t *testing.T) {
	dockerfile := "This is a invalid dockerfile"
	tmpfile, err := os.CreateTemp("", "example")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(dockerfile)); err != nil {
		t.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}
	opts := &config.WarmerOptions{DockerfilePath: tmpfile.Name()}
	baseNames, err := ParseDockerfile(opts)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	if len(baseNames) != 0 {
		t.Fatalf("expected no base names, got %d", len(baseNames))
	}
}

func TestParseDockerfile_MultiStageDockerfile_WithTarget(t *testing.T) {
	dockerfile := `FROM golang:1.20 as BUILDER
LABEL maintainer="alexezio"

FROM alpine:latest as RUNNER
COPY --from=BUILDER /app /app
`
	tmpfile, err := os.CreateTemp("", "example")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(dockerfile)); err != nil {
		t.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	// Without target, both base images should be included
	opts := &config.WarmerOptions{DockerfilePath: tmpfile.Name()}
	baseNames, err := ParseDockerfile(opts)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(baseNames) != 2 {
		t.Fatalf("expected 2 base names, got %d", len(baseNames))
	}

	// With target=RUNNER, only RUNNER and its dependency BUILDER should be warmed
	opts2 := &config.WarmerOptions{DockerfilePath: tmpfile.Name(), Target: []string{"RUNNER"}}
	baseNames2, err := ParseDockerfile(opts2)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(baseNames2) != 2 {
		t.Fatalf("expected 2 base names for RUNNER target (RUNNER + BUILDER), got %d", len(baseNames2))
	}

	// With target=BUILDER, only BUILDER should be warmed
	opts3 := &config.WarmerOptions{DockerfilePath: tmpfile.Name(), Target: []string{"BUILDER"}}
	baseNames3, err := ParseDockerfile(opts3)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(baseNames3) != 1 {
		t.Fatalf("expected 1 base name for BUILDER target, got %d", len(baseNames3))
	}
	if baseNames3[0] != "golang:1.20" {
		t.Fatalf("expected 'golang:1.20', got '%s'", baseNames3[0])
	}
}

func TestParseDockerfile_MultiStageWithOutOfOrderDependencies(t *testing.T) {
	// Test an out-of-order multi-stage Dockerfile where later stages reference earlier ones
	dockerfile := `FROM alpine:3.18 as BUILDER
LABEL maintainer="test"

FROM golang:1.20 as STAGE2
COPY --from=BUILDER /app /app

FROM busybox:latest as RUNNER
COPY --from=BUILDER /lib /lib
COPY --from=STAGE2 /bin /bin
`
	tmpfile, err := os.CreateTemp("", "example")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(dockerfile)); err != nil {
		t.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	// Without target, all three base images should be included
	opts := &config.WarmerOptions{DockerfilePath: tmpfile.Name()}
	baseNames, err := ParseDockerfile(opts)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(baseNames) != 3 {
		t.Fatalf("expected 3 base names, got %d", len(baseNames))
	}

	// With target=RUNNER, we should get all three (RUNNER, STAGE2, BUILDER)
	opts2 := &config.WarmerOptions{DockerfilePath: tmpfile.Name(), Target: []string{"RUNNER"}}
	baseNames2, err := ParseDockerfile(opts2)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(baseNames2) != 3 {
		t.Fatalf("expected 3 base names for RUNNER target, got %d", len(baseNames2))
	}

	// With target=STAGE2, we should get STAGE2 and BUILDER (but not RUNNER or busybox)
	opts3 := &config.WarmerOptions{DockerfilePath: tmpfile.Name(), Target: []string{"STAGE2"}}
	baseNames3, err := ParseDockerfile(opts3)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(baseNames3) != 2 {
		t.Fatalf("expected 2 base names for STAGE2 target, got %d", len(baseNames3))
	}
	// Should not include busybox
	for _, name := range baseNames3 {
		if name == "busybox:latest" {
			t.Fatalf("STAGE2 target should not include busybox, but got %s", name)
		}
	}
}

func TestParseDockerfile_CrossStageMountDependencies(t *testing.T) {
	// Test cross-stage bind mount dependencies (when feature flag is enabled)
	dockerfile := `FROM golang:1.20 as builder
WORKDIR /build

FROM alpine:3.18 as runtime
RUN --mount=type=bind,from=builder,source=/build,target=/build cp /build/app /app
`
	tmpfile, err := os.CreateTemp("", "example")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(dockerfile)); err != nil {
		t.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	// Enable the cross-stage bind mount feature flag
	oldFF := config.FF.RunCrossStageBindMount
	config.FF.RunCrossStageBindMount = true
	defer func() { config.FF.RunCrossStageBindMount = oldFF }()

	// Without target, both base images should be included
	opts := &config.WarmerOptions{DockerfilePath: tmpfile.Name()}
	baseNames, err := ParseDockerfile(opts)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(baseNames) != 2 {
		t.Fatalf("expected 2 base names (golang, alpine), got %d", len(baseNames))
	}

	// With target=runtime, should get both runtime (alpine) and its mount dependency (builder=golang)
	opts2 := &config.WarmerOptions{DockerfilePath: tmpfile.Name(), Target: []string{"runtime"}}
	baseNames2, err := ParseDockerfile(opts2)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(baseNames2) != 2 {
		t.Fatalf("expected 2 base names for runtime target (alpine + golang from mount), got %d", len(baseNames2))
	}

	// With target=builder, only golang should be warmed
	opts3 := &config.WarmerOptions{DockerfilePath: tmpfile.Name(), Target: []string{"builder"}}
	baseNames3, err := ParseDockerfile(opts3)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(baseNames3) != 1 {
		t.Fatalf("expected 1 base name for builder target, got %d", len(baseNames3))
	}
	if baseNames3[0] != "golang:1.20" {
		t.Fatalf("expected 'golang:1.20', got '%s'", baseNames3[0])
	}
}
