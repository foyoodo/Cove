package main

import (
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown/parser"
	"golang.org/x/text/width"
)

// SPEC §5.4.2 box-drawing text tables: Mono/Code 12/18, code-body chrome,
// no language/copy header, no wrap, 10/12/12 padding.
const (
	boxFont     = "ui-monospace"
	boxFontSize = 12
	boxLineH    = 18
	boxPadT     = 10
	boxPadX     = 12
	boxPadB     = 12
	boxMin      = 0x2500
	boxMax      = 0x257F
	blockMin    = 0x2580
	blockMax    = 0x259F
)

// boxTableInfo is the last box-drawing table painted in a frame.
type boxTableInfo struct {
	Src          string
	CellW        float32
	Cols, Rows   int
	GridW, GridH float32
	DashNote     string
}

// isBoxDrawing reports a Unicode Box Drawing code point (U+2500–257F).
func isBoxDrawing(r rune) bool { return r >= boxMin && r <= boxMax }

func isBlockElement(r rune) bool { return r >= blockMin && r <= blockMax }

// lineQualifies reports a line whose first or last rune is box drawing.
func lineQualifies(s string) bool {
	if s == "" {
		return false
	}
	first, _ := utf8.DecodeRuneInString(s)
	last, _ := utf8.DecodeLastRuneInString(s)
	return isBoxDrawing(first) || isBoxDrawing(last)
}

// isBoxTable reports at least three consecutive qualifying lines.
func isBoxTable(lines []string) bool {
	run := 0
	for _, ln := range lines {
		if lineQualifies(ln) {
			run++
			if run >= 3 {
				return true
			}
			continue
		}
		run = 0
	}
	return false
}

// boxCellCount is how many 1ch cells a rune occupies. East Asian Width
// W and F take two; everything else takes one, including U+2580–259F.
func boxCellCount(r rune) int {
	switch width.LookupRune(r).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 2
	default:
		return 1
	}
}

func lineCells(s string) int {
	n := 0
	for _, r := range s {
		n += boxCellCount(r)
	}
	return n
}

type boxLayout struct {
	lines        []string
	cols, rows   int
	cellW        float32
	gridW, gridH float32
}

func layoutBoxTable(lines []string, cellW float32) boxLayout {
	cols := 0
	for _, ln := range lines {
		if n := lineCells(ln); n > cols {
			cols = n
		}
	}
	rows := len(lines)
	if rows < 0 {
		rows = 0
	}
	return boxLayout{
		lines: lines,
		cols:  cols,
		rows:  rows,
		cellW: cellW,
		gridW: float32(cols) * cellW,
		gridH: float32(rows) * boxLineH,
	}
}

func boxASCIIAdvance(c *ui.Context) float32 {
	w, _ := c.MeasureText(0, ui.Span{Text: "0", Font: boxFont, Size: boxFontSize})
	if w < 1 {
		return 7.2
	}
	return w
}

func stripQuotePrefix(s string) string {
	n := 0
	for n < 3 && n < len(s) && s[n] == ' ' {
		n++
	}
	rest := s[n:]
	if !strings.HasPrefix(rest, ">") {
		return s
	}
	rest = rest[1:]
	if strings.HasPrefix(rest, " ") {
		rest = rest[1:]
	}
	return rest
}

func paragraphSourceLines(d *parser.Document, i int) []string {
	b := d.Block(i)
	src := d.Source()
	if b.Start < 0 || b.End > len(src) || b.Start >= b.End {
		return nil
	}
	raw := strings.TrimRight(src[b.Start:b.End], "\n")
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	parts := strings.Split(raw, "\n")
	for i, ln := range parts {
		parts[i] = stripQuotePrefix(ln)
	}
	return parts
}

func boxTableLines(d *parser.Document, i int) ([]string, bool) {
	b := d.Block(i)
	switch b.Kind {
	case parser.Paragraph:
		lines := paragraphSourceLines(d, i)
		return lines, isBoxTable(lines)
	case parser.CodeBlock:
		raw := strings.TrimRight(d.Lines(i), "\n")
		if raw == "" {
			return nil, false
		}
		lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
		return lines, isBoxTable(lines)
	default:
		return nil, false
	}
}

func boxTableSource(d *parser.Document, i int) (string, bool) {
	lines, ok := boxTableLines(d, i)
	if !ok {
		return "", false
	}
	return strings.Join(lines, "\n"), true
}

func boxOverlaySpans(lines []string, cellW float32) []ui.Span {
	spans := make([]ui.Span, 0, 64)
	for i, ln := range lines {
		if i > 0 {
			spans = append(spans, ui.Span{Text: "\n"})
		}
		for _, r := range ln {
			n := boxCellCount(r)
			spans = append(spans, ui.Span{
				Text:        string(r),
				Font:        boxFont,
				Size:        boxFontSize,
				ObjectWidth: float32(n) * cellW,
			})
		}
	}
	if len(spans) == 0 {
		spans = []ui.Span{{Text: "", Font: boxFont, Size: boxFontSize, ObjectWidth: cellW}}
	}
	return spans
}

