package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestModelMenuSelectsEachProviderSharingURL(t *testing.T) {
	var auth, ctype, accept string
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if key := r.Header.Get("api-key"); key != "" {
			auth = "api-key " + key
		}
		ctype = r.Header.Get("Content-Type")
		accept = r.Header.Get("Accept")
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	shared := srv.URL + "/v1"
	keys := newMemKeys()
	path := filepath.Join(t.TempDir(), "conversations.json")
	a := openApp(path, keys)
	a.instant = true
	a.data.Connections = []Connection{
		customConn("conn-north", "North", shared, "shared-model"),
		customConn("conn-south", "South", shared, "shared-model"),
	}
	if err := a.keys.Set("conn-north", "north-key"); err != nil {
		t.Fatal(err)
	}
	if err := a.keys.Set("conn-south", "south-key"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	a.data.Defaults = DefaultConfig()
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Relay", CreatedAt: now, UpdatedAt: now,
		Config: Config{
			Provider: "North", Model: "shared-model", BaseURL: shared, ConnectionID: "conn-north",
			SystemPrompt: DefaultConfig().SystemPrompt,
		},
		Messages: []Message{{ID: "m", Role: roleUser, Content: "hi", Done: true, CreatedAt: now}},
	}}
	a.data.ActiveID = "c1"
	a.save()

	opts := a.menuModels()
	var sawNorth, sawSouth bool
	for _, opt := range opts {
		if opt.model != "shared-model" {
			continue
		}
		if opt.connID == "conn-north" {
			sawNorth = true
		}
		if opt.connID == "conn-south" {
			sawSouth = true
		}
	}
	if !sawNorth || !sawSouth {
		t.Fatalf("menu collapsed the shared model: %+v", opts)
	}

	tt := openTester(a)
	if _, ok := tt.Find("Model North · shared-model"); !ok {
		t.Fatalf("chip %q", tt.Texts())
	}
	if err := tt.Click("Model North · shared-model"); err != nil {
		t.Fatal(err)
	}
	rows := menuRows(t, tt)
	if on, ok := modelChecked(rows, "North", "shared-model"); !ok || !on {
		t.Fatalf("north check %+v", rows)
	}
	if on, ok := modelChecked(rows, "South", "shared-model"); !ok || on {
		t.Fatalf("south check %+v", rows)
	}
	chooseGroupedModel(t, tt, "South", "shared-model")
	if _, ok := tt.Find("Model South · shared-model"); !ok {
		t.Fatalf("label did not update: %q", tt.Texts())
	}
	if _, ok := tt.Find("Model North · shared-model"); ok {
		t.Fatal("old provider label still shown")
	}
	cfg := a.active().Config
	if cfg.ConnectionID != "conn-south" || cfg.Model != "shared-model" || cfg.Provider != "South" {
		t.Fatalf("selection %+v", cfg)
	}
	assertHeaderIdentity(t, filepath.Join(filepath.Dir(path), chatsDir, "c1.jsonl"), "conn-south", "shared-model")

	focusDraft(t, tt)
	tt.Type("south")
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer south-key" || strings.Contains(auth, "north-key") {
		t.Fatalf("south request auth %q body %s", auth, body)
	}
	if ctype != "application/json" || !strings.Contains(accept, "text/event-stream") || !strings.Contains(body, `"model":"shared-model"`) {
		t.Fatalf("south headers content-type %q accept %q body %s", ctype, accept, body)
	}

	if err := tt.Click("Model South · shared-model"); err != nil {
		t.Fatal(err)
	}
	rows = menuRows(t, tt)
	if on, ok := modelChecked(rows, "North", "shared-model"); !ok || on {
		t.Fatalf("north check after south %+v", rows)
	}
	if on, ok := modelChecked(rows, "South", "shared-model"); !ok || !on {
		t.Fatalf("south check after switch %+v", rows)
	}
	chooseGroupedModel(t, tt, "North", "shared-model")
	if _, ok := tt.Find("Model North · shared-model"); !ok {
		t.Fatalf("label did not return to North: %q", tt.Texts())
	}
	if a.active().Config.ConnectionID != "conn-north" || a.active().Config.Model != "shared-model" {
		t.Fatalf("north selection %+v", a.active().Config)
	}
	assertHeaderIdentity(t, filepath.Join(filepath.Dir(path), chatsDir, "c1.jsonl"), "conn-north", "shared-model")

	focusDraft(t, tt)
	tt.Type("north")
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer north-key" || strings.Contains(auth, "south-key") {
		t.Fatalf("north request auth %q", auth)
	}

	b := openApp(path, keys)
	b.instant = true
	tb := openTester(b)
	if b.active().Config.ConnectionID != "conn-north" || b.active().Config.Model != "shared-model" || b.active().Config.Provider != "North" {
		t.Fatalf("restart %+v", b.active().Config)
	}
	if _, ok := tb.Find("Model North · shared-model"); !ok {
		t.Fatalf("restart label %q", tb.Texts())
	}
	if b.resolvedKey(b.active().Config) != "north-key" {
		t.Fatalf("restart key %q", b.resolvedKey(b.active().Config))
	}
}

