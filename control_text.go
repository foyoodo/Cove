package main

import (
	"math"
	"reflect"
	"strings"
	"sync"
	"unicode"
	"unsafe"

	"cove/tools/inkgap"

	"github.com/egoist/mygo/ui"
)

// controlOpticsOff draws single-line controls on the font line box, the
// way they sat before cap-height centering. Tests use it for the before crops.
var controlOpticsOff bool

// A shortcut symbol is centered on the line's cap-height center from its
// own ink, not laid on the baseline. esc is a word, so it stays text.
func isShortcutSymbol(r rune) bool {
	switch r {
	case '↵', '⇧', '⌘', '⌥', '⌃', '⌫', '⌦', '↑', '↓', '←', '→':
		return true
	default:
		return false
	}
}

type controlPart struct {
	text string
	sym  bool
	sep  bool // middle dot, with a 5pt ink gap on each side
}

// splitControlLine keeps a middle dot as its own separator. Symbols
// become their own parts so each one can sit on the cap-height center.
// A single letter, digit, or punctuation key in the combo (L in ⌘L, the
// comma in ⌘,, the period in ⌘.) is a symbol too, so it takes the 1pt
// ink gap. The word after the combo stays text and takes 3pt. A · takes
// 5pt of ink on each side. Spaces that only separated those parts are
// dropped; the row supplies the ink gap.
func splitControlLine(s string) []controlPart {
	rs := []rune(s)
	var parts []controlPart
	for i := 0; i < len(rs); {
		if unicode.IsSpace(rs[i]) {
			i++
			continue
		}
		if rs[i] == '·' {
			parts = append(parts, controlPart{text: "·", sep: true})
			i++
			continue
		}
		if isShortcutSymbol(rs[i]) || comboKeyAt(rs, i) {
			var b strings.Builder
			for i < len(rs) {
				if unicode.IsSpace(rs[i]) {
					k := i
					for k < len(rs) && unicode.IsSpace(rs[k]) {
						k++
					}
					if k < len(rs) && (isShortcutSymbol(rs[k]) || comboKeyAt(rs, k)) {
						i = k
						continue
					}
					break
				}
				if !isShortcutSymbol(rs[i]) && !comboKeyAt(rs, i) {
					break
				}
				b.WriteRune(rs[i])
				i++
			}
			if b.Len() > 0 {
				parts = append(parts, controlPart{text: b.String(), sym: true})
			}
			continue
		}
		var b strings.Builder
		for i < len(rs) && !isShortcutSymbol(rs[i]) && rs[i] != '·' {
			b.WriteRune(rs[i])
			i++
		}
		if text := strings.TrimSpace(b.String()); text != "" {
			parts = append(parts, controlPart{text: text, sym: false})
		}
	}
	return parts
}

func isKeyLetter(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
}

func isKeyDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

// A comma or a period is the key of ⌘, and ⌘., not a word.
func isKeyPunct(r rune) bool {
	return r == ',' || r == '.'
}

// comboKeyAt reports one key in a shortcut combo: a letter, a digit, or
// punctuation sitting beside a modifier. "Return" is a word, and "3.14"
// keeps its period. The mid-dot is not a key.
func comboKeyAt(rs []rune, i int) bool {
	if i < 0 || i >= len(rs) {
		return false
	}
	r := rs[i]
	switch {
	case isKeyLetter(r) || isKeyDigit(r):
		if i > 0 && (isKeyLetter(rs[i-1]) || isKeyDigit(rs[i-1])) {
			return false
		}
		if i+1 < len(rs) && (isKeyLetter(rs[i+1]) || isKeyDigit(rs[i+1])) {
			return false
		}
	case isKeyPunct(r):
		if i > 0 && (isKeyLetter(rs[i-1]) || isKeyDigit(rs[i-1])) && i+1 < len(rs) && (isKeyLetter(rs[i+1]) || isKeyDigit(rs[i+1])) {
			return false
		}
	default:
		return false
	}
	return besideSymbol(rs, i)
}

func besideSymbol(rs []rune, i int) bool {
	for j := i - 1; j >= 0; j-- {
		if unicode.IsSpace(rs[j]) {
			continue
		}
		return isShortcutSymbol(rs[j])
	}
	for j := i + 1; j < len(rs); j++ {
		if unicode.IsSpace(rs[j]) {
			continue
		}
		return isShortcutSymbol(rs[j])
	}
	return false
}

