package main

import (
	"math"
	"path"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown/parser"
)

// Table cells wrap inside a 320pt column (padding included). Pieces,
// measured widths, and painting stay separate so wrapping only changes
// height and alignment.
const (
	tableCellPadX = 10
	tableCellPadY = 5
	tableLineH    = 20
	tableMinRowH  = 30 // 5 + 20 + 5
	tableMaxCol   = 320
	tableBodySize = 13
	tableCodeSize = 13
)

// tableCell is one measured cell. Width is unwrapped text; lines is how
// many FixedLineHeight(20) rows the painted text occupies.
type tableCell struct {
	pieces []cellPiece
	width  float32
	lines  int
	head   bool
	first  bool
}

// tableLayout is the measured geometry of one chat table. Column widths
// do not depend on the message width: the cap is a fixed 320pt.
type tableLayout struct {
	src     string
	theme   string
	rows    [][]tableCell
	widths  []float32
	heights []float32
	total   float32
	aligns  []parser.Align
}

func (a *App) beginTableCache() {
	a.tableCacheSeen = make(map[string]struct{}, len(a.tableCache))
}

func (a *App) endTableCache() {
	for k := range a.tableCache {
		if _, ok := a.tableCacheSeen[k]; !ok {
			delete(a.tableCache, k)
		}
	}
}

func tableThemeKey(p Theme) string {
	return colorKey(p.TextPrimary) + "/" + colorKey(p.TextSecondary) + "/" + colorKey(p.TextLink) + "/" + colorKey(p.CodeInlineBg) + "/" + colorKey(p.CodeInlineText) + "/" + itoa(int(tableBodySize))
}

func colorKey(c ui.Color) string {
	return itoa(int(c.R)) + "," + itoa(int(c.G)) + "," + itoa(int(c.B)) + "," + itoa(int(c.A))
}

func tableCacheID(msg string, start int, theme string) string {
	return msg + "/" + itoa(start) + "/" + theme
}

func tableRowHeight(lines int, header bool) float32 {
	if lines < 1 {
		lines = 1
	}
	if header {
		return tableCellPadY*2 + float32(lines)*tableLineH
	}
	// SPEC §5.4.1: 1px rule + 4 + 20×n + 5. The rule sits in the row's
	// top padding, so a single-line body row is 30, not 31.
	return 1 + (tableCellPadY - 1) + float32(lines)*tableLineH + tableCellPadY
}

// renderTable draws a markdown table with the token surfaces and
// horizontal rules only. Each column is min(natural + padding, 320).
// Text wraps inside that cap; the table hugs the sum of the columns
// and only scrolls when that sum is wider than the message. The stock
// markdown table always paints a vertical border on each cell, so the
// table block is replaced here.
func (a *App) renderTable(c *ui.Context, d *parser.Document, i int) bool {
	b := d.Block(i)
	if b.Kind != parser.Table {
		return false
	}
	src := d.Source()
	if b.Start < 0 || b.End > len(src) || b.Start > b.End {
		return false
	}
	tableSrc := src[b.Start:b.End]
	theme := tableThemeKey(a.look)
	id := tableCacheID(a.hScrollMsg, b.Start, theme)
	if a.tableCacheSeen != nil {
		a.tableCacheSeen[id] = struct{}{}
	}
	lay := a.tableCache[id]
	if lay == nil || lay.src != tableSrc || lay.theme != theme {
		lay = a.measureTable(c, d, i, tableSrc, theme)
		if a.tableCache == nil {
			a.tableCache = map[string]*tableLayout{}
		}
		a.tableCache[id] = lay
	}
	a.paintTableLayout(c, lay, i)
	return true
}

