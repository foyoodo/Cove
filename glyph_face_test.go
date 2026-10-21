package main

import (
	"testing"
)

func TestFaceInkFromDrawingFont(t *testing.T) {
	for _, s := range []string{"H", "↵", "⇧", "⌘", "·", "send"} {
		face := faceOf(s, 11, 400)
		if !face.ok {
			t.Fatalf("%s: the drawing font returned no ink", s)
		}
		if face.right <= face.left || face.bot <= face.top {
			t.Fatalf("%s: empty ink %+v", s, face)
		}
		t.Logf("%s left=%.2f right=%.2f top=%.2f bot=%.2f ascent=%.2f descent=%.2f",
			s, face.left, face.right, face.top, face.bot, face.ascent, face.descent)
	}
	gs := shapeGlyphs("↵", 11, 400)
	if len(gs) == 0 || !gs[0].fallback {
		t.Fatal("↵ should come from a fallback face")
	}
	h := faceOf("H", 11, 400)
	if h.top >= 0 {
		t.Fatalf("H ink top %.2f is not above the baseline", h.top)
	}
	// Cap height sits well above the baseline and the ink does not
	// reach the descender. A flipped y would put the top below it.
	if h.top > -4 || h.bot > 1 {
		t.Fatalf("H ink %.2f..%.2f is not a cap", h.top, h.bot)
	}
	for _, s := range []string{"↵", "⇧", "⌘", "⌥", "⌃", "⌫"} {
		px := opticalOf(s, 11, 13, 400)
		face := faceOf(s, 11, 400)
		pw, fw := px.right-px.left, face.right-face.left
		if px.usedFace {
			t.Errorf("%s: the pixel probe was replaced on a font the probe can see", s)
		}
		if fw > 0 && pw < fw*faceInkFraction {
			t.Errorf("%s probe %.2f is under %.0f%% of the drawing face %.2f", s, pw, faceInkFraction*100, fw)
		}
	}
}

// An empty probe is what macOS returns when the primary face has no
// symbol and the pixel read misses the fallback. The gap has to come
// from the face that draws the mark, and the mark's ink centre has to
// meet the cap centre.
func TestMissingProbeUsesFaceInk(t *testing.T) {
	face := faceOf("↵", 11, 400)
	if !face.ok {
		t.Fatal("↵ has no face ink")
	}
	h := opticalOf("H", 11, 13, 400)
	got := finishSymbolOptical("↵", 11, 13, 400, opticalBox{elemW: 9, elemH: h.elemH}, false)
	if !got.usedFace {
		t.Fatal("empty probe should take the drawing face")
	}
	if got.right-got.left < face.right-face.left-0.05 {
		t.Fatalf("ink width %.2f, face %.2f", got.right-got.left, face.right-face.left)
	}
	centre := (got.top+got.bot)/2 + got.dy
	target := (h.top+h.bot)/2 + h.dy
	if centre < target-0.25 || centre > target+0.25 {
		t.Fatalf("ink centre %.3f, cap centre %.3f", centre, target)
	}
	// Two wide SF-like marks with no pixel ink still keep 1pt between them.
	a := paintedBox{elemW: got.elemW, left: got.left, right: got.right}
	b := a
	sp := chooseSpacer(a, b, 1, 2)
	gap := (a.elemW - a.right) + sp + b.left
	if gap < 0.75 || gap > 1.25 {
		t.Fatalf("gap from face ink %.3f", gap)
	}
	// A fallback face taller than the word grows only the symbol's line.
	// The row centres the two boxes, and the word's cap is at the word's
	// centre, so the symbol's ink has to sit at its own centre.
	tall := finishSymbolOptical("↵", 11, 13, 400, opticalBox{elemW: 9, elemH: h.elemH + 8}, false)
	tallCentre := (tall.top+tall.bot)/2 + tall.dy
	if tallCentre < tall.elemH/2-0.25 || tallCentre > tall.elemH/2+0.25 {
		t.Fatalf("tall symbol centre %.3f, box centre %.3f", tallCentre, tall.elemH/2)
	}
}

// SF-like marks are wider than the Linux face and their ink centre sits
// below the cap centre. The spacer must still leave 1pt of ink, and the
// shift must lift the symbol onto the cap centre.
func TestWideSymbolKeepsInkGap(t *testing.T) {
	shift := shapedGlyph{advance: 11}
	before := paintedBox{elemW: shift.advance, left: 0.3, right: 10.7}
	after := paintedBox{elemW: 11, left: 0.4, right: 10.5}
	sp := chooseSpacer(before, after, 1, 2)
	got := (before.elemW - before.right) + sp + after.left
	if got < 0.75 || got > 1.25 {
		t.Fatalf("⇧→↵ ink gap %.3f, want 1±0.25", got)
	}
	// y down: cap centre at 4, symbol ink centre at 7.5.
	dy := capAlignDy(4, 7.5)
	if dy > -3.2 || dy < -3.8 {
		t.Fatalf("dy %.3f, want about -3.5 so the low symbol meets the cap", dy)
	}
	landed := float32(7.5) + dy
	if landed < 3.75 || landed > 4.25 {
		t.Fatalf("symbol centre lands at %.3f, cap is 4", landed)
	}
}

func TestEmptySymbolUsesDrawnAdvance(t *testing.T) {
	// Primary font reports no ink. The fallback face's advance is 10.
	// Without the widen, the spacer would be target−elemW and the next
	// run would cover the symbol.
	left, right, elemW, widened := widenUnmeasured("⇧", 11, 400, 0, 0, 8)
	if !widened {
		t.Fatal("empty ink should take the shaped advance")
	}
	if right-left < 1 || elemW < right {
		t.Fatalf("ink %.2f..%.2f elem %.2f", left, right, elemW)
	}
	a := paintedBox{elemW: elemW, left: left, right: right}
	b := paintedBox{elemW: elemW, left: left, right: right}
	sp := chooseSpacer(a, b, 1, 2)
	got := (a.elemW - a.right) + sp + b.left
	if got < 0.75 || got > 1.25 {
		t.Fatalf("gap from empty bounds %.3f", got)
	}
}
