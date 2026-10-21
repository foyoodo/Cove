package main

import (
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// Quote prompt format
//
// When a user turn has quotes, the Chat Completions message content is the
// quotes, in the order they were added, followed by the typed text. Each
// quote is a Markdown blockquote. Every non-empty line is prefixed with
// "> ". A blank line inside a quote is a line whose entire text is ">"
// (no trailing space). A blank
// line (a line with no characters) separates one quote from the next, and
// another blank line separates the last quote from the typed text. Nothing
// is truncated. A turn with no quotes sends Content unchanged.
//
//	> one
//	> two
//	> three
//
//	What does this mean?
//
// Two quotes, the first containing a blank line:
//
//	> alpha
//	>
//	> omega
//
//	> second quote
//
//	the typed text

// HistorySelection is the transcript selection the quote bar and ⌘L read.
// Rect is the union of the selected glyph spans, in window DIPs.
// First is the first line's selected span; the bar centers on it.
// LastBottom is the bottom of the last line's span; the bar flips below it.
// SourceMessageID is the first message that drag touched. Multi is a
// selection that crossed messages, so the card has no source line. Code
// is a selection that stayed inside one code block.
type HistorySelection struct {
	Text            string
	SourceMessageID string
	Rect            ui.Rect
	First           ui.Rect
	LastBottom      float32
	Multi           bool
	Code            bool
	Lang            string
}

// quoteRequestText is the user message content sent to the model.
// See the format comment above.
func quoteRequestText(quotes []Quote, body string) string {
	var blocks []string
	for _, q := range quotes {
		if q.Text == "" {
			continue
		}
		blocks = append(blocks, quoteBody(q))
	}
	if len(blocks) == 0 {
		return body
	}
	out := strings.Join(blocks, "\n\n")
	if body == "" {
		return out
	}
	return out + "\n\n" + body
}

// quoteBlock prefixes every non-empty line with "> ". A blank line is ">".
func quoteBlock(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line == "" {
			lines[i] = ">"
			continue
		}
		lines[i] = "> " + line
	}
	return strings.Join(lines, "\n")
}

// quoteBody is the text of one quote as it is sent. A code selection keeps
// its fence so the model still sees a code block inside the blockquote.
func quoteBody(q Quote) string {
	text := q.Text
	if q.Code {
		lang := q.Lang
		if lang == "text" {
			lang = ""
		}
		text = "```" + lang + "\n" + strings.TrimRight(text, "\n") + "\n```"
	}
	return quoteBlock(text)
}

