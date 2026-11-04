package main

import (
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"cove/themes"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown/parser"
)

func tableLook() Theme {
	return Theme{Colors: themes.TideLight()}
}

func tableDoc(src string) *parser.Document {
	return parser.Parse(src)
}

func cellInlines(t *testing.T, src, contains string) []parser.Inline {
	t.Helper()
	d := tableDoc(src)
	for i := 1; i < d.Len(); i++ {
		if d.Block(i).Kind != parser.TableCell {
			continue
		}
		in := d.Inlines(i)
		if strings.Contains(parser.PlainText(in), contains) {
			return in
		}
	}
	t.Fatalf("no table cell containing %q in %q", contains, src)
	return nil
}

func openTable(t *testing.T, body string) (*App, *ui.Tester) {
	t.Helper()
	a := tableShotApp(t, body)
	tt := ui.NewTester(a.view, shotW, shotH)
	tt.Frame()
	return a, tt
}

func TestCellPiecesLinkStrikeImage(t *testing.T) {
	look := tableLook()
	src := strings.Join([]string{
		"| h |",
		"| --- |",
		"| See [docs](https://pkg.go.dev/context) |",
		"| https://example.com |",
		"| *italic* ~~strike~~ |",
		"| ![diagram](pic.png) |",
		"| **bold** |",
		"| `cancel()` |",
		"| Yes |",
		"| Immediately |",
		"| a \\| b |",
		"| `a\\|b` |",
		"| 中文 |",
		"| 🎉 done |",
	}, "\n")

	link := cellPieces(cellInlines(t, src, "docs"), 0, 100, 400, look, 13, 13)
	if spanText(flattenCellPieces(link)) != "See docs" {
		t.Fatalf("link cell text %q", spanText(flattenCellPieces(link)))
	}
	var dest string
	for _, pc := range link {
		if pc.dest != "" {
			dest = pc.dest
			if len(pc.spans) == 0 || pc.spans[0].Color != look.TextLink {
				t.Fatalf("link span color %+v", pc.spans)
			}
		}
	}
	if dest != "https://pkg.go.dev/context" {
		t.Fatalf("link dest %q", dest)
	}

	auto := cellPieces(cellInlines(t, src, "example.com"), 0, 100, 400, look, 13, 13)
	if spanText(flattenCellPieces(auto)) != "https://example.com" {
		t.Fatalf("autolink text %q", spanText(flattenCellPieces(auto)))
	}
	if len(auto) == 0 || auto[0].dest == "" || !auto[0].auto {
		t.Fatalf("autolink pieces %+v", auto)
	}

	mix := cellPieces(cellInlines(t, src, "italic"), 0, 100, 400, look, 13, 13)
	got := flattenCellPieces(mix)
	if spanText(got) != "italic strike" {
		t.Fatalf("italic+strike text %q", spanText(got))
	}
	var italic, strike bool
	for _, sp := range got {
		if sp.Italic && strings.Contains(sp.Text, "italic") {
			italic = true
		}
		if sp.Strikethrough && strings.Contains(sp.Text, "strike") {
			strike = true
		}
	}
	if !italic || !strike {
		t.Fatalf("italic/strike flags %+v", got)
	}

	img := cellPieces(cellInlines(t, src, "diagram"), 0, 100, 400, look, 13, 13)
	if spanText(flattenCellPieces(img)) != "diagram" {
		t.Fatalf("image alt %q", spanText(flattenCellPieces(img)))
	}
	if len(img) == 0 || img[0].dest != "pic.png" {
		t.Fatalf("image dest %+v", img)
	}

	strong := flattenCellPieces(cellPieces(cellInlines(t, src, "bold"), 0, 100, 400, look, 13, 13))
	if len(strong) == 0 || strong[0].Weight != 600 || strong[0].Text != "bold" {
		t.Fatalf("bold spans %+v", strong)
	}

	code := flattenCellPieces(cellPieces(cellInlines(t, src, "cancel()"), 0, 100, 400, look, 13, 13))
	found := false
	for _, sp := range code {
		if sp.Text == "cancel()" && sp.Font == "ui-monospace" && sp.Background == look.CodeInlineBg {
			found = true
		}
	}
	if !found {
		t.Fatalf("inline code spans %+v", code)
	}

	for _, word := range []string{"Yes", "Immediately"} {
		pieces := cellPieces(cellInlines(t, src, word), 0, 100, 400, look, 13, 13)
		if spanText(flattenCellPieces(pieces)) != word {
			t.Fatalf("%s cell %q", word, spanText(flattenCellPieces(pieces)))
		}
	}

	pipe := flattenCellPieces(cellPieces(cellInlines(t, src, "a | b"), 0, 100, 400, look, 13, 13))
	if !strings.Contains(spanText(pipe), "a | b") && !strings.Contains(spanText(pipe), "a|b") {
		t.Fatalf("escaped pipe %q", spanText(pipe))
	}

	zh := spanText(flattenCellPieces(cellPieces(cellInlines(t, src, "中文"), 0, 100, 400, look, 13, 13)))
	if zh != "中文" {
		t.Fatalf("chinese %q", zh)
	}
	emo := spanText(flattenCellPieces(cellPieces(cellInlines(t, src, "done"), 0, 100, 400, look, 13, 13)))
	if !strings.Contains(emo, "🎉") || !strings.Contains(emo, "done") {
		t.Fatalf("emoji %q", emo)
	}
}

