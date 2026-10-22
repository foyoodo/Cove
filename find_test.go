package main

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cove/themes"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestFindRenderedTextSkipsMarkdown(t *testing.T) {
	src := findSrc{
		ID: "a", Role: roleAssistant, Done: true,
		Content: "See **bold** and [hello](https://example.com/secret) plus `code`.\n\n```go\nfunc main() { return }\n```\n",
	}
	doc := renderDoc(src)
	if strings.Contains(doc.Text, "**") || strings.Contains(doc.Text, "https://") || strings.Contains(doc.Text, "example.com") || strings.Contains(doc.Text, "`") {
		t.Fatalf("rendered text kept markup: %q", doc.Text)
	}
	if !strings.Contains(doc.Text, "bold") || !strings.Contains(doc.Text, "hello") || !strings.Contains(doc.Text, "code") || !strings.Contains(doc.Text, "func main") {
		t.Fatalf("rendered text dropped visible words: %q", doc.Text)
	}
	if strings.Contains(doc.Text, "go\n") || strings.HasPrefix(strings.TrimSpace(doc.Text), "go") {
		t.Fatalf("code fence language leaked into rendered text: %q", doc.Text)
	}
	// "go" as a language tag must not be a hit. "main" must.
	if n := len(findFold(doc.Text, "go")); n != 0 {
		t.Fatalf("language tag matched %d times in %q", n, doc.Text)
	}
	if len(findFold(doc.Text, "main")) != 1 {
		t.Fatalf("code body: %q", doc.Text)
	}
	if len(findFold(doc.Text, "secret")) != 0 || len(findFold(doc.Text, "**")) != 0 {
		t.Fatalf("url or markers matched in %q", doc.Text)
	}
	// A match can cross a link boundary: "Request" + link "WithContext".
	cross := renderDoc(findSrc{ID: "c", Role: roleAssistant, Done: true, Content: "Call Request[WithContext](https://pkg.go.dev/net/http) now."})
	// The brackets above are not a link. Use a real link split.
	cross = renderDoc(findSrc{ID: "c", Role: roleAssistant, Done: true, Content: "Call Re[quest.With](https://pkg.go.dev/x)Context now."})
	if !strings.Contains(cross.Text, "Request.WithContext") {
		t.Fatalf("link text did not join: %q", cross.Text)
	}
	if len(findFold(cross.Text, "Request.WithContext")) != 1 {
		t.Fatalf("cross-style match missed in %q", cross.Text)
	}
	if len(findFold(cross.Text, "pkg.go.dev")) != 0 {
		t.Fatalf("link url matched: %q", cross.Text)
	}
	// User bubbles show the source, markers included.
	user := renderDoc(findSrc{ID: "u", Role: roleUser, Done: true, Content: "keep **stars**"})
	if len(findFold(user.Text, "**")) == 0 {
		t.Fatalf("user text should keep markers: %q", user.Text)
	}
	heading := renderDoc(findSrc{ID: "h", Role: roleAssistant, Done: true, Content: "### Tie the request to a context\n\nBuild the request."})
	hs, _ := searchDocs([]findDoc{heading}, "context", 5)
	if len(hs) != 1 || !strings.HasSuffix(hs[0].Before, "a ") || !strings.HasPrefix(hs[0].After, " ") {
		t.Fatalf("snippet %q | %q | %q text %q", hs[0].Before, hs[0].Match, hs[0].After, heading.Text)
	}
	// Case folding is Unicode simple fold, not byte identity.
	if len(findFold("Context", "context")) != 1 || len(findFold("Σigma", "σ")) != 1 {
		t.Fatal("case fold missed")
	}
}

func TestFindSkipsCollapsedThinkingAndStreaming(t *testing.T) {
	closed := renderDoc(findSrc{ID: "a", Role: roleAssistant, Done: true, Open: false, Reasoning: "hidden token zzthink", Content: "visible"})
	if strings.Contains(closed.Text, "zzthink") || len(findFold(closed.Text, "zzthink")) != 0 {
		t.Fatalf("collapsed thinking was indexed: %q", closed.Text)
	}
	open := renderDoc(findSrc{ID: "a", Role: roleAssistant, Done: true, Open: true, Reasoning: "hidden token zzthink", Content: "visible"})
	if len(findFold(open.Text, "zzthink")) != 1 {
		t.Fatalf("open thinking missing: %q", open.Text)
	}
	live := renderDoc(findSrc{ID: "s", Role: roleAssistant, Done: false, Content: "partial zzlive token"})
	if live.Text != "" || len(findFold(live.Text, "zzlive")) != 0 {
		t.Fatalf("streaming reply was indexed: %+v", live)
	}
	done := renderDoc(findSrc{ID: "s", Role: roleAssistant, Done: true, Content: "partial zzlive token"})
	if len(findFold(done.Text, "zzlive")) != 1 {
		t.Fatal("finished reply was not indexed")
	}
}

