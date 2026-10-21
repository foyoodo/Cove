// Package inkgap measures shortcut gaps from the ink in a delivered bitmap.
// A column counts as ink once its coverage exceeds a tenth of the band's
// ink, including the faint end of an anti-aliased stem. The edge sits inside
// that outer column by the uncovered fraction of the pixel.
package inkgap

import (
	"image"
	"image/color"
	"image/draw"
	"math"
)

// Edge is one glyph's ink, in image pixels. Left and Right include the
// faint anti-aliased columns at the stem.
type Edge struct {
	Left, Right float64
}

// Band is one horizontal run of text and the gaps between its glyphs,
// in points. Scale is the device pixels per point the bitmap was drawn at.
type Band struct {
	Y0, Y1 int
	Edges  []Edge
	Gaps   []float64
}

// Measure returns every text band in img. scale is device pixels per point.
func Measure(img image.Image, scale float64) []Band {
	if scale <= 0 {
		scale = 1
	}
	rgba := toRGBA(img)
	b := rgba.Bounds()
	if b.Empty() {
		return nil
	}
	// Crops and single-glyph probes extend through the faint stem column.
	// FocusGaps finds the line without that extension, so a hairline
	// cannot bridge two glyphs, then remeasures the line with it.
	return measureJoin(rgba, scale, 0.35, 0.10)
}

// measureJoin segments glyphs at join times the band peak, then extends
// each glyph through adjacent columns down to extend (0 disables it).
func measureJoin(rgba *image.RGBA, scale, join, extend float64) []Band {
	b := rgba.Bounds()
	rowInk := make([]int, b.Dy())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if deltaAt(rgba, x, y, b) > 18 {
				rowInk[y-b.Min.Y]++
			}
		}
	}
	var bands []Band
	y := 0
	for y < len(rowInk) {
		if rowInk[y] < 2 {
			y++
			continue
		}
		y0 := y
		for y < len(rowInk) && rowInk[y] >= 2 {
			y++
		}
		band := bandAt(rgba, b.Min.Y+y0, b.Min.Y+y, scale, join, extend)
		if len(band.Edges) >= 1 {
			bands = append(bands, band)
		}
	}
	return bands
}

