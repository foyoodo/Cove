package main

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestProviderDotFollowsKindNotName(t *testing.T) {
	a := newTestApp(t)
	models := []ProviderModel{{ID: "gpt-4o", Enabled: true}}
	a.data.Connections = []Connection{
		{ID: "openai-2", Name: "OpenAI 2", Provider: "OpenAI", Models: models, ModelsSet: true},
		{ID: "named-openai", Name: "OpenAI", Provider: "Custom", Custom: true, BaseURL: "https://relay.example/v1", Models: append([]ProviderModel(nil), models...), ModelsSet: true},
	}
	if err := a.keys.Set("openai-2", "sk-work"); err != nil {
		t.Fatal(err)
	}
	if err := a.keys.Set("named-openai", "sk-relay"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	work := Config{Provider: "OpenAI 2", Model: "gpt-4o", ConnectionID: "openai-2", BaseURL: officialBase("OpenAI")}
	relay := Config{Provider: "OpenAI", Model: "gpt-4o", ConnectionID: "named-openai", BaseURL: "https://relay.example/v1"}
	a.data.Defaults = work
	a.data.Conversations = []Conversation{
		{ID: "work", Title: "Work chat", CreatedAt: now, UpdatedAt: now, Loaded: true, Config: work, Messages: []Message{{ID: "m", Role: roleUser, Content: "hi", Done: true, CreatedAt: now}}},
		{ID: "relay", Title: "Relay chat", CreatedAt: now, UpdatedAt: now.Add(-time.Minute), Loaded: true, Config: relay, Messages: []Message{{ID: "m2", Role: roleUser, Content: "hi", Done: true, CreatedAt: now}}},
	}
	a.data.ActiveID = "work"
	tt := openTester(a)
	if a.connectionColor(&a.data.Connections[0]) != a.look.ProviderOpenai {
		t.Fatal("renamed OpenAI preset lost the OpenAI dot")
	}
	if a.connectionColor(&a.data.Connections[1]) != a.look.ProviderCustom {
		t.Fatal("custom provider named OpenAI used the OpenAI dot")
	}
	if a.configColor(work) != a.look.ProviderOpenai || a.configColor(relay) != a.look.ProviderCustom {
		t.Fatalf("chat dots work %v relay %v", a.configColor(work), a.configColor(relay))
	}
	img := tt.Image()
	assertKindDot(t, img, mustFind(t, tt, "OpenAI 2"), a.look.ProviderOpenai, a.look.ProviderCustom)
	assertKindDot(t, img, mustFind(t, tt, "OpenAI 2 · gpt-4o · api.openai.com"), a.look.ProviderOpenai, a.look.ProviderCustom)
	assertKindDot(t, img, mustFind(t, tt, "Work chat"), a.look.ProviderOpenai, a.look.ProviderCustom)

	a.data.ActiveID = "relay"
	tt.Frame()
	img = tt.Image()
	assertKindDot(t, img, mustFind(t, tt, "OpenAI"), a.look.ProviderCustom, a.look.ProviderOpenai)
	assertKindDot(t, img, mustFind(t, tt, "OpenAI · gpt-4o · relay.example"), a.look.ProviderCustom, a.look.ProviderOpenai)
	assertKindDot(t, img, mustFind(t, tt, "Relay chat"), a.look.ProviderCustom, a.look.ProviderOpenai)

	a.settingsOpen = true
	a.settingsSection = "models"
	a.settingsPage = settingsList
	tt.Frame()
	img = tt.Image()
	assertKindDot(t, img, mustFind(t, tt, "OpenAI 2"), a.look.ProviderOpenai, a.look.ProviderCustom)
	assertKindDot(t, img, mustFind(t, tt, "OpenAI"), a.look.ProviderCustom, a.look.ProviderOpenai)
}

func TestCustomDraftTestDoesNotShowSaved(t *testing.T) {
	a := newTestApp(t)
	a.beginCustom()
	a.provEdit.name = "Together AI"
	a.provEdit.baseURL = "https://api.together.xyz/v1"
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	a.finishProviderTest("Together AI", "", []string{"alpha", "beta"}, 200, false, nil, 12, now)
	if !a.appearanceSavedUntil.IsZero() {
		t.Fatal("draft test showed Saved")
	}
	if len(a.data.Connections) != 0 {
		t.Fatalf("draft was written %+v", a.data.Connections)
	}
	if a.provEdit.testKind != "ok" || !a.provEdit.listed || len(a.provEdit.models) != 2 || !a.provEdit.models[0].Enabled {
		t.Fatalf("draft result %+v models %+v", a.provEdit.testKind, a.provEdit.models)
	}
	a.settingsOpen = true
	a.settingsSection = "models"
	tt := openTester(a)
	if tt.HasText("✓ Saved") {
		t.Fatal("draft page drew Saved")
	}
	if !tt.HasText("Fetched models are on. Switch off any you don’t want in the model menu.") {
		t.Fatalf("footnote %q", tt.Texts())
	}
	if !tt.HasText("How Cove talks to this server") || !tt.HasText("Optional · Stored in the macOS Keychain") {
		t.Fatalf("custom form copy %q", tt.Texts())
	}
	if tt.HasText("Anthropic-compatible") {
		t.Fatal("API format still shows both choices at once")
	}
	if err := tt.Click("OpenAI-compatible"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Anthropic-compatible") {
		t.Fatal("API format popup did not open")
	}
	format := mustFind(t, tt, "How Cove talks to this server")
	base := mustFind(t, tt, "Must serve /chat/completions")
	key := mustFind(t, tt, "Optional · Stored in the macOS Keychain")
	name := mustFind(t, tt, "Shown in the model menu")
	if !(name.Y < format.Y && format.Y < base.Y && base.Y < key.Y) {
		t.Fatalf("field order name %v format %v base %v key %v", name, format, base, key)
	}
	count := mustFind(t, tt, "2 of 2 shown")
	refresh := mustFind(t, tt, "Refresh")
	models := mustFind(t, tt, "Models")
	if count.X < models.X+models.W || refresh.X < count.X+count.W-2 {
		t.Fatalf("count not beside Refresh: models %v count %v refresh %v", models, count, refresh)
	}
	add := mustFind(t, tt, "Add model…")
	card := mustFind(t, tt, "settings card")
	if add.X > card.X+card.W/2 {
		t.Fatalf("Add model is not left-aligned: %v card %v", add, card)
	}

	saved := newTestApp(t)
	connectBuiltin(saved, "OpenAI", "sk-openai")
	saved.openConnection("conn-openai", now)
	saved.finishProviderTest("OpenAI", "conn-openai", []string{"gpt-4o"}, 200, false, nil, 8, now)
	if saved.appearanceSavedUntil.IsZero() {
		t.Fatal("a saved provider test did not confirm the save")
	}
}

func TestProviderEmptyIsCentered(t *testing.T) {
	a := newTestApp(t)
	a.settingsOpen = true
	a.settingsSection = "models"
	a.settingsPage = settingsList
	tt := openTester(a)
	card := mustFind(t, tt, "settings card")
	title := mustFind(t, tt, "No providers yet")
	mid := title.Y + title.H/2
	cardMid := card.Y + card.H/2
	if abs32(mid-cardMid) > 48 {
		t.Fatalf("empty state not centered: title %v card %v", title, card)
	}
}

func TestDeletedNoteHugsContent(t *testing.T) {
	a := newTestApp(t)
	connectBuiltin(a, "OpenAI", "sk-openai")
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	note := deletedProviderNote("OpenAI 2", "gpt-4o-mini")
	cfg := Config{Provider: "OpenAI", Model: "gpt-4o-mini", ConnectionID: "conn-openai", BaseURL: officialBase("OpenAI")}
	a.data.Conversations = []Conversation{{
		ID: "c", Title: "Kept", CreatedAt: now, UpdatedAt: now, Loaded: true,
		Config: cfg, SwitchNote: note,
		Messages: []Message{{ID: "m", Role: roleUser, Content: "hi", Done: true, CreatedAt: now}},
	}}
	a.data.ActiveID = "c"
	tt := openTester(a)
	pill := mustFind(t, tt, "switch note")
	comp := mustFind(t, tt, "composer")
	if pill.W > comp.W-80 {
		t.Fatalf("note spans the composer: note %v composer %v", pill, comp)
	}
	if abs32((pill.X+pill.W/2)-(comp.X+comp.W/2)) > 12 {
		t.Fatalf("note is not centered: note %v composer %v", pill, comp)
	}
}

func TestOllamaHostIsLocalhost(t *testing.T) {
	if displayHost(officialBase("Ollama")) != "localhost:11434" {
		t.Fatalf("official host %q from %q", displayHost(officialBase("Ollama")), officialBase("Ollama"))
	}
	if displayHost("http://127.0.0.1:11434/v1") != "localhost:11434" {
		t.Fatal("stored loopback host was not rewritten")
	}
	a := newTestApp(t)
	a.settingsOpen = true
	a.settingsSection = "models"
	a.settingsPage = settingsAdd
	tt := openTester(a)
	if !tt.HasText("localhost:11434") {
		t.Fatalf("preset list %q", tt.Texts())
	}
	for _, s := range tt.Texts() {
		if strings.Contains(s, "127.0.0.1") {
			t.Fatalf("preset list still shows %q", s)
		}
	}
	a.settingsOpen = false
	a.settingsPage = settingsList
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	a.data.Connections = []Connection{{ID: "ollama", Name: "Ollama", Provider: "Ollama", BaseURL: "http://127.0.0.1:11434/v1"}}
	cfg := Config{Provider: "Ollama", Model: "llama3.2", ConnectionID: "ollama", BaseURL: "http://127.0.0.1:11434/v1"}
	a.data.Conversations = []Conversation{{
		ID: "c", Title: "Local", CreatedAt: now, UpdatedAt: now, Loaded: true, Config: cfg,
		Messages: []Message{{ID: "m", Role: roleUser, Content: "hi", Done: true, CreatedAt: now}},
	}}
	a.data.ActiveID = "c"
	tt = openTester(a)
	if !tt.HasText("llama3.2 · localhost:11434") {
		t.Fatalf("subtitle %q", tt.Texts())
	}
}

func TestNoProviderDotIsTertiary(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a := noProviderDotsApp(t)
		tt := openTester(a)
		if dark {
			tt.SetDark(true)
		}
		got := a.configColor(a.active().Config)
		if got != a.look.TextTertiary {
			t.Fatalf("dark %v dot %v, want text/tertiary %v", dark, got, a.look.TextTertiary)
		}
		if got == a.look.ProviderCustom {
			t.Fatalf("dark %v no-provider dot used ProviderCustom", dark)
		}
		img := tt.Image()
		rects := textRects(t, tt, "No provider yet")
		if len(rects) < 2 {
			t.Fatalf("dark %v want title and empty subtitles, got %v", dark, tt.Texts())
		}
		for _, r := range rects {
			assertKindDot(t, img, r, a.look.TextTertiary, a.look.ProviderCustom)
		}
		assertKindDot(t, img, mustFind(t, tt, "No provider chat"), a.look.TextTertiary, a.look.ProviderCustom)
		model := mustFind(t, tt, "No model")
		if dotLeft(img, model, a.look.ProviderCustom) || dotLeft(img, model, a.look.TextTertiary) {
			t.Fatalf("dark %v composer No model drew a dot", dark)
		}
	}
}

