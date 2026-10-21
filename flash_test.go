package main

import (
	"bytes"
	"fmt"
	"image"
	"path/filepath"
	"testing"
	"time"

	"cove/themes"

	"github.com/egoist/mygo/ui"
)

// crossfadeSlack is how far a title-bar pixel may move back along the
// old→new segment. Dither and the bilinear preview wobble by about one
// code. The broken crossfade puts a quarter of the old theme back.
const crossfadeSlack = 0.15

// fadeWin selects the surface and the appearance the switch starts in.
// The zero value is the main window in light appearance.
type fadeWin struct {
	settings bool
	dark     bool
}

func TestThemeCrossfadeNoFlash(t *testing.T) {
	t.Run("theme-miss", func(t *testing.T) {
		crossfadeSwitch(t, themes.ThemeTide, false, false)
	})
	t.Run("theme-hit", func(t *testing.T) {
		crossfadeSwitch(t, themes.ThemeTide, false, true)
	})
	t.Run("appearance-miss", func(t *testing.T) {
		crossfadeSwitch(t, "", true, false)
	})
	t.Run("appearance-hit", func(t *testing.T) {
		crossfadeSwitch(t, "", true, true)
	})
	t.Run("rapid", func(t *testing.T) {
		crossfadeRapid(t)
	})

	// The same monotonic walk on the main window, starting in dark.
	mainDark := fadeWin{dark: true}
	t.Run("main-dark-theme-miss", func(t *testing.T) {
		crossfadeOn(t, themes.ThemeTide, false, false, mainDark)
	})
	t.Run("main-dark-theme-hit", func(t *testing.T) {
		crossfadeOn(t, themes.ThemeTide, false, true, mainDark)
	})
	t.Run("main-dark-to-light-miss", func(t *testing.T) {
		crossfadeOn(t, "", false, false, mainDark)
	})
	t.Run("main-dark-to-light-hit", func(t *testing.T) {
		crossfadeOn(t, "", false, true, mainDark)
	})
	t.Run("main-dark-rapid", func(t *testing.T) {
		crossfadeRapidOn(t, mainDark)
	})

	// Settings is its own window and paints the same backdrop.
	settingsLight := fadeWin{settings: true}
	settingsDark := fadeWin{settings: true, dark: true}
	t.Run("settings-light-theme-miss", func(t *testing.T) {
		crossfadeOn(t, themes.ThemeTide, false, false, settingsLight)
	})
	t.Run("settings-light-theme-hit", func(t *testing.T) {
		crossfadeOn(t, themes.ThemeTide, false, true, settingsLight)
	})
	t.Run("settings-dark-theme-miss", func(t *testing.T) {
		crossfadeOn(t, themes.ThemeTide, false, false, settingsDark)
	})
	t.Run("settings-dark-theme-hit", func(t *testing.T) {
		crossfadeOn(t, themes.ThemeTide, false, true, settingsDark)
	})
	t.Run("settings-light-to-dark-miss", func(t *testing.T) {
		crossfadeOn(t, "", true, false, settingsLight)
	})
	t.Run("settings-light-to-dark-hit", func(t *testing.T) {
		crossfadeOn(t, "", true, true, settingsLight)
	})
	t.Run("settings-dark-to-light-miss", func(t *testing.T) {
		crossfadeOn(t, "", false, false, settingsDark)
	})
	t.Run("settings-dark-to-light-hit", func(t *testing.T) {
		crossfadeOn(t, "", false, true, settingsDark)
	})
	t.Run("settings-light-rapid", func(t *testing.T) {
		crossfadeRapidOn(t, settingsLight)
	})
	t.Run("settings-dark-rapid", func(t *testing.T) {
		crossfadeRapidOn(t, settingsDark)
	})

	// Cache hit is the "*-hit" cases. Cache miss that publishes after the
	// 150ms clock is the "*-miss" cases. These publish the full-resolution
	// bitmap while that clock is still running.
	t.Run("main-light-publish-mid", func(t *testing.T) {
		crossfadeAt(t, themes.ThemeTide, false, false, fadeWin{}, 0.5)
	})
	t.Run("main-dark-publish-mid", func(t *testing.T) {
		crossfadeAt(t, themes.ThemeTide, false, false, mainDark, 0.5)
	})
	t.Run("settings-light-publish-mid", func(t *testing.T) {
		crossfadeAt(t, themes.ThemeTide, false, false, settingsLight, 0.5)
	})
	t.Run("settings-dark-publish-mid", func(t *testing.T) {
		crossfadeAt(t, themes.ThemeTide, false, false, settingsDark, 0.5)
	})
	t.Run("main-light-to-dark-publish-mid", func(t *testing.T) {
		crossfadeAt(t, "", true, false, fadeWin{}, 0.5)
	})
	t.Run("main-dark-to-light-publish-mid", func(t *testing.T) {
		crossfadeAt(t, "", false, false, mainDark, 0.5)
	})
	t.Run("settings-light-to-dark-publish-mid", func(t *testing.T) {
		crossfadeAt(t, "", true, false, settingsLight, 0.5)
	})
	t.Run("settings-dark-to-light-publish-mid", func(t *testing.T) {
		crossfadeAt(t, "", false, false, settingsDark, 0.5)
	})
}

