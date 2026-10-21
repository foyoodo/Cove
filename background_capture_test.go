package main

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cove/themes"

	"github.com/egoist/mygo/ui"
)

// TestCaptureBackgroundShots writes 2× crops for the background check when
// BG_OUT is set. It is not part of the default suite.
func TestCaptureBackgroundShots(t *testing.T) {
	out := os.Getenv("BG_OUT")
	if out == "" {
		t.Skip("set BG_OUT to write background crops")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	const scale float32 = 2
	capturePair(t, out, "light", false, 1120, 760, scale, nil)
	capturePair(t, out, "dark", true, 1120, 760, scale, nil)
	capturePair(t, out, "theme", false, 1120, 760, scale, func(a *App, tt *ui.Tester) {
		a.setTheme(themes.ThemeTide, tt.Now())
		for i := 0; i < 12; i++ {
			tt.Advance(16 * time.Millisecond)
			awaitGradients()
			awaitVeils()
		}
	})
	capturePair(t, out, "resize", false, 1120, 760, scale, func(a *App, tt *ui.Tester) {
		tt.SetSize(960, 640)
		settleSize(tt)
		tt.SetSize(1120, 760)
		settleSize(tt)
	})
	capturePair(t, out, "fullscreen", false, 1440, 900, scale, func(a *App, tt *ui.Tester) {
		tt.SetSize(720, 760)
		settleSize(tt)
		tt.SetSize(1440, 900)
		settleSize(tt)
	})
}

func settleSize(tt *ui.Tester) {
	tt.Frame()
	for i := 0; i < 4; i++ {
		tt.Advance(gradSettle)
		tt.Frame()
		awaitGradients()
		awaitVeils()
	}
}

func capturePair(t *testing.T, out, name string, dark bool, w, h int, scale float32, prep func(*App, *ui.Tester)) {
	t.Helper()
	a := newTestApp(t)
	a.instant = true
	if dark {
		a.data.Appearance = appearanceDark
	} else {
		a.data.Appearance = appearanceLight
	}
	a.data.Theme = themes.ThemeWarm
	now := time.Now()
	a.data.Conversations = []Conversation{{
		ID: "bg", Title: "Background", UpdatedAt: now, CreatedAt: now,
		Config: DefaultConfig(),
		Messages: []Message{
			{ID: "u", Role: roleUser, Content: "Show the gradient.", Done: true, CreatedAt: now},
			{ID: "a", Role: roleAssistant, Content: "Background pixel check.", Done: true, CreatedAt: now},
		},
	}}
	a.data.ActiveID = "bg"
	tt := ui.NewTester(a.view, w, h)
	tt.SetScale(scale)
	tt.Frame()
	awaitGradients()
	tt.Frame()
	if prep != nil {
		prep(a, tt)
		tt.Frame()
	}

	before := copyRGBA(tt.Image())
	writePNG(t, filepath.Join(out, name+"-before.png"), cropBackground(before))
	writePNG(t, filepath.Join(out, name+"-before-pad.png"), cropCardPad(before))
	writePNG(t, filepath.Join(out, name+"-before-full.png"), before)
	writePNG(t, filepath.Join(out, name+"-mask.png"), contentMask(before, scale))
	samples := pureGradientSamples(a, before, scale, float32(w), float32(h))
	if len(samples) < 80 {
		t.Errorf("%s: only %d pure-gradient samples on the frame before the scroll", name, len(samples))
	}
	low := 0
	frames := 0
	msg := &a.active().Messages[len(a.active().Messages)-1]
	var src string
	for _, tok := range streamTokens(180) {
		src += tok
		msg.Content = src
		msg.Done = false
		runOneFrame(tt)
		frames++
		if !samplesMatchBitmap(a, tt.Image(), samples, scale, float32(w), float32(h)) {
			low++
		}
	}
	after := copyRGBA(tt.Image())
	writePNG(t, filepath.Join(out, name+"-after.png"), cropBackground(after))
	writePNG(t, filepath.Join(out, name+"-after-pad.png"), cropCardPad(after))
	writePNG(t, filepath.Join(out, name+"-after-full.png"), after)
	rep := fmt.Sprintf("%s frames=%d samples=%d lowres=%d\n", name, frames, len(samples), low)
	if err := os.WriteFile(filepath.Join(out, name+"-lowres.txt"), []byte(rep), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Log(rep)
	if low != 0 {
		t.Errorf("%s: %d/%d scroll frames used a low-resolution background", name, low, frames)
	}
}

// cropCardPad is a strip just inside the card's left edge, in the list
// padding. It is the translucent card over the gradient, with no text.
func cropCardPad(src *image.RGBA) *image.RGBA {
	return cropPixels(src, image.Rect(490, 300, 520, 550))
}

// cropBackground is the rightmost 48 device columns. That strip is the
// window margin, the card edge, and its shadow: gradient, not transcript.
func cropBackground(src *image.RGBA) *image.RGBA {
	b := src.Bounds()
	x0 := b.Max.X - 48
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	return cropPixels(src, image.Rect(x0, b.Min.Y, b.Max.X, b.Max.Y))
}

func cropPixels(src *image.RGBA, r image.Rectangle) *image.RGBA {
	r = r.Intersect(src.Bounds())
	out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			out.SetRGBA(x-r.Min.X, y-r.Min.Y, src.RGBAAt(x, y))
		}
	}
	return out
}

