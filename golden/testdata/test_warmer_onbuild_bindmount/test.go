package testwarmeronbuildbindmount

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_warmer_onbuild_bindmount",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{"--target=runtime"},
			Env:  map[string]string{"FF_KANIKO_RUN_CROSS_STAGE_BIND_MOUNT": "1", "FF_KANIKO_WARMER_EXECUTOR_PARITY": "1"},
			Plan: "cross_stage_dependency",
		},
	},
}
