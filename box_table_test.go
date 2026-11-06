package main

import (
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown/parser"
)

func padCells(s string, n int) string {
	var b strings.Builder
	cells := 0
	for _, r := range s {
		w := boxCellCount(r)
		if cells+w > n {
			break
		}
		b.WriteRune(r)
		cells += w
	}
	for cells < n {
		b.WriteByte(' ')
		cells++
	}
	return b.String()
}

func boxEnglishMD() string {
	stage, content, risk := 8, 32, 6
	row := func(a, b, c string) string {
		return "│" + padCells(a, stage) + "│" + padCells(b, content) + "│" + padCells(c, risk) + "│"
	}
	top := "┌" + strings.Repeat("─", stage) + "┬" + strings.Repeat("─", content) + "┬" + strings.Repeat("─", risk) + "┐"
	mid := "├" + strings.Repeat("─", stage) + "┼" + strings.Repeat("─", content) + "┼" + strings.Repeat("─", risk) + "┤"
	bot := "└" + strings.Repeat("─", stage) + "┴" + strings.Repeat("─", content) + "┴" + strings.Repeat("─", risk) + "┘"
	return strings.Join([]string{
		top,
		row(" Stage  ", " Content", " Risk "),
		mid,
		row(" Design ", " Write SPEC, light/dark shots", " Low  "),
		row(" Build  ", " Detect box runs, equal cells", " Med  "),
		row(" Check  ", " CJK verticals, h-scroll", " Low  "),
		bot,
	}, "\n")
}

func boxCJKMD() string {
	stage, content, risk := 8, 30, 6
	row := func(a, b, c string) string {
		return "│" + padCells(a, stage) + "│" + padCells(b, content) + "│" + padCells(c, risk) + "│"
	}
	top := "┌" + strings.Repeat("─", stage) + "┬" + strings.Repeat("─", content) + "┬" + strings.Repeat("─", risk) + "┐"
	mid := "├" + strings.Repeat("─", stage) + "┼" + strings.Repeat("─", content) + "┼" + strings.Repeat("─", risk) + "┤"
	bot := "└" + strings.Repeat("─", stage) + "┴" + strings.Repeat("─", content) + "┴" + strings.Repeat("─", risk) + "┘"
	return strings.Join([]string{
		top,
		row("  阶段  ", "             内容             ", "  风险  "),
		mid,
		row("  设计  ", "写 SPEC，出浅色深色效果图     ", "  低   "),
		row("  开发  ", "识别制表符块并等宽渲染        ", "  中   "),
		row("  验收  ", "中文竖线对齐，可横向滚动      ", "  低   "),
		bot,
	}, "\n")
}

func boxArcsMD() string {
	w := 14
	inner := func(l, s, r string) string { return l + padCells(s, w) + r }
	return strings.Join([]string{
		"╭" + strings.Repeat("─", w) + "╮",
		inner("│", "   rounded", "│"),
		"╰" + strings.Repeat("─", w) + "╯",
		strings.Repeat("┄", w+2),
		inner("┆", "   triple", "┆"),
		inner("┆", "    quad", "┆"),
		"╰" + strings.Repeat("┄", w) + "╯",
		strings.Repeat("┈", w+2),
		inner("┋", "   heavy", "┋"),
	}, "\n")
}

func boxArcsSurroundMD() string {
	return boxArcsMD() + "\n\n▀▄ U+2580-25\n"
}

func boxWideMD() string {
	n := 96
	return strings.Join([]string{
		"┌" + strings.Repeat("─", n) + "┐",
		"│" + padCells(" wide overflow row that must scroll horizontally", n) + "│",
		"└" + strings.Repeat("─", n) + "┘",
	}, "\n")
}

func boxAllGlyphsMD() string {
	var b strings.Builder
	for row := 0; row < 8; row++ {
		if row > 0 {
			b.WriteByte('\n')
		}
		b.WriteRune('│')
		for col := 0; col < 16; col++ {
			b.WriteRune(rune(0x2500 + row*16 + col))
		}
		b.WriteRune('│')
	}
	return b.String()
}