func TestTableCellLinkClickAndStyle(t *testing.T) {
	body := strings.Join([]string{
		"Paragraph [docs](https://pkg.go.dev/context) here.",
		"",
		"| Kind | Sample |",
		"| --- | --- |",
		"| Link | See [cell-link](https://example.com/from-cell) |",
		"| Autolink | https://example.org/auto |",
	}, "\n")
	a, tt := openTable(t, body)
	if !tt.HasText("cell-link") {
		t.Fatalf("link cell blank: %q", tt.Texts())
	}
	if !tt.HasText("https://example.org/auto") {
		t.Fatalf("autolink cell blank: %q", tt.Texts())
	}
	link, ok := tt.Find("cell-link")
	if !ok {
		t.Fatalf("missing cell-link in %q", tt.Texts())
	}
	para, ok := tt.Find("docs")
	if !ok {
		t.Fatalf("missing paragraph link in %q", tt.Texts())
	}
	img := tt.Image()
	want := color.RGBA{R: a.look.TextLink.R, G: a.look.TextLink.G, B: a.look.TextLink.B, A: 255}
	cellD := closestColorDist(img, link, want)
	paraD := closestColorDist(img, para, want)
	if paraD > 36 {
		t.Fatalf("paragraph link is not TextLink (Δ %d, want #%02x%02x%02x)", paraD, want.R, want.G, want.B)
	}
	if cellD > 36 {
		t.Fatalf("table link is not TextLink (Δ %d, want #%02x%02x%02x) rect %v", cellD, want.R, want.G, want.B, link)
	}

	tt.Move(link.X+link.W/2, link.Y+link.H/2)
	if tt.Cursor() != ui.CursorPointer {
		t.Fatalf("link hover cursor %v", tt.Cursor())
	}
	if err := tt.Click("cell-link"); err != nil {
		t.Fatal(err)
	}
	opened := tt.OpenedURLs()
	if len(opened) == 0 || opened[len(opened)-1] != "https://example.com/from-cell" {
		t.Fatalf("opened %q", opened)
	}

	if err := tt.Click("https://example.org/auto"); err != nil {
		t.Fatal(err)
	}
	opened = tt.OpenedURLs()
	if len(opened) == 0 || opened[len(opened)-1] != "https://example.org/auto" {
		t.Fatalf("autolink opened %q", opened)
	}
}

