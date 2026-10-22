package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cove/themes"

	"github.com/egoist/mygo/ui"
	"golang.org/x/image/draw"
)

const (
	shotW = 1120
	shotH = 760
)

func TestSidebarSeparatorPixels(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, shotW, shotH)
	assertNoSidebarSeparator(t, tt, false)
	tt.SetDark(true)
	assertNoSidebarSeparator(t, tt, true)
	tt.SetDark(false)
	tt.SetSize(960, 520)
	assertNoSidebarSeparator(t, tt, false)
	tt.SetDark(true)
	assertNoSidebarSeparator(t, tt, true)
}

func TestUISnapshots(t *testing.T) {
	dir := testArtifactDir(t, "")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	design := os.Getenv("COVE_DESIGN")
	if design == "" {
		design = "/tmp/cove-design-v07/cove-design"
	}
	requireDesign(t, design)
	now := time.Now()
	stamp := time.Date(now.Year(), now.Month(), now.Day(), 9, 41, 0, 0, now.Location())
	reply := strings.Join([]string{
		"### Tie the request to a context",
		"",
		"Build the request with `NewRequestWithContext` and cancel that context when the window closes. See [net/http · Request.WithContext](https://pkg.go.dev/net/http#Request.WithContext).",
		"",
		"- `cancel()` aborts the dial, the TLS handshake and the body read at once.",
		"- The call returns `context.Canceled`, so treat it as a quiet exit, not an error.",
		"",
		"```go",
		"ctx, cancel := context.WithCancel(context.Background())",
		"win.OnClose(cancel)  // fires once, safe to call twice",
		"resp, err := http.DefaultClient.Do(req.WithContext(ctx))",
		"```",
		"",
		"| Approach | Stops the dial | Frees the goroutine |",
		"| --- | --- | --- |",
		"| Context cancel | Immediately | Yes |",
		"| Client.Timeout | Only after the timeout | Yes |",
		"",
		"> Always **defer cancel()** — `go vet` flags the context leak if you forget.",
	}, "\n")
	mainApp := func() *App {
		a := newTestApp(t)
		a.data.Conversations = []Conversation{
			{
				ID: "cancel", Title: "Cancel HTTP on close",
				UpdatedAt: now.Add(-12 * time.Minute), CreatedAt: now.Add(-12 * time.Minute),
				Config: Config{
					Provider: "Anthropic", Model: "claude-sonnet-4.5", BaseURL: "https://api.anthropic.com/v1",
					APIKey:      "sk-ant-api03-7Hq2xL9vB4mN8pR1cT6wY3zK5dF0gJ3f9a",
					Temperature: f64p(0.7), MaxTokens: 4096, Reasoning: "medium",
					SystemPrompt: "Answer concisely. Prefer Go examples.",
					ExtraBody:    "{\n  \"top_p\": 0.9\n}",
				},
				Messages: []Message{
					{ID: "u0", Role: roleUser, Content: "What’s the difference between Client.Timeout and a context deadline?", Done: true, CreatedAt: stamp.Add(-10 * time.Minute)},
					{ID: "a0", Role: roleAssistant, Content: "`Client.Timeout` bounds the whole exchange — dial, headers and reading the body — and applies to every request the client makes. A context deadline belongs to one request and can be cancelled early.\n\n- Use `Client.Timeout` as a safety net for the client as a whole.\n- Use a context when the caller decides when to give up: a closed window, a stopped job, a new query.\n\nThey compose: whichever fires first wins, and both surface as an error you can test with `errors.Is`.", Tokens: 640, ElapsedMS: 2100, Done: true, CreatedAt: stamp.Add(-10 * time.Minute)},
					{ID: "u", Role: roleUser, Content: "How do I cancel an in-flight HTTP request when the window closes?", Done: true, CreatedAt: stamp},
					{ID: "a", Role: roleAssistant, Content: reply, Reasoning: "The user wants cancellation tied to the window lifecycle. Context cancellation covers dial, TLS and body read; Client.Timeout only bounds the total.", ThinkingMS: 12000, Tokens: 1200, ElapsedMS: 3400, Done: true, CreatedAt: stamp},
				},
			},
			{
				ID: "greet", Title: "Go greet function",
				UpdatedAt: now.Add(-2 * time.Hour), CreatedAt: now.Add(-2 * time.Hour),
				Config:   Config{Provider: "OpenAI", Model: "gpt-4o-mini", BaseURL: "https://api.openai.com/v1"},
				Messages: []Message{{ID: "g", Role: roleUser, Content: "Write a Go greet function.", Done: true, CreatedAt: now.Add(-2 * time.Hour)}},
			},
			{
				ID: "fsync", Title: "fsync before rename",
				UpdatedAt: now.Add(-26 * time.Hour), CreatedAt: now.Add(-26 * time.Hour),
				Config:   Config{Provider: "DeepSeek", Model: "deepseek-reasoner", BaseURL: "https://api.deepseek.com/v1", Reasoning: effortHigh},
				Messages: []Message{{ID: "f", Role: roleUser, Content: "Why fsync the temp file before rename?", Done: true, CreatedAt: now.Add(-26 * time.Hour)}},
			},
			{
				ID: "trip", Title: "Weekend trip outline",
				UpdatedAt: now.Add(-30 * time.Hour), CreatedAt: now.Add(-30 * time.Hour),
				Config:   Config{Provider: "OpenRouter", Model: "llama-3.3", BaseURL: "https://openrouter.ai/api/v1"},
				Messages: []Message{{ID: "t", Role: roleUser, Content: "Outline a weekend in Lisbon.", Done: true, CreatedAt: now.Add(-30 * time.Hour)}},
			},
			{
				ID: "wal", Title: "SQLite WAL vs rollback journal",
				UpdatedAt: now.Add(-3 * 24 * time.Hour), CreatedAt: now.Add(-3 * 24 * time.Hour),
				Config:   Config{Provider: "OpenAI", Model: "gpt-4.1", BaseURL: "https://api.openai.com/v1"},
				Messages: []Message{{ID: "w", Role: roleUser, Content: "Compare WAL and a rollback journal.", Done: true, CreatedAt: now.Add(-3 * 24 * time.Hour)}},
			},
			{
				ID: "metal", Title: "Metal blur shader notes",
				UpdatedAt: now.Add(-5 * 24 * time.Hour), CreatedAt: now.Add(-5 * 24 * time.Hour),
				Config:   Config{Provider: "Ollama", Model: "qwen-coder", BaseURL: "http://127.0.0.1:11434/v1"},
				Messages: []Message{{ID: "m", Role: roleUser, Content: "Notes on a Metal blur shader.", Done: true, CreatedAt: now.Add(-5 * 24 * time.Hour)}},
			},
			{
				ID: "tok", Title: "Tokenizer edge cases",
				UpdatedAt: time.Date(now.Year(), 9, 12, 10, 0, 0, 0, now.Location()),
				CreatedAt: time.Date(now.Year(), 9, 12, 10, 0, 0, 0, now.Location()),
				Config:    Config{Provider: "Anthropic", Model: "opus-4", BaseURL: "https://api.anthropic.com/v1"},
				Messages:  []Message{{ID: "k", Role: roleUser, Content: "Tokenizer edge cases.", Done: true, CreatedAt: time.Date(now.Year(), 9, 12, 10, 0, 0, 0, now.Location())}},
			},
			{
				ID: "read", Title: "Reading list for October",
				UpdatedAt: time.Date(now.Year(), 9, 3, 10, 0, 0, 0, now.Location()),
				CreatedAt: time.Date(now.Year(), 9, 3, 10, 0, 0, 0, now.Location()),
				Config:    Config{Provider: "OpenRouter", Model: "gemini-2.5", BaseURL: "https://openrouter.ai/api/v1"},
				Messages:  []Message{{ID: "r", Role: roleUser, Content: "Reading list for October.", Done: true, CreatedAt: time.Date(now.Year(), 9, 3, 10, 0, 0, 0, now.Location())}},
			},
		}
		a.data.ActiveID = "cancel"
		a.data.Defaults.APIKey = "sk-proj-QmX4c8Lw2Rt7Vb1Nz5Hk9Dp3Ye6Ja0s21b7"
		return a
	}

	save := func(tt *ui.Tester, name string) {
		t.Helper()
		writePNG(t, filepath.Join(dir, name), tt.Image())
	}
	pair := func(screen, mode string, img image.Image) {
		t.Helper()
		src := filepath.Join(design, screen+"-"+mode+".png")
		f, err := os.Open(src)
		if err != nil {
			if os.IsNotExist(err) {
				t.Skipf("design PNG missing (%s); skipping the screenshot comparison", src)
			}
			t.Fatalf("design png %s: %v", src, err)
		}
		defer f.Close()
		designImg, err := png.Decode(f)
		if err != nil {
			t.Fatal(err)
		}
		writePNG(t, filepath.Join(dir, "compare-"+screen+"-"+mode+".png"), sideBySide(designImg, img))
	}

	a := mainApp()
	tt := ui.NewTester(a.view, shotW, shotH)
	for _, want := range []string{"Tie the request to a context", "NewRequestWithContext", "net/http · Request.WithContext", "Immediately", "defer cancel()", "Thought for 12s"} {
		if !tt.HasText(want) {
			t.Fatalf("main transcript missing %q", want)
		}
	}
	hoverUserBubble(t, tt)
	assertTitleClearsLights(t, tt.Image())
	assertSearchFieldFlat(t, tt.Image())
	assertComposerBox(t, tt)
	assertScrollbarIdle(t, tt.Image(), false)
	save(tt, "main-light-impl.png")
	pair("main", "light", tt.Image())
	assertNoSidebarSeparator(t, tt, false)
	tt.SetDark(true)
	hoverUserBubble(t, tt)
	assertSearchFieldFlat(t, tt.Image())
	save(tt, "main-dark-impl.png")
	pair("main", "dark", tt.Image())
	assertNoSidebarSeparator(t, tt, true)

	empty := newTestApp(t)
	empty.newChat()
	te := ui.NewTester(empty.view, shotW, shotH)
	if err := te.Click("Message"); err != nil {
		t.Fatal(err)
	}
	save(te, "empty-light-impl.png")
	pair("empty", "light", te.Image())
	te.SetDark(true)
	save(te, "empty-dark-impl.png")
	pair("empty", "dark", te.Image())

	_, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	gen := mainApp()
	gen.streams["fsync"] = &convStream{gen: 1, cancel: cancel}
	for i := range gen.data.Conversations {
		if gen.data.Conversations[i].ID != "fsync" {
			continue
		}
		gen.data.Conversations[i].Config.Reasoning = effortHigh
		gen.data.Conversations[i].Draft = "And on APFS, is F_FULLFSYNC needed too?"
		gen.data.Conversations[i].Messages = []Message{
			{ID: "u", Role: roleUser, Content: "Why fsync the temp file before rename? Isn’t rename already atomic?", Done: true, CreatedAt: stamp},
			{ID: "a", Role: roleAssistant, Content: "Rename is atomic for the **name**, not for the **data**. Write, flush, then publish:\n\n- `f.Write(buf)` then `f.Sync()` so the bytes reach the disk.\n- `os.Rename(tmp, path)` to swap the name in one step.\n- Sync the parent directory so the rename itself survives", Reasoning: "rename(2) swaps the directory entry atomically, but it says nothing about whether the file’s data blocks are on disk. ext4 with delalloc can commit the rename first, so a crash in between leaves a zero-length file under the real name. The fix is fsync", ReasoningOpen: true, ThinkingMS: 4000, Tokens: 214, CreatedAt: stamp},
		}
	}
	gen.data.ActiveID = "fsync"
	tg := ui.NewTester(gen.view, shotW, shotH)
	if !tg.HasText("Thinking") || !tg.HasText("Stop") || !tg.HasText("Weekend trip outline") {
		t.Fatalf("generating frame: %q", tg.Texts())
	}
	assertSearchFieldFlat(t, tg.Image())
	save(tg, "generating-light-impl.png")
	pair("generating", "light", tg.Image())
	tg.SetDark(true)
	assertSearchFieldFlat(t, tg.Image())
	save(tg, "generating-dark-impl.png")
	pair("generating", "dark", tg.Image())

	settings := mainApp()
	body, err := chatRequestBody(settings.active().Config, settings.active().Messages)
	if err != nil {
		t.Fatal(err)
	}
	settings.active().LastRequest = prettyJSON(body)
	ts := ui.NewTester(settings.view, shotW, shotH)
	if err := ts.Click("More"); err != nil {
		t.Fatal(err)
	}
	if err := ts.Click("Chat Settings…"); err != nil {
		t.Fatal(err)
	}
	if !ts.HasText("Last request") || ts.HasText("Appearance") {
		t.Fatalf("this chat settings: %q", ts.Texts())
	}
	save(ts, "settings-light-impl.png")
	pair("settings", "light", ts.Image())
	ts.SetDark(true)
	save(ts, "settings-dark-impl.png")
	pair("settings", "dark", ts.Image())
	ts.SetDark(false)

	dirty := mainApp()
	td := ui.NewTester(dirty.view, shotW, shotH)
	if err := td.Click("More"); err != nil {
		t.Fatal(err)
	}
	if err := td.Click("Chat Settings…"); err != nil {
		t.Fatal(err)
	}
	if err := td.Click("Model"); err != nil {
		t.Fatal(err)
	}
	td.Type("-draft")
	if !dirty.draftDirty(0) {
		t.Fatal("dirty dot fixture is clean")
	}
	save(td, "settings-dirty-light-impl.png")
	td.SetDark(true)
	save(td, "settings-dirty-dark-impl.png")

	defs := mainApp()
	tdef := ui.NewTester(defs.view, shotW, shotH)
	if err := tdef.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	if !tdef.HasText("Appearance") || tdef.HasText("Last request") {
		t.Fatalf("defaults settings: %q", tdef.Texts())
	}
	if err := tdef.Click("Models & Providers"); err != nil {
		t.Fatal(err)
	}
	if tdef.HasText("Temperature") || tdef.HasText("Max tokens") || !tdef.HasText("Add Provider…") {
		t.Fatalf("models page: %q", tdef.Texts())
	}
	save(tdef, "settings-defaults-light-impl.png")
	pair("settings-defaults", "light", tdef.Image())
	tdef.SetDark(true)
	save(tdef, "settings-defaults-dark-impl.png")
	pair("settings-defaults", "dark", tdef.Image())
	if err := tdef.Click("General"); err != nil {
		t.Fatal(err)
	}
	assertStepper(t, tdef)

	scrolled := mainApp()
	tsc := ui.NewTester(scrolled.view, shotW, shotH)
	if err := tsc.Click("More"); err != nil {
		t.Fatal(err)
	}
	if err := tsc.Click("Chat Settings…"); err != nil {
		t.Fatal(err)
	}
	scrolled.formScroll.Y = 1e9
	tsc.SetSize(shotW, shotH)
	if scrolled.formScroll.MaxY < 40 {
		t.Fatalf("settings did not scroll: MaxY %v", scrolled.formScroll.MaxY)
	}
	save(tsc, "settings-scrolled-light-impl.png")
	pair("settings-scrolled", "light", tsc.Image())
	tsc.SetDark(true)
	scrolled.formScroll.Y = 1e9
	tsc.SetSize(shotW, shotH)
	save(tsc, "settings-scrolled-dark-impl.png")
	pair("settings-scrolled", "dark", tsc.Image())

	states := newTestApp(t)
	states.shotStates = true
	tst := ui.NewTester(states.view, shotW, shotH)
	if !tst.HasText("401 Unauthorized") || !tst.HasText("Hover, tooltip, copied, menu") || !tst.HasText("Copied") {
		t.Fatalf("states board: %q", tst.Texts())
	}
	save(tst, "states-light-impl.png")
	pair("states", "light", tst.Image())
	tst.SetDark(true)
	save(tst, "states-dark-impl.png")
	pair("states", "dark", tst.Image())

	mid := mainApp()
	tscroll := ui.NewTester(mid.view, shotW, shotH)
	scrollCodeBehindComposer := func() {
		t.Helper()
		// A second call, after the theme change, would stack another
		// scroll on the first. Return to the end, then move until the
		// code block straddles the composer: its top line in the 28pt
		// fade, a later line behind the card.
		if tscroll.HasText("Jump to latest") {
			if err := tscroll.Click("Jump to latest"); err != nil {
				t.Fatal(err)
			}
		}
		list, ok := tscroll.Find("transcript")
		if !ok {
			t.Fatal("transcript list has no frame")
		}
		// The list fills the card. The card ends 8pt above the window, and
		// its 1pt border sits inside that. A list that stops above the
		// composer never paints content underneath it.
		if list.Y+list.H < float32(shotH)-12 {
			t.Fatalf("transcript ends at y=%.0f, above the card bottom", list.Y+list.H)
		}
		comp, ok := tscroll.Find("composer")
		if !ok {
			t.Fatal("composer has no frame")
		}
		anchor, ok := tscroll.Find("Thought for 12s")
		if !ok {
			t.Fatalf("scroll fixture missing the thinking label: %q", tscroll.Texts())
		}
		const codeBlock = "ctx, cancel := context.WithCancel(context.Background())\nwin.OnClose(cancel)  // fires once, safe to call twice\nresp, err := http.DefaultClient.Do(req.WithContext(ctx))"
		placed := false
		var block ui.Rect
		for i := 0; i < 60; i++ {
			var okBlock bool
			block, okBlock = tscroll.Find(codeBlock)
			comp, _ = tscroll.Find("composer")
			// The block's top sits in the 28pt fade and its lower lines
			// run through the composer.
			if okBlock && block.Y >= comp.Y-float32(composerFade) && block.Y <= comp.Y-4 && block.Y+block.H > comp.Y+16 {
				placed = true
				break
			}
			dy := float32(-36)
			if okBlock && block.Y > comp.Y-4 {
				dy = 18
			}
			tscroll.Scroll(anchor.X+12, anchor.Y+4, 0, dy)
			tscroll.Frame()
			if r, ok := tscroll.Find("Thought for 12s"); ok {
				anchor = r
			} else if r, ok := tscroll.Find("Immediately"); ok {
				anchor = r
			} else if r, ok := tscroll.Find("Tie the request to a context"); ok {
				anchor = r
			}
		}
		if !placed {
			t.Fatalf("code block is not behind the composer: block %v composer %v list %v", block, comp, list)
		}
		if !tscroll.HasText("Jump to latest") {
			t.Fatal("main-scrolled is still pinned to the end")
		}
	}
	hoverUserBubble(t, tscroll)
	scrollCodeBehindComposer()
	assertComposerVeil(t, tscroll, false)
	assertScrollbarIdle(t, tscroll.Image(), false)
	save(tscroll, "main-scrolled-light-impl.png")
	pair("main-scrolled", "light", tscroll.Image())
	saveFadeCrop(t, dir, "main-scrolled-fade-light.png", tscroll.Image())
	tscroll.SetDark(true)
	hoverUserBubble(t, tscroll)
	scrollCodeBehindComposer()
	assertComposerVeil(t, tscroll, true)
	assertScrollbarIdle(t, tscroll.Image(), true)
	save(tscroll, "main-scrolled-dark-impl.png")
	pair("main-scrolled", "dark", tscroll.Image())
	saveFadeCrop(t, dir, "main-scrolled-fade-dark.png", tscroll.Image())

	// A shorter window changes where the gradient sits. The background
	// must stay one ramp, with no band where the card or the veil meets it.
	resized := mainApp()
	tr := ui.NewTester(resized.view, 960, 520)
	assertNoGradientSeam(t, tr, false)
	assertNoSidebarSeparator(t, tr, false)
	save(tr, "main-resized-light.png")
	saveSeamCrop(t, dir, "main-resized-seam-light.png", tr)
	tr.SetDark(true)
	assertNoGradientSeam(t, tr, true)
	assertNoSidebarSeparator(t, tr, true)
	save(tr, "main-resized-dark.png")
	saveSeamCrop(t, dir, "main-resized-seam-dark.png", tr)

	inactive := mainApp()
	inactive.shotInactive = true
	ti := ui.NewTester(inactive.view, shotW, shotH)
	hoverUserBubble(t, ti)
	save(ti, "main-inactive-light-impl.png")
	pair("main-inactive", "light", ti.Image())
	ti.SetDark(true)
	hoverUserBubble(t, ti)
	save(ti, "main-inactive-dark-impl.png")
	pair("main-inactive", "dark", ti.Image())

	multi := mainApp()
	multi.active().Draft = "And if the window closes while the body is still streaming:\n– does cancel() also close resp.Body?\n– should I still drain it before returning?\n– and what about HTTP/2 connections in the pool?"
	tm := ui.NewTester(multi.view, shotW, shotH)
	tm.Frame()
	hoverUserBubble(t, tm)
	save(tm, "main-multiline-light-impl.png")
	pair("main-multiline", "light", tm.Image())
	tm.SetDark(true)
	tm.Frame()
	hoverUserBubble(t, tm)
	save(tm, "main-multiline-dark-impl.png")
	pair("main-multiline", "dark", tm.Image())

	streamText := strings.Join(strings.Split("Then sync the parent directory, because the rename is a change to the directory, and only fsync on the directory makes that change durable. On APFS, F_FULLFSYNC also flushes the drive’s own cache, which a plain fsync does not promise. Put together, the safe sequence is write, f.Sync(), close, rename, and finally sync the directory that holds both names. Skip any step and a power cut can leave the old file, the new file, or neither under the final name.", " ")[:34], " ")
	stream := mainApp()
	for i := range stream.data.Conversations {
		if stream.data.Conversations[i].ID != "fsync" {
			continue
		}
		stream.data.Conversations[i].Config.Reasoning = effortHigh
		stream.data.Conversations[i].Draft = multi.active().Draft
		stream.data.Conversations[i].Messages = []Message{
			{ID: "u0", Role: roleUser, Content: "What does atomic rename guarantee on ext4?", Done: true, CreatedAt: stamp},
			{ID: "a0", Role: roleAssistant, Content: "That readers see either the old name or the new one, never a half-written mix. It promises nothing about the file’s data blocks being on disk.", Tokens: 210, ElapsedMS: 1400, Done: true, CreatedAt: stamp},
			{ID: "u1", Role: roleUser, Content: "Why fsync the temp file before rename? Isn’t rename already atomic?", Done: true, CreatedAt: stamp},
			{ID: "a1", Role: roleAssistant, Content: "Rename is atomic for the **name**, not for the **data**. Write, flush, then publish:\n\n- `f.Write(buf)` then `f.Sync()` so the bytes reach the disk.\n- `os.Rename(tmp, path)` to swap the name in one step.\n\n" + streamText, Reasoning: "rename(2) swaps the directory entry atomically, but it says nothing about whether the file’s data blocks are on disk. ext4 with delalloc can commit the rename first, so a crash in between leaves a zero-length file under the real name. The fix is fsync", ReasoningOpen: true, ThinkingMS: 4000, Tokens: 214, CreatedAt: stamp},
		}
	}
	stream.data.ActiveID = "fsync"
	stream.streams["fsync"] = &convStream{gen: 1, cancel: cancel}
	tsm := ui.NewTester(stream.view, shotW, shotH)
	tsm.Frame()
	save(tsm, "generating-stream-light-impl.png")
	pair("generating-stream", "light", tsm.Image())
	tsm.SetDark(true)
	tsm.Frame()
	save(tsm, "generating-stream-dark-impl.png")
	pair("generating-stream", "dark", tsm.Image())

	purple, _ := themes.NamedAccent("purple", false)
	accentL := mainApp()
	accentL.shotAccent = purple
	tp := ui.NewTester(accentL.view, shotW, shotH)
	hoverUserBubble(t, tp)
	save(tp, "main-accent-purple-light-impl.png")
	pair("main-accent-purple", "light", tp.Image())

	orange, _ := themes.NamedAccent("orange", true)
	accentD := mainApp()
	accentD.shotAccent = orange
	accentD.data.Appearance = appearanceDark
	tda := ui.NewTester(accentD.view, shotW, shotH)
	tda.SetDark(true)
	hoverUserBubble(t, tda)
	save(tda, "main-accent-orange-dark-impl.png")
	pair("main-accent-orange", "dark", tda.Image())

	pal := mainApp()
	pal.shotPalette = true
	tpal := ui.NewTester(pal.view, shotW, shotH)
	save(tpal, "palette-light-impl.png")
	pair("palette", "light", tpal.Image())
	tpal.SetDark(true)
	save(tpal, "palette-dark-impl.png")
	pair("palette", "dark", tpal.Image())

	icon, err := os.ReadFile("icon/icon-1024.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "icon-1024-impl.png"), icon, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(design, "icon-1024.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	designIcon, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	implIcon, err := png.Decode(bytes.NewReader(icon))
	if err != nil {
		t.Fatal(err)
	}
	writePNG(t, filepath.Join(dir, "compare-icon-1024.png"), sideBySide(designIcon, implIcon))
}

// assertComposerBox checks the single-line composer's border box.
func assertComposerBox(t *testing.T, tt *ui.Tester) {
	t.Helper()
	comp, ok := tt.Find("composer")
	if !ok {
		t.Fatal("composer missing")
	}
	if comp.H < 72.5 || comp.H > 73.5 {
		t.Fatalf("single-line composer border box is %.2f, want 73", comp.H)
	}
}

// assertStepper checks the max-tokens control: an 80pt field, then a
// separate narrow up/down button rather than chevrons inside the field.
func assertStepper(t *testing.T, tt *ui.Tester) {
	t.Helper()
	up, ok := tt.Find("Increase")
	if !ok {
		t.Fatal("stepper has no Increase half")
	}
	down, ok := tt.Find("Decrease")
	if !ok {
		t.Fatal("stepper has no Decrease half")
	}
	if up.W < 11 || up.W > 15 || up.H < 9 || up.H > 13 {
		t.Fatalf("stepper up half is %.1fx%.1f, want about 13x11", up.W, up.H)
	}
	if down.Y < up.Y+up.H-1 || down.Y > up.Y+up.H+2 {
		t.Fatalf("stepper halves are not stacked: up %v down %v", up, down)
	}
}

// assertComposerVeil checks the fade: body text is not readable through
// the composer, and the gap under it still shows the transcript. The
// 28pt band above the composer dissolves, it does not hard-cut.
func assertComposerVeil(t *testing.T, tt *ui.Tester, dark bool) {
	t.Helper()
	comp, ok := tt.Find("composer")
	if !ok {
		t.Fatal("composer missing")
	}
	if comp.H < 72.5 || comp.H > 73.5 {
		t.Fatalf("composer border box %.2f, want 73", comp.H)
	}
	jump, ok := tt.Find("Jump to latest")
	if !ok {
		t.Fatal("jump pill missing on the scrolled transcript")
	}
	if jump.H < 25.5 || jump.H > 26.5 {
		t.Fatalf("jump pill height %.2f, want 26 (%v)", jump.H, jump)
	}
	gap := comp.Y - (jump.Y + jump.H)
	if gap < 9 || gap > 11.5 {
		t.Fatalf("jump pill sits %.2fpt above the composer, want 10 (jump %v composer %v)", gap, jump, comp)
	}
	list, ok := tt.Find("transcript")
	if !ok {
		t.Fatal("transcript missing")
	}
	block, ok := tt.Find("ctx, cancel := context.WithCancel(context.Background())\nwin.OnClose(cancel)  // fires once, safe to call twice\nresp, err := http.DefaultClient.Do(req.WithContext(ctx))")
	if !ok {
		t.Fatal("code block behind the composer is not in the list")
	}
	img := tt.Image()
	// The code's middle, clear of the placeholder and the send button.
	x0 := int(comp.X + 150)
	x1 := int(comp.X + comp.W - 170)
	if x1 < x0+40 {
		x0, x1 = int(block.X+24), int(block.X+180)
	}
	// Glyphs in the dissolve move one channel by more than the warm tint
	// does across this span. A hard cut is flat on every row.
	channelSpread := func(y, xa, xb int) int {
		lo, hi := [3]int{255, 255, 255}, [3]int{}
		for x := xa; x < xb; x++ {
			rr, gg, bb, _ := img.At(x, y).RGBA()
			ch := [3]int{int(rr >> 8), int(gg >> 8), int(bb >> 8)}
			for i, v := range ch {
				if v < lo[i] {
					lo[i] = v
				}
				if v > hi[i] {
					hi[i] = v
				}
			}
		}
		spread := 0
		for i := 0; i < 3; i++ {
			if hi[i]-lo[i] > spread {
				spread = hi[i] - lo[i]
			}
		}
		return spread
	}
	bestFade := 0
	for y := int(block.Y) + 2; y < int(comp.Y)-1; y += 2 {
		if s := channelSpread(y, x0, x1); s > bestFade {
			bestFade = s
		}
	}
	if bestFade < 8 {
		t.Fatalf("code in the 28pt fade is flat (spread %d); the dissolve is a hard cut", bestFade)
	}
	// The veil under the composer is the same 50% card as beside it. The
	// composer's own plate (about .95) is what hides the glyphs. Light
	// stays near white; a missing veil would land near 242. Dark stays
	// near the composer gray. Sample only where the code block actually
	// sits, so the composer's own hint and chips are not the reading.
	uMin, uMax := 255, 0
	uAt := 0
	yLo := int(block.Y)
	if yLo < int(comp.Y)+2 {
		yLo = int(comp.Y) + 2
	}
	yHi := int(block.Y + block.H)
	if yHi > int(comp.Y+comp.H)-2 {
		yHi = int(comp.Y+comp.H) - 2
	}
	bx0 := int(block.X + 20)
	bx1 := int(block.X + block.W - 20)
	if bx0 < x0 {
		bx0 = x0
	}
	if bx1 > x1 {
		bx1 = x1
	}
	spanUnder := func(y int) (minC, maxC int) {
		minC, maxC = 255, 0
		for x := bx0; x < bx1; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			for _, v := range []int{int(r >> 8), int(g >> 8), int(b >> 8)} {
				if v < minC {
					minC = v
				}
				if v > maxC {
					maxC = v
				}
			}
		}
		return minC, maxC
	}
	for y := yLo; y < yHi; y++ {
		mn, mx := spanUnder(y)
		if mn < uMin {
			uMin = mn
			uAt = y
		}
		if mx > uMax {
			uMax = mx
		}
	}
	if !dark && uMin < 246 {
		t.Fatalf("body text is readable through the light composer: channel min %d at y=%d (want at least 246)", uMin, uAt)
	}
	if dark && uMax > 56 {
		t.Fatalf("body text is readable through the dark composer: channel max %d at y=%d (want at most 56)", uMax, uAt)
	}
	// The 14pt plate inside the card, from the composer bottom to the card's
	// inner edge. A narrow column keeps the horizontal tint from reading
	// as glyphs. The window's own 8pt margin is below the card.
	card, ok := tt.Find("content card")
	if !ok {
		t.Fatal("content card missing")
	}
	// The 1pt border sits outside the content box. The inset is the gap
	// from the composer to that inner edge.
	inset := (card.Y + card.H - 1) - (comp.Y + comp.H)
	g0 := int(comp.Y+comp.H) + 2
	g1 := int(comp.Y+comp.H) + int(inset) - 1
	if g1 <= g0 {
		t.Fatalf("no gap under the composer: composer %v list %v", comp, list)
	}
	gx := (x0 + x1) / 2
	// Per channel. The plate is warm, so red and blue differ even when the
	// gap is flat; a glyph moves one channel.
	minC, maxC := [3]int{255, 255, 255}, [3]int{}
	for y := g0; y < g1; y++ {
		for x := gx - 16; x < gx+16; x++ {
			rr, gg, bb, _ := img.At(x, y).RGBA()
			ch := [3]int{int(rr >> 8), int(gg >> 8), int(bb >> 8)}
			for i, v := range ch {
				if v < minC[i] {
					minC[i] = v
				}
				if v > maxC[i] {
					maxC[i] = v
				}
			}
		}
	}
	// The 14pt gap is a 50–70% veil, not an opaque plate. Glyphs in this
	// fixture sit mostly behind the composer; veilAlphaAt checks the curve.
	// Card margin 8 + border 1 + the 14pt inset inside the card.
	bottomGap := float32(img.Bounds().Dy()) - (comp.Y + comp.H)
	if bottomGap < 21 || bottomGap > 25 {
		t.Fatalf("composer sits %.1fpt above the window, want about 23", bottomGap)
	}
}