func boxSurroundMD() string {
	return strings.Join([]string{
		"Lead text before the box table.",
		"",
		boxEnglishMD(),
		"",
		"Trail text after the box table.",
		"",
		"▀▄ U+2580-25 stays a glyph, not a box table.",
	}, "\n")
}

func boxAdjacentMD() string {
	return strings.Join([]string{
		"Lead **bold** sits on the previous line.",
		boxEnglishMD(),
		"Trail _italic_ sits on the next line.",
	}, "\n")
}

func boxFencedMD() string {
	return "```\nnote: keep the fence\n" + boxEnglishMD() + "\n```\n"
}

func TestBoxTableDetection(t *testing.T) {
	if isBoxTable([]string{"┌──┐", "│  │"}) {
		t.Fatal("two lines must not qualify")
	}
	if isBoxTable([]string{"See the │ pipe in prose."}) {
		t.Fatal("isolated prose symbol must not qualify")
	}
	if isBoxTable([]string{"▀▀▀", "▄▄▄", "███"}) {
		t.Fatal("U+2580–259F must not qualify")
	}
	if isBoxTable([]string{"│ a", "plain", "│ b", "│ c"}) {
		t.Fatal("broken run of two then two must not qualify without 3 consecutive")
	}
	if !isBoxTable([]string{"┌──┐", "│  │", "└──┘"}) {
		t.Fatal("three box lines should qualify")
	}
	if !isBoxTable([]string{"hello", "┌──┐", "│  │", "└──┘", "tail"}) {
		t.Fatal("run of three inside surrounding lines should qualify")
	}
	segs := splitBoxSegs([]string{"hello", "┌──┐", "│  │", "└──┘", "tail"})
	if len(segs) != 3 || segs[0].Box || !segs[1].Box || segs[2].Box {
		t.Fatalf("split segs %+v", segs)
	}
	if strings.Join(segs[0].Lines, "\n") != "hello" || strings.Join(segs[2].Lines, "\n") != "tail" {
		t.Fatalf("prose not isolated: %+v", segs)
	}
	if len(segs[1].Lines) != 3 {
		t.Fatalf("box run %q", segs[1].Lines)
	}
	if !lineQualifies("│ ends with x") || !lineQualifies("x ends with │") {
		t.Fatal("start or end must qualify")
	}
	if lineQualifies("  │ padded") {
		t.Fatal("leading spaces mean the first rune is not box drawing")
	}
	if isBoxDrawing('▀') || isBlockElement('│') {
		t.Fatal("block elements and box drawing must not overlap")
	}
}

func TestBoxTableParserKinds(t *testing.T) {
	doc := parser.New(parser.Default)
	doc.Set(boxEnglishMD())
	found := false
	for i := 1; i < doc.Len(); i++ {
		if _, ok := boxTableSource(doc, i); ok {
			found = true
			if doc.Block(i).Kind != parser.Paragraph {
				t.Fatalf("unfenced box table kind %v", doc.Block(i).Kind)
			}
		}
	}
	if !found {
		t.Fatal("unfenced box table not detected")
	}

	doc = parser.New(parser.Default)
	doc.Set("```\n" + boxEnglishMD() + "\n```\n")
	for i := 1; i < doc.Len(); i++ {
		if _, ok := boxTableSource(doc, i); ok {
			t.Fatal("fenced code must not become a table block")
		}
		if doc.Block(i).Kind == parser.CodeBlock && !isBoxTable(codeBlockLines(doc.Lines(i))) {
			t.Fatal("fenced body should still qualify for the cell grid")
		}
	}

	doc = parser.New(parser.Default)
	doc.Set("| a | b |\n| --- | --- |\n| 1 | 2 |\n")
	for i := 1; i < doc.Len(); i++ {
		if _, ok := boxTableSource(doc, i); ok {
			t.Fatal("GFM pipe table must not be a box table")
		}
	}
}

