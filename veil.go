package main

import (
	"image"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"
)

// The composer veil is the content card, across the whole card, with the
// SPEC opacity ramp baked in: clear 28pt above the composer, 50% at the
// composer's top edge, 50% until 10pt above the card bottom, 70% at the
// card bottom. Strength depends only on y, so the sides match the middle.
// It is cached and drawn over the live transcript with Painter.ImageRegion
// (source-over). The transcript is never baked into the bitmap, so
// scrolling and streaming under the composer keep moving while the veil
// stays put. A theme switch crossfades it on the same clock as the
// background, starting from the veil already on screen.

const (
	veilFade = 1

	veilCacheCap = 8
)

type veilKey struct {
	kind                       int
	pw, ph, x0, y0             int
	winW, winH                 int
	seq                        uint64
	layer                      uint32
	scaleMilli                 int
	rectX, rectY, rectW, rectH int
	composerMilli              int
}

// veilGeom is the veil's place and alpha curve, without the background
// generation or the card color. Those arrive once the bitmap exists.
type veilGeom struct {
	kind                       int
	pw, ph, x0, y0             int
	winW, winH                 int
	scaleMilli                 int
	rectX, rectY, rectW, rectH int
	composerMilli              int
}

func (g veilGeom) key(seq uint64, layer uint32) veilKey {
	return veilKey{
		kind: g.kind, pw: g.pw, ph: g.ph, x0: g.x0, y0: g.y0,
		winW: g.winW, winH: g.winH, seq: seq, layer: layer,
		scaleMilli: g.scaleMilli,
		rectX:      g.rectX, rectY: g.rectY, rectW: g.rectW, rectH: g.rectH,
		composerMilli: g.composerMilli,
	}
}

func (g veilGeom) alphaAt(y float32) float32 {
	r := ui.Rect{
		X: float32(g.rectX) / 1000,
		Y: float32(g.rectY) / 1000,
		W: float32(g.rectW) / 1000,
		H: float32(g.rectH) / 1000,
	}
	composerH := float32(g.composerMilli) / 1000
	bottom := r.H - composerH - float32(composerFade)
	if bottom < 0 {
		bottom = 0
	}
	return veilAlphaAt(r, composerH, bottom, y)
}

type veilEntry struct {
	key veilKey
	bm  *ui.Bitmap
	gen uint64
}

type veilPaint struct {
	to              Theme
	fading, waiting bool
	u               float32
	// swapping is unused. A real veil that is still missing at u = 1
	// stays tinted, then snaps in on the frame it is published.
	swapping bool
	swapU    float32
	note     func(veilGeom)
	// base is the veil already on screen when this fade started. A switch
	// that arrives mid-fade keeps every layer of that blend.
	base []veilSlice
}

// veilSlice is one theme's veil and the opacity it was drawn at.
type veilSlice struct {
	look  Theme
	alpha float32
}

type veilSettle struct {
	pending   bool
	pendingAt time.Time
}

var (
	veilCacheMu sync.Mutex
	veilEntries = map[veilKey]*veilEntry{}
	veilLive    [3]*veilEntry
	veilHold    [3]veilSettle

	veilJobMu      sync.Mutex
	veilJobDone    *sync.Cond
	veilPending    = map[veilKey]veilWork{}
	veilJobRunning bool
	veilGen        atomic.Uint64
	veilSeq        atomic.Uint64
)

type veilWork struct {
	base  *image.NRGBA
	geom  veilGeom
	layer ui.Color
	gen   uint64
}

func init() {
	veilJobDone = sync.NewCond(&veilJobMu)
}

func bumpVeilJobs() {
	veilGen.Add(1)
}

func resetVeilCache() {
	awaitVeils()
	veilCacheMu.Lock()
	veilEntries = map[veilKey]*veilEntry{}
	veilLive = [3]*veilEntry{}
	veilHold = [3]veilSettle{}
	veilCacheMu.Unlock()
	veilJobMu.Lock()
	veilPending = map[veilKey]veilWork{}
	veilJobMu.Unlock()
}

func awaitVeils() {
	veilJobMu.Lock()
	for veilJobRunning || len(veilPending) > 0 {
		veilJobDone.Wait()
	}
	veilJobMu.Unlock()
}