// controlGap is the ink gap. A middle dot has 5pt on each side. Keys in a
// combo have 1pt. A symbol and a word have 3pt.
func controlGap(prev, next controlPart) float32 {
	if prev.sep || next.sep {
		return 5
	}
	if prev.sym && next.sym {
		return 1
	}
	return 3
}

// controlSpacer is the box (or negative margin) that leaves controlGap
// points between the two parts' ink. Side bearings come from the cached
// glyph bounds, so the gap is not the distance between element boxes.
func controlSpacer(prev, next controlPart, size, lineH float32, weight int) float32 {
	return inkSpacer(partEdge(prev, false), partEdge(next, true), size, lineH, weight, controlGap(prev, next))
}

func partEdge(p controlPart, leading bool) string {
	if !p.sym {
		return p.text
	}
	rs := []rune(p.text)
	if len(rs) == 0 {
		return ""
	}
	if leading {
		return string(rs[0])
	}
	return string(rs[len(rs)-1])
}

func inkSpacer(prev, next string, size, lineH float32, weight int, want float32) float32 {
	return specSpacer(prev, size, lineH, next, size, lineH, weight, want)
}

// gridSpacer places the next run from the previous run's drawn ink.
// The text stack draws a glyph at one position per device pixel (the pen
// is floored, and on Linux the glyph cache has one slot), so a fractional
// origin does not move the ink and a fractional translate resamples it.
// The previous ink edge is rounded to a device pixel, and the next origin
// is the whole pixel that leaves the target gap past that edge. Each run
// then starts from where the last one actually landed, and a shortfall
// cannot add into the next gap.
func gridSpacer(before string, beforeSize, beforeLine float32, after string, afterSize, afterLine float32, weight int, target, scale float32, light bool) (spacer, nudge float32) {
	if scale < 2 {
		return specSpacer(before, beforeSize, beforeLine, after, afterSize, afterLine, weight, target), 0
	}
	a := paintedInk(before, beforeSize, beforeLine, weight, scale, light)
	b := paintedInk(after, afterSize, afterLine, weight, scale, light)
	return chooseSpacer(a, b, target, scale), 0
}

// specSpacer is SPEC §8. The spacer width is the target ink gap minus the
// right side bearing of the glyph before minus the left side bearing of
// the glyph after. Both bearings come from the cached opticalOf ink bounds.
// A negative result overlaps the boxes; nothing in the width is a fixed nudge.
func specSpacer(before string, beforeSize, beforeLine float32, after string, afterSize, afterLine float32, weight int, target float32) float32 {
	a := opticalOf(before, beforeSize, beforeLine, weight)
	b := opticalOf(after, afterSize, afterLine, weight)
	return target - (a.elemW - a.right) - b.left
}

// controlLine is one single-line control label. Latin text is shifted so
// its cap-height center is the line box center. Han text uses the Han ink
// center. Each shortcut symbol is its own glyph, ink-centered on that same
// line, 3pt from text and 1pt from the next symbol. A symbol-only label is
// also shifted so the ink box is centered horizontally in the row.
func controlLine(c *ui.Context, s string, size, lineH float32, weight int, color ui.Color) ui.Element {
	return drawControlLine(c, s, size, lineH, weight, color, true)
}

// controlLineFlow is controlLine without the horizontal ink-centering
// shift. The selection bar uses it: the SPEC §8 spacer is the only
// distance between the label and ⌘.
func controlLineFlow(c *ui.Context, s string, size, lineH float32, weight int, color ui.Color) ui.Element {
	return drawControlLine(c, s, size, lineH, weight, color, false)
}

