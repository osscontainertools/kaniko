package testissuemz813

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_issue_mz813",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		// The resolved instruction is keyed, so the WORKDIR cache key tracks A.
		// The two plans differ in the WORKDIR key.
		{
			Args: []string{"--no-push", "--cache", "--build-arg", "A=one"},
			Plan: "resolved_one",
		},
		{
			Args: []string{"--no-push", "--cache", "--build-arg", "A=two"},
			Plan: "resolved_two",
		},
	},
}