func (a *App) measureTable(c *ui.Context, d *parser.Document, i int, src, theme string) *tableLayout {
	b := d.Block(i)
	p := a.look
	var rows [][]tableCell
	for r := i + 1; r < b.Next; r = d.Block(r).Next {
		row := d.Block(r)
		if row.Kind != parser.TableRow {
			continue
		}
		var line []tableCell
		k := 0
		for ci := r + 1; ci < row.Next; ci = d.Block(ci).Next {
			cb := d.Block(ci)
			if cb.Kind != parser.TableCell {
				continue
			}
			weight := 400
			if row.Header {
				weight = 600
			}
			pieces := cellPieces(d.Inlines(ci), 0, len(d.Inlines(ci)), weight, p, tableBodySize, tableCodeSize)
			w := measurePieces(c, pieces)
			line = append(line, tableCell{pieces: pieces, width: w, head: row.Header, first: k == 0})
			k++
		}
		if len(line) > 0 {
			rows = append(rows, line)
		}
	}
	cols := 0
	for _, row := range rows {
		if len(row) > cols {
			cols = len(row)
		}
	}
	natural := make([]float32, cols)
	for _, row := range rows {
		for k, cell := range row {
			if cell.width > natural[k] {
				natural[k] = cell.width
			}
		}
	}
	widths := make([]float32, cols)
	for k := range widths {
		w := natural[k] + tableCellPadX*2
		if w > tableMaxCol {
			w = tableMaxCol
		}
		widths[k] = w
	}
	heights := make([]float32, len(rows))
	for ri, row := range rows {
		header := len(row) > 0 && row[0].head
		maxLines := 1
		for k := range row {
			inner := widths[k] - tableCellPadX*2
			n := wrappedLineCount(row[k].pieces, inner, row[k].width)
			row[k].lines = n
			if n > maxLines {
				maxLines = n
			}
		}
		heights[ri] = tableRowHeight(maxLines, header)
	}
	var total float32
	for _, w := range widths {
		total += w
	}
	aligns := append([]parser.Align(nil), b.Align...)
	return &tableLayout{src: src, theme: theme, rows: rows, widths: widths, heights: heights, total: total, aligns: aligns}
}

func (a *App) paintTableLayout(c *ui.Context, lay *tableLayout, i int) {
	p := a.look
	total := lay.total
	slot := a.hScrollSlot(a.hScrollKey("table", i))
	// Hug the columns when they fit; MaxWidthPercent keeps an overflowing
	// table inside the message so ScrollHorizontal can take over.
	ui.Box(c).Width(total).MaxWidthPercent(100).MinWidth(0).AlignSelf(ui.Start).Radius(radiusCode).Clip().Children(func() {
		ui.ScrollHorizontal(c).FillWidth().MinWidth(0).TrackScroll(&slot.st).Label("table scroller").Children(func() {
			stack := ui.Box(c).Width(total).Shrink(0)
			a.fadeColors(stack)
			stack.Children(func() {
				ui.Column(c).Width(total).Shrink(0).Children(func() {
					for ri, row := range lay.rows {
						bar := ui.Box(c).Height(lay.heights[ri]).Width(total)
						if len(row) > 0 && row[0].head {
							bar.Background(p.TableHeader)
						}
					}
				})
				front := ui.Column(c).Absolute().Left(0).Top(0).Width(total).Shrink(0)
				if fx := tableContentFade(c, slot); fx != nil {
					front.Effect(fx)
				}
				front.Children(func() {
					for ri, row := range lay.rows {
						header := len(row) > 0 && row[0].head
						box := ui.Column(c).Width(total).Height(lay.heights[ri]).Shrink(0)
						box.Children(func() {
							if !header {
								ui.Box(c).Height(1).Width(total).Background(p.TableRule)
							}
							line := ui.Row(c).Grow(1).AlignItems(ui.Stretch).MinHeight(0)
							line.Children(func() {
								for k, cell := range row {
									padTop := float32(tableCellPadY)
									if !header {
										padTop = tableCellPadY - 1
									}
									color := p.TextSecondary
									if cell.head || cell.first {
										color = p.TextPrimary
									}
									cellBox := ui.Column(c).Width(lay.widths[k]).FillHeight().Padding(padTop, tableCellPadX, tableCellPadY, tableCellPadX).AlignItems(ui.Stretch).Justify(ui.Start).Shrink(0).Label("table-col-" + itoa(k))
									cellBox.Children(func() {
										a.paintCellText(c, cell.pieces, color, cell.head, tableAlign(lay.aligns, k))
									})
								}
							})
						})
					}
				})
			})
		})
	})
}

