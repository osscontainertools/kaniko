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
				"85da41b943971c4d3e09af20264c54b75dde9593e8104ffab77fbb86896e4756",
				"52b7a07c8f6ea53bb7b5591998838ffb46212e21e322a98bb88fe3fe8c1d0fca",
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
				"85da41b943971c4d3e09af20264c54b75dde9593e8104ffab77fbb86896e4756",
				"52b7a07c8f6ea53bb7b5591998838ffb46212e21e322a98bb88fe3fe8c1d0fca",
			},
			Plan: "probe_after_miss",
		},
	},
}
