package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"cove/themes"
)

func TestMigrateV2KeepsDataAndDefaultsAppearance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversations.json")
	raw := []byte(`{"version":2,"defaults":{"provider":"OpenAI","model":"kept-model","baseUrl":"https://api.openai.com/v1","systemPrompt":"stay"},"conversations":[{"id":"c1","title":"Kept","config":{"provider":"DeepSeek","model":"deepseek-flash","baseUrl":"https://api.deepseek.com/v1"},"messages":[{"id":"m","role":"user","content":"hello","done":true}],"draft":"later"}],"activeId":"c1","sidebarWidth":300}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != storeVersion {
		t.Fatalf("version %d", got.Version)
	}
	if got.Appearance != appearanceSystem {
		t.Fatalf("appearance %q", got.Appearance)
	}
	if got.Defaults.Model != "kept-model" || got.Defaults.SystemPrompt != "stay" || got.SidebarWidth != 300 {
		t.Fatalf("defaults changed: %+v width %v", got.Defaults, got.SidebarWidth)
	}
	if len(got.Conversations) != 1 || got.Conversations[0].Title != "Kept" || got.Conversations[0].Draft != "later" {
		t.Fatalf("chat lost: %+v", got.Conversations)
	}
	if got.Conversations[0].Config.Model != "deepseek-flash" || got.Conversations[0].Messages[0].Content != "hello" {
		t.Fatalf("message lost: %+v", got.Conversations[0])
	}
	if got.ActiveID != "c1" {
		t.Fatalf("active %q", got.ActiveID)
	}
}

// TestMigrateV06AndV070 loads the on-disk shapes those builds wrote and
// checks schema 4 keeps every chat and every draft that has text.
// v0.6 is schema 2 (no appearance, no theme). v0.7.0 is schema 3
// (appearance, no theme) and schema 4 (theme and the three preferences).
// TestMigrateV2KeepsDataAndDefaultsAppearance and TestLoadSortsAndRepairs
// cover the same loader for a version 2 chat and a version 1 file.
func TestMigrateV06AndV070(t *testing.T) {
	v06 := `{
		"version": 2,
		"defaults": {"provider":"OpenAI","model":"gpt-4o-mini","baseUrl":"https://api.openai.com/v1","systemPrompt":"stay"},
		"sidebarWidth": 280,
		"activeId": "chat",
		"conversations": [
			{"id":"chat","title":"From v0.6","updatedAt":"2026-01-02T00:00:00Z","config":{"provider":"DeepSeek","model":"deepseek-flash","baseUrl":"https://api.deepseek.com/v1"},"messages":[{"id":"m","role":"user","content":"hello from v0.6","done":true}],"draft":"add a closer"},
			{"id":"note","title":"New Chat","updatedAt":"2026-01-03T00:00:00Z","config":{"provider":"OpenAI","model":"gpt-4o-mini","baseUrl":"https://api.openai.com/v1"},"messages":[],"draft":"unsent note"},
			{"id":"blank","title":"New Chat","updatedAt":"2026-01-01T00:00:00Z","messages":[],"draft":"   "}
		]
	}`
	v070Appearance := `{
		"version": 3,
		"appearance": "dark",
		"defaults": {"provider":"OpenAI","model":"kept-model","baseUrl":"https://api.openai.com/v1","systemPrompt":"stay"},
		"sidebarWidth": 240,
		"activeId": "draft",
		"conversations": [
			{"id":"chat","title":"From v0.7.0","updatedAt":"2026-02-02T00:00:00Z","config":{"provider":"OpenAI","model":"gpt-4o-mini","baseUrl":"https://api.openai.com/v1"},"messages":[{"id":"m","role":"assistant","content":"kept reply","done":true}]},
			{"id":"draft","title":"Old title","updatedAt":"2026-02-03T00:00:00Z","messages":[],"draft":"finish the outline"},
			{"id":"blank","title":"New Chat","updatedAt":"2026-02-01T00:00:00Z","messages":[],"draft":""}
		]
	}`
	v070Theme := `{
		"version": 4,
		"appearance": "light",
		"theme": "sage",
		"showReasoning": false,
		"restoreLast": false,
		"keepLastRequest": true,
		"defaults": {"provider":"Anthropic","model":"claude-sonnet-4.5","baseUrl":"https://api.anthropic.com/v1","systemPrompt":"stay"},
		"sidebarWidth": 220,
		"activeId": "chat",
		"conversations": [
			{"id":"chat","title":"Sage chat","updatedAt":"2026-03-02T00:00:00Z","config":{"provider":"Anthropic","model":"claude-sonnet-4.5","baseUrl":"https://api.anthropic.com/v1"},"messages":[{"id":"m","role":"user","content":"keep the sage chat","done":true}],"draft":"still drafting"},
			{"id":"blank","title":"New Chat","updatedAt":"2026-03-01T00:00:00Z","messages":[],"draft":""}
		]
	}`
	t.Run("v0.6", func(t *testing.T) {
		got := loadRaw(t, v06)
		if got.Version != storeVersion || got.Appearance != appearanceSystem || got.Theme != themes.ThemeWarm {
			t.Fatalf("header %+v", got)
		}
		if !got.ShowReasoning || !got.RestoreLast || !got.KeepLastRequest {
			t.Fatalf("prefs reasoning %v restore %v keep %v", got.ShowReasoning, got.RestoreLast, got.KeepLastRequest)
		}
		if got.SidebarWidth != 280 || got.ActiveID != "chat" || got.Defaults.SystemPrompt != "stay" {
			t.Fatalf("defaults %+v width %v active %q", got.Defaults, got.SidebarWidth, got.ActiveID)
		}
		if len(got.Conversations) != 2 {
			t.Fatalf("chats %+v", got.Conversations)
		}
		chat := got.Conversations[1]
		if chat.ID != "chat" || chat.Title != "From v0.6" || chat.Draft != "add a closer" || chat.Messages[0].Content != "hello from v0.6" || chat.Config.Model != "deepseek-flash" {
			t.Fatalf("chat %+v", chat)
		}
		note := got.Conversations[0]
		if note.ID != "note" || note.Draft != "unsent note" || note.Title != "unsent note" || len(note.Messages) != 0 {
			t.Fatalf("draft %+v", note)
		}
		assertReloadKeeps(t, got)
	})
	t.Run("v0.7.0 appearance", func(t *testing.T) {
		got := loadRaw(t, v070Appearance)
		if got.Version != storeVersion || got.Appearance != appearanceDark || got.Theme != themes.ThemeWarm {
			t.Fatalf("header version %d appearance %q theme %q", got.Version, got.Appearance, got.Theme)
		}
		if !got.ShowReasoning || !got.RestoreLast || !got.KeepLastRequest {
			t.Fatalf("prefs were not filled in")
		}
		if got.Defaults.Model != "kept-model" || got.ActiveID != "draft" || got.SidebarWidth != 240 {
			t.Fatalf("defaults %+v active %q width %v", got.Defaults, got.ActiveID, got.SidebarWidth)
		}
		if len(got.Conversations) != 2 {
			t.Fatalf("chats %+v", got.Conversations)
		}
		draft := got.Conversations[0]
		if draft.ID != "draft" || draft.Draft != "finish the outline" || draft.Title != "finish the outline" {
			t.Fatalf("draft %+v", draft)
		}
		chat := got.Conversations[1]
		if chat.ID != "chat" || chat.Title != "From v0.7.0" || chat.Messages[0].Content != "kept reply" {
			t.Fatalf("chat %+v", chat)
		}
		assertReloadKeeps(t, got)
	})
	t.Run("v0.7.0 theme", func(t *testing.T) {
		got := loadRaw(t, v070Theme)
		if got.Version != storeVersion || got.Appearance != appearanceLight || got.Theme != themes.ThemeSage {
			t.Fatalf("header version %d appearance %q theme %q", got.Version, got.Appearance, got.Theme)
		}
		if got.ShowReasoning || got.RestoreLast || !got.KeepLastRequest {
			t.Fatalf("schema 4 prefs changed: reasoning %v restore %v keep %v", got.ShowReasoning, got.RestoreLast, got.KeepLastRequest)
		}
		if got.Defaults.Model != "claude-sonnet-4.5" || got.SidebarWidth != 220 || got.ActiveID != "chat" {
			t.Fatalf("defaults %+v width %v active %q", got.Defaults, got.SidebarWidth, got.ActiveID)
		}
		if len(got.Conversations) != 1 {
			t.Fatalf("empty draft was kept: %+v", got.Conversations)
		}
		chat := got.Conversations[0]
		if chat.Title != "Sage chat" || chat.Draft != "still drafting" || chat.Messages[0].Content != "keep the sage chat" {
			t.Fatalf("chat %+v", chat)
		}
		assertReloadKeeps(t, got)
	})
}

func loadRaw(t *testing.T, raw string) StoreData {
	t.Helper()
	path := filepath.Join(t.TempDir(), "conversations.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func assertReloadKeeps(t *testing.T, data StoreData) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "conversations.json")
	if err := Save(path, &data); err != nil {
		t.Fatal(err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.Version != storeVersion || len(again.Conversations) != len(data.Conversations) {
		t.Fatalf("reload %+v", again.Conversations)
	}
	for i := range data.Conversations {
		if again.Conversations[i].ID != data.Conversations[i].ID || again.Conversations[i].Draft != data.Conversations[i].Draft || again.Conversations[i].Title != data.Conversations[i].Title {
			t.Fatalf("reload chat %d %+v", i, again.Conversations[i])
		}
		if len(again.Conversations[i].Messages) != len(data.Conversations[i].Messages) {
			t.Fatalf("reload messages %d", i)
		}
	}
}

func TestLoadMissing(t *testing.T) {
	data, err := Load(filepath.Join(t.TempDir(), "conversations.json"))
	if err != nil {
		t.Fatal(err)
	}
	if data.Defaults.Model == "" || data.Defaults.BaseURL == "" {
		t.Fatalf("fresh store has no defaults: %+v", data.Defaults)
	}
	if len(data.Conversations) != 0 {
		t.Fatalf("fresh store has chats: %+v", data.Conversations)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Cove", "conversations.json")
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	data := defaultData()
	data.Defaults.APIKey = "sk-test"
	data.Defaults.Model = "demo"
	data.SidebarWidth = 300
	data.Conversations = []Conversation{{
		ID:    "c1",
		Title: "Alpha",
		Config: Config{
			Provider:    "OpenAI",
			Model:       "demo",
			BaseURL:     "https://example.com/v1",
			APIKey:      "sk-chat",
			Temperature: f64p(0.2),
			MaxTokens:   128,
		},
		Messages: []Message{{
			ID: "m1", Role: roleUser, Content: "Hello **there**", Done: true, CreatedAt: now,
		}},
		Draft:     "next",
		CreatedAt: now,
		UpdatedAt: now,
	}}
	data.ActiveID = "c1"
	if err := Save(path, &data); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(filepath.Dir(path), settingsFile))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("store is readable by others: %v", info.Mode().Perm())
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ActiveID != "c1" || got.Defaults.APIKey != "" || got.SidebarWidth != 300 || got.Version != storeVersion {
		t.Fatalf("header lost: %+v", got)
	}
	raw := storeBytes(t, filepath.Dir(path))
	if bytes.Contains(raw, []byte("sk-test")) || bytes.Contains(raw, []byte("sk-chat")) {
		t.Fatalf("API key was written to the store: %s", raw)
	}
	if len(got.Conversations) != 1 {
		t.Fatalf("chats: %+v", got.Conversations)
	}
	c := got.Conversations[0]
	if c.Title != "Alpha" || c.Draft != "next" || c.Config.APIKey != "" || c.Config.MaxTokens != 128 {
		t.Fatalf("conversation lost: %+v", c)
	}
	if c.Config.Temperature == nil || *c.Config.Temperature != 0.2 {
		t.Fatalf("temperature %+v", c.Config.Temperature)
	}
	if len(c.Messages) != 1 || c.Messages[0].Content != "Hello **there**" {
		t.Fatalf("message lost: %+v", c.Messages)
	}
}

func TestSaveKeepsBak(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.json")
	raw := []byte(`{"version":4,"defaults":{"provider":"OpenAI","model":"m","baseUrl":"https://api.openai.com/v1"},"conversations":[{"id":"c","title":"First","messages":[{"id":"m","role":"user","content":"Hi","done":true}]}]}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	a := openApp(path, newMemKeys())
	if a.saveBlocked {
		t.Fatal(a.loadError)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("legacy file still live: %v", err)
	}
	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bak, raw) {
		t.Fatalf("bak bytes %s", bak)
	}
	if a.conversation("c") == nil || a.conversation("c").Title != "First" {
		t.Fatalf("migrated %+v", a.data.Conversations)
	}
}

func TestCorruptFileIsNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.json")
	original := []byte("{not json, but precious")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	a := openApp(path, newMemKeys())
	if !a.saveBlocked {
		t.Fatal("saves should be paused")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("corrupt file still at the live path: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "conversations.corrupt-*.json"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("moved files %v %v", matches, err)
	}
	got, err := os.ReadFile(matches[0])
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("moved bytes %q err %v", got, err)
	}
	a.data.Conversations = []Conversation{{ID: "new", Title: "Should not land", Messages: []Message{{ID: "m", Role: roleUser, Content: "Hi", Done: true}}}}
	a.save()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("a paused save wrote a new conversations file")
	}
	a.allowSave()
	if a.saveBlocked {
		t.Fatal("start fresh left saves paused")
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Conversations) != 1 || loaded.Conversations[0].Title != "Should not land" {
		t.Fatalf("fresh store %+v", loaded.Conversations)
	}
	again, err := os.ReadFile(matches[0])
	if err != nil || !bytes.Equal(again, original) {
		t.Fatal("start fresh rewrote the moved-aside file")
	}
}

func TestCorruptRestoresBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.json")
	kept := defaultData()
	now := time.Now()
	kept.Conversations = []Conversation{{ID: "c", Title: "Kept", CreatedAt: now, UpdatedAt: now, Messages: []Message{{ID: "m", Role: roleUser, Content: "Hi", Done: true}}}}
	kept.ActiveID = "c"
	buf, err := json.Marshal(kept)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".bak", buf, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("CORRUPT"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := openApp(path, newMemKeys())
	if a.saveBlocked {
		t.Fatal(a.loadError)
	}
	if a.active() == nil || a.active().Title != "Kept" {
		t.Fatalf("restored %+v notice %q", a.data.Conversations, a.loadError)
	}
	if !strings.Contains(a.loadError, "backup") {
		t.Fatalf("notice %q", a.loadError)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "conversations.corrupt-*.json"))
	if len(matches) != 1 {
		t.Fatalf("corrupt copies %v", matches)
	}
	raw, err := os.ReadFile(matches[0])
	if err != nil || string(raw) != "CORRUPT" {
		t.Fatalf("corrupt bytes %q %v", raw, err)
	}
}

func TestMigrateAPIKeyOutOfJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.json")
	data := defaultData()
	data.Defaults.APIKey = "sk-default"
	now := time.Now()
	data.Conversations = []Conversation{
		{ID: "a", Title: "A", CreatedAt: now, UpdatedAt: now, Messages: []Message{{ID: "ma", Role: roleUser, Content: "Hi", Done: true}}, Config: Config{Provider: "OpenAI", Model: "m", BaseURL: "https://api.openai.com/v1", APIKey: "sk-shared"}},
		{ID: "b", Title: "B", CreatedAt: now, UpdatedAt: now.Add(time.Second), Messages: []Message{{ID: "mb", Role: roleUser, Content: "Hi", Done: true}}, Config: Config{Provider: "OpenAI", Model: "m", BaseURL: "https://api.openai.com/v1", APIKey: "sk-shared"}},
	}
	data.ActiveID = "b"
	buf, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatal(err)
	}
	keys := newMemKeys()
	a := openApp(path, keys)
	if a.saveBlocked {
		t.Fatal(a.loadError)
	}
	raw := storeBytes(t, dir)
	if bytes.Contains(raw, []byte("sk-")) {
		t.Fatalf("key left in the store: %s", raw)
	}
	if a.data.Defaults.APIKey != "" || a.data.Conversations[0].Config.APIKey != "" {
		t.Fatal("key left on the config")
	}
	id0 := a.data.Conversations[0].Config.ConnectionID
	id1 := a.data.Conversations[1].Config.ConnectionID
	if id0 == "" || id0 != id1 {
		t.Fatalf("shared key got connections %q %q", id0, id1)
	}
	if a.data.Defaults.ConnectionID == "" || a.data.Defaults.ConnectionID == id0 {
		t.Fatalf("defaults connection %q", a.data.Defaults.ConnectionID)
	}
	if a.resolvedKey(a.data.Defaults) != "sk-default" || a.resolvedKey(a.data.Conversations[0].Config) != "sk-shared" {
		t.Fatalf("resolved default %q shared %q", a.resolvedKey(a.data.Defaults), a.resolvedKey(a.data.Conversations[0].Config))
	}
}

func TestLoadCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversations.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("corrupt file returned no error")
	}
}

func TestLoadSortsAndRepairs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversations.json")
	raw := []byte(`{
		"version": 1,
		"defaults": {"provider": "Nope", "model": "m", "baseUrl": "http://localhost:1/v1"},
		"activeId": "missing",
		"conversations": [
			{"id": "old", "title": "", "updatedAt": "2020-01-01T00:00:00Z", "messages": [{"role": "user", "content": "a"}]},
			{"id": "new", "title": "Newer", "updatedAt": "2026-01-01T00:00:00Z", "messages": [{"role": "user", "content": "b"}]},
			{"id": "ghost", "title": "New Chat", "updatedAt": "2025-06-01T00:00:00Z"}
		]
	}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Defaults.Provider != "Custom" {
		t.Fatalf("provider %q", got.Defaults.Provider)
	}
	if got.Conversations[0].ID != "new" || got.Conversations[1].ID != "old" {
		t.Fatalf("order: %+v", got.Conversations)
	}
	if got.ActiveID != "new" {
		t.Fatalf("active %q", got.ActiveID)
	}
	if got.Conversations[1].Title != newChatTitle {
		t.Fatalf("empty title repaired to %q", got.Conversations[1].Title)
	}
	if got.Conversations[1].Messages[0].ID == "" {
		t.Fatal("message id was not repaired")
	}
	if got.Conversations[0].Messages == nil {
		t.Fatal("nil messages stayed nil")
	}
	for _, c := range got.Conversations {
		if c.ID == "ghost" {
			t.Fatal("empty draft survived load")
		}
	}
}

// storeBytes is the on-disk store except backups, corrupt copies, and
// hidden leftovers. Provider tests compare it to prove a click wrote nothing.
func storeBytes(t *testing.T, dir string) []byte {
	t.Helper()
	var buf bytes.Buffer
	for _, name := range []string{settingsFile, indexFile} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatal(err)
		}
		buf.WriteString(name)
		buf.WriteByte('\n')
		buf.Write(b)
		buf.WriteByte('\n')
	}
	chatDir := filepath.Join(dir, chatsDir)
	entries, err := os.ReadDir(chatDir)
	if err != nil {
		if os.IsNotExist(err) {
			return buf.Bytes()
		}
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(chatDir, name))
		if err != nil {
			t.Fatal(err)
		}
		buf.WriteString(name)
		buf.WriteByte('\n')
		buf.Write(b)
	}
	return buf.Bytes()
}