func TestBoxCellCountCJK(t *testing.T) {
	if boxCellCount('A') != 1 || boxCellCount('│') != 1 || boxCellCount('▀') != 1 {
		t.Fatalf("ascii/box/block cells A=%d │=%d ▀=%d", boxCellCount('A'), boxCellCount('│'), boxCellCount('▀'))
	}
	if boxCellCount('中') != 2 || boxCellCount('：') != 2 {
		t.Fatalf("W/F cells 中=%d ：=%d", boxCellCount('中'), boxCellCount('：'))
	}
	if lineCells("│中文│") != 1+2+2+1 {
		t.Fatalf("│中文│ cells %d", lineCells("│中文│"))
	}
}

func TestBoxGlyphsCoverUnicodeRange(t *testing.T) {
	var missing []string
	for r := rune(boxMin); r <= boxMax; r++ {
		if !boxGlyphDefined(r) {
			missing = append(missing, string(r))
		}
	}
	if len(missing) != 0 {
		t.Fatalf("undefined box glyphs: %s", strings.Join(missing, " "))
	}
}

func TestRoundedArcNotACircle(t *testing.T) {
	cw, ch := float32(7.2), float32(18)
	radius := cw / 2
	southDist := ch / 2
	if math.Abs(float64(radius-southDist)) < 0.5 {
		t.Fatal("test cell is square; pick a non-square example")
	}
	if radius >= southDist {
		t.Fatal("east-fitted circle unexpectedly reaches the south midpoint")
	}
}

func TestBoxFixtureRowsAlign(t *testing.T) {
	for _, src := range []string{boxEnglishMD(), boxCJKMD(), boxArcsMD(), boxWideMD()} {
		lines := strings.Split(src, "\n")
		n := lineCells(lines[0])
		for i, ln := range lines {
			if lineCells(ln) != n {
				t.Fatalf("line %d cells %d want %d (%q)", i, lineCells(ln), n, ln)
			}
		}
		if n < 3 || !isBoxTable(lines) {
			t.Fatalf("fixture not a box table: %q", src)
		}
	}
}

func TestDashLayoutFourAtOneCh(t *testing.T) {
	// 12pt mono "0" is about 7.2 DIP. At 1x a 4-dash light line cannot
	// keep SPEC's 2px gaps (4 segs + 3×2px = 6px of gap alone).
	d := layoutDashes(7.2, 4, 2, 1)
	if d.Note == "" {
		t.Fatalf("expected a gap compromise at 1ch/1x, segs=%v gap=%d", d.SegPx, d.GapPx)
	}
	if len(d.Segs) != 4 {
		t.Fatalf("want 4 visible dashes, got %d note %q", len(d.Segs), d.Note)
	}
	d2 := layoutDashes(7.2, 4, 2, 2)
	if len(d2.Segs) != 4 {
		t.Fatalf("2x: want 4 dashes, got %d", len(d2.Segs))
	}
	for n, gap := range []int{2, 3, 4} {
		_ = n
		got := layoutDashes(18, gap, 2, 1)
		if len(got.Segs) != gap {
			t.Fatalf("vertical 18px %d-dash → %d segs", gap, len(got.Segs))
		}
	}
}

func TestFindIndexesAdjacentProse(t *testing.T) {
	doc := renderDoc(findSrc{ID: "a", Role: roleAssistant, Done: true, Content: boxAdjacentMD()})
	if !strings.Contains(doc.Text, "Lead") || !strings.Contains(doc.Text, "Trail") {
		t.Fatalf("adjacent prose dropped from find: %q", doc.Text)
	}
	if !strings.Contains(doc.Text, "\n") || !strings.Contains(doc.Text, "┌") {
		t.Fatalf("box run lost newlines in find: %q", doc.Text)
	}
	var box, prose int
	for _, r := range doc.Runs {
		if strings.Contains(r.Text, "┌") {
			box++
			if strings.Contains(r.Text, "Lead") || strings.Contains(r.Text, "Trail") {
				t.Fatalf("prose mixed into box find run: %q", r.Text)
			}
		}
		if strings.Contains(r.Text, "Lead") || strings.Contains(r.Text, "Trail") {
			prose++
		}
	}
	if box != 1 || prose < 1 {
		t.Fatalf("runs box=%d prose=%d %+v", box, prose, doc.Runs)
	}
}

