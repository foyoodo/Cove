package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cove/themes"

	"github.com/egoist/mygo/ui"
)

// The seam was a second plate drawn only in the composer's shape at 85%.
// The fade behind it was already the full card, and both used the same
// card-over-window color. The sides were a different strength, not a
// different sample. This test covers a flat transcript with one veil:
// each row's strength is constant across the card, including behind the
// composer, and the composer's corners are not a brighter or darker patch.
func TestVeilStrengthConstant(t *testing.T) {
	probe := ui.Color{R: 12, G: 170, B: 48, A: 255}
	for _, preset := range themes.Presets() {
		for _, dark := range []bool{false, true} {
			mode := "light"
			if dark {
				mode = "dark"
			}
			for _, width := range []int{1120, 720} {
				t.Run(fmt.Sprintf("%s-%s-%d", preset.ID, mode, width), func(t *testing.T) {
					p := probe
					a, tt := veilEdgeWindow(t, preset.ID, dark, width, &p)
					assertVeilStrength(t, tt, p)
					assertComposerCorners(t, a, tt, p)
				})
			}
		}
	}
}

// A switch that arrives halfway through keeps the veil that is on screen.
// The next frame is that blend, not the earlier theme and not the
// unblended incoming preview.
func TestVeilMidSwitchMatchesScreen(t *testing.T) {
	resetGradientCache()
	resetVeilCache()
	a := revisionChat(t)
	a.instant = false
	a.data.Appearance = appearanceLight
	tt := ui.NewTester(a.view, 1120, 760)
	settleVeil(t, tt)
	a.setTheme(themes.ThemeTide, tt.Now())
	runOneFrame(tt)
	finishSwitch(t, tt, a)
	a.setTheme(themes.ThemeWarm, tt.Now())
	runOneFrame(tt)
	finishSwitch(t, tt, a)

	x, y := veilOpaqueSample(t, tt)
	pixA := rgbAt(tt, x, y)
	if d := dist3(pixA, cardAt(t, a, x, y)); d > 3 {
		t.Fatalf("settled veil %v is not the card %v (delta %d)", pixA, cardAt(t, a, x, y), d)
	}
	pureB := cardPixel(t, themeLook(themes.ThemeTide, false), a.winPW, a.winPH, x, y)

	a.setTheme(themes.ThemeTide, tt.Now())
	runOneFrame(tt)
	for i := 0; i < 20 && clockU(tt.Now(), a.backdropFadeAt, motionTheme) < 0.5; i++ {
		stepFrame(tt)
	}
	awaitVeils()
	runOneFrame(tt)
	u := clockU(tt.Now(), a.backdropFadeAt, motionTheme)
	if u < 0.45 || u > 0.65 {
		t.Fatalf("veil mid-fade clock %.3f", u)
	}
	pixMid := rgbAt(tt, x, y)
	if dist3(pixMid, pixA) < 4 {
		t.Fatalf("mid veil %v is still the earlier theme %v", pixMid, pixA)
	}
	if dist3(pixMid, pureB) < 4 {
		t.Fatalf("mid veil %v is the unblended incoming theme %v", pixMid, pureB)
	}

	a.setTheme(themes.ThemeSage, tt.Now())
	runOneFrame(tt)
	if clockU(tt.Now(), a.backdropFadeAt, motionTheme) != 0 {
		t.Fatalf("next veil clock started at %.3f", clockU(tt.Now(), a.backdropFadeAt, motionTheme))
	}
	got := rgbAt(tt, x, y)
	if d := dist3(got, pixMid); d > 2 {
		t.Fatalf("veil started from %v, on-screen composite was %v (delta %d)", got, pixMid, d)
	}
	for i := 0; i < 40; i++ {
		if !a.themeMoving() && backdropAdopted(a) && i > 0 {
			break
		}
		stepFrame(tt)
		awaitVeils()
		have := rgbAt(tt, x, y)
		if dist3(have, pixA) <= 2 {
			t.Fatalf("veil reverted to the earlier theme %v (pixel %v)", pixA, have)
		}
		if dist3(have, pureB) <= 2 {
			t.Fatalf("veil showed the unblended incoming theme %v (pixel %v)", pureB, have)
		}
	}
	finishSwitch(t, tt, a)
	end := rgbAt(tt, x, y)
	want := cardAt(t, a, x, y)
	if d := dist3(end, want); d > 3 {
		t.Fatalf("landed veil %v, card %v (delta %d)", end, want, d)
	}
}

