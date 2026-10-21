package main

import (
	"strings"
	"testing"

	"cove/themes"

	"github.com/egoist/mygo/ui"
)

func editThread() Conversation {
	return Conversation{
		ID: "c1", Title: "Notes", Draft: "half written", Config: DefaultConfig(),
		Messages: []Message{
			{ID: "u", Role: roleUser, Content: "Please revise this", Done: true},
			{ID: "a", Role: roleAssistant, Content: "A reply", Done: true},
		},
	}
}

func threadIntact(t *testing.T, conv *Conversation) {
	t.Helper()
	if conv == nil || conv.Draft != "half written" || len(conv.Messages) != 2 ||
		conv.Messages[0].ID != "u" || conv.Messages[0].Content != "Please revise this" ||
		conv.Messages[1].ID != "a" || conv.Messages[1].Content != "A reply" {
		t.Fatalf("thread changed: %+v", conv)
	}
}

func TestEditEscapeRestoresThread(t *testing.T) {
	a := newTestApp(t)
	a.data.Conversations = []Conversation{editThread()}
	a.data.ActiveID = "c1"
	a.save()
	tt := openTester(a)
	if err := tt.Click("Edit"); err != nil {
		t.Fatal(err)
	}
	if a.edit.text != "Please revise this" || a.active().Draft != "half written" || len(a.active().Messages) != 2 {
		t.Fatalf("enter edit: text %q draft %q messages %d", a.edit.text, a.active().Draft, len(a.active().Messages))
	}
	a.edit.text = "Please revise this, and more"
	if len(a.active().Messages) != 2 {
		t.Fatal("editor change truncated the thread")
	}
	got, err := Load(a.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Conversations) != 1 || len(got.Conversations[0].Messages) != 2 || got.Conversations[0].Draft != "half written" {
		t.Fatalf("edit was persisted: %+v", got.Conversations)
	}
	tt.Key(0, ui.KeyEscape)
	if a.edit.convID != "" {
		t.Fatal("still editing after Escape")
	}
	threadIntact(t, a.active())
}

func TestEditSwitchChatAbandons(t *testing.T) {
	a := newTestApp(t)
	notes := editThread()
	other := Conversation{
		ID: "c2", Title: "Other", Config: DefaultConfig(),
		Messages: []Message{{ID: "o", Role: roleUser, Content: "Elsewhere", Done: true}},
	}
	a.data.Conversations = []Conversation{notes, other}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	if err := tt.Click("Edit"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Other"); err != nil {
		t.Fatal(err)
	}
	if a.data.ActiveID != "c2" {
		t.Fatalf("selection %q", a.data.ActiveID)
	}
	if a.edit.convID != "" {
		t.Fatal("edit stayed open on the other chat")
	}
	if err := tt.Click("Notes"); err != nil {
		t.Fatal(err)
	}
	if a.data.ActiveID != "c1" || a.edit.convID != "" {
		t.Fatalf("back on %q editing %q", a.data.ActiveID, a.edit.convID)
	}
	threadIntact(t, a.conversation("c1"))
}

func TestQuitWhileEditingLosesNothing(t *testing.T) {
	a := newTestApp(t)
	a.data.Conversations = []Conversation{editThread()}
	a.data.ActiveID = "c1"
	a.editMessage("c1", "u")
	a.active().Draft = "unsent rewrite"
	a.persistForExit()
	if a.edit.convID != "" {
		t.Fatal("quit left edit mode on")
	}
	threadIntact(t, a.conversation("c1"))
	got, err := Load(a.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Conversations) != 1 {
		t.Fatalf("disk chats %+v", got.Conversations)
	}
	threadIntact(t, &got.Conversations[0])
}

func TestEditSubmitReplacesLaterMessages(t *testing.T) {
	a := newTestApp(t)
	a.data.Conversations = []Conversation{editThread()}
	a.data.ActiveID = "c1"
	a.editMessage("c1", "u")
	if len(a.active().Messages) != 2 {
		t.Fatal("entering edit truncated")
	}
	a.edit.text = "Revised question"
	a.submitEdit()
	if a.confirmKind != "" {
		t.Fatalf("resend opened a dialog %q", a.confirmKind)
	}
	conv := a.active()
	if a.edit.convID != "" {
		t.Fatal("submit left edit mode on")
	}
	if len(conv.Messages) < 2 || conv.Messages[0].ID != "u" || conv.Messages[0].Content != "Revised question" {
		t.Fatalf("submitted thread %+v", conv.Messages)
	}
	for _, m := range conv.Messages {
		if m.ID == "a" {
			t.Fatal("later reply survived submit")
		}
	}
	if conv.Draft != "half written" {
		t.Fatalf("composer draft %q", conv.Draft)
	}
}

