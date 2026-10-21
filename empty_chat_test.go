package main

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func chatWith(id, title, draft string, messages []Message, updated time.Time) Conversation {
	return Conversation{
		ID: id, Title: title, Draft: draft, Messages: messages,
		Config: DefaultConfig(), CreatedAt: updated, UpdatedAt: updated,
	}
}

func userMessage(text string) Message {
	return Message{ID: "m-" + text, Role: roleUser, Content: text, Done: true, CreatedAt: time.Now()}
}

func TestSwitchDiscardsUntouchedAndSelectsClicked(t *testing.T) {
	a := newTestApp(t)
	now := time.Now()
	older := chatWith("old", "Shelf", "", []Message{userMessage("one")}, now.Add(-2*time.Hour))
	middle := chatWith("mid", "Pinned note", "", []Message{userMessage("two")}, now.Add(-time.Minute))
	a.data.Conversations = []Conversation{older, middle}
	a.data.ActiveID = "mid"
	a.newChat()
	emptyID := a.active().ID
	if emptyID == "old" || emptyID == "mid" {
		t.Fatal("new chat reused an existing id")
	}
	a.selectChat("old")
	if a.conversation(emptyID) != nil {
		t.Fatal("untouched chat stayed in the sidebar")
	}
	if a.data.ActiveID != "old" {
		t.Fatalf("selection %q, want the clicked chat", a.data.ActiveID)
	}
	if a.conversation("mid") == nil || a.conversation("old") == nil {
		t.Fatalf("kept chats: %+v", a.data.Conversations)
	}
	got, err := Load(a.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Conversations) != 2 || got.ActiveID != "old" {
		t.Fatalf("disk %+v active %s", got.Conversations, got.ActiveID)
	}
}

func TestClickDiscardsUntouchedAndSelectsRow(t *testing.T) {
	a := newTestApp(t)
	now := time.Now()
	a.data.Conversations = []Conversation{
		chatWith("old", "Shelf", "", []Message{userMessage("one")}, now.Add(-2*time.Hour)),
		chatWith("mid", "Pinned note", "", []Message{userMessage("two")}, now.Add(-time.Minute)),
	}
	a.data.ActiveID = "mid"
	tt := openTester(a)
	if err := tt.Click("New Chat"); err != nil {
		t.Fatal(err)
	}
	if len(a.data.Conversations) != 3 {
		t.Fatalf("new chat count %d", len(a.data.Conversations))
	}
	if err := tt.Click("Shelf"); err != nil {
		t.Fatal(err)
	}
	if a.data.ActiveID != "old" {
		t.Fatalf("selection %q", a.data.ActiveID)
	}
	if len(a.data.Conversations) != 2 {
		t.Fatalf("sidebar still has %d chats", len(a.data.Conversations))
	}
	for _, c := range a.data.Conversations {
		if c.Title == newChatTitle && len(c.Messages) == 0 && strings.TrimSpace(c.Draft) == "" {
			t.Fatal("empty new chat still listed")
		}
	}
}

func TestNewChatFocusesUntouched(t *testing.T) {
	a := newTestApp(t)
	a.newChat()
	id := a.active().ID
	a.setEffort("High")
	a.usePreset("Anthropic")
	a.newChat()
	if len(a.data.Conversations) != 1 || a.active().ID != id {
		t.Fatalf("duplicate empty chat: %+v", a.data.Conversations)
	}
	if a.active().Config.Provider != "Anthropic" || a.active().Config.Reasoning != effortHigh {
		t.Fatalf("focus cleared the pickers: %+v", a.active().Config)
	}
}

