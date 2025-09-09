package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <command> [args...]\n", os.Args[0])
		os.Exit(1)
	}

	newCommand := os.Args[1:]
	cmd := exec.Command(newCommand[0], newCommand[1:]...)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "error starting command: %v\n", err)
		os.Exit(1)
	}

	waitErr := cmd.Wait()

	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				os.Exit(status.ExitStatus())
			}
		}
		fmt.Fprintf(os.Stderr, "process error: %v\n", waitErr)
		os.Exit(1)
	}
}