func TestVeilEdgeShots(t *testing.T) {
	dir := "/opt/cursor/artifacts/veil-edges"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, preset := range themes.Presets() {
		for _, dark := range []bool{false, true} {
			mode := "light"
			if dark {
				mode = "dark"
			}
			for _, width := range []int{1120, 720} {
				_, tt := veilEdgeWindow(t, preset.ID, dark, width, nil)
				scrollTranscriptBehind(t, tt)
				assertVeilSpansCard(t, tt)
				name := "main-scrolled-" + preset.ID + "-" + mode
				if width != 1120 {
					name += fmt.Sprintf("-w%d", width)
				}
				saveVeilEdgeShots(t, dir, name, tt)
			}
		}
	}
}

func veilEdgeWindow(t *testing.T, theme string, dark bool, width int, probe *ui.Color) (*App, *ui.Tester) {
	t.Helper()
	resetGradientCache()
	resetVeilCache()
	a := revisionChat(t)
	padTranscript(a)
	a.veilProbe = probe
	a.instant = false
	a.data.Theme = theme
	a.data.Appearance = appearanceLight
	if dark {
		a.data.Appearance = appearanceDark
	}
	tt := ui.NewTester(a.view, width, 760)
	settleVeil(t, tt)
	return a, tt
}

func scrollTranscriptBehind(t *testing.T, tt *ui.Tester) {
	t.Helper()
	card, ok := tt.Find("content card")
	if !ok {
		t.Fatal("content card missing")
	}
	comp, ok := tt.Find("composer")
	if !ok {
		t.Fatal("composer missing")
	}
	// Wheel events have to land on the transcript, above the composer.
	// Negative dy moves the view toward the start, so a block that sits
	// above the composer travels down behind it.
	sx, sy := card.X+48, card.Y+80
	placed := false
	var block ui.Rect
	for i := 0; i < 80; i++ {
		var found bool
		block, found = tt.Find("code block")
		comp, _ = tt.Find("composer")
		if found && block.Y >= comp.Y-float32(composerFade) && block.Y <= comp.Y-4 && block.Y+block.H > comp.Y+comp.H+2 {
			placed = true
			break
		}
		dy := float32(-48)
		if found && block.Y > comp.Y-4 {
			dy = 24
		}
		tt.Scroll(sx, sy, 0, dy)
	}
	if !placed {
		t.Fatalf("code block is not under the composer: block %v composer %v", block, comp)
	}
}

// padTranscript lengthens the thread above the code block so the list can
// scroll that block behind the composer. The revision fixture is only a
// little taller than the window.
func padTranscript(a *App) {
	stamp := time.Date(2026, 10, 20, 9, 0, 0, 0, time.Local)
	var extra []Message
	body := strings.Repeat("An earlier reply, so the transcript is taller than the window.\n\n", 3)
	for n := 0; n < 4; n++ {
		extra = append(extra,
			Message{ID: fmt.Sprintf("pad-u%d", n), Role: roleUser, Content: fmt.Sprintf("Earlier question %d.", n), Done: true, CreatedAt: stamp},
			Message{ID: fmt.Sprintf("pad-a%d", n), Role: roleAssistant, Content: body, Done: true, CreatedAt: stamp},
		)
	}
	for i := range a.data.Conversations {
		if a.data.Conversations[i].ID != "cancel" {
			continue
		}
		a.data.Conversations[i].Messages = append(extra, a.data.Conversations[i].Messages...)
	}
}

func assertVeilSpansCard(t *testing.T, tt *ui.Tester) {
	t.Helper()
	comp, ok := tt.Find("composer")
	if !ok {
		t.Fatal("composer missing")
	}
	fade, ok := tt.Find("composer fade")
	if !ok {
		t.Fatal("composer fade missing")
	}
	card, ok := tt.Find("content card")
	if !ok {
		t.Fatal("content card missing")
	}
	if fade.X > card.X+2 || fade.X+fade.W < card.X+card.W-2 {
		t.Fatalf("veil %v does not span the card %v", fade, card)
	}
	if fade.X > comp.X+1 || fade.X+fade.W < comp.X+comp.W-1 {
		t.Fatalf("veil %v is narrower than the composer %v", fade, comp)
	}
}

