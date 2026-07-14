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
	videoArgs, copyVideo := videoOutputArgs(ffmpegPath, filePath)
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
	if !copyVideo {
		args = append(args,
			"-force_key_frames", "expr:gte(t,n_forced*"+SegmentTime+")",
			"-sc_threshold", "0",
			"-pix_fmt", "yuv420p",
		)
	}
	hlsFlags := "temp_file"
	if !copyVideo {
		hlsFlags = "independent_segments+temp_file"
	}
	args = append(args,
		"-c:a", "aac",
		"-b:a", "128k",
		"-ac", "2",
		"-f", "hls",
		"-hls_time", SegmentTime,
		"-hls_list_size", "0",
		"-hls_playlist_type", "event",
		"-hls_flags", hlsFlags,
		"-hls_segment_type", "fmp4",
		"-hls_fmp4_init_filename", InitName,
		"-hls_segment_filename", segmentPattern,
		playlistPath,
	)

	cmd := exec.Command(ffmpegPath, args...)
	// ffmpeg resolves hls_fmp4_init_filename relative to its working
	// directory, not the playlist path. Keep init.mp4 alongside the playlist
	// and segments so the HTTP HLS handler can serve every referenced asset.
	cmd.Dir = dir

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

// WaitReady returns as soon as the first playable HLS segment is available.
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

func videoOutputArgs(ffmpegPath, filePath string) ([]string, bool) {
	if sourceHasBrowserCompatibleH264(ffmpegPath, filePath) {
		return []string{"-c:v", "copy"}, true
	}
	return h264EncoderArgs(ffmpegPath), false
}

func sourceHasBrowserCompatibleH264(ffmpegPath, filePath string) bool {
	output, _ := exec.Command(ffmpegPath, "-hide_banner", "-i", filePath).CombinedOutput()
	probe := string(output)
	return strings.Contains(probe, "Video: h264 ") && strings.Contains(probe, ", yuv420p,")
}

func h264EncoderArgs(ffmpegPath string) []string {
	output, err := exec.Command(ffmpegPath, "-hide_banner", "-encoders").CombinedOutput()
	if err != nil {
		return nil
	}
	encoders := string(output)
	switch {
	case strings.Contains(encoders, "libx264"):
		return []string{"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-crf", "23", "-profile:v", "main", "-level:v", "3.1"}
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
