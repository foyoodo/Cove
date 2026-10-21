package main

import (
	"image"
	"math"
	"sync/atomic"

	"cove/themes"

	"github.com/egoist/mygo/ui"
)

// Window background.
//
// MyGo can draw a bitmap as the window background. Painter.ImageRegion
// draws a ui.Bitmap made with ui.NewBitmap; Pixelated samples it 1:1 when
// the bitmap is the snapped device-pixel size of the destination (the
// shrink is ≤ 1, so the GPU does not blend neighbors). There is no CGImage
// path. The bitmap is uploaded as a texture.
//
// The ramp is one function of (x, y), cached per pixel size and stop
// colors. A resize or a theme change builds a new image. The composer
// veil reads those pixels once, off the frame, and composites the card
// color on them with the vertical alpha ramp. Each frame draws that
// bitmap over the live transcript.

type oklab struct{ l, a, b float64 }

type gradSpline struct {
	x []float64
	y []float64
	m []float64
}

type preparedGrad struct {
	l, a, b    gradSpline
	tint       [3]float64 // linear sRGB
	tintA      float64
	tintStart  float64
	tintEnd    float64
	top, mid   uint32
	low, bot   uint32
	tintPacked uint32
}

var (
	veilCalls  atomic.Int64
	veilNanos  atomic.Int64
	veilBuilds atomic.Int64
)

func resetVeilStats() {
	veilCalls.Store(0)
	veilNanos.Store(0)
	veilBuilds.Store(0)
}

// bayer8 is the 8×8 ordered-dither matrix, values 0..63.
var bayer8 = [8][8]uint8{
	{0, 32, 8, 40, 2, 34, 10, 42},
	{48, 16, 56, 24, 50, 18, 58, 26},
	{12, 44, 4, 36, 14, 46, 6, 38},
	{60, 28, 52, 20, 62, 30, 54, 22},
	{3, 35, 11, 43, 1, 33, 9, 41},
	{51, 19, 59, 27, 49, 17, 57, 25},
	{15, 47, 7, 39, 13, 45, 5, 37},
	{63, 31, 55, 23, 61, 29, 53, 21},
}

func packColor(c ui.Color) uint32 {
	return uint32(c.R)<<24 | uint32(c.G)<<16 | uint32(c.B)<<8 | uint32(c.A)
}

func prepareWindowGrad(look Theme) preparedGrad {
	geo := themes.WindowGradient()
	colors := make([]ui.Color, len(geo.Names))
	for i, name := range geo.Names {
		colors[i] = stopColor(look, name)
	}
	n := len(colors)
	pg := preparedGrad{
		tint:       linearSRGB(look.WindowBaseTint),
		tintA:      float64(look.WindowBaseTint.A) / 255,
		tintStart:  geo.TintStart,
		tintEnd:    geo.TintEnd,
		tintPacked: packColor(look.WindowBaseTint),
	}
	if n >= 1 {
		pg.top = packColor(colors[0])
	}
	if n >= 2 {
		pg.mid = packColor(colors[1])
	}
	if n >= 3 {
		pg.low = packColor(colors[2])
	}
	if n >= 4 {
		pg.bot = packColor(colors[3])
	}
	xs := append([]float64(nil), geo.Stops...)
	ls := make([]oklab, n)
	for i, c := range colors {
		ls[i] = rgbOKLab(c)
	}
	pg.l = splineOf(xs, labs(ls, 0))
	pg.a = splineOf(xs, labs(ls, 1))
	pg.b = splineOf(xs, labs(ls, 2))
	return pg
}

func labs(ls []oklab, ch int) []float64 {
	out := make([]float64, len(ls))
	for i, v := range ls {
		switch ch {
		case 0:
			out[i] = v.l
		case 1:
			out[i] = v.a
		default:
			out[i] = v.b
		}
	}
	return out
}

func stopColor(look Theme, name string) ui.Color {
	switch name {
	case "window/baseTop":
		return look.WindowBaseTop
	case "window/baseMid":
		return look.WindowBaseMid
	case "window/baseLow":
		return look.WindowBaseLow
	case "window/baseBottom":
		return look.WindowBaseBottom
	default:
		return look.WindowBaseBottom
	}
}

