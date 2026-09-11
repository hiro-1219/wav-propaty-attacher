package main

import (
	"log"
	"os"

	"gioui.org/app"
	"gioui.org/unit"

	appui "wav-property-attacher/internal/ui"
)

func main() {
	go func() {
		window := new(app.Window)
		window.Option(app.Title("WAV Property Attacher"), app.Size(unit.Dp(920), unit.Dp(760)))
		if err := appui.New(window).Run(); err != nil {
			log.Print(err)
		}
		os.Exit(0)
	}()
	app.Main()
}
