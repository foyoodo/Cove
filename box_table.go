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
	return len(boxRunBounds(lines)) > 0
}

// boxRunBounds is each maximal run of ≥3 consecutive qualifying lines,
// as [start, end) indexes into lines.
func boxRunBounds(lines []string) [][2]int {
	var runs [][2]int
	for i := 0; i < len(lines); {
		if !lineQualifies(lines[i]) {
			i++
			continue
		}
		j := i + 1
		for j < len(lines) && lineQualifies(lines[j]) {
			j++
		}
		if j-i >= 3 {
			runs = append(runs, [2]int{i, j})
		}
		i = j
	}
	return runs
}

// boxSeg is one stretch of a paragraph: either a box-drawing table run
// or the prose around it.
type boxSeg struct {
	Box   bool
	Lines []string
}

// splitBoxSegs isolates each qualifying run. Adjacent non-qualifying
// lines stay as ordinary Markdown, even without a blank line between.
func splitBoxSegs(lines []string) []boxSeg {
	runs := boxRunBounds(lines)
	if len(runs) == 0 {
		return nil
	}
	var segs []boxSeg
	at := 0
	for _, r := range runs {
		if r[0] > at {
			segs = append(segs, boxSeg{Lines: lines[at:r[0]]})
		}
		segs = append(segs, boxSeg{Box: true, Lines: lines[r[0]:r[1]]})
		at = r[1]
	}
	if at < len(lines) {
		segs = append(segs, boxSeg{Lines: lines[at:]})
	}
	return segs
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

func codeBlockLines(s string) []string {
	raw := strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "\n")
}

func paragraphBoxSegs(d *parser.Document, i int) []boxSeg {
	if d.Block(i).Kind != parser.Paragraph {
		return nil
	}
	return splitBoxSegs(paragraphSourceLines(d, i))
}

// boxTableSource is the verbatim text of a paragraph that is only a box
// table (no adjacent prose). Fenced code never qualifies: those blocks
// keep their header and Copy button, and paint qualifying lines on the
// cell grid inside the code body.
func boxTableSource(d *parser.Document, i int) (string, bool) {
	segs := paragraphBoxSegs(d, i)
	if len(segs) != 1 || !segs[0].Box {
		return "", false
	}
	return strings.Join(segs[0].Lines, "\n"), true
}

func fragmentFindText(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	doc := parser.New(parser.Default)
	doc.Set(strings.Join(lines, "\n"))
	var b strings.Builder
	for i := 1; i < doc.Len(); i++ {
		switch doc.Block(i).Kind {
		case parser.Paragraph, parser.Heading, parser.TableCell:
			text := joinStyled(blockParts(doc, i))
			if text == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(text)
		}
	}
	return b.String()
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
	return a.paintBoxParagraph(c, d, i, false)
}

func (a *App) paintBoxTableFind(c *ui.Context, d *parser.Document, i int) bool {
	return a.paintBoxParagraph(c, d, i, true)
}

func (a *App) paintBoxParagraph(c *ui.Context, d *parser.Document, i int, find bool) bool {
	segs := paragraphBoxSegs(d, i)
	if len(segs) == 0 {
		return false
	}
	caret := a.streamCaret && i == lastParagraph(d)
	first := true
	for si, seg := range segs {
		last := si == len(segs)-1
		if !first {
			ui.Box(c).Height(10).AlignSelf(ui.Stretch)
		}
		first = false
		if seg.Box {
			src := strings.Join(seg.Lines, "\n")
			var run *findRun
			if find {
				r, ok := a.takeRun(src)
				if ok && runOverlaps(r, a.finder.hits, a.finder.paintMsg) {
					run = &r
				}
			}
			a.paintBoxTable(c, i*100+si, src, run, caret && last)
			continue
		}
		var run *findRun
		if find {
			text := fragmentFindText(seg.Lines)
			if text != "" {
				r, ok := a.takeRun(text)
				if ok && runOverlaps(r, a.finder.hits, a.finder.paintMsg) {
					run = &r
				}
			}
		}
		a.paintBoxProse(c, seg.Lines, run, caret && last)
	}
	return true
}

