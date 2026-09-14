package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"wav-property-attacher/internal/domain"
	"wav-property-attacher/internal/services"
	"wav-property-attacher/internal/workflow"
)

type App struct {
	window fyne.Window

	pathEntry   *widget.Entry
	queryEntry  *widget.Entry
	apiEntry    *widget.Entry
	fpcalcEntry *widget.Entry
	coverEntry  *widget.Entry
	useFP       *widget.Check

	statusLabel   *widget.Label
	trackLabel    *widget.Label
	coverLabel    *widget.Label
	coverImage    *canvas.Image
	candidateBox  *fyne.Container
	assignmentBox *fyne.Container

	browseButton *widget.Button
	scanButton   *widget.Button
	searchButton *widget.Button
	coverButton  *widget.Button
	exportButton *widget.Button
	cancelButton *widget.Button

	candidateButtons  []*widget.Button
	assignmentSelects []*widget.Select
	controller        *workflow.Controller
	job               *domain.AlbumJob
	busy              bool
	busyKind          string
	cancel            context.CancelFunc
}

func New(window fyne.Window) *App {
	a := &App{window: window}
	a.pathEntry = widget.NewEntry()
	a.pathEntry.SetText(defaultMusicPath())
	a.pathEntry.SetPlaceHolder(`C:\Music\Album`)
	a.queryEntry = widget.NewEntry()
	a.queryEntry.SetPlaceHolder("アルバム名 / アーティスト（空欄ならフォルダ名）")
	a.apiEntry = widget.NewPasswordEntry()
	a.apiEntry.SetPlaceHolder("文字検索だけなら空欄可")
	a.fpcalcEntry = widget.NewEntry()
	a.fpcalcEntry.SetText(services.DefaultFPCalcPath())
	a.coverEntry = widget.NewEntry()
	a.coverEntry.SetPlaceHolder("JPEG / PNG")
	a.useFP = widget.NewCheck("音響指紋と再生秒数をAcoustIDへ送信することに同意する", nil)
	a.statusLabel = widget.NewLabel("アルバムフォルダを選択してください。")
	a.statusLabel.Wrapping = fyne.TextWrapWord
	a.trackLabel = widget.NewLabel("WAVはまだ読み込まれていません。")
	a.coverLabel = widget.NewLabel("ジャケット: 未取得")
	a.coverImage = &canvas.Image{FillMode: canvas.ImageFillContain}
	a.coverImage.SetMinSize(fyne.NewSize(220, 220))
	a.candidateBox = container.NewVBox(widget.NewLabel("検索後にアルバム候補が表示されます。"))
	a.assignmentBox = container.NewVBox(widget.NewLabel("アルバム候補を選択すると曲割当が表示されます。"))

	a.browseButton = widget.NewButton("フォルダを選択", a.chooseFolder)
	a.scanButton = widget.NewButton("WAVを読み込む", a.scan)
	a.searchButton = widget.NewButton("アルバムを検索", a.search)
	a.coverButton = widget.NewButton("画像を選択", a.chooseCover)
	a.exportButton = widget.NewButton("rekordbox用AIFFを生成", a.export)
	a.cancelButton = widget.NewButton("キャンセル", a.cancelWork)
	a.cancelButton.Disable()

	privacy := widget.NewLabel("音源ファイルはアップロードしません。指紋検索を有効にした場合のみ、端末内で生成した音響指紋と再生秒数をAcoustIDへ送信します。")
	privacy.Wrapping = fyne.TextWrapWord

	folderRow := container.NewBorder(nil, nil, nil, a.browseButton, a.pathEntry)
	coverRow := container.NewBorder(nil, nil, nil, a.coverButton, a.coverEntry)
	settings := widget.NewForm(
		widget.NewFormItem("アルバムフォルダ", folderRow),
		widget.NewFormItem("検索語", a.queryEntry),
		widget.NewFormItem("AcoustIDキー", a.apiEntry),
		widget.NewFormItem("fpcalc", a.fpcalcEntry),
		widget.NewFormItem("ローカルジャケット", coverRow),
	)
	coverPanel := container.NewVBox(a.coverImage, a.coverLabel)
	results := container.NewGridWithColumns(2,
		widget.NewCard("アルバム候補", "候補を選択してください", a.candidateBox),
		widget.NewCard("ジャケット", "AIFFへ埋め込まれる画像", coverPanel),
	)
	content := container.NewVBox(
		widget.NewLabelWithStyle("WAV Property Attacher", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		privacy,
		widget.NewSeparator(),
		settings,
		a.useFP,
		container.NewHBox(a.scanButton, a.searchButton, a.cancelButton),
		a.statusLabel,
		a.trackLabel,
		widget.NewSeparator(),
		results,
		widget.NewCard("曲の割当", "各WAVに対応するMusicBrainzトラックを確認・変更できます", a.assignmentBox),
		container.NewCenter(a.exportButton),
	)
	scroll := container.NewVScroll(content)
	scroll.SetMinSize(fyne.NewSize(860, 680))
	window.SetContent(scroll)
	return a
}

func (a *App) chooseFolder() {
	if a.busy {
		a.setStatus("別の処理が進行中です。完了を待つか、キャンセルしてください。", false)
		return
	}
	picker := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil {
			a.setStatus("フォルダを開けません: "+err.Error(), true)
			return
		}
		if uri != nil {
			a.pathEntry.SetText(pathFromURI(uri))
		}
	}, a.window)
	picker.Show()
}

