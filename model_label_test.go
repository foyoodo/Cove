package main

import (
	"image"
	"image/draw"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestComposerModelLabelRule is the chip's name rule. One enabled copy
// shows the model. A second enabled copy of that name shows
// "Provider · model". A disabled copy, or a provider that is not ready,
// does not. The check is the connection id plus the model id.
func TestComposerModelLabelRule(t *testing.T) {
	a := newTestApp(t)
	oc := customConn("conn-oc", "OpenCode", "https://opencode.example/v1", "deepseek-v4.1")
	relay := customConn("conn-relay", "Relay", "https://relay.example/v1", "deepseek-v4.1")
	a.data.Connections = []Connection{oc, relay}
	if err := a.keys.Set("conn-oc", "oc-key"); err != nil {
		t.Fatal(err)
	}
	if err := a.keys.Set("conn-relay", "relay-key"); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Provider: "OpenCode", Model: "deepseek-v4.1", ConnectionID: "conn-oc", BaseURL: oc.BaseURL}
	if got := a.composerModelLabel(cfg); got != "OpenCode · deepseek-v4.1" {
		t.Fatalf("shared name %q", got)
	}
	provider, model := a.composerModelParts(cfg)
	if provider != "OpenCode" || model != "deepseek-v4.1" {
		t.Fatalf("parts %q %q", provider, model)
	}

	// The other provider still lists the name, but it is switched off.
	a.data.Connections[1].Models[0].Enabled = false
	if got := a.composerModelLabel(cfg); got != "deepseek-v4.1" {
		t.Fatalf("disabled twin still prefixed: %q", got)
	}
	a.data.Connections[1].Models[0].Enabled = true
	if err := a.keys.Delete("conn-relay"); err != nil {
		t.Fatal(err)
	}
	if got := a.composerModelLabel(cfg); got != "deepseek-v4.1" {
		t.Fatalf("unready twin still prefixed: %q", got)
	}
	if err := a.keys.Set("conn-relay", "relay-key"); err != nil {
		t.Fatal(err)
	}

	a.data.Connections = []Connection{oc}
	if got := a.composerModelLabel(cfg); got != "deepseek-v4.1" {
		t.Fatalf("single provider %q", got)
	}

	// A qualified catalog id and the bare leaf are the same visible name.
	qualified := customConn("conn-q", "OpenCode", "https://opencode.example/v1", "org/deepseek-v4.1")
	bare := customConn("conn-b", "Bare", "https://bare.example/v1", "deepseek-v4.1")
	a.data.Connections = []Connection{qualified, bare}
	if err := a.keys.Set("conn-q", "q"); err != nil {
		t.Fatal(err)
	}
	if err := a.keys.Set("conn-b", "b"); err != nil {
		t.Fatal(err)
	}
	qcfg := Config{Provider: "OpenCode", Model: "org/deepseek-v4.1", ConnectionID: "conn-q", BaseURL: qualified.BaseURL}
	if got := a.composerModelLabel(qcfg); got != "OpenCode · deepseek-v4.1" {
		t.Fatalf("qualified leaf %q", got)
	}

	if !modelChosen(qcfg, "conn-q", "org/deepseek-v4.1") {
		t.Fatal("matching provider id and model id was not checked")
	}
	if modelChosen(qcfg, "conn-b", "deepseek-v4.1") || modelChosen(qcfg, "conn-b", "org/deepseek-v4.1") || modelChosen(qcfg, "conn-q", "deepseek-v4.1") {
		t.Fatal("checkmark matched the model name without the provider id")
	}
}

