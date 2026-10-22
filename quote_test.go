package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestQuotePromptFormat(t *testing.T) {
	body := quoteRequestText(nil, "plain")
	if body != "plain" {
		t.Fatalf("no quotes changed the body: %q", body)
	}
	long := "one\ntwo\nthree"
	got := quoteRequestText([]Quote{{Text: long, SourceMessageID: "m"}}, "What does this mean?")
	want := "> one\n> two\n> three\n\nWhat does this mean?"
	if got != want {
		t.Fatalf("prompt\n got %q\nwant %q", got, want)
	}
	blank := quoteRequestText([]Quote{
		{Text: "alpha\n\nomega"},
		{Text: "second quote"},
	}, "the typed text")
	wantBlank := "> alpha\n>\n> omega\n\n> second quote\n\nthe typed text"
	if blank != wantBlank {
		t.Fatalf("blank lines\n got %q\nwant %q", blank, wantBlank)
	}
	if strings.Contains(blank, "sourceMessage") {
		t.Fatal("source id leaked into the prompt")
	}
}

func TestQuoteDedupeAndRemove(t *testing.T) {
	var list []Quote
	var ok bool
	list, ok = addQuote(list, Quote{Text: "alpha", SourceMessageID: "a"})
	if !ok || len(list) != 1 {
		t.Fatalf("first add %+v", list)
	}
	list, ok = addQuote(list, Quote{Text: "beta", SourceMessageID: "b"})
	if !ok || len(list) != 2 {
		t.Fatalf("second %+v", list)
	}
	list, ok = addQuote(list, Quote{Text: "alpha", SourceMessageID: "c"})
	if ok || len(list) != 2 || list[0].SourceMessageID != "a" || list[1].Text != "beta" {
		t.Fatalf("non-consecutive duplicate was added: %+v ok %v", list, ok)
	}
	list, ok = addQuote(list, Quote{Text: "  \n"})
	if ok || len(list) != 2 {
		t.Fatalf("blank quote %+v", list)
	}
	list = removeQuote(list, 0)
	if len(list) != 1 || list[0].Text != "beta" {
		t.Fatalf("remove %+v", list)
	}
	list = removeQuote(list, 5)
	if len(list) != 1 {
		t.Fatalf("out of range %+v", list)
	}
	list = removeQuote(list, 0)
	if len(list) != 0 {
		t.Fatalf("clear %+v", list)
	}
}

func resolvePhrase(t *testing.T, a *App, tt *ui.Tester, phrase string) []ui.Rect {
	t.Helper()
	a.quotePhrase = phrase
	tt.Frame()
	rects := append([]ui.Rect(nil), a.quotePhraseAt...)
	a.quotePhrase = ""
	if len(rects) == 0 {
		t.Fatalf("no glyph span for %q", phrase)
	}
	return rects
}

func dragRects(tt *ui.Tester, rects []ui.Rect) {
	first, last := rects[0], rects[len(rects)-1]
	inset := func(r ui.Rect) float32 {
		if r.W < 4 {
			return r.W * 0.25
		}
		return 1
	}
	x0 := first.X + inset(first)
	y0 := first.Y + first.H*0.55
	x1 := last.X + last.W - inset(last)
	y1 := last.Y + last.H*0.55
	if x1 <= x0 {
		x1 = x0 + 2
	}
	tt.Press(x0, y0)
	tt.Move(x1, y1)
	tt.Release(x1, y1)
	tt.Frame()
	// The first frame places the bar from a predicted width. The next
	// reads the measured width, and the one after centers on it.
	tt.Frame()
	tt.Frame()
}

func TestQuoteBarTracksSelection(t *testing.T) {
	line := "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda"
	a := newTestApp(t)
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Notes", Loaded: true, Config: DefaultConfig(),
		Messages: []Message{{ID: "u1", Role: roleUser, Content: line, Done: true}},
	}}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	for _, phrase := range []string{"alpha", "delta epsilon", "lambda"} {
		dragRects(tt, resolvePhrase(t, a, tt, phrase))
		if !a.quoteBarOpen {
			t.Fatalf("%s: bar closed, sel %q", phrase, a.histSel.Text)
		}
		if !strings.Contains(a.histSel.Text, strings.Split(phrase, " ")[0]) {
			t.Fatalf("%s: selected %q", phrase, a.histSel.Text)
		}
		bar, ok := tt.Find("Selection")
		if !ok {
			t.Fatalf("%s: bar element missing", phrase)
		}
		got := bar.X + bar.W/2
		want := a.histSel.First.X + a.histSel.First.W/2
		if abs32(got-want) > 1 {
			t.Fatalf("%s: bar mid %.2f, first-line mid %.2f, bar %+v first %+v", phrase, got, want, bar, a.histSel.First)
		}
	}
}

func TestQuoteCardHeight(t *testing.T) {
	when := time.Date(2026, 10, 20, 9, 41, 0, 0, time.UTC)
	one := Quote{Text: "one line of the quote", Src: "Cove", Time: when, SourceMessageID: "a1"}
	two := Quote{Text: "first line of the quote\nsecond line of the quote", Src: "You", Time: when, SourceMessageID: "u1"}
	bareOne := Quote{Text: "one line and no source", Multi: true}
	bareTwo := Quote{Text: "first line and no source\nsecond line and no source", Multi: true}
	want := []struct {
		q Quote
		h float32
	}{{one, 40}, {two, 55}, {bareOne, 26}, {bareTwo, 41}}
	wide := quoteTextWidth(quoteCardComposer, 1100, sidebarW)
	for _, c := range want {
		if quoteCardEstimate(c.q, wide) != c.h {
			t.Fatalf("estimate %q = %.1f, want %.0f", c.q.Text, quoteCardEstimate(c.q, wide), c.h)
		}
	}
	quotes := []Quote{one, two, bareOne, bareTwo}
	check := func(t *testing.T, tt *ui.Tester, textW float32) {
		t.Helper()
		for _, c := range want {
			r, ok := tt.Find(quoteAccessLabel(c.q))
			if !ok {
				t.Fatalf("card missing for %q", c.q.Text)
			}
			est := quoteCardEstimate(c.q, textW)
			if r.H != c.h || est != r.H {
				t.Fatalf("%q rendered %.2f estimate %.2f want %.0f", c.q.Text, r.H, est, c.h)
			}
		}
		src, sok := tt.Find("Cove · 09:41")
		body, bok := tt.Find(one.Text)
		if !sok || !bok {
			t.Fatal("source or body missing")
		}
		if gap := body.Y - (src.Y + src.H); gap < -0.25 || gap > 0.25 {
			t.Fatalf("source-to-body gap %.2f, want 0", gap)
		}
	}
	composer := Conversation{
		ID: "c1", Title: "Notes", Loaded: true, Config: DefaultConfig(),
		Draft: "next", DraftQuotes: quotes,
		Messages: []Message{{ID: "a1", Role: roleAssistant, Content: "earlier", Done: true, CreatedAt: when}},
	}
	a := newTestApp(t)
	a.data.Conversations = []Conversation{composer}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	check(t, tt, wide)
	card, ok := tt.Find(quoteAccessLabel(one))
	if !ok {
		t.Fatal("composer card missing")
	}
	tt.Move(card.X+card.W-12, card.Y+12)
	tt.Frame()
	btn, bok := tt.Find("Remove quote")
	card, ok = tt.Find(quoteAccessLabel(one))
	if !ok || !bok {
		t.Fatal("remove button missing")
	}
	right := card.X + card.W - (btn.X + btn.W)
	top := btn.Y - card.Y
	if right < 3.75 || right > 4.25 || top < 3.75 || top > 4.25 {
		t.Fatalf("remove inset right %.2f top %.2f, want 4", right, top)
	}

	bubble := Conversation{
		ID: "c1", Title: "Notes", Loaded: true, Config: DefaultConfig(),
		Messages: []Message{{
			ID: "u1", Role: roleUser, Content: "the question", Done: true, CreatedAt: when,
			Quotes: quotes,
		}},
	}
	a = newTestApp(t)
	a.data.Conversations = []Conversation{bubble}
	a.data.ActiveID = "c1"
	tt = openTester(a)
	check(t, tt, quoteTextWidth(quoteCardBubble, 1100, sidebarW))
}

