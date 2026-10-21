package main

import (
	"image"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"
)

// The composer and jump-pill shadows are baked, not drawn with MyGo's
// OpShadow. The raster quantizes each coverage sample with to8 / integer
// over, so a soft shadow of shadow/md (alpha 20 or 82) falls through one
// 8-bit code about every 2pt. On top of that, the contact layer
// (0, 0.5, 1, 0) is snapped with math.Round: the 0.5pt y offset lands on
// a whole pixel, and that pixel is painted at nearly the full contact
// coverage. Measured on white, just under the plate: the border is 0xE5,
// the contact layer alone is 0xEE, the soft layer alone is 0xF4, and the
// two composited in 8-bit are 0xE4 — one code darker than the border, then
// 0xF2, then the soft layer's plateaus. Same pixels with the plate removed,
// so it is not the plate's blend. It is the rounded contact strip stacked
// on the already-quantized soft layer: dark line, light band, rings.
//
// These bitmaps composite both layers in float (continuous offsets, no
// half-pixel snap) and quantize once with the background's ±1 LSB Bayer
// step. A run of identical codes longer than about 1pt is broken where the
// float gradient is still moving. The bitmap is cached like the veil:
// rebuilt when the device size or the shadow color changes, and each build
// is a new image id.

const (
	shadowComposer = 1
	shadowJump     = 2
)

// composerShadow is SPEC shadow.layer/composer. The second layer is
// 0, 6, 16, -6: the shape meets the plate along the bottom and sits 6pt
// in from the sides, so the blur is faint below and almost gone beside it.
var composerShadow = []shadowLayer{
	{x: 0, y: 0.5, blur: 1, spread: 0},
	{x: 0, y: 6, blur: 16, spread: -6},
}

// jumpShadow is the drop layer of SPEC shadow.layer/overlay. The pill's
// 0.5pt border is the overlay hairline; it is the button border, not a
// second shadow.
var jumpShadow = []shadowLayer{
	{x: 0, y: 8, blur: 24, spread: 0},
}

type shadowLayer struct {
	x, y, blur, spread float32
}

type shadowKey struct {
	kind                    int
	w, h                    int
	radiusMilli, scaleMilli int
	color                   uint32
}

type shadowEntry struct {
	key shadowKey
	bm  *ui.Bitmap
	pad int
	gen uint64
}

type shadowWork struct {
	key    shadowKey
	layers []shadowLayer
	color  ui.Color
	pad    int
	gen    uint64
}

var (
	shadowMu      sync.Mutex
	shadowEntries = map[shadowKey]*shadowEntry{}
	shadowLive    [3]*shadowEntry
	shadowHold    [3]shadowSettle

	shadowJobMu      sync.Mutex
	shadowJobDone    *sync.Cond
	shadowPending    = map[shadowKey]shadowWork{}
	shadowJobRunning bool
	shadowGen        atomic.Uint64
	shadowSeq        atomic.Uint64
	shadowBuilds     atomic.Uint64
)

type shadowSettle struct {
	pending   bool
	pendingAt time.Time
	w, h      int
}

func init() {
	shadowJobDone = sync.NewCond(&shadowJobMu)
}

func layersFor(kind int) []shadowLayer {
	switch kind {
	case shadowJump:
		return jumpShadow
	default:
		return composerShadow
	}
}

func resetShadowCache() {
	awaitShadows()
	shadowGen.Add(1)
	shadowMu.Lock()
	shadowEntries = map[shadowKey]*shadowEntry{}
	shadowLive = [3]*shadowEntry{}
	shadowHold = [3]shadowSettle{}
	shadowMu.Unlock()
	shadowJobMu.Lock()
	shadowPending = map[shadowKey]shadowWork{}
	shadowJobMu.Unlock()
}

func awaitShadows() {
	shadowJobMu.Lock()
	for shadowJobRunning || len(shadowPending) > 0 {
		shadowJobDone.Wait()
	}
	shadowJobMu.Unlock()
}

