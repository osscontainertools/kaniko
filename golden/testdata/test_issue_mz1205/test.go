package testissuemz1205

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_issue_mz1205",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{},
			Plan: "skip",
		},
		{
			Args: []string{"--image=scratch"},
			Plan: "image",
		},
	},
}
