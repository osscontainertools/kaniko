//go:build linux && arm

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
	"testing"

	"golang.org/x/sys/unix"
)

func TestARMSyscallNumbers(t *testing.T) {
	tests := map[string]uint32{
		"getuid":    uint32(unix.SYS_GETUID32),
		"getgid":    uint32(unix.SYS_GETGID32),
		"geteuid":   uint32(unix.SYS_GETEUID32),
		"getegid":   uint32(unix.SYS_GETEGID32),
		"getgroups": uint32(unix.SYS_GETGROUPS32),
		"select":    uint32(unix.SYS__NEWSELECT),
		"stat":      uint32(unix.SYS_STAT64),
		"fstat":     uint32(unix.SYS_FSTAT64),
		"lstat":     uint32(unix.SYS_LSTAT64),
	}

	for name, want := range tests {
		if got := nativeArch.syscallNumbers[name]; got != want {
			t.Errorf("%s syscall number = %d, want %d", name, got, want)
		}
	}
	if _, unavailable := nativeArch.unavailable["select"]; unavailable {
		t.Error("select must not be marked unavailable")
	}
}
