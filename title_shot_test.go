package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestTitleScreenshots(t *testing.T) {
	dir := testArtifactDir(t, "")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	save := func(tt *ui.Tester, name string) {
		t.Helper()
		writePNG(t, filepath.Join(dir, name), tt.Image())
	}
	pair := func(tt *ui.Tester, name string) {
		t.Helper()
		save(tt, name+"-light.png")
		tt.SetDark(true)
		save(tt, name+"-dark.png")
		tt.SetDark(false)
	}

	long := "How do I cancel an in-flight HTTP request when the window closes?"
	done := "Cancel HTTP on close"

	temp := titleShotApp(t, long)
	tt := ui.NewTester(temp.view, shotW, shotH)
	if !tt.HasText(long) || !tt.HasText("Tie the request to a context") {
		t.Fatalf("temp shot missing title or transcript: %q", tt.Texts())
	}
	pair(tt, "title-temp")

	fading := titleShotApp(t, done)
	fading.instant = false
	tf := ui.NewTester(fading.view, shotW, shotH)
	fading.instant = false
	fading.reduceMotion = false
	fading.titleFades = map[string]titleFade{
		"cancel": {from: long, start: tf.Now().Add(-100 * time.Millisecond), row: true, bar: true},
	}
	tf.Frame()
	if !tf.HasText(long) || !tf.HasText(done) {
		t.Fatalf("fade shot missing both titles: %q", tf.Texts())
	}
	pair(tf, "title-fade")

	settled := titleShotApp(t, done)
	ts := ui.NewTester(settled.view, shotW, shotH)
	if !ts.HasText(done) {
		t.Fatalf("done shot: %q", ts.Texts())
	}
	pair(ts, "title-done")

	failing := titleShotApp(t, done)
	ff := ui.NewTester(failing.view, shotW, shotH)
	failing.titleFails = map[string]time.Time{"cancel": ff.Now()}
	ff.Frame()
	if !ff.HasText(titleFailText) {
		t.Fatalf("fail shot missing warning label: %q", ff.Texts())
	}
	pair(ff, "title-fail")

	regen := titleShotApp(t, done)
	regen.titleJobs = map[string]*titleJob{"cancel": {gen: 1, manual: true, bar: true}}
	tr := ui.NewTester(regen.view, shotW, shotH)
	if !tr.HasText(titleGenerating) {
		t.Fatalf("regen shot missing spinner label: %q", tr.Texts())
	}
	pair(tr, "title-regen")

	ctxApp := titleShotApp(t, done)
	tc := ui.NewTester(ctxApp.view, shotW, shotH)
	row, ok := tc.Find("Go greet function")
	if !ok {
		t.Fatalf("sidebar row missing: %q", tc.Texts())
	}
	ctxApp.previewMenu = &previewMenu{
		X: row.X + 24, Y: row.Y + 8,
		Items: []previewItem{
			{Label: "Rename"},
			{Label: titleRegenLabel, Hot: true},
			{Sep: true},
			{Label: "Delete"},
		},
	}
	tc.Frame()
	if !tc.HasText("context menu") || !tc.HasText(titleRegenLabel) {
		t.Fatalf("context menu: %q", tc.Texts())
	}
	pair(tc, "ctx-menu")

	dis := titleShotApp(t, done)
	now := time.Now()
	dis.data.Conversations = append([]Conversation{{
		ID: "draft", Title: newChatTitle, CreatedAt: now, UpdatedAt: now, Loaded: true,
		Config:   Config{Provider: "OpenAI", Model: "gpt-4o-mini", BaseURL: "https://api.openai.com/v1"},
		Messages: []Message{},
	}}, dis.data.Conversations...)
	td := ui.NewTester(dis.view, shotW, shotH)
	draft, ok := td.Find(newChatTitle)
	if !ok {
		t.Fatalf("draft row missing: %q", td.Texts())
	}
	dis.previewMenu = &previewMenu{
		X: draft.X + 24, Y: draft.Y + 8,
		Items: []previewItem{
			{Label: "Rename"},
			{Label: titleRegenLabel, Disabled: true},
			{Sep: true},
			{Label: "Delete"},
		},
	}
	td.Frame()
	if !td.HasText("Draft") {
		t.Fatalf("disabled menu row: %q", td.Texts())
	}
	pair(td, "ctx-menu-disabled")

	more := titleShotApp(t, done)
	tm := ui.NewTester(more.view, shotW, shotH)
	if err := tm.Click("More"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Chat Settings…", "Rename", titleRegenLabel, "Delete chat", "Last request"} {
		if !tm.HasText(want) {
			t.Fatalf("more menu missing %q in %q", want, tm.Texts())
		}
	}
	pair(tm, "more-menu")

	settings := titleSettingsApp(t, true, nil)
	tset := ui.NewTester(settings.settingsWindowRoot, 780, 560)
	scrollTitlesIntoView(t, settings, tset)
	for _, want := range []string{"Chat titles", titleToggleLabel, titleToggleHint, titleModelLabel, titleSameAsChat} {
		if !tset.HasText(want) {
			t.Fatalf("settings missing %q in %q", want, tset.Texts())
		}
	}
	pair(tset, "settings-titles")

	off := titleSettingsApp(t, false, nil)
	toff := ui.NewTester(off.settingsWindowRoot, 780, 560)
	scrollTitlesIntoView(t, off, toff)
	pair(toff, "settings-titles-off")

	menuApp := titleSettingsApp(t, true, nil)
	tmenu := ui.NewTester(menuApp.settingsWindowRoot, 780, 560)
	scrollTitlesIntoView(t, menuApp, tmenu)
	if err := tmenu.Click("Title model, " + titleSameAsChat); err != nil {
		t.Fatal(err)
	}
	if !tmenu.HasText("Models & Providers…") || !tmenu.HasText("gpt-4o-mini") {
		t.Fatalf("title model menu: %q", tmenu.Texts())
	}
	if r, ok := tmenu.Find("gpt-4o-mini"); ok {
		tmenu.Move(r.X+r.W/2, r.Y+r.H/2)
	}
	pair(tmenu, "title-model-menu")

	gone := &TitleModel{ProviderID: "missing", ModelID: "grok-4-fast"}
	unavail := titleSettingsApp(t, true, gone)
	tu := ui.NewTester(unavail.settingsWindowRoot, 780, 560)
	scrollTitlesIntoView(t, unavail, tu)
	if !tu.HasText(titleUnavailableHint) || !tu.HasText("grok-4-fast") {
		t.Fatalf("unavailable button: %q", tu.Texts())
	}
	pair(tu, "title-model-unavail")

	tu2 := ui.NewTester(unavail.settingsWindowRoot, 780, 560)
	scrollTitlesIntoView(t, unavail, tu2)
	if err := tu2.Click("Title model, grok-4-fast, unavailable. Titles use each chat’s model."); err != nil {
		t.Fatal(err)
	}
	if !tu2.HasText(titleUnavailable) || !tu2.HasText(titleSameAsChat) {
		t.Fatalf("unavailable menu: %q", tu2.Texts())
	}
	pair(tu2, "title-model-unavail-menu")
}

