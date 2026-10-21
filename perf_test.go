package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cove/themes"

	"github.com/egoist/mygo/ui"
)

func themeLook(id string, dark bool) Theme {
	return Theme{Colors: themes.Palette(id, dark), Dark: dark}
}

func TestAccentDoesNotRegenerate(t *testing.T) {
	resetGradientCache()
	gradSync.Store(false)
	gradMotion.Store(true)
	const w, h = 480, 270
	look := themeLook(themes.ThemeWarm, false)
	now := time.Now()
	presentGradient(look, w, h, now)
	awaitGradients()
	got := gradPublished.Load()
	if got != 1 {
		t.Fatalf("prime published %d", got)
	}
	purple, ok := themes.NamedAccent("purple", false)
	if !ok {
		t.Fatal("purple")
	}
	look.AccentDefault = purple
	look.AccentHover = purple
	look.ButtonPrimaryBg = purple
	before := gradPublished.Load()
	presentGradient(look, w, h, now)
	awaitGradients()
	if gradPublished.Load() != before {
		t.Fatalf("accent published %d, was %d", gradPublished.Load(), before)
	}
}

func TestRapidThemeSwitchOneGeneration(t *testing.T) {
	resetGradientCache()
	gradSync.Store(false)
	gradMotion.Store(true)
	ids := []string{
		themes.ThemeWarm, themes.ThemeTide, themes.ThemeNeutral, themes.ThemeMist, themes.ThemeSage,
		themes.ThemeLavender, themes.ThemeWarm, themes.ThemeTide, themes.ThemeNeutral, themes.ThemeLavender,
	}
	const w, h = 320, 180
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	gateMu.Lock()
	gradRenderGate = func() {
		once.Do(func() { close(entered) })
		<-release
	}
	gateMu.Unlock()
	defer func() {
		gateMu.Lock()
		gradRenderGate = nil
		gateMu.Unlock()
		select {
		case <-release:
		default:
			close(release)
		}
	}()

	presentGradient(themeLook(ids[0], false), w, h, time.Now())
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("generation did not start")
	}
	for i := 1; i < len(ids); i++ {
		presentGradient(themeLook(ids[i], i%2 == 1), w, h, time.Now())
	}
	gateMu.Lock()
	gradRenderGate = nil
	gateMu.Unlock()
	close(release)
	awaitGradients()
	if n := gradPublished.Load(); n != 1 {
		t.Fatalf("published %d generations, want 1 (aborted %d)", n, gradAborted.Load())
	}
	last := themeLook(ids[len(ids)-1], true)
	key := gradKeyOf(w, h, prepareWindowGrad(last))
	cacheMu.Lock()
	got := entries[key]
	cacheMu.Unlock()
	if got == nil {
		t.Fatal("final bitmap missing")
	}
	want := renderGradient(last, w, h, true)
	if d := imageDelta(got.img, want); d != 0 {
		t.Fatalf("final bitmap differs from the last theme by %d", d)
	}
}

func TestResizeDefersBackground(t *testing.T) {
	resetGradientCache()
	gradSync.Store(false)
	gradMotion.Store(true)
	look := themeLook(themes.ThemeTide, false)
	t0 := time.Now()
	presentGradient(look, 400, 240, t0)
	awaitGradients()
	n := gradPublished.Load()
	presentGradient(look, 640, 360, t0)
	if gradPublished.Load() != n {
		t.Fatalf("resize launched immediately, published %d", gradPublished.Load())
	}
	// Still the previous bitmap, stretched by the painter.
	cacheMu.Lock()
	st := sizes[[2]int{640, 360}]
	cacheMu.Unlock()
	if st == nil || st.shown == nil || st.shown.key.w != 400 {
		t.Fatalf("live resize did not keep the previous bitmap: %+v", st)
	}
	presentGradient(look, 640, 360, t0.Add(gradSettle))
	awaitGradients()
	if gradPublished.Load() != n+1 {
		t.Fatalf("settled resize published %d, want %d", gradPublished.Load(), n+1)
	}
	key := gradKeyOf(640, 360, prepareWindowGrad(look))
	cacheMu.Lock()
	_, ok := entries[key]
	cacheMu.Unlock()
	if !ok {
		t.Fatal("settled size was not generated")
	}
}

func TestGradientStaysNearReference(t *testing.T) {
	ids := []string{themes.ThemeWarm, themes.ThemeTide, themes.ThemeNeutral, themes.ThemeMist, themes.ThemeSage, themes.ThemeLavender}
	var worst int
	check := func(id string, dark bool, w, h int) {
		t.Helper()
		look := themeLook(id, dark)
		for _, dither := range []bool{false, true} {
			got := renderGradient(look, w, h, dither)
			ref := renderGradientRef(look, w, h, dither)
			d := imageDelta(got, ref)
			if d > worst {
				worst = d
			}
			if d > 1 {
				t.Errorf("%s dark=%v %dx%d dither=%v max channel delta %d", id, dark, w, h, dither, d)
			}
		}
	}
	for _, id := range ids {
		for _, dark := range []bool{false, true} {
			check(id, dark, 96, 720)
		}
	}
	check(themes.ThemeWarm, false, 1440, 64)
	check(themes.ThemeTide, true, 1440, 64)
	t.Logf("max |fast-reference| %d", worst)
}