func TestQuoteCardWrapsAtWidth(t *testing.T) {
	when := time.Date(2026, 10, 20, 9, 41, 0, 0, time.UTC)
	latin65 := string([]rune(strings.Repeat("Minimum ", 9))[:65])
	latin100 := strings.TrimSpace(strings.Repeat("cancel ", 20))
	latin100 = string([]rune(latin100)[:100])
	han := string([]rune("取消进行中的请求需要绑定窗口关闭时的上下文并停止读取正文")[:24])
	samples := []string{latin65, latin100, han}
	allowed := map[float32]bool{26: true, 41: true, 40: true, 55: true}
	for _, win := range []int{720, 1120} {
		for _, text := range samples {
			for _, src := range []bool{true, false} {
				q := Quote{Text: text, Multi: !src}
				if src {
					q.Src = "Cove"
					q.Time = when
					q.Multi = false
				}
				a := newTestApp(t)
				a.data.Conversations = []Conversation{{
					ID: "c1", Title: "Notes", Loaded: true, Config: DefaultConfig(),
					Draft: "next", DraftQuotes: []Quote{q},
					Messages: []Message{{ID: "a1", Role: roleAssistant, Content: "earlier", Done: true, CreatedAt: when}},
				}}
				a.data.ActiveID = "c1"
				tt := ui.NewTester(a.view, win, 640)
				tt.Frame()
				w := quoteTextWidth(quoteCardComposer, float32(win), a.sidebarWidth)
				r, ok := tt.Find(quoteAccessLabel(q))
				if !ok {
					t.Fatalf("win %d %q missing", win, text)
				}
				est := quoteCardEstimate(q, w)
				if r.H != est || !allowed[r.H] {
					t.Fatalf("composer win %d %q src %v rendered %.2f estimate %.2f width %.1f lines %d", win, text, src, r.H, est, w, quoteLayoutLines(q, w, false))
				}
				body, bok := tt.Find(text)
				if bok && abs32(body.W-w) > 2 {
					t.Fatalf("composer body width %.1f, shaped at %.1f (%q @ %d)", body.W, w, text, win)
				}

				b := newTestApp(t)
				b.data.Conversations = []Conversation{{
					ID: "c1", Title: "Notes", Loaded: true, Config: DefaultConfig(),
					Messages: []Message{{
						ID: "u1", Role: roleUser, Content: "question", Done: true, CreatedAt: when,
						Quotes: []Quote{q},
					}},
				}}
				b.data.ActiveID = "c1"
				bt := ui.NewTester(b.view, win, 640)
				bt.Frame()
				bw := quoteTextWidth(quoteCardBubble, float32(win), b.sidebarWidth)
				br, ok := bt.Find(quoteAccessLabel(q))
				if !ok {
					t.Fatalf("bubble win %d %q missing", win, text)
				}
				best := quoteCardEstimate(q, bw)
				if br.H != best || !allowed[br.H] {
					t.Fatalf("bubble win %d %q src %v rendered %.2f estimate %.2f width %.1f lines %d", win, text, src, br.H, best, bw, quoteLayoutLines(q, bw, false))
				}
			}
		}
	}
	// The narrow composer wraps ~65 characters onto two lines.
	q65 := Quote{Text: latin65, Src: "Cove", Time: when}
	if quoteLayoutLines(q65, quoteTextWidth(quoteCardComposer, 720, sidebarW), false) != 2 {
		fullW, _ := ui.ShapeText(latin65, quoteBodyFont(q65), 10000).Size()
		t.Fatalf("720 should wrap 65 chars, width %.1f string %.1f %q", quoteTextWidth(quoteCardComposer, 720, sidebarW), fullW, latin65)
	}
	if quoteCardEstimate(q65, quoteTextWidth(quoteCardComposer, 720, sidebarW)) != 55 {
		t.Fatal("720 65-char card should be 55pt")
	}
	shot := newTestApp(t)
	shot.data.Conversations = []Conversation{{
		ID: "c1", Title: "Notes", Loaded: true, Config: DefaultConfig(),
		Draft: "next", DraftQuotes: []Quote{q65},
		Messages: []Message{{ID: "a1", Role: roleAssistant, Content: "earlier", Done: true, CreatedAt: when}},
	}}
	shot.data.ActiveID = "c1"
	st := ui.NewTester(shot.view, 720, 640)
	st.Frame()
	card, ok := st.Find(quoteAccessLabel(q65))
	if !ok || card.H != 55 {
		t.Fatalf("720 crop card %+v", card)
	}
	dir := testArtifactDir(t, "quote")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writePNG(t, filepath.Join(dir, "composer-quotes-720-65-light.png"), st.Image())
	line := fmt.Sprintf("quote 720 65-char height=%.3f lines=%d width=%.3f want=55 %s\n",
		card.H, quoteLayoutLines(q65, quoteTextWidth(quoteCardComposer, 720, shot.sidebarWidth), false),
		quoteTextWidth(quoteCardComposer, 720, shot.sidebarWidth), passQuarter(card.H-55))
	prev, _ := os.ReadFile(filepath.Join(dir, "measurements.txt"))
	var kept []string
	for _, ln := range strings.Split(string(prev), "\n") {
		if strings.HasPrefix(ln, "quote 720 ") || strings.HasPrefix(ln, "quote 1120 ") || ln == "" {
			continue
		}
		kept = append(kept, ln)
	}
	note := strings.Join(kept, "\n")
	if note != "" {
		note += "\n"
	}
	// A wide window keeps ~100 characters on one line: 40pt with a source
	// line, and no empty second row.
	q100 := Quote{Text: latin100, Src: "Cove", Time: when}
	wideW := quoteTextWidth(quoteCardComposer, 1120, sidebarW)
	if n := quoteLayoutLines(q100, wideW, false); n != 1 {
		fullW, _ := ui.ShapeText(latin100, quoteBodyFont(q100), 10000).Size()
		t.Fatalf("1120 should keep 100 chars on one line, width %.1f string %.1f lines %d", wideW, fullW, n)
	}
	if quoteCardEstimate(q100, wideW) != 40 {
		t.Fatal("1120 100-char card should be 40pt")
	}
	wideShot := newTestApp(t)
	wideShot.data.Conversations = []Conversation{{
		ID: "c1", Title: "Notes", Loaded: true, Config: DefaultConfig(),
		Draft: "next", DraftQuotes: []Quote{q100},
		Messages: []Message{{ID: "a1", Role: roleAssistant, Content: "earlier", Done: true, CreatedAt: when}},
	}}
	wideShot.data.ActiveID = "c1"
	wt := ui.NewTester(wideShot.view, 1120, 640)
	wt.Frame()
	wideCard, ok := wt.Find(quoteAccessLabel(q100))
	body, bok := wt.Find(latin100)
	if !ok || !bok || wideCard.H != 40 || abs32(body.H-15) > 0.5 {
		t.Fatalf("1120 crop card %+v body %+v", wideCard, body)
	}
	writePNG(t, filepath.Join(dir, "composer-quotes-1120-100-light.png"), wt.Image())
	wideLine := fmt.Sprintf("quote 1120 100-char height=%.3f body=%.3f lines=%d chars=%d width=%.3f want=40 %s\n",
		wideCard.H, body.H, quoteLayoutLines(q100, quoteTextWidth(quoteCardComposer, 1120, wideShot.sidebarWidth), false),
		len([]rune(latin100)), quoteTextWidth(quoteCardComposer, 1120, wideShot.sidebarWidth), passQuarter(wideCard.H-40))
	if err := os.WriteFile(filepath.Join(dir, "measurements.txt"), []byte(note+line+wideLine), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestQuoteBodyWidthMatchesLineCount(t *testing.T) {
	when := time.Date(2026, 10, 20, 9, 41, 0, 0, time.UTC)
	// Long enough to hit the card's wrap width in both the composer and
	// the bubble, so the rendered body is that width rather than the
	// width of a short unwrapped line.
	text := strings.Repeat("Minimum width. ", 12)
	for _, win := range []int{720, 1120} {
		for _, src := range []bool{false, true} {
			q := Quote{Text: text, Multi: !src}
			if src {
				q.Src = "Cove"
				q.Time = when
				q.Multi = false
			}
			check := func(t *testing.T, kind int, quotes []Quote, sent bool) {
				t.Helper()
				a := newTestApp(t)
				conv := Conversation{
					ID: "c1", Title: "Notes", Loaded: true, Config: DefaultConfig(),
					Messages: []Message{{ID: "a1", Role: roleAssistant, Content: "earlier", Done: true, CreatedAt: when}},
				}
				if sent {
					conv.Messages = []Message{{
						ID: "u1", Role: roleUser, Content: "question", Done: true, CreatedAt: when,
						Quotes: quotes,
					}}
				} else {
					conv.Draft = "next"
					conv.DraftQuotes = quotes
				}
				a.data.Conversations = []Conversation{conv}
				a.data.ActiveID = "c1"
				tt := ui.NewTester(a.view, win, 640)
				tt.Frame()
				body, ok := tt.Find(text)
				if !ok {
					t.Fatalf("win %d src %v kind %d body missing", win, src, kind)
				}
				want := quoteTextWidth(kind, float32(win), a.sidebarWidth)
				if abs32(body.W-want) > 0.5 {
					t.Fatalf("win %d src %v kind %d body width %.2f, line-count width %.2f", win, src, kind, body.W, want)
				}
			}
			check(t, quoteCardComposer, []Quote{q}, false)
			check(t, quoteCardBubble, []Quote{q}, true)
		}
	}
}

func TestQuoteBubbleFitsColumn(t *testing.T) {
	when := time.Date(2026, 10, 20, 9, 41, 0, 0, time.UTC)
	// One paragraph long enough to need more than two lines in the bubble.
	text := strings.Repeat("The quoted sentence keeps going past the column. ", 8)
	q := Quote{Text: text, Src: "Cove", Time: when, SourceMessageID: "a1"}
	dir := testArtifactDir(t, "quote")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	type shot struct {
		name       string
		w, h       int
		dark       bool
		scale      float32
		detail     bool
		fullWindow bool
	}
	shots := []shot{
		{"bubble-720-light", 720, 760, false, 1, false, true},
		{"bubble-720-dark", 720, 760, true, 1, false, true},
		{"bubble-min-light", 720, 520, false, 1, false, true},
		{"bubble-720-detail-4x", 720, 760, false, 4, true, false},
		{"bubble-min-detail-4x", 720, 520, false, 4, true, false},
		{"bubble-1120-detail-4x", 1120, 760, false, 4, true, false},
	}
	var lines []string
	for _, sh := range shots {
		a := newTestApp(t)
		a.data.Conversations = []Conversation{{
			ID: "c1", Title: "Notes", Loaded: true, Config: DefaultConfig(),
			Messages: []Message{{
				ID: "u1", Role: roleUser, Content: "Does the quote stay inside the column?", Done: true, CreatedAt: when,
				Quotes: []Quote{q},
			}},
		}}
		a.data.ActiveID = "c1"
		tt := ui.NewTester(a.view, sh.w, sh.h)
		if sh.dark {
			tt.SetDark(true)
		}
		if sh.scale != 1 {
			tt.SetScale(sh.scale)
		}
		tt.Frame()
		measureQuoteFit(t, tt, q, text, quoteCardBubble, float32(sh.w), a.sidebarWidth, sh.scale)
		lines = append(lines, quoteFitLine(tt, q, text, quoteCardBubble, float32(sh.w), a.sidebarWidth, sh.scale, sh.name))
		frame := tt.Image()
		if sh.fullWindow {
			writePNG(t, filepath.Join(dir, sh.name+".png"), frame)
		}
		if sh.detail {
			card, ok := tt.Find(quoteAccessLabel(q))
			if !ok {
				t.Fatal("detail card missing")
			}
			writePNG(t, filepath.Join(dir, sh.name+".png"), cropDIP(frame, card, sh.scale, 28))
		}
		if sh.name == "bubble-720-light" {
			before, _ := tt.Find(quoteAccessLabel(q))
			if err := tt.Click(quoteAccessLabel(q)); err != nil {
				t.Fatal(err)
			}
			after, ok := tt.Find(quoteAccessLabel(q))
			if !ok || after.H <= before.H+12 {
				t.Fatalf("expand did not show the rest: before %.1f after %+v", before.H, after)
			}
			writePNG(t, filepath.Join(dir, "bubble-720-expanded-light.png"), tt.Image())
		}
		// The composer card stays inside the content card too.
		ca := newTestApp(t)
		ca.data.Conversations = []Conversation{{
			ID: "c1", Title: "Notes", Loaded: true, Config: DefaultConfig(),
			Draft: "next", DraftQuotes: []Quote{q},
			Messages: []Message{{ID: "a1", Role: roleAssistant, Content: "earlier", Done: true, CreatedAt: when}},
		}}
		ca.data.ActiveID = "c1"
		ct := ui.NewTester(ca.view, sh.w, sh.h)
		if sh.scale != 1 {
			ct.SetScale(sh.scale)
		}
		ct.Frame()
		measureQuoteFit(t, ct, q, text, quoteCardComposer, float32(sh.w), ca.sidebarWidth, sh.scale)
		lines = append(lines, quoteFitLine(ct, q, text, quoteCardComposer, float32(sh.w), ca.sidebarWidth, sh.scale, "composer "+sh.name))
	}
	prev, _ := os.ReadFile(filepath.Join(dir, "measurements.txt"))
	var kept []string
	for _, ln := range strings.Split(string(prev), "\n") {
		if strings.HasPrefix(ln, "bubble fit ") || strings.HasPrefix(ln, "composer fit ") || ln == "" {
			continue
		}
		kept = append(kept, ln)
	}
	note := strings.Join(kept, "\n")
	if note != "" {
		note += "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "measurements.txt"), []byte(note+strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func measureQuoteFit(t *testing.T, tt *ui.Tester, q Quote, text string, kind int, winW, sidebar, scale float32) {
	t.Helper()
	card, ok := tt.Find(quoteAccessLabel(q))
	body, bok := tt.Find(text)
	content, cok := tt.Find("content card")
	if !ok || !bok || !cok {
		t.Fatalf("win %.0f kind %d card %v body %v content %v", winW, kind, ok, bok, cok)
	}
	limit := content.X + content.W
	var col ui.Rect
	if kind == quoteCardBubble {
		var kol bool
		col, kol = tt.Find("qmsg:u1")
		bubble, uok := tt.Find("user bubble")
		if !kol || !uok {
			t.Fatalf("win %.0f column %v bubble %v", winW, kol, uok)
		}
		if bubble.X+bubble.W > col.X+col.W+0.25 || card.X+card.W > col.X+col.W+0.25 {
			t.Fatalf("win %.0f bubble right %.2f card right %.2f past column right %.2f", winW, bubble.X+bubble.W, card.X+card.W, col.X+col.W)
		}
		limit = col.X + col.W
	}
	shell, sok := tt.Find("composer")
	if kind == quoteCardComposer {
		if !sok {
			t.Fatal("composer shell missing")
		}
		if card.X+card.W > shell.X+shell.W+0.25 {
			t.Fatalf("win %.0f composer card right %.2f past shell %.2f", winW, card.X+card.W, shell.X+shell.W)
		}
	}
	if card.X+card.W > content.X+content.W+0.25 || body.X+body.W > limit+0.25 {
		t.Fatalf("win %.0f kind %d card %.2f body %.2f past limit %.2f content %.2f", winW, kind, card.X+card.W, body.X+body.W, limit, content.X+content.W)
	}
	want := quoteTextWidth(kind, winW, sidebar)
	if abs32(body.W-want) > 0.5 {
		t.Fatalf("win %.0f kind %d body width %.2f, line-count width %.2f", winW, kind, body.W, want)
	}
	img := tt.Image()
	ink, iok := inkRightDIP(img, body, scale)
	if !iok {
		t.Fatalf("win %.0f kind %d no body ink", winW, kind)
	}
	if ink > body.X+body.W+0.25 {
		t.Fatalf("win %.0f kind %d ink right %.2f past body %.2f", winW, kind, ink, body.X+body.W)
	}
	if quoteLayoutLines(q, want, true) > 2 && !line2Ellipsis(img, body, scale) {
		t.Fatalf("win %.0f kind %d line 2 has no ellipsis", winW, kind)
	}
}

func quoteFitLine(tt *ui.Tester, q Quote, text string, kind int, winW, sidebar, scale float32, name string) string {
	card, _ := tt.Find(quoteAccessLabel(q))
	body, _ := tt.Find(text)
	col, _ := tt.Find("qmsg:u1")
	content, _ := tt.Find("content card")
	ink, _ := inkRightDIP(tt.Image(), body, scale)
	edge := content.X + content.W
	if kind == quoteCardBubble {
		edge = col.X + col.W
	}
	return fmt.Sprintf("%s fit card-right=%.2f body-right=%.2f ink-right=%.2f edge=%.2f body-w=%.2f lines=%d ellipsis=%v",
		name, card.X+card.W, body.X+body.W, ink, edge, body.W, quoteLayoutLines(q, quoteTextWidth(kind, winW, sidebar), false), line2Ellipsis(tt.Image(), body, scale))
}

func cropDIP(img *image.RGBA, r ui.Rect, scale, pad float32) image.Image {
	b := img.Bounds()
	x0 := int((r.X - pad) * scale)
	y0 := int((r.Y - pad) * scale)
	x1 := int((r.X + r.W + pad) * scale)
	y1 := int((r.Y + r.H + pad) * scale)
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	return img.SubImage(image.Rect(x0, y0, x1, y1))
}

func inkRightDIP(img *image.RGBA, r ui.Rect, scale float32) (float32, bool) {
	b := img.Bounds()
	x0 := int(r.X * scale)
	x1 := int((r.X + r.W) * scale)
	y0 := int(r.Y * scale)
	y1 := int((r.Y + r.H) * scale)
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	bg := img.RGBAAt(x0, y0)
	best := -1
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			p := img.RGBAAt(x, y)
			if quoteColorDist(p, bg) < 28 {
				continue
			}
			if x > best {
				best = x
			}
		}
	}
	if best < 0 {
		return 0, false
	}
	return float32(best+1) / scale, true
}

func line2Ellipsis(img *image.RGBA, body ui.Rect, scale float32) bool {
	// The second 15pt row. Three dots sit in its trailing 14pt.
	band := ui.Rect{X: body.X + body.W - 18, Y: body.Y + 12, W: 18, H: body.H - 12}
	if band.H < 4 || band.W < 4 {
		return false
	}
	b := img.Bounds()
	x0 := int(band.X * scale)
	x1 := int((band.X + band.W) * scale)
	y0 := int(band.Y * scale)
	y1 := int((band.Y + band.H) * scale)
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	bgx := int((body.X - 3) * scale)
	bgy := int((body.Y + 4) * scale)
	if bgx < b.Min.X {
		bgx = b.Min.X
	}
	if bgy < b.Min.Y {
		bgy = b.Min.Y
	}
	if bgx >= b.Max.X {
		bgx = b.Max.X - 1
	}
	if bgy >= b.Max.Y {
		bgy = b.Max.Y - 1
	}
	bg := img.RGBAAt(bgx, bgy)
	clusters := 0
	open := false
	for x := x0; x < x1; x++ {
		ink := false
		for y := y0; y < y1; y++ {
			if quoteColorDist(img.RGBAAt(x, y), bg) >= 28 {
				ink = true
				break
			}
		}
		if ink && !open {
			clusters++
			open = true
		}
		if !ink {
			open = false
		}
	}
	return clusters >= 2
}

func quoteColorDist(a, b interface {
	RGBA() (uint32, uint32, uint32, uint32)
}) int {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	d := func(x, y uint32) int {
		v := int(x>>8) - int(y>>8)
		if v < 0 {
			return -v
		}
		return v
	}
	return d(ar, br) + d(ag, bg) + d(ab, bb)
}

func TestQuoteBarPlacement(t *testing.T) {
	visible := ui.Rect{X: 200, Y: 80, W: 700, H: 500}
	sel := ui.Rect{X: 240, Y: 200, W: 180, H: 21}
	const barW = float32(160)
	x, y, below, ok := placeQuoteBar(sel, sel.Y+sel.H, visible, barW)
	wantX := sel.X + sel.W/2 - barW/2
	if !ok || below || y != sel.Y-quoteBarH-quoteBarGap || x != wantX {
		t.Fatalf("above: x %.1f y %.1f below %v ok %v", x, y, below, ok)
	}
	// The bar must keep 8pt under the visible top. A selection that would
	// leave only 4pt flips below, even though the bar would still clear
	// the top edge.
	tight := ui.Rect{X: 240, Y: visible.Y + 40, W: 180, H: 21}
	_, y, below, ok = placeQuoteBar(tight, tight.Y+tight.H, visible, barW)
	if !ok || !below || y != tight.Y+tight.H+quoteBarGap {
		t.Fatalf("flip when 4pt from the top: y %.1f below %v ok %v", y, below, ok)
	}
	room := ui.Rect{X: 240, Y: visible.Y + 44, W: 180, H: 21}
	_, y, below, ok = placeQuoteBar(room, room.Y+room.H, visible, barW)
	if !ok || below || y != visible.Y+quoteBarGap {
		t.Fatalf("stay above with 8pt clear: y %.1f below %v ok %v", y, below, ok)
	}
	// A selection whose first line is under the top still flips when the
	// bar would enter the 8pt margin, and the bar sits 8pt below the last
	// line rather than the first.
	top := ui.Rect{X: 240, Y: visible.Y + 4, W: 180, H: 21}
	last := top.Y + top.H + 40
	_, y, below, ok = placeQuoteBar(top, last, visible, barW)
	if !ok || !below || y != last+quoteBarGap {
		t.Fatalf("flip below last line: y %.1f want %.1f below %v ok %v", y, last+quoteBarGap, below, ok)
	}
	if y == top.Y+top.H+quoteBarGap {
		t.Fatal("flipped bar used the first line")
	}
	// Entirely above the viewport: dismiss, do not place.
	gone := ui.Rect{X: 240, Y: 10, W: 180, H: 21}
	if _, _, _, ok = placeQuoteBar(gone, gone.Y+gone.H, visible, barW); ok {
		t.Fatal("selection outside the viewport was placed")
	}
	// Too far right or left: stay 12pt inside the visible edges.
	if quoteBarEdge != 12 {
		t.Fatalf("edge inset %v, bar.edgeInset is 12", quoteBarEdge)
	}
	right := ui.Rect{X: visible.X + visible.W - 10, Y: 200, W: 40, H: 21}
	x, _, _, ok = placeQuoteBar(right, right.Y+right.H, visible, barW)
	if !ok || x != visible.X+visible.W-12-barW {
		t.Fatalf("right clamp x %.1f ok %v", x, ok)
	}
	left := ui.Rect{X: visible.X + 2, Y: 200, W: 24, H: 21}
	x, _, _, ok = placeQuoteBar(left, left.Y+left.H, visible, barW)
	if !ok || x != visible.X+12 {
		t.Fatalf("left clamp x %.1f ok %v", x, ok)
	}
}

func TestAssembleSelectionMergeAndCode(t *testing.T) {
	anchors := map[uint64]quoteAnchor{
		1: {msgID: "first", bounds: ui.Rect{X: 10, Y: 10, W: 40, H: 20}},
		2: {msgID: "second", bounds: ui.Rect{X: 10, Y: 40, W: 80, H: 20}},
		3: {msgID: "code", bounds: ui.Rect{X: 10, Y: 80, W: 100, H: 40}},
	}
	parts := []ui.SelectedText{
		{ID: 1, Text: "Alpha message", Start: 0, End: len([]rune("Alpha message"))},
		{ID: 2, Text: "Beta message", Start: 0, End: len([]rune("Beta"))},
	}
	got := assembleSelection(parts, anchors)
	if got.Text != "Alpha message\n\nBeta" || got.SourceMessageID != "first" || !got.Multi {
		t.Fatalf("merge %+v", got)
	}
	if got.Rect.Y != 10 || got.Rect.H != 50 {
		t.Fatalf("union rect %+v", got.Rect)
	}
	code := "func raw() {\n\treturn 1\n}"
	raw := assembleSelection([]ui.SelectedText{{
		ID: 3, Text: code, Start: 0, End: len([]rune(code)),
	}}, anchors)
	if raw.Text != code || raw.SourceMessageID != "code" {
		t.Fatalf("code quote %q", raw.Text)
	}
}

func TestQuoteShortcutDoesNotConflict(t *testing.T) {
	a := newTestApp(t)
	menu := a.menu()
	got := map[string][]string{}
	var walk func(items []*mygo.MenuItem, path string)
	walk = func(items []*mygo.MenuItem, path string) {
		for _, it := range items {
			if it == nil {
				continue
			}
			name := path
			if it.Label != "" {
				if name != "" {
					name += " > "
				}
				name += it.Label
			}
			if it.Accelerator != "" {
				got[it.Accelerator] = append(got[it.Accelerator], name)
			}
			if len(it.Submenu) > 0 {
				walk(it.Submenu, name)
			}
		}
	}
	walk(menu.Items(), "")
	owners := got["CmdOrCtrl+L"]
	if len(owners) != 1 || !strings.Contains(owners[0], "Add to Chat") {
		t.Fatalf("⌘L owners %v", owners)
	}
	// Bindings that already existed. ⌘L must not be one of them.
	prior := []string{
		"CmdOrCtrl+N", "CmdOrCtrl+,", "Alt+CmdOrCtrl+,", "Shift+CmdOrCtrl+N",
		"CmdOrCtrl+R", "CmdOrCtrl+.", "CmdOrCtrl+F", "CmdOrCtrl+G", "Shift+CmdOrCtrl+G",
		"CmdOrCtrl+Backspace",
	}
	for _, accel := range prior {
		for _, name := range got[accel] {
			if strings.Contains(name, "Add to Chat") {
				t.Fatalf("Add to Chat reused %s (%s)", accel, name)
			}
		}
	}
	// View-only bindings that are not menu items. None of them is ⌘L.
	for _, accel := range []string{"CmdOrCtrl+K", "Up", "Enter", "Shift+Enter"} {
		if _, ok := got[accel]; ok {
			t.Fatalf("view shortcut %s is now a menu item %s", accel, got[accel])
		}
	}

	a.settingsOpen = true
	a.settingsSection = "shortcuts"
	tt := openTester(a)
	blob := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(blob, "Add to Chat") || !strings.Contains(blob, "L") {
		t.Fatalf("shortcuts registry missing Add to Chat:\n%s", blob)
	}
}

func quoteChat() Conversation {
	return Conversation{
		ID: "c1", Title: "Notes", Loaded: true, Config: DefaultConfig(),
		Messages: []Message{
			{ID: "u1", Role: roleUser, Content: "Alpha message", Done: true},
			{ID: "u2", Role: roleUser, Content: "Beta message", Done: true},
			{ID: "a1", Role: roleAssistant, Content: "Hello **world**\n\n```\nfunc raw() {\n\treturn 1\n}\n```\n\n| Head | Tail |\n| --- | --- |\n| cell-alpha | cell-beta |", Done: true, Reasoning: "hidden thought words", ReasoningOpen: true},
		},
	}
}

func dragSelect(t *testing.T, tt *ui.Tester, from, to string) {
	t.Helper()
	a, ok := tt.Find(from)
	if !ok {
		t.Fatalf("missing %q in %q", from, tt.Texts())
	}
	b := a
	if to != from {
		b, ok = tt.Find(to)
		if !ok {
			t.Fatalf("missing %q in %q", to, tt.Texts())
		}
	}
	x0, y0 := a.X+6, a.Y+a.H/2
	x1, y1 := b.X+b.W-6, b.Y+b.H/2
	if x1 <= x0 {
		x1 = x0 + 12
	}
	tt.Press(x0, y0)
	tt.Move(x1, y1)
	if tt.HasText("Add to Chat") {
		t.Fatal("quote bar appeared before mouse-up")
	}
	tt.Release(x1, y1)
}

func TestSelectionBecomesQuote(t *testing.T) {
	a := newTestApp(t)
	a.data.Conversations = []Conversation{quoteChat()}
	a.data.ActiveID = "c1"
	tt := openTester(a)

	dragSelect(t, tt, "Alpha message", "Alpha message")
	if !tt.HasText("Add to Chat") {
		t.Fatal("bar missing after mouse-up")
	}
	tt.Key(ui.Cmd, ui.KeyC)
	if tt.Clipboard() != "Alpha message" {
		t.Fatalf("copy changed: %q", tt.Clipboard())
	}
	if a.histSel.SourceMessageID != "u1" || a.histSel.Text != "Alpha message" {
		t.Fatalf("selection %+v", a.histSel)
	}
	if err := tt.Click("Add to Chat"); err != nil {
		t.Fatal(err)
	}
	conv := a.active()
	if len(conv.DraftQuotes) != 1 || conv.DraftQuotes[0].Text != "Alpha message" || conv.DraftQuotes[0].SourceMessageID != "u1" {
		t.Fatalf("quote %+v", conv.DraftQuotes)
	}
	if !tt.Focused("Message") {
		t.Fatal("Add to Chat did not focus the composer")
	}
	card, ok := tt.Find("Quote from You: Alpha message")
	if !ok {
		t.Fatal("quote card missing")
	}
	tt.Move(card.X+card.W-14, card.Y+12)
	if err := tt.Click("Remove quote"); err != nil {
		t.Fatal(err)
	}
	if len(a.active().DraftQuotes) != 0 {
		t.Fatalf("remove left %+v", a.active().DraftQuotes)
	}
}

func TestMultiMessageAndCodeQuotes(t *testing.T) {
	a := newTestApp(t)
	a.data.Conversations = []Conversation{quoteChat()}
	a.data.ActiveID = "c1"
	tt := openTester(a)

	dragSelect(t, tt, "Alpha message", "Beta message")
	if a.histSel.SourceMessageID != "u1" {
		t.Fatalf("source %q text %q", a.histSel.SourceMessageID, a.histSel.Text)
	}
	if !strings.Contains(a.histSel.Text, "Alpha message") || !strings.Contains(a.histSel.Text, "Beta") {
		t.Fatalf("merged text %q", a.histSel.Text)
	}
	if strings.Index(a.histSel.Text, "Alpha") > strings.Index(a.histSel.Text, "Beta") {
		t.Fatalf("order %q", a.histSel.Text)
	}
	tt.Key(ui.Cmd, ui.KeyL)
	if len(a.active().DraftQuotes) != 1 {
		t.Fatalf("cmd+L quotes %+v", a.active().DraftQuotes)
	}
	// Same text again is not a second quote, even with another message between.
	tt.Key(ui.Cmd, ui.KeyL)
	if len(a.active().DraftQuotes) != 1 {
		t.Fatalf("dedupe %+v", a.active().DraftQuotes)
	}

	code, ok := tt.Find("func raw() {\n\treturn 1\n}")
	if !ok {
		// The block may be one run without the trailing newline the finder uses.
		for _, text := range tt.Texts() {
			if strings.Contains(text, "return 1") && strings.Contains(text, "func raw") {
				code, ok = tt.Find(text)
				break
			}
		}
	}
	if !ok {
		t.Fatalf("code block not visible: %q", tt.Texts())
	}
	tt.Press(code.X+8, code.Y+4)
	tt.Move(code.X+code.W-8, code.Y+code.H-4)
	tt.Release(code.X+code.W-8, code.Y+code.H-4)
	if !strings.Contains(a.histSel.Text, "return 1") || strings.Contains(a.histSel.Text, "```") {
		t.Fatalf("code quote %q", a.histSel.Text)
	}
	if a.histSel.SourceMessageID != "a1" {
		t.Fatalf("code source %q", a.histSel.SourceMessageID)
	}
	tt.Key(ui.Cmd, ui.KeyL)
	if len(a.active().DraftQuotes) != 2 {
		t.Fatalf("quotes after code %+v", a.active().DraftQuotes)
	}
	if strings.Contains(a.active().DraftQuotes[1].Text, "```") {
		t.Fatalf("fence leaked %q", a.active().DraftQuotes[1].Text)
	}

	cell, ok := tt.Find("cell-alpha")
	if !ok {
		t.Fatalf("table cell missing: %q", tt.Texts())
	}
	tt.Press(cell.X+1, cell.Y+cell.H/2)
	tt.Move(cell.X+cell.W-1, cell.Y+cell.H/2)
	tt.Release(cell.X+cell.W-1, cell.Y+cell.H/2)
	if a.histSel.SourceMessageID != "a1" || !strings.Contains(a.histSel.Text, "alpha") {
		t.Fatalf("table quote %+v", a.histSel)
	}

	thought, ok := tt.Find("hidden thought words")
	if !ok {
		t.Fatalf("thinking text missing: %q", tt.Texts())
	}
	tt.Press(thought.X+1, thought.Y+thought.H/2)
	tt.Move(thought.X+thought.W-1, thought.Y+thought.H/2)
	tt.Release(thought.X+thought.W-1, thought.Y+thought.H/2)
	if !strings.Contains(a.histSel.Text, "hidden thought words") || a.histSel.SourceMessageID != "a1" {
		t.Fatalf("thinking quote %+v", a.histSel)
	}
}

func TestQuoteBarReselectAfterRemove(t *testing.T) {
	a := newTestApp(t)
	a.data.Conversations = []Conversation{quoteChat()}
	a.data.ActiveID = "c1"
	tt := openTester(a)

	dragSelect(t, tt, "Alpha message", "Alpha message")
	if err := tt.Click("Add to Chat"); err != nil {
		t.Fatal(err)
	}
	if a.quoteBarOpen || tt.HasText("Add to Chat") {
		t.Fatal("the leftover highlight showed the bar")
	}
	if err := tt.Click("Remove quote"); err != nil {
		t.Fatal(err)
	}
	if len(a.active().DraftQuotes) != 0 {
		t.Fatal("quote was not removed")
	}
	if a.quoteBarOpen || tt.HasText("Add to Chat") {
		t.Fatal("removing the quote showed the bar without a new selection")
	}
	dragSelect(t, tt, "Alpha message", "Alpha message")
	if !a.quoteBarOpen || !tt.HasText("Add to Chat") {
		t.Fatal("reselect after remove did not show the bar")
	}
}

func TestQuoteBarReselectWhileQuoted(t *testing.T) {
	a := newTestApp(t)
	a.data.Conversations = []Conversation{quoteChat()}
	a.data.ActiveID = "c1"
	tt := openTester(a)

	dragSelect(t, tt, "Alpha message", "Alpha message")
	if err := tt.Click("Add to Chat"); err != nil {
		t.Fatal(err)
	}
	dragSelect(t, tt, "Alpha message", "Alpha message")
	if !a.quoteBarOpen || !tt.HasText("Add to Chat") {
		t.Fatal("a new selection of text that is still quoted hid the bar")
	}
	if err := tt.Click("Add to Chat"); err != nil {
		t.Fatal(err)
	}
	if len(a.active().DraftQuotes) != 1 {
		t.Fatalf("dedupe stored %+v", a.active().DraftQuotes)
	}
	tt.Frame()
	if a.quoteFlashAt.IsZero() {
		t.Fatal("adding the quoted text again did not flash the existing card")
	}
}

func TestQuoteBarReselectAfterSend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	a := newTestApp(t)
	cfg := DefaultConfig()
	cfg.BaseURL = srv.URL + "/v1"
	cfg.APIKey = "secret"
	chat := quoteChat()
	chat.Config = cfg
	a.data.Conversations = []Conversation{chat}
	a.data.ActiveID = "c1"
	tt := openTester(a)

	dragSelect(t, tt, "Alpha message", "Alpha message")
	if err := tt.Click("Add to Chat"); err != nil {
		t.Fatal(err)
	}
	tt.Type("hello quote")
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	if len(a.active().DraftQuotes) != 0 {
		t.Fatalf("send left %+v", a.active().DraftQuotes)
	}
	dragSelect(t, tt, "Alpha message", "Alpha message")
	if !a.quoteBarOpen || !tt.HasText("Add to Chat") {
		t.Fatal("reselect after send did not show the bar")
	}
}

