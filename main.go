package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	application := NewApp()
	err := wails.Run(&options.App{
		Title:                    "WAV Property Attacher",
		Width:                    1320,
		Height:                   820,
		MinWidth:                 1080,
		MinHeight:                680,
		BackgroundColour:         &options.RGBA{R: 12, G: 18, B: 28, A: 255},
		AssetServer:              &assetserver.Options{Assets: assets},
		OnStartup:                application.startup,
		OnShutdown:               application.shutdown,
		EnableDefaultContextMenu: true,
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:  true,
			CSSDropProperty: "--wails-drop-target",
			CSSDropValue:    "drop",
		},
		Windows: &windows.Options{
			Theme: windows.SystemDefault,
		},
		Bind: []interface{}{application},
	})
	if err != nil {
		log.Fatal(err)
	}
}
