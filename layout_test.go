package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestSmallWindowLayout(t *testing.T) {
	dir := filepath.Join("/opt/cursor/artifacts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sizes := [][2]int{{700, 500}, {720, 480}, {760, 520}, {800, 560}, {900, 600}}
	shots := map[[2]int]bool{{720, 480}: true, {800, 560}: true, {900, 600}: true}
	for _, sz := range sizes {
		for _, dark := range []bool{false, true} {
			a := smallEmptyApp(t)
			tt := ui.NewTester(a.view, sz[0], sz[1])
			if dark {
				tt.SetDark(true)
			}
			assertSuggestionGrid(t, tt, sz)
			if shots[sz] {
				mode := "light"
				if dark {
					mode = "dark"
				}
				saveShot(t, dir, "small-empty-"+mode+"-"+itoa(sz[0])+"x"+itoa(sz[1])+".png", tt.Image())
			}

			b := smallLongApp(t)
			tb := ui.NewTester(b.view, sz[0], sz[1])
			if dark {
				tb.SetDark(true)
			}
			assertLongLayout(t, tb, sz)
			if shots[sz] {
				mode := "light"
				if dark {
					mode = "dark"
				}
				saveShot(t, dir, "small-long-"+mode+"-"+itoa(sz[0])+"x"+itoa(sz[1])+".png", tb.Image())
			}
		}
	}
}

func smallEmptyApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	now := time.Date(2026, 10, 19, 9, 41, 0, 0, time.Local)
	a.data.Conversations = []Conversation{{
		ID: "e", Title: "Empty", UpdatedAt: now, CreatedAt: now,
		Config: smallConfig(),
	}}
	a.data.ActiveID = "e"
	return a
}

func smallLongApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	// A connected provider keeps the title on "model · host". With no
	// provider the subtitle is "No provider yet", and this layout check
	// never sees the long line.
	connectBuiltin(a, "Anthropic", "sk-anthropic")
	cfg := smallConfig()
	cfg.ConnectionID = "conn-anthropic"
	now := time.Date(2026, 10, 19, 9, 41, 0, 0, time.Local)
	line := "selector := window.deepQuery(\"#composer .toolbar button[data-action=send]\").closest(\"[data-region]\")"
	reply := strings.Join([]string{
		strings.Repeat("This paragraph keeps the transcript longer than a short window, so the end is not also the start.\n\n", 6),
		"A short lead so the code and the table sit above the composer.",
		"",
		"```go",
		line,
		"resp, err := http.DefaultClient.Do(req.WithContext(ctx))",
		"```",
		"",
		"| Approach | Stops the dial | Frees the goroutine |",
		"| --- | --- | --- |",
		"| Context cancel | Immediately | Yes |",
		"| Client.Timeout | Only after the timeout | Yes |",
	}, "\n")
	a.data.Conversations = []Conversation{{
		ID:        "c1",
		Title:     "Cancel HTTP on close and keep the title from running into the toolbar buttons",
		UpdatedAt: time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC), CreatedAt: time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC),
		Config: cfg,
		Messages: []Message{
			{ID: "u", Role: roleUser, Content: "What’s the difference between Client.Timeout and a context deadline when the caller is a window that can close at any moment?", Done: true, CreatedAt: now},
			{ID: "a", Role: roleAssistant, Content: reply, Reasoning: "Think about the window lifecycle before answering.", Done: true, CreatedAt: now},
		},
	}}
	a.data.ActiveID = "c1"
	return a
}

func smallConfig() Config {
	return Config{
		Provider: "Anthropic", Model: "claude-sonnet-4.5", Reasoning: "medium",
		BaseURL: "https://inference.a-very-long-internal-host.example.com/v1",
	}
}

func assertSuggestionGrid(t *testing.T, tt *ui.Tester, sz [2]int) {
	t.Helper()
	card := mustFind(t, tt, "content card")
	left := mustFind(t, tt, "starter Explain a tricky idea")
	right := mustFind(t, tt, "starter Outline a plan")
	assertPadded(t, "starter Explain", card, left, space6)
	assertPadded(t, "starter Outline", card, right, space6)
	detail := mustFind(t, tt, "Short paragraphs, the hard part named.")
	// Two columns at a medium width ellipsize the 222pt line. It still has
	// to be a real line, not a few letters.
	if detail.W < 80 || detail.H < 8 {
		t.Fatalf("%dx%d subtitle squeezed to %.1fx%.1f", sz[0], sz[1], detail.W, detail.H)
	}
	// W is the card's content width. Two columns start at 560, not at the
	// old 284pt-per-card fit.
	two := card.W-2 >= 560
	if two {
		if abs32(left.Y-right.Y) > 2 || right.X < left.X+left.W {
			t.Fatalf("%dx%d expected two columns, explain %v outline %v", sz[0], sz[1], left, right)
		}
	} else if abs32(left.X-right.X) > 2 || right.Y < left.Y+left.H-2 {
		t.Fatalf("%dx%d expected one column, explain %v outline %v", sz[0], sz[1], left, right)
	}
	for _, name := range []string{"starter Review a draft", "starter Compare two approaches", "⌘N  " + newChatTitle, "⌘K  Search"} {
		r, ok := tt.Find(name)
		if !ok || r.H < 1 || r.W < 1 {
			continue
		}
		assertPadded(t, name, card, r, space6)
	}
}

