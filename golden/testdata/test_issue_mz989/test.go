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
			Env:  map[string]string{"FF_KANIKO_SHARED_BASE_CACHE": "0"},
			Plan: "plain",
		},
		{
			// The store hands the push a local copy, and the base uploads instead.
			Args: []string{"-d", "example.com/img:latest"},
			Plan: "stored",
		},
		{
			// Every missed layer reaches example.com/cache before the image push
			// sends the same blob to example.com/img.
			Args: []string{"-d", "example.com/img:latest", "--cache", "--cache-repo", "example.com/cache"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD":   "1",
				"FF_KANIKO_SHARED_BASE_CACHE": "0",
			},
			Plan: "cache_miss",
		},
		{
			// Both layers are read out of example.com/cache and then uploaded to
			// example.com/img anyway.
			Args: []string{"-d", "example.com/img:latest", "--cache", "--cache-repo", "example.com/cache"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD":   "1",
				"FF_KANIKO_SHARED_BASE_CACHE": "0",
			},
			CachedKeys: []string{
				"3da159ee0182e9c6251bd02915edc2b5be6f4f22a8119fcd25a71fb61b9fbcd1",
				"62e7ee4103dd49492957774b4a6fedd82a693d8b89e3e67285bd6d3a98108b89",
			},
			Plan: "cache_hit",
		},
		{
			// --single-snapshot builds one layer for the whole stage, so only the last
			// command reaches the cache repo or the push.
			Args: []string{"-d", "example.com/img:latest", "--cache", "--cache-repo", "example.com/cache", "--single-snapshot"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD":   "1",
				"FF_KANIKO_SHARED_BASE_CACHE": "0",
			},
			Plan: "single_snapshot",
		},
		{
			// Baseline. The cache entry is zstd against a gzip image, so
			// convertLayerMediaType recompresses it and the layer that gets pushed is
			// not the blob the cache repo holds. These uploads must stay uploads.
			Args: []string{"-d", "example.com/img:latest", "--cache", "--cache-repo", "example.com/cache", "--compression", "zstd"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD":   "1",
				"FF_KANIKO_SHARED_BASE_CACHE": "0",
			},
			CachedKeys: []string{
				"3da159ee0182e9c6251bd02915edc2b5be6f4f22a8119fcd25a71fb61b9fbcd1",
				"62e7ee4103dd49492957774b4a6fedd82a693d8b89e3e67285bd6d3a98108b89",
			},
			CacheMediaType: "application/vnd.oci.image.layer.v1.tar+zstd",
			Plan:           "rekeyed",
		},
		{
			// A layer read out of the cache repo passes through unchanged, and
			// go-containerregistry mounts it without the flag.
			Args: []string{"-d", "example.com/img:latest", "--cache", "--cache-repo", "example.com/cache"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD":   "1",
				"FF_KANIKO_CROSS_REPO_MOUNT":  "0",
				"FF_KANIKO_SHARED_BASE_CACHE": "0",
			},
			CachedKeys: []string{
				"3da159ee0182e9c6251bd02915edc2b5be6f4f22a8119fcd25a71fb61b9fbcd1",
				"62e7ee4103dd49492957774b4a6fedd82a693d8b89e3e67285bd6d3a98108b89",
			},
			Plan: "cache_hit",
		},
		{
			// A layout cache has no repository to mount from.
			Args: []string{"-d", "example.com/img:latest", "--cache", "--cache-repo", "oci:/cache"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD":   "1",
				"FF_KANIKO_CROSS_REPO_MOUNT":  "1",
				"FF_KANIKO_SHARED_BASE_CACHE": "0",
			},
			CachedKeys: []string{
				"3da159ee0182e9c6251bd02915edc2b5be6f4f22a8119fcd25a71fb61b9fbcd1",
				"62e7ee4103dd49492957774b4a6fedd82a693d8b89e3e67285bd6d3a98108b89",
			},
			Plan: "layout_cache",
		},
		{
			// The docker gzip entries are relabeled into the oci image. The relabel keeps the
			// digest, so the flag still mounts them.
			Args: []string{"-d", "example.com/img:latest", "--cache", "--cache-repo", "example.com/cache", "--image-format", "oci"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD":         "1",
				"FF_KANIKO_CROSS_REPO_MOUNT":        "1",
				"FF_KANIKO_SHARED_BASE_CACHE":       "0",
				"FF_KANIKO_SKIP_RELABEL_RECOMPRESS": "1",
			},
			CachedKeys: []string{
				"8328b762de0f48ec6cc90c110378e61ce0db514e86ab810971769b55975eabef",
				"583d4732963f7897a17b5d92572eea389ce2b85cc21431f1df1999457e1cbe70",
			},
			Plan: "relabeled_mount",
		},
		{
			// The relabeled layer keeps its origin, so it mounts without the flag.
			Args: []string{"-d", "example.com/img:latest", "--cache", "--cache-repo", "example.com/cache", "--image-format", "oci"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD":         "1",
				"FF_KANIKO_CROSS_REPO_MOUNT":        "0",
				"FF_KANIKO_SHARED_BASE_CACHE":       "0",
				"FF_KANIKO_SKIP_RELABEL_RECOMPRESS": "1",
			},
			CachedKeys: []string{
				"8328b762de0f48ec6cc90c110378e61ce0db514e86ab810971769b55975eabef",
				"583d4732963f7897a17b5d92572eea389ce2b85cc21431f1df1999457e1cbe70",
			},
			Plan: "relabeled_mount",
		},
		{
			// Recompressing the relabel changes the digest, so the flag has nothing to mount.
			Args: []string{"-d", "example.com/img:latest", "--cache", "--cache-repo", "example.com/cache", "--image-format", "oci"},
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD":         "1",
				"FF_KANIKO_CROSS_REPO_MOUNT":        "1",
				"FF_KANIKO_SHARED_BASE_CACHE":       "0",
				"FF_KANIKO_SKIP_RELABEL_RECOMPRESS": "0",
			},
			CachedKeys: []string{
				"8328b762de0f48ec6cc90c110378e61ce0db514e86ab810971769b55975eabef",
				"583d4732963f7897a17b5d92572eea389ce2b85cc21431f1df1999457e1cbe70",
			},
			Plan: "relabeled",
		},
	},
}