func TestFindCapsAtOneThousand(t *testing.T) {
	var docs []findDoc
	for i := 0; i < 1001; i++ {
		docs = append(docs, findDoc{ID: itoa(i), Text: "zzcap"})
	}
	hits, capped := searchDocs(docs, "zzcap", findCap)
	if !capped || len(hits) != findCap {
		t.Fatalf("cap capped=%v len=%d", capped, len(hits))
	}
	if hits[0].MsgID != "0" || hits[len(hits)-1].MsgID != "999" {
		t.Fatalf("cap kept %s..%s", hits[0].MsgID, hits[len(hits)-1].MsgID)
	}
	exact, over := searchDocs(docs[:1000], "zzcap", findCap)
	if over || len(exact) != 1000 {
		t.Fatalf("exact 1000 capped=%v len=%d", over, len(exact))
	}
}

func TestFindMenuAndShortcutConflicts(t *testing.T) {
	a := newTestApp(t)
	a.data.Conversations = []Conversation{{
		ID: "c", Title: "Chat", Config: DefaultConfig(),
		Messages: []Message{{ID: "u", Role: roleUser, Content: "hello context", Done: true, CreatedAt: time.Now()}},
	}}
	a.data.ActiveID = "c"
	menu := a.menu()
	got := map[string]string{}
	leaf := func(path string) string {
		if i := strings.LastIndex(path, " > "); i >= 0 {
			return path[i+3:]
		}
		return path
	}
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
				if prev, ok := got[it.Accelerator]; ok && leaf(prev) != leaf(name) {
					t.Fatalf("accelerator %s on %q and %q", it.Accelerator, prev, name)
				}
				got[it.Accelerator] = name
			}
			if len(it.Submenu) > 0 {
				walk(it.Submenu, name)
			}
		}
	}
	walk(menu.Items(), "")
	want := map[string]string{
		"CmdOrCtrl+N":       "New Chat",
		"CmdOrCtrl+,":       "Settings…",
		"Alt+CmdOrCtrl+,":   "Chat Settings…",
		"Shift+CmdOrCtrl+N": "New Session",
		"CmdOrCtrl+R":       "Regenerate",
		"CmdOrCtrl+.":       "Stop",
		"CmdOrCtrl+F":       "Find…",
		"CmdOrCtrl+G":       "Find Next",
		"Shift+CmdOrCtrl+G": "Find Previous",
	}
	for accel, label := range want {
		if !strings.Contains(got[accel], label) {
			t.Fatalf("%s should be %s, menu has %q (all %v)", accel, label, got[accel], got)
		}
	}
	if _, ok := got["CmdOrCtrl+K"]; ok {
		t.Fatal("⌘K must stay Search Chats, not a menu find command")
	}
	// View-only bindings that must not reuse a menu accelerator.
	viewOnly := []string{"CmdOrCtrl+K", "Up", "Enter", "Shift+Enter", "CmdOrCtrl+Backspace"}
	for _, accel := range viewOnly {
		if _, ok := got[accel]; ok && accel != "CmdOrCtrl+Backspace" {
			t.Fatalf("view shortcut %s collides with menu %s", accel, got[accel])
		}
	}
	if a.findItem == nil || a.findItem.Label != "Find…" || a.findNextItem.Label != "Find Next" || a.findPrevItem.Label != "Find Previous" {
		t.Fatalf("menu items %+v %+v %+v", a.findItem, a.findNextItem, a.findPrevItem)
	}
	for _, path := range got {
		low := strings.ToLower(path)
		if strings.Contains(low, "replace") || strings.Contains(low, "use selection") || strings.Contains(low, "whole word") || strings.Contains(low, "case sensitive") {
			t.Fatalf("find menu has an extra command: %s", path)
		}
	}
	tt := ui.NewTester(a.view, 1120, 760)
	tt.Frame()
	if !a.findItem.IsEnabled() || a.findNextItem.IsEnabled() || a.findPrevItem.IsEnabled() {
		t.Fatalf("before open find=%v next=%v prev=%v", a.findItem.IsEnabled(), a.findNextItem.IsEnabled(), a.findPrevItem.IsEnabled())
	}
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Frame()
	if !a.finder.open || !tt.Focused("Find in chat") {
		t.Fatalf("⌘F open=%v focused=%v texts=%q", a.finder.open, tt.Focused("Find in chat"), tt.Texts())
	}
	tt.Type("context")
	waitFind(t, a)
	tt.Frame()
	if !a.findNextItem.IsEnabled() || !a.findPrevItem.IsEnabled() {
		t.Fatal("find next/prev stayed disabled while the bar is open")
	}
	// ⌘K still focuses chat search and does not close find.
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Frame()
	if !tt.Focused("Search") || !a.finder.open {
		t.Fatal("⌘K no longer focuses Search Chats")
	}
	// ⇧⌘N still inserts a session break.
	tt.Key(ui.Cmd|ui.Shift, ui.KeyN)
	tt.Frame()
	if len(a.active().Breaks) != 1 {
		t.Fatalf("⇧⌘N breaks %+v", a.active().Breaks)
	}
	empty := newTestApp(t)
	empty.menu()
	empty.newChat()
	ui.NewTester(empty.view, 800, 600).Frame()
	if empty.findItem.IsEnabled() {
		t.Fatal("Find stayed enabled on an empty chat")
	}
}

