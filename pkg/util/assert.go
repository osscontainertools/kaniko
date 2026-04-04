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

package util

import "github.com/sirupsen/logrus"

// Assert panics with an "Assertion violated:" prefix when cond is false.
// Use it to document and enforce invariants that must always hold.
func Assert(cond bool, format string, args ...any) {
	if !cond {
		logrus.Panicf("Assertion violated: "+format, args...)
	}
}

// Unreachable panics with an "Unreachable Code:" prefix.
// Use it to mark code paths that must never execute.
func Unreachable(format string, args ...any) {
	logrus.Panicf("Unreachable Code: "+format, args...)
}
