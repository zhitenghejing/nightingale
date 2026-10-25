//go:build linux

package archives

import (
	"os/exec"
	"syscall"
	"time"
)

// Terminate the whole renderer/converter process group on timeout, including
// LibreOffice's child process. Each conversion also owns its private profile.
func prepareCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 2 * time.Second
}