func (a *App) renderBoxTable(c *ui.Context, d *parser.Document, i int) bool {
	src, ok := boxTableSource(d, i)
	if !ok {
		return false
	}
	a.paintBoxTable(c, d, i, src, nil)
	return true
}

func (a *App) paintBoxTableFind(c *ui.Context, d *parser.Document, i int) bool {
	src, ok := boxTableSource(d, i)
	if !ok {
		return false
	}
	run, ok := a.takeRun(src)
	if !ok || !runOverlaps(run, a.finder.hits, a.finder.paintMsg) {
		return false
	}
	a.paintBoxTable(c, d, i, src, &run)
	return true
}

func (a *App) paintBoxTable(c *ui.Context, d *parser.Document, i int, src string, run *findRun) {
	lines := strings.Split(src, "\n")
	cellW := boxASCIIAdvance(c)
	lay := layoutBoxTable(lines, cellW)
	p := a.look
	slot := a.hScrollSlot(a.hScrollKey("box", i))
	edge := scrollEdge(p.CodeBg, a.cardFlat())
	showCaret := a.streamCaret && d.Block(i).Kind == parser.Paragraph && i == lastParagraph(d)
	spans := boxOverlaySpans(lines, cellW)
	note := dashNoteForCell(cellW, 1)
	a.lastBox = boxTableInfo{
		Src: src, CellW: cellW, Cols: lay.cols, Rows: lay.rows,
		GridW: lay.gridW, GridH: lay.gridH, DashNote: note,
	}

	ui.Column(c).FillWidth().MinWidth(0).Radius(radiusCode).Background(p.CodeBg).Border(1, p.CodeBorder).Clip().AlignSelf(ui.Stretch).Label("box table").Children(func() {
		ui.Box(c).AlignSelf(ui.Stretch).Children(func() {
			ui.ScrollHorizontal(c).AlignSelf(ui.Stretch).TrackScroll(&slot.st).Label("box table scroller").Children(func() {
				ui.Box(c).Padding(boxPadT, boxPadX, boxPadB, boxPadX).Children(func() {
					gridW := lay.gridW
					if showCaret {
						gridW += 8
					}
					host := ui.Box(c).Width(gridW).Height(lay.gridH).Label("box table grid")
					host.Draw(func(pn *ui.Painter, r ui.Rect) {
						paintBoxGrid(pn, r, lay, p.CodeText)
					})
					host.Children(func() {
						el := ui.RichText(c, spans...).Font(boxFont).FontSize(boxFontSize).LineHeight(boxLineH / boxFontSize).TextColor(p.CodeText).NoWrap().Selectable()
						if run != nil {
							a.markRun(el, spans, *run, a.finder.paintMsg, 0, 0, ui.Font{Family: boxFont, Size: boxFontSize}, boxLineH/boxFontSize)
						}
						if showCaret {
							lastW := float32(lineCells(lines[len(lines)-1])) * cellW
							top := float32(len(lines)-1) * boxLineH
							ui.Box(c).Absolute().Left(lastW+3).Top(top).Size(2, 17).Radius(1).Background(p.ThemeMark).Opacity(a.caretAlpha(c)).Shrink(0)
						}
					})
				})
			})
			hScrollFades(c, slot, "box", edge)
		})
	})
}

func paintBoxGrid(p *ui.Painter, r ui.Rect, lay boxLayout, color ui.Color) {
	var path ui.Path
	y := r.Y
	for _, ln := range lay.lines {
		x := r.X
		for _, ru := range ln {
			n := boxCellCount(ru)
			w := float32(n) * lay.cellW
			if isBoxDrawing(ru) {
				drawBoxGlyph(p, &path, x, y, lay.cellW, boxLineH, ru, color)
			} else if ru != ' ' && ru != '\t' && ru != 0 {
				drawBoxText(p, x, y, w, boxLineH, ru, color)
			}
			x += w
		}
		y += boxLineH
	}
}

func drawBoxText(p *ui.Painter, x, y, w, h float32, r rune, color ui.Color) {
	sp := ui.Span{Text: string(r), Font: boxFont, Size: boxFontSize, Color: color}
	gw, gh := p.MeasureText(0, sp)
	tx := x + (w-gw)/2
	ty := y + (h-gh)/2
	p.RichText(tx, ty, 0, sp)
}

// boxHit reports the rune whose cell contains (x, y) in grid-local DIPs.
func boxHit(lines []string, cellW, x, y float32) (row, cells int, r rune, ok bool) {
	if cellW < 1 || y < 0 || x < 0 {
		return 0, 0, 0, false
	}
	row = int(y / boxLineH)
	if row < 0 || row >= len(lines) {
		return 0, 0, 0, false
	}
	cx := 0
	for _, ru := range lines[row] {
		n := boxCellCount(ru)
		w := float32(n) * cellW
		if x < float32(cx)*cellW+w {
			return row, cx, ru, true
		}
		cx += n
	}
	return row, cx, 0, false
}
