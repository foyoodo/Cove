package main

import (
	"encoding/json"
	"image"
	"math"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The baked shadow's radial and side profiles stay with the float gradient:
// no run of identical codes longer than about 1pt where that gradient is
// still moving, and no dark spike at the plate edge.
func TestShadowProfiles(t *testing.T) {
	cases := []struct {
		name   string
		w, h   int
		radius float32
		layers []shadowLayer
		color  ui.Color
		scale  float32
	}{
		{"composer-light", 420, 73, 14, composerShadow, ui.Color{A: 0x14}, 1},
		{"composer-dark", 420, 73, 14, composerShadow, ui.Color{A: 0x52}, 1},
		{"composer-light-2x", 210, 73, 14, composerShadow, ui.Color{A: 0x14}, 2},
		{"jump-light", 148, 26, 999, jumpShadow, ui.Color{A: 0x14}, 1},
		{"jump-dark", 148, 26, 999, jumpShadow, ui.Color{A: 0x52}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ew := int(math.Round(float64(tc.w) * float64(tc.scale)))
			eh := int(math.Round(float64(tc.h) * float64(tc.scale)))
			img := renderDropShadow(ew, eh, tc.radius, tc.scale, tc.layers, tc.color)
			if img == nil {
				t.Fatal("no shadow")
			}
			pad := shadowPad(tc.layers, tc.scale)
			rad := clampRadius(float64(tc.radius)*float64(tc.scale), float64(ew), float64(eh))
			g := shadowGeom{ew: ew, eh: eh, radius: rad, scale: float64(tc.scale), layers: tc.layers, color: tc.color}
			// Side, left edge, vertical middle. Below, bottom edge, horizontal middle.
			assertShadowRay(t, img, pad, g, ray{fixed: pad + eh/2, start: pad - 1, axis: 0, dir: -1, n: 36})
			assertShadowRay(t, img, pad, g, ray{fixed: pad + ew/2, start: pad + eh, axis: 1, dir: 1, n: 36})
			assertShadowRadial(t, img, pad, g)
			assertNoEdgeSpike(t, img, pad, g)
		})
	}
}

type shadowGeom struct {
	ew, eh        int
	radius, scale float64
	layers        []shadowLayer
	color         ui.Color
}

type ray struct {
	fixed, start int
	axis         int // 0 walks x, fixed is y; 1 walks y, fixed is x
	dir, n       int
}

func assertShadowRay(t *testing.T, img *image.RGBA, pad int, g shadowGeom, r ray) {
	t.Helper()
	var alphas []byte
	var ideal []float64
	for i := 0; i < r.n; i++ {
		x, y := r.start, r.fixed
		if r.axis == 0 {
			x = r.start + r.dir*i
		} else {
			y = r.start + r.dir*i
		}
		if !image.Pt(x, y).In(img.Bounds()) {
			break
		}
		alphas = append(alphas, img.RGBAAt(x, y).A)
		px := float64(x-pad) + 0.5
		py := float64(y-pad) + 0.5
		ideal = append(ideal, dropAlpha(px, py, float64(g.ew), float64(g.eh), g.radius, g.scale, g.layers, g.color))
	}
	if len(alphas) < 8 {
		t.Fatalf("ray too short: %d", len(alphas))
	}
	assertShadowSamples(t, alphas, ideal, g.scale)
}

func assertShadowRadial(t *testing.T, img *image.RGBA, pad int, g shadowGeom) {
	t.Helper()
	// Bottom-left corner of the plate, walking out along the diagonal.
	r := g.radius
	if r < 1 {
		r = 1
	}
	cx := float64(pad) + r
	cy := float64(pad+g.eh) - r
	ux, uy := -1/math.Sqrt2, 1/math.Sqrt2
	var alphas []byte
	var ideal []float64
	for i := 0; i < 36; i++ {
		d := r + 0.5 + float64(i)
		x := int(math.Floor(cx + ux*d))
		y := int(math.Floor(cy + uy*d))
		if !image.Pt(x, y).In(img.Bounds()) {
			break
		}
		alphas = append(alphas, img.RGBAAt(x, y).A)
		px := float64(x-pad) + 0.5
		py := float64(y-pad) + 0.5
		ideal = append(ideal, dropAlpha(px, py, float64(g.ew), float64(g.eh), g.radius, g.scale, g.layers, g.color))
	}
	if len(alphas) < 8 {
		t.Fatalf("radial ray too short: %d", len(alphas))
	}
	assertShadowSamples(t, alphas, ideal, g.scale)
}