func TestQuoteBarReselectWithoutAdding(t *testing.T) {
	a := newTestApp(t)
	a.data.Conversations = []Conversation{quoteChat()}
	a.data.ActiveID = "c1"
	tt := openTester(a)

	dragSelect(t, tt, "Alpha message", "Alpha message")
	if !a.quoteBarOpen || !tt.HasText("Add to Chat") {
		t.Fatal("first selection did not show the bar")
	}
	dragSelect(t, tt, "Alpha message", "Alpha message")
	if !a.quoteBarOpen || !tt.HasText("Add to Chat") {
		t.Fatal("selecting the same range again did not show the bar")
	}
}

func TestQuoteBarDoubleClickShows(t *testing.T) {
	a := newTestApp(t)
	a.data.Conversations = []Conversation{quoteChat()}
	a.data.ActiveID = "c1"
	tt := openTester(a)

	dragSelect(t, tt, "Alpha message", "Alpha message")
	if err := tt.Click("Add to Chat"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Remove quote"); err != nil {
		t.Fatal(err)
	}
	r, ok := tt.Find("Alpha message")
	if !ok {
		t.Fatal("message missing")
	}
	x, y := r.X+10, r.Y+r.H/2
	tt.Press(x, y)
	tt.Release(x, y)
	tt.Press(x, y)
	tt.Release(x, y)
	if !a.quoteBarOpen || !tt.HasText("Add to Chat") {
		t.Fatalf("double-click did not show the bar, selection %q", a.histSel.Text)
	}
}

