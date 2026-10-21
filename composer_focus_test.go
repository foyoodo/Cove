package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func focusSendApp(t *testing.T) (*App, *ui.Tester) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"pong\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	a := newTestApp(t)
	connectBuiltin(a, "OpenAI", "sk-test")
	now := time.Now()
	cfg := Config{Provider: "OpenAI", Model: "gpt-4o-mini", ConnectionID: "conn-openai", BaseURL: srv.URL + "/v1"}
	a.data.Defaults = cfg
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Focus", CreatedAt: now, UpdatedAt: now, Loaded: true, Config: cfg,
	}}
	a.data.ActiveID = "c1"
	tt := openTester(a)
	focusDraft(t, tt)
	return a, tt
}

func armStream(t *testing.T, a *App) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if a.streams == nil {
		a.streams = map[string]*convStream{}
	}
	a.streams["c1"] = &convStream{gen: 1, cancel: cancel}
	_ = ctx
}

func TestComposerKeepsFocusAfterEnterSend(t *testing.T) {
	a, tt := focusSendApp(t)
	tt.Type("Hello")
	tt.Key(0, ui.KeyEnter)
	if a.active().Draft != "" || len(a.active().Messages) < 2 || a.active().Messages[0].Content != "Hello" {
		t.Fatalf("send %+v draft %q", a.active().Messages, a.active().Draft)
	}
	composerCaret(t, tt)
	tt.Frame()
	composerCaret(t, tt)
	tt.Type("Next")
	if a.active().Draft != "Next" {
		t.Fatalf("next draft %q", a.active().Draft)
	}
	composerCaret(t, tt)
}

func TestComposerKeepsFocusAfterButtonSend(t *testing.T) {
	a, tt := focusSendApp(t)
	tt.Type("Hello")
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	if a.active().Draft != "" || a.active().Messages[0].Content != "Hello" {
		t.Fatalf("button send draft %q messages %+v", a.active().Draft, a.active().Messages)
	}
	composerCaret(t, tt)
	tt.Type("Next")
	if a.active().Draft != "Next" {
		t.Fatalf("next draft %q", a.active().Draft)
	}
}

func TestComposerKeepsFocusAfterModSend(t *testing.T) {
	a, tt := focusSendApp(t)
	a.data.SendWith = "mod"
	tt.Frame()
	focusDraft(t, tt)
	tt.Type("Hello")
	tt.Key(0, ui.KeyEnter)
	if len(a.active().Messages) != 0 || a.active().Draft == "" {
		t.Fatalf("return sent under ⌘↵: draft %q messages %d", a.active().Draft, len(a.active().Messages))
	}
	tt.Key(ui.Cmd, ui.KeyEnter)
	if a.active().Draft != "" || a.active().Messages[0].Content != "Hello" {
		t.Fatalf("⌘↵ draft %q messages %+v", a.active().Draft, a.active().Messages)
	}
	composerCaret(t, tt)
}

func TestComposerKeepsFocusDuringStreaming(t *testing.T) {
	a, tt := focusSendApp(t)
	tt.Type("Hello")
	tt.Key(0, ui.KeyEnter)
	composerCaret(t, tt)
	armStream(t, a)
	tt.Frame()
	composerCaret(t, tt)
	tt.Type("draft")
	if a.active().Draft != "draft" {
		t.Fatalf("could not type while streaming: %q", a.active().Draft)
	}
	composerCaret(t, tt)
	tt.Frame()
	composerCaret(t, tt)
	tt.Key(0, ui.KeyEnter)
	if a.active().Draft != "draft" {
		t.Fatalf("streaming send cleared the draft: %q", a.active().Draft)
	}
	if a.notice != "Stop the current reply before sending another." {
		t.Fatalf("hint %q", a.notice)
	}
	composerCaret(t, tt)
}

func TestComposerKeepsFocusAfterStop(t *testing.T) {
	a, tt := focusSendApp(t)
	tt.Type("Hello")
	tt.Key(0, ui.KeyEnter)
	armStream(t, a)
	tt.Frame()
	composerCaret(t, tt)
	if err := tt.Click("Stop"); err != nil {
		t.Fatal(err)
	}
	if a.isStreaming("c1") {
		t.Fatal("still streaming")
	}
	composerCaret(t, tt)
	tt.Type("after stop")
	if a.active().Draft != "after stop" {
		t.Fatalf("draft %q", a.active().Draft)
	}
}