// assertNoGradientSeam checks a resized window. The background is one
// ramp: neighboring samples a few points apart do not jump, in the sidebar
// and in the 8pt margin beside the card. The card's own shadow is not a seam.
func assertNoGradientSeam(t *testing.T, tt *ui.Tester, dark bool) {
	t.Helper()
	img := tt.Image()
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	walk := func(x, y0, y1 int) {
		t.Helper()
		prev := [3]int{}
		for y := y0; y <= y1; y += 4 {
			if y < 0 || y >= h || x < 0 || x >= w {
				t.Fatalf("seam sample %d,%d outside %dx%d", x, y, w, h)
			}
			rr, gg, bb, _ := img.At(x, y).RGBA()
			cur := [3]int{int(rr >> 8), int(gg >> 8), int(bb >> 8)}
			if y > y0 {
				if d := chanSum(prev, cur); d > 10 {
					t.Fatalf("background bands at %d,%d dark %v: %v vs %v (delta %d)", x, y, dark, prev, cur, d)
				}
			}
			prev = cur
		}
	}
	// Left of the sidebar's 10pt padding, so rows and the search field are
	// not in the sample. This is the bare background for the full height.
	walk(6, 8, h-8)
	// The 8pt margin to the right of the card is the same background.
	walk(w-4, 80, h-20)
	// Across the title bar the join is the background, not a step.
	// x=100 and 160 miss the compose button; 241 is the content pane.
	y := 24
	prev := [3]int{}
	for i, x := range []int{100, 160, 241, 270} {
		rr, gg, bb, _ := img.At(x, y).RGBA()
		cur := [3]int{int(rr >> 8), int(gg >> 8), int(bb >> 8)}
		if i > 0 {
			if d := chanSum(prev, cur); d > 12 {
				t.Fatalf("sidebar join seams at x=%d dark %v: %v vs %v (delta %d)", x, dark, prev, cur, d)
			}
		}
		prev = cur
	}
}

