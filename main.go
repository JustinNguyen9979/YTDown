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
		Height:     560,
		MinWidth:   700,
		MinHeight:  560,
		MaxWidth:   700,
		MaxHeight:  560,
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
