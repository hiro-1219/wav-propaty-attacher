package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"wav-property-attacher/internal/audio"
	"wav-property-attacher/internal/domain"
	"wav-property-attacher/internal/metadata"
	"wav-property-attacher/internal/services"
)

const AppVersion = "0.1.0"

type Controller struct {
	Fingerprinter services.FPCalc
	AcoustID      *services.AcoustID
	MusicBrainz   *services.MusicBrainz
	CoverArt      *services.CoverArt
	Progress      func(string)
	AcoustIDCache map[string][]services.AcoustIDMatch
}

func NewController(apiKey, fpcalcPath string) *Controller {
	return &Controller{
		Fingerprinter: services.FPCalc{Path: fpcalcPath},
		AcoustID:      services.NewAcoustID(apiKey),
		MusicBrainz:   services.NewMusicBrainz(),
		CoverArt:      services.NewCoverArt(),
		AcoustIDCache: map[string][]services.AcoustIDMatch{},
	}
}

func (c *Controller) Scan(_ context.Context, root string) (*domain.AlbumJob, error) {
	tracks, err := audio.ScanAlbum(strings.TrimSpace(root))
	if err != nil {
		return nil, err
	}
	return &domain.AlbumJob{Root: root, Tracks: tracks, Selected: -1, Suggested: InferSearchCriteria(root, tracks)}, nil
}

type searchEvidence struct {
	textScore         int
	fingerprintHits   int
	fingerprintWeight float64
	labels            map[string]bool
}

