package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRefreshDirsSupportsJapanesePaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "日本語のアルバム")
	child := filepath.Join(root, "ディスク一")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	a := &App{dirPath: root}
	a.refreshDirs()
	if a.status != "" {
		t.Fatalf("refresh failed: %s", a.status)
	}
	found := false
	for _, entry := range a.dirs {
		if entry.name == "ディスク一" && entry.path == child {
			found = true
		}
	}
	if !found {
		t.Fatalf("Japanese directory missing from entries: %+v", a.dirs)
	}
}