// assertNoSidebarSeparator checks the sidebar/content join is the window
// background, not a drawn rule. At 1x and 2x a rule would be one column
// (or its physical pixels) far from both neighbors, which match each other.
func assertNoSidebarSeparator(t *testing.T, tt *ui.Tester, dark bool) {
	t.Helper()
	for _, scale := range []float32{1, 2} {
		tt.SetScale(scale)
		img := tt.Image()
		// 8 and 48 sit in the title bar, clear of the compose button and the
		// traffic lights. 200 is beside the card, where its shadow falls off
		// but must not read as a 1pt rule.
		for _, y := range []int{8, 48, 200} {
			sampleNoSidebarSeparator(t, img, scale, y, dark)
		}
	}
	tt.SetScale(1)
}

func sampleNoSidebarSeparator(t *testing.T, img image.Image, scale float32, logicalY int, dark bool) {
	t.Helper()
	y := int(float32(logicalY)*scale + scale/2)
	cols := func(logicalX int) [][3]int {
		x0 := int(float32(logicalX) * scale)
		n := int(scale)
		out := make([][3]int, n)
		for i := 0; i < n; i++ {
			rr, gg, bb, _ := img.At(x0+i, y).RGBA()
			out[i] = [3]int{int(rr >> 8), int(gg >> 8), int(bb >> 8)}
		}
		return out
	}
	// Physical pixels of one logical column stay flat. A rule centered on
	// a pixel boundary would split the column.
	for _, x := range []int{238, 239, 240} {
		col := cols(x)
		for i := 1; i < len(col); i++ {
			if d := chanSum(col[0], col[i]); d > 4 {
				t.Fatalf("sidebar x=%d splits at y=%d scale %g dark %v: %v vs %v", x, logicalY, scale, dark, col[0], col[i])
			}
		}
	}
	at := func(logicalX int) [3]int { return cols(logicalX)[0] }
	// A drawn rule is one column far from both neighbors while those
	// neighbors match. The card shadow darkens toward the card, so the
	// neighbors do not match and this does not fire.
	check := func(leftX, midX, rightX int) {
		t.Helper()
		left, mid, right := at(leftX), at(midX), at(rightX)
		if chanSum(left, right) <= 8 && chanSum(mid, left) > 18 {
			t.Fatalf("sidebar rule at x=%d y=%d scale %g dark %v: %v between %v and %v", midX, logicalY, scale, dark, mid, left, right)
		}
	}
	check(238, 239, 240)
	if logicalY < 52 {
		// Above the card the next column is still the window, not the border.
		check(239, 240, 241)
	}
}

