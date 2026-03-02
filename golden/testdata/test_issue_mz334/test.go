package testissuemz334

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_issue_mz334",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args:        []string{"--no-push", "--cache", "--cache-copy-layers"},
			KeySequence: []string{},
			Plan:        "plan",
		},
		{
			Args: []string{"--no-push", "--cache", "--cache-copy-layers"},
			KeySequence: []string{
				"b0db5c4dc0ee6072dc9f3e18194c1cf4745367521c6a6bb4a8ebb269be5e7c82",
			},
			Plan: "cached",
		},
	},
}
