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

package dockerfile

import (
	"os"
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/testutil"
)

func TestGetAllAllowed(t *testing.T) {
	buildArgs := newBuildArgsFromMap(map[string]*string{
		"ArgNotUsedInDockerfile":              new("fromopt1"),
		"ArgOverriddenByOptions":              new("fromopt2"),
		"ArgNoDefaultInDockerfileFromOptions": new("fromopt3"),
		"HTTP_PROXY":                          new("theproxy"),
		"all_proxy":                           new("theproxy2"),
	})

	buildArgs.AddMetaArgs([]instructions.ArgCommand{
		{
			Args: []instructions.KeyValuePairOptional{
				{
					Key:   "ArgFromMeta",
					Value: new("frommeta1"),
				},
				{
					Key:   "ArgOverriddenByOptions",
					Value: new("frommeta2"),
				},
			},
		},
		{
			Args: []instructions.KeyValuePairOptional{
				{
					Key:   "ArgFromMetaNotUsed",
					Value: new("frommeta3"),
				},
			},
		},
	})

	buildArgs.AddArg("ArgOverriddenByOptions", new("fromdockerfile2"))
	buildArgs.AddArg("ArgWithDefaultInDockerfile", new("fromdockerfile1"))
	buildArgs.AddArg("ArgNoDefaultInDockerfile", nil)
	buildArgs.AddArg("ArgNoDefaultInDockerfileFromOptions", nil)
	buildArgs.AddArg("ArgFromMeta", nil)
	buildArgs.AddArg("ArgFromMetaOverridden", new("fromdockerfile3"))

	all := buildArgs.GetAllAllowed()
	expected := map[string]string{
		"HTTP_PROXY":                          "theproxy",
		"all_proxy":                           "theproxy2",
		"ArgOverriddenByOptions":              "fromopt2",
		"ArgWithDefaultInDockerfile":          "fromdockerfile1",
		"ArgNoDefaultInDockerfileFromOptions": "fromopt3",
		"ArgFromMeta":                         "frommeta1",
		"ArgFromMetaOverridden":               "fromdockerfile3",
	}
	testutil.CheckDeepEqual(t, expected, all)
}

func TestGetAllMeta(t *testing.T) {
	buildArgs := newBuildArgsFromMap(map[string]*string{
		"ArgNotUsedInDockerfile":        new("fromopt1"),
		"ArgOverriddenByOptions":        new("fromopt2"),
		"ArgNoDefaultInMetaFromOptions": new("fromopt3"),
		"HTTP_PROXY":                    new("theproxy"),
	})

	buildArgs.AddMetaArgs([]instructions.ArgCommand{
		{
			Args: []instructions.KeyValuePairOptional{
				{
					Key:   "ArgFromMeta",
					Value: new("frommeta1"),
				},
				{
					Key:   "ArgOverriddenByOptions",
					Value: new("frommeta2"),
				},
			},
		},
		{
			Args: []instructions.KeyValuePairOptional{
				{
					Key:   "ArgNoDefaultInMetaFromOptions",
					Value: nil,
				},
			},
		},
	})

	all := buildArgs.GetAllMeta()
	expected := map[string]string{
		"HTTP_PROXY":                    "theproxy",
		"ArgFromMeta":                   "frommeta1",
		"ArgOverriddenByOptions":        "fromopt2",
		"ArgNoDefaultInMetaFromOptions": "fromopt3",
	}
	testutil.CheckDeepEqual(t, expected, all)
}

func TestResolveEnvironmentBuildArgs(t *testing.T) {
	// registered before t.Setenv so it runs after the environment is restored
	t.Cleanup(config.InitFeatureFlags)
	t.Setenv("FF_KANIKO_SKIP_UNSET_BUILD_ARGS", "1")
	config.InitFeatureFlags()
	t.Setenv("variable1", "value1")
	t.Setenv("empty", "")
	t.Setenv("unset", "")
	if err := os.Unsetenv("unset"); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		description string
		input       []string
		expected    []string
	}{
		{"replace when environment variable is present and value is not specified", []string{"variable1"}, []string{"variable1=value1"}},
		{"do not replace when value is specified", []string{"variable1=explicit", "variable2=value2"}, []string{"variable1=explicit", "variable2=value2"}},
		{"do not replace when empty value is specified", []string{"variable1="}, []string{"variable1="}},
		{"preserve empty string when environment variable is set to empty", []string{"empty"}, []string{"empty="}},
		{"skip when environment variable is unset", []string{"unset", "variable2=value2"}, []string{"variable2=value2"}},
		{"keep earlier explicit value when bare arg is unset", []string{"unset=value1", "unset"}, []string{"unset=value1"}},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			testutil.CheckDeepEqual(t, tt.expected, ResolveEnvironmentBuildArgs(tt.input))
		})
	}
}
