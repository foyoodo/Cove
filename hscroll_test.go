package main

import (
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestHScrollFades(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		fit := openHScroll(t, fitScrollApp(t), 1120, 760, dark)
		if fit.HasText("hscroll fade") {
			t.Fatalf("%s a block that fits still fades: %q", mode, fit.Texts())
		}

		a := smallLongApp(t)
		tt := openHScroll(t, a, 720, 520, dark)
		code := mustFind(t, tt, "code block")
		header := mustFind(t, tt, "code header")
		right := mustFind(t, tt, "hscroll fade right code")
		if tt.HasText("hscroll fade left code") {
			t.Fatalf("%s left fade showed before scrolling", mode)
		}
		if right.Y < header.Y+header.H-1 {
			t.Fatalf("%s right fade covers the title bar: fade %v header %v", mode, right, header)
		}
		if right.W < hScrollFade-1 || right.W > hScrollFade+1 {
			t.Fatalf("%s fade width %.1f", mode, right.W)
		}
		if right.X < code.X || right.X+right.W > code.X+code.W+1 || right.Y+right.H > code.Y+code.H+1 {
			t.Fatalf("%s fade leaves the code block: fade %v block %v", mode, right, code)
		}
		edge := scrollEdge(a.look.CodeBg, a.cardFlat())
		px := tt.Image().RGBAAt(int(right.X+right.W-2), int(right.Y+right.H/2))
		if d := plateDist(px, edge); d > 48 {
			t.Fatalf("%s code edge #%02x%02x%02x vs background #%02x%02x%02x (Δ %d)", mode, px.R, px.G, px.B, edge.R, edge.G, edge.B, d)
		}

		table := mustFind(t, tt, "table scroller")
		head := mustFind(t, tt, "Approach")
		if head.Y < table.Y-1 || head.Y+head.H > table.Y+table.H+1 {
			t.Fatalf("%s header leaves the table: head %v table %v", mode, head, table)
		}

		bodyY := right.Y + right.H/2
		tt.Press(right.X+4, bodyY)
		tt.Move(right.X+right.W-3, bodyY)
		tt.Release(right.X+right.W-3, bodyY)
		tt.Key(ui.Cmd, ui.KeyC)
		got := strings.TrimSpace(tt.Clipboard())
		full := smallLongCode() + "\nresp, err := http.DefaultClient.Do(req.WithContext(ctx))"
		if got == "" || !strings.Contains(full, got) {
			t.Fatalf("%s selection under the fade copied %q", mode, got)
		}

		tt.Scroll(code.X+40, bodyY, 120, 0)
		if !tt.HasText("hscroll fade left code") {
			t.Fatalf("%s left fade missing after scrolling", mode)
		}
		if !tt.HasText("hscroll fade right code") {
			t.Fatalf("%s right fade left early", mode)
		}

		for i := 0; i < 8; i++ {
			tt.Scroll(code.X+48, code.Y+code.H-16, 500, 0)
		}
		if tt.HasText("hscroll fade right code") {
			t.Fatalf("%s right fade stayed at the end", mode)
		}
		if !tt.HasText("hscroll fade left code") {
			t.Fatalf("%s left fade missing at the end", mode)
		}
	}
}

func TestHScrollShots(t *testing.T) {
	dir := testArtifactDir(t, "hscroll")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		a := smallLongApp(t)
		tt := openHScroll(t, a, 720, 520, dark)
		for _, place := range []string{"start", "middle", "end"} {
			placeHScroll(t, a, tt, place)
			code := mustFind(t, tt, "code block")
			table := mustFind(t, tt, "table scroller")
			writePNG(t, filepath.Join(dir, "hscroll-code-"+place+"-"+mode+".png"), cropRect(tt.Image(), code, 2))
			writePNG(t, filepath.Join(dir, "hscroll-table-"+place+"-"+mode+".png"), cropRect(tt.Image(), table, 2))
		}
	}
}

func TestTableFadeMatchesSurface(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		a := smallLongApp(t)
		tt := openHScroll(t, a, 720, 520, dark)
		assertTableEdge(t, tt, mode+" right", true)
		placeHScroll(t, a, tt, "middle")
		assertTableEdge(t, tt, mode+" left", false)
	}
}