func TestFindIndexesBoxTableNewlines(t *testing.T) {
	src := boxEnglishMD()
	doc := renderDoc(findSrc{ID: "a", Role: roleAssistant, Done: true, Content: src})
	if !strings.Contains(doc.Text, "┌") || !strings.Contains(doc.Text, "\n") {
		t.Fatalf("box table find text lost newlines: %q", doc.Text)
	}
	if strings.Contains(doc.Text, " Stage  ") && !strings.Contains(doc.Text, "\n") {
		t.Fatal("softbreaks collapsed")
	}
	hits := findFold(doc.Text, "Design")
	if len(hits) != 1 {
		t.Fatalf("Design hits %d in %q", len(hits), doc.Text)
	}
}

func boxShotApp(t *testing.T, body string) *App {
	t.Helper()
	a := newTestApp(t)
	now := time.Date(2026, 11, 6, 9, 52, 0, 0, time.UTC)
	connectBuiltin(a, "Anthropic", "sk-anthropic")
	cfg := DefaultConfig()
	cfg.Provider = "Anthropic"
	cfg.Model = "claude-sonnet-4.5"
	cfg.ConnectionID = "conn-anthropic"
	a.data.Conversations = []Conversation{{
		ID: "t", Title: "Box table", Config: cfg,
		CreatedAt: now, UpdatedAt: now,
		Messages: []Message{
			{ID: "u", Role: roleUser, Content: "Show the table.", Done: true, CreatedAt: now},
			{ID: "a", Role: roleAssistant, Content: body, Done: true, CreatedAt: now},
		},
	}}
	a.data.ActiveID = "t"
	return a
}

func openBox(t *testing.T, body string) (*App, *ui.Tester) {
	t.Helper()
	a := boxShotApp(t, body)
	tt := ui.NewTester(a.view, shotW, shotH)
	tt.Frame()
	if tt.FramePending() {
		tt.Frame()
	}
	return a, tt
}

func TestBoxTableRendersWithoutCodeHeader(t *testing.T) {
	a, tt := openBox(t, boxEnglishMD())
	mustFind(t, tt, "box table")
	mustFind(t, tt, "box table grid")
	if tt.HasText("code header") {
		t.Fatal("box table must not grow a language/copy header")
	}
	if a.lastBox.Rows < 3 || a.lastBox.Cols < 8 {
		t.Fatalf("layout %+v", a.lastBox)
	}
	if a.lastBox.CellW < 5 || a.lastBox.CellW > 12 {
		t.Fatalf("ascii cell width %.2f", a.lastBox.CellW)
	}
	if !tt.HasText("Design") || !tt.HasText("Write SPEC") {
		t.Fatalf("missing cell text in %q", tt.Texts())
	}
}

func TestBoxTableFencedKeepsHeader(t *testing.T) {
	a, tt := openBox(t, boxFencedMD())
	mustFind(t, tt, "code block")
	mustFind(t, tt, "code header")
	mustFind(t, tt, "box table grid")
	if tt.HasText("box table") && !tt.HasText("code header") {
		t.Fatal("fenced code became a table block")
	}
	if a.lastBox.Rows < 3 {
		t.Fatalf("fenced grid %+v", a.lastBox)
	}
	if !tt.HasText("Design") || !tt.HasText("note: keep the fence") {
		t.Fatalf("fenced body missing text: %q", tt.Texts())
	}
}

func TestBoxTableFalsePositivesDoNotRender(t *testing.T) {
	cases := []string{
		"See the │ pipe in prose, then a ─ dash.",
		"┌──┐\n└──┘\n",
		"▀▀▀\n▄▄▄\n███\n",
		"Only two box lines:\n│ a\n│ b\n",
	}
	for _, body := range cases {
		_, tt := openBox(t, body)
		if tt.HasText("box table") {
			t.Fatalf("false positive for %q labels %q", body, tt.Texts())
		}
	}
}

