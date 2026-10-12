package testwarmer

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_warmer",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{},
			Plan: "default",
		},
		{
			Args: []string{"--build-arg=BASE=ubuntu:24.04"},
			// TODO: the Dockerfile default of BASE overrides --build-arg
			Plan: "build_arg",
		},
		{
			Args: []string{"--image=busybox:1.36"},
			Plan: "image",
		},
		{
			Args:       []string{},
			CachedKeys: []string{"sha256:bb29a696e6cf59b7403f78467ee6afea1fb6d8011e683ab55bcd4a1301186f63"},
			Plan:       "cached",
		},
		{
			Args:       []string{"--force"},
			CachedKeys: []string{"sha256:bb29a696e6cf59b7403f78467ee6afea1fb6d8011e683ab55bcd4a1301186f63"},
			Plan:       "force",
		},
		{
			Args:       []string{"--image=busybox@sha256:0000000000000000000000000000000000000000000000000000000000000001"},
			CachedKeys: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000001"},
			Plan:       "digest",
		},
	},
}
