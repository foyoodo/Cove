package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/egoist/mygo/ui"
)

func TestOpenProviderWritesNothing(t *testing.T) {
	keys := newMemKeys()
	a := openApp(t.TempDir()+"/conversations.json", keys)
	a.instant = true
	temp := 0.4
	a.data.Defaults.Provider = "OpenAI"
	a.data.Defaults.Model = "kept-model"
	a.data.Defaults.BaseURL = "https://kept.example/v1"
	a.data.Defaults.SystemPrompt = "stay"
	a.data.Defaults.Reasoning = "high"
	a.data.Defaults.MaxTokens = 321
	a.data.Defaults.Temperature = &temp
	a.data.Defaults.ConnectionID = "conn-default"
	a.data.Connections = []Connection{
		{ID: "conn-default", Name: "OpenAI", Provider: "OpenAI", BaseURL: "https://kept.example/v1"},
		{ID: "conn-deepseek", Name: "DeepSeek", Provider: "DeepSeek", BaseURL: "https://api.deepseek.com/v1"},
	}
	if err := keys.Set("conn-default", "sk-default"); err != nil {
		t.Fatal(err)
	}
	if err := keys.Set("conn-deepseek", "sk-deep"); err != nil {
		t.Fatal(err)
	}
	a.data.Conversations = []Conversation{{
		ID: "kept-chat", Title: "Kept",
		Config: Config{
			Provider: "OpenAI", Model: "kept-model", BaseURL: "https://kept.example/v1",
			SystemPrompt: "chat prompt", Reasoning: "high", MaxTokens: 321, Temperature: &temp,
			ConnectionID: "conn-default",
		},
		Messages: []Message{{ID: "m", Role: roleUser, Content: "leave this", Done: true}},
		Draft:    "draft stays",
	}}
	if err := Save(a.path, &a.data); err != nil {
		t.Fatal(err)
	}
	fileBefore := storeBytes(t, filepath.Dir(a.path))
	keysBefore := keys.snapshot()

	tt := openTester(a)
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Models & Providers"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	chatsBefore := append([]Conversation(nil), a.data.Conversations...)
	defaultsBefore := a.data.Defaults
	connsBefore := append([]Connection(nil), a.data.Connections...)
	for _, conn := range []Connection{a.data.Connections[0], a.data.Connections[1]} {
		name := conn.Name
		if err := tt.Click(name); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if a.settingsPage != settingsDetail || a.provEdit.name != name {
			t.Fatalf("row %s opened %s page %q", name, a.provEdit.name, a.settingsPage)
		}
		if err := tt.Click("Back"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if a.settingsPage != settingsList {
			t.Fatalf("back from %s left page %q", name, a.settingsPage)
		}
	}
	if err := tt.Click("Add Provider…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.settingsPage != settingsAdd {
		t.Fatalf("add page %q", a.settingsPage)
	}
	if err := tt.Click("Back"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !reflect.DeepEqual(connsBefore, a.data.Connections) {
		t.Fatalf("connections changed %+v", a.data.Connections)
	}
	a.settingsOpen = false
	tt.Frame()
	if a.data.Defaults != defaultsBefore {
		t.Fatalf("defaults changed %+v", a.data.Defaults)
	}
	if !reflect.DeepEqual(chatsBefore, a.data.Conversations) {
		t.Fatal("chats changed")
	}

	fileAfter := storeBytes(t, filepath.Dir(a.path))
	if !bytes.Equal(fileBefore, fileAfter) {
		t.Fatalf("conversations file changed\nbefore %s\nafter  %s", fileBefore, fileAfter)
	}
	if !reflect.DeepEqual(keysBefore, keys.snapshot()) {
		t.Fatalf("keychain changed\nbefore %#v\nafter  %#v", keysBefore, keys.snapshot())
	}
}

func TestSwitchProviderLeavesDefaults(t *testing.T) {
	a := newTestApp(t)
	temp := 0.2
	a.data.Defaults.Provider = "Anthropic"
	a.data.Defaults.Model = "claude-kept"
	a.data.Defaults.BaseURL = "https://api.anthropic.com/v1"
	a.data.Defaults.Reasoning = "low"
	a.data.Defaults.ReasoningStyle = ""
	a.data.Defaults.MaxTokens = 64
	a.data.Defaults.Temperature = &temp
	a.data.Defaults.SystemPrompt = "stay put"
	before := a.data.Defaults
	for _, name := range builtinProviderNames() {
		seedBuiltinRow(a, name)
	}

	tt := openTester(a)
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Models & Providers"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, name := range builtinProviderNames() {
		if err := tt.Click(name); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if a.data.Defaults != before {
			t.Fatalf("after opening %s defaults %+v", name, a.data.Defaults)
		}
		if err := tt.Click("General"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if a.settingsPage != settingsList {
			t.Fatalf("leaving Models kept page %q", a.settingsPage)
		}
		if err := tt.Click("Models & Providers"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	if a.data.Defaults.Model != "claude-kept" || a.data.Defaults.Provider != "Anthropic" || a.data.Defaults.BaseURL != "https://api.anthropic.com/v1" {
		t.Fatalf("defaults %+v", a.data.Defaults)
	}
	if a.data.Defaults.Reasoning != "low" || a.data.Defaults.MaxTokens != 64 || a.data.Defaults.Temperature == nil || *a.data.Defaults.Temperature != 0.2 {
		t.Fatalf("generation defaults %+v", a.data.Defaults)
	}
}

func TestProviderBaseURLSavesOnlyThatField(t *testing.T) {
	keys := newMemKeys()
	a := openApp(t.TempDir()+"/conversations.json", keys)
	a.instant = true
	temp := 1.1
	a.data.Defaults.Provider = "OpenAI"
	a.data.Defaults.Model = "kept-model"
	a.data.Defaults.BaseURL = "https://kept.example/v1"
	a.data.Defaults.Reasoning = "medium"
	a.data.Defaults.MaxTokens = 80
	a.data.Defaults.Temperature = &temp
	if err := keys.Set("later", "sk-untouched"); err != nil {
		t.Fatal(err)
	}
	keysBefore := keys.snapshot()
	seedBuiltinRow(a, "DeepSeek")

	tt := openTester(a)
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Models & Providers"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("DeepSeek"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Base URL"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("https://proxy.example/v1")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()

	if a.data.Defaults.Provider != "OpenAI" || a.data.Defaults.Model != "kept-model" || a.data.Defaults.BaseURL != "https://kept.example/v1" {
		t.Fatalf("defaults rewritten %+v", a.data.Defaults)
	}
	if a.data.Defaults.Reasoning != "medium" || a.data.Defaults.MaxTokens != 80 || a.data.Defaults.Temperature == nil || *a.data.Defaults.Temperature != 1.1 {
		t.Fatalf("generation defaults rewritten %+v", a.data.Defaults)
	}
	if !reflect.DeepEqual(keysBefore, keys.snapshot()) {
		t.Fatalf("keychain changed %#v", keys.snapshot())
	}
	var found bool
	for _, c := range a.data.Connections {
		if c.Provider == "DeepSeek" && c.BaseURL == "https://proxy.example/v1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("provider base URL not stored: %+v", a.data.Connections)
	}
}

func TestProviderModelsEditLeavesDefault(t *testing.T) {
	a := newTestApp(t)
	temp := 0.3
	a.data.Defaults.Provider = "OpenAI"
	a.data.Defaults.Model = "kept-model"
	a.data.Defaults.BaseURL = "https://kept.example/v1"
	a.data.Defaults.SystemPrompt = "stay"
	a.data.Defaults.Reasoning = "low"
	a.data.Defaults.MaxTokens = 40
	a.data.Defaults.Temperature = &temp
	a.data.Connections = []Connection{{
		ID: "conn-openai", Name: "OpenAI", Provider: "OpenAI",
		BaseURL: "https://kept.example/v1",
		Models: []ProviderModel{
			{ID: "kept-model", Enabled: true},
			{ID: "other-model", Enabled: true},
		},
		ModelsSet: true,
	}}
	a.data.Conversations = []Conversation{{
		ID: "c", Title: "Kept",
		Config:   a.data.Defaults,
		Messages: []Message{{ID: "m", Role: roleUser, Content: "hi", Done: true}},
		Draft:    "leave the draft",
	}}
	before := a.data.Defaults
	chats := append([]Conversation(nil), a.data.Conversations...)

	tt := openTester(a)
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Models & Providers"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("OpenAI"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Show kept-model"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	if a.data.Defaults != before {
		t.Fatalf("default changed %+v", a.data.Defaults)
	}
	if !reflect.DeepEqual(chats, a.data.Conversations) {
		t.Fatal("chats changed")
	}
	conn := a.providerConnection("OpenAI")
	if conn == nil || !conn.ModelsSet || !sameModels(enabledIDs(conn.Models), []string{"other-model"}) {
		t.Fatalf("models list %+v", conn)
	}
	if err := tt.Click("General"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Unavailable") {
		t.Fatalf("missing default not marked unavailable: %q", tt.Texts())
	}
	if a.data.Defaults.Model != "kept-model" || a.data.Defaults.Provider != "OpenAI" {
		t.Fatalf("popup rewrote the default %+v", a.data.Defaults)
	}
}

func TestProviderFieldsPersistAcrossSwitch(t *testing.T) {
	a := newTestApp(t)
	temp := 0.6
	a.data.Defaults.Provider = "OpenAI"
	a.data.Defaults.Model = "gpt-4o-mini"
	a.data.Defaults.BaseURL = "https://api.openai.com/v1"
	a.data.Defaults.SystemPrompt = "stay"
	a.data.Defaults.Reasoning = "medium"
	a.data.Defaults.MaxTokens = 100
	a.data.Defaults.Temperature = &temp
	a.data.Conversations = []Conversation{{
		ID: "c", Title: "Kept",
		Config: Config{
			Provider: "Anthropic", Model: "claude-sonnet-4.5",
			BaseURL: "https://api.anthropic.com/v1", SystemPrompt: "chat prompt",
		},
		Messages: []Message{{ID: "m", Role: roleUser, Content: "hi", Done: true}},
	}}
	before := a.data.Defaults
	chats := append([]Conversation(nil), a.data.Conversations...)
	seedBuiltinRow(a, "OpenAI")
	seedBuiltinRow(a, "Anthropic")

	tt := openTester(a)
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Models & Providers"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("OpenAI"); err != nil {
		t.Fatal(err)
	}
	replaceCommitted(t, tt, "Base URL", "https://openai.example/v1")
	if err := tt.Click("Show key"); err != nil {
		t.Fatal(err)
	}
	replaceCommitted(t, tt, "API key", "sk-openai-persist")
	if err := tt.Click("Add model…"); err != nil {
		t.Fatal(err)
	}
	replaceCommitted(t, tt, "Model ID", "gpt-a")
	if err := tt.Click("Add model…"); err != nil {
		t.Fatal(err)
	}
	replaceCommitted(t, tt, "Model ID", "gpt-b")
	if !tt.HasText("✓ Saved") {
		t.Fatal("committed field did not show saved")
	}
	if err := tt.Click("Back"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Anthropic"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Base URL"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("https://anthropic.example/v1")
	if err := tt.Click("Back"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("OpenAI"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.provEdit.baseURL != "https://openai.example/v1" || a.provEdit.apiKey != "sk-openai-persist" {
		t.Fatalf("openai fields %+v", a.provEdit)
	}
	if !containsModel(a.provEdit.models, "gpt-a") || !containsModel(a.provEdit.models, "gpt-b") {
		t.Fatalf("openai models %+v", a.provEdit.models)
	}
	if err := tt.Click("Back"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Anthropic"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.provEdit.baseURL != "https://anthropic.example/v1" {
		t.Fatalf("anthropic base %q", a.provEdit.baseURL)
	}
	if !containsModel(a.provEdit.models, "claude-sonnet-4.5") {
		t.Fatalf("anthropic models %+v", a.provEdit.models)
	}
	if a.provEdit.apiKey != "" {
		t.Fatalf("anthropic key %q", a.provEdit.apiKey)
	}
	if a.data.Defaults != before {
		t.Fatalf("defaults changed %+v", a.data.Defaults)
	}
	if !reflect.DeepEqual(chats, a.data.Conversations) {
		t.Fatal("chats changed")
	}
}

func containsModel(models []ProviderModel, id string) bool {
	for _, m := range models {
		if m.ID == id {
			return true
		}
	}
	return false
}

func seedBuiltinRow(a *App, name string) {
	if a.providerConnection(name) != nil {
		return
	}
	a.data.Connections = append(a.data.Connections, Connection{
		ID: presetSlug(name), Name: name, Provider: name,
	})
}

func connectBuiltin(a *App, name, key string) {
	id := "conn-" + strings.ToLower(name)
	a.data.Connections = append(a.data.Connections, Connection{ID: id, Name: name, Provider: name})
	if err := a.keys.Set(id, key); err != nil {
		panic(err)
	}
}

func replaceCommitted(t *testing.T, tt *ui.Tester, label, text string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type(text)
	tt.Key(0, ui.KeyEnter)
}

func TestMigrateModelsProviders(t *testing.T) {
	custom := loadRaw(t, `{
		"version": 5,
		"defaults": {
			"provider": "Anthropic",
			"model": "claude-kept",
			"baseUrl": "https://proxy.anthropic.example/v1",
			"systemPrompt": "stay",
			"temperature": 0.4,
			"maxTokens": 777,
			"models": "claude-kept, claude-extra, claude-kept"
		},
		"conversations": [
			{"id":"c1","title":"Kept","config":{"provider":"DeepSeek","model":"deepseek-flash","baseUrl":"https://api.deepseek.com/v1","models":"should-clear"},"messages":[{"id":"m","role":"user","content":"hi","done":true}]}
		]
	}`)
	if custom.Version != storeVersion {
		t.Fatalf("version %d", custom.Version)
	}
	if custom.Defaults.Model != "claude-kept" || custom.Defaults.MaxTokens != 777 || custom.Defaults.SystemPrompt != "stay" {
		t.Fatalf("defaults %+v", custom.Defaults)
	}
	if custom.Defaults.Temperature == nil || *custom.Defaults.Temperature != 0.4 {
		t.Fatalf("temperature %+v", custom.Defaults.Temperature)
	}
	if custom.Defaults.LegacyModels != "" || custom.Conversations[0].Config.LegacyModels != "" {
		t.Fatalf("legacy models left on defaults %q chat %q", custom.Defaults.LegacyModels, custom.Conversations[0].Config.LegacyModels)
	}
	anth := findConn(custom.Connections, "Anthropic")
	if anth == nil || anth.BaseURL != "https://proxy.anthropic.example/v1" || !anth.ModelsSet {
		t.Fatalf("anthropic %+v", anth)
	}
	if !sameModels(enabledIDs(anth.Models), []string{"claude-kept", "claude-extra"}) {
		t.Fatalf("models %+v", anth.Models)
	}
	if findConn(custom.Connections, "OpenAI") != nil || findConn(custom.Connections, "DeepSeek") != nil {
		t.Fatalf("other providers were created: %+v", custom.Connections)
	}

	official := loadRaw(t, `{
		"version": 4,
		"defaults": {
			"provider": "OpenAI",
			"model": "gpt-4o-mini",
			"baseUrl": "https://api.openai.com/v1/",
			"temperature": 0.2,
			"maxTokens": 128,
			"models": "gpt-a, gpt-b"
		},
		"conversations": [
			{"id":"c1","title":"Kept","config":{"provider":"OpenAI","model":"gpt-4o-mini","baseUrl":"https://api.openai.com/v1"},"messages":[{"id":"m","role":"user","content":"hi","done":true}]}
		]
	}`)
	if official.Defaults.Model != "gpt-4o-mini" || official.Defaults.MaxTokens != 128 || official.Defaults.Temperature == nil || *official.Defaults.Temperature != 0.2 {
		t.Fatalf("official defaults %+v", official.Defaults)
	}
	openai := findConn(official.Connections, "OpenAI")
	if openai == nil || openai.BaseURL != "" || !sameModels(enabledIDs(openai.Models), []string{"gpt-a", "gpt-b"}) {
		t.Fatalf("official address was stored: %+v", openai)
	}
}

func TestOfficialBaseIsNotPersisted(t *testing.T) {
	a := newTestApp(t)
	seedBuiltinRow(a, "OpenAI")
	tt := openTester(a)
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Models & Providers"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("OpenAI"); err != nil {
		t.Fatal(err)
	}
	replaceCommitted(t, tt, "Base URL", "https://api.openai.com/v1/")
	c := a.providerConnection("OpenAI")
	if c != nil && strings.TrimSpace(c.BaseURL) != "" {
		t.Fatalf("official base stored %q", c.BaseURL)
	}
	if a.provEdit.baseURL != "" {
		t.Fatalf("editor kept official base %q", a.provEdit.baseURL)
	}
}

func TestProviderStatusFollowsLocalProbe(t *testing.T) {
	a := newTestApp(t)
	connectBuiltin(a, "OpenAI", "sk-openai")
	tt := openTester(a)
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Models & Providers"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Key saved") || tt.HasText("Connected") {
		t.Fatalf("before test: %q", tt.Texts())
	}
	if err := tt.Click("OpenAI"); err != nil {
		t.Fatal(err)
	}
	providerFetch = func(base, key, format string) ([]string, int, bool, error) {
		return []string{"gpt-4o", "gpt-4o-mini"}, 200, false, nil
	}
	t.Cleanup(func() { providerFetch = nil })
	if err := tt.Click("Test Connection"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Connected · 2 models") {
		t.Fatalf("test result %q", tt.Texts())
	}
	if a.providerConnection("OpenAI").Probe != probeOK {
		t.Fatalf("probe %q", a.providerConnection("OpenAI").Probe)
	}
	if err := tt.Click("Back"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Connected") {
		t.Fatalf("list after success %q", tt.Texts())
	}
	if err := tt.Click("OpenAI"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Show key"); err != nil {
		t.Fatal(err)
	}
	replaceCommitted(t, tt, "API key", "sk-openai-2")
	if a.providerConnection("OpenAI").Probe != "" {
		t.Fatalf("probe after key change %q", a.providerConnection("OpenAI").Probe)
	}
	if err := tt.Click("Back"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("Connected") || !tt.HasText("Key saved") {
		t.Fatalf("list after key change %q", tt.Texts())
	}
}

// TestConnectionStatusResetsAfterEdit checks the row status: Connected
// only after the latest Test Connection succeeded, Key saved after the
// key changes, and Configured after a keyless provider's base URL changes.
func TestConnectionStatusResetsAfterEdit(t *testing.T) {
	a := newTestApp(t)
	connectBuiltin(a, "OpenAI", "sk-openai")
	providerFetch = func(base, key, format string) ([]string, int, bool, error) {
		return []string{"one", "two"}, 200, false, nil
	}
	t.Cleanup(func() { providerFetch = nil })

	tt := openTester(a)
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Models & Providers"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("OpenAI"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Test Connection"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if kind, label := a.providerStatus(a.providerConnection("OpenAI"), false); kind != "ok" || label != "Connected" {
		t.Fatalf("after test: %s %s", kind, label)
	}
	if err := tt.Click("Back"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Connected") || tt.HasText("Key saved") {
		t.Fatalf("list after test: %q", tt.Texts())
	}

	if err := tt.Click("OpenAI"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Show key"); err != nil {
		t.Fatal(err)
	}
	replaceCommitted(t, tt, "API key", "sk-openai-changed")
	if kind, label := a.providerStatus(a.providerConnection("OpenAI"), false); kind != "saved" || label != "Key saved" {
		t.Fatalf("after key edit: %s %s", kind, label)
	}
	if err := tt.Click("Back"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("Connected") || !tt.HasText("Key saved") {
		t.Fatalf("list after key edit: %q", tt.Texts())
	}

	seedBuiltinRow(a, "Ollama")
	tt.Frame()
	if err := tt.Click("Ollama"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Test Connection"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if kind, label := a.providerStatus(a.providerConnection("Ollama"), true); kind != "ok" || label != "Connected" {
		t.Fatalf("ollama after test: %s %s", kind, label)
	}
	replaceCommitted(t, tt, "Base URL", "http://10.0.0.8:11434/v1")
	if kind, label := a.providerStatus(a.providerConnection("Ollama"), true); kind != "saved" || label != "Configured" {
		t.Fatalf("ollama after base edit: %s %s probe %q base %q", kind, label, a.providerConnection("Ollama").Probe, a.providerConnection("Ollama").BaseURL)
	}
	if err := tt.Click("Back"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("Connected") || !tt.HasText("Configured") || !tt.HasText("Key saved") {
		t.Fatalf("list after base edit: %q", tt.Texts())
	}
}

func TestRuntime401MarksKeyInvalid(t *testing.T) {
	a := newTestApp(t)
	a.data.Connections = []Connection{{ID: "c", Name: "OpenAI", Provider: "OpenAI", Probe: probeOK}}
	a.data.Conversations = []Conversation{{
		ID:       "chat",
		Config:   Config{Provider: "OpenAI", ConnectionID: "c", Model: "gpt-4o-mini", BaseURL: "https://api.openai.com/v1"},
		Messages: []Message{{ID: "m", Role: roleUser, Content: "hi", Done: true}},
	}}
	a.noteProviderAuth("chat", &StatusError{Status: 500, Body: "nope"})
	if a.providerConnection("OpenAI").Probe != probeOK {
		t.Fatal("500 cleared a good probe")
	}
	a.noteProviderAuth("chat", &StatusError{Status: 401, Body: "bad key"})
	if a.providerConnection("OpenAI").Probe != probeInvalid {
		t.Fatalf("probe %q", a.providerConnection("OpenAI").Probe)
	}
	kind, label := a.providerStatus(a.providerConnection("OpenAI"), false)
	if kind != "invalid" || label != "Key invalid" {
		t.Fatalf("status %s %s", kind, label)
	}
}

func TestDeleteCustomFallsBack(t *testing.T) {
	a := newTestApp(t)
	connectBuiltin(a, "OpenAI", "sk-openai")
	a.data.Connections = append(a.data.Connections, Connection{
		ID: "custom-1", Name: "Together AI", Provider: "Custom", Custom: true,
		BaseURL: "https://api.together.xyz/v1",
		Models:  []ProviderModel{{ID: "meta-llama", Enabled: true}}, ModelsSet: true,
	})
	if err := a.keys.Set("custom-1", "tg-key"); err != nil {
		t.Fatal(err)
	}
	a.data.Defaults.Provider = "Together AI"
	a.data.Defaults.Model = "meta-llama"
	a.data.Defaults.ConnectionID = "custom-1"
	a.deleteCustomProvider("custom-1", time.Now())
	if a.providerConnection("Together AI") != nil {
		t.Fatal("custom provider still present")
	}
	if strings.TrimSpace(a.resolvedKey(Config{ConnectionID: "custom-1"})) != "" {
		t.Fatal("key still stored")
	}
	if a.data.Defaults.Provider != "OpenAI" || a.data.Defaults.Model != "gpt-4o-mini" {
		t.Fatalf("fallback %+v", a.data.Defaults)
	}
	if a.settingsPage != settingsList || !strings.Contains(a.providerNotice, "Together AI") || !strings.Contains(a.providerNotice, "gpt-4o-mini") {
		t.Fatalf("page %q notice %q", a.settingsPage, a.providerNotice)
	}
}

// TestDefaultModelPrefixStaysVisible reads the text layout the painter
// draws, at 720×520. A name that belongs to one provider is the model id
// alone. A duplicated name keeps the provider, and a long id ellipsizes
// only the model, on the closed button and on menu rows. Menu rows always
// draw "Provider · model". The space after the dot matches the space before it.
func TestDefaultModelPrefixStaysVisible(t *testing.T) {
	short := newTestApp(t)
	short.data.Defaults.Provider = "OpenAI"
	short.data.Defaults.Model = "gpt-4o-mini"
	short.data.Defaults.ConnectionID = "conn-openai"
	connectBuiltin(short, "OpenAI", "sk-openai")
	short.prefReady = false
	short.settingsOpen = true
	short.settingsSection = "general"
	short.settingsPage = settingsList
	st := ui.NewTester(short.view, 720, 520)
	if err := st.Click("gpt-4o-mini"); err != nil {
		t.Fatal(err)
	}
	assertDrawn(t, st, "gpt-4o-mini", "gpt-4o-mini", false, 1)
	assertDrawn(t, st, "OpenAI ·", "OpenAI ·", false, 1)
	assertDrawn(t, st, " gpt-4o-mini", " gpt-4o-mini", false, 1)
	if n := countDrawn(st, "OpenAI ·"); n != 1 {
		t.Fatalf("unique name prefixed the trigger and the row: %d", n)
	}
	assertDotGaps(t, st, 1)

	const long = "accounts/fireworks/models/llama-v3p1-405b-instruct-extra-long-id"
	const prefix = "OpenRouter ·"
	longApp := func(shared bool) *App {
		a := newTestApp(t)
		a.data.Defaults.Provider = "OpenRouter"
		a.data.Defaults.Model = long
		a.data.Defaults.ConnectionID = "conn-openrouter"
		a.data.Connections = append(a.data.Connections, Connection{
			ID: "conn-openrouter", Name: "OpenRouter", Provider: "OpenRouter",
			Models: []ProviderModel{
				{ID: long, Enabled: true},
				{ID: "openai/gpt-4o-mini", Enabled: true},
			},
			ModelsSet: true,
		})
		if err := a.keys.Set("conn-openrouter", "sk-or-test"); err != nil {
			t.Fatal(err)
		}
		if shared {
			a.data.Connections = append(a.data.Connections, Connection{
				ID: "conn-relay", Name: "Relay", Provider: "Custom", Custom: true,
				BaseURL: "https://relay.example/v1",
				Models:  []ProviderModel{{ID: long, Enabled: true}}, ModelsSet: true,
			})
			if err := a.keys.Set("conn-relay", "sk-relay"); err != nil {
				t.Fatal(err)
			}
		}
		a.prefReady = false
		a.settingsOpen = true
		a.settingsSection = "general"
		a.settingsPage = settingsList
		return a
	}
	dir := filepath.Join("/opt/cursor/artifacts", "r4")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{false, true} {
		a := longApp(false)
		tt := ui.NewTester(a.view, 720, 520)
		if dark {
			tt.SetDark(true)
		}
		if err := tt.Click(long); err != nil {
			t.Fatal(err)
		}
		assertDrawn(t, tt, prefix, prefix, false, 2)
		assertDrawn(t, tt, " openai/gpt-4o-mini", " openai/gpt-4o-mini", false, 1)
		assertDotGaps(t, tt, 2)
		n := 0
		for _, d := range drawnTexts(tt) {
			raw := strings.TrimPrefix(d.text, " ")
			if raw != long {
				continue
			}
			n++
			body := strings.TrimPrefix(strings.TrimSuffix(d.shown, "…"), " ")
			if !d.truncated || strings.TrimPrefix(d.shown, " ") == long || !strings.HasPrefix(long, body) || !strings.HasPrefix(strings.TrimPrefix(d.shown, " "), "accounts/fireworks/") {
				t.Fatalf("long model draw %+v", d)
			}
			if strings.Contains(d.shown, "OpenRoute") {
				t.Fatalf("model draw ate the provider: %+v", d)
			}
		}
		if n < 2 {
			t.Fatalf("long model drawn %d times, want the button and its menu row", n)
		}
		name := "settings-general-long-model-light.png"
		if dark {
			name = "settings-general-long-model-dark.png"
		}
		writePNG(t, filepath.Join(dir, name), tt.Image())
	}

	dup := longApp(true)
	td := ui.NewTester(dup.view, 720, 520)
	if err := td.Click("OpenRouter"); err != nil {
		t.Fatal(err)
	}
	assertDrawn(t, td, "OpenRouter", "OpenRouter", false, 1)
	assertDrawn(t, td, " ·", " ·", false, 1)
	n := 0
	for _, d := range drawnTexts(td) {
		if d.text != " "+long {
			continue
		}
		n++
		if !d.truncated || strings.Contains(d.shown, "OpenRoute") {
			t.Fatalf("shared long model %+v", d)
		}
	}
	if n < 2 {
		t.Fatalf("shared model drawn %d times, want the trigger and its row", n)
	}
}

func countDrawn(tt *ui.Tester, text string) int {
	n := 0
	for _, d := range drawnTexts(tt) {
		if d.text == text {
			n++
		}
	}
	return n
}

// drawnText is one text node as the painter lays it out. shown is the
// runes on the first line, plus the ellipsis when the layout cut them.
type drawnText struct {
	text, shown string
	truncated   bool
}

func drawnTexts(tt *ui.Tester) []drawnText {
	rt := reflectField(reflect.ValueOf(tt).Elem(), "rt")
	root := reflectField(reflectField(rt, "c"), "root")
	var out []drawnText
	var walk func(reflect.Value)
	walk = func(n reflect.Value) {
		if !n.IsValid() || n.IsNil() {
			return
		}
		el := n.Elem()
		if text := el.FieldByName("text").String(); text != "" {
			if d, ok := drawnOf(text, el.FieldByName("tl")); ok {
				out = append(out, d)
			}
		}
		for ch := el.FieldByName("first"); ch.IsValid() && !ch.IsNil(); ch = reflectField(ch, "next") {
			walk(ch)
		}
	}
	walk(root)
	return out
}

func drawnOf(text string, tl reflect.Value) (drawnText, bool) {
	if !tl.IsValid() || tl.IsNil() {
		return drawnText{}, false
	}
	lay := tl.Elem()
	lines := lay.FieldByName("Lines")
	if lines.Len() == 0 {
		return drawnText{}, false
	}
	line := lines.Index(0)
	start := int(line.FieldByName("Start").Int())
	end := int(line.FieldByName("End").Int())
	runes := []rune(lay.FieldByName("Params").FieldByName("Text").String())
	shown := ""
	if start >= 0 && end <= len(runes) && start <= end {
		shown = string(runes[start:end])
	}
	trunc := lay.FieldByName("Truncated").Bool()
	if trunc {
		shown += "…"
	}
	return drawnText{text: text, shown: shown, truncated: trunc}, true
}

// assertDotGaps checks every drawn "Provider ·" / " model" pair. The
// advance before the dot and the advance after it are the two spaces.
func assertDotGaps(t *testing.T, tt *ui.Tester, min int) {
	t.Helper()
	n := 0
	var walk func(reflect.Value)
	walk = func(node reflect.Value) {
		if !node.IsValid() || node.IsNil() {
			return
		}
		el := node.Elem()
		var prev reflect.Value
		for ch := el.FieldByName("first"); ch.IsValid() && !ch.IsNil(); ch = reflectField(ch, "next") {
			if prev.IsValid() {
				left := prev.Elem().FieldByName("text").String()
				right := ch.Elem().FieldByName("text").String()
				if strings.HasSuffix(left, " ·") && strings.HasPrefix(right, " ") {
					before := spaceBeforeDot(left, prev.Elem().FieldByName("tl"))
					after := leadingSpace(ch.Elem().FieldByName("tl"))
					if before < 1 || after < 1 || abs32(before-after) > 1 {
						t.Fatalf("dot gap in %q / %q: before %.2f after %.2f", left, right, before, after)
					}
					n++
				}
			}
			prev = ch
			walk(ch)
		}
	}
	rt := reflectField(reflect.ValueOf(tt).Elem(), "rt")
	walk(reflectField(reflectField(rt, "c"), "root"))
	if n < min {
		t.Fatalf("measured %d dot gaps, want at least %d", n, min)
	}
}

func spaceBeforeDot(text string, tl reflect.Value) float32 {
	dot := strings.IndexRune(text, '·')
	gs := lineGlyphs(tl)
	for i, g := range gs {
		if i == 0 || g.cluster > dot || dot >= g.cluster+g.runes {
			continue
		}
		return g.x - gs[i-1].x
	}
	return 0
}

func leadingSpace(tl reflect.Value) float32 {
	gs := lineGlyphs(tl)
	if len(gs) < 2 {
		return 0
	}
	return gs[1].x - gs[0].x
}

type laidGlyph struct {
	cluster, runes int
	x              float32
}

func lineGlyphs(tl reflect.Value) []laidGlyph {
	if !tl.IsValid() || tl.IsNil() {
		return nil
	}
	lines := tl.Elem().FieldByName("Lines")
	if lines.Len() == 0 {
		return nil
	}
	gs := lines.Index(0).FieldByName("Glyphs")
	out := make([]laidGlyph, gs.Len())
	for i := range out {
		g := gs.Index(i)
		out[i] = laidGlyph{
			cluster: int(g.FieldByName("Cluster").Int()),
			runes:   int(g.FieldByName("Runes").Int()),
			x:       float32(g.FieldByName("X").Float()),
		}
	}
	return out
}

func assertDrawn(t *testing.T, tt *ui.Tester, text, shown string, truncated bool, min int) {
	t.Helper()
	n := 0
	for _, d := range drawnTexts(tt) {
		if d.text != text {
			continue
		}
		n++
		if d.truncated != truncated || d.shown != shown {
			t.Fatalf("drawn %q: got %q truncated=%v, want %q truncated=%v", text, d.shown, d.truncated, shown, truncated)
		}
	}
	if n < min {
		t.Fatalf("drew %q %d times, want at least %d", text, n, min)
	}
}

func reflectField(v reflect.Value, name string) reflect.Value {
	if v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if !v.IsValid() || v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	f := v.FieldByName(name)
	return reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem()
}

func findConn(list []Connection, name string) *Connection {
	for i := range list {
		if list[i].Provider == name || list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}
