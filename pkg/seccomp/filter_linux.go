//go:build linux && (amd64 || arm64 || arm || s390x || riscv64 || ppc64le)

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
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	bpfLDWABS             = 0x20
	bpfJEQK               = 0x15
	bpfJGEK               = 0x35
	bpfJSETK              = 0x45
	bpfRETK               = 0x06
	seccompDataNrOffset   = 0
	seccompDataArchOffset = 4
	seccompDataArg0Offset = 16
	seccompDataArg0Hi     = 20
)

type archConfig struct {
	auditArch        uint32
	syscallNumbers   map[string]uint32
	unavailable      map[string]struct{}
	cloneArgOffset   uint32
	checkX32Syscalls bool
}

func syscallSet(names ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		set[name] = struct{}{}
	}
	return set
}

func BuildFilter(profilePath string) ([]unix.SockFilter, error) {
	if profilePath != "" {
		return nil, fmt.Errorf("custom seccomp profiles are not supported")
	}

	allowed := make([]uint32, 0, len(DefaultSyscalls))
	for _, name := range DefaultSyscalls {
		n, ok := nativeArch.syscallNumbers[name]
		if !ok {
			if _, unavailable := nativeArch.unavailable[name]; unavailable {
				continue
			}
			return nil, fmt.Errorf("unknown syscall %q in default seccomp profile for %s", name, runtime.GOARCH)
		}
		allowed = append(allowed, n)
	}

	clone, ok := nativeArch.syscallNumbers["clone"]
	if !ok {
		return nil, fmt.Errorf("clone syscall is not defined for %s", runtime.GOARCH)
	}
	clone3, ok := nativeArch.syscallNumbers["clone3"]
	if !ok {
		return nil, fmt.Errorf("clone3 syscall is not defined for %s", runtime.GOARCH)
	}

	filter := []unix.SockFilter{
		{Code: bpfLDWABS, K: seccompDataArchOffset},
		{Code: bpfJEQK, Jt: 1, K: nativeArch.auditArch},
		{Code: bpfRETK, K: unix.SECCOMP_RET_KILL_PROCESS},
		{Code: bpfLDWABS, K: seccompDataNrOffset},
	}
	if nativeArch.checkX32Syscalls {
		filter = append(filter,
			unix.SockFilter{Code: bpfJGEK, Jf: 1, K: 0x40000000},
			unix.SockFilter{Code: bpfRETK, K: unix.SECCOMP_RET_KILL_PROCESS},
		)
	}
	for _, n := range allowed {
		filter = append(filter,
			unix.SockFilter{Code: bpfJEQK, Jf: 1, K: n},
			unix.SockFilter{Code: bpfRETK, K: unix.SECCOMP_RET_ALLOW},
		)
	}
	filter = append(filter,
		unix.SockFilter{Code: bpfJEQK, Jf: 4, K: clone},
		unix.SockFilter{Code: bpfLDWABS, K: nativeArch.cloneArgOffset},
		unix.SockFilter{Code: bpfJSETK, Jf: 1, K: 0x7e020080},
		unix.SockFilter{Code: bpfRETK, K: unix.SECCOMP_RET_KILL_PROCESS},
		unix.SockFilter{Code: bpfRETK, K: unix.SECCOMP_RET_ALLOW},
		unix.SockFilter{Code: bpfJEQK, Jf: 1, K: clone3},
		unix.SockFilter{Code: bpfRETK, K: uint32(unix.SECCOMP_RET_ERRNO) | uint32(unix.ENOSYS)},
		unix.SockFilter{Code: bpfRETK, K: unix.SECCOMP_RET_KILL_PROCESS},
	)
	if len(filter) > 0xffff {
		return nil, fmt.Errorf("seccomp filter is too large: %d instructions", len(filter))
	}
	return filter, nil
}

func installFilter(filter []unix.SockFilter) error {
	if len(filter) == 0 {
		return fmt.Errorf("seccomp filter is empty")
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("setting no_new_privs: %w", err)
	}
	program := unix.SockFprog{
		Len:    uint16(len(filter)),
		Filter: &filter[0],
	}
	if err := unix.Prctl(unix.PR_SET_SECCOMP, unix.SECCOMP_MODE_FILTER, uintptr(unsafe.Pointer(&program)), 0, 0); err != nil {
		return fmt.Errorf("installing seccomp filter: %w", err)
	}
	runtime.KeepAlive(filter)
	runtime.KeepAlive(&program)
	return nil
}

func InstallDefaultFilter() error {
	filter, err := BuildFilter("")
	if err != nil {
		return err
	}
	return installFilter(filter)
}