func TestWhitespaceAndPickerDoNotKeepNewChat(t *testing.T) {
	a := newTestApp(t)
	now := time.Now()
	a.data.Conversations = []Conversation{
		chatWith("keep", "Kept", "", []Message{userMessage("stay")}, now.Add(-time.Hour)),
	}
	a.data.ActiveID = "keep"
	a.newChat()
	a.active().Draft = " \n\t "
	a.setEffort("Low")
	a.active().Config.Model = "some-other-model"
	emptyID := a.active().ID
	a.selectChat("keep")
	if a.conversation(emptyID) != nil {
		t.Fatal("whitespace draft or model change kept the chat")
	}
	if a.data.ActiveID != "keep" {
		t.Fatalf("selection %q", a.data.ActiveID)
	}
}

func TestDraftAndSentMessageStay(t *testing.T) {
	a := newTestApp(t)
	a.newChat()
	a.active().Draft = "  hold this  "
	draftID := a.active().ID
	a.newChat()
	if a.conversation(draftID) == nil || a.conversation(draftID).Draft != "  hold this  " {
		t.Fatalf("draft chat: %+v", a.conversation(draftID))
	}
	if a.active().ID == draftID || len(a.data.Conversations) != 2 {
		t.Fatalf("new chat did not open beside the draft: %+v", a.data.Conversations)
	}
	a.active().Messages = []Message{userMessage("sent")}
	a.active().Draft = ""
	sentID := a.active().ID
	a.newChat()
	third := a.active().ID
	if a.conversation(sentID) == nil || len(a.conversation(sentID).Messages) != 1 {
		t.Fatal("sent chat was discarded")
	}
	if a.conversation(draftID) == nil || a.conversation(draftID).Draft != "  hold this  " {
		t.Fatal("draft was dropped while opening another chat")
	}
	a.selectChat(draftID)
	if a.conversation(third) != nil {
		t.Fatal("untouched third chat lingered after leaving it")
	}
	if a.data.ActiveID != draftID {
		t.Fatalf("selection %q", a.data.ActiveID)
	}
	got, err := Load(a.path)
	if err != nil {
		t.Fatal(err)
	}
	var draft string
	for _, c := range got.Conversations {
		if c.ID == draftID {
			draft = c.Draft
		}
		if c.ID == third {
			t.Fatal("empty chat was persisted")
		}
	}
	if draft != "  hold this  " {
		t.Fatalf("persisted draft %q", draft)
	}
}

func TestDeleteEmptyChatSkipsDialog(t *testing.T) {
	a := newTestApp(t)
	now := time.Now()
	kept := chatWith("old", "Shelf", "", []Message{userMessage("one")}, now)
	draft := chatWith("draft", newChatTitle, "Sketch a weekend", nil, now)
	a.data.Conversations = []Conversation{kept, draft}
	a.data.ActiveID = "old"
	a.newChat()
	emptyID := a.active().ID
	a.askDelete()
	if a.confirmKind != "" {
		t.Fatal("an empty chat opened a confirmation")
	}
	if a.conversation(emptyID) != nil {
		t.Fatal("the empty chat was not deleted")
	}
	if a.data.ActiveID != "old" {
		t.Fatalf("selection %q", a.data.ActiveID)
	}
	a.selectChat("draft")
	a.askDelete()
	if a.confirmKind != "" {
		t.Fatal("a draft with no messages opened a confirmation")
	}
	if a.conversation("draft") != nil || a.conversation("old") == nil {
		t.Fatalf("chats %+v", a.data.Conversations)
	}
}

func TestQuitAndCloseDropUntouched(t *testing.T) {
	a := newTestApp(t)
	a.newChat()
	a.active().Draft = "notes to keep"
	kept := a.active().ID
	a.newChat()
	a.setEffort("Medium")
	emptyID := a.active().ID
	// Quit and the window close hook both go through persistForExit.
	a.persistForExit()
	if a.conversation(emptyID) != nil {
		t.Fatal("untouched chat survived exit")
	}
	if a.conversation(kept) == nil || a.conversation(kept).Draft != "notes to keep" {
		t.Fatalf("draft chat: %+v", a.conversation(kept))
	}
	if a.data.ActiveID != kept {
		t.Fatalf("active after exit %q", a.data.ActiveID)
	}
	got, err := Load(a.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Conversations) != 1 || got.Conversations[0].ID != kept || got.Conversations[0].Draft != "notes to keep" {
		t.Fatalf("disk %+v", got.Conversations)
	}
	if got.ActiveID != kept {
		t.Fatalf("disk active %s", got.ActiveID)
	}
}

