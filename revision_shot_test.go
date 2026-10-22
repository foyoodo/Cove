package main

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cove/themes"

	"github.com/egoist/mygo/ui"
	"golang.org/x/image/draw"
)

const revisionDesign = "/cursor/stores/self/design-v07-r3"

// TestRevisionScreens renders every r2 design screen in light and dark,
// writes the implementation shot, and rebuilds a side-by-side board against
// the design PNG. Design files are 2×, so the tester uses half their pixel
// size. Every page is also saved at the 720×520 minimum.
func TestRevisionScreens(t *testing.T) {
	dir := testArtifactDir(t, "r2")
	qaDir := testArtifactDir(t, "qa")
	minDir := filepath.Join(dir, "min")
	for _, d := range []string{dir, qaDir, minDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	requireDesign(t, revisionDesign)
	entries, err := os.ReadDir(revisionDesign)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".png") || e.Name() == "icon-1024.png" {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".png"))
	}
	if len(names) < 20 {
		t.Fatalf("design pngs: %d", len(names))
	}
	seenMin := map[string]bool{}
	for _, name := range names {
		designPath := filepath.Join(revisionDesign, name+".png")
		baseName := strings.TrimSuffix(strings.TrimSuffix(name, "-light"), "-dark")
		if baseName == "message-edit" {
			alt := filepath.Join("/cursor/stores/self/design-v07-r4", name+".png")
			if _, err := os.Stat(alt); err == nil {
				designPath = alt
			}
		}
		designImg := mustPNG(t, designPath)
		b := designImg.Bounds()
		w, h := b.Dx()/2, b.Dy()/2
		if w < 200 || h < 200 {
			t.Fatalf("%s design size %dx%d", name, b.Dx(), b.Dy())
		}
		dark := strings.HasSuffix(name, "-dark")
		base := strings.TrimSuffix(strings.TrimSuffix(name, "-light"), "-dark")
		img := renderRevision(t, base, w, h, dark)
		writePNG(t, filepath.Join(dir, name+"-impl.png"), img)
		board := boardBeside(designImg, img)
		writePNG(t, filepath.Join(dir, "compare-"+name+".png"), board)
		writeQABoard(t, qaDir, base, dark, board)
		if w == 720 && h == 520 {
			seenMin[base+"|"+modeName(dark)] = true
			writePNG(t, filepath.Join(minDir, name+".png"), img)
		}
	}
	pages := []string{
		"main", "empty", "generating",
		"settings-general", "settings-models", "settings-appearance", "settings-shortcuts", "settings-advanced",
		"chat-settings", "dialog-delete", "dialog-resend", "message-edit", "message-edit-confirm",
	}
	for _, base := range pages {
		for _, dark := range []bool{false, true} {
			key := base + "|" + modeName(dark)
			if seenMin[key] {
				continue
			}
			img := renderRevision(t, base, 720, 520, dark)
			writePNG(t, filepath.Join(minDir, base+"-"+modeName(dark)+".png"), img)
			if base == "message-edit" {
				designPath := filepath.Join("/cursor/stores/self/design-v07-r4", "message-edit-min-"+modeName(dark)+".png")
				if _, err := os.Stat(designPath); err == nil {
					writePNG(t, filepath.Join(dir, "compare-message-edit-min-"+modeName(dark)+".png"), boardBeside(mustPNG(t, designPath), img))
				}
			}
		}
	}
	assertAboutVersion(t, dir)
}

func modeName(dark bool) string {
	if dark {
		return "dark"
	}
	return "light"
}

func writeQABoard(t *testing.T, dir, base string, dark bool, board image.Image) {
	t.Helper()
	var qa string
	switch base {
	case "main-floating":
		qa = "composer-floating"
	case "main":
		qa = "sidebar-selection"
	case "settings-general":
		qa = "settings-window"
	case "min-main":
		qa = "min-window"
	case "dialog-delete":
		qa = "dialog"
	case "message-edit":
		qa = "message-edit"
	case "presets":
		qa = "presets-board"
	default:
		return
	}
	writePNG(t, filepath.Join(dir, qa+"-"+modeName(dark)+".png"), board)
}

