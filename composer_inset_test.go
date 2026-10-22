package main

import (
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestComposerInsets(t *testing.T) {
	want := []struct {
		w, side, bottom float32
	}{
		{600, 8, 8},
		{736, 8, 8},
		{800, 40, 14},
		{1000, 140, 14},
		{1400, 340, 14},
	}
	for _, c := range want {
		side, bottom := composerInsets(c.w)
		if side != c.side || bottom != c.bottom {
			t.Fatalf("width %.0f: side %.2f bottom %.2f, want %.0f and %.0f", c.w, side, bottom, c.side, c.bottom)
		}
	}
	side, bottom := composerInsets(600)
	if radiusContentCard != radiusComposer+side || side != bottom {
		t.Fatalf("narrow concentricity: card %d composer %d inset %.2f/%.2f", radiusContentCard, radiusComposer, side, bottom)
	}
	// Half a point on either side of 736 stays on the same slope.
	// The clamp and the formula meet at 8, so nothing jumps.
	for _, w := range []float32{735, 735.5, 736, 736.5, 737} {
		s, b := composerInsets(w)
		prev, prevB := composerInsets(w - 0.5)
		if math.Abs(float64(s-prev)) > 0.25+1e-4 || math.Abs(float64(b-prevB)) > 0.25+1e-4 {
			t.Fatalf("jump at %.1f: side %.3f→%.3f bottom %.3f→%.3f", w, prev, s, prevB, b)
		}
	}
	if s, b := composerInsets(735); s != 8 || b != 8 {
		t.Fatalf("735: %.2f %.2f", s, b)
	}
	if s, b := composerInsets(737); math.Abs(float64(s-8.5)) > 1e-4 || math.Abs(float64(b-8.5)) > 1e-4 {
		t.Fatalf("737: %.2f %.2f", s, b)
	}
}

func TestProviderShowsSavedKeyBeforeRead(t *testing.T) {
	inner := newCountingKeyring()
	path := filepath.Join(t.TempDir(), "conversations.json")
	seed := openApp(path, inner)
	seed.instant = true
	seed.data.Connections = []Connection{{
		ID: "alpha", Name: "Alpha", Provider: "OpenAI",
		Models: []ProviderModel{{ID: "gpt-4o", Enabled: true}}, ModelsSet: true,
	}}
	seed.data.Defaults.ConnectionID = "alpha"
	seed.data.Defaults.Provider = "Alpha"
	seed.data.Defaults.Model = "gpt-4o"
	seed.data.Defaults.BaseURL = "https://api.openai.com/v1"
	inner.seed("alpha", "sk-alpha")
	seed.saveSync()

	a := openApp(path, inner)
	if inner.Gets("alpha") != 0 {
		t.Fatalf("launch read the key %d times", inner.Gets("alpha"))
	}
	kind, label := a.providerStatus(a.connByID("alpha"), false)
	if kind == "none" || label == "Not set up" {
		t.Fatalf("provider list shows %s %q before the first read", kind, label)
	}
	found := false
	for _, opt := range a.menuModels() {
		if opt.connID == "alpha" && opt.model == "gpt-4o" {
			found = true
		}
	}
	if !found {
		t.Fatalf("model menu hid the provider before the first read: %+v", a.menuModels())
	}
	if inner.Gets("alpha") != 0 {
		t.Fatalf("menu or list read the key %d times", inner.Gets("alpha"))
	}
}

func TestComposerInsetCrops(t *testing.T) {
	dir := testArtifactDir(t, "composer-insets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	shots := []struct {
		name  string
		w, h  int
		dark  bool
		cardW float32
	}{
		{"720x520-light", 720, 520, false, 0},
		{"720x520-dark", 720, 520, true, 0},
		{"1200x800-light", 1200, 800, false, 0},
		{"1200x800-dark", 1200, 800, true, 0},
		{"card735-light", 985, 520, false, 735},
		{"card737-light", 987, 520, false, 737},
	}
	for _, shot := range shots {
		a := newTestApp(t)
		a.newChat()
		a.active().Config.Model = "gpt-4o"
		a.active().Config.Provider = "OpenAI"
		a.active().Config.BaseURL = "https://api.openai.com/v1"
		if shot.dark {
			a.data.Appearance = appearanceDark
		}
		tt := ui.NewTester(a.view, shot.w, shot.h)
		tt.SetScale(2)
		tt.SetDark(shot.dark)
		tt.Frame()
		card, ok := tt.Find("content card")
		if !ok {
			t.Fatal("content card missing")
		}
		comp, ok := tt.Find("composer")
		if !ok {
			t.Fatal("composer missing")
		}
		innerW := card.W - 2
		if shot.cardW != 0 && math.Abs(float64(innerW-shot.cardW)) > 0.6 {
			t.Fatalf("%s card inner width %.2f, want %.0f (outer %.2f)", shot.name, innerW, shot.cardW, card.W)
		}
		side, bottom := composerInsets(innerW)
		gotSide := comp.X - (card.X + 1)
		gotRight := (card.X + card.W - 1) - (comp.X + comp.W)
		gotBottom := (card.Y + card.H - 1) - (comp.Y + comp.H)
		if math.Abs(float64(gotSide-side)) > 0.6 || math.Abs(float64(gotRight-side)) > 0.6 || math.Abs(float64(gotBottom-bottom)) > 0.6 {
			t.Fatalf("%s insets side %.2f/%.2f bottom %.2f, want %.2f and %.2f (card %.1f)", shot.name, gotSide, gotRight, gotBottom, side, bottom, innerW)
		}
		img := tt.Image()
		f, err := os.Create(filepath.Join(dir, shot.name+"-2x.png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			f.Close()
			t.Fatal(err)
		}
		f.Close()
		scale := float32(2)
		t.Logf("%s card %.1f×%.1f side %.2fpt (%.0fpx) bottom %.2fpt (%.0fpx) card r %dpt (%dpx) composer r %dpt (%dpx) png %dx%d",
			shot.name, innerW, card.H, gotSide, gotSide*scale, gotBottom, gotBottom*scale,
			radiusContentCard, radiusContentCard*int(scale), radiusComposer, radiusComposer*int(scale),
			img.Bounds().Dx(), img.Bounds().Dy())
		if img.Bounds().Dx() != shot.w*2 || img.Bounds().Dy() != shot.h*2 {
			t.Fatalf("%s png %v", shot.name, img.Bounds())
		}
		// The outer corner at 2× is 44px. A pixel just inside that radius
		// belongs to the card; a pixel just outside it does not.
		assertCardRadius(t, img, card, scale, float32(radiusContentCard))
	}
	// The two crops on either side of 736 differ by the continuous step,
	// half a point, which is one device pixel at 2×.
	s0, _ := composerInsets(735)
	s1, _ := composerInsets(737)
	if math.Abs(float64((s1-s0)*2)-1) > 0.05 {
		t.Fatalf("736±1 side step is %.2fpx, want 1", (s1-s0)*2)
	}
}

func assertCardRadius(t *testing.T, img *image.RGBA, card ui.Rect, scale, radius float32) {
	t.Helper()
	// Bottom-right corner of the border box. The center of the quarter
	// circle is one radius in from the outer edges.
	cx := (card.X + card.W - radius) * scale
	cy := (card.Y + card.H - radius) * scale
	rad := radius * scale
	// A device pixel whose center is about 2px outside the radius, still
	// inside the border box, must be outside the rounded card.
	px := int(cx + rad*0.8)
	py := int(cy + rad*0.8)
	dx := float32(px) + 0.5 - cx
	dy := float32(py) + 0.5 - cy
	if dx*dx+dy*dy < (rad+1)*(rad+1) {
		t.Fatalf("sample (%d,%d) is not outside r%.0f", px, py, rad)
	}
	out := img.RGBAAt(px, py)
	inX := int(cx)
	inY := int(cy + rad*0.5)
	in := img.RGBAAt(inX, inY)
	if out == in {
		t.Fatalf("corner r%.0fpx: outside pixel %+v matches the card %+v", rad, out, in)
	}
}
