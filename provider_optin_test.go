package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestProviderListStartsEmpty(t *testing.T) {
	a := newTestApp(t)
	if len(a.data.Connections) != 0 {
		t.Fatalf("fresh connections %+v", a.data.Connections)
	}
	tt := openTester(a)
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Models & Providers"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("No providers yet") || !tt.HasText("Add a provider to start chatting.") {
		t.Fatalf("empty state %q", tt.Texts())
	}
	if err := tt.Click("Add Provider…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.settingsPage != settingsAdd || !tt.HasText("OpenAI") || !tt.HasText("Custom…") {
		t.Fatalf("add page %q texts %q", a.settingsPage, tt.Texts())
	}
	if len(a.data.Connections) != 0 {
		t.Fatal("opening Add Provider wrote a connection")
	}
}

func TestAddPresetTwiceKeepsStableIDs(t *testing.T) {
	a := newTestApp(t)
	a.beginPreset("OpenAI")
	if a.provEdit.name != "OpenAI" || !a.provEdit.draft {
		t.Fatalf("draft %+v", a.provEdit)
	}
	a.provEdit.apiKey = "sk-work"
	a.commitDraft(time.Now())
	if len(a.data.Connections) != 1 || a.data.Connections[0].ID != "openai" || a.data.Connections[0].Name != "OpenAI" {
		t.Fatalf("first %+v", a.data.Connections)
	}
	if a.data.Connections[0].Format != formatOpenAI || a.data.Connections[0].BaseURL != "" {
		t.Fatalf("preset stored the official URL or the wrong format %+v", a.data.Connections[0])
	}
	if a.settingsPage != settingsDetail {
		t.Fatalf("page %q", a.settingsPage)
	}
	a.beginPreset("OpenAI")
	if a.provEdit.name != "OpenAI 2" {
		t.Fatalf("second name %q", a.provEdit.name)
	}
	a.commitDraft(time.Now())
	if len(a.data.Connections) != 2 || a.data.Connections[1].ID != "openai-2" || a.data.Connections[1].Name != "OpenAI 2" {
		t.Fatalf("second %+v", a.data.Connections)
	}
	if a.data.Connections[1].Format != formatOpenAI || a.data.Connections[1].BaseURL != "" {
		t.Fatalf("second stored %+v", a.data.Connections[1])
	}
	if a.resolvedKey(Config{ConnectionID: "openai"}) != "sk-work" {
		t.Fatal("first key missing")
	}
	a.provEdit.name = "Work OpenAI"
	a.saveProviderName(time.Now())
	renamed := a.connByID("openai-2")
	if renamed == nil || renamed.Name != "Work OpenAI" || renamed.ID != "openai-2" {
		t.Fatalf("rename %+v", renamed)
	}
}

func TestDeleteProviderRetargetsChats(t *testing.T) {
	a := newTestApp(t)
	connectBuiltin(a, "OpenAI", "sk-openai")
	a.data.Connections = append(a.data.Connections, Connection{
		ID: "openai-2", Name: "OpenAI 2", Provider: "OpenAI",
		Models: []ProviderModel{{ID: "gpt-4o", Enabled: true}}, ModelsSet: true,
	})
	if err := a.keys.Set("openai-2", "sk-personal"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	a.data.Defaults.Provider = "OpenAI"
	a.data.Defaults.Model = "gpt-4o-mini"
	a.data.Defaults.ConnectionID = "conn-openai"
	a.data.Defaults.BaseURL = officialBase("OpenAI")
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Work", CreatedAt: now, UpdatedAt: now,
		Config: Config{
			Provider: "OpenAI 2", Model: "gpt-4o", ConnectionID: "openai-2",
			BaseURL: officialBase("OpenAI"), SystemPrompt: a.data.Defaults.SystemPrompt,
		},
		Messages: []Message{{ID: "m", Role: roleUser, Content: "hi", Done: true, CreatedAt: now}},
		Loaded:   true,
	}}
	a.data.ActiveID = "c1"
	a.deleteProvider("openai-2", now)
	conv := a.conversation("c1")
	if conv.Config.ConnectionID != "conn-openai" || conv.Config.Model != "gpt-4o-mini" {
		t.Fatalf("chat %+v", conv.Config)
	}
	if conv.SwitchNote != deletedProviderNote("OpenAI 2", "gpt-4o-mini") {
		t.Fatalf("note %q", conv.SwitchNote)
	}
	if a.connByID("openai-2") != nil || a.resolvedKey(Config{ConnectionID: "openai-2"}) != "" {
		t.Fatal("provider or key still present")
	}
	tt := openTester(a)
	if !tt.HasText(deletedProviderNote("OpenAI 2", "gpt-4o-mini")) {
		t.Fatalf("composer note %q", tt.Texts())
	}
	if err := tt.Click("Dismiss"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.conversation("c1").SwitchNote != "" || tt.HasText("was removed") {
		t.Fatalf("note stayed %q texts %q", a.conversation("c1").SwitchNote, tt.Texts())
	}
}

