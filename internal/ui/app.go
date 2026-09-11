package ui

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"wav-property-attacher/internal/domain"
	"wav-property-attacher/internal/services"
	"wav-property-attacher/internal/workflow"
)

type asyncResult struct {
	kind   string
	job    *domain.AlbumJob
	path   string
	status string
	err    error
}

type directoryEntry struct {
	name string
	path string
	up   bool
	btn  widget.Clickable
}

type App struct {
	window *app.Window
	theme  *material.Theme
	ops    op.Ops

	pathEditor   widget.Editor
	queryEditor  widget.Editor
	apiEditor    widget.Editor
	fpcalcEditor widget.Editor
	coverEditor  widget.Editor
	useFP        widget.Bool

	browseButton widget.Clickable
	scanButton   widget.Clickable
	searchButton widget.Clickable
	coverButton  widget.Clickable
	exportButton widget.Clickable
	cancelButton widget.Clickable
	chooseButton widget.Clickable
	candidates   []widget.Clickable
	assignments  []widget.Clickable

	pageList layout.List
	dirList  layout.List
	showDirs bool
	dirPath  string
	dirs     []directoryEntry

	controller *workflow.Controller
	job        *domain.AlbumJob
	status     string
	busy       bool
	busyKind   string
	cancel     context.CancelFunc
	results    chan asyncResult
}

func New(window *app.Window) *App {
	a := &App{window: window, theme: material.NewTheme(), results: make(chan asyncResult, 8), status: "アルバムフォルダを選択してください。"}
	a.pageList.Axis = layout.Vertical
	a.dirList.Axis = layout.Vertical
	a.pathEditor.SingleLine, a.queryEditor.SingleLine, a.apiEditor.SingleLine = true, true, true
	a.fpcalcEditor.SingleLine, a.coverEditor.SingleLine = true, true
	a.pathEditor.SetText(defaultMusicPath())
	a.fpcalcEditor.SetText(workflowFPPath())
	// Fingerprints disclose the musical identity of the local audio. Require an
	// explicit opt-in for each application session.
	a.useFP.Value = false
	return a
}

