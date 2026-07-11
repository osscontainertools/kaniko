/*
Copyright 2018 Google LLC

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

package snapshot

import (
	"testing"
)

func Test_FlattenPaths(t *testing.T) {
	layers := []map[string]string{
		{
			"a": "2",
			"b": "3",
		},
		{
			"b": "5",
			"c": "6",
		},
		{
			"a": "8",
		},
	}

	whiteouts := []map[string]struct{}{
		{
			"a": {}, // delete a
		},
		{
			"b": {}, // delete b
		},
		{
			"c": {}, // delete c
		},
	}

	lm := LayeredMap{
		adds:    []map[string]string{layers[0]},
		deletes: []map[string]struct{}{whiteouts[0]},
	}

	paths := lm.GetCurrentPaths()

	assertPath := func(f string, exists bool) {
		_, ok := paths[f]
		if exists && !ok {
			t.Fatalf("expected path '%s' to be present.", f)
		} else if !exists && ok {
			t.Fatalf("expected path '%s' not to be present.", f)
		}
	}

	assertPath("a", false)
	assertPath("b", true)

	lm = LayeredMap{
		adds:    []map[string]string{layers[0], layers[1]},
		deletes: []map[string]struct{}{whiteouts[0], whiteouts[1]},
	}
	paths = lm.GetCurrentPaths()

	assertPath("a", false)
	assertPath("b", false)
	assertPath("c", true)

	lm = LayeredMap{
		adds:    []map[string]string{layers[0], layers[1], layers[2]},
		deletes: []map[string]struct{}{whiteouts[0], whiteouts[1], whiteouts[2]},
	}
	paths = lm.GetCurrentPaths()

	assertPath("a", true)
	assertPath("b", false)
	assertPath("c", false)
}
