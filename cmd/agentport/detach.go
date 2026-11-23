package main

import (
	"fmt"
	"os"
	"os/exec"
)

// startDetached starts command so that it outlives this process and never holds
// the caller's standard streams. An ssh LocalCommand, for example, waits for
// every process that inherited its output pipes: a child that kept them would
// hang the connection. stdin is the null device unless the caller set it,
// stdout and stderr are the null device, and the child gets its own session on
// unix or its own process group on Windows. The caller must not wait for it.
func startDetached(command *exec.Cmd) error {
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open the null device: %w", err)
	}
	defer null.Close()
	if command.Stdin == nil {
		command.Stdin = null
	}
	command.Stdout, command.Stderr = null, null
	detachProcess(command)
	return command.Start()
}