func (a *App) noteVeil(g veilGeom) {
	if !a.veilNoted {
		a.veilGeoms = a.veilGeoms[:0]
		a.veilNoted = true
	}
	a.veilGeoms = append(a.veilGeoms, g)
	a.veilFresh = true
	a.veilWinW, a.veilWinH = g.winW, g.winH
}

func veilReady(g veilGeom, seq uint64, layer uint32) bool {
	veilCacheMu.Lock()
	_, ok := veilEntries[g.key(seq, layer)]
	veilCacheMu.Unlock()
	return ok
}

// paintCardVeil draws the cached veil over whatever the transcript has
// already painted. A miss builds the bitmap off the frame, except in
// instant mode, which renders it here so screenshots stay exact.
func paintCardVeil(p *ui.Painter, r ui.Rect, winW, winH float32, look Theme, kind int, composerH float32, alphaAt func(float32) float32, phase veilPaint) {
	t0 := time.Now()
	defer func() {
		veilCalls.Add(1)
		veilNanos.Add(int64(time.Since(t0)))
	}()
	if r.H <= 0 || r.W <= 0 || winH < 1 || winW < 1 {
		return
	}
	scale := p.Scale()
	if scale < 1 {
		scale = 1
	}
	x0, pw := deviceSpan(r.X, r.X+r.W, scale)
	y0, ph := deviceSpan(r.Y, r.Y+r.H, scale)
	if pw < 1 || ph < 1 {
		return
	}
	_, winPW := deviceSpan(0, winW, scale)
	_, winPH := deviceSpan(0, winH, scale)
	g := veilGeom{
		kind: kind, pw: pw, ph: ph, x0: x0, y0: y0,
		winW: winPW, winH: winPH,
		scaleMilli:    int(math.Round(float64(scale * 1000))),
		rectX:         int(math.Round(float64(r.X * 1000))),
		rectY:         int(math.Round(float64(r.Y * 1000))),
		rectW:         int(math.Round(float64(r.W * 1000))),
		rectH:         int(math.Round(float64(r.H * 1000))),
		composerMilli: int(math.Round(float64(composerH * 1000))),
	}
	if phase.note != nil {
		phase.note(g)
	}
	if phase.fading {
		paintVeilFade(p, r, look, g, alphaAt, phase)
		return
	}
	if bm := acquireVeil(p, look, g, alphaAt, true, true); bm != nil {
		blitVeil(p, bm, r, 1)
	}
	if phase.waiting {
		acquireVeil(p, phase.to, g, nil, false, true)
	}
}

// paintVeilFade draws the veil already on screen under the target veil.
// The base stays at the alphas it had, and the incoming veil is
// source-over at the clock alpha. While the target bitmap is still
// missing, the base stays up so the pane does not uncover the transcript.
// A bitmap that arrives after the clock is drawn at u = 1 on the frame
// it is ready.
func paintVeilFade(p *ui.Painter, r ui.Rect, look Theme, g veilGeom, alphaAt func(float32) float32, phase veilPaint) {
	target := acquireVeil(p, phase.to, g, nil, false, false)
	if target != nil && phase.u > 0 && phase.u >= 1 {
		blitVeil(p, target, r, 1)
		return
	}
	if phase.u < 1 || target == nil {
		paintVeilBase(p, r, look, g, alphaAt, phase)
	}
	if target != nil && phase.u > 0 {
		opacity := phase.u
		if phase.swapping {
			opacity = phase.swapU
		}
		blitVeil(p, target, r, opacity)
	}
}

func paintVeilBase(p *ui.Painter, r ui.Rect, look Theme, g veilGeom, alphaAt func(float32) float32, phase veilPaint) {
	if len(phase.base) > 0 {
		for _, layer := range phase.base {
			if layer.alpha <= 0 {
				continue
			}
			bm := acquireVeil(p, layer.look, g, alphaAt, true, false)
			if bm == nil {
				bm = stretchedVeil(g)
			}
			blitVeil(p, bm, r, layer.alpha)
		}
		return
	}
	bm := acquireVeil(p, look, g, alphaAt, true, false)
	if bm == nil {
		bm = stretchedVeil(g)
	}
	blitVeil(p, bm, r, 1)
}

