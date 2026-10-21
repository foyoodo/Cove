package main

import (
	"image"
	"image/color"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"cove/themes"

	"github.com/egoist/mygo/ui"
)

// The window bitmap is a vertical OKLab ramp blended with a horizontal
// tint. Both are separable, so the ramp is one sample per row and the
// tint weight is one sample per column. Pixels are a lookup plus a blend,
// quantized with a 64×64 blue-noise tile, then clamped so neighbors stay
// within one code.
//
// Generation runs off the frame goroutine. A theme or appearance change
// starts its 150ms crossfade in the click frame: the last bitmap fades
// toward an undithered preview, stretched with linear filtering, and
// the control mix runs on that same clock. The preview is the smooth ramp
// at 1/8 with no blue noise and no run-cap nudge. A nudge at that scale
// upscales into a grid or a soft step, which reads as blur. Bilinear of
// the smooth ramp stays within one code of the full-resolution bitmap.
// When the dithered full-size bitmap lands, it crossfades in over at most
// 150ms. The worker wakes the window, so none of this waits for input.
// Accent changes do not rebuild the bitmap. A resize keeps stretching
// the previous one.

const (
	srgbN = 65536

	// gradFade is the shared theme transition. The control mix and the
	// preview crossfade both last this long and start together in the
	// click frame. The preview-to-full-resolution crossfade is at most
	// this long as well.
	gradFade = 150 * time.Millisecond
	// fullBand is the longest identical run the dithered full-resolution
	// bitmap may keep. previewDiv is the linear scale of the immediate
	// preview: the smooth ramp at 1/8, no dither, no run cap. Bilinear
	// upscale of that ramp stays within one code of the full bitmap.
	// previewSyncLimit is how long that preview may take on the click frame
	// before the rest are built ahead of time.
	fullBand         = 24
	previewDiv       = 8
	previewSyncLimit = 4 * time.Millisecond
	// gradSettle is how long a size must hold still before a new bitmap
	// is built. Live resize keeps stretching the previous one.
	gradSettle = 120 * time.Millisecond
	// themeSwitchMainBound is the hard cap on main-thread work for a
	// theme switch at a maximized window (presentGradient). The build
	// fails when a switch exceeds it.
	themeSwitchMainBound = 16 * time.Millisecond

	gradCacheCap = 6
)

var (
	encLUT [srgbN]float32 // encodeSRGB(i/(srgbN-1)) * 255

	gradSync   atomic.Bool
	gradMotion atomic.Bool
	gradWake   atomic.Value // func()
	// gradientWakeHook runs beside Window.Invalidate. Headless tests have
	// no window, so this is how they see the worker ask for a frame.
	gradientWakeHook atomic.Value // func()
	// themeInvalidateHook runs beside the invalidation a theme or
	// appearance change posts itself. A bitmap already in cache never
	// reaches the worker, so wakeGradient does not run, and headless
	// tests have no window.
	themeInvalidateHook atomic.Value // func()

	cacheMu   sync.Mutex
	entries   = map[gradKey]*gradEntry{}
	sizes     = map[[2]int]*sizeLive{}
	lastReady *gradEntry

	jobMu         sync.Mutex
	jobDone       *sync.Cond
	pending       = map[gradKey]preparedGrad{}
	pendingResize = map[gradKey]bool{}
	epochs        = map[[2]int]uint64{}
	jobRunning    bool
	// inflight is the bitmap the worker is rendering. A repeat request
	// for that same key must not bump the epoch, or the 16ms clock would
	// abort the build before it could publish and wake the window.
	inflight   gradKey
	inflightEp uint64
	inflightOn bool

	gradPublished atomic.Int64
	gradAborted   atomic.Int64
	gradWakes     atomic.Int64
	previewLast   atomic.Int64
	gradSeq       atomic.Uint64
	gradLastGen   atomic.Int64
	gradLastRamp  atomic.Int64
	gradLastQuant atomic.Int64
	gradLastLimit atomic.Int64
	gradMainLast  atomic.Int64
	gradMainMax   atomic.Int64

	gateMu         sync.Mutex
	gradRenderGate func()

	previewMu   sync.Mutex
	previews    = map[previewKey]*ui.Bitmap{}
	previewSeq  atomic.Uint64
	previewSlow atomic.Bool
	previewOnce sync.Once
)

// previewKey identifies an undithered 1/8 preview. It is not the
// full-resolution cache key. The preview is filtered, not dithered.
type previewKey struct {
	w, h               int
	top, mid, low, bot uint32
	tint               uint32
}

type gradKey struct {
	w, h               int
	top, mid, low, bot uint32
	tint               uint32
}

type gradEntry struct {
	key  gradKey
	prep preparedGrad
	img  *image.NRGBA
	bm   *ui.Bitmap
	seq  uint64
}

type sizeLive struct {
	shown, from *gradEntry
	fadeAt      time.Time
	pendingAt   time.Time
	pending     bool
}

func init() {
	buildSRGB()
	jobDone = sync.NewCond(&jobMu)
	ensurePreviewPolicy()
}

func buildSRGB() {
	for i := 0; i < srgbN; i++ {
		lin := float64(i) / float64(srgbN-1)
		encLUT[i] = float32(encodeSRGB(lin) * 255)
	}
}

// encodeCode is encodeSRGB(lin)*255. Knots are exact. Between them the
// curve is interpolated, and a value that sits on a quantization boundary
// is recomputed so the code matches the direct pow.
func encodeCode(lin float64) float64 {
	if lin <= 0 {
		return 0
	}
	if lin >= 1 {
		return 255
	}
	pos := lin * float64(srgbN-1)
	i := int(pos)
	if i >= srgbN-1 {
		return float64(encLUT[srgbN-1])
	}
	f := pos - float64(i)
	return float64(encLUT[i])*(1-f) + float64(encLUT[i+1])*f
}

func quantLin(lin, noise float64, dither bool) int {
	n := encodeCode(lin)
	if dither {
		n += noise
	} else {
		n += 0.5
	}
	if n <= 0 {
		return 0
	}
	if n >= 255 {
		return 255
	}
	return int(n)
}

func setGradientMode(syncPaint, motion bool, wake func()) {
	gradSync.Store(syncPaint)
	gradMotion.Store(motion)
	if wake != nil {
		gradWake.Store(wake)
	}
}

