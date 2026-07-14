package testissuemz334

import "github.com/osscontainertools/kaniko/golden/types"

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
			},
			CachedKeys: []string{
				"72e9e0e54e4522d381e54427f5ac6f24dd09910e1ff8d4bc7f60d02f54e2cdc3",
				"3829b10dc17cc7bafd22450e05b7f265b73e94d46b08817d0502848b21dd69aa",
				"256455fba386c671b4808e621379712ca6dfecce4d4e9ed2d6edab8b5e415b75",
				"7ffdded6b9b986b59b4e27ca74ec544a28acfcbd0f87e6f848d64c2631f4d9f0",
				"7e3daa5c6ad10774b5430117c90f8fe60002da5f2f2175c52dd094506355eb81",
				"2aba9bf1ce0db5b0cae37fddf680713ba7cd60797cb0742498830f4267db95c0",
				"e0a60f4194bd9f2ee44a9063f3036eda32322bc8ae7773d897e9ca27dc356fcb",
				"2fa9a748e3206653ffc13bd26b1cb8f29117a513d4c8bb7bcd95a6a8a0618e4f",
			},
			Plan: "inferred",
		},
	},
}