// paintDropShadow draws the cached shadow behind the plate. r is the
// plate's border box. A miss builds off the frame, except in instant mode,
// which renders here so a shot is exact. The previous bitmap is stretched
// until the new one is published.
func paintDropShadow(p *ui.Painter, r ui.Rect, radius float32, kind int, color ui.Color) {
	if r.W < 1 || r.H < 1 || kind <= 0 {
		return
	}
	scale := p.Scale()
	if scale < 1 {
		scale = 1
	}
	x0, elemW := deviceSpan(r.X, r.X+r.W, scale)
	y0, elemH := deviceSpan(r.Y, r.Y+r.H, scale)
	if elemW < 1 || elemH < 1 {
		return
	}
	layers := layersFor(kind)
	key := shadowKey{
		kind:        kind,
		w:           elemW,
		h:           elemH,
		radiusMilli: int(math.Round(float64(radius * 1000))),
		scaleMilli:  int(math.Round(float64(scale * 1000))),
		color:       packColor(color),
	}
	if e := lookupShadow(key); e != nil {
		rememberShadow(kind, e)
		blitShadow(p, e, x0, y0, scale)
		return
	}
	if gradSync.Load() {
		img := renderDropShadow(elemW, elemH, radius, scale, layers, color)
		e := publishShadow(key, img, shadowPad(layers, scale))
		if e != nil {
			rememberShadow(kind, e)
			blitShadow(p, e, x0, y0, scale)
		}
		return
	}
	if shadowDue(kind, elemW, elemH, p.Now()) {
		launchShadow(key, layers, color, shadowPad(layers, scale))
	}
	if e := stretchedShadow(kind, color); e != nil {
		blitShadow(p, e, x0, y0, scale)
	}
	p.After(8 * time.Millisecond)
}

func blitShadow(p *ui.Painter, e *shadowEntry, x0, y0 int, scale float32) {
	if e == nil || e.bm == nil {
		return
	}
	pad := e.pad
	dest := ui.Rect{
		X: (float32(x0) - float32(pad)) / scale,
		Y: (float32(y0) - float32(pad)) / scale,
		W: float32(e.key.w+2*pad) / scale,
		H: float32(e.key.h+2*pad) / scale,
	}
	p.ImageRegion(e.bm, ui.Rect{}, dest, ui.ImageOptions{})
}

func lookupShadow(key shadowKey) *shadowEntry {
	shadowMu.Lock()
	e := shadowEntries[key]
	shadowMu.Unlock()
	return e
}

func rememberShadow(kind int, e *shadowEntry) {
	if kind < 0 || kind >= len(shadowLive) || e == nil {
		return
	}
	shadowMu.Lock()
	shadowLive[kind] = e
	shadowHold[kind] = shadowSettle{}
	shadowMu.Unlock()
}

func stretchedShadow(kind int, color ui.Color) *shadowEntry {
	if kind < 0 || kind >= len(shadowLive) {
		return nil
	}
	shadowMu.Lock()
	e := shadowLive[kind]
	shadowMu.Unlock()
	if e == nil || e.key.color != packColor(color) {
		return nil
	}
	return e
}

// shadowDue reports whether a size change has held still long enough to
// rebuild. A color change at the same pixel size builds immediately.
func shadowDue(kind, w, h int, now time.Time) bool {
	if kind < 0 || kind >= len(shadowLive) {
		return true
	}
	shadowMu.Lock()
	defer shadowMu.Unlock()
	last := shadowLive[kind]
	if last == nil || (last.key.w == w && last.key.h == h) {
		return true
	}
	st := &shadowHold[kind]
	if !st.pending || st.w != w || st.h != h {
		st.pending = true
		st.pendingAt = now
		st.w, st.h = w, h
	}
	if now.Before(st.pendingAt.Add(gradSettle)) {
		return false
	}
	st.pending = false
	return true
}

func launchShadow(key shadowKey, layers []shadowLayer, color ui.Color, pad int) {
	gen := shadowGen.Load()
	shadowJobMu.Lock()
	defer shadowJobMu.Unlock()
	if _, have := shadowPending[key]; have {
		return
	}
	shadowMu.Lock()
	_, have := shadowEntries[key]
	shadowMu.Unlock()
	if have {
		return
	}
	shadowPending[key] = shadowWork{key: key, layers: layers, color: color, pad: pad, gen: gen}
	if !shadowJobRunning {
		shadowJobRunning = true
		go shadowWorker()
	}
}

