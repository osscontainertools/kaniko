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

package util

import (
	"os"
	"strings"

	"github.com/sirupsen/logrus"
)

var disabledAssertions map[string]struct{}

func init() {
	val := os.Getenv("KANIKO_IGNORE_ASSERTIONS")
	if val == "" {
		return
	}
	disabledAssertions = make(map[string]struct{})
	for _, name := range strings.Split(val, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			disabledAssertions[name] = struct{}{}
		}
	}
}

// Assert panics with an "Assertion violated:" prefix when cond is false.
// name identifies this assertion; set KANIKO_IGNORE_ASSERTIONS=name to skip it.
// Use it to document and enforce invariants that must always hold.
func Assert(name string, cond bool, format string, args ...any) {
	if !cond {
		if _, disabled := disabledAssertions[name]; disabled {
			logrus.Warnf("Assertion disabled ["+name+"]: "+format, args...)
			return
		}
		logrus.Panicf("Assertion violated ["+name+"]: "+format, args...)
	}
}

// Unreachable panics with an "Unreachable Code:" prefix.
// Use it to mark code paths that must never execute.
func Unreachable(format string, args ...any) {
	logrus.Panicf("Unreachable Code: "+format, args...)
}
