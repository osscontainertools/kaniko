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
	"os"
	"testing"

	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/dockerfile"
	"github.com/osscontainertools/kaniko/pkg/util"
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

func TestIsURL(t *testing.T) {
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
			testutil.CheckDeepEqual(t, tt.expected, util.IsURL(tt.path))
		})
	}
}

func TestResolveBuildArgs(t *testing.T) {
	t.Cleanup(config.InitFeatureFlags)

	t.Setenv("KANIKO_TEST_UNSET", "")
	if err := os.Unsetenv("KANIKO_TEST_UNSET"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANIKO_TEST_EMPTY", "")
	t.Setenv("KANIKO_TEST_SET", "fromenv")

	t.Run("legacy sets unset build arg to empty", func(t *testing.T) {
		t.Setenv("FF_KANIKO_SKIP_UNSET_BUILD_ARGS", "false")
		config.InitFeatureFlags()

		testOpts := config.KanikoOptions{
			BuildArgs: []string{"KANIKO_TEST_UNSET", "KANIKO_TEST_EMPTY", "KANIKO_TEST_SET"},
		}
		resolveBuildArgs(&testOpts)

		ba := dockerfile.NewBuildArgs(testOpts.BuildArgs)
		ba.AddArg("KANIKO_TEST_UNSET", new("default"))
		ba.AddArg("KANIKO_TEST_EMPTY", new("default"))
		ba.AddArg("KANIKO_TEST_SET", new("default"))
		testutil.CheckDeepEqual(t, map[string]string{
			"KANIKO_TEST_UNSET": "",
			"KANIKO_TEST_EMPTY": "",
			"KANIKO_TEST_SET":   "fromenv",
		}, ba.GetAllAllowed())
	})

	t.Run("skip unset build arg keeps ARG default", func(t *testing.T) {
		t.Setenv("FF_KANIKO_SKIP_UNSET_BUILD_ARGS", "true")
		config.InitFeatureFlags()

		testOpts := config.KanikoOptions{
			BuildArgs: []string{"KANIKO_TEST_UNSET", "KANIKO_TEST_EMPTY", "KANIKO_TEST_SET"},
		}
		resolveBuildArgs(&testOpts)

		ba := dockerfile.NewBuildArgs(testOpts.BuildArgs)
		ba.AddArg("KANIKO_TEST_UNSET", new("default"))
		ba.AddArg("KANIKO_TEST_EMPTY", new("default"))
		ba.AddArg("KANIKO_TEST_SET", new("default"))
		testutil.CheckDeepEqual(t, map[string]string{
			"KANIKO_TEST_UNSET": "default",
			"KANIKO_TEST_EMPTY": "",
			"KANIKO_TEST_SET":   "fromenv",
		}, ba.GetAllAllowed())
	})
}
