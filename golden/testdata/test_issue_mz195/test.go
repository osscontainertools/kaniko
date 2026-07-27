package testissuemz195

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_issue_mz195",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{"--no-push"},
			Env: map[string]string{
				"FF_KANIKO_SHARED_BASE_CACHE": "0",
			},
			// TODO: clean after first-stage is unnecesary
			Plan: "normal",
		},
		{
			Args: []string{"--no-push", "--target=fifth-stage"},
			Env: map[string]string{
				"FF_KANIKO_SHARED_BASE_CACHE": "0",
			},
			Plan: "normal",
		},
		{
			Args: []string{"--destination=registry"},
			Env: map[string]string{
				"FF_KANIKO_SHARED_BASE_CACHE": "0",
			},
			Plan: "push",
		},
		{
			Args: []string{"--no-push", "--target=fourth-stage"},
			Env: map[string]string{
				"FF_KANIKO_SHARED_BASE_CACHE": "0",
			},
			Plan: "fourth",
		},
		{
			Args: []string{"--no-push", "--target=noise"},
			Env: map[string]string{
				"FF_KANIKO_SHARED_BASE_CACHE": "0",
			},
			Plan: "noise",
		},
	},
}