func TestFinalR3NoProviderCrop(t *testing.T) {
	dir := "/cursor/stores/self/artifacts/final-r3"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{false, true} {
		mode := modeName(dark)
		writePNG(t, filepath.Join(dir, "no-provider-"+mode+".png"), crop2x(t, dark, noProviderDotsApp(t)))
	}
}

func noProviderDotsApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	a.data.Connections = nil
	a.data.Defaults = Config{}
	a.data.Conversations = []Conversation{{
		ID: "c", Title: "No provider chat", CreatedAt: now, UpdatedAt: now, Loaded: true,
		Config: Config{},
	}}
	a.data.ActiveID = "c"
	return a
}

func TestFinalR2Crops(t *testing.T) {
	dir := "/cursor/stores/self/artifacts/final-r2"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{false, true} {
		mode := modeName(dark)
		writePNG(t, filepath.Join(dir, "openai2-dots-"+mode+".png"), crop2x(t, dark, openai2DotsApp(t)))
		writePNG(t, filepath.Join(dir, "custom-empty-"+mode+".png"), crop2x(t, dark, customFormApp(t, false)))
		writePNG(t, filepath.Join(dir, "custom-tested-"+mode+".png"), crop2x(t, dark, customFormApp(t, true)))
		writePNG(t, filepath.Join(dir, "providers-empty-"+mode+".png"), crop2x(t, dark, emptyProvidersApp(t)))
		writePNG(t, filepath.Join(dir, "deleted-note-"+mode+".png"), crop2x(t, dark, deletedNoteApp(t)))
		writePNG(t, filepath.Join(dir, "providers-list-"+mode+".png"), crop2x(t, dark, providersListApp(t)))
		writePNG(t, filepath.Join(dir, "add-provider-"+mode+".png"), crop2x(t, dark, addProviderApp(t)))
		writePNG(t, filepath.Join(dir, "delete-confirm-"+mode+".png"), crop2x(t, dark, deleteConfirmApp(t)))
		writePNG(t, filepath.Join(dir, "no-providers-"+mode+".png"), crop2x(t, dark, noProvidersChat(t)))
		writePNG(t, filepath.Join(dir, "preset-detail-"+mode+".png"), crop2x(t, dark, presetDetailApp(t)))
	}
}

