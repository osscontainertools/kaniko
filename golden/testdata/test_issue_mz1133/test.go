package testissuemz1133

import "github.com/osscontainertools/kaniko/golden/types"

// every cacheable key the SALT=one build produces, so that build is all hits
// and the cases below show what is left of it. ARG is metadata only and caches
// nothing, so it reads as a miss in every plan.
var seeded = []string{
	"344874f93cf2bba692143c791c494cf46fa1c66d8b516aa00241c689d26e4227",
	"b5919662addb433cc36a3c1cdbf550b76ae9bc4ad239adb389fe325e6f8f46b9",
	"2444c4f32ce670eadc6dfb2d8a5ab1f057766da1293334039ba99536e8e6f33c",
	"f9e845d0b012dbb36137eda5a670fad1a0958d03d08382b599dd512346818c23",
	"6ccec7df4efb59d9afa92991c7424380f2ee1c99f5271b15ddb3b08cee21037d",
	"32f7548bf86d1f167398bea9fb6f52145c1122fc69987d2657bd30f26ffb2350",
}

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
		// the build the keys were taken from
		{
			Args:       []string{"--no-push", "--cache", "--cache-copy-layers", "--build-arg", "SALT=one"},
			Env:        env,
			CachedKeys: seeded,
			Plan:       "salt_one",
		},
		// the salt moves every key that descends from the chain, so the two
		// --link copies are the only ones left hitting
		{
			Args:       []string{"--no-push", "--cache", "--cache-copy-layers", "--build-arg", "SALT=two"},
			Env:        env,
			CachedKeys: seeded,
			Plan:       "salt_two",
		},
		// with the flag off they descend from the chain like everything else and
		// nothing is left
		{
			Args:       []string{"--no-push", "--cache", "--cache-copy-layers", "--build-arg", "SALT=two"},
			Env:        unlinkedEnv,
			CachedKeys: seeded,
			Plan:       "unlinked",
		},
	},
}
