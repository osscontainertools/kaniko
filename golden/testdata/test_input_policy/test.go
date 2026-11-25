package testinputpolicy

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = []types.GoldenTests{
	{
		Name:       "input_policy",
		Dockerfile: "Dockerfile",
		Tests: []types.GoldenTest{
			{
				Args: []string{"--no-push", "--context=dir://testdata/test_input_policy"},
				Env:  map[string]string{"FF_KANIKO_INPUT_POLICY": "1"},
				Plan: "allow_local",
			},
		},
	},
}
