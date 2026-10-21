package main

import (
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestQAMockupCompares lays each approved board beside the screen that
// implements it. Design files are 2×. Light, dark, and the 720×520 boards
// are all included.
func TestQAMockupCompares(t *testing.T) {
	designDir := "/cursor/stores/self/design-v07-r4/qa"
	requireDesign(t, designDir)
	type shot struct {
		file string
		dir  string
		img  image.Image
	}
	var shots []shot
	add := func(file, dir string, img image.Image) {
		shots = append(shots, shot{file, dir, img})
	}
	for _, dark := range []bool{false, true} {
		mode := modeName(dark)
		add("find-bar-"+mode+".png", "find", findWindow(t, dark, 1120, 760, "context", false, false))
		add("find-empty-"+mode+".png", "find", findWindow(t, dark, 1120, 760, "zzqx", false, false))
		add("find-jump-"+mode+".png", "find", findWindow(t, dark, 1120, 760, "context", true, false))
		add("find-flash-"+mode+".png", "find", findWindow(t, dark, 1120, 760, "context", true, true))
		add("find-min-"+mode+".png", "find", findWindow(t, dark, 720, 520, "context", false, false))

		add("providers-optin-empty-"+mode+".png", "providers", providerWindow(t, dark, 780, 620, "empty"))
		add("providers-optin-add-"+mode+".png", "providers", providerWindow(t, dark, 780, 620, "add"))
		add("providers-optin-custom-"+mode+".png", "providers", providerWindow(t, dark, 780, 620, "custom"))
		add("providers-optin-duplicates-"+mode+".png", "providers", providerWindow(t, dark, 1120, 760, "duplicates"))
		add("providers-optin-delete-"+mode+".png", "providers", providerWindow(t, dark, 1120, 760, "delete"))
		add("providers-optin-no-provider-"+mode+".png", "providers", noProviderWindow(t, dark, 1120, 760))
		add("providers-optin-min-"+mode+".png", "providers", providerWindow(t, dark, 720, 520, "min"))
		add("models-providers-"+mode+".png", "providers", providerWindow(t, dark, 1120, 760, "duplicates"))

		add("model-picker-dup-button-"+mode+".png", "model-switch", modelPickerWindow(t, dark, 1120, 760, false))
		add("model-picker-dup-menu-"+mode+".png", "model-switch", modelPickerWindow(t, dark, 1120, 760, true))
		add("model-picker-dup-min-"+mode+".png", "model-switch", modelPickerWindow(t, dark, 720, 520, false))

		add("min-window-"+mode+".png", "sidebar-resize", sidebarMinWindow(t, dark))
	}
	for _, s := range shots {
		dir := filepath.Join("/opt/cursor/artifacts", s.dir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		design := mustPNG(t, filepath.Join(designDir, s.file))
		writePNG(t, filepath.Join(dir, "impl-"+s.file), s.img)
		writePNG(t, filepath.Join(dir, "compare-"+s.file), comparePreserve(design, s.img))
	}
}

func findWindow(t *testing.T, dark bool, w, h int, query string, jump, flash bool) image.Image {
	t.Helper()
	a := revisionChat(t)
	a.menu()
	tt := ui.NewTester(a.view, w, h)
	if dark {
		tt.SetDark(true)
	}
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Frame()
	if query != "" {
		tt.Type(query)
		waitFind(t, a)
		tt.Frame()
	}
	if jump || flash {
		if len(a.finder.hits) == 0 {
			t.Fatal("find query produced no hits")
		}
		k := 0
		if len(a.finder.hits) > 2 {
			k = 2
		}
		a.jumpFind(k, tt.Now())
		tt.Frame()
		for i := 0; i < 8 && !a.finder.settled; i++ {
			tt.Frame()
		}
		if !flash {
			tt.Advance(700 * time.Millisecond)
			tt.Frame()
		}
	}
	return tt.Image()
}

func providerWindow(t *testing.T, dark bool, w, h int, kind string) image.Image {
	t.Helper()
	a := newTestApp(t)
	a.settingsOpen = true
	a.settingsSection = "models"
	switch kind {
	case "empty":
		a.settingsPage = settingsList
	case "add":
		a.settingsPage = settingsAdd
	case "custom":
		a.beginCustom()
		a.provEdit.name = "Together AI"
		a.provEdit.baseURL = "https://api.together.xyz/v1"
		a.provEdit.models = []ProviderModel{
			{ID: "meta-llama/Llama-3.3-70B-Instruct-Turbo", Enabled: true},
			{ID: "deepseek-ai/DeepSeek-V3", Enabled: false, Fresh: true},
		}
		a.provEdit.listed = true
		a.provEdit.testKind = "ok"
		a.provEdit.testLine = "Connected · 2 models · 312 ms"
	case "duplicates", "delete", "min":
		models := []ProviderModel{{ID: "gpt-4o", Enabled: true}, {ID: "gpt-4o-mini", Enabled: true}}
		a.data.Connections = []Connection{
			{ID: "openai", Name: "OpenAI", Provider: "OpenAI", Models: models, ModelsSet: true},
			{ID: "openai-2", Name: "OpenAI 2", Provider: "OpenAI", Models: append([]ProviderModel(nil), models...), ModelsSet: true},
		}
		if err := a.keys.Set("openai", "sk-work"); err != nil {
			t.Fatal(err)
		}
		if err := a.keys.Set("openai-2", "sk-personal"); err != nil {
			t.Fatal(err)
		}
		a.settingsPage = settingsList
		if kind == "delete" {
			a.openConnection("openai-2", time.Now())
			a.confirmKind = confirmDeleteProvider
			a.deleteID = "openai-2"
		}
		if kind == "min" {
			a.data.Connections = append(a.data.Connections, Connection{
				ID: "shot-fireworks", Name: "Fireworks", Provider: "Custom", Custom: true,
				BaseURL: "https://api.fireworks.ai/inference/v1",
				Models: []ProviderModel{{
					ID: "accounts/fireworks/models/llama-v3p1-405b-instruct", Enabled: true,
				}},
				ModelsSet: true,
			})
		}
	}
	tt := ui.NewTester(a.view, w, h)
	if dark {
		tt.SetDark(true)
	}
	tt.Frame()
	return tt.Image()
}

func noProviderWindow(t *testing.T, dark bool, w, h int) image.Image {
	t.Helper()
	a := newTestApp(t)
	now := time.Now()
	a.data.Connections = nil
	a.data.Defaults.Model = ""
	a.data.Defaults.Provider = ""
	a.data.Defaults.ConnectionID = ""
	a.data.Conversations = []Conversation{{
		ID: "c", Title: newChatTitle, CreatedAt: now, UpdatedAt: now, Loaded: true,
		Config: a.data.Defaults, Draft: "hello",
	}}
	a.data.ActiveID = "c"
	a.send("hello")
	tt := ui.NewTester(a.view, w, h)
	if dark {
		tt.SetDark(true)
	}
	tt.Frame()
	return tt.Image()
}

func modelPickerWindow(t *testing.T, dark bool, w, h int, open bool) image.Image {
	t.Helper()
	models := []ProviderModel{{ID: "gpt-4o", Enabled: true}, {ID: "gpt-4o-mini", Enabled: true}}
	a := newTestApp(t)
	a.data.Connections = []Connection{
		{ID: "openai", Name: "OpenAI", Provider: "OpenAI", Models: models, ModelsSet: true},
		{ID: "openai-2", Name: "OpenAI 2", Provider: "OpenAI", Models: append([]ProviderModel(nil), models...), ModelsSet: true},
	}
	if err := a.keys.Set("openai", "sk-work"); err != nil {
		t.Fatal(err)
	}
	if err := a.keys.Set("openai-2", "sk-personal"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	cfg := Config{Provider: "OpenAI 2", Model: "gpt-4o", ConnectionID: "openai-2", BaseURL: officialBase("OpenAI")}
	a.data.Defaults = cfg
	a.data.Conversations = []Conversation{{
		ID: "c", Title: "Work", CreatedAt: now, UpdatedAt: now, Loaded: true,
		Config:   cfg,
		Messages: []Message{{ID: "m", Role: roleUser, Content: "Ship the model picker.", Done: true, CreatedAt: now}},
	}}
	a.data.ActiveID = "c"
	tt := ui.NewTester(a.view, w, h)
	if dark {
		tt.SetDark(true)
	}
	tt.Frame()
	if open {
		if err := tt.Click("Model OpenAI 2 · gpt-4o"); err != nil {
			t.Fatal(err)
		}
	}
	return tt.Image()
}

func sidebarMinWindow(t *testing.T, dark bool) image.Image {
	t.Helper()
	a := revisionChat(t)
	a.sidebarWidth = sidebarMax
	tt := ui.NewTester(a.view, 720, 520)
	if dark {
		tt.SetDark(true)
	}
	tt.Frame()
	return tt.Image()
}
