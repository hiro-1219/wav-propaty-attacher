package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAcoustIDLookupSendsFingerprintNotAudio(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method=%s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.Form.Get("fingerprint"); got != "encoded-fingerprint" {
			t.Errorf("fingerprint=%q", got)
		}
		if got := r.Form.Get("duration"); got != "181" {
			t.Errorf("duration=%q", got)
		}
		for key := range r.Form {
			switch key {
			case "client", "format", "meta", "duration", "fingerprint":
			default:
				t.Errorf("unexpected request field %q", key)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","results":[{"score":0.98,"recordings":[{"id":"recording-id"}]}]}`))
	}))
	defer server.Close()

	client := NewAcoustID("application-key")
	client.BaseURL = server.URL
	client.Client = NewRateClient(0, "test")
	matches, err := client.Lookup(context.Background(), "encoded-fingerprint", 181)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].RecordingID != "recording-id" || matches[0].Score != 0.98 {
		t.Fatalf("matches=%+v", matches)
	}
}

func TestMusicBrainzLookupPreservesArtistJoinPhrase(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "release-id", "title": "Album", "date": "2024-05-03",
			"artist-credit": []map[string]any{{"name": "Alice", "joinphrase": " & "}, {"name": "Bob", "joinphrase": ""}},
			"release-group": map[string]any{"id": "group-id"},
			"media": []map[string]any{{"position": 1, "tracks": []map[string]any{{
				"position": 1, "number": "1", "length": 180000, "title": "Track",
				"artist-credit": []map[string]any{{"name": "Alice", "joinphrase": " feat. "}, {"name": "Carol", "joinphrase": ""}},
				"recording":     map[string]any{"id": "recording-id", "title": "Track"},
			}}}},
		})
	}))
	defer server.Close()

	client := NewMusicBrainz()
	client.BaseURL = server.URL
	client.Client = NewRateClient(0, "test")
	release, err := client.Lookup(context.Background(), "release-id")
	if err != nil {
		t.Fatal(err)
	}
	if release.AlbumArtist != "Alice & Bob" || len(release.Tracks) != 1 || release.Tracks[0].Artist != "Alice feat. Carol" {
		t.Fatalf("release=%+v", release)
	}
}