func crossfadeSwitch(t *testing.T, theme string, dark, hit bool) {
	t.Helper()
	crossfadeOn(t, theme, dark, hit, fadeWin{})
}

func crossfadeOn(t *testing.T, theme string, toDark, hit bool, win fadeWin) {
	t.Helper()
	// A miss holds the dithered build until the 150ms clock has ended.
	crossfadeAt(t, theme, toDark, hit, win, 1)
}

func crossfadeAt(t *testing.T, theme string, toDark, hit bool, win fadeWin, publishAt float32) {
	t.Helper()
	a, tt, x, y := newCrossfadeWindow(t, win)
	startDark := win.dark
	targetDark := startDark
	if theme == "" {
		targetDark = toDark
	}
	targetLook := themeLook(a.data.Theme, targetDark)
	backTheme := a.data.Theme
	if theme != "" {
		targetLook = themeLook(theme, targetDark)
	}
	var target [3]byte
	if hit {
		applyCrossfade(a, tt, theme, toDark)
		runOneFrame(tt)
		finishSwitch(t, tt, a)
		target = titlePixel(tt, x, y)
		if theme != "" {
			applyCrossfade(a, tt, backTheme, startDark)
		} else {
			applyCrossfade(a, tt, "", startDark)
		}
		runOneFrame(tt)
		finishSwitch(t, tt, a)
	} else {
		target = ditherTitle(t, targetLook, a.winPW, a.winPH, x, y)
	}

	var release func()
	if !hit {
		release = holdDitheredBuilds()
		defer release()
	}
	log := enableThemeDebug(t)
	cache := &gpuImageCache{}
	walk := newCrossfadeWalk(t, a, tt, log, cache, x, y, func() [3]byte { return target })

	applyCrossfade(a, tt, theme, toDark)
	runOneFrame(tt)
	walk.check()
	released := hit
	for i := 0; i < 50; i++ {
		if !hit && !released && clockU(tt.Now(), a.backdropFadeAt, motionTheme) >= publishAt {
			release()
			released = true
			awaitGradients()
			awaitVeils()
		}
		if released && i > 0 && !a.themeMoving() && backdropAdopted(a) {
			break
		}
		stepFrame(tt)
		walk.check()
	}
	if a.themeMoving() || !backdropAdopted(a) {
		t.Fatalf("switch did not land (moving %v adopted %v)", a.themeMoving(), backdropAdopted(a))
	}
	if !hit && !walk.sawPreview {
		t.Fatal("cold switch never drew the preview")
	}
	if hit && walk.sawPreview {
		t.Fatal("cache hit drew a preview")
	}
	if !walk.sawFull {
		t.Fatal("full-resolution layer was never drawn")
	}
	// A later paint, with no clock running, must not put the theme this
	// switch already left back on screen.
	runOneFrame(tt)
	walk.check()
	if d := dist3(titlePixel(tt, x, y), target); d > 2 {
		t.Fatalf("landed %v, dithered target %v (delta %d)", titlePixel(tt, x, y), target, d)
	}
}

func crossfadeRapid(t *testing.T) {
	t.Helper()
	crossfadeRapidOn(t, fadeWin{})
}

// TestThemeCrossfadeMidSwitch starts theme C while A→B is halfway through.
// C's first frame has to be the blend on screen. A later frame must not
// be A again, and must not be B's unblended preview.
func TestThemeCrossfadeMidSwitch(t *testing.T) {
	t.Run("main-light", func(t *testing.T) {
		crossfadeMid(t, fadeWin{})
	})
	t.Run("main-dark", func(t *testing.T) {
		crossfadeMid(t, fadeWin{dark: true})
	})
	t.Run("settings-light", func(t *testing.T) {
		crossfadeMid(t, fadeWin{settings: true})
	})
	t.Run("settings-dark", func(t *testing.T) {
		crossfadeMid(t, fadeWin{settings: true, dark: true})
	})
}

