package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func focusDraft(t *testing.T, tt *ui.Tester) {
	t.Helper()
	if err := tt.Click("Message"); err != nil {
		t.Fatal(err)
	}
}

func newTestApp(t *testing.T) *App {
	t.Helper()
	a := openApp(t.TempDir()+"/conversations.json", newMemKeys())
	a.instant = true
	return a
}

func openTester(a *App) *ui.Tester {
	return ui.NewTester(a.view, 1100, 760)
}

func composerCaret(t *testing.T, tt *ui.Tester) {
	t.Helper()
	if !tt.Focused("Message") {
		t.Fatal("composer is not first responder")
	}
	caret, ok := tt.TextCaret()
	if !ok || caret.H < 1 || caret.W < 1 {
		t.Fatalf("composer caret %+v visible %v", caret, ok)
	}
}

func TestNewChatFocusesComposer(t *testing.T) {
	a := newTestApp(t)
	now := time.Now()
	a.data.Conversations = []Conversation{chatWith("draft", newChatTitle, "", nil, now)}
	a.data.ActiveID = "draft"
	tt := openTester(a)
	composerCaret(t, tt)

	if err := tt.Click("Search"); err != nil {
		t.Fatal(err)
	}
	if tt.Focused("Message") {
		t.Fatal("search did not take focus")
	}
	if err := tt.Click("New Chat"); err != nil {
		t.Fatal(err)
	}
	if len(a.data.Conversations) != 1 || a.active().ID != "draft" {
		t.Fatalf("compose button duplicated the draft: %+v", a.data.Conversations)
	}
	composerCaret(t, tt)

	if err := tt.Click("Search"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyN)
	if len(a.data.Conversations) != 1 || a.active().ID != "draft" || a.active().Draft != "" || len(a.active().Messages) != 0 {
		t.Fatalf("⌘N on an empty draft created another chat: %+v", a.data.Conversations)
	}
	composerCaret(t, tt)

	if err := tt.Click(starters[0].Title); err != nil {
		t.Fatal(err)
	}
	if len(a.data.Conversations) != 1 || a.active().ID != "draft" || len(a.active().Messages) != 0 {
		t.Fatalf("suggestion sent or opened another chat: %+v", a.active())
	}
	if a.active().Draft != starters[0].Prompt {
		t.Fatalf("suggestion draft %q", a.active().Draft)
	}
	composerCaret(t, tt)
	tt.Type("X")
	if a.active().Draft != starters[0].Prompt+"X" {
		t.Fatalf("caret was not at the end: %q", a.active().Draft)
	}
	a.active().Draft = ""

	// File › New Chat calls newChat directly.
	if err := tt.Click("Search"); err != nil {
		t.Fatal(err)
	}
	a.newChat()
	tt.Frame()
	composerCaret(t, tt)

	if err := tt.Click("Search"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Draft"); err != nil {
		t.Fatal(err)
	}
	composerCaret(t, tt)
	tt.Type("Hi")
	if a.active().Draft != "Hi" {
		t.Fatalf("draft %q", a.active().Draft)
	}

	a.active().Draft = ""
	a.openSettings()
	tt.Frame()
	if err := tt.Click("Base URL"); err != nil {
		t.Fatal(err)
	}
	base := a.form.BaseURL
	a.newChat()
	tt.Frame()
	tt.Type("Z")
	if a.form.BaseURL != base+"Z" {
		t.Fatalf("sheet lost the keystroke: %q", a.form.BaseURL)
	}
	if strings.Contains(a.active().Draft, "Z") {
		t.Fatal("composer took the keystroke from the sheet")
	}

	a.modalOpen = false
	tt.Frame()
	a.active().Messages = []Message{userMessage("kept")}
	a.askDelete()
	tt.Frame()
	if !tt.Focused("Cancel") {
		t.Fatal("Cancel is not focused")
	}
	a.newChat()
	tt.Frame()
	if !tt.Focused("Cancel") || tt.Focused("Message") {
		t.Fatal("composer stole focus from the dialog")
	}

	a.confirmKind = ""
	tt.Frame()
	a.openDefaults()
	tt.Frame()
	prompt, ok := tt.Find("System prompt")
	if !ok {
		t.Fatal("system prompt label missing")
	}
	// The field sits under the label and its description. The stack does
	// not focus from the label, so the click lands in the text area.
	tt.ClickAt(prompt.X+40, prompt.Y+prompt.H+52)
	a.newChat()
	tt.Frame()
	tt.Type("S")
	if !strings.Contains(a.pref.cfg.SystemPrompt, "S") || a.pref.cfg.SystemPrompt == "You are a helpful assistant. Be clear and concise." {
		t.Fatalf("settings lost the keystroke: %q", a.pref.cfg.SystemPrompt)
	}
	if a.active() != nil && strings.Contains(a.active().Draft, "S") {
		t.Fatal("composer took the keystroke from Settings")
	}
}

