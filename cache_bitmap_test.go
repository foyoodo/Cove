package main

import (
	"bytes"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"cove/themes"

	"github.com/egoist/mygo/ui"
)

// holdDitheredBuilds blocks full-resolution gradient builds. Previews are
// not dithered, so they still publish while a build is held.
func holdDitheredBuilds() func() {
	release := make(chan struct{})
	var once sync.Once
	gateMu.Lock()
	prev := gradRenderGate
	gradRenderGate = func() { <-release }
	gateMu.Unlock()
	return func() {
		once.Do(func() {
			gateMu.Lock()
			gradRenderGate = prev
			gateMu.Unlock()
			close(release)
		})
	}
}

func lookID(id string, dark bool) Theme {
	return Theme{Colors: themes.Resolve(id, dark, true, ui.Color{}), Dark: dark, Focused: true}
}

func dist3(a, b [3]byte) int {
	d := absInt(int(a[0]) - int(b[0]))
	if g := absInt(int(a[1]) - int(b[1])); g > d {
		d = g
	}
	if bl := absInt(int(a[2]) - int(b[2])); bl > d {
		d = bl
	}
	return d
}

func previewColorAt(t *testing.T, bm *ui.Bitmap, winW, winH, x, y int) [3]byte {
	t.Helper()
	_, _, bw, bh, pix := bitmapIdentity(t, bm)
	src := &image.NRGBA{Pix: pix, Stride: bw * 4, Rect: image.Rect(0, 0, bw, bh)}
	u := (float32(x) + 0.5) * float32(bw) / float32(winW)
	v := (float32(y) + 0.5) * float32(bh) / float32(winH)
	r, g, b := sampleBilinear(src, u, v)
	return [3]byte{r, g, b}
}

func assertPreviewOnScreen(t *testing.T, tt *ui.Tester, a *App, before [3]byte) {
	t.Helper()
	if a.previewBM == nil {
		t.Fatal("preview bitmap is not on the clock")
	}
	img := tt.Image()
	b := img.Bounds()
	x, y := b.Dx()-8, 2
	got := img.RGBAAt(x, y)
	have := [3]byte{got.R, got.G, got.B}
	want := previewColorAt(t, a.previewBM, b.Dx(), b.Dy(), x, y)
	if dist3(have, want) > 8 {
		t.Fatalf("presented (%d,%d) %v is not the filtered preview %v", x, y, got, want)
	}
	if dist3(have, before) <= dist3(have, want) {
		t.Fatalf("presented %v did not move toward the new preview %v from %v", got, want, before)
	}
}

func assertTextureReplaced(t *testing.T, before, after *ui.Bitmap) {
	t.Helper()
	id0, ver0, w0, h0, pix0 := bitmapIdentity(t, before)
	id1, ver1, w1, h1, pix1 := bitmapIdentity(t, after)
	if id0 == id1 {
		t.Fatalf("bitmap kept image id %d", id0)
	}
	if sliceData(pix0) == sliceData(pix1) {
		t.Fatal("bitmaps share a pixel buffer")
	}
	var cache gpuImageCache
	tex0 := cache.imageTexture(id0, ver0, w0, h0, pix0)
	stale := append([]byte(nil), pix0...)
	stale[0] ^= 0xff
	if cache.imageTexture(id0, ver0, w0, h0, stale) != tex0 || cache.slots[id0].pix[0] != pix0[0] {
		t.Fatal("same image id and version refreshed the cached texture")
	}
	tex1 := cache.imageTexture(id1, ver1, w1, h1, pix1)
	if tex1 == tex0 {
		t.Fatal("new image id reused the cached texture")
	}
	if !bytes.Equal(cache.slots[id1].pix, pix1) {
		t.Fatal("cached texture bytes are not the new bitmap")
	}
}

func liveVeil(kind int) *veilEntry {
	veilCacheMu.Lock()
	defer veilCacheMu.Unlock()
	if kind < 0 || kind >= len(veilLive) {
		return nil
	}
	return veilLive[kind]
}