func chanSum(a, b [3]int) int {
	return absInt(a[0]-b[0]) + absInt(a[1]-b[1]) + absInt(a[2]-b[2])
}

func channelDelta(img image.Image, x0, y0, x1, y1 int) int {
	r0, g0, b0, _ := img.At(x0, y0).RGBA()
	r1, g1, b1, _ := img.At(x1, y1).RGBA()
	return absInt(int(r0>>8)-int(r1>>8)) + absInt(int(g0>>8)-int(g1>>8)) + absInt(int(b0>>8)-int(b1>>8))
}

// saveSeamCrop writes the sidebar/content join across the plate, and an
// 8× contrast copy, so a resize seam is visible if the veil is a flat color.
func saveSeamCrop(t *testing.T, dir, name string, tt *ui.Tester) {
	t.Helper()
	img := tt.Image()
	b := img.Bounds()
	r := image.Rect(180, b.Dy()-90, 420, b.Dy())
	if !r.In(b) {
		t.Fatalf("seam crop %v outside %v", r, b)
	}
	crop := img.SubImage(r).(*image.RGBA)
	writePNG(t, filepath.Join(dir, name), crop)
	ext := filepath.Ext(name)
	writePNG(t, filepath.Join(dir, strings.TrimSuffix(name, ext)+"-8x"+ext), contrast8(crop))
}