func drawControlLine(c *ui.Context, s string, size, lineH float32, weight int, color ui.Color, centerX bool) ui.Element {
	parts := splitControlLine(s)
	if controlOpticsOff || len(parts) == 0 {
		t := ui.Text(c, s).FontSize(size).TextColor(color).SingleLine().Shrink(0).Label(s)
		if weight > 0 {
			t.FontWeight(weight)
		}
		if lineH > 0 {
			t.FixedLineHeight(lineH)
		}
		return t
	}
	onlySym := true
	for _, p := range parts {
		if !p.sym {
			onlySym = false
			break
		}
	}
	scale := windowScale(c)
	// Dark windows draw light glyphs. The probe has to use that polarity or
	// the half-coverage edge sits on the other side of the pixel.
	light := windowDark(c)
	row := ui.Row(c).Gap(0).AlignItems(ui.Center).Shrink(0).Label(s)
	if centerX && onlySym {
		if dx := symbolRowDX(parts, size, lineH, weight, scale, light); dx != 0 {
			row.Translate(dx, 0)
		}
	}
	hasSym := false
	for _, p := range parts {
		if p.sym || p.sep {
			hasSym = true
			break
		}
	}
	row.Children(func() {
		// Whole pixel ahead of a shortcut that shares its row with a word.
		// The painter floors each pen, and this keeps that floor on one
		// pixel for the whole line. Centered symbol rows do not take it.
		if scale >= 2 && hasSym && !onlySym {
			ui.Box(c).Width(1 / scale).Shrink(0)
		}
		for i, p := range parts {
			gap, nudge := float32(0), float32(0)
			if i > 0 {
				gap, nudge = gridSpacer(partEdge(parts[i-1], false), size, lineH, partEdge(p, true), size, lineH, weight, controlGap(parts[i-1], p), scale, light)
			}
			placeNudge := func(el ui.Element) {
				if nudge != 0 {
					el.Translate(nudge, 0)
				}
			}
			if p.sep {
				openInkGap(c, gap)
				dot := controlText(c, "·", size, lineH, weight, color).Label("·")
				if gap < 0 {
					dot.MarginStart(gap)
				}
				placeNudge(dot)
				continue
			}
			if p.sym {
				runes := []rune(p.text)
				place := func(r rune) ui.Element {
					if isShortcutSymbol(r) {
						return symbolGlyph(c, string(r), size, lineH, weight, color)
					}
					// A letter, digit, comma, or period keeps the text
					// baseline. Ink-centering it would lift it off the line.
					return controlText(c, string(r), size, lineH, weight, color).Label(string(r))
				}
				// A run of symbols is its own row so tests can find "⇧↵"
				// or "⌘L" without taking the first mark on the line.
				if len(runes) > 1 {
					openInkGap(c, gap)
					el := ui.Row(c).Gap(0).AlignItems(ui.Center).Shrink(0).Label(p.text)
					el.Children(func() {
						for j, r := range runes {
							g, n := float32(0), float32(0)
							if j > 0 {
								g, n = gridSpacer(string(runes[j-1]), size, lineH, string(r), size, lineH, weight, 1, scale, light)
							}
							openInkGap(c, g)
							glyph := place(r)
							if g < 0 {
								glyph.MarginStart(g)
							}
							if n != 0 {
								glyph.Translate(n, 0)
							}
						}
					})
					if gap < 0 {
						el.MarginStart(gap)
					}
					placeNudge(el)
					continue
				}
				openInkGap(c, gap)
				glyph := place(runes[0])
				if gap < 0 {
					glyph.MarginStart(gap)
				}
				placeNudge(glyph)
				continue
			}
			openInkGap(c, gap)
			text := controlText(c, p.text, size, lineH, weight, color)
			if gap < 0 {
				text.MarginStart(gap)
			}
			placeNudge(text)
		}
		// Whole pixel after the last run. A line packed against the
		// trailing edge, such as the composer hint, lands on it here.
		// The gaps between runs are unchanged.
		if scale >= 2 && !onlySym {
			ui.Box(c).Width(1 / scale).Shrink(0)
		}
	})
	return row
}

// openInkGap inserts a spacer when the ink gap is wider than the side
// bearings. A negative gap overlaps the boxes with a margin instead,
// because a box cannot have a negative width.
func openInkGap(c *ui.Context, gap float32) {
	if gap > 0 {
		ui.Box(c).Width(gap).Shrink(0)
	}
}

