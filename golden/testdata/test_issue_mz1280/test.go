package testissuemz1280

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_issue_mz1280",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args:       []string{"--no-push", "--cache", "--cache-copy-layers"},
			Env:        map[string]string{"FF_KANIKO_RUN_CROSS_STAGE_BIND_MOUNT": "1"},
			CachedKeys: []string{},
			Plan:       "plan",
		},
		{
			Args:       []string{"--no-push", "--cache", "--cache-copy-layers"},
			Env:        map[string]string{"FF_KANIKO_RUN_CROSS_STAGE_BIND_MOUNT": "1"},
			CachedKeys: []string{"87d3878d835c8a83b18463b619bfedafab58ccbaa7e40fd9dfeeb70181601a22"},
			Plan:       "cached",
		},
		{
			Args: []string{"--no-push", "--cache", "--cache-copy-layers"},
			Env:  map[string]string{"FF_KANIKO_RUN_CROSS_STAGE_BIND_MOUNT": "1"},
			CachedKeys: []string{
				"87d3878d835c8a83b18463b619bfedafab58ccbaa7e40fd9dfeeb70181601a22",
				"e6b1fddcc648a51262620e8200bcf64bae9c1e870d32fcf8ddc2a7d0d7c57f18",
			},
			Plan: "eliminated",
		},
	},
}
