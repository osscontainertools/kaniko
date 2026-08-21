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

package creds

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
)

func TestPathScopedKeychain(t *testing.T) {
	tests := []struct {
		name  string
		auths map[string]string
		ref   string
		want  string
	}{{
		name: "exact repository entry",
		auths: map[string]string{
			"registry.example.com":                   "host:pw",
			"registry.example.com/org/project/image": "exact:pw",
		},
		ref:  "registry.example.com/org/project/image",
		want: "exact:pw",
	}, {
		name: "intermediate namespace entry",
		auths: map[string]string{
			"registry.example.com":     "host:pw",
			"registry.example.com/org": "org:pw",
		},
		ref:  "registry.example.com/org/project/image",
		want: "org:pw",
	}, {
		name: "most specific entry wins",
		auths: map[string]string{
			"registry.example.com":             "host:pw",
			"registry.example.com/org":         "org:pw",
			"registry.example.com/org/project": "project:pw",
		},
		ref:  "registry.example.com/org/project/image",
		want: "project:pw",
	}, {
		name:  "registry host entry",
		auths: map[string]string{"registry.example.com": "host:pw"},
		ref:   "registry.example.com/org/project/image",
		want:  "host:pw",
	}, {
		name:  "legacy registry url entry",
		auths: map[string]string{"https://registry.example.com/v1/": "legacy:pw"},
		ref:   "registry.example.com/org/project/image",
		want:  "legacy:pw",
	}, {
		name:  "docker hub default entry",
		auths: map[string]string{"https://index.docker.io/v1/": "hub:pw"},
		ref:   "ubuntu",
		want:  "hub:pw",
	}, {
		name:  "namespace entry is not a string prefix",
		auths: map[string]string{"registry.example.com/org": "org:pw"},
		ref:   "registry.example.com/org-admin/image",
		want:  "anonymous",
	}, {
		name: "namespace entry falls back to the host entry",
		auths: map[string]string{
			"registry.example.com":     "host:pw",
			"registry.example.com/org": "org:pw",
		},
		ref:  "registry.example.com/org-admin/image",
		want: "host:pw",
	}, {
		name: "sibling namespace entries are not consulted",
		auths: map[string]string{
			"registry.example.com/org-a": "a:pw",
			"registry.example.com/org-b": "b:pw",
		},
		ref:  "registry.example.com/org-c/image",
		want: "anonymous",
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			auths := map[string]map[string]string{}
			for key, cred := range tc.auths {
				auths[key] = map[string]string{"auth": base64.StdEncoding.EncodeToString([]byte(cred))}
			}
			dockerConfig, err := json.Marshal(map[string]any{"auths": auths})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			err = os.WriteFile(filepath.Join(dir, "config.json"), dockerConfig, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", t.TempDir())
			t.Setenv("DOCKER_CONFIG", dir)
			t.Setenv("DOCKER_AUTH_CONFIG", "")
			t.Setenv("REGISTRY_AUTH_FILE", "")
			t.Setenv("XDG_RUNTIME_DIR", "")

			repo, err := name.NewRepository(tc.ref)
			if err != nil {
				t.Fatal(err)
			}
			auth, err := PathScopedKeychain.Resolve(repo)
			if err != nil {
				t.Fatal(err)
			}

			got := "anonymous"
			if auth != authn.Anonymous {
				cfg, err := auth.Authorization()
				if err != nil {
					t.Fatal(err)
				}
				got = cfg.Username + ":" + cfg.Password
			}
			if got != tc.want {
				t.Errorf("resolved %s to %s, want %s", tc.ref, got, tc.want)
			}
		})
	}
}