func TestSendWithoutProviderKeepsDraft(t *testing.T) {
	a := newTestApp(t)
	now := time.Now()
	a.data.Connections = nil
	a.data.Defaults.Model = ""
	a.data.Defaults.Provider = ""
	a.data.Defaults.BaseURL = ""
	a.data.Defaults.ConnectionID = ""
	a.data.Conversations = []Conversation{{
		ID: "c", Title: newChatTitle, CreatedAt: now, UpdatedAt: now,
		Config: a.data.Defaults, Messages: []Message{}, Loaded: true,
		Draft: "keep me",
	}}
	a.data.ActiveID = "c"
	tt := openTester(a)
	if _, ok := tt.Find("No model"); !ok {
		t.Fatalf("chip %q", tt.Texts())
	}
	if _, ok := tt.Find("No provider yet"); !ok {
		t.Fatalf("subtitle %q", tt.Texts())
	}
	a.send("keep me")
	if len(a.conversation("c").Messages) != 0 || a.conversation("c").Draft == "" {
		t.Fatalf("sent anyway messages %d draft %q", len(a.conversation("c").Messages), a.conversation("c").Draft)
	}
	tt.Frame()
	if !tt.HasText("Add a provider to start chatting.") {
		t.Fatalf("hint %q", tt.Texts())
	}
}

func TestAdoptDropsUntouchedPresets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.json")
	raw := []byte(`{
		"version": 6,
		"connections": [
			{"id": "openai", "name": "OpenAI", "provider": "OpenAI"},
			{"id": "ollama", "name": "Ollama", "provider": "Ollama"},
			{"id": "xai", "name": "xAI", "provider": "xAI"},
			{"id": "deepseek", "name": "DeepSeek", "provider": "DeepSeek", "baseUrl": "https://proxy.example/v1"}
		],
		"defaults": {"provider": "OpenAI", "model": "gpt-4o-mini", "baseUrl": "https://api.openai.com/v1", "systemPrompt": "stay"}
	}`)
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	idx := []byte(`{"schema":5,"chats":[{"id":"c","title":"Local","provider":"Ollama","model":"llama3.2","messageCount":1}]}`)
	if err := os.WriteFile(filepath.Join(dir, "index.json"), idx, 0o600); err != nil {
		t.Fatal(err)
	}
	keys := newMemKeys()
	if err := keys.Set("openai", "sk-openai"); err != nil {
		t.Fatal(err)
	}
	a := openApp(path, keys)
	ids := map[string]bool{}
	for _, c := range a.data.Connections {
		ids[c.ID] = true
	}
	if !ids["openai"] || !ids["ollama"] || !ids["deepseek"] || ids["xai"] {
		t.Fatalf("kept %+v", a.data.Connections)
	}
	if a.data.pendingOptIn {
		t.Fatal("prune still pending")
	}
	b := openApp(path, keys)
	for _, c := range b.data.Connections {
		if c.ID == "xai" {
			t.Fatal("untouched preset returned")
		}
	}
}

func TestCustomFirstFetchEnablesAll(t *testing.T) {
	a := newTestApp(t)
	a.data.Connections = []Connection{{
		ID: "together", Name: "Together", Provider: "Custom", Custom: true,
		BaseURL: "https://api.together.xyz/v1",
	}}
	if err := a.keys.Set("together", "tg-key"); err != nil {
		t.Fatal(err)
	}
	connectBuiltin(a, "OpenAI", "sk-openai")
	var fetched []string
	providerFetch = func(base, key, format string) ([]string, int, bool, error) {
		if strings.Contains(base, "together") {
			return append([]string(nil), fetched...), 200, false, nil
		}
		return []string{"gpt-4o", "gpt-4o-mini"}, 200, false, nil
	}
	t.Cleanup(func() { providerFetch = nil })

	a.viewCustom("together")
	fetched = []string{"alpha", "beta", "alpha"}
	a.runProviderTest(time.Now())
	c := a.connByID("together")
	if c == nil || !c.ModelsSet || c.Probe != probeOK {
		t.Fatalf("after first test %+v", c)
	}
	if !sameModels(enabledIDs(c.Models), []string{"alpha", "beta"}) {
		t.Fatalf("first list %+v", c.Models)
	}
	for _, m := range c.Models {
		if !m.Enabled || m.Fresh {
			t.Fatalf("first model %+v", m)
		}
	}
	if !sameModels(enabledIDs(a.provEdit.models), []string{"alpha", "beta"}) {
		t.Fatalf("editor %+v", a.provEdit.models)
	}

	fetched = []string{"alpha", "beta", "gamma"}
	a.refreshProviderModels(time.Now())
	c = a.connByID("together")
	got := map[string]ProviderModel{}
	for _, m := range c.Models {
		got[m.ID] = m
	}
	if !got["alpha"].Enabled || got["alpha"].Fresh || !got["beta"].Enabled || got["beta"].Fresh {
		t.Fatalf("kept models %+v", c.Models)
	}
	if got["gamma"].Enabled || !got["gamma"].Fresh {
		t.Fatalf("new model %+v", got["gamma"])
	}

	a.viewProvider("OpenAI")
	a.runProviderTest(time.Now())
	if openai := a.providerConnection("OpenAI"); openai == nil || openai.ModelsSet || openai.Probe != probeOK {
		t.Fatalf("builtin catalog replaced %+v", openai)
	}

	b := newTestApp(t)
	b.data.Connections = []Connection{{
		ID: "relay", Name: "Relay", Provider: "Custom", Custom: true,
		BaseURL: "https://relay.example/v1",
	}}
	if err := b.keys.Set("relay", "relay-key"); err != nil {
		t.Fatal(err)
	}
	b.viewCustom("relay")
	providerFetch = func(base, key, format string) ([]string, int, bool, error) {
		return []string{"m1", "m2"}, 200, false, nil
	}
	b.refreshProviderModels(time.Now())
	relay := b.connByID("relay")
	if relay == nil || !relay.ModelsSet || !sameModels(enabledIDs(relay.Models), []string{"m1", "m2"}) {
		t.Fatalf("refresh first list %+v", relay)
	}
	for _, m := range relay.Models {
		if !m.Enabled || m.Fresh {
			t.Fatalf("first refresh marked new %+v", m)
		}
	}
}