func TestThemeSwitchMainThreadBound(t *testing.T) {
	// Serial on purpose: do not call t.Parallel. The 100ms budget is wall
	// time for one generation. A sample that shares the machine with other
	// work can land a few milliseconds over; that sample is taken again.
	// The limit stays 100ms.
	const readyLimit = 100 * time.Millisecond
	const w, h = 2880, 1800
	warm := themeLook(themes.ThemeWarm, false)
	tide := themeLook(themes.ThemeTide, false)
	now := time.Now()
	var primeMain, primeGen, switchMain, gen, shownAt time.Duration
	var primeRamp, primeQuant, primeLimit time.Duration
	var switchRamp, switchQuant, switchLimit time.Duration
	var samples []time.Duration
	ready := false
	for attempt := 1; attempt <= 3; attempt++ {
		runtime.GC()
		resetGradientCache()
		gradSync.Store(false)
		gradMotion.Store(true)

		primeStart := time.Now()
		presentGradient(warm, w, h, now)
		primeMain = time.Since(primeStart)
		genWait := time.Now()
		awaitGradients()
		primeGen = time.Since(genWait)
		primeRamp = time.Duration(gradLastRamp.Load())
		primeQuant = time.Duration(gradLastQuant.Load())
		primeLimit = time.Duration(gradLastLimit.Load())
		if gradPublished.Load() != 1 {
			t.Fatalf("prime published %d", gradPublished.Load())
		}

		switchStart := time.Now()
		presentGradient(tide, w, h, now)
		switchMain = time.Since(switchStart)
		visible := time.Now()
		awaitGradients()
		gen = time.Since(visible)
		presentGradient(tide, w, h, time.Now())
		shownAt = time.Since(visible)
		switchRamp = time.Duration(gradLastRamp.Load())
		switchQuant = time.Duration(gradLastQuant.Load())
		switchLimit = time.Duration(gradLastLimit.Load())
		samples = append(samples, shownAt)
		ok := primeMain <= themeSwitchMainBound && switchMain <= themeSwitchMainBound && shownAt <= readyLimit
		t.Logf("attempt %d duration %s main-prime %s main-switch %s pass=%v", attempt, shownAt, primeMain, switchMain, ok)
		if ok {
			ready = true
			break
		}
	}
	listed := make([]string, len(samples))
	for i, d := range samples {
		listed[i] = d.String()
	}
	t.Logf("theme-bitmap: attempts=%d durations=[%s]", len(samples), strings.Join(listed, " "))
	if !ready {
		t.Fatalf("new bitmap not ready to show within %s after 3 serial attempts (gen %s, ready %s)", readyLimit, gen, shownAt)
	}
	key := gradKeyOf(w, h, prepareWindowGrad(tide))
	cacheMu.Lock()
	e := entries[key]
	st := sizes[[2]int{w, h}]
	cacheMu.Unlock()
	if e == nil || st == nil || st.shown == nil || st.shown.key != key {
		t.Fatal("switched bitmap is not the one on screen")
	}
	if gradPublished.Load() != 2 {
		t.Fatalf("published %d, want 2", gradPublished.Load())
	}

	// Accent at the same maximized size must not build another bitmap.
	accent := tide
	purple, _ := themes.NamedAccent("purple", false)
	accent.AccentDefault = purple
	before := gradPublished.Load()
	accStart := time.Now()
	presentGradient(accent, w, h, time.Now())
	accMain := time.Since(accStart)
	awaitGradients()
	if gradPublished.Load() != before {
		t.Fatalf("accent regenerated (%d -> %d)", before, gradPublished.Load())
	}

	// A burst collapses onto the last theme.
	resetGradientCache()
	gradSync.Store(false)
	holdRapid(t, w, h)

	report := fmt.Sprintf(`Cove theme-switch perf, AFTER the background fix
host: GOARCH=%s GOOS=%s NumCPU=%d
renderer: separable OKLab row LUT + horizontal tint column, 64×64 blue-noise tile, parallel row fill, off the frame goroutine
crossfade: %s, shared with the control mix (motionTheme %s). Both start in the click frame. The background crossfades to a 1/%d undithered preview, the smooth ramp upscaled with linear filtering and no blue noise. The dithered full-resolution bitmap is built off the frame and crossfades in over at most %s. Accent-only changes start the mix immediately and do not rebuild a bitmap.
resize: stretch the previous bitmap; regenerate after %s still
cache: per (pixel size, window stop colors). Accent is not a key. Cap %d entries.
prewarm: skipped. 6 themes × 2 appearances × 2880×1800×4 bytes ≈ 248 MB, plus the bitmap copy, which is not a reasonable idle cache.

hard bound: presentGradient (main-thread theme-switch work at %d×%d) must return in ≤ %s or the test fails.
measured main thread, prime (schedule first %d×%d): %s
measured main thread, warm→tide switch: %s
measured main thread, accent switch: %s
bound: %s
generation, first %d×%d bitmap (includes upload): %s
  phases: ramp %s, quantize %s, limit %s
generation, theme switch, off the main thread: %s
  phases: ramp %s, quantize %s, limit %s
theme-bitmap attempts (pass criteria unchanged; every attempt is listed):
%s
new bitmap ready for the fade within: %s (limit 100ms)
regenerations per theme switch: 1
regenerations per accent switch: 0
animated mix no longer rebuilds: the bitmap uses the target palette, not the per-frame mix (before: 13 regenerations, ~15s on this host)

Before, on this host (see before.txt): renderGradient 2880×1800 ≈ 1.06s on the frame goroutine, one regeneration per instant switch, 13 during the 200ms color mix.

This VM is %s/%s with %d cores, not an Apple Silicon Mac. The pixel loop is a memory-bandwidth-bound quantize pass plus a neighbor clamp split into vertical strips, one fewer than the host processors and capped at eight, so a switch frame still has a processor. A measured generation of %s here is the number to trust. An M-series core is often faster on this scalar/memory mix, sometimes around 1.5–2×; that would put generation near %s–%s. It is not a measurement taken on Apple Silicon, and it is not a claim that every Mac is under 15ms. The 15ms figure was the design target for the LUT loop; the bound that fails the build is the main-thread cap above.
`,
		runtime.GOARCH, runtime.GOOS, runtime.NumCPU(),
		gradFade, motionTheme, previewDiv, motionTheme,
		gradSettle,
		gradCacheCap,
		w, h, themeSwitchMainBound,
		w, h, primeMain,
		switchMain,
		accMain,
		themeSwitchMainBound,
		w, h, primeGen,
		primeRamp, primeQuant, primeLimit,
		gen,
		switchRamp, switchQuant, switchLimit,
		themeAttemptReport(samples),
		shownAt,
		runtime.GOOS, runtime.GOARCH, runtime.NumCPU(),
		gen,
		gen*2/3, gen/2,
	)
	path := perfReportPath(t)
	if err := os.WriteFile(path, []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s\n%s", path, report)
}

func holdRapid(t *testing.T, w, h int) {
	t.Helper()
	ids := []string{
		themes.ThemeWarm, themes.ThemeTide, themes.ThemeNeutral, themes.ThemeMist, themes.ThemeSage,
		themes.ThemeLavender, themes.ThemeWarm, themes.ThemeTide, themes.ThemeNeutral, themes.ThemeSage,
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	gateMu.Lock()
	gradRenderGate = func() {
		once.Do(func() { close(entered) })
		<-release
	}
	gateMu.Unlock()
	defer func() {
		gateMu.Lock()
		gradRenderGate = nil
		gateMu.Unlock()
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	presentGradient(themeLook(ids[0], false), w, h, time.Now())
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("generation did not start")
	}
	for i := 1; i < len(ids); i++ {
		presentGradient(themeLook(ids[i], false), w, h, time.Now())
	}
	gateMu.Lock()
	gradRenderGate = nil
	gateMu.Unlock()
	close(release)
	awaitGradients()
	if n := gradPublished.Load(); n != 1 {
		t.Fatalf("rapid switch at %dx%d published %d, want 1 (aborted %d)", w, h, n, gradAborted.Load())
	}
	last := themeLook(ids[len(ids)-1], false)
	key := gradKeyOf(w, h, prepareWindowGrad(last))
	cacheMu.Lock()
	got := entries[key]
	cacheMu.Unlock()
	if got == nil {
		t.Fatal("rapid switch lost the final bitmap")
	}
}

// switchTiming is one theme switch at a device size. main is
// build+layout+paint of the click frame, present excluded. first is the
// tester clock from the click until the first frame whose colors or
// background pixels differ. done is the tester clock when the 150ms mix
// finishes. adoptWall is wall time until the dithered bitmap is on the
// clock or pinned; adoptClock is that same moment on the tester clock.
// gen is the off-thread full-resolution build.
type switchTiming struct {
	build, layout, paint, main, present   time.Duration
	first, firstMain, firstPresent        time.Duration
	preview, gen, adoptWall, adoptClock   time.Duration
	done, swapDur                         time.Duration
	wakes                                 int64
	windowBefore, windowSwap, windowAfter *image.RGBA
}

func TestThemeSwitchMainThreadLatency(t *testing.T) {
	resetGradientCache()
	small := measureSwitchLatency(t, 2880, 1800)
	resetGradientCache()
	large := measureSwitchLatency(t, 5120, 2880)
	bound := themeSwitchMainBound
	if small.main > bound || large.main > bound {
		t.Fatalf("switch frame build+layout+paint exceeds %s: 2880×1800 %s (build %s layout %s paint %s preview %s), 5120×2880 %s (build %s layout %s paint %s preview %s)",
			bound,
			small.main, small.build, small.layout, small.paint, small.preview,
			large.main, large.build, large.layout, large.paint, large.preview)
	}
	if small.first > bound || large.first > bound {
		t.Fatalf("click to first changed frame exceeds %s: 2880×1800 %s, 5120×2880 %s", bound, small.first, large.first)
	}
	if small.preview > previewSyncLimit {
		t.Fatalf("2880×1800 preview %s exceeded %s; it must be built on the click frame or prebuilt", small.preview, previewSyncLimit)
	}
	for _, tm := range []switchTiming{small, large} {
		if tm.done < motionTheme || tm.done >= motionTheme+16*time.Millisecond {
			t.Fatalf("visible mix finished at %s; want the first frame at or after %s", tm.done, motionTheme)
		}
	}
	delta := previewDelta(t)

	section := fmt.Sprintf(`
theme switch latency
The click frame starts the 150ms control mix and a 1/%d undithered preview of the target gradient. The preview is that ramp sampled smoothly, with no blue noise and no run-cap nudge: a nudge at 1/8 upscales into a grid or a soft step, and the step reads as blur. The bitmap is not pixelated, so the painter upscales it with linear filtering. Bilinear of the smooth ramp stays within one code of the dithered full-resolution bitmap on every theme, so the preview stage has no visible blur. Sidebar selection and the other theme colors are the same mix, and they first move on the frame after the click. The dithered full-resolution bitmap stays off the frame goroutine. When it is published it crossfades in over the time left on the 150ms clock, or over 150ms when that remainder is under a frame. wakeGradient invalidates the window from the build goroutine (darwin: the run loop is signaled), so no pointer, scroll, or resize is delivered after the switch. Present is the software raster of the whole window on this VM; it is not part of the switch-frame bound.

On the Mac build e02d9b58 the new theme was saved and then appeared only after a restart. Moving the mouse did not show it. setTheme and setAppearance wrote the store from inside the frame that delivered the click. requestFrame does nothing while that frame is running, and only the window that handled the click is rebuilt in the same frame. The other window was never invalidated. Pointer motion requests a frame only for a scrollbar, a pressed control, or an element that tracks the pointer; the window background does not, so a move delivered nothing. That build also never woke the run loop when the worker finished, and it held the palette until the bitmap existed. A restart loaded the saved theme, which is why the change showed up then. A switch whose full-resolution bitmap is already cached still never starts the worker, so wakeGradient does not run for it either. setTheme, setAppearance, and the start of the clock now invalidate both windows immediately. The control mix starts on the frame that delivers, and it does not wait on the bitmap. Each published gradient is a new scene image with a private pixel copy. Metal and GL keep a texture per image ID and upload again only when the image version changes (imageTexture in internal/gpu/metal and internal/gpu/gl). Sharing the worker buffer would let a later render rewrite bytes the GPU had already copied into that texture, and an unchanged version would not upload them again. The software rasterizer reads the buffer at draw time, so the Linux tests stayed green. TestThemeBitmapIdentity asserts a new image ID and a new cached texture per generation. TestThemeSwitchLandsWithoutInput switches with no click, scroll, or pointer afterward, including a cache hit that does not publish a bitmap, and checks that the presented title-bar pixel matches the new theme. The veil, the 1/8 preview, and the bitmap rebuilt after a resize each get a new image too. TestCachedBitmapsUpdate checks that a theme switch, a light/dark switch, a follow-system switch, and a settled resize each present a new texture for those bitmaps.

On the Mac build 986cdce1 the new theme showed up without a restart, and the window background flashed while it arrived. The crossfade kept the outgoing bitmap opaque, then drew the preview at alpha u times (1 minus the full-resolution swap) and the full-resolution bitmap at the swap alpha. Source-over of those three weights is not a blend that only moves forward. The outgoing theme's share is (1 minus u times (1 minus s)) times (1 minus s). After the preview is past halfway, that share rises again as soon as the swap starts, so the previous background comes back in the middle of the handoff. A maximized window hits this whenever the dithered build finishes after about 75 ms, which the measured generations do. The veil had the same shape of bug: the outgoing veil and the incoming veil were both drawn below full opacity, and the card under them showed through mid-fade. The Metal texture cache was not the cause. imageTexture uploads a new image with replaceRegion before the command buffer is encoded, and it keeps the texture for 240 frames; an image that is still the crossfade's outgoing layer is not evicted. The fix draws the outgoing layer opaque and the incoming layer at the clock alpha on top. The preview stays at u while the full-resolution bitmap fades in at the swap alpha. The veil's outgoing bitmap stays opaque the same way. A frame with no outgoing bitmap draws the new one opaque, so the layer clear color stays covered. TestThemeCrossfadeNoFlash renders every frame of a cold switch (the full-resolution build held until the preview clock ends, which is the case that brings the old theme back), a cache hit, a light-to-dark switch both cold and cached, and a run of rapid switches. Each drawn image is uploaded through the texture-cache rule before its pixels are read. Every frame's title-bar pixel stays opaque and only moves toward the new color.

Theme debug log, off unless one of these is set before Cove launches. Quit Cove first. Either:
COVE_DEBUG_THEME=1
in the environment Cove is launched from, or:
defaults write dev.cove.chat CoveDebugTheme -bool true
Then open Cove again. Either one is enough. Lines go to ~/Library/Logs/Cove/theme-debug.log and to stderr. Each line is the time, the event (switch requested, build started, build done, preview shown, full-res presented, veil presented, resize regen, or frame), theme, appearance, pixel size, bitmap generation, and image=, the scene image id Metal and GL use as the texture key. A frame line is written on every background paint and is not deduped. It records the outgoing layer, the preview, and the full-resolution layer as alpha, image id, and upload. upload is 1 on the frame that first draws that image id, which is the frame Metal creates the texture, and 0 when the texture is already resident. A resize regen line is the new full-resolution bitmap. To turn it off, quit Cove, unset COVE_DEBUG_THEME, and run:
defaults delete dev.cove.chat CoveDebugTheme
While the flag is off the paint path only checks it, and does not format a line or open the log.

click → first changed frame (bound ≤ %s, the frame after the click): 2880×1800 %s, 5120×2880 %s
that frame, build+layout+paint / present: 2880×1800 %s / %s, 5120×2880 %s / %s
switch frame (the click, u = 0) main thread, build+layout+paint, preview included, present excluded (bound ≤ %s): 2880×1800 %s (present %s), 5120×2880 %s (present %s)
visible mix complete, tester clock (first frame at or after %s, and not the frame before): 2880×1800 %s, 5120×2880 %s
preview render on the click frame: 2880×1800 %s, 5120×2880 %s (sync limit %s at 2880×1800; slower machines prebuild per theme)
preview vs full-res, bilinear upscale, max per-channel delta across every theme, light and dark, at 2880×1800 and 5120×2880 (bound ≤ 1/255): %d/255
full-res generation, off thread: 2880×1800 %s, 5120×2880 %s
full-res crossfade start, wall from the click: 2880×1800 %s, 5120×2880 %s
full-res crossfade start, tester clock from the click: 2880×1800 %s, 5120×2880 %s
full-res crossfade duration (≤ %s): 2880×1800 %s, 5120×2880 %s
wakeGradient calls on the cold switch: 2880×1800 %d, 5120×2880 %d
TestThemeBuildInvalidatesWithoutInput: after the off-thread build completes, the wake func runs and the app's invalidate hook runs, with no Click, Scroll, or Advance.
crops: artifacts/theme-latency/preview-4x.png is the filtered preview stage at 4×. before.png / before-4x.png are the same preview, at-swap.png is the full-resolution bitmap it crossfades to, after.png is the pinned window. window-before.png is the live preview frame.
`,
		previewDiv,
		bound, small.first, large.first,
		small.firstMain, small.firstPresent, large.firstMain, large.firstPresent,
		bound, small.main, small.present, large.main, large.present,
		motionTheme, small.done, large.done,
		small.preview, large.preview, previewSyncLimit,
		delta,
		small.gen, large.gen,
		small.adoptWall, large.adoptWall,
		small.adoptClock, large.adoptClock,
		motionTheme, small.swapDur, large.swapDur,
		small.wakes, large.wakes,
	)
	saveWindowCrops(t, small)
	path := perfReportPath(t)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(section); err != nil {
		t.Fatal(err)
	}
	t.Log(section)
}

func measureSwitchLatency(t *testing.T, w, h int) switchTiming {
	t.Helper()
	a := revisionChat(t)
	a.instant = false
	tt := ui.NewTester(a.view, w, h)
	settleVeil(t, tt)
	fp := attachFrameProbe(tt)
	mark := a.look.ThemeMark
	pix := windowPatch(tt)
	wakes := gradWakes.Load()
	click := tt.Now()
	// Drop garbage from earlier tests so the switch frame is the work
	// of the switch, not a collection those tests left due.
	runtime.GC()
	wall0 := time.Now()
	a.data.Theme = themes.ThemeTide
	runOneFrame(tt)
	var out switchTiming
	out.build, out.layout, out.paint = fp.laps[0], fp.laps[1], fp.laps[2]
	out.main = out.build + out.layout + out.paint
	out.present = fp.laps[3]
	out.preview = time.Duration(previewLast.Load())
	if !a.fadeLive || !a.blendStart.Equal(click) || !a.backdropFadeAt.Equal(click) {
		t.Fatalf("%dx%d clock did not start on the click: live %v blend %v fade %v now %v", w, h, a.fadeLive, a.blendStart, a.backdropFadeAt, tt.Now())
	}
	if a.fullBM != nil || a.previewBM == nil {
		t.Fatalf("%dx%d did not show a preview: full %v preview %v", w, h, a.fullBM != nil, a.previewBM != nil)
	}
	note := func() {
		if out.adoptClock == 0 && backdropAdopted(a) {
			out.adoptClock = tt.Now().Sub(click)
			out.adoptWall = time.Since(wall0)
			out.swapDur = a.swapDur
			if a.swapAt.IsZero() || a.swapDur <= 0 || a.swapDur > motionTheme {
				t.Fatalf("%dx%d full-res crossfade is %s (armed %v), want (0, %s]", w, h, a.swapDur, !a.swapAt.IsZero(), motionTheme)
			}
			if a.previewBM != nil && !a.fadeLive {
				t.Fatalf("%dx%d full-res replaced the preview without a crossfade", w, h)
			}
		}
		if w != 2880 {
			return
		}
		if out.windowBefore == nil && a.fadeLive && a.fullBM == nil && tt.Now().After(click) {
			out.windowBefore = themeCrop(tt.Image())
		}
		if out.windowSwap == nil && backdropAdopted(a) {
			out.windowSwap = themeCrop(tt.Image())
		} else if out.windowSwap != nil && out.windowAfter == nil {
			out.windowAfter = themeCrop(tt.Image())
		}
	}
	changed := a.look.ThemeMark != mark || !bytes.Equal(pix, windowPatch(tt))
	if changed {
		out.first = 0
		out.firstMain = out.main
		out.firstPresent = out.present
	} else {
		tt.Advance(16 * time.Millisecond)
		out.first = tt.Now().Sub(click)
		out.firstMain = fp.laps[0] + fp.laps[1] + fp.laps[2]
		out.firstPresent = fp.laps[3]
		if a.look.ThemeMark == mark && bytes.Equal(pix, windowPatch(tt)) {
			t.Fatalf("%dx%d nothing changed on the frame after the click", w, h)
		}
	}
	note()
	for i := 0; i < 40 && !a.blendStart.IsZero(); i++ {
		prev := tt.Now()
		tt.Advance(16 * time.Millisecond)
		if !tt.Now().After(prev) {
			t.Fatalf("%dx%d mix stalled", w, h)
		}
		note()
	}
	out.done = tt.Now().Sub(click)
	if !a.blendStart.IsZero() {
		t.Fatalf("%dx%d control mix did not finish (now %s)", w, h, out.done)
	}
	for i := 0; i < 80 && !backdropAdopted(a); i++ {
		prev := tt.Now()
		tt.Advance(16 * time.Millisecond)
		if !tt.Now().After(prev) {
			t.Fatalf("%dx%d adopt wait stalled", w, h)
		}
		note()
	}
	if !backdropAdopted(a) {
		t.Fatalf("%dx%d full-res bitmap was not adopted (live %v published %d)", w, h, a.fadeLive, gradPublished.Load())
	}
	if out.adoptClock == 0 {
		out.adoptClock = tt.Now().Sub(click)
		out.adoptWall = time.Since(wall0)
	}
	for i := 0; i < 40 && a.themeMoving(); i++ {
		prev := tt.Now()
		tt.Advance(16 * time.Millisecond)
		if !tt.Now().After(prev) {
			break
		}
	}
	if w == 2880 {
		out.windowAfter = themeCrop(tt.Image())
	}
	out.gen = time.Duration(gradLastGen.Load())
	out.wakes = gradWakes.Load() - wakes
	if out.wakes < 1 {
		t.Fatalf("%dx%d worker did not wake the window", w, h)
	}
	return out
}

func backdropAdopted(a *App) bool {
	if a.fullBM != nil {
		return true
	}
	if a.winPW < 1 || a.fadeLive {
		return false
	}
	e := cachedGradient(a.lookTarget, a.winPW, a.winPH)
	if e == nil {
		return false
	}
	cacheMu.Lock()
	st := sizes[[2]int{a.winPW, a.winPH}]
	cacheMu.Unlock()
	return st != nil && st.shown == e && st.from == nil
}

func imageDelta(a, b *image.NRGBA) int {
	if a.Rect.Dx() != b.Rect.Dx() || a.Rect.Dy() != b.Rect.Dy() {
		return 255
	}
	w, h := a.Rect.Dx(), a.Rect.Dy()
	maxD := 0
	for y := 0; y < h; y++ {
		ar := a.Pix[y*a.Stride : y*a.Stride+w*4]
		br := b.Pix[y*b.Stride : y*b.Stride+w*4]
		for x := 0; x < w*4; x += 4 {
			for c := 0; c < 3; c++ {
				d := int(ar[x+c]) - int(br[x+c])
				if d < 0 {
					d = -d
				}
				if d > maxD {
					maxD = d
				}
			}
		}
	}
	return maxD
}

func themeCrop(src *image.RGBA) *image.RGBA {
	const cw, ch = 320, 180
	b := src.Bounds()
	x0 := b.Max.X - cw - 16
	y0 := b.Min.Y + 28
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x0+cw > b.Max.X {
		x0 = b.Max.X - cw
	}
	if y0+ch > b.Max.Y {
		y0 = b.Max.Y - ch
	}
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	dst := image.NewRGBA(image.Rect(0, 0, cw, ch))
	for y := 0; y < ch; y++ {
		for x := 0; x < cw; x++ {
			dst.SetRGBA(x, y, src.RGBAAt(x0+x, y0+y))
		}
	}
	return dst
}

func nrgbaCrop(src *image.NRGBA) *image.RGBA {
	const cw, ch = 320, 180
	b := src.Bounds()
	x0 := b.Min.X + b.Dx()*7/8
	y0 := b.Min.Y + b.Dy()*3/4
	if x0+cw > b.Max.X {
		x0 = b.Max.X - cw
	}
	if y0+ch > b.Max.Y {
		y0 = b.Max.Y - ch
	}
	dst := image.NewRGBA(image.Rect(0, 0, cw, ch))
	for y := 0; y < ch; y++ {
		for x := 0; x < cw; x++ {
			c := src.NRGBAAt(x0+x, y0+y)
			dst.SetRGBA(x, y, color.RGBA{R: c.R, G: c.G, B: c.B, A: 255})
		}
	}
	return dst
}

func zoom4(src *image.RGBA) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w*4, h*4))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := src.RGBAAt(b.Min.X+x, b.Min.Y+y)
			for dy := 0; dy < 4; dy++ {
				row := dst.Pix[(y*4+dy)*dst.Stride:]
				for dx := 0; dx < 4; dx++ {
					o := (x*4 + dx) * 4
					row[o], row[o+1], row[o+2], row[o+3] = c.R, c.G, c.B, c.A
				}
			}
		}
	}
	return dst
}

