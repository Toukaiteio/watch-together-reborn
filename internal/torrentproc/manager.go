package torrentproc

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const startupTimeout = 90 * time.Second
const shutdownTimeout = 3 * time.Second

type Process struct {
	mu  sync.Mutex
	cmd *exec.Cmd
	url string
}

func Start(magnetURI string) (*Process, error) {
	magnetURI = strings.TrimSpace(magnetURI)
	if magnetURI == "" {
		return nil, fmt.Errorf("magnet URI is empty")
	}

	cmd, stdout, cleanup, err := buildCommand(magnetURI)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("capture helper stderr: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start torrent helper %s: %w", cmd.Path, err)
	}

	lineCh := make(chan string, 1)
	errCh := make(chan error, 1)
	stderrBuf := &safeBuffer{}
	go readFirstLine(stdout, lineCh, errCh)
	go captureStderr(stderr, stderrBuf)

	timer := time.NewTimer(startupTimeout)
	defer timer.Stop()

	select {
	case line := <-lineCh:
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "http://") && !strings.HasPrefix(line, "https://") {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return nil, fmt.Errorf("unexpected torrent helper startup output from %s: %q%s", cmd.Path, line, formatStderr(stderrBuf.String()))
		}
		return &Process{cmd: cmd, url: line}, nil
	case err := <-errCh:
		_ = cmd.Process.Kill()
		waitErr := cmd.Wait()
		return nil, fmt.Errorf("torrent helper failed before startup url from %s: read stdout: %w; process: %v%s", cmd.Path, err, waitErr, formatStderr(stderrBuf.String()))
	case <-timer.C:
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("torrent helper startup timed out from %s%s", cmd.Path, formatStderr(stderrBuf.String()))
	}
}

func (p *Process) URL() string {
	return p.url
}

func (p *Process) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil || p.cmd.Process == nil {
		return
	}

	// Ask the loopback-only helper to close its torrent client and remove its
	// temporary cache before falling back to a hard kill.
	shutdownURL := strings.TrimSuffix(p.url, "/video") + "/shutdown"
	request, err := http.NewRequest(http.MethodPost, shutdownURL, nil)
	if err == nil {
		client := &http.Client{Timeout: shutdownTimeout}
		response, requestErr := client.Do(request)
		if requestErr == nil && response != nil {
			_ = response.Body.Close()
		}
	}

	waitCh := make(chan struct{})
	go func() {
		_ = p.cmd.Wait()
		close(waitCh)
	}()
	select {
	case <-waitCh:
	case <-time.After(shutdownTimeout):
		_ = p.cmd.Process.Kill()
		<-waitCh
	}
	p.cmd = nil
}

func buildCommand(magnetURI string) (*exec.Cmd, io.ReadCloser, func(), error) {
	helperPath := findHelperBinary()
	if helperPath != "" {
		cmd := exec.Command(helperPath, "--magnet", magnetURI)
		applyProcessAttrs(cmd)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, nil, func() {}, fmt.Errorf("capture helper stdout: %w", err)
		}
		return cmd, stdout, func() {}, nil
	}

	root := findRepoRoot()
	if root == "" {
		return nil, nil, func() {}, fmt.Errorf("torrent helper binary not found; expected wt-torrent-helper%s next to the app executable or under build/bin", executableSuffix())
	}

	cmd := exec.Command("go", "run", ".", "--magnet", magnetURI)
	cmd.Dir = filepath.Join(root, "torrent-helper")
	applyProcessAttrs(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, func() {}, fmt.Errorf("capture go run stdout: %w", err)
	}
	return cmd, stdout, func() {}, nil
}

func findHelperBinary() string {
	name := "wt-torrent-helper"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	exePath, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exePath), name)
		if fileExists(candidate) {
			return candidate
		}
	}

	if cwd, err := os.Getwd(); err == nil {
		candidate := filepath.Join(cwd, name)
		if fileExists(candidate) {
			return candidate
		}
		candidate = filepath.Join(cwd, "build", "bin", name)
		if fileExists(candidate) {
			return candidate
		}
	}

	return ""
}

func executableSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func findRepoRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	dir := cwd
	for {
		if fileExists(filepath.Join(dir, "wails.json")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func readFirstLine(r io.Reader, lineCh chan<- string, errCh chan<- error) {
	reader := bufio.NewReader(r)
	line, err := reader.ReadString('\n')
	if err != nil {
		if err == io.EOF && strings.TrimSpace(line) != "" {
			lineCh <- line
			return
		}
		errCh <- err
		return
	}
	lineCh <- line
}

type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	originalLen := len(p)
	b.mu.Lock()
	defer b.mu.Unlock()
	const max = 8192
	remaining := max - b.b.Len()
	if remaining <= 0 {
		return originalLen, nil
	}
	if len(p) > remaining {
		p = p[:remaining]
	}
	_, _ = b.b.Write(p)
	return originalLen, nil
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(b.b.String())
}

func captureStderr(r io.Reader, b *safeBuffer) {
	_, _ = io.Copy(b, r)
}

func formatStderr(stderr string) string {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return ""
	}
	return "; stderr: " + stderr
}