func assertVeilReplaced(t *testing.T, tt *ui.Tester, oldID uint64, oldPix []byte) {
	t.Helper()
	v := liveVeil(veilFade)
	if v == nil || v.bm == nil {
		t.Fatal("fade veil is not cached")
	}
	id, ver, w, h, pix := bitmapIdentity(t, v.bm)
	if id == oldID {
		t.Fatalf("veil kept image id %d", id)
	}
	changed := len(pix) != len(oldPix)
	if !changed {
		for i := range pix {
			if pix[i] != oldPix[i] {
				changed = true
				break
			}
		}
	}
	if !changed {
		t.Fatal("veil pixels did not change")
	}
	assertVeilComposite(t, tt, v, oldPix)
	var cache gpuImageCache
	tex := cache.imageTexture(id, ver, w, h, pix)
	if tex == 0 || !bytes.Equal(cache.slots[id].pix, pix) {
		t.Fatal("veil texture was not uploaded from the new bitmap")
	}
	stale := append([]byte(nil), pix...)
	if len(stale) > 0 {
		stale[0] ^= 0xff
	}
	if cache.imageTexture(id, ver, w, h, stale) != tex || (len(pix) > 0 && cache.slots[id].pix[0] != pix[0]) {
		t.Fatal("veil texture refreshed without a new image id")
	}
	_ = oldID
}

func geomOfVeil(k veilKey) veilGeom {
	return veilGeom{
		kind: k.kind, pw: k.pw, ph: k.ph, x0: k.x0, y0: k.y0,
		winW: k.winW, winH: k.winH, scaleMilli: k.scaleMilli,
		rectX: k.rectX, rectY: k.rectY, rectW: k.rectW, rectH: k.rectH,
		composerMilli: k.composerMilli,
	}
}

func unpackPacked(p uint32) ui.Color {
	return ui.Color{R: uint8(p >> 24), G: uint8(p >> 16), B: uint8(p >> 8), A: uint8(p)}
}

func gradientBySeq(seq uint64) *image.NRGBA {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	for _, e := range entries {
		if e != nil && e.seq == seq {
			return e.img
		}
	}
	return nil
}

// blendPremul is the software raster's blend of a premultiplied source
// (components 0 to 1) over an opaque pixel.
func blendPremul(dst [4]byte, src [4]float32) [4]byte {
	a := src[3]
	if a <= 0 {
		return dst
	}
	inv := 1 - a
	ch := func(s float32, d byte) byte {
		v := s*255 + float32(d)*inv + 0.5
		if v < 0 {
			return 0
		}
		if v > 255 {
			return 255
		}
		return byte(v)
	}
	return [4]byte{ch(src[0], dst[0]), ch(src[1], dst[1]), ch(src[2], dst[2]), ch(a, dst[3])}
}

func layerPremul(c ui.Color) [4]float32 {
	a := float32(c.A) / 255
	return [4]float32{float32(c.R) / 255 * a, float32(c.G) / 255 * a, float32(c.B) / 255 * a, a}
}

