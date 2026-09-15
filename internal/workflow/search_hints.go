package workflow

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"wav-property-attacher/internal/domain"
)

var folderYearPattern = regexp.MustCompile(`(?:^|[\s\[(])(19\d{2}|20\d{2})(?:$|[\s\])])`)

func InferSearchCriteria(root string, tracks []domain.LocalTrack) domain.SearchCriteria {
	criteria := domain.SearchCriteria{
		AlbumTitle: consensusMetadata(tracks, "album"),
		Artist:     consensusMetadata(tracks, "artist"),
	}
	folder := strings.TrimSpace(filepath.Base(filepath.Clean(root)))
	if match := folderYearPattern.FindStringSubmatch(folder); len(match) > 1 {
		criteria.Year = match[1]
		folder = strings.Join(strings.Fields(folderYearPattern.ReplaceAllString(folder, " ")), " ")
	}
	if criteria.AlbumTitle == "" {
		criteria.AlbumTitle = folder
		if parts := strings.SplitN(folder, " - ", 2); len(parts) == 2 {
			if criteria.Artist == "" {
				criteria.Artist = strings.TrimSpace(parts[0])
			}
			criteria.AlbumTitle = strings.TrimSpace(parts[1])
		}
	}
	return criteria
}

func consensusMetadata(tracks []domain.LocalTrack, key string) string {
	counts := map[string]int{}
	canonical := map[string]string{}
	for _, track := range tracks {
		value := strings.TrimSpace(track.Existing[key])
		if value == "" {
			continue
		}
		normalized := strings.ToLower(value)
		counts[normalized]++
		canonical[normalized] = value
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	if len(keys) == 0 {
		return ""
	}
	return canonical[keys[0]]
}
