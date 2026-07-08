package segment

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func findFFmpeg() (string, error) {
	name := "ffmpeg"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	candidates := []string{}
	if configured := os.Getenv("WT_FFMPEG_PATH"); configured != "" {
		candidates = append(candidates, configured)
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), name))
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(cwd, name),
			filepath.Join(cwd, "build", "bin", name),
			filepath.Join(cwd, "build", "ffmpeg", "current", name),
		)
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	if path, err := exec.LookPath("ffmpeg"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("ffmpeg not found; bundle it next to the app or set WT_FFMPEG_PATH")
}