// TestModelMenuGroupsUnderProvider is the composer menu: a disabled
// provider name heads each group, and the rows under it are model ids.
// "Provider · model" stays on the closed chip when that name is enabled
// on two providers.
func TestModelMenuGroupsUnderProvider(t *testing.T) {
	base := "https://api.openai.com/v1"
	keys := newMemKeys()
	path := filepath.Join(t.TempDir(), "conversations.json")
	a := openApp(path, keys)
	a.instant = true
	openai := customConn("openai", "OpenAI", base, "gpt-4o", "gpt-4o-mini")
	openai.Models = append(openai.Models, ProviderModel{ID: "o3", Enabled: false})
	openai2 := customConn("openai-2", "OpenAI 2", base, "gpt-4o", "gpt-4o-mini")
	a.data.Connections = []Connection{openai, openai2}
	if err := a.keys.Set("openai", "key-1"); err != nil {
		t.Fatal(err)
	}
	if err := a.keys.Set("openai-2", "key-2"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	a.data.Defaults = DefaultConfig()
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Relay", CreatedAt: now, UpdatedAt: now,
		Config: Config{
			Provider: "OpenAI 2", Model: "gpt-4o", BaseURL: base, ConnectionID: "openai-2",
			SystemPrompt: DefaultConfig().SystemPrompt,
		},
		Messages: []Message{{ID: "m", Role: roleUser, Content: "hi", Done: true, CreatedAt: now}},
	}}
	a.data.ActiveID = "c1"
	a.save()

	tt := openTester(a)
	if _, ok := tt.Find("Model OpenAI 2 · gpt-4o"); !ok {
		t.Fatalf("shared chip %q", tt.Texts())
	}
	if err := tt.Click("Model OpenAI 2 · gpt-4o"); err != nil {
		t.Fatal(err)
	}
	got := modelMenuLabels(t, tt)
	want := []string{
		"OpenAI", "gpt-4o", "gpt-4o-mini",
		"OpenAI 2", "gpt-4o", "gpt-4o-mini", "-",
		"Models & Providers…",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("menu %v, want %v", got, want)
	}
	rows := menuRows(t, tt)
	for _, r := range rows {
		if strings.Contains(r.label, " · ") {
			t.Fatalf("row still joins provider and model: %+v", rows)
		}
		if r.label == "o3" || strings.Contains(r.label, "o3") {
			t.Fatalf("disabled model listed: %+v", rows)
		}
	}
	if !headerRow(rows, "OpenAI") || !headerRow(rows, "OpenAI 2") {
		t.Fatalf("headers %+v", rows)
	}
	if on, ok := modelChecked(rows, "OpenAI 2", "gpt-4o"); !ok || !on {
		t.Fatalf("OpenAI 2 gpt-4o check %+v", rows)
	}
	for _, pair := range [][2]string{{"OpenAI", "gpt-4o"}, {"OpenAI", "gpt-4o-mini"}, {"OpenAI 2", "gpt-4o-mini"}} {
		if on, ok := modelChecked(rows, pair[0], pair[1]); !ok || on {
			t.Fatalf("%s %s check %+v", pair[0], pair[1], rows)
		}
	}

	chooseGroupedModel(t, tt, "OpenAI", "gpt-4o-mini")
	cfg := a.active().Config
	if cfg.ConnectionID != "openai" || cfg.Model != "gpt-4o-mini" || cfg.Provider != "OpenAI" {
		t.Fatalf("choice %+v", cfg)
	}
	if _, ok := tt.Find("Model OpenAI · gpt-4o-mini"); !ok {
		t.Fatalf("chip after choice %q", tt.Texts())
	}
	if err := tt.Click("Model OpenAI · gpt-4o-mini"); err != nil {
		t.Fatal(err)
	}
	rows = menuRows(t, tt)
	if on, ok := modelChecked(rows, "OpenAI", "gpt-4o-mini"); !ok || !on {
		t.Fatalf("OpenAI gpt-4o-mini check %+v", rows)
	}
	if on, ok := modelChecked(rows, "OpenAI 2", "gpt-4o"); !ok || on {
		t.Fatalf("OpenAI 2 gpt-4o stayed checked %+v", rows)
	}
}