func crossfadeMid(t *testing.T, win fadeWin) {
	t.Helper()
	a, tt, x, y := newCrossfadeWindow(t, win)
	pixA := titlePixel(tt, x, y)
	// The dithered target is rendered on this thread. Doing it while builds
	// are held never returns.
	target := ditherTitle(t, themeLook(themes.ThemeSage, win.dark), a.winPW, a.winPH, x, y)
	release := holdDitheredBuilds()
	defer release()

	a.setTheme(themes.ThemeTide, tt.Now())
	runOneFrame(tt)
	for i := 0; i < 20 && clockU(tt.Now(), a.backdropFadeAt, motionTheme) < 0.5; i++ {
		stepFrame(tt)
	}
	u := clockU(tt.Now(), a.backdropFadeAt, motionTheme)
	if u < 0.45 || u > 0.65 {
		t.Fatalf("mid-fade clock %.3f, want about 0.5", u)
	}
	pixMid := titlePixel(tt, x, y)
	if a.previewBM == nil {
		t.Fatal("B preview missing at mid-fade")
	}
	pixB := previewColorAt(t, a.previewBM, a.winPW, a.winPH, x, y)
	if dist3(pixMid, pixA) < 4 {
		t.Fatalf("mid blend %v is still A %v", pixMid, pixA)
	}
	if dist3(pixMid, pixB) < 4 {
		t.Fatalf("mid blend %v is already unblended B %v", pixMid, pixB)
	}

	log := enableThemeDebug(t)
	cache := &gpuImageCache{}
	walk := newCrossfadeWalk(t, a, tt, log, cache, x, y, func() [3]byte { return target })

	now := tt.Now()
	a.setTheme(themes.ThemeSage, now)
	runOneFrame(tt)
	if clockU(tt.Now(), a.backdropFadeAt, motionTheme) != 0 {
		t.Fatalf("C clock started at %.3f", clockU(tt.Now(), a.backdropFadeAt, motionTheme))
	}
	got := titlePixel(tt, x, y)
	if d := dist3(got, pixMid); d > 2 {
		t.Fatalf("C started from %v, on-screen composite was %v (delta %d); A %v unblended B %v", got, pixMid, d, pixA, pixB)
	}
	walk.check()
	crossfadeMidPixel(t, got, pixA, pixB, pixMid)

	released := false
	for i := 0; i < 50; i++ {
		if !released && clockU(tt.Now(), a.backdropFadeAt, motionTheme) >= 1 {
			release()
			released = true
			awaitGradients()
			awaitVeils()
		}
		if released && i > 0 && !a.themeMoving() && backdropAdopted(a) {
			break
		}
		stepFrame(tt)
		walk.check()
		crossfadeMidPixel(t, titlePixel(tt, x, y), pixA, pixB, pixMid)
	}
	if a.themeMoving() || !backdropAdopted(a) {
		t.Fatalf("switch did not land (moving %v adopted %v)", a.themeMoving(), backdropAdopted(a))
	}
	if !walk.sawPreview {
		t.Fatal("mid switch never drew the preview")
	}
	if !walk.sawFull {
		t.Fatal("full-resolution layer was never drawn")
	}
	runOneFrame(tt)
	walk.check()
	crossfadeMidPixel(t, titlePixel(tt, x, y), pixA, pixB, pixMid)
	if d := dist3(titlePixel(tt, x, y), target); d > 2 {
		t.Fatalf("landed %v, dithered target %v (delta %d)", titlePixel(tt, x, y), target, d)
	}
}

func crossfadeMidPixel(t *testing.T, have, pixA, pixB, pixMid [3]byte) {
	t.Helper()
	if dist3(pixMid, pixA) > 2 && dist3(have, pixA) <= 2 {
		t.Fatalf("frame reverted to A %v (pixel %v, composite %v)", pixA, have, pixMid)
	}
	if dist3(pixMid, pixB) > 2 && dist3(have, pixB) <= 2 {
		t.Fatalf("frame showed B's unblended preview %v (pixel %v, composite %v)", pixB, have, pixMid)
	}
}

