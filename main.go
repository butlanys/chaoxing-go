// chaoxing-go 超星学习通自动化助手（Go 重写版 / Wails GUI）
//
// 原项目: https://github.com/Samueli924/chaoxing (Python, GPL-3.0)
// 本项目遵循 GPL-3.0，仅供学习讨论，禁止用于盈利。
package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "超星学习通助手 · chaoxing-go",
		Width:     1280,
		Height:    840,
		MinWidth:  1024,
		MinHeight: 700,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []any{
			app,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
		},
		Linux: &linux.Options{
			ProgramName: "chaoxing-go",
		},
	})
	if err != nil {
		println("启动失败:", err.Error())
	}
}
