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
			CachedKeys: []string{
				"3c8511b0336f6b6e23af2df0a22a040c8ce0db5fd79d5476395bf55b47f760c6",
				"0ec71de61bfde2d75e8bcea855bbc8c9eeb7aa46fccbd83d90f69ec00f74af4d",
				"3a267e9eab738aef490c14130100de22f72cb59f27d1944ea7a156b04243d00c",
			},
			Plan: "cached",
		},
		{
			Args: []string{"--no-push", "--cache", "--cache-copy-layers"},
			CachedKeys: []string{
				"3c8511b0336f6b6e23af2df0a22a040c8ce0db5fd79d5476395bf55b47f760c6",
				"0ec71de61bfde2d75e8bcea855bbc8c9eeb7aa46fccbd83d90f69ec00f74af4d",
				"3a267e9eab738aef490c14130100de22f72cb59f27d1944ea7a156b04243d00c",
				"08cbb69fc33c0f8e866dca205f31f87d7b64e3a558ff4025b90c422ded171f1d",
				"b375d5467ac3cf6c5cb13a8c217a458ed6a96fa44eacae3c4f5f729baaa21ddf",
				"27a86e48f43d8dbd810dd7b08f6e9f9353f0f209de02d6c2bd22c00cd33edeb4",
			},
			Plan: "eliminated",
		},
	},
}
