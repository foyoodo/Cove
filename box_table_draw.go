package main

import (
	"fmt"
	"math"

	"github.com/egoist/mygo/ui"
)

// boxGlyph is one U+2500–257F character. Weights are 0 none, 1 light,
// 2 heavy, 3 double. Dashed / rounded / diagonal glyphs set those fields
// instead of (or in addition to) the four arms.
type boxGlyph struct {
	n, e, s, w uint8
	dash       uint8 // 2, 3, or 4; 0 is a solid stroke
	dashV      bool
	arc        uint8 // 1 SE ╭, 2 SW ╮, 3 NW ╯, 4 NE ╰
	diag       uint8 // bit 1 = ╱, bit 2 = ╲
}

var boxGlyphs [128]boxGlyph

func init() {
	initBoxGlyphs()
}

func wgt(b byte) uint8 {
	switch b {
	case 'L':
		return 1
	case 'H':
		return 2
	case 'D':
		return 3
	default:
		return 0
	}
}

func putBox(r rune, nesw string) {
	boxGlyphs[r-boxMin] = boxGlyph{n: wgt(nesw[0]), e: wgt(nesw[1]), s: wgt(nesw[2]), w: wgt(nesw[3])}
}

func initBoxGlyphs() {
	// Solid light / heavy / double bars and junctions. NESW, one letter
	// per arm: . none, L light, H heavy, D double.
	putBox(0x2500, ".L.L") // ─
	putBox(0x2501, ".H.H") // ━
	putBox(0x2502, "L.L.") // │
	putBox(0x2503, "H.H.") // ┃

	putBox(0x250C, ".LL.") // ┌
	putBox(0x250D, ".HL.") // ┍
	putBox(0x250E, ".LH.") // ┎
	putBox(0x250F, ".HH.") // ┏
	putBox(0x2510, "..LL") // ┐
	putBox(0x2511, "..LH") // ┑
	putBox(0x2512, "..HL") // ┒
	putBox(0x2513, "..HH") // ┓
	putBox(0x2514, "L.L.") // └
	putBox(0x2515, "L.H.") // ┕  UP LIGHT AND RIGHT HEAVY? N=L E=H → "LHL." wait
	// 2514 LIGHT UP AND RIGHT: N+E
	putBox(0x2514, "LL..")
	putBox(0x2515, "LH..") // UP LIGHT AND RIGHT HEAVY
	putBox(0x2516, "HL..") // UP HEAVY AND RIGHT LIGHT
	putBox(0x2517, "HH..") // HEAVY UP AND RIGHT
	putBox(0x2518, "L..L") // ┘ LIGHT UP AND LEFT
	putBox(0x2519, "L..H") // UP LIGHT AND LEFT HEAVY
	putBox(0x251A, "H..L") // UP HEAVY AND LEFT LIGHT
	putBox(0x251B, "H..H") // HEAVY UP AND LEFT

	putBox(0x251C, "LLL.") // ├
	putBox(0x251D, "LHL.") // ┝ VERTICAL LIGHT AND RIGHT HEAVY
	putBox(0x251E, "HLL.") // ┞ UP HEAVY AND RIGHT DOWN LIGHT
	putBox(0x251F, "LLH.") // ┟ DOWN HEAVY AND RIGHT UP LIGHT
	putBox(0x2520, "HLH.") // ┠ VERTICAL HEAVY AND RIGHT LIGHT
	putBox(0x2521, "HHL.") // ┡ DOWN LIGHT AND RIGHT UP HEAVY
	putBox(0x2522, "LHH.") // ┢ UP LIGHT AND RIGHT DOWN HEAVY
	putBox(0x2523, "HHH.") // ┣

	putBox(0x2524, "L.LL") // ┤
	putBox(0x2525, "L.HL") // ┥ VERTICAL LIGHT AND LEFT HEAVY
	putBox(0x2526, "H.LL") // ┦ UP HEAVY AND LEFT DOWN LIGHT
	putBox(0x2527, "L.LH") // ┧ DOWN HEAVY AND LEFT UP LIGHT
	putBox(0x2528, "H.LH") // ┨ VERTICAL HEAVY AND LEFT LIGHT
	putBox(0x2529, "H.HL") // ┩ DOWN LIGHT AND LEFT UP HEAVY
	putBox(0x252A, "L.HH") // ┪ UP LIGHT AND LEFT DOWN HEAVY
	putBox(0x252B, "H.HH") // ┫

	putBox(0x252C, ".LLL") // ┬
	putBox(0x252D, ".LLH") // ┭ LEFT HEAVY AND RIGHT DOWN LIGHT
	putBox(0x252E, ".HLL") // ┮ RIGHT HEAVY AND LEFT DOWN LIGHT
	putBox(0x252F, ".HLH") // ┯ DOWN LIGHT AND HORIZONTAL HEAVY
	putBox(0x2530, ".LHL") // ┰ DOWN HEAVY AND HORIZONTAL LIGHT
	putBox(0x2531, ".LHH") // ┱ RIGHT LIGHT AND LEFT DOWN HEAVY
	putBox(0x2532, ".HHL") // ┲ LEFT LIGHT AND RIGHT DOWN HEAVY
	putBox(0x2533, ".HHH") // ┳

	putBox(0x2534, "L.LL") // ┴ LIGHT UP AND HORIZONTAL — N E W: "L.L L" = "L.LL"
	putBox(0x2534, "L.LL")
	// 2534 is N=L E=L S=none W=L → "L.LL" but that's the same encoding as ┤ 2524
	// 2524 LIGHT VERTICAL AND LEFT is N=L E=none S=L W=L → "L.LL"
	// Collision! 2524 I set as "L.LL" which is N L, E none, S L, W L. Correct for ┤.
	// 2534 LIGHT UP AND HORIZONTAL is N L, E L, S none, W L → "LL.L"
	putBox(0x2534, "LL.L")
	putBox(0x2535, "LH.L") // ┵ LEFT HEAVY AND RIGHT UP LIGHT: N=L E=L W=H → "LL.H"?
	// LEFT HEAVY AND RIGHT UP LIGHT: W heavy, E light, N light → "LL.H"
	putBox(0x2535, "LL.H")
	putBox(0x2536, "LH.L") // ┶ RIGHT HEAVY AND LEFT UP LIGHT: E=H W=L N=L → "LH.L"
	putBox(0x2537, "LH.H") // ┷ UP LIGHT AND HORIZONTAL HEAVY: N=L E=H W=H → "LH.H"
	putBox(0x2538, "HL.L") // ┸ UP HEAVY AND HORIZONTAL LIGHT: N=H E=L W=L → "HL.L"
	putBox(0x2539, "HH.L") // ┹ RIGHT LIGHT AND LEFT UP HEAVY: N=H E=L W=H → "HL.H"? N=H E=L W=H → "HL.H"
	putBox(0x2539, "HL.H")
	putBox(0x253A, "HH.L") // ┺ LEFT LIGHT AND RIGHT UP HEAVY: N=H E=H W=L → "HH.L"
	putBox(0x253B, "HH.H") // ┻ HEAVY UP AND HORIZONTAL

	putBox(0x253C, "LLLL") // ┼
	putBox(0x253D, "LLLH") // ┽ LEFT HEAVY AND RIGHT VERTICAL LIGHT: W=H E=L N=L S=L
	putBox(0x253E, "LHLL") // ┾ RIGHT HEAVY AND LEFT VERTICAL LIGHT: E=H W=L N=L S=L
	putBox(0x253F, "LHLH") // ┿ VERTICAL LIGHT AND HORIZONTAL HEAVY: N=L S=L E=H W=H
	putBox(0x2540, "HLLL") // ╀ UP HEAVY AND DOWN HORIZONTAL LIGHT
	putBox(0x2541, "LLHL") // ╁ DOWN HEAVY AND UP HORIZONTAL LIGHT: S=H N=L E=L W=L → "LLHL"? NESW = N L, E L, S H, W L → "LLHL"
	putBox(0x2542, "HLHL") // ╂ VERTICAL HEAVY AND HORIZONTAL LIGHT: N=H S=H E=L W=L
	putBox(0x2543, "HLLH") // ╃ LEFT UP HEAVY AND RIGHT DOWN LIGHT: N=H W=H E=L S=L
	putBox(0x2544, "HHLL") // ╄ RIGHT UP HEAVY AND LEFT DOWN LIGHT: N=H E=H S=L W=L
	putBox(0x2545, "LLHH") // ╅ LEFT DOWN HEAVY AND RIGHT UP LIGHT: S=H W=H N=L E=L → "LLHH"? N=L E=L S=H W=H → "LLHH"
	putBox(0x2546, "LHHL") // ╆ RIGHT DOWN HEAVY AND LEFT UP LIGHT: S=H E=H N=L W=L → "LHHL"
	putBox(0x2547, "HHLH") // ╇ DOWN LIGHT AND UP HORIZONTAL HEAVY: S=L N=H E=H W=H
	putBox(0x2548, "LHHH") // ╈ UP LIGHT AND DOWN HORIZONTAL HEAVY: N=L S=H E=H W=H
	putBox(0x2549, "HLHH") // ╉ RIGHT LIGHT AND LEFT VERTICAL HEAVY: E=L N=H S=H W=H → "H.HH" plus E=L → "HLHH"
	putBox(0x254A, "HHHL") // ╊ LEFT LIGHT AND RIGHT VERTICAL HEAVY: W=L N=H S=H E=H
	putBox(0x254B, "HHHH") // ╋

	putBox(0x2550, ".D.D") // ═
	putBox(0x2551, "D.D.") // ║
	putBox(0x2552, ".DL.") // ╒ DOWN SINGLE AND RIGHT DOUBLE: S=L E=D
	putBox(0x2553, ".LD.") // ╓ DOWN DOUBLE AND RIGHT SINGLE: S=D E=L
	putBox(0x2554, ".DD.") // ╔
	putBox(0x2555, "..LD") // ╕ DOWN SINGLE AND LEFT DOUBLE: S=L W=D → "..LD"
	putBox(0x2556, "..DL") // ╖ DOWN DOUBLE AND LEFT SINGLE: S=D W=L
	putBox(0x2557, "..DD") // ╗
	putBox(0x2558, "L.D.") // ╘ UP SINGLE AND RIGHT DOUBLE: N=L E=D → "LD.."
	putBox(0x2558, "LD..")
	putBox(0x2559, "D.L.") // ╙ UP DOUBLE AND RIGHT SINGLE: N=D E=L → "DL.."
	putBox(0x2559, "DL..")
	putBox(0x255A, "DD..") // ╚
	putBox(0x255B, "L..D") // ╛ UP SINGLE AND LEFT DOUBLE
	putBox(0x255C, "D..L") // ╜ UP DOUBLE AND LEFT SINGLE
	putBox(0x255D, "D..D") // ╝
	putBox(0x255E, "LDL.") // ╞ VERTICAL SINGLE AND RIGHT DOUBLE: N=L S=L E=D
	putBox(0x255F, "DLD.") // ╟ VERTICAL DOUBLE AND RIGHT SINGLE
	putBox(0x2560, "DDD.") // ╠
	putBox(0x2561, "L.LD") // ╡ VERTICAL SINGLE AND LEFT DOUBLE: N=L S=L W=D
	putBox(0x2562, "D.DL") // ╢ VERTICAL DOUBLE AND LEFT SINGLE
	putBox(0x2563, "D.DD") // ╣
	putBox(0x2564, ".DLD") // ╤ DOWN SINGLE AND HORIZONTAL DOUBLE: S=L E=D W=D
	putBox(0x2565, ".LDL") // ╥ DOWN DOUBLE AND HORIZONTAL SINGLE: S=D E=L W=L
	putBox(0x2566, ".DDD") // ╦
	putBox(0x2567, "L.DD") // ╧ UP SINGLE AND HORIZONTAL DOUBLE: N=L E=D W=D → "LD.D"
	putBox(0x2567, "LD.D")
	putBox(0x2568, "D.LL") // ╨ UP DOUBLE AND HORIZONTAL SINGLE: N=D E=L W=L → "DL.L"
	putBox(0x2568, "DL.L")
	putBox(0x2569, "D.DD") // ╩ wait N=D E=D W=D → "DD.D"
	putBox(0x2569, "DD.D")
	putBox(0x256A, "LDLD") // ╪ VERTICAL SINGLE AND HORIZONTAL DOUBLE
	putBox(0x256B, "DLDL") // ╫ VERTICAL DOUBLE AND HORIZONTAL SINGLE
	putBox(0x256C, "DDDD") // ╬

	putBox(0x2574, "...L") // ╴ LIGHT LEFT
	putBox(0x2575, "L...") // ╵ LIGHT UP
	putBox(0x2576, ".L..") // ╶ LIGHT RIGHT
	putBox(0x2577, "..L.") // ╷ LIGHT DOWN
	putBox(0x2578, "...H") // ╸ HEAVY LEFT
	putBox(0x2579, "H...") // ╹ HEAVY UP
	putBox(0x257A, ".H..") // ╺ HEAVY RIGHT
	putBox(0x257B, "..H.") // ╻ HEAVY DOWN
	putBox(0x257C, ".H.L") // ╼ LIGHT LEFT AND HEAVY RIGHT
	putBox(0x257D, "L.H.") // ╽ LIGHT UP AND HEAVY DOWN
	putBox(0x257E, ".L.H") // ╾ HEAVY LEFT AND LIGHT RIGHT
	putBox(0x257F, "H.L.") // ╿ HEAVY UP AND LIGHT DOWN

	// Dashes: 2/3/4 segments, light or heavy, H or V.
	setDash := func(r rune, n uint8, heavy, vert bool) {
		g := boxGlyph{dash: n, dashV: vert}
		if vert {
			if heavy {
				g.n, g.s = 2, 2
			} else {
				g.n, g.s = 1, 1
			}
		} else {
			if heavy {
				g.e, g.w = 2, 2
			} else {
				g.e, g.w = 1, 1
			}
		}
		boxGlyphs[r-boxMin] = g
	}
	setDash(0x2504, 3, false, false) // ┄
	setDash(0x2505, 3, true, false)  // ┅
	setDash(0x2506, 3, false, true)  // ┆
	setDash(0x2507, 3, true, true)   // ┇
	setDash(0x2508, 4, false, false) // ┈
	setDash(0x2509, 4, true, false)  // ┉
	setDash(0x250A, 4, false, true)  // ┊
	setDash(0x250B, 4, true, true)   // ┋
	setDash(0x254C, 2, false, false) // ╌
	setDash(0x254D, 2, true, false)  // ╍
	setDash(0x254E, 2, false, true)  // ╎
	setDash(0x254F, 2, true, true)   // ╏

	boxGlyphs[0x256D-boxMin] = boxGlyph{arc: 1, e: 1, s: 1} // ╭ SE
	boxGlyphs[0x256E-boxMin] = boxGlyph{arc: 2, w: 1, s: 1} // ╮ SW
	boxGlyphs[0x256F-boxMin] = boxGlyph{arc: 3, w: 1, n: 1} // ╯ NW
	boxGlyphs[0x2570-boxMin] = boxGlyph{arc: 4, e: 1, n: 1} // ╰ NE
	boxGlyphs[0x2571-boxMin] = boxGlyph{diag: 1}            // ╱
	boxGlyphs[0x2572-boxMin] = boxGlyph{diag: 2}            // ╲
	boxGlyphs[0x2573-boxMin] = boxGlyph{diag: 3}            // ╳
}

