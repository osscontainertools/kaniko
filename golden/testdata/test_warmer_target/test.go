package testwarmertarget

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_warmer_target",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{"--target=runtime"},
			Plan: "earlier_stage_dependency",
		},
	},
}
