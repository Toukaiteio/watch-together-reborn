package segment

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	PlaylistName = "index.m3u8"
	InitName     = "init.mp4"
	SegmentTime  = "4"
	MimeCodec    = `video/mp4; codecs="avc1.4d401f,mp4a.40.2"`
)

// Segmenter owns one ffmpeg-backed HLS generation session.
type Segmenter struct {
	dir    string
	cmd    *exec.Cmd
	stderr bytes.Buffer
	done   chan error
	once   sync.Once
}

func Start(filePath string) (*Segmenter, error) {
	ffmpegPath, err := findFFmpeg()
	if err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "wt-hls-*")
	if err != nil {
		return nil, fmt.Errorf("create hls temp dir: %w", err)
	}

	playlistPath := filepath.Join(dir, PlaylistName)
	segmentPattern := filepath.Join(dir, "seg_%05d.m4s")
	videoArgs := h264EncoderArgs(ffmpegPath)
	if len(videoArgs) == 0 {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("no usable H.264 encoder found in ffmpeg")
	}

	args := []string{
		"-hide_banner",
		"-y",
		"-i", filePath,
		"-map", "0:v:0",
		"-map", "0:a:0?",
	}
	args = append(args, videoArgs...)
	args = append(args,
		"-force_key_frames", "expr:gte(t,n_forced*"+SegmentTime+")",
		"-sc_threshold", "0",
		"-profile:v", "main",
		"-level:v", "3.1",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-b:a", "128k",
		"-ac", "2",
		"-f", "hls",
		"-hls_time", SegmentTime,
		"-hls_list_size", "0",
		"-hls_playlist_type", "event",
		"-hls_flags", "independent_segments+temp_file",
		"-hls_segment_type", "fmp4",
		"-hls_fmp4_init_filename", InitName,
		"-hls_segment_filename", segmentPattern,
		playlistPath,
	)

	cmd := exec.Command(ffmpegPath, args...)

	s := &Segmenter{
		dir:  dir,
		cmd:  cmd,
		done: make(chan error, 1),
	}
	cmd.Stderr = &s.stderr

	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("start ffmpeg: %w", err)
	}

	go func() {
		s.done <- cmd.Wait()
	}()

	return s, nil
}

func (s *Segmenter) Dir() string {
	return s.dir
}

func (s *Segmenter) PlaylistPath() string {
	return filepath.Join(s.dir, PlaylistName)
}

func (s *Segmenter) WaitReady(timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()

	for {
		select {
		case err := <-s.done:
			s.done <- err
			if err != nil {
				return fmt.Errorf("ffmpeg exited before first segment: %w%s", err, s.stderrSuffix())
			}
			if s.hasPlayablePlaylist() {
				return nil
			}
			return fmt.Errorf("ffmpeg exited before producing a playable playlist%s", s.stderrSuffix())
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for HLS segment%s", s.stderrSuffix())
		case <-ticker.C:
			if s.hasPlayablePlaylist() {
				return nil
			}
		}
	}
}

func (s *Segmenter) Cleanup() {
	s.once.Do(func() {
		if s.cmd != nil && s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		select {
		case <-s.done:
		case <-time.After(2 * time.Second):
		}
		_ = os.RemoveAll(s.dir)
	})
}

func (s *Segmenter) hasPlayablePlaylist() bool {
	data, err := os.ReadFile(s.PlaylistPath())
	if err != nil {
		return false
	}
	content := string(data)
	return strings.Contains(content, "#EXTINF") &&
		strings.Contains(content, ".m4s") &&
		strings.Contains(content, InitName)
}

func (s *Segmenter) stderrSuffix() string {
	text := strings.TrimSpace(s.stderr.String())
	if text == "" {
		return ""
	}
	if len(text) > 1600 {
		text = text[len(text)-1600:]
	}
	return ": " + text
}

func h264EncoderArgs(ffmpegPath string) []string {
	output, err := exec.Command(ffmpegPath, "-hide_banner", "-encoders").CombinedOutput()
	if err != nil {
		return nil
	}
	encoders := string(output)
	switch {
	case strings.Contains(encoders, "libx264"):
		return []string{"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-crf", "23"}
	case strings.Contains(encoders, "h264_mf"):
		return []string{"-c:v", "h264_mf", "-b:v", "2500k"}
	case strings.Contains(encoders, "h264_nvenc"):
		return []string{"-c:v", "h264_nvenc", "-b:v", "2500k"}
	case strings.Contains(encoders, "h264_qsv"):
		return []string{"-c:v", "h264_qsv", "-b:v", "2500k"}
	case strings.Contains(encoders, "h264_amf"):
		return []string{"-c:v", "h264_amf", "-b:v", "2500k"}
	default:
		return nil
	}
}