func assertLongLayout(t *testing.T, tt *ui.Tester, sz [2]int) {
	t.Helper()
	card := mustFind(t, tt, "content card")
	regen := mustFind(t, tt, "Regenerate")
	model := mustFind(t, tt, "claude-sonnet-4.5 · inference.a-very-long-internal-host.example.com")
	if model.X+model.W > regen.X-1 {
		t.Fatalf("%dx%d model runs into Regenerate: model %v button %v", sz[0], sz[1], model, regen)
	}
	assertTitleClearsButtons(t, tt.Image(), regen)
	meta := mustFind(t, tt, "Jan 2")
	if meta.X+meta.W > card.X-4 {
		t.Fatalf("%dx%d sidebar time leaves the sidebar: time %v card %v", sz[0], sz[1], meta, card)
	}
	assertGapBefore(t, tt.Image(), meta)

	scrollBy(t, tt, -8000)
	user := mustFind(t, tt, "What’s the difference between Client.Timeout and a context deadline when the caller is a window that can close at any moment?")
	assertPadded(t, "user bubble", card, user, space6)
	scrollBy(t, tt, 8000)
	code := mustFind(t, tt, "code block")
	assertPadded(t, "code block", card, code, space6)
	lineW, _ := ui.ShapeText("selector := window.deepQuery(\"#composer .toolbar button[data-action=send]\").closest(\"[data-region]\")", ui.Font{Family: "ui-monospace", Size: 12.5}, 0).Size()
	if code.W > lineW-8 {
		t.Fatalf("%dx%d code block %.1f is not narrower than its long line %.1f", sz[0], sz[1], code.W, lineW)
	}
	table := mustFind(t, tt, "table scroller")
	assertPadded(t, "table", card, table, space6)
	before := mustFind(t, tt, "Frees the goroutine")
	// A column that starts past the viewport has to come in by scrolling,
	// not by widening the table under the card.
	if before.X > table.X+table.W-8 || before.W < 8 {
		tt.Scroll(table.X+20, table.Y+20, 160, 0)
		after := mustFind(t, tt, "Frees the goroutine")
		if after.X >= before.X-1 || after.W < 8 {
			t.Fatalf("%dx%d table did not scroll horizontally: before %v after %v viewport %v", sz[0], sz[1], before, after, table)
		}
	}

	comp := mustFind(t, tt, "composer")
	send := mustFind(t, tt, "Send")
	contentW := paneInnerWidth(float32(sz[0]), sidebarFor(float32(sz[0])))
	hint, hintOK := tt.Find("↵ send · ⇧↵ newline")
	if contentW < 560 {
		if hintOK && hint.W > 1 && hint.H > 1 {
			t.Fatalf("%dx%d hint should hide below 560pt of content", sz[0], sz[1])
		}
	} else {
		if !hintOK {
			t.Fatalf("%dx%d missing send hint", sz[0], sz[1])
		}
		if send.X+send.W > comp.X+comp.W-4 || hint.X+hint.W > send.X-2 {
			t.Fatalf("%dx%d composer controls overflow: composer %v hint %v send %v", sz[0], sz[1], comp, hint, send)
		}
		if hint.X < comp.X || send.X < comp.X {
			t.Fatalf("%dx%d composer controls left the shell: composer %v hint %v send %v", sz[0], sz[1], comp, hint, send)
		}
	}
	if send.X < comp.X || send.X+send.W > comp.X+comp.W-4 {
		t.Fatalf("%dx%d send left the composer: composer %v send %v", sz[0], sz[1], comp, send)
	}

	scrollBy(t, tt, -220)
	jump := mustFind(t, tt, "Jump to latest")
	if jump.X < card.X+4 || jump.X+jump.W > card.X+card.W-4 {
		t.Fatalf("%dx%d jump pill leaves the card: pill %v card %v", sz[0], sz[1], jump, card)
	}
	if jump.Y+jump.H > comp.Y-2 {
		t.Fatalf("%dx%d jump pill overlaps the composer: pill %v composer %v", sz[0], sz[1], jump, comp)
	}
}