func tableAlign(aligns []parser.Align, k int) ui.Align {
	if k >= len(aligns) {
		return ui.Start
	}
	switch aligns[k] {
	case parser.AlignCenter:
		return ui.Center
	case parser.AlignRight:
		return ui.End
	default:
		return ui.Start
	}
}

func wrappedLineCount(pieces []cellPiece, wrapW, natural float32) int {
	hard := 1
	for _, pc := range pieces {
		for _, sp := range pc.spans {
			hard += strings.Count(sp.Text, "\n")
		}
	}
	if hard < 1 {
		hard = 1
	}
	if wrapW < 8 {
		wrapW = 8
	}
	if natural <= wrapW {
		return hard
	}
	spans := flattenCellPieces(pieces)
	if len(spans) == 0 {
		return 1
	}
	layout := ui.ShapeRichText(spans, ui.Font{Size: tableBodySize}, wrapW)
	n := shapedLineCount(layout)
	if n < hard {
		n = hard
	}
	return n
}

func shapedLineCount(layout *ui.TextLayout) int {
	n := 1
	y := layout.Caret(0).Y
	i := 0
	for step := 0; step < 20000; step++ {
		j := layout.NextBoundary(i)
		if j <= i {
			break
		}
		cy := layout.Caret(j).Y
		if cy > y+0.5 {
			n++
			y = cy
		}
		i = j
	}
	return n
}

// cellPiece is one run of a table cell, or of a streaming paragraph.
// dest is set for a link, which paints as ui.Link so clicks and hover
// match a paragraph.
type cellPiece struct {
	spans   []ui.Span
	dest    string
	title   string
	pending bool
	auto    bool
}

type inlineStyle struct {
	weight         int
	italic, strike bool
	inLink         bool
	dest, title    string
	pending, auto  bool
}

func cellPieces(in []parser.Inline, from, to, weight int, p Theme, body, code float32) []cellPiece {
	if from < 0 {
		from = 0
	}
	if to > len(in) {
		to = len(in)
	}
	var out []cellPiece
	walkCellInlines(in, from, to, inlineStyle{weight: weight}, p, body, code, &out)
	return out
}

