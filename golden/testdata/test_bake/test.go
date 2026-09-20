package testbake

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_bake",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{"app"},
			Env: map[string]string{
				"FF_KANIKO_SHARED_BASE_CACHE": "0",
			},
			Plan: "app",
		},
		{
			Args: []string{"tools"},
			Env: map[string]string{
				"FF_KANIKO_SHARED_BASE_CACHE": "0",
			},
			Plan: "tools",
		},
		{
			Args: []string{"app", "--set", "app.destination=registry.example.com/app:override"},
			Env: map[string]string{
				"FF_KANIKO_SHARED_BASE_CACHE": "0",
			},
			Plan: "app_override",
		},
		{
			Args: []string{},
			Env: map[string]string{
				"FF_KANIKO_SHARED_BASE_CACHE": "0",
			},
			Plan: "all",
		},
	},
}
