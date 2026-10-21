package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func blurThenActivate(tt *ui.Tester) {
	tt.SetFocused(false)
	tt.SetFocused(true)
}

func TestActivateRestoresComposerCaret(t *testing.T) {
	a, tt := focusSendApp(t)
	tt.Type("abcdef")
	tt.Key(0, ui.KeyLeft)
	tt.Key(0, ui.KeyLeft)
	tt.Key(ui.Shift, ui.KeyLeft)
	if !a.composerCaretSet || a.composerAnchor == a.composerCaretPos {
		t.Fatalf("selection not recorded anchor %d caret %d", a.composerAnchor, a.composerCaretPos)
	}
	anchor, caret := a.composerAnchor, a.composerCaretPos
	draft := a.active().Draft
	blurThenActivate(tt)
	if !tt.Focused("Message") {
		t.Fatal("composer did not take focus")
	}
	if a.active().Draft != draft {
		t.Fatalf("draft changed to %q", a.active().Draft)
	}
	if a.composerAnchor != anchor || a.composerCaretPos != caret {
		t.Fatalf("caret %d,%d want %d,%d", a.composerAnchor, a.composerCaretPos, anchor, caret)
	}
}

func TestActivateCaretAtEndWithoutRecord(t *testing.T) {
	a, tt := focusSendApp(t)
	a.active().Draft = "hello"
	tt.Frame()
	if err := tt.Click("Search"); err != nil {
		t.Fatal(err)
	}
	a.composerCaretSet = false
	blurThenActivate(tt)
	if !tt.Focused("Message") {
		t.Fatal("composer did not take focus")
	}
	tt.Type("!")
	if a.active().Draft != "hello!" {
		t.Fatalf("caret was not at the end: %q", a.active().Draft)
	}
}

func TestActivateKeepsInlineEdit(t *testing.T) {
	_, tt := focusSendApp(t)
	tt.Type("Hello")
	tt.Key(0, ui.KeyEnter)
	if err := tt.Click("Edit"); err != nil {
		t.Fatal(err)
	}
	if !tt.Focused("Edit message") {
		t.Fatal("inline editor did not take focus")
	}
	blurThenActivate(tt)
	if !tt.Focused("Edit message") || tt.Focused("Message") {
		t.Fatal("reactivation left the inline edit")
	}
}

func TestActivateKeepsFindField(t *testing.T) {
	a, tt := focusSendApp(t)
	tt.Type("Hello")
	tt.Key(0, ui.KeyEnter)
	tt.Key(ui.Cmd, ui.KeyF)
	if !a.finder.open || !tt.Focused("Find in chat") {
		t.Fatalf("find open=%v focused=%v", a.finder.open, tt.Focused("Find in chat"))
	}
	blurThenActivate(tt)
	if !tt.Focused("Find in chat") || tt.Focused("Message") {
		t.Fatal("reactivation left the find field")
	}

	// Find open, but the field itself is not focused: the composer returns.
	if err := tt.Click("Message"); err != nil {
		t.Fatal(err)
	}
	if a.finder.focused {
		t.Fatal("find field still focused")
	}
	blurThenActivate(tt)
	if !tt.Focused("Message") {
		t.Fatal("composer did not return while find was open but unfocused")
	}
}

func TestActivateKeepsRename(t *testing.T) {
	a, tt := focusSendApp(t)
	a.beginRename()
	tt.Frame()
	if !a.renameOpen || !tt.Focused("Name") {
		t.Fatalf("rename open=%v name focused=%v", a.renameOpen, tt.Focused("Name"))
	}
	blurThenActivate(tt)
	if !a.renameOpen || !tt.Focused("Name") || tt.Focused("Message") {
		t.Fatal("reactivation left the rename field")
	}
}

func TestActivateKeepsConfirm(t *testing.T) {
	_, tt := focusSendApp(t)
	tt.Type("Hello")
	tt.Key(0, ui.KeyEnter)
	tt.Key(ui.Cmd, ui.KeyBackspace)
	if !tt.Focused("Cancel") {
		t.Fatal("confirm dialog did not take focus")
	}
	blurThenActivate(tt)
	if !tt.Focused("Cancel") || tt.Focused("Message") {
		t.Fatal("reactivation left the confirm dialog")
	}
}