func assertVeilStrength(t *testing.T, tt *ui.Tester, probe ui.Color) {
	t.Helper()
	comp, card := composerAndCard(t, tt)
	v, pix, bw := fadeVeilPixels(t)
	// A row in the fade, above the composer shadow and checked around the
	// jump pill. The flat transcript makes a strength error show up as a
	// channel delta against the card-colored veil.
	yFade := int(comp.Y) - 24
	assertRowAlpha(t, v, pix, bw, card, yFade)
	assertRowFrame(t, tt, v, pix, bw, card, yFade, probe)
	// Through the composer and below it, beside and behind share one alpha.
	for _, y := range []int{int(comp.Y) + 8, int(comp.Y+comp.H) + 4} {
		assertRowAlpha(t, v, pix, bw, card, y)
		beside := int(card.X) + 6
		behind := int(comp.X + comp.W/2)
		ab, okB := veilAlphaAtPixel(v, pix, bw, beside, y)
		ah, okH := veilAlphaAtPixel(v, pix, bw, behind, y)
		if !okB || !okH || absInt(int(ab)-int(ah)) > 1 {
			t.Fatalf("strength beside the composer (%d) is not the strength behind it (%d) at y=%d", ab, ah, y)
		}
	}
}

func assertRowAlpha(t *testing.T, v *veilEntry, pix []byte, bw int, card ui.Rect, y int) {
	t.Helper()
	minA, maxA := 255, 0
	n := 0
	for x := int(card.X) + 4; x < int(card.X+card.W)-6; x++ {
		a, ok := veilAlphaAtPixel(v, pix, bw, x, y)
		if !ok {
			t.Fatalf("veil does not cover (%d,%d)", x, y)
		}
		if int(a) < minA {
			minA = int(a)
		}
		if int(a) > maxA {
			maxA = int(a)
		}
		n++
	}
	if n < 8 {
		t.Fatalf("row y=%d has %d veil samples", y, n)
	}
	if maxA-minA > 1 {
		t.Fatalf("veil strength jumps across y=%d: alpha %d..%d", y, minA, maxA)
	}
}

func assertRowFrame(t *testing.T, tt *ui.Tester, v *veilEntry, pix []byte, bw int, card ui.Rect, y int, probe ui.Color) {
	t.Helper()
	jump, hasJump := tt.Find("Jump to latest")
	for x := int(card.X) + 4; x < int(card.X+card.W)-8; x++ {
		if hasJump && float32(x) >= jump.X && float32(x) < jump.X+jump.W && float32(y) >= jump.Y && float32(y) < jump.Y+jump.H {
			continue
		}
		src, ok := veilPixel(v, pix, bw, x, y)
		if !ok {
			t.Fatalf("veil does not cover (%d,%d)", x, y)
		}
		want := blendVeilOver(src, probe)
		got := rgbAt(tt, x, y)
		if d := dist3(got, want); d > 1 {
			t.Fatalf("flat transcript strength at (%d,%d): got %v, veil-over-flat %v (delta %d)", x, y, got, want, d)
		}
	}
}

func assertComposerCorners(t *testing.T, a *App, tt *ui.Tester, probe ui.Color) {
	t.Helper()
	comp, card := composerAndCard(t, tt)
	v, pix, bw := fadeVeilPixels(t)
	plate := a.look.LayerComposer
	// Just inside the composer's rounded corner, where the veil shows,
	// and a neighbor on that same veil clear of the plate. The neighbor
	// has to stay inside the card's corner: at an 8pt inset the old
	// 8px step lands outside r22.
	type corner struct{ x, y, nx, ny int }
	inset := 2
	topY := int(comp.Y) + inset
	botY := int(comp.Y+comp.H) - 1 - inset
	lx, ly := veilNeighbor(card, comp, topY, true)
	rx, ry := veilNeighbor(card, comp, topY, false)
	blx, bly := veilNeighbor(card, comp, botY, true)
	brx, bry := veilNeighbor(card, comp, botY, false)
	corners := []corner{
		{int(comp.X) + inset, topY, lx, ly},
		{int(comp.X+comp.W) - 1 - inset, topY, rx, ry},
		{int(comp.X) + inset, botY, blx, bly},
		{int(comp.X+comp.W) - 1 - inset, botY, brx, bry},
	}
	for _, c := range corners {
		// A composer corner that sits in the card's own rounded corner is
		// the card edge, not a plate seam. At an 8pt inset the bottom
		// corners are inside that quarter-circle.
		if inCornerZone(card, c.x, c.y) {
			continue
		}
		src, ok := veilPixel(v, pix, bw, c.x, c.y)
		if _, nok := veilPixel(v, pix, bw, c.nx, c.ny); !ok || !nok {
			t.Fatalf("corner (%d,%d) or neighbor (%d,%d) is outside the veil", c.x, c.y, c.nx, c.ny)
		}
		exp := blendVeilOver(src, probe)
		got := rgbAt(tt, c.x, c.y)
		neighbor := rgbAt(tt, c.nx, c.ny)
		// The shadow may darken the corner. A plate patch would push a
		// channel above the veil color, toward the composer fill.
		for i, ch := range got {
			if int(ch) > int(exp[i])+1 {
				t.Fatalf("composer corner (%d,%d) %v is brighter than the veil %v (plate %v)", c.x, c.y, got, exp, plate)
			}
		}
		// The composer's shadow darkens the corner by a few codes (measured
		// 7 on the flat probe). The old plate jumped by several times that.
		if d := dist3(got, neighbor); d > 12 {
			t.Fatalf("composer corner (%d,%d) %v seams against %v (delta %d)", c.x, c.y, got, neighbor, d)
		}
	}
}

