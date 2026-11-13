package testissuemz1420

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_issue_mz1420",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{},
			Plan: "all",
		},
		{
			Args: []string{"golang", "--set", "golang.build_arg.VERSION=1.4"},
			Plan: "set",
		},
	},
}

var WarmerTests = types.GoldenTests{
	Name: "test_issue_mz1420",
	Tests: []types.GoldenTest{
		{
			Args: []string{},
			Plan: "warm_all",
		},
		{
			Args: []string{"golang", "--set", "golang.build_arg.BASE=busybox:1.36"},
			Plan: "warm_set",
		},
	},
}
