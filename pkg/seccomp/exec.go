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

package seccomp

import (
	"fmt"
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

const RunnerArg = "--internal-seccomp-run"

func RunAndExec(args []string, env []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no command supplied to seccomp runner")
	}
	// Seccomp filters are per-thread, so keep exec on the thread that installs it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := InstallDefaultFilter(); err != nil {
		return err
	}
	if err := unix.Exec(args[0], args, env); err != nil {
		return fmt.Errorf("executing command %q: %w", args[0], err)
	}
	return nil
}

func RunAndExecFromArgs() error {
	return RunAndExec(os.Args[2:], os.Environ())
}
