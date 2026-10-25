//go:build !linux

package archives

import "os/exec"

func prepareCommand(cmd *exec.Cmd) {}
