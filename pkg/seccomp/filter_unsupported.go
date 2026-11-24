//go:build !linux || (!amd64 && !arm64 && !arm && !s390x && !riscv64 && !ppc64le)

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
)

func BuildFilter(string) ([]FilterInstruction, error) {
	return nil, fmt.Errorf("seccomp is not supported on %s/%s", runtime.GOOS, runtime.GOARCH)
}

func InstallDefaultFilter() error {
	return fmt.Errorf("seccomp is not supported on %s/%s", runtime.GOOS, runtime.GOARCH)
}