func walkCellInlines(in []parser.Inline, from, to int, st inlineStyle, p Theme, body, code float32, out *[]cellPiece) {
	for k := from; k < to; k = in[k].Next {
		n := in[k]
		switch n.Kind {
		case parser.Text:
			addCellSpan(out, st, p, ui.Span{Text: n.Text, Size: body, Weight: st.weight, Italic: st.italic, Strikethrough: st.strike})
		case parser.SoftBreak:
			addCellSpan(out, st, p, ui.Span{Text: " ", Size: body, Weight: st.weight, Italic: st.italic, Strikethrough: st.strike})
		case parser.LineBreak:
			addCellSpan(out, st, p, ui.Span{Text: "\n", Size: body, Weight: st.weight, Italic: st.italic, Strikethrough: st.strike})
		case parser.Code:
			sp := ui.Span{
				Text: n.Text, Font: "ui-monospace", Size: code, Weight: 400,
				Background: p.CodeInlineBg, Radius: 4,
				Italic: st.italic, Strikethrough: st.strike,
			}
			if !st.inLink {
				sp.Color = p.CodeInlineText
			}
			addCellSpan(out, st, p, sp)
		case parser.Strong:
			cs := st
			cs.weight = 600
			walkCellInlines(in, k+1, n.Next, cs, p, body, code, out)
		case parser.Emphasis:
			cs := st
			cs.italic = true
			walkCellInlines(in, k+1, n.Next, cs, p, body, code, out)
		case parser.Strikethrough:
			cs := st
			cs.strike = true
			walkCellInlines(in, k+1, n.Next, cs, p, body, code, out)
		case parser.Link:
			if st.inLink {
				walkCellInlines(in, k+1, n.Next, st, p, body, code, out)
				continue
			}
			cs := st
			cs.inLink, cs.dest, cs.title = true, n.Dest, n.Title
			cs.pending, cs.auto = n.Pending, n.Auto
			walkCellInlines(in, k+1, n.Next, cs, p, body, code, out)
		case parser.Image:
			alt := strings.TrimSpace(parser.PlainText(in[k+1 : n.Next]))
			if alt == "" {
				alt = imageCellName(n.Dest)
			}
			if st.inLink || n.Dest == "" {
				addCellSpan(out, st, p, ui.Span{Text: alt, Size: body, Weight: st.weight, Italic: st.italic, Strikethrough: st.strike})
				continue
			}
			cs := st
			cs.inLink, cs.dest, cs.title = true, n.Dest, n.Title
			addCellSpan(out, cs, p, ui.Span{
				Text: alt, Size: body, Weight: st.weight,
				Color: p.TextLink, Italic: st.italic, Strikethrough: st.strike,
			})
		case parser.Math:
			addCellSpan(out, st, p, ui.Span{
				Text: n.Text, Font: "ui-monospace", Size: code, Weight: st.weight,
				Color: p.TextSecondary, Italic: st.italic, Strikethrough: st.strike,
			})
		default:
			if n.Next > k+1 {
				walkCellInlines(in, k+1, n.Next, st, p, body, code, out)
			} else if n.Text != "" {
				addCellSpan(out, st, p, ui.Span{Text: n.Text, Size: body, Weight: st.weight, Italic: st.italic, Strikethrough: st.strike})
			}
		}
		if in[k].Next <= k {
			break
		}
	}
}

func addCellSpan(out *[]cellPiece, st inlineStyle, p Theme, sp ui.Span) {
	if sp.Text == "" {
		return
	}
	if st.inLink && sp.Color.A == 0 {
		sp.Color = p.TextLink
	}
	n := len(*out)
	if n > 0 {
		last := &(*out)[n-1]
		if last.dest == st.dest && last.title == st.title && last.pending == st.pending && last.auto == st.auto {
			last.spans = append(last.spans, sp)
			return
		}
	}
	*out = append(*out, cellPiece{
		spans: []ui.Span{sp}, dest: st.dest, title: st.title,
		pending: st.pending, auto: st.auto,
	})
}

func flattenCellPieces(pieces []cellPiece) []ui.Span {
	var n int
	for _, pc := range pieces {
		n += len(pc.spans)
	}
	out := make([]ui.Span, 0, n)
	for _, pc := range pieces {
		out = append(out, pc.spans...)
	}
	return out
}

func measurePieces(c *ui.Context, pieces []cellPiece) float32 {
	spans := flattenCellPieces(pieces)
	if len(spans) == 0 {
		return 0
	}
	w, _ := c.MeasureText(0, spans...)
	return float32(math.Ceil(float64(w)))
}

func imageCellName(dest string) string {
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return "image"
	}
	if i := strings.IndexAny(dest, "?#"); i >= 0 {
		dest = dest[:i]
	}
	dest = strings.TrimRight(dest, "/")
	base := path.Base(dest)
	if base == "" || base == "." || base == "/" {
		return "image"
	}
	return base
}

