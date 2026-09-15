package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wav-property-attacher/internal/domain"
)

func TestResolveAlbumDropSupportsJapaneseFolderAndWAV(t *testing.T) {
	root := filepath.Join(t.TempDir(), "日本語アルバム")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	wav := filepath.Join(root, "01 曲名.wav")
	if err := os.WriteFile(wav, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	if got, err := app.ResolveAlbumDrop(root); err != nil || got != root {
		t.Fatalf("folder drop: got=%q err=%v", got, err)
	}
	if got, err := app.ResolveAlbumDrop(wav); err != nil || got != root {
		t.Fatalf("WAV drop: got=%q err=%v", got, err)
	}
}

func TestResolveCoverDropRejectsNonImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cover.gif")
	if err := os.WriteFile(path, []byte("GIF89a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewApp().ResolveCoverDrop(path); err == nil {
		t.Fatal("expected unsupported cover extension to fail")
	}
}

func TestViewDoesNotExposeFingerprint(t *testing.T) {
	job := &domain.AlbumJob{
		Root:     "/album",
		Tracks:   []domain.LocalTrack{{Relative: "01.wav", Duration: 181 * time.Second, Fingerprint: "private-fingerprint"}},
		Selected: -1,
	}
	view := viewOf(job)
	if len(view.Tracks) != 1 || view.Tracks[0].Duration != "3:01" {
		t.Fatalf("view=%+v", view)
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private-fingerprint") {
		t.Fatal("fingerprint leaked into frontend view")
	}
}

func TestSetAssignment(t *testing.T) {
	app := NewApp()
	app.job = &domain.AlbumJob{
		Selected:    0,
		Candidates:  []domain.Release{{Tracks: []domain.ReleaseTrack{{Title: "Track 1"}}}},
		Assignments: []domain.TrackAssignment{{LocalIndex: 0, ReleaseIndex: -1}},
	}
	if err := app.SetAssignment(0, 0, false); err != nil {
		t.Fatal(err)
	}
	if app.job.Assignments[0].ReleaseIndex != 0 || app.job.Assignments[0].Excluded {
		t.Fatalf("assignment=%+v", app.job.Assignments[0])
	}
	if err := app.SetAssignment(0, -1, true); err != nil {
		t.Fatal(err)
	}
	if !app.job.Assignments[0].Excluded {
		t.Fatal("assignment was not excluded")
	}
}