func splineOf(x, y []float64) gradSpline {
	s := gradSpline{x: x, y: y, m: monotoneTangents(x, y)}
	return s
}

// monotoneTangents is Fritsch–Carlson. A sign change or a flat segment
// gets a zero slope, so a valley is smooth instead of a kink.
func monotoneTangents(x, y []float64) []float64 {
	n := len(y) - 1
	d := make([]float64, n)
	for i := 0; i < n; i++ {
		d[i] = (y[i+1] - y[i]) / (x[i+1] - x[i])
	}
	m := make([]float64, n+1)
	if n == 0 {
		return m
	}
	m[0] = d[0]
	m[n] = d[n-1]
	for i := 1; i < n; i++ {
		if d[i-1] == 0 || d[i] == 0 || (d[i-1] > 0) != (d[i] > 0) {
			m[i] = 0
			continue
		}
		m[i] = (d[i-1] + d[i]) / 2
	}
	for i := 0; i < n; i++ {
		if d[i] == 0 {
			m[i] = 0
			m[i+1] = 0
			continue
		}
		a := m[i] / d[i]
		b := m[i+1] / d[i]
		h := a*a + b*b
		if h > 9 {
			tau := 3 / math.Sqrt(h)
			m[i] = tau * a * d[i]
			m[i+1] = tau * b * d[i]
		}
	}
	return m
}

func (s gradSpline) eval(t float64) float64 {
	n := len(s.y)
	if n == 0 {
		return 0
	}
	if t <= s.x[0] {
		return s.y[0]
	}
	if t >= s.x[n-1] {
		return s.y[n-1]
	}
	i := 0
	for i < n-2 && t > s.x[i+1] {
		i++
	}
	h := s.x[i+1] - s.x[i]
	u := 0.0
	if h > 0 {
		u = (t - s.x[i]) / h
	}
	u2 := u * u
	u3 := u2 * u
	h00 := 2*u3 - 3*u2 + 1
	h10 := u3 - 2*u2 + u
	h01 := -2*u3 + 3*u2
	h11 := u3 - u2
	return h00*s.y[i] + h10*h*s.m[i] + h01*s.y[i+1] + h11*h*s.m[i+1]
}

func smoothstep01(t float64) float64 {
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	return t * t * (3 - 2*t)
}

// sampleBase is the window color at a unit point, in straight sRGB 0..1,
// before the dither. y is the monotone cubic through the stops in OKLab.
// x fades the tint in with smoothstep.
func sampleBase(g preparedGrad, x, y float64) (r, gch, b float64) {
	lab := oklab{g.l.eval(y), g.a.eval(y), g.b.eval(y)}
	lr, lg, lb := oklabLinear(lab)
	k := 0.0
	span := g.tintEnd - g.tintStart
	if span > 0 {
		k = smoothstep01((x - g.tintStart) / span)
	}
	a := g.tintA * k
	lr = lr*(1-a) + g.tint[0]*a
	lg = lg*(1-a) + g.tint[1]*a
	lb = lb*(1-a) + g.tint[2]*a
	return encodeSRGB(lr), encodeSRGB(lg), encodeSRGB(lb)
}

// bayerOffset is the 8×8 Bayer threshold in [0, 1], one value for R, G,
// and B. Magnitude is one code at most (amplitude ≤ 1/255).
func bayerOffset(px, py int) float64 {
	return float64(bayer8[py&7][px&7]) / 63
}

// quantChannel is the only 8-bit step. Dithered noise is the Bayer
// threshold in [0, 1], added before truncation. Undithered rounds to
// nearest so the comparison image is the plain ramp.
func quantChannel(v float64, dither bool, noise float64) int {
	var n float64
	if dither {
		n = math.Floor(v*255 + noise)
	} else {
		n = math.Floor(v*255 + 0.5)
	}
	if n <= 0 {
		return 0
	}
	if n >= 255 {
		return 255
	}
	return int(n)
}