func TestEmptyAndNewChat(t *testing.T) {
	a := newTestApp(t)
	tt := openTester(a)
	if !tt.HasText("No chats yet") || !tt.HasText("A native chat for any OpenAI-compatible model.") {
		t.Fatalf("empty state missing: %q", tt.Texts())
	}
	if err := tt.Click("New Chat"); err != nil {
		t.Fatal(err)
	}
	if len(a.data.Conversations) != 1 || a.active() == nil {
		t.Fatalf("no chat after the button: %+v", a.data.Conversations)
	}
	if !tt.HasText("What should we work on?") {
		t.Fatalf("empty transcript missing: %q", tt.Texts())
	}
	if !tt.HasText("New Chat") {
		t.Fatalf("draft title: %q", tt.Texts())
	}
	for _, text := range tt.Texts() {
		if text == "New chat" {
			t.Fatalf("title uses sentence case %q", text)
		}
	}
	id := a.active().ID
	tt.Key(ui.Cmd, ui.KeyN)
	if len(a.data.Conversations) != 1 || a.active().ID != id {
		t.Fatalf("Cmd+N duplicated an empty chat: %d id %s", len(a.data.Conversations), a.data.ActiveID)
	}
}

func TestRenameSwitchDelete(t *testing.T) {
	a := newTestApp(t)
	tt := openTester(a)
	if err := tt.Click("New Chat"); err != nil {
		t.Fatal(err)
	}
	focusDraft(t, tt)
	tt.Type("Alpha topic")
	tt.Key(0, ui.KeyEnter)
	if a.active().Title != "Alpha topic" {
		t.Fatalf("title %q", a.active().Title)
	}
	tt.Key(ui.Cmd, ui.KeyN)
	focusDraft(t, tt)
	tt.Type("Beta topic")
	tt.Key(0, ui.KeyEnter)
	if err := tt.Click("Alpha topic"); err != nil {
		t.Fatal(err)
	}
	if a.active().Title != "Alpha topic" {
		t.Fatalf("switch landed on %q", a.active().Title)
	}
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Rename"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("Project notes")
	if err := tt.Click("Save name"); err != nil {
		t.Fatal(err)
	}
	if a.active().Title != "Project notes" {
		t.Fatalf("renamed to %q", a.active().Title)
	}

	before := len(a.data.Conversations)
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Delete chat"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if len(a.data.Conversations) != before {
		t.Fatal("cancel deleted the chat")
	}
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Delete chat"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Delete"); err != nil {
		t.Fatal(err)
	}
	if len(a.data.Conversations) != before-1 {
		t.Fatalf("chats left: %d", len(a.data.Conversations))
	}
	got, err := Load(a.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Conversations) != len(a.data.Conversations) {
		t.Fatalf("disk has %d chats, memory %d", len(got.Conversations), len(a.data.Conversations))
	}
}

