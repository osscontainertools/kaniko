package testissuemz1212

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_issue_mz1212",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{},
			Env:  map[string]string{"FF_KANIKO_CACHE_COPY_FROM": "0"},
			Plan: "from_only",
		},
		{
			Args: []string{},
			Env:  map[string]string{"FF_KANIKO_CACHE_COPY_FROM": "1"},
			Plan: "copy_from",
		},
	},
}
