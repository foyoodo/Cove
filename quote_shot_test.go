package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestQuoteVisuals(t *testing.T) {
	dir := "/cursor/stores/self/artifacts/quote"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 10, 20, 9, 41, 0, 0, time.UTC)
	you := time.Date(2026, 10, 20, 9, 40, 0, 0, time.UTC)

	body := "Build the request with NewRequestWithContext and cancel that context when the window closes. See the docs.\n\n" +
		"- cancel() aborts the dial, the TLS handshake and the body read at once.\n" +
		"- The call returns context.Canceled, so treat it as a quiet exit, not an error.\n\n" +
		"```go\nctx, cancel := context.WithCancel(context.Background())\nwin.OnClose(cancel)\n```\n\n" +
		"| Approach | Stops the dial | Frees the goroutine |\n| --- | --- | --- |\n| Context cancel | Immediately | Yes |\n"
	asst := func(id, content string, at time.Time) Message {
		return Message{ID: id, Role: roleAssistant, Content: content, Done: true, CreatedAt: at, Reasoning: "Look at the request lifetime.", ReasoningOpen: false}
	}
	user := func(id, content string, at time.Time, quotes []Quote) Message {
		return Message{ID: id, Role: roleUser, Content: content, Done: true, CreatedAt: at, Quotes: quotes}
	}

	open := func(t *testing.T, w, h int, dark bool, conv Conversation) (*App, *ui.Tester) {
		t.Helper()
		a := newTestApp(t)
		a.data.Conversations = []Conversation{conv}
		a.data.ActiveID = conv.ID
		tt := ui.NewTester(a.view, w, h)
		if dark {
			tt.SetDark(true)
		}
		tt.Frame()
		return a, tt
	}
	save := func(t *testing.T, name string, tt *ui.Tester) {
		t.Helper()
		writePNG(t, filepath.Join(dir, name), tt.Image())
	}
	selectPhrase := func(t *testing.T, a *App, tt *ui.Tester, phrase string) {
		t.Helper()
		dragRects(tt, resolvePhrase(t, a, tt, phrase))
		if !strings.Contains(a.histSel.Text, phrase) {
			t.Fatalf("selected %q, want %q", a.histSel.Text, phrase)
		}
	}

	conv := Conversation{
		ID: "c1", Title: "Cancel HTTP on close", Loaded: true, Config: DefaultConfig(),
		CreatedAt: you, UpdatedAt: when,
		Messages: []Message{
			user("u1", "How do I cancel an in-flight HTTP request when the window closes?", you, nil),
			asst("a1", "Tie the request to a context\n\n"+body, when),
		},
	}
	a, tt := open(t, 1120, 760, false, conv)
	selectPhrase(t, a, tt, "cancel that context when the window closes")
	if !a.quoteBarOpen || a.quoteBarBelow {
		t.Fatalf("bar above: open %v below %v", a.quoteBarOpen, a.quoteBarBelow)
	}
	save(t, "selection-bar-light.png", tt)

	tt.SetDark(true)
	tt.Frame()
	save(t, "selection-bar-dark.png", tt)
	tt.SetDark(false)
	tt.Frame()

	// A drag that starts in the user bubble and ends in the reply's heading.
	userSpan := resolvePhrase(t, a, tt, "in-flight HTTP request")
	replySpan := resolvePhrase(t, a, tt, "Tie the request")
	tt.Press(userSpan[0].X+1, userSpan[0].Y+userSpan[0].H*0.55)
	last := replySpan[len(replySpan)-1]
	tt.Move(last.X+last.W-1, last.Y+last.H*0.55)
	tt.Release(last.X+last.W-1, last.Y+last.H*0.55)
	tt.Frame()
	if !a.histSel.Multi {
		t.Fatalf("multi selection %+v", a.histSel)
	}
	save(t, "selection-multi-light.png", tt)

	// Flip: scroll the opening line to the top of the transcript.
	filler := strings.Repeat("The reply keeps going so the opening line can sit under the card top.\n\n", 8)
	conv.Messages[1].Content = "FLIP TARGET sits on the first line of the reply.\n\n" + body + filler
	a.data.Conversations[0] = conv
	tt.Frame()
	r, ok := tt.Find("transcript")
	if !ok {
		t.Fatal("no transcript")
	}
	tt.Scroll(r.X+30, r.Y+40, 0, -8000)
	tt.Frame()
	selectPhrase(t, a, tt, "How do I cancel an in-flight HTTP request when the window closes?")
	if !a.quoteBarBelow {
		t.Fatalf("expected the bar below the selection, rect %+v bar y %.1f", a.histSel.Rect, a.quoteBarY)
	}
	save(t, "selection-bar-flip-light.png", tt)

	quotes := []Quote{
		{Text: "cancel that context when the window closes", Src: "Cove", Time: when, SourceMessageID: "a1"},
		{Text: "ctx, cancel := context.WithCancel(context.Background())\nwin.OnClose(cancel)", Src: "Cove", Time: when, Code: true, Lang: "go", SourceMessageID: "a1"},
		{Text: "How do I cancel an in-flight HTTP request", Src: "You", Time: you, SourceMessageID: "u1"},
	}
	quoted := Conversation{
		ID: "c1", Title: "Cancel HTTP on close", Loaded: true, Config: DefaultConfig(),
		CreatedAt: you, UpdatedAt: when,
		Draft:       "Which of these should I do first?",
		DraftQuotes: quotes,
		Messages: []Message{
			user("u1", "How do I cancel an in-flight HTTP request when the window closes?", you, nil),
			asst("a1", "Tie the request to a context\n\n"+body, when),
		},
	}
	_, tt = open(t, 1120, 760, false, quoted)
	save(t, "composer-quotes-light.png", tt)
	tt.SetDark(true)
	tt.Frame()
	save(t, "composer-quotes-dark.png", tt)

	five := append([]Quote{}, quotes...)
	five = append(five,
		Quote{Text: "aborts the dial, the TLS handshake and the body read at once.", Src: "Cove", Time: when, SourceMessageID: "a1"},
		Quote{Text: "flags the context leak if you forget.", Src: "Cove", Time: when, SourceMessageID: "a1"},
	)
	quoted.Draft = ""
	quoted.DraftQuotes = five
	_, tt = open(t, 1120, 760, false, quoted)
	save(t, "composer-quotes-max-light.png", tt)

	// Duplicate add flashes the existing card.
	qa, qtt := open(t, 1120, 760, false, quoted)
	qa.quoteFlashIndex = 0
	qa.quoteFlashPending = true
	qtt.Frame()
	qtt.Advance(300 * time.Millisecond)
	qtt.Frame()
	if qa.quoteFlashAt.IsZero() {
		t.Fatal("dedupe flash never started")
	}
	save(t, "composer-quotes-flash-light.png", qtt)

	// A sixth add does not insert a card. It shows the limit note.
	limitApp, limitTT := open(t, 1120, 760, false, quoted)
	limitApp.histSel = HistorySelection{Text: "a sixth quote that must be refused"}
	limitApp.addSelectionToComposer()
	limitTT.Frame()
	if !limitTT.HasText("Up to 5 quotes per message.") {
		t.Fatalf("limit note missing: %q", limitTT.Texts())
	}
	if len(limitApp.active().DraftQuotes) != 5 {
		t.Fatalf("sixth quote was stored: %d", len(limitApp.active().DraftQuotes))
	}
	save(t, "composer-quotes-limit-light.png", limitTT)

	// Hover reveals the remove button.
	hoverApp, hoverTT := open(t, 1120, 760, false, quoted)
	card, ok := hoverTT.Find(quoteAccessLabel(five[0]))
	if !ok {
		t.Fatal("hover card missing")
	}
	hoverTT.SetScale(2)
	hoverTT.Frame()
	card, ok = hoverTT.Find(quoteAccessLabel(five[0]))
	if !ok {
		t.Fatal("hover card missing after scale")
	}
	// Land on the card so the button is shown, then on the button itself
	// so its hover fill is the thing under the pointer.
	hoverTT.Move(card.X+card.W-12, card.Y+12)
	hoverTT.Frame()
	btn, bok := hoverTT.Find("Remove quote")
	if !bok {
		t.Fatal("remove button missing on card hover")
	}
	hoverTT.Move(btn.X+btn.W/2, btn.Y+btn.H/2)
	hoverTT.Frame()
	btn, bok = hoverTT.Find("Remove quote")
	card, ok = hoverTT.Find(quoteAccessLabel(five[0]))
	if !ok || !bok {
		t.Fatal("remove button missing on hover")
	}
	right := card.X + card.W - (btn.X + btn.W)
	topGap := btn.Y - card.Y
	removeLine := fmt.Sprintf("quote remove right=%.3f top=%.3f want=4 offset-right=%+.3f offset-top=%+.3f %s",
		right, topGap, right-4, topGap-4, passQuarter(right-4)+" "+passQuarter(topGap-4))
	if right < 3.75 || right > 4.25 || topGap < 3.75 || topGap > 4.25 {
		t.Fatalf("remove inset %s", removeLine)
	}
	img := hoverTT.Image()
	sx := int((btn.X + 3) * 2)
	sy := int((btn.Y + btn.H/2) * 2)
	cx := int((card.X + 24) * 2)
	cy := int((card.Y + card.H/2) * 2)
	if sx >= 0 && sy >= 0 && sx < img.Bounds().Dx() && sy < img.Bounds().Dy() {
		bp, cp := img.RGBAAt(sx, sy), img.RGBAAt(cx, cy)
		if bp.R == cp.R && bp.G == cp.G && bp.B == cp.B {
			t.Fatalf("remove hover fill matches the card at %d,%d #%02x%02x%02x", sx, sy, bp.R, bp.G, bp.B)
		}
	}
	measurePath := filepath.Join(dir, "measurements.txt")
	prev, _ := os.ReadFile(measurePath)
	note := string(prev)
	if note != "" && !strings.HasSuffix(note, "\n") {
		note += "\n"
	}
	if err := os.WriteFile(measurePath, []byte(note+removeLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	save(t, "composer-quotes-remove-light.png", hoverTT)
	_ = hoverApp

	// Adding a quote while the stack is full scrolls the newest card into view.
	older := quotes[:2]
	scrollConv := quoted
	scrollConv.Draft = ""
	scrollConv.DraftQuotes = append([]Quote{}, older...)
	scrollApp, scrollTT := open(t, 1120, 760, false, scrollConv)
	for _, text := range []string{
		"third quote keeps the stack growing\nand wraps to a second line",
		"fourth quote keeps the stack growing\nand wraps to a second line",
		"newest quote is the one that should scroll into view\nand it is two lines tall",
	} {
		scrollApp.histSel = HistorySelection{Text: text}
		scrollApp.addSelectionToComposer()
		scrollTT.Frame()
	}
	if len(scrollApp.active().DraftQuotes) != 5 {
		t.Fatalf("scroll stack %d", len(scrollApp.active().DraftQuotes))
	}
	newestQuote := scrollApp.active().DraftQuotes[len(scrollApp.active().DraftQuotes)-1]
	newest, ok := scrollTT.Find(quoteAccessLabel(newestQuote))
	field, fok := scrollTT.Find("Message")
	first, firstOK := scrollTT.Find(quoteAccessLabel(older[0]))
	if !ok || !fok || !firstOK {
		t.Fatalf("scroll anchors newest %v field %v first %v", ok, fok, firstOK)
	}
	// The quote area is 180pt. Following the end puts the newest card in
	// that window and leaves the first card above it.
	if newest.Y+newest.H > field.Y+2 || newest.Y < field.Y-200 {
		t.Fatalf("newest %+v is outside the quote area above the field %+v", newest, field)
	}
	if first.Y+first.H > field.Y-180 {
		t.Fatalf("first card %+v is still inside the quote viewport; field %+v", first, field)
	}
	save(t, "composer-quotes-scroll-light.png", scrollTT)

	longQuote := "cancel that context when the window closes. The handler should abort the dial, the handshake, and a body read that has already started, then treat context.Canceled as a quiet exit."
	sentQuotes := []Quote{
		{Text: longQuote, Src: "Cove", Time: when, SourceMessageID: "a1"},
		{Text: "ctx, cancel := context.WithCancel(context.Background())\nwin.OnClose(cancel)", Src: "Cove", Time: when, Code: true, Lang: "go", SourceMessageID: "a1"},
	}
	sent := Conversation{
		ID: "c1", Title: "Cancel HTTP on close", Loaded: true, Config: DefaultConfig(),
		Messages: []Message{
			user("u1", "How do I cancel an in-flight HTTP request when the window closes?", you, nil),
			asst("a1", "Tie the request to a context\n\n"+body, when),
			user("u2", "Does this also stop the body read if headers already arrived?", when.Add(time.Minute), sentQuotes),
			asst("a2", "Yes — cancel the context in the window's close handler; the in-flight Do call returns context.Canceled right away.", when.Add(2*time.Minute)),
		},
	}
	_, tt = open(t, 1120, 760, false, sent)
	save(t, "sent-bubble-quote.png", tt)
	tt.SetDark(true)
	tt.Frame()
	save(t, "sent-bubble-quote-dark.png", tt)
	tt.SetDark(false)
	tt.Frame()
	label := quoteAccessLabel(sentQuotes[0])
	before, ok := tt.Find(label)
	if !ok {
		t.Fatalf("sent quote missing, texts %q", tt.Texts())
	}
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	after, ok := tt.Find(label)
	if !ok || after.H < before.H+12 {
		t.Fatalf("expand did not grow the card: before %+v after %+v ok %v", before, after, ok)
	}
	if !tt.HasText("quiet exit.") {
		t.Fatal("expanded card does not show the rest of the quote")
	}
	save(t, "sent-bubble-quote-expanded.png", tt)

	_, tt = open(t, 720, 520, false, quoted)
	save(t, "width720.png", tt)
	tt.SetDark(true)
	tt.Frame()
	save(t, "width720-dark.png", tt)

	// At 720 the bar keeps Add to Chat and drops the shortcut hint.
	narrow := conv
	narrow.Messages[1].Content = "Tie the request to a context\n\n" + body
	na, nt := open(t, 720, 520, false, narrow)
	selectPhrase(t, na, nt, "See the docs")
	if !na.quoteBarOpen {
		t.Fatal("bar missing at 720")
	}
	if nt.HasText("⌘L") {
		t.Fatal("720 bar still shows the ⌘L hint")
	}
	save(t, "selection-bar-720.png", nt)
}

func TestQuoteBarHintMeasure(t *testing.T) {
	dir := "/cursor/stores/self/artifacts/quote"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		a := newTestApp(t)
		if dark {
			a.data.Appearance = appearanceDark
		} else {
			a.data.Appearance = appearanceLight
		}
		a.data.Conversations = []Conversation{{
			ID: "c1", Title: "Notes", Loaded: true, Config: DefaultConfig(),
			Messages: []Message{{ID: "u1", Role: roleUser, Content: "Measure the shortcut hint on the selection bar.", Done: true}},
		}}
		a.data.ActiveID = "c1"
		tt := ui.NewTester(a.view, 1100, 760)
		tt.SetScale(4)
		tt.SetDark(dark)
		tt.Frame()
		dragRects(tt, resolvePhrase(t, a, tt, "shortcut hint"))
		if !a.quoteBarOpen {
			t.Fatal("bar closed")
		}
		img := copyRGBA(tt.Image())
		lines = append(lines, measureQuoteBar(t, tt, img, mode)...)
		bar := mustFind(t, tt, "Selection")
		crop := dipRect(bar.X-8, bar.Y-8, bar.W+16, bar.H+16, 4, img.Bounds())
		writePNG(t, filepath.Join(dir, "selection-bar-"+mode+"-after.png"), cropImage(img, crop))
	}
	body := "bar\n" + strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "measurements.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, line := range lines {
		if strings.Contains(line, "FAIL") {
			t.Errorf("%s", line)
		}
	}
}

