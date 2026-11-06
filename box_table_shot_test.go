package main

import (
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func writeBoxShot(t *testing.T, dir, name string, img image.Image) {
	t.Helper()
	writePNG(t, filepath.Join(dir, name+".png"), img)
}

func cropGrid(t *testing.T, tt *ui.Tester, pad int) *image.RGBA {
	t.Helper()
	grid := mustFind(t, tt, "box table")
	return cropRect(tt.Image(), grid, pad)
}

func TestBoxTableDesignShots(t *testing.T) {
	dir := testArtifactDir(t, "boxtable")
	zoom := testArtifactDir(t, "boxtable-zoom")
	fixtures := []struct {
		name, body string
		wide       bool
		w, h       int
	}{
		{"boxtable-en", boxEnglishMD(), false, shotW, shotH},
		{"boxtable-cjk", boxCJKMD(), false, shotW, shotH},
		{"boxtable-arcs", boxArcsSurroundMD(), false, shotW, shotH},
		{"boxtable-wide", boxWideMD(), false, 720, 520},
		{"boxtable-glyphs", boxAllGlyphsMD(), false, shotW, shotH},
		{"boxtable-surround", boxSurroundMD(), false, shotW, shotH},
		{"boxtable-adjacent", boxAdjacentMD(), false, shotW, shotH},
		{"boxtable-fenced", boxFencedMD(), false, shotW, shotH},
	}
	for _, fx := range fixtures {
		for _, dark := range []bool{false, true} {
			mode := "light"
			if dark {
				mode = "dark"
			}
			a := boxShotApp(t, fx.body)
			tt := ui.NewTester(a.view, fx.w, fx.h)
			if dark {
				tt.SetDark(true)
			}
			tt.Frame()
			if tt.FramePending() {
				tt.Frame()
			}
			if fx.wide {
				placeHScroll(t, a, tt, "middle")
			}
			if fx.name == "boxtable-fenced" {
				mustFind(t, tt, "code header")
				mustFind(t, tt, "box table grid")
			} else {
				mustFind(t, tt, "box table")
			}
			writeBoxShot(t, dir, fx.name+"-"+mode, tt.Image())
			cropLabel := "box table"
			if fx.name == "boxtable-fenced" {
				cropLabel = "code block"
			}
			grid := mustFind(t, tt, cropLabel)
			writeBoxShot(t, dir, fx.name+"-"+mode+"-crop", cropRect(tt.Image(), grid, 8))
		}
	}

	// Scrolled wide table at the end, plus 1x/2x CJK and 8x join close-ups.
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		a := boxShotApp(t, boxWideMD())
		tt := ui.NewTester(a.view, 720, 520)
		if dark {
			tt.SetDark(true)
		}
		tt.Frame()
		if tt.FramePending() {
			tt.Frame()
		}
		placeHScroll(t, a, tt, "middle")
		writeBoxShot(t, dir, "boxtable-wide-mid-"+mode, tt.Image())
		writeBoxShot(t, dir, "boxtable-wide-mid-"+mode+"-crop", cropGrid(t, tt, 8))
		placeHScroll(t, a, tt, "end")
		writeBoxShot(t, dir, "boxtable-wide-scrolled-"+mode, tt.Image())
		writeBoxShot(t, dir, "boxtable-wide-scrolled-"+mode+"-crop", cropGrid(t, tt, 8))
	}

	for _, scale := range []float32{1, 2} {
		for _, dark := range []bool{false, true} {
			mode := "light"
			if dark {
				mode = "dark"
			}
			a := boxShotApp(t, boxCJKMD())
			tt := ui.NewTester(a.view, shotW, shotH)
			tt.SetScale(scale)
			if dark {
				tt.SetDark(true)
			}
			tt.Frame()
			if tt.FramePending() {
				tt.Frame()
			}
			grid := mustFind(t, tt, "box table")
			tag := mode + "-@" + itoa(int(scale)) + "x"
			writeBoxShot(t, dir, "boxtable-cjk-"+tag, tt.Image())
			writeBoxShot(t, zoom, "boxtable-cjk-zoom-"+tag, cropScaled(tt.Image(), grid, scale, 8))
		}
	}

	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		a := boxShotApp(t, boxArcsMD())
		tt := ui.NewTester(a.view, shotW, shotH)
		if dark {
			tt.SetDark(true)
		}
		tt.Frame()
		grid := mustFind(t, tt, "box table grid")
		r := ui.Rect{X: grid.X, Y: grid.Y, W: a.lastBox.CellW * 8, H: boxLineH * 3}
		writeBoxShot(t, zoom, "boxtable-arcs-rounded-"+mode+"-8x", scaleNN(cropRect(tt.Image(), r, 2), 8))
		r = ui.Rect{X: grid.X, Y: grid.Y + boxLineH*3, W: a.lastBox.CellW * 8, H: boxLineH * 4}
		writeBoxShot(t, zoom, "boxtable-arcs-dashed-"+mode+"-8x", scaleNN(cropRect(tt.Image(), r, 2), 8))
	}

	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		a := boxShotApp(t, boxEnglishMD())
		tt := ui.NewTester(a.view, shotW, shotH)
		if dark {
			tt.SetDark(true)
		}
		tt.Frame()
		grid := mustFind(t, tt, "box table grid")
		// First ┬ sits after the 8-cell Stage column plus the left │.
		cw := a.lastBox.CellW
		join := ui.Rect{X: grid.X + cw*9 + cw/2 - 8, Y: grid.Y + boxLineH/2 - 8, W: 16, H: 16}
		writeBoxShot(t, zoom, "boxtable-join-"+mode+"-8x", scaleNN(cropRect(tt.Image(), join, 0), 8))
		glyph := ui.Rect{X: grid.X, Y: grid.Y, W: cw * 12, H: boxLineH * 4}
		writeBoxShot(t, zoom, "boxtable-glyph-close-"+mode+"-8x", scaleNN(cropRect(tt.Image(), glyph, 2), 8))
	}

	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		a := boxShotApp(t, boxAllGlyphsMD())
		tt := ui.NewTester(a.view, shotW, shotH)
		if dark {
			tt.SetDark(true)
		}
		tt.Frame()
		grid := mustFind(t, tt, "box table grid")
		writeBoxShot(t, zoom, "boxtable-glyphs-close-"+mode+"-4x", scaleNN(cropRect(tt.Image(), grid, 4), 4))
	}

	if err := os.WriteFile(filepath.Join(dir, "geometry-notes.txt"), []byte(boxGeometryNotes()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func cropScaled(src *image.RGBA, r ui.Rect, scale float32, pad int) *image.RGBA {
	rr := ui.Rect{X: r.X * scale, Y: r.Y * scale, W: r.W * scale, H: r.H * scale}
	return cropRect(src, rr, int(float32(pad)*scale+0.5))
}

func boxGeometryNotes() string {
	d1 := layoutDashes(7.2, 4, 2, 1)
	d2 := layoutDashes(7.2, 4, 2, 2)
	return "SPEC §5.4.2 geometry notes\n" +
		"Rounded ╭╮╯╰: SPEC says a quarter circle centred on the cell with\n" +
		"radius half a cell. Two independent conflicts:\n" +
		"1) A 1ch×18pt cell is not square, so no circle hits both edge midpoints.\n" +
		"2) Even on a square, a cell-centred quarter through east and south\n" +
		"   midpoints has a VERTICAL tangent at the east midpoint, so it cannot\n" +
		"   join a neighbouring ─ (G1 / edge-midpoint tangency is the visual bar).\n" +
		"Smallest faithful stroke: quarter ellipse centred on the interior\n" +
		"corner, semi-axes (cw/2, ch/2), kappa 0.5522847498, endpoints at the\n" +
		"edge midpoints, horizontal/vertical tangents.\n\n" +
		"4-dash at 1ch light, 1x (7.2 DIP): " + d1.Note + "\n" +
		"4-dash at 1ch light, 2x: " + d2.Note + "\n" +
		"U+2580–259F are painted with the monospace glyph, never self-drawn.\n" +
		"Headless tester default scale is 1. 2x CJK shots use Tester.SetScale(2).\n" +
		"Software rasterizer; round StrokePath caps on arcs. Real-Mac Core Text\n" +
		"advances and subpixel AA will differ slightly.\n" +
		"Fenced ``` code blocks always keep the language header and Copy button.\n" +
		"They are never converted to a table block. Qualifying box-drawing lines\n" +
		"inside the fence use this same cell grid so CJK and joins still line up.\n" +
		"An unfenced table block contains only the qualifying ≥3-line run;\n" +
		"adjacent prose in the same paragraph stays ordinary Markdown.\n" +
		"Quarter-ellipse arcs and the 1x 4-dash 1px gap are Designer-accepted.\n"
}
