package main

import (
	"strings"
	"testing"
)

func TestExtractSelectedFileIndex(t *testing.T) {
	clean, index := extractSelectedFileIndex("magnet:?xt=urn:btih:abc&dn=series&wt_file_index=7")
	if index == nil || *index != 7 {
		t.Fatalf("expected selected index 7, got %#v", index)
	}
	if strings.Contains(clean, "wt_file_index") {
		t.Fatalf("expected internal selection parameter to be removed, got %q", clean)
	}
	if !strings.Contains(clean, "xt=urn%3Abtih%3Aabc") {
		t.Fatalf("expected magnet metadata to be preserved, got %q", clean)
	}
}

func TestExtractSelectedFileIndexRejectsInvalidValue(t *testing.T) {
	_, index := extractSelectedFileIndex("magnet:?xt=urn:btih:abc&wt_file_index=-1")
	if index != nil {
		t.Fatalf("expected invalid index to be ignored, got %#v", index)
	}
}