func saveThemePNG(t *testing.T, name string, img image.Image) {
	t.Helper()
	dir := "/opt/cursor/artifacts/theme-latency"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writePNG(t, dir+"/"+name+".png", img)
}

func saveWindowCrops(t *testing.T, tm switchTiming) {
	t.Helper()
	if tm.windowBefore != nil {
		saveThemePNG(t, "window-before", tm.windowBefore)
		saveThemePNG(t, "window-before-4x", zoom4(tm.windowBefore))
	}
	if tm.windowSwap != nil {
		saveThemePNG(t, "window-at-swap", tm.windowSwap)
		saveThemePNG(t, "window-at-swap-4x", zoom4(tm.windowSwap))
	}
	if tm.windowAfter != nil {
		saveThemePNG(t, "after", tm.windowAfter)
		saveThemePNG(t, "after-4x", zoom4(tm.windowAfter))
	}
}

// previewDelta is the max per-channel difference between the bilinear
// upscale of the undithered 1/8 preview and the dithered full-resolution
// bitmap. The preview is the smooth ramp: no blue noise, no run-cap nudge.
// One code is the bound. A larger gap is the blur or the band the nudge used
// to leave.
func previewDelta(t *testing.T) int {
	t.Helper()
	worst := 0
	for _, preset := range themes.Presets() {
		for _, dark := range []bool{false, true} {
			look := themeLook(preset.ID, dark)
			for _, sz := range [][2]int{{2880, 1800}, {5120, 2880}} {
				fw, fh := sz[0], sz[1]
				full := renderGradient(look, fw, fh, true)
				pw, ph := previewSize(fw, fh)
				img, _ := renderPrepared(prepareWindowGrad(look), pw, ph, false, 0, nil)
				if img == nil {
					t.Fatalf("%s dark=%v %dx%d preview render failed", preset.ID, dark, fw, fh)
				}
				up := upscaleBilinear(img, fw, fh)
				if d := imageDelta(up, full); d > worst {
					worst = d
				}
				if preset.ID != themes.ThemeTide || dark || fw != 2880 {
					continue
				}
				dithered, _ := renderPrepared(prepareWindowGrad(look), pw, ph, true, fullBand, nil)
				if dithered != nil && imageDelta(img, dithered) == 0 {
					t.Fatal("preview matches the dithered bitmap")
				}
				before := nrgbaCrop(up)
				swap := nrgbaCrop(full)
				saveThemePNG(t, "before", before)
				saveThemePNG(t, "before-4x", zoom4(before))
				saveThemePNG(t, "preview-4x", zoom4(previewStrip(up)))
				saveThemePNG(t, "at-swap", swap)
				saveThemePNG(t, "at-swap-4x", zoom4(swap))
			}
		}
	}
	if worst > 1 {
		t.Fatalf("bilinear preview is %d/255 from the full-resolution bitmap", worst)
	}
	return worst
}