func scrollTitlesIntoView(t *testing.T, a *App, tt *ui.Tester) {
	t.Helper()
	for i := 0; i < 14; i++ {
		r, ok := tt.Find("Chat titles")
		if ok && r.Y > 90 && r.Y < 240 {
			return
		}
		a.formScroll.Y += 48
		tt.Frame()
	}
	if !tt.HasText("Chat titles") {
		t.Fatalf("Chat titles not on screen: %q", tt.Texts())
	}
}

func titleSettingsApp(t *testing.T, on bool, model *TitleModel) *App {
	t.Helper()
	a := newTestApp(t)
	a.data.AutoTitles = on
	a.data.TitleModel = model
	a.data.Defaults.SystemPrompt = "Answer concisely. Prefer Go examples."
	a.data.Connections = []Connection{
		{ID: "openai", Name: "OpenAI", Provider: "OpenAI", ModelsSet: true, Models: []ProviderModel{
			{ID: "gpt-4o", Enabled: true}, {ID: "gpt-4o-mini", Enabled: true}, {ID: "o3-mini", Enabled: true},
		}},
		{ID: "anthropic", Name: "Anthropic", Provider: "Anthropic", ModelsSet: true, Models: []ProviderModel{
			{ID: "claude-sonnet-4.5", Enabled: true}, {ID: "claude-opus-4", Enabled: true},
		}},
		{ID: "deepseek", Name: "DeepSeek", Provider: "DeepSeek", ModelsSet: true, Models: []ProviderModel{
			{ID: "deepseek-chat", Enabled: true}, {ID: "deepseek-reasoner", Enabled: true},
		}},
		{ID: "together", Name: "Together AI", Provider: "Custom", Custom: true, BaseURL: "https://api.together.xyz/v1", Format: formatOpenAI, ModelsSet: true, Models: []ProviderModel{
			{ID: "meta-llama/Llama-3-70b", Enabled: true},
		}},
	}
	for _, id := range []string{"openai", "anthropic", "deepseek", "together"} {
		if err := a.keys.Set(id, "sk-test"); err != nil {
			t.Fatal(err)
		}
	}
	a.prefReady = false
	a.settingsSection = "general"
	return a
}

