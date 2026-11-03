package testissuemz1349

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_issue_mz1349",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{},
			Plan: "all",
		},
		{
			Args: []string{"app", "--set", "app.cache_to=example.com/app/cache/override"},
			Plan: "app_override",
		},
	},
}
