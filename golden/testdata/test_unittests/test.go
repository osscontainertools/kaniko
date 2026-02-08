package testunittests

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = []types.GoldenTests{
	{
		Name:       "test_unittests_without_copyfrom",
		Dockerfile: "Dockerfile.wo_copyfrom",
		Tests: []types.GoldenTest{
			{
				Args: []string{"--no-push", "--target=base-dev"},
				Plan: "wo_copyfrom_dev.txt",
			},
			{
				Args: []string{"--no-push", "--target=base-dev"},
				Env: map[string]string{
					"FF_KANIKO_SQUASH_STAGES": "0",
				},
				Plan: "wo_copyfrom_dev.txt",
			},
			{
				Args: []string{"--no-push", "--target=base-prod"},
				Plan: "wo_copyfrom_prod.txt",
			},
			{
				Args: []string{"--no-push", "--target=base-prod"},
				Env: map[string]string{
					"FF_KANIKO_SQUASH_STAGES": "0",
				},
				Plan: "wo_copyfrom_prod.txt",
			},
			{
				Args: []string{"--no-push"},
				Plan: "wo_copyfrom_final.txt",
			},
			{
				Args: []string{"--no-push"},
				Env: map[string]string{
					"FF_KANIKO_SQUASH_STAGES": "0",
				},
				Plan: "wo_copyfrom_final_nosquash.txt",
			},
		},
	},
	{
		Name:       "test_unittests_with_copyfrom",
		Dockerfile: "Dockerfile.copyfrom",
		Tests: []types.GoldenTest{
			{
				Args: []string{"--no-push", "--target=base-dev"},
				Plan: "wo_copyfrom_dev.txt",
			},
			{
				Args: []string{"--no-push", "--target=base-prod"},
				Plan: "wo_copyfrom_prod.txt",
			},
			{
				Args: []string{"--no-push"},
				Plan: "copyfrom_final.txt",
			},
		},
	},
	{
		Name:       "test_unittests_with_two_copyfrom",
		Dockerfile: "Dockerfile.two_copyfrom",
		Tests: []types.GoldenTest{
			{
				Args: []string{"--no-push", "--target=base-dev"},
				Plan: "wo_copyfrom_dev.txt",
			},
			{
				Args: []string{"--no-push", "--target=base-prod"},
				Plan: "wo_copyfrom_prod.txt",
			},
			{
				Args: []string{"--no-push"},
				Plan: "two_copyfrom_final.txt",
			},
		},
	},
	{
		Name:       "test_unittests_with_two_copyfrom_and_arg",
		Dockerfile: "Dockerfile.two_copyfrom_and_arg",
		Tests: []types.GoldenTest{
			{
				Args: []string{"--no-push", "--target=base"},
				Plan: "two_copyfrom_and_arg_base.txt",
			},
			{
				Args: []string{"--no-push", "--target=base"},
				Env: map[string]string{
					"FF_KANIKO_SQUASH_STAGES": "0",
				},
				Plan: "two_copyfrom_and_arg_base.txt",
			},
			{
				Args: []string{"--no-push"},
				Plan: "two_copyfrom_and_arg_final.txt",
			},
			{
				Args: []string{"--no-push"},
				Env: map[string]string{
					"FF_KANIKO_SQUASH_STAGES": "0",
				},
				Plan: "two_copyfrom_and_arg_final_no_squash.txt",
			},
		},
	},
	{
		Name:       "test_unittests_final_without_dependencies",
		Dockerfile: "Dockerfile.final_wo_deps",
		Tests: []types.GoldenTest{
			{
				Args: []string{"--no-push", "--target=final"},
				Plan: "final_wo_deps_final.txt",
			},
			{
				Args: []string{"--no-push", "--target=buzz"},
				Plan: "final_wo_deps_buzz.txt",
			},
			{
				Args: []string{"--no-push", "--target=fizz"},
				Plan: "final_wo_deps_fizz.txt",
			},
			{
				Args: []string{"--no-push"},
				Plan: "final_wo_deps_final.txt",
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
				Plan: "multiple_copy.txt",
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
				Plan: "alias.txt",
			},
		},
	},
	{
		Name:       "test_unittests_global_arg",
		Dockerfile: "Dockerfile.global_arg",
		Tests: []types.GoldenTest{
			{
				Args: []string{"--no-push"},
				Plan: "global_arg.txt",
			},
			{
				Args: []string{"--no-push", "--target=stage1"},
				Plan: "global_arg_stage1.txt",
			},
		},
	},
}