func (c *Controller) Identify(ctx context.Context, job *domain.AlbumJob, criteria domain.SearchCriteria, useFingerprint bool) error {
	if job == nil || len(job.Tracks) == 0 {
		return errors.New("先にWAVフォルダを読み込んでください")
	}
	criteria = fillCriteria(criteria, job.Suggested)
	if criteria.AlbumTitle == "" {
		criteria.AlbumTitle = filepath.Base(filepath.Clean(job.Root))
	}
	if useFingerprint && strings.TrimSpace(c.AcoustID.APIKey) == "" {
		return errors.New("音響指紋検索にはAcoustIDアプリケーションキーが必要です。キーを入力するか、指紋送信を無効にしてください")
	}

	evidence := map[string]*searchEvidence{}
	job.Warnings = nil
	hits, textErr := c.searchText(ctx, criteria, len(job.Tracks), evidence)
	if textErr != nil {
		addWarning(job, "MusicBrainzの一部の文字検索に失敗しました")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	releases := c.lookupHits(ctx, hits, 8, nil)
	if err := ctx.Err(); err != nil {
		return err
	}
	releases = rankAndDecorate(criteria, job.Tracks, releases, evidence)
	if ambiguous(releases, .72, .08) && len(hits) > 8 {
		releases = c.lookupHits(ctx, hits, 12, releases)
		releases = rankAndDecorate(criteria, job.Tracks, releases, evidence)
	}

	if useFingerprint && ambiguous(releases, .72, .08) {
		processed := map[int]bool{}
		c.fingerprintTracks(ctx, job, sampleTrackIndices(job.Tracks, 3), processed, evidence)
		if err := ctx.Err(); err != nil {
			return err
		}
		releases = c.lookupEvidence(ctx, releases, evidence, 16)
		releases = rankAndDecorate(criteria, job.Tracks, releases, evidence)
		if ambiguous(releases, .65, .05) {
			c.fingerprintTracks(ctx, job, sampleTrackIndices(job.Tracks, 7), processed, evidence)
			if err := ctx.Err(); err != nil {
				return err
			}
			releases = c.lookupEvidence(ctx, releases, evidence, 16)
			releases = rankAndDecorate(criteria, job.Tracks, releases, evidence)
		}
	}
	if len(releases) == 0 {
		if textErr != nil {
			return fmt.Errorf("MusicBrainz検索に失敗しました: %w", textErr)
		}
		return errors.New("候補が見つかりませんでした。検索条件を修正するか、MusicBrainz URL/MBIDを指定してください")
	}

	job.Candidates = releases
	job.Selected = -1
	job.Assignments = nil
	if confident(releases) {
		return c.SelectCandidate(ctx, job, 0)
	}
	return nil
}

func fillCriteria(value, fallback domain.SearchCriteria) domain.SearchCriteria {
	if strings.TrimSpace(value.AlbumTitle) == "" {
		value.AlbumTitle = fallback.AlbumTitle
	}
	if strings.TrimSpace(value.Artist) == "" {
		value.Artist = fallback.Artist
	}
	if strings.TrimSpace(value.Year) == "" {
		value.Year = fallback.Year
	}
	value.AlbumTitle = strings.TrimSpace(value.AlbumTitle)
	value.Artist = strings.TrimSpace(value.Artist)
	value.Year = strings.TrimSpace(value.Year)
	return value
}

func (c *Controller) searchText(ctx context.Context, criteria domain.SearchCriteria, trackCount int, evidence map[string]*searchEvidence) ([]services.MusicBrainzSearchHit, error) {
	queries := musicBrainzQueries(criteria, trackCount)
	byID := map[string]services.MusicBrainzSearchHit{}
	var lastErr error
	for index, query := range queries {
		c.report(fmt.Sprintf("検索中: MusicBrainz文字検索（%d/%d）", index+1, len(queries)))
		hits, err := c.MusicBrainz.SearchReleases(ctx, query, 25)
		if err != nil {
			lastErr = err
			continue
		}
		for _, hit := range hits {
			if previous, ok := byID[hit.ID]; !ok || hit.SearchScore > previous.SearchScore {
				byID[hit.ID] = hit
			}
			ev := ensureEvidence(evidence, hit.ID)
			if hit.SearchScore > ev.textScore {
				ev.textScore = hit.SearchScore
			}
			ev.labels["文字一致"] = true
			if hit.TrackCount == trackCount {
				ev.labels["曲数一致"] = true
			}
		}
		if index == 0 && strongSearchHits(hits, trackCount) {
			break
		}
	}
	result := make([]services.MusicBrainzSearchHit, 0, len(byID))
	for _, hit := range byID {
		result = append(result, hit)
	}
	sort.SliceStable(result, func(i, j int) bool {
		iScore, jScore := result[i].SearchScore, result[j].SearchScore
		if result[i].TrackCount == trackCount {
			iScore += 15
		}
		if result[j].TrackCount == trackCount {
			jScore += 15
		}
		return iScore > jScore
	})
	return result, lastErr
}

func (c *Controller) lookupHits(ctx context.Context, hits []services.MusicBrainzSearchHit, limit int, existing []domain.Release) []domain.Release {
	seen := map[string]bool{}
	for _, release := range existing {
		seen[release.ID] = true
	}
	out := append([]domain.Release(nil), existing...)
	if limit > len(hits) {
		limit = len(hits)
	}
	for i := 0; i < limit; i++ {
		if seen[hits[i].ID] {
			continue
		}
		c.report(fmt.Sprintf("検索中: 候補詳細を取得しています（%d/%d）", i+1, limit))
		release, err := c.MusicBrainz.Lookup(ctx, hits[i].ID)
		if err == nil && len(release.Tracks) > 0 {
			out = append(out, release)
			seen[release.ID] = true
		}
	}
	return out
}

func (c *Controller) fingerprintTracks(ctx context.Context, job *domain.AlbumJob, indices []int, processed map[int]bool, evidence map[string]*searchEvidence) {
	for _, index := range indices {
		if processed[index] {
			continue
		}
		processed[index] = true
		if err := ctx.Err(); err != nil {
			return
		}
		track := &job.Tracks[index]
		c.report(fmt.Sprintf("検索中: 音響指紋を照合しています（%d/%d） %s", len(processed), len(indices), track.Relative))
		if track.Fingerprint == "" {
			fp, err := c.Fingerprinter.Calculate(ctx, track.Path)
			if err != nil {
				addWarning(job, "一部の曲で音響指紋を生成できませんでした")
				continue
			}
			track.Fingerprint = fp.Fingerprint
		}
		matches, cached := c.AcoustIDCache[track.AudioSHA256]
		if !cached {
			var err error
			matches, err = c.AcoustID.Lookup(ctx, track.Fingerprint, int(track.Duration.Seconds()+.5))
			if err != nil {
				addWarning(job, "一部の曲でAcoustID照合に失敗しました")
				continue
			}
			c.AcoustIDCache[track.AudioSHA256] = append([]services.AcoustIDMatch(nil), matches...)
		}
		sort.SliceStable(matches, func(i, j int) bool { return matches[i].Score > matches[j].Score })
		accepted := 0
		matchedReleases := map[string]bool{}
		for _, match := range matches {
			if match.Score < .45 || accepted >= 3 {
				continue
			}
			accepted++
			if accepted == 1 {
				track.RecordingID = match.RecordingID
			}
			ids := match.ReleaseIDs
			if len(ids) == 0 && match.RecordingID != "" {
				ids, _ = c.MusicBrainz.ForRecording(ctx, match.RecordingID)
			}
			for _, id := range ids {
				if matchedReleases[id] {
					continue
				}
				matchedReleases[id] = true
				ev := ensureEvidence(evidence, id)
				ev.fingerprintHits++
				ev.fingerprintWeight += match.Score
				ev.labels["指紋一致"] = true
			}
		}
	}
}

func addWarning(job *domain.AlbumJob, message string) {
	for _, existing := range job.Warnings {
		if existing == message {
			return
		}
	}
	job.Warnings = append(job.Warnings, message)
}

func (c *Controller) lookupEvidence(ctx context.Context, releases []domain.Release, evidence map[string]*searchEvidence, limit int) []domain.Release {
	seen := map[string]bool{}
	for _, release := range releases {
		seen[release.ID] = true
	}
	type weighted struct {
		id     string
		hits   int
		weight float64
	}
	var ids []weighted
	for id, ev := range evidence {
		if !seen[id] && ev.fingerprintHits > 0 {
			ids = append(ids, weighted{id, ev.fingerprintHits, ev.fingerprintWeight})
		}
	}
	sort.SliceStable(ids, func(i, j int) bool {
		if ids[i].hits != ids[j].hits {
			return ids[i].hits > ids[j].hits
		}
		if ids[i].weight != ids[j].weight {
			return ids[i].weight > ids[j].weight
		}
		return ids[i].id < ids[j].id
	})
	for _, item := range ids {
		if len(releases) >= limit {
			break
		}
		release, err := c.MusicBrainz.Lookup(ctx, item.id)
		if err == nil && len(release.Tracks) > 0 {
			releases = append(releases, release)
		}
	}
	return releases
}

func ensureEvidence(all map[string]*searchEvidence, id string) *searchEvidence {
	if all[id] == nil {
		all[id] = &searchEvidence{labels: map[string]bool{}}
	}
	return all[id]
}

func rankAndDecorate(criteria domain.SearchCriteria, tracks []domain.LocalTrack, releases []domain.Release, evidence map[string]*searchEvidence) []domain.Release {
	releases, _ = RankWithCriteria(criteria, tracks, releases)
	for i := range releases {
		ev := evidence[releases[i].ID]
		releases[i].Evidence = nil
		releases[i].FingerprintHits = 0
		if ev == nil {
			continue
		}
		releases[i].FingerprintHits = ev.fingerprintHits
		for label := range ev.labels {
			releases[i].Evidence = append(releases[i].Evidence, label)
		}
		sort.Strings(releases[i].Evidence)
	}
	return releases
}

func ambiguous(releases []domain.Release, threshold, margin float64) bool {
	if len(releases) == 0 || releases[0].Score < threshold {
		return true
	}
	return len(releases) > 1 && releases[0].Score-releases[1].Score < margin
}

func confident(releases []domain.Release) bool {
	if len(releases) == 0 || releases[0].Score < .75 {
		return false
	}
	return len(releases) == 1 || releases[0].Score-releases[1].Score >= .10
}

func strongSearchHits(hits []services.MusicBrainzSearchHit, trackCount int) bool {
	return len(hits) > 0 && hits[0].SearchScore >= 90 && hits[0].TrackCount == trackCount
}

func sampleTrackIndices(tracks []domain.LocalTrack, maximum int) []int {
	if maximum <= 0 || len(tracks) == 0 {
		return nil
	}
	if maximum > len(tracks) {
		maximum = len(tracks)
	}
	seen := map[int]bool{}
	var result []int
	add := func(index int) {
		if !seen[index] {
			seen[index] = true
			result = append(result, index)
		}
	}
	discs := map[int]bool{}
	for i, track := range tracks {
		if !discs[track.DiscHint] && len(result) < maximum {
			discs[track.DiscHint] = true
			add(i)
		}
	}
	if maximum == 1 {
		add(0)
	} else {
		for i := 0; i < maximum; i++ {
			add(i * (len(tracks) - 1) / (maximum - 1))
		}
	}
	for i := 0; len(result) < maximum && i < len(tracks); i++ {
		add(i)
	}
	return result[:maximum]
}

func musicBrainzQueries(criteria domain.SearchCriteria, trackCount int) []string {
	strict := []string{"release:" + quoteLucene(criteria.AlbumTitle)}
	if criteria.Artist != "" {
		strict = append(strict, "artist:"+quoteLucene(criteria.Artist))
	}
	if trackCount > 0 {
		strict = append(strict, "tracks:"+strconv.Itoa(trackCount))
	}
	if criteria.Year != "" {
		strict = append(strict, "date:"+quoteLucene(criteria.Year))
	}
	queries := []string{strings.Join(strict, " AND ")}
	withoutStructure := []string{"release:" + quoteLucene(criteria.AlbumTitle)}
	if criteria.Artist != "" {
		withoutStructure = append(withoutStructure, "artist:"+quoteLucene(criteria.Artist))
	}
	queries = append(queries, strings.Join(withoutStructure, " AND "))
	queries = append(queries, "release:("+escapeLucene(criteria.AlbumTitle)+")"+func() string {
		if criteria.Artist != "" {
			return " AND artist:(" + escapeLucene(criteria.Artist) + ")"
		}
		return ""
	}())
	return uniqueStrings(queries)
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func (c *Controller) report(message string) {
	if c.Progress != nil {
		c.Progress(message)
	}
}

func (c *Controller) SelectCandidate(ctx context.Context, job *domain.AlbumJob, index int) error {
	if index < 0 || index >= len(job.Candidates) {
		return errors.New("候補番号が不正です")
	}
	job.Selected = index
	_, job.Assignments = ScoreAndAssign(job.Root, job.Tracks, job.Candidates[index])
	cover, mime, err := c.CoverArt.Front(ctx, job.Candidates[index].ID)
	if err == nil {
		if normalized, normalizedMIME, normalizeErr := metadata.NormalizeCover(cover, mime); normalizeErr == nil {
			job.Cover, job.CoverMIME = normalized, normalizedMIME
		}
	}
	return nil
}

var musicBrainzReleaseIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func ParseMusicBrainzReleaseID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if marker := strings.Index(strings.ToLower(value), "/release/"); marker >= 0 {
		value = value[marker+len("/release/"):]
		if end := strings.IndexAny(value, "/?#"); end >= 0 {
			value = value[:end]
		}
	}
	if !musicBrainzReleaseIDPattern.MatchString(value) {
		return "", errors.New("MusicBrainzのRelease URLまたはRelease MBIDを指定してください")
	}
	return strings.ToLower(value), nil
}

func (c *Controller) LookupDirect(ctx context.Context, job *domain.AlbumJob, value string) error {
	if job == nil || len(job.Tracks) == 0 {
		return errors.New("先にWAVフォルダを読み込んでください")
	}
	id, err := ParseMusicBrainzReleaseID(value)
	if err != nil {
		return err
	}
	c.report("MusicBrainzから指定Releaseを取得しています…")
	release, err := c.MusicBrainz.Lookup(ctx, id)
	if err != nil {
		return err
	}
	if len(release.Tracks) == 0 {
		return errors.New("指定Releaseに曲情報がありません")
	}
	release.Evidence = []string{"MBID直接指定"}
	release.Score, _ = ScoreAndAssignWithCriteria(job.Suggested, job.Tracks, release)
	index := -1
	for i := range job.Candidates {
		if job.Candidates[i].ID == id {
			job.Candidates[i] = release
			index = i
			break
		}
	}
	if index < 0 {
		job.Candidates = append([]domain.Release{release}, job.Candidates...)
		index = 0
	}
	return c.SelectCandidate(ctx, job, index)
}

func (c *Controller) LoadLocalCover(path string, job *domain.AlbumJob) error {
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return err
	}
	if len(data) > 10<<20 {
		return errors.New("画像は10 MiB以下にしてください")
	}
	mime := detectImage(data)
	if mime == "" {
		return errors.New("JPEGまたはPNGを指定してください")
	}
	normalized, normalizedMIME, err := metadata.NormalizeCover(data, mime)
	if err != nil {
		return err
	}
	job.Cover, job.CoverMIME = normalized, normalizedMIME
	return nil
}