func assertShadowSamples(t *testing.T, alphas []byte, ideal []float64, scale float64) {
	t.Helper()
	if scale < 1 {
		scale = 1
	}
	start := 0
	for start < len(alphas) {
		end := start
		for end+1 < len(alphas) && alphas[end+1] == alphas[start] {
			end++
		}
		run := end - start + 1
		// A change of one code or less is flat in 8-bit: the curve has not
		// reached the next value, so one code across that span is the gradient.
		if runTooLong(run, scale) && math.Abs(ideal[end]-ideal[start]) > 1.0/255 {
			t.Errorf("plateau of %d px (%.2fpt) at %d, alpha %d, float %.4f→%.4f",
				run, float64(run-1)/scale, start, alphas[start], ideal[start], ideal[end])
		}
		start = end + 1
	}
	var maxA byte
	var maxIdeal float64
	for i, a := range alphas {
		if a > maxA {
			maxA = a
		}
		if ideal[i] > maxIdeal {
			maxIdeal = ideal[i]
		}
	}
	if maxIdeal > 0.02 && maxA < 2 {
		t.Errorf("float shadow peaks at %.3f but the bitmap alpha peaks at %d", maxIdeal, maxA)
	}
	// Interior of the falloff: a pixel darker than both neighbors by more
	// than one code is the hairline (or a quantization dip). The first
	// sample is the onset, which is allowed to be the darkest.
	for i := 1; i < len(alphas)-1; i++ {
		if int(alphas[i]) > int(alphas[i-1])+1 && int(alphas[i]) > int(alphas[i+1])+1 {
			t.Errorf("dark spike at %d: %d between %d and %d", i, alphas[i], alphas[i-1], alphas[i+1])
		}
	}
}

// assertNoEdgeSpike checks the first pixel outside the plate against the
// float shadow. The snapped contact layer used to be a full code darker
// than that gradient, which is the hairline.
func assertNoEdgeSpike(t *testing.T, img *image.RGBA, pad int, g shadowGeom) {
	t.Helper()
	points := [][2]int{
		{pad - 1, pad + g.eh/2},
		{pad + g.ew/2, pad + g.eh},
	}
	for _, p := range points {
		x, y := p[0], p[1]
		if !image.Pt(x, y).In(img.Bounds()) {
			t.Fatalf("edge %d,%d outside", x, y)
		}
		got := int(img.RGBAAt(x, y).A)
		px := float64(x-pad) + 0.5
		py := float64(y-pad) + 0.5
		ideal := dropAlpha(px, py, float64(g.ew), float64(g.eh), g.radius, g.scale, g.layers, g.color)
		want := int(math.Floor(ideal*255 + 0.5))
		if d := got - want; d > 1 || d < -1 {
			t.Errorf("edge alpha %d at %d,%d, float %.4f quantizes to %d", got, x, y, ideal, want)
		}
		// The next pixel outward must not be lighter than this one by more
		// than the float curve, plus one code. A darker edge than that
		// curve is the hairline.
		nx, ny := x, y
		if y == pad+g.eh/2 {
			nx = x - 1
		} else {
			ny = y + 1
		}
		if !image.Pt(nx, ny).In(img.Bounds()) {
			continue
		}
		next := int(img.RGBAAt(nx, ny).A)
		nIdeal := dropAlpha(float64(nx-pad)+0.5, float64(ny-pad)+0.5, float64(g.ew), float64(g.eh), g.radius, g.scale, g.layers, g.color)
		drop := (ideal - nIdeal) * 255
		if float64(got-next) > drop+1.5 {
			t.Errorf("edge darker than the float falloff: %d then %d, float drop %.2f", got, next, drop)
		}
	}
}

