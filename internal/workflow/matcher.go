package workflow

import (
	"math"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"wav-property-attacher/internal/domain"
)

func ScoreAndAssign(root string, locals []domain.LocalTrack, release domain.Release) (float64, []domain.TrackAssignment) {
	assignments := align(locals, release.Tracks)
	matched, exact, durationScore, orderScore := 0, 0, 0.0, 0.0
	last := -1
	for _, assignment := range assignments {
		if assignment.ReleaseIndex < 0 {
			continue
		}
		matched++
		local, remote := locals[assignment.LocalIndex], release.Tracks[assignment.ReleaseIndex]
		if local.RecordingID != "" && local.RecordingID == remote.RecordingID {
			exact++
		}
		delta := math.Abs(float64(local.Duration.Milliseconds()-int64(remote.LengthMS))) / 1000
		durationScore += math.Max(0, 1-delta/10)
		if assignment.ReleaseIndex > last {
			orderScore++
		}
		last = assignment.ReleaseIndex
	}
	denom := math.Max(1, float64(len(locals)))
	fingerprint := float64(exact) / denom
	duration := durationScore / denom
	count := float64(min(len(locals), len(release.Tracks))) / float64(max(1, max(len(locals), len(release.Tracks))))
	order := orderScore / denom
	text := tokenSimilarity(filepath.Base(root), release.Title)
	score := .50*fingerprint + .20*duration + .15*count + .10*order + .05*text
	if exact == 0 {
		score = .45*duration + .30*count + .15*order + .10*text
	}
	return score, assignments
}

func Rank(root string, locals []domain.LocalTrack, releases []domain.Release) ([]domain.Release, map[string][]domain.TrackAssignment) {
	mapping := map[string][]domain.TrackAssignment{}
	for i := range releases {
		releases[i].Score, mapping[releases[i].ID] = ScoreAndAssign(root, locals, releases[i])
	}
	sort.SliceStable(releases, func(i, j int) bool { return releases[i].Score > releases[j].Score })
	return releases, mapping
}

func align(locals []domain.LocalTrack, remote []domain.ReleaseTrack) []domain.TrackAssignment {
	n, m := len(locals), len(remote)
	dp := make([][]float64, n+1)
	action := make([][]byte, n+1)
	for i := range dp {
		dp[i] = make([]float64, m+1)
		action[i] = make([]byte, m+1)
	}
	for i := 1; i <= n; i++ {
		dp[i][0] = float64(i)
		action[i][0] = 'L'
	}
	for j := 1; j <= m; j++ {
		dp[0][j] = float64(j)
		action[0][j] = 'R'
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			l, r := locals[i-1], remote[j-1]
			cost := math.Min(2, math.Abs(float64(l.Duration.Milliseconds()-int64(r.LengthMS)))/10000)
			if l.RecordingID != "" {
				if l.RecordingID == r.RecordingID {
					cost = 0
				} else {
					cost = 2
				}
			}
			best, act := dp[i-1][j-1]+cost, byte('M')
			if v := dp[i-1][j] + 1; v < best {
				best, act = v, 'L'
			}
			if v := dp[i][j-1] + 1; v < best {
				best, act = v, 'R'
			}
			dp[i][j], action[i][j] = best, act
		}
	}
	result := make([]domain.TrackAssignment, n)
	for i := range result {
		result[i] = domain.TrackAssignment{LocalIndex: i, ReleaseIndex: -1}
	}
	for i, j := n, m; i > 0 || j > 0; {
		switch action[i][j] {
		case 'M':
			result[i-1].ReleaseIndex = j - 1
			i--
			j--
		case 'L':
			i--
		default:
			j--
		}
	}
	return result
}

func tokenSimilarity(a, b string) float64 {
	tokens := func(s string) map[string]bool {
		fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
		out := map[string]bool{}
		for _, f := range fields {
			if f != "" {
				out[f] = true
			}
		}
		return out
	}
	aa, bb := tokens(a), tokens(b)
	union, intersection := 0, 0
	for k := range aa {
		union++
		if bb[k] {
			intersection++
		}
	}
	for k := range bb {
		if !aa[k] {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
