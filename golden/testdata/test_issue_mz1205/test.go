package testissuemz1205

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_issue_mz1205",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{},
			Plan: "pull",
		},
		{
			Args: []string{},
			Env:  map[string]string{"FF_KANIKO_WARMER_SKIP_SCRATCH": "1"},
			Plan: "skip",
		},
		{
			Args: []string{"--image=scratch"},
			Env:  map[string]string{"FF_KANIKO_WARMER_SKIP_SCRATCH": "1"},
			Plan: "image",
		},
	},
}
