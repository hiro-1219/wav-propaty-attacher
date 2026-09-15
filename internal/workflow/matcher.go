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
	return ScoreAndAssignWithCriteria(domain.SearchCriteria{AlbumTitle: filepath.Base(root)}, locals, release)
}

func ScoreAndAssignWithCriteria(criteria domain.SearchCriteria, locals []domain.LocalTrack, release domain.Release) (float64, []domain.TrackAssignment) {
	assignments := align(locals, release.Tracks)
	matched, exact, durationScore, orderScore, titleScore, titled := 0, 0, 0.0, 0.0, 0.0, 0
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
		if local.TitleHint != "" {
			titleScore += tokenSimilarity(local.TitleHint, remote.Title)
			titled++
		}
		last = assignment.ReleaseIndex
	}
	denom := math.Max(1, float64(len(locals)))
	fingerprint := float64(exact) / denom
	duration := durationScore / denom
	count := float64(min(len(locals), len(release.Tracks))) / float64(max(1, max(len(locals), len(release.Tracks))))
	order := orderScore / denom
	trackTitles := 0.0
	if titled > 0 {
		trackTitles = titleScore / float64(titled)
	}
	albumText := tokenSimilarity(criteria.AlbumTitle, release.Title)
	artistText := tokenSimilarity(criteria.Artist, release.AlbumArtist)
	metadataText := 0.0
	if criteria.AlbumTitle != "" && criteria.Artist != "" {
		metadataText = .65*albumText + .35*artistText
	} else if criteria.AlbumTitle != "" {
		metadataText = albumText
	} else if criteria.Artist != "" {
		metadataText = artistText
	}
	score := .40*fingerprint + .25*duration + .15*count + .10*metadataText + .10*trackTitles
	if exact == 0 {
		score = .35*duration + .25*count + .20*trackTitles + .15*metadataText + .05*order
	}
	return score, assignments
}

func Rank(root string, locals []domain.LocalTrack, releases []domain.Release) ([]domain.Release, map[string][]domain.TrackAssignment) {
	return RankWithCriteria(domain.SearchCriteria{AlbumTitle: filepath.Base(root)}, locals, releases)
}

func RankWithCriteria(criteria domain.SearchCriteria, locals []domain.LocalTrack, releases []domain.Release) ([]domain.Release, map[string][]domain.TrackAssignment) {
	mapping := map[string][]domain.TrackAssignment{}
	for i := range releases {
		releases[i].Score, mapping[releases[i].ID] = ScoreAndAssignWithCriteria(criteria, locals, releases[i])
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
	normalize := func(s string) []rune {
		return []rune(strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return unicode.ToLower(r)
			}
			return -1
		}, s))
	}
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
	tokenScore := 0.0
	if union > 0 {
		tokenScore = float64(intersection) / float64(union)
	}
	aRunes, bRunes := normalize(a), normalize(b)
	if len(aRunes) == 0 || len(bRunes) == 0 {
		return tokenScore
	}
	if string(aRunes) == string(bRunes) {
		return 1
	}
	grams := func(value []rune) map[string]bool {
		out := map[string]bool{}
		if len(value) == 1 {
			out[string(value)] = true
			return out
		}
		for i := 0; i+1 < len(value); i++ {
			out[string(value[i:i+2])] = true
		}
		return out
	}
	ag, bg := grams(aRunes), grams(bRunes)
	gramUnion, gramIntersection := len(ag), 0
	for value := range bg {
		if ag[value] {
			gramIntersection++
		} else {
			gramUnion++
		}
	}
	gramScore := float64(gramIntersection) / float64(max(1, gramUnion))
	return math.Max(tokenScore, gramScore)
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
