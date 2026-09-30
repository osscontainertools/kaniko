package testissuemz1133

import "github.com/osscontainertools/kaniko/golden/types"

// the key the --link copy is cached under, seeded into every case below
var linkKey = []string{"6a9effed78e57934c40f5a32cb08a30adb13b071be8bce91881fd2c387fb6aab"}

var Tests = types.GoldenTests{
	Name:       "test_issue_mz1133",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		// The salt changes the RUN key, and with it the chain key the plain COPY
		// misses under. The two plans differ in those two keys and agree on the
		// COPY --link key, which stays a hit although the command above it missed.
		{
			Args: []string{"--no-push", "--cache", "--cache-copy-layers", "--build-arg", "SALT=one"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD": "1",
				"FF_KANIKO_COPY_LINK":       "1",
			},
			CachedKeys: linkKey,
			Plan:       "salt_one",
		},
		{
			Args: []string{"--no-push", "--cache", "--cache-copy-layers", "--build-arg", "SALT=two"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD": "1",
				"FF_KANIKO_COPY_LINK":       "1",
			},
			CachedKeys: linkKey,
			Plan:       "salt_two",
		},
		// With the flag off the copy is keyed on the chain like any other, so the
		// same cache entry is not found and the modifier only reaches the plan text.
		// The trailing RUN also moves, the chain carried the copy's own inputs here
		// and the link key in salt_one.
		{
			Args: []string{"--no-push", "--cache", "--cache-copy-layers", "--build-arg", "SALT=one"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD": "1",
				"FF_KANIKO_COPY_LINK":       "0",
			},
			CachedKeys: linkKey,
			Plan:       "unlinked",
		},
	},
}