func scrollBy(t *testing.T, tt *ui.Tester, dy float32) {
	t.Helper()
	card := mustFind(t, tt, "content card")
	tt.Scroll(card.X+40, card.Y+90, 0, dy)
}

func assertPadded(t *testing.T, name string, card, r ui.Rect, pad float32) {
	t.Helper()
	if r.W < 1 || r.H < 1 {
		return
	}
	if r.X < card.X+pad || r.X+r.W > card.X+card.W-pad {
		t.Fatalf("%s outside the %gpt padding: item x=%.1f..%.1f card x=%.1f..%.1f", name, pad, r.X, r.X+r.W, card.X, card.X+card.W)
	}
}

func assertGapBefore(t *testing.T, img image.Image, meta ui.Rect) {
	t.Helper()
	y := int(meta.Y + meta.H/2)
	bg := img.At(int(meta.X)+2, int(meta.Y)-3)
	gap := 0
	for x := int(meta.X) - 1; x > int(meta.X)-48 && x > 0; x-- {
		if contrasts(img, x, y, bg) {
			break
		}
		gap++
	}
	if gap < 6 {
		t.Fatalf("sidebar title meets the time at x=%.0f, gap %dpx", meta.X, gap)
	}
}

func contrasts(img image.Image, x, y int, bg color.Color) bool {
	b := img.Bounds()
	if x < b.Min.X || y < b.Min.Y || x >= b.Max.X || y >= b.Max.Y {
		return false
	}
	r, g, bl, _ := img.At(x, y).RGBA()
	br, bgc, bb, _ := bg.RGBA()
	d := absInt(int(r)-int(br)) + absInt(int(g)-int(bgc)) + absInt(int(bl)-int(bb))
	return d/257 > 90
}

func assertTitleClearsButtons(t *testing.T, img image.Image, regen ui.Rect) {
	t.Helper()
	// The title is the upper line of the toolbar. Its ink must stop
	// short of the Regenerate button; the ellipsis sits in that gap.
	bg := img.At(int(regen.X)+8, 4)
	minGap := 1000
	for y := int(regen.Y + 2); y < int(regen.Y+14); y++ {
		gap := 0
		for x := int(regen.X) - 1; x > 0 && gap < 80; x-- {
			if contrasts(img, x, y, bg) {
				break
			}
			gap++
		}
		if gap < minGap {
			minGap = gap
		}
	}
	if minGap < 2 {
		t.Fatalf("title runs into Regenerate, gap %dpx", minGap)
	}
}

func mustFind(t *testing.T, tt *ui.Tester, s string) ui.Rect {
	t.Helper()
	r, ok := tt.Find(s)
	if !ok {
		t.Fatalf("missing %q in %q", s, tt.Texts())
	}
	return r
}

// The suggestion row used to be two Size(300) cards and a 10pt gap, centered
// in the 24pt padded column. iconLeftInCard is where that left icon tile
// started, from the card's inner left. Negative means the card clip removed it.
func iconLeftInCard(cardBorder float32) float32 {
	content := cardBorder - 2
	inner := content - 2*space6
	return space6 + (inner-float32(cardW*2+cardGap))/2 + 12
}

func TestFixedGridClippedLeftIcons(t *testing.T) {
	// A 700×500 window with the 240pt sidebar lays the card out 451pt wide.
	// The centered 610pt row then starts 68pt left of the card, so the left
	// tile (28pt, 12pt into its card) is entirely outside the clip. The right
	// tile is one card and a gap further along, still inside. Both columns
	// use the same four SVGs, so a missing asset cannot explain a left-only gap.
	const cardBorder = 451
	const content = cardBorder - 2
	left := iconLeftInCard(cardBorder)
	if left+28 >= 0 {
		t.Fatalf("left icon tile should be outside the card, origin %.1f", left)
	}
	right := left + cardW + cardGap
	if right < 12 || right+28 > content-12 {
		t.Fatalf("right icon should stay inside the card, origin %.1f content %d", right, content)
	}
	// A 620pt card is the same overflow, not yet past the glyph: the row is
	// 40pt wider than the padded column, 20pt a side, and the tile starts
	// 16pt in. Narrower than about 530pt of content, the tile is gone.
	at620 := iconLeftInCard(620)
	if at620 < 0 || at620 > 24 {
		t.Fatalf("620pt card should leave the left tile just inside, origin %.1f", at620)
	}
}

