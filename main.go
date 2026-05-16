package main

import (
	"context"
	"embed"
	platform "ytdown/flatform"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend
var assets embed.FS

// Version is set during build using -ldflags
var Version = "Dev"

// Global platform manager for consistent path resolution
var platformManager platform.Manager

func main() {
	if err := InitLogger(); err != nil {
		panic(err)
	}
	defer CloseLogger()

	platformManager = platform.NewManager()
	app := NewAppWithManager(platformManager)

	err := wails.Run(&options.App{
		Title:      "YTDown",
		Width:      1500,
		Height:     450, // Initial height
		MinWidth:   1400,
		MinHeight:  520, // Very low minimum to allow hugging
		MaxWidth:   0,
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
