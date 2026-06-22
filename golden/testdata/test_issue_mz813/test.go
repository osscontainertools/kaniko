package testissuemz813

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_issue_mz813",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		// Without FF_KANIKO_CACHEKEY_FOLD_ARGS the WORKDIR is keyed on the raw
		// instruction, so A=one and A=two share one cache key and a build that only
		// changes A serves a stale layer. The two plans are identical.
		{
			Args: []string{"--no-push", "--cache", "--build-arg", "A=one"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD": "1",
			},
			Plan: "unfolded_one",
		},
		{
			Args: []string{"--no-push", "--cache", "--build-arg", "A=two"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD": "1",
			},
			Plan: "unfolded_two",
		},
		// With the flag the referenced build args fold into the key, so the WORKDIR
		// cache key tracks A. The two plans differ in the WORKDIR key.
		{
			Args: []string{"--no-push", "--cache", "--build-arg", "A=one"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD":    "1",
				"FF_KANIKO_CACHEKEY_FOLD_ARGS": "1",
			},
			Plan: "folded_one",
		},
		{
			Args: []string{"--no-push", "--cache", "--build-arg", "A=two"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD":    "1",
				"FF_KANIKO_CACHEKEY_FOLD_ARGS": "1",
			},
			Plan: "folded_two",
		},
	},
}
