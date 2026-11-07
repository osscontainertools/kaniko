package testbakecontextdockerfile

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_bake_context_dockerfile",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{"both"},
			Plan: "both",
		},
		{
			Args: []string{"only_context"},
			Plan: "only_context",
		},
		{
			Args: []string{"only_dockerfile"},
			Plan: "only_dockerfile",
		},
		{
			Args: []string{"neither"},
			Plan: "neither",
		},
		{
			Args: []string{},
			Plan: "all",
		},
	},
}
