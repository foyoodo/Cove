package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestMain(m *testing.M) {
	// A finished reply must not also post a title during the rest of the
	// suite. Title tests opt back in.
	openAppHook = func(a *App) {
		a.data.AutoTitles = false
	}
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
		{in: "「Quoted title」", want: "Quoted title", ok: true},
		{in: "「Quoted title」。", want: "Quoted title", ok: true},
		{in: "What's the plan", want: "What's the plan", ok: true},
		{in: "Don’t stop", want: "Don’t stop", ok: true},
		{in: "’Don’t stop’", want: "Don’t stop", ok: true},
		{in: "Done。", want: "Done", ok: true},
		{in: "Hello:", want: "Hello", ok: true},
		{in: "Hello，", want: "Hello", ok: true},
		{in: "Ship it 🚀", want: "Ship it", ok: true},
		{in: "⭐ Ship", want: "Ship", ok: true},
		{in: "⏰ Wake", want: "Wake", ok: true},
		{in: "⌚ time", want: "time", ok: true},
		{in: "Save with ⌘S", want: "Save with ⌘S", ok: true},
		{in: "⌘⌥⌃⇧", want: "⌘⌥⌃⇧", ok: true},
		{in: "Press ⌘⌫", want: "Press ⌘⌫", ok: true},
		{in: "⏰ then ⌘K", want: "then ⌘K", ok: true},
		{in: "⬆ Up", want: "Up", ok: true},
		{in: "© Copy", want: "Copy", ok: true},
		{in: "©️ Copy", want: "Copy", ok: true},
		{in: "A\u200dB", want: "AB", ok: true},
		{in: "Star\uFE0F left", want: "Star left", ok: true},
		{in: "Hello\nworld.", want: "Hello world", ok: true},
		{in: "<think>secret</think>Actual title", want: "Actual title", ok: true},
		{in: "<THINKING>hidden</THINKING> Visible", want: "Visible", ok: true},
		{in: "before <reasoning>hide</reasoning> after.", want: "before after", ok: true},
		{in: "<think>no closer", ok: false},
		{in: "title <think>still open", ok: false},
		{in: "\"\"", ok: false},
		{in: "🎉", ok: false},
		{in: "   ", ok: false},
		{in: strings.Repeat("字", 16), want: strings.Repeat("字", 15), ok: true},
		{in: "one two three four five six seven eight", want: "one two three four five six", ok: true},
		{in: "abcdefghijklmnopqrstuvwxyz012:3456789", want: "abcdefghijklmnopqrstuvwxyz012", ok: true},
		{in: "abcdefghijklmnopqrstuvwxyz01：3456789", want: "abcdefghijklmnopqrstuvwxyz01", ok: true},
		{in: "abcdefghijklmnopqrstuvwxyz012,3456789", want: "abcdefghijklmnopqrstuvwxyz012", ok: true},
		{in: "abcdefghijklmnopqrstuvwxyz01，3456789", want: "abcdefghijklmnopqrstuvwxyz01", ok: true},
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

func TestTitleRequestParams(t *testing.T) {
	turns := []apiMessage{{Role: roleUser, Content: "hello"}, {Role: roleAssistant, Content: "world"}}
	cases := []struct {
		name   string
		cfg    Config
		omit   bool
		capKey string
		effort string // empty means no reasoning_effort
		extra  string // a substring the body must contain, or ""
	}{
		{name: "gpt-4o", cfg: Config{Provider: "OpenAI", Model: "gpt-4o"}, capKey: "max_completion_tokens"},
		{name: "vendor gpt-4o", cfg: Config{Provider: "OpenAI", Model: "openai/gpt-4o"}, capKey: "max_completion_tokens"},
		{name: "o3", cfg: Config{Provider: "OpenAI", Model: "o3-mini"}, capKey: "max_completion_tokens", effort: "low"},
		{name: "vendor o3", cfg: Config{Provider: "OpenAI", Model: "openai/o3"}, capKey: "max_completion_tokens", effort: "low"},
		{name: "o1", cfg: Config{Provider: "OpenAI", Model: "o1-preview"}, capKey: "max_completion_tokens", effort: "low"},
		{name: "gpt-5", cfg: Config{Provider: "OpenAI", Model: "gpt-5"}, capKey: "max_completion_tokens", effort: "minimal"},
		{name: "gpt-5.1", cfg: Config{Provider: "OpenAI", Model: "gpt-5.1"}, capKey: "max_completion_tokens", effort: "none"},
		{name: "gpt-5.2", cfg: Config{Provider: "OpenAI", Model: "gpt-5.2-mini"}, capKey: "max_completion_tokens", effort: "none"},
		{name: "grok-4", cfg: Config{Provider: "xAI", Model: "grok-4"}, capKey: "max_tokens"},
		{name: "grok-4-fast", cfg: Config{Provider: "xAI", Model: "grok-4-fast"}, capKey: "max_tokens"},
		{name: "grok-3-mini", cfg: Config{Provider: "xAI", Model: "grok-3-mini"}, capKey: "max_tokens"},
		{name: "deepseek-r1", cfg: Config{Provider: "DeepSeek", Model: "deepseek-r1"}, capKey: "max_tokens", extra: `"type":"disabled"`},
		{name: "vendor r1", cfg: Config{Provider: "DeepSeek", Model: "deepseek/deepseek-reasoner"}, capKey: "max_tokens", extra: `"type":"disabled"`},
		{name: "qwq", cfg: Config{Provider: "Custom", Model: "qwq-32b", ReasoningStyle: "None"}, capKey: "max_tokens"},
		{name: "qwen3", cfg: Config{Provider: "Custom", Model: "qwen3-32b", ReasoningStyle: "None"}, capKey: "max_tokens"},
		{name: "gpt-oss", cfg: Config{Provider: "Custom", Model: "gpt-oss-120b", ReasoningStyle: "None"}, capKey: "max_tokens"},
		{name: "glm", cfg: Config{Provider: "Custom", Model: "glm-4.5", ReasoningStyle: "None"}, capKey: "max_tokens"},
		{name: "gemini", cfg: Config{Provider: "Custom", Model: "gemini-2.5-pro", ReasoningStyle: "None"}, capKey: "max_tokens"},
		{name: "default chat omits", cfg: Config{Provider: "OpenAI", Model: "o3"}, omit: true, capKey: "max_completion_tokens"},
		{name: "openrouter", cfg: Config{Provider: "OpenRouter", Model: "openai/gpt-oss-120b"}, capKey: "max_tokens", extra: `"enabled":false`},
		{name: "ollama", cfg: Config{Provider: "Ollama", Model: "qwen3"}, capKey: "max_tokens", effort: "none", extra: `"think":false`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := titleRequestBody(tc.cfg, turns, tc.omit)
			if err != nil {
				t.Fatal(err)
			}
			assertTitleBody(t, body, tc.capKey, titleTokensReason)
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatal(err)
			}
			got, _ := payload["reasoning_effort"].(string)
			if got != tc.effort {
				t.Fatalf("reasoning_effort %q, want %q in %s", got, tc.effort, body)
			}
			if tc.extra != "" && !strings.Contains(string(body), tc.extra) {
				t.Fatalf("body %s missing %s", body, tc.extra)
			}
			if tc.omit {
				for _, key := range []string{"reasoning_effort", "reasoning", "thinking", "think"} {
					if _, ok := payload[key]; ok {
						t.Fatalf("default chat sent %s in %s", key, body)
					}
				}
			}
		})
	}
}

