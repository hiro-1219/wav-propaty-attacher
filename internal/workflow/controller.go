package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
}

func NewController(apiKey, fpcalcPath string) *Controller {
	return &Controller{
		Fingerprinter: services.FPCalc{Path: fpcalcPath},
		AcoustID:      services.NewAcoustID(apiKey),
		MusicBrainz:   services.NewMusicBrainz(),
		CoverArt:      services.NewCoverArt(),
	}
}

func (c *Controller) Scan(_ context.Context, root string) (*domain.AlbumJob, error) {
	tracks, err := audio.ScanAlbum(strings.TrimSpace(root))
	if err != nil {
		return nil, err
	}
	return &domain.AlbumJob{Root: root, Tracks: tracks, Selected: -1}, nil
}

func (c *Controller) Identify(ctx context.Context, job *domain.AlbumJob, query string, useFingerprint bool) error {
	if job == nil || len(job.Tracks) == 0 {
		return errors.New("先にWAVフォルダを読み込んでください")
	}
	if useFingerprint && strings.TrimSpace(c.AcoustID.APIKey) == "" {
		return errors.New("音響指紋検索にはAcoustIDアプリケーションキーが必要です。キーを入力するか、指紋送信を無効にしてください")
	}
	releaseVotes := map[string]int{}
	if useFingerprint {
		seenRecordings := map[string]bool{}
		for i := range job.Tracks {
			if err := ctx.Err(); err != nil {
				return err
			}
			c.report(fmt.Sprintf("検索中: 音響指紋を生成しています（%d/%d） %s", i+1, len(job.Tracks), job.Tracks[i].Relative))
			fp, err := c.Fingerprinter.Calculate(ctx, job.Tracks[i].Path)
			if err != nil {
				continue
			}
			job.Tracks[i].Fingerprint = fp.Fingerprint
			matches, err := c.AcoustID.Lookup(ctx, fp.Fingerprint, int(fp.Duration+0.5))
			if err != nil || len(matches) == 0 {
				continue
			}
			sort.Slice(matches, func(a, b int) bool { return matches[a].Score > matches[b].Score })
			if matches[0].Score < .5 {
				continue
			}
			job.Tracks[i].RecordingID = matches[0].RecordingID
			if seenRecordings[matches[0].RecordingID] {
				continue
			}
			seenRecordings[matches[0].RecordingID] = true
			ids, err := c.MusicBrainz.ForRecording(ctx, matches[0].RecordingID)
			if err != nil {
				continue
			}
			for _, id := range ids {
				releaseVotes[id]++
			}
			if len(seenRecordings) >= 5 {
				break
			}
		}
	}

	var ids []string
	for id := range releaseVotes {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return releaseVotes[ids[i]] > releaseVotes[ids[j]] })
	if len(ids) > 10 {
		ids = ids[:10]
	}
	if len(ids) == 0 {
		if strings.TrimSpace(query) == "" {
			query = filepath.Base(filepath.Clean(job.Root))
		}
		c.report("検索中: MusicBrainzを文字検索しています…")
		var err error
		ids, err = c.MusicBrainz.Search(ctx, "release:"+quoteLucene(query), 10)
		if err != nil {
			return err
		}
	}
	if len(ids) == 0 {
		return errors.New("MusicBrainzにアルバム候補が見つかりませんでした")
	}

	seen := map[string]bool{}
	var releases []domain.Release
	for index, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		c.report(fmt.Sprintf("検索中: アルバム候補の詳細を取得しています（%d/%d）", index+1, len(ids)))
		release, err := c.MusicBrainz.Lookup(ctx, id)
		if err == nil && len(release.Tracks) > 0 {
			releases = append(releases, release)
		}
	}
	if len(releases) == 0 {
		return errors.New("アルバム候補の詳細を取得できませんでした")
	}
	job.Candidates, _ = Rank(job.Root, job.Tracks, releases)
	job.Selected = 0
	_, job.Assignments = ScoreAndAssign(job.Root, job.Tracks, job.Candidates[0])
	cover, mime, err := c.CoverArt.Front(ctx, job.Candidates[0].ID)
	if err == nil {
		if normalized, normalizedMIME, normalizeErr := metadata.NormalizeCover(cover, mime); normalizeErr == nil {
			job.Cover, job.CoverMIME = normalized, normalizedMIME
		}
	}
	return nil
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
func quoteLucene(value string) string { return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"` }