func (a *App) Run() error {
	for {
		switch e := a.window.Event().(type) {
		case app.DestroyEvent:
			if a.cancel != nil {
				a.cancel()
			}
			return e.Err
		case app.FrameEvent:
			a.consumeResults()
			gtx := app.NewContext(&a.ops, e)
			a.handleActions(gtx)
			a.layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

func (a *App) consumeResults() {
	for {
		select {
		case result := <-a.results:
			if result.kind == "progress" {
				a.status = result.status
				continue
			}
			a.busy, a.busyKind, a.cancel = false, "", nil
			if result.err != nil {
				a.status = "エラー: " + result.err.Error()
				continue
			}
			if result.job != nil {
				a.job = result.job
			}
			switch result.kind {
			case "scan":
				a.status = fmt.Sprintf("%d曲を読み込みました。検索条件を確認してください。", len(a.job.Tracks))
			case "identify":
				a.candidates = make([]widget.Clickable, len(a.job.Candidates))
				a.assignments = make([]widget.Clickable, len(a.job.Assignments))
				a.status = fmt.Sprintf("%d件のアルバム候補を取得しました。内容を確認してください。", len(a.job.Candidates))
			case "select":
				a.assignments = make([]widget.Clickable, len(a.job.Assignments))
				a.status = "候補と曲割当を更新しました。"
			case "cover":
				a.status = "ローカル画像をジャケットに設定しました。"
			case "export":
				a.status = "AIFFの生成が完了しました: " + result.path
			}
		default:
			return
		}
	}
}

func (a *App) handleActions(gtx layout.Context) {
	if a.browseButton.Clicked(gtx) && !a.busy {
		a.showDirs = !a.showDirs
		if a.showDirs {
			a.dirPath = strings.TrimSpace(a.pathEditor.Text())
			if a.dirPath == "" {
				a.dirPath = defaultMusicPath()
			}
			a.refreshDirs()
		}
	}
	for i := range a.dirs {
		if a.dirs[i].btn.Clicked(gtx) {
			// refreshDirs replaces a.dirs. Stop iterating the old index range
			// immediately; the new directory can contain fewer entries.
			next := a.dirs[i].path
			a.dirPath = next
			a.refreshDirs()
			break
		}
	}
	if a.chooseButton.Clicked(gtx) && a.showDirs {
		a.pathEditor.SetText(a.dirPath)
		a.showDirs = false
	}
	if a.scanButton.Clicked(gtx) && !a.busy {
		controller := workflow.NewController(strings.TrimSpace(a.apiEditor.Text()), strings.TrimSpace(a.fpcalcEditor.Text()))
		a.controller = controller
		a.run("scan", func(ctx context.Context) (*domain.AlbumJob, string, error) {
			j, e := controller.Scan(ctx, a.pathEditor.Text())
			return j, "", e
		})
	}
	if a.searchButton.Clicked(gtx) {
		switch {
		case a.busy:
			a.status = "別の処理が進行中です。完了を待つか、キャンセルしてください。"
		case a.job == nil || len(a.job.Tracks) == 0:
			a.status = "エラー: 先に「WAVを読み込む」を実行してください。"
		case a.useFP.Value && strings.TrimSpace(a.apiEditor.Text()) == "":
			a.status = "エラー: 音響指紋検索にはAcoustIDアプリケーションキーが必要です。キーを入力するか、指紋送信を無効にしてください。"
		default:
			controller := workflow.NewController(strings.TrimSpace(a.apiEditor.Text()), strings.TrimSpace(a.fpcalcEditor.Text()))
			controller.Progress = func(status string) {
				select {
				case a.results <- asyncResult{kind: "progress", status: status}:
					a.window.Invalidate()
				default:
				}
			}
			a.controller = controller
			job := a.job
			query := a.queryEditor.Text()
			use := a.useFP.Value
			a.run("identify", func(ctx context.Context) (*domain.AlbumJob, string, error) {
				e := controller.Identify(ctx, job, query, use)
				return job, "", e
			})
		}
	}
	for i := range a.candidates {
		if a.candidates[i].Clicked(gtx) && !a.busy {
			idx := i
			job := a.job
			controller := a.controller
			a.run("select", func(ctx context.Context) (*domain.AlbumJob, string, error) {
				e := controller.SelectCandidate(ctx, job, idx)
				return job, "", e
			})
		}
	}
	if a.job != nil && a.job.Selected >= 0 && a.job.Selected < len(a.job.Candidates) {
		releaseTrackCount := len(a.job.Candidates[a.job.Selected].Tracks)
		for i := range a.assignments {
			if a.assignments[i].Clicked(gtx) && i < len(a.job.Assignments) {
				assignment := &a.job.Assignments[i]
				switch {
				case assignment.Excluded:
					assignment.Excluded = false
					assignment.ReleaseIndex = -1
				case assignment.ReleaseIndex+1 < releaseTrackCount:
					assignment.ReleaseIndex++
				default:
					assignment.ReleaseIndex = -1
					assignment.Excluded = true
				}
				a.status = "曲割当を手動で変更しました。未割当の曲がある場合は再度クリックしてください。"
			}
		}
	}
	if a.coverButton.Clicked(gtx) && !a.busy && a.job != nil {
		job := a.job
		path := a.coverEditor.Text()
		controller := a.controller
		a.run("cover", func(context.Context) (*domain.AlbumJob, string, error) {
			e := controller.LoadLocalCover(path, job)
			return job, "", e
		})
	}
	if a.exportButton.Clicked(gtx) && !a.busy && a.job != nil {
		job := a.job
		controller := a.controller
		a.run("export", func(ctx context.Context) (*domain.AlbumJob, string, error) {
			path, e := controller.Export(ctx, job)
			return job, path, e
		})
	}
	if a.cancelButton.Clicked(gtx) && a.busy && a.cancel != nil {
		a.cancel()
		a.status = "キャンセルしています…"
	}
}

func (a *App) run(kind string, work func(context.Context) (*domain.AlbumJob, string, error)) {
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.busy = true
	a.busyKind = kind
	switch kind {
	case "identify":
		if a.useFP.Value {
			a.status = "検索中: ローカルで音響指紋を生成し、アルバム候補を照合しています…"
		} else {
			a.status = "検索中: MusicBrainzからアルバム候補を取得しています…"
		}
	case "scan":
		a.status = "WAVファイルを読み込んでいます…"
	case "export":
		a.status = "AIFFを生成して音声データを検証しています…"
	default:
		a.status = "処理中…"
	}
	go func() {
		job, path, err := work(ctx)
		a.results <- asyncResult{kind: kind, job: job, path: path, err: err}
		a.window.Invalidate()
	}()
}

func (a *App) layout(gtx layout.Context) layout.Dimensions {
	searchLabel := "アルバムを検索"
	if a.busyKind == "identify" {
		searchLabel = "検索中…"
	}
	inset := layout.Inset{Top: unit.Dp(14), Right: unit.Dp(18), Bottom: unit.Dp(18), Left: unit.Dp(18)}
	return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		items := []layout.Widget{
			func(gtx layout.Context) layout.Dimensions {
				title := material.H4(a.theme, "WAV Property Attacher")
				title.Alignment = text.Middle
				return title.Layout(gtx)
			},
			spacer(8),
			func(gtx layout.Context) layout.Dimensions {
				body := material.Body2(a.theme, "音源はアップロードしません。指紋検索を有効にした場合のみ、端末内で生成した音響指紋と再生秒数をAcoustIDへ送信します。")
				body.Color = color.NRGBA{R: 55, G: 65, B: 81, A: 255}
				return body.Layout(gtx)
			},
			spacer(12),
			func(gtx layout.Context) layout.Dimensions {
				return labeledEditor(gtx, a.theme, "アルバムフォルダ", &a.pathEditor, "C:\\Music\\Album")
			},
			func(gtx layout.Context) layout.Dimensions {
				return buttonRow(gtx, material.Button(a.theme, &a.browseButton, "フォルダを参照"), material.Button(a.theme, &a.scanButton, "WAVを読み込む"))
			},
		}
		if a.showDirs {
			items = append(items, a.layoutDirectoryBrowser)
		}
		items = append(items,
			func(gtx layout.Context) layout.Dimensions {
				return labeledEditor(gtx, a.theme, "検索語（空欄ならフォルダ名）", &a.queryEditor, "アルバム名 / アーティスト")
			},
			func(gtx layout.Context) layout.Dimensions {
				return labeledEditor(gtx, a.theme, "AcoustIDアプリケーションキー", &a.apiEditor, "文字検索だけなら空欄可")
			},
			func(gtx layout.Context) layout.Dimensions {
				return labeledEditor(gtx, a.theme, "fpcalcのパス", &a.fpcalcEditor, "tools/fpcalc")
			},
			func(gtx layout.Context) layout.Dimensions {
				return material.CheckBox(a.theme, &a.useFP, "上記を理解し、音響指紋と再生秒数をAcoustIDへ送信する").Layout(gtx)
			},
			func(gtx layout.Context) layout.Dimensions {
				return buttonRow(gtx, material.Button(a.theme, &a.searchButton, searchLabel), material.Button(a.theme, &a.cancelButton, "キャンセル"))
			},
			spacer(10),
			func(gtx layout.Context) layout.Dimensions {
				label := material.Body1(a.theme, a.status)
				if strings.HasPrefix(a.status, "エラー") {
					label.Color = color.NRGBA{R: 180, G: 25, B: 35, A: 255}
				}
				return label.Layout(gtx)
			},
		)
		if a.job != nil {
			items = append(items, a.layoutJob)
		}
		return a.pageList.Layout(gtx, len(items), func(gtx layout.Context, index int) layout.Dimensions {
			return layout.Inset{Bottom: unit.Dp(7)}.Layout(gtx, items[index])
		})
	})
}

func (a *App) layoutDirectoryBrowser(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(material.Body1(a.theme, "参照中: "+a.dirPath).Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Max.Y = gtx.Dp(unit.Dp(180))
				return a.dirList.Layout(gtx, len(a.dirs), func(gtx layout.Context, i int) layout.Dimensions {
					return material.Button(a.theme, &a.dirs[i].btn, a.dirs[i].name).Layout(gtx)
				})
			}),
			layout.Rigid(material.Button(a.theme, &a.chooseButton, "このフォルダを選択").Layout),
		)
	})
}