// wakeGradient asks the windows for a frame. The gradient and veil
// workers call it after a bitmap is published, including when no pointer
// event is coming. Window.Invalidate is safe from that goroutine: it
// posts to the main thread, which requests a frame and wakes the run loop.
func wakeGradient() {
	gradWakes.Add(1)
	v := gradWake.Load()
	if v == nil {
		return
	}
	v.(func())()
}

func noteThemeInvalidate() {
	v := themeInvalidateHook.Load()
	if v == nil {
		return
	}
	if hook := v.(func()); hook != nil {
		hook()
	}
}

func noteMain(d time.Duration) {
	ns := d.Nanoseconds()
	gradMainLast.Store(ns)
	for {
		old := gradMainMax.Load()
		if ns <= old || gradMainMax.CompareAndSwap(old, ns) {
			return
		}
	}
}

func gradKeyOf(w, h int, p preparedGrad) gradKey {
	return gradKey{w: w, h: h, top: p.top, mid: p.mid, low: p.low, bot: p.bot, tint: p.tintPacked}
}

// resetGradientCache drops bitmaps and counters. Tests call it so a
// measurement is not a hit on another test's image.
func resetGradientCache() {
	awaitGradients()
	cacheMu.Lock()
	entries = map[gradKey]*gradEntry{}
	sizes = map[[2]int]*sizeLive{}
	lastReady = nil
	cacheMu.Unlock()
	jobMu.Lock()
	pending = map[gradKey]preparedGrad{}
	pendingResize = map[gradKey]bool{}
	epochs = map[[2]int]uint64{}
	inflightOn = false
	jobMu.Unlock()
	gradPublished.Store(0)
	gradAborted.Store(0)
	gradWakes.Store(0)
	gradLastGen.Store(0)
	gradMainLast.Store(0)
	gradMainMax.Store(0)
	previewLast.Store(0)
	previewMu.Lock()
	previews = map[previewKey]*ui.Bitmap{}
	previewMu.Unlock()
	resetVeilCache()
	resetShadowCache()
	// A cold preview that misses previewSyncLimit is slower than a frame. Startup
	// builds them ahead of time; tests that drop the cache do too, so a
	// measured click is the cache hit a running app takes.
	if previewSlow.Load() {
		prebuildPreviews()
	}
}

func awaitGradients() {
	jobMu.Lock()
	for jobRunning || len(pending) > 0 {
		jobDone.Wait()
	}
	jobMu.Unlock()
}

func renderGradient(look Theme, w, h int, dither bool) *image.NRGBA {
	run := 0
	if dither {
		run = fullBand
	}
	img, _ := renderPrepared(prepareWindowGrad(look), w, h, dither, run, nil)
	if img == nil {
		return image.NewNRGBA(image.Rect(0, 0, max(w, 1), max(h, 1)))
	}
	return img
}

func renderPrepared(g preparedGrad, w, h int, dither bool, run int, abort func() bool) (*image.NRGBA, bool) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	// The test gate blocks a full-resolution build. A preview is small and
	// must still be able to finish while that build is held.
	if dither {
		renderGate()
	}
	if abort != nil && abort() {
		return nil, true
	}
	t0 := time.Now()
	rows, cols, tint := buildRamp(g, w, h)
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	t1 := time.Now()
	if fillQuant(img, rows, cols, tint, dither, abort) {
		return nil, true
	}
	t2 := time.Now()
	if run > 0 && limitInPlace(img, rows, cols, tint, run, abort) {
		return nil, true
	}
	gradLastRamp.Store(int64(t1.Sub(t0)))
	gradLastQuant.Store(int64(t2.Sub(t1)))
	gradLastLimit.Store(int64(time.Since(t2)))
	return img, false
}

func renderGate() {
	gateMu.Lock()
	g := gradRenderGate
	gateMu.Unlock()
	if g != nil {
		g()
	}
}

func buildRamp(g preparedGrad, w, h int) (rows [][3]float64, cols []float64, tint [3]float64) {
	rows = make([][3]float64, h)
	fh := float64(h)
	for y := 0; y < h; y++ {
		t := (float64(y) + 0.5) / fh
		lab := oklab{g.l.eval(t), g.a.eval(t), g.b.eval(t)}
		r, gc, b := oklabLinear(lab)
		rows[y] = [3]float64{r, gc, b}
	}
	cols = make([]float64, w)
	fw := float64(w)
	span := g.tintEnd - g.tintStart
	for x := 0; x < w; x++ {
		xx := (float64(x) + 0.5) / fw
		k := 0.0
		if span > 0 {
			k = smoothstep01((xx - g.tintStart) / span)
		}
		cols[x] = g.tintA * k
	}
	tint = [3]float64{g.tint[0], g.tint[1], g.tint[2]}
	return rows, cols, tint
}

// gradWorkers is the parallelism for one full-resolution pass. A preview
// stays on the caller. A full-size pass leaves one processor idle so the
// frame that requested the bitmap is not stuck behind a 10ms preemption
// while every P runs the pixel loop. That stall is what pushed a switch
// frame past the 16ms cap.
func gradWorkers(limit int) int {
	n := runtime.GOMAXPROCS(0)
	if n > 1 {
		n--
	}
	if n > 8 {
		n = 8
	}
	if limit > 0 && n > limit {
		n = limit
	}
	if n < 1 {
		n = 1
	}
	return n
}

// fillQuant writes the unclamped quantization. Dither uses the 64×64
// blue-noise tile. Undithered rounds to nearest.
func fillQuant(dst *image.NRGBA, rows [][3]float64, cols []float64, tint [3]float64, dither bool, abort func() bool) bool {
	h := dst.Rect.Dy()
	w := dst.Rect.Dx()
	n := 1
	// Previews are about 1/8 and run on the click frame. Parallelism there
	// just waits for a P the full-resolution build is already using.
	if w*h >= 512*512 {
		n = gradWorkers(h)
	}
	var aborted atomic.Bool
	if n == 1 {
		quantRows(dst, rows, cols, tint, dither, 0, h, abort, &aborted)
		return aborted.Load()
	}
	var wg sync.WaitGroup
	band := (h + n - 1) / n
	for wi := 0; wi < n; wi++ {
		y0 := wi * band
		if y0 >= h {
			break
		}
		y1 := y0 + band
		if y1 > h {
			y1 = h
		}
		wg.Add(1)
		go func(y0, y1 int) {
			defer wg.Done()
			quantRows(dst, rows, cols, tint, dither, y0, y1, abort, &aborted)
		}(y0, y1)
	}
	wg.Wait()
	return aborted.Load()
}