func assertVeilComposite(t *testing.T, tt *ui.Tester, v *veilEntry, oldPix []byte) {
	t.Helper()
	_, _, bw, bh, pix := bitmapIdentity(t, v.bm)
	if bw != v.key.pw || bh != v.key.ph {
		t.Fatalf("veil bitmap %dx%d, geometry %dx%d", bw, bh, v.key.pw, v.key.ph)
	}
	base := gradientBySeq(v.key.seq)
	if base == nil {
		t.Fatalf("gradient generation %d behind the veil is gone", v.key.seq)
	}
	fresh := renderVeil(base, geomOfVeil(v.key), unpackPacked(v.key.layer), nil)
	if fresh == nil || !bytes.Equal(fresh.Pix, pix) {
		t.Fatal("cached veil is not a render of the current theme at this size")
	}
	// Beside the composer the card is empty, and the veil is that card
	// color, so drawing it does not move the pixel. Drawing the previous
	// veil does. The frame has to match the new card and not the old veil.
	layer := unpackPacked(v.key.layer)
	over := layerPremul(layer)
	sameSize := len(oldPix) == len(pix)
	best := 0
	found := false
	var sx, sy int
	var cardPx, oldPx [3]byte
	// The gutter beside the composer is the veil. Its width follows the
	// inset, and the card corner is 22pt, so the bottom of that gutter
	// is the rounded corner. Sample beside the top of the composer.
	scale := float32(v.key.scaleMilli) / 1000
	if scale < 1 {
		scale = 1
	}
	cardW := float32(v.key.rectW) / 1000
	side, bottom := composerInsets(cardW)
	sidePx := int(side * scale)
	if sidePx < 4 {
		sidePx = 4
	}
	compPx := int(float32(v.key.composerMilli) / 1000 * scale)
	bottomPx := int(bottom * scale)
	// Midway down the composer the veil is at half strength, so two themes
	// differ, and this y is below the card's top corner and above its
	// bottom corner.
	yTop := bh - bottomPx - compPx/2 - int(4*scale)
	yBot := yTop + int(8*scale)
	if yTop < 0 {
		yTop = 0
	}
	if yBot > bh {
		yBot = bh
	}
	var xs []int
	for x := 2; x < sidePx; x++ {
		xs = append(xs, x, bw-1-x)
	}
	for y := yTop; y < yBot; y++ {
		for _, x := range xs {
			gx, gy := v.key.x0+x, v.key.y0+y
			if gx < 0 || gy < 0 || gx >= base.Rect.Dx() || gy >= base.Rect.Dy() {
				continue
			}
			bi := base.PixOffset(gx, gy)
			bg := [4]byte{base.Pix[bi], base.Pix[bi+1], base.Pix[bi+2], 255}
			card := blendPremul(bg, over)
			vi := (y*bw + x) * 4
			d := 0
			var prev [4]byte
			if sameSize && oldPix[vi+3] >= 80 {
				prev = blendPremul(card, [4]float32{
					float32(oldPix[vi]) / 255, float32(oldPix[vi+1]) / 255,
					float32(oldPix[vi+2]) / 255, float32(oldPix[vi+3]) / 255,
				})
				d = dist3([3]byte{card[0], card[1], card[2]}, [3]byte{prev[0], prev[1], prev[2]})
			}
			if !found || d > best {
				found = true
				best = d
				sx, sy = gx, gy
				cardPx = [3]byte{card[0], card[1], card[2]}
				oldPx = [3]byte{prev[0], prev[1], prev[2]}
			}
		}
	}
	if !found {
		t.Fatal("no veil pixel clear of the composer")
	}
	assertVeilPresented(t, tt, v)
	img := tt.Image()
	if sx < 0 || sy < 0 || sx >= img.Bounds().Dx() || sy >= img.Bounds().Dy() {
		t.Fatalf("veil sample (%d,%d) outside the frame %v", sx, sy, img.Bounds())
	}
	got := img.RGBAAt(sx, sy)
	have := [3]byte{got.R, got.G, got.B}
	if dist3(have, cardPx) > 3 {
		t.Fatalf("frame (%d,%d) %v is not the new veil's card %v", sx, sy, got, cardPx)
	}
	if sameSize {
		if best < 4 {
			t.Fatalf("previous veil would not show against the new card (delta %d)", best)
		}
		if dist3(have, oldPx) <= dist3(have, cardPx) {
			t.Fatalf("frame %v still matches the previous veil %v, not %v", got, oldPx, cardPx)
		}
	}
}

// assertVeilPresented checks the image id blitVeil handed to the painter
// on a frame. Empty card pixels match the card with or without the veil,
// so the drawn texture id is what shows the new bitmap was presented.
func assertVeilPresented(t *testing.T, tt *ui.Tester, v *veilEntry) {
	t.Helper()
	if themeDebugOn.Load() {
		t.Fatal("theme debug was already on")
	}
	path := filepath.Join(t.TempDir(), "theme-debug.log")
	themeDebugOverride.Store(path)
	var buf bytes.Buffer
	themeDebugMu.Lock()
	prevErr := themeDebugErr
	themeDebugErr = &buf
	themeDebugLogged = nil
	themeDebugMu.Unlock()
	themeDebugOn.Store(true)
	rememberBitmap(v.bm, v.gen)
	defer func() {
		themeDebugOn.Store(false)
		themeDebugOverride.Store("")
		themeDebugMu.Lock()
		themeDebugErr = prevErr
		if themeDebugFile != nil {
			themeDebugFile.Close()
			themeDebugFile = nil
			themeDebugOpened = ""
		}
		themeDebugMu.Unlock()
	}()
	tt.Frame()
	w, h := v.bm.Size()
	line := fmt.Sprintf("veil presented theme=")
	tail := fmt.Sprintf("size=%dx%d gen=%d image=%d", w, h, v.gen, bitmapImageID(v.bm))
	body := buf.Bytes()
	if !bytes.Contains(body, []byte(line)) || !bytes.Contains(body, []byte(tail)) {
		t.Fatalf("presented frame did not draw the veil\nwant %s … %s\n%s", line, tail, buf.String())
	}
}

