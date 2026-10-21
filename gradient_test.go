package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"testing"

	"cove/themes"

	"github.com/egoist/mygo/ui"
	"golang.org/x/image/draw"
)

func TestWindowStopsStayInTokens(t *testing.T) {
	g := themes.WindowGradient()
	want := []float64{0, 0.38, 0.70, 1}
	if len(g.Stops) != len(want) {
		t.Fatalf("stops %v", g.Stops)
	}
	for i := range want {
		if math.Abs(g.Stops[i]-want[i]) > 1e-9 {
			t.Fatalf("stop %d = %v", i, g.Stops)
		}
	}
	if math.Abs(g.TintStart-0.35) > 1e-9 || math.Abs(g.TintEnd-1) > 1e-9 {
		t.Fatalf("tint %v %v", g.TintStart, g.TintEnd)
	}
	dark := themes.Palette(themes.ThemeWarm, true)
	if dark.WindowBaseLow.R != 0x1F || dark.WindowBaseLow.G != 0x1C || dark.WindowBaseLow.B != 0x1F {
		t.Fatalf("warm dark baseLow #%02X%02X%02X", dark.WindowBaseLow.R, dark.WindowBaseLow.G, dark.WindowBaseLow.B)
	}
	tide := themes.Palette(themes.ThemeTide, true)
	if tide.WindowBaseLow.R != 0x16 || tide.WindowBaseLow.G != 0x1F || tide.WindowBaseLow.B != 0x20 {
		t.Fatalf("tide dark baseLow #%02X%02X%02X", tide.WindowBaseLow.R, tide.WindowBaseLow.G, tide.WindowBaseLow.B)
	}
}

func TestGradientStepsStayWithinOne(t *testing.T) {
	const limit = 24 // SPEC §4.5: an undithered band wider than this is visible
	heights := []int{1800, 2880}
	var worstDelta, worstRun, worstBefore int
	for _, id := range []string{themes.ThemeWarm, themes.ThemeTide, themes.ThemeNeutral, themes.ThemeMist, themes.ThemeSage, themes.ThemeLavender} {
		for _, dark := range []bool{false, true} {
			look := Theme{Colors: themes.Palette(id, dark), Dark: dark}
			for _, h := range heights {
				img := renderGradient(look, 64, h, true)
				plain := renderGradient(look, 64, h, false)
				d, run := gradientStats(img)
				_, before := gradientStats(plain)
				if d > worstDelta {
					worstDelta = d
				}
				if run > worstRun {
					worstRun = run
				}
				if before > worstBefore {
					worstBefore = before
				}
				if d > 1 {
					t.Errorf("%s dark=%v h=%d max adjacent step %d", id, dark, h, d)
				}
				if run > limit {
					t.Errorf("%s dark=%v h=%d identical run %d", id, dark, h, run)
				}
			}
		}
	}
	t.Logf("max adjacent step %d, longest dithered identical run %d, longest undithered identical run %d", worstDelta, worstRun, worstBefore)
}

type gradMeasure struct {
	maxR, maxG, maxB int
	maxDelta         int
	pixelRun         int
	rowRun           int
}

func (m gradMeasure) max() int {
	if m.pixelRun > m.rowRun {
		return m.pixelRun
	}
	return m.rowRun
}

func gradientStats(img *image.NRGBA) (maxDelta, maxRun int) {
	m := measureGradient(img)
	return m.maxDelta, m.max()
}

func measureGradient(img *image.NRGBA) gradMeasure {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	var m gradMeasure
	for x := 0; x < w; x++ {
		run := 1
		for y := 1; y < h; y++ {
			dr, dg, db := chanDelta(img, x, y, x, y-1)
			m.note(dr, dg, db)
			run = noteRun(run, dr, dg, db, &m.pixelRun)
		}
	}
	for y := 0; y < h; y++ {
		run := 1
		for x := 1; x < w; x++ {
			dr, dg, db := chanDelta(img, x, y, x-1, y)
			m.note(dr, dg, db)
			run = noteRun(run, dr, dg, db, &m.pixelRun)
		}
	}
	m.rowRun = 1
	best := 1
	for y := 1; y < h; y++ {
		if rowEqual(img, y, y-1) {
			m.rowRun++
			if m.rowRun > best {
				best = m.rowRun
			}
		} else {
			m.rowRun = 1
		}
	}
	m.rowRun = best
	return m
}

func noteRun(run, dr, dg, db int, best *int) int {
	if dr == 0 && dg == 0 && db == 0 {
		run++
		if run > *best {
			*best = run
		}
		return run
	}
	return 1
}

func (m *gradMeasure) note(dr, dg, db int) {
	if dr > m.maxR {
		m.maxR = dr
	}
	if dg > m.maxG {
		m.maxG = dg
	}
	if db > m.maxB {
		m.maxB = db
	}
	if d := dr; d > m.maxDelta {
		m.maxDelta = d
	}
	if dg > m.maxDelta {
		m.maxDelta = dg
	}
	if db > m.maxDelta {
		m.maxDelta = db
	}
}

func chanDelta(img *image.NRGBA, x, y, x2, y2 int) (r, g, b int) {
	a := img.NRGBAAt(x, y)
	c := img.NRGBAAt(x2, y2)
	return absInt(int(a.R) - int(c.R)), absInt(int(a.G) - int(c.G)), absInt(int(a.B) - int(c.B))
}