func (a *App) layoutJob(gtx layout.Context) layout.Dimensions {
	widgets := []layout.Widget{func(gtx layout.Context) layout.Dimensions {
		return material.H6(a.theme, fmt.Sprintf("読み込み済み: %d曲", len(a.job.Tracks))).Layout(gtx)
	}}
	for i := range a.job.Candidates {
		idx := i
		widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
			r := a.job.Candidates[idx]
			prefix := "○"
			if a.job.Selected == idx {
				prefix = "●"
			}
			return material.Button(a.theme, &a.candidates[idx], fmt.Sprintf("%s %.0f%%  %s — %s (%s, %d曲)", prefix, r.Score*100, r.Title, r.AlbumArtist, r.Date, len(r.Tracks))).Layout(gtx)
		})
	}
	if a.job.Selected >= 0 && a.job.Selected < len(a.job.Candidates) {
		r := a.job.Candidates[a.job.Selected]
		widgets = append(widgets, func(gtx layout.Context) layout.Dimensions { return material.H6(a.theme, "曲の割当").Layout(gtx) })
		widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
			return material.Caption(a.theme, "各行をクリックすると、割当候補 → 除外 → 未割当の順で変更できます。").Layout(gtx)
		})
		for i := range a.job.Assignments {
			assignmentIndex := i
			widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
				assignment := a.job.Assignments[assignmentIndex]
				local := a.job.Tracks[assignment.LocalIndex]
				target := "未割当"
				if assignment.Excluded {
					target = "出力から除外"
				} else if assignment.ReleaseIndex >= 0 && assignment.ReleaseIndex < len(r.Tracks) {
					remote := r.Tracks[assignment.ReleaseIndex]
					target = fmt.Sprintf("Disc %d-%02d  %s — %s", remote.Disc, remote.Position, remote.Title, remote.Artist)
				}
				return material.Button(a.theme, &a.assignments[assignmentIndex], local.Relative+"  →  "+target).Layout(gtx)
			})
		}
	}
	widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
		return labeledEditor(gtx, a.theme, "ローカルジャケット（任意）", &a.coverEditor, "JPEG / PNG")
	}, func(gtx layout.Context) layout.Dimensions {
		return buttonRow(gtx, material.Button(a.theme, &a.coverButton, "画像を使用"), material.Button(a.theme, &a.exportButton, "rekordbox用AIFFを生成"))
	})
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, funcsToChildren(widgets)...)
}