func crossfadeRapidOn(t *testing.T, win fadeWin) {
	t.Helper()
	a, tt, x, y := newCrossfadeWindow(t, win)
	ids := []string{themes.ThemeTide, themes.ThemeMist, themes.ThemeSage, themes.ThemeLavender}
	want := map[string][3]byte{}
	for _, id := range ids {
		want[id] = ditherTitle(t, themeLook(id, win.dark), a.winPW, a.winPH, x, y)
	}
	release := holdDitheredBuilds()
	defer release()
	log := enableThemeDebug(t)
	cache := &gpuImageCache{}
	walk := newCrossfadeWalk(t, a, tt, log, cache, x, y, func() [3]byte {
		return want[a.data.Theme]
	})

	for i, id := range ids {
		a.setTheme(id, tt.Now())
		runOneFrame(tt)
		walk.check()
		if i == len(ids)-1 {
			break
		}
		stepFrame(tt)
		walk.check()
	}
	for i := 0; i < 20 && clockU(tt.Now(), a.backdropFadeAt, motionTheme) < 1; i++ {
		stepFrame(tt)
		walk.check()
	}
	if clockU(tt.Now(), a.backdropFadeAt, motionTheme) < 1 {
		t.Fatal("last switch did not reach the end of the preview clock while the build was held")
	}
	release()
	awaitGradients()
	awaitVeils()
	for i := 0; i < 40 && (a.themeMoving() || !backdropAdopted(a)); i++ {
		stepFrame(tt)
		walk.check()
	}
	if a.themeMoving() || !backdropAdopted(a) {
		t.Fatalf("rapid switch did not land (moving %v adopted %v)", a.themeMoving(), backdropAdopted(a))
	}
	if !walk.sawPreview || !walk.sawFull {
		t.Fatal("rapid switch did not draw both the preview and the full-resolution bitmap")
	}
	got := titlePixel(tt, x, y)
	if d := dist3(got, want[themes.ThemeLavender]); d > 2 {
		t.Fatalf("landed %v, lavender %v (delta %d)", got, want[themes.ThemeLavender], d)
	}
	runOneFrame(tt)
	walk.check()
	if walk.frames < 8 {
		t.Fatalf("rapid switch recorded %d frames", walk.frames)
	}
}

func newCrossfadeWindow(t *testing.T, win fadeWin) (*App, *ui.Tester, int, int) {
	t.Helper()
	resetGradientCache()
	resetVeilCache()
	a := revisionChat(t)
	a.instant = false
	a.data.Appearance = appearanceLight
	if win.dark {
		a.data.Appearance = appearanceDark
	}
	const winW, winH = 640, 400
	view := a.view
	if win.settings {
		a.settingsOpen = true
		a.settingsSection = "appearance"
		view = a.settingsWindowRoot
	}
	tt := ui.NewTester(view, winW, winH)
	if win.settings {
		// The settings surface does not paint a composer veil.
		settleBackdrop(t, tt, a)
	} else {
		settleVeil(t, tt)
	}
	if a.winPW != winW || a.winPH != winH {
		t.Fatalf("window pixels %dx%d, tester %dx%d", a.winPW, a.winPH, winW, winH)
	}
	x, y := winW-8, 2
	start := ditherTitle(t, themeLook(a.data.Theme, win.dark), a.winPW, a.winPH, x, y)
	if d := dist3(titlePixel(tt, x, y), start); d > 2 {
		surface := "main"
		if win.settings {
			surface = "settings"
		}
		t.Fatalf("%s sample (%d,%d) %v is not the backdrop %v", surface, x, y, titlePixel(tt, x, y), start)
	}
	return a, tt, x, y
}

// settleBackdrop waits until the window is showing its cached full-resolution
// gradient. It does not wait on the composer veil.
func settleBackdrop(t *testing.T, tt *ui.Tester, a *App) {
	t.Helper()
	for i := 0; i < 40; i++ {
		tt.Frame()
		awaitGradients()
		if backdropAdopted(a) && !a.themeMoving() && shownGradient(a.winPW, a.winPH) != nil {
			tt.Frame()
			return
		}
		prev := tt.Now()
		tt.Advance(16 * time.Millisecond)
		if !tt.Now().After(prev) {
			tt.Frame()
		}
	}
	t.Fatalf("backdrop never settled (moving %v adopted %v size %dx%d)", a.themeMoving(), backdropAdopted(a), a.winPW, a.winPH)
}

func applyCrossfade(a *App, tt *ui.Tester, theme string, dark bool) {
	if theme != "" {
		a.setTheme(theme, tt.Now())
		return
	}
	mode := appearanceLight
	if dark {
		mode = appearanceDark
	}
	a.setAppearance(mode, tt.Now())
}

func stepFrame(tt *ui.Tester) {
	prev := tt.Now()
	tt.Advance(16 * time.Millisecond)
	if !tt.Now().After(prev) {
		tt.Frame()
	}
}

func titlePixel(tt *ui.Tester, x, y int) [3]byte {
	c := tt.Image().RGBAAt(x, y)
	return [3]byte{c.R, c.G, c.B}
}