func pixDelta(img *image.NRGBA, x, y, x2, y2 int) int {
	a := img.NRGBAAt(x, y)
	b := img.NRGBAAt(x2, y2)
	d := absInt(int(a.R) - int(b.R))
	if g := absInt(int(a.G) - int(b.G)); g > d {
		d = g
	}
	if bl := absInt(int(a.B) - int(b.B)); bl > d {
		d = bl
	}
	return d
}

func rowEqual(img *image.NRGBA, y, y2 int) bool {
	w := img.Rect.Dx()
	for x := 0; x < w; x++ {
		if pixDelta(img, x, y, x, y2) != 0 {
			return false
		}
	}
	return true
}

func TestWindowBitmapIsTheBackground(t *testing.T) {
	a := newTestApp(t)
	a.data.Appearance = appearanceLight
	const w, h = 400, 300
	tt := ui.NewTester(a.view, w, h)
	tt.SetScale(2)
	img := tt.Image()
	// Title-bar area, clear of the traffic lights, on the right.
	px, py := w*2-8, 12
	got := img.RGBAAt(px, py)
	want := windowPixel(a.look, 2, w, h, px, py)
	if got.R != want.R || got.G != want.G || got.B != want.B {
		t.Fatalf("window pixel (%d,%d) = %v, bitmap %v", px, py, got, want)
	}
}

func TestBandingArtifacts(t *testing.T) {
	dir := filepath.Join("/opt/cursor/artifacts", "banding")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const (
		dipW, dipH = 1440, 900
		scale      = 2
	)
	pw, ph := dipW*scale, dipH*scale
	var lines []string
	lines = append(lines, "theme\tmode\tpixels\tmaxΔR\tmaxΔG\tmaxΔB\tmaxΔ\tidentical-row-run\tidentical-pixel-run\tundithered-maxΔ\tundithered-row-run\tundithered-pixel-run")
	ids := []string{themes.ThemeWarm, themes.ThemeTide, themes.ThemeNeutral, themes.ThemeMist, themes.ThemeSage, themes.ThemeLavender}
	for _, id := range ids {
		for _, dark := range []bool{false, true} {
			mode := "light"
			if dark {
				mode = "dark"
			}
			a := revisionChat(t)
			a.data.Theme = id
			if dark {
				a.data.Appearance = appearanceDark
			} else {
				a.data.Appearance = appearanceLight
			}
			tt := ui.NewTester(a.view, dipW, dipH)
			tt.SetScale(scale)
			writePNG(t, filepath.Join(dir, fmt.Sprintf("%s-%s-1440x900@2x.png", id, mode)), tt.Image())

			look := Theme{Colors: themes.Palette(id, dark), Dark: dark}
			// 900pt and 1440pt tall, both at the maximized 1440pt width, 2×.
			for _, gh := range []int{ph, 1440 * scale} {
				grad := renderGradient(look, pw, gh, true)
				plain := renderGradient(look, pw, gh, false)
				got := measureGradient(grad)
				before := measureGradient(plain)
				lines = append(lines, fmt.Sprintf("%s\t%s\t%dx%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d",
					id, mode, pw, gh, got.maxR, got.maxG, got.maxB, got.maxDelta, got.rowRun, got.pixelRun, before.maxDelta, before.rowRun, before.pixelRun))
				if got.maxDelta > 1 || got.rowRun > 24 || got.pixelRun > 24 {
					t.Errorf("%s %s %dx%d maxΔ %d row-run %d pixel-run %d", id, mode, pw, gh, got.maxDelta, got.rowRun, got.pixelRun)
				}
				if gh != ph {
					continue
				}
				y0 := int(0.70 * float64(gh))
				crop := grad.SubImage(image.Rect(pw-400, y0, pw, gh)).(*image.NRGBA)
				zoomed := scaleNearest(crop, 4)
				writePNG(t, filepath.Join(dir, fmt.Sprintf("%s-%s-70-100-4x.png", id, mode)), zoomed)
				writePNG(t, filepath.Join(dir, fmt.Sprintf("%s-%s-70-100-4x-stretch.png", id, mode)), stretchContrast(zoomed))
			}
		}
	}
	body := ""
	for _, ln := range lines {
		body += ln + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "measurements.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func scaleNearest(src *image.NRGBA, n int) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx()*n, b.Dy()*n))
	draw.NearestNeighbor.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return dst
}

func stretchContrast(src *image.NRGBA) *image.NRGBA {
	b := src.Bounds()
	min := [3]int{255, 255, 255}
	max := [3]int{0, 0, 0}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := src.NRGBAAt(x, y)
			ch := [3]int{int(c.R), int(c.G), int(c.B)}
			for i := range ch {
				if ch[i] < min[i] {
					min[i] = ch[i]
				}
				if ch[i] > max[i] {
					max[i] = ch[i]
				}
			}
		}
	}
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := src.NRGBAAt(x, y)
			ch := [3]uint8{c.R, c.G, c.B}
			var out [3]uint8
			for i := range ch {
				span := max[i] - min[i]
				if span < 1 {
					out[i] = ch[i]
					continue
				}
				out[i] = uint8((int(ch[i]) - min[i]) * 255 / span)
			}
			dst.SetNRGBA(x-b.Min.X, y-b.Min.Y, color.NRGBA{R: out[0], G: out[1], B: out[2], A: 255})
		}
	}
	return dst
}