// requireDesign skips when the design PNG or directory is not on this
// machine. The test exists to lay the implementation beside that file.
func requireDesign(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("design file missing (%s); skipping the screenshot comparison", path)
		}
		t.Fatal(err)
	}
}

func mustPNG(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("design PNG missing (%s); skipping the screenshot comparison", path)
		}
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func boardBeside(designImg, impl image.Image) image.Image {
	ib := impl.Bounds()
	w, h := ib.Dx(), ib.Dy()
	const gap = 12
	scaled := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(scaled, scaled.Bounds(), designImg, designImg.Bounds(), draw.Over, nil)
	out := image.NewNRGBA(image.Rect(0, 0, w*2+gap, h))
	draw.Draw(out, out.Bounds(), image.NewUniform(color.NRGBA{R: 255, G: 255, B: 255, A: 255}), image.Point{}, draw.Src)
	draw.Draw(out, image.Rect(0, 0, w, h), scaled, image.Point{}, draw.Src)
	draw.Draw(out, image.Rect(w+gap, 0, w*2+gap, h), impl, ib.Min, draw.Src)
	return out
}

func renderRevision(t *testing.T, base string, w, h int, dark bool) image.Image {
	t.Helper()
	if base == "presets" {
		return themeBoard(t, dark)
	}
	a := revisionScene(t, base)
	tt := ui.NewTester(a.view, w, h)
	if dark {
		tt.SetDark(true)
	}
	if wantsRevisionHover(base) {
		if r, ok := tt.Find("How do I cancel an in-flight HTTP request when the window closes?"); ok && r.H > 1 {
			tt.Move(r.X+r.W/2, r.Y+r.H/2)
		}
	}
	if (base == "empty" || base == "min-empty") && w <= 720 {
		if tt.HasText("Compare two approaches") {
			t.Fatal("the fourth suggestion is still shown at 720")
		}
		for _, title := range []string{"Explain a tricky idea", "Outline a plan", "Review a draft"} {
			if !tt.HasText(title) {
				t.Fatalf("compact empty missing %s: %q", title, tt.Texts())
			}
		}
	}
	if base == "generating" && !tt.HasText("Stop") {
		t.Fatalf("generating: %q", tt.Texts())
	}
	if base == "main-scrolled" {
		if r, ok := tt.Find("transcript"); ok {
			for i := 0; i < 6; i++ {
				tt.Scroll(r.X+40, r.Y+80, 0, -120)
			}
		}
	}
	if base == "message-edit" || base == "message-edit-confirm" || base == "dialog-resend" {
		if _, ok := tt.Find("Replaces 3 later messages"); !ok && !tt.HasText("Replaces") {
			t.Fatalf("%s missing the replaces hint: %q", base, tt.Texts())
		}
	}
	if base == "settings-shortcuts" {
		blob := strings.Join(tt.Texts(), "\n")
		if strings.Contains(blob, "Hide Sidebar") || strings.Contains(blob, "⌃⌘S") {
			t.Fatalf("sidebar collapse still listed:\n%s", blob)
		}
		if tt.HasText("Keyboard Shortcuts") {
			t.Fatalf("sidebar label drifted:\n%s", blob)
		}
		for _, want := range []string{"Shortcuts", "Search Chats", "Stop Generating", "New Chat", "Edit Last Message", "Chat Settings"} {
			if !tt.HasText(want) {
				t.Fatalf("shortcuts missing %q: %q", want, tt.Texts())
			}
		}
	}
	return tt.Image()
}

func wantsRevisionHover(base string) bool {
	switch base {
	case "main", "main-floating", "min-main":
		return true
	default:
		return strings.HasPrefix(base, "main-theme-")
	}
}