func blitVeil(p *ui.Painter, bm *ui.Bitmap, r ui.Rect, opacity float32) {
	if bm == nil || opacity <= 0 {
		return
	}
	opt := ui.ImageOptions{}
	if opacity < 1 {
		opt.Tint = ui.Color{R: 255, G: 255, B: 255, A: alphaByte(opacity)}
	}
	p.ImageRegion(bm, ui.Rect{}, r, opt)
	themeLogDrawn(themeEventVeil, bm)
}

// acquireVeil returns the cached veil. poke arms a painter repaint when
// the bitmap is still missing; the theme clock passes false, because a
// repaint would move the veil without rebuilding the control mix.
func acquireVeil(p *ui.Painter, look Theme, g veilGeom, alphaAt func(float32) float32, allowStretch, poke bool) *ui.Bitmap {
	var e *gradEntry
	if gradSync.Load() {
		e = gradientEntrySync(look, g.winW, g.winH)
	} else {
		e = cachedGradient(look, g.winW, g.winH)
		if e == nil {
			ensureGradient(look, g.winW, g.winH)
			// The size already changed. Start the settle clock now, so the
			// veil builds on the frame the new background arrives instead of
			// waiting out gradSettle a second time.
			veilArm(g, p.Now())
			if poke {
				p.After(8 * time.Millisecond)
			}
			if allowStretch {
				return stretchedVeil(g)
			}
			return nil
		}
	}
	if e == nil || e.img == nil {
		return nil
	}
	key := g.key(e.seq, packColor(look.LayerContent))
	if v := lookupVeil(key); v != nil {
		rememberVeil(g.kind, v)
		return v.bm
	}
	if gradSync.Load() {
		img := renderVeil(e.img, g, look.LayerContent, alphaAt)
		v := publishVeil(key, img)
		rememberVeil(g.kind, v)
		return v.bm
	}
	if !veilDue(g, p.Now()) {
		if poke {
			p.After(8 * time.Millisecond)
		}
		if allowStretch {
			return stretchedVeil(g)
		}
		return nil
	}
	launchVeil(key, e.img, g, look.LayerContent)
	if poke {
		p.After(8 * time.Millisecond)
	}
	if allowStretch {
		return stretchedVeil(g)
	}
	return nil
}

func lookupVeil(key veilKey) *veilEntry {
	veilCacheMu.Lock()
	v := veilEntries[key]
	veilCacheMu.Unlock()
	return v
}

func rememberVeil(kind int, v *veilEntry) {
	if kind < 0 || kind >= len(veilLive) || v == nil {
		return
	}
	veilCacheMu.Lock()
	veilLive[kind] = v
	veilHold[kind] = veilSettle{}
	veilCacheMu.Unlock()
}

func stretchedVeil(g veilGeom) *ui.Bitmap {
	if g.kind < 0 || g.kind >= len(veilLive) {
		return nil
	}
	veilCacheMu.Lock()
	v := veilLive[g.kind]
	veilCacheMu.Unlock()
	if v == nil {
		return nil
	}
	return v.bm
}

// veilArm records that the veil's pixel rect moved. It does not clear the
// clock, so a caller that cannot build yet (the gradient is still in
// flight) still gets a rebuild once the size has held still.
func veilArm(g veilGeom, now time.Time) {
	if g.kind < 0 || g.kind >= len(veilLive) {
		return
	}
	veilCacheMu.Lock()
	defer veilCacheMu.Unlock()
	last := veilLive[g.kind]
	if last == nil {
		return
	}
	if last.key.pw == g.pw && last.key.ph == g.ph && last.key.x0 == g.x0 && last.key.y0 == g.y0 {
		return
	}
	st := &veilHold[g.kind]
	if !st.pending {
		st.pending = true
		st.pendingAt = now
	}
}

// veilDue reports whether a size change has held still long enough to
// rebuild. A theme change at the same pixel rect builds immediately.
func veilDue(g veilGeom, now time.Time) bool {
	if g.kind < 0 || g.kind >= len(veilLive) {
		return true
	}
	veilCacheMu.Lock()
	defer veilCacheMu.Unlock()
	last := veilLive[g.kind]
	if last == nil {
		return true
	}
	moved := last.key.pw != g.pw || last.key.ph != g.ph || last.key.x0 != g.x0 || last.key.y0 != g.y0
	if !moved {
		return true
	}
	st := &veilHold[g.kind]
	if !st.pending {
		st.pending = true
		st.pendingAt = now
	}
	if now.Before(st.pendingAt.Add(gradSettle)) {
		return false
	}
	st.pending = false
	return true
}

