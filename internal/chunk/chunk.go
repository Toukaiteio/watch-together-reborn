package chunk

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"watch-together-reborn/internal/segment"
)

// Progress describes media-share preparation progress for a local file.
type Progress struct {
	Stage   string  `json:"stage"`
	Current int64   `json:"current"`
	Total   int64   `json:"total"`
	Percent float64 `json:"percent"`
}

// SegmentInfo describes one distributed media unit.
type SegmentInfo struct {
	Index     int     `json:"index"`
	Path      string  `json:"path"`
	Duration  float64 `json:"duration"`
	StartTime float64 `json:"startTime"`
	Size      int64   `json:"size"`
	IsInit    bool    `json:"isInit,omitempty"`
}

// Manifest describes a segmented local video share session.
type Manifest struct {
	FileName      string        `json:"fileName"`
	MimeCodec     string        `json:"mimeCodec"`
	SegmentTime   float64       `json:"segmentTime"`
	TotalDuration float64       `json:"totalDuration"`
	TotalChunks   int           `json:"totalChunks"`
	Complete      bool          `json:"complete"`
	Chunks        []SegmentInfo `json:"chunks"`
}

// ChunkManager manages a dynamic fMP4 segment share rooted at one HLS output dir.
type ChunkManager struct {
	mu           sync.RWMutex
	hlsDir       string
	fileName     string
	manifest     *Manifest
	chunkPaths   map[int]string
	chunkModTime int64
}

// NewChunkManager keeps backward compatibility for older callers that only have a file path.
// Local-share scheme 3 should prefer NewChunkManagerFromHLSDir so the fMP4 assets are reused.
func NewChunkManager(filePath string) (*ChunkManager, error) {
	return nil, fmt.Errorf("chunk manager now requires an HLS/fMP4 segment directory")
}

// NewChunkManagerWithProgress keeps the old signature but reports a clear failure.
func NewChunkManagerWithProgress(filePath string, onProgress func(Progress)) (*ChunkManager, error) {
	if onProgress != nil {
		onProgress(Progress{Stage: "error", Current: 0, Total: 0, Percent: 0})
	}
	return nil, fmt.Errorf("chunk manager now requires an HLS/fMP4 segment directory")
}

// NewChunkManagerFromHLSDir exposes the generated fMP4 init/media segments as share chunks.
func NewChunkManagerFromHLSDir(hlsDir string, onProgress func(Progress)) (*ChunkManager, error) {
	if onProgress != nil {
		onProgress(Progress{Stage: "starting", Current: 0, Total: 0, Percent: 0})
	}

	mgr := &ChunkManager{
		hlsDir:     hlsDir,
		chunkPaths: make(map[int]string),
	}
	if err := mgr.refreshLocked(); err != nil {
		if onProgress != nil {
			onProgress(Progress{Stage: "error", Current: 0, Total: 0, Percent: 0})
		}
		return nil, err
	}

	if onProgress != nil {
		total := int64(len(mgr.manifest.Chunks))
		onProgress(Progress{Stage: "complete", Current: total, Total: total, Percent: 100})
	}
	return mgr, nil
}

// ManifestJSON returns the latest manifest as JSON bytes.
func (cm *ChunkManager) ManifestJSON() []byte {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if err := cm.refreshLocked(); err != nil || cm.manifest == nil {
		return nil
	}
	data, _ := json.Marshal(cm.manifest)
	return data
}

// Manifest returns the latest manifest snapshot.
func (cm *ChunkManager) Manifest() *Manifest {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if err := cm.refreshLocked(); err != nil || cm.manifest == nil {
		return nil
	}
	copyManifest := *cm.manifest
	copyManifest.Chunks = append([]SegmentInfo(nil), cm.manifest.Chunks...)
	return &copyManifest
}

// GetChunk returns the latest bytes for a specific init/media segment index.
func (cm *ChunkManager) GetChunk(index int) ([]byte, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if err := cm.refreshLocked(); err != nil {
		return nil, err
	}
	path, ok := cm.chunkPaths[index]
	if !ok {
		return nil, fmt.Errorf("chunk index %d out of range", index)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read chunk %d: %w", index, err)
	}
	return data, nil
}

