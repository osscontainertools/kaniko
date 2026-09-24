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

package hint

import (
	"fmt"
	"os"
	"strings"

	"github.com/osscontainertools/kaniko/pkg/timing"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
)

type Rule string

const (
	SnapshotCacheDir Rule = "SnapshotCacheDir"
	SnapshotVCSDir   Rule = "SnapshotVCSDir"
)

var ignoredHints map[Rule]struct{}

func init() {
	val := os.Getenv("KANIKO_IGNORE_HINTS")
	if val == "" {
		return
	}
	ignoredHints = make(map[Rule]struct{})
	for name := range strings.SplitSeq(val, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			ignoredHints[Rule(name)] = struct{}{}
		}
	}
}

func Report(rule Rule, format string, args ...any) {
	if _, ignored := ignoredHints[rule]; ignored {
		return
	}
	msg := fmt.Sprintf(format, args...)
	logrus.Infof("HINT %s: %s", rule, msg)
	timing.AddEvent("kaniko.hint",
		attribute.String("kaniko.hint.rule", string(rule)),
		attribute.String("kaniko.hint.message", msg),
	)
}