func TestQuoteCloseVisibleAtRest(t *testing.T) {
	_, tt, q := quoteCloseApp(t, 2, false)
	card, btn := quoteCloseBoxes(t, tt, q)
	ink, ok := measureQuoteInk(tt.Image(), btn, 2)
	if !ok {
		t.Fatal("× is hidden at rest")
	}
	t.Logf("rest ink %dx%d px", ink.w, ink.h)
	if ink.w < 8 || ink.h < 8 {
		t.Fatalf("rest ink %dx%d px is not a drawn ×", ink.w, ink.h)
	}
	right := card.X + card.W - (btn.X + btn.W)
	top := btn.Y - card.Y
	if right < 3.75 || right > 4.25 || top < 3.75 || top > 4.25 {
		t.Fatalf("close inset right %.2f top %.2f, want 4", right, top)
	}
	if btn.W < 20 || btn.H < 20 {
		t.Fatalf("hit area %.2f×%.2f, want at least 20×20", btn.W, btn.H)
	}
}

func TestQuoteCloseClick(t *testing.T) {
	a, tt, _ := quoteCloseApp(t, 1, false)
	if err := tt.Click("Remove quote"); err != nil {
		t.Fatal(err)
	}
	if len(a.active().DraftQuotes) != 0 {
		t.Fatalf("click at rest left %+v", a.active().DraftQuotes)
	}
}

