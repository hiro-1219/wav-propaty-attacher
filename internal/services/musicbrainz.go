package services

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"wav-property-attacher/internal/domain"
)

type MusicBrainz struct {
	BaseURL string
	Client  *RateClient
}

func NewMusicBrainz() *MusicBrainz {
	return &MusicBrainz{BaseURL: "https://musicbrainz.org/ws/2", Client: NewRateClient(time.Second, "WavPropertyAttacher/0.1 (https://github.com/hiro-1219/wav-property-attacher)")}
}

type mbArtist struct {
	Name       string `json:"name"`
	JoinPhrase string `json:"joinphrase"`
}
type mbTrack struct {
	Position     int        `json:"position"`
	Number       string     `json:"number"`
	Length       int        `json:"length"`
	Title        string     `json:"title"`
	ArtistCredit []mbArtist `json:"artist-credit"`
	Recording    struct {
		ID           string     `json:"id"`
		Title        string     `json:"title"`
		ArtistCredit []mbArtist `json:"artist-credit"`
	} `json:"recording"`
}
type mbMedium struct {
	Position int       `json:"position"`
	Tracks   []mbTrack `json:"tracks"`
}
type mbRelease struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	Date         string     `json:"date"`
	Country      string     `json:"country"`
	Score        int        `json:"score"`
	TrackCount   int        `json:"track-count"`
	ArtistCredit []mbArtist `json:"artist-credit"`
	Media        []mbMedium `json:"media"`
	ReleaseGroup struct {
		ID string `json:"id"`
	} `json:"release-group"`
}

type MusicBrainzSearchHit struct {
	ID          string
	Title       string
	Artist      string
	TrackCount  int
	SearchScore int
}

func (m *MusicBrainz) Search(ctx context.Context, query string, limit int) ([]string, error) {
	hits, err := m.SearchReleases(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(hits))
	for _, hit := range hits {
		ids = append(ids, hit.ID)
	}
	return ids, nil
}

func (m *MusicBrainz) SearchReleases(ctx context.Context, query string, limit int) ([]MusicBrainzSearchHit, error) {
	if limit <= 0 || limit > 25 {
		limit = 10
	}
	endpoint := m.BaseURL + "/release/?fmt=json&limit=" + strconv.Itoa(limit) + "&query=" + url.QueryEscape(query)
	var response struct {
		Releases []mbRelease `json:"releases"`
	}
	if err := m.Client.DoJSON(ctx, http.MethodGet, endpoint, nil, &response); err != nil {
		return nil, err
	}
	hits := make([]MusicBrainzSearchHit, 0, len(response.Releases))
	for _, release := range response.Releases {
		hits = append(hits, MusicBrainzSearchHit{ID: release.ID, Title: release.Title, Artist: credit(release.ArtistCredit), TrackCount: release.TrackCount, SearchScore: release.Score})
	}
	return hits, nil
}

func (m *MusicBrainz) ForRecording(ctx context.Context, recordingID string) ([]string, error) {
	endpoint := m.BaseURL + "/release?fmt=json&limit=100&recording=" + url.QueryEscape(recordingID)
	var response struct {
		Releases []mbRelease `json:"releases"`
	}
	if err := m.Client.DoJSON(ctx, http.MethodGet, endpoint, nil, &response); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(response.Releases))
	for _, release := range response.Releases {
		ids = append(ids, release.ID)
	}
	return ids, nil
}

func (m *MusicBrainz) Lookup(ctx context.Context, id string) (domain.Release, error) {
	endpoint := m.BaseURL + "/release/" + url.PathEscape(id) + "?fmt=json&inc=recordings+artist-credits+release-groups"
	var value mbRelease
	if err := m.Client.DoJSON(ctx, http.MethodGet, endpoint, nil, &value); err != nil {
		return domain.Release{}, err
	}
	release := domain.Release{ID: value.ID, ReleaseGroupID: value.ReleaseGroup.ID, Title: value.Title, AlbumArtist: credit(value.ArtistCredit), Date: value.Date, Country: value.Country}
	for _, medium := range value.Media {
		for _, track := range medium.Tracks {
			artist := credit(track.ArtistCredit)
			if artist == "" {
				artist = credit(track.Recording.ArtistCredit)
			}
			title := track.Title
			if title == "" {
				title = track.Recording.Title
			}
			release.Tracks = append(release.Tracks, domain.ReleaseTrack{Disc: medium.Position, Position: track.Position, Number: track.Number, Title: title, Artist: artist, RecordingID: track.Recording.ID, LengthMS: track.Length})
		}
	}
	return release, nil
}

func credit(parts []mbArtist) string {
	var b strings.Builder
	for _, part := range parts {
		b.WriteString(part.Name)
		b.WriteString(part.JoinPhrase)
	}
	return b.String()
}