func (a *App) paintCellText(c *ui.Context, pieces []cellPiece, color ui.Color, head bool, align ui.Align) {
	rest := pieces
	var el ui.Element
	switch {
	case len(pieces) == 0:
		el = ui.Text(c, "")
	case pieces[0].dest == "":
		el = ui.RichText(c, pieces[0].spans...)
		rest = pieces[1:]
	default:
		el = ui.RichText(c)
	}
	el.FontSize(tableBodySize).FixedLineHeight(tableLineH).TextColor(color)
	if head {
		el.FontWeight(600)
	}
	el.FillWidth().MinWidth(0).TextAlign(align)
	if len(rest) == 0 {
		return
	}
	el.Children(func() {
		for i := range rest {
			a.paintCellPiece(c, rest[i])
		}
	})
}

func (a *App) paintCellPiece(c *ui.Context, pc cellPiece) {
	if pc.dest == "" || pc.pending {
		ui.RichText(c, pc.spans...)
		return
	}
	p := a.look
	l := ui.Link(c, "", "")
	if l.Clicked() {
		if a.md.OnLink != nil {
			a.md.OnLink(pc.dest)
		} else {
			c.OpenURL(pc.dest)
		}
	}
	l.TextColor(p.TextLink)
	switch {
	case pc.title != "":
		l.Tooltip(pc.title)
	case !pc.auto && pc.dest != "" && !strings.HasPrefix(pc.dest, "#"):
		l.Tooltip(pc.dest)
	}
	l.Children(func() { ui.RichText(c, pc.spans...) })
}

// inlineSpans turns a block's inlines into rich-text spans: body text,
// semibold, and inline code as a filled chip (code/inlineBg, no border).
// Links, strikethrough, and images keep the same text as a paragraph.
func inlineSpans(in []parser.Inline, from, to, weight int, p Theme, body, code float32) []ui.Span {
	return flattenCellPieces(cellPieces(in, from, to, weight, p, body, code))
}

// renderStreamCaret replaces the paragraph still being written so a 2×17
// tide caret sits at the end of its text. It blinks 600ms on / 400ms off.
func (a *App) renderStreamCaret(c *ui.Context, d *parser.Document, i int) bool {
	if !a.streamCaret || d.Block(i).Kind != parser.Paragraph || i != lastParagraph(d) {
		return false
	}
	p := a.look
	in := d.Inlines(i)
	spans := inlineSpans(in, 0, len(in), 400, p, 14, 12.5)
	if len(spans) == 0 {
		spans = []ui.Span{{Text: "", Size: 14}}
	}
	avail := float32(readingWidth - 40)
	alpha := a.caretAlpha(c)
	if a.streamCached(spans, avail) {
		a.paintCachedCaret(c, spans, avail, alpha, p)
		return true
	}
	plainW, _ := c.MeasureText(0, spans...)
	if plainW <= avail {
		a.streamLines, a.streamText, a.streamCut = nil, "", 0
		ui.Row(c).Gap(3).AlignItems(ui.Center).Shrink(0).Children(func() {
			ui.RichText(c, spans...).FontSize(14).LineHeight(21.0 / 14.0).TextColor(p.TextPrimary).Selectable()
			ui.Box(c).Size(2, 17).Radius(1).Background(p.ThemeMark).Opacity(alpha).Shrink(0)
		})
		return true
	}
	// A short paragraph keeps the single text element, so existing shots
	// stay pixel-identical. Past six lines the whole element would be
	// reshaped on every token; finished lines are cached instead.
	if plainW <= avail*6 {
		a.streamLines, a.streamText, a.streamCut = nil, "", 0
		shown := append(append([]ui.Span{}, spans...), ui.Span{Text: "\uFFFC", ObjectWidth: 5, ObjectHeight: 17})
		layout := ui.ShapeRichText(shown, ui.Font{Size: 14}, avail)
		ui.Column(c).Shrink(0).Children(func() {
			ui.RichText(c, shown...).FontSize(14).LineHeight(21.0 / 14.0).TextColor(p.TextPrimary).Selectable().MaxWidth(avail)
			for _, o := range layout.Objects() {
				ui.Box(c).Absolute().Left(o.Rect.X+3).Top(o.Rect.Y).Size(2, 17).Radius(1).Background(p.ThemeMark).Opacity(alpha)
			}
		})
		return true
	}
	a.paintCachedCaret(c, spans, avail, alpha, p)
	return true
}

