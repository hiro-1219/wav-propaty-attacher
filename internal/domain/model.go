package domain

import "time"

type AudioFormat struct {
	Channels      uint16
	SampleRate    uint32
	BitsPerSample uint16
	BlockAlign    uint16
	Frames        uint32
}

type LocalTrack struct {
	Path        string
	Relative    string
	DiscHint    int
	TrackHint   int
	Duration    time.Duration
	Format      AudioFormat
	AudioSHA256 string
	Fingerprint string
	RecordingID string
	Existing    map[string]string
}

type ArtistCredit struct {
	Name string `json:"name"`
}

type ReleaseTrack struct {
	Disc        int
	Position    int
	Number      string
	Title       string
	Artist      string
	RecordingID string
	LengthMS    int
}

type Release struct {
	ID             string
	ReleaseGroupID string
	Title          string
	AlbumArtist    string
	Date           string
	Country        string
	Tracks         []ReleaseTrack
	CoverURL       string
	Score          float64
}

type TrackAssignment struct {
	LocalIndex   int
	ReleaseIndex int
	Excluded     bool
}

type AlbumJob struct {
	Root        string
	Tracks      []LocalTrack
	Candidates  []Release
	Selected    int
	Assignments []TrackAssignment
	Cover       []byte
	CoverMIME   string
}

type ExportManifest struct {
	Version      int             `json:"version"`
	CreatedAt    time.Time       `json:"created_at"`
	AppVersion   string          `json:"app_version"`
	ReleaseID    string          `json:"release_id"`
	ReleaseGroup string          `json:"release_group_id,omitempty"`
	Album        string          `json:"album"`
	AlbumArtist  string          `json:"album_artist"`
	Files        []ManifestTrack `json:"files"`
}

type ManifestTrack struct {
	SourceRelative string `json:"source_relative"`
	SourceAudioSHA string `json:"source_audio_sha256"`
	OutputRelative string `json:"output_relative"`
	OutputAudioSHA string `json:"output_audio_sha256"`
	RecordingID    string `json:"recording_id,omitempty"`
	Title          string `json:"title"`
	Artist         string `json:"artist"`
	Disc           int    `json:"disc"`
	Track          int    `json:"track"`
}
