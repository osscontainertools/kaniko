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
				"e6201ef5c7fab656a2bb0615888d02f181fc7677dbeaaace249484bbdbf79106",
				"09e840ef4fefde5c634154ead19bbff9cfa881073b77cb3c85a400fe215bd518",
				"26d0935bb7ffd5e1f69ecc89608706dfda254c8e9e217f8ccf171d6f3078e0d1",
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
				"e6201ef5c7fab656a2bb0615888d02f181fc7677dbeaaace249484bbdbf79106",
				"09e840ef4fefde5c634154ead19bbff9cfa881073b77cb3c85a400fe215bd518",
				"26d0935bb7ffd5e1f69ecc89608706dfda254c8e9e217f8ccf171d6f3078e0d1",
				"c8dcad366cc9096323cf121a96ee9ae0437840e8e324cb3c82e457fef8fd9a0f",
				"ac12aaca9dc32e6f46dbe8a81c7320d9183c47afc03864a06aab26162e3d839e",
				"92ece401b53237b1cb1a1a76f4b6c5a8985786382640c449926478a0c906c367",
				"504aa65388888b60fc488199c324b1055d6193dc33f7a93aee592e77345f062a",
				"8a2e7b51565c0ae062ec5289398fcd9c36e40b6fe7da39051fb2b94d0da06c89",
			},
			Plan: "inferred",
		},
	},
}
