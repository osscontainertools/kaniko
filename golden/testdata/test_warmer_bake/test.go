package testwarmerbake

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name: "test_warmer_bake",
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
			Args: []string{"neither", "--set", "neither.dockerfile=app/Dockerfile.custom"},
			Plan: "set_dockerfile",
		},
		{
			Args: []string{},
			Plan: "all",
		},
	},
}
