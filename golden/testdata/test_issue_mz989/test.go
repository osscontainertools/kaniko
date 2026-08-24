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
				"FF_KANIKO_RESOLVE_CACHE_KEY": "0",
				"FF_KANIKO_ROLLING_CACHE_KEY": "0",
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
				"FF_KANIKO_RESOLVE_CACHE_KEY": "0",
				"FF_KANIKO_ROLLING_CACHE_KEY": "0",
				"FF_KANIKO_SHARED_BASE_CACHE": "0",
			},
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
			Env: map[string]string{
				"FF_KANIKO_CACHE_LOOKAHEAD":   "1",
				"FF_KANIKO_RESOLVE_CACHE_KEY": "0",
				"FF_KANIKO_ROLLING_CACHE_KEY": "0",
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
				"FF_KANIKO_RESOLVE_CACHE_KEY": "0",
				"FF_KANIKO_ROLLING_CACHE_KEY": "0",
				"FF_KANIKO_SHARED_BASE_CACHE": "0",
			},
			CachedKeys: []string{
				"9960b0560d3e4212d47329ac9e3379b8891474e43756b7650ae3bc18092b62f7",
				"c4d1d053ed51898d12bc0fe84e96f70fc77ad862f9e38b677cf97c1cddd78784",
			},
			Plan: "rekeyed",
		},
	},
}