func lookupBox(r rune) boxGlyph {
	if r < boxMin || r > boxMax {
		return boxGlyph{}
	}
	return boxGlyphs[r-boxMin]
}

func boxGlyphDefined(r rune) bool {
	g := lookupBox(r)
	return g.n|g.e|g.s|g.w|g.dash|g.arc|g.diag != 0
}

const ellipseKappa = 0.5522847498

type dashLayout struct {
	Segs  [][2]float32 // start, length in DIPs
	GapPx int
	SegPx []int
	Note  string
}

func layoutDashes(lengthDIP float32, n, gapMinPx int, scale float32) dashLayout {
	if n < 2 {
		n = 2
	}
	if scale < 0.5 {
		scale = 1
	}
	lengthPx := int(math.Round(float64(lengthDIP * scale)))
	if lengthPx < 1 {
		lengthPx = 1
	}
	try := func(gap int) (bool, []int) {
		if gap < 0 {
			return false, nil
		}
		rest := lengthPx - (n-1)*gap
		if rest < n {
			return false, nil
		}
		base := rest / n
		extra := rest % n
		out := make([]int, n)
		for i := range out {
			out[i] = base
			if i < extra {
				out[i]++
			}
		}
		return true, out
	}
	ok, segs := try(gapMinPx)
	gap := gapMinPx
	note := ""
	if !ok {
		ok, segs = try(1)
		gap = 1
		if ok {
			note = fmt.Sprintf("gap reduced from %dpx to 1px to keep %d dashes in %dpx", gapMinPx, n, lengthPx)
		}
	}
	if !ok {
		if lengthPx >= n {
			gap = (lengthPx - n) / (n - 1)
			if n == 1 {
				gap = 0
			}
			rest := lengthPx - (n-1)*gap
			segs = make([]int, n)
			for i := range segs {
				segs[i] = 1
			}
			extra := rest - n
			for i := 0; extra > 0 && i < n; i++ {
				segs[i]++
				extra--
			}
			note = fmt.Sprintf("1px ticks in %dpx cell; SPEC %dpx gaps infeasible for %d dashes", lengthPx, gapMinPx, n)
		} else {
			segs = make([]int, n)
			for i := 0; i < lengthPx; i++ {
				segs[i%n]++
			}
			gap = 0
			note = fmt.Sprintf("collapsed %d dashes into %dpx", n, lengthPx)
		}
	}
	out := make([][2]float32, 0, n)
	x := 0
	for i, s := range segs {
		if s > 0 {
			out = append(out, [2]float32{float32(x) / scale, float32(s) / scale})
		}
		x += s
		if i < n-1 {
			x += gap
		}
	}
	return dashLayout{Segs: out, GapPx: gap, SegPx: segs, Note: note}
}