// contentMask is black over the transcript column and the sidebar
// Settings shortcut, and white everywhere else. The white pixels are
// the gradient, the card chrome, and the padding inside the translucent
// card. The shortcut glyph is chrome: the gap commits after b58e9f9
// moved ⌘, and that ink is not the gradient.
func contentMask(src *image.RGBA, scale float32) *image.RGBA {
	b := src.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for i := 0; i < len(out.Pix); i += 4 {
		out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = 255, 255, 255, 255
	}
	// Sidebar 240, title 52, card inset 8, list padding 24. Device pixels.
	s := int(scale)
	x0, y0 := (240+24)*s, (52+8)*s
	x1, y1 := b.Dx()-(8+24)*s, b.Dy()-(8+8)*s
	if x1 < x0 {
		x1 = x0
	}
	if y1 < y0 {
		y1 = y0
	}
	blackOut(out, x0, y0, x1, y1)
	// Settings ⌘, sits at the trailing edge of the sidebar footer.
	// Device slot measured on the 2× captures: x 415–438, and 36–55px
	// up from the bottom.
	blackOut(out, 408, b.Dy()-58, 448, b.Dy()-32)
	return out
}

func blackOut(img *image.RGBA, x0, y0, x1, y1 int) {
	b := img.Bounds()
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > b.Dx() {
		x1 = b.Dx()
	}
	if y1 > b.Dy() {
		y1 = b.Dy()
	}
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			i := img.PixOffset(x, y)
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 0, 0, 0, 255
		}
	}
}

// pureGradientSamples is a grid of device pixels that, on the settled
// frame, already equal the full-resolution dither. A stretched preview
// fails this grid; one lucky margin pixel would not.
func pureGradientSamples(a *App, img *image.RGBA, scale, winW, winH float32) []image.Point {
	b := img.Bounds()
	var pts []image.Point
	x := b.Max.X - 3
	for y := 2; y < b.Max.Y; y += 8 {
		if pixelEqualsWindow(a, img, scale, winW, winH, x, y) {
			pts = append(pts, image.Pt(x, y))
		}
	}
	y := 2
	for x := 4; x < b.Max.X; x += 16 {
		if pixelEqualsWindow(a, img, scale, winW, winH, x, y) {
			pts = append(pts, image.Pt(x, y))
		}
	}
	return pts
}

func samplesMatchBitmap(a *App, img *image.RGBA, pts []image.Point, scale, winW, winH float32) bool {
	for _, p := range pts {
		if !pixelEqualsWindow(a, img, scale, winW, winH, p.X, p.Y) {
			return false
		}
	}
	return true
}

func pixelEqualsWindow(a *App, img *image.RGBA, scale, winW, winH float32, px, py int) bool {
	got := img.RGBAAt(px, py)
	want := windowPixel(a.look, scale, winW, winH, px, py)
	return got.R == want.R && got.G == want.G && got.B == want.B
}
