package testunittests

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = []types.GoldenTests{
	{
		Name:       "test_unittests_without_copyfrom",
		Dockerfile: "Dockerfile.wo_copyfrom",
		Tests: []types.GoldenTest{
			{
				Args: []string{"--no-push", "--target=base-dev"},
				Env: map[string]string{
					"FF_KANIKO_SHARED_BASE_CACHE": "0",
				},
				Plan: "wo_copyfrom_dev",
			},
			{
				Args: []string{"--no-push", "--target=base-prod"},
				Env: map[string]string{
					"FF_KANIKO_SHARED_BASE_CACHE": "0",
				},
				Plan: "wo_copyfrom_prod",
			},
			{
				Args: []string{"--no-push"},
				Env: map[string]string{
					"FF_KANIKO_SHARED_BASE_CACHE": "0",
				},
				Plan: "wo_copyfrom_final",
			},
		},
	},
	{
		Name:       "test_unittests_with_copyfrom",
		Dockerfile: "Dockerfile.copyfrom",
		Tests: []types.GoldenTest{
			{
				Args: []string{"--no-push", "--target=base-dev"},
				Env: map[string]string{
					"FF_KANIKO_SHARED_BASE_CACHE": "0",
				},
				Plan: "wo_copyfrom_dev",
			},
			{
				Args: []string{"--no-push", "--target=base-prod"},
				Env: map[string]string{
					"FF_KANIKO_SHARED_BASE_CACHE": "0",
				},
				Plan: "wo_copyfrom_prod",
			},
			{
				Args: []string{"--no-push"},
				Env: map[string]string{
					"FF_KANIKO_SHARED_BASE_CACHE": "0",
				},
				Plan: "copyfrom_final",
			},
		},
	},
	{
		Name:       "test_unittests_with_two_copyfrom",
		Dockerfile: "Dockerfile.two_copyfrom",
		Tests: []types.GoldenTest{
			{
				Args: []string{"--no-push", "--target=base-dev"},
				Env: map[string]string{
					"FF_KANIKO_SHARED_BASE_CACHE": "0",
				},
				Plan: "wo_copyfrom_dev",
			},
			{
				Args: []string{"--no-push", "--target=base-prod"},
				Env: map[string]string{
					"FF_KANIKO_SHARED_BASE_CACHE": "0",
				},
				Plan: "wo_copyfrom_prod",
			},
			{
				Args: []string{"--no-push"},
				Env: map[string]string{
					"FF_KANIKO_SHARED_BASE_CACHE": "0",
				},
				Plan: "two_copyfrom_final",
			},
		},
	},
	{
		Name:       "test_unittests_with_two_copyfrom_and_arg",
		Dockerfile: "Dockerfile.two_copyfrom_and_arg",
		Tests: []types.GoldenTest{
			{
				Args: []string{"--no-push", "--target=base"},
				Env: map[string]string{
					"FF_KANIKO_SHARED_BASE_CACHE": "0",
				},
				Plan: "two_copyfrom_and_arg_base",
			},
			{
				Args: []string{"--no-push"},
				Env: map[string]string{
					"FF_KANIKO_SHARED_BASE_CACHE": "0",
				},
				Plan: "two_copyfrom_and_arg_final",
			},
		},
	},
	{
		Name:       "test_unittests_final_without_dependencies",
		Dockerfile: "Dockerfile.final_wo_deps",
		Tests: []types.GoldenTest{
			{
				Args: []string{"--no-push", "--target=final"},
				Env: map[string]string{
					"FF_KANIKO_SHARED_BASE_CACHE": "0",
				},
				Plan: "final_wo_deps_final",
			},
			{
				Args: []string{"--no-push", "--target=buzz"},
				Env: map[string]string{
					"FF_KANIKO_SHARED_BASE_CACHE": "0",
				},
				Plan: "final_wo_deps_buzz",
			},
			{
				Args: []string{"--no-push", "--target=fizz"},
				Env: map[string]string{
					"FF_KANIKO_SHARED_BASE_CACHE": "0",
				},
				Plan: "final_wo_deps_fizz",
			},
			{
				Args: []string{"--no-push"},
				Env: map[string]string{
					"FF_KANIKO_SHARED_BASE_CACHE": "0",
				},
				Plan: "final_wo_deps_final",
			},
		},
	},
	{
		Name:       "test_unittests_multiple_copy",
		Dockerfile: "Dockerfile.multiple_copy",
		Tests: []types.GoldenTest{
			// TODO: if we overwrite the target of a COPY later-on
			// There is no need to run the command twice.
			{
				Args: []string{"--no-push"},
				Env: map[string]string{
					"FF_KANIKO_SHARED_BASE_CACHE": "0",
				},
				Plan: "multiple_copy",
			},
		},
	},
	{
		Name:       "test_unittests_alias",
		Dockerfile: "Dockerfile.alias",
		Tests: []types.GoldenTest{
			// TODO: alias stages get fully unrolled instead of inlined.
			{
				Args: []string{"--no-push"},
				Env: map[string]string{
					"FF_KANIKO_SHARED_BASE_CACHE": "0",
				},
				Plan: "alias",
			},
		},
	},
	{
		Name:       "test_unittests_global_arg",
		Dockerfile: "Dockerfile.global_arg",
		Tests: []types.GoldenTest{
			{
				Args: []string{"--no-push"},
				Env: map[string]string{
					"FF_KANIKO_SHARED_BASE_CACHE": "0",
				},
				Plan: "global_arg",
			},
			{
				Args: []string{"--no-push", "--target=stage1"},
				Env: map[string]string{
					"FF_KANIKO_SHARED_BASE_CACHE": "0",
				},
				Plan: "global_arg_stage1",
			},
		},
	},
}
