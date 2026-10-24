package main

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cove/themes"

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

func TestEditResendFocusesComposer(t *testing.T) {
	a, tt := focusChats(t)
	if err := tt.Click("Edit"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.edit.convID == "" {
		t.Fatal("edit did not open")
	}
	if err := tt.Click("Send edit"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.edit.convID != "" {
		t.Fatal("Save & Resend left edit mode on")
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
	for i := 0; i < 3; i++ {
		tt.Key(0, ui.KeyLeft)
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
	if back.X+1 < caret.X-2 || back.X > caret.X+2 {
		t.Fatalf("mid-text caret moved to %.1f from %.1f", back.X, caret.X)
	}
	tt.Type("X")
	if a.active().Draft != "hello woXrld" {
		t.Fatalf("caret was not mid-text: %q", a.active().Draft)
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

func TestNewSessionButtonFocusesComposer(t *testing.T) {
	a, tt := focusChats(t)
	if err := tt.Click("Search"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("New Session"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	requireComposer(t, a, tt)
	if a.active() == nil || len(a.active().Breaks) == 0 || a.active().Breaks[len(a.active().Breaks)-1].Kind != sessionManual {
		t.Fatal("New Session button did not insert a session break")
	}
}

func TestNewSessionMenuFocusesComposer(t *testing.T) {
	a, tt := focusChats(t)
	a.menu()
	tt.Frame()
	if a.newSessionItem == nil || !a.newSessionItem.IsEnabled() {
		t.Fatal("Chat > New Session is disabled")
	}
	if err := tt.Click("Search"); err != nil {
		t.Fatal(err)
	}
	a.newSessionItem.Click(a.newSessionItem, nil)
	tt.Frame()
	requireComposer(t, a, tt)
	if a.active() == nil || len(a.active().Breaks) == 0 || a.active().Breaks[len(a.active().Breaks)-1].Kind != sessionManual {
		t.Fatal("Chat > New Session did not insert a session break")
	}
}

func TestStopClickFocusesComposer(t *testing.T) {
	a, tt := focusChats(t)
	armStream(t, a)
	tt.Frame()
	if err := tt.Click("Search"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Stop"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	requireComposer(t, a, tt)
	if a.isStreaming(a.active().ID) {
		t.Fatal("Stop left the reply running")
	}
}

func TestRegenerateShortcutFocusesComposer(t *testing.T) {
	a, tt := focusChats(t)
	last := a.active().Messages[len(a.active().Messages)-1].ID
	if err := tt.Click("Search"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyR)
	tt.Frame()
	requireComposer(t, a, tt)
	msgs := a.active().Messages
	if len(msgs) == 0 || msgs[len(msgs)-1].ID == last {
		t.Fatal("⌘R did not start a new reply")
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
	dir := testArtifactDir(t, "focus-rings")
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
		conv := a.active()
		conv.Config.Model = "o3-mini"
		conv.Messages[1].Content = "ans\n\n```\nfmt.Println(1)\n```\n"
		conv.Messages[1].Reasoning = "weighed it"
		conv.Messages[1].ThinkingMS = 12000
		tt.SetDark(dark)
		tt.Frame()
		focusDraft(t, tt)
		tt.Key(ui.Shift, ui.KeyTab)
		requireSidebar(t, a, "Focus")
		writeCrop(t, dir, "sidebar-"+suffix+".png", tt, "Focus", chatRowRect(t, tt, "Focus"), 12)
		save("sidebar-window-"+suffix+".png", tt)

		tt.Key(ui.Shift, ui.KeyTab)
		if a.settled.kind != focusSearch {
			t.Fatalf("search kind %d", a.settled.kind)
		}
		cropSearchRing(t, dir, "search-"+suffix+".png", tt)

		enableFullKeyboard(tt)
		focusDraft(t, tt)
		tt.Type("hi")
		if !focusUntil(t, tt, "Model o3-mini", 6) {
			t.Fatal("model chip is not a tab stop")
		}
		crop("model-"+suffix+".png", tt, "Model o3-mini", 12)
		if !focusUntil(t, tt, "Reasoning: Default", 4) {
			t.Fatal("reasoning chip is not a tab stop")
		}
		crop("reasoning-"+suffix+".png", tt, "Reasoning: Default", 14)
		if !focusUntil(t, tt, "New Session", 4) {
			t.Fatal("New Session is not a tab stop")
		}
		crop("new-session-"+suffix+".png", tt, "New Session", 14)
		if !focusUntil(t, tt, "Send", 4) {
			t.Fatal("Send is not a tab stop")
		}
		crop("send-"+suffix+".png", tt, "Send", 14)
		if !focusUntil(t, tt, "Regenerate", 4) {
			t.Fatal("title-bar Regenerate is not a tab stop")
		}
		crop("regen-"+suffix+".png", tt, "Regenerate", 14)
		if !focusUntil(t, tt, "More", 4) {
			t.Fatal("More is not a tab stop")
		}
		crop("more-"+suffix+".png", tt, "More", 14)
		if !focusUntil(t, tt, "Copy", 8) {
			t.Fatal("Copy is not a tab stop")
		}
		crop("copy-"+suffix+".png", tt, "Copy", 14)
		if !focusUntil(t, tt, "Edit", 4) {
			t.Fatal("Edit is not a tab stop")
		}
		crop("edit-"+suffix+".png", tt, "Edit", 14)
		if !focusUntil(t, tt, "Thought for 12s", 6) {
			t.Fatal("thinking disclosure is not a tab stop")
		}
		cropWider(t, dir, "thinking-"+suffix+".png", tt, "Thought for 12s", 14, 40)
		if !focusUntil(t, tt, "Copy code", 8) {
			t.Fatal("code-block Copy is not a tab stop")
		}
		crop("code-copy-"+suffix+".png", tt, "Copy code", 16)
		if block, ok := tt.Find("code block"); ok {
			corner := ui.Rect{X: block.X - 10, Y: block.Y - 10, W: 44, H: 44}
			writeCrop(t, dir, "code-corner-"+suffix+".png", tt, "code block", corner, 0)
		} else {
			t.Fatal("code block missing for the corner crop")
		}

		tt.SetFocused(false)
		tt.Frame()
		save("inactive-"+suffix+".png", tt)

		shotUnsupportedReasoning(t, dir, suffix, dark)
		shotSettingsRings(t, dir, suffix, dark)
		shotQuoteRing(t, dir, suffix)
		shotDialogRing(t, dir, suffix)
		shotContrastRings(t, dir, suffix, dark)
		shotExtraRings(t, dir, suffix, dark)
	}
	shotContrastAccent(t, dir, "contrast-sidebar-orange-light.png", false, "orange")
	shotContrastAccent(t, dir, "contrast-sidebar-green-light.png", false, "green")
	shotContrastAccent(t, dir, "contrast-sidebar-purple-dark.png", true, "purple")
	writeFocusGallery(t, dir)
}

func focusUntil(t *testing.T, tt *ui.Tester, label string, n int) bool {
	t.Helper()
	for i := 0; i < n; i++ {
		if tt.Focused(label) {
			return true
		}
		tt.Key(0, ui.KeyTab)
	}
	return tt.Focused(label)
}

func cropLabeled(t *testing.T, dir, name string, tt *ui.Tester, label string, pad, maxH, maxW float32) {
	t.Helper()
	cropPick(t, dir, name, tt, label, pad, maxH, maxW, false)
}

func cropLargest(t *testing.T, dir, name string, tt *ui.Tester, label string, pad, maxH, maxW float32) {
	t.Helper()
	cropPick(t, dir, name, tt, label, pad, maxH, maxW, true)
}

func cropPick(t *testing.T, dir, name string, tt *ui.Tester, label string, pad, maxH, maxW float32, largest bool) {
	t.Helper()
	if r, ok := tt.FocusedRect(label); ok && (maxH <= 0 || r.H <= maxH) && (maxW <= 0 || r.W <= maxW) {
		writeCrop(t, dir, name, tt, label, r, pad)
		return
	}
	var best ui.Rect
	ok := false
	for _, r := range tt.FindAll(label) {
		if maxH > 0 && r.H > maxH {
			continue
		}
		if maxW > 0 && r.W > maxW {
			continue
		}
		if !ok || (largest && r.W*r.H > best.W*best.H) || (!largest && r.W*r.H < best.W*best.H) {
			best, ok = r, true
		}
	}
	if !ok {
		r, found := tt.Find(label)
		if !found {
			t.Fatalf("missing %q for %s", label, name)
		}
		best = r
	}
	writeCrop(t, dir, name, tt, label, best, pad)
}

func bringIntoView(t *testing.T, a *App, tt *ui.Tester, label string) {
	t.Helper()
	for i := 0; i < 30; i++ {
		r, ok := tt.FocusedRect(label)
		if !ok || r.H < 1 || r.W < 1 {
			for _, cand := range tt.FindAll(label) {
				if cand.H > r.H {
					r, ok = cand, true
				}
			}
		}
		if ok && r.H > 1 && r.W > 1 && r.Y >= 60 && r.Y+r.H <= 730 {
			return
		}
		if ok && r.Y < 60 && a.formScroll.Y > 0 {
			a.formScroll.Y -= 64
			if a.formScroll.Y < 0 {
				a.formScroll.Y = 0
			}
		} else {
			a.formScroll.Y += 64
		}
		tt.Frame()
	}
}

func chatRowRect(t *testing.T, tt *ui.Tester, title string) ui.Rect {
	t.Helper()
	var best ui.Rect
	ok := false
	consider := func(r ui.Rect) {
		if r.X > sidebarW || r.H < 16 || r.H > 40 || r.W < 8 {
			return
		}
		if !ok || r.W > best.W {
			best, ok = r, true
		}
	}
	for _, r := range tt.FindAll(title) {
		consider(r)
	}
	prefix := title + ", "
	for _, text := range tt.Texts() {
		if strings.HasPrefix(text, prefix) {
			for _, r := range tt.FindAll(text) {
				consider(r)
			}
		}
	}
	if !ok {
		t.Fatalf("no sidebar row for %q", title)
	}
	return best
}

func navRowRect(t *testing.T, tt *ui.Tester, title string) ui.Rect {
	t.Helper()
	var best ui.Rect
	ok := false
	for _, r := range tt.FindAll(title) {
		if r.X >= sidebarNarrow || r.H < 16 || r.H > 40 {
			continue
		}
		if !ok || r.W > best.W {
			best, ok = r, true
		}
	}
	if !ok {
		t.Fatalf("no settings nav row for %q", title)
	}
	return best
}

func cropSearchRing(t *testing.T, dir, name string, tt *ui.Tester) {
	t.Helper()
	r, ok := tt.FocusedRect("Search")
	if !ok {
		r, ok = tt.Find("Search")
	}
	if !ok {
		t.Fatalf("missing search for %s", name)
	}
	// The ring hugs the capsule around the field, inset 10 from the sidebar.
	row := ui.Rect{X: 10, Y: r.Y - (rowHeight-r.H)/2, W: sidebarW - 20, H: rowHeight}
	if row.Y < 0 {
		row.Y = 0
	}
	writeCrop(t, dir, name, tt, "Search", row, 10)
}

func cropWider(t *testing.T, dir, name string, tt *ui.Tester, label string, pad, maxH float32) {
	t.Helper()
	var best ui.Rect
	ok := false
	for _, r := range tt.FindAll(label) {
		if maxH > 0 && r.H > maxH {
			continue
		}
		if !ok || r.W > best.W {
			best, ok = r, true
		}
	}
	if !ok {
		t.Fatalf("missing %q for %s", label, name)
	}
	writeCrop(t, dir, name, tt, label, best, pad)
}

func writeCrop(t *testing.T, dir, name string, tt *ui.Tester, label string, r ui.Rect, pad float32) {
	t.Helper()
	img := focusCrop(tt.Image(), r, pad, 1)
	b := img.Bounds()
	if b.Dx() < 2 || b.Dy() < 2 {
		t.Fatalf("%s crop of %q is empty at %+v", name, label, r)
	}
	writeFocusPNG(t, filepath.Join(dir, name), img)
}

func shotUnsupportedReasoning(t *testing.T, dir, suffix string, dark bool) {
	t.Helper()
	_, tt := focusChats(t)
	tt.SetDark(dark)
	tt.Frame()
	chip, ok := tt.Find("Reasoning: Default")
	if !ok {
		t.Fatal("unsupported reasoning chip")
	}
	// A keyboard tip from the same instant wins the tie. Step the clock
	// so this hover is the later one, then rest past the tooltip delay.
	tt.Advance(time.Millisecond)
	tt.Move(chip.X+chip.W/2, chip.Y+chip.H/2)
	tt.Advance(1500 * time.Millisecond)
	tt.Frame()
	const tip = "This model doesn't support reasoning effort."
	if !tt.HasText(tip) {
		t.Fatalf("unsupported reasoning tooltip missing: %q", tt.Texts())
	}
	tipR, ok := tt.Find(tip)
	if !ok {
		t.Fatalf("tooltip rect missing: %q", tt.Texts())
	}
	chip, ok = tt.Find("Reasoning: Default")
	if !ok {
		t.Fatal("unsupported reasoning chip left after the tooltip")
	}
	writeCrop(t, dir, "reasoning-unsupported-"+suffix+".png", tt, tip, unionRect(chip, tipR), 16)
}

func shotSettingsRings(t *testing.T, dir, suffix string, dark bool) {
	t.Helper()
	a, tt := focusChats(t)
	// The temperature slider is disabled until a value is sent. Turn it on
	// so the knob is a tab stop and the ring shot shows the thumb.
	temp := 1.2
	a.data.Defaults.Temperature = &temp
	a.prefReady = false
	tt.SetDark(dark)
	tt.Frame()
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	enableFullKeyboard(tt)
	if !focusUntil(t, tt, "Search settings", 4) {
		t.Fatal("settings search is not a tab stop")
	}
	cropLabeled(t, dir, "settings-field-"+suffix+".png", tt, "Search settings", 10, 28, 0)
	if !focusUntil(t, tt, "General", 4) {
		t.Fatal("settings nav is not a tab stop")
	}
	nav := navRowRect(t, tt, "General")
	writeCrop(t, dir, "settings-nav-"+suffix+".png", tt, "General", nav, 12)
	tt.ClickAt(nav.X+nav.W/2, nav.Y+nav.H/2)
	if a.settingsNavKey {
		t.Fatal("click left the ring on the settings nav")
	}
	writeCrop(t, dir, "settings-nav-click-"+suffix+".png", tt, "General", navRowRect(t, tt, "General"), 12)
	if !focusUntil(t, tt, "Temperature", 8) {
		t.Fatal("slider is not a tab stop")
	}
	cropLabeled(t, dir, "slider-"+suffix+".png", tt, "Temperature", 12, 20, 0)
	if !focusUntil(t, tt, "Max tokens", 6) {
		t.Fatal("number field is not a tab stop")
	}
	cropLabeled(t, dir, "number-"+suffix+".png", tt, "Max tokens", 10, 28, 100)
	if !focusUntil(t, tt, "Increase", 4) {
		t.Fatal("stepper is not a tab stop")
	}
	cropLabeled(t, dir, "stepper-"+suffix+".png", tt, "Increase", 14, 16, 0)
	if !focusUntil(t, tt, "Send with control", 8) {
		t.Fatal("segmented control is not a tab stop")
	}
	cropLabeled(t, dir, "segmented-"+suffix+".png", tt, "Send with control", 10, 0, 0)
	if !focusUntil(t, tt, "Show reasoning while generating", 6) {
		t.Fatal("switch is not a tab stop")
	}
	cropLabeled(t, dir, "settings-switch-"+suffix+".png", tt, "Show reasoning while generating", 10, 22, 40)
	if !focusUntil(t, tt, titleToggleLabel, 6) {
		t.Fatal("title switch is not a tab stop")
	}
	bringIntoView(t, a, tt, titleToggleLabel)
	cropLabeled(t, dir, "title-switch-"+suffix+".png", tt, titleToggleLabel, 10, 22, 40)
	titleModel := "Title model, " + titleSameAsChat
	if !focusUntil(t, tt, titleModel, 4) {
		t.Fatal("title model is not a tab stop")
	}
	bringIntoView(t, a, tt, titleModel)
	cropLabeled(t, dir, "title-model-"+suffix+".png", tt, titleModel, 10, 30, 0)
	if !focusUntil(t, tt, "View acknowledgements", 8) {
		t.Fatal("link is not a tab stop")
	}
	bringIntoView(t, a, tt, "View acknowledgements")
	cropLabeled(t, dir, "link-"+suffix+".png", tt, "View acknowledgements", 8, 0, 0)

	if err := tt.Click("Appearance"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !focusUntil(t, tt, "Warm", 8) {
		t.Fatal("swatch is not a tab stop")
	}
	cropLabeled(t, dir, "swatch-"+suffix+".png", tt, "Warm", 12, 0, 0)

	if err := tt.Click("Models & Providers"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !focusUntil(t, tt, "OpenAI", 8) {
		t.Fatal("provider row is not a tab stop")
	}
	cropLabeled(t, dir, "provider-"+suffix+".png", tt, "OpenAI", 8, 0, 0)
	if err := tt.Click("OpenAI"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !focusUntil(t, tt, "Back", 6) {
		t.Fatal("back button is not a tab stop")
	}
	cropLabeled(t, dir, "nav-back-"+suffix+".png", tt, "Back", 10, 0, 0)
}

func shotContrastRings(t *testing.T, dir, suffix string, dark bool) {
	t.Helper()
	a, tt := focusChats(t)
	tt.SetDark(dark)
	tt.Frame()
	tt.SetPreferences(ui.Preferences{FullKeyboardAccess: true, HighContrast: true, TextScale: 1})
	focusDraft(t, tt)
	tt.Type("hi")
	tt.Key(ui.Shift, ui.KeyTab)
	requireSidebar(t, a, "Focus")
	writeCrop(t, dir, "contrast-sidebar-"+suffix+".png", tt, "Focus", chatRowRect(t, tt, "Focus"), 12)
	if !focusUntil(t, tt, "Regenerate", 12) {
		t.Fatal("contrast Regenerate is not a tab stop")
	}
	cropLabeled(t, dir, "contrast-button-"+suffix+".png", tt, "Regenerate", 14, 48, 48)
	tt.Key(ui.Shift, ui.KeyTab)
	for i := 0; i < 6 && !tt.Focused("Search"); i++ {
		tt.Key(ui.Shift, ui.KeyTab)
	}
	if !tt.Focused("Search") {
		t.Fatal("contrast search field")
	}
	cropSearchRing(t, dir, "contrast-field-"+suffix+".png", tt)
}

func shotExtraRings(t *testing.T, dir, suffix string, dark bool) {
	t.Helper()
	a, tt := focusChats(t)
	a.active().Messages[1].Content = "ans\n\n```\nfmt.Println(1)\n```\n"
	tt.SetDark(dark)
	tt.Frame()
	enableFullKeyboard(tt)
	focusDraft(t, tt)
	if !focusUntil(t, tt, "Copy", 16) {
		t.Fatal("Copy is not a tab stop")
	}
	btn, ok := tt.FocusedRect("Copy")
	if !ok {
		t.Fatal("Copy rect")
	}
	tt.ClickAt(btn.X+btn.W/2, btn.Y+btn.H/2)
	tt.Frame()
	if a.settled.visible {
		t.Fatal("click left a ring on Copy")
	}
	writeCrop(t, dir, "copy-click-"+suffix+".png", tt, "Copy", btn, 14)

	focusDraft(t, tt)
	if !focusUntil(t, tt, "More", 16) {
		t.Fatal("More is not a tab stop")
	}
	tt.KeyDown(ui.KeyDown)
	tt.KeyUp(ui.KeyDown)
	tt.Frame()
	if !a.popupOpen() {
		t.Fatal("keyboard did not open More")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.popupOpen() || !tt.Focused("More") || !a.settled.visible {
		t.Fatalf("Esc menu open=%v more=%v ring=%v", a.popupOpen(), tt.Focused("More"), a.settled.visible)
	}
	cropLabeled(t, dir, "more-restore-"+suffix+".png", tt, "More", 14, 48, 48)

	tt.Key(ui.Cmd, ui.KeyF)
	tt.Frame()
	if !tt.Focused("Find in chat") {
		t.Fatal("find field")
	}
	cropLabeled(t, dir, "find-"+suffix+".png", tt, "Find in chat", 10, 36, 0)
	tt.Key(0, ui.KeyEscape)
	tt.Frame()

	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Rename"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.Focused("Name") {
		t.Fatal("rename field")
	}
	cropLabeled(t, dir, "rename-"+suffix+".png", tt, "Name", 10, 40, 0)
	tt.Key(0, ui.KeyEscape)
	tt.Frame()

	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Appearance"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	writeFocusPNG(t, filepath.Join(dir, "appearance-"+suffix+".png"), tt.Image())
}

func shotContrastAccent(t *testing.T, dir, name string, dark bool, accentID string) {
	t.Helper()
	accent, ok := themes.NamedAccent(accentID, dark)
	if !ok {
		t.Fatalf("accent %s", accentID)
	}
	want := themes.FocusContrastRing(accentID, dark)
	a, tt := focusChats(t)
	tt.SetDark(dark)
	tt.Frame()
	tt.SetPreferences(ui.Preferences{FullKeyboardAccess: true, HighContrast: true, TextScale: 1, Accent: accent})
	focusDraft(t, tt)
	tt.Key(ui.Shift, ui.KeyTab)
	requireSidebar(t, a, "Focus")
	row := chatRowRect(t, tt, "Focus")
	if !rectHasColor(tt, row, want, 18) {
		t.Fatalf("%s ring is not %v", name, want)
	}
	writeCrop(t, dir, name, tt, "Focus", row, 12)
}

func rectHasColor(tt *ui.Tester, r ui.Rect, want ui.Color, tol int) bool {
	target := color.RGBA{R: want.R, G: want.G, B: want.B, A: 255}
	x0, y0 := int(r.X), int(r.Y)
	x1, y1 := int(r.X+r.W), int(r.Y+r.H)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if rgbaDelta(sampleAt(tt, float32(x), float32(y)), target) <= tol {
				return true
			}
		}
	}
	return false
}

func shotQuoteRing(t *testing.T, dir, suffix string) {
	t.Helper()
	a, tt := focusChats(t)
	if strings.HasSuffix(suffix, "dark") {
		tt.SetDark(true)
		tt.Frame()
	}
	a.active().DraftQuotes = []Quote{{Text: "A quoted line", Src: "You"}}
	tt.Frame()
	enableFullKeyboard(tt)
	focusDraft(t, tt)
	// The quote chip sits just before the composer in the tab order.
	tt.Key(ui.Shift, ui.KeyTab)
	if !tt.Focused("Remove quote") {
		t.Fatal("quote close is not a tab stop")
	}
	cropLabeled(t, dir, "quote-close-"+suffix+".png", tt, "Remove quote", 10, 0, 0)
}

func shotDialogRing(t *testing.T, dir, suffix string) {
	t.Helper()
	a, tt := focusChats(t)
	if strings.HasSuffix(suffix, "dark") {
		tt.SetDark(true)
		tt.Frame()
	}
	tt.Key(ui.Shift, ui.KeyTab)
	requireSidebar(t, a, "Focus")
	tt.Key(ui.Cmd, ui.KeyBackspace)
	tt.Frame()
	if !tt.Focused("Cancel") {
		t.Fatal("delete dialog did not focus Cancel")
	}
	cropLabeled(t, dir, "dialog-cancel-"+suffix+".png", tt, "Cancel", 8, 0, 0)
}

func writeFocusGallery(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"light", "dark"} {
		var imgs []image.Image
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, "-"+suffix+".png") || strings.HasPrefix(name, "gallery-") {
				continue
			}
			f, err := os.Open(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			img, err := png.Decode(f)
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
			imgs = append(imgs, img)
		}
		if len(imgs) == 0 {
			t.Fatalf("no %s ring shots", suffix)
		}
		gap := 12
		w, h := 0, gap
		for _, img := range imgs {
			b := img.Bounds()
			if b.Dx() > w {
				w = b.Dx()
			}
			h += b.Dy() + gap
		}
		canvas := image.NewRGBA(image.Rect(0, 0, w+gap*2, h))
		draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.RGBA{246, 246, 247, 255}), image.Point{}, draw.Src)
		y := gap
		for _, img := range imgs {
			b := img.Bounds()
			draw.Draw(canvas, image.Rect(gap, y, gap+b.Dx(), y+b.Dy()), img, b.Min, draw.Over)
			y += b.Dy() + gap
		}
		writeFocusPNG(t, filepath.Join(dir, "gallery-"+suffix+".png"), canvas)
	}
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