func launchVeil(key veilKey, base *image.NRGBA, g veilGeom, layer ui.Color) {
	if base == nil {
		return
	}
	gen := veilGen.Load()
	veilJobMu.Lock()
	defer veilJobMu.Unlock()
	veilCacheMu.Lock()
	_, have := veilEntries[key]
	veilCacheMu.Unlock()
	if have {
		return
	}
	veilPending[key] = veilWork{base: base, geom: g, layer: layer, gen: gen}
	if !veilJobRunning {
		veilJobRunning = true
		go veilWorker()
	}
}

func takeVeil() (veilKey, veilWork, bool) {
	veilJobMu.Lock()
	defer veilJobMu.Unlock()
	for k, w := range veilPending {
		delete(veilPending, k)
		return k, w, true
	}
	veilJobRunning = false
	veilJobDone.Broadcast()
	return veilKey{}, veilWork{}, false
}

func veilWorker() {
	for {
		key, work, ok := takeVeil()
		if !ok {
			return
		}
		if veilGen.Load() != work.gen {
			continue
		}
		img := renderVeil(work.base, work.geom, work.layer, nil)
		if veilGen.Load() != work.gen || img == nil {
			continue
		}
		publishVeil(key, img)
		wakeGradient()
	}
}

func publishVeil(key veilKey, img *image.RGBA) *veilEntry {
	bm := ownBitmapRGBA(img, true)
	if bm == nil {
		return nil
	}
	gen := veilSeq.Add(1)
	rememberBitmap(bm, gen)
	v := &veilEntry{key: key, bm: bm, gen: gen}
	veilCacheMu.Lock()
	defer veilCacheMu.Unlock()
	if old := veilEntries[key]; old != nil {
		return old
	}
	veilEntries[key] = v
	if len(veilEntries) > veilCacheCap {
		for k, e := range veilEntries {
			if e == v || veilPinned(e) {
				continue
			}
			delete(veilEntries, k)
			if len(veilEntries) <= veilCacheCap {
				break
			}
		}
	}
	return v
}

func veilPinned(e *veilEntry) bool {
	for _, live := range veilLive {
		if live == e {
			return true
		}
	}
	return false
}

// renderVeil paints the card color (the card composited on the window
// pixel) and bakes the vertical ramp into the alpha. Every pixel in a
// row uses the same alpha. alphaAt nil uses the geometry's curve. The
// result is premultiplied and does not contain transcript pixels.
func renderVeil(base *image.NRGBA, g veilGeom, layer ui.Color, alphaAt func(float32) float32) *image.RGBA {
	veilBuilds.Add(1)
	if base == nil || g.pw < 1 || g.ph < 1 {
		return nil
	}
	if alphaAt == nil {
		alphaAt = g.alphaAt
	}
	scale := float32(g.scaleMilli) / 1000
	if scale < 1 {
		scale = 1
	}
	img := image.NewRGBA(image.Rect(0, 0, g.pw, g.ph))
	bw, bh := base.Rect.Dx(), base.Rect.Dy()
	for j := 0; j < g.ph; j++ {
		py := g.y0 + j
		if py < 0 {
			py = 0
		}
		if py >= bh {
			py = bh - 1
		}
		y := (float32(g.y0+j) + 0.5) / scale
		a := alphaByte(alphaAt(y))
		row := img.Pix[j*img.Stride : j*img.Stride+g.pw*4]
		for i := 0; i < g.pw; i++ {
			px := g.x0 + i
			if px < 0 {
				px = 0
			}
			if px >= bw {
				px = bw - 1
			}
			bi := base.PixOffset(px, py)
			over := layer.Over(ui.Color{R: base.Pix[bi], G: base.Pix[bi+1], B: base.Pix[bi+2], A: 255})
			over.A = a
			pr, pg, pb, pa := premul(over)
			o := i * 4
			row[o] = pr
			row[o+1] = pg
			row[o+2] = pb
			row[o+3] = pa
		}
	}
	return img
}