func TestTableCellInlineRendering(t *testing.T) {
	a, tt := openTable(t, tableInlineMarkdown())
	mustFind(t, tt, "table scroller")
	for _, want := range []string{
		"cancel()", "docs", "https://example.com", "bold", "italic", "strike",
		"中文", "🎉 done", "Yes",
	} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %q", want, tt.Texts())
		}
	}
	if !tt.HasText("a|b") && !tt.HasText("a | b") && !tt.HasText("a\\|b") {
		t.Fatalf("escaped pipe missing: %q", tt.Texts())
	}
	joined := strings.Join(tt.Texts(), "\n")
	if strings.Contains(joined, "ita…") || strings.Contains(joined, "ita...") {
		t.Fatalf("italic+strike ellipsized: %q", tt.Texts())
	}
	mix, ok := tt.Find("italic strike")
	if !ok {
		mix, ok = tt.Find("italic")
		if !ok || !tt.HasText("strike") {
			t.Fatalf("italic+strike not fully shown: %q", tt.Texts())
		}
	}
	if mix.W < 50 {
		t.Fatalf("italic+strike ellipsized to width %.1f (%q)", mix.W, tt.Texts())
	}

	yes := mustFind(t, tt, "Yes")
	if hasSuccessIcon(tt.Image(), yes, a.look.StatusSuccess) {
		t.Fatal("Yes cell still paints a check icon")
	}

	altBody := strings.Join([]string{
		"| Kind | Sample |",
		"| --- | --- |",
		"| Image | ![diagram](pic.png) |",
		"| Immediately | Immediately |",
	}, "\n")
	b, tb := openTable(t, altBody)
	if !tb.HasText("diagram") {
		t.Fatalf("image alt missing: %q", tb.Texts())
	}
	if err := tb.Click("diagram"); err != nil {
		t.Fatal(err)
	}
	if got := tb.OpenedURLs(); len(got) == 0 || got[len(got)-1] != "pic.png" {
		t.Fatalf("image opened %q", got)
	}
	imm := mustFind(t, tb, "Immediately")
	if hasSuccessIcon(tb.Image(), imm, b.look.StatusSuccess) {
		t.Fatal("Immediately cell still paints a check icon")
	}
}

func TestTableCellBoldWeight(t *testing.T) {
	body := strings.Join([]string{
		"| Kind | Sample |",
		"| --- | --- |",
		"| Bold | **wwww** |",
		"| Plain | wwww |",
	}, "\n")
	_, tt := openTable(t, body)
	look := tableLook()
	strong := flattenCellPieces(cellPieces(cellInlines(t, body, "wwww"), 0, 100, 400, look, 13, 13))
	if len(strong) == 0 || strong[0].Weight != 600 || strong[0].Text != "wwww" {
		t.Fatalf("cell bold weight %+v", strong)
	}
	plainW, _ := ui.ShapeText("wwww", ui.Font{Size: 13, Weight: 400}, 0).Size()
	boldW, _ := ui.ShapeText("wwww", ui.Font{Size: 13, Weight: 600}, 0).Size()
	if boldW <= plainW {
		t.Skipf("this font does not distinguish 400 and 600 (%.1f vs %.1f)", plainW, boldW)
	}
	found := tt.FindAll("wwww")
	if len(found) < 2 {
		t.Fatalf("wwww occurrences %v in %q", found, tt.Texts())
	}
	if found[0].W+0.5 < found[1].W {
		t.Fatalf("cell bold width %.1f is not heavier than plain %.1f", found[0].W, found[1].W)
	}
}

func TestTableCellOnlyLinkVisible(t *testing.T) {
	body := "| h |\n| --- |\n| [only-link](https://example.com/only) |\n"
	_, tt := openTable(t, body)
	if !tt.HasText("only-link") {
		t.Fatalf("link-only cell empty: %q", tt.Texts())
	}
	if err := tt.Click("only-link"); err != nil {
		t.Fatal(err)
	}
	if got := tt.OpenedURLs(); len(got) == 0 || got[len(got)-1] != "https://example.com/only" {
		t.Fatalf("opened %q", got)
	}
}

