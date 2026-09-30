package testissuemz1133

import "github.com/osscontainertools/kaniko/golden/types"

// the key the --link heredoc copy is cached under, seeded into every case below
var linkKey = []string{"f9e845d0b012dbb36137eda5a670fad1a0958d03d08382b599dd512346818c23"}

var env = map[string]string{
	"FF_KANIKO_CACHE_LOOKAHEAD":             "1",
	"FF_KANIKO_INFER_CROSS_STAGE_CACHE_KEY": "1",
	"FF_KANIKO_ROLLING_CACHE_KEY":           "1",
	"FF_KANIKO_COPY_LINK":                   "1",
}

var unlinkedEnv = map[string]string{
	"FF_KANIKO_CACHE_LOOKAHEAD":             "1",
	"FF_KANIKO_INFER_CROSS_STAGE_CACHE_KEY": "1",
	"FF_KANIKO_ROLLING_CACHE_KEY":           "1",
	"FF_KANIKO_COPY_LINK":                   "0",
}

var Tests = types.GoldenTests{
	Name:       "test_issue_mz1133",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		// The salt changes the RUN key, and with it the chain key the plain COPY
		// and /after miss under. Across the two plans those move and both --link
		// keys stay put, the heredoc one a hit under the seeded entry and the
		// cross-stage one a redirect that never needs the builder built.
		{
			Args:       []string{"--no-push", "--cache", "--cache-copy-layers", "--build-arg", "SALT=one"},
			Env:        env,
			CachedKeys: linkKey,
			Plan:       "salt_one",
		},
		{
			Args:       []string{"--no-push", "--cache", "--cache-copy-layers", "--build-arg", "SALT=two"},
			Env:        env,
			CachedKeys: linkKey,
			Plan:       "salt_two",
		},
		// With the flag off both copies are keyed on the chain like any other, so
		// the seeded entry is not found and every key moves with the salt.
		{
			Args:       []string{"--no-push", "--cache", "--cache-copy-layers", "--build-arg", "SALT=one"},
			Env:        unlinkedEnv,
			CachedKeys: linkKey,
			Plan:       "unlinked_one",
		},
		{
			Args:       []string{"--no-push", "--cache", "--cache-copy-layers", "--build-arg", "SALT=two"},
			Env:        unlinkedEnv,
			CachedKeys: linkKey,
			Plan:       "unlinked_two",
		},
	},
}
