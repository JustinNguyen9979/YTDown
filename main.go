package main

import (
	"context"
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:      "YTDown",
		Width:      700,
		Height:     450, // Initial height
		MinWidth:   700,
		MinHeight:  300, // Very low minimum to allow hugging
		MaxWidth:   700,
		MaxHeight:  900, // Sufficient maximum for long lists
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 27, B: 27, A: 1},
		OnBeforeClose: func(ctx context.Context) (prevent bool) {
			return false
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		panic(err)
	}
}
