package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	silenceAutoTitles = true
	os.Exit(m.Run())
}

func TestSanitizeTitle(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{in: "\"Cancel HTTP on close.\"", want: "Cancel HTTP on close", ok: true},
		{in: "“Quoted title.”", want: "Quoted title", ok: true},
		{in: "Ship it 🚀", want: "Ship it", ok: true},
		{in: "Hello\nworld.", want: "Hello world", ok: true},
		{in: "\"\"", ok: false},
		{in: "🎉", ok: false},
		{in: "   ", ok: false},
		{in: strings.Repeat("字", 16), want: strings.Repeat("字", 15), ok: true},
		{in: "one two three four five six seven eight", want: "one two three four five six", ok: true},
	}
	for _, tc := range cases {
		got, ok := sanitizeTitle(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("sanitize(%q) = %q %v, want %q %v", tc.in, got, ok, tc.want, tc.ok)
		}
		if ok && strings.Contains(got, "…") {
			t.Errorf("sanitize(%q) added an ellipsis: %q", tc.in, got)
		}
		if ok && displayWidth(got) > titleWidthMax {
			t.Errorf("sanitize(%q) width %d", tc.in, displayWidth(got))
		}
	}
}

func TestTemporaryTitle(t *testing.T) {
	if got := temporaryTitle("  How do I cancel?\nSecond line"); got != "How do I cancel?" {
		t.Fatalf("first line: %q", got)
	}
	fenced := "```go\nfmt.Println()\n```\nAfter the fence"
	if got := temporaryTitle(fenced); got != "After the fence" {
		t.Fatalf("fence: %q", got)
	}
	if got := temporaryTitle("> quoted\nnext"); got != "quoted" {
		t.Fatalf("quote mark: %q", got)
	}
	if got := temporaryTitle("1. First item"); got != "First item" {
		t.Fatalf("number: %q", got)
	}
	long := strings.Repeat("a", 90)
	got := temporaryTitle(long)
	if len([]rune(got)) != titleStoredMax || !strings.HasSuffix(got, "…") {
		t.Fatalf("stored cap: %q (%d)", got, len([]rune(got)))
	}
	if temporaryTitle("```\nonly code\n```") != newChatTitle {
		t.Fatal("code-only message should stay New Chat")
	}
}

func TestTitleOutputTokens(t *testing.T) {
	if titleOutputTokens("gpt-4o") != titleTokensNormal || titleOutputTokens("grok-4-fast") != titleTokensNormal {
		t.Fatalf("normal caps %d %d", titleOutputTokens("gpt-4o"), titleOutputTokens("grok-4-fast"))
	}
	for _, model := range []string{"o3-mini", "o1-preview", "gpt-5", "deepseek-reasoner", "openai/o4-mini", "some-thinking-model"} {
		if titleOutputTokens(model) != titleTokensReason {
			t.Fatalf("%s cap %d", model, titleOutputTokens(model))
		}
	}
	turns := []apiMessage{{Role: roleUser, Content: "hello"}, {Role: roleAssistant, Content: "world"}}
	normal, err := titleRequestBody(Config{Provider: "OpenAI", Model: "gpt-4o"}, turns)
	if err != nil {
		t.Fatal(err)
	}
	reason, err := titleRequestBody(Config{Provider: "OpenAI", Model: "o3-mini"}, turns)
	if err != nil {
		t.Fatal(err)
	}
	deep, err := titleRequestBody(Config{Provider: "DeepSeek", Model: "deepseek-reasoner"}, turns)
	if err != nil {
		t.Fatal(err)
	}
	assertTitleBody(t, normal, "max_completion_tokens", titleTokensNormal, false)
	assertTitleBody(t, reason, "max_completion_tokens", titleTokensReason, true)
	assertTitleBody(t, deep, "max_tokens", titleTokensReason, true)
	if strings.Contains(string(normal), "reasoning_effort") || strings.Contains(string(normal), "\"stream\":true") {
		t.Fatalf("gpt-4o body %s", normal)
	}
	var deepBody map[string]any
	if err := json.Unmarshal(deep, &deepBody); err != nil {
		t.Fatal(err)
	}
	thinking, _ := deepBody["thinking"].(map[string]any)
	if thinking["type"] != "disabled" {
		t.Fatalf("deepseek thinking %#v", deepBody["thinking"])
	}
}

