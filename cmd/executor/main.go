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

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/osscontainertools/kaniko/cmd/executor/cmd"
	"github.com/osscontainertools/kaniko/pkg/creds"
	"github.com/osscontainertools/kaniko/pkg/metrics"
)

func main() {
	helper, ok := creds.Helpers[filepath.Base(os.Args[0])]
	if ok {
		os.Exit(helper())
	}

	cmd.StartMetrics()
	defer finishMetricsOnPanic()

	if err := cmd.RootCmd.Execute(); err != nil {
		metrics.Finish(err, 1)
		os.Exit(1)
	}
	metrics.Finish(nil, 0)
}

// finishMetricsOnPanic writes the metrics of a build that panicked on the main
// goroutine, then lets the panic go on. Without metrics it does not recover.
func finishMetricsOnPanic() {
	if !metrics.Active() {
		return
	}
	if r := recover(); r != nil {
		metrics.Finish(fmt.Errorf("panic: %v", r), 2)
		panic(r)
	}
}
