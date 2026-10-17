package testissuemz791

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_issue_mz791",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		// The resolved instruction is keyed, so the COPY cache key tracks A.
		// The two plans differ in the COPY key.
		{
			Args: []string{"--no-push", "--cache", "--cache-copy-layers", "--build-arg", "A=one"},
			Plan: "resolved_one",
		},
		{
			Args: []string{"--no-push", "--cache", "--cache-copy-layers", "--build-arg", "A=two"},
			Plan: "resolved_two",
		},
	},
}
