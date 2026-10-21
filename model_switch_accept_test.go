package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestModelMenuOpensOnClick is acceptance case (0): a click opens the
// composer model menu. The dot, the model name, and the chevron are all
// the button. An empty provider list still opens, with one item, so a
// menu that collected nothing cannot swallow the click.
func TestModelMenuOpensOnClick(t *testing.T) {
	a := modelSwitchApp(t, t.TempDir())
	tt := openTester(a)
	clickOpensModelMenu(t, tt, "Model alpha", "alpha")
	menu := tt.Menu()
	if !menuHas(menu, "North") || !menuHas(menu, "alpha") || !menuHas(menu, "South") || !menuHas(menu, "beta") {
		t.Fatalf("menu %v", menu)
	}
	for _, label := range menu {
		if strings.Contains(label, " · ") {
			t.Fatalf("menu item still joins provider and model: %q in %v", label, menu)
		}
	}
	tt.CloseMenu()

	b := newTestApp(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	b.data.Connections = nil
	b.data.Defaults.Model = ""
	b.data.Defaults.Provider = ""
	b.data.Defaults.ConnectionID = ""
	b.data.Defaults.BaseURL = ""
	b.data.Conversations = []Conversation{{
		ID: "c", Title: "Empty", CreatedAt: now, UpdatedAt: now,
		Config:   Config{},
		Messages: []Message{{ID: "m", Role: roleUser, Content: "hi", Done: true, CreatedAt: now}},
	}}
	b.data.ActiveID = "c"
	tb := openTester(b)
	clickOpensModelMenu(t, tb, "Model No model", "No model")
	if got := tb.Menu(); len(got) != 1 || got[0] != "Add a provider…" {
		t.Fatalf("empty provider menu %v", got)
	}
}

// TestModelMenuCheckFollowsButtonChat dismisses the menu and opens it on
// another chat. The check marks are that chat's, and the choice still
// lands on the chat that opened the menu when the active chat changes
// before the choice frame.
func TestModelMenuCheckFollowsButtonChat(t *testing.T) {
	a := modelSwitchApp(t, t.TempDir())
	a.data.Connections[1].Models = append(a.data.Connections[1].Models, ProviderModel{ID: "gamma", Enabled: true})
	c2 := a.conversation("c2")
	c2.Config.Provider = "South"
	c2.Config.Model = "beta"
	c2.Config.ConnectionID = "conn-south"
	c2.Config.BaseURL = a.connByID("conn-south").BaseURL
	sentinel := a.data.Defaults
	sentinel.Model = "default-model"
	sentinel.ConnectionID = "conn-default"
	sentinel.Provider = "Default"
	sentinel.BaseURL = "https://default.example/v1"
	a.data.Defaults = sentinel
	a.save()

	tt := openTester(a)
	if err := tt.Click("Model alpha"); err != nil {
		t.Fatal(err)
	}
	if tt.Menu() == nil {
		t.Fatal("menu did not open")
	}
	rows := menuRows(t, tt)
	if on, ok := modelChecked(rows, "North", "alpha"); !ok || !on {
		t.Fatalf("c1 checks %+v", rows)
	}
	if on, ok := modelChecked(rows, "South", "beta"); !ok || on {
		t.Fatalf("c1 checked south %+v", rows)
	}
	tt.CloseMenu()

	a.selectChat("c2")
	tt.Frame()
	if err := tt.Click("Model beta"); err != nil {
		t.Fatal(err)
	}
	rows = menuRows(t, tt)
	if on, ok := modelChecked(rows, "South", "beta"); !ok || !on {
		t.Fatalf("c2 beta %+v", rows)
	}
	if on, ok := modelChecked(rows, "North", "alpha"); !ok || on {
		t.Fatalf("c2 checked north %+v", rows)
	}
	if on, ok := modelChecked(rows, "South", "gamma"); !ok || on {
		t.Fatalf("c2 gamma %+v", rows)
	}
	// The menu belongs to c2. Switching the active chat before the choice
	// is delivered must not retarget c1.
	a.selectChat("c1")
	chooseGroupedModel(t, tt, "South", "gamma")
	if got := a.conversation("c2").Config; got.Model != "gamma" || got.ConnectionID != "conn-south" || got.Provider != "South" {
		t.Fatalf("choice missed the chat that opened the menu %+v", got)
	}
	if got := a.conversation("c1").Config; got.Model != "alpha" || got.ConnectionID != "conn-north" {
		t.Fatalf("choice hit the chat on screen %+v", got)
	}
	if a.data.Defaults != sentinel {
		t.Fatalf("global default changed %+v", a.data.Defaults)
	}
}

// TestModelChoiceKeepsGlobalDefault chooses a model from the composer menu
// and checks the chip, the next request, chat switching, and a restart.
// The global default and the other chat stay put.
func TestModelChoiceKeepsGlobalDefault(t *testing.T) {
	var gotURL, auth, body string
	north := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.Host + r.URL.Path
		auth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"n\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer north.Close()
	south := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.Host + r.URL.Path
		auth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"s\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer south.Close()

	dir := t.TempDir()
	a := modelSwitchApp(t, dir)
	a.data.Connections[0].BaseURL = north.URL + "/v1"
	a.data.Connections[1].BaseURL = south.URL + "/v1"
	a.data.Connections[1].Models = append(a.data.Connections[1].Models, ProviderModel{ID: "gamma", Enabled: true})
	c1 := a.conversation("c1")
	c1.Config.BaseURL = north.URL + "/v1"
	c2 := a.conversation("c2")
	c2.Config = Config{
		Provider: "South", Model: "beta", BaseURL: south.URL + "/v1",
		ConnectionID: "conn-south", SystemPrompt: DefaultConfig().SystemPrompt,
	}
	sentinel := a.data.Defaults
	sentinel.Model = "default-model"
	sentinel.ConnectionID = "conn-default"
	sentinel.BaseURL = "https://default.example/v1"
	a.data.Defaults = sentinel
	a.save()

	tt := openTester(a)
	if _, ok := tt.Find("alpha"); !ok {
		t.Fatalf("chip %q", tt.Texts())
	}
	if err := tt.Click("Model alpha"); err != nil {
		t.Fatal(err)
	}
	if tt.Menu() == nil {
		t.Fatal("menu did not open")
	}
	chooseGroupedModel(t, tt, "South", "gamma")
	if _, ok := tt.Find("gamma"); !ok {
		t.Fatalf("chip did not update: %q", tt.Texts())
	}
	if _, ok := tt.Find("alpha"); ok {
		t.Fatal("old model still on the chip")
	}
	got := a.conversation("c1").Config
	if got.ConnectionID != "conn-south" || got.Model != "gamma" || got.Provider != "South" || got.BaseURL != south.URL+"/v1" {
		t.Fatalf("c1 %+v", got)
	}
	if other := a.conversation("c2").Config; other.Model != "beta" || other.ConnectionID != "conn-south" {
		t.Fatalf("other chat %+v", other)
	}
	if a.data.Defaults != sentinel {
		t.Fatalf("global default %+v", a.data.Defaults)
	}
	assertHeaderIdentity(t, filepath.Join(dir, chatsDir, "c1.jsonl"), "conn-south", "gamma")
	assertHeaderIdentity(t, filepath.Join(dir, chatsDir, "c2.jsonl"), "conn-south", "beta")

	a.selectChat("c2")
	tt.Frame()
	if _, ok := tt.Find("beta"); !ok {
		t.Fatalf("other chat chip %q", tt.Texts())
	}
	if _, ok := tt.Find("gamma"); ok {
		t.Fatal("choice showed on the other chat")
	}
	a.selectChat("c1")
	tt.Frame()
	if _, ok := tt.Find("gamma"); !ok {
		t.Fatalf("chip lost across chat switch %q", tt.Texts())
	}

	focusDraft(t, tt)
	tt.Type("next")
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	host := strings.TrimPrefix(strings.TrimPrefix(south.URL, "https://"), "http://")
	if !strings.Contains(gotURL, host) || !strings.Contains(gotURL, "/chat/completions") {
		t.Fatalf("request url %q, want host %s", gotURL, host)
	}
	if auth != "Bearer south-key" {
		t.Fatalf("auth %q", auth)
	}
	if !strings.Contains(body, `"model":"gamma"`) || strings.Contains(body, `"model":"alpha"`) || strings.Contains(body, `"model":"beta"`) || strings.Contains(body, `"model":"default-model"`) {
		t.Fatalf("body %s", body)
	}
	if a.data.Defaults != sentinel {
		t.Fatal("send changed the global default")
	}

	b := openApp(filepath.Join(dir, "conversations.json"), a.keys)
	b.instant = true
	if b.data.Defaults != sentinel {
		t.Fatalf("restart defaults %+v", b.data.Defaults)
	}
	b.ensureChat("c1")
	b.ensureChat("c2")
	if got := b.conversation("c1").Config; got.Model != "gamma" || got.ConnectionID != "conn-south" || got.BaseURL != south.URL+"/v1" {
		t.Fatalf("restart c1 %+v", got)
	}
	if got := b.conversation("c2").Config; got.Model != "beta" || got.ConnectionID != "conn-south" {
		t.Fatalf("restart c2 %+v", got)
	}
	tb := openTester(b)
	if _, ok := tb.Find("gamma"); !ok {
		t.Fatalf("restart chip %q", tb.Texts())
	}
}

// TestCatalogIDMatchesHeader keeps the menu, the JSONL header, and the
// request body on the catalog id. A schema 5 string list, a disabled
// model, a renamed provider, and a list edited after the menu has opened
// all stay in that id space. The short name is only what the chip draws.
func TestCatalogIDMatchesHeader(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	var legacy Connection
	raw := []byte(`{"id":"conn-or","name":"OpenRouter","provider":"OpenRouter","models":["openai/gpt-4o","openai/gpt-4o-mini"]}`)
	if err := json.Unmarshal(raw, &legacy); err != nil {
		t.Fatal(err)
	}
	if !legacy.ModelsSet || len(legacy.Models) != 2 || !legacy.Models[0].Enabled || legacy.Models[0].ID != "openai/gpt-4o" || legacy.Models[1].ID != "openai/gpt-4o-mini" {
		t.Fatalf("schema 5 list %+v", legacy)
	}
	legacy.BaseURL = srv.URL + "/v1"
	legacy.Models = append(legacy.Models, ProviderModel{ID: "openai/o3", Enabled: false})
	bare := customConn("conn-bare", "Bare", srv.URL+"/v1", "gpt-4o")

	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.json")
	a := openApp(path, newMemKeys())
	a.instant = true
	a.data.Connections = []Connection{legacy, bare}
	if err := a.keys.Set("conn-or", "or-key"); err != nil {
		t.Fatal(err)
	}
	if err := a.keys.Set("conn-bare", "bare-key"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	sentinel := Config{
		Provider: "Default", Model: "default-model", ConnectionID: "conn-default",
		BaseURL: "https://default.example/v1", SystemPrompt: "stay",
	}
	a.data.Defaults = sentinel
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Relay", CreatedAt: now, UpdatedAt: now,
		Config: Config{
			Provider: "OpenRouter", Model: "gpt-4o", BaseURL: legacy.BaseURL,
			ConnectionID: "conn-or", SystemPrompt: "stay",
		},
		Messages: []Message{{ID: "m", Role: roleUser, Content: "hi", Done: true, CreatedAt: now}},
	}}
	a.data.ActiveID = "c1"
	a.save()

	tt := openTester(a)
	if _, ok := tt.Find("Model OpenRouter · gpt-4o"); !ok {
		t.Fatalf("chip %q", tt.Texts())
	}
	if err := tt.Click("Model OpenRouter · gpt-4o"); err != nil {
		t.Fatal(err)
	}
	if tt.Menu() == nil {
		t.Fatal("menu did not open")
	}
	rows := menuRows(t, tt)
	if _, ok := modelChecked(rows, "OpenRouter", "openai/gpt-4o"); !ok {
		t.Fatalf("menu dropped the catalog id %+v", rows)
	}
	if _, ok := modelChecked(rows, "OpenRouter", "openai/gpt-4o-mini"); !ok {
		t.Fatalf("schema 5 id missing %+v", rows)
	}
	if _, ok := modelChecked(rows, "Bare", "gpt-4o"); !ok {
		t.Fatalf("bare id missing %+v", rows)
	}
	if _, collapsed := modelChecked(rows, "OpenRouter", "gpt-4o"); collapsed {
		t.Fatal("menu item used the short name")
	}
	if on, _ := modelChecked(rows, "OpenRouter", "openai/gpt-4o"); on {
		t.Fatalf("catalog row checked for a bare header %+v", rows)
	}
	if on, _ := modelChecked(rows, "Bare", "gpt-4o"); on {
		t.Fatalf("bare row checked %+v", rows)
	}
	if !unavailableChecked(rows, "gpt-4o") {
		t.Fatalf("bare header was aliased onto a catalog row %+v", rows)
	}
	for _, r := range rows {
		if strings.Contains(r.label, "openai/o3") || strings.HasSuffix(r.label, "· o3") {
			t.Fatalf("disabled model listed: %q", r.label)
		}
	}

	chooseGroupedModel(t, tt, "OpenRouter", "openai/gpt-4o")
	if _, ok := tt.Find("Model OpenRouter · gpt-4o"); !ok {
		t.Fatalf("chip after choice %q", tt.Texts())
	}
	cfg := a.conversation("c1").Config
	if cfg.Model != "openai/gpt-4o" || cfg.ConnectionID != "conn-or" || cfg.Provider != "OpenRouter" {
		t.Fatalf("header identity %+v", cfg)
	}
	if a.data.Defaults != sentinel {
		t.Fatalf("global default %+v", a.data.Defaults)
	}
	header := filepath.Join(dir, chatsDir, "c1.jsonl")
	assertHeaderIdentity(t, header, "conn-or", "openai/gpt-4o")

	focusDraft(t, tt)
	tt.Type("qualified")
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `"model":"openai/gpt-4o"`) || strings.Contains(body, `"model":"gpt-4o"`) {
		t.Fatalf("body %s", body)
	}

	for i := range a.data.Connections[0].Models {
		if a.data.Connections[0].Models[i].ID == "openai/gpt-4o-mini" {
			a.data.Connections[0].Models[i].Enabled = false
		}
	}
	tt.Frame()
	if err := tt.Click("Model OpenRouter · gpt-4o"); err != nil {
		t.Fatal(err)
	}
	rows = menuRows(t, tt)
	if _, ok := modelChecked(rows, "OpenRouter", "openai/gpt-4o-mini"); ok {
		t.Fatal("disabled model stayed in a menu built earlier")
	}
	if on, ok := modelChecked(rows, "OpenRouter", "openai/gpt-4o"); !ok || !on {
		t.Fatalf("enabled catalog id missing after the list changed %+v", rows)
	}
	tt.CloseMenu()

	a.connByID("conn-or").Name = "Router West"
	tt.Frame()
	if _, ok := tt.Find("Model Router West · gpt-4o"); !ok {
		t.Fatalf("chip after rename %q", tt.Texts())
	}
	if err := tt.Click("Model Router West · gpt-4o"); err != nil {
		t.Fatal(err)
	}
	rows = menuRows(t, tt)
	if _, ok := modelChecked(rows, "Router West", "openai/gpt-4o"); !ok {
		t.Fatalf("rename left the old menu label %+v", rows)
	}
	if _, ok := modelChecked(rows, "OpenRouter", "openai/gpt-4o"); ok {
		t.Fatal("menu kept the name from before the rename")
	}
	tt.CloseMenu()
	if a.conversation("c1").Config.Model != "openai/gpt-4o" {
		t.Fatalf("rename rewrote the model id %q", a.conversation("c1").Config.Model)
	}
	assertHeaderIdentity(t, header, "conn-or", "openai/gpt-4o")

	for i := range a.data.Connections[0].Models {
		if a.data.Connections[0].Models[i].ID == "openai/gpt-4o-mini" {
			a.data.Connections[0].Models[i].Enabled = true
		}
	}
	tt.Frame()
	if err := tt.Click("Model Router West · gpt-4o"); err != nil {
		t.Fatal(err)
	}
	chooseGroupedModel(t, tt, "Router West", "openai/gpt-4o-mini")
	cfg = a.conversation("c1").Config
	if cfg.Model != "openai/gpt-4o-mini" || cfg.Provider != "Router West" || cfg.ConnectionID != "conn-or" {
		t.Fatalf("choice after rename %+v", cfg)
	}
	if _, ok := tt.Find("gpt-4o-mini"); !ok {
		t.Fatalf("chip %q", tt.Texts())
	}
	if a.data.Defaults != sentinel {
		t.Fatalf("global default %+v", a.data.Defaults)
	}
	assertHeaderIdentity(t, header, "conn-or", "openai/gpt-4o-mini")
}