func quantRows(dst *image.NRGBA, rows [][3]float64, cols []float64, tint [3]float64, dither bool, y0, y1 int, abort func() bool, aborted *atomic.Bool) {
	w := dst.Rect.Dx()
	tr, tg, tb := tint[0], tint[1], tint[2]
	if dither {
		quantDither(dst, rows, cols, tr, tg, tb, w, y0, y1, abort, aborted)
		return
	}
	for y := y0; y < y1; y++ {
		if aborted.Load() {
			return
		}
		if y&31 == 0 && abort != nil && abort() {
			aborted.Store(true)
			return
		}
		br, bg, bb := rows[y][0], rows[y][1], rows[y][2]
		row := dst.Pix[y*dst.Stride : y*dst.Stride+w*4]
		for x := 0; x < w; x++ {
			a := cols[x]
			ia := 1 - a
			o := x * 4
			row[o] = uint8(quantLin(br*ia+tr*a, 0, false))
			row[o+1] = uint8(quantLin(bg*ia+tg*a, 0, false))
			row[o+2] = uint8(quantLin(bb*ia+tb*a, 0, false))
			row[o+3] = 255
		}
	}
}

func quantDither(dst *image.NRGBA, rows [][3]float64, cols []float64, tr, tg, tb float64, w, y0, y1 int, abort func() bool, aborted *atomic.Bool) {
	stride := dst.Stride
	pix := dst.Pix
	for y := y0; y < y1; y++ {
		if aborted.Load() {
			return
		}
		if y&31 == 0 && abort != nil && abort() {
			aborted.Store(true)
			return
		}
		br, bg, bb := rows[y][0], rows[y][1], rows[y][2]
		row := pix[y*stride : y*stride+w*4]
		yi := (y & 63) << 6
		for x := 0; x < w; x++ {
			a := cols[x]
			ia := 1 - a
			noise := float64(blueNoise[yi|(x&63)]) * (1.0 / 255)
			o := x * 4
			row[o] = uint8(quantLin(br*ia+tr*a, noise, true))
			row[o+1] = uint8(quantLin(bg*ia+tg*a, noise, true))
			row[o+2] = uint8(quantLin(bb*ia+tb*a, noise, true))
			row[o+3] = 255
		}
	}
}

// limitInPlace pulls each channel to within one code of the pixel on its
// left and the pixel above, then breaks an identical run that would pass
// run by nudging one channel toward the float sample. The full-resolution
// bitmap passes fullBand. Blue noise reaches 1, so neighbors can quantize
// two codes apart where the ramp crosses an integer; the clamp closes that gap.
func limitInPlace(img *image.NRGBA, rows [][3]float64, cols []float64, tint [3]float64, run int, abort func() bool) bool {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	if w < 1 || h < 1 {
		return false
	}
	n := gradWorkers(w / 160)
	if n < 2 {
		return limitInPlaceSerial(img, rows, cols, tint, run, abort)
	}
	return limitInPlaceStrips(img, rows, cols, tint, run, abort, n)
}

// limitInPlaceStrips splits the bitmap into vertical strips. A pixel still
// depends on the one to its left and the one above, so each strip waits
// for the strip on its left to finish the same row, then runs that row.
// The hand-off carries the horizontal run length, so a run that crosses
// a strip boundary nudges on the same pixel the serial pass would.
func limitInPlaceStrips(img *image.NRGBA, rows [][3]float64, cols []float64, tint [3]float64, run int, abort func() bool, n int) bool {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	type seam struct {
		r, g, b int
		hRun    int
	}
	seams := make([]seam, n*h)
	ready := make([]atomic.Uint32, n*h)
	var aborted atomic.Bool
	var wg sync.WaitGroup
	base, extra, x := w/n, w%n, 0
	for s := 0; s < n; s++ {
		ww := base
		if s < extra {
			ww++
		}
		x0, x1 := x, x+ww
		x = x1
		wg.Add(1)
		go func(s, x0, x1 int) {
			defer wg.Done()
			publish := func(y int, st seam) {
				seams[s*h+y] = st
				ready[s*h+y].Store(1)
			}
			bail := func(from int) {
				for y := from; y < h; y++ {
					if ready[s*h+y].Load() == 0 {
						publish(y, seam{})
					}
				}
			}
			vRun := make([]int, w)
			tr, tg, tb := tint[0], tint[1], tint[2]
			pix := img.Pix
			stride := img.Stride
			for y := 0; y < h; y++ {
				var left seam
				if x0 > 0 {
					for ready[(s-1)*h+y].Load() == 0 {
						if aborted.Load() {
							bail(y)
							return
						}
						runtime.Gosched()
					}
					left = seams[(s-1)*h+y]
				}
				if aborted.Load() || (y&31 == 0 && abort != nil && abort()) {
					aborted.Store(true)
					bail(y)
					return
				}
				row := pix[y*stride : y*stride+w*4]
				br, bg, bb := rows[y][0], rows[y][1], rows[y][2]
				var rr, gg, bb2, hRun int
				if y == 0 {
					rr, gg, bb2, hRun = limitSpanY0(row, x0, x1, vRun, cols, br, bg, bb, tr, tg, tb, run, left.r, left.g, left.b, left.hRun)
				} else {
					up := pix[(y-1)*stride : (y-1)*stride+w*4]
					rr, gg, bb2, hRun = limitSpanY(row, up, x0, x1, vRun, cols, br, bg, bb, tr, tg, tb, run, left.r, left.g, left.b, left.hRun)
				}
				publish(y, seam{rr, gg, bb2, hRun})
			}
		}(s, x0, x1)
	}
	wg.Wait()
	return aborted.Load()
}

