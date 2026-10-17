package testissuemz703

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_issue_mz703",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{"--no-push", "--cache"},
			CachedKeys: []string{
				"e035ca8f4e103dcf7c33e3b9326f1b2cc1aadf83afa8c0fd120a294f57bdedd1",
				"e613d59aa1b40471526654121565470c7c79e1489561c6eac9328415551d60ec",
			},
			Plan: "legacy_stop_after_miss",
		},
		{
			Args: []string{"--no-push", "--cache"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_PROBE_AFTER_MISS": "1",
			},
			CachedKeys: []string{
				"e035ca8f4e103dcf7c33e3b9326f1b2cc1aadf83afa8c0fd120a294f57bdedd1",
				"e613d59aa1b40471526654121565470c7c79e1489561c6eac9328415551d60ec",
			},
			Plan: "probe_after_miss",
		},
	},
}
