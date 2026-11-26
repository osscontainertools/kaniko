package testwarmerrunmountbind

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_warmer_run_mount_bind",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{},
			Env:  map[string]string{"FF_KANIKO_CACHE_RUN_MOUNT_BIND_FROM": "0"},
			Plan: "from_only",
		},
		{
			Args: []string{},
			Env:  map[string]string{"FF_KANIKO_CACHE_RUN_MOUNT_BIND_FROM": "1"},
			Plan: "run_mount_bind_from",
		},
	},
}