func (c *Controller) Export(ctx context.Context, job *domain.AlbumJob) (string, error) {
	if job == nil || job.Selected < 0 || job.Selected >= len(job.Candidates) {
		return "", errors.New("アルバム候補が選択されていません")
	}
	release := job.Candidates[job.Selected]
	if len(job.Assignments) != len(job.Tracks) {
		return "", errors.New("曲の割当が完了していません")
	}
	outputRoot := filepath.Join(job.Root, "rekordbox")
	if err := os.MkdirAll(outputRoot, 0o755); err != nil {
		return "", err
	}
	existing, _ := readManifest(filepath.Join(outputRoot, ".wav-property-attacher.json"))
	existingOutputs := map[string]domain.ManifestTrack{}
	for _, file := range existing.Files {
		existingOutputs[filepath.Clean(file.OutputRelative)] = file
	}

	stage, err := os.MkdirTemp(outputRoot, ".wpa-staging-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	manifest := domain.ExportManifest{Version: 1, CreatedAt: time.Now().UTC(), AppVersion: AppVersion, ReleaseID: release.ID, ReleaseGroup: release.ReleaseGroupID, Album: release.Title, AlbumArtist: release.AlbumArtist}
	discTotals := map[int]int{}
	maxDisc := 1
	for _, track := range release.Tracks {
		discTotals[track.Disc]++
		if track.Disc > maxDisc {
			maxDisc = track.Disc
		}
	}
	for _, assignment := range job.Assignments {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if assignment.Excluded {
			continue
		}
		if assignment.ReleaseIndex < 0 || assignment.ReleaseIndex >= len(release.Tracks) {
			return "", fmt.Errorf("%sの割当がありません", job.Tracks[assignment.LocalIndex].Relative)
		}
		local, remote := job.Tracks[assignment.LocalIndex], release.Tracks[assignment.ReleaseIndex]
		outputRel := strings.TrimSuffix(local.Relative, filepath.Ext(local.Relative)) + ".aiff"
		finalPath := filepath.Join(outputRoot, outputRel)
		if _, err := os.Stat(finalPath); err == nil {
			previous, known := existingOutputs[filepath.Clean(outputRel)]
			if !known || filepath.Clean(previous.SourceRelative) != filepath.Clean(local.Relative) || previous.SourceAudioSHA != local.AudioSHA256 {
				return "", fmt.Errorf("由来を確認できない既存ファイルがあります: %s", outputRel)
			}
		}
		tag := metadata.Tag{Title: remote.Title, Artist: remote.Artist, AlbumArtist: release.AlbumArtist, Album: release.Title, Track: fmt.Sprintf("%d/%d", remote.Position, discTotals[remote.Disc]), Disc: fmt.Sprintf("%d/%d", remote.Disc, maxDisc), Year: firstYear(release.Date), ReleaseDate: release.Date, RecordingID: remote.RecordingID, ReleaseID: release.ID, ReleaseGroupID: release.ReleaseGroupID, Picture: metadata.Picture{MIME: job.CoverMIME, Data: job.Cover}}
		stagePath := filepath.Join(stage, outputRel)
		hash, err := audio.ExportAIFF(local.Path, stagePath, tag)
		if err != nil {
			return "", fmt.Errorf("%s: %w", local.Relative, err)
		}
		manifest.Files = append(manifest.Files, domain.ManifestTrack{SourceRelative: local.Relative, SourceAudioSHA: local.AudioSHA256, OutputRelative: outputRel, OutputAudioSHA: hash, RecordingID: remote.RecordingID, Title: remote.Title, Artist: remote.Artist, Disc: remote.Disc, Track: remote.Position})
	}
	if len(job.Cover) > 0 {
		ext := ".jpg"
		if job.CoverMIME == "image/png" {
			ext = ".png"
		}
		if err := os.WriteFile(filepath.Join(stage, "cover"+ext), job.Cover, 0o644); err != nil {
			return "", err
		}
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(stage, ".wav-property-attacher.json"), manifestData, 0o644); err != nil {
		return "", err
	}
	if err := commitStage(stage, outputRoot); err != nil {
		return "", err
	}
	return outputRoot, nil
}

func commitStage(stage, outputRoot string) error {
	var files []string
	err := filepath.WalkDir(stage, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, e := filepath.Rel(stage, path)
			if e != nil {
				return e
			}
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		return err
	}
	rollback := filepath.Join(stage, ".rollback")
	moved := []string{}
	backedUp := map[string]bool{}
	restore := func() {
		for i := len(moved) - 1; i >= 0; i-- {
			_ = os.Remove(filepath.Join(outputRoot, moved[i]))
		}
		for rel := range backedUp {
			old := filepath.Join(rollback, rel)
			if _, err := os.Stat(old); err == nil {
				_ = os.MkdirAll(filepath.Dir(filepath.Join(outputRoot, rel)), 0o755)
				_ = os.Rename(old, filepath.Join(outputRoot, rel))
			}
		}
	}
	for _, rel := range files {
		src, dst := filepath.Join(stage, rel), filepath.Join(outputRoot, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			restore()
			return err
		}
		if _, err := os.Stat(dst); err == nil {
			old := filepath.Join(rollback, rel)
			_ = os.MkdirAll(filepath.Dir(old), 0o755)
			if err := os.Rename(dst, old); err != nil {
				restore()
				return err
			}
			backedUp[rel] = true
		}
		if err := os.Rename(src, dst); err != nil {
			restore()
			return err
		}
		moved = append(moved, rel)
	}
	return nil
}

func readManifest(path string) (domain.ExportManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.ExportManifest{}, err
	}
	var m domain.ExportManifest
	err = json.Unmarshal(data, &m)
	return m, err
}
func detectImage(data []byte) string {
	if len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
		return "image/jpeg"
	}
	if len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n" {
		return "image/png"
	}
	return ""
}
func firstYear(date string) string {
	if len(date) >= 4 {
		if _, err := strconv.Atoi(date[:4]); err == nil {
			return date[:4]
		}
	}
	return ""
}
func quoteLucene(value string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`) + `"`
}
func escapeLucene(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `+`, `\+`, `-`, `\-`, `&&`, `\&&`, `||`, `\||`, `!`, `\!`, `(`, `\(`, `)`, `\)`, `{`, `\{`, `}`, `\}`, `[`, `\[`, `]`, `\]`, `^`, `\^`, `"`, `\"`, `~`, `\~`, `*`, `\*`, `?`, `\?`, `:`, `\:`, `/`, `\/`)
	return strings.TrimSpace(replacer.Replace(value))
}
