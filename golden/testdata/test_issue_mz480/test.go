package testissuemz480

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_issue_mz480",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{"--target=final", "--destination=registry"},
			// TODO: clean after "base" stage is unnecesary
			Plan: "final",
		},
		{
			Args: []string{"--target=final", "--target=build", "--destination=registry"},
			// TODO: clean after "base" stage is unnecesary
			Plan: "final",
		},
		{
			Args: []string{"--target=final", "--target=test", "--destination=registry"},
			// TODO: clean after "base" stage is unnecesary
			// TODO: saving the "final" stage is unnecessary
			// TODO: clean after "final" stage is unnecesary
			Plan: "final_test",
		},
	},
}
