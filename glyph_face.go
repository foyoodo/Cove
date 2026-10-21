package main

import (
	"math"
	"reflect"

	"github.com/egoist/mygo/ui"
)

// shapedGlyph is one cluster after the system shaper, including font
// fallback. The font pointer is the face that draws the glyph, not the
// family the run asked for.
type shapedGlyph struct {
	text     string
	x        float32
	advance  float32
	ascent   float32
	descent  float32
	id       uint32
	native   uintptr
	fallback bool
	family   string
}

// shapeGlyphs shapes sample with the UI font the controls use. A glyph
// whose face is not the face of "H" in the same style was chosen by
// fallback (on macOS, often a symbol font for ⇧ ↵ ⌘).
func shapeGlyphs(sample string, size float32, weight int) []shapedGlyph {
	if weight <= 0 {
		weight = 400
	}
	face := ui.Font{Size: size, Weight: weight}
	gs := ui.Shape(sample, face)
	hs := ui.Shape("H", face)
	var primary uintptr
	if len(hs) > 0 {
		primary = glyphNative(hs[0])
	}
	out := make([]shapedGlyph, 0, len(gs))
	rs := []rune(sample)
	for _, g := range gs {
		native, ascent, descent := glyphFace(g)
		text := ""
		if g.Cluster >= 0 && g.Cluster < len(rs) {
			n := g.Runes
			if n < 1 {
				n = 1
			}
			end := g.Cluster + n
			if end > len(rs) {
				end = len(rs)
			}
			text = string(rs[g.Cluster:end])
		}
		out = append(out, shapedGlyph{
			text:     text,
			x:        g.X,
			advance:  g.Advance,
			ascent:   ascent,
			descent:  descent,
			id:       glyphID(g),
			native:   native,
			fallback: native != 0 && primary != 0 && native != primary,
			family:   fontFamilyName(native),
		})
	}
	return out
}

func glyphNative(g ui.Glyph) uintptr {
	n, _, _ := glyphFace(g)
	return n
}

func glyphID(g ui.Glyph) uint32 {
	f := reflect.ValueOf(g).FieldByName("id")
	if !f.IsValid() || f.Kind() != reflect.Uint32 {
		return 0
	}
	return uint32(f.Uint())
}

// faceBox is the ink of the font that draws a string, after fallback.
// left and right are from the start of the pen. top and bot are y-down
// from the baseline. ascent and descent are that face's em metrics.
type faceBox struct {
	left, right float32
	top, bot    float32
	ascent      float32
	descent     float32
	ok          bool
}

func faceOf(sample string, size float32, weight int) faceBox {
	if weight <= 0 {
		weight = 400
	}
	key := opticalKey{sample: "face:" + sample, size: size, lineH: 0, weight: weight}
	opticalMu.Lock()
	if box, ok := faceCache[key]; ok {
		opticalMu.Unlock()
		return box
	}
	opticalMu.Unlock()
	box := measureFace(sample, size, weight)
	opticalMu.Lock()
	faceCache[key] = box
	opticalMu.Unlock()
	return box
}

var faceCache = map[opticalKey]faceBox{}

func measureFace(sample string, size float32, weight int) faceBox {
	gs := shapeGlyphs(sample, size, weight)
	var box faceBox
	for _, g := range gs {
		if !box.ok {
			box.ascent, box.descent = g.ascent, g.descent
		} else {
			if g.ascent > box.ascent {
				box.ascent = g.ascent
			}
			if g.descent > box.descent {
				box.descent = g.descent
			}
		}
		l, top, r, bot, ok := glyphFaceBounds(g.native, g.id)
		if !ok {
			continue
		}
		l += g.x
		r += g.x
		if !box.ok {
			box.left, box.right, box.top, box.bot = l, r, top, bot
			box.ok = true
			continue
		}
		if l < box.left {
			box.left = l
		}
		if r > box.right {
			box.right = r
		}
		if top < box.top {
			box.top = top
		}
		if bot > box.bot {
			box.bot = bot
		}
	}
	return box
}

// symbolBaseline is the baseline mygo uses inside a line of elemH.
// The line's ascent and descent are the primary face grown to fit the
// fallback face, and the extra line height is split above and below.
func symbolBaseline(elemH, primAscent, primDescent, faceAscent, faceDescent float32) float32 {
	ascent, descent := primAscent, primDescent
	if faceAscent > ascent {
		ascent = faceAscent
	}
	if faceDescent > descent {
		descent = faceDescent
	}
	return (elemH-ascent-descent)/2 + ascent
}

