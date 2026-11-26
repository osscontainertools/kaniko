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

func TestParseDockerfile_RunMountBindFrom(t *testing.T) {
	dockerfile := `FROM alpine:latest AS builder
RUN --mount=type=bind,from=alpine:latest,target=/alpine true
RUN --mount=type=bind,from=busybox:1.36,target=/busybox true
RUN --mount=type=bind,from=busybox:1.36,target=/busybox-copy true
RUN --mount=type=bind,from=builder,target=/builder true
RUN --mount=type=bind,from=0,target=/stage true
`
	dockerfilePath := t.TempDir() + "/Dockerfile"
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(config.InitFeatureFlags)
	t.Setenv("FF_KANIKO_CACHE_COPY_FROM", "0")
	t.Setenv("FF_KANIKO_CACHE_RUN_MOUNT_BIND_FROM", "0")
	config.InitFeatureFlags()

	opts := &config.WarmerOptions{DockerfilePath: dockerfilePath}
	baseNames, err := ParseDockerfile(opts)
	if err != nil {
		t.Fatalf("expected no error with flag disabled, got %v", err)
	}
	if len(baseNames) != 1 || baseNames[0] != "alpine:latest" {
		t.Fatalf("expected only alpine:latest with flag disabled, got %v", baseNames)
	}

	t.Setenv("FF_KANIKO_CACHE_RUN_MOUNT_BIND_FROM", "1")
	config.InitFeatureFlags()

	baseNames, err = ParseDockerfile(opts)
	if err != nil {
		t.Fatalf("expected no error with flag enabled, got %v", err)
	}
	if len(baseNames) != 2 || baseNames[0] != "alpine:latest" || baseNames[1] != "busybox:1.36" {
		t.Fatalf("expected deduplicated alpine:latest and busybox:1.36, got %v", baseNames)
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