func TestShadowTokens(t *testing.T) {
	raw, err := os.ReadFile("themes/tokens.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Shadow map[string][]struct {
			X, Y, Blur, Spread float64
			Color              string
		} `json:"shadow"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	comp := doc.Shadow["layer/composer"]
	if len(comp) != 2 {
		t.Fatalf("layer/composer %#v", comp)
	}
	if comp[1].X != 0 || comp[1].Y != 6 || comp[1].Blur != 16 || comp[1].Spread != -6 {
		t.Fatalf("second composer layer = %+v, want 0,6,16,-6", comp[1])
	}
	if composerShadow[1].x != 0 || composerShadow[1].y != 6 || composerShadow[1].blur != 16 || composerShadow[1].spread != -6 {
		t.Fatalf("code second layer = %+v", composerShadow[1])
	}
	over := doc.Shadow["layer/overlay"]
	if len(over) < 1 || over[0].Y != 8 || over[0].Blur != 24 || over[0].Spread != 0 {
		t.Fatalf("overlay drop = %+v", over)
	}
	if jumpShadow[0].y != 8 || jumpShadow[0].blur != 24 || jumpShadow[0].spread != 0 {
		t.Fatalf("jump shadow = %+v", jumpShadow[0])
	}
}

func TestShadowCacheIdentity(t *testing.T) {
	resetShadowCache()
	prev := gradSync.Load()
	gradSync.Store(true)
	t.Cleanup(func() { gradSync.Store(prev) })
	color := ui.Color{A: 0x14}
	var buildsBefore uint64
	view := func(c *ui.Context) {
		ui.Box(c).Fill().Background(ui.Color{R: 255, G: 255, B: 255, A: 255})
		b := ui.Box(c).Absolute().Left(30).Top(30).Width(180).Height(56)
		b.Draw(func(p *ui.Painter, r ui.Rect) {
			paintDropShadow(p, r, 14, shadowComposer, color)
		})
	}
	buildsBefore = shadowBuilds.Load()
	tt := ui.NewTester(view, 280, 160)
	runOneFrame(tt)
	if got := shadowBuilds.Load() - buildsBefore; got != 1 {
		t.Fatalf("builds %d, want one new", got)
	}
	e := lookupShadow(shadowKey{
		kind:        shadowComposer,
		w:           180,
		h:           56,
		radiusMilli: 14000,
		scaleMilli:  1000,
		color:       packColor(color),
	})
	if e == nil || e.bm == nil {
		t.Fatal("shadow not cached")
	}
	id := bitmapImageID(e.bm)
	if id == 0 {
		t.Fatal("shadow image id is 0")
	}
	runOneFrame(tt)
	if bitmapImageID(lookupShadow(e.key).bm) != id {
		t.Fatal("cached shadow changed image id without a rebuild")
	}
	if shadowBuilds.Load() != buildsBefore+1 {
		t.Fatal("a settled frame rebuilt the shadow")
	}
	color = ui.Color{A: 0x52}
	runOneFrame(tt)
	e2 := lookupShadow(shadowKey{
		kind:        shadowComposer,
		w:           180,
		h:           56,
		radiusMilli: 14000,
		scaleMilli:  1000,
		color:       packColor(color),
	})
	if e2 == nil {
		t.Fatal("dark shadow not cached")
	}
	if bitmapImageID(e2.bm) == id {
		t.Fatal("a new build reused the previous image id")
	}
}

// The baked hole is the plate's border box. A wrapper that is a pixel off
// leaves a dark strip under the stroke.
func TestShadowHoleMatchesPlate(t *testing.T) {
	_, tt := veilEdgeWindow(t, "warm", false, 1120, nil)
	scrollTranscriptBehind(t, tt)
	comp, ok := tt.Find("composer")
	if !ok {
		t.Fatal("composer missing")
	}
	assertShadowHole(t, comp, shadowLive[shadowComposer])
	jump, ok := tt.Find("Jump to latest")
	if !ok {
		t.Fatal("jump pill missing")
	}
	assertShadowHole(t, jump, shadowLive[shadowJump])
}

func assertShadowHole(t *testing.T, plate ui.Rect, e *shadowEntry) {
	t.Helper()
	if e == nil {
		t.Fatal("no shadow")
	}
	_, ew := deviceSpan(plate.X, plate.X+plate.W, 1)
	_, eh := deviceSpan(plate.Y, plate.Y+plate.H, 1)
	if e.key.w != ew || e.key.h != eh {
		t.Fatalf("shadow %dx%d, plate device %dx%d (rect %v)", e.key.w, e.key.h, ew, eh, plate)
	}
}