// controlText is single-line control text centered by cap height, or by
// Han ink when the label is CJK. The shift moves the glyphs inside the
// line box, so a parent that centers the line box centers the cap line.
func controlText(c *ui.Context, s string, size, lineH float32, weight int, color ui.Color) ui.Element {
	t := ui.Text(c, s).FontSize(size).TextColor(color).SingleLine().Shrink(0)
	if weight > 0 {
		t.FontWeight(weight)
	}
	if lineH > 0 {
		t.FixedLineHeight(lineH)
	}
	box := opticalOf(opticalSample(s), size, lineH, weight)
	if box.elemH > 0 {
		t.Height(box.elemH)
	}
	if !controlOpticsOff && box.dy != 0 {
		t.Translate(0, box.dy)
	}
	return t
}

func symbolGlyph(c *ui.Context, sym string, size, lineH float32, weight int, color ui.Color) ui.Element {
	return placedGlyph(c, sym, size, lineH, weight, color, false)
}

// controlKey is one keycap label. ⌘ and ⌥ are ink-centered. A letter,
// digit, comma, or period stays on the text baseline, the same as L, and
// is only shifted sideways so the ink sits in the middle of the cap.
func controlKey(c *ui.Context, key string, size float32, weight int, color ui.Color) ui.Element {
	rs := []rune(key)
	if len(rs) == 1 && isShortcutSymbol(rs[0]) {
		return placedGlyph(c, key, size, 0, weight, color, true)
	}
	t := controlText(c, key, size, 0, weight, color)
	if len(rs) == 1 && !controlOpticsOff {
		if dx := deviceSnap(opticalOf(key, size, 0, weight).dx, windowScale(c)); dx != 0 {
			t.Translate(dx, 0)
		}
	}
	return t
}

func placedGlyph(c *ui.Context, sym string, size, lineH float32, weight int, color ui.Color, centerX bool) ui.Element {
	t := ui.Text(c, sym).FontSize(size).TextColor(color).SingleLine().Shrink(0).Label(sym)
	if weight > 0 {
		t.FontWeight(weight)
	}
	if lineH > 0 {
		t.FixedLineHeight(lineH)
	}
	box := opticalOf(sym, size, lineH, weight)
	if box.elemH > 0 {
		t.Height(box.elemH)
	}
	if controlOpticsOff {
		return t
	}
	dx := float32(0)
	if centerX {
		dx = box.dx
	}
	dx = deviceSnap(dx, windowScale(c))
	if dx != 0 || box.dy != 0 {
		t.Translate(dx, box.dy)
	}
	return t
}

// deviceSnap rounds a DIP offset onto a whole device pixel. The text
// painter floors a glyph's pen, and a fractional translate resamples the
// mask, so a cap-height or keycap shift that is not a whole pixel softens
// the stem.
func deviceSnap(v, scale float32) float32 {
	if scale < 2 || v == 0 {
		return v
	}
	// Half a pixel rounds toward zero. Rounding it away from zero walks
	// the glyph onto the next pixel and opens the ink gap past ±0.25.
	px := float64(v * scale)
	var n float64
	if px >= 0 {
		n = math.Floor(px + 0.5 - 1e-9)
	} else {
		n = math.Ceil(px - 0.5 + 1e-9)
	}
	return float32(n) / scale
}

func opticalSample(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		return b.String()
	}
	return "H"
}

// opticalKey is the face and the glyph. The default UI font is the face
// these controls draw; weight is part of that face, and lineH is the line
// box the ink is centered in. The shift is measured from the glyph's ink,
// never stored as a fixed per-symbol constant: the same mark needs a
// different correction at 11pt than at 13pt.
type opticalKey struct {
	sample string
	size   float32
	lineH  float32
	weight int
}

type opticalBox struct {
	elemW, elemH float32
	top, bot     float32
	left, right  float32
	dy, dx       float32
	// usedFace reports that the pixel probe missed the face that draws
	// this shortcut, so the box came from that face's glyph bounds.
	usedFace bool
}

var (
	opticalMu    sync.Mutex
	opticalCache = map[opticalKey]opticalBox{}
)

