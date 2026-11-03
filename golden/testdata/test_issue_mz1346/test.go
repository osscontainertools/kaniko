package testissuemz1346

import "github.com/osscontainertools/kaniko/golden/types"

// The first layer is cached, so each plan shows a hit and the cache uploads of the missed second layer.
var cached = []string{"8328b762de0f48ec6cc90c110378e61ce0db514e86ab810971769b55975eabef"}

var Tests = types.GoldenTests{
	Name:       "test_issue_mz1346",
	Dockerfile: "Dockerfile",
	Tests: []types.GoldenTest{
		{
			Args:       []string{"-d", "example.com/img:latest", "--cache"},
			CachedKeys: cached,
			Plan:       "fallback",
		},
		{
			Args:       []string{"-d", "example.com/img:latest", "--cache", "--cache-repo", "example.com/cache", "--no-push-cache"},
			CachedKeys: cached,
			Plan:       "no_push_cache",
		},
		{
			Args:       []string{"-d", "example.com/img:latest", "--cache-from", "example.com/main-cache", "--cache-to", "example.com/branch-cache"},
			CachedKeys: cached,
			Plan:       "from_to",
		},
		{
			Args:       []string{"-d", "example.com/img:latest", "--cache-from", "example.com/main-cache"},
			CachedKeys: cached,
			Plan:       "from_only",
		},
		{
			Args: []string{"-d", "example.com/img:latest", "--cache-to", "example.com/a-cache", "--cache-to", "example.com/b-cache"},
			Plan: "to_twice",
		},
		{
			Args:       []string{"-d", "example.com/img:latest", "--cache-from", "example.com/branch-cache", "--cache-from", "example.com/main-cache", "--cache-to", "example.com/branch-cache"},
			CachedKeys: cached,
			Plan:       "from_twice",
		},
	},
}