func TestTableEscapedPipeChineseEmojiCode(t *testing.T) {
	body := strings.Join([]string{
		"| Kind | Sample |",
		"| --- | --- |",
		"| Pipe | a \\| b |",
		"| Code pipe | `x\\|y` |",
		"| Chinese | 中文测试 |",
		"| Emoji | 🎉🚀 |",
		"| Code | `cancel()` |",
	}, "\n")
	_, tt := openTable(t, body)
	joined := strings.Join(tt.Texts(), " ")
	if !strings.Contains(joined, "a | b") && !strings.Contains(joined, "a|b") {
		t.Fatalf("escaped pipe missing: %q", tt.Texts())
	}
	if !tt.HasText("中文测试") {
		t.Fatalf("chinese missing: %q", tt.Texts())
	}
	if !tt.HasText("🎉🚀") {
		t.Fatalf("emoji missing: %q", tt.Texts())
	}
	if !tt.HasText("cancel()") {
		t.Fatalf("inline code missing: %q", tt.Texts())
	}
}

func TestTableStreamHoldsHeader(t *testing.T) {
	a := tableShotApp(t, "")
	a.data.Conversations[0].Messages[1] = Message{
		ID: "a", Role: roleAssistant, Content: "| Approach | Stops the dial |\n", Done: false,
		CreatedAt: time.Date(2026, 11, 4, 9, 41, 0, 0, time.UTC),
	}
	a.streams = map[string]*convStream{"t": {gen: 1, cancel: func() {}}}
	tt := ui.NewTester(a.view, shotW, shotH)
	tt.Frame()
	joined := strings.Join(tt.Texts(), "\n")
	if strings.Contains(joined, "| Approach") || strings.Contains(joined, "| ---") {
		t.Fatalf("raw pipes flashed: %q", tt.Texts())
	}
	a.data.Conversations[0].Messages[1].Content = "| Approach | Stops the dial |\n| --- | --- |\n| Context cancel | Immediately |\n"
	tt.Frame()
	if !tt.HasText("Approach") || !tt.HasText("Immediately") {
		t.Fatalf("finished table missing: %q", tt.Texts())
	}
	if strings.Contains(strings.Join(tt.Texts(), "\n"), "| Approach") {
		t.Fatalf("finished table still shows raw pipes: %q", tt.Texts())
	}
}

func TestTableNestedAndRagged(t *testing.T) {
	body := strings.Join([]string{
		"> quote before",
		">",
		"> | Q | R |",
		"> | --- | --- |",
		"> | quoted | cell |",
		"",
		"- list before",
		"",
		"  | A | B | C |",
		"  | --- | --- | --- |",
		"  | 1 | 2 |",
		"  | 3 |",
	}, "\n")
	_, tt := openTable(t, body)
	if !tt.HasText("quoted") || !tt.HasText("cell") {
		t.Fatalf("blockquote table missing: %q", tt.Texts())
	}
	if !tt.HasText("1") || !tt.HasText("2") || !tt.HasText("3") {
		t.Fatalf("list/ragged table missing: %q", tt.Texts())
	}
}

func TestTableWrapCapAndRowHeight(t *testing.T) {
	sentence := "How many times a failed delivery is retried before the event is parked in the dead-letter queue. Retries use exponential backoff with full jitter, capped at 30 s between attempts."
	body := "| Setting | What it does |\n| --- | --- |\n| MaxRetries | " + sentence + " |\n"
	_, tt := openTable(t, body)
	table := mustFind(t, tt, "table scroller")
	if table.H <= tableMinRowH+1+tableMinRowH {
		t.Fatalf("wrapped row did not grow: table %v", table)
	}
	joined := strings.Join(tt.Texts(), "\n")
	if strings.Contains(joined, "…") || strings.Contains(joined, "...") {
		t.Fatalf("wrapped cell ellipsized: %q", tt.Texts())
	}
	for _, word := range []string{"How many times", "dead-letter queue", "30 s between attempts"} {
		if !tt.HasText(word) && !strings.Contains(joined, word) {
			t.Fatalf("wrapped cell missing %q in %q", word, tt.Texts())
		}
	}

	long := strings.Repeat("m", 240)
	capBody := "| h |\n| --- |\n| " + long + " |\n"
	_, cap := openTable(t, capBody)
	col := mustFind(t, cap, "table scroller")
	if col.W > tableMaxCol+1 {
		t.Fatalf("column exceeded 320 including padding: %.1f", col.W)
	}
	if col.W < tableMaxCol-8 {
		t.Fatalf("long cell did not fill the 320 cap: %.1f", col.W)
	}
	if col.H <= tableMinRowH+1+tableMinRowH {
		t.Fatalf("long token did not wrap: table %v", col)
	}
	if strings.Contains(strings.Join(cap.Texts(), "\n"), "…") {
		t.Fatalf("capped cell ellipsized: %q", cap.Texts())
	}
}

