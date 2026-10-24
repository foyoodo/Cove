package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func focusChats(t *testing.T) (*App, *ui.Tester) {
	t.Helper()
	a, tt := focusSendApp(t)
	now := time.Now()
	cfg := a.data.Defaults
	a.data.Conversations = append(a.data.Conversations,
		Conversation{ID: "c2", Title: "Alpha", CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour), Loaded: true, Config: cfg, Messages: []Message{userMessage("hi"), {ID: "a1", Role: roleAssistant, Content: "yo", Done: true}}},
		Conversation{ID: "c3", Title: "Beta", CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour), Loaded: true, Config: cfg, Messages: []Message{userMessage("hey"), {ID: "a2", Role: roleAssistant, Content: "sup", Done: true}}},
	)
	a.data.Conversations[0].Messages = []Message{userMessage("q"), {ID: "a0", Role: roleAssistant, Content: "ans", Done: true}}
	tt.Frame()
	focusDraft(t, tt)
	tt.Frame()
	return a, tt
}

func requireComposer(t *testing.T, a *App, tt *ui.Tester) {
	t.Helper()
	if a.settled.kind != focusComposer || !tt.Focused("Message") {
		t.Fatalf("composer focus kind %d focused %v", a.settled.kind, tt.Focused("Message"))
	}
}

func requireSidebar(t *testing.T, a *App, title string) {
	t.Helper()
	if a.settled.kind != focusSidebar {
		t.Fatalf("sidebar focus kind %d active %q", a.settled.kind, a.active().Title)
	}
	if a.active() == nil || a.active().Title != title {
		t.Fatalf("sidebar chat %q, want %q", a.active().Title, title)
	}
}

func enableFullKeyboard(tt *ui.Tester) {
	tt.SetPreferences(ui.Preferences{FullKeyboardAccess: true, TextScale: 1})
	tt.Frame()
}

func TestLaunchFocusesComposer(t *testing.T) {
	now := time.Now()
	a := newTestApp(t)
	a.data.Conversations = []Conversation{chatWith("x", "Alpha", "", []Message{userMessage("one")}, now)}
	a.data.ActiveID = "x"
	tt := openTester(a)
	tt.Frame()
	requireComposer(t, a, tt)

	a = newTestApp(t)
	a.data.Conversations = []Conversation{chatWith("x", newChatTitle, "", nil, now)}
	a.data.ActiveID = "x"
	tt = openTester(a)
	tt.Frame()
	requireComposer(t, a, tt)

	a = newTestApp(t)
	a.data.Conversations = nil
	a.data.ActiveID = ""
	tt = openTester(a)
	tt.Frame()
	if a.active() != nil {
		t.Fatalf("empty store grew a chat %q", a.active().Title)
	}
	if tt.Focused("Message") || a.settled.kind == focusComposer {
		t.Fatal("an empty store focused a composer")
	}
}