func crop2x(t *testing.T, dark bool, a *App) image.Image {
	t.Helper()
	tt := ui.NewTester(a.view, 1120, 760)
	if dark {
		tt.SetDark(true)
	}
	tt.SetScale(2)
	return tt.Image()
}

func openai2DotsApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	models := []ProviderModel{{ID: "gpt-4o", Enabled: true}}
	a.data.Connections = []Connection{
		{ID: "openai-2", Name: "OpenAI 2", Provider: "OpenAI", Models: models, ModelsSet: true},
		{ID: "named-openai", Name: "OpenAI", Provider: "Custom", Custom: true, BaseURL: "https://relay.example/v1", Models: append([]ProviderModel(nil), models...), ModelsSet: true},
	}
	if err := a.keys.Set("openai-2", "sk-work"); err != nil {
		t.Fatal(err)
	}
	if err := a.keys.Set("named-openai", "sk-relay"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	work := Config{Provider: "OpenAI 2", Model: "gpt-4o", ConnectionID: "openai-2", BaseURL: officialBase("OpenAI")}
	relay := Config{Provider: "OpenAI", Model: "gpt-4o", ConnectionID: "named-openai", BaseURL: "https://relay.example/v1"}
	a.data.Conversations = []Conversation{
		{ID: "work", Title: "Work chat", CreatedAt: now, UpdatedAt: now, Loaded: true, Config: work, Messages: []Message{{ID: "m", Role: roleUser, Content: "Ship the picker.", Done: true, CreatedAt: now}}},
		{ID: "relay", Title: "Relay chat", CreatedAt: now, UpdatedAt: now.Add(-time.Minute), Loaded: true, Config: relay, Messages: []Message{{ID: "m2", Role: roleUser, Content: "hi", Done: true, CreatedAt: now}}},
	}
	a.data.ActiveID = "work"
	return a
}