func (a *App) chooseCover() {
	if a.busy {
		a.setStatus("別の処理が進行中です。完了を待つか、キャンセルしてください。", false)
		return
	}
	picker := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			a.setStatus("画像を開けません: "+err.Error(), true)
			return
		}
		if reader == nil {
			return
		}
		path := pathFromURI(reader.URI())
		_ = reader.Close()
		a.coverEntry.SetText(path)
		a.loadCover(path)
	}, a.window)
	picker.SetFilter(storage.NewExtensionFileFilter([]string{".jpg", ".jpeg", ".png"}))
	picker.Show()
}

func (a *App) scan() {
	if !a.ready() {
		return
	}
	controller := a.newController()
	root := strings.TrimSpace(a.pathEntry.Text)
	a.run("scan", func(ctx context.Context) (*domain.AlbumJob, string, error) {
		job, err := controller.Scan(ctx, root)
		return job, "", err
	})
}

func (a *App) search() {
	if !a.ready() {
		return
	}
	if a.job == nil || len(a.job.Tracks) == 0 {
		a.setStatus("先に「WAVを読み込む」を実行してください。", true)
		return
	}
	if a.useFP.Checked && strings.TrimSpace(a.apiEntry.Text) == "" {
		a.setStatus("音響指紋検索にはAcoustIDアプリケーションキーが必要です。キーを入力するか、指紋送信を無効にしてください。", true)
		return
	}
	controller := a.newController()
	job, query, useFP := a.job, a.queryEntry.Text, a.useFP.Checked
	a.run("identify", func(ctx context.Context) (*domain.AlbumJob, string, error) {
		err := controller.Identify(ctx, job, query, useFP)
		return job, "", err
	})
}

func (a *App) selectCandidate(index int) {
	if !a.ready() || a.job == nil || index < 0 || index >= len(a.job.Candidates) {
		return
	}
	controller := a.controller
	if controller == nil {
		controller = a.newController()
	}
	job := a.job
	a.run("select", func(ctx context.Context) (*domain.AlbumJob, string, error) {
		err := controller.SelectCandidate(ctx, job, index)
		return job, "", err
	})
}

func (a *App) loadCover(path string) {
	if a.job == nil {
		a.setStatus("先にアルバムを検索してください。", true)
		return
	}
	controller := a.controller
	if controller == nil {
		controller = a.newController()
	}
	job := a.job
	a.run("cover", func(context.Context) (*domain.AlbumJob, string, error) {
		err := controller.LoadLocalCover(path, job)
		return job, "", err
	})
}