func (a *App) paintBoxProse(c *ui.Context, lines []string, run *findRun, caret bool) {
	if len(lines) == 0 {
		return
	}
	doc := parser.New(parser.Default)
	doc.Set(strings.Join(lines, "\n"))
	p := a.look
	for i := 1; i < doc.Len(); i++ {
		b := doc.Block(i)
		if b.Kind != parser.Paragraph && b.Kind != parser.Heading {
			continue
		}
		parts := blockParts(doc, i)
		text := joinStyled(parts)
		spans := inlineSpans(doc.Inlines(i), 0, len(doc.Inlines(i)), 400, p, 14, 12.5)
		if run != nil && text != "" {
			spans = a.hitSpans(parts, *run, a.finder.paintMsg, 14)
		}
		if len(spans) == 0 {
			spans = []ui.Span{{Text: "", Size: 14}}
		}
		if caret && i == lastParagraph(doc) {
			ui.Row(c).Gap(3).AlignItems(ui.Center).Shrink(0).Children(func() {
				el := ui.RichText(c, spans...).FontSize(14).LineHeight(21.0 / 14.0).TextColor(p.TextPrimary).Selectable()
				if run != nil {
					a.markRun(el, spans, *run, a.finder.paintMsg, 0, 0, ui.Font{Size: 14}, 21.0/14.0)
				}
				ui.Box(c).Size(2, 17).Radius(1).Background(p.ThemeMark).Opacity(a.caretAlpha(c)).Shrink(0)
			})
			continue
		}
		el := ui.RichText(c, spans...).FontSize(14).LineHeight(21.0 / 14.0).TextColor(p.TextPrimary).Selectable().FillWidth().MinWidth(0)
		if run != nil {
			a.markRun(el, spans, *run, a.finder.paintMsg, 0, 0, ui.Font{Size: 14}, 21.0/14.0)
		}
	}
}

func (a *App) paintBoxTable(c *ui.Context, key int, src string, run *findRun, caret bool) {
	p := a.look
	slot := a.hScrollSlot(a.hScrollKey("box", key))
	edge := scrollEdge(p.CodeBg, a.cardFlat())
	ui.Column(c).FillWidth().MinWidth(0).Radius(radiusCode).Background(p.CodeBg).Border(1, p.CodeBorder).Clip().AlignSelf(ui.Stretch).Label("box table").Children(func() {
		ui.Box(c).AlignSelf(ui.Stretch).Children(func() {
			ui.ScrollHorizontal(c).AlignSelf(ui.Stretch).TrackScroll(&slot.st).Label("box table scroller").Children(func() {
				ui.Box(c).Padding(boxPadT, boxPadX, boxPadB, boxPadX).Children(func() {
					a.paintBoxGridHost(c, src, run, caret)
				})
			})
			hScrollFades(c, slot, "box", edge)
		})
	})
}

func (a *App) paintCodeBoxBody(c *ui.Context, lines []string, run *findRun) {
	if len(lines) == 0 {
		return
	}
	a.paintBoxGridHost(c, strings.Join(lines, "\n"), run, false)
}

func (a *App) paintBoxGridHost(c *ui.Context, src string, run *findRun, caret bool) {
	lines := strings.Split(src, "\n")
	cellW := boxASCIIAdvance(c)
	lay := layoutBoxTable(lines, cellW)
	p := a.look
	spans := boxOverlaySpans(lines, cellW)
	a.lastBox = boxTableInfo{
		Src: src, CellW: cellW, Cols: lay.cols, Rows: lay.rows,
		GridW: lay.gridW, GridH: lay.gridH, DashNote: dashNoteForCell(cellW, 1),
	}
	gridW := lay.gridW
	if caret {
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
		if caret && len(lines) > 0 {
			lastW := float32(lineCells(lines[len(lines)-1])) * cellW
			top := float32(len(lines)-1) * boxLineH
			ui.Box(c).Absolute().Left(lastW+3).Top(top).Size(2, 17).Radius(1).Background(p.ThemeMark).Opacity(a.caretAlpha(c)).Shrink(0)
		}
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
