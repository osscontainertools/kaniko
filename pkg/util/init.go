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
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// NativeInitEnv is set on the kaniko child of RunAsInit.
const NativeInitEnv = "KANIKO_NATIVE_INIT_CHILD"

// RunAsInit re-execs kaniko as its only child and acts as init: it reaps every orphan,
// forwards termination signals, and returns the child's exit code once it exits.
func RunAsInit() (int, error) {
	sigs := make(chan os.Signal, 16)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	exe, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("resolving kaniko executable: %w", err)
	}
	child, err := os.StartProcess(exe, os.Args, &os.ProcAttr{
		Env:   append(os.Environ(), NativeInitEnv+"=1"),
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
	})
	if err != nil {
		return 0, fmt.Errorf("starting kaniko as child of init: %w", err)
	}
	go func() {
		for sig := range sigs {
			// from PID 1, pid -1 reaches every process in the container except init itself
			_ = syscall.Kill(-1, sig.(syscall.Signal))
		}
	}()
	// init has no children besides kaniko and the orphans it inherits,
	// so waiting on any pid cannot steal an exit status someone else waits for
	for {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &status, 0, nil)
		if err != nil && err != syscall.EINTR {
			return 0, fmt.Errorf("waiting for kaniko: %w", err)
		}
		if pid == child.Pid {
			if status.Signaled() {
				return 128 + int(status.Signal()), nil
			}
			return status.ExitStatus(), nil
		}
	}
}