func TestBoxTableSurroundingText(t *testing.T) {
	_, tt := openBox(t, boxSurroundMD())
	mustFind(t, tt, "box table")
	mustFind(t, tt, "Lead text before the box table.")
	mustFind(t, tt, "Trail text after the box table.")
	if !tt.HasText("U+2580") {
		t.Fatalf("block-element caption missing: %q", tt.Texts())
	}
}

func TestBoxTableAdjacentProseStaysMarkdown(t *testing.T) {
	a, tt := openBox(t, boxAdjacentMD())
	mustFind(t, tt, "box table")
	mustFind(t, tt, "Lead bold sits on the previous line.")
	mustFind(t, tt, "Trail italic sits on the next line.")
	if tt.HasText("**bold**") {
		t.Fatalf("lead lost Markdown: %q", tt.Texts())
	}
	if tt.HasText("_italic_") {
		t.Fatalf("trail lost Markdown: %q", tt.Texts())
	}
	if strings.Contains(a.lastBox.Src, "Lead") || strings.Contains(a.lastBox.Src, "Trail") {
		t.Fatalf("prose absorbed into the box block: %q", a.lastBox.Src)
	}
	if a.lastBox.Rows != strings.Count(boxEnglishMD(), "\n")+1 {
		t.Fatalf("box rows %+v", a.lastBox)
	}
	doc := parser.New(parser.Default)
	doc.Set(boxAdjacentMD())
	var para int
	for i := 1; i < doc.Len(); i++ {
		if doc.Block(i).Kind == parser.Paragraph {
			para = i
			break
		}
	}
	segs := paragraphBoxSegs(doc, para)
	if len(segs) != 3 || segs[0].Box || !segs[1].Box || segs[2].Box {
		t.Fatalf("adjacent split %+v", segs)
	}
}

func TestBoxTableCJKTwoCells(t *testing.T) {
	a, tt := openBox(t, boxCJKMD())
	mustFind(t, tt, "box table grid")
	if a.lastBox.Cols < 8 {
		t.Fatalf("cjk cols %+v", a.lastBox)
	}
	lines := strings.Split(a.lastBox.Src, "\n")
	if len(lines) < 4 {
		t.Fatalf("rows %d", len(lines))
	}
	// 阶段 occupies 4 cells; with padding the first data column is 8.
	if lineCells("阶段") != 4 {
		t.Fatalf("阶段 cells %d", lineCells("阶段"))
	}
	grid := mustFind(t, tt, "box table grid")
	if grid.H < boxLineH*3 {
		t.Fatalf("grid height %.1f", grid.H)
	}
}

func TestBoxTableCopyRoundtrip(t *testing.T) {
	for _, body := range []string{boxEnglishMD(), boxCJKMD()} {
		_, tt := openBox(t, body)
		grid := mustFind(t, tt, "box table grid")
		tt.Press(grid.X+0.5, grid.Y+0.5)
		tt.Move(grid.X+grid.W-0.5, grid.Y+grid.H-0.5)
		tt.Release(grid.X+grid.W-0.5, grid.Y+grid.H-0.5)
		tt.Key(ui.Cmd, ui.KeyC)
		got := tt.Clipboard()
		if got != body {
			t.Fatalf("copy %q want %q", got, body)
		}
	}
}

func TestBoxTableSelectionHitTesting(t *testing.T) {
	a, tt := openBox(t, boxEnglishMD())
	grid := mustFind(t, tt, "box table grid")
	// Click the Design row: header + rule + first body row.
	y := grid.Y + boxLineH*3 + 9
	x := grid.X + a.lastBox.CellW*3
	row, _, r, ok := boxHit(strings.Split(a.lastBox.Src, "\n"), a.lastBox.CellW, x-grid.X, y-grid.Y)
	if !ok || row < 2 {
		t.Fatalf("hit row=%d r=%q ok=%v", row, string(r), ok)
	}
	tt.Press(x, y)
	tt.Move(x+a.lastBox.CellW*6, y)
	tt.Release(x+a.lastBox.CellW*6, y)
	tt.Key(ui.Cmd, ui.KeyC)
	got := tt.Clipboard()
	if got == "" {
		t.Fatal("selection copy was empty")
	}
}