func opticalOf(sample string, size, lineH float32, weight int) opticalBox {
	if sample == "" {
		sample = "H"
	}
	if weight <= 0 {
		weight = 400
	}
	key := opticalKey{sample, size, lineH, weight}
	opticalMu.Lock()
	if box, ok := opticalCache[key]; ok {
		opticalMu.Unlock()
		return box
	}
	opticalMu.Unlock()
	box := measureOptical(sample, size, lineH, weight)
	opticalMu.Lock()
	opticalCache[key] = box
	opticalMu.Unlock()
	return box
}

func opticalDy(sample string, size, lineH float32, weight int) float32 {
	return opticalOf(sample, size, lineH, weight).dy
}

// textBaselineDelta is how far a cap-height shortcut line must move to share
// the baseline of a plain label when both are centered on the same row.
// labelSize is the plain text; shortcutSize is the control line.
func textBaselineDelta(labelSize float32, labelWeight int, shortcutSize float32, shortcutWeight int) float32 {
	return baselineFromCenter(labelSize, labelWeight, true) - baselineFromCenter(shortcutSize, shortcutWeight, false)
}

func baselineFromCenter(size float32, weight int, plain bool) float32 {
	box := opticalOf("H", size, 0, weight)
	m := ui.Font{Size: size, Weight: weight}.Metrics()
	base := (box.elemH-m.Ascent-m.Descent)/2 + m.Ascent
	dy := float32(0)
	if !plain {
		dy = box.dy
	}
	return base - box.elemH/2 + dy
}

func measureOptical(sample string, size, lineH float32, weight int) opticalBox {
	// Four pixels a point, so the cached shift is finer than the 0.25pt budget.
	const scale float32 = 4
	tt := ui.NewTester(func(c *ui.Context) {
		white := ui.Color{R: 255, G: 255, B: 255, A: 255}
		black := ui.Color{A: 255}
		c.Root().Background(white)
		ui.Row(c).Padding(16).Background(white).Children(func() {
			t := ui.Text(c, sample).FontSize(size).FontWeight(weight).TextColor(black).SingleLine().Shrink(0).Label("optical")
			if lineH > 0 {
				t.FixedLineHeight(lineH)
			}
		})
	}, 480, 120)
	tt.SetScale(scale)
	tt.Frame()
	r, ok := tt.Find("optical")
	if !ok || r.W < 1 || r.H < 1 {
		return opticalBox{}
	}
	img := tt.Image()
	b := img.Bounds()
	x0 := int(r.X*scale) - 4
	x1 := int((r.X+r.W)*scale) + 4
	y0 := int(r.Y*scale) - 4
	y1 := int((r.Y+r.H)*scale) + 4
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	top, bot := y1, y0
	left, right := x1, x0
	found := false
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			px := img.RGBAAt(x, y)
			if px.R > 180 && px.G > 180 && px.B > 180 {
				continue
			}
			found = true
			if y < top {
				top = y
			}
			if y > bot {
				bot = y
			}
			if x < left {
				left = x
			}
			if x > right {
				right = x
			}
		}
	}
	if !found {
		return finishSymbolOptical(sample, size, lineH, weight, opticalBox{elemW: r.W, elemH: r.H}, false)
	}
	// Ink covers [left, right] and [top, bot] inclusive. Centers are in DIPs
	// from the element's top-left, and the shift puts that center on the
	// line-box center. Cached per sample, size, line height and weight.
	inkTop := float32(top) / scale
	inkBot := float32(bot+1) / scale
	inkLeft := float32(left) / scale
	inkRight := float32(right+1) / scale
	box := opticalBox{
		elemW: r.W, elemH: r.H,
		top: inkTop - r.Y, bot: inkBot - r.Y,
		left: inkLeft - r.X, right: inkRight - r.X,
	}
	box.dy = r.H/2 - (box.top+box.bot)/2
	box.dx = r.W/2 - (box.left+box.right)/2
	return finishSymbolOptical(sample, size, lineH, weight, box, true)
}

