//go:build linux && amd64

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

func TestBuildFilter(t *testing.T) {
	f, err := BuildFilter("")
	if err != nil {
		t.Fatal(err)
	}
	if len(f) == 0 {
		t.Fatal("empty filter")
	}
	if f[len(f)-1].K != unix.SECCOMP_RET_KILL_PROCESS {
		t.Fatal("filter must kill processes for unlisted syscalls")
	}
}

func TestBuildFilterRestrictsCloneNamespaces(t *testing.T) {
	filter, err := BuildFilter("")
	if err != nil {
		t.Fatal(err)
	}

	cloneCheck := 6 + 2*len(DefaultSyscalls)
	if got, want := filter[cloneCheck].K, uint32(unix.SYS_CLONE); got != want {
		t.Fatalf("clone check syscall number = %d, want %d", got, want)
	}
	if got, want := filter[cloneCheck+2].K, uint32(0x7e020080); got != want {
		t.Fatalf("clone namespace mask = %#x, want %#x", got, want)
	}
	if got, want := filter[cloneCheck+3].K, uint32(unix.SECCOMP_RET_KILL_PROCESS); got != want {
		t.Fatalf("clone namespace action = %#x, want %#x", got, want)
	}

	clone3Check := cloneCheck + 5
	if got, want := filter[clone3Check].K, uint32(unix.SYS_CLONE3); got != want {
		t.Fatalf("clone3 check syscall number = %d, want %d", got, want)
	}
	if got, want := filter[clone3Check+1].K, uint32(unix.SECCOMP_RET_ERRNO|unix.ENOSYS); got != want {
		t.Fatalf("clone3 fallback action = %#x, want %#x", got, want)
	}
}

func TestBuildFilterRejectsCustomProfile(t *testing.T) {
	if _, err := BuildFilter("custom.json"); err == nil {
		t.Fatal("expected custom profile to be rejected")
	}
}
