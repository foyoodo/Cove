package main

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown/parser"
)

// Find in the open chat (SPEC §18.3). Search is plain text, case-insensitive,
// over the text the user can read: markdown markers and link URLs are not
// part of it. The index covers the whole conversation, including history
// before a New Session divider, not only the rows on screen.

const (
	findBarH     = 32
	findRowH     = 62
	findRows     = 6
	findCap      = 1000
	findDebounce = 80 * time.Millisecond
	findFlashDur = 600 * time.Millisecond
)

// findState is the open find session. Hits and docs are replaced on the main
// thread. The build goroutine only publishes through findPending.
type findState struct {
	open         bool
	draft        string
	appliedDraft string
	dirtyDraft   string
	dirtyAt      time.Time
	query        string
	listOpen     bool
	cur          int
	sel          int
	selMoved     bool
	hits         []findHit
	capped       bool
	docs         []findDoc
	convID       string
	ready        bool
	busy         bool
	again        bool
	gen          uint64
	wantFocus    bool
	focused      bool
	enterHandled bool
	lift         float32
	jumpMsg      string
	jumpStart    int
	settled      bool
	nudges       int
	markFresh    bool
	markX        float32
	markY        float32
	markW        float32
	markH        float32
	flashID      string
	flashAt      time.Time
	flashPending bool
	frameNow     time.Time
	listScroll   ui.ScrollState
	paintDoc     *findDoc
	paintRun     int
	paintMsg     string
	hooked       bool
	savedBlock   func(*ui.Context, *parser.Document, int) bool
}

type findHit struct {
	MsgID         string
	Start, End    int // bytes in the message's rendered text
	Before, Match string
	After         string
	Lead, Trail   bool
}

type findRun struct {
	Kind string // "text", "code", "think", "user"
	Text string
	At   int // byte offset of Text in findDoc.Text
}

type findDoc struct {
	ID, Role, Content, Reasoning string
	When                         time.Time
	Open, Done                   bool
	Text                         string
	Runs                         []findRun
}

type findSrc struct {
	ID, Role, Content, Reasoning string
	Open, Done                   bool
	When                         time.Time
}

type findBuild struct {
	gen    uint64
	convID string
	docs   []findDoc
}

type inlStyle struct {
	weight         int
	italic, strike bool
	code, link     bool
	color          ui.Color
}

type styled struct {
	text string
	st   inlStyle
}

func foldRune(r rune) rune {
	best := r
	n := unicode.SimpleFold(r)
	for n != r {
		if n < best {
			best = n
		}
		n = unicode.SimpleFold(n)
	}
	return best
}

func asciiText(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 128 {
			return false
		}
	}
	return true
}

// findFold reports byte ranges of query in text using Unicode simple case folding.
func findFold(text, query string) [][2]int {
	if query == "" || text == "" {
		return nil
	}
	if asciiText(text) && asciiText(query) {
		return findASCII(text, query)
	}
	tr := []rune(text)
	q := make([]rune, 0, len(query))
	for _, r := range query {
		q = append(q, foldRune(r))
	}
	if len(q) == 0 || len(q) > len(tr) {
		return nil
	}
	fr := make([]rune, len(tr))
	at := make([]int, len(tr)+1)
	off := 0
	for i, r := range tr {
		fr[i] = foldRune(r)
		at[i] = off
		off += utf8.RuneLen(r)
	}
	at[len(tr)] = off
	var hits [][2]int
	for i := 0; i+len(q) <= len(fr); {
		match := true
		for j, r := range q {
			if fr[i+j] != r {
				match = false
				break
			}
		}
		if !match {
			i++
			continue
		}
		hits = append(hits, [2]int{at[i], at[i+len(q)]})
		i += len(q)
	}
	return hits
}

func findASCII(text, query string) [][2]int {
	low := strings.ToLower(text)
	q := strings.ToLower(query)
	var hits [][2]int
	for i := 0; i < len(low); {
		j := strings.Index(low[i:], q)
		if j < 0 {
			break
		}
		s := i + j
		hits = append(hits, [2]int{s, s + len(q)})
		i = s + len(q)
	}
	return hits
}

func formatTag(tag string) bool {
	t := strings.ToLower(strings.Trim(tag, "<>/ \t\n"))
	if i := strings.IndexAny(t, " \t\n"); i >= 0 {
		t = t[:i]
	}
	switch t {
	case "b", "strong", "i", "em", "code", "br", "mark", "sub", "sup", "del", "s", "u":
		return true
	}
	return false
}