func takeShadow() (shadowWork, bool) {
	shadowJobMu.Lock()
	defer shadowJobMu.Unlock()
	for k, w := range shadowPending {
		delete(shadowPending, k)
		return w, true
	}
	shadowJobRunning = false
	shadowJobDone.Broadcast()
	return shadowWork{}, false
}

func shadowWorker() {
	for {
		work, ok := takeShadow()
		if !ok {
			return
		}
		if shadowGen.Load() != work.gen {
			continue
		}
		img := renderDropShadow(work.key.w, work.key.h, float32(work.key.radiusMilli)/1000, float32(work.key.scaleMilli)/1000, work.layers, work.color)
		if shadowGen.Load() != work.gen || img == nil {
			continue
		}
		publishShadow(work.key, img, work.pad)
		wakeGradient()
	}
}

func publishShadow(key shadowKey, img *image.RGBA, pad int) *shadowEntry {
	bm := ownBitmapRGBA(img, true)
	if bm == nil {
		return nil
	}
	gen := shadowSeq.Add(1)
	rememberBitmap(bm, gen)
	e := &shadowEntry{key: key, bm: bm, pad: pad, gen: gen}
	shadowMu.Lock()
	defer shadowMu.Unlock()
	if old := shadowEntries[key]; old != nil {
		return old
	}
	shadowEntries[key] = e
	if len(shadowEntries) > 12 {
		for k, old := range shadowEntries {
			if old == e || shadowPinned(old) {
				continue
			}
			delete(shadowEntries, k)
			if len(shadowEntries) <= 12 {
				break
			}
		}
	}
	return e
}

func shadowPinned(e *shadowEntry) bool {
	for _, live := range shadowLive {
		if live == e {
			return true
		}
	}
	return false
}

func shadowPad(layers []shadowLayer, scale float32) int {
	s := float64(scale)
	if s < 1 {
		s = 1
	}
	pad := 2.0
	for _, l := range layers {
		sigma := float64(l.blur) * s / 2
		reach := 3*sigma + math.Abs(float64(l.x))*s + math.Abs(float64(l.y))*s + float64(l.spread)*s
		if reach > pad {
			pad = reach
		}
	}
	return int(math.Ceil(pad)) + 2
}

// renderDropShadow composites layers in float over a transparent bitmap.
// Pixel (0, 0) is the outer corner of the padding; the plate occupies
// [pad, pad+elemW) × [pad, pad+elemH). Offsets stay in continuous device
// pixels — the 0.5pt contact shift is not rounded to a whole pixel.
func renderDropShadow(elemW, elemH int, radius, scale float32, layers []shadowLayer, color ui.Color) *image.RGBA {
	shadowBuilds.Add(1)
	if elemW < 1 || elemH < 1 {
		return nil
	}
	if scale < 1 {
		scale = 1
	}
	pad := shadowPad(layers, scale)
	w := elemW + 2*pad
	h := elemH + 2*pad
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	ideal := make([]float64, w*h)
	q := make([]int, w*h)
	bayerQ := make([]int, w*h)
	rad := clampRadius(float64(radius)*float64(scale), float64(elemW), float64(elemH))
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			px := float64(i-pad) + 0.5
			py := float64(j-pad) + 0.5
			a := dropAlpha(px, py, float64(elemW), float64(elemH), rad, float64(scale), layers, color)
			n := j*w + i
			ideal[n] = a
			q[n] = quantChannel(a, true, bayerOffset(i, j))
			bayerQ[n] = q[n]
		}
	}
	breakShadowRuns(ideal, q, bayerQ, w, h, float64(scale))
	for j := 0; j < h; j++ {
		row := img.Pix[j*img.Stride:]
		for i := 0; i < w; i++ {
			a := q[j*w+i]
			if a < 0 {
				a = 0
			}
			if a > 255 {
				a = 255
			}
			o := i * 4
			// Black shadow, premultiplied. A colored shadow would scale RGB
			// by the same quantized alpha.
			row[o] = 0
			row[o+1] = 0
			row[o+2] = 0
			row[o+3] = byte(a)
		}
	}
	return img
}

