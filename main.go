package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := application.New(application.Options{
		Name:        "Brewctl",
		Description: "A control center for Homebrew",
		Services: []application.Service{
			application.NewService(&BrewService{}),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		// Only used by the browser build (-tags server).
		Server: application.ServerOptions{
			Host: "127.0.0.1",
			Port: 9246,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "Brewctl",
		Width:     1180,
		Height:    760,
		MinWidth:  900,
		MinHeight: 560,
		Mac: application.MacWindow{
			Backdrop: application.MacBackdropTranslucent,
			TitleBar: application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
