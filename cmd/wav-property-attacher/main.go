package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	appui "wav-property-attacher/internal/ui"
)

func main() {
	application := app.NewWithID("io.github.hiro-1219.wav-property-attacher")
	window := application.NewWindow("WAV Property Attacher")
	window.Resize(fyne.NewSize(960, 800))
	appui.New(window)
	window.ShowAndRun()
}