func TestActivateKeepsMaxContextField(t *testing.T) {
	a, tt := focusSendApp(t)
	a.settingsOpen = true
	a.settingsSection = "general"
	tt.Frame()
	if err := tt.Click("Unlimited"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Limit to"); err != nil {
		t.Fatalf("limit option: %v texts %q", err, tt.Texts())
	}
	tt.Frame()
	if !tt.Focused("Rounds per session") {
		t.Fatalf("number field did not take focus: %q", tt.Texts())
	}
	blurThenActivate(tt)
	if !tt.Focused("Rounds per session") || tt.Focused("Message") {
		t.Fatal("reactivation left the Max context field")
	}
}

func TestActivateComposerOverHistorySelection(t *testing.T) {
	a := newTestApp(t)
	now := time.Now()
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Notes", Loaded: true, Config: DefaultConfig(), UpdatedAt: now, CreatedAt: now,
		Messages: []Message{{ID: "u1", Role: roleUser, Content: "Alpha message", Done: true, CreatedAt: now}},
	}}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	dragSelect(t, tt, "Alpha message", "Alpha message")
	if !a.quoteBarOpen || !tt.HasText("Add to Chat") {
		t.Fatal("selection bar did not open")
	}
	sel := a.histSel.Text
	blurThenActivate(tt)
	if !tt.Focused("Message") {
		t.Fatal("composer did not take focus from a history selection")
	}
	if !a.quoteBarOpen || !tt.HasText("Add to Chat") {
		t.Fatal("reactivation cleared the Add to Chat bar")
	}
	if a.histSel.Text != sel {
		t.Fatalf("selection changed from %q to %q", sel, a.histSel.Text)
	}
}

func TestActivateKeepsListScroll(t *testing.T) {
	a := newTestApp(t)
	now := time.Now()
	msgs := make([]Message, 0, 40)
	for i := range 40 {
		msgs = append(msgs, Message{
			ID: fmt.Sprintf("m%d", i), Role: roleUser, Done: true, CreatedAt: now,
			Content: fmt.Sprintf("Scroll line %d %s", i, strings.Repeat("word ", 8)),
		})
	}
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Long", Loaded: true, Config: DefaultConfig(), UpdatedAt: now, CreatedAt: now,
		Messages: msgs,
	}}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	r, ok := tt.Find("transcript")
	if !ok {
		t.Fatal("no transcript")
	}
	tt.Scroll(r.X+40, r.Y+80, 0, -8000)
	tt.Frame()
	if a.chat.AtEnd() {
		t.Fatal("scroll did not leave the end")
	}
	first, last := a.chat.Visible()
	blurThenActivate(tt)
	f2, l2 := a.chat.Visible()
	if a.chat.AtEnd() || f2 != first || l2 != last {
		t.Fatalf("list moved on reactivation: end %v visible %d-%d was %d-%d", a.chat.AtEnd(), f2, l2, first, last)
	}
	if !tt.Focused("Message") {
		t.Fatal("composer did not take focus")
	}
}

func TestActivateClickKeepsTarget(t *testing.T) {
	_, tt := focusSendApp(t)
	tt.Type("Hello")
	tt.SetFocused(false)
	if err := tt.Click("Search"); err != nil {
		t.Fatal(err)
	}
	if !tt.Focused("Search") {
		t.Fatal("click did not focus Search")
	}
	tt.SetFocused(true)
	if !tt.Focused("Search") || tt.Focused("Message") {
		t.Fatal("activation stole focus from the clicked Search field")
	}
}

func TestActivateDuringStreaming(t *testing.T) {
	a, tt := focusSendApp(t)
	tt.Type("Hello")
	tt.Key(0, ui.KeyEnter)
	armStream(t, a)
	tt.Frame()
	tt.Type("draft")
	tt.Key(0, ui.KeyLeft)
	caret := a.composerCaretPos
	blurThenActivate(tt)
	if !a.isStreaming("c1") {
		t.Fatal("reactivation stopped the reply")
	}
	if !tt.Focused("Message") {
		t.Fatal("composer did not take focus while streaming")
	}
	if a.active().Draft != "draft" || a.composerCaretPos != caret {
		t.Fatalf("draft %q caret %d want draft at %d", a.active().Draft, a.composerCaretPos, caret)
	}
}