func ditherTitle(t *testing.T, look Theme, w, h, x, y int) [3]byte {
	t.Helper()
	img := renderGradient(look, w, h, true)
	if img.Rect.Dx() != w || img.Rect.Dy() != h {
		t.Fatalf("rendered %v, want %dx%d", img.Rect, w, h)
	}
	i := img.PixOffset(x, y)
	return [3]byte{img.Pix[i], img.Pix[i+1], img.Pix[i+2]}
}

type crossfadeWalk struct {
	t          *testing.T
	a          *App
	tt         *ui.Tester
	log        *themeDebugBuf
	cache      *gpuImageCache
	x, y       int
	target     func() [3]byte
	segKey     string
	oldPix     [3]byte
	peak       float64
	frames     int
	sawPreview bool
	sawFull    bool
	maxPreview float32
	maxFull    float32
	origin     [3]byte
	away       int
}

func newCrossfadeWalk(t *testing.T, a *App, tt *ui.Tester, log *themeDebugBuf, cache *gpuImageCache, x, y int, target func() [3]byte) *crossfadeWalk {
	t.Helper()
	return &crossfadeWalk{t: t, a: a, tt: tt, log: log, cache: cache, x: x, y: y, target: target}
}

func (w *crossfadeWalk) check() {
	w.t.Helper()
	w.frames++
	img := w.tt.Image()
	px := img.RGBAAt(w.x, w.y)
	if px.A != 255 {
		w.t.Fatalf("frame %d background alpha %d at (%d,%d)", w.frames, px.A, w.x, w.y)
	}
	have := [3]byte{px.R, px.G, px.B}
	sum := sum3(have)
	if sum < 40 || sum > 760 {
		w.t.Fatalf("frame %d background %v looks like the clear color", w.frames, have)
	}
	frames := w.log.takeFrames(w.a.winPW, w.a.winPH)
	if len(frames) == 0 {
		w.t.Fatalf("frame %d painted no background\n%s", w.frames, w.log.buf.String())
	}
	// Advance can paint every frame that came due in the step. The
	// picture on screen is the last of those. Earlier ones still uploaded
	// their textures; dropping them makes the last look like a first use.
	for _, earlier := range frames[:len(frames)-1] {
		w.absorbUploads(earlier, img.Bounds().Dx(), img.Bounds().Dy())
	}
	fr := frames[len(frames)-1]
	if fr.fromID != 0 && fr.fromA < 0.999 {
		w.t.Fatalf("frame %d outgoing layer alpha %.3f", w.frames, fr.fromA)
	}
	// A mid-fade switch freezes the on-screen blend as baseStack. That
	// stack is the opaque base; the log's from/preview/full slots are
	// only the layers added by this switch.
	covered := fr.fromA >= 0.999 || fr.previewA >= 0.999 || fr.fullA >= 0.999
	if !covered && !(w.a.baseDrawn && layersCover(w.a.baseStack)) {
		w.t.Fatalf("frame %d has no opaque background layer: %+v", w.frames, fr)
	}
	w.blendUploaded(fr, have, img.Bounds().Dx(), img.Bounds().Dy())

	target := w.target()
	// The outgoing bitmap's id is not part of the key. A publish that
	// swaps that bitmap back to the old theme has to fail this walk,
	// not start a fresh segment.
	key := fmt.Sprintf("%s/%v", w.a.data.Theme, w.a.lookTarget.Dark)
	fresh := key != w.segKey
	if fresh {
		w.segKey = key
		w.oldPix = have
		w.origin = have
		w.peak = 0
		w.away = 0
		w.maxPreview = 0
		w.maxFull = 0
	}

	if w.a.fadeLive {
		u := clockU(w.tt.Now(), w.a.backdropFadeAt, motionTheme)
		swapU := float32(0)
		if w.a.fullBM != nil && !w.a.swapAt.IsZero() {
			swapU = clockU(w.tt.Now(), w.a.swapAt, w.a.swapDur)
		}
		if fr.previewID != 0 {
			// The preview stays at the clock while a base is drawn, including
			// a frozen blend. Once that base is retired it stays opaque
			// under the full-res fade.
			want := u
			if fr.fromID == 0 && !w.a.baseDrawn {
				want = 1
			}
			if absF(fr.previewA-want) > 0.01 {
				w.t.Fatalf("frame %d preview alpha %.3f, want %.3f, clock %.3f, swap %.3f", w.frames, fr.previewA, want, u, swapU)
			}
		}
		if fr.fullID != 0 && absF(fr.fullA-swapU) > 0.01 {
			sole := fr.fullA > 0.999 && swapU == 0 && fr.fromID == 0 && fr.previewID == 0
			if !sole {
				w.t.Fatalf("frame %d full-res alpha %.3f, swap %.3f", w.frames, fr.fullA, swapU)
			}
		}
		if fr.previewID != 0 && fr.previewA > 0 {
			w.sawPreview = true
			if fr.previewA > w.maxPreview {
				w.maxPreview = fr.previewA
			}
		}
		if fr.fullID != 0 && fr.fullA > 0 {
			w.sawFull = true
			if fr.fullA > w.maxFull {
				w.maxFull = fr.fullA
			}
		}
		if w.sawPreview && fr.fullA < 0.999 && fr.previewA+0.02 < w.maxPreview {
			w.t.Fatalf("frame %d preview alpha %.3f fell from %.3f before full-res covered; the old theme shows through", w.frames, fr.previewA, w.maxPreview)
		}
		if w.sawPreview && w.maxPreview > 0.01 && fr.previewA < 0.01 && fr.fullA < 0.999 {
			w.t.Fatalf("frame %d dropped the preview while full-res was %.3f and drew the old theme", w.frames, fr.fullA)
		}
		if w.sawFull && fr.fullA+0.02 < w.maxFull {
			w.t.Fatalf("frame %d full-res alpha %.3f fell from %.3f back toward the old theme", w.frames, fr.fullA, w.maxFull)
		}
	}
	if fresh {
		return
	}
	away := dist3(have, w.origin)
	if away > w.away {
		w.away = away
	}
	if w.away > 18 && away < w.away-18 {
		w.t.Fatalf("frame %d went back to the old theme: pixel %v is %d from the start, was %d away, target %v", w.frames, have, away, w.away, target)
	}
	prog, okp := segmentProgress(w.origin, target, have)
	if !okp {
		return
	}
	if prog < w.peak-crossfadeSlack {
		w.t.Fatalf("frame %d went back to the old theme: progress %.3f fell from %.3f, pixel %v from %v toward %v", w.frames, prog, w.peak, have, w.origin, target)
	}
	if prog > w.peak {
		w.peak = prog
	}
	lo, hi := sum3(w.oldPix), sum3(target)
	if lo > hi {
		lo, hi = hi, lo
	}
	if sum < lo-18 || sum > hi+18 {
		w.t.Fatalf("frame %d brightness %d outside %d..%d, pixel %v", w.frames, sum, lo-18, hi+18, have)
	}
}

