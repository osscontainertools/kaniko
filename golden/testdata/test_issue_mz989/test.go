package testissuemz989

import "github.com/osscontainertools/kaniko/golden/types"

// mz989 accounts for every layer the push sends, as a cross-repo mount or an upload.
// A mount costs no bytes, so an UPLOAD line for a blob the destination registry already
// holds is waste. The plans below record where kaniko wastes it today: a base read back
// from the shared store, and a missed layer that goes to the cache repo and then goes up
// a second time inside the image.
var Tests = types.GoldenTests{
	Name:       "test_issue_mz989",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			// The base still knows where it came from, so its layers mount.
			Args: []string{"-d", "example.com/img:latest"},
			Plan: "plain",
		},
		{
			// The mount decision is made at push time, so the plan does not move.
			Args: []string{"-d", "example.com/img:latest"},
			Env:  map[string]string{"FF_KANIKO_CROSS_REPO_MOUNT": "1"},
			Plan: "plain",
		},
		{
			// The store hands the push a local copy, and the base uploads instead.
			Args: []string{"-d", "example.com/img:latest"},
			Env:  map[string]string{"FF_KANIKO_SHARED_BASE_CACHE": "1"},
			Plan: "stored",
		},
		{
			// Every missed layer reaches example.com/cache before the image push
			// sends the same blob to example.com/img.
			Args: []string{"-d", "example.com/img:latest", "--cache", "--cache-repo", "example.com/cache"},
			Env:  map[string]string{"FF_KANIKO_CACHE_LOOKAHEAD": "1", "FF_KANIKO_CROSS_REPO_MOUNT": "1"},
			Plan: "cache_miss",
		},
		{
			// Both layers are read out of example.com/cache and then uploaded to
			// example.com/img anyway.
			Args: []string{"-d", "example.com/img:latest", "--cache", "--cache-repo", "example.com/cache"},
			Env:  map[string]string{"FF_KANIKO_CACHE_LOOKAHEAD": "1", "FF_KANIKO_CROSS_REPO_MOUNT": "1"},
			CachedKeys: []string{
				"9960b0560d3e4212d47329ac9e3379b8891474e43756b7650ae3bc18092b62f7",
				"c4d1d053ed51898d12bc0fe84e96f70fc77ad862f9e38b677cf97c1cddd78784",
			},
			Plan: "cache_hit",
		},
		{
			// --single-snapshot builds one layer for the whole stage, so only the last
			// command reaches the cache repo or the push.
			Args: []string{"-d", "example.com/img:latest", "--cache", "--cache-repo", "example.com/cache", "--single-snapshot"},
			Env:  map[string]string{"FF_KANIKO_CACHE_LOOKAHEAD": "1", "FF_KANIKO_CROSS_REPO_MOUNT": "1"},
			Plan: "single_snapshot",
		},
		{
			// Baseline. The cache entry is zstd against a gzip image, so
			// convertLayerMediaType recompresses it and the layer that gets pushed is
			// not the blob the cache repo holds. These uploads must stay uploads.
			Args: []string{"-d", "example.com/img:latest", "--cache", "--cache-repo", "example.com/cache", "--compression", "zstd"},
			Env:  map[string]string{"FF_KANIKO_CACHE_LOOKAHEAD": "1", "FF_KANIKO_CROSS_REPO_MOUNT": "1"},
			CachedKeys: []string{
				"9960b0560d3e4212d47329ac9e3379b8891474e43756b7650ae3bc18092b62f7",
				"c4d1d053ed51898d12bc0fe84e96f70fc77ad862f9e38b677cf97c1cddd78784",
			},
			CacheMediaType: "application/vnd.oci.image.layer.v1.tar+zstd",
			Plan:           "rekeyed",
		},
		{
			// A layer read out of the cache repo passes through unchanged, and
			// go-containerregistry mounts it without the flag.
			Args: []string{"-d", "example.com/img:latest", "--cache", "--cache-repo", "example.com/cache"},
			Env:  map[string]string{"FF_KANIKO_CACHE_LOOKAHEAD": "1"},
			CachedKeys: []string{
				"9960b0560d3e4212d47329ac9e3379b8891474e43756b7650ae3bc18092b62f7",
				"c4d1d053ed51898d12bc0fe84e96f70fc77ad862f9e38b677cf97c1cddd78784",
			},
			Plan: "cache_hit",
		},
		{
			// A layout cache has no repository to mount from.
			Args: []string{"-d", "example.com/img:latest", "--cache", "--cache-repo", "oci:/cache"},
			Env:  map[string]string{"FF_KANIKO_CACHE_LOOKAHEAD": "1", "FF_KANIKO_CROSS_REPO_MOUNT": "1"},
			CachedKeys: []string{
				"9960b0560d3e4212d47329ac9e3379b8891474e43756b7650ae3bc18092b62f7",
				"c4d1d053ed51898d12bc0fe84e96f70fc77ad862f9e38b677cf97c1cddd78784",
			},
			Plan: "layout_cache",
		},
		{
			// The docker gzip entries are relabeled into the oci image. The relabel keeps the
			// digest, so the flag still mounts them.
			Args: []string{"-d", "example.com/img:latest", "--cache", "--cache-repo", "example.com/cache", "--image-format", "oci"},
			Env:  map[string]string{"FF_KANIKO_CACHE_LOOKAHEAD": "1", "FF_KANIKO_CROSS_REPO_MOUNT": "1", "FF_KANIKO_SKIP_RELABEL_RECOMPRESS": "1"},
			CachedKeys: []string{
				"169858ec48524dcf8072fe7d9853fd2a1b885612e782f62135c7844f742ec463",
				"5ba430ac16bd5c0263ae55b68687c96a6fc2c0ebf3e232ea894707492381f906",
			},
			Plan: "relabeled_mount",
		},
		{
			// Without the flag the relabeled layer has lost its origin, so it uploads.
			Args: []string{"-d", "example.com/img:latest", "--cache", "--cache-repo", "example.com/cache", "--image-format", "oci"},
			Env:  map[string]string{"FF_KANIKO_CACHE_LOOKAHEAD": "1", "FF_KANIKO_SKIP_RELABEL_RECOMPRESS": "1"},
			CachedKeys: []string{
				"169858ec48524dcf8072fe7d9853fd2a1b885612e782f62135c7844f742ec463",
				"5ba430ac16bd5c0263ae55b68687c96a6fc2c0ebf3e232ea894707492381f906",
			},
			Plan: "relabeled",
		},
		{
			// Recompressing the relabel changes the digest, so the flag has nothing to mount.
			Args: []string{"-d", "example.com/img:latest", "--cache", "--cache-repo", "example.com/cache", "--image-format", "oci"},
			Env:  map[string]string{"FF_KANIKO_CACHE_LOOKAHEAD": "1", "FF_KANIKO_CROSS_REPO_MOUNT": "1"},
			CachedKeys: []string{
				"169858ec48524dcf8072fe7d9853fd2a1b885612e782f62135c7844f742ec463",
				"5ba430ac16bd5c0263ae55b68687c96a6fc2c0ebf3e232ea894707492381f906",
			},
			Plan: "relabeled",
		},
	},
}