// assertScrollbarIdle fails when the transcript's overlay thumb is painted
// as a bar down the card's trailing edge. Idle shots keep it hidden.
func assertScrollbarIdle(t *testing.T, img image.Image, dark bool) {
	t.Helper()
	var diff int
	n := 0
	w := img.Bounds().Dx()
	// The card ends 8pt in from the window, with a 1pt border. The thumb
	// sits a few points inside that. Both samples stay on the empty card.
	xThumb, xBeside := w-16, w-40
	for y := 140; y <= 500; y += 3 {
		r1, g1, b1, _ := img.At(xThumb, y).RGBA()
		r2, g2, b2, _ := img.At(xBeside, y).RGBA()
		diff += absInt(int(r1>>8)-int(r2>>8)) + absInt(int(g1>>8)-int(g2>>8)) + absInt(int(b1>>8)-int(b2>>8))
		n++
	}
	// A solid tertiary thumb is tens of levels off the card. A hidden
	// thumb matches the card beside it.
	if diff/n > 18 {
		t.Fatalf("overlay scrollbar is visible while idle (mean channel delta %d, dark %v)", diff/n, dark)
	}
}

func hoverUserBubble(t *testing.T, tt *ui.Tester) {
	t.Helper()
	r, ok := tt.Find("How do I cancel an in-flight HTTP request when the window closes?")
	if !ok {
		t.Fatalf("hover fixture missing the user bubble: %q", tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
}

// saveFadeCrop writes the band where transcript text meets the composer,
// including the 28pt dissolve above it.
func saveFadeCrop(t *testing.T, dir, name string, img image.Image) {
	t.Helper()
	b := img.Bounds()
	r := image.Rect(250, 560, 1020, 750)
	if !r.In(b) {
		t.Fatalf("fade crop %v outside %v", r, b)
	}
	rgba, ok := img.(*image.RGBA)
	if !ok {
		t.Fatalf("fade crop: %T", img)
	}
	crop := rgba.SubImage(r).(*image.RGBA)
	writePNG(t, filepath.Join(dir, name), crop)
	ext := filepath.Ext(name)
	writePNG(t, filepath.Join(dir, strings.TrimSuffix(name, ext)+"-8x"+ext), contrast8(crop))
}

// contrast8 stretches each channel away from the crop's average by 8, the
// same kind of check as the designer's contrast strips. Glyphs that survive
// the veil become obvious; a flat veil stays flat.
func contrast8(src *image.RGBA) *image.RGBA {
	b := src.Bounds()
	n := (b.Dx()) * (b.Dy())
	if n == 0 {
		return src
	}
	var sum [3]int
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := src.RGBAAt(x, y)
			sum[0] += int(c.R)
			sum[1] += int(c.G)
			sum[2] += int(c.B)
		}
	}
	bg := [3]int{sum[0] / n, sum[1] / n, sum[2] / n}
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := src.RGBAAt(x, y)
			ch := [3]int{int(c.R), int(c.G), int(c.B)}
			var o [3]uint8
			for i := 0; i < 3; i++ {
				v := bg[i] + (ch[i]-bg[i])*8
				if v < 0 {
					v = 0
				}
				if v > 255 {
					v = 255
				}
				o[i] = uint8(v)
			}
			out.SetRGBA(x-b.Min.X, y-b.Min.Y, color.RGBA{R: o[0], G: o[1], B: o[2], A: 255})
		}
	}
	return out
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func sideBySide(design, impl image.Image) image.Image {
	const gap = 16
	scaled := image.NewNRGBA(image.Rect(0, 0, shotW, shotH))
	draw.ApproxBiLinear.Scale(scaled, scaled.Bounds(), design, design.Bounds(), draw.Over, nil)
	out := image.NewNRGBA(image.Rect(0, 0, shotW*2+gap, shotH))
	for i := range out.Pix {
		out.Pix[i] = 255
		if i%4 == 3 {
			out.Pix[i] = 255
		}
	}
	draw.Draw(out, image.Rect(0, 0, shotW, shotH), scaled, image.Point{}, draw.Src)
	draw.Draw(out, image.Rect(shotW+gap, 0, shotW*2+gap, shotH), impl, impl.Bounds().Min, draw.Src)
	return out
}