// veilNeighbor is a point just outside the composer on this row, inside
// the card's rounded rect.
func veilNeighbor(card, comp ui.Rect, y int, left bool) (int, int) {
	for dist := 8; dist >= 2; dist-- {
		x := int(comp.X) - dist
		if !left {
			x = int(comp.X+comp.W) - 1 + dist
		}
		if pointInCard(card, x, y, float32(radiusContentCard)) {
			return x, y
		}
	}
	// The bottom corner has no horizontal room inside r22. Step up the
	// gutter until the point is on the straight side of the card.
	x := int(comp.X) - 3
	if !left {
		x = int(comp.X+comp.W) + 2
	}
	for ny := y; ny > int(card.Y)+2; ny -= 3 {
		if pointInCard(card, x, ny, float32(radiusContentCard)) {
			return x, ny
		}
	}
	return x, y
}

func inCornerZone(card ui.Rect, x, y int) bool {
	fx := float32(x) + 0.5
	fy := float32(y) + 0.5
	r := float32(radiusContentCard)
	nearX := fx < card.X+r || fx > card.X+card.W-r
	nearY := fy < card.Y+r || fy > card.Y+card.H-r
	return nearX && nearY
}

func pointInCard(card ui.Rect, x, y int, radius float32) bool {
	fx := float32(x) + 0.5
	fy := float32(y) + 0.5
	if fx < card.X || fy < card.Y || fx >= card.X+card.W || fy >= card.Y+card.H {
		return false
	}
	left := fx < card.X+radius
	right := fx > card.X+card.W-radius
	top := fy < card.Y+radius
	bottom := fy > card.Y+card.H-radius
	if (!left && !right) || (!top && !bottom) {
		return true
	}
	cx, cy := card.X+radius, card.Y+radius
	if right {
		cx = card.X + card.W - radius
	}
	if bottom {
		cy = card.Y + card.H - radius
	}
	dx, dy := fx-cx, fy-cy
	return dx*dx+dy*dy <= radius*radius
}

func composerAndCard(t *testing.T, tt *ui.Tester) (comp, card ui.Rect) {
	t.Helper()
	var ok bool
	comp, ok = tt.Find("composer")
	if !ok {
		t.Fatal("composer missing")
	}
	card, ok = tt.Find("content card")
	if !ok {
		t.Fatal("content card missing")
	}
	return comp, card
}

func fadeVeilPixels(t *testing.T) (*veilEntry, []byte, int) {
	t.Helper()
	veilCacheMu.Lock()
	v := veilLive[veilFade]
	veilCacheMu.Unlock()
	if v == nil || v.bm == nil {
		t.Fatal("veil bitmap missing")
	}
	_, _, bw, _, pix := bitmapIdentity(t, v.bm)
	if bw != v.key.pw {
		t.Fatalf("veil bitmap width %d, key %d", bw, v.key.pw)
	}
	return v, pix, bw
}

func veilAlphaAtPixel(v *veilEntry, pix []byte, bw, x, y int) (byte, bool) {
	p, ok := veilPixel(v, pix, bw, x, y)
	if !ok {
		return 0, false
	}
	return p[3], true
}

