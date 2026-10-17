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

// Renders the feature flag profiles from pkg/config into README.md.
package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/osscontainertools/kaniko/pkg/config"
)

const readme = "README.md"

func main() {
	content, err := os.ReadFile(readme)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out := string(content)
	for name, flags := range config.Profiles {
		start := fmt.Sprintf("<!-- START profile %s generated, re-run `go run ./hack/profiles` to update -->\n", name)
		end := fmt.Sprintf("<!-- END profile %s -->", name)
		block := regexp.MustCompile("(?s)" + regexp.QuoteMeta(start) + ".*?" + regexp.QuoteMeta(end))
		if !block.MatchString(out) {
			fmt.Fprintf(os.Stderr, "%s has no markers for profile %s\n", readme, name)
			os.Exit(1)
		}
		rendered := start + "```sh\n" + strings.Join(flags, "=true\n") + "=true\n```\n" + end
		out = block.ReplaceAllLiteralString(out, rendered)
	}
	err = os.WriteFile(readme, []byte(out), 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