func walkInlines(in []parser.Inline, from, to int, st inlStyle, out *[]styled) {
	for k := from; k < to; k = in[k].Next {
		n := &in[k]
		switch n.Kind {
		case parser.Text:
			if n.Text != "" {
				*out = append(*out, styled{n.Text, st})
			}
		case parser.Code:
			cs := st
			cs.code = true
			if n.Text != "" {
				*out = append(*out, styled{n.Text, cs})
			}
		case parser.Math:
			if n.Text != "" {
				*out = append(*out, styled{n.Text, st})
			}
		case parser.SoftBreak:
			*out = append(*out, styled{" ", st})
		case parser.LineBreak:
			*out = append(*out, styled{"\n", st})
		case parser.Emphasis:
			cs := st
			cs.italic = true
			walkInlines(in, k+1, n.Next, cs, out)
		case parser.Strong:
			cs := st
			cs.weight = 600
			walkInlines(in, k+1, n.Next, cs, out)
		case parser.Strikethrough:
			cs := st
			cs.strike = true
			walkInlines(in, k+1, n.Next, cs, out)
		case parser.Link:
			cs := st
			cs.link = true
			walkInlines(in, k+1, n.Next, cs, out)
		case parser.Image:
			alt := parser.PlainText(in[k+1 : n.Next])
			if alt == "" {
				alt = "image"
			}
			*out = append(*out, styled{alt, st})
		case parser.HTML:
			if !formatTag(n.Text) && n.Text != "" {
				*out = append(*out, styled{n.Text, st})
			}
		case parser.FootnoteRef:
			*out = append(*out, styled{"[" + n.Text + "]", st})
		}
	}
}

func blockParts(d *parser.Document, i int) []styled {
	in := d.Inlines(i)
	var out []styled
	walkInlines(in, 0, len(in), inlStyle{}, &out)
	return out
}

func joinStyled(parts []styled) string {
	n := 0
	for _, p := range parts {
		n += len(p.text)
	}
	var b strings.Builder
	b.Grow(n)
	for _, p := range parts {
		b.WriteString(p.text)
	}
	return b.String()
}

func renderDoc(m findSrc) findDoc {
	d := findDoc{
		ID: m.ID, Role: m.Role, Content: m.Content, Reasoning: m.Reasoning,
		When: m.When, Open: m.Open, Done: m.Done,
	}
	if m.Role != roleUser && !m.Done {
		return d
	}
	if m.Role == roleUser {
		if m.Content != "" {
			d.Text = m.Content
			d.Runs = []findRun{{Kind: "user", Text: m.Content}}
		}
		return d
	}
	var b strings.Builder
	add := func(kind, text string) {
		if text == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		at := b.Len()
		b.WriteString(text)
		d.Runs = append(d.Runs, findRun{Kind: kind, Text: text, At: at})
	}
	if m.Open && strings.TrimSpace(m.Reasoning) != "" {
		add("think", strings.TrimSpace(m.Reasoning))
	}
	doc := parser.New(parser.Default)
	doc.Set(m.Content)
	for i := 1; i < doc.Len(); i++ {
		blk := doc.Block(i)
		switch blk.Kind {
		case parser.Paragraph, parser.Heading, parser.TableCell:
			add("text", joinStyled(blockParts(doc, i)))
		case parser.CodeBlock:
			add("code", strings.TrimRight(doc.Lines(i), "\n"))
		}
	}
	d.Text = b.String()
	return d
}

func buildDocs(prev []findDoc, snap []findSrc) []findDoc {
	out := make([]findDoc, len(snap))
	for i, m := range snap {
		if i < len(prev) && prev[i].ID == m.ID && prev[i].Role == m.Role && prev[i].Done == m.Done && prev[i].Open == m.Open && prev[i].Content == m.Content && prev[i].Reasoning == m.Reasoning {
			out[i] = prev[i]
			continue
		}
		out[i] = renderDoc(m)
	}
	return out
}

func snapMessages(conv *Conversation) []findSrc {
	if conv == nil {
		return nil
	}
	out := make([]findSrc, len(conv.Messages))
	for i, m := range conv.Messages {
		out[i] = findSrc{
			ID: m.ID, Role: m.Role, Content: m.Content, Reasoning: m.Reasoning,
			Open: m.ReasoningOpen, Done: m.Done || m.Role == roleUser, When: m.CreatedAt,
		}
	}
	return out
}

func collapseSpace(s string) string {
	rs := []rune(s)
	if len(rs) == 0 {
		return ""
	}
	lead := unicode.IsSpace(rs[0])
	trail := unicode.IsSpace(rs[len(rs)-1])
	fields := strings.Fields(s)
	out := strings.Join(fields, " ")
	if len(fields) == 0 {
		return " "
	}
	if lead {
		out = " " + out
	}
	if trail {
		out += " "
	}
	return out
}