func assertTitleBody(t *testing.T, body []byte, capKey string, cap int, reasoning bool) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["stream"] != false {
		t.Fatalf("stream %#v", payload["stream"])
	}
	got, _ := payload[capKey].(float64)
	if int(got) != cap {
		t.Fatalf("%s = %v, want %d in %s", capKey, payload[capKey], cap, body)
	}
	_, hasEffort := payload["reasoning_effort"]
	if hasEffort != reasoning && capKey == "max_completion_tokens" {
		t.Fatalf("reasoning_effort present %v, want %v in %s", hasEffort, reasoning, body)
	}
	msgs, _ := payload["messages"].([]any)
	if len(msgs) < 2 {
		t.Fatalf("messages %#v", payload["messages"])
	}
}

func TestAutoTitlesDefaultOnWhenMissing(t *testing.T) {
	if !defaultData().AutoTitles {
		t.Fatal("default is off")
	}
	var missing settingsDoc
	if err := json.Unmarshal([]byte(`{"version":7,"showReasoning":true}`), &missing); err != nil {
		t.Fatal(err)
	}
	if missing.AutoTitles != nil {
		t.Fatal("missing key should be nil")
	}
	if !settingsToData(missing).AutoTitles {
		t.Fatal("missing autoTitles loaded as off")
	}
	var off settingsDoc
	if err := json.Unmarshal([]byte(`{"version":7,"autoTitles":false}`), &off); err != nil {
		t.Fatal(err)
	}
	if settingsToData(off).AutoTitles {
		t.Fatal("explicit false loaded as on")
	}
}

func TestTitleOnceAndManualProtection(t *testing.T) {
	var n int
	prev := titleComplete
	titleComplete = func(context.Context, Config, []byte) (string, error) {
		n++
		return "\"Cancel HTTP on close.\"", nil
	}
	t.Cleanup(func() { titleComplete = prev })

	a := titleTestApp(t)
	a.maybeAutoTitle("chat")
	if n != 1 {
		t.Fatalf("requests %d", n)
	}
	conv := a.conversation("chat")
	if conv.Title != "Cancel HTTP on close" || conv.TitleSource != titleSourceAuto || !conv.TitleGenerated {
		t.Fatalf("after generate %+v", conv.Title)
	}
	if !conv.UpdatedAt.Equal(conv.CreatedAt) {
		t.Fatalf("title generation touched the clock: %s", conv.UpdatedAt)
	}

	loaded, err := loadFull(filepath.Dir(a.path))
	if err != nil {
		t.Fatal(err)
	}
	var found *Conversation
	for i := range loaded.Conversations {
		if loaded.Conversations[i].ID == "chat" {
			found = &loaded.Conversations[i]
		}
	}
	if found == nil || !found.TitleGenerated || found.TitleSource != titleSourceAuto || found.Title != "Cancel HTTP on close" {
		t.Fatalf("reloaded %#v", found)
	}
	a.maybeAutoTitle("chat")
	if n != 1 {
		t.Fatalf("repeated request %d", n)
	}

	a.conversation("chat").Title = "Kept name"
	a.conversation("chat").TitleSource = titleSourceManual
	a.conversation("chat").TitleGenerated = false
	a.maybeAutoTitle("chat")
	if n != 1 || a.conversation("chat").Title != "Kept name" || a.conversation("chat").TitleGenerated {
		t.Fatalf("manual chat was titled: %q requests %d generated %v", a.conversation("chat").Title, n, a.conversation("chat").TitleGenerated)
	}
}

func TestTitleRenameOpenDiscards(t *testing.T) {
	a := titleTestApp(t)
	a.conversation("chat").Title = "Temporary"
	a.conversation("chat").TitleSource = titleSourceTemp
	a.renameOpen = true
	a.data.ActiveID = "chat"
	a.titleGens = map[string]int{"chat": 1}
	a.titleJobs = map[string]*titleJob{"chat": {gen: 1}}
	a.finishTitle("chat", 1, false, "A generated title", nil)
	conv := a.conversation("chat")
	if conv.Title != "Temporary" || conv.TitleSource != titleSourceTemp {
		t.Fatalf("rename-open applied %#v %s", conv.Title, conv.TitleSource)
	}
}

