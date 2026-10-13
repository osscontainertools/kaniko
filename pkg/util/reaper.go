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
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/sirupsen/logrus"
)

// runGroups holds the process groups of RUN commands in flight, keyed by their leader's pid.
// The mutex also spans starting a leader, so the reaper never sees a leader before it is registered.
var (
	runGroupsMu sync.Mutex
	runGroups   = map[int]struct{}{}
)

// StartInit takes over the duties of PID 1: reaping the processes RUN commands orphan,
// and forwarding termination signals to the RUN commands in flight.
func StartInit() {
	if os.Getpid() != 1 {
		return
	}
	sigs := make(chan os.Signal, 16)
	signal.Notify(sigs, syscall.SIGCHLD, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	go func() {
		for sig := range sigs {
			if sig == syscall.SIGCHLD {
				reapOrphans()
			} else {
				forwardSignal(sig.(syscall.Signal))
			}
		}
	}()
}

// StartRunGroup starts cmd as the leader of its own process group and registers that group.
// cmd.Wait stays responsible for the leader, the reaper only collects the rest.
func StartRunGroup(cmd *exec.Cmd) error {
	runGroupsMu.Lock()
	defer runGroupsMu.Unlock()
	err := cmd.Start()
	if err != nil {
		return err
	}
	runGroups[cmd.Process.Pid] = struct{}{}
	return nil
}

func ReleaseRunGroup(pgid int) {
	runGroupsMu.Lock()
	defer runGroupsMu.Unlock()
	delete(runGroups, pgid)
}

func forwardSignal(sig syscall.Signal) {
	runGroupsMu.Lock()
	defer runGroupsMu.Unlock()
	if len(runGroups) == 0 {
		logrus.Warnf("received %s, exiting", sig)
		os.Exit(128 + int(sig))
	}
	for pgid := range runGroups {
		logrus.Warnf("received %s, forwarding to process group %d", sig, pgid)
		err := syscall.Kill(-pgid, sig)
		if err != nil && err != syscall.ESRCH {
			logrus.Warnf("forwarding %s to process group %d: %v", sig, pgid, err)
		}
	}
}

// reapOrphans collects exited children that no os/exec.Cmd waits for.
// Children in kaniko's own process group belong to os/exec callers such as credential helpers,
// and RUN leaders belong to their cmd.Wait. Reaping either would make their Wait fail with ECHILD.
func reapOrphans() {
	runGroupsMu.Lock()
	defer runGroupsMu.Unlock()
	self := os.Getpid()
	ownGroup := syscall.Getpgrp()
	stats, err := filepath.Glob("/proc/[0-9]*/stat")
	if err != nil {
		return
	}
	for _, stat := range stats {
		data, err := os.ReadFile(stat)
		if err != nil {
			continue
		}
		// comm may contain spaces and parentheses, the fixed fields follow its last ')'
		fields := strings.Fields(string(data[strings.LastIndexByte(string(data), ')')+1:]))
		if len(fields) < 3 || fields[0] != "Z" {
			continue
		}
		ppid, _ := strconv.Atoi(fields[1])
		pgid, _ := strconv.Atoi(fields[2])
		pid, _ := strconv.Atoi(filepath.Base(filepath.Dir(stat)))
		_, leader := runGroups[pid]
		if ppid == self && pgid != ownGroup && !leader {
			var status syscall.WaitStatus
			_, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
			if err != nil {
				logrus.Debugf("reaping orphan %d: %v", pid, err)
			}
		}
	}
}
