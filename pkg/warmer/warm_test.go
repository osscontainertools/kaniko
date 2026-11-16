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

func TestDeduplicateImages_CliAndDockerfile(t *testing.T) {
	cliImages := []string{"alpine:latest"}
	dockerfileImages := []string{"alpine:latest", "debian:bookworm"}

	result := deduplicateImages(cliImages, dockerfileImages)

	if len(result) != 2 {
		t.Fatalf("expected 2 images, got %d: %v", len(result), result)
	}
	if result[0] != "alpine:latest" {
		t.Errorf("expected first image to be alpine:latest (CLI), got %s", result[0])
	}
	if result[1] != "debian:bookworm" {
		t.Errorf("expected second image to be debian:bookworm, got %s", result[1])
	}
}

func TestDeduplicateImages_DuplicateCliImages(t *testing.T) {
	cliImages := []string{"alpine:latest", "alpine:latest", "nginx:latest"}
	dockerfileImages := []string{"debian:bookworm", "alpine:latest"}

	result := deduplicateImages(cliImages, dockerfileImages)

	expected := []string{"alpine:latest", "nginx:latest", "debian:bookworm"}
	if len(result) != len(expected) {
		t.Fatalf("expected %d images, got %d: %v", len(expected), len(result), result)
	}
	for i, exp := range expected {
		if result[i] != exp {
			t.Errorf("index %d: expected %s, got %s", i, exp, result[i])
		}
	}
}

func TestDeduplicateImages_EmptyInputs(t *testing.T) {
	// Both empty
	result := deduplicateImages([]string{}, []string{})
	if len(result) != 0 {
		t.Errorf("expected empty result, got %v", result)
	}

	// Only CLI images
	result = deduplicateImages([]string{"alpine:latest"}, []string{})
	if len(result) != 1 || result[0] != "alpine:latest" {
		t.Errorf("expected [alpine:latest], got %v", result)
	}

	// Only Dockerfile images
	result = deduplicateImages([]string{}, []string{"debian:bookworm"})
	if len(result) != 1 || result[0] != "debian:bookworm" {
		t.Errorf("expected [debian:bookworm], got %v", result)
	}
}

func TestDeduplicateImages_PreservesOrder(t *testing.T) {
	// CLI images order should be preserved
	cliImages := []string{"img1", "img2", "img3"}
	dockerfileImages := []string{"img4", "img5"}
	result := deduplicateImages(cliImages, dockerfileImages)
	expected := []string{"img1", "img2", "img3", "img4", "img5"}
	if len(result) != len(expected) {
		t.Fatalf("expected %d images, got %d", len(expected), len(result))
	}
	for i := range expected {
		if result[i] != expected[i] {
			t.Errorf("index %d: expected %s, got %s", i, expected[i], result[i])
		}
	}

	// Dockerfile images order should be preserved (after CLI)
	cliImages = []string{"img1"}
	dockerfileImages = []string{"img2", "img3", "img2"} // duplicate in dockerfile
	result = deduplicateImages(cliImages, dockerfileImages)
	expected = []string{"img1", "img2", "img3"}
	if len(result) != len(expected) {
		t.Fatalf("expected %d images, got %d", len(expected), len(result))
	}
	for i := range expected {
		if result[i] != expected[i] {
			t.Errorf("index %d: expected %s, got %s", i, expected[i], result[i])
		}
	}
}
