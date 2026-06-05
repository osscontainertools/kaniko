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
	},
}
