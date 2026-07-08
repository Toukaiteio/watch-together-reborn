//go:build !windows

package torrentproc

import "os/exec"

func applyProcessAttrs(_ *exec.Cmd) {}
