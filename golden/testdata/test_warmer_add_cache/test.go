/*
Copyright 2026 OSS Container Tools

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package testwarmeraddcache

import "github.com/osscontainertools/kaniko/golden/types"

var Tests = []types.GoldenTests{
	{
		Name:       "test_warmer_add_cache",
		Dockerfile: "Dockerfile",
		Tests: []types.GoldenTest{
			{
				Args: []string{"--add-cache"},
				Plan: "add_cache",
			},
			{
				Args: []string{},
				Plan: "without_add_cache",
			},
			{
				Args:          []string{"--add-cache"},
				CachedAddKeys: []string{"sha256-1111111111111111111111111111111111111111111111111111111111111111"},
				Plan:          "add_cache_hit",
			},
			{
				Args:      []string{"--add-cache", "--require-add-checksum"},
				WantError: "--require-add-checksum: ADD https://example.com/file.txt has no --checksum",
			},
		},
	},
	{
		Name:       "test_warmer_add_checksum",
		Dockerfile: "Dockerfile.checksum",
		Tests: []types.GoldenTest{
			{
				Args: []string{"--add-cache", "--require-add-checksum"},
				Plan: "add_cache_checksum",
			},
			{
				Args:          []string{"--add-cache"},
				CachedAddKeys: []string{"sha256-2222222222222222222222222222222222222222222222222222222222222222"},
				Plan:          "add_cache_checksum_hit",
			},
		},
	},
}