// finishSymbolOptical centres a shortcut symbol on the cap height of H.
// The centre is the middle of H's ink, not the x-height. On macOS the
// primary UI font often has no ⇧ ↵ ⌘, and Core Text draws a fallback
// face. A pixel probe that misses that face, or that reads the primary
// font's empty slot, would report no width (the next run lands on the
// symbol) and dy 0 (the mark stays on the baseline, below the caps).
// The bounds then come from the face that actually draws the glyph.
func finishSymbolOptical(sample string, size, lineH float32, weight int, box opticalBox, found bool) opticalBox {
	rs := []rune(sample)
	if len(rs) != 1 || !isShortcutSymbol(rs[0]) {
		return box
	}
	face := faceOf(sample, size, weight)
	left, right, elemW, used := inkFromDrawingFont(sample, size, weight, box.left, box.right, box.elemW)
	box.left, box.right, box.elemW = left, right, elemW
	box.usedFace = used
	h := opticalOf("H", size, lineH, weight)
	vertBad := !found
	if face.ok {
		fh := face.bot - face.top
		if !found || fh > 0 && box.bot-box.top < fh*faceInkFraction {
			vertBad = true
		}
	}
	if !vertBad {
		box.dy = capAlignDy((h.top+h.bot)/2+h.dy, (box.top+box.bot)/2)
		return box
	}
	if !face.ok {
		box.dy = h.dy
		return box
	}
	prim := faceOf("H", size, weight)
	base := symbolBaseline(box.elemH, prim.ascent, prim.descent, face.ascent, face.descent)
	box.top = base + face.top
	box.bot = base + face.bot
	box.usedFace = true
	target := (h.top+h.bot)/2 + h.dy
	if h.elemH > 0 && (box.elemH-h.elemH > 0.5 || h.elemH-box.elemH > 0.5) {
		// The row centres the line boxes. The word's cap is at its own
		// centre, so a taller symbol meets that cap at its own centre.
		target = box.elemH / 2
	}
	box.dy = capAlignDy(target, (box.top+box.bot)/2)
	return box
}

func symbolRowDX(parts []controlPart, size, lineH float32, weight int, scale float32, light bool) float32 {
	var syms []string
	for _, p := range parts {
		if !p.sym {
			continue
		}
		for _, r := range p.text {
			syms = append(syms, string(r))
		}
	}
	if len(syms) == 0 {
		return 0
	}
	var inkL, inkR float32
	x := float32(0)
	for i, sym := range syms {
		elemW, left, right := glyphInk(sym, size, lineH, weight, scale, light)
		if i > 0 {
			sp, _ := gridSpacer(syms[i-1], size, lineH, sym, size, lineH, weight, 1, scale, light)
			x += sp
		}
		if i == 0 {
			inkL = x + left
		}
		inkR = x + right
		x += elemW
	}
	// The centering shift is one translation of the whole run. A fraction of a
	// pixel resamples each glyph differently and opens the gaps. A whole
	// pixel moves them together, and the run's own positions stay fractional.
	dx := x/2 - (inkL+inkR)/2
	if scale >= 2 {
		dx = float32(math.Round(float64(dx*scale))) / scale
	}
	return dx
}

// shortcutInkRightBearing is the space from the line's layout edge to the
// last glyph's ink. centered includes the horizontal ink-centering shift
// a symbol-only controlLine applies; the selection bar does not.
func shortcutInkRightBearing(s string, size, lineH float32, weight int, centered bool, scale float32) float32 {
	parts := splitControlLine(s)
	var syms []string
	for _, p := range parts {
		if !p.sym {
			continue
		}
		for _, r := range p.text {
			syms = append(syms, string(r))
		}
	}
	if len(syms) == 0 {
		return 0
	}
	x := float32(0)
	var inkR float32
	for i, sym := range syms {
		elemW, _, right := glyphInk(sym, size, lineH, weight, scale, false)
		if i > 0 {
			sp, _ := gridSpacer(syms[i-1], size, lineH, sym, size, lineH, weight, 1, scale, false)
			x += sp
		}
		inkR = x + right
		x += elemW
	}
	dx := float32(0)
	if centered {
		dx = symbolRowDX(parts, size, lineH, weight, scale, false)
	}
	return x - (inkR + dx)
}

// shortcutRowWidth is the layout width of a symbol-only line, including
// the ink spacers between its keys.
func shortcutRowWidth(s string, size, lineH float32, weight int, scale float32) float32 {
	parts := splitControlLine(s)
	var syms []string
	for _, p := range parts {
		if !p.sym {
			continue
		}
		for _, r := range p.text {
			syms = append(syms, string(r))
		}
	}
	x := float32(0)
	for i, sym := range syms {
		if i > 0 {
			sp, _ := gridSpacer(syms[i-1], size, lineH, sym, size, lineH, weight, 1, scale, false)
			x += sp
		}
		elemW, _, _ := glyphInk(sym, size, lineH, weight, scale, false)
		x += elemW
	}
	return x
}