func dashNoteForCell(cellW float32, scale float32) string {
	return layoutDashes(cellW, 4, 2, scale).Note
}

func strokePx(p *ui.Painter, px int) float32 {
	s := p.Scale()
	if s < 0.5 {
		s = 1
	}
	if px < 1 {
		px = 1
	}
	return float32(px) / s
}

func snapCenter(p *ui.Painter, pos float32, widthPx int) float32 {
	s := p.Scale()
	if s < 0.5 {
		s = 1
	}
	posPx := float64(pos * s)
	if widthPx%2 == 1 {
		posPx = math.Floor(posPx) + 0.5
	} else {
		posPx = math.Round(posPx)
	}
	return float32(posPx) / s
}

func hStroke(p *ui.Painter, x0, x1, y float32, widthPx int, c ui.Color) {
	if x1 < x0 {
		x0, x1 = x1, x0
	}
	y = snapCenter(p, y, widthPx)
	p.Line(x0, y, x1, y, strokePx(p, widthPx), c)
}

func vStroke(p *ui.Painter, x, y0, y1 float32, widthPx int, c ui.Color) {
	if y1 < y0 {
		y0, y1 = y1, y0
	}
	x = snapCenter(p, x, widthPx)
	p.Line(x, y0, x, y1, strokePx(p, widthPx), c)
}