func assertAboutVersion(t *testing.T, dir string) {
	t.Helper()
	for _, dark := range []bool{false, true} {
		a := revisionChat(t)
		a.settingsOpen = true
		a.settingsSection = "general"
		tt := ui.NewTester(a.view, 780, 900)
		if dark {
			tt.SetDark(true)
		}
		if !tt.HasText("Version 0.7.0 · macOS 26") {
			a.formScroll.Y = 1e9
			tt.SetSize(780, 900)
		}
		if !tt.HasText("Version 0.7.0 · macOS 26") || !tt.HasText("Cove") || !tt.HasText("Acknowledgements") {
			t.Fatalf("about %s: %q", modeName(dark), tt.Texts())
		}
		if tt.HasText("Check for Updates…") {
			t.Fatalf("about %s still offers updates: %q", modeName(dark), tt.Texts())
		}
		writePNG(t, filepath.Join(dir, "settings-about-"+modeName(dark)+".png"), tt.Image())
	}
}

func revisionScene(t *testing.T, base string) *App {
	t.Helper()
	switch {
	case base == "states":
		a := newTestApp(t)
		a.shotStates = true
		return a
	case base == "empty" || base == "min-empty":
		a := newTestApp(t)
		a.newChat()
		return a
	case base == "generating" || base == "generating-stream":
		return revisionGenerating(t, base == "generating-stream")
	case base == "main-inactive":
		a := revisionChat(t)
		a.shotInactive = true
		return a
	case base == "main-multiline":
		a := revisionChat(t)
		a.active().Draft = "And if the window closes while the body is still streaming:\n– does cancel() also close resp.Body?\n– should I still drain it before returning?"
		return a
	case base == "main-accent-purple":
		a := revisionChat(t)
		purple, _ := themes.NamedAccent("purple", false)
		a.shotAccent = purple
		return a
	case base == "main-accent-orange":
		a := revisionChat(t)
		orange, _ := themes.NamedAccent("orange", true)
		a.shotAccent = orange
		a.data.Appearance = appearanceDark
		return a
	case base == "palette":
		a := revisionChat(t)
		a.shotPalette = true
		return a
	case base == "chat-settings":
		a := revisionChat(t)
		a.active().LastRequest = "{\n  \"model\": \"claude-sonnet-4.5\",\n  \"stream\": true\n}"
		a.openSettingsTab(0)
		return a
	case base == "dialog-delete":
		a := revisionChat(t)
		a.deleteID = "cancel"
		a.confirmKind = confirmDelete
		return a
	case base == "dialog-resend" || base == "message-edit-confirm" || base == "message-edit":
		a := revisionChat(t)
		// First user turn, so three messages follow and the hint is plural.
		a.editMessage("cancel", "u0")
		a.edit.text = "What’s the difference between Client.Timeout and a per-request context deadline in Go?"
		a.shotPinEdit = true
		return a
	case strings.HasPrefix(base, "main-theme-"):
		a := revisionChat(t)
		a.data.Theme = themes.NormalizeTheme(strings.TrimPrefix(base, "main-theme-"))
		return a
	case strings.HasPrefix(base, "settings-") || base == "min-settings" || base == "min-appearance":
		a := revisionChat(t)
		a.settingsOpen = true
		a.settingsSection = revisionSettingsSection(base)
		if a.settingsSection == "models" {
			seedModelsScreenshot(a)
		}
		return a
	default:
		return revisionChat(t)
	}
}

func TestSidebarFixtureHasOneDraft(t *testing.T) {
	for _, base := range []string{"main", "min-main", "dialog-delete"} {
		a := revisionScene(t, base)
		drafts := 0
		for i := range a.data.Conversations {
			c := &a.data.Conversations[i]
			title := sidebarTitle(c)
			if len(c.Messages) == 0 {
				drafts++
				if title == newChatTitle || strings.TrimSpace(c.Draft) == "" {
					t.Fatalf("%s empty draft %q", base, title)
				}
			} else if title == newChatTitle {
				t.Fatalf("%s real chat titled %q", base, c.Title)
			}
		}
		if drafts != 1 {
			t.Fatalf("%s has %d drafts", base, drafts)
		}
		w, h := 1120, 760
		if base == "min-main" {
			w, h = 720, 520
		}
		tt := ui.NewTester(a.view, w, h)
		for _, want := range []string{"Go greet function", "fsync before rename", "Weekend trip outline", "Sketch a weekend in Lisbon"} {
			if !tt.HasText(want) {
				t.Fatalf("%s missing %q: %q", base, want, tt.Texts())
			}
		}
		rows := 0
		for _, text := range tt.Texts() {
			if text == newChatTitle {
				rows++
			}
		}
		// Compose button, and its tooltip. Not a sidebar row.
		if rows > 2 {
			t.Fatalf("%s shows %d %q rows: %q", base, rows, newChatTitle, tt.Texts())
		}
	}
}

