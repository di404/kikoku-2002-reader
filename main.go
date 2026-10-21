package main

import (
	"embed"
	"log"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var frontend embed.FS

func main() {
	if len(os.Args) > 2 && os.Args[1] == "-preview" {
		runPreview(os.Args[2])
		return
	}
	app := NewApp()
	err := wails.Run(&options.App{
		Title:     "鬼哭街",
		Width:     1280,
		Height:    960,
		MinWidth:  640,
		MinHeight: 480,
		AssetServer: &assetserver.Options{
			Assets:  frontend,
			Handler: assetHandler{app},
		},
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 255},
		OnStartup:        app.startup,
		OnDomReady:       app.domReady,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
		Mac: &mac.Options{
			TitleBar:   mac.TitleBarHiddenInset(),
			Appearance: mac.NSAppearanceNameDarkAqua,
			About:      &mac.AboutInfo{Title: "鬼哭街 原版阅读器", Message: "Nitroplus《鬼哭街》2002 原版 · 妹乃萌汉化"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
