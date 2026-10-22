package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cove/themes"

	"github.com/egoist/mygo/ui"
)

// shadowCase is one theme, mode, and window of the content-card shadow.
type shadowCase struct {
	theme    string
	dark     bool
	settings bool
}

// shadowMatrix is all 6 themes × light and dark × main and settings.
func shadowMatrix() []shadowCase {
	var out []shadowCase
	for _, p := range themes.Presets() {
		for _, dark := range []bool{false, true} {
			for _, settings := range []bool{false, true} {
				out = append(out, shadowCase{theme: p.ID, dark: dark, settings: settings})
			}
		}
	}
	return out
}

func (c shadowCase) mode() string {
	if c.dark {
		return "dark"
	}
	return "light"
}

func (c shadowCase) window() string {
	if c.settings {
		return "settings"
	}
	return "main"
}

func (c shadowCase) name() string {
	return c.window() + "-" + c.theme + "-" + c.mode()
}

func TestContentCardShadowContained(t *testing.T) {
	for _, id := range []string{themes.ThemeWarm, themes.ThemeTide, themes.ThemeNeutral, themes.ThemeMist, themes.ThemeSage, themes.ThemeLavender} {
		dark := themes.Resolve(id, true, true, ui.Color{}).ShadowCard
		if dark.R != 0 || dark.G != 0 || dark.B != 0 || dark.A != 0x40 {
			t.Fatalf("%s dark shadow/card #%02x%02x%02x%02x, want alpha 64", id, dark.R, dark.G, dark.B, dark.A)
		}
		light := themes.Resolve(id, false, true, ui.Color{}).ShadowCard
		if light.R != 0x3C || light.G != 0x28 || light.B != 0x32 || light.A != 0x12 {
			t.Fatalf("%s light shadow/card #%02x%02x%02x%02x", id, light.R, light.G, light.B, light.A)
		}
	}
	prev := gradSync.Load()
	gradSync.Store(true)
	t.Cleanup(func() {
		gradSync.Store(prev)
		cardShadowShot = 0
	})
	if n := len(shadowMatrix()); n != 24 {
		t.Fatalf("matrix has %d cases, want 24", n)
	}
	for _, tc := range shadowMatrix() {
		with := openCardShadow(t, tc.theme, tc.dark, tc.settings, 0)
		card := cardShadowRect(t, with, tc.settings)
		off := openCardShadow(t, tc.theme, tc.dark, tc.settings, 1)
		d, x, y := maxShadowDelta(t, tc.name(), with.Image(), off.Image(), card)
		if d > 1 {
			t.Fatalf("%s pixel %d,%d differs from the bare background by %d/255 (card %v)", tc.name(), x, y, d, card)
		}
	}
}

func TestContentCardShadowShots(t *testing.T) {
	dir := testArtifactDir(t, "shadows-all")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	prev := gradSync.Load()
	gradSync.Store(true)
	t.Cleanup(func() {
		gradSync.Store(prev)
		cardShadowShot = 0
	})
	var lines []string
	pass, fail := 0, 0
	for _, tc := range shadowMatrix() {
		with := openCardShadow(t, tc.theme, tc.dark, tc.settings, 0)
		card := cardShadowRect(t, with, tc.settings)
		crop := mainBottomCrop(card)
		if tc.settings {
			crop = settingsJoinCrop(card)
		}
		writePNG(t, filepath.Join(dir, tc.name()+".png"), scaleNN(cropImage(with.Image(), crop), 8))
		off := openCardShadow(t, tc.theme, tc.dark, tc.settings, 1)
		d, _, _ := maxShadowDelta(t, tc.name(), with.Image(), off.Image(), card)
		status := "PASS"
		if d > 1 {
			status = "FAIL"
			fail++
		} else {
			pass++
		}
		lines = append(lines, fmt.Sprintf("%s max=%d %s", tc.name(), d, status))
	}
	lines = append(lines, fmt.Sprintf("%d PASS, %d FAIL", pass, fail))
	body := ""
	for _, line := range lines {
		body += line + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "diff.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if fail > 0 {
		t.Fatalf("%d shadow cases exceed 1/255 beyond 8pt\n%s", fail, body)
	}
}

func openCardShadow(t *testing.T, theme string, dark, settings bool, shot int) *ui.Tester {
	t.Helper()
	cardShadowShot = shot
	a := newTestApp(t)
	a.data.Theme = theme
	a.data.Appearance = appearanceLight
	if dark {
		a.data.Appearance = appearanceDark
	}
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	a.data.Conversations = []Conversation{{
		ID: "c", Title: "Notes", Config: DefaultConfig(), UpdatedAt: now, CreatedAt: now,
		Messages: []Message{{ID: "u", Role: roleUser, Content: "Hello", Done: true, CreatedAt: now}},
	}}
	a.data.ActiveID = "c"
	w, h := 960, 640
	if settings {
		a.settingsOpen = true
		a.settingsSection = "appearance"
		w, h = 780, 560
	}
	tt := ui.NewTester(a.view, w, h)
	tt.SetDark(dark)
	tt.Frame()
	return tt
}

func cardShadowRect(t *testing.T, tt *ui.Tester, settings bool) ui.Rect {
	t.Helper()
	label := "content card"
	if settings {
		label = "settings card"
	}
	return mustFind(t, tt, label)
}

// maxShadowDelta is the largest per-channel difference, in the region more
// than 8pt left of or below the card, between the card shadow and the same
// frame with no card shadow. x, y is where that maximum sits.
func maxShadowDelta(t *testing.T, name string, with, off *image.RGBA, card ui.Rect) (d, atX, atY int) {
	t.Helper()
	if with.Bounds() != off.Bounds() {
		t.Fatalf("%s size %v vs %v", name, with.Bounds(), off.Bounds())
	}
	b := with.Bounds()
	const gap = 8
	checked := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			left := float32(x)+0.5 < card.X-gap
			below := float32(y)+0.5 > card.Y+card.H+gap
			if !left && !below {
				continue
			}
			if got := shadowDelta(with.RGBAAt(x, y), off.RGBAAt(x, y)); got > d {
				d, atX, atY = got, x, y
			}
			checked++
		}
	}
	if checked < 1000 {
		t.Fatalf("%s only checked %d pixels beyond 8pt", name, checked)
	}
	return d, atX, atY
}

func shadowDelta(a, b color.RGBA) int {
	d := absInt(int(a.R) - int(b.R))
	if g := absInt(int(a.G) - int(b.G)); g > d {
		d = g
	}
	if bl := absInt(int(a.B) - int(b.B)); bl > d {
		d = bl
	}
	return d
}

func mainBottomCrop(card ui.Rect) image.Rectangle {
	x1 := int(card.X) + 64
	y0 := int(card.Y+card.H) - 28
	return image.Rect(0, y0, x1, y0+48)
}

func settingsJoinCrop(card ui.Rect) image.Rectangle {
	x0 := int(card.X) - 40
	if x0 < 0 {
		x0 = 0
	}
	return image.Rect(x0, int(card.Y)-4, int(card.X)+20, int(card.Y+card.H)+12)
}

func cropImage(src *image.RGBA, r image.Rectangle) *image.RGBA {
	b := src.Bounds()
	r = r.Intersect(b)
	out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	for y := r.Min.Y; y < r.Max.Y; y++ {
		copy(out.Pix[(y-r.Min.Y)*out.Stride:(y-r.Min.Y)*out.Stride+r.Dx()*4], src.Pix[y*src.Stride+r.Min.X*4:y*src.Stride+r.Max.X*4])
	}
	return out
}
