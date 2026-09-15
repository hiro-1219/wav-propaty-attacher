package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestAudioSourceUsesOpaqueLocalURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "日本語 曲.wav")
	app := NewApp()
	app.job = &domain.AlbumJob{Tracks: []domain.LocalTrack{{Path: path, Relative: "01 日本語 曲.wav", AudioSHA256: "1234567890abcdef-extra", Duration: 91 * time.Second}}}
	source, err := app.GetAudioSource(0)
	if err != nil {
		t.Fatal(err)
	}
	if source.Name != "01 日本語 曲.wav" || source.Duration != "1:31" || !strings.HasPrefix(source.URL, "/local-audio/0?v=1234567890abcdef") {
		t.Fatalf("source=%+v", source)
	}
	if strings.Contains(source.URL, filepath.ToSlash(path)) || strings.Contains(source.URL, "日本語") {
		t.Fatalf("local path leaked: %q", source.URL)
	}
}

func TestAudioMiddlewareServesOnlyCurrentTrackWithRange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "track.wav")
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.job = &domain.AlbumJob{Tracks: []domain.LocalTrack{{Path: path, AudioSHA256: "audio-version"}}}
	handler := app.audioMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }))
	req := httptest.NewRequest(http.MethodGet, "/local-audio/0?v=audio-version", nil)
	req.Header.Set("Range", "bytes=2-5")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	response := recorder.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusPartialContent || string(body) != "2345" || response.Header.Get("Content-Type") != "audio/wav" {
		t.Fatalf("status=%d type=%q body=%q", response.StatusCode, response.Header.Get("Content-Type"), body)
	}

	notFound := httptest.NewRecorder()
	handler.ServeHTTP(notFound, httptest.NewRequest(http.MethodGet, "/local-audio/1", nil))
	if notFound.Code != http.StatusNotFound {
		t.Fatalf("status=%d", notFound.Code)
	}
	stale := httptest.NewRecorder()
	handler.ServeHTTP(stale, httptest.NewRequest(http.MethodGet, "/local-audio/0?v=stale", nil))
	if stale.Code != http.StatusNotFound {
		t.Fatalf("stale status=%d", stale.Code)
	}
	passthrough := httptest.NewRecorder()
	handler.ServeHTTP(passthrough, httptest.NewRequest(http.MethodGet, "/style.css", nil))
	if passthrough.Code != http.StatusTeapot {
		t.Fatalf("passthrough status=%d", passthrough.Code)
	}
}