func limitInPlaceSerial(img *image.NRGBA, rows [][3]float64, cols []float64, tint [3]float64, run int, abort func() bool) bool {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	pix := img.Pix
	stride := img.Stride
	vRun := make([]int, w)
	tr, tg, tb := tint[0], tint[1], tint[2]
	for y := 0; y < h; y++ {
		if y&31 == 0 && abort != nil && abort() {
			return true
		}
		row := pix[y*stride : y*stride+w*4]
		br, bg, bb := rows[y][0], rows[y][1], rows[y][2]
		if y == 0 {
			limitRowH(row, w, vRun, cols, br, bg, bb, tr, tg, tb, run)
			continue
		}
		up := pix[(y-1)*stride : (y-1)*stride+w*4]
		hRun := 0
		// First pixel has no left neighbor.
		r0 := clampOne(int(row[0]), int(up[0]))
		g0 := clampOne(int(row[1]), int(up[1]))
		b0 := clampOne(int(row[2]), int(up[2]))
		if vRun[0] >= run && r0 == int(up[0]) && g0 == int(up[1]) && b0 == int(up[2]) {
			r0, g0, b0 = nudgeAt(r0, g0, b0, 0, 0, 0, int(up[0]), int(up[1]), int(up[2]), false, true, 0, vRun[0], cols[0], br, bg, bb, tr, tg, tb, run)
		}
		row[0], row[1], row[2] = uint8(r0), uint8(g0), uint8(b0)
		if r0 == int(up[0]) && g0 == int(up[1]) && b0 == int(up[2]) {
			vRun[0]++
		} else {
			vRun[0] = 1
		}
		hRun = 1
		lr, lg, lb := r0, g0, b0
		for x := 1; x < w; x++ {
			o := x * 4
			ur := int(up[o])
			ug := int(up[o+1])
			ub := int(up[o+2])
			r := clampBoth(int(row[o]), lr, ur)
			g := clampBoth(int(row[o+1]), lg, ug)
			b := clampBoth(int(row[o+2]), lb, ub)
			if (hRun >= run && r == lr && g == lg && b == lb) || (vRun[x] >= run && r == ur && g == ug && b == ub) {
				r, g, b = nudgeAt(r, g, b, lr, lg, lb, ur, ug, ub, true, true, hRun, vRun[x], cols[x], br, bg, bb, tr, tg, tb, run)
			}
			if uint8(r) != row[o] || uint8(g) != row[o+1] || uint8(b) != row[o+2] {
				row[o] = uint8(r)
				row[o+1] = uint8(g)
				row[o+2] = uint8(b)
			}
			if r == lr && g == lg && b == lb {
				hRun++
			} else {
				hRun = 1
			}
			if r == ur && g == ug && b == ub {
				vRun[x]++
			} else {
				vRun[x] = 1
			}
			lr, lg, lb = r, g, b
		}
	}
	return false
}

// limitRowH is the first row: no pixel above it, so only a horizontal run
// can force a nudge. vRun is seeded to 1, matching pixelRun on the next row.
func limitRowH(row []byte, w int, vRun []int, cols []float64, br, bg, bb, tr, tg, tb float64, run int) {
	hRun := 0
	lr, lg, lb := 0, 0, 0
	for x := 0; x < w; x++ {
		o := x * 4
		r := int(row[o])
		g := int(row[o+1])
		b := int(row[o+2])
		hasL := x > 0
		if hasL && (outOf(r, lr) || outOf(g, lg) || outOf(b, lb)) {
			r = clampOne(r, lr)
			g = clampOne(g, lg)
			b = clampOne(b, lb)
		}
		if hasL && hRun >= run && r == lr && g == lg && b == lb {
			r, g, b = nudgeAt(r, g, b, lr, lg, lb, 0, 0, 0, true, false, hRun, 0, cols[x], br, bg, bb, tr, tg, tb, run)
		}
		row[o] = uint8(r)
		row[o+1] = uint8(g)
		row[o+2] = uint8(b)
		if hasL && r == lr && g == lg && b == lb {
			hRun++
		} else {
			hRun = 1
		}
		vRun[x] = 1
		lr, lg, lb = r, g, b
	}
}

// limitSpanY0 is one strip of the first row. left/hRun are the pixel and
// run length just before x0; they are zero when x0 is the row start.
func limitSpanY0(row []byte, x0, x1 int, vRun []int, cols []float64, br, bg, bb, tr, tg, tb float64, run, lr, lg, lb, hRun int) (int, int, int, int) {
	for x := x0; x < x1; x++ {
		o := x * 4
		r := int(row[o])
		g := int(row[o+1])
		b := int(row[o+2])
		hasL := x > 0
		if hasL && (outOf(r, lr) || outOf(g, lg) || outOf(b, lb)) {
			r = clampOne(r, lr)
			g = clampOne(g, lg)
			b = clampOne(b, lb)
		}
		if hasL && hRun >= run && r == lr && g == lg && b == lb {
			r, g, b = nudgeAt(r, g, b, lr, lg, lb, 0, 0, 0, true, false, hRun, 0, cols[x], br, bg, bb, tr, tg, tb, run)
		}
		row[o] = uint8(r)
		row[o+1] = uint8(g)
		row[o+2] = uint8(b)
		if hasL && r == lr && g == lg && b == lb {
			hRun++
		} else {
			hRun = 1
		}
		vRun[x] = 1
		lr, lg, lb = r, g, b
	}
	return lr, lg, lb, hRun
}

// limitSpanY is one strip of a later row. The pixel above is already this
// strip's previous row. left/hRun come from the strip on the left.
func limitSpanY(row, up []byte, x0, x1 int, vRun []int, cols []float64, br, bg, bb, tr, tg, tb float64, run, lr, lg, lb, hRun int) (int, int, int, int) {
	if x0 == 0 {
		r0 := clampOne(int(row[0]), int(up[0]))
		g0 := clampOne(int(row[1]), int(up[1]))
		b0 := clampOne(int(row[2]), int(up[2]))
		if vRun[0] >= run && r0 == int(up[0]) && g0 == int(up[1]) && b0 == int(up[2]) {
			r0, g0, b0 = nudgeAt(r0, g0, b0, 0, 0, 0, int(up[0]), int(up[1]), int(up[2]), false, true, 0, vRun[0], cols[0], br, bg, bb, tr, tg, tb, run)
		}
		row[0], row[1], row[2] = uint8(r0), uint8(g0), uint8(b0)
		if r0 == int(up[0]) && g0 == int(up[1]) && b0 == int(up[2]) {
			vRun[0]++
		} else {
			vRun[0] = 1
		}
		hRun = 1
		lr, lg, lb = r0, g0, b0
		x0 = 1
	}
	for x := x0; x < x1; x++ {
		o := x * 4
		ur := int(up[o])
		ug := int(up[o+1])
		ub := int(up[o+2])
		r := clampBoth(int(row[o]), lr, ur)
		g := clampBoth(int(row[o+1]), lg, ug)
		b := clampBoth(int(row[o+2]), lb, ub)
		if (hRun >= run && r == lr && g == lg && b == lb) || (vRun[x] >= run && r == ur && g == ug && b == ub) {
			r, g, b = nudgeAt(r, g, b, lr, lg, lb, ur, ug, ub, true, true, hRun, vRun[x], cols[x], br, bg, bb, tr, tg, tb, run)
		}
		if uint8(r) != row[o] || uint8(g) != row[o+1] || uint8(b) != row[o+2] {
			row[o] = uint8(r)
			row[o+1] = uint8(g)
			row[o+2] = uint8(b)
		}
		if r == lr && g == lg && b == lb {
			hRun++
		} else {
			hRun = 1
		}
		if r == ur && g == ug && b == ub {
			vRun[x]++
		} else {
			vRun[x] = 1
		}
		lr, lg, lb = r, g, b
	}
	return lr, lg, lb, hRun
}

