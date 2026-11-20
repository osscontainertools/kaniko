package testissuemz1427

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_issue_mz1427",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{"--destination=registry.example.com/team/app:1"},
			Plan: "stream",
		},
		{
			Args: []string{"--destination=registry.example.com/team/base:2"},
			Plan: "stream_same_repo",
		},
		{
			Args: []string{"--no-push", "--destination=other.example.com/team/app:1"},
			Plan: "stream_no_push",
		},
		{
			Args: []string{"--build-arg=BASE=busybox:1", "--destination=docker.io/someone/app:1", "--registry-mirror=mirror.example.com"},
			Plan: "fetch_mirror",
		},
		{
			Args: []string{"--destination=other.example.com/team/app:1"},
			Plan: "fetch_other_registry",
		},
		{
			Args: []string{"--destination=registry.example.com/team/app:1", "--destination=other.example.com/team/app:1"},
			Plan: "fetch_mixed",
		},
		{
			Args: []string{"--build-arg=BASE=registry.example.com:5000/team/base:1", "--destination=registry.example.com/team/app:1"},
			Plan: "fetch_port",
		},
		{
			Args: []string{"--destination=registry.example.com/team/app:1", "--tar-path=/workspace/image.tar"},
			Plan: "fetch_tar",
		},
		{
			Args: []string{"--no-push", "--oci-layout-path=/workspace/layout"},
			Plan: "fetch_oci_layout",
		},
		{
			Args: []string{"--destination=registry.example.com/team/app:1", "--reproducible"},
			Plan: "fetch_reproducible",
		},
		{
			Args: []string{"--destination=registry.example.com/team/app:1", "--reproducible"},
			Env:  map[string]string{"FF_KANIKO_REPRODUCIBLE_PRESERVE_BASE_LAYERS": "0"},
			Plan: "fetch_reproducible",
		},
		{
			Args: []string{"--no-push", "--reproducible"},
			Plan: "fetch_reproducible_no_push",
		},
		{
			Args: []string{"--destination=registry.example.com/team/app:1", "--registry-map=registry.example.com=mirror.example.com"},
			Plan: "fetch_registry_map",
		},
		{
			Args: []string{"--destination=registry.example.com/team/app:1"},
			Env:  map[string]string{"FF_KANIKO_CROSS_REPO_MOUNT": "0"},
			Plan: "stream",
		},
		{
			Args: []string{"--destination=other.example.com/team/app:1"},
			Env:  map[string]string{"FF_KANIKO_CROSS_REPO_MOUNT": "0"},
			Plan: "fetch_other_registry",
		},
		{
			Args: []string{"--destination=registry.example.com/team/app:1", "--image-format=oci"},
			Env:  map[string]string{"FF_KANIKO_CROSS_REPO_MOUNT": "0"},
			Plan: "stream_image_format",
		},
	},
}