func TestComposerKeepsFocusAfterRegenerate(t *testing.T) {
	_, tt := focusSendApp(t)
	tt.Type("Hello")
	tt.Key(0, ui.KeyEnter)
	if err := tt.Click("Search"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Regenerate"); err != nil {
		t.Fatal(err)
	}
	composerCaret(t, tt)
}

func TestComposerKeepsFocusAfterEditSend(t *testing.T) {
	a, tt := focusSendApp(t)
	tt.Type("Hello")
	tt.Key(0, ui.KeyEnter)
	if err := tt.Click("Edit"); err != nil {
		t.Fatal(err)
	}
	if !tt.Focused("Edit message") {
		t.Fatal("inline editor did not take focus")
	}
	tt.Key(0, ui.KeyEnter)
	if a.edit.convID != "" {
		t.Fatal("edit still open")
	}
	composerCaret(t, tt)
	tt.Type("after edit")
	if a.active().Draft != "after edit" {
		t.Fatalf("draft %q", a.active().Draft)
	}
}

func TestComposerKeepsFocusAfterFindClose(t *testing.T) {
	a, tt := focusSendApp(t)
	tt.Type("Hello")
	tt.Key(0, ui.KeyEnter)
	tt.Key(ui.Cmd, ui.KeyF)
	if !a.finder.open || !tt.Focused("Find in chat") {
		t.Fatalf("find open=%v focused=%v", a.finder.open, tt.Focused("Find in chat"))
	}
	tt.Key(0, ui.KeyEscape)
	if a.finder.open {
		t.Fatal("find stayed open")
	}
	composerCaret(t, tt)
}

func TestComposerKeepsFocusAfterSheetDismiss(t *testing.T) {
	a, tt := focusSendApp(t)
	tt.Type("Hello")
	tt.Key(0, ui.KeyEnter)
	composerCaret(t, tt)
	tt.Key(ui.Cmd, ui.KeyBackspace)
	if a.confirmKind != confirmDelete || !tt.Focused("Cancel") {
		t.Fatalf("dialog %q cancel focused %v", a.confirmKind, tt.Focused("Cancel"))
	}
	if tt.Focused("Message") {
		t.Fatal("composer kept focus while the dialog was up")
	}
	tt.Key(0, ui.KeyEscape)
	if a.confirmKind != "" {
		t.Fatal("dialog stayed open")
	}
	composerCaret(t, tt)
}

func TestComposerFocusElsewhereIsRespected(t *testing.T) {
	a, tt := focusSendApp(t)
	tt.Type("Hello")
	tt.Key(0, ui.KeyEnter)
	armStream(t, a)
	tt.Frame()
	if err := tt.Click("Search"); err != nil {
		t.Fatal(err)
	}
	if !tt.Focused("Search") || tt.Focused("Message") {
		t.Fatal("search did not take focus")
	}
	tt.Frame()
	tt.Frame()
	if tt.Focused("Message") || !tt.Focused("Search") {
		t.Fatal("streaming re-render stole focus back to the composer")
	}
	tt.Key(ui.Cmd, ui.KeyBackspace)
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	if tt.Focused("Message") {
		t.Fatal("dismissing the dialog pulled focus off Search")
	}
	if !tt.Focused("Search") {
		t.Fatalf("search lost focus, texts %q", tt.Texts())
	}
}

func TestComposerKeepsFocusAfterFailedSend(t *testing.T) {
	a, tt := focusSendApp(t)
	a.data.Connections = nil
	a.active().Config = Config{}
	tt.Frame()
	focusDraft(t, tt)
	tt.Type("keep me")
	tt.Key(0, ui.KeyEnter)
	if len(a.active().Messages) != 0 || a.active().Draft != "keep me" {
		t.Fatalf("failed send messages %d draft %q", len(a.active().Messages), a.active().Draft)
	}
	composerCaret(t, tt)
	tt.Type(" still")
	if a.active().Draft != "keep me still" {
		t.Fatalf("draft %q", a.active().Draft)
	}
}