func TestDeleteProviderDialogCountsChats(t *testing.T) {
	a := newTestApp(t)
	connectBuiltin(a, "OpenAI", "sk-openai")
	a.data.Connections = append(a.data.Connections, Connection{
		ID: "openai-2", Name: "OpenAI 2", Provider: "OpenAI",
		Models: []ProviderModel{{ID: "gpt-4o", Enabled: true}}, ModelsSet: true,
	})
	if err := a.keys.Set("openai-2", "sk-personal"); err != nil {
		t.Fatal(err)
	}
	a.data.Defaults.Provider = "OpenAI"
	a.data.Defaults.Model = "gpt-4o-mini"
	a.data.Defaults.ConnectionID = "conn-openai"
	a.data.Defaults.BaseURL = officialBase("OpenAI")
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	msg := func(id string) []Message {
		return []Message{{ID: id, Role: roleUser, Content: id, Done: true, CreatedAt: now}}
	}
	chat := func(id, conn, model string) Conversation {
		return Conversation{
			ID: id, Title: id, CreatedAt: now, UpdatedAt: now, Loaded: true,
			Config: Config{
				Provider: "OpenAI 2", Model: model, ConnectionID: conn,
				BaseURL: officialBase("OpenAI"), SystemPrompt: a.data.Defaults.SystemPrompt,
			},
			Messages: msg(id),
		}
	}
	a.data.Conversations = []Conversation{chat("c1", "openai-2", "gpt-4o"), chat("c2", "openai-2", "gpt-4o")}
	a.data.ActiveID = "c1"
	a.settingsOpen = true
	a.settingsSection = "models"
	a.viewConnection("openai-2")
	a.settingsPage = settingsDetail
	tt := ui.NewTester(a.view, 720, 520)
	if err := tt.Click("Delete Provider…"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Delete “OpenAI 2”?") || !tt.HasText("2 chats use models from this provider. They’ll switch to your default model.") {
		t.Fatalf("two chats %q", tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}

	a.data.Conversations = []Conversation{chat("c1", "openai-2", "gpt-4o")}
	tt = ui.NewTester(a.view, 720, 520)
	if err := tt.Click("Delete Provider…"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("1 chat uses models from this provider. It’ll switch to your default model.") || tt.HasText("2 chats use") {
		t.Fatalf("one chat %q", tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}

	a.data.Conversations = nil
	tt = ui.NewTester(a.view, 720, 520)
	if err := tt.Click("Delete Provider…"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("chats use") || tt.HasText("chat uses") || !tt.HasText("Its API key is removed from the Keychain.") {
		t.Fatalf("no chats %q", tt.Texts())
	}
}

func TestOptInPresetAndCustomShareURL(t *testing.T) {
	var auth string
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		path = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	a := newTestApp(t)
	if len(a.data.Connections) != 0 {
		t.Fatalf("fresh list %+v", a.data.Connections)
	}
	a.beginPreset("OpenAI")
	if a.provEdit.format != formatOpenAI || a.provEdit.baseURL != "" {
		t.Fatalf("preset draft %+v", a.provEdit)
	}
	a.provEdit.apiKey = "sk-preset"
	a.commitDraft(time.Now())

	off := officialBase("OpenAI")
	a.addForm = addDraft{name: "Relay", base: off, key: "sk-relay", format: formatOpenAI}
	a.confirmAddProvider(time.Now())
	if len(a.data.Connections) != 2 {
		t.Fatalf("connections %+v", a.data.Connections)
	}
	preset := a.connByID("openai")
	customID := ""
	for i := range a.data.Connections {
		if a.data.Connections[i].Custom {
			customID = a.data.Connections[i].ID
		}
	}
	custom := a.connByID(customID)
	if preset == nil || custom == nil || custom.ID == preset.ID || custom.ID == "openai" {
		t.Fatalf("instances preset %+v custom %+v", preset, custom)
	}
	if preset.Format != formatOpenAI || preset.BaseURL != "" || connEffective(preset) != off {
		t.Fatalf("preset endpoint %+v", preset)
	}
	if custom.Name != "Relay" || custom.Provider != "Custom" || custom.BaseURL != off || custom.Format != formatOpenAI {
		t.Fatalf("custom %+v", custom)
	}
	if connEffective(preset) != connEffective(custom) {
		t.Fatalf("bases %q %q", connEffective(preset), connEffective(custom))
	}
	if a.resolvedKey(Config{ConnectionID: preset.ID}) != "sk-preset" || a.resolvedKey(Config{ConnectionID: custom.ID}) != "sk-relay" {
		t.Fatal("keys are not independent")
	}

	a.provEdit.newModel = "gpt-4o-mini"
	a.addEditorModel(time.Now())
	var sawPreset, sawCustom bool
	for _, opt := range a.menuModels() {
		if opt.model != "gpt-4o-mini" {
			continue
		}
		if opt.connID == "openai" {
			sawPreset = true
		}
		if opt.connID == customID {
			sawCustom = true
		}
	}
	if !sawPreset || !sawCustom {
		t.Fatalf("menu collapsed the shared URL: %+v", a.menuModels())
	}

	shared := srv.URL + "/v1"
	for i := range a.data.Connections {
		a.data.Connections[i].BaseURL = shared
	}
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	prompt := DefaultConfig().SystemPrompt
	a.data.Defaults = Config{
		Provider: "OpenAI", Model: "gpt-4o-mini", ConnectionID: "openai",
		BaseURL: shared, SystemPrompt: prompt,
	}
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Relay", CreatedAt: now, UpdatedAt: now,
		Config:   a.data.Defaults,
		Messages: []Message{{ID: "m", Role: roleUser, Content: "hi", Done: true, CreatedAt: now}},
	}}
	a.data.ActiveID = "c1"

	tt := openTester(a)
	if _, ok := tt.Find("Model OpenAI · gpt-4o-mini"); !ok {
		t.Fatalf("chip %q", tt.Texts())
	}
	if err := tt.Click("Model OpenAI · gpt-4o-mini"); err != nil {
		t.Fatal(err)
	}
	rows := menuRows(t, tt)
	if on, ok := modelChecked(rows, "OpenAI", "gpt-4o-mini"); !ok || !on {
		t.Fatalf("openai check %+v", rows)
	}
	if on, ok := modelChecked(rows, "Relay", "gpt-4o-mini"); !ok || on {
		t.Fatalf("relay check %+v", rows)
	}
	chooseGroupedModel(t, tt, "Relay", "gpt-4o-mini")
	cfg := a.active().Config
	if cfg.ConnectionID != customID || cfg.Model != "gpt-4o-mini" || cfg.Provider != "Relay" {
		t.Fatalf("custom choice %+v", cfg)
	}
	focusDraft(t, tt)
	tt.Type("relay")
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer sk-relay" || strings.Contains(auth, "sk-preset") || !strings.Contains(path, "/chat/completions") {
		t.Fatalf("relay auth %q path %q", auth, path)
	}

	if err := tt.Click("Model Relay · gpt-4o-mini"); err != nil {
		t.Fatal(err)
	}
	rows = menuRows(t, tt)
	if on, ok := modelChecked(rows, "OpenAI", "gpt-4o-mini"); !ok || on {
		t.Fatalf("openai check after relay %+v", rows)
	}
	if on, ok := modelChecked(rows, "Relay", "gpt-4o-mini"); !ok || !on {
		t.Fatalf("relay check after switch %+v", rows)
	}
	chooseGroupedModel(t, tt, "OpenAI", "gpt-4o-mini")
	if a.active().Config.ConnectionID != "openai" {
		t.Fatalf("preset choice %+v", a.active().Config)
	}
	focusDraft(t, tt)
	tt.Type("preset")
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer sk-preset" || strings.Contains(auth, "sk-relay") {
		t.Fatalf("preset auth %q", auth)
	}
}

