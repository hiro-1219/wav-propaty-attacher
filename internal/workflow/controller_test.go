package workflow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wav-property-attacher/internal/domain"
	"wav-property-attacher/internal/services"
)

func TestIdentifyRequiresKeyBeforeFingerprintWork(t *testing.T) {
	controller := NewController("", filepath.Join(t.TempDir(), "missing-fpcalc"))
	job := &domain.AlbumJob{Tracks: []domain.LocalTrack{{Path: "does-not-need-to-exist.wav"}}}
	err := controller.Identify(context.Background(), job, domain.SearchCriteria{AlbumTitle: "Album"}, true)
	if err == nil || !strings.Contains(err.Error(), "AcoustID") {
		t.Fatalf("expected AcoustID key validation error, got %v", err)
	}
}

func TestInferSearchCriteriaFromJapaneseFolder(t *testing.T) {
	tracks := []domain.LocalTrack{{Existing: map[string]string{}}, {Existing: map[string]string{}}}
	criteria := InferSearchCriteria(filepath.Join("library", "アーティスト - アルバム [2024]"), tracks)
	if criteria.Artist != "アーティスト" || criteria.AlbumTitle != "アルバム" || criteria.Year != "2024" {
		t.Fatalf("criteria=%+v", criteria)
	}
}

func TestInferSearchCriteriaPrefersEmbeddedTags(t *testing.T) {
	tracks := []domain.LocalTrack{
		{Existing: map[string]string{"album": "Tagged Album", "artist": "Tagged Artist"}},
		{Existing: map[string]string{"album": "Tagged Album", "artist": "Tagged Artist"}},
	}
	criteria := InferSearchCriteria(filepath.Join("library", "Wrong - Folder"), tracks)
	if criteria.AlbumTitle != "Tagged Album" || criteria.Artist != "Tagged Artist" {
		t.Fatalf("criteria=%+v", criteria)
	}
}

func TestMusicBrainzQueriesEscapeLuceneAndUseStructure(t *testing.T) {
	queries := musicBrainzQueries(domain.SearchCriteria{AlbumTitle: `A+B (Deluxe)`, Artist: `X/Y`, Year: "2020"}, 12)
	if len(queries) != 3 || !strings.Contains(queries[0], "tracks:12") || !strings.Contains(queries[0], `date:"2020"`) {
		t.Fatalf("queries=%v", queries)
	}
	if !strings.Contains(queries[2], `A\+B \(Deluxe\)`) || !strings.Contains(queries[2], `X\/Y`) {
		t.Fatalf("relaxed query=%q", queries[2])
	}
}

func TestSampleTrackIndicesCoversAlbum(t *testing.T) {
	tracks := make([]domain.LocalTrack, 10)
	indices := sampleTrackIndices(tracks, 3)
	if len(indices) != 3 || indices[0] != 0 || indices[len(indices)-1] != 9 {
		t.Fatalf("indices=%v", indices)
	}
}

func TestParseMusicBrainzReleaseID(t *testing.T) {
	want := "12345678-1234-1234-1234-123456789abc"
	got, err := ParseMusicBrainzReleaseID("https://musicbrainz.org/release/" + want + "?foo=bar")
	if err != nil || got != want {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if _, err := ParseMusicBrainzReleaseID("not-an-id"); err == nil {
		t.Fatal("expected invalid ID error")
	}
}

func TestStrongStructuredSearchStopsBeforeRelaxedQueries(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"releases":[{"id":"release-id","title":"Album","score":99,"track-count":2}]}`))
	}))
	defer server.Close()
	controller := NewController("", "")
	controller.MusicBrainz.BaseURL = server.URL
	controller.MusicBrainz.Client = services.NewRateClient(0, "test")
	evidence := map[string]*searchEvidence{}
	hits, err := controller.searchText(context.Background(), domain.SearchCriteria{AlbumTitle: "Album"}, 2, evidence)
	if err != nil || len(hits) != 1 || requests != 1 {
		t.Fatalf("hits=%v requests=%d err=%v", hits, requests, err)
	}
}

func TestFingerprintResultsAreCachedByAudioHash(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"score":0.9,"recordings":[{"id":"recording-id","releases":[{"id":"release-id"}]}]}]}`))
	}))
	defer server.Close()
	controller := NewController("key", "")
	controller.AcoustID.BaseURL = server.URL
	controller.AcoustID.Client = services.NewRateClient(0, "test")
	job := &domain.AlbumJob{Tracks: []domain.LocalTrack{{AudioSHA256: "audio-hash", Fingerprint: "fingerprint", Duration: time.Minute}}}
	for range 2 {
		controller.fingerprintTracks(context.Background(), job, []int{0}, map[int]bool{}, map[string]*searchEvidence{})
	}
	if requests != 1 {
		t.Fatalf("requests=%d", requests)
	}
}

func TestConfidentCandidateRequiresMargin(t *testing.T) {
	if confident([]domain.Release{{Score: .80}, {Score: .74}}) {
		t.Fatal("narrow margin must require user selection")
	}
	if !confident([]domain.Release{{Score: .80}, {Score: .60}}) {
		t.Fatal("clear leader should be selected")
	}
}

func TestCommitStageRestoresPreviousFilesOnFailure(t *testing.T) {
	root := t.TempDir()
	stage := filepath.Join(root, "stage")
	output := filepath.Join(root, "output")
	if err := os.MkdirAll(filepath.Join(stage, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(stage, "a.txt"), "new")
	write(filepath.Join(stage, "nested", "b.txt"), "new nested")
	write(filepath.Join(output, "a.txt"), "old")
	// This regular file prevents creation of output/nested/b.txt after a.txt
	// has already been replaced, forcing the rollback path.
	write(filepath.Join(output, "nested"), "directory collision")

	if err := commitStage(stage, output); err == nil {
		t.Fatal("expected staged commit to fail")
	}
	data, err := os.ReadFile(filepath.Join(output, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old" {
		t.Fatalf("a.txt was not restored: %q", data)
	}
}
