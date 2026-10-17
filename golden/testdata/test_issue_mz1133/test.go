package testissuemz1133

import "github.com/osscontainertools/kaniko/golden/types"

// every cacheable key the SALT=one build produces, so that build is all hits
// and the cases below show what is left of it. ARG is metadata only and caches
// nothing, so it reads as a miss in every plan.
var seeded = []string{
	"e6646061fe04d14f4ec0590e9f083233bbee002fe35ef23f775c528881fb4c2e",
	"159fbabd7e1c2a9a62703056e277b6de6d05767cf2afa5b6891c76caf6961823",
	"fae3084c4a5083e3de7647cc49585a608817a8a98c366485bd6e5048b00e24aa",
	"672d3b4df66350ca016867dc5676c83bc3e432781b5240bd4bc70299bd5eb48b",
	"3ffa357153b58cd43d03708945f66cfad3f5dc1d208da527bd5cfe5fa11a539b",
	"57aca4e3f53788ebde7a424866d5695c9d329f7848908a7f6611880c7d9a518f",
}

var env = map[string]string{
	"FF_KANIKO_CACHE_LOOKAHEAD":             "1",
	"FF_KANIKO_INFER_CROSS_STAGE_CACHE_KEY": "1",
	"FF_KANIKO_ROLLING_CACHE_KEY":           "1",
	"FF_KANIKO_COPY_LINK":                   "1",
	"FF_KANIKO_SHARED_BASE_CACHE":           "0",
	"FF_KANIKO_SKIP_CACHED_STAGES":          "0",
}

var unlinkedEnv = map[string]string{
	"FF_KANIKO_CACHE_LOOKAHEAD":             "1",
	"FF_KANIKO_INFER_CROSS_STAGE_CACHE_KEY": "1",
	"FF_KANIKO_ROLLING_CACHE_KEY":           "1",
	"FF_KANIKO_COPY_LINK":                   "0",
	"FF_KANIKO_SHARED_BASE_CACHE":           "0",
	"FF_KANIKO_SKIP_CACHED_STAGES":          "0",
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
