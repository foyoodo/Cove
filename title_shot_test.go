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
	dir := testArtifactDir(t, "auto-title")
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
	assertSingleTurn(t, temp, long)
	tt := ui.NewTester(temp.view, shotW, shotH)
	if !tt.HasText(long) || !tt.HasText("Tie the request to a context") {
		t.Fatalf("temp shot missing title or transcript: %q", tt.Texts())
	}
	pair(tt, "title-temp")

	fading := titleShotApp(t, done)
	assertSingleTurn(t, fading, long)
	tf := ui.NewTester(fading.view, shotW, shotH)
	// Settle the theme before the cross-fade. SetDark while instant is
	// false starts the theme animation, and the dark frame was captured
	// still on the light side of that mix.
	saveFade := func(dark bool, name string) {
		t.Helper()
		fading.instant = true
		fading.reduceMotion = false
		tf.SetDark(dark)
		fading.instant = false
		fading.titleFades = map[string]titleFade{
			"cancel": {from: long, start: tf.Now().Add(-100 * time.Millisecond), row: true, bar: true},
		}
		tf.Frame()
		if !tf.HasText(long) || !tf.HasText(done) {
			t.Fatalf("fade shot missing both titles: %q", tf.Texts())
		}
		save(tf, name)
	}
	saveFade(false, "title-fade-light.png")
	saveFade(true, "title-fade-dark.png")
	fading.instant = true
	tf.SetDark(false)

	settled := titleShotApp(t, done)
	assertSingleTurn(t, settled, long)
	ts := ui.NewTester(settled.view, shotW, shotH)
	if !ts.HasText(done) {
		t.Fatalf("done shot: %q", ts.Texts())
	}
	if !ts.HasText("Anthropic · claude-sonnet-4.5") || !ts.HasText("1.2k tokens") {
		t.Fatalf("reply footer: %q", ts.Texts())
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
	// Current chat, as the sidebar menu sets it: the row and the title bar
	// both go grey, and the time column is the 10pt spinner.
	regen.titleJobs = map[string]*titleJob{"cancel": {gen: 1, manual: true, bar: true}}
	if !regen.titleDimmed("cancel", true) || !regen.titleDimmed("cancel", false) {
		t.Fatal("current chat should dim the title bar and the sidebar row")
	}
	tr := ui.NewTester(regen.view, shotW, shotH)
	if !tr.HasText(titleGenerating) {
		t.Fatalf("regen shot missing spinner label: %q", tr.Texts())
	}
	pair(tr, "title-regen")

	ctxApp := titleShotApp(t, done)
	tc := ui.NewTester(ctxApp.view, shotW, shotH)
	showCtx(t, ctxApp, tc, "Go greet function, 2h", []shotMenuItem{
		{Label: "Rename"},
		{Label: titleRegenLabel, Hot: true},
		{Sep: true},
		{Label: "Delete"},
	})
	if !tc.HasText("context menu") || !tc.HasText(titleRegenLabel) {
		t.Fatalf("context menu: %q", tc.Texts())
	}
	pair(tc, "ctx-menu")
	framePaintHook = nil

	dis := titleShotApp(t, done)
	now := time.Now()
	dis.data.Conversations = append([]Conversation{{
		ID: "draft", Title: newChatTitle, CreatedAt: now, UpdatedAt: now, Loaded: true,
		Config: Config{
			Provider: "OpenAI", Model: "gpt-4o-mini", ConnectionID: "openai",
			BaseURL: "https://api.openai.com/v1",
		},
		Messages: []Message{},
	}}, dis.data.Conversations...)
	td := ui.NewTester(dis.view, shotW, shotH)
	// "New Chat" is also the empty-state copy. The row's access label is
	// unique, and the menu has to open on that row rather than the title bar.
	showCtx(t, dis, td, "New Chat, Draft", []shotMenuItem{
		{Label: "Rename"},
		{Label: titleRegenLabel, Disabled: true},
		{Sep: true},
		{Label: "Delete"},
	})
	if !td.HasText("Draft") || !td.HasText(titleRegenLabel+", disabled") {
		t.Fatalf("disabled menu row: %q", td.Texts())
	}
	pair(td, "ctx-menu-disabled")
	framePaintHook = nil

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

	kb := titleShotApp(t, done)
	tk := ui.NewTester(kb.view, shotW, shotH)
	openMoreFromKeyboard(t, tk)
	pair(tk, "more-menu-kb")

	streaming := titleShotApp(t, done)
	streaming.streams = map[string]*convStream{"cancel": {gen: 1, cancel: func() {}}}
	tstream := ui.NewTester(streaming.view, shotW, shotH)
	if !tstream.HasText("Generating") {
		t.Fatalf("streaming sidebar: %q", tstream.Texts())
	}
	if err := tstream.Click("More"); err != nil {
		t.Fatal(err)
	}
	for _, row := range kindMarks(t, tstream, "more") {
		if row.label == titleRegenLabel && row.on {
			t.Fatal("Regenerate Title stayed enabled while a reply is streaming")
		}
	}
	pair(tstream, "more-menu-stream")

	narrow := titleShotApp(t, done)
	tn := ui.NewTester(narrow.view, 720, 520)
	if err := tn.Click("More"); err != nil {
		t.Fatal(err)
	}
	pair(tn, "more-menu-min")

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

	minSet := titleSettingsApp(t, true, nil)
	tmin := ui.NewTester(minSet.settingsWindowRoot, 720, 520)
	scrollTitlesIntoView(t, minSet, tmin)
	pair(tmin, "settings-titles-min")

	menuApp := titleSettingsApp(t, true, nil)
	tmenu := ui.NewTester(menuApp.settingsWindowRoot, 780, 560)
	scrollTitlesIntoView(t, menuApp, tmenu)
	if err := tmenu.Click("Title model, " + titleSameAsChat); err != nil {
		t.Fatal(err)
	}
	if !tmenu.HasText("Models & Providers…") || !tmenu.HasText("gpt-4o-mini") {
		t.Fatalf("title model menu: %q", tmenu.Texts())
	}
	var mini ui.Rect
	for _, row := range kindMarks(t, tmenu, "title") {
		if row.label == "gpt-4o-mini" {
			mini = row.rect
		}
	}
	if mini.W < 8 {
		t.Fatal("gpt-4o-mini row missing from the title model menu")
	}
	tmenu.Move(mini.X+mini.W/2, mini.Y+mini.H/2)
	// The row under the pointer takes the highlight on the following frame.
	tmenu.Frame()
	if hot := hotLabeled(kindMarks(t, tmenu, "title")); hot.label != "gpt-4o-mini" {
		t.Fatalf("title model hover highlight %q, want gpt-4o-mini", hot.label)
	}
	pair(tmenu, "title-model-menu")

	searchApp := titleSettingsApp(t, true, nil)
	for i := range searchApp.data.Connections {
		if searchApp.data.Connections[i].ID != "openai" {
			continue
		}
		searchApp.data.Connections[i].Models = append(searchApp.data.Connections[i].Models,
			ProviderModel{ID: "gpt-4.1", Enabled: true},
			ProviderModel{ID: "gpt-4.1-mini", Enabled: true},
		)
	}
	tsr := ui.NewTester(searchApp.settingsWindowRoot, 780, 560)
	scrollTitlesIntoView(t, searchApp, tsr)
	if err := tsr.Click("Title model, " + titleSameAsChat); err != nil {
		t.Fatal(err)
	}
	if !tsr.HasText("Search models") {
		t.Fatal("ten models did not show the search field")
	}
	tsr.Type("claude")
	if !tsr.HasText("claude-sonnet-4.5") {
		t.Fatalf("title model search: %q", tsr.Texts())
	}
	for _, row := range kindMarks(t, tsr, "title") {
		if strings.Contains(strings.ToLower(row.label), "gpt") || strings.Contains(strings.ToLower(row.label), "deepseek") || row.label == titleSameAsChat {
			t.Fatalf("search kept %q", row.label)
		}
	}
	pair(tsr, "title-model-search")

	picked := titleSettingsApp(t, true, &TitleModel{ProviderID: "anthropic", ModelID: "claude-sonnet-4.5"})
	tpick := ui.NewTester(picked.settingsWindowRoot, 780, 560)
	scrollTitlesIntoView(t, picked, tpick)
	if _, ok := tpick.Find("Title model, claude-sonnet-4.5"); !ok {
		t.Fatalf("picked title model: %q", tpick.Texts())
	}
	pair(tpick, "title-model-picked")

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
	if !tu2.HasText(titleUnavailable) || !tu2.HasText(titleSameAsChat) || !tu2.HasText("deepseek-reasoner") || !tu2.HasText("Together AI") {
		t.Fatalf("unavailable menu: %q", tu2.Texts())
	}
	pair(tu2, "title-model-unavail-menu")
}