func drawWeightH(p *ui.Painter, x0, x1, y float32, w uint8, c ui.Color) {
	switch w {
	case 1:
		hStroke(p, x0, x1, y, 1, c)
	case 2:
		hStroke(p, x0, x1, y, 2, c)
	case 3:
		off := 1.5 / max(p.Scale(), 1)
		hStroke(p, x0, x1, y-off, 1, c)
		hStroke(p, x0, x1, y+off, 1, c)
	}
}

func drawWeightV(p *ui.Painter, x, y0, y1 float32, w uint8, c ui.Color) {
	switch w {
	case 1:
		vStroke(p, x, y0, y1, 1, c)
	case 2:
		vStroke(p, x, y0, y1, 2, c)
	case 3:
		off := 1.5 / max(p.Scale(), 1)
		vStroke(p, x-off, y0, y1, 1, c)
		vStroke(p, x+off, y0, y1, 1, c)
	}
}

func drawDashesH(p *ui.Painter, x, y, length float32, n, widthPx int, c ui.Color) {
	gapMin := 2
	if widthPx >= 2 {
		gapMin = 3
	}
	d := layoutDashes(length, n, gapMin, p.Scale())
	for _, seg := range d.Segs {
		hStroke(p, x+seg[0], x+seg[0]+seg[1], y, widthPx, c)
	}
}