func TestFindBarPlateHidesTranscript(t *testing.T) {
	light := findBarPlate(themes.ContentFlat("warm", false), themes.Palette("warm", false).LayerComposer)
	dark := findBarPlate(themes.ContentFlat("warm", true), themes.Palette("warm", true).LayerComposer)
	if light.A != 255 || dark.A != 255 {
		t.Fatalf("plate alpha light %d dark %d", light.A, dark.A)
	}
	// A line of glyphs wide enough to run under the bar, past the search field.
	line := strings.Repeat("H", 80)
	a := newTestApp(t)
	now := time.Now()
	msgs := []Message{{ID: "u", Role: roleUser, Content: line, Done: true, CreatedAt: now}}
	for i := 0; i < 30; i++ {
		msgs = append(msgs, Message{ID: "m" + itoa(i), Role: roleUser, Content: line + " " + itoa(i), Done: true, CreatedAt: now})
	}
	a.data.Conversations = []Conversation{{ID: "c", Title: "Chat", Config: DefaultConfig(), Messages: msgs}}
	a.data.ActiveID = "c"
	for _, darkMode := range []bool{false, true} {
		tt := ui.NewTester(a.view, 1120, 760)
		if darkMode {
			tt.SetDark(true)
		}
		tt.Frame()
		tt.Key(ui.Cmd, ui.KeyF)
		tt.Frame()
		bar, ok := tt.Find("find bar")
		if !ok {
			t.Fatal("find bar missing")
		}
		field, ok := tt.Find("Find in chat")
		if !ok {
			t.Fatal("find field missing")
		}
		// Nudge the transcript so a wide line crosses the bar, then sample
		// that line where the search field does not cover it.
		var msg ui.Rect
		found := false
		for step := 0; step < 8 && !found; step++ {
			if step > 0 {
				tt.Scroll(bar.X+bar.W/2, bar.Y+100, 0, 180)
				tt.Frame()
			}
			for _, s := range tt.Texts() {
				if !strings.HasPrefix(s, line) {
					continue
				}
				r, ok := tt.Find(s)
				if !ok || r.Y >= bar.Y+bar.H || r.Y+r.H <= bar.Y+4 {
					continue
				}
				msg = r
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("dark %v no transcript line sits under the bar %+v", darkMode, bar)
		}
		img := tt.Image()
		x := int(field.X + field.W + 48)
		if float32(x) < msg.X+8 || float32(x) > msg.X+msg.W-8 {
			x = int(msg.X + msg.W*0.75)
		}
		y := int(bar.Y + bar.H/2)
		if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
			t.Fatalf("sample %d,%d outside %v", x, y, img.Bounds())
		}
		c := img.RGBAAt(x, y)
		plate := findBarPlate(themes.ContentFlat(a.data.Theme, darkMode), a.look.LayerComposer)
		if d := plateDist(c, plate); d > 18 {
			t.Fatalf("dark %v bar pixel %d,%d = #%02x%02x%02x, plate #%02x%02x%02x (Δ %d) — transcript is showing through", darkMode, x, y, c.R, c.G, c.B, plate.R, plate.G, plate.B, d)
		}
	}
}