func replaceEdit(t *testing.T, tt *ui.Tester) {
	t.Helper()
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("Revised question")
}

func assertResent(t *testing.T, a *App) {
	t.Helper()
	if a.confirmKind != "" {
		t.Fatalf("resend opened a dialog %q", a.confirmKind)
	}
	conv := a.active()
	if a.edit.convID != "" {
		t.Fatal("resend left edit mode on")
	}
	if conv == nil || len(conv.Messages) < 2 || conv.Messages[0].ID != "u" || conv.Messages[0].Content != "Revised question" {
		t.Fatalf("submitted thread %+v", conv)
	}
	for _, m := range conv.Messages {
		if m.ID == "a" {
			t.Fatal("later reply survived the resend")
		}
	}
	if conv.Draft != "half written" {
		t.Fatalf("composer draft %q", conv.Draft)
	}
}

func TestEditEnterResendsWithoutDialog(t *testing.T) {
	a := newTestApp(t)
	a.data.Conversations = []Conversation{editThread()}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	if err := tt.Click("Edit"); err != nil {
		t.Fatal(err)
	}
	replaceEdit(t, tt)
	tt.Key(0, ui.KeyEnter)
	if tt.HasText("Resend from here?") {
		t.Fatal("enter showed the resend dialog")
	}
	assertResent(t, a)
}