func TestSettingsAndInheritedDefaults(t *testing.T) {
	a := newTestApp(t)
	connectBuiltin(a, "Anthropic", "sk-anthropic")
	tt := openTester(a)
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("Last request") {
		t.Fatal("default model settings show last request")
	}
	if err := tt.Click("Models & Providers"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("Last request") {
		t.Fatal("provider settings show last request")
	}
	if err := tt.Click("General"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("gpt-4o-mini"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(" claude-sonnet-4.5"); err != nil {
		t.Fatal(err)
	}
	if a.data.Defaults.Model != "claude-sonnet-4.5" || a.data.Defaults.Provider != "Anthropic" {
		t.Fatalf("defaults %+v", a.data.Defaults)
	}
	a.settingsOpen = false
	tt.Frame()
	if err := tt.Click("New Chat"); err != nil {
		t.Fatal(err)
	}
	if a.active().Config.Model != "claude-sonnet-4.5" || a.active().Config.Provider != "Anthropic" {
		t.Fatalf("new chat %+v", a.active().Config)
	}
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Chat Settings…"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Last request") {
		t.Fatal("conversation settings missing last request")
	}
	if err := tt.Click("Model"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("chat-model")
	if err := tt.Click("Base URL"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("http://127.0.0.1:9/v1")
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	if a.active().Config.Model != "chat-model" || a.active().Config.BaseURL != "http://127.0.0.1:9/v1" {
		t.Fatalf("saved config %+v", a.active().Config)
	}
	if a.data.Defaults.Model != "claude-sonnet-4.5" {
		t.Fatal("conversation settings changed the defaults")
	}
}

func TestSendWithoutKeyShowsError(t *testing.T) {
	a := newTestApp(t)
	tt := openTester(a)
	if err := tt.Click("New Chat"); err != nil {
		t.Fatal(err)
	}
	focusDraft(t, tt)
	tt.Type("Hello")
	tt.Key(0, ui.KeyEnter)
	conv := a.active()
	if len(conv.Messages) != 2 || conv.Messages[0].Content != "Hello" {
		t.Fatalf("messages %+v", conv.Messages)
	}
	if conv.Messages[1].Error == "" || !tt.HasText("API key") {
		t.Fatalf("error %q texts %q", conv.Messages[1].Error, tt.Texts())
	}
	if conv.Draft != "" {
		t.Fatalf("draft left %q", conv.Draft)
	}
}

func TestShiftEnterInsertsNewline(t *testing.T) {
	a := newTestApp(t)
	tt := openTester(a)
	if err := tt.Click("New Chat"); err != nil {
		t.Fatal(err)
	}
	focusDraft(t, tt)
	tt.Type("one")
	tt.Key(ui.Shift, ui.KeyEnter)
	tt.Type("two")
	if a.active().Draft != "one\ntwo" {
		t.Fatalf("draft %q", a.active().Draft)
	}
	if len(a.active().Messages) != 0 {
		t.Fatal("shift+enter sent the draft")
	}
}

func TestCopyAndSelection(t *testing.T) {
	a := newTestApp(t)
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Notes",
		Config: DefaultConfig(),
		Messages: []Message{
			{ID: "u1", Role: roleUser, Content: "Hello from the user", Done: true},
			{ID: "a1", Role: roleAssistant, Content: "Hello **world**", Done: true},
		},
	}}
	a.data.ActiveID = "c1"
	tt := openTester(a)

	copies := 0
	for _, text := range tt.Texts() {
		if text == "Copy" {
			copies++
		}
	}
	if copies < 2 {
		t.Fatalf("want a copy button on both messages, texts %q", tt.Texts())
	}
	if err := tt.Click("Copy"); err != nil {
		t.Fatal(err)
	}
	if tt.Clipboard() != "Hello from the user" {
		t.Fatalf("copy button wrote %q", tt.Clipboard())
	}
	if !tt.HasText("Copied") {
		t.Fatalf("no copied feedback in %q", tt.Texts())
	}
	tt.Advance(2 * time.Second)
	if tt.HasText("Copied") {
		t.Fatal("copied feedback stayed up")
	}

	userBox, ok := tt.Find("Hello from the user")
	if !ok {
		t.Fatal("user message is not visible")
	}
	tt.Press(userBox.X+8, userBox.Y+userBox.H/2)
	tt.Move(userBox.X+userBox.W-8, userBox.Y+userBox.H/2)
	tt.Release(userBox.X+userBox.W-8, userBox.Y+userBox.H/2)
	tt.Key(ui.Cmd, ui.KeyC)
	if tt.Clipboard() != "Hello from the user" {
		t.Fatalf("mouse selection copied %q", tt.Clipboard())
	}

	reply, ok := tt.Find("Hello world")
	if !ok {
		t.Fatal("assistant markdown is not visible")
	}
	tt.Press(reply.X+4, reply.Y+reply.H/2)
	tt.Move(reply.X+reply.W-4, reply.Y+reply.H/2)
	tt.Release(reply.X+reply.W-4, reply.Y+reply.H/2)
	tt.Key(ui.Cmd, ui.KeyC)
	got := tt.Clipboard()
	if !strings.Contains(got, "Hello") || !strings.Contains(got, "world") || strings.Contains(got, "**") {
		t.Fatalf("assistant selection copied %q", got)
	}
	if strings.Contains(got, "**") {
		t.Fatalf("selection copied raw markdown %q", got)
	}

	// The assistant copy button is the second Copy. Click the rendered
	// word's copy by walking texts: press the button whose label follows
	// the reply. A direct click on the remaining Copy hits the first one,
	// which is the user's, so click by the assistant message's button
	// through a fresh app that only has the reply.
	b := newTestApp(t)
	b.data.Conversations = []Conversation{{
		ID: "c1", Title: "Notes", Config: DefaultConfig(),
		Messages: []Message{{ID: "a1", Role: roleAssistant, Content: "Hello **world**", Done: true}},
	}}
	b.data.ActiveID = "c1"
	tb := openTester(b)
	if err := tb.Click("Copy"); err != nil {
		t.Fatal(err)
	}
	if tb.Clipboard() != "Hello **world**" {
		t.Fatalf("assistant copy button wrote rendered text %q", tb.Clipboard())
	}
}

func TestStreamingReplyAndRegenerate(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if calls == 2 && strings.Contains(string(body), "Old reply") {
			t.Errorf("regenerate sent the old reply: %s", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		word := "First reply"
		if calls > 1 {
			word = "Second reply"
		}
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\""+word+"\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	a := newTestApp(t)
	a.data.Defaults.BaseURL = srv.URL + "/v1"
	a.data.Defaults.APIKey = "secret"
	a.data.Defaults.Model = "demo"
	tt := openTester(a)
	if err := tt.Click("New Chat"); err != nil {
		t.Fatal(err)
	}
	focusDraft(t, tt)
	tt.Type("Hello model")
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	conv := a.active()
	if len(conv.Messages) != 2 || conv.Messages[1].Content != "First reply" || !conv.Messages[1].Done {
		t.Fatalf("reply %+v", conv.Messages)
	}
	if !tt.HasText("First reply") {
		t.Fatalf("reply not shown: %q", tt.Texts())
	}
	if err := tt.Click("Regenerate"); err != nil {
		t.Fatal(err)
	}
	conv = a.active()
	if len(conv.Messages) != 2 || conv.Messages[1].Content != "Second reply" {
		t.Fatalf("regenerated %+v", conv.Messages)
	}
	if calls != 2 {
		t.Fatalf("calls %d", calls)
	}
}

func TestAPIErrorInline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		io.WriteString(w, `{"error":{"message":"quota exceeded"}}`)
	}))
	defer srv.Close()
	a := newTestApp(t)
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: newChatTitle,
		Config:   Config{Model: "demo", BaseURL: srv.URL + "/v1", APIKey: "secret", Temperature: f64p(0.7)},
		Messages: []Message{},
	}}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	focusDraft(t, tt)
	tt.Type("Hello")
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	errText := a.active().Messages[len(a.active().Messages)-1].Error
	if !strings.Contains(errText, "402") || !strings.Contains(errText, "quota exceeded") {
		t.Fatalf("error %q", errText)
	}
	if !tt.HasText("quota exceeded") {
		t.Fatalf("error not inline: %q", tt.Texts())
	}
}

