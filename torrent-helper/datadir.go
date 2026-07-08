package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const minTorrentFreeSpace int64 = 512 << 20

type dataDirCandidate struct {
	path string
	free int64
}

func createTorrentDataDir() (string, error) {
	if envDir := os.Getenv("WT_TORRENT_DATA_DIR"); envDir != "" {
		return createSessionDir(envDir)
	}

	candidates := torrentDataDirCandidates()
	best, err := bestWritableDataDir(candidates)
	if err != nil {
		return "", err
	}
	return createSessionDir(best.path)
}

func torrentDataDirCandidates() []string {
	var dirs []string
	add := func(path string) {
		path = filepath.Clean(path)
		for _, existing := range dirs {
			if samePath(existing, path) {
				return
			}
		}
		dirs = append(dirs, path)
	}

	if exePath, err := os.Executable(); err == nil {
		add(filepath.Join(filepath.Dir(exePath), "wt-magnet-cache"))
	}
	if cwd, err := os.Getwd(); err == nil {
		add(filepath.Join(cwd, "wt-magnet-cache"))
	}
	if cacheDir, err := os.UserCacheDir(); err == nil {
		add(filepath.Join(cacheDir, "watch-together-reborn", "magnet-cache"))
	}
	add(filepath.Join(os.TempDir(), "watch-together-reborn-magnet-cache"))
	return dirs
}

func bestWritableDataDir(paths []string) (dataDirCandidate, error) {
	var best dataDirCandidate
	var lastErr error
	for _, path := range paths {
		if err := os.MkdirAll(path, 0o755); err != nil {
			lastErr = err
			continue
		}
		probe, err := os.CreateTemp(path, ".write-test-*")
		if err != nil {
			lastErr = err
			continue
		}
		probePath := probe.Name()
		_ = probe.Close()
		_ = os.Remove(probePath)

		free, err := diskFreeBytes(path)
		if err != nil {
			lastErr = err
			continue
		}
		if best.path == "" || free > best.free {
			best = dataDirCandidate{path: path, free: free}
		}
	}
	if best.path == "" {
		if lastErr != nil {
			return best, lastErr
		}
		return best, fmt.Errorf("no writable magnet cache directory")
	}
	return best, nil
}

func createSessionDir(base string) (string, error) {
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	return os.MkdirTemp(base, "session-*")
}

func ensureEnoughDiskSpace(path string, fileSize int64) error {
	free, err := diskFreeBytes(path)
	if err != nil {
		return fmt.Errorf("check magnet cache free space: %w", err)
	}
	required := fileSize + minTorrentFreeSpace
	if free < required {
		return fmt.Errorf("magnet cache disk space is insufficient: need at least %.1f GB free, only %.1f GB free at %s", bytesToGB(required), bytesToGB(free), path)
	}
	return nil
}

func bytesToGB(value int64) float64 {
	return float64(value) / (1 << 30)
}