func customFormApp(t *testing.T, tested bool) *App {
	t.Helper()
	a := newTestApp(t)
	a.beginCustom()
	a.provEdit.name = "Together AI"
	a.provEdit.baseURL = "https://api.together.xyz/v1"
	if tested {
		a.finishProviderTest("Together AI", "", []string{"alpha", "beta"}, 200, false, nil, 312, time.Now())
	}
	a.settingsOpen = true
	a.settingsSection = "models"
	return a
}

func emptyProvidersApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	a.settingsOpen = true
	a.settingsSection = "models"
	a.settingsPage = settingsList
	return a
}

func deletedNoteApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	connectBuiltin(a, "OpenAI", "sk-openai")
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	cfg := Config{Provider: "OpenAI", Model: "gpt-4o-mini", ConnectionID: "conn-openai", BaseURL: officialBase("OpenAI")}
	a.data.Conversations = []Conversation{{
		ID: "c", Title: "Kept", CreatedAt: now, UpdatedAt: now, Loaded: true,
		Config: cfg, SwitchNote: deletedProviderNote("OpenAI 2", "gpt-4o-mini"),
		Messages: []Message{{ID: "m", Role: roleUser, Content: "hi", Done: true, CreatedAt: now}},
	}}
	a.data.ActiveID = "c"
	return a
}

func providersListApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	models := []ProviderModel{{ID: "gpt-4o", Enabled: true}, {ID: "gpt-4o-mini", Enabled: true}}
	a.data.Connections = []Connection{
		{ID: "openai", Name: "OpenAI", Provider: "OpenAI", Models: models, ModelsSet: true, Probe: probeOK},
		{ID: "openai-2", Name: "OpenAI 2", Provider: "OpenAI", Models: append([]ProviderModel(nil), models...), ModelsSet: true},
	}
	if err := a.keys.Set("openai", "sk-work"); err != nil {
		t.Fatal(err)
	}
	if err := a.keys.Set("openai-2", "sk-personal"); err != nil {
		t.Fatal(err)
	}
	a.settingsOpen = true
	a.settingsSection = "models"
	a.settingsPage = settingsList
	return a
}

func addProviderApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	a.settingsOpen = true
	a.settingsSection = "models"
	a.settingsPage = settingsAdd
	return a
}

func deleteConfirmApp(t *testing.T) *App {
	t.Helper()
	a := providersListApp(t)
	a.openConnection("openai-2", time.Now())
	a.confirmKind = confirmDeleteProvider
	a.deleteID = "openai-2"
	return a
}

func noProvidersChat(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	a.data.Connections = nil
	a.data.Defaults = Config{}
	a.data.Conversations = []Conversation{{
		ID: "c", Title: newChatTitle, CreatedAt: now, UpdatedAt: now, Loaded: true,
		Config: Config{}, Draft: "hello",
	}}
	a.data.ActiveID = "c"
	a.addProviderNote = "c"
	return a
}

func presetDetailApp(t *testing.T) *App {
	t.Helper()
	a := providersListApp(t)
	a.openConnection("openai", time.Now())
	return a
}

func assertKindDot(t *testing.T, img image.Image, r ui.Rect, want, other ui.Color) {
	t.Helper()
	y0 := int(r.Y + r.H/2)
	for _, y := range []int{y0 - 2, y0 - 1, y0, y0 + 1, y0 + 2} {
		for x := int(r.X) - 28; x < int(r.X)+16; x++ {
			if x < 0 || y < 0 {
				continue
			}
			c := img.At(x, y)
			if colorNear(c, want) && !colorNear(c, other) {
				return
			}
		}
	}
	t.Fatalf("no %v dot near %v", want, r)
}

func dotLeft(img image.Image, r ui.Rect, want ui.Color) bool {
	y0 := int(r.Y + r.H/2)
	for _, y := range []int{y0 - 1, y0, y0 + 1} {
		for x := int(r.X) - 18; x < int(r.X)-2; x++ {
			if x < 0 || y < 0 {
				continue
			}
			if colorNear(img.At(x, y), want) {
				return true
			}
		}
	}
	return false
}

func textRects(t *testing.T, tt *ui.Tester, text string) []ui.Rect {
	t.Helper()
	rt := reflectField(reflect.ValueOf(tt).Elem(), "rt")
	if rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	labels := reflectField(rt, "labels")
	var out []ui.Rect
	for i := 0; i < labels.Len(); i++ {
		n := labels.Index(i)
		if n.FieldByName("text").String() != text {
			continue
		}
		r := n.FieldByName("r")
		out = append(out, ui.Rect{
			X: float32(r.FieldByName("X").Float()),
			Y: float32(r.FieldByName("Y").Float()),
			W: float32(r.FieldByName("W").Float()),
			H: float32(r.FieldByName("H").Float()),
		})
	}
	return out
}

func colorNear(c color.Color, want ui.Color) bool {
	r, g, b, a := c.RGBA()
	if a < 200*257 {
		return false
	}
	d := absInt(int(r)-int(want.R)*257) + absInt(int(g)-int(want.G)*257) + absInt(int(b)-int(want.B)*257)
	return d < 48*257
}