// glyphInk is the element's layout width and the ink left and right, in
// DIPs from the element's origin. At 2× and above those edges are the ones
// the device pixel actually paints, so a spacer built from them survives
// the pixel round. Below 2× the ideal optical bounds are already within a
// point, and snapping them would move controls the tests lay out at 1×.
func glyphInk(sample string, size, lineH float32, weight int, scale float32, light bool) (elemW, left, right float32) {
	if scale >= 2 {
		box := paintedInk(sample, size, lineH, weight, scale, light)
		return box.elemW, box.left, box.right
	}
	box := opticalOf(sample, size, lineH, weight)
	return box.elemW, box.left, box.right
}

// paintedKey is a glyph measured on one device scale. The same advance
// paints a different ink edge at 2× than at 4×, because each scale rounds
// to its own pixel.
type paintedKey struct {
	sample string
	size   float32
	lineH  float32
	weight int
	scale  float32
	light  bool
}

type paintedBox struct {
	elemW float32
	left  float32
	right float32
}

var (
	paintedMu    sync.Mutex
	paintedCache = map[paintedKey]paintedBox{}
)

func paintedInk(sample string, size, lineH float32, weight int, scale float32, light bool) paintedBox {
	if sample == "" {
		sample = "H"
	}
	if weight <= 0 {
		weight = 400
	}
	if scale < 2 {
		scale = 2
	}
	key := paintedKey{sample, size, lineH, weight, scale, light}
	paintedMu.Lock()
	if box, ok := paintedCache[key]; ok {
		paintedMu.Unlock()
		return box
	}
	paintedMu.Unlock()
	box := measurePaintedInk(sample, size, lineH, weight, scale, light)
	paintedMu.Lock()
	paintedCache[key] = box
	paintedMu.Unlock()
	return box
}

// measurePaintedInk renders one control run the way drawControlLine paints
// it and reads the half-coverage edges tools/inkgap reads. The 16 DIP pad
// is an integer, so at 2× and 4× the pen's fractional part is zero here.
// That is the same phase gridSpacer leaves after it rounds an origin
// distance to the device pixel. The floor that snaps a glyph is in the
// text painter, pen = floor((x+glyphX)*scale); this probe does not move
// the runs on screen. The cap-height shift is applied so a symbol and a
// word are measured where they are drawn.
func measurePaintedInk(sample string, size, lineH float32, weight int, scale float32, light bool) paintedBox {
	ref := opticalSample(sample)
	if rs := []rune(sample); len(rs) == 1 && isShortcutSymbol(rs[0]) {
		ref = sample
	}
	shift := opticalOf(ref, size, lineH, weight)
	tt := ui.NewTester(func(c *ui.Context) {
		bg := ui.Color{R: 255, G: 255, B: 255, A: 255}
		fg := ui.Color{A: 255}
		if light {
			bg = ui.Color{R: 28, G: 28, B: 30, A: 255}
			fg = ui.Color{R: 236, G: 236, B: 236, A: 255}
		}
		c.Root().Background(bg)
		ui.Row(c).Padding(16).Background(bg).Children(func() {
			t := ui.Text(c, sample).FontSize(size).FontWeight(weight).TextColor(fg).SingleLine().Shrink(0).Label("painted")
			if lineH > 0 {
				t.FixedLineHeight(lineH)
			}
			if shift.elemH > 0 {
				t.Height(shift.elemH)
			}
			if shift.dy != 0 {
				t.Translate(0, shift.dy)
			}
		})
	}, 480, 120)
	tt.SetScale(scale)
	tt.Frame()
	r, ok := tt.Find("painted")
	if !ok || r.W < 1 || r.H < 1 {
		return paintedBox{}
	}
	img := tt.Image()
	s := scale
	y0 := int(r.Y*s) - 4
	y1 := int((r.Y+r.H)*s) + 4
	// Half-coverage edges, the same ones tools/inkgap reads back from a PNG.
	var edges []inkgap.Edge
	for _, band := range inkgap.Measure(img, float64(s)) {
		if band.Y1 < y0 || band.Y0 > y1 {
			continue
		}
		edges = append(edges, band.Edges...)
	}
	box := paintedBox{elemW: r.W}
	if len(edges) > 0 {
		box.left = float32(edges[0].Left)/s - r.X
		box.right = float32(edges[len(edges)-1].Right)/s - r.X
	}
	var usedFace bool
	box.left, box.right, box.elemW, usedFace = inkFromDrawingFont(sample, size, weight, box.left, box.right, box.elemW)
	// Face bounds are the outline. The gap is read at half coverage, about
	// half a device pixel inside each edge, so the pixel the spacer picks
	// is the one a screenshot measures.
	if usedFace {
		inset := float32(0.5) / scale
		if box.right-box.left > 2*inset+0.5 {
			box.left += inset
			box.right -= inset
		}
	}
	return box
}