func TestTitleRetriesWithoutReasoning(t *testing.T) {
	turns := []apiMessage{{Role: roleUser, Content: "hello"}, {Role: roleAssistant, Content: "world"}}
	body, err := titleRequestBody(Config{Provider: "OpenAI", Model: "o3"}, turns, false)
	if err != nil {
		t.Fatal(err)
	}
	var calls [][]byte
	prev := titleComplete
	titleComplete = func(_ context.Context, _ Config, b []byte) (string, error) {
		calls = append(calls, append([]byte(nil), b...))
		if len(calls) == 1 {
			return "", &StatusError{Status: 400, Body: "unknown parameter reasoning_effort"}
		}
		return "Retried title", nil
	}
	t.Cleanup(func() { titleComplete = prev })
	text, err := completeTitle(context.Background(), Config{Provider: "OpenAI", Model: "o3"}, body)
	if err != nil {
		t.Fatal(err)
	}
	if text != "Retried title" || len(calls) != 2 {
		t.Fatalf("text %q calls %d", text, len(calls))
	}
	var second map[string]any
	if err := json.Unmarshal(calls[1], &second); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"reasoning_effort", "reasoning", "thinking", "think"} {
		if _, ok := second[key]; ok {
			t.Fatalf("retry still has %s", key)
		}
	}
	if _, ok := second["max_completion_tokens"]; !ok {
		t.Fatalf("retry dropped the cap: %s", calls[1])
	}

	calls = nil
	titleComplete = func(_ context.Context, _ Config, b []byte) (string, error) {
		calls = append(calls, b)
		return "", &StatusError{Status: 500, Body: "down"}
	}
	_, err = completeTitle(context.Background(), Config{}, body)
	if statusOf(err) != 500 || len(calls) != 1 {
		t.Fatalf("500 retried: calls %d err %v", len(calls), err)
	}

	calls = nil
	plain, err := titleRequestBody(Config{Provider: "OpenAI", Model: "gpt-4o"}, turns, true)
	if err != nil {
		t.Fatal(err)
	}
	titleComplete = func(_ context.Context, _ Config, b []byte) (string, error) {
		calls = append(calls, b)
		return "", &StatusError{Status: 400, Body: "other"}
	}
	_, err = completeTitle(context.Background(), Config{}, plain)
	if len(calls) != 1 {
		t.Fatalf("plain 400 retried: %d", len(calls))
	}
}

