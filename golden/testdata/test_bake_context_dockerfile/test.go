package testbakecontextdockerfile

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_bake_context_dockerfile",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{"both"},
			Plan: "both",
			Env:  map[string]string{"FF_KANIKO_BAKE_CONTEXT_DOCKERFILE": "1"},
		},
		{
			Args: []string{"only_context"},
			Plan: "only_context",
			Env:  map[string]string{"FF_KANIKO_BAKE_CONTEXT_DOCKERFILE": "1"},
		},
		{
			Args: []string{"only_dockerfile"},
			Plan: "only_dockerfile",
			Env:  map[string]string{"FF_KANIKO_BAKE_CONTEXT_DOCKERFILE": "1"},
		},
		{
			Args: []string{"neither"},
			Plan: "neither",
			Env:  map[string]string{"FF_KANIKO_BAKE_CONTEXT_DOCKERFILE": "1"},
		},
		{
			Args: []string{},
			Plan: "all",
			Env:  map[string]string{"FF_KANIKO_BAKE_CONTEXT_DOCKERFILE": "1"},
		},
	},
}