// windowScale is the device pixels per DIP of the window being built.
// Context does not publish it, and the spacer has to know which pixel the
// painter will round onto. The headless tester stores the scale; a window
// reports it from its surface.
func windowDark(c *ui.Context) bool {
	base := windowHost(c)
	if !base.IsValid() {
		return false
	}
	if d := fieldOf(base, "dark"); d.IsValid() && d.Kind() == reflect.Bool {
		return d.Bool()
	}
	conn := fieldOf(base, "conn")
	if conn.IsValid() && conn.Kind() == reflect.Pointer && !conn.IsNil() {
		isDark := fieldOf(conn.Elem(), "IsDark")
		if isDark.IsValid() && !isDark.IsNil() {
			if fn, ok := isDark.Interface().(func() bool); ok && fn != nil {
				return fn()
			}
		}
	}
	return false
}

func windowHost(c *ui.Context) reflect.Value {
	if c == nil {
		return reflect.Value{}
	}
	v := reflect.ValueOf(c)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return reflect.Value{}
	}
	rt := fieldOf(v.Elem(), "rt")
	if !rt.IsValid() || rt.Kind() != reflect.Pointer || rt.IsNil() {
		return reflect.Value{}
	}
	host := fieldOf(rt.Elem(), "host")
	if !host.IsValid() || host.IsNil() {
		return reflect.Value{}
	}
	base := host.Elem()
	if base.Kind() == reflect.Pointer {
		if base.IsNil() {
			return reflect.Value{}
		}
		base = base.Elem()
	}
	return base
}

func windowScale(c *ui.Context) float32 {
	if c == nil {
		return 1
	}
	v := reflect.ValueOf(c)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return 1
	}
	rt := fieldOf(v.Elem(), "rt")
	if !rt.IsValid() || rt.Kind() != reflect.Pointer || rt.IsNil() {
		return 1
	}
	host := fieldOf(rt.Elem(), "host")
	if !host.IsValid() || host.IsNil() {
		return 1
	}
	base := host.Elem()
	if !base.IsValid() {
		return 1
	}
	if base.Kind() == reflect.Pointer {
		if base.IsNil() {
			return 1
		}
		base = base.Elem()
	}
	if sc := fieldOf(base, "scale"); sc.IsValid() && sc.Kind() == reflect.Float32 {
		if s := float32(sc.Float()); s > 0 {
			return s
		}
	}
	conn := fieldOf(base, "conn")
	if conn.IsValid() && conn.Kind() == reflect.Pointer && !conn.IsNil() {
		surf := fieldOf(conn.Elem(), "Surface")
		if surf.IsValid() && !surf.IsNil() {
			if sizer, ok := surf.Interface().(interface {
				Size() (float64, float64, float64)
			}); ok {
				_, _, scale := sizer.Size()
				if scale > 0 {
					return float32(scale)
				}
			}
		}
	}
	return 1
}

func fieldOf(v reflect.Value, name string) reflect.Value {
	if !v.IsValid() {
		return reflect.Value{}
	}
	f := v.FieldByName(name)
	if !f.IsValid() || f.CanInterface() {
		return f
	}
	if !f.CanAddr() {
		return reflect.Value{}
	}
	return reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem()
}
