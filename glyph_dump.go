package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"

	"github.com/egoist/mygo/ui"
)

// dumpGlyphMetrics prints the face, ink and gaps of every shortcut row and
// writes 2× PNG crops rendered by the same controlLine the window builds.
// Run on a Mac:
//
//	Cove.app/Contents/MacOS/Cove --dump-glyph-metrics
//
// An optional second argument is the output directory (default glyph-metrics).
func dumpGlyphMetrics(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	rows := []struct {
		name, text string
		size, line float32
	}{
		{"composer-hint", "↵ send · ⇧↵ newline", 11, 13},
		{"mod-hint", "⌘↵ send · ↵ newline", 11, 13},
		{"stop-hint", "⌘. to stop", 11, 13},
		{"empty-new", "⌘N  New Chat", 11, 0},
		{"empty-settings", "⌘,  Settings", 11, 0},
		{"empty-search", "⌘K  Search", 11, 0},
		{"sidebar", "⌘,", 11, 0},
	}
	fmt.Println("glyph metrics at 2×. gaps are ink to ink, in points.")
	fmt.Println("capDelta is the symbol ink centre minus the cap-height centre, after the shift the window applies. 0 lines them up. y grows downward.")
	fmt.Println("layout=face means the pixel probe missed the drawing font, so the gap and the shift use that font's glyph bounds.")
	fmt.Println("PNGs are 2× crops of controlLine, the path the window paints. Measure them with tools/inkgap.")
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		for _, row := range rows {
			fmt.Printf("\n# %s %s\n", row.name, mode)
			printRowMetrics(row.text, row.size, row.line, 400)
			name := row.name + "-" + mode + "-2x.png"
			if err := renderShortcutRow(filepath.Join(dir, name), row.text, row.size, row.line, dark); err != nil {
				return err
			}
			fmt.Printf("  wrote %s\n", name)
		}
	}
	return nil
}

func printRowMetrics(text string, size, lineH float32, weight int) {
	parts := splitControlLine(text)
	var prev string
	var prevRight, prevW float32
	for i, p := range parts {
		edge := partEdge(p, true)
		if p.sym {
			for j, r := range []rune(p.text) {
				s := string(r)
				if j > 0 {
					printGap(prev, s, size, lineH, weight, 1, prevRight, prevW)
				} else if i > 0 {
					printGap(prev, s, size, lineH, weight, controlGap(parts[i-1], p), prevRight, prevW)
				}
				printGlyph(s, size, lineH, weight)
				l, r, w := rowInk(s, size, lineH, weight)
				_, prevRight, prevW, _ = widenUnmeasured(s, size, weight, l, r, w)
				prev = s
			}
			continue
		}
		if i > 0 {
			printGap(prev, edge, size, lineH, weight, controlGap(parts[i-1], p), prevRight, prevW)
		}
		printGlyph(edge, size, lineH, weight)
		l, r, w := rowInk(edge, size, lineH, weight)
		_, prevRight, prevW, _ = widenUnmeasured(edge, size, weight, l, r, w)
		prev = edge
	}
}

func rowInk(sample string, size, lineH float32, weight int) (left, right, elemW float32) {
	box := opticalOf(sample, size, lineH, weight)
	return box.left, box.right, box.elemW
}

func printGlyph(sample string, size, lineH float32, weight int) {
	gs := shapeGlyphs(sample, size, weight)
	box := opticalOf(sample, size, lineH, weight)
	// Symbols are shifted onto the cap. Words, the separator and
	// punctuation keys keep the text's own shift, so a comma stays on
	// the baseline.
	applied := box.dy
	if rs := []rune(sample); len(rs) != 1 || !isShortcutSymbol(rs[0]) {
		applied = opticalOf(opticalSample(sample), size, lineH, weight).dy
	}
	h := opticalOf("H", size, lineH, weight)
	cap := (h.top + h.bot) / 2
	sym := (box.top + box.bot) / 2
	delta := sym + applied - (cap + h.dy)
	fam := ""
	fb := false
	adv := float32(0)
	for _, g := range gs {
		adv += g.advance
		if fam == "" || g.fallback {
			fam = g.family
			fb = fb || g.fallback
		}
	}
	if fam == "" {
		fam = "(unnamed)"
	}
	face := faceOf(sample, size, weight)
	from := "probe"
	if box.usedFace {
		from = "face"
	}
	fmt.Printf("  glyph %q font=%s fallback=%v advance=%.2f ascent=%.2f descent=%.2f ink=%.2f..%.2f facePen=%.2f..%.2f faceBase=%.2f..%.2f layout=%s dy=%.2f capDelta=%.2f\n",
		sample, fam, fb, adv, firstAscent(gs), firstDescent(gs), box.left, box.right, face.left, face.right, face.top, face.bot, from, applied, delta)
}

func firstAscent(gs []shapedGlyph) float32 {
	if len(gs) == 0 {
		return 0
	}
	return gs[0].ascent
}

func firstDescent(gs []shapedGlyph) float32 {
	if len(gs) == 0 {
		return 0
	}
	return gs[0].descent
}

func printGap(before, after string, size, lineH float32, weight int, target, _, _ float32) {
	a := paintedInk(before, size, lineH, weight, 2, false)
	b := paintedInk(after, size, lineH, weight, 2, false)
	sp := chooseSpacer(a, b, target, 2)
	got := (a.elemW - a.right) + sp + b.left
	fmt.Printf("  gap %s→%s %.2fpt target %.0f\n", before, after, got, target)
}

func renderShortcutRow(path, text string, size, lineH float32, dark bool) error {
	tt := ui.NewTester(func(c *ui.Context) {
		bg := ui.Color{R: 255, G: 255, B: 255, A: 255}
		fg := ui.Color{A: 255}
		if dark {
			bg = ui.Color{R: 28, G: 28, B: 30, A: 255}
			fg = ui.Color{R: 236, G: 236, B: 236, A: 255}
		}
		c.Root().Background(bg)
		ui.Row(c).Padding(12).Background(bg).Children(func() {
			controlLine(c, text, size, lineH, 400, fg).Label("row")
		})
	}, 640, 80)
	tt.SetScale(2)
	tt.SetDark(dark)
	tt.Frame()
	img := tt.Image()
	if row, ok := tt.Find("row"); ok {
		img = cropMetrics(img, row, 2, 8)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func cropMetrics(img *image.RGBA, r ui.Rect, scale, pad float32) *image.RGBA {
	b := img.Bounds()
	x0 := int(r.X*scale) - int(pad*scale)
	y0 := int(r.Y*scale) - int(pad*scale)
	x1 := int((r.X+r.W)*scale) + int(pad*scale)
	y1 := int((r.Y+r.H)*scale) + int(pad*scale)
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
	out := image.NewRGBA(image.Rect(0, 0, x1-x0, y1-y0))
	for y := y0; y < y1; y++ {
		copy(out.Pix[(y-y0)*out.Stride:], img.Pix[y*img.Stride+x0*4:y*img.Stride+x1*4])
	}
	return out
}