func TestDeleteDefaultFallsBack(t *testing.T) {
	a := newTestApp(t)
	connectBuiltin(a, "OpenAI", "sk-openai")
	a.data.Connections = append(a.data.Connections, Connection{
		ID: "openai-2", Name: "OpenAI 2", Provider: "OpenAI", Format: formatOpenAI,
		Models: []ProviderModel{{ID: "gpt-4o", Enabled: true}}, ModelsSet: true,
	})
	if err := a.keys.Set("openai-2", "sk-personal"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	a.data.Defaults = Config{
		Provider: "OpenAI 2", Model: "gpt-4o", ConnectionID: "openai-2",
		BaseURL: officialBase("OpenAI"), SystemPrompt: DefaultConfig().SystemPrompt,
	}
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Work", CreatedAt: now, UpdatedAt: now, Loaded: true,
		Config:   a.data.Defaults,
		Messages: []Message{{ID: "m", Role: roleUser, Content: "hi", Done: true, CreatedAt: now}},
	}}
	a.data.ActiveID = "c1"
	a.deleteProvider("openai-2", now)
	if a.data.Defaults.ConnectionID != "conn-openai" || a.data.Defaults.Model != "gpt-4o-mini" {
		t.Fatalf("default %+v", a.data.Defaults)
	}
	conv := a.conversation("c1")
	if conv.Config.ConnectionID != "conn-openai" || conv.Config.Model != "gpt-4o-mini" {
		t.Fatalf("chat %+v", conv.Config)
	}
	if conv.SwitchNote != deletedProviderNote("OpenAI 2", "gpt-4o-mini") {
		t.Fatalf("note %q", conv.SwitchNote)
	}
	if a.providerNotice != "OpenAI 2 was deleted. New chats now use gpt-4o-mini (OpenAI)." || a.resolvedKey(Config{ConnectionID: "openai-2"}) != "" {
		t.Fatalf("notice %q key still %q", a.providerNotice, a.resolvedKey(Config{ConnectionID: "openai-2"}))
	}
}

