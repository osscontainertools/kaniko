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

package cache

import (
	"os"
	"testing"

	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/image/remote"
)

const (
	image = "foo:latest"
)

func Test_Warmer_Warm_not_in_cache(t *testing.T) {
	tmp, err := os.MkdirTemp("", "")
	t.Error(err)
	defer os.RemoveAll(tmp)

	cw := &Warmer{
		Remote: remote.RetrieveRemoteImage,
		Local:  LocalSource,
		TmpDir: tmp,
	}

	opts := &config.WarmerOptions{}

	_, err = cw.Warm(image, opts)
	if err != nil {
		t.Errorf("expected error to be nil but was %v", err)
		t.FailNow()
	}
}

func Test_Warmer_Warm_in_cache_not_expired(t *testing.T) {
	tmp, err := os.MkdirTemp("", "")
	t.Error(err)
	defer os.RemoveAll(tmp)

	cw := &Warmer{
		Remote: remote.RetrieveRemoteImage,
		Local:  LocalSource,
		TmpDir: tmp,
	}

	opts := &config.WarmerOptions{}

	_, err = cw.Warm(image, opts)
	if !IsAlreadyCached(err) {
		t.Errorf("expected error to be already cached err but was %v", err)
		t.FailNow()
	}
}

func Test_Warmer_Warm_in_cache_expired(t *testing.T) {
	tmp, err := os.MkdirTemp("", "")
	t.Error(err)
	defer os.RemoveAll(tmp)

	cw := &Warmer{
		Remote: remote.RetrieveRemoteImage,
		Local:  LocalSource,
		TmpDir: tmp,
	}

	opts := &config.WarmerOptions{}

	_, err = cw.Warm(image, opts)
	if !IsAlreadyCached(err) {
		t.Errorf("expected error to be already cached err but was %v", err)
		t.FailNow()
	}
}

func TestParseDockerfile_SingleStageDockerfile(t *testing.T) {
	dockerfile := `FROM alpine:latest
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