// renderGradientRef is the previous per-pixel renderer. Tests compare the
// fast path with it; nothing on the frame path calls it.
func renderGradientRef(look Theme, w, h int, dither bool) *image.NRGBA {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	g := prepareWindowGrad(look)
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	fw, fh := float64(w), float64(h)
	for py := 0; py < h; py++ {
		y := (float64(py) + 0.5) / fh
		row := img.Pix[py*img.Stride : py*img.Stride+w*4]
		for px := 0; px < w; px++ {
			x := (float64(px) + 0.5) / fw
			r, gc, b := sampleBase(g, x, y)
			ideal := [3]float64{r * 255, gc * 255, b * 255}
			var q [3]int
			noise := 0.0
			if dither {
				noise = bayerOffset(px, py)
			}
			for c := range q {
				q[c] = quantChannel(ideal[c]/255, dither, noise)
			}
			if dither {
				q = limitDither(img, px, py, q, ideal)
			}
			o := px * 4
			row[o] = uint8(q[0])
			row[o+1] = uint8(q[1])
			row[o+2] = uint8(q[2])
			row[o+3] = 255
		}
	}
	return img
}

// limitDither keeps the Bayer code, then enforces the two acceptance
// bounds: adjacent pixels differ by at most 1 per channel, and no run
// of identical pixels is longer than 24 (SPEC §4.5). The same Bayer
// offset was already added to every channel; the limiter only pulls a
// code back into that range, which is what stops a 2-code step where a
// steep sample meets the two ends of the matrix.
func limitDither(img *image.NRGBA, x, y int, q [3]int, ideal [3]float64) [3]int {
	lo, hi := feasible(img, x, y)
	for c := range q {
		if q[c] < lo[c] {
			q[c] = lo[c]
		}
		if q[c] > hi[c] {
			q[c] = hi[c]
		}
	}
	const band = 24
	vRun, hRun := 0, 0
	var up, left [3]int
	if y > 0 {
		up = pixAt(img, x, y-1)
		vRun = pixelRun(img, x, y, 0, -1)
	}
	if x > 0 {
		left = pixAt(img, x-1, y)
		hRun = pixelRun(img, x, y, -1, 0)
	}
	if (vRun >= band && q == up) || (hRun >= band && q == left) {
		if moved, ok := nudgePixel(q, ideal, lo, hi, up, left, vRun >= band, hRun >= band); ok {
			q = moved
		}
	}
	return q
}

func feasible(img *image.NRGBA, x, y int) (lo, hi [3]int) {
	for c := range lo {
		lo[c], hi[c] = 0, 255
	}
	if x > 0 {
		left := pixAt(img, x-1, y)
		for c := range lo {
			lo[c] = max(lo[c], left[c]-1)
			hi[c] = min(hi[c], left[c]+1)
		}
	}
	if y > 0 {
		up := pixAt(img, x, y-1)
		for c := range lo {
			lo[c] = max(lo[c], up[c]-1)
			hi[c] = min(hi[c], up[c]+1)
		}
	}
	for c := range lo {
		if hi[c] < lo[c] {
			// The two neighbors are already a legal pair, so the
			// overlap is never empty. Keep the code in range anyway.
			hi[c] = lo[c]
		}
	}
	return lo, hi
}

func pixAt(img *image.NRGBA, x, y int) [3]int {
	i := img.PixOffset(x, y)
	s := img.Pix
	return [3]int{int(s[i]), int(s[i+1]), int(s[i+2])}
}

// pixelRun counts how many pixels in dir match the pixel next to (x, y).
func pixelRun(img *image.NRGBA, x, y, dx, dy int) int {
	v := pixAt(img, x+dx, y+dy)
	run := 1
	for i := 2; i <= 24; i++ {
		xx, yy := x+dx*i, y+dy*i
		if xx < 0 || yy < 0 || xx >= img.Rect.Dx() || yy >= img.Rect.Dy() {
			break
		}
		if pixAt(img, xx, yy) != v {
			break
		}
		run++
	}
	return run
}