func (w *crossfadeWalk) blendUploaded(fr frameDraw, have [3]byte, winW, winH int) {
	w.t.Helper()
	dst := [3]float32{}
	cover := float32(0)
	if w.a.baseDrawn {
		for _, layer := range w.a.baseStack {
			if layer.bm == nil || layer.alpha <= 0 {
				continue
			}
			// The frozen stack is not in the frame log, so it has no
			// upload bit. The textures were already resident when this
			// switch started.
			w.compositeLayer(&dst, &cover, layer.bm, layer.alpha, -1, winW, winH)
		}
	}
	for _, layer := range fr.layers() {
		if layer.id == 0 || layer.alpha <= 0 {
			if layer.upload != 0 {
				w.t.Fatalf("frame %d logged an upload for a layer that was not drawn", w.frames)
			}
			continue
		}
		bm := bitmapByID(w.a, layer.id)
		if bm == nil {
			w.t.Fatalf("frame %d drew image %d, which is not a live background bitmap", w.frames, layer.id)
		}
		w.compositeLayer(&dst, &cover, bm, layer.alpha, layer.upload, winW, winH)
	}
	if cover < 0.999 {
		w.t.Fatalf("frame %d coverage %.3f lets the clear color through", w.frames, cover)
	}
	got := [3]byte{roundByte(dst[0]), roundByte(dst[1]), roundByte(dst[2])}
	if d := dist3(got, have); d > 16 {
		w.t.Fatalf("frame %d gpu composite %v, framebuffer %v (delta %d)", w.frames, got, have, d)
	}
}

func (w *crossfadeWalk) compositeLayer(dst *[3]float32, cover *float32, bm *ui.Bitmap, alpha float32, upload int, winW, winH int) {
	w.t.Helper()
	id, ver, bw, bh, pix := bitmapIdentity(w.t, bm)
	before := 0
	if s := w.cache.slots[id]; s != nil {
		before = s.uploads
	}
	w.cache.imageTexture(id, ver, bw, bh, pix)
	slot := w.cache.slots[id]
	if slot == nil || len(slot.pix) != len(pix) || slot.pix[3] != 255 {
		w.t.Fatalf("frame %d image %d uploaded an empty texture", w.frames, id)
	}
	if upload >= 0 {
		fresh := slot.uploads > before
		if fresh != (upload == 1) {
			w.t.Fatalf("frame %d image %d upload log %d, cache fresh %v (uploads %d)", w.frames, id, upload, fresh, slot.uploads)
		}
	}
	rgb := samplePix(slot.pix, bw, bh, winW, winH, w.x, w.y)
	a := float32(alphaByte(alpha)) / 255
	inv := 1 - a
	for i := 0; i < 3; i++ {
		dst[i] = float32(rgb[i])*a + dst[i]*inv
	}
	*cover = a + *cover*inv
}