// seedModelsScreenshot fills the provider list the way the design shows it.
// Status comes only from the saved key and the last local test.
func seedModelsScreenshot(a *App) {
	temp := 0.7
	a.data.Defaults.Temperature = &temp
	a.data.Defaults.MaxTokens = 4096
	a.data.Defaults.Provider = "OpenAI"
	a.data.Defaults.Model = "gpt-4o-mini"
	a.data.Defaults.BaseURL = officialBase("OpenAI")
	a.prefReady = false
	a.settingsPage = settingsList
	type row struct {
		id, name, provider, base, key, probe string
		custom                               bool
		models                               []ProviderModel
	}
	rows := []row{
		{id: "shot-openai", name: "OpenAI", provider: "OpenAI", key: "sk-proj-QmX4c8Lw2Rt7Vb1Nz5Hk9Dp3Ye6Ja0s21b7"},
		{id: "shot-anthropic", name: "Anthropic", provider: "Anthropic", key: "sk-ant-api03-7Hq2xL9vB4mN8pR1cT6wY3zK5dF0gJ3f9a", probe: probeOK, models: []ProviderModel{
			{ID: "claude-sonnet-4.5", Enabled: true},
			{ID: "claude-opus-4", Enabled: true},
			{ID: "claude-haiku-4.5", Enabled: false},
		}},
		{id: "shot-deepseek", name: "DeepSeek", provider: "DeepSeek", key: "sk-deepseek-screenshot-key-0000"},
		{id: "shot-openrouter", name: "OpenRouter", provider: "OpenRouter", key: "sk-or-screenshot-key-0000000000"},
		{id: "shot-xai", name: "xAI", provider: "xAI", key: "xai-screenshot-key-000000000000", probe: probeInvalid},
		{id: "shot-together", name: "Together AI", provider: "Custom", custom: true, base: "https://api.together.xyz/v1", key: "tg-screenshot-key-000000000000"},
		{id: "shot-local", name: "Local", provider: "Custom", custom: true, base: "http://127.0.0.1:8080/v1"},
	}
	for _, row := range rows {
		conn := Connection{
			ID: row.id, Name: row.name, Provider: row.provider, BaseURL: row.base,
			Custom: row.custom, Probe: row.probe, Models: row.models, ModelsSet: len(row.models) > 0,
		}
		a.data.Connections = append(a.data.Connections, conn)
		if row.key != "" {
			if err := a.keys.Set(row.id, row.key); err != nil {
				panic(err)
			}
		}
	}
}

func revisionSettingsSection(base string) string {
	switch {
	case strings.Contains(base, "models"):
		return "models"
	case strings.Contains(base, "appearance"):
		return "appearance"
	case strings.Contains(base, "shortcut"):
		return "shortcuts"
	case strings.Contains(base, "advanced"):
		return "advanced"
	default:
		return "general"
	}
}

func revisionGenerating(t *testing.T, stream bool) *App {
	t.Helper()
	a := revisionChat(t)
	_, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	now := time.Now()
	stamp := time.Date(now.Year(), now.Month(), now.Day(), 9, 41, 0, 0, now.Location())
	body := "Rename is atomic for the **name**, not for the **data**."
	if stream {
		body += " Write, flush, then publish. On APFS, F_FULLFSYNC also flushes the drive cache. The safe sequence is write, f.Sync(), close, rename, then sync the directory."
	}
	for i := range a.data.Conversations {
		if a.data.Conversations[i].ID != "fsync" {
			continue
		}
		a.data.Conversations[i].Config.Reasoning = effortHigh
		a.data.Conversations[i].Messages = []Message{
			{ID: "u", Role: roleUser, Content: "Why fsync the temp file before rename? Isn’t rename already atomic?", Done: true, CreatedAt: stamp},
			{ID: "a", Role: roleAssistant, Content: body, Reasoning: "rename(2) swaps the directory entry atomically, but the data blocks may still be in the cache.", ReasoningOpen: true, ThinkingMS: 4000, CreatedAt: stamp},
		}
	}
	a.data.ActiveID = "fsync"
	a.streams["fsync"] = &convStream{gen: 1, cancel: cancel}
	return a
}