func TestHScrollR3Shots(t *testing.T) {
	dir := testArtifactDir(t, "hscroll-r3")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		a := smallLongApp(t)
		tt := openHScroll(t, a, 720, 520, dark)
		table := mustFind(t, tt, "table scroller")
		writePNG(t, filepath.Join(dir, "table-right-"+mode+".png"), scaleNN(cropRect(tt.Image(), tableEdgeCrop(table, true), 0), 8))
		placeHScroll(t, a, tt, "middle")
		table = mustFind(t, tt, "table scroller")
		writePNG(t, filepath.Join(dir, "table-left-"+mode+".png"), scaleNN(cropRect(tt.Image(), tableEdgeCrop(table, false), 0), 8))
	}
}

// tableEdgeCrop is the fade band, a strip of the rows beside it, and a
// few pixels past the outline so a plate outside the table would show.
func tableEdgeCrop(table ui.Rect, right bool) ui.Rect {
	const beside = 28
	const past = 8
	w := float32(hScrollFade + beside + past)
	x := table.X - past
	if right {
		x = table.X + table.W - float32(hScrollFade) - beside
	}
	return ui.Rect{X: x, Y: table.Y - 6, W: w, H: table.H + 12}
}

func assertTableEdge(t *testing.T, tt *ui.Tester, mode string, right bool) {
	t.Helper()
	table := mustFind(t, tt, "table scroller")
	head := mustFind(t, tt, "Approach")
	if head.Y < table.Y-1 || head.Y > table.Y+28 {
		t.Fatalf("%s header is not the top row: head %v table %v", mode, head, table)
	}
	img := tt.Image()
	// outside is the column just before the band. inside is the first
	// column of it. outer is the last column still inside the straight
	// edge; the outline pixel itself is antialiased.
	var outside, inside, outer, toward int
	if right {
		band := int(table.X + table.W - hScrollFade)
		outside = band - 1
		inside = band + 1
		outer = int(table.X+table.W) - 1
		toward = -1
	} else {
		band := int(table.X)
		outside = band + hScrollFade + 1
		inside = band + hScrollFade - 1
		outer = int(table.X)
		toward = 1
	}
	// The page behind the table is a gradient, so a row's own background
	// is the empty pixel of that row just beside the band, not a pixel
	// from the middle of the viewport.
	// Header, then each body row. Rules are 1px between 30px rows.
	y := table.Y
	rows := 0
	var headerOwn, headerEdge, bodyOwn, bodyEdge color.RGBA
	for y+30 <= table.Y+table.H+1 && rows < 8 {
		sample := int(y + 27)
		if float32(sample) > table.Y+table.H-10 {
			sample = int(y + 4)
		}
		if float32(sample) < table.Y+10 {
			sample = int(y + 27)
		}
		a := img.RGBAAt(outside, sample)
		b := img.RGBAAt(inside, sample)
		if d := lumDelta(a, b); d > 2 {
			t.Fatalf("%s seam at %d,%d: just outside #%02x%02x%02x just inside #%02x%02x%02x (|Δ| %.1f)", mode, inside, sample, a.R, a.G, a.B, b.R, b.G, b.B, d)
		}
		own := img.RGBAAt(outside, sample)
		edge := img.RGBAAt(outer, sample)
		if d := lumDelta(edge, own); d > 2 {
			t.Fatalf("%s outer column #%02x%02x%02x != row bg #%02x%02x%02x at y %d (Δ %.1f)", mode, edge.R, edge.G, edge.B, own.R, own.G, own.B, sample, d)
		}
		if rows == 0 {
			headerOwn, headerEdge = own, edge
		} else if rows == 1 {
			bodyOwn, bodyEdge = own, edge
		}
		rows++
		y += 31
	}
	if rows < 2 {
		t.Fatalf("%s expected header and a body row, table %v", mode, table)
	}
	// Header and body keep different surfaces at the outer column. One
	// wash painted over both would flatten that.
	if dRow, dEdge := lumDelta(headerOwn, bodyOwn), lumDelta(headerEdge, bodyEdge); dRow > 4 && dEdge+1 < dRow/2 {
		t.Fatalf("%s outer column lost the row surfaces (beside Δ %.1f, edge Δ %.1f)", mode, dRow, dEdge)
	}
	// Glyphs dissolve. Text just outside the band is still text; the outer
	// column of that row matches its padding.
	glyphY := int(table.Y + 15)
	padY := int(table.Y + 27)
	glyphX, glyphD := 0, 0.0
	for i := 1; i <= 160; i++ {
		x := outside + toward*i
		if x < int(table.X)+4 || x > int(table.X+table.W)-4 {
			break
		}
		if d := lumDelta(img.RGBAAt(x, glyphY), img.RGBAAt(x, padY)); d > glyphD {
			glyphX, glyphD = x, d
		}
	}
	if glyphD < 40 {
		t.Fatalf("%s expected header text beside the fade (Δ %.1f)", mode, glyphD)
	}
	// The outer column of the header, below the corner radius, matches the
	// header padding. A glyph left at full strength is far above this.
	pad := img.RGBAAt(outer, padY)
	var shown float64
	for y := int(table.Y) + 8; y < int(table.Y)+28; y++ {
		if d := lumDelta(img.RGBAAt(outer, y), pad); d > shown {
			shown = d
		}
	}
	if shown > 8 {
		t.Fatalf("%s header text still reads at the outer column (Δ %.1f)", mode, shown)
	}
	// The rule under the header fades with the text.
	ruleY := int(table.Y + 30)
	if d := lumDelta(img.RGBAAt(glyphX, ruleY), img.RGBAAt(glyphX, ruleY+8)); d < 4 {
		t.Fatalf("%s row rule is not visible beside the fade (|Δ| %.1f)", mode, d)
	}
	if d := lumDelta(img.RGBAAt(outer, ruleY), img.RGBAAt(outer, ruleY+8)); d > 2 {
		t.Fatalf("%s row rule stays at the outer edge (|Δ| %.1f)", mode, d)
	}
	// Nothing past the outline, including beside the rounded corner.
	yMid := int(table.Y + table.H/2)
	yCorner := int(table.Y) + 2
	xOut := int(table.X+table.W) + 2
	xFar := int(table.X+table.W) + 8
	if !right {
		xOut = int(table.X) - 3
		xFar = int(table.X) - 9
	}
	for _, y := range []int{yMid, yCorner, int(table.Y+table.H) - 2} {
		if d := lumDelta(img.RGBAAt(xOut, y), img.RGBAAt(xFar, y)); d > 2 {
			t.Fatalf("%s draws outside the table at %d,%d (|Δ| %.1f)", mode, xOut, y, d)
		}
	}
}