func nudgeAt(r, g, b, lr, lg, lb, ur, ug, ub int, hasL, hasU bool, hRun, vRun int, a, br, bg, bb, tr, tg, tb float64, run int) (int, int, int) {
	ia := 1 - a
	ideal := [3]float64{
		encodeCode(br*ia + tr*a),
		encodeCode(bg*ia + tg*a),
		encodeCode(bb*ia + tb*a),
	}
	q := [3]int{r, g, b}
	left := [3]int{lr, lg, lb}
	up := [3]int{ur, ug, ub}
	lo, hi := bayerBounds(left, up, hasL, hasU)
	if moved, ok := nudgePixel(q, ideal, lo, hi, up, left, hasU && vRun >= run, hasL && hRun >= run); ok {
		return moved[0], moved[1], moved[2]
	}
	return r, g, b
}

// outOf reports a channel step larger than one code.
func outOf(q, n int) bool {
	d := q - n
	return uint(d+1) > 2
}

func clampOne(q, n int) int {
	if q > n+1 {
		return n + 1
	}
	if q < n-1 {
		return n - 1
	}
	return q
}

func clampBoth(q, left, up int) int {
	lo := max(left-1, up-1, 0)
	hi := min(left+1, up+1, 255)
	if hi < lo {
		hi = lo
	}
	return min(max(q, lo), hi)
}

func bayerBounds(left, up [3]int, hasL, hasU bool) (lo, hi [3]int) {
	for c := 0; c < 3; c++ {
		lo[c], hi[c] = 0, 255
		if hasL {
			lo[c], hi[c] = left[c]-1, left[c]+1
		}
		if hasU {
			if up[c]-1 > lo[c] {
				lo[c] = up[c] - 1
			}
			if up[c]+1 < hi[c] {
				hi[c] = up[c] + 1
			}
		}
		if lo[c] < 0 {
			lo[c] = 0
		}
		if hi[c] > 255 {
			hi[c] = 255
		}
		if hi[c] < lo[c] {
			hi[c] = lo[c]
		}
	}
	return lo, hi
}

func launchGrad(key gradKey, prep preparedGrad) {
	jobMu.Lock()
	defer jobMu.Unlock()
	cacheMu.Lock()
	_, have := entries[key]
	cacheMu.Unlock()
	if have {
		return
	}
	sz := [2]int{key.w, key.h}
	if _, already := pending[key]; already {
		return
	}
	if inflightOn && inflight == key && epochs[sz] == inflightEp {
		return
	}
	epochs[sz]++
	for k := range pending {
		if k.w == key.w && k.h == key.h {
			delete(pending, k)
			delete(pendingResize, k)
		}
	}
	pending[key] = prep
	if shownSizeMismatch(key) {
		pendingResize[key] = true
	} else {
		delete(pendingResize, key)
	}
	if !jobRunning {
		jobRunning = true
		go gradWorker()
	}
}

func takeJob() (gradKey, preparedGrad, uint64, bool, bool) {
	jobMu.Lock()
	defer jobMu.Unlock()
	for k, p := range pending {
		delete(pending, k)
		resize := pendingResize[k]
		delete(pendingResize, k)
		inflight = k
		inflightEp = epochs[[2]int{k.w, k.h}]
		inflightOn = true
		return k, p, inflightEp, resize, true
	}
	inflightOn = false
	jobRunning = false
	jobDone.Broadcast()
	return gradKey{}, preparedGrad{}, 0, false, false
}

// shownSizeMismatch reports that the bitmap on screen for this pixel size
// was built for a different size. The job is a resize regeneration, not a
// theme change. The previous bitmap keeps being stretched until this one
// is published.
func shownSizeMismatch(key gradKey) bool {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	st := sizes[[2]int{key.w, key.h}]
	if st == nil || st.shown == nil {
		return false
	}
	return st.shown.key.w != key.w || st.shown.key.h != key.h
}

func gradWorker() {
	for {
		key, prep, ep, resize, ok := takeJob()
		if !ok {
			return
		}
		themeLog(themeEventBuildStart, key.w, key.h, 0, 0)
		sz := [2]int{key.w, key.h}
		start := time.Now()
		img, aborted := renderPrepared(prep, key.w, key.h, true, fullBand, func() bool {
			jobMu.Lock()
			stale := epochs[sz] != ep
			jobMu.Unlock()
			return stale
		})
		if aborted || img == nil {
			retireInflight(key, ep)
			gradAborted.Add(1)
			continue
		}
		// The copy is the gap a faster build exposed: the epoch check used
		// to run here, then bitmapFromNRGBA copied ~20MB, and a switch in
		// that window still published the theme it had already replaced.
		e := &gradEntry{key: key, prep: prep, img: img, bm: bitmapFromNRGBA(img), seq: gradSeq.Add(1)}
		rememberBitmap(e.bm, e.seq)
		switch commitGradient(e, ep) {
		case commitStale:
			gradAborted.Add(1)
			continue
		case commitKept:
			continue
		}
		gradPublished.Add(1)
		gradLastGen.Store(int64(time.Since(start)))
		if resize {
			// The next theme switch at this size needs its own preview
			// image. Stretching the previous size's preview keeps that
			// texture id on screen.
			publishSizePreview(key, prep)
			themeLogBitmap(themeEventResize, e.bm, e.seq)
		} else {
			themeLogBitmap(themeEventBuildDone, e.bm, e.seq)
		}
		wakeGradient()
	}
}

type commitKind int

const (
	commitStale commitKind = iota
	commitKept
	commitFresh
)

func retireInflight(key gradKey, ep uint64) {
	jobMu.Lock()
	if inflightOn && inflight == key && inflightEp == ep {
		inflightOn = false
	}
	jobMu.Unlock()
}