func (a *App) export() {
	if !a.ready() {
		return
	}
	if a.job == nil || a.job.Selected < 0 {
		a.setStatus("先にアルバム候補を選択してください。", true)
		return
	}
	controller := a.controller
	if controller == nil {
		controller = a.newController()
	}
	job := a.job
	a.run("export", func(ctx context.Context) (*domain.AlbumJob, string, error) {
		path, err := controller.Export(ctx, job)
		return job, path, err
	})
}

func (a *App) newController() *workflow.Controller {
	controller := workflow.NewController(strings.TrimSpace(a.apiEntry.Text), strings.TrimSpace(a.fpcalcEntry.Text))
	controller.Progress = func(message string) {
		fyne.Do(func() {
			if a.busy {
				a.setStatus(message, false)
			}
		})
	}
	a.controller = controller
	return controller
}

func (a *App) run(kind string, work func(context.Context) (*domain.AlbumJob, string, error)) {
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel, a.busy, a.busyKind = cancel, true, kind
	a.setBusy(true)
	switch kind {
	case "scan":
		a.setStatus("WAVファイルを読み込んでいます…", false)
	case "identify":
		a.setStatus("アルバムを検索しています…", false)
	case "select":
		a.setStatus("選択したアルバムの詳細とジャケットを取得しています…", false)
	case "cover":
		a.setStatus("ジャケット画像を読み込んでいます…", false)
	case "export":
		a.setStatus("AIFFを生成して音声データを検証しています…", false)
	}
	go func() {
		job, path, err := work(ctx)
		fyne.Do(func() { a.finish(kind, job, path, err) })
	}()
}

func (a *App) finish(kind string, job *domain.AlbumJob, path string, err error) {
	a.busy, a.busyKind, a.cancel = false, "", nil
	a.setBusy(false)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			a.setStatus("処理をキャンセルしました。", false)
		} else {
			a.setStatus(err.Error(), true)
		}
		return
	}
	if job != nil {
		a.job = job
	}
	switch kind {
	case "scan":
		a.trackLabel.SetText(fmt.Sprintf("読み込み済み: %d曲", len(a.job.Tracks)))
		a.candidateBox.Objects = []fyne.CanvasObject{widget.NewLabel("「アルバムを検索」を押してください。")}
		a.candidateBox.Refresh()
		a.assignmentBox.Objects = []fyne.CanvasObject{widget.NewLabel("検索後に曲割当が表示されます。")}
		a.assignmentBox.Refresh()
		a.setStatus(fmt.Sprintf("%d曲を読み込みました。検索条件を確認してください。", len(a.job.Tracks)), false)
	case "identify":
		a.rebuildCandidates()
		a.rebuildAssignments()
		a.refreshCover()
		a.setStatus(fmt.Sprintf("%d件のアルバム候補を取得しました。内容を確認してください。", len(a.job.Candidates)), false)
	case "select":
		a.rebuildCandidates()
		a.rebuildAssignments()
		a.refreshCover()
		a.setStatus("候補、曲割当、ジャケットを更新しました。", false)
	case "cover":
		a.refreshCover()
		a.setStatus("ローカル画像をジャケットに設定しました。", false)
	case "export":
		a.setStatus("AIFFの生成が完了しました: "+path, false)
	}
}

func (a *App) rebuildCandidates() {
	if a.job == nil || len(a.job.Candidates) == 0 {
		a.candidateBox.Objects = []fyne.CanvasObject{widget.NewLabel("アルバム候補がありません。")}
		a.candidateBox.Refresh()
		return
	}
	objects := make([]fyne.CanvasObject, 0, len(a.job.Candidates))
	a.candidateButtons = make([]*widget.Button, 0, len(a.job.Candidates))
	for i, release := range a.job.Candidates {
		index := i
		prefix := "○"
		if a.job.Selected == i {
			prefix = "●"
		}
		label := fmt.Sprintf("%s %.0f%%  %s — %s\n%s / %d曲", prefix, release.Score*100, release.Title, release.AlbumArtist, release.Date, len(release.Tracks))
		button := widget.NewButton(label, func() { a.selectCandidate(index) })
		a.candidateButtons = append(a.candidateButtons, button)
		objects = append(objects, button)
	}
	a.candidateBox.Objects = objects
	a.candidateBox.Refresh()
}

