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
			KeySequence: []string{
				"2b9fe1fa3aca624785f33c81b10e22fdd7957bbe1791e0b390432b7dcd8d3b3c",
				"fbd83b67ae5fc062049c8f0f8caf849790fad221667b7b49db5bc5e53847dfbf",
			},
			Plan: "legacy_stop_after_miss",
		},
		{
			Args: []string{"--no-push", "--cache"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD":        "1",
				"FF_KANIKO_CACHE_PROBE_AFTER_MISS": "1",
			},
			KeySequence: []string{
				"2b9fe1fa3aca624785f33c81b10e22fdd7957bbe1791e0b390432b7dcd8d3b3c",
				"fbd83b67ae5fc062049c8f0f8caf849790fad221667b7b49db5bc5e53847dfbf",
			},
			Plan: "probe_after_miss",
		},
	},
}
