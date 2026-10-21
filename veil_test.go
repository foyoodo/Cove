package main

import (
	"bytes"
	"fmt"
	"os"
	"testing"
	"time"

	"cove/themes"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// Bounds at 2880×1800. The veil cap is the frame-goroutine budget from
// the request. Idle and scroll fit in one 16ms frame. The streaming
// frame's own cap is TestVeilStreamFrameBound.
const (
	veilFrameBound   = 8 * time.Millisecond
	idleFrameBound   = 16 * time.Millisecond
	scrollFrameBound = 16 * time.Millisecond
)

func TestThemeFadeClockMatchesControls(t *testing.T) {
	if motionTheme != 150*time.Millisecond || gradFade != motionTheme {
		t.Fatalf("theme duration %s, background fade %s; want one 150ms clock", motionTheme, gradFade)
	}
	resetGradientCache()
	a := revisionChat(t)
	a.instant = false
	tt := ui.NewTester(a.view, 1024, 700)
	settleVeil(t, tt)

	// No pointer, scroll, or resize after this. The fade has to run on
	// the After timer the click frame armed, and on the worker's wake.
	orig := a.look.ThemeMark
	pix := windowPatch(tt)
	wakes := gradWakes.Load()
	a.data.Theme = themes.ThemeTide
	tt.Frame()
	if !a.fadeLive || !a.blendStart.Equal(tt.Now()) || !a.backdropFadeAt.Equal(a.blendStart) {
		t.Fatalf("clock did not start on the click: live=%v blend=%v fade=%v now=%v", a.fadeLive, a.blendStart, a.backdropFadeAt, tt.Now())
	}
	if a.fullBM != nil || a.previewBM == nil {
		t.Fatalf("click frame waited for the full bitmap: full %v preview %v", a.fullBM != nil, a.previewBM != nil)
	}
	if a.look.ThemeMark != orig || a.look.SelectionRow != a.blendFrom.SelectionRow || a.look.TextPrimary != a.blendFrom.TextPrimary {
		t.Fatalf("mix left the start color on the click frame: mark %v row %v text %v", a.look.ThemeMark, a.look.SelectionRow, a.look.TextPrimary)
	}
	start := a.blendStart
	tt.Advance(16 * time.Millisecond)
	if dt := tt.Now().Sub(start); dt <= 0 || dt > 16*time.Millisecond {
		t.Fatalf("first follow-up frame at %s, want within 16ms and after the click", dt)
	}
	if a.look.ThemeMark == orig && bytes.Equal(pix, windowPatch(tt)) {
		t.Fatal("no control color or background pixel changed on the frame after the click, and no input was sent")
	}
	mixU := clockU(tt.Now(), a.blendStart, motionTheme)
	mixed := themes.MixColors(a.blendFrom.Colors, a.lookTarget.Colors, mixU)
	if a.look.SelectionRow != mixed.SelectionRow || a.look.TextPrimary != mixed.TextPrimary || a.look.ButtonPrimaryBg != mixed.ButtonPrimaryBg || a.look.LayerContent != mixed.LayerContent {
		t.Fatalf("theme colors are off the preview clock at u=%v", mixU)
	}
	if a.look.SelectionRow == a.blendFrom.SelectionRow {
		t.Fatal("selected row did not move on the frame after the click")
	}
	var prev time.Time
	for i := 0; i < 20 && !a.blendStart.IsZero(); i++ {
		prev = tt.Now()
		tt.Advance(16 * time.Millisecond)
		if !tt.Now().After(prev) {
			t.Fatal("control mix stalled with no input")
		}
	}
	done := tt.Now().Sub(start)
	if !a.blendStart.IsZero() || done < motionTheme || done >= motionTheme+16*time.Millisecond || prev.Sub(start) >= motionTheme {
		t.Fatalf("control mix finished at %s (frame before %s); want the first frame at or after %s", done, prev.Sub(start), motionTheme)
	}
	if !backdropAdopted(a) && (!a.fadeLive || a.previewBM == nil) {
		t.Fatal("the 150ms clock ended without the preview still up or the full bitmap adopted")
	}
	if a.look.ThemeMark != a.lookTarget.ThemeMark {
		t.Fatalf("palette %v did not land on %v", a.look.ThemeMark, a.lookTarget.ThemeMark)
	}
	for i := 0; i < 90 && a.themeMoving(); i++ {
		prev := tt.Now()
		tt.Advance(16 * time.Millisecond)
		if !tt.Now().After(prev) {
			t.Fatalf("fade stalled at %v (live %v full %v veil stand-in %v)", tt.Now(), a.fadeLive, a.fullBM != nil, a.veilStandIn)
		}
	}
	if a.themeMoving() {
		t.Fatalf("fade did not finish with no input (live %v full %v stand-in %v fresh %v geoms %d)", a.fadeLive, a.fullBM != nil, a.veilStandIn, a.veilFresh, len(a.veilGeoms))
	}
	if gradWakes.Load() <= wakes {
		t.Fatalf("off-thread build did not wake the window (%d -> %d)", wakes, gradWakes.Load())
	}
	assertFullResPinned(t, a)

	// A newer switch before time moves restarts from the palette still
	// on screen. At u = 0 that is the original theme.
	resetGradientCache()
	a = revisionChat(t)
	a.instant = false
	tt = ui.NewTester(a.view, 1024, 700)
	settleVeil(t, tt)
	orig = a.look.ThemeMark
	a.data.Theme = themes.ThemeTide
	tt.Frame()
	tide := a.lookTarget.ThemeMark
	a.data.Theme = themes.ThemeNeutral
	tt.Frame()
	if a.blendFrom.ThemeMark != orig || a.look.ThemeMark != orig {
		t.Fatalf("cancelled switch left the palette: from %v shown %v orig %v", a.blendFrom.ThemeMark, a.look.ThemeMark, orig)
	}
	if a.lookTarget.ThemeMark == tide || a.lookTarget.ThemeMark == orig {
		t.Fatalf("target %v is not the newer theme (tide %v orig %v)", a.lookTarget.ThemeMark, tide, orig)
	}
	if !a.fadeLive || !a.blendStart.Equal(tt.Now()) || !a.backdropFadeAt.Equal(a.blendStart) {
		t.Fatalf("cancelled switch did not restart the clock: live %v start %v fade %v now %v", a.fadeLive, a.blendStart, a.backdropFadeAt, tt.Now())
	}

	// Accent does not wait on a bitmap and does not build one.
	resetGradientCache()
	a = revisionChat(t)
	a.instant = false
	tt = ui.NewTester(a.view, 1024, 700)
	settleVeil(t, tt)
	before := gradPublished.Load()
	purple, ok := themes.NamedAccent("purple", false)
	if !ok {
		t.Fatal("purple accent")
	}
	a.shotAccent = purple
	tt.Frame()
	if a.fadeLive || !a.blendStart.Equal(tt.Now()) {
		t.Fatalf("accent mix did not start immediately: live=%v start=%v now=%v", a.fadeLive, a.blendStart, tt.Now())
	}
	mark := a.look.ThemeMark
	tt.Advance(16 * time.Millisecond)
	if a.look.AccentDefault == a.blendFrom.AccentDefault {
		t.Fatal("accent color did not move on the frame after the click")
	}
	if a.look.ThemeMark != mark {
		t.Fatalf("accent moved the theme mark: %v -> %v", mark, a.look.ThemeMark)
	}
	awaitGradients()
	if gradPublished.Load() != before {
		t.Fatalf("accent rebuilt the background (%d -> %d)", before, gradPublished.Load())
	}

	// Explicit dark starts the same clock on the click frame.
	resetGradientCache()
	a = revisionChat(t)
	a.instant = false
	tt = ui.NewTester(a.view, 1024, 700)
	settleVeil(t, tt)
	a.data.Appearance = appearanceDark
	tt.Frame()
	if !a.lookTarget.Dark || !a.fadeLive || !a.blendStart.Equal(tt.Now()) || !a.backdropFadeAt.Equal(a.blendStart) {
		t.Fatalf("dark appearance did not start on the click: dark %v live %v blend %v fade %v now %v", a.lookTarget.Dark, a.fadeLive, a.blendStart, a.backdropFadeAt, tt.Now())
	}

	// Follow system: the host appearance changes with no theme id change.
	resetGradientCache()
	a = revisionChat(t)
	a.instant = false
	a.data.Appearance = appearanceSystem
	tt = ui.NewTester(a.view, 1024, 700)
	settleVeil(t, tt)
	tt.SetDark(true)
	if !a.lookTarget.Dark || !a.fadeLive || !a.blendStart.Equal(tt.Now()) || !a.backdropFadeAt.Equal(a.blendStart) {
		t.Fatalf("follow-system did not start on the system change: dark %v live %v blend %v fade %v now %v", a.lookTarget.Dark, a.fadeLive, a.blendStart, a.backdropFadeAt, tt.Now())
	}

	// Explicit light does not follow a dark system, and switching back to
	// light from an explicit dark starts immediately too.
	resetGradientCache()
	a = revisionChat(t)
	a.instant = false
	a.data.Appearance = appearanceDark
	tt = ui.NewTester(a.view, 1024, 700)
	settleVeil(t, tt)
	a.data.Appearance = appearanceLight
	tt.Frame()
	if a.lookTarget.Dark || !a.fadeLive || !a.blendStart.Equal(tt.Now()) || !a.backdropFadeAt.Equal(a.blendStart) {
		t.Fatalf("light appearance did not start on the click: dark %v live %v blend %v fade %v now %v", a.lookTarget.Dark, a.fadeLive, a.blendStart, a.backdropFadeAt, tt.Now())
	}
}

func windowPatch(tt *ui.Tester) []byte {
	img := tt.Image()
	b := img.Bounds()
	x0 := b.Max.X - 16
	y0 := b.Min.Y + 4
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	var buf []byte
	for y := y0; y < y0+8 && y < b.Max.Y; y++ {
		row := img.Pix[y*img.Stride:]
		for x := x0; x < b.Max.X; x++ {
			o := x * 4
			buf = append(buf, row[o], row[o+1], row[o+2])
		}
	}
	return buf
}

func assertFullResPinned(t *testing.T, a *App) {
	t.Helper()
	e := cachedGradient(a.lookTarget, a.winPW, a.winPH)
	if e == nil || e.key.w != a.winPW || e.key.h != a.winPH {
		t.Fatalf("full-res bitmap missing at %dx%d", a.winPW, a.winPH)
	}
	cacheMu.Lock()
	st := sizes[[2]int{a.winPW, a.winPH}]
	cacheMu.Unlock()
	if st == nil || st.shown != e || st.from != nil {
		t.Fatal("dithered full-res bitmap is not the one left on screen")
	}
}

func TestVeilScrollStreamPixelsChange(t *testing.T) {
	resetGradientCache()
	a := revisionChat(t)
	a.instant = false
	// The token has to be on screen this frame. The default reveal
	// would still be catching up.
	a.md.Reveal = &markdown.Reveal{Unit: markdown.AtOnce, Fade: -1}
	tt := ui.NewTester(a.view, 1100, 760)
	settleVeil(t, tt)

	builds := veilBuilds.Load()
	shadows := shadowBuilds.Load()
	before := fadeSample(t, tt)
	conv := a.active()
	msg := &conv.Messages[len(conv.Messages)-1]
	msg.Content += " streaming-token"
	msg.Done = false
	a.streams[conv.ID] = &convStream{gen: 1, cancel: func() {}}
	tt.Frame()
	streamed := fadeSample(t, tt)
	if veilBuilds.Load() != builds {
		t.Fatalf("streaming regenerated the veil: %d -> %d", builds, veilBuilds.Load())
	}
	if shadowBuilds.Load() != shadows {
		t.Fatalf("streaming regenerated a shadow: %d -> %d", shadows, shadowBuilds.Load())
	}
	if bytes.Equal(before, streamed) {
		t.Fatal("streaming token left the fade-region pixels unchanged")
	}

	builds = veilBuilds.Load()
	shadows = shadowBuilds.Load()
	again := fadeSample(t, tt)
	anchor := fadeAnchor(t, tt)
	tt.Scroll(anchor.X+12, anchor.Y+8, 0, -160)
	scrolled := fadeSample(t, tt)
	if veilBuilds.Load() != builds {
		t.Fatalf("scroll regenerated the veil: %d -> %d", builds, veilBuilds.Load())
	}
	if shadowBuilds.Load() != shadows {
		t.Fatalf("scroll regenerated a shadow: %d -> %d", shadows, shadowBuilds.Load())
	}
	if bytes.Equal(again, scrolled) {
		t.Fatal("scroll left the fade-region pixels unchanged")
	}
}

func TestVeilFrameBound(t *testing.T) {
	resetGradientCache()
	smallIdle, smallScroll, smallStream := measureVeilFrames(t, 1440, 900, 2)
	resetGradientCache()
	bigIdle, bigScroll, bigStream := measureVeilFrames(t, 2880, 1800, 1)
	if bigIdle.veil > veilFrameBound || bigScroll.veil > veilFrameBound || bigStream.veil > veilFrameBound {
		t.Fatalf("veil at 2880×1800 exceeds %s: idle %s scroll %s stream %s", veilFrameBound, bigIdle.veil, bigScroll.veil, bigStream.veil)
	}
	if bigIdle.wall > idleFrameBound {
		t.Fatalf("idle frame %s exceeds %s", bigIdle.wall, idleFrameBound)
	}
	if bigScroll.wall > scrollFrameBound {
		t.Fatalf("scroll frame %s exceeds %s", bigScroll.wall, scrollFrameBound)
	}
	if bigIdle.builds != 0 || bigScroll.builds != 0 || bigStream.builds != 0 {
		t.Fatalf("veil rebuilt during the measured frames: idle %d scroll %d stream %d", bigIdle.builds, bigScroll.builds, bigStream.builds)
	}

	section := fmt.Sprintf(`
composer veil
The veil is the content card across the full card width. Strength follows the SPEC curve (clear 28pt above the composer, 50%% from the composer's top until 10pt above the card bottom, 70%% at the card bottom) and does not change across a row. It is one Painter.ImageRegion (scene.OpImage, source-over) over the live transcript. Content is not in the cache. A miss builds off the frame when the background bitmap changes; a hit is a blit. The crossfade uses the background's clock and keeps the veil already on screen as its base.

Before, on this host (per-pixel composite on the frame goroutine, both fade and pad every frame):
1440×900@2x idle wall 47.936ms veil 22.850ms calls 2 builds 2
1440×900@2x scroll wall 54.528ms veil 22.711ms calls 2 builds 2
1440×900@2x stream wall 118.859ms veil 23.305ms calls 2 builds 2
2880×1800@1x idle wall 31.825ms veil 10.428ms calls 2 builds 2
2880×1800@1x scroll wall 32.857ms veil 11.079ms calls 2 builds 2
2880×1800@1x stream wall 105.930ms veil 10.929ms calls 2 builds 2

After:
1440×900@2x idle wall %s veil %s calls %d builds %d
1440×900@2x scroll wall %s veil %s calls %d builds %d
1440×900@2x stream wall %s veil %s calls %d builds %d
2880×1800@1x idle wall %s veil %s calls %d builds %d
2880×1800@1x scroll wall %s veil %s calls %d builds %d
2880×1800@1x stream wall %s veil %s calls %d builds %d

Hard bounds at 2880×1800 (the build fails when a measured frame exceeds one):
veil, each of idle / scroll / stream: %s
idle frame: %s
scroll frame: %s
The streaming row above is one frame: one veil blit, not the tester settle loop. Its wall time is that frame's present. On this fixture the present resamples a large band of the gradient, so the wall is not the steady per-token cost. That distribution is the next section. Veil regenerations while scrolling or streaming: 0.

Theme clock: the control mix, the sidebar selection, and the preview crossfade start in the click frame (blendStart == backdropFadeAt) and the colors become visible on the next frame. Both last %s. The preview is an undithered 1/%d bitmap drawn with linear filtering. The dithered full-resolution bitmap crossfades in over at most %s once the worker wakes the window, with no further input. A switch that cancels an in-flight build restarts from the palette still on screen. Accent-only changes start the %s mix immediately and do not rebuild a bitmap. Light, dark, and follow-system use the same immediate clock. TestThemeFadeClockMatchesControls.
`,
		smallIdle.wall, smallIdle.veil, smallIdle.calls, smallIdle.builds,
		smallScroll.wall, smallScroll.veil, smallScroll.calls, smallScroll.builds,
		smallStream.wall, smallStream.veil, smallStream.calls, smallStream.builds,
		bigIdle.wall, bigIdle.veil, bigIdle.calls, bigIdle.builds,
		bigScroll.wall, bigScroll.veil, bigScroll.calls, bigScroll.builds,
		bigStream.wall, bigStream.veil, bigStream.calls, bigStream.builds,
		veilFrameBound, idleFrameBound, scrollFrameBound,
		motionTheme, previewDiv, motionTheme, motionTheme,
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

type frameCost struct {
	wall, veil time.Duration
	calls      int64
	builds     int64
}

func measureVeilFrames(t *testing.T, w, h int, scale float32) (idle, scroll, stream frameCost) {
	t.Helper()
	a := revisionChat(t)
	a.instant = false
	tt := ui.NewTester(a.view, w, h)
	tt.SetScale(scale)
	settleVeil(t, tt)

	resetVeilStats()
	idleStart := time.Now()
	tt.Frame()
	idle = frameCost{time.Since(idleStart), time.Duration(veilNanos.Load()), veilCalls.Load(), veilBuilds.Load()}

	anchor := fadeAnchor(t, tt)
	resetVeilStats()
	scrollStart := time.Now()
	tt.Scroll(anchor.X+12, anchor.Y+8, 0, -80)
	scroll = frameCost{time.Since(scrollStart), time.Duration(veilNanos.Load()), veilCalls.Load(), veilBuilds.Load()}

	resetVeilStats()
	conv := a.active()
	msg := &conv.Messages[len(conv.Messages)-1]
	msg.Content += " token"
	msg.Done = false
	a.streams[conv.ID] = &convStream{gen: 1, cancel: func() {}}
	// One build. Tester.Frame would also run the follow-up frames the
	// end-follow glide and the scrollbar ask for, and the clock does not
	// move during that settle, so the glide would never finish.
	a.md.Reveal = &markdown.Reveal{Unit: markdown.AtOnce, Fade: -1}
	streamStart := time.Now()
	runOneFrame(tt)
	stream = frameCost{time.Since(streamStart), time.Duration(veilNanos.Load()), veilCalls.Load(), veilBuilds.Load()}
	return idle, scroll, stream
}

func settleVeil(t *testing.T, tt *ui.Tester) {
	t.Helper()
	saw := false
	for i := 0; i < 60; i++ {
		tt.Frame()
		awaitGradients()
		awaitVeils()
		awaitShadows()
		if veilBuilds.Load() > 0 {
			saw = true
		}
		if !saw {
			continue
		}
		resetVeilStats()
		tt.Frame()
		awaitGradients()
		awaitVeils()
		awaitShadows()
		if veilBuilds.Load() == 0 && veilCalls.Load() >= 1 {
			return
		}
	}
	t.Fatal("veil never settled on a cached blit")
}

func fadeAnchor(t *testing.T, tt *ui.Tester) ui.Rect {
	t.Helper()
	anchor, ok := tt.Find("Thought for 12s")
	if !ok {
		anchor, ok = tt.Find("Tie the request to a context")
	}
	if !ok {
		t.Fatalf("no scroll anchor in %q", tt.Texts())
	}
	return anchor
}

func fadeSample(t *testing.T, tt *ui.Tester) []byte {
	t.Helper()
	r, ok := tt.Find("composer fade")
	if !ok {
		t.Fatal("composer fade is not in the frame")
	}
	img := tt.Image()
	b := img.Bounds()
	scale := 1
	if b.Dx() > 0 && int(r.W) > 0 && b.Dx() > int(r.W)*2 {
		scale = 2
	}
	x0 := int(r.X)*scale + 40*scale
	x1 := int(r.X+r.W)*scale - 40*scale
	// The list's bottom padding leaves the last line in the lower part of
	// the 28pt fade, so the sample covers that whole band.
	y0 := int(r.Y)*scale + 4*scale
	y1 := y0 + 36*scale
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	if x1 <= x0 || y1 <= y0 {
		t.Fatalf("fade sample empty: rect %v image %v", r, b)
	}
	var buf []byte
	for y := y0; y < y1; y += scale {
		row := img.Pix[y*img.Stride:]
		for x := x0; x < x1; x += scale {
			o := x * 4
			buf = append(buf, row[o], row[o+1], row[o+2], row[o+3])
		}
	}
	return buf
}