// previewStrip is a tall slice of the filtered preview, from a slow part
// of the ramp, so a 4× zoom shows a band if one is there.
func previewStrip(src *image.NRGBA) *image.RGBA {
	const cw, ch = 96, 240
	b := src.Bounds()
	x0 := b.Min.X + b.Dx()/8
	y0 := b.Min.Y + b.Dy()/3
	if x0+cw > b.Max.X {
		x0 = b.Max.X - cw
	}
	if y0+ch > b.Max.Y {
		y0 = b.Max.Y - ch
	}
	dst := image.NewRGBA(image.Rect(0, 0, cw, ch))
	for y := 0; y < ch; y++ {
		for x := 0; x < cw; x++ {
			c := src.NRGBAAt(x0+x, y0+y)
			dst.SetRGBA(x, y, color.RGBA{R: c.R, G: c.G, B: c.B, A: 255})
		}
	}
	return dst
}

func upscaleBilinear(src *image.NRGBA, fw, fh int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, fw, fh))
	pw, ph := src.Rect.Dx(), src.Rect.Dy()
	sx := float32(pw) / float32(fw)
	sy := float32(ph) / float32(fh)
	for y := 0; y < fh; y++ {
		v := (float32(y) + 0.5) * sy
		row := dst.Pix[y*dst.Stride:]
		for x := 0; x < fw; x++ {
			u := (float32(x) + 0.5) * sx
			r, g, b := sampleBilinear(src, u, v)
			o := x * 4
			row[o], row[o+1], row[o+2], row[o+3] = r, g, b, 255
		}
	}
	return dst
}