func TestQuoteCloseHoverAndFocus(t *testing.T) {
	_, tt, q := quoteCloseApp(t, 2, false)
	_, btn := quoteCloseBoxes(t, tt, q)
	rest := quoteCircleSample(tt, btn)
	tt.Move(btn.X+btn.W/2, btn.Y+btn.H/2)
	tt.Frame()
	btn, _ = tt.Find("Remove quote")
	hover := quoteCircleSample(tt, btn)
	if quoteChannelDelta(rest, hover) < 4 {
		t.Fatalf("button hover did not fill the circle: rest %v hover %v", rest, hover)
	}
	// Leave the button so the hover fill is gone, then focus it from the keyboard.
	tt.Move(0, 0)
	tt.Frame()
	focusRemoveQuote(t, tt)
	if !tt.Focused("Remove quote") {
		t.Fatal("accessibility target Remove quote is not focused")
	}
	img := tt.Image()
	// The focus ring sits just outside the circle. Sample 2pt above its top edge.
	px := int((btn.X+btn.W/2)*2 + 0.5)
	py := int((btn.Y-2)*2 + 0.5)
	ring := img.RGBAAt(px, py)
	card := img.RGBAAt(int(btn.X*2)+2, int((btn.Y+btn.H+6)*2))
	if quoteChannelDelta(ring, card) < 8 {
		t.Fatalf("keyboard focus did not draw a ring at %d,%d ring %v card %v", px, py, ring, card)
	}
}

