package workflow

import (
	"testing"
	"time"

	"wav-property-attacher/internal/domain"
)

func TestScoreAndAssignPrefersRecordingIDs(t *testing.T) {
	locals := []domain.LocalTrack{{RecordingID: "r1", Duration: 180 * time.Second}, {RecordingID: "r2", Duration: 200 * time.Second}}
	release := domain.Release{Title: "Album", Tracks: []domain.ReleaseTrack{{RecordingID: "r1", LengthMS: 180000}, {RecordingID: "r2", LengthMS: 200000}}}
	score, assignments := ScoreAndAssign("Album", locals, release)
	if score < .9 {
		t.Fatalf("score=%f", score)
	}
	if assignments[0].ReleaseIndex != 0 || assignments[1].ReleaseIndex != 1 {
		t.Fatalf("assignments=%+v", assignments)
	}
}

func TestScoreWithoutFingerprintUsesDurationAndOrder(t *testing.T) {
	locals := []domain.LocalTrack{{Duration: 180 * time.Second}, {Duration: 200 * time.Second}}
	release := domain.Release{Title: "Album", Tracks: []domain.ReleaseTrack{{LengthMS: 180000}, {LengthMS: 200000}}}
	score, assignments := ScoreAndAssign("Album", locals, release)
	if score < .8 {
		t.Fatalf("score=%f", score)
	}
	if assignments[1].ReleaseIndex != 1 {
		t.Fatalf("assignments=%+v", assignments)
	}
}