func sampleBilinear(src *image.NRGBA, u, v float32) (uint8, uint8, uint8) {
	u, v = u-0.5, v-0.5
	x0 := int(math.Floor(float64(u)))
	y0 := int(math.Floor(float64(v)))
	tx := u - float32(x0)
	ty := v - float32(y0)
	w := src.Rect.Dx() - 1
	h := src.Rect.Dy() - 1
	at := func(x, y int) (float32, float32, float32) {
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		if x > w {
			x = w
		}
		if y > h {
			y = h
		}
		i := src.PixOffset(x, y)
		return float32(src.Pix[i]), float32(src.Pix[i+1]), float32(src.Pix[i+2])
	}
	r00, g00, b00 := at(x0, y0)
	r10, g10, b10 := at(x0+1, y0)
	r01, g01, b01 := at(x0, y0+1)
	r11, g11, b11 := at(x0+1, y0+1)
	mix := func(a, b, c, d float32) uint8 {
		top := a*(1-tx) + b*tx
		bot := c*(1-tx) + d*tx
		v := (top*(1-ty) + bot*ty) / 255
		if v <= 0 {
			return 0
		}
		if v >= 1 {
			return 255
		}
		return uint8(v*255 + 0.5)
	}
	return mix(r00, r10, r01, r11), mix(g00, g10, g01, g11), mix(b00, b10, b01, b11)
}