// FocusEdges is the ink of the control line, remeasured on the glyph
// rows with the faint stem column included. A wide hole in the frame is
// dropped, and the edges after the last such hole are the line.
func FocusEdges(img image.Image, scale float64) []Edge {
	// 0.35, with no faint-column extension, separates glyphs in a frame
	// that also contains a hairline. The edges below are remeasured
	// with the extension.
	bands := measureJoin(toRGBA(img), scale, 0.35, 0)
	if len(bands) == 0 {
		return nil
	}
	best := bands[0]
	for _, b := range bands[1:] {
		if len(b.Edges) > len(best.Edges) {
			best = b
		}
	}
	cut := -1
	for i, g := range best.Gaps {
		// A wide hole with a real line after it separates the control from
		// other glyphs in the same band. A trailing hole is just margin.
		if g > 20 && len(best.Gaps)-i-1 >= 4 {
			cut = i
		}
	}
	rgba := toRGBA(img)
	b := rgba.Bounds()
	edge0 := 0
	if cut >= 0 && cut+1 < len(best.Edges) {
		edge0 = cut + 1
	}
	x0 := int(math.Floor(best.Edges[edge0].Left)) - 8
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	// Stop before the next control (the send button sits in the same
	// band). Its ink would raise the peak and drop the faint stem columns.
	x1 := b.Max.X
	for i := edge0; i < len(best.Gaps); i++ {
		// 8pt is wider than any shortcut gap (5pt) and narrower than the
		// space before a neighboring control such as the stop button.
		if best.Gaps[i] > 8 && i+1 < len(best.Edges) {
			x1 = int(math.Ceil(best.Edges[i].Right)) + 16
			break
		}
	}
	if x1 == b.Max.X && len(best.Edges) > 0 {
		x1 = int(math.Ceil(best.Edges[len(best.Edges)-1].Right)) + 16
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if x1 <= x0 {
		if cut >= 0 && cut+1 < len(best.Edges) {
			return best.Edges[cut+1:]
		}
		return best.Edges
	}
	y0, y1 := glyphRows(rgba, x0, x1, best.Y0, best.Y1)
	sub := rgba.SubImage(image.Rect(x0, y0, x1, y1))
	focused := measureJoin(toRGBA(sub), scale, 0.35, 0.10)
	if len(focused) == 0 {
		if cut < 0 {
			return best.Edges
		}
		if cut+1 < len(best.Edges) {
			return best.Edges[cut+1:]
		}
		return best.Edges
	}
	out := focused[0]
	for _, band := range focused[1:] {
		if len(band.Edges) > len(out.Edges) {
			out = band
		}
	}
	return out.Edges
}

// FocusGaps is the glyph gaps of the control line in a screenshot.
func FocusGaps(img image.Image, scale float64) []float64 {
	edges := FocusEdges(img, scale)
	if len(edges) < 2 {
		return nil
	}
	var gaps []float64
	for i := 0; i+1 < len(edges); i++ {
		gaps = append(gaps, (edges[i+1].Left-edges[i].Right)/scale)
	}
	return gaps
}

// Gaps is the point gap between successive glyphs of the band that contains
// the most glyphs. A crop of one control line has one such band.
func Gaps(img image.Image, scale float64) []float64 {
	bands := Measure(img, scale)
	if len(bands) == 0 {
		return nil
	}
	best := bands[0]
	for _, b := range bands[1:] {
		if len(b.Edges) > len(best.Edges) {
			best = b
		}
	}
	return best.Gaps
}

// glyphRows is the contiguous run of high-contrast rows in [y0, y1)
// across [x0, x1). A hairline is wide but faint, so it stays out and the
// half-coverage edge is taken from the glyphs alone.
func glyphRows(img *image.RGBA, x0, x1, y0, y1 int) (int, int) {
	b := img.Bounds()
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	type run struct{ y0, y1, score int }
	var best run
	var cur run
	open := false
	for y := y0; y < y1; y++ {
		contrast := rowContrast(img, x0, x1, y)
		if contrast >= 160 {
			if !open {
				cur = run{y0: y}
				open = true
			}
			cur.y1 = y + 1
			cur.score += contrast
			continue
		}
		if open {
			if cur.score > best.score {
				best = cur
			}
			open = false
		}
	}
	if open && cur.score > best.score {
		best = cur
	}
	if best.y1 <= best.y0 {
		return y0, y1
	}
	top := best.y0 - 1
	bot := best.y1 + 1
	if top < y0 {
		top = y0
	}
	if bot > y1 {
		bot = y1
	}
	return top, bot
}

func rowContrast(img *image.RGBA, x0, x1, y int) int {
	minS, maxS := 255*3, 0
	for x := x0; x < x1; x++ {
		px := img.RGBAAt(x, y)
		s := int(px.R) + int(px.G) + int(px.B)
		if s < minS {
			minS = s
		}
		if s > maxS {
			maxS = s
		}
	}
	return maxS - minS
}

func bandAt(img *image.RGBA, y0, y1 int, scale, join, extend float64) Band {
	b := img.Bounds()
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	bg := surfaceColor(img, y0, y1)
	n := b.Dx()
	cov := make([]float64, n)
	var peak float64
	for x := 0; x < n; x++ {
		xx := b.Min.X + x
		maxD := 0
		for y := y0; y < y1; y++ {
			if d := deltaRGB(img.RGBAAt(xx, y), bg); d > maxD {
				maxD = d
			}
		}
		cov[x] = float64(maxD)
		if cov[x] > peak {
			peak = cov[x]
		}
	}
	band := Band{Y0: y0, Y1: y1}
	if peak < 24 {
		return band
	}
	// The core of a glyph is coverage >= join (0.35), so a hairline or a
	// 1pt hole does not bridge two keys. extend (0.10) then pulls in the
	// adjacent faint stem column, about 0.13 on the leading edge of "n".
	var segs [][2]int
	in := false
	start := 0
	for x, c := range cov {
		on := c >= peak*join
		if on && !in {
			in = true
			start = x
		}
		if in && (!on || x == n-1) {
			end := x - 1
			if on && x == n-1 {
				end = x
			}
			if end >= start {
				segs = append(segs, [2]int{start, end})
			}
			in = false
		}
	}
	floor := peak * extend
	for i, s := range segs {
		a, b1 := s[0], s[1]
		if extend > 0 {
			// One faint column, and only when the next core is more than
			// a couple of pixels away. A 1pt key gap is about two pixels;
			// extending both sides of it would close the hole.
			if i > 0 && a-segs[i-1][1] > 2 && a > 0 && cov[a-1] >= floor {
				a--
			}
			if i+1 < len(segs) && segs[i+1][0]-b1 > 2 && b1+1 < n && cov[b1+1] >= floor {
				b1++
			}
			if i == 0 && a > 0 && cov[a-1] >= floor {
				a--
			}
			if i+1 == len(segs) && b1+1 < n && cov[b1+1] >= floor {
				b1++
			}
		}
		if e, ok := edgeOf(cov, a, b1, b.Min.X, peak); ok {
			band.Edges = append(band.Edges, e)
		}
	}
	// A hole under about half a point is the counter of one glyph, not a gap
	// between keys. Join those pieces before measuring.
	if len(band.Edges) > 1 {
		merged := []Edge{band.Edges[0]}
		for _, e := range band.Edges[1:] {
			prev := &merged[len(merged)-1]
			if (e.Left-prev.Right)/scale < 0.25 {
				if e.Right > prev.Right {
					prev.Right = e.Right
				}
				continue
			}
			merged = append(merged, e)
		}
		band.Edges = merged
	}
	for i := 0; i+1 < len(band.Edges); i++ {
		band.Gaps = append(band.Gaps, (band.Edges[i+1].Left-band.Edges[i].Right)/scale)
	}
	return band
}

// edgeOf places the ink edge inside the outer column. Coverage c fills
// fraction c of that pixel, measured from the outside: a 0.13 column on
// the left puts the edge 0.87px into the pixel. cov is indexed from the
// image's minimum x; x0 is that minimum. peak is the band's ink extreme.
func edgeOf(cov []float64, start, end, x0 int, peak float64) (Edge, bool) {
	if peak < 24 || start > end {
		return Edge{}, false
	}
	leftC := cov[start] / peak
	rightC := cov[end] / peak
	if leftC > 1 {
		leftC = 1
	}
	if rightC > 1 {
		rightC = 1
	}
	left := float64(x0+start) + (1 - leftC)
	right := float64(x0+end) + rightC
	if right < left {
		return Edge{}, false
	}
	return Edge{Left: left, Right: right}, true
}

func deltaAt(img *image.RGBA, x, y int, b image.Rectangle) int {
	return deltaRGB(img.RGBAAt(x, y), img.RGBAAt(b.Min.X, b.Min.Y))
}

func deltaRGB(px, bg color.RGBA) int {
	return abs(int(px.R)-int(bg.R)) + abs(int(px.G)-int(bg.G)) + abs(int(px.B)-int(bg.B))
}

// surfaceColor is the flat control surface in this band: the average of
// the columns whose pixels barely change. Glyph columns are the ones that
// vary, so they are left out. A crop of one line and a full screenshot
// then share the same background for the same glyphs.
func surfaceColor(img *image.RGBA, y0, y1 int) color.RGBA {
	b := img.Bounds()
	type acc struct {
		n, r, g, bl int
	}
	var flat acc
	for x := b.Min.X; x < b.Max.X; x++ {
		minR, minG, minB := 255, 255, 255
		maxR, maxG, maxB := 0, 0, 0
		var sum acc
		for y := y0; y < y1; y++ {
			px := img.RGBAAt(x, y)
			sum.n++
			sum.r += int(px.R)
			sum.g += int(px.G)
			sum.bl += int(px.B)
			if int(px.R) < minR {
				minR = int(px.R)
			}
			if int(px.G) < minG {
				minG = int(px.G)
			}
			if int(px.B) < minB {
				minB = int(px.B)
			}
			if int(px.R) > maxR {
				maxR = int(px.R)
			}
			if int(px.G) > maxG {
				maxG = int(px.G)
			}
			if int(px.B) > maxB {
				maxB = int(px.B)
			}
		}
		if maxR-minR+maxG-minG+maxB-minB > 24 {
			continue
		}
		flat.n += sum.n
		flat.r += sum.r
		flat.g += sum.g
		flat.bl += sum.bl
	}
	if flat.n == 0 {
		return img.RGBAAt(b.Min.X, y0)
	}
	return color.RGBA{R: uint8(flat.r / flat.n), G: uint8(flat.g / flat.n), B: uint8(flat.bl / flat.n), A: 255}
}

func toRGBA(src image.Image) *image.RGBA {
	if rgba, ok := src.(*image.RGBA); ok {
		return rgba
	}
	b := src.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, src, b.Min, draw.Src)
	return dst
}

func abs(v int) int {
	return int(math.Abs(float64(v)))
}
