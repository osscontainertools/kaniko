package testissuemz334

import "github.com/osscontainertools/kaniko/golden/types"

// shared so the elimination-off and elimination-on cases prove squashing is key-neutral
var chainKeys = []string{
	"ec50b204a07f169d1beec66434b673ca44caf8b98ee6c98e886320969926d029",
	"9097bbf817837a54a5ba9b91d9d2a771a145d25f5389c96a4c3315119aa482a5",
	"dd8070993f952ce8287efcf5c6b32d3d2399905c6fc8dc9b4036d4196b80e10f",
	"8ef1283833b79b08b093a99de08ffc3fed1fbbf8fb328f81e9d7561af0524e95",
	"c59fad0bd865d2ed209d0f7a29d55161a8332681a1b68abc9e98da2dca1254cb",
	"e3d0a39d0f55303c063b93635b40f56d535f5bd2b48b770af66bf0fe61b7debc",
}

var Tests = types.GoldenTests{
	Name:       "test_issue_mz334",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args:       []string{"--no-push", "--cache", "--cache-copy-layers"},
			CachedKeys: []string{},
			Plan:       "plan",
		},
		{
			Args: []string{"--no-push", "--cache", "--cache-copy-layers"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD": "1",
			},
			CachedKeys: []string{
				"72e9e0e54e4522d381e54427f5ac6f24dd09910e1ff8d4bc7f60d02f54e2cdc3",
				"3829b10dc17cc7bafd22450e05b7f265b73e94d46b08817d0502848b21dd69aa",
				"256455fba386c671b4808e621379712ca6dfecce4d4e9ed2d6edab8b5e415b75",
			},
			Plan: "cached",
		},
		{
			Args: []string{"--no-push", "--cache", "--cache-copy-layers"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD":             "1",
				"FF_KANIKO_INFER_CROSS_STAGE_CACHE_KEY": "1",
				"FF_KANIKO_ROLLING_CACHE_KEY":           "1",
			},
			CachedKeys: chainKeys,
			Plan:       "inferred",
		},
		{
			Args: []string{"--no-push", "--cache", "--cache-copy-layers"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD":             "1",
				"FF_KANIKO_INFER_CROSS_STAGE_CACHE_KEY": "1",
				"FF_KANIKO_ROLLING_CACHE_KEY":           "1",
				"FF_KANIKO_STAGE_ELIMINATION":           "1",
			},
			CachedKeys: chainKeys,
			Plan:       "eliminated",
		},
	},
}