func drawDashesV(p *ui.Painter, x, y, length float32, n, widthPx int, c ui.Color) {
	gapMin := 2
	if widthPx >= 2 {
		gapMin = 3
	}
	d := layoutDashes(length, n, gapMin, p.Scale())
	for _, seg := range d.Segs {
		vStroke(p, x, y+seg[0], y+seg[0]+seg[1], widthPx, c)
	}
}

func drawBoxGlyph(p *ui.Painter, path *ui.Path, x, y, cw, ch float32, r rune, c ui.Color) {
	g := lookupBox(r)
	cx := x + cw/2
	cy := y + ch/2
	if g.arc != 0 {
		drawBoxArc(p, path, x, y, cw, ch, g.arc, c)
		return
	}
	if g.diag != 0 {
		w := strokePx(p, 1)
		path.Reset()
		if g.diag&1 != 0 {
			path.MoveTo(x+cw, y)
			path.LineTo(x, y+ch)
		}
		if g.diag&2 != 0 {
			path.MoveTo(x, y)
			path.LineTo(x+cw, y+ch)
		}
		p.StrokePath(path, w, c)
		return
	}
	if g.dash != 0 {
		widthPx := 1
		if g.e == 2 || g.n == 2 {
			widthPx = 2
		}
		if g.dashV {
			drawDashesV(p, cx, y, ch, int(g.dash), widthPx, c)
		} else {
			drawDashesH(p, x, cy, cw, int(g.dash), widthPx, c)
		}
		return
	}
	if g.e != 0 {
		drawWeightH(p, cx, x+cw, cy, g.e, c)
	}
	if g.w != 0 {
		drawWeightH(p, x, cx, cy, g.w, c)
	}
	if g.s != 0 {
		drawWeightV(p, cx, cy, y+ch, g.s, c)
	}
	if g.n != 0 {
		drawWeightV(p, cx, y, cy, g.n, c)
	}
}

