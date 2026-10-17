package testissuemz1128

import "github.com/osscontainertools/kaniko/golden/types"

// mz1128: common and meta both have two children so squashing cannot merge them
// away, and nothing copies from them, so neither needs the root filesystem.
// common gets there through a cache hit, meta through a metadata-only command.
// the RUN in common, the only cache hit in the build
var commonKey = []string{"2f174f5173821b006024ced92778fbf0999c942ac7a65c6cd0de303ac59616de"}

var Tests = types.GoldenTests{
	Name:       "test_issue_mz1128",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args:       []string{"--no-push", "--cache", "--cache-copy-layers"},
			CachedKeys: commonKey,
			Plan:       "image-stage",
		},
	},
}