func revisionChat(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	now := time.Now()
	stamp := time.Date(now.Year(), now.Month(), now.Day(), 9, 41, 0, 0, now.Location())
	reply := strings.Join([]string{
		"### Tie the request to a context",
		"",
		"Build the request with `NewRequestWithContext` and cancel that context when the window closes.",
		"",
		"```go",
		"ctx, cancel := context.WithCancel(context.Background())",
		"win.OnClose(cancel)",
		"resp, err := http.DefaultClient.Do(req.WithContext(ctx))",
		"```",
		"",
		"| Approach | Stops the dial |",
		"| --- | --- |",
		"| Context cancel | Immediately |",
		"",
		"> Always **defer cancel()**.",
	}, "\n")
	a.data.Conversations = []Conversation{
		{
			ID: "cancel", Title: "Cancel HTTP on close",
			UpdatedAt: now.Add(-12 * time.Minute), CreatedAt: now.Add(-12 * time.Minute),
			Config: Config{
				Provider: "Anthropic", Model: "claude-sonnet-4.5", BaseURL: "https://api.anthropic.com/v1",
				Temperature: f64p(0.7), MaxTokens: 4096, Reasoning: "medium",
			},
			Messages: []Message{
				{ID: "u0", Role: roleUser, Content: "What’s the difference between Client.Timeout and a context deadline?", Done: true, CreatedAt: stamp},
				{ID: "a0", Role: roleAssistant, Content: "`Client.Timeout` bounds the whole exchange. A context deadline belongs to one request and can be cancelled early.", Done: true, CreatedAt: stamp},
				{ID: "u", Role: roleUser, Content: "How do I cancel an in-flight HTTP request when the window closes?", Done: true, CreatedAt: stamp},
				{ID: "a", Role: roleAssistant, Content: reply, Reasoning: "Cancellation has to follow the window.", ThinkingMS: 12000, Done: true, CreatedAt: stamp},
			},
		},
		{
			ID: "draft", Title: newChatTitle, Draft: "Sketch a weekend in Lisbon",
			UpdatedAt: now.Add(-40 * time.Minute), CreatedAt: now.Add(-40 * time.Minute),
			Config: DefaultConfig(),
		},
		{
			ID: "greet", Title: "Go greet function",
			UpdatedAt: now.Add(-2 * time.Hour), CreatedAt: now.Add(-2 * time.Hour),
			Config:   Config{Provider: "OpenAI", Model: "gpt-4o-mini", BaseURL: "https://api.openai.com/v1"},
			Messages: []Message{{ID: "g", Role: roleUser, Content: "Write a Go greet function.", Done: true, CreatedAt: now.Add(-2 * time.Hour)}},
		},
		{
			ID: "fsync", Title: "fsync before rename",
			UpdatedAt: now.Add(-26 * time.Hour), CreatedAt: now.Add(-26 * time.Hour),
			Config:   Config{Provider: "DeepSeek", Model: "deepseek-reasoner", BaseURL: "https://api.deepseek.com/v1", Reasoning: effortHigh},
			Messages: []Message{{ID: "f", Role: roleUser, Content: "Why fsync the temp file before rename?", Done: true, CreatedAt: now.Add(-26 * time.Hour)}},
		},
		{
			ID: "trip", Title: "Weekend trip outline",
			UpdatedAt: now.Add(-30 * time.Hour), CreatedAt: now.Add(-30 * time.Hour),
			Config:   Config{Provider: "OpenRouter", Model: "llama-3.3", BaseURL: "https://openrouter.ai/api/v1"},
			Messages: []Message{{ID: "t", Role: roleUser, Content: "Outline a weekend in Lisbon.", Done: true, CreatedAt: now.Add(-30 * time.Hour)}},
		},
	}
	a.data.ActiveID = "cancel"
	return a
}

