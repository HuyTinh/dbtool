//go:build windows

package procutil

import (
	"fmt"
	"os"
	"os/exec"
)

func SetupProcAttr(cmd *exec.Cmd) {
	// Windows does not support POSIX process group attributes like Setpgid
}

func KillProcessGroup(cmd *exec.Cmd, _ os.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	// On Windows, taskkill /T /F kills the process and all child processes started by it
	return exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprint(cmd.Process.Pid)).Run()
}
