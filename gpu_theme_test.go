package main

import (
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"cove/themes"

	"github.com/egoist/mygo/ui"
)

// bitmapIdentity reads the scene image a Bitmap draws. The GPU cache keys
// a texture by that image's ID and uploads again only when its version
// changes. Bitmap's first field is the *scene.Image. On a 64-bit machine
// the image is id uint64, version uint64, W int, H int, then the Pix
// slice, which is the layout in mygo v0.5.5 internal/scene.
func bitmapIdentity(t *testing.T, b *ui.Bitmap) (id, ver uint64, w, h int, pix []byte) {
	t.Helper()
	if b == nil {
		t.Fatal("nil bitmap")
	}
	img := *(*unsafe.Pointer)(unsafe.Pointer(b))
	if img == nil {
		t.Fatal("bitmap has no scene image")
	}
	id = *(*uint64)(img)
	ver = *(*uint64)(unsafe.Add(img, 8))
	w = *(*int)(unsafe.Add(img, 16))
	h = *(*int)(unsafe.Add(img, 24))
	pix = *(*[]byte)(unsafe.Add(img, 32))
	if id == 0 || w < 1 || h < 1 || len(pix) != w*h*4 {
		t.Fatalf("scene image layout id %d version %d %dx%d pix %d", id, ver, w, h, len(pix))
	}
	return id, ver, w, h, pix
}

func sliceData(b []byte) uintptr {
	if len(b) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&b[0]))
}

// gpuImageCache is the imageTexture contract from internal/gpu/metal and
// internal/gpu/gl. A new image ID allocates a new texture and copies Pix.
// The same ID and size re-uploads only when Version changes. The same ID
// and version keeps the bytes already uploaded, even if the caller has
// since rewritten the buffer the image points at.
type gpuSlot struct {
	tex     uint64
	ver     uint64
	w, h    int
	pix     []byte
	uploads int
}

type gpuImageCache struct {
	slots map[uint64]*gpuSlot
	next  uint64
}

func (c *gpuImageCache) imageTexture(id, ver uint64, w, h int, pix []byte) uint64 {
	if c.slots == nil {
		c.slots = map[uint64]*gpuSlot{}
	}
	t := c.slots[id]
	if t == nil || t.w != w || t.h != h {
		c.next++
		t = &gpuSlot{tex: c.next, ver: ver, w: w, h: h, pix: append([]byte(nil), pix...)}
		t.uploads++
		c.slots[id] = t
		return t.tex
	}
	if t.ver != ver {
		t.pix = append([]byte(nil), pix...)
		t.ver = ver
		t.uploads++
	}
	return t.tex
}

func TestThemeBitmapIdentity(t *testing.T) {
	const w, h = 32, 24
	imgA := renderGradient(themeLook(themes.ThemeWarm, false), w, h, true)
	imgB := renderGradient(themeLook(themes.ThemeTide, false), w, h, true)
	bmA := bitmapFromNRGBA(imgA)
	bmB := bitmapFromNRGBA(imgB)
	idA, verA, wa, ha, pixA := bitmapIdentity(t, bmA)
	idB, verB, wb, hb, pixB := bitmapIdentity(t, bmB)
	if wa != w || ha != h || wb != w || hb != h {
		t.Fatalf("image size %dx%d and %dx%d, want %dx%d", wa, ha, wb, hb, w, h)
	}
	if idA == idB {
		t.Fatalf("two generations share image id %d", idA)
	}
	if verA != 1 || verB != 1 {
		t.Fatalf("new images should start at version 1, got %d and %d", verA, verB)
	}
	if sliceData(pixA) == sliceData(pixB) || sliceData(pixA) == sliceData(imgA.Pix) || sliceData(pixB) == sliceData(imgB.Pix) {
		t.Fatal("bitmap shares a pixel buffer with another generation or with the worker image")
	}
	kept := pixA[0]
	imgA.Pix[0] ^= 0x5a
	if pixA[0] != kept {
		t.Fatal("mutating the worker buffer changed the published bitmap")
	}

	bmA2 := bitmapFromNRGBA(imgA)
	idA2, _, _, _, pixA2 := bitmapIdentity(t, bmA2)
	if idA2 == idA || sliceData(pixA2) == sliceData(pixA) {
		t.Fatalf("republishing reused image %d or its buffer", idA)
	}

	var cache gpuImageCache
	texA := cache.imageTexture(idA, verA, w, h, pixA)
	uploads := cache.slots[idA].uploads
	// Same ID and version must not refresh, even when the bytes handed
	// in are a later generation's.
	texSame := cache.imageTexture(idA, verA, w, h, pixA2)
	if texSame != texA || cache.slots[idA].uploads != uploads {
		t.Fatal("same image id and version uploaded the texture again")
	}
	if cache.slots[idA].pix[0] != kept {
		t.Fatal("cached texture changed without a new image id or version")
	}
	texB := cache.imageTexture(idB, verB, w, h, pixB)
	if texB == texA {
		t.Fatal("new image id reused the background texture")
	}
	if cache.slots[idB].pix[0] != pixB[0] || len(cache.slots[idB].pix) != len(pixB) {
		t.Fatal("cached texture is not the new bitmap")
	}
	for i := range pixB {
		if cache.slots[idB].pix[i] != pixB[i] {
			t.Fatalf("cached texture byte %d is %#x, bitmap %#x", i, cache.slots[idB].pix[i], pixB[i])
		}
	}
}

