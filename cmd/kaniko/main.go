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

// Package main is a single kaniko binary that changes its behaviour based on
// the name it was invoked as (busybox style). Create a hardlink or symlink
// named `executor` to run a build, or `warmer` to warm the layer cache.
// Invoked as anything else (including `kaniko`) it prints usage and exits.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	executorcmd "github.com/osscontainertools/kaniko/cmd/executor/cmd"
	warmercmd "github.com/osscontainertools/kaniko/cmd/warmer/cmd"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/creds"
	"github.com/osscontainertools/kaniko/pkg/util"
)

func main() {
	name := filepath.Base(os.Args[0])

	helper, ok := creds.Helpers[name]
	if ok {
		os.Exit(helper())
	}

	if config.FF.NativeInit && os.Getpid() == 1 {
		code, err := util.RunAsInit()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(code)
	}

	switch name {
	case "executor":
		if err := executorcmd.RootCmd.Execute(); err != nil {
			os.Exit(1)
		}
	case "warmer":
		if err := warmercmd.RootCmd.Execute(); err != nil {
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "kaniko: %q is not a recognized mode\n", name)
		fmt.Fprintln(os.Stderr, "Invoke this binary as `executor` or `warmer` (for example via a hardlink or symlink) to select the mode.")
		os.Exit(2)
	}
}