func TestPreviewMatchesFullRes(t *testing.T) {
	delta := previewDelta(t)
	t.Logf("preview div %d bilinear upscale delta %d/255", previewDiv, delta)
}

func TestThemeBuildInvalidatesWithoutInput(t *testing.T) {
	resetGradientCache()
	var n atomic.Int32
	setGradientMode(false, true, func() { n.Add(1) })
	ensureGradient(themeLook(themes.ThemeSage, true), 960, 600)
	awaitGradients()
	if n.Load() == 0 {
		t.Fatal("off-thread build finished without calling the wake func, and no input was sent")
	}
	setGradientMode(false, true, func() {})

	resetGradientCache()
	n.Store(0)
	gradientWakeHook.Store(func() { n.Add(1) })
	defer gradientWakeHook.Store(func() {})
	a := revisionChat(t)
	a.instant = false
	tt := ui.NewTester(a.view, 900, 600)
	settleVeil(t, tt)
	n.Store(0)
	wakes := gradWakes.Load()
	a.data.Theme = themes.ThemeMist
	tt.Frame()
	awaitGradients()
	awaitVeils()
	if n.Load() == 0 || gradWakes.Load() <= wakes {
		t.Fatalf("build did not invalidate with no further input (hook %d wakes %d -> %d)", n.Load(), wakes, gradWakes.Load())
	}
}

