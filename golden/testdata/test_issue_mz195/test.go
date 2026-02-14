package testissuemz195

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_issue_mz195",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{"--no-push"},
			// TODO: clean after first-stage is unnecesary
			Plan: "normal",
		},
		{
			Args: []string{"--no-push", "--target=fifth-stage"},
			Plan: "normal",
		},
		{
			Args: []string{"--destination=registry"},
			Plan: "push",
		},
		{
			Args: []string{"--skip-unused-stages=false", "--no-push"},
			Plan: "noskip",
		},
		{
			Args: []string{"--no-push"},
			Env: map[string]string{
				"FF_KANIKO_SQUASH_STAGES": "0",
			},
			Plan: "nosquash",
		},
		{
			Args: []string{"--no-push", "--target=fourth-stage"},
			Plan: "fourth",
		},
		{
			Args: []string{"--no-push", "--target=noise"},
			Plan: "noise",
		},
	},
}
