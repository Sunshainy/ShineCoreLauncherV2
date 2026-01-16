// ShineCore Launcher
// This is the main entry point for the Wails application.
package main

import (
	"embed"
	"log/slog"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"shinecore/internal/app"
	"shinecore/internal/build"
	"shinecore/internal/logging"
)

//go:embed frontend/dist
var assets embed.FS

func main() {
	// Initialize logging
	logging.Init()

	slog.Info("starting ShineCore",
		"version", build.Version,
		"release", build.Release,
		"platform", build.OS(),
		"arch", build.Arch(),
	)

	// Create the application instance
	application := app.New()

	// Run the Wails application
	err := wails.Run(&options.App{
		Title:     "ShineCore",
		Width:     1024,
		Height:    640,
		MinWidth:  1024,
		MinHeight: 640,
		MaxWidth:  1024,
		MaxHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        application.Startup,
		OnDomReady:       application.DomReady,
		Bind: []interface{}{
			application,
		},
		Debug: options.Debug{
			OpenInspectorOnStartup: false,
		},
		Frameless: true,
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			DisableWindowIcon:    false,
		},
		Mac: &mac.Options{
			TitleBar: &mac.TitleBar{
				TitlebarAppearsTransparent: true,
				HideTitle:                  false,
				HideTitleBar:               false,
				FullSizeContent:            true,
				UseToolbar:                 false,
				HideToolbarSeparator:       true,
			},
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
		},
		Linux: &linux.Options{
			ProgramName: "ShineCore",
		},
	})

	if err != nil {
		slog.Error("application error", "error", err)
		os.Exit(1)
	}
}