// TestComposerModelButtonShots draws the chip for one provider, for a
// shared name, and for a shared name that does not fit the 180pt button.
// The provider run stays whole. The model run takes the ellipsis.
func TestComposerModelButtonShots(t *testing.T) {
	dir := "/opt/cursor/artifacts/model-switch"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// deepseek-v4.1 is 10px past the 180pt chip once the dot and chevron
	// are in, so the shot uses the same name trimmed to what still fits
	// whole. The string rule above keeps the full example.
	const model = "deepseek-v4"
	oc := customConn("conn-oc", "OpenCode", "https://opencode.example/v1", model)
	relay := customConn("conn-relay", "Relay", "https://relay.example/v1", model)
	cfg := Config{Provider: "OpenCode", Model: model, ConnectionID: "conn-oc", BaseURL: oc.BaseURL}

	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		single := chipFixture(t, []Connection{oc}, map[string]string{"conn-oc": "oc-key"}, cfg)
		st := openTester(single)
		if dark {
			st.SetDark(true)
		}
		if _, ok := st.Find("Model " + model); !ok {
			t.Fatalf("single chip %q", st.Texts())
		}
		if _, ok := st.Find("Model OpenCode · " + model); ok {
			t.Fatal("single provider drew a prefix")
		}
		saveModelButton(t, st, "Model "+model, "single-"+mode+".png")

		dup := chipFixture(t, []Connection{oc, relay}, map[string]string{"conn-oc": "oc-key", "conn-relay": "relay-key"}, cfg)
		dt := openTester(dup)
		if dark {
			dt.SetDark(true)
		}
		if _, ok := dt.Find("Model OpenCode · " + model); !ok {
			t.Fatalf("shared chip %q", dt.Texts())
		}
		assertDrawn(t, dt, "OpenCode", "OpenCode", false, 1)
		assertDrawn(t, dt, " ·", " ·", false, 1)
		assertDrawn(t, dt, " "+model, " "+model, false, 1)
		if err := dt.Click("Model OpenCode · " + model); err != nil {
			t.Fatal(err)
		}
		rows := menuRows(t, dt)
		if on, ok := modelChecked(rows, "OpenCode", model); !ok || !on {
			t.Fatalf("opencode check %+v", rows)
		}
		if on, ok := modelChecked(rows, "Relay", model); !ok || on {
			t.Fatalf("relay check %+v", rows)
		}
		dt.CloseMenu()
		saveModelButton(t, dt, "Model OpenCode · "+model, "dup-"+mode+".png")
	}

	const long = "deepseek-v4.1-extended-context-preview"
	longOC := customConn("conn-oc", "OpenCode", "https://opencode.example/v1", long)
	longRelay := customConn("conn-relay", "Relay", "https://relay.example/v1", long)
	longCfg := Config{Provider: "OpenCode", Model: long, ConnectionID: "conn-oc", BaseURL: longOC.BaseURL}
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		a := chipFixture(t, []Connection{longOC, longRelay}, map[string]string{"conn-oc": "oc-key", "conn-relay": "relay-key"}, longCfg)
		tt := ui.NewTester(a.view, 720, 520)
		if dark {
			tt.SetDark(true)
		}
		btn, ok := tt.Find("Model OpenCode · " + long)
		if !ok {
			t.Fatalf("narrow chip %q", tt.Texts())
		}
		if btn.W > 180.5 {
			t.Fatalf("button wider than 180: %.1f", btn.W)
		}
		assertDrawn(t, tt, "OpenCode", "OpenCode", false, 1)
		assertDrawn(t, tt, " ·", " ·", false, 1)
		n := 0
		for _, d := range drawnTexts(tt) {
			if d.text != " "+long {
				continue
			}
			n++
			body := strings.TrimPrefix(strings.TrimSuffix(d.shown, "…"), " ")
			if !d.truncated || d.shown == " "+long || !strings.HasPrefix(long, body) || strings.Contains(d.shown, "OpenCode") {
				t.Fatalf("model run %+v", d)
			}
		}
		if n < 1 {
			t.Fatal("long model was not drawn")
		}
		saveModelButton(t, tt, "Model OpenCode · "+long, "truncate-"+mode+".png")
	}
}

func chipFixture(t *testing.T, conns []Connection, keys map[string]string, cfg Config) *App {
	t.Helper()
	a := newTestApp(t)
	a.data.Connections = conns
	for id, secret := range keys {
		if err := a.keys.Set(id, secret); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	a.data.Defaults = cfg
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Relay", CreatedAt: now, UpdatedAt: now, Config: cfg,
		Messages: []Message{{ID: "m", Role: roleUser, Content: "hi", Done: true, CreatedAt: now}},
	}}
	a.data.ActiveID = "c1"
	return a
}

func saveModelButton(t *testing.T, tt *ui.Tester, a11y, name string) {
	t.Helper()
	dir := "/opt/cursor/artifacts/model-switch"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r, ok := tt.Find(a11y)
	if !ok {
		t.Fatalf("save %s: %q missing", name, a11y)
	}
	img := tt.Image()
	b := img.Bounds()
	x0 := int(r.X) - 28
	y0 := int(r.Y) - 18
	x1 := int(r.X+r.W) + 36
	y1 := int(r.Y+r.H) + 18
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	dst := image.NewRGBA(image.Rect(0, 0, x1-x0, y1-y0))
	draw.Draw(dst, dst.Bounds(), img, image.Pt(x0, y0), draw.Src)
	writePNG(t, filepath.Join(dir, name), dst)
	writePNG(t, filepath.Join(dir, "window-"+name), img)
}
