package testwarmernotarget

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_warmer_no_target",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{Plan: "all_stages"},
	},
}
