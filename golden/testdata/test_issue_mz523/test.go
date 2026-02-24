package testissuemz523

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_issue_mz523",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{"--no-push"},
			Plan: "normal",
		},
		{
			Args: []string{"--no-push"},
			Env: map[string]string{
				"FF_KANIKO_SKIP_INTERSTAGE_CLEANUP": "1",
			},
			Plan: "nocleanup",
		},
	},
}