// dropAlpha is the straight alpha of every layer, source-over, masked to
// the outside of the plate. It is the float gradient the dither quantizes.
func dropAlpha(px, py, bw, bh, radius, scale float64, layers []shadowLayer, color ui.Color) float64 {
	plate := roundRectCoverage(px, py, 0, 0, bw, bh, radius)
	outside := 1 - plate
	if outside <= 0 {
		return 0
	}
	sa := float64(color.A) / 255
	if sa <= 0 {
		return 0
	}
	var acc float64
	for _, l := range layers {
		ox := float64(l.x) * scale
		oy := float64(l.y) * scale
		spread := float64(l.spread) * scale
		x := ox - spread
		y := oy - spread
		w := bw + 2*spread
		h := bh + 2*spread
		if w <= 0.5 || h <= 0.5 {
			continue
		}
		rad := clampRadius(radius+spread, w, h)
		sigma := float64(l.blur) * scale / 2
		var cov float64
		if sigma < 0.5 {
			cov = roundRectCoverage(px, py, x, y, w, h, rad)
		} else {
			cov = blurredRoundRect(px, py, x, y, w, h, rad, sigma)
		}
		if cov <= 0 {
			continue
		}
		a := sa * cov * outside
		acc += a * (1 - acc)
	}
	if acc < 0 {
		return 0
	}
	if acc > 1 {
		return 1
	}
	return acc
}

func clampRadius(r, w, h float64) float64 {
	if r < 0 {
		r = 0
	}
	if w > 0 && r > w/2 {
		r = w / 2
	}
	if h > 0 && r > h/2 {
		r = h / 2
	}
	return r
}

// roundRectCoverage is the plate's coverage at a pixel center, 1 inside.
// The edge is antialiased across one pixel.
func roundRectCoverage(px, py, x, y, w, h, r float64) float64 {
	if w <= 0 || h <= 0 {
		return 0
	}
	r = clampRadius(r, w, h)
	cx := x + w/2
	cy := y + h/2
	qx := math.Abs(px-cx) - (w/2 - r)
	qy := math.Abs(py-cy) - (h/2 - r)
	ox, oy := math.Max(qx, 0), math.Max(qy, 0)
	dist := math.Hypot(ox, oy) + math.Min(math.Max(qx, qy), 0) - r
	v := 0.5 - dist
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 1
	}
	return v
}

