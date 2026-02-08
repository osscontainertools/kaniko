package testissuemz195

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_issue_mz195",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{"--no-push"},
			// TODO: clean after first-stage is unnecesary
			Plan: "normal.txt",
		},
		{
			Args: []string{"--no-push", "--target=fifth-stage"},
			Plan: "normal.txt",
		},
		{
			Args: []string{"--destination=registry"},
			Plan: "push.txt",
		},
		{
			Args: []string{"--skip-unused-stages=false", "--no-push"},
			Plan: "noskip.txt",
		},
		{
			Args: []string{"--no-push"},
			Env: map[string]string{
				"FF_KANIKO_SQUASH_STAGES": "0",
			},
			Plan: "nosquash.txt",
		},
		{
			Args: []string{"--no-push", "--target=fourth-stage"},
			Plan: "fourth.txt",
		},
		{
			Args: []string{"--no-push", "--target=noise"},
			Plan: "noise.txt",
		},
	},
}