// TestBuiltinCatalogIDStaysQualified uses a preset whose catalog ids are
// provider-qualified. The menu lists those ids, and a model switched on
// after the first open is in the next menu.
func TestBuiltinCatalogIDStaysQualified(t *testing.T) {
	a := newTestApp(t)
	a.data.Connections = []Connection{{
		ID: "openrouter", Name: "OpenRouter", Provider: "OpenRouter",
	}}
	if err := a.keys.Set("openrouter", "k"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	a.data.Defaults.Model = "default-model"
	a.data.Defaults.ConnectionID = "conn-default"
	a.data.Defaults.Provider = "Default"
	a.data.Conversations = []Conversation{{
		ID: "c", Title: "T", CreatedAt: now, UpdatedAt: now,
		Config: Config{
			Provider: "OpenRouter", Model: "openai/gpt-4o-mini", ConnectionID: "openrouter",
			BaseURL: "https://openrouter.ai/api/v1", SystemPrompt: DefaultConfig().SystemPrompt,
		},
		Messages: []Message{{ID: "m", Role: roleUser, Content: "hi", Done: true, CreatedAt: now}},
	}}
	a.data.ActiveID = "c"
	sentinel := a.data.Defaults

	tt := openTester(a)
	if _, ok := tt.Find("gpt-4o-mini"); !ok {
		t.Fatalf("chip %q", tt.Texts())
	}
	if _, ok := tt.Find("openai/gpt-4o-mini"); ok {
		t.Fatal("chip drew the catalog id")
	}
	if err := tt.Click("Model gpt-4o-mini"); err != nil {
		t.Fatal(err)
	}
	if tt.Menu() == nil {
		t.Fatal("menu did not open")
	}
	rows := menuRows(t, tt)
	if on, ok := modelChecked(rows, "OpenRouter", "openai/gpt-4o-mini"); !ok || !on {
		t.Fatalf("preset catalog id %+v", rows)
	}
	if _, collapsed := modelChecked(rows, "OpenRouter", "gpt-4o-mini"); collapsed {
		t.Fatal("preset menu collapsed the catalog id")
	}
	if _, on := modelChecked(rows, "OpenRouter", "anthropic/claude-sonnet-4.5"); on {
		t.Fatal("disabled preset model was listed")
	}
	tt.CloseMenu()

	c := a.connByID("openrouter")
	c.Models = []ProviderModel{
		{ID: "openai/gpt-4o-mini", Enabled: true},
		{ID: "anthropic/claude-sonnet-4.5", Enabled: true},
	}
	c.ModelsSet = true
	if err := tt.Click("Model gpt-4o-mini"); err != nil {
		t.Fatal(err)
	}
	rows = menuRows(t, tt)
	if _, hasClaude := modelChecked(rows, "OpenRouter", "anthropic/claude-sonnet-4.5"); !hasClaude {
		t.Fatalf("menu kept the list from before the edit %+v", rows)
	}
	if on, ok := modelChecked(rows, "OpenRouter", "openai/gpt-4o-mini"); !ok || !on {
		t.Fatalf("preset row lost its check %+v", rows)
	}
	chooseGroupedModel(t, tt, "OpenRouter", "anthropic/claude-sonnet-4.5")
	if a.active().Config.Model != "anthropic/claude-sonnet-4.5" || a.active().Config.ConnectionID != "openrouter" {
		t.Fatalf("choice %+v", a.active().Config)
	}
	if a.data.Defaults != sentinel {
		t.Fatalf("global default %+v", a.data.Defaults)
	}
}

func clickOpensModelMenu(t *testing.T, tt *ui.Tester, a11y, visible string) {
	t.Helper()
	r, ok := tt.Find(a11y)
	if !ok {
		t.Fatalf("model button %q missing: %q", a11y, tt.Texts())
	}
	text, ok := tt.Find(visible)
	if !ok {
		t.Fatalf("model name %q missing: %q", visible, tt.Texts())
	}
	cx, cy := text.X+text.W/2, text.Y+text.H/2
	if cx < r.X || cx > r.X+r.W || cy < r.Y || cy > r.Y+r.H {
		t.Fatalf("model name %q at %.1f,%.1f is outside the button %+v", visible, cx, cy, r)
	}
	points := [][2]float32{
		{r.X + 4, r.Y + r.H/2},
		{cx, cy},
		{r.X + r.W/2, r.Y + r.H/2},
		{r.X + r.W - 6, r.Y + r.H/2},
	}
	for _, p := range points {
		tt.CloseMenu()
		tt.ClickAt(p[0], p[1])
		if tt.Menu() == nil {
			t.Fatalf("click at %.1f,%.1f on %q did not open the model menu", p[0], p[1], a11y)
		}
	}
}

func menuHas(menu []string, label string) bool {
	for _, item := range menu {
		if item == label {
			return true
		}
	}
	return false
}