func (a *App) refreshDirs() {
	entries, err := os.ReadDir(a.dirPath)
	if err != nil {
		a.status = "フォルダを開けません: " + err.Error()
		return
	}
	a.dirPath, _ = filepath.Abs(a.dirPath)
	dirs := []directoryEntry{}
	parent := filepath.Dir(a.dirPath)
	if parent != a.dirPath {
		dirs = append(dirs, directoryEntry{name: "..", path: parent, up: true})
	}
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, directoryEntry{name: entry.Name(), path: filepath.Join(a.dirPath, entry.Name())})
		}
	}
	sort.Slice(dirs, func(i, j int) bool {
		if dirs[i].up {
			return true
		}
		if dirs[j].up {
			return false
		}
		return strings.ToLower(dirs[i].name) < strings.ToLower(dirs[j].name)
	})
	a.dirs = dirs
}

func labeledEditor(gtx layout.Context, theme *material.Theme, label string, editor *widget.Editor, hint string) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, layout.Rigid(material.Caption(theme, label).Layout), layout.Rigid(material.Editor(theme, editor, hint).Layout))
}
func buttonRow(gtx layout.Context, buttons ...material.ButtonStyle) layout.Dimensions {
	children := make([]layout.FlexChild, 0, len(buttons))
	for i := range buttons {
		button := buttons[i]
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Right: unit.Dp(8)}.Layout(gtx, button.Layout)
		}))
	}
	return layout.Flex{}.Layout(gtx, children...)
}
func spacer(dp float32) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions { return layout.Spacer{Height: unit.Dp(dp)}.Layout(gtx) }
}
func funcsToChildren(w []layout.Widget) []layout.FlexChild {
	out := make([]layout.FlexChild, 0, len(w))
	for _, fn := range w {
		f := fn
		out = append(out, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return layout.Inset{Bottom: unit.Dp(5)}.Layout(gtx, f) }))
	}
	return out
}
func defaultMusicPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	music := filepath.Join(home, "Music")
	if _, err := os.Stat(music); err == nil {
		return music
	}
	return home
}
func workflowFPPath() string { return services.DefaultFPCalcPath() }
