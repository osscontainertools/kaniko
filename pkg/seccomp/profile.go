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

var DefaultSyscalls = []string{
	"read", "write", "openat", "close", "execve", "execveat", "stat", "fstat", "lstat",
	"exit_group", "exit", "getpid", "getuid", "getgid", "futex",
	"getppid", "geteuid", "getegid", "getgroups", "getresuid", "getresgid", "capget", "capset",
	"mmap", "mprotect", "munmap", "brk", "rt_sigaction", "rt_sigprocmask",
	"rt_sigreturn", "ioctl", "pread64", "pwrite64", "readv", "writev",
	"access", "faccessat", "faccessat2", "pipe", "select", "sched_yield", "mremap", "msync",
	"mincore", "madvise", "dup", "dup2", "nanosleep", "gettid",
	"gettimeofday", "clock_gettime", "clock_nanosleep", "set_tid_address",
	"set_robust_list", "prlimit64", "getdents", "getdents64", "fcntl",
	"mkdir", "mkdirat", "unlink", "unlinkat", "rename", "renameat",
	"link", "linkat", "symlink", "symlinkat", "readlink", "readlinkat",
	"chmod", "fchmod", "fchmodat", "chown", "fchown", "fchownat",
	"umask", "getcwd", "chdir", "fchdir", "newfstatat", "statx", "lseek",
	"wait4", "waitid", "kill", "tgkill", "vfork", "pipe2", "poll",
	"ppoll", "pselect6", "dup3", "socket", "connect", "accept", "accept4",
	"bind", "listen", "getsockname", "getpeername", "sendto", "recvfrom",
	"sendmsg", "recvmsg", "setsockopt", "getsockopt", "shutdown", "sendfile",
	"copy_file_range", "fsync", "fdatasync", "ftruncate", "getrandom",
	"arch_prctl", "rseq", "prctl", "membarrier",
}

type Profile struct {
	Syscalls []string `json:"syscalls"`
}
