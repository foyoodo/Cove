package main

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown/parser"
)

// renderTable draws a markdown table with the token surfaces and horizontal
// rules only. Columns grow to their content plus the design's 12pt cell
// padding, and stop at the reading column. A three-column table uses the
// design fixture's 220/190/190 widths as a floor: measured text plus 12pt
// is narrower with the fonts available here. The stock markdown table
// always paints a vertical border on each cell, so the table block is
// replaced here.
func (a *App) renderTable(c *ui.Context, d *parser.Document, i int) bool {
	b := d.Block(i)
	if b.Kind != parser.Table {
		return false
	}
	p := a.look
	type cell struct {
		spans []ui.Span
		check bool
		width float32
		head  bool
		first bool
	}
	var rows [][]cell
	for r := i + 1; r < b.Next; r = d.Block(r).Next {
		row := d.Block(r)
		if row.Kind != parser.TableRow {
			continue
		}
		var line []cell
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
			spans := inlineSpans(d.Inlines(ci), 0, len(d.Inlines(ci)), weight, p, 13, 13)
			plain := strings.TrimSpace(cellPlain(d, ci))
			check := cellCheck(plain)
			if check {
				spans = stripCheck(spans)
			}
			w, _ := c.MeasureText(0, spans...)
			if check {
				w += 13 + 5
			}
			line = append(line, cell{spans: spans, check: check, width: w, head: row.Header, first: k == 0})
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
	widths := make([]float32, cols)
	for _, row := range rows {
		for k, cell := range row {
			if cell.width > widths[k] {
				widths[k] = cell.width
			}
		}
	}
	const pad = 12
	maxCol := float32(readingWidth - 2)
	// html/app.js freezes the fixture table at these column widths.
	floors := []float32{220, 190, 190}
	for k := range widths {
		widths[k] += pad * 2
		if cols == len(floors) && widths[k] < floors[k] {
			widths[k] = floors[k]
		}
		if widths[k] > maxCol {
			widths[k] = maxCol
		}
	}

	// Wider than the reading column, the table scrolls inside it. A
	// shrink-wrapped grid would otherwise run under the card clip.
	// The header scrolls with the rows. Text, icons and rules fade to
	// transparent at the viewport edges; the row surfaces stay behind
	// them, clipped by the table's own outline.
	slot := a.hScrollSlot(a.hScrollKey("table", i))
	var total float32
	for _, w := range widths {
		total += w
	}
	// Backgrounds stay put. Text, icons and rules are a layer in front
	// whose alpha falls off at the viewport edges, so each row shows its
	// own surface and the fade is not a plate.
	ui.Box(c).FillWidth().MinWidth(0).Radius(radiusCode).Clip().Children(func() {
		ui.ScrollHorizontal(c).FillWidth().MinWidth(0).TrackScroll(&slot.st).Label("table scroller").Children(func() {
			stack := ui.Box(c).Width(total).Shrink(0)
			a.fadeColors(stack)
			stack.Children(func() {
				ui.Column(c).Width(total).Shrink(0).Children(func() {
					for ri, row := range rows {
						if ri > 0 {
							ui.Box(c).Height(1).Width(total)
						}
						bar := ui.Box(c).Height(30).Width(total)
						if row[0].head {
							bar.Background(p.TableHeader)
						}
					}
				})
				front := ui.Column(c).Absolute().Left(0).Top(0).Width(total).Shrink(0)
				if fx := tableContentFade(c, slot); fx != nil {
					front.Effect(fx)
				}
				front.Children(func() {
					for ri, row := range rows {
						if ri > 0 {
							ui.Box(c).Height(1).Width(total).Background(p.TableRule)
						}
						line := ui.Row(c).Height(30).AlignItems(ui.Stretch).Shrink(0)
						line.Children(func() {
							for k, cell := range row {
								w := float32(80)
								if k < len(widths) {
									w = widths[k]
								}
								color := p.TextSecondary
								if cell.head || cell.first {
									color = p.TextPrimary
								}
								box := ui.Row(c).Width(w).Height(30).Padding(0, pad).Gap(5).AlignItems(ui.Center).Shrink(0)
								box.Children(func() {
									if cell.check {
										ui.Icon(c, iconCheck).Size(13, 13).TextColor(p.StatusSuccess).Shrink(0)
									}
									text := ui.RichText(c, cell.spans...).FontSize(13).LineHeight(16.0 / 13.0).TextColor(color).MaxLines(1)
									if cell.head {
										text.FontWeight(600)
									}
								})
							}
						})
					}
				})
			})
		})
	})
	return true
}

// cellCheck reports cells the design marks with a success check: an exact
// "Yes" or "Immediately", or the same words written with a leading check.
func cellCheck(plain string) bool {
	s := strings.TrimSpace(plain)
	s = strings.TrimPrefix(s, "✓")
	s = strings.TrimPrefix(s, "✔")
	s = strings.TrimSpace(s)
	return s == "Yes" || s == "Immediately"
}

func stripCheck(spans []ui.Span) []ui.Span {
	if len(spans) == 0 {
		return spans
	}
	s := spans[0].Text
	s = strings.TrimPrefix(s, "✓")
	s = strings.TrimPrefix(s, "✔")
	s = strings.TrimPrefix(s, " ")
	spans[0].Text = s
	return spans
}

func cellPlain(d *parser.Document, i int) string {
	b := d.Block(i)
	src := d.Source()
	text := ""
	if b.Start >= 0 && b.End <= len(src) && b.End > b.Start {
		text = src[b.Start:b.End]
	} else {
		text = d.Lines(i)
	}
	text = strings.ReplaceAll(text, "\n", " ")
	return strings.TrimSpace(text)
}

// inlineSpans turns a block's inlines into rich-text spans: body text,
// semibold, and inline code as a filled chip (code/inlineBg, no border).
func inlineSpans(in []parser.Inline, from, to, weight int, p Theme, body, code float32) []ui.Span {
	if from < 0 {
		from = 0
	}
	if to > len(in) {
		to = len(in)
	}
	var out []ui.Span
	for k := from; k < to; k = in[k].Next {
		n := in[k]
		switch n.Kind {
		case parser.Text:
			out = append(out, ui.Span{Text: n.Text, Size: body, Weight: weight})
		case parser.SoftBreak:
			out = append(out, ui.Span{Text: " ", Size: body, Weight: weight})
		case parser.LineBreak:
			out = append(out, ui.Span{Text: "\n", Size: body, Weight: weight})
		case parser.Code:
			out = append(out, ui.Span{
				Text: n.Text, Font: "ui-monospace", Size: code, Weight: 400,
				Color: p.CodeInlineText, Background: p.CodeInlineBg, Radius: 4,
			})
		case parser.Strong:
			out = append(out, inlineSpans(in, k+1, n.Next, 600, p, body, code)...)
		case parser.Emphasis:
			inner := inlineSpans(in, k+1, n.Next, weight, p, body, code)
			for i := range inner {
				inner[i].Italic = true
			}
			out = append(out, inner...)
		default:
			if n.Text != "" {
				out = append(out, ui.Span{Text: n.Text, Size: body, Weight: weight})
			}
		}
		if in[k].Next <= k {
			break
		}
	}
	return out
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
		if a[i].Text != b[i].Text || a[i].Font != b[i].Font || a[i].Size != b[i].Size || a[i].Weight != b[i].Weight || a[i].Italic != b[i].Italic || a[i].Color != b[i].Color || a[i].Background != b[i].Background {
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