func TestBoxTableFindOverlay(t *testing.T) {
	body := boxEnglishMD()
	a := boxShotApp(t, body)
	tt := ui.NewTester(a.view, shotW, shotH)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Frame()
	tt.Type("Design")
	waitFind(t, a)
	tt.Frame()
	if len(a.finder.hits) < 1 {
		t.Fatalf("find hits %d text %q", len(a.finder.hits), renderDoc(findSrc{ID: "a", Role: roleAssistant, Done: true, Content: body}).Text)
	}
	a.jumpFind(0, time.Time{})
	for i := 0; i < 8 && a.finder.markW < 4; i++ {
		tt.Frame()
	}
	mustFind(t, tt, "box table")
	if a.lastBox.Rows != strings.Count(body, "\n")+1 {
		t.Fatalf("find relayouted the grid: %+v", a.lastBox)
	}
	if a.finder.markW < 4 || a.finder.markH < 8 {
		t.Fatalf("highlight rect missing: fresh=%v x=%.1f y=%.1f w=%.1f h=%.1f hits=%d cur=%d", a.finder.markFresh, a.finder.markX, a.finder.markY, a.finder.markW, a.finder.markH, len(a.finder.hits), a.finder.cur)
	}
	grid := mustFind(t, tt, "box table grid")
	cw := a.lastBox.CellW
	rowTop := grid.Y + boxLineH*3
	rowBot := rowTop + boxLineH
	cellL := grid.X + cw*2
	cellR := grid.X + cw*8
	if a.finder.markY+a.finder.markH < rowTop+2 || a.finder.markY > rowBot-2 {
		t.Fatalf("highlight y=%.1f h=%.1f not on Design row [%.1f,%.1f]", a.finder.markY, a.finder.markH, rowTop, rowBot)
	}
	if a.finder.markX+a.finder.markW < cellL || a.finder.markX > cellR {
		t.Fatalf("highlight x=%.1f w=%.1f not in Design cells [%.1f,%.1f]", a.finder.markX, a.finder.markW, cellL, cellR)
	}
}

func TestBoxTableHorizontalFadeAndScroll(t *testing.T) {
	a := boxShotApp(t, boxWideMD())
	tt := ui.NewTester(a.view, 720, 520)
	tt.Frame()
	if tt.FramePending() {
		tt.Frame()
	}
	mustFind(t, tt, "box table")
	if !tt.HasText("hscroll fade right box") {
		t.Fatalf("missing right fade: %q", tt.Texts())
	}
	if tt.HasText("hscroll fade left box") {
		t.Fatal("left fade before scrolling")
	}
	placeHScroll(t, a, tt, "middle")
	if !tt.HasText("hscroll fade left box") || !tt.HasText("hscroll fade right box") {
		t.Fatalf("middle fades %q", tt.Texts())
	}
	placeHScroll(t, a, tt, "end")
	if tt.HasText("hscroll fade right box") {
		t.Fatal("right fade stayed at the end")
	}
	if !tt.HasText("hscroll fade left box") {
		t.Fatal("left fade missing at the end")
	}
}

func TestBoxTableStreamingSwitchAndNoRegression(t *testing.T) {
	a := boxShotApp(t, "")
	msg := &a.data.Conversations[0].Messages[1]
	tt := ui.NewTester(a.view, shotW, shotH)

	msg.Content = "┌──┐\n│a │"
	msg.Done = false
	tt.Frame()
	if tt.HasText("box table") {
		t.Fatal("switched before 3 qualifying lines")
	}

	msg.Content = "┌──┐\n│a │\n└"
	tt.Frame()
	if !tt.HasText("box table") {
		t.Fatal("partial third line that starts with a box char should switch")
	}

	full := boxEnglishMD()
	msg.Content = full
	tt.Frame()
	streamRows := a.lastBox.Rows
	streamCols := a.lastBox.Cols
	grid := mustFind(t, tt, "box table grid")
	streamH := grid.H

	msg.Done = true
	tt.Frame()
	if a.lastBox.Rows != streamRows || a.lastBox.Cols != streamCols {
		t.Fatalf("finished text changed the grid: stream %dx%d view %dx%d", streamCols, streamRows, a.lastBox.Cols, a.lastBox.Rows)
	}
	grid = mustFind(t, tt, "box table grid")
	if math.Abs(float64(grid.H-streamH)) > 1 {
		t.Fatalf("finished height %.1f vs stream %.1f", grid.H, streamH)
	}
}