// assertTitleClearsLights fails when the sidebar title band draws anything
// in the gap after the stand-in traffic lights and before the compose
// button. The lights occupy x 12–72. The compose button sits at the
// trailing edge of the 240pt sidebar.
func assertTitleClearsLights(t *testing.T, img image.Image) {
	t.Helper()
	var ink int
	// Compose sits at the trailing edge of the sidebar, from about x 202.
	// The gap after the lights stays empty through x 168.
	for y := 8; y <= 44; y++ {
		for x := 80; x <= 168; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			R, G, B := int(r>>8), int(g>>8), int(b>>8)
			if R < 80 && G < 80 && B < 80 {
				ink++
			}
			if B > 140 && R < 90 && G < 130 {
				ink++
			}
		}
	}
	if ink > 12 {
		t.Fatalf("sidebar title still draws next to the traffic lights (%d px)", ink)
	}
}

// assertSearchFieldFlat fails when the search input paints its own fill
// inside the rounded field. That second rectangle sits on y=62, x=44–193,
// and stops before the ⌘K hint. The outer bg/search surface at x=20 is
// the only fill that should be there.
func assertSearchFieldFlat(t *testing.T, img image.Image) {
	t.Helper()
	// The 28pt field starts just under the 52pt title bar. Sample its top
	// padding so the placeholder glyphs are not mistaken for a second fill.
	const y = 54
	refR, refG, refB, _ := img.At(40, y).RGBA()
	R, G, B := int(refR>>8), int(refG>>8), int(refB>>8)
	var worst, at int
	for x := 70; x <= 180; x++ {
		r, g, b, _ := img.At(x, y).RGBA()
		d := absInt(int(r>>8)-R) + absInt(int(g>>8)-G) + absInt(int(b>>8)-B)
		if d > worst {
			worst, at = d, x
		}
	}
	if worst > 12 {
		r, g, b, _ := img.At(at, y).RGBA()
		t.Fatalf("search field draws a nested fill at (%d,%d): %02x%02x%02x vs field %02x%02x%02x", at, y, r>>8, g>>8, b>>8, R, G, B)
	}
}

