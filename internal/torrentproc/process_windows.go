//go:build windows

package torrentproc

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

func applyProcessAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}