func TestTableLongTokenBreaks(t *testing.T) {
	url := "https://pkg.go.dev/github.com/acme/webhook/v2@v2.4.1/retry#ExponentialBackoffWithJitter"
	hash := "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	body := strings.Join([]string{
		"| Artifact | Source / checksum |",
		"| --- | --- |",
		"| webhook v2.4.1 | " + url + " |",
		"| webhook-darwin-arm64.tar.gz | " + hash + " |",
	}, "\n")
	_, tt := openTable(t, body)
	table := mustFind(t, tt, "table scroller")
	if table.W > tableMaxCol*2+2 {
		t.Fatalf("URL/hash columns exceeded 320 each: table %.1f", table.W)
	}
	if !tt.HasText("webhook v2.4.1") || !tt.HasText("pkg.go.dev") {
		t.Fatalf("URL cell truncated: %q", tt.Texts())
	}
	if !strings.Contains(strings.Join(tt.Texts(), "\n"), "ExponentialBackoffWithJitter") {
		t.Fatalf("URL tail missing: %q", tt.Texts())
	}
	if !strings.Contains(strings.Join(tt.Texts(), "\n"), "9f86d081884c7d659a2feaa0c55ad015a3") {
		t.Fatalf("hash missing: %q", tt.Texts())
	}
	if table.H <= tableMinRowH*2+1 {
		t.Fatalf("long URL/hash did not wrap: table %v", table)
	}
}

func TestTableSingleLineRowIs30(t *testing.T) {
	body := "| A | B |\n| --- | --- |\n| 1 | 2 |\n"
	_, tt := openTable(t, body)
	table := mustFind(t, tt, "table scroller")
	want := float32(tableMinRowH + 1 + tableMinRowH)
	if table.H < want-1 || table.H > want+1 {
		t.Fatalf("single-line table height %.1f, want %.1f (5+20+5 per row)", table.H, want)
	}
}

func TestTableHugsContentWidth(t *testing.T) {
	body := "| A | B |\n| --- | --- |\n| 1 | 2 |\n"
	_, tt := openTable(t, body)
	table := mustFind(t, tt, "table scroller")
	card := mustFind(t, tt, "content card")
	if table.W > 280 {
		t.Fatalf("narrow table stretched to %.1f (card %.1f)", table.W, card.W)
	}
	if table.W < 40 {
		t.Fatalf("narrow table too small: %.1f", table.W)
	}
}

func TestTableAlignment(t *testing.T) {
	_, tt := openTable(t, tableAlignMarkdown())
	status := mustFind(t, tt, "Status")
	passes := tt.FindAll("Pass")
	if len(passes) == 0 {
		t.Fatalf("missing Pass cells in %q", tt.Texts())
	}
	statusMid := status.X + status.W/2
	passMid := passes[0].X + passes[0].W/2
	if abs32(statusMid-passMid) > 10 {
		t.Fatalf("Status not centered with Pass: header %v pass %v", status, passes[0])
	}
	lat := mustFind(t, tt, "p95 latency")
	ms := mustFind(t, tt, "182 ms")
	if abs32((lat.X+lat.W)-(ms.X+ms.W)) > 10 {
		t.Fatalf("p95 latency not right-aligned with 182 ms: header %v cell %v", lat, ms)
	}
	req := mustFind(t, tt, "Requests")
	n := mustFind(t, tt, "1,204,331")
	if abs32((req.X+req.W)-(n.X+n.W)) > 10 {
		t.Fatalf("Requests not right-aligned: header %v cell %v", req, n)
	}
	errH := mustFind(t, tt, "Error rate")
	rate := mustFind(t, tt, "0.02%")
	if abs32(errH.X-rate.X) > 10 {
		t.Fatalf("Error rate not left-aligned: header %v cell %v", errH, rate)
	}
	region := mustFind(t, tt, "Region")
	east := mustFind(t, tt, "us-east-1")
	if abs32(region.X-east.X) > 10 {
		t.Fatalf("Region not left-aligned: header %v cell %v", region, east)
	}
}

