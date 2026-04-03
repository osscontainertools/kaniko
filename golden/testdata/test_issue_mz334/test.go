package testissuemz334

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_issue_mz334",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args:        []string{"--no-push", "--cache", "--cache-copy-layers"},
			KeySequence: []string{},
			Plan:        "plan",
		},
		{
			Args: []string{"--no-push", "--cache", "--cache-copy-layers"},
			KeySequence: []string{
				"6aa779a5de60b51cbd4da93da19836256506aa601b2d8504b4b04535400e28e3",
				"ef4c3eb7a0cf798f95fd39cd3a879d76b7c70e6e5f52e48d47a53131d89a006e",
				"13cf7ef4764f03137a8ba5cb33074a77c4977aabb88f7b3e01e87fc0b3c4e331",
			},
			Plan: "cached",
		},
		{
			Args: []string{"--no-push", "--cache", "--cache-copy-layers"},
			Env: map[string]string{
				"FF_KANIKO_INFER_CROSS_STAGE_CACHE_KEY": "1",
			},
			KeySequence: []string{
				"6aa779a5de60b51cbd4da93da19836256506aa601b2d8504b4b04535400e28e3",
				"ef4c3eb7a0cf798f95fd39cd3a879d76b7c70e6e5f52e48d47a53131d89a006e",
				"13cf7ef4764f03137a8ba5cb33074a77c4977aabb88f7b3e01e87fc0b3c4e331",
			},
			Plan: "cached",
		},
	},
}
