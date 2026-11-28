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

// +gobra

package util

import (
	"fmt"
	"path/filepath"
	"strings"

	securejoin "github.com/cyphar/filepath-securejoin"
)

// confinedEntry resolves the parent of a tar entry name under root and appends the last element
// lexically, so a symlink at that name is replaced and not written through.
// @ requires filepath.IsClean(name)
// @ ensures err == nil ==> filepath.Within(root, path)
// @ ensures err == nil ==> filepath.Resolved(root, path) || filepath.Resolved(root, filepath.Dir(path))
// @ decreases
func confinedEntry(root, name string) (path string, err error) {
	if name == ".." || strings.HasPrefix(name, "../") {
		return "", fmt.Errorf("tar entry %q is not allowed: references parent directory", name)
	}
	dir, err := securejoin.SecureJoin(root, filepath.Dir(name))
	if err != nil {
		return "", fmt.Errorf("resolving path for %q: %w", name, err)
	}
	return filepath.Join(dir, filepath.Base(name)), nil
}
