//go:build darwin

package main

import (
	"structs"
	"sync"

	"github.com/ebitengine/purego"
)

// cgRect matches Core Graphics. y grows up from the baseline.
type cgRect struct {
	_          structs.HostLayout
	x, y, w, h float64
}

var (
	ctBounds     func(font uintptr, orientation uint32, glyphs *uint16, rects *cgRect, n int) cgRect
	ctBoundsOnce sync.Once
	ctBoundsOK   bool
)

func ensureFaceBounds() {
	ctBoundsOnce.Do(func() {
		ct, err := purego.Dlopen("/System/Library/Frameworks/CoreText.framework/CoreText", purego.RTLD_LAZY)
		if err != nil {
			return
		}
		purego.RegisterLibFunc(&ctBounds, ct, "CTFontGetBoundingRectsForGlyphs")
		ctBoundsOK = ctBounds != nil
	})
}

// glyphFaceBounds is the ink of one glyph in the face that draws it,
// in points. top and bot are y-down from the baseline.
func glyphFaceBounds(native uintptr, id uint32) (left, top, right, bot float32, ok bool) {
	ensureFaceBounds()
	if !ctBoundsOK || native == 0 {
		return 0, 0, 0, 0, false
	}
	g := uint16(id)
	var r cgRect
	ctBounds(native, 0, &g, &r, 1)
	if r.w <= 0 || r.h <= 0 {
		return 0, 0, 0, 0, false
	}
	return float32(r.x), float32(-(r.y + r.h)), float32(r.x + r.w), float32(-r.y), true
}