func TestEditSendButtonResendsWithoutDialog(t *testing.T) {
	a := newTestApp(t)
	a.data.Conversations = []Conversation{editThread()}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	if err := tt.Click("Edit"); err != nil {
		t.Fatal(err)
	}
	replaceEdit(t, tt)
	if err := tt.Click("Send edit"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("Resend from here?") {
		t.Fatal("send button showed the resend dialog")
	}
	assertResent(t, a)
}

func TestEditSendWithCommandReturn(t *testing.T) {
	a := newTestApp(t)
	a.data.SendWith = "mod"
	a.data.Conversations = []Conversation{editThread()}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	if err := tt.Click("Edit"); err != nil {
		t.Fatal(err)
	}
	replaceEdit(t, tt)
	tt.Key(0, ui.KeyEnter)
	if a.edit.convID == "" || a.confirmKind != "" || len(a.active().Messages) != 2 {
		t.Fatalf("plain enter sent: editing %q dialog %q messages %d", a.edit.convID, a.confirmKind, len(a.active().Messages))
	}
	if !strings.Contains(a.edit.text, "\n") {
		t.Fatalf("plain enter did not insert a newline: %q", a.edit.text)
	}
	threadIntact(t, a.active())
	tt.Key(ui.Cmd, ui.KeyEnter)
	if tt.HasText("Resend from here?") {
		t.Fatal("command-return showed the resend dialog")
	}
	assertResent(t, a)
}

func TestEditShiftEnterDoesNotSend(t *testing.T) {
	a := newTestApp(t)
	a.data.Conversations = []Conversation{editThread()}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	if err := tt.Click("Edit"); err != nil {
		t.Fatal(err)
	}
	replaceEdit(t, tt)
	tt.Key(ui.Shift, ui.KeyEnter)
	if a.edit.convID == "" || a.confirmKind != "" {
		t.Fatalf("shift+enter sent: editing %q dialog %q", a.edit.convID, a.confirmKind)
	}
	if !strings.Contains(a.edit.text, "\n") {
		t.Fatalf("shift+enter did not insert a newline: %q", a.edit.text)
	}
	threadIntact(t, a.active())
}

func TestDeleteDialogUnchangedWhileEditing(t *testing.T) {
	a := newTestApp(t)
	a.data.Conversations = []Conversation{editThread()}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	if err := tt.Click("Edit"); err != nil {
		t.Fatal(err)
	}
	replaceEdit(t, tt)
	a.askDelete()
	tt.Frame()
	if a.confirmKind != confirmDelete {
		t.Fatalf("delete dialog %q", a.confirmKind)
	}
	tt.Key(0, ui.KeyEnter)
	if a.confirmKind != "" {
		t.Fatal("enter did not dismiss the delete dialog")
	}
	if a.edit.convID == "" || !strings.Contains(a.edit.text, "Revised question") {
		t.Fatalf("enter resubmitted the edit: %+v", a.edit)
	}
	threadIntact(t, a.active())

	a.askDelete()
	tt.Frame()
	if err := tt.Click("Delete"); err != nil {
		t.Fatal(err)
	}
	if a.conversation("c1") != nil {
		t.Fatal("delete did not remove the chat")
	}
}

func TestEditClickOutsideCancels(t *testing.T) {
	a := newTestApp(t)
	a.data.Conversations = []Conversation{editThread()}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	if err := tt.Click("Edit"); err != nil {
		t.Fatal(err)
	}
	a.edit.text = "changed in place"
	if err := tt.Click("later a"); err != nil {
		t.Fatal(err)
	}
	if a.edit.convID != "" {
		t.Fatal("clicking a later message left the editor open")
	}
	threadIntact(t, a.active())

	if err := tt.Click("Edit"); err != nil {
		t.Fatal(err)
	}
	a.edit.text = "changed again"
	if err := tt.Click("composer lock"); err != nil {
		t.Fatal(err)
	}
	if a.edit.convID != "" {
		t.Fatal("clicking the composer left the editor open")
	}
	threadIntact(t, a.active())
}

func TestEditReplacesHint(t *testing.T) {
	a := newTestApp(t)
	a.data.Conversations = []Conversation{editThread()}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	if err := tt.Click("Edit"); err != nil {
		t.Fatal(err)
	}
	hint, ok := tt.Find("Replaces 1 later message")
	if !ok {
		t.Fatalf("singular hint missing: %q", tt.Texts())
	}
	cancel, ok := tt.Find("Cancel")
	if !ok || hint.X+hint.W > cancel.X {
		t.Fatalf("hint should sit left of Cancel: hint %+v cancel %+v", hint, cancel)
	}
	replaceEdit(t, tt)
	if err := tt.Click("Send edit"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("Resend from here?") || tt.HasText("Replaces") {
		t.Fatal("send left a dialog or the replaces hint")
	}
	assertResent(t, a)

	plural := editThread()
	plural.Messages = append(plural.Messages,
		Message{ID: "u2", Role: roleUser, Content: "And another", Done: true},
		Message{ID: "a2", Role: roleAssistant, Content: "More", Done: true},
	)
	a = newTestApp(t)
	a.data.Conversations = []Conversation{plural}
	a.data.ActiveID = "c1"
	tt = openTester(a)
	if err := tt.Click("Edit"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("Replaces 3 later messages"); !ok {
		t.Fatalf("plural hint: %q", tt.Texts())
	}

	only := editThread()
	only.Messages = only.Messages[:1]
	a = newTestApp(t)
	a.data.Conversations = []Conversation{only}
	a.data.ActiveID = "c1"
	tt = openTester(a)
	if err := tt.Click("Edit"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("Replaces") {
		t.Fatalf("hint shown with nothing after the message: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	if a.confirmKind != "" || a.active().Messages[0].Content != "Please revise this" {
		t.Fatalf("enter on the last message: dialog %q messages %+v", a.confirmKind, a.active().Messages)
	}
}

func TestEditDirectSendWithoutLater(t *testing.T) {
	a := newTestApp(t)
	conv := editThread()
	conv.Messages = conv.Messages[:1]
	a.data.Conversations = []Conversation{conv}
	a.data.ActiveID = "c1"
	a.editMessage("c1", "u")
	a.edit.text = "Only this"
	a.submitEdit()
	if a.confirmKind != "" {
		t.Fatal("a trailing message should send without a dialog")
	}
	got := a.active()
	if got.Messages[0].Content != "Only this" || got.Messages[0].ID != "u" {
		t.Fatalf("%+v", got.Messages)
	}
}

func TestThemePersists(t *testing.T) {
	a := newTestApp(t)
	tt := openTester(a)
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Appearance"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Tide"); err != nil {
		t.Fatal(err)
	}
	if a.data.Theme != themes.ThemeTide {
		t.Fatalf("theme %q", a.data.Theme)
	}
	if a.look.ThemeMark != themes.Palette(themes.ThemeTide, false).ThemeMark {
		t.Fatalf("mark did not follow Tide: %+v", a.look.ThemeMark)
	}
	got, err := Load(a.path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Theme != themes.ThemeTide {
		t.Fatalf("theme not saved: %q", got.Theme)
	}
	again := openApp(a.path, newMemKeys())
	if again.data.Theme != themes.ThemeTide {
		t.Fatalf("reopen theme %q", again.data.Theme)
	}
}