func plateDist(c color.RGBA, p ui.Color) int {
	dr := int(c.R) - int(p.R)
	dg := int(c.G) - int(p.G)
	db := int(c.B) - int(p.B)
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

func TestFindKeyboardAndEmpty(t *testing.T) {
	a := newTestApp(t)
	now := time.Now()
	a.data.Conversations = []Conversation{{
		ID: "c", Title: "Chat", Config: DefaultConfig(),
		Messages: []Message{
			{ID: "u0", Role: roleUser, Content: "first context here", Done: true, CreatedAt: now},
			{ID: "a0", Role: roleAssistant, Content: "second context there", Done: true, CreatedAt: now},
			{ID: "u1", Role: roleUser, Content: "third context end", Done: true, CreatedAt: now},
		},
	}}
	a.data.ActiveID = "c"
	a.menu()
	tt := ui.NewTester(a.view, 1120, 760)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Frame()
	tt.Type("context")
	waitFind(t, a)
	tt.Frame()
	if len(a.finder.hits) != 3 || a.findCount() != "1 of 3" {
		t.Fatalf("hits %d count %q", len(a.finder.hits), a.findCount())
	}
	if !a.finder.listOpen || !tt.HasText("find-row-0") {
		t.Fatalf("list closed: %q", tt.Texts())
	}
	if !a.finder.focused || !tt.Focused("Find in chat") {
		t.Fatalf("field focus finder=%v tester=%v", a.finder.focused, tt.Focused("Find in chat"))
	}
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if a.finder.sel != 1 || a.findCount() != "2 of 3" {
		t.Fatalf("down sel=%d count=%q", a.finder.sel, a.findCount())
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	for i := 0; i < 8 && !a.finder.settled; i++ {
		tt.Frame()
	}
	if a.finder.listOpen || a.finder.cur != 1 {
		t.Fatalf("enter list=%v cur=%d", a.finder.listOpen, a.finder.cur)
	}
	tt.Key(ui.Cmd, ui.KeyG)
	tt.Frame()
	if a.finder.cur != 2 {
		t.Fatalf("⌘G cur=%d", a.finder.cur)
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyG)
	tt.Key(ui.Cmd|ui.Shift, ui.KeyG)
	tt.Frame()
	if a.finder.cur != 0 {
		t.Fatalf("⇧⌘G cur=%d", a.finder.cur)
	}
	// Wrap.
	tt.Key(ui.Cmd|ui.Shift, ui.KeyG)
	tt.Frame()
	if a.finder.cur != 2 {
		t.Fatalf("wrap cur=%d", a.finder.cur)
	}
	yBefore := 0
	if r, ok := tt.Find("third context end"); ok {
		yBefore = int(r.Y)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.finder.open || tt.HasText("find bar") {
		t.Fatal("escape left the bar open")
	}
	if r, ok := tt.Find("third context end"); ok && yBefore != 0 && int(r.Y) != yBefore {
		t.Fatalf("escape moved the message from %d to %.0f", yBefore, r.Y)
	}
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Frame()
	tt.Type("zzqx")
	waitFind(t, a)
	tt.Frame()
	if a.findCount() != "No results" || !tt.HasText("No results") {
		t.Fatalf("empty count %q texts %q", a.findCount(), tt.Texts())
	}
	if err := tt.Click("Done"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.finder.open {
		t.Fatal("Done left the bar open")
	}
}

func TestFindLongChatJumpCentersEarliest(t *testing.T) {
	const n = 2000
	a := newTestApp(t)
	now := time.Now()
	msgs := make([]Message, n)
	for i := 0; i < n; i++ {
		body := "later note " + itoa(i) + " mentions zxfind once"
		if i == 0 {
			body = "alpha zxfind omega"
		}
		msgs[i] = Message{ID: "m" + itoa(i), Role: roleUser, Content: body, Done: true, CreatedAt: now}
	}
	a.data.Conversations = []Conversation{{
		ID: "long", Title: "Long", Config: DefaultConfig(), Messages: msgs,
	}}
	a.data.ActiveID = "long"
	a.menu()
	tt := ui.NewTester(a.view, 1120, 760)
	tt.Frame()
	start := time.Now()
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Frame()
	if time.Since(start) > 50*time.Millisecond {
		t.Fatalf("opening find blocked the main thread for %s", time.Since(start))
	}
	if !a.finder.open {
		t.Fatal("find did not open")
	}
	tt.Type("zxfind")
	waitFind(t, a)
	tt.Frame()
	if !a.finder.capped || len(a.finder.hits) != findCap || a.findCount() != "1 of 1000+" {
		t.Fatalf("count %q hits %d capped %v", a.findCount(), len(a.finder.hits), a.finder.capped)
	}
	if a.finder.hits[0].MsgID != "m0" {
		t.Fatalf("earliest hit %s", a.finder.hits[0].MsgID)
	}
	if err := tt.Click("find-row-0"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for i := 0; i < 8 && !a.finder.settled; i++ {
		tt.Frame()
	}
	_, onScreen := tt.Find("alpha zxfind omega")
	if !a.finder.settled || a.finder.cur != 0 || !onScreen {
		t.Fatalf("settled %v cur %d nudges %d fresh %v mark %.1f lift %.1f onScreen %v jump %s", a.finder.settled, a.finder.cur, a.finder.nudges, a.finder.markFresh, a.finder.markY, a.finder.lift, onScreen, a.finder.jumpMsg)
	}
	bar, ok := tt.Find("find bar")
	if !ok {
		t.Fatal("find bar missing")
	}
	comp, ok := tt.Find("composer")
	if !ok {
		t.Fatal("composer missing")
	}
	want := (bar.Y + bar.H + comp.Y) / 2
	got := a.finder.markY + a.finder.markH/2
	if got < want-4 || got > want+4 {
		t.Fatalf("earliest hit center %.2f, visual center %.2f (bar %.1f composer %.1f)", got, want, bar.Y+bar.H, comp.Y)
	}
	el, ok := tt.Find("alpha zxfind omega")
	if !ok {
		t.Fatalf("target text not on screen: %q", tt.Texts())
	}
	lay := ui.ShapeText("alpha zxfind omega", ui.Font{Size: 14}, el.W-24)
	rects := lay.SelectionRects(ui.TextInputRange{Start: utf16Len("alpha "), End: utf16Len("alpha zxfind")})
	if len(rects) == 0 {
		t.Fatal("no glyph rect")
	}
	wantX := el.X + 12 + rects[0].X - 1
	if a.finder.markX < wantX-4 || a.finder.markX > wantX+4 {
		t.Fatalf("highlight x %.2f, glyph x %.2f (el %.1f glyph %.1f)", a.finder.markX, wantX, el.X, rects[0].X)
	}
	// The viewport shows the target, not a blank gap where the row should be.
	if el.Y < bar.Y || el.Y > comp.Y {
		t.Fatalf("target %.1f is outside the band %.1f..%.1f", el.Y, bar.Y, comp.Y)
	}
}

// TestFindRingMatchesFill checks the current-match stroke against the fill
// when the match follows inline code, bold, and a link on one line. The
// stroke used to be shaped in the body font, so a monospace prefix slid it
// about 12pt left of the fill.
func TestFindRingMatchesFill(t *testing.T) {
	dir := testArtifactDir(t, "find-r3")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r4 := testArtifactDir(t, "find-r4")
	if err := os.MkdirAll(r4, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		body   string
		query  string
		names  []string
		lines  int // visual segments of the one assistant hit; 0 uses names
		needle string
		wide   int
	}{
		{
			body:  "`code` hit **bold** hit [docs](https://example.com/secret) hit",
			query: "hit",
			names: []string{"code", "bold", "link"},
		},
		{
			body:  "`Client.Timeout` bounds the whole exchange. A context deadline belongs to one request and can be cancelled early.",
			query: "context",
			names: []string{"sentence"},
		},
		{
			body:  "wrapped English match continues here onto the next line of the paragraph and still has more words after the break so the highlight splits.",
			query: "wrapped English match continues here onto the next line of the paragraph and still has more words",
			names: []string{"wrap-en"},
			lines: 2,
			wide:  860,
		},
		{
			body:  strings.Repeat("填", 32) + "跨行中文匹配应当分成两段继续写完这句话。",
			query: "跨行中文匹配应当分成两段",
			names: []string{"wrap-zh"},
			lines: 2,
			wide:  860,
		},
		{
			body:  "这是一句不换行的中文句子，用来核对环和底色。",
			query: "中文",
			names: []string{"zh"},
			lines: 1,
		},
	}
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		for _, tc := range cases {
			wide := tc.wide
			if wide == 0 {
				wide = 1120
			}
			a, tt := findRingChat(t, tc.body, dark, wide)
			tt.Type(tc.query)
			waitFind(t, a)
			tt.Frame()
			var assistant []int
			for i, h := range a.finder.hits {
				if h.MsgID == "a" {
					assistant = append(assistant, i)
				}
			}
			if len(assistant) != len(tc.names) {
				t.Fatalf("%s %s hits %d, want %d (%q)", mode, tc.query, len(assistant), len(tc.names), tt.Texts())
			}
			for n, idx := range assistant {
				a.jumpFind(idx, tt.Now())
				tt.Frame()
				for step := 0; step < 8 && !a.finder.settled; step++ {
					tt.Frame()
				}
				fg := copyRGBA(tt.Image())
				savedHits, savedCur, savedQuery := a.finder.hits, a.finder.cur, a.finder.query
				a.finder.hits = nil
				a.finder.cur = -1
				a.finder.query = ""
				tt.Frame()
				bg := tt.Image()
				a.finder.hits, a.finder.cur, a.finder.query = savedHits, savedCur, savedQuery
				needle := tc.needle
				if needle == "" {
					needle = tc.query
				}
				line, ok := ringLine(tt, needle)
				if !ok {
					t.Fatalf("%s %s line missing %q", mode, tc.names[0], tt.Texts())
				}
				if tc.lines > 0 {
					blobs := shadeBlobs(fg, bg, a.look.ThemeMark, line, 0.10)
					if len(blobs) != tc.lines {
						t.Fatalf("%s %s segments %+v, want %d", mode, tc.names[0], blobs, tc.lines)
					}
					cover := blobs[0]
					for i, fill := range blobs {
						ring := shadeBoxNear(fg, bg, a.look.ThemeMark, fill, 0.40)
						if ring.W < 1 || !rectsClose(ring, fill, 2) {
							t.Fatalf("%s %s segment %d ring %+v fill %+v", mode, tc.names[0], i, ring, fill)
						}
						cover = unionRect(cover, fill)
					}
					writePNG(t, filepath.Join(r4, "find-ring-"+tc.names[0]+"-"+mode+".png"), scaleNN(cropRect(fg, cover, 10), 8))
					tt.Frame()
					break
				}
				runs := shadeRuns(fg, bg, a.look.ThemeMark, line, 0.10)
				if len(runs) != len(tc.names) {
					t.Fatalf("%s %s runs %+v, want %d", mode, tc.names[n], runs, len(tc.names))
				}
				fill := shadeBox(fg, bg, a.look.ThemeMark, line, runs[n], 0.10)
				ring := shadeBox(fg, bg, a.look.ThemeMark, line, runs[n], 0.40)
				if ring.W < 1 || !rectsClose(ring, fill, 2) {
					t.Fatalf("%s after %s ring %+v fill %+v", mode, tc.names[n], ring, fill)
				}
				writePNG(t, filepath.Join(dir, "find-ring-"+tc.names[n]+"-"+mode+".png"), scaleNN(cropRect(fg, unionRect(ring, fill), 10), 8))
				tt.Frame()
			}
		}
	}
}

func findRingChat(t *testing.T, body string, dark bool, wide int) (*App, *ui.Tester) {
	t.Helper()
	a := newTestApp(t)
	now := time.Now()
	msgs := make([]Message, 0, 26)
	for i := 0; i < 12; i++ {
		msgs = append(msgs, Message{ID: "p" + itoa(i), Role: roleUser, Content: "padding " + itoa(i), Done: true, CreatedAt: now})
	}
	msgs = append(msgs, Message{ID: "a", Role: roleAssistant, Content: body, Done: true, CreatedAt: now})
	for i := 0; i < 12; i++ {
		msgs = append(msgs, Message{ID: "q" + itoa(i), Role: roleUser, Content: "later " + itoa(i), Done: true, CreatedAt: now})
	}
	a.data.Conversations = []Conversation{{ID: "c", Title: "Chat", Config: DefaultConfig(), Messages: msgs}}
	a.data.ActiveID = "c"
	tt := ui.NewTester(a.view, wide, 760)
	if dark {
		tt.SetDark(true)
	}
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Frame()
	return a, tt
}

func ringLine(tt *ui.Tester, needle string) (ui.Rect, bool) {
	var best ui.Rect
	found := false
	for _, s := range tt.Texts() {
		if !strings.Contains(s, needle) {
			continue
		}
		r, ok := tt.Find(s)
		if !ok || r.W < 40 || r.H < 8 {
			continue
		}
		if !found || r.W*r.H > best.W*best.H {
			best = r
			found = true
		}
	}
	return best, found
}

// shadeBlobs is each separated highlight in el: one box per line of a
// wrapped match, and one box per match when several sit on one line.
func shadeBlobs(fg, bg *image.RGBA, mark ui.Color, el ui.Rect, lo float32) []ui.Rect {
	b := fg.Bounds()
	x0 := int(el.X) - 4
	y0 := int(el.Y) - 4
	x1 := int(el.X+el.W) + 4
	y1 := int(el.Y+el.H) + 4
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
	w := x1 - x0
	h := y1 - y0
	if w <= 0 || h <= 0 {
		return nil
	}
	on := make([]bool, w*h)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if flashCoverAlpha(fg.RGBAAt(x, y), bg.RGBAAt(x, y), mark) >= lo {
				on[(y-y0)*w+(x-x0)] = true
			}
		}
	}
	seen := make([]bool, len(on))
	var blobs []ui.Rect
	dirs := [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	for i := range on {
		if !on[i] || seen[i] {
			continue
		}
		minX, minY := i%w, i/w
		maxX, maxY := minX, minY
		n := 0
		stack := []int{i}
		seen[i] = true
		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			cx, cy := cur%w, cur/w
			n++
			if cx < minX {
				minX = cx
			}
			if cy < minY {
				minY = cy
			}
			if cx > maxX {
				maxX = cx
			}
			if cy > maxY {
				maxY = cy
			}
			for _, d := range dirs {
				nx, ny := cx+d[0], cy+d[1]
				if nx < 0 || ny < 0 || nx >= w || ny >= h {
					continue
				}
				j := ny*w + nx
				if !on[j] || seen[j] {
					continue
				}
				seen[j] = true
				stack = append(stack, j)
			}
		}
		if n < 40 || maxX-minX < 8 || maxY-minY < 8 {
			continue
		}
		blobs = append(blobs, ui.Rect{
			X: float32(x0 + minX), Y: float32(y0 + minY),
			W: float32(maxX-minX) + 1, H: float32(maxY-minY) + 1,
		})
	}
	return blobs
}

// shadeRuns finds each highlight along the middle of the line. A gap wider
// than 12px starts the next match.
func shadeRuns(fg, bg *image.RGBA, mark ui.Color, el ui.Rect, lo float32) []ui.Rect {
	b := fg.Bounds()
	var best []ui.Rect
	for y := int(el.Y); y < int(el.Y+el.H); y++ {
		if y < b.Min.Y || y >= b.Max.Y {
			continue
		}
		var runs []ui.Rect
		start, end, gap := -1, -1, 0
		flush := func() {
			if start >= 0 && end-start+1 >= 8 {
				runs = append(runs, ui.Rect{X: float32(start), W: float32(end-start) + 1})
			}
			start, end, gap = -1, -1, 0
		}
		for x := int(el.X); x < int(el.X+el.W); x++ {
			if x < b.Min.X || x >= b.Max.X {
				continue
			}
			if flashCoverAlpha(fg.RGBAAt(x, y), bg.RGBAAt(x, y), mark) >= lo {
				if start < 0 {
					start = x
				}
				end = x
				gap = 0
				continue
			}
			if start < 0 {
				continue
			}
			gap++
			if gap > 12 {
				flush()
			}
		}
		flush()
		if len(runs) > len(best) {
			best = runs
		}
	}
	return best
}

// shadeBoxNear is the box of pixels at or above lo just around one segment,
// so a wrapped match's other line is not folded in.
func shadeBoxNear(fg, bg *image.RGBA, mark ui.Color, around ui.Rect, lo float32) ui.Rect {
	minX, minY := 1<<30, 1<<30
	maxX, maxY := -1, -1
	b := fg.Bounds()
	for y := int(around.Y) - 1; y <= int(around.Y+around.H)+1; y++ {
		for x := int(around.X) - 2; x <= int(around.X+around.W)+2; x++ {
			if x < b.Min.X || y < b.Min.Y || x >= b.Max.X || y >= b.Max.Y {
				continue
			}
			if flashCoverAlpha(fg.RGBAAt(x, y), bg.RGBAAt(x, y), mark) < lo {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if maxX < 0 {
		return ui.Rect{}
	}
	return ui.Rect{X: float32(minX), Y: float32(minY), W: float32(maxX-minX) + 1, H: float32(maxY-minY) + 1}
}

// shadeBox is the box of pixels at or above lo whose x lies on run.
func shadeBox(fg, bg *image.RGBA, mark ui.Color, el, run ui.Rect, lo float32) ui.Rect {
	minX, minY := 1<<30, 1<<30
	maxX, maxY := -1, -1
	b := fg.Bounds()
	x0 := int(run.X) - 2
	x1 := int(run.X+run.W) + 2
	for y := int(el.Y) - 6; y < int(el.Y+el.H)+8; y++ {
		for x := x0; x <= x1; x++ {
			if x < b.Min.X || y < b.Min.Y || x >= b.Max.X || y >= b.Max.Y {
				continue
			}
			if flashCoverAlpha(fg.RGBAAt(x, y), bg.RGBAAt(x, y), mark) < lo {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if maxX < 0 {
		return ui.Rect{}
	}
	return ui.Rect{X: float32(minX), Y: float32(minY), W: float32(maxX-minX) + 1, H: float32(maxY-minY) + 1}
}

func rectsClose(a, b ui.Rect, slop float32) bool {
	d := func(x, y float32) float32 {
		if x < y {
			return y - x
		}
		return x - y
	}
	return d(a.X, b.X) <= slop && d(a.Y, b.Y) <= slop &&
		d(a.X+a.W, b.X+b.W) <= slop && d(a.Y+a.H, b.Y+b.H) <= slop
}

func cropRect(src *image.RGBA, r ui.Rect, pad int) *image.RGBA {
	b := src.Bounds()
	x0 := int(r.X) - pad
	y0 := int(r.Y) - pad
	x1 := int(r.X+r.W) + pad
	y1 := int(r.Y+r.H) + pad
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
	out := image.NewRGBA(image.Rect(0, 0, x1-x0, y1-y0))
	for y := y0; y < y1; y++ {
		copy(out.Pix[(y-y0)*out.Stride:(y-y0)*out.Stride+(x1-x0)*4], src.Pix[y*src.Stride+x0*4:y*src.Stride+x1*4])
	}
	return out
}

func scaleNN(src *image.RGBA, n int) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx()*n, b.Dy()*n))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			c := src.RGBAAt(x, y)
			for dy := 0; dy < n; dy++ {
				for dx := 0; dx < n; dx++ {
					dst.SetRGBA(x*n+dx, y*n+dy, c)
				}
			}
		}
	}
	return dst
}

func utf16Len(s string) int {
	return utf16Off(s, len(s))
}

func TestFindFieldWidth(t *testing.T) {
	now := time.Now()
	cases := []struct {
		w, h int
		want float32
		slop float32
	}{
		{1120, 760, 420, 0.5},
		{720, 520, 320, 8},
	}
	for _, tc := range cases {
		a := newTestApp(t)
		a.data.Conversations = []Conversation{{
			ID: "c", Title: "Chat", Config: DefaultConfig(),
			Messages: []Message{{ID: "u", Role: roleUser, Content: "hello", Done: true, CreatedAt: now}},
		}}
		a.data.ActiveID = "c"
		tt := ui.NewTester(a.view, tc.w, tc.h)
		tt.Frame()
		tt.Key(ui.Cmd, ui.KeyF)
		tt.Frame()
		field, ok := tt.Find("find field")
		if !ok {
			t.Fatalf("%d find field missing", tc.w)
		}
		if field.W < tc.want-tc.slop || field.W > tc.want+tc.slop {
			t.Fatalf("%d-wide field is %.2f, want %.0f ± %.0f", tc.w, field.W, tc.want, tc.slop)
		}
	}
}

// TestFindFlashPeakAlpha checks the jump wash is one layer: 14% light / 8%
// dark. A second outset on the assistant block that holds the current match
// used to stack to about 26% / 15%.
func TestFindFlashPeakAlpha(t *testing.T) {
	reply := "context sits in this reply"
	for _, dark := range []bool{false, true} {
		a := newTestApp(t)
		now := time.Now()
		msgs := make([]Message, 0, 42)
		for i := 0; i < 20; i++ {
			msgs = append(msgs, Message{ID: "p" + itoa(i), Role: roleUser, Content: "padding line " + itoa(i), Done: true, CreatedAt: now})
		}
		msgs = append(msgs,
			Message{ID: "u", Role: roleUser, Content: "please find context", Done: true, CreatedAt: now},
			Message{ID: "a", Role: roleAssistant, Content: reply, Done: true, CreatedAt: now},
		)
		for i := 0; i < 20; i++ {
			msgs = append(msgs, Message{ID: "q" + itoa(i), Role: roleUser, Content: "later line " + itoa(i), Done: true, CreatedAt: now})
		}
		a.data.Conversations = []Conversation{{
			ID: "c", Title: "Chat", Config: DefaultConfig(),
			Messages: msgs,
		}}
		a.data.ActiveID = "c"
		tt := ui.NewTester(a.view, 1120, 760)
		if dark {
			tt.SetDark(true)
		}
		tt.Frame()
		tt.Key(ui.Cmd, ui.KeyF)
		tt.Frame()
		tt.Type("context")
		waitFind(t, a)
		tt.Frame()
		if !jumpFindMsg(t, a, tt, "a") {
			t.Fatalf("dark %v no assistant hit", dark)
		}
		r, ok := tt.Find(reply)
		if !ok {
			t.Fatalf("dark %v reply not on screen: %q", dark, tt.Texts())
		}
		// Right of the glyphs, still inside the reply, where a second
		// block wash would stack on the whole-reply flash.
		x := int(r.X + r.W*0.75)
		y := int(r.Y + r.H/2)
		// Image() aliases the tester's buffer, so copy before the next frame.
		fg := copyRGBA(tt.Image()).RGBAAt(x, y)
		a.finder.flashID = ""
		a.finder.flashAt = time.Time{}
		tt.Frame()
		bg := tt.Image().RGBAAt(x, y)
		got := flashCoverAlpha(fg, bg, a.look.ThemeMark)
		want := float32(findFlashA(dark))
		if got < want-0.03 || got > want+0.03 {
			t.Fatalf("dark %v assistant peak alpha %.3f, want %.2f (fg #%02x%02x%02x bg #%02x%02x%02x at %d,%d)", dark, got, want, fg.R, fg.G, fg.B, bg.R, bg.G, bg.B, x, y)
		}
		if !jumpFindMsg(t, a, tt, "u") {
			t.Fatalf("dark %v no user hit", dark)
		}
		ur, ok := tt.Find("please find context")
		if !ok {
			t.Fatalf("dark %v user bubble missing", dark)
		}
		// Just above the bubble, inside the 8pt outset and outside its fill.
		ux := int(ur.X + ur.W/2)
		uy := int(ur.Y) - 4
		ufg := copyRGBA(tt.Image()).RGBAAt(ux, uy)
		a.finder.flashID = ""
		a.finder.flashAt = time.Time{}
		tt.Frame()
		ubg := tt.Image().RGBAAt(ux, uy)
		ugot := flashCoverAlpha(ufg, ubg, a.look.ThemeMark)
		if ugot < want-0.03 || ugot > want+0.03 {
			t.Fatalf("dark %v user peak alpha %.3f, want %.2f (fg #%02x%02x%02x bg #%02x%02x%02x at %d,%d)", dark, ugot, want, ufg.R, ufg.G, ufg.B, ubg.R, ubg.G, ubg.B, ux, uy)
		}
	}
}

func copyRGBA(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}

func jumpFindMsg(t *testing.T, a *App, tt *ui.Tester, id string) bool {
	t.Helper()
	for i, h := range a.finder.hits {
		if h.MsgID != id {
			continue
		}
		a.jumpFind(i, tt.Now())
		tt.Frame()
		for n := 0; n < 8 && !a.finder.settled; n++ {
			tt.Frame()
		}
		return true
	}
	return false
}

// flashCoverAlpha is how much of mark is painted over bg to produce fg,
// weighted toward the channel where mark and bg differ most.
func flashCoverAlpha(fg, bg color.RGBA, mark ui.Color) float32 {
	var sum, w float32
	pairs := [3][3]float32{
		{float32(fg.R), float32(bg.R), float32(mark.R)},
		{float32(fg.G), float32(bg.G), float32(mark.G)},
		{float32(fg.B), float32(bg.B), float32(mark.B)},
	}
	for _, p := range pairs {
		span := p[2] - p[1]
		if span < 0 {
			span = -span
		}
		if span < 12 {
			continue
		}
		sum += (p[0] - p[1]) / (p[2] - p[1]) * span
		w += span
	}
	if w == 0 {
		return 0
	}
	return sum / w
}

func waitFind(t *testing.T, a *App) {
	t.Helper()
	dead := time.Now().Add(3 * time.Second)
	for time.Now().Before(dead) {
		a.pollFind()
		if a.finder.ready {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("find index was not ready")
}