func makeSnippet(text string, start, end int) (before, match, after string, lead, trail bool) {
	if start < 0 {
		start = 0
	}
	if end > len(text) {
		end = len(text)
	}
	if start > end {
		start = end
	}
	rs := []rune(text)
	b0 := utf8.RuneCountInString(text[:start])
	b1 := b0 + utf8.RuneCountInString(text[start:end])
	if b0 > len(rs) {
		b0 = len(rs)
	}
	if b1 > len(rs) {
		b1 = len(rs)
	}
	from := b0 - 40
	if from < 0 {
		from = 0
	}
	to := b1 + 110
	if to > len(rs) {
		to = len(rs)
	}
	before = collapseSpace(string(rs[from:b0]))
	match = string(rs[b0:b1])
	after = collapseSpace(string(rs[b1:to]))
	return before, match, after, from > 0, to < len(rs)
}

func searchDocs(docs []findDoc, query string, cap int) ([]findHit, bool) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, false
	}
	var hits []findHit
	capped := false
	for _, d := range docs {
		if d.Text == "" {
			continue
		}
		for _, span := range findFold(d.Text, query) {
			if len(hits) == cap {
				capped = true
				return hits, capped
			}
			before, match, after, lead, trail := makeSnippet(d.Text, span[0], span[1])
			hits = append(hits, findHit{
				MsgID: d.ID, Start: span[0], End: span[1],
				Before: before, Match: match, After: after, Lead: lead, Trail: trail,
			})
		}
	}
	return hits, false
}

func (a *App) findStale(conv *Conversation) bool {
	if conv == nil || a.finder.convID != conv.ID || !a.finder.ready {
		return true
	}
	if len(a.finder.docs) != len(conv.Messages) {
		return true
	}
	for i := range conv.Messages {
		m := &conv.Messages[i]
		d := &a.finder.docs[i]
		done := m.Done || m.Role == roleUser
		if d.ID != m.ID || d.Role != m.Role || d.Open != m.ReasoningOpen {
			return true
		}
		if m.Role != roleUser && !m.Done {
			if d.Done {
				return true
			}
			continue
		}
		if d.Done != done || d.Content != m.Content || d.Reasoning != m.Reasoning {
			return true
		}
	}
	return false
}

func (a *App) scheduleFind(conv *Conversation) {
	if conv == nil || !a.finder.open || a.finder.busy {
		if conv != nil && a.finder.open && a.finder.busy {
			a.finder.again = true
		}
		return
	}
	if !a.findStale(conv) {
		return
	}
	a.finder.busy = true
	a.finder.gen++
	gen := a.finder.gen
	id := conv.ID
	prev := a.finder.docs
	snap := snapMessages(conv)
	go func() {
		docs := buildDocs(prev, snap)
		a.finderMu.Lock()
		a.findPending = &findBuild{gen: gen, convID: id, docs: docs}
		a.finderMu.Unlock()
		if a.win != nil {
			a.win.Invalidate()
		}
	}()
}

func (a *App) pollFind() {
	a.finderMu.Lock()
	b := a.findPending
	a.findPending = nil
	a.finderMu.Unlock()
	if b == nil {
		return
	}
	a.finder.busy = false
	if b.gen == a.finder.gen {
		a.finder.docs = b.docs
		a.finder.convID = b.convID
		a.finder.ready = true
		if a.finder.query != "" {
			a.recomputeHits(true)
		}
	}
	if a.finder.again {
		a.finder.again = false
		a.scheduleFind(a.active())
	}
}

func (a *App) recomputeHits(keep bool) {
	var prevID string
	prevStart, prev := -1, -1
	if keep && a.finder.cur >= 0 && a.finder.cur < len(a.finder.hits) {
		h := a.finder.hits[a.finder.cur]
		prevID, prevStart, prev = h.MsgID, h.Start, a.finder.cur
	}
	hits, capped := searchDocs(a.finder.docs, a.finder.query, findCap)
	a.finder.hits = hits
	a.finder.capped = capped
	if !keep {
		return
	}
	a.finder.cur = -1
	for i, h := range hits {
		if h.MsgID == prevID && h.Start == prevStart {
			a.finder.cur = i
			a.finder.sel = i
			return
		}
	}
	if prev >= 0 && len(hits) > 0 {
		if prev >= len(hits) {
			prev = len(hits) - 1
		}
		a.finder.cur = prev
		a.finder.sel = prev
	}
}

func docByID(docs []findDoc, id string) *findDoc {
	for i := range docs {
		if docs[i].ID == id {
			return &docs[i]
		}
	}
	return nil
}

func runOverlaps(run findRun, hits []findHit, id string) bool {
	end := run.At + len(run.Text)
	for i := range hits {
		h := &hits[i]
		if h.MsgID != id {
			continue
		}
		if h.End > run.At && h.Start < end {
			return true
		}
	}
	return false
}