func TestThemeSwitchRapidNoFlicker(t *testing.T) {
	resetGradientCache()
	a := revisionChat(t)
	a.instant = false
	tt := ui.NewTester(a.view, 2880, 1800)
	settleVeil(t, tt)
	fp := attachFrameProbe(tt)
	ids := []string{themes.ThemeTide, themes.ThemeNeutral, themes.ThemeMist, themes.ThemeSage, themes.ThemeLavender}
	endpoints := map[ui.Color]bool{a.look.ThemeMark: true}
	left := map[ui.Color]bool{}
	var lastShown ui.Color
	var haveShown bool
	var haveTarget bool
	var target ui.Color
	var maxMain time.Duration
	observe := func() {
		t.Helper()
		mark := a.look.ThemeMark
		if left[mark] {
			t.Fatalf("flickered back to an abandoned theme mark %+v", mark)
		}
		if haveShown && mark != lastShown && endpoints[lastShown] {
			left[lastShown] = true
		}
		lastShown = mark
		haveShown = true
		if a.fullBM != nil {
			e := cachedGradient(a.lookTarget, a.winPW, a.winPH)
			if e == nil || e.bm != a.fullBM {
				t.Fatal("frame showed a bitmap that is not the theme just picked")
			}
		}
	}
	observe()
	for i, id := range ids {
		if haveTarget && a.look.ThemeMark != target {
			left[target] = true
		}
		runtime.GC()
		a.data.Theme = id
		runOneFrame(tt)
		main := fp.laps[0] + fp.laps[1] + fp.laps[2]
		if main > maxMain {
			maxMain = main
		}
		if main > themeSwitchMainBound {
			t.Fatalf("switch %d main thread %s exceeds %s", i+1, main, themeSwitchMainBound)
		}
		target = a.lookTarget.ThemeMark
		haveTarget = true
		endpoints[target] = true
		observe()
		if i+1 == len(ids) {
			break
		}
		stepped := time.Duration(0)
		for stepped < 40*time.Millisecond {
			prev := tt.Now()
			tt.Advance(16 * time.Millisecond)
			if !tt.Now().After(prev) {
				t.Fatal("rapid switch stalled between themes")
			}
			stepped += tt.Now().Sub(prev)
			observe()
		}
	}
	for i := 0; i < 40 && a.themeMoving(); i++ {
		prev := tt.Now()
		tt.Advance(16 * time.Millisecond)
		if !tt.Now().After(prev) {
			t.Fatal("rapid switch did not finish")
		}
		observe()
	}
	if a.themeMoving() {
		t.Fatalf("rapid switch still moving (live %v stand-in %v)", a.fadeLive, a.veilStandIn)
	}
	last := a.lookTarget.ThemeMark
	if a.look.ThemeMark != last || left[last] {
		t.Fatalf("ended on %+v, target %+v", a.look.ThemeMark, last)
	}
	assertFullResPinned(t, a)
	published, aborted := rapidCancel(t)

	section := fmt.Sprintf(`
rapid theme switches
Five themes, each switch frame 40ms of tester time after the previous one, at 2880×1800. Each switch frame's build+layout+paint stays within %s (max measured %s). The run ends on the last theme, and a frame that has left an abandoned theme's exact mark does not show it again. A separate burst, five beginThemeSwitch calls 40ms of wall time apart at 2880×1800 with no presents between them, published %d bitmap and aborted %d stale builds. The published bitmap is the last theme.
`,
		themeSwitchMainBound, maxMain, published, aborted,
	)
	path := perfReportPath(t)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(section); err != nil {
		t.Fatal(err)
	}
	t.Log(section)
}

