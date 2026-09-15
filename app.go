package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"wav-property-attacher/internal/domain"
	"wav-property-attacher/internal/services"
	"wav-property-attacher/internal/workflow"
)

type Settings struct {
	Root           string `json:"root"`
	AlbumTitle     string `json:"albumTitle"`
	Artist         string `json:"artist"`
	Year           string `json:"year"`
	APIKey         string `json:"apiKey"`
	FPCalcPath     string `json:"fpcalcPath"`
	UseFingerprint bool   `json:"useFingerprint"`
}

type Defaults struct {
	MusicPath  string `json:"musicPath"`
	FPCalcPath string `json:"fpcalcPath"`
}

type TrackView struct {
	Index     int    `json:"index"`
	Relative  string `json:"relative"`
	Duration  string `json:"duration"`
	DiscHint  int    `json:"discHint"`
	TrackHint int    `json:"trackHint"`
}

type ReleaseTrackView struct {
	Index    int    `json:"index"`
	Disc     int    `json:"disc"`
	Position int    `json:"position"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Duration string `json:"duration"`
}

type ReleaseView struct {
	Index           int                `json:"index"`
	ID              string             `json:"id"`
	ReleaseGroupID  string             `json:"releaseGroupId"`
	Title           string             `json:"title"`
	AlbumArtist     string             `json:"albumArtist"`
	Date            string             `json:"date"`
	Country         string             `json:"country"`
	ScorePercent    int                `json:"scorePercent"`
	Confidence      string             `json:"confidence"`
	Evidence        []string           `json:"evidence"`
	FingerprintHits int                `json:"fingerprintHits"`
	Tracks          []ReleaseTrackView `json:"tracks"`
}

type AssignmentView struct {
	LocalIndex   int    `json:"localIndex"`
	LocalName    string `json:"localName"`
	ReleaseIndex int    `json:"releaseIndex"`
	Excluded     bool   `json:"excluded"`
}

type JobView struct {
	Root            string           `json:"root"`
	Tracks          []TrackView      `json:"tracks"`
	Candidates      []ReleaseView    `json:"candidates"`
	Selected        int              `json:"selected"`
	Assignments     []AssignmentView `json:"assignments"`
	CoverDataURL    string           `json:"coverDataUrl"`
	CoverMIME       string           `json:"coverMime"`
	CoverKiB        int              `json:"coverKiB"`
	SuggestedAlbum  string           `json:"suggestedAlbum"`
	SuggestedArtist string           `json:"suggestedArtist"`
	SuggestedYear   string           `json:"suggestedYear"`
	Warnings        []string         `json:"warnings"`
}

type App struct {
	mu         sync.Mutex
	ctx        context.Context
	job        *domain.AlbumJob
	controller *workflow.Controller
	cancel     context.CancelFunc
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()
}

func (a *App) shutdown(context.Context) { a.Cancel() }

func (a *App) GetDefaults() Defaults {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	music := filepath.Join(home, "Music")
	if _, err := os.Stat(music); err != nil {
		music = home
	}
	return Defaults{MusicPath: music, FPCalcPath: services.DefaultFPCalcPath()}
}

func (a *App) ChooseFolder() (string, error) {
	ctx := a.runtimeContext()
	return runtime.OpenDirectoryDialog(ctx, runtime.OpenDialogOptions{Title: "WAVアルバムフォルダを選択"})
}

func (a *App) ChooseCover() (string, error) {
	ctx := a.runtimeContext()
	return runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{
		Title:   "アルバムジャケットを選択",
		Filters: []runtime.FileFilter{{DisplayName: "画像 (*.jpg;*.jpeg;*.png)", Pattern: "*.jpg;*.jpeg;*.png"}},
	})
}

func (a *App) ResolveAlbumDrop(path string) (string, error) {
	path = strings.TrimSpace(path)
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return filepath.Clean(path), nil
	}
	if strings.EqualFold(filepath.Ext(path), ".wav") {
		return filepath.Dir(path), nil
	}
	return "", errors.New("アルバムフォルダ、またはその中のWAVファイルをドロップしてください")
}

func (a *App) ResolveCoverDrop(path string) (string, error) {
	path = strings.TrimSpace(path)
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", errors.New("JPEGまたはPNG画像をドロップしてください")
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".png":
		return filepath.Clean(path), nil
	default:
		return "", errors.New("JPEGまたはPNG画像をドロップしてください")
	}
}

func (a *App) Scan(settings Settings) (JobView, error) {
	ctx, finish, err := a.begin()
	if err != nil {
		return JobView{}, err
	}
	defer finish()
	controller := a.makeController(settings)
	job, err := controller.Scan(ctx, strings.TrimSpace(settings.Root))
	if err != nil {
		return JobView{}, err
	}
	a.mu.Lock()
	a.job, a.controller = job, controller
	a.mu.Unlock()
	return viewOf(job), nil
}

func (a *App) Search(settings Settings) (JobView, error) {
	ctx, finish, err := a.begin()
	if err != nil {
		return JobView{}, err
	}
	defer finish()
	job, err := a.currentJob()
	if err != nil {
		return JobView{}, err
	}
	controller := a.currentController()
	a.configureController(controller, settings)
	criteria := domain.SearchCriteria{AlbumTitle: settings.AlbumTitle, Artist: settings.Artist, Year: settings.Year}
	if err := controller.Identify(ctx, job, criteria, settings.UseFingerprint); err != nil {
		return JobView{}, err
	}
	a.mu.Lock()
	a.controller = controller
	a.mu.Unlock()
	return viewOf(job), nil
}

func (a *App) LookupRelease(value string) (JobView, error) {
	ctx, finish, err := a.begin()
	if err != nil {
		return JobView{}, err
	}
	defer finish()
	job, err := a.currentJob()
	if err != nil {
		return JobView{}, err
	}
	if err := a.currentController().LookupDirect(ctx, job, value); err != nil {
		return JobView{}, err
	}
	return viewOf(job), nil
}

func (a *App) SelectCandidate(index int) (JobView, error) {
	ctx, finish, err := a.begin()
	if err != nil {
		return JobView{}, err
	}
	defer finish()
	job, err := a.currentJob()
	if err != nil {
		return JobView{}, err
	}
	controller := a.currentController()
	if err := controller.SelectCandidate(ctx, job, index); err != nil {
		return JobView{}, err
	}
	return viewOf(job), nil
}

func (a *App) SetAssignment(localIndex, releaseIndex int, excluded bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		return errors.New("処理中は曲割当を変更できません")
	}
	if a.job == nil || localIndex < 0 || localIndex >= len(a.job.Assignments) {
		return errors.New("曲割当の指定が不正です")
	}
	if !excluded {
		if a.job.Selected < 0 || a.job.Selected >= len(a.job.Candidates) {
			return errors.New("アルバム候補が選択されていません")
		}
		if releaseIndex < -1 || releaseIndex >= len(a.job.Candidates[a.job.Selected].Tracks) {
			return errors.New("割当先トラックが不正です")
		}
	} else {
		releaseIndex = -1
	}
	a.job.Assignments[localIndex].ReleaseIndex = releaseIndex
	a.job.Assignments[localIndex].Excluded = excluded
	return nil
}

func (a *App) LoadCover(path string) (JobView, error) {
	_, finish, err := a.begin()
	if err != nil {
		return JobView{}, err
	}
	defer finish()
	job, err := a.currentJob()
	if err != nil {
		return JobView{}, err
	}
	if err := a.currentController().LoadLocalCover(path, job); err != nil {
		return JobView{}, err
	}
	return viewOf(job), nil
}

func (a *App) Export() (string, error) {
	ctx, finish, err := a.begin()
	if err != nil {
		return "", err
	}
	defer finish()
	job, err := a.currentJob()
	if err != nil {
		return "", err
	}
	return a.currentController().Export(ctx, job)
}

func (a *App) Cancel() {
	a.mu.Lock()
	cancel := a.cancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *App) begin() (context.Context, func(), error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		return nil, nil, errors.New("別の処理が進行中です")
	}
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	a.cancel = cancel
	return ctx, func() {
		a.mu.Lock()
		a.cancel = nil
		a.mu.Unlock()
		cancel()
	}, nil
}

func (a *App) makeController(settings Settings) *workflow.Controller {
	controller := workflow.NewController(strings.TrimSpace(settings.APIKey), strings.TrimSpace(settings.FPCalcPath))
	a.configureController(controller, settings)
	return controller
}

func (a *App) configureController(controller *workflow.Controller, settings Settings) {
	controller.AcoustID.APIKey = strings.TrimSpace(settings.APIKey)
	controller.Fingerprinter.Path = strings.TrimSpace(settings.FPCalcPath)
	controller.Progress = func(message string) {
		ctx := a.runtimeContext()
		if ctx != nil {
			runtime.EventsEmit(ctx, "search:progress", message)
		}
	}
}

func (a *App) currentJob() (*domain.AlbumJob, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.job == nil {
		return nil, errors.New("先にWAVフォルダを読み込んでください")
	}
	return a.job, nil
}

func (a *App) currentController() *workflow.Controller {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.controller == nil {
		a.controller = workflow.NewController("", services.DefaultFPCalcPath())
	}
	return a.controller
}

func (a *App) runtimeContext() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ctx
}

func viewOf(job *domain.AlbumJob) JobView {
	view := JobView{Root: job.Root, Selected: job.Selected, CoverMIME: job.CoverMIME, SuggestedAlbum: job.Suggested.AlbumTitle, SuggestedArtist: job.Suggested.Artist, SuggestedYear: job.Suggested.Year, Warnings: append([]string(nil), job.Warnings...)}
	for index, track := range job.Tracks {
		view.Tracks = append(view.Tracks, TrackView{Index: index, Relative: track.Relative, Duration: durationText(track.Duration), DiscHint: track.DiscHint, TrackHint: track.TrackHint})
	}
	for index, release := range job.Candidates {
		score := int(release.Score*100 + .5)
		if score < 0 {
			score = 0
		}
		if score > 100 {
			score = 100
		}
		confidence := "低"
		if score >= 75 {
			confidence = "高"
		} else if score >= 60 {
			confidence = "中"
		}
		candidate := ReleaseView{Index: index, ID: release.ID, ReleaseGroupID: release.ReleaseGroupID, Title: release.Title, AlbumArtist: release.AlbumArtist, Date: release.Date, Country: release.Country, ScorePercent: score, Confidence: confidence, Evidence: append([]string(nil), release.Evidence...), FingerprintHits: release.FingerprintHits}
		for trackIndex, track := range release.Tracks {
			candidate.Tracks = append(candidate.Tracks, ReleaseTrackView{Index: trackIndex, Disc: track.Disc, Position: track.Position, Title: track.Title, Artist: track.Artist, Duration: durationText(time.Duration(track.LengthMS) * time.Millisecond)})
		}
		view.Candidates = append(view.Candidates, candidate)
	}
	for _, assignment := range job.Assignments {
		if assignment.LocalIndex < 0 || assignment.LocalIndex >= len(job.Tracks) {
			continue
		}
		view.Assignments = append(view.Assignments, AssignmentView{LocalIndex: assignment.LocalIndex, LocalName: job.Tracks[assignment.LocalIndex].Relative, ReleaseIndex: assignment.ReleaseIndex, Excluded: assignment.Excluded})
	}
	if len(job.Cover) > 0 {
		view.CoverDataURL = "data:" + job.CoverMIME + ";base64," + base64.StdEncoding.EncodeToString(job.Cover)
		view.CoverKiB = (len(job.Cover) + 1023) / 1024
	}
	return view
}

func durationText(value time.Duration) string {
	seconds := int(value.Round(time.Second) / time.Second)
	if seconds < 0 {
		seconds = 0
	}
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}
