package main

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
)

// appIconPNG is the same art as resources/Cove.icns and icon/icon-1024.png:
// a dark graphite squircle, a white bubble, and the tide wave. It is drawn
// in those colors. Theme presets do not tint it.
//
//go:embed icon/icon-1024.png
var appIconPNG []byte

var coveAppIcon = mustCoveAppIcon()

func mustCoveAppIcon() *ui.Bitmap {
	b, err := ui.DecodeBitmap(appIconPNG)
	if err != nil {
		panic(err)
	}
	return b
}

// appIcon is the product mark for the empty-state hero and Settings › About.
// Message avatars stay on mark, which follows the theme.
func (a *App) appIcon(c *ui.Context, size float32) {
	ui.Image(c, coveAppIcon).Size(size, size).Shrink(0).Label("Cove app icon")
}