func TestStopButton(t *testing.T) {
	a := newTestApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	a.streams = map[string]*convStream{"c1": {gen: 1, cancel: cancel}}
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Live", Config: DefaultConfig(),
		Messages: []Message{
			{ID: "u", Role: roleUser, Content: "Hi", Done: true},
			{ID: "a", Role: roleAssistant, Content: "Partial", Done: false},
		},
	}}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	if !tt.HasText("Generating") {
		t.Fatalf("no streaming indicator: %q", tt.Texts())
	}
	if err := tt.Click("Stop"); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() == nil {
		t.Fatal("stop did not cancel the request")
	}
	msg := a.active().Messages[1]
	if !msg.Done || msg.Content != "Partial" {
		t.Fatalf("after stop: %+v", msg)
	}
	if a.isStreaming("c1") {
		t.Fatal("still streaming")
	}
}

func TestJumpToLatest(t *testing.T) {
	a := newTestApp(t)
	msgs := make([]Message, 0, 40)
	for i := range 40 {
		msgs = append(msgs, Message{
			ID:      newID(),
			Role:    roleUser,
			Content: strings.Repeat("Line of a long conversation. ", 8),
			Done:    true,
		})
		_ = i
	}
	msgs[len(msgs)-1].Content = "LAST MESSAGE MARKER"
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Long", Config: DefaultConfig(), Messages: msgs,
	}}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	if tt.HasText("Jump to latest") {
		t.Fatal("jump button showed while the list was at the end")
	}
	r, ok := tt.Find("LAST MESSAGE MARKER")
	if !ok {
		t.Fatal("latest message is not in view")
	}
	tt.Scroll(r.X+4, r.Y+4, 0, -4000)
	tt.Frame()
	if !tt.HasText("Jump to latest") {
		t.Fatal("scrolling up did not offer jump to latest")
	}
	if err := tt.Click("Jump to latest"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.chat.AtEnd() {
		t.Fatal("jump did not land at the end")
	}
}