func TestUseModelSameNameDoesNotCollapse(t *testing.T) {
	a := newTestApp(t)
	shared := "https://shared.example/v1"
	a.data.Connections = []Connection{
		customConn("conn-a", "Relay", shared, "shared-model"),
		customConn("conn-b", "Relay", shared, "shared-model"),
	}
	if err := a.keys.Set("conn-a", "key-a"); err != nil {
		t.Fatal(err)
	}
	if err := a.keys.Set("conn-b", "key-b"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	a.data.Conversations = []Conversation{{
		ID: "c", Title: "T", CreatedAt: now, UpdatedAt: now,
		Config:   Config{Provider: "Relay", Model: "shared-model", BaseURL: shared, ConnectionID: "conn-a"},
		Messages: []Message{{ID: "m", Role: roleUser, Content: "hi", Done: true, CreatedAt: now}},
	}}
	a.data.ActiveID = "c"
	var ids []string
	for _, opt := range a.menuModels() {
		if opt.model == "shared-model" {
			ids = append(ids, opt.connID)
		}
	}
	if strings.Join(ids, ",") != "conn-a,conn-b" {
		t.Fatalf("menu ids %v", ids)
	}
	if !modelChosen(a.active().Config, "conn-a", "shared-model") || modelChosen(a.active().Config, "conn-b", "shared-model") {
		t.Fatal("both relays looked selected")
	}
	a.useModel("conn-b", "shared-model")
	cfg := a.active().Config
	if cfg.ConnectionID != "conn-b" || cfg.Model != "shared-model" || cfg.BaseURL != shared {
		t.Fatalf("useModel %+v", cfg)
	}
	if a.resolvedKey(cfg) != "key-b" {
		t.Fatalf("key %q", a.resolvedKey(cfg))
	}
	if modelChosen(cfg, "conn-a", "shared-model") || !modelChosen(cfg, "conn-b", "shared-model") {
		t.Fatal("checkmark followed the name instead of the id")
	}
}

func TestChatHeaderMigratesURLAndBareModel(t *testing.T) {
	const shared = "https://shared.example/v1"
	const eastURL = "https://east.example/v1"
	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.json")
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	defaults := DefaultConfig()
	defaults.Provider = "North"
	defaults.Model = "shared-model"
	defaults.BaseURL = shared
	defaults.ConnectionID = "conn-north"
	data := defaultData()
	data.Defaults = defaults
	data.Connections = []Connection{
		customConn("conn-north", "North", shared, "shared-model"),
		customConn("conn-south", "South", shared, "shared-model", "south-only"),
		customConn("conn-east", "East", eastURL, "shared-model"),
	}
	msg := func(id string) []Message {
		return []Message{{ID: id, Role: roleUser, Content: id, Done: true, CreatedAt: now}}
	}
	data.Conversations = []Conversation{
		{ID: "named", Title: "Named", CreatedAt: now, UpdatedAt: now, Messages: msg("n"), Config: Config{Provider: "South", Model: "shared-model", BaseURL: shared, ConnectionID: "conn-south", SystemPrompt: defaults.SystemPrompt}},
		{ID: "bare", Title: "Bare", CreatedAt: now, UpdatedAt: now.Add(time.Second), Messages: msg("b"), Config: Config{Provider: "North", Model: "south-only", BaseURL: shared, ConnectionID: "conn-north", SystemPrompt: defaults.SystemPrompt}},
		{ID: "ambiguous", Title: "Ambiguous", CreatedAt: now, UpdatedAt: now.Add(2 * time.Second), Messages: msg("a"), Config: Config{Provider: "North", Model: "shared-model", BaseURL: shared, ConnectionID: "conn-north", SystemPrompt: defaults.SystemPrompt}},
		{ID: "pinned", Title: "Pinned", CreatedAt: now, UpdatedAt: now.Add(3 * time.Second), Messages: msg("p"), Config: Config{Provider: "South", Model: "shared-model", BaseURL: shared, ConnectionID: "conn-south", SystemPrompt: defaults.SystemPrompt}},
		{ID: "east", Title: "East", CreatedAt: now, UpdatedAt: now.Add(4 * time.Second), Messages: msg("e"), Config: Config{Provider: "East", Model: "shared-model", BaseURL: eastURL, ConnectionID: "conn-east", SystemPrompt: defaults.SystemPrompt}},
	}
	data.ActiveID = "named"
	if err := Save(path, &data); err != nil {
		t.Fatal(err)
	}
	replaceChatSettings(t, filepath.Join(dir, chatsDir, "named.jsonl"), `{"provider":"South","model":"shared-model","baseUrl":"https://shared.example/v1"}`)
	replaceChatSettings(t, filepath.Join(dir, chatsDir, "ambiguous.jsonl"), `{"model":"shared-model","baseUrl":"https://shared.example/v1"}`)
	replaceChatSettings(t, filepath.Join(dir, chatsDir, "pinned.jsonl"), `{"connectionId":"conn-south","provider":"North","model":"shared-model","baseUrl":"https://shared.example/v1"}`)
	replaceChatSettings(t, filepath.Join(dir, chatsDir, "east.jsonl"), `{"baseUrl":"https://east.example/v1","model":"shared-model"}`)

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	named := findConv(got.Conversations, "named")
	if named.Config.ConnectionID != "conn-south" || named.Config.Model != "shared-model" || named.Config.Provider != "South" {
		t.Fatalf("named provider was resolved from the shared URL: %+v", named.Config)
	}
	bare := findConv(got.Conversations, "bare")
	if bare.Config.ConnectionID != "conn-south" || bare.Config.Model != "south-only" {
		t.Fatalf("bare model %+v", bare.Config)
	}
	amb := findConv(got.Conversations, "ambiguous")
	if amb.Config.ConnectionID != "conn-north" {
		t.Fatalf("shared URL picked a provider: %+v", amb.Config)
	}
	pinned := findConv(got.Conversations, "pinned")
	if pinned.Config.ConnectionID != "conn-south" {
		t.Fatalf("explicit id was replaced: %+v", pinned.Config)
	}
	east := findConv(got.Conversations, "east")
	if east.Config.ConnectionID != "conn-east" || east.Config.Provider != "East" {
		t.Fatalf("unique URL %+v", east.Config)
	}

	// The first Save cached headers that already had connection ids. The
	// patched files are what an older build left on disk, so the next save
	// has to read them the way a new process would.
	delete(writers, storeRoot(path))
	if err := Save(path, &got); err != nil {
		t.Fatal(err)
	}
	assertHeaderIdentity(t, filepath.Join(dir, chatsDir, "named.jsonl"), "conn-south", "shared-model")
	assertHeaderIdentity(t, filepath.Join(dir, chatsDir, "bare.jsonl"), "conn-south", "south-only")
	ambRaw, err := os.ReadFile(filepath.Join(dir, chatsDir, "ambiguous.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ambRaw, []byte("conn-south")) {
		t.Fatalf("ambiguous header took the other provider:\n%s", ambRaw)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if findConv(again.Conversations, "named").Config.ConnectionID != "conn-south" || findConv(again.Conversations, "bare").Config.ConnectionID != "conn-south" || findConv(again.Conversations, "east").Config.ConnectionID != "conn-east" {
		t.Fatalf("reload lost the migrated ids")
	}
}

func TestUpgradeMigratesChatsAndDefaults(t *testing.T) {
	const eastURL = "https://east.example/v1"
	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.json")
	settings := []byte(`{
		"version": 7,
		"activeId": "amb",
		"defaults": {
			"provider": "South",
			"model": "shared-model",
			"baseUrl": "https://shared.example/v1",
			"systemPrompt": "stay",
			"maxTokens": 128
		},
		"connections": [
			{"id":"conn-north","name":"North","provider":"Custom","custom":true,"baseUrl":"https://shared.example/v1","modelsSet":true,"models":[{"id":"shared-model","enabled":true}]},
			{"id":"conn-south","name":"South","provider":"Custom","custom":true,"baseUrl":"https://shared.example/v1","modelsSet":true,"models":[{"id":"shared-model","enabled":true},{"id":"south-only","enabled":true}]},
			{"id":"conn-east","name":"East","provider":"Custom","custom":true,"baseUrl":"https://east.example/v1","modelsSet":true,"models":[{"id":"org/east-model","enabled":true}]}
		]
	}`)
	if err := os.WriteFile(filepath.Join(dir, settingsFile), settings, 0o600); err != nil {
		t.Fatal(err)
	}
	idx := []byte(`{"schema":5,"chats":[
		{"id":"amb","title":"Amb","model":"shared-model","messageCount":1,"schema":5},
		{"id":"bare","title":"Bare","model":"south-only","messageCount":1,"schema":5},
		{"id":"east","title":"East","provider":"East","model":"org/east-model","messageCount":1,"schema":5},
		{"id":"pinned","title":"Pinned","provider":"North","model":"shared-model","messageCount":1,"schema":5}
	]}`)
	if err := os.WriteFile(filepath.Join(dir, indexFile), idx, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, chatsDir), 0o700); err != nil {
		t.Fatal(err)
	}
	writeLegacyChat(t, filepath.Join(dir, chatsDir, "amb.jsonl"), "amb", "Amb", `{"model":"shared-model","baseUrl":"https://shared.example/v1","maxTokens":64}`)
	writeLegacyChat(t, filepath.Join(dir, chatsDir, "bare.jsonl"), "bare", "Bare", `{"model":"south-only"}`)
	writeLegacyChat(t, filepath.Join(dir, chatsDir, "east.jsonl"), "east", "East", `{"provider":"East","model":"org/east-model","baseUrl":"https://east.example/v1"}`)
	writeLegacyChat(t, filepath.Join(dir, chatsDir, "pinned.jsonl"), "pinned", "Pinned", `{"connectionId":"conn-south","provider":"North","model":"shared-model","baseUrl":"https://shared.example/v1"}`)

	var logs []string
	prev := storeLog
	storeLog = func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	}
	defer func() { storeLog = prev }()

	keys := newMemKeys()
	if err := keys.Set("conn-north", "north-key"); err != nil {
		t.Fatal(err)
	}
	if err := keys.Set("conn-south", "south-key"); err != nil {
		t.Fatal(err)
	}
	a := openApp(path, keys)
	if a.data.Defaults.Model != "shared-model" || a.data.Defaults.MaxTokens != 128 || a.data.Defaults.SystemPrompt != "stay" {
		t.Fatalf("defaults model changed %+v", a.data.Defaults)
	}
	if a.data.Defaults.ConnectionID != "conn-south" || a.data.Defaults.Provider != "South" {
		t.Fatalf("defaults resolved %+v", a.data.Defaults)
	}
	if ambig := ambiguousLogs(logs); len(ambig) != 0 {
		t.Fatalf("opening settings logged a chat: %v", ambig)
	}

	for _, id := range []string{"amb", "bare", "east", "pinned"} {
		a.ensureChat(id)
	}
	ambig := ambiguousLogs(logs)
	if len(ambig) != 1 || !strings.Contains(ambig[0], "conn-north") || !strings.Contains(ambig[0], "shared-model") || strings.Contains(ambig[0], "using conn-south") {
		t.Fatalf("ambiguous log %v", ambig)
	}
	amb := a.conversation("amb")
	if amb.Config.ConnectionID != "conn-north" || amb.Config.Model != "shared-model" || amb.Config.MaxTokens != 64 || amb.Config.Provider != "North" {
		t.Fatalf("ambiguous chat %+v", amb.Config)
	}
	if a.resolvedKey(amb.Config) != "north-key" {
		t.Fatalf("ambiguous key %q", a.resolvedKey(amb.Config))
	}
	bare := a.conversation("bare")
	if bare.Config.ConnectionID != "conn-south" || bare.Config.Model != "south-only" {
		t.Fatalf("bare %+v", bare.Config)
	}
	if a.resolvedKey(bare.Config) != "south-key" {
		t.Fatalf("bare key %q", a.resolvedKey(bare.Config))
	}
	east := a.conversation("east")
	if east.Config.ConnectionID != "conn-east" || east.Config.Model != "org/east-model" || east.Config.Provider != "East" || east.Config.BaseURL != eastURL {
		t.Fatalf("east %+v", east.Config)
	}
	pinned := a.conversation("pinned")
	if pinned.Config.ConnectionID != "conn-south" || pinned.Config.Model != "shared-model" {
		t.Fatalf("pinned %+v", pinned.Config)
	}

	a.saveSync()
	assertHeaderIdentity(t, filepath.Join(dir, chatsDir, "amb.jsonl"), "conn-north", "shared-model")
	assertHeaderIdentity(t, filepath.Join(dir, chatsDir, "east.jsonl"), "conn-east", "org/east-model")
	rawIdx, err := os.ReadFile(filepath.Join(dir, indexFile))
	if err != nil {
		t.Fatal(err)
	}
	var doc indexDoc
	if err := json.Unmarshal(rawIdx, &doc); err != nil {
		t.Fatal(err)
	}
	gotIdx := map[string]indexEntry{}
	for _, e := range doc.Chats {
		gotIdx[e.ID] = e
	}
	if gotIdx["amb"].ConnectionID != "conn-north" || gotIdx["amb"].Model != "shared-model" {
		t.Fatalf("index amb %+v", gotIdx["amb"])
	}
	if gotIdx["east"].ConnectionID != "conn-east" || gotIdx["east"].Model != "org/east-model" {
		t.Fatalf("index east %+v", gotIdx["east"])
	}
	settingsRaw, err := os.ReadFile(filepath.Join(dir, settingsFile))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(settingsRaw, []byte(`"model": "shared-model"`)) || !bytes.Contains(settingsRaw, []byte(`"connectionId": "conn-south"`)) {
		t.Fatalf("settings defaults\n%s", settingsRaw)
	}

	logs = nil
	b := openApp(path, keys)
	for _, id := range []string{"amb", "bare", "east", "pinned"} {
		b.ensureChat(id)
	}
	if again := ambiguousLogs(logs); len(again) != 0 {
		t.Fatalf("second launch logged %v", again)
	}
	if b.data.Defaults.Model != "shared-model" || b.data.Defaults.ConnectionID != "conn-south" || b.data.Defaults.MaxTokens != 128 {
		t.Fatalf("reloaded defaults %+v", b.data.Defaults)
	}
	if b.conversation("amb").Config.Model != "shared-model" || b.conversation("amb").Config.ConnectionID != "conn-north" || b.conversation("amb").Config.MaxTokens != 64 {
		t.Fatalf("reloaded amb %+v", b.conversation("amb").Config)
	}
	if b.conversation("east").Config.Model != "org/east-model" || b.conversation("bare").Config.Model != "south-only" {
		t.Fatal("reloaded model strings changed")
	}
	if b.resolvedKey(b.conversation("amb").Config) != "north-key" {
		t.Fatalf("reloaded key %q", b.resolvedKey(b.conversation("amb").Config))
	}
}

func ambiguousLogs(logs []string) []string {
	var out []string
	for _, line := range logs {
		if strings.Contains(line, "ambiguous model reference") {
			out = append(out, line)
		}
	}
	return out
}

func writeLegacyChat(t *testing.T, path, id, title, settings string) {
	t.Helper()
	body := fmt.Sprintf("{\"type\":\"chat\",\"schema\":5,\"id\":%q,\"title\":%q,\"createdAt\":\"2026-10-20T12:00:00Z\",\"settings\":%s}\n{\"type\":\"message\",\"id\":\"m\",\"role\":\"user\",\"content\":\"hi\",\"createdAt\":\"2026-10-20T12:00:00Z\",\"status\":\"complete\"}\n", id, title, settings)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func customConn(id, name, base string, models ...string) Connection {
	var list []ProviderModel
	for _, m := range models {
		list = append(list, ProviderModel{ID: m, Enabled: true})
	}
	return Connection{
		ID: id, Name: name, Provider: "Custom", Custom: true, BaseURL: base,
		Models: list, ModelsSet: true,
	}
}

func assertHeaderIdentity(t *testing.T, path, connID, model string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	settings := headerSettings(t, raw)
	var hdr map[string]any
	if err := json.Unmarshal(settings, &hdr); err != nil {
		t.Fatalf("settings %s: %v", settings, err)
	}
	if hdr["connectionId"] != connID || hdr["model"] != model {
		t.Fatalf("header identity %+v, want connection %s model %s", hdr, connID, model)
	}
}

func replaceChatSettings(t *testing.T, path, settings string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	nl := bytes.IndexByte(raw, '\n')
	if nl < 0 {
		t.Fatal("chat file has no header line")
	}
	var header chatHeader
	if err := json.Unmarshal(raw[:nl], &header); err != nil {
		t.Fatal(err)
	}
	header.Settings = json.RawMessage(settings)
	line, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	next := append(append(line, '\n'), raw[nl+1:]...)
	if err := os.WriteFile(path, next, 0o600); err != nil {
		t.Fatal(err)
	}
}

type menuRow struct {
	label       string
	checked, on bool
	separator   bool
	hot         bool
	rect        ui.Rect
}

// modelMenuOpen reports whether the composer model popup is on screen.
func modelMenuOpen(tt *ui.Tester) bool {
	_, ok := tt.Find("Model")
	return ok
}

// closeModelMenu dismisses the composer model popup.
func closeModelMenu(t *testing.T, tt *ui.Tester) {
	t.Helper()
	if !modelMenuOpen(tt) {
		return
	}
	tt.Key(0, ui.KeyEscape)
	if modelMenuOpen(tt) {
		tt.ClickAt(8, 8)
	}
	if modelMenuOpen(tt) {
		t.Fatal("model menu stayed open")
	}
}

// modelMenuLabels is the open popup in order, "-" for a separator.
func modelMenuLabels(t *testing.T, tt *ui.Tester) []string {
	t.Helper()
	rows := menuRows(t, tt)
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.separator {
			out = append(out, "-")
			continue
		}
		out = append(out, r.label)
	}
	return out
}

// menuRows is the open popup in order. A provider header is a disabled
// unchecked row. Model rows under it are enabled. on is whether the row
// can be chosen.
func menuRows(t *testing.T, tt *ui.Tester) []menuRow {
	t.Helper()
	if !modelMenuOpen(tt) {
		t.Fatal("menu is not open")
	}
	var out []menuRow
	for _, lab := range menuLabels(t, tt) {
		kind, provider, model, enabled, checked, hot, ok := parseModelMenuMark(lab.text)
		if !ok {
			continue
		}
		row := menuRow{checked: checked, on: enabled, hot: hot, rect: lab.r}
		switch kind {
		case "sep":
			row.separator = true
		case "header":
			row.label = provider
		case "unavailable":
			row.label = model + " (unavailable)"
		default:
			row.label = model
		}
		out = append(out, row)
	}
	if len(out) == 0 {
		t.Fatal("model menu has no rows")
	}
	return out
}

func parseModelMenuMark(s string) (kind, provider, model string, enabled, checked, hot, ok bool) {
	parts := strings.Split(s, "\x1f")
	if len(parts) != 7 || parts[0] != "model-menu-row" {
		return "", "", "", false, false, false, false
	}
	return parts[1], parts[2], parts[3], parts[4] == "1", parts[5] == "1", parts[6] == "1", true
}

// modelChecked reports the check on the model row under a provider header.
func modelChecked(rows []menuRow, provider, model string) (checked, ok bool) {
	group := ""
	for _, r := range rows {
		if r.separator {
			continue
		}
		if !r.on && !r.checked && !strings.HasSuffix(r.label, " (unavailable)") {
			group = r.label
			continue
		}
		if group == provider && r.label == model {
			return r.checked, true
		}
	}
	return false, false
}

// unavailableChecked is the disabled checked row for a stored model that
// is not in the enabled list.
func unavailableChecked(rows []menuRow, model string) bool {
	want := model + " (unavailable)"
	for _, r := range rows {
		if r.label == want && r.checked && !r.on {
			return true
		}
	}
	return false
}

// headerRow is a disabled unchecked provider name used as a group header.
func headerRow(rows []menuRow, provider string) bool {
	for _, r := range rows {
		if r.label == provider && !r.on && !r.checked && !r.separator {
			return true
		}
	}
	return false
}

// chooseGroupedModel chooses the model row under that provider header.
// Two providers can list the same model id; the first label match is not enough.
func chooseGroupedModel(t *testing.T, tt *ui.Tester, provider, model string) {
	t.Helper()
	var hit ui.Rect
	found := false
	for _, lab := range menuLabels(t, tt) {
		kind, prov, id, enabled, _, _, ok := parseModelMenuMark(lab.text)
		if !ok || kind != "model" || !enabled || prov != provider || id != model {
			continue
		}
		hit = lab.r
		found = true
		break
	}
	if !found {
		t.Fatalf("menu has no %q under %q: %v", model, provider, modelMenuLabels(t, tt))
	}
	if hit.W < 1 || hit.H < 1 {
		t.Fatalf("menu row %s / %s is not on screen (%+v)", provider, model, hit)
	}
	tt.ClickAt(hit.X+hit.W/2, hit.Y+hit.H/2)
	for i := 0; i < 8 && tt.FramePending(); i++ {
		tt.Frame()
	}
}

type menuLabel struct {
	text string
	r    ui.Rect
}

func menuLabels(t *testing.T, tt *ui.Tester) []menuLabel {
	t.Helper()
	rt := reflectField(reflect.ValueOf(tt).Elem(), "rt")
	if rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	labels := reflectField(rt, "labels")
	out := make([]menuLabel, 0, labels.Len())
	for i := 0; i < labels.Len(); i++ {
		n := labels.Index(i)
		r := n.FieldByName("r")
		out = append(out, menuLabel{
			text: n.FieldByName("text").String(),
			r: ui.Rect{
				X: float32(r.FieldByName("X").Float()),
				Y: float32(r.FieldByName("Y").Float()),
				W: float32(r.FieldByName("W").Float()),
				H: float32(r.FieldByName("H").Float()),
			},
		})
	}
	return out
}