func TestSuggestionIconsRender(t *testing.T) {
	// Minimum, a two-column width, and the design size. Below 560pt of
	// content the fourth card is hidden.
	sizes := [][2]int{{720, 520}, {880, 600}, {1120, 760}}
	names := []string{"Explain a tricky idea", "Outline a plan", "Review a draft", "Compare two approaches"}
	dir := "/opt/cursor/artifacts"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, sz := range sizes {
		for _, dark := range []bool{false, true} {
			a := smallEmptyApp(t)
			tt := ui.NewTester(a.view, sz[0], sz[1])
			if dark {
				tt.SetDark(true)
			}
			card := mustFind(t, tt, "content card")
			comp := mustFind(t, tt, "composer")
			seen := map[string]bool{}
			check := func() {
				t.Helper()
				for _, name := range names {
					tile, ok := tt.Find("starter icon " + name)
					// Skip a tile still under the composer or cut by the scroll.
					if !ok || tile.W < 26 || tile.H < 26 || tile.Y < card.Y+4 || tile.Y+tile.H > comp.Y-2 {
						continue
					}
					assertPadded(t, name+" icon", card, tile, space6)
					btn := mustFind(t, tt, "starter "+name)
					if tile.X < btn.X || tile.X+tile.W > btn.X+btn.W+0.5 {
						t.Fatalf("%dx%d %s icon %v leaves its card %v", sz[0], sz[1], name, tile, btn)
					}
					// A tile in the composer fade is still in the tree but the
					// glyph is washed out. Leave it unseen and scroll it up.
					if accentPixels(tt.Image(), tile) < 12 {
						continue
					}
					seen[name] = true
				}
			}
			for step := 0; step < 8 && len(seen) < len(names); step++ {
				check()
				if len(seen) == len(names) {
					break
				}
				scrollBy(t, tt, 140)
			}
			want := len(names)
			if paneInnerWidth(float32(sz[0]), sidebarFor(float32(sz[0]))) < 560 {
				want = 3
			}
			if len(seen) < want {
				t.Fatalf("%dx%d dark=%v icons rendered %v", sz[0], sz[1], dark, seen)
			}
			mode := "light"
			if dark {
				mode = "dark"
			}
			saveShot(t, dir, "icons-"+mode+"-"+itoa(sz[0])+"x"+itoa(sz[1])+".png", tt.Image())
		}
	}
}

func accentPixels(img image.Image, r ui.Rect) int {
	n := 0
	b := img.Bounds()
	x0, y0 := int(r.X+1), int(r.Y+1)
	x1, y1 := int(r.X+r.W-1), int(r.Y+r.H-1)
	for y := y0; y < y1; y++ {
		if y < b.Min.Y || y >= b.Max.Y {
			continue
		}
		for x := x0; x < x1; x++ {
			if x < b.Min.X || x >= b.Max.X {
				continue
			}
			cr, cg, cb, _ := img.At(x, y).RGBA()
			// The glyph is accent blue. The soft tile is the same hue at a
			// low alpha, still bluer than the warm card.
			if cb > cr+40*257 && cb > cg+12*257 && cb > 140*257 {
				n++
			}
		}
	}
	return n
}

func TestSettingsCardFillsUnderTitle(t *testing.T) {
	a := newTestApp(t)
	a.settingsOpen = true
	a.settingsSection = "general"
	const w, h = 780, 900
	tt := ui.NewTester(a.view, w, h)
	card, ok := tt.Find("settings card")
	if !ok {
		t.Fatal("settings card is missing")
	}
	if card.Y < 51 || card.Y > 54 {
		t.Fatalf("card y %.1f, want it flush under the 52pt title bar", card.Y)
	}
	if bottom := card.Y + card.H; bottom < float32(h)-10 || bottom > float32(h)-6 {
		t.Fatalf("card bottom %.1f, want %d", bottom, h-8)
	}
	if right := card.X + card.W; right < float32(w)-10 || right > float32(w)-6 {
		t.Fatalf("card right %.1f, want %d", right, w-8)
	}
	if _, ok := tt.Find("Back"); !ok {
		t.Fatal("title bar is missing the disabled back button")
	}
	if _, ok := tt.Find("Forward"); !ok {
		t.Fatal("title bar is missing the disabled forward button")
	}
	for _, text := range tt.Texts() {
		if text == "Keyboard Shortcuts" {
			t.Fatal("sidebar still says Keyboard Shortcuts")
		}
	}
	if !tt.HasText("Default model") || !tt.HasText("↵ Return") || !tt.HasText("Sent with every new chat") {
		t.Fatalf("general: %q", tt.Texts())
	}
}

func saveShot(t *testing.T, dir, name string, img image.Image) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
