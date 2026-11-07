package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// opaqueWindowAlpha is full opacity.
// A value of 1 is nearly transparent.
const opaqueWindowAlpha uint8 = 255

const (
	// minimumWindowWidth is 1136 pixels, because narrower layouts compress the workbench panes.
	minimumWindowWidth = 1136
	// minimumWindowHeight is 874 pixels, because shorter layouts clip the work area.
	minimumWindowHeight = 874
	// initialWindowWidth opens at the minimum, because the workbench should start without clipping.
	initialWindowWidth = minimumWindowWidth
	// initialWindowHeight opens at the minimum, because the workbench should start without clipping.
	initialWindowHeight = minimumWindowHeight
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	err := wails.Run(&options.App{
		Title:     "nup-gui",
		Width:     initialWindowWidth,
		Height:    initialWindowHeight,
		MinWidth:  minimumWindowWidth,
		MinHeight: minimumWindowHeight,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: opaqueWindowAlpha},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:  true,
			CSSDropProperty: "--wails-drop-target",
			CSSDropValue:    "drop",
		},
		OnStartup: app.startup,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