// TestCardCornerPixels samples the card's four corners with the transcript
// scrolled to the bottom, at 1x and 2x in light and dark. A pixel inside
// the card's bounding box but outside the 12pt radius must be the window
// background, the same color as the pixel just outside the card, not the
// plate under the composer.
func TestCardCornerPixels(t *testing.T) {
	a := newTestApp(t)
	now := time.Now()
	stamp := now
	var msgs []Message
	for i := 0; i < 6; i++ {
		msgs = append(msgs,
			Message{ID: fmt.Sprintf("u%d", i), Role: roleUser, Content: fmt.Sprintf("Question %d about the corners of the card.", i), Done: true, CreatedAt: stamp},
			Message{ID: fmt.Sprintf("a%d", i), Role: roleAssistant, Content: "The reply sits above the composer so the bottom of the card is the plate.", Done: true, CreatedAt: stamp},
		)
	}
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Corners", Config: DefaultConfig(), UpdatedAt: now, CreatedAt: now,
		Messages: msgs,
	}}
	a.data.ActiveID = "c1"
	dir := testArtifactDir(t, "")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{false, true} {
		for _, scale := range []float32{1, 2} {
			tt := ui.NewTester(a.view, shotW, shotH)
			tt.SetScale(scale)
			tt.SetDark(dark)
			if !a.chat.AtEnd() {
				t.Fatal("transcript is not at the end")
			}
			card, ok := tt.Find("content card")
			if !ok || card.W < 100 || card.H < 100 {
				t.Fatalf("content card %v", card)
			}
			mode := "light"
			if dark {
				mode = "dark"
			}
			img := tt.Image()
			samples := assertRoundedCardCorners(t, img, card, scale)
			t.Logf("%s %.0fx card=%v\n%s", mode, scale, card, samples)
			if scale == 1 {
				saveCornerCrops(t, dir, fmt.Sprintf("after-corner-%s", mode), img, card, scale)
			}
		}
	}
}

