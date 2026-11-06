//go:build darwin

package main

// fitWindowToWorkArea is a no-op on macOS: the first window stays
// 1120×760, and Mac shot tests keep that size.
func fitWindowToWorkArea(wantW, wantH, minW, minH int) (w, h int) {
	return wantW, wantH
}
