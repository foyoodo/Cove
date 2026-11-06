//go:build !darwin

package main

import "github.com/egoist/mygo"

// fitWindowToWorkArea sizes a new window so it fits the primary monitor's
// work area (taskbar / panels excluded) with a small margin, then MyGo
// centers it. The size never drops below minW×minH.
func fitWindowToWorkArea(wantW, wantH, minW, minH int) (w, h int) {
	d := mygo.Screen.PrimaryDisplay()
	return fitWindowSize(wantW, wantH, minW, minH, d.WorkArea.Width, d.WorkArea.Height, windowFitMargin)
}
