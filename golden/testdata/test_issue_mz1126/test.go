package testissuemz1126

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_issue_mz1126",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{"--customPlatform=linux/arm64"},
			Plan: "empty",
		},
		{
			Args: []string{"--customPlatform=linux/arm64"},
			Env:  map[string]string{"FF_KANIKO_WARMER_PREDEFINED_ARGS": "1"},
			Plan: "resolved",
		},
	},
}
