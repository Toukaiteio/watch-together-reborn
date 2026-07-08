package chunk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChunkManagerParsesFMP4Playlist(t *testing.T) {
	tmpDir := t.TempDir()
	writeBytes(t, filepath.Join(tmpDir, "init.mp4"), []byte("init"))
	writeBytes(t, filepath.Join(tmpDir, "seg_00000.m4s"), []byte("seg0"))
	writeBytes(t, filepath.Join(tmpDir, "seg_00001.m4s"), []byte("seg1"))
	writeBytes(t, filepath.Join(tmpDir, "index.m3u8"), []byte(`#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:4
#EXT-X-PLAYLIST-TYPE:EVENT
#EXT-X-MAP:URI="init.mp4"
#EXTINF:4.000,
seg_00000.m4s
#EXTINF:2.500,
seg_00001.m4s
#EXT-X-ENDLIST
`))

	mgr, err := NewChunkManagerFromHLSDir(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewChunkManagerFromHLSDir: %v", err)
	}

	manifest := mgr.Manifest()
	if manifest == nil {
		t.Fatal("expected manifest")
	}
	if manifest.TotalChunks != 3 {
		t.Fatalf("TotalChunks = %d, want 3", manifest.TotalChunks)
	}
	if !manifest.Complete {
		t.Fatal("expected manifest to be complete")
	}
	if manifest.Chunks[0].IsInit != true {
		t.Fatal("expected first chunk to be init segment")
	}
	if manifest.Chunks[1].StartTime != 0 {
		t.Fatalf("first media start = %v, want 0", manifest.Chunks[1].StartTime)
	}
	if manifest.Chunks[2].StartTime != 4 {
		t.Fatalf("second media start = %v, want 4", manifest.Chunks[2].StartTime)
	}

	data, err := mgr.GetChunk(2)
	if err != nil {
		t.Fatalf("GetChunk(2): %v", err)
	}
	if string(data) != "seg1" {
		t.Fatalf("chunk payload = %q, want seg1", string(data))
	}
}

func TestChunkManagerRefreshesWhenPlaylistGrows(t *testing.T) {
	tmpDir := t.TempDir()
	writeBytes(t, filepath.Join(tmpDir, "init.mp4"), []byte("init"))
	writeBytes(t, filepath.Join(tmpDir, "seg_00000.m4s"), []byte("seg0"))
	writeBytes(t, filepath.Join(tmpDir, "index.m3u8"), []byte(`#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:4
#EXT-X-MAP:URI="init.mp4"
#EXTINF:4.000,
seg_00000.m4s
`))

	mgr, err := NewChunkManagerFromHLSDir(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewChunkManagerFromHLSDir: %v", err)
	}
	if got := mgr.Manifest().TotalChunks; got != 2 {
		t.Fatalf("initial TotalChunks = %d, want 2", got)
	}

	writeBytes(t, filepath.Join(tmpDir, "seg_00001.m4s"), []byte("seg1"))
	writeBytes(t, filepath.Join(tmpDir, "index.m3u8"), []byte(`#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:4
#EXT-X-MAP:URI="init.mp4"
#EXTINF:4.000,
seg_00000.m4s
#EXTINF:4.000,
seg_00001.m4s
#EXT-X-ENDLIST
`))

	manifest := mgr.Manifest()
	if manifest.TotalChunks != 3 {
		t.Fatalf("refreshed TotalChunks = %d, want 3", manifest.TotalChunks)
	}
	if !manifest.Complete {
		t.Fatal("expected refreshed manifest to be complete")
	}
}

func writeBytes(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