func rapidCancel(t *testing.T) (published, aborted int64) {
	t.Helper()
	resetGradientCache()
	a := &App{}
	a.look = themeLook(themes.ThemeWarm, false)
	a.lookTarget = a.look
	a.haveLook = true
	a.winPW, a.winPH = 2880, 1800
	ids := []string{themes.ThemeTide, themes.ThemeNeutral, themes.ThemeMist, themes.ThemeSage, themes.ThemeLavender}
	now := time.Now()
	for i, id := range ids {
		target := themeLook(id, false)
		a.beginThemeSwitch(now, target)
		a.lookTarget = target
		if i+1 < len(ids) {
			time.Sleep(40 * time.Millisecond)
			now = now.Add(40 * time.Millisecond)
		}
	}
	awaitGradients()
	published = gradPublished.Load()
	aborted = gradAborted.Load()
	last := themeLook(ids[len(ids)-1], false)
	key := gradKeyOf(2880, 1800, prepareWindowGrad(last))
	cacheMu.Lock()
	got := entries[key]
	cacheMu.Unlock()
	if got == nil || published != 1 || aborted < 1 {
		t.Fatalf("rapid cancel published %d aborted %d final %v", published, aborted, got != nil)
	}
	for _, id := range ids[:len(ids)-1] {
		k := gradKeyOf(2880, 1800, prepareWindowGrad(themeLook(id, false)))
		cacheMu.Lock()
		_, ok := entries[k]
		cacheMu.Unlock()
		if ok {
			t.Fatalf("intermediate theme %s stayed in the cache", id)
		}
	}
	return published, aborted
}

func themeAttemptReport(samples []time.Duration) string {
	var b strings.Builder
	for i, d := range samples {
		fmt.Fprintf(&b, "attempt %d: %s\n", i+1, d)
	}
	if len(samples) == 1 {
		b.WriteString("second pass: not taken\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// perfReportPath is /opt/cursor/artifacts/perf/after.txt, creating the
// directory so a test can append the report without another test having
// created it first.
func perfReportPath(t *testing.T) string {
	t.Helper()
	dir := "/opt/cursor/artifacts/perf"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir + "/after.txt"
}