func TestDarkAppearance(t *testing.T) {
	a := newTestApp(t)
	tt := openTester(a)
	tt.SetDark(true)
	if !a.dark {
		t.Fatal("dark appearance did not follow the system")
	}
	tt.SetDark(false)
	if a.dark {
		t.Fatal("light appearance did not follow the system")
	}
}

func TestSuggestionFillsDraft(t *testing.T) {
	a := newTestApp(t)
	tt := openTester(a)
	if err := tt.Click("New Chat"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(starters[0].Title); err != nil {
		t.Fatal(err)
	}
	if a.active().Draft != starters[0].Prompt {
		t.Fatalf("draft %q", a.active().Draft)
	}
}

func TestThinkingSection(t *testing.T) {
	a := newTestApp(t)
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Think", Config: DefaultConfig(), UpdatedAt: time.Now(), CreatedAt: time.Now(),
		Messages: []Message{
			{ID: "u", Role: roleUser, Content: "Why?", Done: true, CreatedAt: time.Now()},
			{ID: "a", Role: roleAssistant, Content: "Because.", Reasoning: "I weighed two options.", ThinkingMS: 12000, Done: true},
		},
	}}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	if !tt.HasText("Thought for 12s") {
		t.Fatalf("duration label missing: %q", tt.Texts())
	}
	if tt.HasText("I weighed two options.") {
		t.Fatal("collapsed thinking was visible")
	}
	if err := tt.Click("Thought for 12s"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("I weighed two options.") {
		t.Fatalf("expanded thinking missing: %q", tt.Texts())
	}
	if !tt.HasText("Today") {
		t.Fatalf("sidebar group missing: %q", tt.Texts())
	}
	// Built with no stored model, so the footer does not borrow the chat model.
	if tt.HasText("gpt-4o-mini") {
		t.Fatal("unstamped reply showed the chat model")
	}
}

func TestEditMessage(t *testing.T) {
	a := newTestApp(t)
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Notes", Config: DefaultConfig(),
		Messages: []Message{
			{ID: "u", Role: roleUser, Content: "Please revise this", Done: true},
			{ID: "a", Role: roleAssistant, Content: "A reply", Done: true},
		},
	}}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	if err := tt.Click("Edit"); err != nil {
		t.Fatal(err)
	}
	conv := a.active()
	if a.edit.text != "Please revise this" || conv.Draft != "" || len(conv.Messages) != 2 || conv.Messages[1].Content != "A reply" {
		t.Fatalf("edit changed the thread: text %q draft %q messages %+v", a.edit.text, conv.Draft, conv.Messages)
	}
	if a.edit.msgID != "u" {
		t.Fatalf("edit target %q", a.edit.msgID)
	}
}