// blurredRoundRect is the box blurred by a gaussian of stddev sigma
// (CSS blur/2), with circular corners. Eight samples along y, exact along x.
func blurredRoundRect(px, py, x, y, w, h, radius, sigma float64) float64 {
	cx := x + w/2
	cy := y + h/2
	hx := w / 2
	hy := h / 2
	rx := px - cx
	ry := py - cy
	const n = 8
	ext := 3 * sigma
	low, high := ry-hy, ry+hy
	start := math.Min(math.Max(-ext, low), high)
	end := math.Min(math.Max(ext, low), high)
	if end-start < 1e-6 {
		return 0
	}
	step := (end - start) / n
	k := math.Sqrt(0.5) / sigma
	yy := start + step*0.5
	var v float64
	norm := 1 / (math.Sqrt(2*math.Pi) * sigma)
	for range n {
		half := hx
		if radius > 0 {
			if delta := math.Min(hy-radius-math.Abs(ry-yy), 0); delta < 0 {
				half = hx - radius + math.Sqrt(math.Max(0, radius*radius-delta*delta))
			}
		}
		g := math.Exp(-(yy*yy)/(2*sigma*sigma)) * norm
		v += g * step * 0.5 * (math.Erf((rx+half)*k) - math.Erf((rx-half)*k))
		yy += step
	}
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// breakShadowRuns pulls adjacent codes to within 1 of each other, then
// splits a run longer than about 1pt when the float alpha still changes
// across it. A nudge happens once per pixel and stays within ±1 of Bayer.
func breakShadowRuns(ideal []float64, q, bayer []int, w, h int, scale float64) {
	if scale < 1 {
		scale = 1
	}
	clampShadowAdjacent(ideal, q, bayer, w, h)
	nudged := make([]bool, len(q))
	for pass := 0; pass < 8; pass++ {
		if !splitRuns(ideal, q, nudged, w, h, scale, 1, 0) &&
			!splitRuns(ideal, q, nudged, w, h, scale, 0, 1) &&
			!splitRuns(ideal, q, nudged, w, h, scale, 1, 1) &&
			!splitRuns(ideal, q, nudged, w, h, scale, 1, -1) {
			return
		}
	}
}

// clampShadowAdjacent keeps a Bayer step from landing two codes away from
// a neighbor. The replacement is the neighbor's code ±1 closest to the
// float alpha, and still within ±1 of Bayer.
func clampShadowAdjacent(ideal []float64, q, bayer []int, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			if x > 0 {
				pullShadow(ideal, q, bayer, i, i-1)
			}
			if y > 0 {
				pullShadow(ideal, q, bayer, i, i-w)
			}
		}
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func pullShadow(ideal []float64, q, bayer []int, i, nb int) {
	d := q[i] - q[nb]
	if d < 2 && d > -2 {
		return
	}
	dir := 1
	if d < 0 {
		dir = -1
	}
	best := q[i]
	bestDist := math.Abs(ideal[i]*255 - float64(q[i]))
	for step := 1; step < absInt(d); step++ {
		n := q[nb] + dir*step
		if n < bayer[i]-1 || n > bayer[i]+1 || n < 0 || n > 255 {
			continue
		}
		if dist := math.Abs(ideal[i]*255 - float64(n)); dist < bestDist {
			best, bestDist = n, dist
		}
	}
	q[i] = best
}

func runTooLong(run int, scale float64) bool {
	if run <= 2 {
		return false
	}
	return float64(run-1)/scale > 1.25
}

func splitRuns(ideal []float64, q []int, nudged []bool, w, h int, scale float64, dx, dy int) bool {
	changed := false
	type start struct{ x, y int }
	var starts []start
	if dy == 0 {
		for y := 0; y < h; y++ {
			starts = append(starts, start{0, y})
		}
	} else if dx == 0 {
		for x := 0; x < w; x++ {
			starts = append(starts, start{x, 0})
		}
	} else if dy > 0 {
		for x := 0; x < w; x++ {
			starts = append(starts, start{x, 0})
		}
		for y := 1; y < h; y++ {
			starts = append(starts, start{0, y})
		}
	} else {
		for x := 0; x < w; x++ {
			starts = append(starts, start{x, h - 1})
		}
		for y := 0; y < h-1; y++ {
			starts = append(starts, start{0, y})
		}
	}
	for _, s := range starts {
		x, y := s.x, s.y
		for x >= 0 && y >= 0 && x < w && y < h {
			v := q[y*w+x]
			x1, y1 := x, y
			n := 1
			for {
				nx, ny := x1+dx, y1+dy
				if nx < 0 || ny < 0 || nx >= w || ny >= h || q[ny*w+nx] != v {
					break
				}
				x1, y1 = nx, ny
				n++
			}
			if runTooLong(n, scale) {
				i0 := y*w + x
				i1 := y1*w + x1
				if math.Abs(ideal[i1]-ideal[i0]) > 1.0/255 {
					mid := n / 2
					mx, my := x+dx*mid, y+dy*mid
					im := my*w + mx
					if !nudged[im] {
						dir := 1
						if ideal[i1] < ideal[i0] {
							dir = -1
						}
						if nudgeShadow(q, ideal[im], im, dir) {
							nudged[im] = true
							changed = true
						}
					}
				}
			}
			x, y = x1+dx, y1+dy
		}
	}
	return changed
}

func nudgeShadow(q []int, ideal float64, i, dir int) bool {
	n := q[i] + dir
	nearest := int(math.Floor(ideal*255 + 0.5))
	if n < 0 || n > 255 || n < nearest-1 || n > nearest+1 || n == q[i] {
		return false
	}
	q[i] = n
	return true
}
