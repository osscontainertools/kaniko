/*
Copyright 2018 Google LLC

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

package cmd

import (
	"testing"

	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/testutil"
)

func TestSkipPath(t *testing.T) {
	tests := []struct {
		description string
		path        string
		expected    bool
	}{
		{
			description: "path is a http url",
			path:        "http://test",
			expected:    true,
		},
		{
			description: "path is a https url",
			path:        "https://test",
			expected:    true,
		},
		{
			description: "path is a empty",
			path:        "",
			expected:    true,
		},
		{
			description: "path is already abs",
			path:        "/tmp/test",
			expected:    true,
		},
		{
			description: "path is relative",
			path:        ".././test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			testutil.CheckDeepEqual(t, tt.expected, shdSkip(tt.path))
		})
	}
}

func TestResolveCacheSpecs(t *testing.T) {
	t.Run("feature flag disabled", func(t *testing.T) {
		config.FF.CacheFromTo = false
		opts := &config.KanikoOptions{
			CacheFrom: "type=registry,ref=example.com/cache",
		}
		err := resolveCacheSpecs(opts, false, false, false)
		testutil.CheckError(t, true, err)
	})

	t.Run("feature flag enabled - only cache-from registry", func(t *testing.T) {
		config.FF.CacheFromTo = true
		opts := &config.KanikoOptions{
			CacheFrom: "type=registry,ref=example.com/cache",
		}
		err := resolveCacheSpecs(opts, false, false, false)
		testutil.CheckNoError(t, err)
		testutil.CheckDeepEqual(t, true, opts.Cache)
		testutil.CheckDeepEqual(t, "example.com/cache", opts.CacheRepo)
		testutil.CheckDeepEqual(t, true, opts.NoPushCache)
	})

	t.Run("feature flag enabled - only cache-from local", func(t *testing.T) {
		config.FF.CacheFromTo = true
		opts := &config.KanikoOptions{
			CacheFrom: "type=local,src=/my/local/cache",
			CacheDir:  "/cache",
		}
		err := resolveCacheSpecs(opts, false, false, false)
		testutil.CheckNoError(t, err)
		testutil.CheckDeepEqual(t, true, opts.Cache)
		testutil.CheckDeepEqual(t, "/my/local/cache", opts.CacheDir)
		testutil.CheckDeepEqual(t, true, opts.NoPushCache)
	})

	t.Run("feature flag enabled - cache-from and cache-to registry", func(t *testing.T) {
		config.FF.CacheFromTo = true
		opts := &config.KanikoOptions{
			CacheFrom: "type=registry,ref=example.com/cache-from",
			CacheTo:   "type=registry,ref=example.com/cache-to",
		}
		err := resolveCacheSpecs(opts, false, false, false)
		testutil.CheckNoError(t, err)
		testutil.CheckDeepEqual(t, true, opts.Cache)
		testutil.CheckDeepEqual(t, "example.com/cache-to", opts.CacheRepo)
		testutil.CheckDeepEqual(t, false, opts.NoPushCache)
	})

	t.Run("feature flag enabled - only cache-to registry", func(t *testing.T) {
		config.FF.CacheFromTo = true
		opts := &config.KanikoOptions{
			CacheTo: "type=registry,ref=example.com/cache-to",
		}
		err := resolveCacheSpecs(opts, false, false, false)
		testutil.CheckNoError(t, err)
		testutil.CheckDeepEqual(t, true, opts.Cache)
		testutil.CheckDeepEqual(t, "example.com/cache-to", opts.CacheRepo)
		testutil.CheckDeepEqual(t, false, opts.NoPushCache)
	})

	t.Run("feature flag enabled - cache-to with explicit no-push-cache fails", func(t *testing.T) {
		config.FF.CacheFromTo = true
		opts := &config.KanikoOptions{
			CacheTo:     "type=registry,ref=example.com/cache-to",
			NoPushCache: true,
		}
		err := resolveCacheSpecs(opts, true, false, false)
		testutil.CheckError(t, true, err)
	})

	t.Run("feature flag enabled - cache-from registry overrides explicit cache-repo with warning", func(t *testing.T) {
		config.FF.CacheFromTo = true
		opts := &config.KanikoOptions{
			CacheRepo: "example.com/old-cache",
			CacheFrom: "type=registry,ref=example.com/new-cache",
		}
		err := resolveCacheSpecs(opts, false, true, false)
		testutil.CheckNoError(t, err)
		testutil.CheckDeepEqual(t, "example.com/new-cache", opts.CacheRepo)
	})

	t.Run("feature flag enabled - cache-to registry overrides explicit cache-repo with warning", func(t *testing.T) {
		config.FF.CacheFromTo = true
		opts := &config.KanikoOptions{
			CacheRepo: "example.com/old-cache",
			CacheTo:   "type=registry,ref=example.com/new-cache",
		}
		err := resolveCacheSpecs(opts, false, true, false)
		testutil.CheckNoError(t, err)
		testutil.CheckDeepEqual(t, "example.com/new-cache", opts.CacheRepo)
	})
}

func TestIsUrl(t *testing.T) {
	tests := []struct {
		description string
		path        string
		expected    bool
	}{
		{
			description: "path is a http url",
			path:        "http://test",
			expected:    true,
		},
		{
			description: "path is a https url",
			path:        "https://test",
			expected:    true,
		},
		{
			description: "path is a empty",
			path:        "",
		},
		{
			description: "path is already abs",
			path:        "/tmp/test",
		},
		{
			description: "path is relative",
			path:        ".././test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			testutil.CheckDeepEqual(t, tt.expected, isURL(tt.path))
		})
	}
}

func TestResolveEnvironmentBuildArgs(t *testing.T) {
	tests := []struct {
		description               string
		input                     []string
		expected                  []string
		mockedEnvironmentResolver func(string) string
	}{
		{
			description: "replace when environment variable is present and value is not specified",
			input:       []string{"variable1"},
			expected:    []string{"variable1=value1"},
			mockedEnvironmentResolver: func(variable string) string {
				if variable == "variable1" {
					return "value1"
				}
				return ""
			},
		},
		{
			description: "do not replace when environment variable is present and value is specified",
			input:       []string{"variable1=value1", "variable2=value2"},
			expected:    []string{"variable1=value1", "variable2=value2"},
			mockedEnvironmentResolver: func(_ string) string {
				return "unexpected"
			},
		},
		{
			description: "do not replace when environment variable is present and empty value is specified",
			input:       []string{"variable1="},
			expected:    []string{"variable1="},
			mockedEnvironmentResolver: func(_ string) string {
				return "unexpected"
			},
		},
		{
			description: "replace with empty value when environment variable is not present or empty and value is not specified",
			input:       []string{"variable1", "variable2=value2"},
			expected:    []string{"variable1=", "variable2=value2"},
			mockedEnvironmentResolver: func(_ string) string {
				return ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			resolveEnvironmentBuildArgs(tt.input, tt.mockedEnvironmentResolver)
			testutil.CheckDeepEqual(t, tt.expected, tt.input)
		})
	}
}