func layersCover(layers []fadeSlice) bool {
	cover := float32(0)
	for _, layer := range layers {
		if layer.bm == nil || layer.alpha <= 0 {
			continue
		}
		a := float32(alphaByte(layer.alpha)) / 255
		cover = a + cover*(1-a)
	}
	return cover >= 0.999
}

func bitmapByID(a *App, id uint64) *ui.Bitmap {
	if id == 0 {
		return nil
	}
	for _, layer := range a.baseStack {
		if layer.bm != nil && bitmapImageID(layer.bm) == id {
			return layer.bm
		}
	}
	for _, bm := range []*ui.Bitmap{a.fadeFromBM, a.previewBM, a.fullBM, shownGradient(a.winPW, a.winPH)} {
		if bm != nil && bitmapImageID(bm) == id {
			return bm
		}
	}
	cacheMu.Lock()
	defer cacheMu.Unlock()
	for _, e := range entries {
		if e != nil && e.bm != nil && bitmapImageID(e.bm) == id {
			return e.bm
		}
	}
	return nil
}

type frameLayer struct {
	alpha  float32
	id     uint64
	upload int
}

type frameDraw struct {
	fromA, previewA, fullA    float32
	fromID, previewID, fullID uint64
	fromUp, previewUp, fullUp int
}

func (f frameDraw) layers() []frameLayer {
	return []frameLayer{
		{f.fromA, f.fromID, f.fromUp},
		{f.previewA, f.previewID, f.previewUp},
		{f.fullA, f.fullID, f.fullUp},
	}
}

type themeDebugBuf struct {
	buf *bytes.Buffer
	off int
}

func (d *themeDebugBuf) takeFrames(w, h int) []frameDraw {
	b := d.buf.Bytes()
	chunk := b[d.off:]
	d.off = len(b)
	size := fmt.Sprintf("size=%dx%d", w, h)
	var out []frameDraw
	for _, line := range bytes.Split(chunk, []byte("\n")) {
		if !bytes.Contains(line, []byte(" frame from=")) || !bytes.Contains(line, []byte(size)) {
			continue
		}
		var date, clock string
		var fa, pa, ua float64
		var fid, pid, uid uint64
		var fu, pu, uu int
		n, err := fmt.Sscanf(string(line), "%s %s frame from=%f/%d/upload=%d preview=%f/%d/upload=%d full=%f/%d/upload=%d",
			&date, &clock, &fa, &fid, &fu, &pa, &pid, &pu, &ua, &uid, &uu)
		if err != nil || n != 11 {
			continue
		}
		out = append(out, frameDraw{
			fromA: float32(fa), previewA: float32(pa), fullA: float32(ua),
			fromID: fid, previewID: pid, fullID: uid,
			fromUp: fu, previewUp: pu, fullUp: uu,
		})
	}
	return out
}

func (w *crossfadeWalk) absorbUploads(fr frameDraw, winW, winH int) {
	w.t.Helper()
	if w.a.baseDrawn {
		for _, layer := range w.a.baseStack {
			if layer.bm == nil || layer.alpha <= 0 {
				continue
			}
			w.compositeLayer(&([3]float32{}), new(float32), layer.bm, layer.alpha, -1, winW, winH)
		}
	}
	for _, layer := range fr.layers() {
		if layer.id == 0 || layer.alpha <= 0 {
			continue
		}
		bm := bitmapByID(w.a, layer.id)
		if bm == nil {
			continue
		}
		w.compositeLayer(&([3]float32{}), new(float32), bm, layer.alpha, layer.upload, winW, winH)
	}
}

func enableThemeDebug(t *testing.T) *themeDebugBuf {
	t.Helper()
	if themeDebugOn.Load() {
		t.Fatal("theme debug was already on")
	}
	buf := &bytes.Buffer{}
	path := filepath.Join(t.TempDir(), "theme-debug.log")
	themeDebugOverride.Store(path)
	themeDebugMu.Lock()
	themeDebugErr = buf
	themeDebugLogged = nil
	themeDebugSeen = nil
	themeDebugGen = nil
	themeDebugName = ""
	themeDebugMode = ""
	if themeDebugFile != nil {
		themeDebugFile.Close()
		themeDebugFile = nil
		themeDebugOpened = ""
	}
	themeDebugMu.Unlock()
	themeDebugOn.Store(true)
	t.Cleanup(func() {
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
	})
	return &themeDebugBuf{buf: buf}
}