func TestQuoteCloseInk(t *testing.T) {
	raw, err := os.ReadFile("themes/tokens.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Layout struct {
			Quote struct {
				Card struct {
					Remove struct {
						Size     float64 `json:"size"`
						IconInk  float64 `json:"iconInk"`
						Stroke   float64 `json:"stroke"`
						Position string  `json:"position"`
					} `json:"remove"`
				} `json:"card"`
			} `json:"quote"`
		} `json:"layout"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	tok := doc.Layout.Quote.Card.Remove
	if tok.Size != quoteCloseSize || tok.IconInk != quoteCloseInk || tok.Stroke != quoteCloseStroke || !strings.Contains(tok.Position, "4") {
		t.Fatalf("tokens %+v, constants size %v iconInk %v stroke %v inset %v", tok, quoteCloseSize, quoteCloseInk, quoteCloseStroke, quoteCloseInset)
	}
	if !strings.Contains(quoteCloseMarkup(), fmt.Sprintf(`stroke-width="%g"`, float64(quoteCloseStroke))) {
		t.Fatal("close icon is not stroked at 1.5pt")
	}
	_, tt, q := quoteCloseApp(t, 2, false)
	_, btn := quoteCloseBoxes(t, tt, q)
	ink, ok := measureQuoteInk(tt.Image(), btn, 2)
	if !ok {
		t.Fatal("no × ink")
	}
	if absFloat(float64(ink.w)-16) > 0.5 || absFloat(float64(ink.h)-16) > 0.5 {
		t.Fatalf("ink bbox %dx%d px at 2x, want 16±0.5", ink.w, ink.h)
	}
	if absFloat(ink.offX) > 0.25 || absFloat(ink.offY) > 0.25 {
		t.Fatalf("ink centre offset %+.3fpt,%+.3fpt, want within ±0.25pt", ink.offX, ink.offY)
	}
	t.Logf("ink bbox %dx%d px centre offset %+.3fpt,%+.3fpt", ink.w, ink.h, ink.offX, ink.offY)
}

func TestQuoteCloseCrops(t *testing.T) {
	dir := testArtifactDir(t, "quote-close-v2")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		_, tt, q := quoteCloseApp(t, 2, dark)
		card, btn := quoteCloseBoxes(t, tt, q)
		saveQuoteCrop(t, filepath.Join(dir, "close-"+mode+"-rest.png"), tt, card)
		tt.Move(btn.X+btn.W/2, btn.Y+btn.H/2)
		tt.Frame()
		card, _ = quoteCloseBoxes(t, tt, q)
		saveQuoteCrop(t, filepath.Join(dir, "close-"+mode+"-hover.png"), tt, card)
		tt.Move(0, 0)
		tt.Frame()
		focusRemoveQuote(t, tt)
		card, _ = quoteCloseBoxes(t, tt, q)
		saveQuoteCrop(t, filepath.Join(dir, "close-"+mode+"-focus.png"), tt, card)
	}
}

func quoteCloseApp(t *testing.T, scale float32, dark bool) (*App, *ui.Tester, Quote) {
	t.Helper()
	when := time.Date(2026, 10, 20, 10, 49, 0, 0, time.UTC)
	q := Quote{Text: "cancel that context when the window closes", Src: "Cove", Time: when, SourceMessageID: "a1"}
	a := newTestApp(t)
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Notes", Loaded: true, Config: DefaultConfig(),
		Draft: "next", DraftQuotes: []Quote{q},
		Messages: []Message{{ID: "a1", Role: roleAssistant, Content: "Alpha message", Done: true, CreatedAt: when}},
	}}
	a.data.ActiveID = "c1"
	tt := ui.NewTester(a.view, 1120, 760)
	if scale > 0 {
		tt.SetScale(scale)
	}
	if dark {
		tt.SetDark(true)
	}
	tt.Frame()
	return a, tt, q
}

func quoteCloseBoxes(t *testing.T, tt *ui.Tester, q Quote) (card, btn ui.Rect) {
	t.Helper()
	var ok bool
	card, ok = tt.Find(quoteAccessLabel(q))
	if !ok {
		t.Fatal("quote card missing")
	}
	btn, ok = tt.Find("Remove quote")
	if !ok {
		t.Fatal("Remove quote missing")
	}
	return card, btn
}

func focusRemoveQuote(t *testing.T, tt *ui.Tester) {
	t.Helper()
	tt.SetFocused(true)
	for i := 0; i < 40; i++ {
		if tt.Focused("Remove quote") {
			tt.Frame()
			return
		}
		tt.Key(0, ui.KeyTab)
	}
	t.Fatal("Remove quote never took keyboard focus")
}

// quoteCircleSample is a point inside the 20pt circle and outside the ×.
func quoteCircleSample(tt *ui.Tester, btn ui.Rect) color.RGBA {
	img := tt.Image()
	b := img.Bounds()
	scale := float32(b.Dx()) / 1120
	x := int((btn.X+btn.W/2+7)*scale + 0.5)
	y := int((btn.Y+btn.H/2)*scale + 0.5)
	if x < 0 || y < 0 || x >= b.Dx() || y >= b.Dy() {
		return color.RGBA{}
	}
	return img.RGBAAt(x, y)
}

type quoteInk struct {
	w, h       int
	offX, offY float64
}

func measureQuoteInk(img *image.RGBA, btn ui.Rect, scale float32) (quoteInk, bool) {
	b := img.Bounds()
	sx := int((btn.X+btn.W/2+7)*scale + 0.5)
	sy := int((btn.Y+btn.H/2)*scale + 0.5)
	if sx < 0 || sy < 0 || sx >= b.Dx() || sy >= b.Dy() {
		return quoteInk{}, false
	}
	bg := img.RGBAAt(sx, sy)
	x0 := int(btn.X*scale) - 1
	y0 := int(btn.Y*scale) - 1
	x1 := int((btn.X+btn.W)*scale) + 2
	y1 := int((btn.Y+btn.H)*scale) + 2
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > b.Dx() {
		x1 = b.Dx()
	}
	if y1 > b.Dy() {
		y1 = b.Dy()
	}
	minX, minY, maxX, maxY := x1, y1, x0, y0
	found := false
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if quoteChannelDelta(img.RGBAAt(x, y), bg) < 24 {
				continue
			}
			if !found || x < minX {
				minX = x
			}
			if !found || y < minY {
				minY = y
			}
			if !found || x > maxX {
				maxX = x
			}
			if !found || y > maxY {
				maxY = y
			}
			found = true
		}
	}
	if !found {
		return quoteInk{}, false
	}
	inkCX := float64(minX+maxX+1) / 2
	inkCY := float64(minY+maxY+1) / 2
	btnCX := float64(btn.X+btn.W/2) * float64(scale)
	btnCY := float64(btn.Y+btn.H/2) * float64(scale)
	return quoteInk{
		w: maxX - minX + 1, h: maxY - minY + 1,
		offX: (inkCX - btnCX) / float64(scale),
		offY: (inkCY - btnCY) / float64(scale),
	}, true
}

func quoteChannelDelta(a, b color.RGBA) int {
	d := absInt(int(a.R) - int(b.R))
	if g := absInt(int(a.G) - int(b.G)); g > d {
		d = g
	}
	if bl := absInt(int(a.B) - int(b.B)); bl > d {
		d = bl
	}
	return d
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func saveQuoteCrop(t *testing.T, path string, tt *ui.Tester, card ui.Rect) {
	t.Helper()
	img := tt.Image()
	scale := float32(2)
	pad := float32(12)
	b := img.Bounds()
	x0 := int((card.X - pad) * scale)
	y0 := int((card.Y - pad) * scale)
	x1 := int((card.X+card.W+pad)*scale + 0.5)
	y1 := int((card.Y+card.H+pad)*scale + 0.5)
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > b.Dx() {
		x1 = b.Dx()
	}
	if y1 > b.Dy() {
		y1 = b.Dy()
	}
	writePNG(t, path, cropImage(img, image.Rect(x0, y0, x1, y1)))
}

func TestQuoteBarDismiss(t *testing.T) {
	a := newTestApp(t)
	a.data.Conversations = []Conversation{quoteChat()}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	dragSelect(t, tt, "Alpha message", "Alpha message")
	if !a.quoteBarOpen {
		t.Fatal("bar did not open")
	}
	tt.Key(0, ui.KeyEscape)
	if a.quoteBarOpen || tt.HasText("Add to Chat") {
		t.Fatal("escape left the bar up")
	}
	tt.Key(ui.Cmd, ui.KeyC)
	if tt.Clipboard() != "Alpha message" {
		t.Fatalf("escape broke copy: %q", tt.Clipboard())
	}

	dragSelect(t, tt, "Alpha message", "Alpha message")
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	if a.quoteBarOpen {
		t.Fatal("click elsewhere left the bar up")
	}
	if len(a.active().DraftQuotes) != 0 {
		t.Fatal("click elsewhere added a quote")
	}
}

func TestQuoteBarScrollDismiss(t *testing.T) {
	a := newTestApp(t)
	msgs := make([]Message, 0, 30)
	for i := range 30 {
		msgs = append(msgs, Message{
			ID:      fmt.Sprintf("m%d", i),
			Role:    roleUser,
			Content: fmt.Sprintf("Scroll line %d %s", i, strings.Repeat("word ", 12)),
			Done:    true,
		})
	}
	msgs[len(msgs)-1].ID = "last"
	msgs[len(msgs)-1].Content = "TAIL MESSAGE MARKER"
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Long", Loaded: true, Config: DefaultConfig(), Messages: msgs,
	}}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	dragSelect(t, tt, "TAIL MESSAGE MARKER", "TAIL MESSAGE MARKER")
	if !a.quoteBarOpen {
		t.Fatal("bar did not open on the tail")
	}
	r, ok := tt.Find("transcript")
	if !ok {
		t.Fatal("no transcript")
	}
	tt.Scroll(r.X+40, r.Y+80, 0, -4000)
	tt.Frame()
	if a.quoteBarOpen || tt.HasText("Add to Chat") {
		t.Fatal("bar stayed after the selection scrolled away")
	}
}

func TestCmdLFocusWithoutSelection(t *testing.T) {
	a := newTestApp(t)
	a.data.Conversations = []Conversation{quoteChat()}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	if err := tt.Click("Search"); err != nil {
		t.Fatal(err)
	}
	if tt.Focused("Message") {
		t.Fatal("search did not take focus")
	}
	tt.Key(ui.Cmd, ui.KeyL)
	if !tt.Focused("Message") {
		t.Fatal("⌘L without a selection did not focus the composer")
	}
	if len(a.active().DraftQuotes) != 0 {
		t.Fatalf("⌘L invented a quote %+v", a.active().DraftQuotes)
	}
}

func TestSendClearsQuotesAndKeepsThemOnTheMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	a := newTestApp(t)
	cfg := DefaultConfig()
	cfg.BaseURL = srv.URL + "/v1"
	cfg.APIKey = "secret"
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Notes", Loaded: true, Config: cfg,
		DraftQuotes: []Quote{{Text: "kept quote", SourceMessageID: "old"}},
		Messages:    []Message{{ID: "prev", Role: roleUser, Content: "earlier", Done: true}},
	}}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	focusDraft(t, tt)
	tt.Type("hello quote")
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	conv := a.active()
	if len(conv.DraftQuotes) != 0 || conv.Draft != "" {
		t.Fatalf("composer still has draft %q quotes %+v", conv.Draft, conv.DraftQuotes)
	}
	var sent *Message
	for i := range conv.Messages {
		if conv.Messages[i].Role == roleUser && conv.Messages[i].Content == "hello quote" {
			sent = &conv.Messages[i]
		}
	}
	if sent == nil || len(sent.Quotes) != 1 || sent.Quotes[0].Text != "kept quote" || sent.Quotes[0].SourceMessageID != "old" {
		t.Fatalf("sent message %+v", conv.Messages)
	}
}

func TestQuoteDraftSurvivesSwitchAndReload(t *testing.T) {
	a := newTestApp(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	cfg := DefaultConfig()
	a.data.Conversations = []Conversation{
		{
			ID: "c1", Title: "Notes", Loaded: true, Config: cfg, CreatedAt: now, UpdatedAt: now,
			Draft: "unsent note", DraftQuotes: []Quote{{Text: "draft quote", SourceMessageID: "u"}},
			Messages: []Message{{
				ID: "u", Role: roleUser, Content: "sent body", Done: true, CreatedAt: now,
				Quotes: []Quote{{Text: "history quote\nstill history", SourceMessageID: "src"}},
			}},
		},
		{
			ID: "c2", Title: "Other", Loaded: true, Config: cfg, CreatedAt: now, UpdatedAt: now.Add(time.Second),
			Messages: []Message{{ID: "o", Role: roleUser, Content: "Elsewhere", Done: true, CreatedAt: now}},
		},
	}
	a.data.ActiveID = "c1"
	a.save()
	a.selectChat("c2")
	if len(a.conversation("c1").DraftQuotes) != 1 || a.conversation("c1").Draft != "unsent note" {
		t.Fatalf("switch dropped the draft %+v", a.conversation("c1").DraftQuotes)
	}
	a.selectChat("c1")
	if a.conversation("c1").DraftQuotes[0].Text != "draft quote" {
		t.Fatalf("switch back %+v", a.conversation("c1").DraftQuotes)
	}

	b := openApp(a.path, newMemKeys())
	b.ensureChat("c1")
	got := b.conversation("c1")
	if got == nil || got.Draft != "unsent note" || len(got.DraftQuotes) != 1 || got.DraftQuotes[0].Text != "draft quote" || got.DraftQuotes[0].SourceMessageID != "u" {
		t.Fatalf("reload draft %+v quotes %+v", got, quotesOf(got))
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "sent body" || len(got.Messages[0].Quotes) != 1 || got.Messages[0].Quotes[0].Text != "history quote\nstill history" {
		t.Fatalf("reload history %+v", got.Messages)
	}
	// A legacy line with no quotes field is unchanged.
	legacy := filepath.Join(storeRoot(a.path), chatsDir, "old.jsonl")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	raw := []byte("{\"type\":\"chat\",\"schema\":5,\"id\":\"old\",\"title\":\"Old\",\"createdAt\":\"2026-01-02T03:04:05Z\",\"settings\":{}}\n{\"type\":\"message\",\"id\":\"u1\",\"role\":\"user\",\"content\":\"from-disk\",\"createdAt\":\"2026-01-02T03:04:06Z\",\"status\":\"complete\"}\n")
	if err := os.WriteFile(legacy, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := readChat(legacy, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(old.Messages) != 1 || old.Messages[0].Quotes != nil || old.Messages[0].Content != "from-disk" || old.DraftQuotes != nil {
		t.Fatalf("legacy %+v", old.Messages)
	}
	line, err := encodeMessage(old, old.Messages[0])
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(line, []byte("quotes")) || bytes.Contains(line, []byte("draftQuotes")) {
		t.Fatalf("legacy rewrite added quotes: %s", line)
	}
}

func quotesOf(c *Conversation) []Quote {
	if c == nil {
		return nil
	}
	return c.DraftQuotes
}

func TestQuoteRequestBodyAndEditRegenerate(t *testing.T) {
	var bodies [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, b)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	quote := "The first line of the quoted passage.\n\nThe second line keeps going.\nThe third line is still part of the quote."
	a := newTestApp(t)
	cfg := DefaultConfig()
	cfg.BaseURL = srv.URL + "/v1"
	cfg.APIKey = "secret"
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Notes", Loaded: true, Config: cfg, CreatedAt: now, UpdatedAt: now,
		Draft: "What does this mean?",
		DraftQuotes: []Quote{{
			Text:            quote,
			SourceMessageID: "src",
		}},
		Messages: []Message{{ID: "seed", Role: roleAssistant, Content: "earlier", Done: true, CreatedAt: now}},
	}}
	a.data.ActiveID = "c1"
	a.send(a.active().Draft)
	if len(bodies) != 1 {
		t.Fatalf("requests %d", len(bodies))
	}
	content := userContent(t, bodies[0])
	want := "> The first line of the quoted passage.\n>\n> The second line keeps going.\n> The third line is still part of the quote.\n\nWhat does this mean?"
	if content != want {
		t.Fatalf("request content\n got %q\nwant %q", content, want)
	}
	if strings.Count(content, "\n") < 3 {
		t.Fatal("quote was truncated below 3 lines")
	}
	dir := testArtifactDir(t, "quote")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "request-body.json"), bodies[0], 0o644); err != nil {
		t.Fatal(err)
	}

	conv := a.active()
	var user *Message
	for i := range conv.Messages {
		if conv.Messages[i].Role == roleUser && conv.Messages[i].Content == "What does this mean?" {
			user = &conv.Messages[i]
		}
	}
	if user == nil || len(user.Quotes) != 1 || user.Quotes[0].Text != quote {
		t.Fatalf("stored quotes %+v", conv.Messages)
	}

	// Edit-and-resend keeps the quotes and sends them with the new text.
	a.editMessage("c1", user.ID)
	a.edit.text = "Revised question"
	a.submitEdit()
	if len(bodies) != 2 {
		t.Fatalf("edit requests %d", len(bodies))
	}
	edited := userContent(t, bodies[1])
	if !strings.HasPrefix(edited, "> The first line of the quoted passage.") || !strings.HasSuffix(edited, "Revised question") {
		t.Fatalf("edit body %q", edited)
	}
	conv = a.active()
	var revised *Message
	for i := range conv.Messages {
		if conv.Messages[i].Role == roleUser && conv.Messages[i].Content == "Revised question" {
			revised = &conv.Messages[i]
		}
	}
	if revised == nil || len(revised.Quotes) != 1 || revised.Quotes[0].Text != quote {
		t.Fatalf("edit dropped quotes %+v", conv.Messages)
	}

	a.regenerate()
	if len(bodies) != 3 {
		t.Fatalf("regenerate requests %d", len(bodies))
	}
	again := userContent(t, bodies[2])
	if again != edited {
		t.Fatalf("regenerate body\n got %q\nwant %q", again, edited)
	}
	if strings.Contains(again, "ok") {
		t.Fatal("regenerate sent the previous reply")
	}
}

func userContent(t *testing.T, body []byte) string {
	t.Helper()
	var payload struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	for i := len(payload.Messages) - 1; i >= 0; i-- {
		if payload.Messages[i].Role == "user" {
			return payload.Messages[i].Content
		}
	}
	t.Fatalf("no user message in %s", body)
	return ""
}