// commitGradient inserts e only when ep is still the newest request for
// that pixel size. launchGrad bumps the epoch under jobMu, and this holds
// that same lock across the check and the cache insert, so a theme that
// finished rendering cannot publish after a newer switch has replaced it.
func commitGradient(e *gradEntry, ep uint64) commitKind {
	sz := [2]int{e.key.w, e.key.h}
	jobMu.Lock()
	defer jobMu.Unlock()
	if inflightOn && inflight == e.key && inflightEp == ep {
		inflightOn = false
	}
	if epochs[sz] != ep {
		return commitStale
	}
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if entries[e.key] != nil {
		// Keep the bitmap the theme clock already holds. Replacing the
		// pointer makes a later frame fail the "theme just picked" check.
		return commitKept
	}
	entries[e.key] = e
	lastReady = e
	evictLocked(e)
	return commitFresh
}

// publishSizePreview caches the undithered 1/8 preview for a bitmap that
// was regenerated because the window size changed. It runs on the gradient
// worker, off the frame.
func publishSizePreview(key gradKey, prep preparedGrad) {
	w, h := previewSize(key.w, key.h)
	pk := previewKey{w: w, h: h, top: key.top, mid: key.mid, low: key.low, bot: key.bot, tint: key.tint}
	if cachedPreview(pk) != nil {
		return
	}
	img, aborted := renderPrepared(prep, w, h, false, 0, nil)
	if aborted || img == nil {
		return
	}
	bm := bitmapFiltered(img)
	gen := previewSeq.Add(1)
	rememberBitmap(bm, gen)
	storePreview(pk, bm)
}

func referenced(e *gradEntry) bool {
	if e == nil {
		return false
	}
	if e == lastReady {
		return true
	}
	for _, st := range sizes {
		if st != nil && (st.shown == e || st.from == e) {
			return true
		}
	}
	return false
}

func evictLocked(keep *gradEntry) {
	if len(entries) <= gradCacheCap {
		return
	}
	for k, e := range entries {
		if e == keep || referenced(e) {
			continue
		}
		delete(entries, k)
		if len(entries) <= gradCacheCap {
			return
		}
	}
}

