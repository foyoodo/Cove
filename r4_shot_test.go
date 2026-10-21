package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestR4Screens renders the Models & Providers redesign, General › New
// chats, and the edit-resend hint, then lays each one beside the design.
func TestR4Screens(t *testing.T) {
	dir := filepath.Join("/opt/cursor/artifacts", "r4")
	if err := os.MkdirAll(filepath.Join(dir, "min"), 0o755); err != nil {
		t.Fatal(err)
	}
	designDir := "/cursor/stores/self/design-v07-r4"
	requireDesign(t, designDir)
	names := []string{
		"settings-models-light", "settings-models-dark",
		"provider-detail-light", "provider-detail-dark",
		"provider-detail-min-light", "provider-detail-min-dark",
		"provider-add-light", "provider-add-dark",
		"provider-delete-light", "provider-delete-dark",
		"settings-models-notice-light", "settings-models-notice-dark",
		"settings-general-light", "settings-general-dark",
		"settings-general-min-light", "settings-general-min-dark",
		"message-edit-light", "message-edit-dark",
		"message-edit-min-light", "message-edit-min-dark",
	}
	for _, name := range names {
		design := mustPNG(t, filepath.Join(designDir, name+".png"))
		b := design.Bounds()
		w, h := b.Dx()/2, b.Dy()/2
		dark := strings.HasSuffix(name, "-dark")
		base := strings.TrimSuffix(strings.TrimSuffix(name, "-light"), "-dark")
		tt := renderR4(t, base, w, h, dark)
		img := tt.Image()
		writePNG(t, filepath.Join(dir, name+"-impl.png"), img)
		writePNG(t, filepath.Join(dir, "compare-"+name+".png"), boardBeside(design, img))
		assertNoHOverflow(t, tt, w, base)
	}
	// The minimum window, including pages whose design file is the
	// default 780×560 size.
	for _, base := range []string{"settings-models", "provider-detail", "provider-add", "provider-delete", "settings-models-notice", "settings-general"} {
		for _, dark := range []bool{false, true} {
			tt := renderR4(t, base, 720, 520, dark)
			assertNoHOverflow(t, tt, 720, base)
			writePNG(t, filepath.Join(dir, "min", base+"-"+modeName(dark)+".png"), tt.Image())
		}
	}
}

func renderR4(t *testing.T, base string, w, h int, dark bool) *ui.Tester {
	t.Helper()
	a := r4Scene(t, base)
	tt := ui.NewTester(a.view, w, h)
	if dark {
		tt.SetDark(true)
	}
	tt.Frame()
	return tt
}

func r4Scene(t *testing.T, base string) *App {
	t.Helper()
	switch base {
	case "message-edit", "message-edit-min":
		return revisionScene(t, "message-edit")
	case "settings-general", "settings-general-min":
		a := revisionChat(t)
		seedModelsScreenshot(a)
		a.data.Defaults.SystemPrompt = "You are a helpful assistant. Be concise."
		a.prefReady = false
		a.settingsOpen = true
		a.settingsSection = "general"
		a.settingsPage = settingsList
		return a
	default:
		a := revisionChat(t)
		seedModelsScreenshot(a)
		a.settingsOpen = true
		a.settingsSection = "models"
		prepareProviderShot(a, base)
		return a
	}
}

func prepareProviderShot(a *App, base string) {
	switch base {
	case "provider-detail":
		a.viewCustom("shot-together")
		a.settingsPage = settingsDetail
		a.provEdit.testKind = "ok"
		a.provEdit.testLine = "Connected · 12 models · 312 ms"
		a.provEdit.models = []ProviderModel{
			{ID: "meta-llama/Llama-3.3-70B-Instruct-Turbo", Enabled: true},
			{ID: "Qwen/Qwen2.5-72B-Instruct-Turbo", Enabled: true},
			{ID: "deepseek-ai/DeepSeek-V3", Enabled: false, Fresh: true},
			{ID: "mistralai/Mixtral-8x22B-Instruct-v0.1", Enabled: false},
		}
	case "provider-detail-min":
		a.data.Connections = append(a.data.Connections, Connection{
			ID: "shot-fireworks", Name: "accounts/fireworks/models/llama-v3p1-405b-instruct",
			Provider: "Custom", Custom: true, BaseURL: "https://api.fireworks.ai/inference/v1",
			Models: []ProviderModel{{
				ID: "accounts/fireworks/models/llama-v3p1-405b-instruct", Enabled: true,
			}},
			ModelsSet: true,
		})
		_ = a.keys.Set("shot-fireworks", "fw-screenshot-key-0000000000000000")
		a.viewCustom("shot-fireworks")
		a.settingsPage = settingsDetail
		a.provEdit.refreshNote = "accounts/fireworks/models/llama-v3p1-405b-instruct doesn’t list its models. Showing your saved list — add models by ID."
	case "provider-add":
		a.settingsPage = settingsAdd
	case "provider-delete":
		a.viewCustom("shot-together")
		a.settingsPage = settingsDetail
		a.confirmKind = confirmDeleteProvider
		a.deleteID = "shot-together"
	case "settings-models-notice":
		a.settingsPage = settingsList
		a.providerNotice = "Together AI was deleted. New chats now use gpt-4o (OpenAI)."
	default:
		a.settingsPage = settingsList
	}
}

func assertNoHOverflow(t *testing.T, tt *ui.Tester, w int, base string) {
	t.Helper()
	labels := []string{"Add Provider…", "Test Connection", "Refresh", "Default model", "Temperature", "Max tokens", "Replaces 3 later messages"}
	for _, label := range labels {
		r, ok := tt.Find(label)
		if !ok {
			continue
		}
		if r.X < -0.5 || r.X+r.W > float32(w)+0.5 {
			t.Fatalf("%s %q overflows horizontally: x %.1f w %.1f window %d", base, label, r.X, r.W, w)
		}
	}
}
