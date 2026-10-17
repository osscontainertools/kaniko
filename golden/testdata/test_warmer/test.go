package testwarmer

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_warmer",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Env:  map[string]string{"FF_KANIKO_CACHE_IMAGE_INDEX": "0"},
			Args: []string{},
			Plan: "default",
		},
		{
			Env:  map[string]string{"FF_KANIKO_CACHE_IMAGE_INDEX": "0"},
			Args: []string{"--build-arg=BASE=ubuntu:24.04"},
			// TODO: the Dockerfile default of BASE overrides --build-arg
			Plan: "build_arg",
		},
		{
			Env:  map[string]string{"FF_KANIKO_CACHE_IMAGE_INDEX": "0"},
			Args: []string{"--image=busybox:1.36"},
			Plan: "image",
		},
		{
			Env:        map[string]string{"FF_KANIKO_CACHE_IMAGE_INDEX": "0"},
			Args:       []string{},
			CachedKeys: []string{"sha256:bb29a696e6cf59b7403f78467ee6afea1fb6d8011e683ab55bcd4a1301186f63"},
			Plan:       "cached",
		},
		{
			Env:        map[string]string{"FF_KANIKO_CACHE_IMAGE_INDEX": "0"},
			Args:       []string{"--force"},
			CachedKeys: []string{"sha256:bb29a696e6cf59b7403f78467ee6afea1fb6d8011e683ab55bcd4a1301186f63"},
			Plan:       "force",
		},
		{
			Env:        map[string]string{"FF_KANIKO_CACHE_IMAGE_INDEX": "0"},
			Args:       []string{"--image=busybox@sha256:0000000000000000000000000000000000000000000000000000000000000001"},
			CachedKeys: []string{"sha256:0000000000000000000000000000000000000000000000000000000000000001"},
			Plan:       "digest",
		},
		{
			Args:       []string{},
			CachedKeys: []string{"sha256:bb29a696e6cf59b7403f78467ee6afea1fb6d8011e683ab55bcd4a1301186f63"},
			Plan:       "index",
		},
	},
}