func emptyDraftCount(a *App) int {
	n := 0
	for i := range a.data.Conversations {
		if untouched(&a.data.Conversations[i]) {
			n++
		}
	}
	return n
}

// TestRepeatedNewChatSwitchAndRestart is §18: ⌘N three times leaves one
// empty draft, leaving it deletes it, and a restart does not bring empty
// drafts back — including ones an older build saved under Yesterday. A
// draft with typed text stays, titled from that text.
func TestRepeatedNewChatSwitchAndRestart(t *testing.T) {
	a := newTestApp(t)
	now := time.Now()
	a.data.Conversations = []Conversation{
		chatWith("old", "Shelf", "", []Message{userMessage("one")}, now.Add(-time.Hour)),
	}
	a.data.ActiveID = "old"
	tt := openTester(a)
	for i := 0; i < 3; i++ {
		tt.Key(ui.Cmd, ui.KeyN)
	}
	if emptyDraftCount(a) != 1 || len(a.data.Conversations) != 2 {
		t.Fatalf("repeated ⌘N: %+v", a.data.Conversations)
	}
	if err := tt.Click("Shelf"); err != nil {
		t.Fatal(err)
	}
	if emptyDraftCount(a) != 0 || len(a.data.Conversations) != 1 {
		t.Fatalf("switching away: %+v", a.data.Conversations)
	}

	tt.Key(ui.Cmd, ui.KeyN)
	focusDraft(t, tt)
	tt.Type("Sketch a weekend in Lisbon")
	a.persistForExit()
	restarted := openApp(a.path, newMemKeys())
	if emptyDraftCount(restarted) != 0 {
		t.Fatalf("restart kept an empty draft: %+v", restarted.data.Conversations)
	}
	var titled string
	for _, c := range restarted.data.Conversations {
		if c.ID == "old" {
			continue
		}
		titled = c.Title
		if c.Draft != "Sketch a weekend in Lisbon" {
			t.Fatalf("draft text %q", c.Draft)
		}
	}
	if titled != "Sketch a weekend in Lisbon" {
		t.Fatalf("draft title %q, chats %+v", titled, restarted.data.Conversations)
	}

	yesterday := now.Add(-30 * time.Hour)
	stale := defaultData()
	stale.Conversations = []Conversation{
		chatWith("old", "Shelf", "", []Message{userMessage("one")}, now),
		chatWith("today", newChatTitle, "", nil, now.Add(-time.Minute)),
		chatWith("y1", newChatTitle, "", nil, yesterday),
		chatWith("y2", newChatTitle, " \n\t ", nil, yesterday.Add(-time.Hour)),
		chatWith("text", newChatTitle, "Sketch a weekend in Lisbon", nil, yesterday),
	}
	stale.ActiveID = "y1"
	if err := Save(a.path, &stale); err != nil {
		t.Fatal(err)
	}
	got, err := Load(a.path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ActiveID == "y1" || got.ActiveID == "y2" || got.ActiveID == "today" {
		t.Fatalf("active empty draft survived: %s", got.ActiveID)
	}
	ids := map[string]bool{}
	for _, c := range got.Conversations {
		ids[c.ID] = true
		if len(c.Messages) == 0 && strings.TrimSpace(c.Draft) == "" {
			t.Fatalf("empty draft reloaded: %+v", c)
		}
	}
	if ids["today"] || ids["y1"] || ids["y2"] || !ids["old"] || !ids["text"] {
		t.Fatalf("loaded %+v", got.Conversations)
	}
	for _, c := range got.Conversations {
		if c.ID == "text" && c.Title != "Sketch a weekend in Lisbon" {
			t.Fatalf("text draft title %q", c.Title)
		}
	}
}
