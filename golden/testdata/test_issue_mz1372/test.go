package testissuemz1372

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = []types.GoldenTests{
	{
		Name:       "copy",
		Dockerfile: "Dockerfile.copy",
		Tests: []types.GoldenTest{
			{Args: []string{"--no-push", "--customPlatform=linux/amd64"}, Plan: "copy"},
		},
	},
	{
		Name:       "mount",
		Dockerfile: "Dockerfile.mount",
		Tests: []types.GoldenTest{
			{
				Args: []string{"--no-push", "--customPlatform=linux/amd64"},
				Env:  map[string]string{"FF_KANIKO_RUN_CROSS_STAGE_BIND_MOUNT": "1"},
				Plan: "mount",
			},
		},
	},
}
