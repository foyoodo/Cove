package main

import (
	"image"
	"image/color"
	"image/draw"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// TestModelSwitchLandsWithoutInput is the model-menu form of
// TestThemeSwitchLandsWithoutInput. useModel writes the chat from the
// menu frame, where requestFrame does nothing, and then invalidates.
// No click, move, or scroll follows the call. One delivered frame is
// enough for the chip and the title to show the new model.
func TestModelSwitchLandsWithoutInput(t *testing.T) {
	a := modelSwitchApp(t, t.TempDir())
	tt := openTester(a)
	if _, ok := tt.Find("alpha"); !ok {
		t.Fatalf("chip before switch: %q", tt.Texts())
	}
	if _, ok := tt.Find("North · alpha"); ok {
		t.Fatal("unique model name was prefixed with the provider")
	}

	var n atomic.Int32
	themeInvalidateHook.Store(func() { n.Add(1) })
	defer themeInvalidateHook.Store(func() {})

	a.useModel("conn-south", "beta")
	if n.Load() == 0 {
		t.Fatal("useModel saved the model without invalidating a window")
	}
	if _, ok := tt.Find("alpha"); !ok {
		t.Fatal("chip changed before the invalidate frame")
	}
	cfg := a.conversation("c1").Config
	if cfg.ConnectionID != "conn-south" || cfg.Model != "beta" || cfg.Provider != "South" || cfg.BaseURL != a.connByID("conn-south").BaseURL {
		t.Fatalf("state before the frame %+v", cfg)
	}

	runOneFrame(tt)
	if _, ok := tt.Find("beta"); !ok {
		t.Fatalf("chip did not land: %q", tt.Texts())
	}
	if _, ok := tt.Find("alpha"); ok {
		t.Fatal("old model still on screen after one frame")
	}
	if strings.Contains(strings.Join(tt.Texts(), "\n"), "South · beta") {
		t.Fatal("unique model picked up a provider prefix")
	}
}

// TestModelSwitchPersistsAcrossProviders follows one menu choice from the
// chip through the JSONL header to the next request, including a second
// provider with its own model id and base URL, a switch in the middle of
// a chat, and a choice that must stay on the chat the menu was opened for.
func TestModelSwitchPersistsAcrossProviders(t *testing.T) {
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
	a.conversation("c1").Config.BaseURL = north.URL + "/v1"
	a.conversation("c1").Messages = []Message{{
		ID: "m", Role: roleUser, Content: "already said", Done: true, CreatedAt: time.Now(),
	}}
	other := a.conversation("c2")
	otherBefore := other.Config

	tt := openTester(a)
	if _, ok := tt.Find("alpha"); !ok {
		t.Fatalf("chip %q", tt.Texts())
	}
	if err := tt.Click("Model alpha"); err != nil {
		t.Fatal(err)
	}
	rows := menuRows(t, tt)
	if on, ok := modelChecked(rows, "North", "alpha"); !ok || !on {
		t.Fatalf("checkmark before %+v", rows)
	}
	if on, ok := modelChecked(rows, "South", "beta"); !ok || on {
		t.Fatalf("south checked before %+v", rows)
	}
	writeMenuBoard(t, rows, "menu-before.png")
	cropChip(t, tt, "chip-before.png")

	// The menu was built for c1. Switching the active chat before the
	// choice is delivered must not retarget c2.
	a.selectChat("c2")
	chooseGroupedModel(t, tt, "South", "beta")
	got := a.conversation("c1").Config
	if got.ConnectionID != "conn-south" || got.Model != "beta" || got.Provider != "South" {
		t.Fatalf("c1 after menu %+v", got)
	}
	if got.BaseURL != south.URL+"/v1" {
		t.Fatalf("c1 kept the old endpoint %q", got.BaseURL)
	}
	if a.conversation("c2").Config != otherBefore {
		t.Fatalf("choice landed on the other chat %+v", a.conversation("c2").Config)
	}
	header := filepath.Join(dir, chatsDir, "c1.jsonl")
	assertHeaderIdentity(t, header, "conn-south", "beta")

	a.selectChat("c1")
	tt.Frame()
	if _, ok := tt.Find("beta"); !ok {
		t.Fatalf("chip after switch %q", tt.Texts())
	}
	cropChip(t, tt, "chip-after.png")
	if err := tt.Click("Model beta"); err != nil {
		t.Fatal(err)
	}
	rows = menuRows(t, tt)
	if on, ok := modelChecked(rows, "North", "alpha"); !ok || on {
		t.Fatalf("north checked after %+v", rows)
	}
	if on, ok := modelChecked(rows, "South", "beta"); !ok || !on {
		t.Fatalf("checkmark after %+v", rows)
	}
	writeMenuBoard(t, rows, "menu-after.png")
	tt.CloseMenu()

	focusDraft(t, tt)
	tt.Type("next turn")
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	host := strings.TrimPrefix(strings.TrimPrefix(south.URL, "https://"), "http://")
	if !strings.Contains(gotURL, host) || !strings.Contains(gotURL, "/chat/completions") {
		t.Fatalf("request url %q, want host %s and /chat/completions", gotURL, host)
	}
	if auth != "Bearer south-key" || strings.Contains(auth, "north") {
		t.Fatalf("auth %q", auth)
	}
	if !strings.Contains(body, `"model":"beta"`) || strings.Contains(body, `"model":"alpha"`) {
		t.Fatalf("body %s", body)
	}
	if !strings.Contains(body, "already said") || !strings.Contains(body, "next turn") {
		t.Fatalf("mid-chat history dropped: %s", body)
	}
	if a.conversation("c1").Messages[0].Content != "already said" {
		t.Fatal("switch rewrote earlier messages")
	}
	assertHeaderIdentity(t, header, "conn-south", "beta")

	b := openApp(filepath.Join(dir, "conversations.json"), a.keys)
	b.instant = true
	// The sidebar index stores the display name and the model only.
	// Opening the chat reads connectionId from the JSONL header.
	b.ensureChat("c1")
	reloaded := b.conversation("c1").Config
	if reloaded.ConnectionID != "conn-south" || reloaded.Model != "beta" || reloaded.Provider != "South" || reloaded.BaseURL != south.URL+"/v1" {
		t.Fatalf("reload %+v", reloaded)
	}
	if b.resolvedKey(reloaded) != "south-key" {
		t.Fatalf("reload key %q", b.resolvedKey(reloaded))
	}
}

func TestModelSwitchReport(t *testing.T) {
	section := `
Model switch
The composer model menu identified a choice by the provider's display name and the model id. Two ready providers that share a model id, or a chat header that never stored a connection id, could check one row and send the next request with the other provider's base URL and key. A choice is the connection id plus the model id. useModelChat writes both onto that chat only, copies that connection's display name and base URL, clears any inline key, and keeps the pair in the JSONL header even when the model string matches the app default. The menu check uses the same pair, so reopening it after a chat switch still marks the row that will be sent. The chip shows the provider name only when that model name is enabled on two or more ready providers.

The choice is applied inside the frame the menu delivers. requestFrame does nothing while that frame is running, and on macOS the display link can drop the frame asked for from the menu tracking loop: it clears its due flag and returns without a frame while the layer needs display, then pauses. Pointer motion does not ask for another frame. The chip, the title-bar subtitle, and the check mark stayed on the old model until some later click. useModelChat and setEffortChat now invalidate both windows after a real change. Invalidate posts to the main thread and, on darwin, signals the run loop, so the new model is presented with no further pointer, scroll, or key. TestModelSwitchLandsWithoutInput calls useModel with no Click, Move, or Scroll afterward and checks the chip on the single frame that invalidation delivers. TestModelSwitchPersistsAcrossProviders follows a menu choice on one chat, while another chat is active, through the header connectionId and model fields to the next request's model and /chat/completions endpoint, including a switch after the chat already has messages.

A click on the chip opens the menu. The button is the hit target for the dot, the model name, and the chevron, and an empty provider list still opens "Add a provider…" rather than a menu with no items. Choosing an enabled model updates that chip on the choice frame. The JSONL header stores the catalog id (openai/gpt-4o, not the short name the chip draws) and the next request body sends that same id. The choice stays on that chat across a chat switch and a restart. Other chats and the global default are left as they were. Check marks follow the button's chat, so dismissing the menu and opening it on another chat does not show the previous chat's row. Disabled models leave the list the next time it opens. Renaming a provider changes the menu label and leaves the model id alone. A schema 5 model list of strings is that same id space.

crops: artifacts/model-switch/chip-before.png and chip-after.png are the composer chip. menu-before.png and menu-after.png are the open menu's checks. The headless tester does not paint the native menu into the window, so those two boards are the menu items and their check marks.

Stored model references are the pair (connection id, model id). The model string is left as it was stored.

settings.json defaults is General's default model. Temperature, max tokens, reasoning, and the system prompt sit on that same config; they are not a map keyed by model id. An old file that only had a provider name, a base URL, or a bare model gets a connection id on load. The model string is not rewritten.

A chat JSONL header stores connectionId and model. That is the chat's model, including a draft that has not been sent. Max tokens, temperature, reasoning, and extra body on that header are this chat's overrides of the defaults, not a per-model table. maxRounds is the chat's New Session limit, not a per-model context cap. There is no title-generation model. Titles come from the draft or the first message.

index.json stores the provider name, the model id, and the connection id for the sidebar. An older index has no connection id until that chat is saved again. The request is not built from the index. Opening the chat reads the header.

Assistant message lines record the model id that produced them. They are not read back and they do not choose the next request. The credential store is keyed by connection id. Nothing is keyed by a base URL.

Migration resolves a unique provider name, then a model only one connection lists, then a base URL only one connection uses. When several connections still match, the first in list order is used and logged as an ambiguous model reference. Two providers with the same base URL and the same model name stay separate menu rows. The check and the request follow the connection id, so the request sends that provider's key.

The General default-model control uses the same label rule as the composer chip. The provider name is shown only when that model name is enabled on two ready providers, and a narrow control ellipsizes the model first.
`
	path := perfReportPath(t)
	raw, err := os.ReadFile(path)
	if err == nil && strings.Contains(string(raw), "\nModel switch\n") {
		t.Log("Model switch section already present")
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(section); err != nil {
		t.Fatal(err)
	}
}

func modelSwitchApp(t *testing.T, dir string) *App {
	t.Helper()
	path := filepath.Join(dir, "conversations.json")
	keys := newMemKeys()
	a := openApp(path, keys)
	a.instant = true
	north := "https://north.example/v1"
	south := "https://south.example/v1"
	a.data.Connections = []Connection{
		customConn("conn-north", "North", north, "alpha"),
		customConn("conn-south", "South", south, "beta"),
	}
	if err := a.keys.Set("conn-north", "north-key"); err != nil {
		t.Fatal(err)
	}
	if err := a.keys.Set("conn-south", "south-key"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	cfg := Config{Provider: "North", Model: "alpha", BaseURL: north, ConnectionID: "conn-north", SystemPrompt: DefaultConfig().SystemPrompt}
	other := Config{Provider: "North", Model: "alpha", BaseURL: north, ConnectionID: "conn-north", SystemPrompt: DefaultConfig().SystemPrompt}
	a.data.Defaults = cfg
	a.data.Conversations = []Conversation{
		{ID: "c1", Title: "Relay", CreatedAt: now, UpdatedAt: now, Config: cfg, Messages: []Message{{ID: "m0", Role: roleUser, Content: "hi", Done: true, CreatedAt: now}}},
		{ID: "c2", Title: "Other", CreatedAt: now, UpdatedAt: now.Add(-time.Minute), Config: other, Messages: []Message{{ID: "m2", Role: roleUser, Content: "other", Done: true, CreatedAt: now}}},
	}
	a.data.ActiveID = "c1"
	a.save()
	return a
}

func cropChip(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	dir := "/opt/cursor/artifacts/model-switch"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	img := tt.Image()
	b := img.Bounds()
	// The chip sits on the composer, along the bottom of the card.
	r := image.Rect(b.Min.X+200, b.Max.Y-120, b.Max.X-40, b.Max.Y-8)
	dst := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(dst, dst.Bounds(), img, r.Min, draw.Src)
	writePNG(t, filepath.Join(dir, name), dst)
	writePNG(t, filepath.Join(dir, "window-"+name), img)
}

func writeMenuBoard(t *testing.T, rows []menuRow, name string) {
	t.Helper()
	dir := "/opt/cursor/artifacts/model-switch"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range rows {
		if !r.separator {
			n++
		}
	}
	img := image.NewRGBA(image.Rect(0, 0, 280, 28+22*n))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{R: 28, G: 28, B: 30, A: 255}), image.Point{}, draw.Src)
	d := &font.Drawer{Dst: img, Src: image.NewUniform(color.White), Face: basicfont.Face7x13}
	i := 0
	for _, r := range rows {
		if r.separator {
			continue
		}
		mark := "  "
		if r.checked {
			mark = "✓ "
		}
		d.Dot = fixed.P(12, 22+i*22)
		d.DrawString(mark + r.label)
		i++
	}
	writePNG(t, filepath.Join(dir, name), img)
}