// nudgePixel moves one channel by one code so a run already at the 24px
// band limit does not grow. The step stays inside the neighbor bounds,
// so it cannot open a 2-code gap. Among legal steps, the one closest to
// the float sample wins; a step that would keep a full run is skipped.
func nudgePixel(q [3]int, ideal [3]float64, lo, hi [3]int, up, left [3]int, vFull, hFull bool) ([3]int, bool) {
	best := q
	bestDist := math.Inf(1)
	found := false
	for c := 0; c < 3; c++ {
		for _, to := range []int{q[c] - 1, q[c] + 1} {
			if to < lo[c] || to > hi[c] {
				continue
			}
			cand := q
			cand[c] = to
			if vFull && cand == up {
				continue
			}
			if hFull && cand == left {
				continue
			}
			dist := math.Abs(float64(to) - ideal[c])
			if dist < bestDist {
				bestDist = dist
				best = cand
				found = true
			}
		}
	}
	return best, found
}

func (p preparedGrad) same(o preparedGrad) bool {
	return p.top == o.top && p.mid == o.mid && p.low == o.low && p.bot == o.bot && p.tintPacked == o.tintPacked
}

func deviceSpan(v0, v1, scale float32) (int, int) {
	a := int(math.Round(float64(v0 * scale)))
	b := int(math.Round(float64(v1 * scale)))
	if b < a {
		b = a
	}
	return a, b - a
}

func alphaByte(a float32) uint8 {
	if a <= 0 {
		return 0
	}
	if a >= 1 {
		return 255
	}
	return uint8(a*255 + 0.5)
}

func premul(c ui.Color) (r, g, b, a uint8) {
	a = c.A
	if a == 0 {
		return 0, 0, 0, 0
	}
	if a == 255 {
		return c.R, c.G, c.B, 255
	}
	r = uint8((uint16(c.R)*uint16(a) + 127) / 255)
	g = uint8((uint16(c.G)*uint16(a) + 127) / 255)
	b = uint8((uint16(c.B)*uint16(a) + 127) / 255)
	return r, g, b, a
}

func rgbOKLab(c ui.Color) oklab {
	r, g, b := linearOf(c.R), linearOf(c.G), linearOf(c.B)
	l := 0.4122214708*r + 0.5363325363*g + 0.0514459929*b
	m := 0.2119034982*r + 0.6806995451*g + 0.1073969566*b
	s := 0.0883024619*r + 0.2817188376*g + 0.6299787005*b
	l, m, s = math.Cbrt(l), math.Cbrt(m), math.Cbrt(s)
	return oklab{
		l: 0.2104542553*l + 0.7936177850*m - 0.0040720468*s,
		a: 1.9779984951*l - 2.4285922050*m + 0.4505937099*s,
		b: 0.0259040371*l + 0.7827717662*m - 0.8086757660*s,
	}
}

func oklabLinear(c oklab) (r, g, b float64) {
	l_ := c.l + 0.3963377774*c.a + 0.2158037573*c.b
	m_ := c.l - 0.1055613458*c.a - 0.0638541728*c.b
	s_ := c.l - 0.0894841775*c.a - 1.2914855480*c.b
	l := l_ * l_ * l_
	m := m_ * m_ * m_
	s := s_ * s_ * s_
	r = 4.0767416621*l - 3.3077115913*m + 0.2309699292*s
	g = -1.2684380046*l + 2.6097574011*m - 0.3413193965*s
	b = -0.0041960863*l - 0.7034186147*m + 1.7076147010*s
	return r, g, b
}

func linearSRGB(c ui.Color) [3]float64 {
	return [3]float64{linearOf(c.R), linearOf(c.G), linearOf(c.B)}
}

func linearOf(v uint8) float64 {
	c := float64(v) / 255
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func encodeSRGB(c float64) float64 {
	if c <= 0 {
		return 0
	}
	if c >= 1 {
		return 1
	}
	if c <= 0.0031308 {
		return 12.92 * c
	}
	return 1.055*math.Pow(c, 1/2.4) - 0.055
}