func TestTitleModelFallback(t *testing.T) {
	a := titleTestApp(t)
	conv := a.conversation("chat")
	cfg, ok := a.titleConfig(conv, true)
	if !ok || cfg.Model != "gpt-4o-mini" || cfg.ConnectionID != "openai" {
		t.Fatalf("same as chat: ok %v model %s conn %s", ok, cfg.Model, cfg.ConnectionID)
	}
	a.data.TitleModel = &TitleModel{ProviderID: "openai", ModelID: "gpt-4o"}
	cfg, ok = a.titleConfig(conv, true)
	if !ok || cfg.Model != "gpt-4o" {
		t.Fatalf("picked model: ok %v %s", ok, cfg.Model)
	}
	a.data.TitleModel = &TitleModel{ProviderID: "missing", ModelID: "grok-4-fast"}
	cfg, ok = a.titleConfig(conv, true)
	if !ok || cfg.Model != conv.Config.Model || cfg.ConnectionID != conv.Config.ConnectionID {
		t.Fatalf("fallback ok %v cfg %+v", ok, cfg)
	}
	if a.data.TitleModel == nil || a.data.TitleModel.ModelID != "grok-4-fast" {
		t.Fatalf("unavailable selection was cleared: %#v", a.data.TitleModel)
	}
	if a.titleModelAvailable(a.data.TitleModel) {
		t.Fatal("missing provider reported available")
	}
}

func TestTitleToggleOffSendsNothing(t *testing.T) {
	var n int
	prev := titleComplete
	titleComplete = func(context.Context, Config, []byte) (string, error) {
		n++
		return "Nope", nil
	}
	t.Cleanup(func() { titleComplete = prev })
	a := titleTestApp(t)
	a.data.AutoTitles = false
	a.maybeAutoTitle("chat")
	if n != 0 {
		t.Fatalf("toggle off sent %d", n)
	}
	if a.conversation("chat").TitleGenerated || a.conversation("chat").Title != "New Chat" {
		t.Fatalf("toggle off changed the chat: %q generated %v", a.conversation("chat").Title, a.conversation("chat").TitleGenerated)
	}
}

func TestTitleFilterKeepsRow(t *testing.T) {
	a := newTestApp(t)
	now := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	a.data.Conversations = []Conversation{{
		ID: "c", Title: "Cancel HTTP on close", CreatedAt: now, UpdatedAt: now,
	}}
	a.query = "cancel"
	a.rebuildSide(now)
	if len(a.sideRows) != 2 {
		t.Fatalf("rows before %d", len(a.sideRows))
	}
	a.data.Conversations[0].Title = "Something else"
	a.rebuildSide(now)
	if len(a.sideRows) != 2 {
		t.Fatalf("row dropped while the query stayed: %d", len(a.sideRows))
	}
	a.query = "missing"
	a.rebuildSide(now)
	if len(a.sideRows) != 0 {
		t.Fatalf("new query kept the row: %+v", a.sideRows)
	}
}

func titleTestApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	a.data.AutoTitles = true
	a.data.Connections = []Connection{{
		ID: "openai", Name: "OpenAI", Provider: "OpenAI", BaseURL: "https://api.openai.com/v1",
		ModelsSet: true,
		Models:    []ProviderModel{{ID: "gpt-4o-mini", Enabled: true}, {ID: "gpt-4o", Enabled: true}},
	}}
	if err := a.keys.Set("openai", "sk-test"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	a.data.Conversations = []Conversation{{
		ID: "chat", Title: newChatTitle, CreatedAt: now, UpdatedAt: now, Loaded: true,
		Config: Config{Provider: "OpenAI", Model: "gpt-4o-mini", ConnectionID: "openai", BaseURL: "https://api.openai.com/v1"},
		Messages: []Message{
			{ID: "u", Role: roleUser, Content: "How do I cancel an in-flight HTTP request when the window closes?", Done: true, CreatedAt: now},
			{ID: "a", Role: roleAssistant, Content: "Use a context.", Done: true, CreatedAt: now},
		},
	}}
	a.data.ActiveID = "chat"
	a.saveSync()
	return a
}
