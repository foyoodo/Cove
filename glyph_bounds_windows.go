//go:build !darwin && !linux

package main

// glyphFaceBounds has no face-ink helper on Windows (or other non-Pango
// systems). Layout still uses the rasterizer's ink; a missing face bound
// is the same as a Pango lookup that failed.
func glyphFaceBounds(uintptr, uint32) (left, top, right, bot float32, ok bool) {
	return 0, 0, 0, 0, false
}
