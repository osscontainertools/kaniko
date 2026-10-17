package testissuemz822

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_issue_mz822",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		// The resolved heredoc body is folded into the key, so the COPY cache key
		// tracks A. The unquoted delimiter expands the body, so the key matches
		// what the executor writes. The two plans differ in the COPY key.
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