func themeBoard(t *testing.T, dark bool) image.Image {
	t.Helper()
	const cw, ch = 480, 320
	ids := []string{themes.ThemeWarm, themes.ThemeTide, themes.ThemeNeutral, themes.ThemeMist, themes.ThemeSage, themes.ThemeLavender}
	out := image.NewNRGBA(image.Rect(0, 0, cw*3, ch*2))
	for i, id := range ids {
		a := revisionChat(t)
		a.data.Theme = id
		tt := ui.NewTester(a.view, cw, ch)
		if dark {
			tt.SetDark(true)
		}
		img := tt.Image()
		col, row := i%3, i/3
		draw.Draw(out, image.Rect(col*cw, row*ch, (col+1)*cw, (row+1)*ch), img, img.Bounds().Min, draw.Src)
	}
	return out
}

// TestThinkingCollapseShots saves frames of a long thread while Thinking
// collapses and expands, plus the reply's y at each frame.
func TestThinkingCollapseShots(t *testing.T) {
	dir := testArtifactDir(t, "thinking")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	reasoning := strings.Repeat("Weigh the crash window against the directory update. ", 8)
	const reply = "Reply line stays put."
	const header = "Thought for 34s"
	stamp := time.Now()
	var msgs []Message
	for i := 0; i < 8; i++ {
		msgs = append(msgs,
			Message{ID: fmt.Sprintf("u%d", i), Role: roleUser, Content: fmt.Sprintf("Question %d about durability on a busy volume.", i), Done: true, CreatedAt: stamp},
			Message{ID: fmt.Sprintf("a%d", i), Role: roleAssistant, Content: fmt.Sprintf("Earlier answer %d stands on its own line.", i), Done: true, CreatedAt: stamp},
		)
	}
	msgs = append(msgs,
		Message{ID: "uL", Role: roleUser, Content: "Question above the tracked reply.", Done: true, CreatedAt: stamp},
		Message{ID: "aL", Role: roleAssistant, Content: reply, Reasoning: reasoning, ReasoningOpen: true, ThinkingMS: 34000, Done: true, CreatedAt: stamp},
	)
	a := newTestApp(t)
	a.instant = false
	now := time.Now()
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Long thread", Config: DefaultConfig(), UpdatedAt: now, CreatedAt: now,
		Messages: msgs,
	}}
	a.data.ActiveID = "c1"
	tt := ui.NewTester(a.view, 1120, 760)
	for i := 0; i < 30; i++ {
		tt.Advance(16 * time.Millisecond)
	}
	if _, ok := tt.Find(header); !ok {
		t.Fatalf("header missing: %q", tt.Texts())
	}
	var log strings.Builder
	for _, phase := range []string{"collapse", "expand"} {
		if err := tt.Click(header); err != nil {
			t.Fatal(err)
		}
		var ys []float32
		for frame := 0; frame < 20; frame++ {
			tt.Advance(16 * time.Millisecond)
			r, ok := tt.Find(reply)
			if !ok || r.H < 1 {
				t.Fatalf("%s frame %d lost the reply", phase, frame)
			}
			ys = append(ys, r.Y)
			if frame == 0 || frame == 4 || frame == 9 || frame == 14 || frame == 19 {
				writePNG(t, filepath.Join(dir, fmt.Sprintf("%s-%02d.png", phase, frame)), tt.Image())
			}
		}
		fmt.Fprintf(&log, "%s reply-y:", phase)
		for _, y := range ys {
			fmt.Fprintf(&log, " %.1f", y)
		}
		log.WriteByte('\n')
		last := ys[len(ys)-1] - ys[len(ys)-2]
		if last < -0.6 || last > 0.6 {
			t.Fatalf("%s last frame jumped by %.2f: %v", phase, last, ys)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "frames.txt"), []byte(log.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Log(log.String())
}
