package testissuemz813

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_issue_mz813",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		// Without FF_KANIKO_RESOLVE_CACHE_KEY the raw instruction is keyed, so
		// A=one and A=two share one WORKDIR cache key and a build that only changes
		// A serves a stale layer. The two plans are identical.
		{
			Args: []string{"--no-push", "--cache", "--build-arg", "A=one"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD": "1",
			},
			Plan: "unresolved_one",
		},
		{
			Args: []string{"--no-push", "--cache", "--build-arg", "A=two"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD": "1",
			},
			Plan: "unresolved_two",
		},
		// With the flag the resolved instruction is keyed, so the WORKDIR cache key
		// tracks A. The two plans differ in the WORKDIR key.
		{
			Args: []string{"--no-push", "--cache", "--build-arg", "A=one"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD":   "1",
				"FF_KANIKO_RESOLVE_CACHE_KEY": "1",
			},
			Plan: "resolved_one",
		},
		{
			Args: []string{"--no-push", "--cache", "--build-arg", "A=two"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD":   "1",
				"FF_KANIKO_RESOLVE_CACHE_KEY": "1",
			},
			Plan: "resolved_two",
		},
	},
}
