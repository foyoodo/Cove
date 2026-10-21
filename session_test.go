package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sessionServer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv, &bodies
}

func sessionApp(t *testing.T) (*App, *[]string) {
	t.Helper()
	srv, bodies := sessionServer(t)
	a := newTestApp(t)
	a.data.Defaults.BaseURL = srv.URL + "/v1"
	a.data.Defaults.APIKey = "secret"
	a.data.Defaults.Model = "demo"
	a.data.Defaults.SystemPrompt = "SESSION-PROMPT"
	a.data.Defaults.Provider = "OpenAI"
	return a, bodies
}

func decodeReq(t *testing.T, body string) []apiMessage {
	t.Helper()
	var payload struct {
		Messages []apiMessage `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("request: %v\n%s", err, body)
	}
	return payload.Messages
}

func assertSystemAndUsers(t *testing.T, body string, users ...string) {
	t.Helper()
	msgs := decodeReq(t, body)
	if len(msgs) == 0 || msgs[0].Role != "system" || msgs[0].Content != "SESSION-PROMPT" {
		t.Fatalf("system prompt missing: %+v", msgs)
	}
	var got []string
	for _, m := range msgs[1:] {
		if m.Role == "user" {
			got = append(got, m.Content)
		}
	}
	if strings.Join(got, "|") != strings.Join(users, "|") {
		t.Fatalf("user messages %q, want %q (full %+v)", got, users, msgs)
	}
}

func TestSessionManualBreak(t *testing.T) {
	a, bodies := sessionApp(t)
	a.newChat()
	a.send("before")
	if !a.CanInsertBreak() || !a.InsertSessionBreak() {
		t.Fatal("manual break was refused")
	}
	if a.InsertSessionBreak() || a.CanInsertBreak() {
		t.Fatal("a second break on an empty session was stored")
	}
	if len(a.active().Breaks) != 1 || a.active().Breaks[0].Kind != sessionManual || a.active().Breaks[0].Limit != 0 {
		t.Fatalf("break %+v", a.active().Breaks)
	}
	a.send("after")
	assertSystemAndUsers(t, (*bodies)[len(*bodies)-1], "after")
	if strings.Contains((*bodies)[len(*bodies)-1], "before") {
		t.Fatalf("manual break still sent the earlier turn: %s", (*bodies)[len(*bodies)-1])
	}
}

func TestSessionAutoBreakAtLimit(t *testing.T) {
	a, bodies := sessionApp(t)
	a.newChat()
	a.SetMaxContextRounds(2)
	a.send("one")
	a.send("two")
	if len(a.active().Breaks) != 0 {
		t.Fatalf("broke before the limit: %+v", a.active().Breaks)
	}
	assertSystemAndUsers(t, (*bodies)[1], "one", "two")
	a.send("three")
	br := a.active().Breaks
	if len(br) != 1 || br[0].Kind != sessionAuto || br[0].Limit != 2 || br[0].ID == "" || br[0].CreatedAt.IsZero() {
		t.Fatalf("auto break %+v", br)
	}
	if br[0].After != 4 {
		t.Fatalf("auto break sits at %d, want just before the third user message", br[0].After)
	}
	assertSystemAndUsers(t, (*bodies)[2], "three")
	id := a.active().ID
	delete(writers, storeRoot(a.path))
	got, err := Load(a.path)
	if err != nil {
		t.Fatal(err)
	}
	var conv *Conversation
	for i := range got.Conversations {
		if got.Conversations[i].ID == id {
			conv = &got.Conversations[i]
		}
	}
	if conv == nil || conv.MaxRounds != 2 || len(conv.Breaks) != 1 || conv.Breaks[0].Kind != sessionAuto || conv.Breaks[0].Limit != 2 {
		t.Fatalf("auto break did not survive restart: %+v", conv)
	}
}

func TestSessionUnlimitedSendsFullHistory(t *testing.T) {
	a, bodies := sessionApp(t)
	a.newChat()
	if a.active().MaxRounds != 0 {
		t.Fatal("new chat did not start unlimited")
	}
	a.send("one")
	a.send("two")
	a.send("three")
	if len(a.active().Breaks) != 0 {
		t.Fatalf("unlimited chat broke: %+v", a.active().Breaks)
	}
	assertSystemAndUsers(t, (*bodies)[2], "one", "two", "three")
}

func TestSessionEditBeforeBreakDropsIt(t *testing.T) {
	a, bodies := sessionApp(t)
	a.newChat()
	a.send("keep")
	if !a.InsertSessionBreak() {
		t.Fatal("no break")
	}
	a.send("later")
	conv := a.active()
	if len(conv.Breaks) != 1 || len(conv.Messages) != 4 {
		t.Fatalf("setup %+v breaks %+v", conv.Messages, conv.Breaks)
	}
	a.editMessage(conv.ID, conv.Messages[0].ID)
	a.edit.text = "revised"
	a.submitEdit()
	conv = a.active()
	if len(conv.Breaks) != 0 {
		t.Fatalf("edit left the break %+v", conv.Breaks)
	}
	if len(conv.Messages) != 2 || conv.Messages[0].Content != "revised" {
		t.Fatalf("thread %+v", conv.Messages)
	}
	assertSystemAndUsers(t, (*bodies)[len(*bodies)-1], "revised")
	if strings.Contains((*bodies)[len(*bodies)-1], "later") || strings.Contains((*bodies)[len(*bodies)-1], "keep") {
		t.Fatalf("edit resent the dropped tail: %s", (*bodies)[len(*bodies)-1])
	}
}

func TestSessionDeleteTrailingBreak(t *testing.T) {
	a, bodies := sessionApp(t)
	a.newChat()
	a.send("kept")
	if !a.InsertSessionBreak() {
		t.Fatal("no break")
	}
	if !a.DeleteTrailingBreak() {
		t.Fatal("empty trailing break was not deleted")
	}
	if len(a.active().Breaks) != 0 || a.DeleteTrailingBreak() {
		t.Fatalf("breaks after delete %+v", a.active().Breaks)
	}
	a.send("next")
	assertSystemAndUsers(t, (*bodies)[len(*bodies)-1], "kept", "next")

	if !a.InsertSessionBreak() {
		t.Fatal("no second break")
	}
	a.send("after")
	if a.DeleteTrailingBreak() {
		t.Fatal("deleted a break that already has a later turn")
	}
	if len(a.active().Breaks) != 1 {
		t.Fatalf("breaks %+v", a.active().Breaks)
	}
}

func TestSessionRestartAndIndex(t *testing.T) {
	a, _ := sessionApp(t)
	a.newChat()
	a.SetMaxContextRounds(3)
	a.send("before")
	before, err := os.ReadFile(chatPath(t, a))
	if err != nil {
		t.Fatal(err)
	}
	if !a.InsertSessionBreak() {
		t.Fatal("no break")
	}
	after, err := os.ReadFile(chatPath(t, a))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(after), string(before)) {
		t.Fatal("session break rewrote the chat instead of appending")
	}
	if strings.Count(string(after), `"type":"session"`) != 1 || !strings.Contains(string(after), `"kind":"manual"`) {
		t.Fatalf("session record missing: %s", after)
	}
	if !strings.Contains(string(after), `"schema":5`) || strings.Contains(string(after), `"maxRounds":3`) == false {
		t.Fatalf("header: %s", after)
	}
	id := a.active().ID
	breakID := a.active().Breaks[0].ID
	created := a.active().Breaks[0].CreatedAt

	idxRaw, err := os.ReadFile(filepath.Join(filepath.Dir(a.path), indexFile))
	if err != nil {
		t.Fatal(err)
	}
	var idx indexDoc
	if err := json.Unmarshal(idxRaw, &idx); err != nil {
		t.Fatal(err)
	}
	if idx.Schema != 5 {
		t.Fatalf("index schema %d", idx.Schema)
	}
	var count int
	for _, c := range idx.Chats {
		if c.ID == id {
			count = c.MessageCount
		}
	}
	if count != 2 {
		t.Fatalf("index counted the break as a message: %d", count)
	}
	if strings.Contains(string(idxRaw), "session") {
		t.Fatalf("index stored the break: %s", idxRaw)
	}

	delete(writers, storeRoot(a.path))
	got, err := Load(a.path)
	if err != nil {
		t.Fatal(err)
	}
	var conv *Conversation
	for i := range got.Conversations {
		if got.Conversations[i].ID == id {
			conv = &got.Conversations[i]
		}
	}
	if conv == nil || conv.MaxRounds != 3 || len(conv.Breaks) != 1 {
		t.Fatalf("reloaded %+v", conv)
	}
	if conv.Breaks[0].ID != breakID || conv.Breaks[0].Kind != sessionManual || !conv.Breaks[0].CreatedAt.Equal(created) {
		t.Fatalf("break did not survive: %+v", conv.Breaks[0])
	}
	if conv.Breaks[0].After != 2 {
		t.Fatalf("break position %d", conv.Breaks[0].After)
	}
}

func TestSessionOldChatUnchanged(t *testing.T) {
	srv, bodies := sessionServer(t)
	dir := t.TempDir()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	header := `{"type":"chat","schema":5,"id":"old","createdAt":"2026-01-02T03:04:05Z","title":"Old","settings":{}}`
	u := `{"type":"message","id":"u1","role":"user","content":"from-disk","createdAt":"2026-01-02T03:04:06Z","status":"complete"}`
	asst := `{"type":"message","id":"a1","role":"assistant","content":"stored-reply","createdAt":"2026-01-02T03:04:07Z","status":"complete"}`
	future := `{"type":"future","id":"nope"}`
	raw := header + "\n" + u + "\n" + future + "\n" + asst + "\n"
	if err := os.MkdirAll(filepath.Join(dir, chatsDir), 0o700); err != nil {
		t.Fatal(err)
	}
	chat := filepath.Join(dir, chatsDir, "old.jsonl")
	if err := os.WriteFile(chat, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(chat)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := readChat(chat, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	afterRead, err := os.ReadFile(chat)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(afterRead) {
		t.Fatal("reading an old chat rewrote it")
	}
	if loaded.MaxRounds != 0 || len(loaded.Breaks) != 0 || len(loaded.Messages) != 2 {
		t.Fatalf("old chat %+v breaks %+v", loaded.Messages, loaded.Breaks)
	}
	if loaded.Messages[0].Content != "from-disk" || loaded.Messages[1].Content != "stored-reply" {
		t.Fatalf("messages %+v", loaded.Messages)
	}

	data := defaultData()
	data.Defaults.BaseURL = srv.URL + "/v1"
	data.Defaults.Model = "demo"
	data.Defaults.SystemPrompt = "SESSION-PROMPT"
	data.Defaults.Provider = "OpenAI"
	data.MaxRounds = 4
	data.ActiveID = "old"
	data.Conversations = []Conversation{{
		ID: "old", Title: "Old", CreatedAt: now, UpdatedAt: now,
		Config: data.Defaults, MessageCount: 2,
	}}
	path := filepath.Join(dir, "conversations.json")
	if err := Save(path, &data); err != nil {
		t.Fatal(err)
	}
	// Save of an unloaded index row must not rewrite the chat file.
	if got, err := os.ReadFile(chat); err != nil || string(got) != raw {
		t.Fatalf("save rewrote the old chat:\n%s", got)
	}
	delete(writers, dir)
	a := openApp(path, newMemKeys())
	a.instant = true
	a.data.Defaults.APIKey = "secret"
	a.ensureChat("old")
	conv := a.conversation("old")
	if conv == nil || conv.MaxRounds != 0 || len(conv.Breaks) != 0 || len(conv.Messages) != 2 {
		t.Fatalf("opened old chat %+v", conv)
	}
	conv.Config.APIKey = "secret"
	conv.Config.SystemPrompt = "SESSION-PROMPT"
	a.data.ActiveID = "old"
	a.send("fresh")
	assertSystemAndUsers(t, (*bodies)[0], "from-disk", "fresh")
	if strings.Contains((*bodies)[0], `"type":"session"`) {
		t.Fatal("request contained a session record")
	}
	disk, err := os.ReadFile(chat)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(disk), `"type":"session"`) || strings.Contains(string(disk), "maxRounds") {
		t.Fatalf("old chat gained session fields:\n%s", disk)
	}
	if !strings.Contains(string(disk), `"type":"future"`) || !strings.HasPrefix(string(disk), raw) {
		t.Fatal("sending rewrote the old chat instead of appending the new message")
	}
}

func TestSessionDefaultAppliesToNewChatsOnly(t *testing.T) {
	a, _ := sessionApp(t)
	a.newChat()
	a.send("existing")
	path := chatPath(t, a)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	a.SetDefaultMaxContextRounds(4)
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("changing the default rewrote an existing chat")
	}
	if a.conversation(a.data.ActiveID).MaxRounds != 0 {
		t.Fatal("existing chat picked up the new default")
	}
	settings, err := os.ReadFile(filepath.Join(filepath.Dir(a.path), settingsFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(settings), `"maxRounds": 4`) {
		t.Fatalf("settings default missing: %s", settings)
	}
	prev := a.data.ActiveID
	a.newChat()
	if a.data.ActiveID == prev || a.active().MaxRounds != 4 {
		t.Fatalf("new chat rounds %d", a.active().MaxRounds)
	}
	a.SetMaxContextRounds(150)
	if a.active().MaxRounds != 100 {
		t.Fatalf("clamp %d", a.active().MaxRounds)
	}
	a.SetMaxContextRounds(0)
	if a.active().MaxRounds != 0 {
		t.Fatal("zero was not stored as unlimited")
	}
}

func TestSessionPerChatMaxRoundsOverridesGlobal(t *testing.T) {
	a, bodies := sessionApp(t)
	a.SetDefaultMaxContextRounds(20)
	a.newChat()
	if a.data.MaxRounds != 20 || a.active().MaxRounds != 20 {
		t.Fatalf("new chat copied global %d chat %d", a.data.MaxRounds, a.active().MaxRounds)
	}
	a.SetMaxContextRounds(3)
	if a.data.MaxRounds != 20 || a.active().MaxRounds != 3 {
		t.Fatalf("per-chat limit global %d chat %d", a.data.MaxRounds, a.active().MaxRounds)
	}
	for _, text := range []string{"one", "two", "three"} {
		a.send(text)
	}
	if len(a.active().Breaks) != 0 {
		t.Fatalf("broke before the 4th round: %+v", a.active().Breaks)
	}
	a.send("four")
	br := a.active().Breaks
	if len(br) != 1 || br[0].Kind != sessionAuto || br[0].Limit != 3 || br[0].After != 6 {
		t.Fatalf("4th round break %+v", br)
	}
	if got := SessionLabel(br[0]); got != "New Session · auto (3-round limit)" {
		t.Fatalf("divider label %q", got)
	}
	assertSystemAndUsers(t, (*bodies)[3], "four")
	if strings.Contains((*bodies)[3], "one") || strings.Contains((*bodies)[3], "three") {
		t.Fatalf("4th round sent history before the divider: %s", (*bodies)[3])
	}

	a.SetDefaultMaxContextRounds(5)
	if a.data.MaxRounds != 5 || a.active().MaxRounds != 3 {
		t.Fatalf("global change rewrote the chat: global %d chat %d", a.data.MaxRounds, a.active().MaxRounds)
	}
	a.send("five")
	a.send("six")
	if len(a.active().Breaks) != 1 {
		t.Fatalf("broke before 3 rounds in the new session: %+v", a.active().Breaks)
	}
	a.send("seven")
	br = a.active().Breaks
	if len(br) != 2 || br[1].Kind != sessionAuto || br[1].Limit != 3 || br[1].After != 12 {
		t.Fatalf("next auto-break followed global 5, not the chat limit 3: %+v", br)
	}
	if got := SessionLabel(br[1]); got != "New Session · auto (3-round limit)" {
		t.Fatalf("second divider label %q", got)
	}
	assertSystemAndUsers(t, (*bodies)[len(*bodies)-1], "seven")
	if strings.Contains((*bodies)[len(*bodies)-1], "four") || strings.Contains((*bodies)[len(*bodies)-1], "six") {
		t.Fatalf("second break sent the session it cut: %s", (*bodies)[len(*bodies)-1])
	}
}

func TestSessionRoundsIgnoreEarlierSession(t *testing.T) {
	a, bodies := sessionApp(t)
	a.newChat()
	a.SetMaxContextRounds(2)
	a.send("old-one")
	a.send("old-two")
	if !a.InsertSessionBreak() {
		t.Fatal("no break")
	}
	a.send("new-one")
	if len(a.active().Breaks) != 1 {
		t.Fatalf("auto break counted rounds from before the manual break: %+v", a.active().Breaks)
	}
	assertSystemAndUsers(t, (*bodies)[len(*bodies)-1], "new-one")
	a.send("new-two")
	if len(a.active().Breaks) != 1 {
		t.Fatalf("broke inside the new session too early: %+v", a.active().Breaks)
	}
	assertSystemAndUsers(t, (*bodies)[len(*bodies)-1], "new-one", "new-two")
}

func TestSessionLabel(t *testing.T) {
	if got := SessionLabel(SessionBreak{Kind: sessionManual}); got != "New Session" {
		t.Fatalf("manual %q", got)
	}
	if got := SessionLabel(SessionBreak{Kind: sessionAuto, Limit: 1}); got != "New Session · auto (1-round limit)" {
		t.Fatalf("one %q", got)
	}
	if got := SessionLabel(SessionBreak{Kind: sessionAuto, Limit: 20}); got != "New Session · auto (20-round limit)" {
		t.Fatalf("twenty %q", got)
	}
	if got := SessionLabel(SessionBreak{Kind: sessionAuto}); got != "New Session · auto (1-round limit)" {
		t.Fatalf("missing limit %q", got)
	}
}

func TestSessionSuggestedLimitIsNotStored(t *testing.T) {
	if LimitRoundsInitial != 20 {
		t.Fatalf("initial %d", LimitRoundsInitial)
	}
	a, _ := sessionApp(t)
	a.newChat()
	a.send("hello")
	raw, err := os.ReadFile(chatPath(t, a))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "maxRounds") || a.active().MaxRounds != 0 {
		t.Fatalf("fresh chat stored a limit:\n%s", raw)
	}
}

func chatPath(t *testing.T, a *App) string {
	t.Helper()
	p, err := chatFile(storeRoot(a.path), a.active().ID)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