// Cleanup is a no-op because the underlying segmenter owns lifecycle of the HLS dir.
func (cm *ChunkManager) Cleanup() {}

func (cm *ChunkManager) refreshLocked() error {
	playlistPath := filepath.Join(cm.hlsDir, segment.PlaylistName)
	info, err := os.Stat(playlistPath)
	if err != nil {
		return fmt.Errorf("stat playlist: %w", err)
	}
	modTime := info.ModTime().UnixNano()
	if cm.manifest != nil && modTime == cm.chunkModTime {
		return nil
	}

	data, err := os.ReadFile(playlistPath)
	if err != nil {
		return fmt.Errorf("read playlist: %w", err)
	}

	manifest, paths, err := buildManifest(cm.hlsDir, string(data))
	if err != nil {
		return err
	}
	if cm.fileName != "" {
		manifest.FileName = cm.fileName
	}

	cm.manifest = manifest
	cm.chunkPaths = paths
	cm.chunkModTime = modTime
	return nil
}

func buildManifest(hlsDir, playlist string) (*Manifest, map[int]string, error) {
	lines := strings.Split(strings.ReplaceAll(playlist, "\r\n", "\n"), "\n")
	manifest := &Manifest{
		FileName:    filepath.Base(hlsDir),
		MimeCodec:   segment.MimeCodec,
		SegmentTime: 4,
	}

	chunkPaths := make(map[int]string)
	chunks := make([]SegmentInfo, 0, 16)
	startTime := 0.0
	nextDuration := 0.0
	nextIndex := 0

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "#EXT-X-TARGETDURATION:"):
			value := strings.TrimPrefix(line, "#EXT-X-TARGETDURATION:")
			if parsed, err := strconv.ParseFloat(value, 64); err == nil && parsed > 0 {
				manifest.SegmentTime = parsed
			}
		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			initURI := parseAttribute(line, "URI")
			if initURI == "" {
				continue
			}
			absPath := filepath.Join(hlsDir, filepath.Base(initURI))
			size := fileSize(absPath)
			chunks = append(chunks, SegmentInfo{
				Index:  nextIndex,
				Path:   "/video/chunk/" + strconv.Itoa(nextIndex),
				Size:   size,
				IsInit: true,
			})
			chunkPaths[nextIndex] = absPath
			nextIndex++
		case strings.HasPrefix(line, "#EXTINF:"):
			value := strings.TrimSuffix(strings.TrimPrefix(line, "#EXTINF:"), ",")
			if parsed, err := strconv.ParseFloat(value, 64); err == nil && parsed >= 0 {
				nextDuration = parsed
			}
		case strings.HasPrefix(line, "#EXT-X-ENDLIST"):
			manifest.Complete = true
		case !strings.HasPrefix(line, "#"):
			absPath := filepath.Join(hlsDir, filepath.Base(line))
			size := fileSize(absPath)
			chunks = append(chunks, SegmentInfo{
				Index:     nextIndex,
				Path:      "/video/chunk/" + strconv.Itoa(nextIndex),
				Duration:  nextDuration,
				StartTime: startTime,
				Size:      size,
			})
			chunkPaths[nextIndex] = absPath
			startTime += nextDuration
			nextDuration = 0
			nextIndex++
		}
	}

	if len(chunks) < 2 {
		return nil, nil, fmt.Errorf("playlist has not produced enough fMP4 assets yet")
	}

	manifest.TotalChunks = len(chunks)
	manifest.TotalDuration = startTime
	manifest.Chunks = chunks
	return manifest, chunkPaths, nil
}

func parseAttribute(line, key string) string {
	prefix := key + "=\""
	idx := strings.Index(line, prefix)
	if idx == -1 {
		return ""
	}
	start := idx + len(prefix)
	end := strings.Index(line[start:], "\"")
	if end == -1 {
		return ""
	}
	return line[start : start+end]
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