// streamCached reports that this paragraph is already split into finished
// lines and the new text only extends that split.
func (a *App) streamCached(spans []ui.Span, avail float32) bool {
	if len(a.streamLines) == 0 || a.streamAvail != avail || a.streamCut > len(a.streamText) {
		return false
	}
	text := spanText(spans)
	return strings.HasPrefix(text, a.streamText[:a.streamCut])
}

func (a *App) paintCachedCaret(c *ui.Context, spans []ui.Span, avail, alpha float32, p Theme) {
	lines, tail := a.wrapStreamParagraph(c, spans, avail)
	ui.Column(c).Gap(0).Shrink(0).Children(func() {
		for _, line := range lines {
			ui.RichText(c, line.spans...).FontSize(14).LineHeight(21.0 / 14.0).TextColor(p.TextPrimary).Selectable().MaxWidth(avail)
		}
		// Same caret as a one-line paragraph: a 2×17 bar just after the text.
		// It is not an inline object, so finishing a line does not reshape
		// the lines above it.
		ui.Row(c).Gap(3).AlignItems(ui.Center).Shrink(0).Children(func() {
			if len(tail) == 0 {
				tail = []ui.Span{{Text: "", Size: 14}}
			}
			ui.RichText(c, tail...).FontSize(14).LineHeight(21.0 / 14.0).TextColor(p.TextPrimary).Selectable().MaxWidth(avail)
			ui.Box(c).Size(2, 17).Radius(1).Background(p.ThemeMark).Opacity(alpha).Shrink(0)
		})
	})
}

// streamLine is one finished line of the paragraph being written.
// at is its source offset; next is where the following line starts,
// which can be past the displayed text when spaces or a newline were consumed.
type streamLine struct {
	spans    []ui.Span
	at, next int
}

// wrapStreamParagraph splits spans into finished lines and the open tail.
// Finished lines stay cached while new tokens are only appended, so each
// token measures the tail and not the paragraph above it.
func (a *App) wrapStreamParagraph(c *ui.Context, spans []ui.Span, avail float32) (lines []streamLine, tail []ui.Span) {
	text := spanText(spans)
	// A token only appends. When the previous render is still a prefix,
	// finished lines are unchanged and this token measures the tail.
	if a.streamAvail == avail && a.streamText != "" && a.streamCut <= len(a.streamText) && strings.HasPrefix(text, a.streamText) {
		finished, tail, cut := wrapStreamTail(c, spans, text, a.streamCut, avail)
		a.streamLines = append(a.streamLines, finished...)
		a.streamCut = cut
		a.streamText = text
		return a.streamLines, tail
	}
	kept := a.streamText
	if a.streamCut < len(kept) {
		kept = kept[:a.streamCut]
	}
	if a.streamAvail != avail || a.streamCut > len(text) || !strings.HasPrefix(text, kept) {
		a.streamLines = nil
		a.streamCut = 0
	}
	a.streamAvail = avail
	for i, line := range a.streamLines {
		n := spanTextLen(line.spans)
		if line.at+n > len(text) || !sameStreamSpans(sliceStreamSpans(spans, line.at, line.at+n), line.spans) {
			a.streamLines = a.streamLines[:i]
			if i == 0 {
				a.streamCut = 0
			} else {
				a.streamCut = a.streamLines[i-1].next
			}
			break
		}
	}
	finished, tail, cut := wrapStreamTail(c, spans, text, a.streamCut, avail)
	a.streamLines = append(a.streamLines, finished...)
	a.streamCut = cut
	a.streamText = text
	return a.streamLines, tail
}