func TestDeleteDialogCountThenNotice(t *testing.T) {
	a := newTestApp(t)
	connectBuiltin(a, "OpenAI", "sk-openai")
	a.data.Connections = append(a.data.Connections, Connection{
		ID: "openai-2", Name: "OpenAI 2", Provider: "OpenAI",
		Models: []ProviderModel{{ID: "gpt-4o", Enabled: true}}, ModelsSet: true,
	})
	if err := a.keys.Set("openai-2", "sk-personal"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	prompt := DefaultConfig().SystemPrompt
	a.data.Defaults = Config{
		Provider: "OpenAI", Model: "gpt-4o-mini", ConnectionID: "conn-openai",
		BaseURL: officialBase("OpenAI"), SystemPrompt: prompt,
	}
	msg := func(id string) []Message {
		return []Message{{ID: id, Role: roleUser, Content: id, Done: true, CreatedAt: now}}
	}
	chat := func(id, provider, conn, model string) Conversation {
		return Conversation{
			ID: id, Title: id, CreatedAt: now, UpdatedAt: now, Loaded: true,
			Config: Config{
				Provider: provider, Model: model, ConnectionID: conn,
				BaseURL: officialBase("OpenAI"), SystemPrompt: prompt,
			},
			Messages: msg(id),
		}
	}
	a.data.Conversations = []Conversation{
		chat("c1", "OpenAI 2", "openai-2", "gpt-4o"),
		chat("c2", "OpenAI 2", "openai-2", "gpt-4o"),
		chat("keep", "OpenAI", "conn-openai", "gpt-4o-mini"),
	}
	a.data.ActiveID = "c1"
	a.settingsOpen = true
	a.settingsSection = "models"
	a.viewConnection("openai-2")
	a.settingsPage = settingsDetail
	tt := ui.NewTester(a.view, 720, 520)
	if err := tt.Click("Delete Provider…"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("2 chats use models from this provider. They’ll switch to your default model.") {
		t.Fatalf("count %q", tt.Texts())
	}
	if err := tt.Click("Delete"); err != nil {
		t.Fatal(err)
	}
	want := deletedProviderNote("OpenAI 2", "gpt-4o-mini")
	for _, id := range []string{"c1", "c2"} {
		conv := a.conversation(id)
		if conv.Config.ConnectionID != "conn-openai" || conv.Config.Model != "gpt-4o-mini" || conv.SwitchNote != want {
			t.Fatalf("%s %+v note %q", id, conv.Config, conv.SwitchNote)
		}
	}
	keep := a.conversation("keep")
	if keep.Config.Model != "gpt-4o-mini" || keep.Config.ConnectionID != "conn-openai" || keep.SwitchNote != "" {
		t.Fatalf("untouched %+v note %q", keep.Config, keep.SwitchNote)
	}
	a.settingsOpen = false
	main := openTester(a)
	if !main.HasText(want) {
		t.Fatalf("composer %q", main.Texts())
	}
	if err := main.Click("Dismiss"); err != nil {
		t.Fatal(err)
	}
	main.Frame()
	if a.conversation("c1").SwitchNote != "" || a.conversation("c2").SwitchNote != want {
		t.Fatalf("dismiss c1 %q c2 %q", a.conversation("c1").SwitchNote, a.conversation("c2").SwitchNote)
	}
	a.data.ActiveID = "c2"
	main.Frame()
	if !main.HasText(want) {
		t.Fatalf("second chat note %q", main.Texts())
	}

	b := newTestApp(t)
	connectBuiltin(b, "OpenAI", "sk-openai")
	b.data.Connections = append(b.data.Connections, Connection{
		ID: "openai-2", Name: "OpenAI 2", Provider: "OpenAI",
		Models: []ProviderModel{{ID: "gpt-4o", Enabled: true}}, ModelsSet: true,
	})
	if err := b.keys.Set("openai-2", "sk-personal"); err != nil {
		t.Fatal(err)
	}
	b.data.Defaults = Config{
		Provider: "OpenAI 2", Model: "gpt-4o", ConnectionID: "openai-2",
		BaseURL: officialBase("OpenAI"), SystemPrompt: prompt,
	}
	b.data.Conversations = []Conversation{
		chat("c1", "OpenAI 2", "openai-2", "gpt-4o"),
		chat("keep", "OpenAI", "conn-openai", "gpt-4o-mini"),
	}
	b.data.ActiveID = "c1"
	b.settingsOpen = true
	b.settingsSection = "models"
	b.viewConnection("openai-2")
	b.settingsPage = settingsDetail
	tb := ui.NewTester(b.view, 720, 520)
	if err := tb.Click("Delete Provider…"); err != nil {
		t.Fatal(err)
	}
	if !tb.HasText("1 chat uses models from this provider. It’ll switch to your default model.") || tb.HasText("2 chats use") {
		t.Fatalf("fallback count %q", tb.Texts())
	}
	if err := tb.Click("Delete"); err != nil {
		t.Fatal(err)
	}
	if b.data.Defaults.ConnectionID != "conn-openai" || b.data.Defaults.Model != "gpt-4o-mini" {
		t.Fatalf("fallback default %+v", b.data.Defaults)
	}
	moved := b.conversation("c1")
	if moved.Config.ConnectionID != "conn-openai" || moved.Config.Model != "gpt-4o-mini" || moved.SwitchNote != want {
		t.Fatalf("fallback chat %+v note %q", moved.Config, moved.SwitchNote)
	}
	held := b.conversation("keep")
	if held.Config.Model != "gpt-4o-mini" || held.SwitchNote != "" {
		t.Fatalf("held %+v note %q", held.Config, held.SwitchNote)
	}
	b.settingsOpen = false
	shown := openTester(b)
	if !shown.HasText(want) {
		t.Fatalf("fallback composer %q", shown.Texts())
	}
}

func TestProviderOptInMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.json")
	raw := []byte(`{
		"version": 6,
		"connections": [
			{"id": "openai", "name": "OpenAI", "provider": "OpenAI"},
			{"id": "anthropic", "name": "Anthropic", "provider": "Anthropic", "probe": "ok"},
			{"id": "deepseek", "name": "DeepSeek", "provider": "DeepSeek", "baseUrl": "https://proxy.example/v1"},
			{"id": "xai", "name": "xAI", "provider": "xAI"},
			{"id": "ollama", "name": "Ollama", "provider": "Ollama"}
		],
		"defaults": {"provider": "DeepSeek", "model": "deepseek-flash", "baseUrl": "https://proxy.example/v1", "systemPrompt": "stay", "maxTokens": 256}
	}`)
	if err := os.WriteFile(filepath.Join(dir, settingsFile), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	idx := []byte(`{"schema":5,"chats":[{"id":"local","title":"Local","model":"llama3.2","messageCount":1}]}`)
	if err := os.WriteFile(filepath.Join(dir, indexFile), idx, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, chatsDir), 0o700); err != nil {
		t.Fatal(err)
	}
	writeLegacyChat(t, filepath.Join(dir, chatsDir, "local.jsonl"), "local", "Local", `{"model":"llama3.2"}`)
	keys := newMemKeys()
	if err := keys.Set("openai", "sk-openai"); err != nil {
		t.Fatal(err)
	}
	a := openApp(path, keys)
	ids := map[string]*Connection{}
	for i := range a.data.Connections {
		c := &a.data.Connections[i]
		ids[c.ID] = c
	}
	if ids["xai"] != nil || ids["openai"] == nil || ids["anthropic"] == nil || ids["deepseek"] == nil || ids["ollama"] == nil {
		t.Fatalf("kept %+v", a.data.Connections)
	}
	if ids["openai"].Format != formatOpenAI || a.resolvedKey(Config{ConnectionID: "openai"}) != "sk-openai" {
		t.Fatalf("configured openai %+v", ids["openai"])
	}
	if ids["anthropic"].Format != formatAnthropic || ids["anthropic"].Probe != probeOK {
		t.Fatalf("tested anthropic %+v", ids["anthropic"])
	}
	if ids["deepseek"].BaseURL != "https://proxy.example/v1" || ids["deepseek"].Format != formatOpenAI {
		t.Fatalf("modified deepseek %+v", ids["deepseek"])
	}
	if ids["ollama"].Format != formatOpenAI || a.resolvedKey(Config{ConnectionID: "ollama"}) != "" {
		t.Fatalf("keyless ollama %+v", ids["ollama"])
	}
	if a.data.pendingOptIn {
		t.Fatal("prune still pending")
	}
	if a.data.Defaults.ConnectionID != "deepseek" || a.data.Defaults.Model != "deepseek-flash" || a.data.Defaults.MaxTokens != 256 || a.data.Defaults.SystemPrompt != "stay" {
		t.Fatalf("defaults %+v", a.data.Defaults)
	}
	a.ensureChat("local")
	local := a.conversation("local")
	if local == nil || local.Config.ConnectionID != "ollama" || local.Config.Model != "llama3.2" || local.Config.Provider != "Ollama" {
		t.Fatalf("keyless chat %+v", local)
	}
	a.saveSync()
	assertHeaderIdentity(t, filepath.Join(dir, chatsDir, "local.jsonl"), "ollama", "llama3.2")

	b := openApp(path, keys)
	for _, c := range b.data.Connections {
		if c.ID == "xai" {
			t.Fatal("untouched preset returned")
		}
	}
	if b.data.Defaults.ConnectionID != "deepseek" || b.data.Defaults.Model != "deepseek-flash" {
		t.Fatalf("reloaded defaults %+v", b.data.Defaults)
	}
	if b.resolvedKey(Config{ConnectionID: "openai"}) != "sk-openai" {
		t.Fatal("keychain id did not survive")
	}
	b.ensureChat("local")
	again := b.conversation("local")
	if again == nil || again.Config.ConnectionID != "ollama" || again.Config.Model != "llama3.2" {
		t.Fatalf("reloaded chat %+v", again)
	}
}

func TestModelListUsesFormat(t *testing.T) {
	for _, p := range presets {
		if strings.EqualFold(p.Name, "Gemini") {
			t.Fatal("gemini is not an implemented format")
		}
	}
	if normalizeFormat("") != formatOpenAI || normalizeFormat("anthropic") != formatAnthropic || normalizeFormat("gemini") != formatOpenAI {
		t.Fatalf("normalize %q %q %q", normalizeFormat(""), normalizeFormat("anthropic"), normalizeFormat("gemini"))
	}
	if presetFormat("Anthropic") != formatAnthropic || presetFormat("Ollama") != formatOpenAI {
		t.Fatalf("preset formats %s %s", presetFormat("Anthropic"), presetFormat("Ollama"))
	}

	var method, reqPath, auth, apiKey, version string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		reqPath = r.URL.Path
		auth = r.Header.Get("Authorization")
		apiKey = r.Header.Get("x-api-key")
		version = r.Header.Get("anthropic-version")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":[{"id":"listed"}]}`)
	}))
	defer srv.Close()
	base := srv.URL + "/v1"

	ids, status, unsupported, err := fetchModelIDs(base, "sk-ant", formatAnthropic)
	if err != nil || unsupported || status != 200 || len(ids) != 1 || ids[0] != "listed" {
		t.Fatalf("anthropic list ids %v status %d unsupported %v err %v", ids, status, unsupported, err)
	}
	if method != http.MethodGet || reqPath != "/v1/models" || apiKey != "sk-ant" || version != anthropicVersion || auth != "" {
		t.Fatalf("anthropic request %s %s auth %q key %q version %q", method, reqPath, auth, apiKey, version)
	}

	_, _, _, err = fetchModelIDs(base, "sk-oai", formatOpenAI)
	if err != nil || auth != "Bearer sk-oai" || apiKey != "" || reqPath != "/v1/models" {
		t.Fatalf("openai request auth %q key %q path %q err %v", auth, apiKey, reqPath, err)
	}
	_, _, _, err = fetchModelIDs(base, "sk-empty", "")
	if err != nil || auth != "Bearer sk-empty" || apiKey != "" {
		t.Fatalf("empty format auth %q key %q err %v", auth, apiKey, err)
	}

	req, err := http.NewRequest(http.MethodGet, "https://cove.openai.azure.com/openai/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	setModelListAuth(req, "az-key", formatOpenAI)
	if req.Header.Get("api-key") != "az-key" || req.Header.Get("Authorization") != "" || req.Header.Get("x-api-key") != "" {
		t.Fatalf("azure headers %v", req.Header)
	}
}