func TestReasoningStreamAndHistory(t *testing.T) {
	var calls int
	var second string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if calls == 2 {
			second = string(body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"step one\"}}]}\n\n")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Done thinking\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	a := newTestApp(t)
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: newChatTitle,
		Config: Config{
			Provider: "OpenAI", Model: "demo", BaseURL: srv.URL + "/v1",
			APIKey: "secret", Temperature: f64p(0.7), Reasoning: effortHigh,
		},
	}}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	focusDraft(t, tt)
	tt.Type("Question")
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	msg := a.active().Messages[len(a.active().Messages)-1]
	if msg.Content != "Done thinking" || msg.Reasoning != "step one" || msg.ReasoningOpen || msg.ThinkingMS <= 0 || !msg.Done {
		t.Fatalf("reply %+v", msg)
	}
	if !tt.HasText("Thought for") {
		t.Fatalf("collapsed label missing: %q", tt.Texts())
	}
	if tt.HasText("step one") {
		t.Fatal("reasoning stayed visible after the reply finished")
	}
	if err := tt.Click("Regenerate"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(second, "step one") || strings.Contains(second, "reasoning_content") {
		t.Fatalf("history included reasoning: %s", second)
	}
	if !strings.Contains(second, `"reasoning_effort":"high"`) {
		t.Fatalf("effort missing: %s", second)
	}
}

func TestLastRequestView(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	a := newTestApp(t)
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Relay",
		Config: Config{
			Provider: "DeepSeek", Model: "my-relay-model", BaseURL: srv.URL + "/v1",
			APIKey: "secret", Temperature: f64p(0.7), Reasoning: effortOff,
			ExtraBody: `{"reasoning_effort":"none"}`,
		},
	}}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	focusDraft(t, tt)
	tt.Type("Hello")
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `"thinking":{"type":"disabled"}`) || !strings.Contains(got, `"reasoning_effort":"none"`) {
		t.Fatalf("body %s", got)
	}
	if !strings.Contains(got, `"model":"my-relay-model"`) {
		t.Fatalf("model rewritten: %s", got)
	}
	if strings.Contains(a.active().LastRequest, "secret") {
		t.Fatalf("key leaked into the debug view: %s", a.active().LastRequest)
	}
	if !strings.Contains(a.active().LastRequest, "Hello") {
		t.Fatalf("full body dropped the message: %s", a.active().LastRequest)
	}
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Last request"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText(`"reasoning_effort": "none"`) || !tt.HasText(`"type": "disabled"`) || !tt.HasText("Hello") {
		t.Fatalf("full debug view missing the body: %q", tt.Texts())
	}
	short := abbreviateRequest(a.active().LastRequest)
	if strings.Contains(short, "Hello") || !strings.Contains(short, "… 1 messages …") {
		t.Fatalf("settings preview %s", short)
	}
}

func TestLateStreamCallbackAfterStop(t *testing.T) {
	a := newTestApp(t)
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Live",
		Messages: []Message{{ID: "m", Role: roleAssistant, Content: "Hello", Done: false}},
	}}
	a.data.ActiveID = "c1"
	_, cancel := context.WithCancel(context.Background())
	a.streams["c1"] = &convStream{gen: 1, cancel: cancel}
	a.stopChat("c1")
	msg := a.data.Conversations[0].Messages[0]
	if !msg.Done || msg.Content != "Hello" {
		t.Fatalf("after stop %+v", msg)
	}
	if a.isStreaming("c1") {
		t.Fatal("chat still streaming")
	}
	a.applyStream("c1", "m", 1, "Hello MORE", "late thought")
	msg = a.data.Conversations[0].Messages[0]
	if msg.Content != "Hello" || msg.Reasoning != "" {
		t.Fatalf("late callback wrote %+v", msg)
	}
	// Stop ran before the 40ms batch was posted. The finishing reply still
	// flushes what it had already received.
	a.finishReply("c1", "m", 1, "Hello there", "thought", nil)
	msg = a.data.Conversations[0].Messages[0]
	if msg.Content != "Hello there" || msg.Reasoning != "thought" || !msg.Done {
		t.Fatalf("unflushed batch dropped: %+v", msg)
	}
	a.finishReply("c1", "m", 1, "newer reply", "", nil)
	if a.data.Conversations[0].Messages[0].Content != "Hello there" {
		t.Fatal("a second late finish overwrote the reply")
	}
}