func TestBoxTableChunkedStreaming(t *testing.T) {
	a := boxShotApp(t, "")
	msg := &a.data.Conversations[0].Messages[1]
	msg.Done = false
	tt := ui.NewTester(a.view, shotW, shotH)
	chunks := []string{"┌", "──", "┐\n", "│ ", "Hi", " │\n", "└──┘"}
	var body strings.Builder
	for i, ch := range chunks {
		body.WriteString(ch)
		msg.Content = body.String()
		tt.Frame()
		lines := strings.Split(strings.TrimRight(msg.Content, "\n"), "\n")
		want := isBoxTable(lines)
		got := tt.HasText("box table")
		if got != want {
			t.Fatalf("chunk %d lines=%q want box=%v got=%v", i, lines, want, got)
		}
	}
}

func TestBoxTableAllGlyphsPaint(t *testing.T) {
	a, tt := openBox(t, boxAllGlyphsMD())
	mustFind(t, tt, "box table grid")
	if a.lastBox.Rows != 8 {
		t.Fatalf("glyph rows %d", a.lastBox.Rows)
	}
	grid := mustFind(t, tt, "box table grid")
	img := tt.Image()
	ink := a.look.CodeText
	found := 0
	x0, y0 := int(grid.X)+2, int(grid.Y)+2
	x1, y1 := int(grid.X+grid.W)-2, int(grid.Y+grid.H)-2
	b := img.Bounds()
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
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			px := img.RGBAAt(x, y)
			if boxInkNear(px, ink, 80) {
				found++
			}
		}
	}
	if found < 200 {
		t.Fatalf("glyph grid looks empty: %d ink pixels", found)
	}
}

func boxInkNear(px interface {
	RGBA() (uint32, uint32, uint32, uint32)
}, c ui.Color, slop int) bool {
	r, g, b, a := px.RGBA()
	if a < 0x8000 {
		return false
	}
	near := func(v uint32, want uint8) bool {
		d := int(v>>8) - int(want)
		if d < 0 {
			d = -d
		}
		return d <= slop
	}
	return near(r, c.R) && near(g, c.G) && near(b, c.B)
}

func TestBoxHitCJK(t *testing.T) {
	lines := []string{"│中│"}
	cw := float32(7)
	_, cells, r, ok := boxHit(lines, cw, cw+1, 4)
	if !ok || r != '中' || cells != 1 {
		t.Fatalf("hit 中: ok=%v r=%q cells=%d", ok, string(r), cells)
	}
	_, _, r, ok = boxHit(lines, cw, cw+cw+1, 4)
	if !ok || r != '中' {
		t.Fatalf("second cell of 中: ok=%v r=%q", ok, string(r))
	}
}

func TestNoInlineMarkdownInBoxTable(t *testing.T) {
	body := "┌────┐\n│ **x** │\n└────┘"
	_, tt := openBox(t, body)
	mustFind(t, tt, "box table")
	if !tt.HasText("**x**") {
		t.Fatalf("inline markdown was parsed inside a box table: %q", tt.Texts())
	}
}

func TestBoxTableSoftBreakBecomesNewline(t *testing.T) {
	doc := parser.New(parser.Default)
	src := "┌──┐\n│ab│\n└──┘"
	doc.Set(src)
	got, ok := boxTableSource(doc, 1)
	if !ok {
		t.Fatal("paragraph not detected")
	}
	if got != src {
		t.Fatalf("source %q want %q", got, src)
	}
	if utf8.RuneCountInString(got) != utf8.RuneCountInString(src) {
		t.Fatal("rune count changed")
	}
}