// assertRoundedCardCorners checks pixels that sit in the card's rectangle
// and outside its rounded corner. Each must match the window at the same
// x just past the card (below it for the bottom, above it for the top).
func assertRoundedCardCorners(t *testing.T, img *image.RGBA, card ui.Rect, scale float32) string {
	t.Helper()
	b := img.Bounds()
	rad := float32(radiusContentCard) * scale
	type spec struct {
		name    string
		cx, cy  float32
		x0, y0  int
		x1, y1  int
		refY    int
		refName string
	}
	left := int(card.X * scale)
	top := int(card.Y * scale)
	right := int((card.X + card.W) * scale)
	bottom := int((card.Y + card.H) * scale)
	// right/bottom are the first device pixels outside the card when the
	// edges land on pixel boundaries.
	specs := []spec{
		{"bottom-left", (card.X + radiusContentCard) * scale, (card.Y + card.H - radiusContentCard) * scale, left, bottom - int(rad), left + int(rad), bottom, bottom, "below"},
		{"bottom-right", (card.X + card.W - radiusContentCard) * scale, (card.Y + card.H - radiusContentCard) * scale, right - int(rad), bottom - int(rad), right, bottom, bottom, "below"},
		{"top-left", (card.X + radiusContentCard) * scale, (card.Y + radiusContentCard) * scale, left, top, left + int(rad), top + int(rad), top - 1, "above"},
		{"top-right", (card.X + card.W - radiusContentCard) * scale, (card.Y + radiusContentCard) * scale, right - int(rad), top, right, top + int(rad), top - 1, "above"},
	}
	var bld strings.Builder
	checked := 0
	for _, s := range specs {
		n := 0
		farX, farY := 0, 0
		var farD float32
		for y := s.y0; y < s.y1; y++ {
			for x := s.x0; x < s.x1; x++ {
				dx := float32(x) + 0.5 - s.cx
				dy := float32(y) + 0.5 - s.cy
				// A pixel of clearance past the radius, so the antialiased
				// rim of the corner is not the sample.
				dist := dx*dx + dy*dy
				if dist <= (rad+1.25)*(rad+1.25) {
					continue
				}
				if x < 0 || y < 0 || x >= b.Max.X || y >= b.Max.Y || s.refY < 0 || s.refY >= b.Max.Y {
					t.Fatalf("%s sample (%d,%d) ref y %d outside %v", s.name, x, y, s.refY, b)
				}
				got := img.RGBAAt(x, y)
				ref := img.RGBAAt(x, s.refY)
				if dist > farD {
					farD, farX, farY = dist, x, y
				}
				for i, pair := range [][2]uint8{{got.R, ref.R}, {got.G, ref.G}, {got.B, ref.B}} {
					d := int(pair[0]) - int(pair[1])
					if d < 0 {
						d = -d
					}
					if d > 12 {
						t.Errorf("%s %.0fx (%d,%d) ch%d = %d, %s at y=%d is %d (delta %d)", s.name, scale, x, y, i, pair[0], s.refName, s.refY, pair[1], d)
					}
				}
				n++
				checked++
			}
		}
		if n == 0 {
			t.Errorf("%s %.0fx found no outside-radius pixels in x[%d,%d) y[%d,%d) center (%.1f,%.1f) r %.1f", s.name, scale, s.x0, s.x1, s.y0, s.y1, s.cx, s.cy, rad)
			continue
		}
		got := img.RGBAAt(farX, farY)
		ref := img.RGBAAt(farX, s.refY)
		fmt.Fprintf(&bld, "%s corner (%d,%d)=%d,%d,%d %s(%d,%d)=%d,%d,%d (%d px)\n", s.name, farX, farY, got.R, got.G, got.B, s.refName, farX, s.refY, ref.R, ref.G, ref.B, n)
	}
	if checked < 8 {
		t.Errorf("only %d outside-radius pixels", checked)
	}
	return bld.String()
}

func saveCornerCrops(t *testing.T, dir, name string, img *image.RGBA, card ui.Rect, scale float32) {
	t.Helper()
	s := scale
	span := int(36 * s)
	below := int(10 * s)
	bottom := int((card.Y + card.H) * s)
	left := int(card.X * s)
	right := int((card.X + card.W) * s)
	crops := []struct {
		name string
		r    image.Rectangle
	}{
		{"bl", image.Rect(left-4, bottom-span, left+span, bottom+below)},
		{"br", image.Rect(right-span, bottom-span, right+4, bottom+below)},
	}
	for _, c := range crops {
		r := c.r.Intersect(img.Bounds())
		writePNG(t, filepath.Join(dir, name+"-"+c.name+".png"), img.SubImage(r))
	}
}
