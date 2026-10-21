package main

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestNewSessionButtonAndShortcut(t *testing.T) {
	a := revisionChat(t)
	a.menu()
	if a.newSessionItem == nil || a.newSessionItem.Accelerator != "Shift+CmdOrCtrl+N" || a.newSessionItem.Label != "New Session" {
		t.Fatalf("menu item %+v", a.newSessionItem)
	}
	before := len(a.data.Conversations)
	tt := ui.NewTester(a.view, 1120, 760)
	tt.Frame()
	if !a.newSessionItem.IsEnabled() {
		t.Fatal("menu item was disabled on a chat with messages")
	}
	r, ok := tt.Find("New Session")
	if !ok {
		t.Fatalf("button missing: %q", tt.Texts())
	}
	if r.W < 23 || r.W > 25 || r.H < 23 || r.H > 25 {
		t.Fatalf("button size %.1fx%.1f, want 24x24", r.W, r.H)
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Advance(700 * time.Millisecond)
	tt.Frame()
	if !tt.HasText("⇧⌘N") {
		t.Fatalf("tooltip missing key: %q", tt.Texts())
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyN)
	tt.Frame()
	conv := a.active()
	if len(conv.Breaks) != 1 || conv.Breaks[0].Kind != sessionManual {
		t.Fatalf("shortcut break %+v", conv.Breaks)
	}
	if len(a.data.Conversations) != before {
		t.Fatal("⇧⌘N opened a new chat")
	}
	if !tt.HasText("New Session") {
		t.Fatal("divider missing")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyN)
	tt.Frame()
	if len(a.active().Breaks) != 1 || len(a.data.Conversations) != before {
		t.Fatalf("second shortcut changed the chat: breaks %d convs %d", len(a.active().Breaks), len(a.data.Conversations))
	}
	if !tt.HasText("Remove New Session") {
		t.Fatal("empty divider has no remove button")
	}
	if err := tt.Click("Remove New Session"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.active().Breaks) != 0 || tt.HasText("Remove New Session") {
		t.Fatalf("remove left %+v", a.active().Breaks)
	}
}

func TestNewSessionDisabledReasons(t *testing.T) {
	a := newTestApp(t)
	a.menu()
	a.newChat()
	tt := ui.NewTester(a.view, 1120, 760)
	tt.Frame()
	if a.newSessionItem.IsEnabled() {
		t.Fatal("menu item stayed enabled on an empty chat")
	}
	before := len(a.data.Conversations)
	r, ok := tt.Find("New Session")
	if !ok {
		t.Fatal("empty chat hid the button")
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Advance(700 * time.Millisecond)
	tt.Frame()
	if !tt.HasText("Send a message first") {
		t.Fatalf("empty reason missing: %q", tt.Texts())
	}
	if err := tt.Click("New Session"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.active().Breaks) != 0 || len(a.data.Conversations) != before {
		t.Fatal("disabled button inserted a break or a chat")
	}

	b := revisionChat(t)
	b.streams = map[string]*convStream{b.active().ID: {cancel: func() {}}}
	tb := ui.NewTester(b.view, 1120, 760)
	tb.Frame()
	r, ok = tb.Find("New Session")
	if !ok {
		t.Fatal("streaming chat hid the button")
	}
	tb.Move(r.X+r.W/2, r.Y+r.H/2)
	tb.Advance(700 * time.Millisecond)
	tb.Frame()
	if !tb.HasText("Available when the reply finishes") {
		t.Fatalf("streaming reason missing: %q", tb.Texts())
	}
	if err := tb.Click("New Session"); err != nil {
		t.Fatal(err)
	}
	tb.Frame()
	if len(b.active().Breaks) != 0 {
		t.Fatal("streaming insert was accepted")
	}
}

func TestNewSessionSearchStaysCommandK(t *testing.T) {
	a := revisionChat(t)
	tt := ui.NewTester(a.view, 1120, 760)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Frame()
	if !tt.Focused("Search") {
		t.Fatal("⌘K did not focus Search")
	}
	if len(a.active().Breaks) != 0 {
		t.Fatal("⌘K inserted a session break")
	}
	a.settingsOpen = true
	a.settingsSection = "shortcuts"
	tt.Frame()
	blob := strings.Join(tt.Texts(), "\n")
	for _, want := range []string{"New Session", "Search Chats", "⌘", "K", "⇧", "N"} {
		if !strings.Contains(blob, want) {
			t.Fatalf("shortcuts missing %q:\n%s", want, blob)
		}
	}
}

func TestNewSessionDividerRules(t *testing.T) {
	a := revisionChat(t)
	conv := a.active()
	conv.Breaks = []SessionBreak{{
		ID: "mid", Kind: sessionManual, CreatedAt: time.Now(), After: 2,
	}}
	a.editMessage(conv.ID, conv.Messages[0].ID)
	tt := ui.NewTester(a.view, 1120, 760)
	tt.Frame()
	if !tt.HasText("Replaces 3 later messages") {
		t.Fatalf("hint counted the divider: %q", tt.Texts())
	}
	if !tt.HasText("New Session") {
		t.Fatal("divider hidden while editing")
	}
	if tt.HasText("Remove New Session") {
		t.Fatal("a divider with later messages offered remove")
	}

	b := revisionChat(t)
	b.active().Breaks = []SessionBreak{{
		ID: "auto", Kind: sessionAuto, Limit: 1, CreatedAt: time.Now(), After: 2,
	}}
	b.shotPinBreak = true
	tb := ui.NewTester(b.view, 1120, 760)
	tb.Frame()
	if !tb.HasText("New Session · auto (1-round limit)") {
		t.Fatalf("auto label: %q", tb.Texts())
	}
	b.active().Breaks[0].Limit = 20
	tb.Frame()
	if !tb.HasText("New Session · auto (20-round limit)") {
		t.Fatalf("auto label: %q", tb.Texts())
	}
}

func TestMaxContextControls(t *testing.T) {
	a := revisionChat(t)
	a.settingsOpen = true
	a.settingsSection = "general"
	tt := ui.NewTester(a.view, 1120, 760)
	tt.Frame()
	if !tt.HasText("Max context") || !tt.HasText("Unlimited") {
		t.Fatalf("general control: %q", tt.Texts())
	}
	if tt.HasText("rounds") {
		t.Fatal("unlimited general row showed the number")
	}
	if err := tt.Click("Unlimited"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Limit to"); err != nil {
		t.Fatalf("limit option: %v texts %q", err, tt.Texts())
	}
	tt.Frame()
	if a.data.MaxRounds != 20 {
		t.Fatalf("first Limit to stored %d", a.data.MaxRounds)
	}
	if !tt.HasText("rounds") || !tt.HasText("Rounds per session") {
		t.Fatalf("limit field: %q", tt.Texts())
	}
	prev := a.active().MaxRounds
	if prev != 0 {
		t.Fatal("existing chat picked up the default")
	}
	a.newChat()
	if a.active().MaxRounds != 20 {
		t.Fatalf("new chat copied %d", a.active().MaxRounds)
	}

	b := revisionChat(t)
	b.active().MaxRounds = 4
	b.openSettingsTab(0)
	if b.formRounds != 4 {
		t.Fatalf("sheet opened at %d", b.formRounds)
	}
	tb := ui.NewTester(b.view, 1120, 760)
	tb.Frame()
	if !tb.HasText("Question + answer = 1 round") || !tb.HasText("Limit to") {
		t.Fatalf("sheet: %q", tb.Texts())
	}
	if err := tb.Click("Use Defaults"); err != nil {
		t.Fatal(err)
	}
	tb.Frame()
	if b.formRounds != 0 {
		t.Fatalf("use defaults left %d", b.formRounds)
	}
	if b.active().MaxRounds != 4 {
		t.Fatal("use defaults wrote the chat before Save")
	}
	if err := tb.Click("Unlimited"); err != nil {
		t.Fatal(err)
	}
	tb.Frame()
	if err := tb.Click("Limit to"); err != nil {
		t.Fatalf("sheet limit option: %v texts %q", err, tb.Texts())
	}
	tb.Frame()
	// The sheet already showed 4, so Limit to restores that number.
	if b.formRounds != 4 || b.active().MaxRounds != 4 {
		t.Fatalf("sheet recalled form %d chat %d", b.formRounds, b.active().MaxRounds)
	}

	c := revisionChat(t)
	c.openSettingsTab(0)
	if c.formRounds != 0 || c.active().MaxRounds != 0 {
		t.Fatalf("fresh sheet form %d chat %d", c.formRounds, c.active().MaxRounds)
	}
	tc := ui.NewTester(c.view, 1120, 760)
	tc.Frame()
	if err := tc.Click("Unlimited"); err != nil {
		t.Fatal(err)
	}
	tc.Frame()
	if err := tc.Click("Limit to"); err != nil {
		t.Fatalf("fresh limit option: %v texts %q", err, tc.Texts())
	}
	tc.Frame()
	if c.formRounds != 20 || c.active().MaxRounds != 0 {
		t.Fatalf("first sheet limit form %d chat %d", c.formRounds, c.active().MaxRounds)
	}
	if err := tc.Click("Save"); err != nil {
		t.Fatal(err)
	}
	tc.Frame()
	if c.active().MaxRounds != 20 {
		t.Fatalf("save stored %d", c.active().MaxRounds)
	}
}

func TestNewSessionNoOverflowAtMinimum(t *testing.T) {
	a := revisionChat(t)
	a.active().Breaks = []SessionBreak{{
		ID: "b", Kind: sessionAuto, Limit: 20, CreatedAt: time.Now(), After: 2,
	}}
	a.shotPinBreak = true
	tt := ui.NewTester(a.view, 720, 520)
	tt.Frame()
	assertInside(t, tt, 720, "New Session", "New Session · auto (20-round limit)")
	a.settingsOpen = true
	a.settingsSection = "general"
	a.data.MaxRounds = 20
	tt.Frame()
	assertInside(t, tt, 720, "Max context", "Limit to", "rounds")
}

func assertInside(t *testing.T, tt *ui.Tester, w float32, labels ...string) {
	t.Helper()
	for _, label := range labels {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("missing %q: %q", label, tt.Texts())
		}
		if r.X < -1 || r.X+r.W > w+1 {
			t.Fatalf("%q overflows: x %.1f w %.1f window %.0f", label, r.X, r.W, w)
		}
	}
}
