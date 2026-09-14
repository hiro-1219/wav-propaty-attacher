package ui

import (
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2/storage"
	fynetest "fyne.io/fyne/v2/test"
)

func TestNewBuildsFyneInterface(t *testing.T) {
	application := fynetest.NewApp()
	defer application.Quit()
	window := application.NewWindow("test")
	defer window.Close()

	ui := New(window)
	if window.Content() == nil || ui.searchButton == nil || ui.coverImage == nil {
		t.Fatal("Fyne interface was not fully constructed")
	}
	if ui.useFP.Checked {
		t.Fatal("fingerprint disclosure consent must default to disabled")
	}
}

func TestPathFromURISupportsJapanesePaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "日本語のアルバム", "ディスク一")
	uri := storage.NewFileURI(path)
	if got := pathFromURI(uri); got != path {
		t.Fatalf("path=%q, want %q", got, path)
	}
}