func TestManualModelWhenFetchFails(t *testing.T) {
	a := newTestApp(t)
	a.data.Connections = []Connection{{
		ID: "local", Name: "Local", Provider: "Custom", Custom: true,
		BaseURL: "http://127.0.0.1:9/v1", Format: formatOpenAI,
	}}
	if err := a.keys.Set("local", "local-key"); err != nil {
		t.Fatal(err)
	}
	providerFetch = func(base, key, format string) ([]string, int, bool, error) {
		if format != formatOpenAI || key != "local-key" {
			t.Errorf("fetch format %q key %q", format, key)
		}
		return nil, http.StatusNotFound, true, nil
	}
	t.Cleanup(func() { providerFetch = nil })
	a.viewCustom("local")
	a.refreshProviderModels(time.Now())
	if len(a.provEdit.models) != 0 || a.connByID("local").ModelsSet {
		t.Fatalf("failed fetch wrote a list editor %+v conn %+v", a.provEdit.models, a.connByID("local"))
	}
	if !strings.Contains(a.provEdit.refreshNote, "add models by ID") {
		t.Fatalf("note %q", a.provEdit.refreshNote)
	}
	a.provEdit.newModel = "llama3.2"
	a.addEditorModel(time.Now())
	c := a.connByID("local")
	if c == nil || !c.ModelsSet || len(c.Models) != 1 || c.Models[0].ID != "llama3.2" || !c.Models[0].Enabled {
		t.Fatalf("manual model %+v", c)
	}
}