func lumDelta(a, b color.RGBA) float64 {
	la := 0.2126*float64(a.R) + 0.7152*float64(a.G) + 0.0722*float64(a.B)
	lb := 0.2126*float64(b.R) + 0.7152*float64(b.G) + 0.0722*float64(b.B)
	d := la - lb
	if d < 0 {
		d = -d
	}
	return d
}

func openHScroll(t *testing.T, a *App, w, h int, dark bool) *ui.Tester {
	t.Helper()
	tt := ui.NewTester(a.view, w, h)
	if dark {
		tt.SetDark(true)
	}
	tt.Frame()
	tt.Advance(time.Millisecond)
	return tt
}

func fitScrollApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	now := time.Now()
	body := "```go\nfmt.Println(\"hi\")\n```\n\n| A | B |\n| --- | --- |\n| one | two |\n"
	a.data.Conversations = []Conversation{{
		ID: "c", Title: "Fits", Config: DefaultConfig(),
		Messages: []Message{
			{ID: "u", Role: roleUser, Content: "short", Done: true, CreatedAt: now},
			{ID: "a", Role: roleAssistant, Content: body, Done: true, CreatedAt: now},
		},
	}}
	a.data.ActiveID = "c"
	return a
}

func smallLongCode() string {
	return "selector := window.deepQuery(\"#composer .toolbar button[data-action=send]\").closest(\"[data-region]\")"
}

func placeHScroll(t *testing.T, a *App, tt *ui.Tester, place string) {
	t.Helper()
	frac := float32(0)
	switch place {
	case "middle":
		frac = 0.5
	case "end":
		frac = 1
	}
	for _, slot := range a.hScrolls {
		slot.st.X = slot.st.MaxX * frac
	}
	tt.Frame()
}
