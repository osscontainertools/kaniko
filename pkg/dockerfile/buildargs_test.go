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
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
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
			ResolveEnvironmentBuildArgs(tt.input, tt.mockedEnvironmentResolver)
			testutil.CheckDeepEqual(t, tt.expected, tt.input)
		})
	}
}