func shownGradient(w, h int) *ui.Bitmap {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	st := sizes[[2]int{w, h}]
	if st == nil || st.shown == nil {
		return nil
	}
	return st.shown.bm
}

func assertPresentedTheme(t *testing.T, tt *ui.Tester, a *App, before [3]byte) (id uint64) {
	t.Helper()
	img := tt.Image()
	b := img.Bounds()
	px, py := b.Dx()-8, 2
	got := img.RGBAAt(px, py)
	want := windowPixel(a.lookTarget, 1, float32(b.Dx()), float32(b.Dy()), px, py)
	if got.R != want.R || got.G != want.G || got.B != want.B {
		t.Fatalf("presented (%d,%d) = %v, theme bitmap %v", px, py, got, want)
	}
	if got.R == before[0] && got.G == before[1] && got.B == before[2] {
		t.Fatalf("presented pixel stayed %v", got)
	}
	bm := shownGradient(a.winPW, a.winPH)
	id, _, bw, bh, pix := bitmapIdentity(t, bm)
	if bw != a.winPW || bh != a.winPH {
		t.Fatalf("shown bitmap %dx%d, window %dx%d", bw, bh, a.winPW, a.winPH)
	}
	// The texture the renderer would keep for this image is a new upload
	// of these bytes. The presented pixel is that bitmap, not a stale copy.
	i := pixOffset(bw, px, py)
	if i+2 >= len(pix) || pix[i] != got.R || pix[i+1] != got.G || pix[i+2] != got.B {
		t.Fatalf("shown image pixel %d = %v, presented %v", i, pix[i:i+3], got)
	}
	return id
}

func pixOffset(w, x, y int) int {
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if x >= w {
		x = w - 1
	}
	return (y*w + x) * 4
}