func TestCustomDraftFetchDoesNotSave(t *testing.T) {
	a := newTestApp(t)
	a.beginCustom()
	if a.settingsPage != settingsPending || !a.provEdit.draft || !a.provEdit.custom {
		t.Fatalf("custom draft %+v page %q", a.provEdit, a.settingsPage)
	}
	a.provEdit.name = "Relay"
	a.provEdit.baseURL = "https://relay.example/v1"
	providerFetch = func(base, key, format string) ([]string, int, bool, error) {
		return []string{"m1", "m2"}, 200, false, nil
	}
	t.Cleanup(func() { providerFetch = nil })
	a.runProviderTest(time.Now())
	if len(a.data.Connections) != 0 {
		t.Fatalf("test wrote a provider %+v", a.data.Connections)
	}
	if !a.provEdit.listed || !sameModels(enabledIDs(a.provEdit.models), []string{"m1", "m2"}) {
		t.Fatalf("first list %+v listed %v", a.provEdit.models, a.provEdit.listed)
	}
	for _, m := range a.provEdit.models {
		if !m.Enabled || m.Fresh {
			t.Fatalf("first model %+v", m)
		}
	}
	a.commitDraft(time.Now())
	if len(a.data.Connections) != 1 || !a.data.Connections[0].ModelsSet || a.data.Connections[0].Name != "Relay" {
		t.Fatalf("after add %+v", a.data.Connections)
	}
	a.beginCustom()
	a.provEdit.name = "relay"
	a.provEdit.baseURL = "https://other.example/v1"
	a.saveProviderName(time.Now())
	if a.provEdit.nameError != "Another provider already uses this name." {
		t.Fatalf("name error %q", a.provEdit.nameError)
	}
	a.commitDraft(time.Now())
	if len(a.data.Connections) != 1 {
		t.Fatalf("duplicate saved %+v", a.data.Connections)
	}
	a.viewCustom(a.data.Connections[0].ID)
	providerFetch = func(base, key, format string) ([]string, int, bool, error) {
		return []string{"m1", "m2", "m3"}, 200, false, nil
	}
	a.refreshProviderModels(time.Now())
	c := a.connByID(a.data.Connections[0].ID)
	got := map[string]ProviderModel{}
	for _, m := range c.Models {
		got[m.ID] = m
	}
	if !got["m1"].Enabled || got["m1"].Fresh || got["m3"].Enabled || !got["m3"].Fresh {
		t.Fatalf("refresh %+v", c.Models)
	}
}

