package testwarmercopyfrom

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_warmer_copyfrom",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{"--target=runtime"},
			Plan: "cross_stage_dependency",
		},
	},
}
