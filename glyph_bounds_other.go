//go:build !darwin

package main

import (
	"sync"

	"github.com/ebitengine/purego"
)

// pangoRect is PangoRectangle, in 1/1024 of a point, y down from the baseline.
type pangoRect struct {
	x, y, width, height int32
}

const pangoUnit = 1024

var (
	pangoExtents     func(font uintptr, glyph uint32, ink, logical *pangoRect)
	pangoExtentsOnce sync.Once
	pangoExtentsOK   bool
)

func ensureFaceBounds() {
	pangoExtentsOnce.Do(func() {
		lib, err := purego.Dlopen("libpango-1.0.so.0", purego.RTLD_LAZY)
		if err != nil {
			return
		}
		purego.RegisterLibFunc(&pangoExtents, lib, "pango_font_get_glyph_extents")
		pangoExtentsOK = pangoExtents != nil
	})
}

// glyphFaceBounds is the ink of one glyph in the face that draws it,
// in points. top and bot are y-down from the baseline.
func glyphFaceBounds(native uintptr, id uint32) (left, top, right, bot float32, ok bool) {
	ensureFaceBounds()
	if !pangoExtentsOK || native == 0 || id == 0x0FFFFFFF {
		return 0, 0, 0, 0, false
	}
	var ink, logical pangoRect
	pangoExtents(native, id, &ink, &logical)
	if ink.width <= 0 || ink.height <= 0 {
		return 0, 0, 0, 0, false
	}
	s := float32(pangoUnit)
	return float32(ink.x) / s, float32(ink.y) / s, float32(ink.x+ink.width) / s, float32(ink.y+ink.height) / s, true
}