func veilPixel(v *veilEntry, pix []byte, bw, x, y int) ([4]byte, bool) {
	i := x - v.key.x0
	j := y - v.key.y0
	if i < 0 || j < 0 || i >= v.key.pw || j >= v.key.ph {
		return [4]byte{}, false
	}
	o := (j*bw + i) * 4
	return [4]byte{pix[o], pix[o+1], pix[o+2], pix[o+3]}, true
}

func blendVeilOver(src [4]byte, dst ui.Color) [3]byte {
	inv := 255 - int(src[3])
	ch := func(p, d byte) byte {
		v := int(p) + (int(d)*inv+127)/255
		if v > 255 {
			return 255
		}
		return byte(v)
	}
	return [3]byte{ch(src[0], dst.R), ch(src[1], dst.G), ch(src[2], dst.B)}
}

func veilOpaqueSample(t *testing.T, tt *ui.Tester) (int, int) {
	t.Helper()
	comp, ok := tt.Find("composer")
	if !ok {
		t.Fatal("composer missing")
	}
	x := int(comp.X) - 48
	y := int(comp.Y) + 24
	if x < 8 {
		x = int(comp.X+comp.W) + 48
	}
	return x, y
}

func rgbAt(tt *ui.Tester, x, y int) [3]byte {
	c := tt.Image().RGBAAt(x, y)
	return [3]byte{c.R, c.G, c.B}
}

func cardAt(t *testing.T, a *App, x, y int) [3]byte {
	t.Helper()
	return cardPixel(t, a.lookTarget, a.winPW, a.winPH, x, y)
}

func cardPixel(t *testing.T, look Theme, w, h, x, y int) [3]byte {
	t.Helper()
	bg := gradientPixel(t, look, w, h, x, y)
	over := look.LayerContent.Over(ui.Color{R: bg[0], G: bg[1], B: bg[2], A: 255})
	return [3]byte{over.R, over.G, over.B}
}

func gradientPixel(t *testing.T, look Theme, w, h, x, y int) [3]byte {
	t.Helper()
	e := cachedGradient(look, w, h)
	if e == nil || e.img == nil {
		t.Fatalf("no background at %dx%d", w, h)
	}
	if x < 0 || y < 0 || x >= e.img.Rect.Dx() || y >= e.img.Rect.Dy() {
		t.Fatalf("background sample %d,%d outside %v", x, y, e.img.Rect)
	}
	i := e.img.PixOffset(x, y)
	return [3]byte{e.img.Pix[i], e.img.Pix[i+1], e.img.Pix[i+2]}
}

func saveVeilEdgeShots(t *testing.T, dir, name string, tt *ui.Tester) {
	t.Helper()
	img := tt.Image()
	comp, ok := tt.Find("composer")
	if !ok {
		t.Fatal("composer missing")
	}
	writePNG(t, filepath.Join(dir, name+".png"), img)
	side := image.Rect(int(comp.X)-36, int(comp.Y)-40, int(comp.X)+28, int(comp.Y+comp.H)+28)
	writePNG(t, filepath.Join(dir, name+"-left-4x.png"), zoom4(cropRGBA(img, side)))
	side.Min.X = int(comp.X+comp.W) - 28
	side.Max.X = int(comp.X+comp.W) + 36
	writePNG(t, filepath.Join(dir, name+"-right-4x.png"), zoom4(cropRGBA(img, side)))
	below := image.Rect(int(comp.X+comp.W/2)-48, int(comp.Y+comp.H)-8, int(comp.X+comp.W/2)+48, int(comp.Y+comp.H)+28)
	writePNG(t, filepath.Join(dir, name+"-below-4x.png"), zoom4(cropRGBA(img, below)))
	if jump, ok := tt.Find("Jump to latest"); ok {
		pill := image.Rect(int(jump.X)-28, int(jump.Y)-8, int(jump.X+jump.W)+28, int(jump.Y+jump.H)+36)
		writePNG(t, filepath.Join(dir, name+"-jump-4x.png"), zoom4(cropRGBA(img, pill)))
	}
}

func cropRGBA(src *image.RGBA, r image.Rectangle) *image.RGBA {
	r = r.Intersect(src.Bounds())
	dst := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	for y := 0; y < r.Dy(); y++ {
		for x := 0; x < r.Dx(); x++ {
			c := src.RGBAAt(r.Min.X+x, r.Min.Y+y)
			dst.SetRGBA(x, y, color.RGBA{R: c.R, G: c.G, B: c.B, A: 255})
		}
	}
	return dst
}