func TestTableOverflowScrollsWithFades(t *testing.T) {
	a := tableShotApp(t, tableWideMarkdown())
	tt := openHScroll(t, a, 720, 520, false)
	table := mustFind(t, tt, "table scroller")
	if table.W > 520 {
		t.Fatalf("overflowing table was not clipped to the message: %.1f", table.W)
	}
	if !tt.HasText("Column 1 heading") {
		t.Fatalf("first column missing: %q", tt.Texts())
	}
	var slot *hScrollSlot
	for _, s := range a.hScrolls {
		if s.st.MaxX > 1 {
			slot = s
			break
		}
	}
	if slot == nil || slot.st.MaxX <= 1 {
		t.Fatalf("wide table did not overflow: scrolls %+v table %v", a.hScrolls, table)
	}
	if slot.st.X > 1 {
		t.Fatalf("wide table started scrolled: %.1f", slot.st.X)
	}
	assertTableEdge(t, tt, "wide right", "Column 1 heading", true)
	scrollFarRight(tt, table.X+24, table.Y+20)
	table = mustFind(t, tt, "table scroller")
	if !tt.HasText("Column 10 heading") {
		t.Fatalf("last column missing after scroll: %q", tt.Texts())
	}
	head := mustFind(t, tt, "Column 10 heading")
	if head.X+head.W > table.X+table.W+2 {
		t.Fatalf("last column still clipped after scroll: head %v table %v", head, table)
	}
	var scrolled *hScrollSlot
	for _, s := range a.hScrolls {
		if s.st.X > 1 {
			scrolled = s
			break
		}
	}
	if scrolled == nil {
		t.Fatalf("wide table did not scroll: %+v", a.hScrolls)
	}
	placeHScroll(t, a, tt, "middle")
	assertTableEdge(t, tt, "wide left", "Column 1 heading", false)
}

func closestColorDist(img *image.RGBA, r ui.Rect, want color.RGBA) int {
	best := 1 << 20
	x0, y0 := int(r.X), int(r.Y)
	x1, y1 := int(r.X+r.W), int(r.Y+r.H)
	if x1 <= x0 {
		x1 = x0 + 1
	}
	if y1 <= y0 {
		y1 = y0 + 1
	}
	b := img.Bounds()
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if x < b.Min.X || y < b.Min.Y || x >= b.Max.X || y >= b.Max.Y {
				continue
			}
			if d := tableColorDist(img.RGBAAt(x, y), want); d < best {
				best = d
			}
		}
	}
	return best
}

func tableColorDist(a, b color.RGBA) int {
	dr := int(a.R) - int(b.R)
	dg := int(a.G) - int(b.G)
	db := int(a.B) - int(b.B)
	if dr < 0 {
		dr = -dr
	}
	if dg < 0 {
		dg = -dg
	}
	if db < 0 {
		db = -db
	}
	return dr + dg + db
}

func hasSuccessIcon(img *image.RGBA, r ui.Rect, success ui.Color) bool {
	want := color.RGBA{R: success.R, G: success.G, B: success.B, A: 255}
	x0 := int(r.X) - 22
	if x0 < 0 {
		x0 = 0
	}
	x1 := int(r.X) + 4
	y0 := int(r.Y) - 2
	if y0 < 0 {
		y0 = 0
	}
	y1 := int(r.Y + r.H + 2)
	b := img.Bounds()
	hits := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if x < b.Min.X || y < b.Min.Y || x >= b.Max.X || y >= b.Max.Y {
				continue
			}
			if tableColorDist(img.RGBAAt(x, y), want) < 28 {
				hits++
			}
		}
	}
	return hits >= 8
}