func (a *App) rebuildAssignments() {
	if a.job == nil || a.job.Selected < 0 || a.job.Selected >= len(a.job.Candidates) {
		return
	}
	release := a.job.Candidates[a.job.Selected]
	objects := make([]fyne.CanvasObject, 0, len(a.job.Assignments))
	a.assignmentSelects = make([]*widget.Select, 0, len(a.job.Assignments))
	for assignmentIndex := range a.job.Assignments {
		index := assignmentIndex
		assignment := a.job.Assignments[index]
		local := a.job.Tracks[assignment.LocalIndex]
		options := []string{"未割当", "出力から除外"}
		for _, track := range release.Tracks {
			options = append(options, fmt.Sprintf("Disc %d-%02d  %s — %s", track.Disc, track.Position, track.Title, track.Artist))
		}
		selected := "未割当"
		if assignment.Excluded {
			selected = "出力から除外"
		} else if assignment.ReleaseIndex >= 0 && assignment.ReleaseIndex < len(release.Tracks) {
			selected = options[assignment.ReleaseIndex+2]
		}
		selectWidget := widget.NewSelect(options, func(value string) {
			current := &a.job.Assignments[index]
			switch value {
			case "未割当":
				current.ReleaseIndex, current.Excluded = -1, false
			case "出力から除外":
				current.ReleaseIndex, current.Excluded = -1, true
			default:
				for optionIndex, option := range options[2:] {
					if option == value {
						current.ReleaseIndex, current.Excluded = optionIndex, false
						break
					}
				}
			}
			a.setStatus("曲割当を手動で変更しました。", false)
		})
		selectWidget.SetSelected(selected)
		a.assignmentSelects = append(a.assignmentSelects, selectWidget)
		objects = append(objects, widget.NewForm(widget.NewFormItem(local.Relative, selectWidget)))
	}
	a.assignmentBox.Objects = objects
	a.assignmentBox.Refresh()
}

func (a *App) refreshCover() {
	if a.job == nil || len(a.job.Cover) == 0 {
		a.coverImage.Image = nil
		a.coverImage.Refresh()
		a.coverLabel.SetText("ジャケット: 未取得（ローカル画像を選択できます）")
		return
	}
	decoded, _, err := image.Decode(bytes.NewReader(a.job.Cover))
	if err != nil {
		a.coverLabel.SetText("ジャケットをプレビューできません: " + err.Error())
		return
	}
	a.coverImage.Image = decoded
	a.coverImage.Refresh()
	a.coverLabel.SetText(fmt.Sprintf("ジャケット: %s / %d KiB", a.job.CoverMIME, (len(a.job.Cover)+1023)/1024))
}

func (a *App) setBusy(busy bool) {
	buttons := []*widget.Button{a.browseButton, a.scanButton, a.searchButton, a.coverButton, a.exportButton}
	buttons = append(buttons, a.candidateButtons...)
	for _, button := range buttons {
		if busy {
			button.Disable()
		} else {
			button.Enable()
		}
	}
	for _, assignment := range a.assignmentSelects {
		if busy {
			assignment.Disable()
		} else {
			assignment.Enable()
		}
	}
	if busy {
		a.cancelButton.Enable()
	} else {
		a.cancelButton.Disable()
	}
	if a.busyKind == "identify" {
		a.searchButton.SetText("検索中…")
	} else {
		a.searchButton.SetText("アルバムを検索")
	}
}

func (a *App) cancelWork() {
	if a.cancel != nil {
		a.cancel()
		a.setStatus("キャンセルしています…", false)
	}
}

func (a *App) ready() bool {
	if a.busy {
		a.setStatus("別の処理が進行中です。完了を待つか、キャンセルしてください。", false)
		return false
	}
	return true
}

func (a *App) setStatus(message string, isError bool) {
	if isError {
		message = "エラー: " + message
	}
	a.statusLabel.SetText(message)
}

func pathFromURI(uri fyne.URI) string {
	if uri == nil {
		return ""
	}
	return filepath.FromSlash(uri.Path())
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