func TestSidebarClickOpensComposer(t *testing.T) {
	a, tt := focusChats(t)
	if err := tt.Click("Alpha"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	requireComposer(t, a, tt)
	if a.active().Title != "Alpha" {
		t.Fatalf("active %q", a.active().Title)
	}
	tt.Type("zz")
	if a.active().Draft != "zz" {
		t.Fatalf("draft %q", a.active().Draft)
	}
}

func TestSidebarKeyboardStaysInList(t *testing.T) {
	a, tt := focusChats(t)
	tt.Key(ui.Shift, ui.KeyTab)
	requireSidebar(t, a, "Focus")
	tt.Key(0, ui.KeyDown)
	requireSidebar(t, a, "Alpha")
	tt.Key(0, ui.KeyDown)
	requireSidebar(t, a, "Beta")
	tt.Key(0, ui.KeyUp)
	requireSidebar(t, a, "Alpha")
	tt.Key(0, ui.KeyEnter)
	requireComposer(t, a, tt)
	if a.active().Title != "Alpha" {
		t.Fatalf("enter left %q", a.active().Title)
	}
	tt.Key(ui.Shift, ui.KeyTab)
	requireSidebar(t, a, "Alpha")
	tt.Key(0, ui.KeyTab)
	requireComposer(t, a, tt)
}

func TestNewChatAndEmptyDeleteFocus(t *testing.T) {
	a, tt := focusChats(t)
	tt.Key(ui.Cmd, ui.KeyN)
	if a.active() == nil || len(a.active().Messages) != 0 {
		t.Fatal("⌘N did not open an empty draft")
	}
	requireComposer(t, a, tt)
	emptyID := a.active().ID
	tt.Key(ui.Cmd, ui.KeyBackspace)
	tt.Frame()
	if a.conversation(emptyID) != nil {
		t.Fatal("empty draft survived ⌘⌫")
	}
	if a.confirmKind != "" {
		t.Fatal("empty draft asked to confirm")
	}
	requireComposer(t, a, tt)
	if a.active().Title != "Focus" {
		t.Fatalf("adjacent chat %q", a.active().Title)
	}
}

func TestDeleteSelectsOlderChat(t *testing.T) {
	a, tt := focusChats(t)
	if a.active().Title != "Focus" {
		t.Fatalf("start %q", a.active().Title)
	}
	tt.Key(ui.Cmd, ui.KeyBackspace)
	if a.confirmKind != confirmDelete || !tt.Focused("Cancel") {
		t.Fatalf("confirm %q cancel %v", a.confirmKind, tt.Focused("Cancel"))
	}
	tt.Key(ui.Cmd, ui.KeyBackspace)
	tt.Frame()
	if a.conversation("c1") != nil {
		t.Fatal("Focus chat survived delete")
	}
	if a.active() == nil || a.active().Title != "Alpha" {
		t.Fatalf("active after delete %v", a.active())
	}
	requireComposer(t, a, tt)
}

func TestSidebarDeleteStaysOnAdjacentRow(t *testing.T) {
	a, tt := focusChats(t)
	tt.Key(ui.Shift, ui.KeyTab)
	requireSidebar(t, a, "Focus")
	tt.Key(ui.Cmd, ui.KeyBackspace)
	if a.confirmKind != confirmDelete {
		t.Fatal("sidebar delete skipped the dialog")
	}
	tt.Key(ui.Cmd, ui.KeyBackspace)
	tt.Frame()
	requireSidebar(t, a, "Alpha")
}

func TestMouseMenuRestoresComposer(t *testing.T) {
	a, tt := focusChats(t)
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	requireComposer(t, a, tt)

	label := "Model gpt-4o-mini"
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	requireComposer(t, a, tt)
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	requireComposer(t, a, tt)
}

func TestKeyboardMenuRestoresChip(t *testing.T) {
	a, tt := focusChats(t)
	enableFullKeyboard(tt)
	tt.Key(0, ui.KeyTab)
	if !tt.Focused("Model gpt-4o-mini") {
		t.Fatalf("tab did not reach the model chip, kind %d", a.settled.kind)
	}
	tt.Key(0, ui.KeySpace)
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if !tt.Focused("Model gpt-4o-mini") {
		t.Fatal("keyboard menu Esc did not return to the model chip")
	}
	if !a.settled.visible {
		t.Fatal("keyboard menu Esc returned without the focus ring")
	}
}

func TestRenameAndSettingsRestore(t *testing.T) {
	a, tt := focusChats(t)
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Rename"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.renameOpen || !tt.Focused("Name") {
		t.Fatalf("rename open %v name %v", a.renameOpen, tt.Focused("Name"))
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.renameOpen {
		t.Fatal("rename stayed open")
	}
	requireComposer(t, a, tt)

	tt.Key(ui.Shift, ui.KeyTab)
	requireSidebar(t, a, "Focus")
	tt.RightClick("Focus")
	tt.Frame()
	if err := tt.ChooseMenuItem("Rename"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.renameOpen {
		t.Fatal("context rename did not open")
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	requireSidebar(t, a, "Focus")

	tt.Key(0, ui.KeyTab)
	requireComposer(t, a, tt)
	tt.Key(ui.Cmd|ui.Alt, ui.KeyComma)
	if !a.modalOpen {
		t.Fatal("chat settings did not open")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	requireComposer(t, a, tt)
}

func TestEditCancelFocusesComposer(t *testing.T) {
	a, tt := focusChats(t)
	if err := tt.Click("Edit"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.edit.convID == "" {
		t.Fatal("edit did not open")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.edit.convID != "" {
		t.Fatal("edit stayed open")
	}
	requireComposer(t, a, tt)
}

func TestSearchCommitAndEscape(t *testing.T) {
	a, tt := focusChats(t)
	tt.Key(ui.Cmd, ui.KeyK)
	if a.settled.kind != focusSearch || !tt.Focused("Search") {
		t.Fatalf("⌘K kind %d", a.settled.kind)
	}
	tt.Type("alp")
	tt.Frame()
	if a.query != "alp" {
		t.Fatalf("query %q", a.query)
	}
	tt.Key(0, ui.KeyEscape)
	if a.query != "" {
		t.Fatalf("first Esc left %q", a.query)
	}
	if a.settled.kind != focusSearch || !tt.Focused("Search") {
		t.Fatalf("first Esc left search, kind %d", a.settled.kind)
	}
	tt.Key(0, ui.KeyEscape)
	requireComposer(t, a, tt)

	tt.Key(ui.Cmd, ui.KeyK)
	tt.Type("beta")
	tt.Key(0, ui.KeyEnter)
	if a.query != "" {
		t.Fatalf("enter left query %q", a.query)
	}
	if a.active().Title != "Beta" {
		t.Fatalf("enter opened %q", a.active().Title)
	}
	requireComposer(t, a, tt)

	tt.Key(ui.Cmd, ui.KeyK)
	tt.Key(0, ui.KeyDown)
	requireSidebar(t, a, "Beta")
}

func TestFindRestoresSelection(t *testing.T) {
	a := newTestApp(t)
	a.data.Conversations = []Conversation{quoteChat()}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	focusDraft(t, tt)
	dragSelect(t, tt, "Alpha message", "Alpha message")
	tt.Frame()
	if strings.TrimSpace(a.histSel.Text) == "" || a.settled.kind != focusMessage {
		t.Fatalf("selection %q kind %d", a.histSel.Text, a.settled.kind)
	}
	tt.Key(ui.Cmd, ui.KeyF)
	if !a.finder.open || !tt.Focused("Find in chat") {
		t.Fatalf("find open %v focused %v", a.finder.open, tt.Focused("Find in chat"))
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.finder.open {
		t.Fatal("find stayed open")
	}
	if !strings.Contains(a.histSel.Text, "Alpha message") {
		t.Fatalf("selection dropped %q", a.histSel.Text)
	}
	if tt.Focused("Message") || a.settled.kind == focusComposer {
		t.Fatal("find close moved to the composer")
	}
	tt.Key(ui.Cmd, ui.KeyC)
	if tt.Clipboard() != "Alpha message" {
		t.Fatalf("copy after find %q", tt.Clipboard())
	}

	focusDraft(t, tt)
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	requireComposer(t, a, tt)
}

func TestMessageSelectionHandoff(t *testing.T) {
	a := newTestApp(t)
	a.data.Conversations = []Conversation{quoteChat()}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	focusDraft(t, tt)
	tt.Type("keep")
	dragSelect(t, tt, "Alpha message", "Alpha message")
	tt.Frame()
	if !a.quoteBarOpen {
		t.Fatal("quote bar did not open")
	}
	tt.Key(0, ui.KeyEscape)
	if a.quoteBarOpen {
		t.Fatal("first Esc left the quote bar up")
	}
	if !strings.Contains(a.histSel.Text, "Alpha message") {
		t.Fatalf("first Esc cleared %q", a.histSel.Text)
	}
	if tt.Focused("Message") {
		t.Fatal("first Esc left the selection")
	}
	tt.Key(ui.Cmd, ui.KeyC)
	if tt.Clipboard() != "Alpha message" {
		t.Fatalf("copy %q", tt.Clipboard())
	}
	before := a.active().Draft
	tt.Key(0, ui.KeyN)
	if a.active().Draft != before {
		t.Fatalf("handoff wrote %q into draft %q", a.active().Draft, before)
	}
	requireComposer(t, a, tt)
	if active, off := tt.TextInputActive(); !active || off {
		t.Fatalf("composer input method active %v off %v", active, off)
	}

	dragSelect(t, tt, "Alpha message", "Alpha message")
	tt.Frame()
	tt.Key(0, ui.KeySpace)
	if tt.Focused("Message") || a.active().Draft != before {
		t.Fatalf("space moved focus or draft %q kind %d", a.active().Draft, a.settled.kind)
	}
	tt.Key(ui.Cmd, ui.KeyC)
	if !strings.Contains(tt.Clipboard(), "Alpha") {
		t.Fatalf("⌘C after space %q", tt.Clipboard())
	}
	tt.Key(0, ui.KeyEscape)
	if a.quoteBarOpen || a.settled.kind == focusMessage {
		tt.Key(0, ui.KeyEscape)
	}
	requireComposer(t, a, tt)
}

func TestCopyClickDoesNotTakeFocus(t *testing.T) {
	a, tt := focusChats(t)
	requireComposer(t, a, tt)
	if err := tt.Click("Copy"); err != nil {
		t.Fatal(err)
	}
	requireComposer(t, a, tt)
	if a.settled.visible {
		t.Fatal("clicking Copy showed a focus ring")
	}
}

func TestCopyRingOnlyFromKeyboard(t *testing.T) {
	a, tt := focusChats(t)
	enableFullKeyboard(tt)
	var reached bool
	for i := 0; i < 16; i++ {
		tt.Key(0, ui.KeyTab)
		if tt.Focused("Copy") {
			reached = true
			break
		}
	}
	if !reached {
		t.Fatal("Tab with Full Keyboard Access did not reach Copy")
	}
	if !a.settled.visible {
		t.Fatal("Tab to Copy did not mark the ring visible")
	}
	btn, ok := tt.Find("Copy")
	if !ok {
		t.Fatal("Copy has no rect")
	}
	ring := sampleAt(tt, btn.X+btn.W/2, btn.Y-1)
	bg := sampleAt(tt, btn.X+btn.W/2, btn.Y-8)
	if rgbaDelta(ring, bg) < 8 {
		t.Fatalf("Copy ring %v matches the background %v at %+v", ring, bg, btn)
	}
}

func TestEscapeDoesNotStopStreaming(t *testing.T) {
	a, tt := focusChats(t)
	armStream(t, a)
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	if !a.isStreaming("c1") {
		t.Fatal("Esc stopped the stream")
	}
	requireComposer(t, a, tt)
}

func TestPerChatCaretRestores(t *testing.T) {
	a, tt := focusChats(t)
	tt.Type("hello world")
	if a.active().Draft != "hello world" {
		t.Fatalf("draft %q", a.active().Draft)
	}
	caret, ok := tt.TextCaret()
	if !ok {
		t.Fatal("no caret after typing")
	}
	if err := tt.Click("Alpha"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	requireComposer(t, a, tt)
	if a.active().Draft != "" {
		t.Fatalf("alpha draft %q", a.active().Draft)
	}
	if err := tt.Click("Focus"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	requireComposer(t, a, tt)
	if a.active().Draft != "hello world" {
		t.Fatalf("restored draft %q", a.active().Draft)
	}
	back, ok := tt.TextCaret()
	if !ok {
		t.Fatal("no caret after switching back")
	}
	if back.X+1 < caret.X {
		t.Fatalf("caret moved to %.1f from %.1f", back.X, caret.X)
	}
	tt.Type("!")
	if a.active().Draft != "hello world!" {
		t.Fatalf("caret was not at the end: %q", a.active().Draft)
	}
}

func TestSettingsPaneKeepsNavFocus(t *testing.T) {
	a, tt := focusChats(t)
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.settingsOpen {
		t.Fatal("settings did not open")
	}
	if err := tt.Click("Appearance"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.settingsSection != "appearance" {
		t.Fatalf("section %q", a.settingsSection)
	}
	if a.settled.kind != focusSettingsNav {
		t.Fatalf("pane click kind %d", a.settled.kind)
	}
	tt.Key(0, ui.KeyDown)
	if a.settingsSection == "appearance" {
		t.Fatal("↓ did not change the settings pane")
	}
	if a.settled.kind != focusSettingsNav {
		t.Fatalf("↓ left the nav, kind %d section %q", a.settled.kind, a.settingsSection)
	}
}

func TestNewSessionShortcutFocusesComposer(t *testing.T) {
	a, tt := focusChats(t)
	if err := tt.Click("Search"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyN)
	tt.Frame()
	requireComposer(t, a, tt)
	if a.active() == nil || len(a.active().Breaks) == 0 || a.active().Breaks[len(a.active().Breaks)-1].Kind != sessionManual {
		t.Fatal("⇧⌘N did not insert a session break")
	}
}

func sampleAt(tt *ui.Tester, x, y float32) color.RGBA {
	img := tt.Image()
	b := img.Bounds()
	px, py := int(x+0.5), int(y+0.5)
	if px < b.Min.X {
		px = b.Min.X
	}
	if py < b.Min.Y {
		py = b.Min.Y
	}
	if px >= b.Max.X {
		px = b.Max.X - 1
	}
	if py >= b.Max.Y {
		py = b.Max.Y - 1
	}
	return img.RGBAAt(px, py)
}

func rgbaDelta(a, b color.RGBA) int {
	d := absInt(int(a.R) - int(b.R))
	if g := absInt(int(a.G) - int(b.G)); g > d {
		d = g
	}
	if bl := absInt(int(a.B) - int(b.B)); bl > d {
		d = bl
	}
	return d
}

func TestFocusRingShots(t *testing.T) {
	dir := filepath.Join("/opt/cursor/artifacts", "focus-rings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	save := func(name string, tt *ui.Tester) {
		t.Helper()
		writeFocusPNG(t, filepath.Join(dir, name), tt.Image())
	}
	crop := func(name string, tt *ui.Tester, label string, pad float32) {
		t.Helper()
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("missing %q for %s", label, name)
		}
		writeFocusPNG(t, filepath.Join(dir, name), focusCrop(tt.Image(), r, pad, 1))
	}

	for _, dark := range []bool{false, true} {
		suffix := "light"
		if dark {
			suffix = "dark"
		}
		a, tt := focusChats(t)
		tt.SetDark(dark)
		tt.Frame()
		focusDraft(t, tt)
		tt.Key(ui.Shift, ui.KeyTab)
		requireSidebar(t, a, "Focus")
		crop("sidebar-"+suffix+".png", tt, "Focus", 8)
		save("sidebar-window-"+suffix+".png", tt)

		tt.Key(ui.Shift, ui.KeyTab)
		if a.settled.kind != focusSearch {
			t.Fatalf("search kind %d", a.settled.kind)
		}
		crop("search-"+suffix+".png", tt, "Search", 10)

		enableFullKeyboard(tt)
		tt.Key(0, ui.KeyTab) // list
		tt.Key(0, ui.KeyTab) // composer, no ring
		tt.Key(0, ui.KeyTab) // model
		crop("model-"+suffix+".png", tt, "Model gpt-4o-mini", 10)
		tt.Key(0, ui.KeyTab) // effort, may be absent
		if !tt.Focused("Send") {
			tt.Key(0, ui.KeyTab)
		}
		if !tt.Focused("Send") {
			tt.Key(0, ui.KeyTab)
		}
		if tt.Focused("Send") {
			crop("send-"+suffix+".png", tt, "Send", 12)
		}
		for i := 0; i < 6 && !tt.Focused("More"); i++ {
			tt.Key(0, ui.KeyTab)
		}
		if tt.Focused("More") {
			crop("more-"+suffix+".png", tt, "More", 10)
		}
		for i := 0; i < 8 && !tt.Focused("Copy"); i++ {
			tt.Key(0, ui.KeyTab)
		}
		if tt.Focused("Copy") {
			crop("copy-"+suffix+".png", tt, "Copy", 12)
		}

		tt.SetFocused(false)
		tt.Frame()
		save("inactive-"+suffix+".png", tt)
	}

	_, tt := focusChats(t)
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("General"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	crop("settings-nav-light.png", tt, "General", 8)
	for i := 0; i < 40 && !tt.Focused("Show reasoning while generating"); i++ {
		tt.Key(0, ui.KeyTab)
	}
	if tt.Focused("Show reasoning while generating") {
		crop("settings-switch-light.png", tt, "Show reasoning while generating", 8)
	}
	if err := tt.Click("Models & Providers"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("OpenAI"); ok {
		for i := 0; i < 8 && !tt.Focused("OpenAI"); i++ {
			tt.Key(0, ui.KeyTab)
		}
		if tt.Focused("OpenAI") {
			crop("provider-light.png", tt, "OpenAI", 8)
		}
	}

	tt.SetDark(true)
	tt.Frame()
	if err := tt.Click("General"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	crop("settings-nav-dark.png", tt, "General", 8)
}

func focusCrop(img *image.RGBA, r ui.Rect, pad, scale float32) image.Image {
	b := img.Bounds()
	x0 := int((r.X - pad) * scale)
	y0 := int((r.Y - pad) * scale)
	x1 := int((r.X+r.W+pad)*scale + 0.5)
	y1 := int((r.Y+r.H+pad)*scale + 0.5)
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
	return cropImage(img, image.Rect(x0, y0, x1, y1))
}

func writeFocusPNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
