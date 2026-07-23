package testissuemz936

import "github.com/osscontainertools/kaniko/golden/types"

// mz936 shared base-layer dedup, asserted through the dryrun plan under
// FF_KANIKO_SHARED_BASE_CACHE. A base pulled by several stages is downloaded once
// (first stage stores it, the rest load it), a base persisted for a downstream
// stage is stored, and on a push the final stage's base is stored because the
// push re-reads its layers to upload them.
var Tests = types.GoldenTests{
	Name:       "test_issue_mz936",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{"--no-push"},
			Env:  map[string]string{"FF_KANIKO_SHARED_BASE_CACHE": "1"},
			Plan: "shared",
		},
		{
			Args: []string{"-d", "example.com/img:latest"},
			Env:  map[string]string{"FF_KANIKO_SHARED_BASE_CACHE": "1"},
			Plan: "push",
		},
		{
			// Flag off: every base streams, the behavior before this change.
			Args: []string{"--no-push"},
			Plan: "streamed",
		},
	},
}
