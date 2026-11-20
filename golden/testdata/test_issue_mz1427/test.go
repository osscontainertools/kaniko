package testissuemz1427

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = types.GoldenTests{
	Name:       "test_issue_mz1427",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args: []string{"--destination=docker.io/someone/app:1"},
			Plan: "stream",
		},
		{
			Args: []string{"--destination=busybox:2"},
			Plan: "stream_same_repo",
		},
		{
			Args: []string{"--no-push", "--destination=other.example.com/team/app:1"},
			Plan: "stream_no_push",
		},
		{
			Args: []string{"--destination=other.example.com/team/app:1"},
			Plan: "fetch_other_registry",
		},
		{
			Args: []string{"--destination=docker.io/someone/app:1", "--destination=other.example.com/team/app:1"},
			Plan: "fetch_mixed",
		},
		{
			Args: []string{"--destination=docker.io/someone/app:1", "--tar-path=/workspace/image.tar"},
			Plan: "fetch_tar",
		},
		{
			Args: []string{"--no-push", "--oci-layout-path=/workspace/layout"},
			Plan: "fetch_oci_layout",
		},
		{
			Args: []string{"--destination=docker.io/someone/app:1", "--reproducible"},
			Plan: "fetch_reproducible",
		},
		{
			Args: []string{"--destination=docker.io/someone/app:1", "--reproducible"},
			Env:  map[string]string{"FF_KANIKO_REPRODUCIBLE_PRESERVE_BASE_LAYERS": "0"},
			Plan: "fetch_reproducible",
		},
		{
			Args: []string{"--no-push", "--reproducible"},
			Plan: "fetch_reproducible_no_push",
		},
		{
			Args: []string{"--destination=docker.io/someone/app:1", "--registry-mirror=mirror.gcr.io"},
			Plan: "fetch_mirror",
		},
		// Without the mount index only go-containerregistry mounts, from the reference the layer was pulled with.
		{
			Args: []string{"--destination=docker.io/someone/app:1"},
			Env:  map[string]string{"FF_KANIKO_CROSS_REPO_MOUNT": "0"},
			Plan: "stream_ggcr_mount",
		},
		{
			Args: []string{"--destination=other.example.com/team/app:1"},
			Env:  map[string]string{"FF_KANIKO_CROSS_REPO_MOUNT": "0"},
			Plan: "fetch_other_registry",
		},
		{
			Args: []string{"--destination=docker.io/someone/app:1", "--image-format=oci"},
			Env:  map[string]string{"FF_KANIKO_CROSS_REPO_MOUNT": "0"},
			Plan: "stream_image_format",
		},
	},
}
