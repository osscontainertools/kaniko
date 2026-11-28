package testwarmertarget

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_warmer_target",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{"--target=runtime"},
			Env:  map[string]string{"FF_KANIKO_WARMER_EXECUTOR_PARITY": "1"},
			Plan: "earlier_stage_dependency",
		},
		{
			Args: []string{"--target=runtime,unrelated"},
			Env:  map[string]string{"FF_KANIKO_WARMER_EXECUTOR_PARITY": "1"},
			Plan: "multiple_targets",
		},
	},
}