func TestSendWhileAnotherChatStreams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"pong\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	a := newTestApp(t)
	_, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	now := time.Now()
	a.data.Conversations = []Conversation{
		{ID: "c1", Title: "Busy", UpdatedAt: now, CreatedAt: now, Config: Config{Provider: "OpenAI", Model: "m", BaseURL: srv.URL, APIKey: "k"}},
		{ID: "c2", Title: "Free", UpdatedAt: now.Add(-time.Hour), CreatedAt: now, Config: Config{Provider: "OpenAI", Model: "m", BaseURL: srv.URL, APIKey: "k"}},
	}
	a.data.ActiveID = "c1"
	a.streams["c1"] = &convStream{gen: 1, cancel: cancel}
	a.data.ActiveID = "c2"
	a.send("hello from the other chat")
	if a.notice != "" {
		t.Fatal(a.notice)
	}
	free := a.conversation("c2")
	if free == nil || len(free.Messages) < 2 || free.Messages[0].Content != "hello from the other chat" || free.Messages[1].Content != "pong" {
		t.Fatalf("other chat %+v", free)
	}
	if !a.isStreaming("c1") {
		t.Fatal("the busy chat was stopped")
	}
	a.data.ActiveID = "c1"
	a.send("nope")
	if a.notice == "" {
		t.Fatal("the streaming chat accepted another send")
	}
}

// TestThinkingMotionFrames steps a long thread's thinking disclosure and
// records the reply's first line. Collapsing and expanding must not snap
// on the last frame: at the end of the transcript the line stays put, and
// away from the end it eases with the panel.
func TestThinkingMotionFrames(t *testing.T) {
	reasoning := strings.Repeat("Weigh the crash window against the directory update. ", 6)
	const reply = "Reply line stays put."
	const header = "Thought for 34s"
	stamp := time.Now()
	build := func(after int) []Message {
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
		for i := 0; i < after; i++ {
			msgs = append(msgs,
				Message{ID: fmt.Sprintf("ub%d", i), Role: roleUser, Content: fmt.Sprintf("Follow-up %d after the tracked reply.", i), Done: true, CreatedAt: stamp},
				Message{ID: fmt.Sprintf("ab%d", i), Role: roleAssistant, Content: fmt.Sprintf("Follow-up answer %d keeps the thread going past the thinking block.", i), Done: true, CreatedAt: stamp},
			)
		}
		return msgs
	}
	for _, instant := range []bool{false, true} {
		t.Run(fmt.Sprintf("instant=%v", instant), func(t *testing.T) {
			t.Run("end", func(t *testing.T) {
				trackThinking(t, instant, build(0), "end")
			})
			t.Run("mid", func(t *testing.T) {
				trackThinking(t, instant, build(4), "mid")
			})
			t.Run("near", func(t *testing.T) {
				trackThinking(t, instant, build(0), "near")
			})
		})
	}
}

