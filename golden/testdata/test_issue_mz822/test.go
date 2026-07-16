package testissuemz822

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_issue_mz822",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		// Without FF_KANIKO_RESOLVE_CACHE_KEY the heredoc body is omitted from the
		// key, so A=one and A=two write different content but share one COPY cache
		// key and a build that only changes A serves a stale file. Both cases pin
		// the same plan, so the collision is asserted directly.
		{
			Args: []string{"--no-push", "--cache", "--build-arg", "A=one"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD": "1",
				"FF_KANIKO_EXPAND_HEREDOC":  "1",
			},
			Plan: "unresolved",
		},
		{
			Args: []string{"--no-push", "--cache", "--build-arg", "A=two"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD": "1",
				"FF_KANIKO_EXPAND_HEREDOC":  "1",
			},
			Plan: "unresolved",
		},
		// With the flag the resolved heredoc body is folded into the key, so the
		// COPY cache key tracks A. The unquoted delimiter expands the body, so
		// FF_KANIKO_EXPAND_HEREDOC must be on for the key to match what the
		// executor writes. The two plans differ in the COPY key.
		{
			Args: []string{"--no-push", "--cache", "--build-arg", "A=one"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD":   "1",
				"FF_KANIKO_RESOLVE_CACHE_KEY": "1",
				"FF_KANIKO_EXPAND_HEREDOC":    "1",
			},
			Plan: "resolved_one",
		},
		{
			Args: []string{"--no-push", "--cache", "--build-arg", "A=two"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD":   "1",
				"FF_KANIKO_RESOLVE_CACHE_KEY": "1",
				"FF_KANIKO_EXPAND_HEREDOC":    "1",
			},
			Plan: "resolved_two",
		},
	},
}