func titleShotApp(t *testing.T, title string) *App {
	t.Helper()
	a := newTestApp(t)
	a.data.Connections = []Connection{{
		ID: "anthropic", Name: "Anthropic", Provider: "Anthropic",
		ModelsSet: true,
		Models:    []ProviderModel{{ID: "claude-sonnet-4.5", Enabled: true}},
	}}
	if err := a.keys.Set("anthropic", "sk-ant"); err != nil {
		t.Fatal(err)
	}
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
	a.data.Conversations = []Conversation{
		{
			ID: "cancel", Title: title, TitleSource: titleSourceTemp,
			UpdatedAt: now.Add(-12 * time.Minute), CreatedAt: now.Add(-12 * time.Minute),
			Loaded: true,
			Config: Config{
				Provider: "Anthropic", Model: "claude-sonnet-4.5", BaseURL: "https://api.anthropic.com/v1",
				Temperature: f64p(0.7), MaxTokens: 4096, Reasoning: "medium",
				SystemPrompt: "Answer concisely. Prefer Go examples.",
			},
			Messages: []Message{
				{ID: "u0", Role: roleUser, Content: "What’s the difference between Client.Timeout and a context deadline?", Done: true, CreatedAt: stamp.Add(-10 * time.Minute)},
				{ID: "a0", Role: roleAssistant, Content: "`Client.Timeout` bounds the whole exchange — dial, headers and reading the body — and applies to every request the client makes. A context deadline belongs to one request and can be cancelled early.", Tokens: 640, ElapsedMS: 2100, Done: true, CreatedAt: stamp.Add(-10 * time.Minute)},
				{ID: "u", Role: roleUser, Content: "How do I cancel an in-flight HTTP request when the window closes?", Done: true, CreatedAt: stamp},
				{ID: "a", Role: roleAssistant, Content: reply, Reasoning: "The user wants cancellation tied to the window lifecycle.", ThinkingMS: 12000, Tokens: 1200, ElapsedMS: 3400, Done: true, CreatedAt: stamp},
			},
		},
		{ID: "greet", Title: "Go greet function", UpdatedAt: now.Add(-2 * time.Hour), CreatedAt: now.Add(-2 * time.Hour), Config: Config{Provider: "OpenAI", Model: "gpt-4o-mini", BaseURL: "https://api.openai.com/v1"}, Messages: []Message{{ID: "g", Role: roleUser, Content: "Write a Go greet function.", Done: true}}},
		{ID: "fsync", Title: "fsync before rename", UpdatedAt: now.Add(-26 * time.Hour), CreatedAt: now.Add(-26 * time.Hour), Config: Config{Provider: "DeepSeek", Model: "deepseek-reasoner", BaseURL: "https://api.deepseek.com/v1"}, Messages: []Message{{ID: "f", Role: roleUser, Content: "Why fsync the temp file before rename?", Done: true}}},
		{ID: "trip", Title: "Weekend trip outline", UpdatedAt: now.Add(-30 * time.Hour), CreatedAt: now.Add(-30 * time.Hour), Config: Config{Provider: "OpenRouter", Model: "llama-3.3", BaseURL: "https://openrouter.ai/api/v1"}, Messages: []Message{{ID: "t", Role: roleUser, Content: "Outline a weekend in Lisbon.", Done: true}}},
		{ID: "wal", Title: "SQLite WAL vs rollback journal", UpdatedAt: now.Add(-3 * 24 * time.Hour), CreatedAt: now.Add(-3 * 24 * time.Hour), Config: Config{Provider: "OpenAI", Model: "gpt-4.1", BaseURL: "https://api.openai.com/v1"}, Messages: []Message{{ID: "w", Role: roleUser, Content: "Compare WAL and a rollback journal.", Done: true}}},
		{ID: "metal", Title: "Metal blur shader notes", UpdatedAt: now.Add(-5 * 24 * time.Hour), CreatedAt: now.Add(-5 * 24 * time.Hour), Config: Config{Provider: "Ollama", Model: "qwen-coder", BaseURL: "http://127.0.0.1:11434/v1"}, Messages: []Message{{ID: "m", Role: roleUser, Content: "Notes on a Metal blur shader.", Done: true}}},
		{ID: "tok", Title: "Tokenizer edge cases", UpdatedAt: time.Date(now.Year(), 9, 12, 10, 0, 0, 0, now.Location()), CreatedAt: time.Date(now.Year(), 9, 12, 10, 0, 0, 0, now.Location()), Config: Config{Provider: "Anthropic", Model: "opus-4", BaseURL: "https://api.anthropic.com/v1"}, Messages: []Message{{ID: "k", Role: roleUser, Content: "Tokenizer edge cases.", Done: true}}},
		{ID: "read", Title: "Reading list for October", UpdatedAt: time.Date(now.Year(), 9, 3, 10, 0, 0, 0, now.Location()), CreatedAt: time.Date(now.Year(), 9, 3, 10, 0, 0, 0, now.Location()), Config: Config{Provider: "OpenRouter", Model: "gemini-2.5", BaseURL: "https://openrouter.ai/api/v1"}, Messages: []Message{{ID: "r", Role: roleUser, Content: "Reading list for October.", Done: true}}},
	}
	a.data.ActiveID = "cancel"
	return a
}