func finishSwitch(t *testing.T, tt *ui.Tester, a *App) {
	t.Helper()
	awaitGradients()
	for i := 0; i < 50 && (a.themeMoving() || !backdropAdopted(a)); i++ {
		awaitGradients()
		awaitVeils()
		prev := tt.Now()
		tt.Advance(16 * time.Millisecond)
		if !tt.Now().After(prev) {
			tt.Frame()
		}
	}
	awaitVeils()
	tt.Frame()
	if a.themeMoving() || !backdropAdopted(a) {
		t.Fatalf("switch did not land (moving %v adopted %v veil %v)", a.themeMoving(), backdropAdopted(a), a.veilStandIn)
	}
}

func checkCachedSwitch(t *testing.T, kind string) {
	t.Helper()
	resetGradientCache()
	a := revisionChat(t)
	a.instant = false
	a.data.Appearance = appearanceLight
	if kind == "system" {
		a.data.Appearance = appearanceSystem
	}
	const winW, winH = 640, 400
	tt := ui.NewTester(a.view, winW, winH)
	settleVeil(t, tt)

	bg := shownGradient(a.winPW, a.winPH)
	bgID := bitmapImageID(bg)
	top := tt.Image().RGBAAt(winW-8, 2)
	beforePx := [3]byte{top.R, top.G, top.B}
	veil := liveVeil(veilFade)
	if veil == nil {
		t.Fatal("fade veil was not cached")
	}
	veilID, _, _, _, veilPix := bitmapIdentity(t, veil.bm)
	oldVeil := append([]byte(nil), veilPix...)
	oldPreview := previewBitmap(a.lookTarget, a.winPW, a.winPH)

	release := holdDitheredBuilds()
	defer release()
	switch kind {
	case "theme":
		a.setTheme(themes.ThemeTide, tt.Now())
		runOneFrame(tt)
	case "appearance":
		a.setAppearance(appearanceDark, tt.Now())
		runOneFrame(tt)
	case "system":
		tt.SetDark(true)
	default:
		t.Fatalf("kind %s", kind)
	}
	for i := 0; i < 12; i++ {
		tt.Advance(16 * time.Millisecond)
	}
	assertPreviewOnScreen(t, tt, a, beforePx)
	assertTextureReplaced(t, oldPreview, a.previewBM)

	release()
	finishSwitch(t, tt, a)
	if bitmapImageID(shownGradient(a.winPW, a.winPH)) == bgID {
		t.Fatal("background kept the previous image")
	}
	assertPresentedTheme(t, tt, a, beforePx)
	assertVeilReplaced(t, tt, veilID, oldVeil)
}

func TestCachedBitmapsUpdate(t *testing.T) {
	t.Run("theme", func(t *testing.T) { checkCachedSwitch(t, "theme") })
	t.Run("appearance", func(t *testing.T) { checkCachedSwitch(t, "appearance") })
	t.Run("system", func(t *testing.T) { checkCachedSwitch(t, "system") })
}

