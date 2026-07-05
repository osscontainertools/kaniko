package testissuemz703

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_issue_mz703",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{"--no-push", "--cache"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD": "1",
			},
			CachedKeys: []string{
				"ef4ba1bfa1a8010630d9a007fad694d95d88419c791f0053b5525169f21e3247",
				"2f4043dde38e8a86a388c786d43c46606463533a2eca79177a7246698f9b62a7",
			},
			Plan: "legacy_stop_after_miss",
		},
		{
			Args: []string{"--no-push", "--cache"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD":        "1",
				"FF_KANIKO_CACHE_PROBE_AFTER_MISS": "1",
			},
			CachedKeys: []string{
				"ef4ba1bfa1a8010630d9a007fad694d95d88419c791f0053b5525169f21e3247",
				"2f4043dde38e8a86a388c786d43c46606463533a2eca79177a7246698f9b62a7",
			},
			Plan: "probe_after_miss",
		},
	},
}
