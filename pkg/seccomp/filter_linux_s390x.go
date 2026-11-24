//go:build linux && s390x

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

import "golang.org/x/sys/unix"

var nativeArch = archConfig{
	auditArch: unix.AUDIT_ARCH_S390X,
	syscallNumbers: map[string]uint32{
		"read": uint32(unix.SYS_READ), "write": uint32(unix.SYS_WRITE), "openat": uint32(unix.SYS_OPENAT),
		"close": uint32(unix.SYS_CLOSE), "execve": uint32(unix.SYS_EXECVE), "execveat": uint32(unix.SYS_EXECVEAT),
		"stat": uint32(unix.SYS_STAT), "fstat": uint32(unix.SYS_FSTAT), "lstat": uint32(unix.SYS_LSTAT),
		"exit_group": uint32(unix.SYS_EXIT_GROUP), "exit": uint32(unix.SYS_EXIT), "getpid": uint32(unix.SYS_GETPID),
		"getuid": uint32(unix.SYS_GETUID), "getgid": uint32(unix.SYS_GETGID), "futex": uint32(unix.SYS_FUTEX),
		"getppid": uint32(unix.SYS_GETPPID), "geteuid": uint32(unix.SYS_GETEUID), "getegid": uint32(unix.SYS_GETEGID),
		"getgroups": uint32(unix.SYS_GETGROUPS), "getresuid": uint32(unix.SYS_GETRESUID),
		"getresgid": uint32(unix.SYS_GETRESGID), "capget": uint32(unix.SYS_CAPGET), "capset": uint32(unix.SYS_CAPSET),
		"mmap": uint32(unix.SYS_MMAP), "mprotect": uint32(unix.SYS_MPROTECT), "munmap": uint32(unix.SYS_MUNMAP),
		"brk": uint32(unix.SYS_BRK), "rt_sigaction": uint32(unix.SYS_RT_SIGACTION),
		"rt_sigprocmask": uint32(unix.SYS_RT_SIGPROCMASK), "rt_sigreturn": uint32(unix.SYS_RT_SIGRETURN),
		"ioctl": uint32(unix.SYS_IOCTL), "pread64": uint32(unix.SYS_PREAD64), "pwrite64": uint32(unix.SYS_PWRITE64),
		"readv": uint32(unix.SYS_READV), "writev": uint32(unix.SYS_WRITEV), "access": uint32(unix.SYS_ACCESS),
		"faccessat": uint32(unix.SYS_FACCESSAT), "faccessat2": uint32(unix.SYS_FACCESSAT2),
		"pipe": uint32(unix.SYS_PIPE), "select": uint32(unix.SYS_SELECT), "sched_yield": uint32(unix.SYS_SCHED_YIELD),
		"mremap": uint32(unix.SYS_MREMAP), "msync": uint32(unix.SYS_MSYNC), "mincore": uint32(unix.SYS_MINCORE),
		"madvise": uint32(unix.SYS_MADVISE), "dup": uint32(unix.SYS_DUP), "dup2": uint32(unix.SYS_DUP2),
		"nanosleep": uint32(unix.SYS_NANOSLEEP), "gettid": uint32(unix.SYS_GETTID),
		"gettimeofday": uint32(unix.SYS_GETTIMEOFDAY), "clock_gettime": uint32(unix.SYS_CLOCK_GETTIME),
		"clock_nanosleep": uint32(unix.SYS_CLOCK_NANOSLEEP), "set_tid_address": uint32(unix.SYS_SET_TID_ADDRESS),
		"set_robust_list": uint32(unix.SYS_SET_ROBUST_LIST), "prlimit64": uint32(unix.SYS_PRLIMIT64),
		"getdents": uint32(unix.SYS_GETDENTS), "getdents64": uint32(unix.SYS_GETDENTS64),
		"fcntl": uint32(unix.SYS_FCNTL), "mkdir": uint32(unix.SYS_MKDIR), "mkdirat": uint32(unix.SYS_MKDIRAT),
		"unlink": uint32(unix.SYS_UNLINK), "unlinkat": uint32(unix.SYS_UNLINKAT),
		"rename": uint32(unix.SYS_RENAME), "renameat": uint32(unix.SYS_RENAMEAT),
		"link": uint32(unix.SYS_LINK), "linkat": uint32(unix.SYS_LINKAT),
		"symlink": uint32(unix.SYS_SYMLINK), "symlinkat": uint32(unix.SYS_SYMLINKAT),
		"readlink": uint32(unix.SYS_READLINK), "readlinkat": uint32(unix.SYS_READLINKAT),
		"chmod": uint32(unix.SYS_CHMOD), "fchmod": uint32(unix.SYS_FCHMOD), "fchmodat": uint32(unix.SYS_FCHMODAT),
		"chown": uint32(unix.SYS_CHOWN), "fchown": uint32(unix.SYS_FCHOWN), "fchownat": uint32(unix.SYS_FCHOWNAT),
		"umask": uint32(unix.SYS_UMASK), "getcwd": uint32(unix.SYS_GETCWD), "chdir": uint32(unix.SYS_CHDIR),
		"fchdir": uint32(unix.SYS_FCHDIR), "newfstatat": uint32(unix.SYS_NEWFSTATAT),
		"statx": uint32(unix.SYS_STATX), "lseek": uint32(unix.SYS_LSEEK), "wait4": uint32(unix.SYS_WAIT4),
		"waitid": uint32(unix.SYS_WAITID), "kill": uint32(unix.SYS_KILL), "tgkill": uint32(unix.SYS_TGKILL),
		"vfork": uint32(unix.SYS_VFORK), "pipe2": uint32(unix.SYS_PIPE2), "poll": uint32(unix.SYS_POLL),
		"ppoll": uint32(unix.SYS_PPOLL), "pselect6": uint32(unix.SYS_PSELECT6), "dup3": uint32(unix.SYS_DUP3),
		"socket": uint32(unix.SYS_SOCKET), "connect": uint32(unix.SYS_CONNECT), "accept4": uint32(unix.SYS_ACCEPT4),
		"bind": uint32(unix.SYS_BIND), "listen": uint32(unix.SYS_LISTEN), "getsockname": uint32(unix.SYS_GETSOCKNAME),
		"getpeername": uint32(unix.SYS_GETPEERNAME), "sendto": uint32(unix.SYS_SENDTO),
		"recvfrom": uint32(unix.SYS_RECVFROM), "sendmsg": uint32(unix.SYS_SENDMSG),
		"recvmsg": uint32(unix.SYS_RECVMSG), "setsockopt": uint32(unix.SYS_SETSOCKOPT),
		"getsockopt": uint32(unix.SYS_GETSOCKOPT), "shutdown": uint32(unix.SYS_SHUTDOWN),
		"sendfile": uint32(unix.SYS_SENDFILE), "copy_file_range": uint32(unix.SYS_COPY_FILE_RANGE),
		"fsync": uint32(unix.SYS_FSYNC), "fdatasync": uint32(unix.SYS_FDATASYNC),
		"ftruncate": uint32(unix.SYS_FTRUNCATE), "getrandom": uint32(unix.SYS_GETRANDOM),
		"rseq": uint32(unix.SYS_RSEQ), "prctl": uint32(unix.SYS_PRCTL), "membarrier": uint32(unix.SYS_MEMBARRIER),
		"clone": uint32(unix.SYS_CLONE), "clone3": uint32(unix.SYS_CLONE3),
	},
	unavailable: map[string]struct{}{
		"accept": {}, "arch_prctl": {},
	},
	cloneArgOffset: seccompDataArg0Hi,
}
