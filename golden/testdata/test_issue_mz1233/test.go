package testissuemz1233

import "github.com/osscontainertools/kaniko/golden/types"

// Dependency paths must use the same instruction-order precedence as the build.
// Reverse-order cases preserve existing Kaniko behavior; they do not assert Docker parity.
// All bases are scratch so these planning regressions do not pull images.
var Tests = []types.GoldenTests{
	{
		Name:       "env_after_arg",
		Dockerfile: "Dockerfile.env_after_arg",
		Tests: []types.GoldenTest{
			{Args: []string{"--no-push", "--context=."}, Plan: "env_after_arg"},
			{Args: []string{"--no-push", "--context=.", "--build-arg=FOO=cli"}, Plan: "env_after_arg"},
		},
	},
	{
		Name:       "arg_after_env",
		Dockerfile: "Dockerfile.arg_after_env",
		Tests: []types.GoldenTest{
			{Args: []string{"--no-push", "--context=."}, Plan: "arg_after_env"},
		},
	},
	{
		Name:       "env_expansion",
		Dockerfile: "Dockerfile.env_expansion",
		Tests: []types.GoldenTest{
			{Args: []string{"--no-push", "--context=."}, Plan: "env_expansion"},
		},
	},
	{
		Name:       "arg_reintroduced",
		Dockerfile: "Dockerfile.arg_reintroduced",
		Tests: []types.GoldenTest{
			{Args: []string{"--no-push", "--context=."}, Plan: "arg_reintroduced"},
		},
	},
	{
		Name:       "env_same_instruction",
		Dockerfile: "Dockerfile.env_same_instruction",
		Tests: []types.GoldenTest{
			{Args: []string{"--no-push", "--context=."}, Plan: "env_same_instruction"},
		},
	},
}
