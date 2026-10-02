package testissuemz1128

import "github.com/osscontainertools/kaniko/golden/types"

// mz1128: common and meta both have two children so squashing cannot merge them
// away, and nothing copies from them, so neither needs the root filesystem.
// common gets there through a cache hit, meta through a metadata-only command.
// the RUN in common, the only cache hit in the build
var commonKey = []string{"1c016060d47d3e3e06f1495da9217e1c46134d7cfb263f4fdfd1c49201c6733c"}

var Tests = types.GoldenTests{
	Name:       "test_issue_mz1128",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args:       []string{"--no-push", "--cache", "--cache-copy-layers"},
			Env:        map[string]string{"FF_KANIKO_CACHE_LOOKAHEAD": "1"},
			CachedKeys: commonKey,
			Plan:       "unpacked",
		},
		{
			Args: []string{"--no-push", "--cache", "--cache-copy-layers"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD": "1",
				"FF_KANIKO_IMAGE_STAGES":    "1",
			},
			CachedKeys: commonKey,
			Plan:       "image-stage",
		},
	},
}
