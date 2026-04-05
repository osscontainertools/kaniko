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
				"a5d81b3f44d687d6d5747e1426182678d9de0955ec62dc9414b1595ac7135739",
				"d09be30ac411858dcedee559207040d83e889d7e9f84ed2321bed1a25526587b",
				"808f41990687790cdbc912f55d1f148f51767f1983e1f83f9d7401c51b528f8c",
			},
			Redirects: []string{
				"b335c526b2027ca13bffbd9a230579740eee25f795f3461fa4a2d210cf0bb7d1",
				"424e812a63f0638baa10274c3fa44cb6addb2990c0bc92c4a2c7f414c10cb9d4",
			},
			Plan: "cached_with_infer",
		},
	},
}