// glyphFace reads the shaper's font for g. The face pointer is unexported
// on ui.Glyph; it is the font after fallback.
func glyphFace(g ui.Glyph) (native uintptr, ascent, descent float32) {
	f := reflect.ValueOf(g).FieldByName("font")
	if !f.IsValid() || f.Kind() != reflect.Pointer || f.IsNil() {
		return 0, 0, 0
	}
	type head struct {
		size, ascent, descent, lineGap float32
		native                         uintptr
	}
	h := (*head)(f.UnsafePointer())
	if h == nil {
		return 0, 0, 0
	}
	return h.native, h.ascent, h.descent
}

// chooseSpacer is the device-pixel gap between two ink boxes. The next
// origin is the whole pixel that leaves target points past the previous
// run's ink, after that edge is rounded to a pixel.
func chooseSpacer(a, b paintedBox, target, scale float32) float32 {
	if scale < 2 {
		return target - (a.elemW - a.right) - b.left
	}
	prev := math.Round(float64(a.right * scale))
	origin := math.Round(prev + float64((target-b.left)*scale))
	gapOf := func(px float64) float64 {
		return (px + float64(b.left*scale) - float64(a.right*scale)) / float64(scale)
	}
	best := origin
	if math.Abs(gapOf(origin)-float64(target)) > 0.25 {
		bestErr := math.Abs(gapOf(origin) - float64(target))
		for _, cand := range []float64{origin - 1, origin + 1} {
			if err := math.Abs(gapOf(cand) - float64(target)); err < bestErr {
				best, bestErr = cand, err
			}
		}
	}
	return float32(best)/scale - a.elemW
}

func shapedAdvance(sample string, size float32, weight int) float32 {
	var adv float32
	for _, g := range shapeGlyphs(sample, size, weight) {
		adv += g.advance
	}
	return adv
}

// widenUnmeasured replaces an empty or tiny ink box with the advance of
// the face that actually draws the glyph. A missing-glyph slot in the
// primary font reports no ink while the fallback face draws a wide mark,
// and a spacer built from the empty box pulls the next run on top of it.
func widenUnmeasured(sample string, size float32, weight int, left, right, elemW float32) (float32, float32, float32, bool) {
	rs := []rune(sample)
	if len(rs) != 1 || !isShortcutSymbol(rs[0]) {
		return left, right, elemW, false
	}
	adv := shapedAdvance(sample, size, weight)
	if adv <= 0 {
		return left, right, elemW, false
	}
	inkW := right - left
	if inkW >= adv*0.45 {
		return left, right, elemW, false
	}
	if elemW < adv {
		elemW = adv
	}
	return 0, adv, elemW, true
}

// capAlignDy moves a symbol so its ink centre meets the cap-height centre.
// y grows downward. A symbol whose ink sits low (larger centre) gets a
// negative shift. cap and sym are centres measured from the same line top.
func capAlignDy(capCenter, symCenter float32) float32 {
	return capCenter - symCenter
}

// faceInkFraction is how narrow a pixel probe may be, next to the ink of
// the face that draws the glyph, before the probe is treated as the wrong
// face or an empty one. On macOS the primary UI font often has no ⇧ ↵ ⌘.
// Core Text draws a fallback face, and a probe that misses that face (or
// reads the primary font's empty slot) reports a box far narrower than
// the mark on screen. The spacer then pulls the next run on top of it,
// and a missing vertical box leaves the mark on the baseline, below the
// caps. A probe of the real mark stays within this fraction: the Linux
// face's tightest shortcut is still about three quarters of its ink.
const faceInkFraction = 0.65

// inkFromDrawingFont returns the probe's horizontal ink, or the drawing
// face's ink when the probe is empty or far narrower than that face.
// usedFace reports the substitution. elemW stays the layout width.
func inkFromDrawingFont(sample string, size float32, weight int, left, right, elemW float32) (float32, float32, float32, bool) {
	rs := []rune(sample)
	if len(rs) != 1 || !isShortcutSymbol(rs[0]) {
		return left, right, elemW, false
	}
	face := faceOf(sample, size, weight)
	fw := face.right - face.left
	if face.ok && fw > 0 && right-left < fw*faceInkFraction {
		return face.left, face.right, elemW, true
	}
	l, r, w, _ := widenUnmeasured(sample, size, weight, left, right, elemW)
	return l, r, w, false
}
