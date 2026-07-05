//go:build !windows

package procutil

import (
	"os"
	"os/exec"
	"syscall"
)

func SetupProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func KillProcessGroup(cmd *exec.Cmd, sig os.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	sysSig, ok := sig.(syscall.Signal)
	if !ok {
		sysSig = syscall.SIGTERM
	}
	// A negative PID kills the process group in syscall.Kill
	return syscall.Kill(-cmd.Process.Pid, sysSig)
}