func assertSingleTurn(t *testing.T, a *App, question string) {
	t.Helper()
	conv := a.conversation("cancel")
	if conv == nil {
		t.Fatal("missing shot chat")
	}
	users, replies := 0, 0
	var first string
	for _, m := range conv.Messages {
		switch m.Role {
		case roleUser:
			users++
			if first == "" {
				first = m.Content
			}
		case roleAssistant:
			replies++
		}
	}
	if users != 1 || replies != 1 {
		t.Fatalf("shot chat has %d user messages and %d replies", users, replies)
	}
	if temporaryTitle(first) != question {
		t.Fatalf("temporary title %q, want the first line %q", temporaryTitle(first), question)
	}
}

func openMoreFromKeyboard(t *testing.T, tt *ui.Tester) {
	t.Helper()
	// A pointer click on More does not move the keyboard, and Escape
	// returns to the composer. Full Keyboard Access reaches the button,
	// and Space opens the menu from there.
	tt.SetPreferences(ui.Preferences{FullKeyboardAccess: true, TextScale: 1})
	tt.Frame()
	for i := 0; i < 24 && !tt.Focused("More"); i++ {
		tt.Key(0, ui.KeyTab)
	}
	if !tt.Focused("More") {
		t.Fatal("Tab did not reach More")
	}
	tt.Key(0, ui.KeySpace)
	if hot := hotLabeled(kindMarks(t, tt, "more")); hot.label != "Chat Settings…" {
		t.Fatalf("keyboard More highlight %q", hot.label)
	}
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

type shotMenuItem struct {
	Label    string
	Hot      bool
	Disabled bool
	Sep      bool
}

// showCtx draws the headless stand-in for the system context menu on the
// sidebar row named by its access label, with the 2pt inset focus frame
// from §20.4. The hook is test-only; production leaves it nil.
func showCtx(t *testing.T, app *App, tt *ui.Tester, access string, items []shotMenuItem) {
	t.Helper()
	row, ok := tt.Find(access)
	if !ok {
		t.Fatalf("sidebar row %q missing: %q", access, tt.Texts())
	}
	if row.Y < 48 {
		t.Fatalf("row %q is on the title bar at y %.1f", access, row.Y)
	}
	framePaintHook = func(a *App, c *ui.Context) {
		if a != app {
			return
		}
		paintShotMenu(c, a, row, items)
	}
	tt.Frame()
}

func paintShotMenu(c *ui.Context, a *App, row ui.Rect, items []shotMenuItem) {
	p := a.look
	ui.Box(c).Absolute().Left(row.X+2).Top(row.Y+2).Width(row.W-4).Height(row.H-4).Radius(radiusRow).Border(2, p.AccentDefault).PassThrough()
	panel := ui.Column(c).Absolute().Left(row.X+12).Top(row.Y+4).Width(188).Radius(8).Background(p.LayerOverlay).Padding(4, 0).Label("context menu")
	panel.Border(0.5, p.LayerOverlayBorder)
	panel.Shadow(0, 10, 28, 0, p.ShadowMd)
	panel.Children(func() {
		for _, it := range items {
			if it.Sep {
				ui.Box(c).Height(1).FillWidth().Margin(4, 10).Background(p.SeparatorDefault)
				continue
			}
			line := ui.Row(c).Height(22).FillWidth().Padding(0, 12).AlignItems(ui.Center).Radius(5)
			fg := p.TextPrimary
			label := it.Label
			if it.Disabled {
				fg = p.TextTertiary
				label = it.Label + ", disabled"
				line.Disabled(true)
			}
			if it.Hot && !it.Disabled {
				line.Background(p.AccentDefault)
				fg = p.TextOnAccent
			}
			line.Children(func() {
				ui.Text(c, it.Label).FontSize(13).TextColor(fg).SingleLine()
			})
			line.Label(label)
		}
	})
}

func titleShotApp(t *testing.T, title string) *App {
	t.Helper()
	a := newTestApp(t)
	a.data.Connections = []Connection{
		{ID: "openai", Name: "OpenAI", Provider: "OpenAI", BaseURL: "https://api.openai.com/v1", ModelsSet: true, Models: []ProviderModel{
			{ID: "gpt-4o-mini", Enabled: true}, {ID: "gpt-4.1", Enabled: true},
		}},
		{ID: "anthropic", Name: "Anthropic", Provider: "Anthropic", BaseURL: "https://api.anthropic.com/v1", ModelsSet: true, Models: []ProviderModel{
			{ID: "claude-sonnet-4.5", Enabled: true}, {ID: "opus-4", Enabled: true},
		}},
		{ID: "deepseek", Name: "DeepSeek", Provider: "DeepSeek", BaseURL: "https://api.deepseek.com/v1", ModelsSet: true, Models: []ProviderModel{
			{ID: "deepseek-reasoner", Enabled: true},
		}},
		{ID: "openrouter", Name: "OpenRouter", Provider: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1", ModelsSet: true, Models: []ProviderModel{
			{ID: "llama-3.3", Enabled: true}, {ID: "gemini-2.5", Enabled: true},
		}},
		{ID: "ollama", Name: "Ollama", Provider: "Ollama", BaseURL: "http://127.0.0.1:11434/v1", ModelsSet: true, Models: []ProviderModel{
			{ID: "qwen-coder", Enabled: true},
		}},
		{ID: "together", Name: "Together AI", Provider: "Custom", Custom: true, BaseURL: "https://api.together.xyz/v1", Format: formatOpenAI, ModelsSet: true, Models: []ProviderModel{
			{ID: "meta-llama/Llama-3-70b", Enabled: true},
		}},
	}
	for _, id := range []string{"openai", "anthropic", "deepseek", "openrouter", "ollama", "together"} {
		if err := a.keys.Set(id, "sk-test"); err != nil {
			t.Fatal(err)
		}
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
				Provider: "Anthropic", Model: "claude-sonnet-4.5", ConnectionID: "anthropic", BaseURL: "https://api.anthropic.com/v1",
				Temperature: f64p(0.7), MaxTokens: 4096, Reasoning: "medium",
				SystemPrompt: "Answer concisely. Prefer Go examples.",
			},
			Messages: []Message{
				{ID: "u", Role: roleUser, Content: "How do I cancel an in-flight HTTP request when the window closes?", Done: true, CreatedAt: stamp},
				{ID: "a", Role: roleAssistant, Content: reply, Reasoning: "The user wants cancellation tied to the window lifecycle.", ThinkingMS: 12000, Model: "claude-sonnet-4.5", ProviderName: "Anthropic", ProviderKind: "Anthropic", ProviderID: "anthropic", Tokens: 1200, ElapsedMS: 3400, Done: true, CreatedAt: stamp},
			},
		},
		{ID: "greet", Title: "Go greet function", UpdatedAt: now.Add(-2 * time.Hour), CreatedAt: now.Add(-2 * time.Hour), Config: Config{Provider: "OpenAI", Model: "gpt-4o-mini", ConnectionID: "openai", BaseURL: "https://api.openai.com/v1"}, Messages: []Message{{ID: "g", Role: roleUser, Content: "Write a Go greet function.", Done: true}}},
		{ID: "fsync", Title: "fsync before rename", UpdatedAt: now.Add(-26 * time.Hour), CreatedAt: now.Add(-26 * time.Hour), Config: Config{Provider: "DeepSeek", Model: "deepseek-reasoner", ConnectionID: "deepseek", BaseURL: "https://api.deepseek.com/v1"}, Messages: []Message{{ID: "f", Role: roleUser, Content: "Why fsync the temp file before rename?", Done: true}}},
		{ID: "trip", Title: "Weekend trip outline", UpdatedAt: now.Add(-30 * time.Hour), CreatedAt: now.Add(-30 * time.Hour), Config: Config{Provider: "OpenRouter", Model: "llama-3.3", ConnectionID: "openrouter", BaseURL: "https://openrouter.ai/api/v1"}, Messages: []Message{{ID: "t", Role: roleUser, Content: "Outline a weekend in Lisbon.", Done: true}}},
		{ID: "wal", Title: "SQLite WAL vs rollback journal", UpdatedAt: now.Add(-3 * 24 * time.Hour), CreatedAt: now.Add(-3 * 24 * time.Hour), Config: Config{Provider: "OpenAI", Model: "gpt-4.1", ConnectionID: "openai", BaseURL: "https://api.openai.com/v1"}, Messages: []Message{{ID: "w", Role: roleUser, Content: "Compare WAL and a rollback journal.", Done: true}}},
		{ID: "metal", Title: "Metal blur shader notes", UpdatedAt: now.Add(-5 * 24 * time.Hour), CreatedAt: now.Add(-5 * 24 * time.Hour), Config: Config{Provider: "Ollama", Model: "qwen-coder", ConnectionID: "ollama", BaseURL: "http://127.0.0.1:11434/v1"}, Messages: []Message{{ID: "m", Role: roleUser, Content: "Notes on a Metal blur shader.", Done: true}}},
		{ID: "tok", Title: "Tokenizer edge cases", UpdatedAt: time.Date(now.Year(), 9, 12, 10, 0, 0, 0, now.Location()), CreatedAt: time.Date(now.Year(), 9, 12, 10, 0, 0, 0, now.Location()), Config: Config{Provider: "Anthropic", Model: "opus-4", ConnectionID: "anthropic", BaseURL: "https://api.anthropic.com/v1"}, Messages: []Message{{ID: "k", Role: roleUser, Content: "Tokenizer edge cases.", Done: true}}},
		{ID: "read", Title: "Reading list for October", UpdatedAt: time.Date(now.Year(), 9, 3, 10, 0, 0, 0, now.Location()), CreatedAt: time.Date(now.Year(), 9, 3, 10, 0, 0, 0, now.Location()), Config: Config{Provider: "Custom", Model: "meta-llama/Llama-3-70b", ConnectionID: "together", BaseURL: "https://api.together.xyz/v1"}, Messages: []Message{{ID: "r", Role: roleUser, Content: "Reading list for October.", Done: true}}},
	}
	a.data.ActiveID = "cancel"
	return a
}
