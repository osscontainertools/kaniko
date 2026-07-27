package testissuemz487

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_issue_mz487",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{"--no-push"},
			Env: map[string]string{
				"FF_KANIKO_SHARED_BASE_CACHE": "0",
			},
			Plan: "plan",
		},
	},
}