func TestCachedBitmapsFollowResize(t *testing.T) {
	resetGradientCache()
	a := revisionChat(t)
	a.instant = false
	a.data.Appearance = appearanceLight
	tt := ui.NewTester(a.view, 640, 400)
	settleVeil(t, tt)
	look := a.lookTarget
	oldBG := bitmapImageID(shownGradient(a.winPW, a.winPH))
	veil := liveVeil(veilFade)
	if veil == nil {
		t.Fatal("fade veil was not cached")
	}
	oldVeilID, _, _, _, veilPix := bitmapIdentity(t, veil.bm)
	oldVeil := append([]byte(nil), veilPix...)
	oldPreview := previewBitmap(look, a.winPW, a.winPH)
	top := tt.Image().RGBAAt(tt.Image().Bounds().Dx()-8, 2)
	beforePx := [3]byte{top.R, top.G, top.B}

	tt.SetSize(900, 560)
	if got := bitmapImageID(shownGradient(a.winPW, a.winPH)); got != oldBG {
		t.Fatalf("live resize replaced the background before it held still: %d -> %d", oldBG, got)
	}
	var resized *ui.Bitmap
	for i := 0; i < 6; i++ {
		tt.Advance(gradSettle)
		awaitGradients()
		awaitVeils()
		tt.Frame()
		resized = shownGradient(a.winPW, a.winPH)
		if resized == nil {
			continue
		}
		_, _, w, h, _ := bitmapIdentity(t, resized)
		if bitmapImageID(resized) != oldBG && w == a.winPW && h == a.winPH {
			break
		}
	}
	if resized == nil || bitmapImageID(resized) == oldBG {
		t.Fatal("settled resize kept the stretched background")
	}
	for i := 0; i < 4; i++ {
		awaitVeils()
		tt.Frame()
		if v := liveVeil(veilFade); v != nil && bitmapImageID(v.bm) != oldVeilID {
			break
		}
	}
	_, _, w, h, _ := bitmapIdentity(t, resized)
	if w != a.winPW || h != a.winPH {
		t.Fatalf("background %dx%d, window %dx%d", w, h, a.winPW, a.winPH)
	}
	assertPresentedTheme(t, tt, a, beforePx)
	assertVeilReplaced(t, tt, oldVeilID, oldVeil)

	newPrev := previewBitmap(look, a.winPW, a.winPH)
	if bitmapImageID(newPrev) == bitmapImageID(oldPreview) {
		t.Fatal("resize kept the preview image")
	}
	pw, ph := newPrev.Size()
	wantW, wantH := previewSize(a.winPW, a.winPH)
	if pw != wantW || ph != wantH {
		t.Fatalf("preview %dx%d, want the 1/%d size %dx%d", pw, ph, previewDiv, wantW, wantH)
	}
	assertTextureReplaced(t, oldPreview, newPrev)

	// The preview of a theme switch at the new size is that new image's
	// sibling for the other theme, not the bitmap from before the resize.
	settled := tt.Image().RGBAAt(tt.Image().Bounds().Dx()-8, 2)
	settledPx := [3]byte{settled.R, settled.G, settled.B}
	release := holdDitheredBuilds()
	defer release()
	a.setTheme(themes.ThemeTide, tt.Now())
	runOneFrame(tt)
	for i := 0; i < 12; i++ {
		tt.Advance(16 * time.Millisecond)
	}
	if bitmapImageID(a.previewBM) == bitmapImageID(oldPreview) || bitmapImageID(a.previewBM) == bitmapImageID(newPrev) {
		t.Fatal("theme switch after resize reused a preview image")
	}
	assertPreviewOnScreen(t, tt, a, settledPx)
}

func TestVeilAndPreviewOwnTheirPixels(t *testing.T) {
	img := renderGradient(themeLook(themes.ThemeWarm, false), 16, 8, false)
	bm := bitmapFiltered(img)
	_, _, _, _, pix := bitmapIdentity(t, bm)
	kept := pix[0]
	img.Pix[0] ^= 0x5a
	if pix[0] != kept {
		t.Fatal("preview shares the worker buffer")
	}
	rgba := image.NewRGBA(image.Rect(0, 0, 8, 4))
	rgba.Pix[0] = 10
	veil := ownBitmapRGBA(rgba, true)
	_, _, _, _, vp := bitmapIdentity(t, veil)
	rgba.Pix[0] = 200
	if vp[0] != 10 {
		t.Fatal("veil shares the worker buffer")
	}
	if bitmapImageID(bm) == 0 || bitmapImageID(bm) == bitmapImageID(veil) {
		t.Fatal("preview and veil did not get distinct image ids")
	}
}