func assertTitleBody(t *testing.T, body []byte, capKey string, cap int) {
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
	cfg, usingChat, ok := a.titleConfig(conv, true)
	if !ok || !usingChat || cfg.Model != "gpt-4o-mini" || cfg.ConnectionID != "openai" {
		t.Fatalf("same as chat: ok %v using %v model %s conn %s", ok, usingChat, cfg.Model, cfg.ConnectionID)
	}
	a.data.TitleModel = &TitleModel{ProviderID: "openai", ModelID: "gpt-4o"}
	cfg, usingChat, ok = a.titleConfig(conv, true)
	if !ok || usingChat || cfg.Model != "gpt-4o" {
		t.Fatalf("picked model: ok %v using %v %s", ok, usingChat, cfg.Model)
	}
	a.data.TitleModel = &TitleModel{ProviderID: "missing", ModelID: "grok-4-fast"}
	cfg, usingChat, ok = a.titleConfig(conv, true)
	if !ok || !usingChat || cfg.Model != conv.Config.Model || cfg.ConnectionID != conv.Config.ConnectionID {
		t.Fatalf("fallback ok %v using %v cfg %+v", ok, usingChat, cfg)
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
		ID: "chat", Title: newChatTitle, TitleSource: titleSourceTemp, CreatedAt: now, UpdatedAt: now, Loaded: true,
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

func TestLegacyChatKeepsItsTitle(t *testing.T) {
	dir := t.TempDir()
	chatDir := filepath.Join(dir, "chats")
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := strings.Join([]string{
		`{"type":"chat","schema":5,"id":"old","createdAt":"2026-01-02T15:04:05Z","title":"Notes from last year","settings":{}}`,
		`{"type":"message","id":"u","role":"user","content":"hello","status":"complete"}`,
		`{"type":"message","id":"a","role":"assistant","content":"world","status":"complete"}`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(chatDir, "old.jsonl"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	conv, err := readChat(filepath.Join(chatDir, "old.jsonl"), Config{})
	if err != nil {
		t.Fatal(err)
	}
	if !conv.TitleGenerated || conv.TitleSource != "" || conv.Title != "Notes from last year" {
		t.Fatalf("loaded legacy title %q source %q generated %v", conv.Title, conv.TitleSource, conv.TitleGenerated)
	}
	empty := strings.Join([]string{
		`{"type":"chat","schema":5,"id":"fresh","createdAt":"2026-01-02T15:04:05Z","title":"New Chat","settings":{}}`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(chatDir, "fresh.jsonl"), []byte(empty), 0o644); err != nil {
		t.Fatal(err)
	}
	fresh, err := readChat(filepath.Join(chatDir, "fresh.jsonl"), Config{})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.TitleGenerated || fresh.TitleSource != "" {
		t.Fatalf("empty chat was protected: generated %v source %q", fresh.TitleGenerated, fresh.TitleSource)
	}
	indexed := convFromIndex(indexEntry{ID: "old", Title: "Notes from last year", MessageCount: 2})
	if !indexed.TitleGenerated || indexed.Title != "Notes from last year" {
		t.Fatalf("index row %+v generated %v", indexed.Title, indexed.TitleGenerated)
	}
	if convFromIndex(indexEntry{ID: "fresh", Title: "New Chat"}).TitleGenerated {
		t.Fatal("empty index row was protected")
	}

	var n int
	prev := titleComplete
	titleComplete = func(context.Context, Config, []byte) (string, error) {
		n++
		return "Replaced", nil
	}
	t.Cleanup(func() { titleComplete = prev })
	a := titleTestApp(t)
	chat := a.conversation("chat")
	chat.Title = "Hand named"
	chat.TitleSource = ""
	chat.TitleGenerated = false
	a.maybeAutoTitle("chat")
	a.finishReplyTitle("chat")
	if n != 0 || chat.Title != "Hand named" || chat.TitleGenerated {
		t.Fatalf("legacy in-memory chat was titled: %q requests %d generated %v", chat.Title, n, chat.TitleGenerated)
	}
}

func TestRegenerateReplacesManualTitle(t *testing.T) {
	prev := titleComplete
	titleComplete = func(context.Context, Config, []byte) (string, error) {
		return "Fresh title", nil
	}
	t.Cleanup(func() { titleComplete = prev })
	a := titleTestApp(t)
	conv := a.conversation("chat")
	conv.Title = "Hand named"
	conv.TitleSource = titleSourceManual
	conv.TitleGenerated = true
	a.regenerateTitle("chat", false)
	if conv.Title != "Fresh title" || conv.TitleSource != titleSourceAuto {
		t.Fatalf("regenerate left %q %s", conv.Title, conv.TitleSource)
	}
}

func TestRenameCancelsInFlightTitle(t *testing.T) {
	a := titleTestApp(t)
	conv := a.conversation("chat")
	conv.Title = "Hand named"
	conv.TitleSource = titleSourceManual
	cancelled := false
	a.titleGens = map[string]int{"chat": 1}
	a.titleJobs = map[string]*titleJob{"chat": {gen: 1, manual: true, cancel: func() { cancelled = true }}}
	a.beginRename()
	if !cancelled || a.titleJobs["chat"] != nil || !a.renameOpen {
		t.Fatalf("beginRename cancel %v job %#v open %v", cancelled, a.titleJobs["chat"], a.renameOpen)
	}
	a.finishTitle("chat", 1, true, "Generated during rename", nil)
	if conv.Title != "Hand named" || conv.TitleSource != titleSourceManual {
		t.Fatalf("late result during rename: %q %s", conv.Title, conv.TitleSource)
	}
	a.renameText = "Typed name"
	a.titleGens["chat"] = 2
	a.titleJobs["chat"] = &titleJob{gen: 2, manual: true, cancel: func() {}}
	a.commitRename()
	if conv.Title != "Typed name" || conv.TitleSource != titleSourceManual || a.renameOpen {
		t.Fatalf("commit %#v %s open %v", conv.Title, conv.TitleSource, a.renameOpen)
	}
	a.finishTitle("chat", 2, true, "Generated after rename", nil)
	if conv.Title != "Typed name" || conv.TitleSource != titleSourceManual {
		t.Fatalf("late result after commit: %q %s", conv.Title, conv.TitleSource)
	}
}

func TestChatDefaultReasoningOmitsFields(t *testing.T) {
	var body []byte
	prev := titleComplete
	titleComplete = func(_ context.Context, _ Config, b []byte) (string, error) {
		body = append([]byte(nil), b...)
		return "Named", nil
	}
	t.Cleanup(func() { titleComplete = prev })
	a := titleTestApp(t)
	conv := a.conversation("chat")
	conv.Config.Model = "o3"
	conv.Config.Reasoning = ""
	a.maybeAutoTitle("chat")
	if strings.Contains(string(body), "reasoning") {
		t.Fatalf("default chat sent reasoning: %s", body)
	}
	a.data.Connections[0].Models = append(a.data.Connections[0].Models, ProviderModel{ID: "o3", Enabled: true})
	a.data.TitleModel = &TitleModel{ProviderID: "openai", ModelID: "o3"}
	conv.Title = newChatTitle
	conv.TitleSource = titleSourceTemp
	conv.TitleGenerated = false
	a.maybeAutoTitle("chat")
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["reasoning_effort"] != "low" {
		t.Fatalf("dedicated o3 effort %#v in %s", payload["reasoning_effort"], body)
	}
}

func TestCanRegenerateUsesModel(t *testing.T) {
	a := titleTestApp(t)
	conv := a.conversation("chat")
	if !a.canRegenerateTitle(conv) {
		t.Fatal("a chat with a model should regenerate")
	}
	conv.Config.Model = ""
	if a.canRegenerateTitle(conv) {
		t.Fatal("empty model")
	}
	conv.Config.Model = "gpt-4o-mini"
	conv.Config.ConnectionID = "gone"
	if a.canRegenerateTitle(conv) {
		t.Fatal("missing connection")
	}
	conv.Config.ConnectionID = ""
	a.data.Connections = nil
	if a.canRegenerateTitle(conv) {
		t.Fatal("no providers")
	}
}

func TestTitleFailAccessLabel(t *testing.T) {
	a := titleTestApp(t)
	now := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	a.rebuildSide(now)
	a.titleFails = map[string]time.Time{"chat": now}
	found := false
	for i := range a.sideRows {
		if strings.Contains(a.sideAccessLabel(i, now), titleFailText) {
			found = true
		}
	}
	if !found {
		t.Fatal("warning row label omitted the failure")
	}
}

func TestTitleModelMenuShowsModelName(t *testing.T) {
	a := titleSettingsApp(t, true, &TitleModel{ProviderID: "missing", ModelID: "grok-4-fast"})
	items, _, _ := a.titleModelEntries()
	sawReasoner := false
	sawTogether := false
	for _, e := range items {
		if e.Kind != popItem || e.ID == titleModelSame {
			continue
		}
		if strings.Contains(e.Text, " · ") {
			t.Fatalf("row %q includes the provider; the header carries it", e.Text)
		}
		if e.Text == "deepseek-reasoner" {
			sawReasoner = true
		}
		if e.Group == "Together AI" && e.Text == "meta-llama/Llama-3-70b" {
			sawTogether = true
		}
		if e.Text == "grok-4-fast" && (!e.Disabled || e.Secondary != titleUnavailable || !e.Checked) {
			t.Fatalf("unavailable row %+v", e)
		}
	}
	if !sawReasoner || !sawTogether {
		t.Fatalf("menu missing deepseek-reasoner (%v) or Together (%v)", sawReasoner, sawTogether)
	}
}

// TestTitleAsyncCancel runs the real title goroutine. The callback is
// delivered back to this goroutine, so finishTitle never overlaps the
// cancel. go test -race covers the handoff.
// TestTitleTimeoutKeepsTemporaryTitle is an automatic request whose
// server never answers. The deadline is the same context the app uses,
// shortened so the test does not wait 15s. SPEC §20: a timeout keeps the
// temporary title, stays silent, and titleGenerated stays recorded so the
// chat is not asked again. titleSource stays temp.
func TestTitleTimeoutKeepsTemporaryTitle(t *testing.T) {
	prevDur := titleTimeout
	titleTimeout = 15 * time.Millisecond
	t.Cleanup(func() { titleTimeout = prevDur })
	var got error
	prev := titleComplete
	titleComplete = func(ctx context.Context, _ Config, _ []byte) (string, error) {
		<-ctx.Done()
		got = ctx.Err()
		return "Should not apply", got
	}
	t.Cleanup(func() { titleComplete = prev })

	a := titleTestApp(t)
	conv := a.conversation("chat")
	const temp = "How do I cancel an in-flight HTTP request"
	conv.Title = temp
	conv.TitleSource = titleSourceTemp
	conv.Config.APIKey = "sk-test"
	start := time.Now()
	a.maybeAutoTitle("chat")
	if waited := time.Since(start); waited > time.Second {
		t.Fatalf("timeout path waited %s", waited)
	}
	if !errors.Is(got, context.DeadlineExceeded) {
		t.Fatalf("request ended with %v", got)
	}
	if conv.Title != temp || conv.TitleSource != titleSourceTemp || !conv.TitleGenerated {
		t.Fatalf("after timeout title %q source %s generated %v", conv.Title, conv.TitleSource, conv.TitleGenerated)
	}
	if a.announce != "" || len(a.titleFails) != 0 || a.titleBusy("chat") {
		t.Fatalf("timeout surfaced announce %q fails %d busy %v", a.announce, len(a.titleFails), a.titleBusy("chat"))
	}
	tt := ui.NewTester(a.view, 1100, 760)
	if tt.HasText(titleFailText) {
		t.Fatal("timeout painted the failure warning")
	}
}

// TestTitleApplyKeepsComposerFocus lands a title while the composer is
// first responder, the draft is non-empty, and the caret is in the middle.
func TestTitleApplyKeepsComposerFocus(t *testing.T) {
	a := titleTestApp(t)
	conv := a.conversation("chat")
	conv.Title = "How do I cancel"
	conv.TitleSource = titleSourceTemp
	conv.Draft = ""
	tt := ui.NewTester(a.view, 1100, 760)
	if err := tt.Click("Message"); err != nil {
		t.Fatal(err)
	}
	tt.Type("abcdefghij")
	for range 5 {
		tt.Key(0, ui.KeyLeft)
	}
	if !tt.Focused("Message") {
		t.Fatal("composer is not first responder")
	}
	if !a.composerCaretSet || a.composerAnchor != 5 || a.composerCaretPos != 5 {
		t.Fatalf("caret anchor %d pos %d set %v", a.composerAnchor, a.composerCaretPos, a.composerCaretSet)
	}
	anchor, caret := a.composerAnchor, a.composerCaretPos
	a.titleGens = map[string]int{conv.ID: 1}
	a.titleJobs = map[string]*titleJob{conv.ID: {gen: 1}}
	a.finishTitle(conv.ID, 1, false, "Cancel HTTP on close", nil)
	tt.Frame()
	if conv.Title != "Cancel HTTP on close" || conv.TitleSource != titleSourceAuto {
		t.Fatalf("title did not land: %q %s", conv.Title, conv.TitleSource)
	}
	if !tt.Focused("Message") {
		t.Fatal("title moved first responder")
	}
	if conv.Draft != "abcdefghij" {
		t.Fatalf("draft changed to %q", conv.Draft)
	}
	if a.composerAnchor != anchor || a.composerCaretPos != caret {
		t.Fatalf("caret %d,%d want %d,%d", a.composerAnchor, a.composerCaretPos, anchor, caret)
	}
	tt.Type("X")
	if conv.Draft != "abcdeXfghij" {
		t.Fatalf("caret was not still in the middle: %q", conv.Draft)
	}
}

func TestTitleAsyncCancel(t *testing.T) {
	runTitleAsync(t,
		func(a *App) { a.maybeAutoTitle("chat") },
		func(a *App) { a.deleteConversation("chat") },
		false,
		func(a *App) {
			if a.conversation("chat") != nil {
				t.Fatal("deleted chat came back")
			}
		},
	)
	runTitleAsync(t,
		func(a *App) { a.maybeAutoTitle("chat") },
		func(a *App) { a.persistForExit() },
		false,
		func(a *App) {
			conv := a.conversation("chat")
			if conv == nil || conv.Title != newChatTitle || conv.TitleSource == titleSourceAuto {
				t.Fatalf("quit applied a title: %+v", conv)
			}
			if a.titleBusy("chat") {
				t.Fatal("quit left the job")
			}
		},
	)
	runTitleAsync(t,
		func(a *App) { a.maybeAutoTitle("chat") },
		func(a *App) { a.cancelTitle("chat", false) },
		false,
		func(a *App) {
			conv := a.conversation("chat")
			if conv.Title != newChatTitle || conv.TitleSource == titleSourceAuto {
				t.Fatalf("stale result applied: %q %s", conv.Title, conv.TitleSource)
			}
		},
	)
	runTitleAsync(t,
		func(a *App) { a.maybeAutoTitle("chat") },
		func(a *App) {
			a.data.AutoTitles = false
			a.cancelAutoTitles()
		},
		false,
		func(a *App) {
			conv := a.conversation("chat")
			if conv.Title != newChatTitle || a.titleBusy("chat") {
				t.Fatalf("toggle off applied %q busy %v", conv.Title, a.titleBusy("chat"))
			}
		},
	)
	runTitleAsync(t,
		func(a *App) {
			conv := a.conversation("chat")
			conv.Title = "Hand named"
			conv.TitleSource = titleSourceManual
			a.regenerateTitle("chat", false)
		},
		func(a *App) { a.cancelAutoTitles() },
		true,
		func(a *App) {
			conv := a.conversation("chat")
			if conv.Title != "Async Title" || conv.TitleSource != titleSourceAuto {
				t.Fatalf("manual regenerate was dropped by the toggle: %q %s", conv.Title, conv.TitleSource)
			}
		},
	)
}

func runTitleAsync(t *testing.T, start func(*App), during func(*App), release bool, check func(*App)) {
	t.Helper()
	a := titleTestApp(t)
	a.titleAsync = true
	a.conversation("chat").Config.APIKey = "sk-test"
	started := make(chan struct{})
	letGo := make(chan struct{})
	prev := titleComplete
	t.Cleanup(func() { titleComplete = prev })
	titleComplete = func(ctx context.Context, _ Config, _ []byte) (string, error) {
		close(started)
		select {
		case <-ctx.Done():
			return "Late title", ctx.Err()
		case <-letGo:
			return "Async Title", nil
		}
	}
	callbacks := make(chan func(), 2)
	a.uiSync = func(fn func()) { callbacks <- fn }
	start(a)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("title goroutine did not start")
	}
	during(a)
	if release {
		close(letGo)
	}
	select {
	case fn := <-callbacks:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("title callback did not return")
	}
	titleComplete = prev
	check(a)
}

// TestSidebarRegenerateDimsCurrentTitleBar is the sidebar context menu.
// The chat already on screen greys the title bar with its row. Another
// chat only spins in the sidebar, and the open chat stays put.
func TestSidebarRegenerateDimsCurrentTitleBar(t *testing.T) {
	question := "How do I cancel an in-flight HTTP request when the window closes?"
	a := titleShotApp(t, question)
	tt := ui.NewTester(a.view, 1120, 760)
	prev := titleComplete
	t.Cleanup(func() { titleComplete = prev })

	check := func(id string, wantBar bool) {
		t.Helper()
		titleComplete = func(context.Context, Config, []byte) (string, error) {
			job := a.titleJobs[id]
			if job == nil || !job.manual || job.bar != wantBar {
				t.Errorf("job for %s = %#v, want manual bar=%v", id, job, wantBar)
			}
			if a.data.ActiveID != "cancel" {
				t.Errorf("active chat changed to %s", a.data.ActiveID)
			}
			return "", errors.New("stopped")
		}
	}
	rowLabel := func(id string) string {
		t.Helper()
		conv := a.conversation(id)
		when := conv.UpdatedAt
		meta := sidebarWhen(tt.Now(), when)
		return conv.Title + ", " + meta
	}

	check("cancel", true)
	if err := tt.RightClick(rowLabel("cancel")); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(titleRegenLabel); err != nil {
		t.Fatal(err)
	}

	check("greet", false)
	if err := tt.RightClick(rowLabel("greet")); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(titleRegenLabel); err != nil {
		t.Fatal(err)
	}
}

// TestTitleModelHoverFollowsPointer is SPEC §11.3.4: the pointer and the
// keyboard share one highlight. Opening paints the current row. The next
// frame after the pointer moves onto another row follows it.
func TestTitleModelHoverFollowsPointer(t *testing.T) {
	a := titleSettingsApp(t, true, nil)
	tt := ui.NewTester(a.settingsWindowRoot, 780, 560)
	scrollTitlesIntoView(t, a, tt)
	if err := tt.Click("Title model, " + titleSameAsChat); err != nil {
		t.Fatal(err)
	}
	if hot := hotLabeled(kindMarks(t, tt, "title")); hot.label != titleSameAsChat {
		t.Fatalf("open highlight %q", hot.label)
	}
	var row ui.Rect
	for _, r := range kindMarks(t, tt, "title") {
		if r.label == "gpt-4o-mini" {
			row = r.rect
		}
	}
	if row.W < 8 {
		t.Fatal("gpt-4o-mini row missing")
	}
	tt.Move(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	hot := hotLabeled(kindMarks(t, tt, "title"))
	if hot.label != "gpt-4o-mini" {
		t.Fatalf("hover highlight %q, want gpt-4o-mini", hot.label)
	}
	still := false
	for _, r := range kindMarks(t, tt, "title") {
		if r.label == titleSameAsChat && r.checked {
			still = true
		}
	}
	if !still {
		t.Fatal("Same as Chat lost its check when the highlight moved")
	}
}

// TestTitleSpinnerIsTenPoints checks the sidebar spinner. The box is 10pt
// and the arc's ink fills that box; a smaller path reads as about 8pt.
func TestTitleSpinnerIsTenPoints(t *testing.T) {
	a := titleShotApp(t, "Cancel HTTP on close")
	a.titleJobs = map[string]*titleJob{"cancel": {gen: 1, manual: true, bar: true}}
	tt := ui.NewTester(a.view, 1120, 760)
	r, ok := tt.Find(titleGenerating)
	if !ok {
		t.Fatal("spinner missing")
	}
	if r.W < 9.5 || r.W > 10.5 || r.H < 9.5 || r.H > 10.5 {
		t.Fatalf("spinner box %.2fx%.2f, want 10pt", r.W, r.H)
	}
	img := tt.Image()
	bx, by := int(r.X)-3, int(r.Y+r.H/2)
	br, bg, bb, _ := img.At(bx, by).RGBA()
	minX, minY := 1<<20, 1<<20
	maxX, maxY := -1, -1
	for y := int(r.Y); y < int(r.Y+r.H+0.5); y++ {
		for x := int(r.X); x < int(r.X+r.W+0.5); x++ {
			rr, gg, b2, _ := img.At(x, y).RGBA()
			if channelGap(rr, br)+channelGap(gg, bg)+channelGap(b2, bb) < 48 {
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
	w, h := maxX-minX+1, maxY-minY+1
	if w < 9 || h < 9 {
		t.Fatalf("spinner ink %dx%d inside %.1fx%.1f, want about 10pt", w, h, r.W, r.H)
	}
}

func channelGap(a, b uint32) int {
	da, db := int(a>>8), int(b>>8)
	if da < db {
		return db - da
	}
	return da - db
}