func segmentProgress(old, neu, p [3]byte) (float64, bool) {
	var num, den float64
	for i := 0; i < 3; i++ {
		d := float64(int(neu[i]) - int(old[i]))
		num += float64(int(p[i])-int(old[i])) * d
		den += d * d
	}
	if den < 16 {
		return 1, false
	}
	return num / den, true
}

func sum3(c [3]byte) int {
	return int(c[0]) + int(c[1]) + int(c[2])
}

func absF(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func roundByte(v float32) byte {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return byte(v + 0.5)
}

func samplePix(pix []byte, bw, bh, winW, winH, x, y int) [3]byte {
	if bw == winW && bh == winH {
		i := (y*bw + x) * 4
		return [3]byte{pix[i], pix[i+1], pix[i+2]}
	}
	src := &image.NRGBA{Pix: pix, Stride: bw * 4, Rect: image.Rect(0, 0, bw, bh)}
	u := (float32(x) + 0.5) * float32(bw) / float32(winW)
	v := (float32(y) + 0.5) * float32(bh) / float32(winH)
	r, g, b := sampleBilinear(src, u, v)
	return [3]byte{r, g, b}
}

// Adopting a published bitmap used to install the previous theme as the
// crossfade's from layer, so the next paint was the old theme at full
// opacity. The clock owns that fade; this adoption must show the new theme.
func TestAdoptedFullResDoesNotFadeFromOld(t *testing.T) {
	resetGradientCache()
	gradSync.Store(false)
	gradMotion.Store(true)
	const w, h = 320, 180
	now := time.Now()
	warm := themeLook(themes.ThemeWarm, false)
	tide := themeLook(themes.ThemeTide, false)
	presentGradient(warm, w, h, now)
	awaitGradients()
	presentGradient(tide, w, h, now)
	awaitGradients()
	base, over, alpha, _ := presentGradient(tide, w, h, now.Add(time.Millisecond))
	if over != nil && alpha < 0.99 {
		t.Fatalf("adopting full-res faded in from the old theme at alpha %.3f", alpha)
	}
	if base == nil {
		t.Fatal("no bitmap after publish")
	}
	assertBitmapTheme(t, base, tide, warm)
}

// The settings window is a second size. Its map entry is still the old
// theme when the main window's bitmap publishes. Painting that entry is
// the old theme coming back.
func TestOtherWindowSizeDoesNotPaintOldTheme(t *testing.T) {
	resetGradientCache()
	gradSync.Store(false)
	gradMotion.Store(true)
	now := time.Now()
	warm := themeLook(themes.ThemeWarm, false)
	tide := themeLook(themes.ThemeTide, false)
	presentGradient(warm, 640, 400, now)
	awaitGradients()
	presentGradient(warm, 780, 560, now)
	awaitGradients()
	presentGradient(tide, 640, 400, now)
	awaitGradients()
	base, over, alpha, _ := presentGradient(tide, 780, 560, now.Add(time.Millisecond))
	if over != nil && alpha < 0.99 {
		t.Fatalf("other window size faded from the old theme at alpha %.3f", alpha)
	}
	if base == nil {
		t.Fatal("other window size painted nothing")
	}
	assertBitmapTheme(t, base, tide, warm)
}

func assertBitmapTheme(t *testing.T, bm *ui.Bitmap, want, old Theme) {
	t.Helper()
	_, _, bw, bh, pix := bitmapIdentity(t, bm)
	if bitmapMatches(pix, bw, bh, want) {
		return
	}
	if bitmapMatches(pix, bw, bh, old) {
		t.Fatalf("%dx%d bitmap is the old theme", bw, bh)
	}
	t.Fatalf("%dx%d bitmap is neither theme", bw, bh)
}

func bitmapMatches(pix []byte, bw, bh int, look Theme) bool {
	img := renderGradient(look, bw, bh, true)
	pts := [][2]int{{0, 0}, {bw / 2, 0}, {0, bh / 2}, {bw / 2, bh / 2}, {bw - 1, bh - 1}}
	for _, p := range pts {
		i := (p[1]*bw + p[0]) * 4
		j := img.PixOffset(p[0], p[1])
		if dist3([3]byte{pix[i], pix[i+1], pix[i+2]}, [3]byte{img.Pix[j], img.Pix[j+1], img.Pix[j+2]}) > 1 {
			return false
		}
	}
	return true
}