func TestThemeSwitchLandsWithoutInput(t *testing.T) {
	resetGradientCache()
	a := revisionChat(t)
	a.instant = false
	a.data.Appearance = appearanceLight
	const winW, winH = 640, 400
	tt := ui.NewTester(a.view, winW, winH)
	settleVeil(t, tt)

	beforeBM := shownGradient(a.winPW, a.winPH)
	idBefore, _, _, _, _ := bitmapIdentity(t, beforeBM)
	top := tt.Image().RGBAAt(winW-8, 2)
	beforePx := [3]byte{top.R, top.G, top.B}
	warmMark := a.look.ThemeMark

	var n atomic.Int32
	themeInvalidateHook.Store(func() { n.Add(1) })
	defer themeInvalidateHook.Store(func() {})

	// The switch is the settings action: save, then ask for a frame.
	// Nothing points, clicks, or scrolls after this.
	a.setTheme(themes.ThemeTide, tt.Now())
	if n.Load() == 0 {
		t.Fatal("setTheme saved the theme without invalidating a window")
	}
	if a.data.Theme != themes.ThemeTide {
		t.Fatalf("theme not saved: %s", a.data.Theme)
	}
	if a.look.ThemeMark != warmMark {
		t.Fatal("live palette changed before the invalidate frame")
	}

	runOneFrame(tt)
	if !a.fadeLive || !a.blendStart.Equal(tt.Now()) {
		t.Fatalf("clock did not start on the invalidate frame: live %v blend %v now %v", a.fadeLive, a.blendStart, tt.Now())
	}
	if a.look.SelectionRow != a.blendFrom.SelectionRow {
		t.Fatal("palette waited for the bitmap")
	}
	tt.Advance(16 * time.Millisecond)
	if a.look.SelectionRow == a.blendFrom.SelectionRow {
		t.Fatal("controls did not move on the frame after the switch")
	}

	for i := 0; i < 40 && (a.themeMoving() || !backdropAdopted(a)); i++ {
		awaitGradients()
		awaitVeils()
		prev := tt.Now()
		tt.Advance(16 * time.Millisecond)
		if !tt.Now().After(prev) {
			break
		}
	}
	if a.themeMoving() || !backdropAdopted(a) {
		t.Fatalf("cold switch did not land (moving %v adopted %v)", a.themeMoving(), backdropAdopted(a))
	}
	idTide := assertPresentedTheme(t, tt, a, beforePx)
	if idTide == idBefore {
		t.Fatalf("presented background kept image id %d", idTide)
	}
	var cache gpuImageCache
	_, verBefore, bw, bh, pixBefore := bitmapIdentity(t, beforeBM)
	_, verTide, tw, th, pixTide := bitmapIdentity(t, shownGradient(a.winPW, a.winPH))
	texBefore := cache.imageTexture(idBefore, verBefore, bw, bh, pixBefore)
	texTide := cache.imageTexture(idTide, verTide, tw, th, pixTide)
	if texTide == texBefore {
		t.Fatal("renderer kept the previous background texture")
	}
	if cache.slots[idTide].pix[0] != pixTide[0] {
		t.Fatal("uploaded texture is not the tide bitmap")
	}

	// The warm bitmap is still cached, so the worker must not be what
	// lands the switch. The invalidate posted by setTheme is.
	shown := tt.Image().RGBAAt(winW-8, 2)
	tidePx := [3]byte{shown.R, shown.G, shown.B}
	pub := gradPublished.Load()
	hooks := n.Load()
	a.setTheme(themes.ThemeWarm, tt.Now())
	if n.Load() == hooks {
		t.Fatal("cached theme switch did not invalidate")
	}
	if gradPublished.Load() != pub {
		t.Fatalf("cached switch published a bitmap (%d -> %d); the test would pass on a worker wake alone", pub, gradPublished.Load())
	}
	if a.look.ThemeMark == warmMark {
		t.Fatal("live theme changed before the cache-hit frame")
	}
	runOneFrame(tt)
	if a.look.SelectionRow != a.blendFrom.SelectionRow {
		t.Fatal("cache-hit switch held the palette")
	}
	tt.Advance(16 * time.Millisecond)
	if a.look.SelectionRow == a.blendFrom.SelectionRow {
		t.Fatal("cache-hit controls did not move on the next frame")
	}
	for i := 0; i < 40 && (a.themeMoving() || !backdropAdopted(a)); i++ {
		awaitGradients()
		awaitVeils()
		prev := tt.Now()
		tt.Advance(16 * time.Millisecond)
		if !tt.Now().After(prev) {
			break
		}
	}
	if a.themeMoving() || !backdropAdopted(a) {
		t.Fatalf("cached switch did not land (moving %v adopted %v)", a.themeMoving(), backdropAdopted(a))
	}
	_, verWarm, ww, wh, pixWarm := bitmapIdentity(t, shownGradient(a.winPW, a.winPH))
	idWarm := assertPresentedTheme(t, tt, a, tidePx)
	if idWarm == idTide {
		t.Fatalf("cache-hit switch presented image id %d again", idWarm)
	}
	texWarm := cache.imageTexture(idWarm, verWarm, ww, wh, pixWarm)
	if texWarm == texTide {
		t.Fatal("cache-hit switch reused the tide texture")
	}
}

func TestAppearanceSwitchInvalidates(t *testing.T) {
	resetGradientCache()
	a := revisionChat(t)
	a.instant = false
	a.data.Appearance = appearanceLight
	tt := ui.NewTester(a.view, 640, 400)
	settleVeil(t, tt)

	var n atomic.Int32
	themeInvalidateHook.Store(func() { n.Add(1) })
	defer themeInvalidateHook.Store(func() {})
	a.setAppearance(appearanceDark, tt.Now())
	if n.Load() == 0 {
		t.Fatal("setAppearance saved the mode without invalidating a window")
	}
	if a.look.Dark {
		t.Fatal("live appearance changed before the invalidate frame")
	}
	runOneFrame(tt)
	if !a.lookTarget.Dark || !a.fadeLive {
		t.Fatalf("appearance did not start on the invalidate frame: dark %v live %v", a.lookTarget.Dark, a.fadeLive)
	}
	if a.look.SelectionRow != a.blendFrom.SelectionRow {
		t.Fatal("appearance switch held the palette for the bitmap")
	}
}