func measureQuoteBar(t *testing.T, tt *ui.Tester, img *image.RGBA, mode string) []string {
	t.Helper()
	label := mustFind(t, tt, "Add to Chat")
	pair := mustFind(t, tt, "⌘L")
	icon := mustFind(t, tt, "bar quote")
	btn := mustFind(t, tt, "Add selection to chat")
	bar := mustFind(t, tt, "Selection")
	bg := sampleBG(img, ui.Rect{X: bar.X + 2, Y: bar.Y + 2, W: 6, H: 6}, 4)
	center := bar.Y + bar.H/2
	spans := inkClusters(img, pair, bg, 4)
	if len(spans) != 2 {
		return []string{fmt.Sprintf("bar %s ⌘L clusters=%d want=2 FAIL", mode, len(spans))}
	}
	cmdY := float32(spans[0].top+spans[0].bot+1) / 2 / 4
	lY := float32(spans[1].top+spans[1].bot+1) / 2 / 4
	gapCmdL := looseGap(img, spans[0], spans[1], bg, 4)
	labelInk := inkBox(t, img, label, bg, 4)
	iconInk := inkBox(t, img, icon, bg, 4)
	// Pads and the 8pt gap use the same faint fringe the spacer was built from.
	cmdLeft := float32(expandInk(img, spans[0].left, spans[0].left-16, spans[0].top, spans[0].bot+1, bg, -1)) / 4
	lRight := float32(expandInk(img, spans[1].right, spans[1].right+16, spans[1].top, spans[1].bot+1, bg, 1)+1) / 4
	labelRight := float32(expandInk(img, int(labelInk.right*4)-1, int(labelInk.right*4)+16, int(label.Y*4), int((label.Y+label.H)*4), bg, 1)+1) / 4
	iconLeft := float32(expandInk(img, int(iconInk.left*4), int(iconInk.left*4)-16, int(icon.Y*4), int((icon.Y+icon.H)*4), bg, -1)) / 4
	leftPad := iconLeft - btn.X
	rightPad := btn.X + btn.W - lRight
	gapLabel := cmdLeft - labelRight
	lines := []string{
		fmt.Sprintf("bar %s ⌘ center=%.3f control=%.3f offset=%+.3f %s", mode, cmdY, center, cmdY-center, passQuarter(cmdY-center)),
		fmt.Sprintf("bar %s L center=%.3f control=%.3f offset=%+.3f %s", mode, lY, center, lY-center, passQuarter(lY-center)),
		fmt.Sprintf("bar %s quote center=%.3f control=%.3f offset=%+.3f %s", mode, iconInk.midY, center, iconInk.midY-center, passQuarter(iconInk.midY-center)),
		fmt.Sprintf("bar %s ⌘ to L gap=%.3f want=1 offset=%+.3f %s", mode, gapCmdL, gapCmdL-1, passQuarter(gapCmdL-1)),
		fmt.Sprintf("bar %s Add to Chat to ⌘ gap=%.3f want=8 offset=%+.3f %s", mode, gapLabel, gapLabel-8, passQuarter(gapLabel-8)),
		fmt.Sprintf("bar %s button left pad gap=%.3f want=6 offset=%+.3f %s", mode, leftPad, leftPad-6, passQuarter(leftPad-6)),
		fmt.Sprintf("bar %s button right pad gap=%.3f want=6 offset=%+.3f %s", mode, rightPad, rightPad-6, passQuarter(rightPad-6)),
	}
	return lines
}

type dipInk struct {
	left, right, midY float32
}

func inkBox(t *testing.T, img *image.RGBA, r ui.Rect, bg color.RGBA, scale int) dipInk {
	t.Helper()
	left, right, ok := inkBoundsX(img, r, bg, scale)
	top, bot, yok := inkBounds(img, r, bg, scale)
	if !ok || !yok {
		t.Fatalf("no ink in %+v", r)
	}
	s := float32(scale)
	return dipInk{
		left:  float32(left) / s,
		right: float32(right+1) / s,
		midY:  float32(top+bot+1) / 2 / s,
	}
}

func passQuarter(off float32) string {
	if off < -0.25 || off > 0.25 {
		return "FAIL"
	}
	return "PASS"
}