// addQuote appends q when its text is non-blank and not already in list.
// The same text anywhere in the list is a duplicate; the earlier one stays.
// quoteFold collapses whitespace so two selections of the same words are
// one quote. The stored text stays exactly what was selected.
func quoteFold(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

const quoteMax = 5

func addQuote(list []Quote, q Quote) ([]Quote, bool) {
	if strings.TrimSpace(q.Text) == "" {
		return list, false
	}
	if quoteDupIndex(list, q.Text) >= 0 || len(list) >= quoteMax {
		return list, false
	}
	return append(list, q), true
}

// quoteDupIndex is the card whose folded text matches, or -1.
func quoteDupIndex(list []Quote, text string) int {
	key := quoteFold(text)
	if key == "" {
		return -1
	}
	for i, e := range list {
		if quoteFold(e.Text) == key {
			return i
		}
	}
	return -1
}

// removeQuote drops the quote at i. An out-of-range index leaves the list.
func removeQuote(list []Quote, i int) []Quote {
	if i < 0 || i >= len(list) {
		return list
	}
	out := make([]Quote, 0, len(list)-1)
	out = append(out, list[:i]...)
	out = append(out, list[i+1:]...)
	return out
}

// quoteAnchor is one selectable paragraph tied to the message it was built for.
type quoteAnchor struct {
	msgID  string
	bounds ui.Rect
	code   bool
	lang   string
}

// assembleSelection joins the selected paragraphs in build order. The source
// is the message of the first non-empty part. Code stays whatever the
// paragraph held, which for a code block is the raw source.
func assembleSelection(parts []ui.SelectedText, anchors map[uint64]quoteAnchor) HistorySelection {
	var b strings.Builder
	var source string
	var rect ui.Rect
	have := false
	multi := false
	code := true
	saw := false
	lang := ""
	var prevMsg string
	for _, p := range parts {
		chunk := quoteRuneSlice(p.Text, p.Start, p.End)
		if chunk == "" {
			continue
		}
		anc, ok := anchors[p.ID]
		if b.Len() > 0 {
			if ok && prevMsg != "" && anc.msgID != prevMsg {
				b.WriteString("\n\n")
			} else {
				b.WriteByte('\n')
			}
		}
		b.WriteString(chunk)
		if ok {
			if source == "" {
				source = anc.msgID
			} else if anc.msgID != source {
				multi = true
			}
			prevMsg = anc.msgID
			if !anc.code {
				code = false
			} else if lang == "" {
				lang = anc.lang
			} else if lang != anc.lang {
				code = false
			}
		} else {
			code = false
		}
		saw = true
		if ok && anc.bounds.W > 0 && anc.bounds.H > 0 {
			if !have {
				rect = anc.bounds
				have = true
			} else {
				rect = unionRect(rect, anc.bounds)
			}
		}
	}
	if !saw {
		code = false
	}
	return HistorySelection{
		Text: b.String(), SourceMessageID: source, Rect: rect,
		Multi: multi, Code: code && saw, Lang: lang,
	}
}

func quoteRuneSlice(s string, from, to int) string {
	if from > to {
		from, to = to, from
	}
	if from < 0 {
		from = 0
	}
	i, n := 0, 0
	a, z := len(s), len(s)
	for i < len(s) {
		if n == from {
			a = i
		}
		if n == to {
			z = i
			break
		}
		_, w := utf8.DecodeRuneInString(s[i:])
		if w <= 0 {
			break
		}
		i += w
		n++
	}
	if from >= n {
		return ""
	}
	// to landed past the last rune. An exact end is already in z.
	if to > n {
		z = len(s)
	}
	if z < a {
		return ""
	}
	return s[a:z]
}

func rectsIntersect(a, b ui.Rect) bool {
	if a.W <= 0 || a.H <= 0 || b.W <= 0 || b.H <= 0 {
		return false
	}
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

func unionRect(a, b ui.Rect) ui.Rect {
	x0 := a.X
	if b.X < x0 {
		x0 = b.X
	}
	y0 := a.Y
	if b.Y < y0 {
		y0 = b.Y
	}
	x1 := a.X + a.W
	if b.X+b.W > x1 {
		x1 = b.X + b.W
	}
	y1 := a.Y + a.H
	if b.Y+b.H > y1 {
		y1 = b.Y + b.H
	}
	return ui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// selectionInView reports whether sel meets the transcript viewport.
// A rect that has not been laid out yet is treated as still in view so the
// bar is not dismissed before the first geometry lands.
func selectionInView(sel, visible ui.Rect) bool {
	if sel.W <= 0 || sel.H <= 0 || visible.W <= 0 || visible.H <= 0 {
		return true
	}
	return rectsIntersect(sel, visible)
}

// placeQuoteBar centers the bar on the first line's selected span, 8pt
// above that line. It flips to 8pt below lastBottom when the bar would
// not clear the top of visible, and keeps quoteBarEdge inside the visible
// sides. ok is false when the span is outside visible.
func placeQuoteBar(first ui.Rect, lastBottom float32, visible ui.Rect, barW float32) (x, y float32, below, ok bool) {
	if first.W <= 0 || first.H <= 0 || barW <= 0 {
		return 0, 0, false, false
	}
	if lastBottom < first.Y+first.H {
		lastBottom = first.Y + first.H
	}
	span := ui.Rect{X: first.X, Y: first.Y, W: first.W, H: lastBottom - first.Y}
	if !selectionInView(span, visible) {
		return 0, 0, false, false
	}
	x = first.X + first.W/2 - barW/2
	inset := float32(quoteBarEdge)
	if visible.W > 0 {
		minX := visible.X + inset
		maxX := visible.X + visible.W - inset - barW
		if maxX < minX {
			x = minX
		} else {
			if x < minX {
				x = minX
			}
			if x > maxX {
				x = maxX
			}
		}
	}
	y = first.Y - quoteBarGap - quoteBarH
	// Keep at least 8pt under the visible top (card top, or the find bar).
	if visible.H > 0 && y < visible.Y+quoteBarGap {
		below = true
		y = lastBottom + quoteBarGap
		if y+quoteBarH > visible.Y+visible.H-quoteBarGap {
			y = visible.Y + quoteBarGap
		}
	}
	return x, y, below, true
}

// firstLineSpan is the union of the selected fragments on the first line.
func firstLineSpan(lines []ui.Rect) ui.Rect {
	if len(lines) == 0 {
		return ui.Rect{}
	}
	out := lines[0]
	for _, r := range lines[1:] {
		if abs32(r.Y-out.Y) > 0.5 {
			break
		}
		out = unionRect(out, r)
	}
	return out
}

// lastLineBottom is the bottom of the selected fragments on the last line.
func lastLineBottom(lines []ui.Rect) float32 {
	if len(lines) == 0 {
		return 0
	}
	lastY := lines[len(lines)-1].Y
	bot := float32(0)
	for _, r := range lines {
		if abs32(r.Y-lastY) <= 0.5 && r.Y+r.H > bot {
			bot = r.Y + r.H
		}
	}
	return bot
}

func unionLines(lines []ui.Rect) ui.Rect {
	out := lines[0]
	for _, r := range lines[1:] {
		out = unionRect(out, r)
	}
	return out
}

// selectionMapped reports whether any selected paragraph is still built.
// A virtualized row that scrolled away leaves the engine selection pointing
// at paragraphs this frame did not build.
func selectionMapped(parts []ui.SelectedText, anchors map[uint64]quoteAnchor) bool {
	if len(parts) == 0 {
		return true
	}
	for _, p := range parts {
		if _, ok := anchors[p.ID]; ok {
			return true
		}
	}
	return false
}

func pointInRect(x, y float32, r ui.Rect) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}
