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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/osscontainertools/kaniko/pkg/cache"
	"github.com/osscontainertools/kaniko/pkg/config"
)

func TestWarmersStoreIndexAliasWhenPlatformImageIsAlreadyCached(t *testing.T) {
	for _, useOCI := range []bool{false, true} {
		for _, lockRecheck := range []bool{false, true} {
			name := "tarball"
			if useOCI {
				name = "oci"
			}
			if lockRecheck {
				name += "_lock_recheck"
			} else {
				name += "_resolve_hit"
			}

			t.Run(name, func(t *testing.T) {
				oldRemote, oldLocal := RemoteSource, LocalSource
				oldOCIWarmer, oldWarmerCacheLock := config.FF.OCIWarmer, config.FF.WarmerCacheLock
				t.Cleanup(func() {
					RemoteSource, LocalSource = oldRemote, oldLocal
					config.FF.OCIWarmer, config.FF.WarmerCacheLock = oldOCIWarmer, oldWarmerCacheLock
				})
				config.FF.OCIWarmer = useOCI
				config.FF.WarmerCacheLock = lockRecheck

				cacheDir := t.TempDir()
				platform := "linux/amd64"
				imageDigest, err := empty.Image.Digest()
				if err != nil {
					t.Fatal(err)
				}
				indexDigest := "sha256:" + strings.Repeat("a", 64)
				if indexDigest == imageDigest.String() {
					t.Fatal("test index digest unexpectedly matches platform image digest")
				}
				if err := writeCachedTestImage(cacheDir, imageDigest.String(), useOCI); err != nil {
					t.Fatal(err)
				}

				localCalls := 0
				LocalSource = func(_ *config.CacheOptions, key, _ string) (v1.Image, error) {
					if key == imageDigest.String() {
						localCalls++
						if !lockRecheck || localCalls > 1 {
							return empty.Image, nil
						}
					}
					return nil, cache.NotFoundErr{}
				}
				RemoteSource = func(string, config.RegistryOptions, string) (v1.Image, error) {
					return empty.Image, nil
				}

				imageRef := "example.com/image@" + indexDigest
				opts := &config.WarmerOptions{
					CacheOptions:   config.CacheOptions{CacheDir: cacheDir, CacheTTL: time.Hour},
					CustomPlatform: platform,
				}
				if useOCI {
					err = ociWarmToFile(cacheDir, imageRef, opts)
				} else {
					err = warmToFile(cacheDir, imageRef, opts)
				}
				if err != nil {
					t.Fatalf("warming image: %v", err)
				}

				cached, err := cache.LocalSourceForPlatform(&opts.CacheOptions, indexDigest, platform)
				if err != nil {
					t.Fatalf("lookup by index digest: %v", err)
				}
				cachedDigest, err := cached.Digest()
				if err != nil {
					t.Fatal(err)
				}
				if cachedDigest != imageDigest {
					t.Fatalf("expected platform image digest %s, got %s", imageDigest, cachedDigest)
				}
			})
		}
	}
}

func writeCachedTestImage(cacheDir, digest string, useOCI bool) error {
	imagePath := filepath.Join(cacheDir, digest)
	if useOCI {
		layoutPath, err := layout.Write(imagePath, empty.Index)
		if err != nil {
			return err
		}
		return layoutPath.AppendImage(empty.Image)
	}

	file, err := os.Create(imagePath)
	if err != nil {
		return err
	}
	ref, err := name.NewTag("example.com/image:cached")
	if err != nil {
		_ = file.Close()
		return err
	}
	if err := tarball.Write(ref, empty.Image, file); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
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