func TestThemeDebugLog(t *testing.T) {
	if themeDebugOn.Load() {
		t.Fatal("COVE_DEBUG_THEME is set; this suite expects the log off")
	}
	writes := themeDebugWrites.Load()
	themeLog(themeEventSwitch, 4, 4, 1, 1)
	themeLogDrawn(themeEventVeil, nil)
	themeLogBackdrop(4, 4, 1, nil, 1, nil, 1, nil)
	if themeDebugWrites.Load() != writes {
		t.Fatal("theme log wrote a line while it was off")
	}
	if !themeDebugDefaultValue("1") || !themeDebugDefaultValue(" true\n") || themeDebugDefaultValue("0") || themeDebugDefaultValue("") {
		t.Fatal("defaults bool parse")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "Library", "Logs", "Cove", "theme-debug.log")
	themeDebugOverride.Store(path)
	var buf bytes.Buffer
	themeDebugMu.Lock()
	themeDebugErr = &buf
	themeDebugName = "tide"
	themeDebugMode = "dark"
	themeDebugLogged = nil
	themeDebugSeen = nil
	themeDebugMu.Unlock()
	themeDebugOn.Store(true)
	defer func() {
		themeDebugOn.Store(false)
		themeDebugOverride.Store("")
		themeDebugMu.Lock()
		themeDebugErr = nil
		themeDebugName = ""
		themeDebugMode = ""
		themeDebugLogged = nil
		themeDebugSeen = nil
		if themeDebugFile != nil {
			themeDebugFile.Close()
			themeDebugFile = nil
			themeDebugOpened = ""
		}
		themeDebugMu.Unlock()
	}()

	themeLog(themeEventSwitch, 640, 400, 0, 0)
	themeLog(themeEventBuildStart, 640, 400, 0, 0)
	img := renderGradient(themeLook(themes.ThemeTide, true), 8, 4, true)
	bm := bitmapFromNRGBA(img)
	rememberBitmap(bm, 7)
	themeLogBitmap(themeEventBuildDone, bm, 7)
	themeLogDrawn(themeEventPreview, bm)
	themeLogDrawn(themeEventPreview, bm)
	themeLogDrawn(themeEventFull, bm)
	themeLogDrawn(themeEventVeil, bm)
	themeLogBitmap(themeEventResize, bm, 9)

	text := buf.String()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != text {
		t.Fatalf("log file differs from stderr\nfile:\n%s\nstderr:\n%s", body, text)
	}
	id := bitmapImageID(bm)
	for _, ev := range []string{themeEventSwitch, themeEventBuildStart, themeEventBuildDone, themeEventPreview, themeEventFull, themeEventVeil, themeEventResize} {
		if !bytes.Contains(body, []byte(ev)) {
			t.Fatalf("log missing %q\n%s", ev, text)
		}
	}
	if bytes.Count(body, []byte(themeEventPreview)) != 1 {
		t.Fatalf("preview logged more than once\n%s", text)
	}
	want := fmt.Sprintf("theme=tide appearance=dark size=8x4 gen=7 image=%d", id)
	if !bytes.Contains(body, []byte(want)) {
		t.Fatalf("log missing %q\n%s", want, text)
	}
	if !bytes.Contains(body, []byte("gen=9")) {
		t.Fatalf("resize generation missing\n%s", text)
	}

	themeLogBackdrop(8, 4, 1, bm, 0.5, bm, 0, nil)
	themeLogBackdrop(8, 4, 1, bm, 0.5, bm, 0, nil)
	body, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	first := fmt.Sprintf("frame from=1.000/%d/upload=1 preview=0.500/%d/upload=0 full=0.000/0/upload=0", id, id)
	second := fmt.Sprintf("frame from=1.000/%d/upload=0 preview=0.500/%d/upload=0 full=0.000/0/upload=0", id, id)
	if !bytes.Contains(body, []byte(first)) || !bytes.Contains(body, []byte(second)) {
		t.Fatalf("frame line missing\nwant %s\nand %s\n%s", first, second, body)
	}
	if string(body) != buf.String() {
		t.Fatal("frame lines diverged from stderr")
	}

	themeDebugOn.Store(false)
	n := themeDebugWrites.Load()
	themeLog(themeEventSwitch, 1, 1, 0, 0)
	themeLogDrawn(themeEventFull, bm)
	themeLogBackdrop(8, 4, 1, bm, 1, bm, 1, bm)
	if themeDebugWrites.Load() != n {
		t.Fatal("log kept writing after it was turned off")
	}
}