// ownBitmap copies img into a fresh RGBA before the scene image is built.
// Metal and GL cache one texture per scene.Image ID and upload again only
// when that image's version changes (imageTexture in internal/gpu/metal
// and internal/gpu/gl). scene.NewImage shares the pixel buffer of an
// *image.RGBA whose rows are packed from the origin, so wrapping the
// worker's NRGBA buffer would leave the GPU holding bytes a later render
// can overwrite, while the software rasterizer reads those bytes at draw
// time and would still pass. A new bitmap is also a new image ID, so the
// texture key changes. Bitmap.Update is the in-place path and is not used
// for a theme generation.
func ownBitmap(img *image.NRGBA, pixelated bool) *ui.Bitmap {
	if img == nil {
		return nil
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	copyRows(dst.Pix, dst.Stride, img.Pix, img.Stride, b.Min.X, b.Min.Y, b.Dx(), b.Dy())
	return finishBitmap(dst, pixelated)
}

// ownBitmapRGBA is ownBitmap for a veil, which is already premultiplied.
// NewBitmap would share img's pixels, and the GPU would keep the texture
// it uploaded for that image id.
func ownBitmapRGBA(img *image.RGBA, pixelated bool) *ui.Bitmap {
	if img == nil {
		return nil
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	copyRows(dst.Pix, dst.Stride, img.Pix, img.Stride, b.Min.X, b.Min.Y, b.Dx(), b.Dy())
	return finishBitmap(dst, pixelated)
}

func copyRows(dst []byte, dstStride int, src []byte, srcStride, x0, y0, w, h int) {
	n := w * 4
	for y := 0; y < h; y++ {
		so := (y0+y)*srcStride + x0*4
		copy(dst[y*dstStride:y*dstStride+n], src[so:so+n])
	}
}

func finishBitmap(dst *image.RGBA, pixelated bool) *ui.Bitmap {
	bm := ui.NewBitmap(dst)
	if pixelated {
		return bm.Pixelated()
	}
	return bm
}

// bitmapFromNRGBA is the full-resolution bitmap. It is pixelated: each
// device pixel is one texel.
func bitmapFromNRGBA(img *image.NRGBA) *ui.Bitmap {
	return ownBitmap(img, true)
}

// bitmapFiltered is the preview. It is not pixelated, so stretching it
// up to the window filters neighboring texels instead of drawing blocks.
func bitmapFiltered(img *image.NRGBA) *ui.Bitmap {
	return ownBitmap(img, false)
}

func previewSize(fullW, fullH int) (int, int) {
	w, h := fullW/previewDiv, fullH/previewDiv
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return w, h
}

func previewKeyOf(look Theme, w, h int) previewKey {
	p := prepareWindowGrad(look)
	return previewKey{w: w, h: h, top: p.top, mid: p.mid, low: p.low, bot: p.bot, tint: p.tintPacked}
}

func cachedPreview(key previewKey) *ui.Bitmap {
	previewMu.Lock()
	bm := previews[key]
	previewMu.Unlock()
	return bm
}

func storePreview(key previewKey, bm *ui.Bitmap) {
	previewMu.Lock()
	if previews[key] == nil {
		previews[key] = bm
	}
	previewMu.Unlock()
}

// ensurePreviewPolicy times one maximized-window preview (2880×1800 / previewDiv).
// At or under previewSyncLimit, a theme switch builds its preview on the
// click frame. Slower than that, every theme's light and dark preview is
// built now, at both maximized sizes, and a switch stretches one of those.
func ensurePreviewPolicy() {
	previewOnce.Do(func() {
		look := Theme{Colors: themes.Palette(themes.ThemeWarm, false)}
		start := time.Now()
		buildPreview(look, 2880/previewDiv, 1800/previewDiv)
		if time.Since(start) > previewSyncLimit {
			previewSlow.Store(true)
			prebuildPreviews()
		}
	})
}

func buildPreview(look Theme, w, h int) *ui.Bitmap {
	key := previewKeyOf(look, w, h)
	if bm := cachedPreview(key); bm != nil {
		return bm
	}
	// No blue noise. Nearest quantization plus the short run break, then
	// the painter's linear filter. Dither at this scale upscales as blotches.
	img, _ := renderPrepared(prepareWindowGrad(look), w, h, false, 0, nil)
	if img == nil {
		img = image.NewNRGBA(image.Rect(0, 0, max(w, 1), max(h, 1)))
	}
	bm := bitmapFiltered(img)
	rememberBitmap(bm, previewSeq.Add(1))
	storePreview(key, bm)
	return cachedPreview(key)
}

func prebuildPreviews() {
	sizes := [][2]int{{2880 / previewDiv, 1800 / previewDiv}, {5120 / previewDiv, 2880 / previewDiv}}
	for _, preset := range themes.Presets() {
		for _, dark := range []bool{false, true} {
			look := Theme{Colors: themes.Palette(preset.ID, dark), Dark: dark}
			for _, sz := range sizes {
				buildPreview(look, sz[0], sz[1])
			}
		}
	}
}

func nearestPreview(look Theme, w, h int) *ui.Bitmap {
	prep := prepareWindowGrad(look)
	previewMu.Lock()
	defer previewMu.Unlock()
	var best *ui.Bitmap
	bestD := int(^uint(0) >> 1)
	for k, bm := range previews {
		if k.top != prep.top || k.mid != prep.mid || k.low != prep.low || k.bot != prep.bot || k.tint != prep.tintPacked {
			continue
		}
		d := (k.w-w)*(k.w-w) + (k.h-h)*(k.h-h)
		if d < bestD {
			bestD = d
			best = bm
		}
	}
	return best
}

// previewBitmap is the undithered 1/8 gradient for look at fullW×fullH.
// The caller stretches it with linear filtering. A machine that cannot
// render the 2880×1800 preview within previewSyncLimit gets a prebuilt one.
func previewBitmap(look Theme, fullW, fullH int) *ui.Bitmap {
	ensurePreviewPolicy()
	w, h := previewSize(fullW, fullH)
	key := previewKeyOf(look, w, h)
	if bm := cachedPreview(key); bm != nil {
		previewLast.Store(0)
		return bm
	}
	if previewSlow.Load() {
		if bm := nearestPreview(look, w, h); bm != nil {
			previewLast.Store(0)
			return bm
		}
	}
	start := time.Now()
	bm := buildPreview(look, w, h)
	elapsed := time.Since(start)
	previewLast.Store(int64(elapsed))
	if elapsed > previewSyncLimit && previewSlow.CompareAndSwap(false, true) {
		go prebuildPreviews()
	}
	return bm
}

func gradientEntrySync(look Theme, w, h int) *gradEntry {
	prep := prepareWindowGrad(look)
	key := gradKeyOf(w, h, prep)
	cacheMu.Lock()
	if e := entries[key]; e != nil {
		cacheMu.Unlock()
		return e
	}
	cacheMu.Unlock()
	img := renderGradient(look, w, h, true)
	e := &gradEntry{key: key, prep: prep, img: img, bm: bitmapFromNRGBA(img), seq: gradSeq.Add(1)}
	cacheMu.Lock()
	if old := entries[key]; old != nil {
		cacheMu.Unlock()
		return old
	}
	entries[key] = e
	lastReady = e
	evictLocked(e)
	cacheMu.Unlock()
	return e
}

func gradientImage(look Theme, w, h int) *image.NRGBA {
	if w < 1 || h < 1 {
		return nil
	}
	if gradSync.Load() {
		return gradientEntrySync(look, w, h).img
	}
	prep := prepareWindowGrad(look)
	key := gradKeyOf(w, h, prep)
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if e := entries[key]; e != nil {
		return e.img
	}
	if st := sizes[[2]int{w, h}]; st != nil && st.shown != nil {
		return st.shown.img
	}
	if lastReady != nil {
		return lastReady.img
	}
	return nil
}

// presentGradient decides which bitmaps to draw and, when the frame is
// not in instant mode, schedules generation without building pixels.
// again is how soon the frame should paint to continue a fade, a resize
// settle, or a job that has not landed yet.
func presentGradient(look Theme, w, h int, now time.Time) (base, over *ui.Bitmap, alpha float32, again time.Duration) {
	start := time.Now()
	defer func() { noteMain(time.Since(start)) }()
	if w < 1 || h < 1 {
		return nil, nil, 0, 0
	}
	prep := prepareWindowGrad(look)
	key := gradKeyOf(w, h, prep)
	motion := gradMotion.Load()

	cacheMu.Lock()
	st := sizes[[2]int{w, h}]
	if st == nil {
		st = &sizeLive{}
		if lastReady != nil {
			st.shown = lastReady
		}
		sizes[[2]int{w, h}] = st
	}
	if e := entries[key]; e != nil && (st.shown == nil || st.shown.key != e.key) {
		// The theme clock owns the crossfade. Adopting the published
		// bitmap must not install the previous theme as the from layer:
		// that frame is the old theme at full opacity, then a second fade.
		st.from = nil
		st.fadeAt = time.Time{}
		st.shown = e
		st.pending = false
		lastReady = e
	}
	need := entries[key] == nil
	shown := st.shown
	from := st.from
	fadeAt := st.fadeAt
	var wait time.Duration
	var launch bool
	if need {
		sameSize := shown != nil && shown.key.w == w && shown.key.h == h
		if shown != nil && !sameSize {
			if !st.pending || st.pendingAt.IsZero() {
				st.pending = true
				st.pendingAt = now
			}
			due := st.pendingAt.Add(gradSettle)
			if !now.Before(due) {
				launch = true
				st.pending = false
				wait = 8 * time.Millisecond
			} else {
				wait = due.Sub(now)
			}
		} else {
			launch = true
			wait = 8 * time.Millisecond
		}
	}
	cacheMu.Unlock()

	if launch {
		launchGrad(key, prep)
	}
	if shown == nil || shown.bm == nil {
		if wait == 0 && need {
			wait = 8 * time.Millisecond
		}
		if alt := bitmapForTheme(key); alt != nil {
			return alt, nil, 0, wait
		}
		return nil, nil, 0, wait
	}
	// This size is still showing another theme, and its own bitmap is not
	// ready. A bitmap of the requested theme at another size (the other
	// window, or the one the clock just finished) is the picture on screen.
	// Returning the old theme here is the flash.
	if themeKeyDiffers(shown.key, key) {
		if alt := bitmapForTheme(key); alt != nil {
			return alt, nil, 0, wait
		}
	}
	if from != nil && from.bm != nil && !fadeAt.IsZero() && motion {
		u := float32(now.Sub(fadeAt)) / float32(gradFade)
		if u < 0 {
			u = 0
		}
		if u < 1 {
			if wait == 0 || wait > 16*time.Millisecond {
				wait = 16 * time.Millisecond
			}
			return from.bm, shown.bm, u, wait
		}
	}
	return shown.bm, nil, 0, wait
}

func themeKeyDiffers(a, b gradKey) bool {
	return a.top != b.top || a.mid != b.mid || a.low != b.low || a.bot != b.bot || a.tint != b.tint
}

// bitmapForTheme is a finished bitmap of key's theme at any size. The
// window that does not have its own yet would otherwise keep the old one.
func bitmapForTheme(key gradKey) *ui.Bitmap {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if lastReady != nil && lastReady.bm != nil && !themeKeyDiffers(lastReady.key, key) {
		return lastReady.bm
	}
	for _, e := range entries {
		if e != nil && e.bm != nil && !themeKeyDiffers(e.key, key) {
			return e.bm
		}
	}
	return nil
}

// cachedGradient is the finished bitmap for this look and pixel size.
// It does not schedule work and it does not change what is on screen.
func cachedGradient(look Theme, w, h int) *gradEntry {
	if w < 1 || h < 1 {
		return nil
	}
	key := gradKeyOf(w, h, prepareWindowGrad(look))
	cacheMu.Lock()
	e := entries[key]
	cacheMu.Unlock()
	return e
}

// ensureGradient starts a build when the bitmap is missing. It does not
// adopt the result into the on-screen pair. The theme clock swaps it in
// when the worker publishes it and wakes the window.
func ensureGradient(look Theme, w, h int) *gradEntry {
	if e := cachedGradient(look, w, h); e != nil {
		return e
	}
	if w < 1 || h < 1 {
		return nil
	}
	prep := prepareWindowGrad(look)
	launchGrad(gradKeyOf(w, h, prep), prep)
	return nil
}

// displayedGradient is the bitmap pair already on screen. It does not
// adopt a newly finished entry, so a theme switch can freeze this picture
// as the start of the crossfade.
func displayedGradient(w, h int, now time.Time) (base, over *ui.Bitmap, alpha float32) {
	if w < 1 || h < 1 {
		return nil, nil, 0
	}
	cacheMu.Lock()
	defer cacheMu.Unlock()
	st := sizes[[2]int{w, h}]
	if st == nil || st.shown == nil || st.shown.bm == nil {
		if lastReady != nil && lastReady.bm != nil {
			return lastReady.bm, nil, 0
		}
		return nil, nil, 0
	}
	if st.from != nil && st.from.bm != nil && !st.fadeAt.IsZero() && gradMotion.Load() {
		u := float32(now.Sub(st.fadeAt)) / float32(gradFade)
		if u < 0 {
			u = 0
		}
		if u < 1 {
			return st.from.bm, st.shown.bm, u
		}
	}
	return st.shown.bm, nil, 0
}

// pinGradientShown makes entry the current bitmap for this size with no
// crossfade. The shared clock calls it when its own fade ends, so the
// next presentGradient does not start a second one.
func pinGradientShown(look Theme, w, h int) {
	e := cachedGradient(look, w, h)
	if e == nil {
		return
	}
	cacheMu.Lock()
	st := sizes[[2]int{w, h}]
	if st == nil {
		st = &sizeLive{}
		sizes[[2]int{w, h}] = st
	}
	st.shown = e
	st.from = nil
	st.fadeAt = time.Time{}
	st.pending = false
	lastReady = e
	cacheMu.Unlock()
}

func paintWindowBackground(p *ui.Painter, r, hole ui.Rect, look Theme) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	scale := p.Scale()
	if scale < 1 {
		scale = 1
	}
	_, pw := deviceSpan(r.X, r.X+r.W, scale)
	_, ph := deviceSpan(r.Y, r.Y+r.H, scale)
	if pw < 1 || ph < 1 {
		return
	}
	if gradSync.Load() {
		e := gradientEntrySync(look, pw, ph)
		paintBackdropBitmap(p, e.bm, e.img, r, hole, 1)
		themeLogBackdrop(pw, ph, 1, e.bm, 0, nil, 0, nil)
		return
	}
	base, over, alpha, again := presentGradient(look, pw, ph, p.Now())
	baseA := float32(0)
	overA := float32(0)
	if base != nil {
		paintBackdropBitmap(p, base, nil, r, hole, 1)
		baseA = 1
		if over == nil || alpha < 0.5 {
			themeLogDrawn(themeEventFull, base)
		}
	}
	if over != nil && alpha > 0 {
		paintBackdropBitmap(p, over, nil, r, hole, alpha)
		overA = alpha
		if alpha >= 0.5 {
			themeLogDrawn(themeEventFull, over)
		}
	}
	themeLogBackdrop(pw, ph, baseA, base, 0, nil, overA, over)
	if again > 0 {
		p.After(again)
	}
}

// paintBackdropBitmap draws bm across the whole of r in one image. The
// software raster copies a 1:1 opaque bitmap instead of bilinear-sampling
// it, so a large scroll stays cheap and the pixels stay the bitmap's.
func paintBackdropBitmap(p *ui.Painter, bm *ui.Bitmap, img *image.NRGBA, r, hole ui.Rect, alpha float32) {
	if bm == nil || alpha <= 0 || r.W <= 0 || r.H <= 0 {
		return
	}
	opt := ui.ImageOptions{}
	if alpha < 1 {
		opt.Tint = ui.Color{R: 255, G: 255, B: 255, A: alphaByte(alpha)}
	}
	p.ImageRegion(bm, ui.Rect{}, r, opt)
	return
}

// windowPixel is the dithered background at one device pixel of the
// full window. It is the same image paintWindowBackground draws.
func windowPixel(look Theme, scale, winW, winH float32, px, py int) color.NRGBA {
	if scale < 1 {
		scale = 1
	}
	_, pw := deviceSpan(0, winW, scale)
	_, ph := deviceSpan(0, winH, scale)
	g := gradientEntrySync(look, pw, ph)
	if px < 0 {
		px = 0
	}
	if py < 0 {
		py = 0
	}
	if px >= g.key.w {
		px = g.key.w - 1
	}
	if py >= g.key.h {
		py = g.key.h - 1
	}
	i := g.img.PixOffset(px, py)
	s := g.img.Pix
	return color.NRGBA{R: s[i], G: s[i+1], B: s[i+2], A: 255}
}
