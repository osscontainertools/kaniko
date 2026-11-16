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
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/sirupsen/logrus"
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

func withWarmSources(t *testing.T, remote FetchRemoteImage, local FetchLocalSource) {
	t.Helper()
	origRemote, origLocal := RemoteSource, LocalSource
	RemoteSource = remote
	LocalSource = local
	t.Cleanup(func() { RemoteSource, LocalSource = origRemote, origLocal })
}

func captureWarns(t *testing.T, fn func()) string {
	t.Helper()
	logger := logrus.StandardLogger()
	origOut, origLevel := logger.Out, logger.Level
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.SetLevel(logrus.WarnLevel)
	t.Cleanup(func() {
		logger.SetOutput(origOut)
		logger.SetLevel(origLevel)
	})
	fn()
	return buf.String()
}

func TestWarmCachePartialFailure(t *testing.T) {
	withWarmSources(t,
		func(image string, _ config.RegistryOptions, _ string) (v1.Image, []byte, error) {
			if image == "bad" {
				return nil, nil, errors.New("boom")
			}
			return empty.Image, nil, nil
		},
		func(*config.CacheOptions, string, string) (v1.Image, error) {
			return nil, errors.New("miss")
		},
	)

	cacheDir := t.TempDir()
	opts := &config.WarmerOptions{
		CacheOptions: config.CacheOptions{CacheDir: cacheDir},
		Images:       []string{"good", "bad"},
	}

	logs := captureWarns(t, func() {
		if err := WarmCache(opts); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})
	if !strings.Contains(logs, "Failed to warm 1/2 image(s): bad") {
		t.Fatalf("expected failed image summary, got logs: %s", logs)
	}
}

func TestWarmCachePartialFailureStrict(t *testing.T) {
	withWarmSources(t,
		func(image string, _ config.RegistryOptions, _ string) (v1.Image, []byte, error) {
			if image == "bad" {
				return nil, nil, errors.New("boom")
			}
			return empty.Image, nil, nil
		},
		func(*config.CacheOptions, string, string) (v1.Image, error) {
			return nil, errors.New("miss")
		},
	)

	cacheDir := t.TempDir()
	opts := &config.WarmerOptions{
		CacheOptions:  config.CacheOptions{CacheDir: cacheDir},
		Images:        []string{"good", "bad"},
		FailOnPartial: true,
	}

	err := WarmCache(opts)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to warm 1/2 image(s): bad") {
		t.Fatalf("expected error to name failed image, got %v", err)
	}
}

func TestWarmCacheAllFail(t *testing.T) {
	withWarmSources(t,
		func(string, config.RegistryOptions, string) (v1.Image, []byte, error) {
			return nil, nil, errors.New("boom")
		},
		func(*config.CacheOptions, string, string) (v1.Image, error) {
			return nil, errors.New("miss")
		},
	)

	cacheDir := t.TempDir()
	opts := &config.WarmerOptions{
		CacheOptions: config.CacheOptions{CacheDir: cacheDir},
		Images:       []string{"bad1", "bad2"},
	}

	err := WarmCache(opts)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to warm 2/2 image(s): bad1, bad2") {
		t.Fatalf("expected error to name failed images, got %v", err)
	}
}

func TestWarmCacheAllSucceed(t *testing.T) {
	withWarmSources(t,
		func(string, config.RegistryOptions, string) (v1.Image, []byte, error) {
			return empty.Image, nil, nil
		},
		func(*config.CacheOptions, string, string) (v1.Image, error) {
			return nil, errors.New("miss")
		},
	)

	cacheDir := t.TempDir()
	opts := &config.WarmerOptions{
		CacheOptions: config.CacheOptions{CacheDir: cacheDir},
		Images:       []string{"good1", "good2"},
	}

	if err := WarmCache(opts); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestRenderPlanPartialFailure(t *testing.T) {
	withWarmSources(t,
		func(image string, _ config.RegistryOptions, _ string) (v1.Image, []byte, error) {
			if image == "bad" {
				return nil, nil, errors.New("boom")
			}
			return empty.Image, nil, nil
		},
		func(*config.CacheOptions, string, string) (v1.Image, error) {
			return nil, errors.New("miss")
		},
	)

	opts := &config.WarmerOptions{
		CacheOptions: config.CacheOptions{CacheDir: t.TempDir()},
	}

	var plan bytes.Buffer
	logs := captureWarns(t, func() {
		if err := RenderPlan(&plan, []string{"good", "bad"}, opts); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})
	if !strings.Contains(logs, "Failed to warm 1/2 image(s): bad") {
		t.Fatalf("expected failed image summary, got logs: %s", logs)
	}
}

func TestRenderPlanPartialFailureStrict(t *testing.T) {
	withWarmSources(t,
		func(image string, _ config.RegistryOptions, _ string) (v1.Image, []byte, error) {
			if image == "bad" {
				return nil, nil, errors.New("boom")
			}
			return empty.Image, nil, nil
		},
		func(*config.CacheOptions, string, string) (v1.Image, error) {
			return nil, errors.New("miss")
		},
	)

	opts := &config.WarmerOptions{
		CacheOptions:  config.CacheOptions{CacheDir: t.TempDir()},
		FailOnPartial: true,
	}

	var plan bytes.Buffer
	err := RenderPlan(&plan, []string{"good", "bad"}, opts)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to warm 1/2 image(s): bad") {
		t.Fatalf("expected error to name failed image, got %v", err)
	}
}

func TestRenderPlanAllFail(t *testing.T) {
	withWarmSources(t,
		func(string, config.RegistryOptions, string) (v1.Image, []byte, error) {
			return nil, nil, errors.New("boom")
		},
		func(*config.CacheOptions, string, string) (v1.Image, error) {
			return nil, errors.New("miss")
		},
	)

	opts := &config.WarmerOptions{
		CacheOptions: config.CacheOptions{CacheDir: t.TempDir()},
	}

	var plan bytes.Buffer
	err := RenderPlan(&plan, []string{"bad1", "bad2"}, opts)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to warm 2/2 image(s): bad1, bad2") {
		t.Fatalf("expected error to name failed images, got %v", err)
	}
}