func drawBoxArc(p *ui.Painter, path *ui.Path, x, y, cw, ch float32, quad uint8, c ui.Color) {
	// SPEC asks for a quarter circle centred on the cell. That cannot
	// keep a horizontal tangent at the east/west midpoints (the neighbour
	// ─), even in a square cell: a cell-centred ellipse through those
	// midpoints is vertical there. The visual bar is G1 continuity at
	// the edge midpoints, so the quarter ellipse is centred on the
	// interior corner and uses semi-axes (cw/2, ch/2).
	rx, ry := cw/2, ch/2
	k := float32(ellipseKappa)
	path.Reset()
	switch quad {
	case 1: // ╭ top-left; interior SE
		cx, cy := x+cw, y+ch
		path.MoveTo(cx, cy-ry)
		path.CubeTo(cx-k*rx, cy-ry, cx-rx, cy-k*ry, cx-rx, cy)
	case 2: // ╮ top-right; interior SW
		cx, cy := x, y+ch
		path.MoveTo(cx, cy-ry)
		path.CubeTo(cx+k*rx, cy-ry, cx+rx, cy-k*ry, cx+rx, cy)
	case 3: // ╯ bottom-right; interior NW
		cx, cy := x, y
		path.MoveTo(cx, cy+ry)
		path.CubeTo(cx+k*rx, cy+ry, cx+rx, cy+k*ry, cx+rx, cy)
	case 4: // ╰ bottom-left; interior NE
		cx, cy := x+cw, y
		path.MoveTo(cx, cy+ry)
		path.CubeTo(cx-k*rx, cy+ry, cx-rx, cy+k*ry, cx-rx, cy)
	default:
		return
	}
	p.StrokePath(path, strokePx(p, 1), c)
}