func trackThinking(t *testing.T, instant bool, msgs []Message, place string) {
	t.Helper()
	const reply = "Reply line stays put."
	const header = "Thought for 34s"
	a := newTestApp(t)
	a.instant = instant
	now := time.Now()
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Long thread", Config: DefaultConfig(), UpdatedAt: now, CreatedAt: now,
		Messages: msgs,
	}}
	a.data.ActiveID = "c1"
	tt := ui.NewTester(a.view, 1120, 760)
	if _, ok := tt.Find(header); !ok {
		t.Fatalf("header missing: %q", tt.Texts())
	}
	switch place {
	case "end":
		r, ok := tt.Find(reply)
		if !ok || r.H < 1 || !a.chat.AtEnd() {
			t.Fatalf("reply at the end: %v ok=%v atEnd=%v", r, ok, a.chat.AtEnd())
		}
	case "mid":
		placeReply(t, tt, a, reply, 180, 460)
	case "near":
		// A short scroll leaves the end, still close enough that collapsing
		// the panel has to clamp back into the max offset.
		start, _ := tt.Find(reply)
		tt.Scroll(start.X+20, start.Y+8, 0, -50)
		r, ok := tt.Find(reply)
		if !ok || r.H < 1 || a.chat.AtEnd() || r.Y < start.Y+8 {
			t.Fatalf("near-end place: start %v now %v ok=%v atEnd=%v", start, r, ok, a.chat.AtEnd())
		}
	default:
		t.Fatalf("place %q", place)
	}
	start, _ := tt.Find(reply)
	collapse := sampleThinking(t, tt, a, header, reply)
	assertThinkingMotion(t, collapse, place, -1)
	expand := sampleThinking(t, tt, a, header, reply)
	assertThinkingMotion(t, expand, place, +1)
	if place == "end" || place == "mid" {
		end := expand[len(expand)-1]
		if d := end - start.Y; d < -1 || d > 1 {
			t.Fatalf("expand did not return to %.2f (ended at %.2f): collapse %v expand %v", start.Y, end, collapse, expand)
		}
	}
}

func placeReply(t *testing.T, tt *ui.Tester, a *App, reply string, minY, maxY float32) {
	t.Helper()
	for i := 0; i < 40; i++ {
		r, ok := tt.Find(reply)
		if ok && r.H > 1 && r.Y > minY && r.Y < maxY && !a.chat.AtEnd() {
			return
		}
		dy := float32(-70)
		if ok && r.H > 1 && r.Y >= maxY {
			dy = 70
		}
		x, y := float32(500), float32(400)
		if ok && r.H > 1 {
			x, y = r.X+20, r.Y+8
		}
		tt.Scroll(x, y, 0, dy)
	}
	r, _ := tt.Find(reply)
	t.Fatalf("could not place the reply: %v atEnd=%v", r, a.chat.AtEnd())
}

func sampleThinking(t *testing.T, tt *ui.Tester, a *App, header, reply string) []float32 {
	t.Helper()
	if err := tt.Click(header); err != nil {
		t.Fatal(err)
	}
	var ys []float32
	for i := 0; i < 20; i++ {
		tt.Advance(16 * time.Millisecond)
		r, ok := tt.Find(reply)
		if !ok || r.H < 1 {
			t.Fatalf("frame %d lost the reply (atEnd=%v)", i, a.chat.AtEnd())
		}
		ys = append(ys, r.Y)
	}
	t.Logf("reply y: %v", ys)
	return ys
}

func assertThinkingMotion(t *testing.T, ys []float32, place string, sign float32) {
	t.Helper()
	if len(ys) < 8 {
		t.Fatalf("too few frames: %v", ys)
	}
	if place == "end" {
		for i, y := range ys {
			d := y - ys[0]
			if d < -0.6 || d > 0.6 {
				t.Fatalf("frame %d left the anchor by %.2f: %v", i, d, ys)
			}
		}
		return
	}
	eased := false
	for i := 1; i < len(ys); i++ {
		d := ys[i] - ys[i-1]
		if sign < 0 && d > 0.6 || sign > 0 && d < -0.6 {
			t.Fatalf("frame %d reversed by %.2f: %v", i, d, ys)
		}
		ad := d
		if ad < 0 {
			ad = -ad
		}
		if eased && ad > 2 {
			t.Fatalf("frame %d snapped by %.2f after the motion had eased: %v", i, ad, ys)
		}
		if ad < 1 {
			eased = true
		}
	}
	n := len(ys)
	if d := ys[n-1] - ys[n-2]; d < -0.6 || d > 0.6 {
		t.Fatalf("last frame jumped by %.2f: %v", d, ys)
	}
}