// wrapStreamTail wraps text[start:]. cut is the source offset where the
// tail begins; the tail itself is not cached, so the next token can extend it.
func wrapStreamTail(c *ui.Context, spans []ui.Span, text string, start int, avail float32) (finished []streamLine, tail []ui.Span, cut int) {
	for start < len(text) {
		if streamWidth(c, spans, start, len(text)) <= avail {
			break
		}
		end, next := streamBreak(c, spans, text, start, avail)
		if next <= start {
			break
		}
		finished = append(finished, streamLine{spans: sliceStreamSpans(spans, start, end), at: start, next: next})
		start = next
	}
	return finished, sliceStreamSpans(spans, start, len(text)), start
}

// streamBreak returns the displayed end of the line starting at start, and
// the source offset of the next line. Trailing spaces and a hard newline
// are consumed so the next line does not start with them; a newline is not
// part of the displayed line.
func streamBreak(c *ui.Context, spans []ui.Span, text string, start int, avail float32) (end, next int) {
	fitEnd, fitNext := start, start
	i := start
	for i < len(text) {
		if text[i] == '\n' {
			return i, i + 1
		}
		j := i
		for j < len(text) && text[j] != ' ' && text[j] != '\n' {
			j++
		}
		word := j
		for j < len(text) && text[j] == ' ' {
			j++
		}
		if word < len(text) && text[word] == '\n' {
			if i == start || streamWidth(c, spans, start, word) <= avail {
				return word, word + 1
			}
			return fitEnd, fitNext
		}
		if streamWidth(c, spans, start, word) <= avail {
			fitEnd, fitNext = word, j
			i = j
			continue
		}
		if fitEnd == start {
			if word == start {
				return start + 1, start + 1
			}
			return word, word
		}
		return fitEnd, fitNext
	}
	return fitEnd, fitNext
}

func streamWidth(c *ui.Context, spans []ui.Span, from, to int) float32 {
	line := sliceStreamSpans(spans, from, to)
	if len(line) == 0 {
		return 0
	}
	w, _ := c.MeasureText(0, line...)
	return w
}

func spanText(spans []ui.Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

func spanTextLen(spans []ui.Span) int {
	n := 0
	for _, s := range spans {
		n += len(s.Text)
	}
	return n
}

func sliceStreamSpans(spans []ui.Span, from, to int) []ui.Span {
	if from >= to {
		return nil
	}
	var out []ui.Span
	off := 0
	for _, s := range spans {
		n := len(s.Text)
		lo, hi := off, off+n
		off = hi
		if hi <= from || lo >= to {
			continue
		}
		if lo < from {
			s.Text = s.Text[from-lo:]
			lo = from
		}
		if hi > to {
			s.Text = s.Text[:to-lo]
		}
		if s.Text != "" {
			out = append(out, s)
		}
	}
	return out
}

func sameStreamSpans(a, b []ui.Span) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Text != b[i].Text || a[i].Font != b[i].Font || a[i].Size != b[i].Size || a[i].Weight != b[i].Weight || a[i].Italic != b[i].Italic || a[i].Strikethrough != b[i].Strikethrough || a[i].Color != b[i].Color || a[i].Background != b[i].Background {
			return false
		}
	}
	return true
}

func lastParagraph(d *parser.Document) int {
	last := -1
	for i := 1; i < d.Len(); i++ {
		if d.Block(i).Kind == parser.Paragraph {
			last = i
		}
	}
	return last
}

// caretAlpha is the stream caret: on for 600ms, off for 400ms. Reduce
// Motion and tests hold it on.
func (a *App) caretAlpha(c *ui.Context) float32 {
	if a.instant || a.reduceMotion {
		return 1
	}
	c.After(50 * time.Millisecond)
	ms := c.Now().UnixMilli() % int64(motionCaret/time.Millisecond)
	if ms < 0 {
		ms += int64(motionCaret / time.Millisecond)
	}
	if ms < 600 {
		return 1
	}
	return 0
}