func TestDeleteLastProviderShowsNoModel(t *testing.T) {
	a := newTestApp(t)
	connectBuiltin(a, "OpenAI", "sk-openai")
	now := time.Now()
	cfg := Config{Provider: "OpenAI", Model: "gpt-4o", ConnectionID: "conn-openai", BaseURL: officialBase("OpenAI")}
	a.data.Defaults = cfg
	a.data.Conversations = []Conversation{{
		ID: "c", Title: "Chat", CreatedAt: now, UpdatedAt: now, Loaded: true,
		Config: cfg, Draft: "keep me",
		Messages: []Message{{ID: "m", Role: roleUser, Content: "hi", Done: true, CreatedAt: now}},
	}}
	a.data.ActiveID = "c"
	a.deleteProvider("conn-openai", now)
	conv := a.conversation("c")
	if conv.Config.ConnectionID != "conn-openai" || conv.Config.Model != "gpt-4o" || conv.SwitchNote != "" {
		t.Fatalf("reference %+v note %q", conv.Config, conv.SwitchNote)
	}
	if a.composerModelLabel(conv.Config) != "No model" {
		t.Fatalf("label %q", a.composerModelLabel(conv.Config))
	}
	tt := openTester(a)
	if _, ok := tt.Find("No model"); !ok {
		t.Fatalf("button %q", tt.Texts())
	}
	a.send("keep me")
	if len(a.conversation("c").Messages) != 1 || a.conversation("c").Draft != "keep me" {
		t.Fatalf("sent messages %d draft %q", len(a.conversation("c").Messages), a.conversation("c").Draft)
	}
	tt.Frame()
	if !tt.HasText("Add a provider to start chatting.") {
		t.Fatalf("hint %q", tt.Texts())
	}
}

func TestProvidersOptInReport(t *testing.T) {
	section := `
Providers opt-in
The stored provider list is only the instances the user added. A fresh install has none. Each instance has a stable id, a display name, an API format, a base URL, a keychain id, and a model list. Built-in presets are templates on Add Provider. Adding one copies the name, the official base URL, and the format into a new instance. The official base URL is not written; an empty base URL still means that endpoint. The same preset can be added again. The second is named OpenAI 2 and its id is openai-2. The name can be changed later. The id stays.

A custom provider takes a name, a required base URL, an optional API key, and an API format. The formats the app implements are OpenAI-compatible and Anthropic-compatible. Gemini is not implemented. Chat requests stay on the OpenAI-compatible completions client. The model list is GET {base}/models. Anthropic sends x-api-key and anthropic-version 2023-06-01. OpenAI-compatible sends a bearer token, or api-key on Azure. If that list fails, the user can type a model id. Add Provider is a pushed page. Custom… pushes the same detail page as a draft; Test Connection and the first Refresh enable every fetched model and do not write a provider until Add. A later Refresh adds new ids switched off, with a New badge. Names are unique without regard to case.

Any added provider can be deleted. The dialog counts chats that use it, for example "2 chats use models from this provider. They’ll switch to your default model." Those chats then use the default model and show a one-time composer note, Switched to <default> — “<name>” was removed., until it is dismissed or the next send. If the default model itself belonged to the deleted provider, the existing fallback picks the next ready model first, and the chats and the note use that model. The keychain item for that id is removed. Deleting the last provider leaves the chat's connection id and model id in place, the button reads No model, and Send shows Add a provider to start chatting. above the composer. No dialog.

With no provider, the composer model menu is Add a provider…, which opens Settings, Models & Providers. Send does not start a request. It leaves the draft and shows Add a provider to start chatting.

Upgrade from a store older than version 7 keeps a preset that has a key, a non-official base URL, a saved model list, a test result, or a reference from a chat header, the global default, or a model id only one connection offers. That last case keeps a keyless local preset such as Ollama when a chat names llama3.2 and nothing else does. Untouched presets are dropped. Kept ids are not rewritten, so keychain entries still resolve. Chat and global model strings stay as stored and gain the connection id of that same provider.

Tests: TestProviderListStartsEmpty, TestOptInPresetAndCustomShareURL, TestAddPresetTwiceKeepsStableIDs, TestDeleteProviderRetargetsChats, TestDeleteDefaultFallsBack, TestDeleteLastProviderShowsNoModel, TestCustomDraftFetchDoesNotSave, TestProviderOptInMigration, TestModelListUsesFormat, TestManualModelWhenFetchFails, TestSendWithoutProviderKeepsDraft.
`
	path := perfReportPath(t)
	raw, err := os.ReadFile(path)
	body := ""
	if err == nil {
		body = string(raw)
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	const heading = "\nProviders opt-in\n"
	if i := strings.Index(body, heading); i >= 0 {
		rest := body[i+len(heading):]
		cut := len(rest)
		for _, next := range []string{"\nSidebar resize\n", "\nShortcuts\n", "\nModel switch\n"} {
			if j := strings.Index(rest, next); j >= 0 && j < cut {
				cut = j
			}
		}
		body = body[:i] + section + rest[cut:]
	} else {
		body += section
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
