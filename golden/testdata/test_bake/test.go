package testbake

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_bake",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{"app"},
			Plan: "app",
		},
		{
			Args: []string{"tools"},
			Plan: "tools",
		},
		{
			Args: []string{"app", "--set", "app.destination=registry.example.com/app:override"},
			Plan: "app_override",
		},
	},
}
