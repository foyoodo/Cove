package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func replyServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var models []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &req)
		mu.Lock()
		models = append(models, req.Model)
		n := len(models)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"reply %d\"}}]}\n\n", n)
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), models...)
	}
}

func modelChatApp(t *testing.T, base string) *App {
	t.Helper()
	a := newTestApp(t)
	a.data.Connections = []Connection{
		{
			ID: "conn-openai", Name: "Work OpenAI", Provider: "OpenAI", BaseURL: base,
			ModelsSet: true,
			Models:    []ProviderModel{{ID: "gpt-4o", Enabled: true}, {ID: "o3-mini", Enabled: true}},
		},
		{
			ID: "conn-anthropic", Name: "Anthropic", Provider: "Anthropic", BaseURL: base,
			ModelsSet: true,
			Models:    []ProviderModel{{ID: "claude-sonnet-4.5", Enabled: true}},
		},
	}
	if err := a.keys.Set("conn-openai", "sk-openai"); err != nil {
		t.Fatal(err)
	}
	if err := a.keys.Set("conn-anthropic", "sk-anthropic"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 20, 15, 4, 0, 0, time.UTC)
	cfg := Config{Provider: "Work OpenAI", Model: "gpt-4o", ConnectionID: "conn-openai", BaseURL: base}
	a.data.Defaults = Config{Provider: "Anthropic", Model: "claude-sonnet-4.5", ConnectionID: "conn-anthropic", BaseURL: base}
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Model labels", CreatedAt: now, UpdatedAt: now, Loaded: true,
		Config: cfg, Messages: []Message{},
	}}
	a.data.ActiveID = "c1"
	return a
}

func assistantAt(t *testing.T, conv *Conversation, n int) Message {
	t.Helper()
	seen := 0
	for _, m := range conv.Messages {
		if m.Role != roleAssistant {
			continue
		}
		if seen == n {
			return m
		}
		seen++
	}
	t.Fatalf("assistant %d missing in %d messages", n, len(conv.Messages))
	return Message{}
}

func assertStamp(t *testing.T, m Message, name, kind, id, model string, custom bool) {
	t.Helper()
	if m.ProviderName != name || m.ProviderKind != kind || m.ProviderID != id || m.Model != model || m.ProviderCustom != custom {
		t.Fatalf("stamp name %q kind %q id %q model %q custom %v", m.ProviderName, m.ProviderKind, m.ProviderID, m.Model, m.ProviderCustom)
	}
	want := name + " · " + model
	if name == "" {
		want = model
	}
	if got := replyModelLabel(&m); got != want {
		t.Fatalf("label %q, want %q", got, want)
	}
}

func TestMessageModelStaysAfterSwitch(t *testing.T) {
	srv, models := replyServer(t)
	a := modelChatApp(t, srv.URL+"/v1")
	a.send("First question")
	first := assistantAt(t, a.active(), 0)
	assertStamp(t, first, "Work OpenAI", "OpenAI", "conn-openai", "gpt-4o", false)
	if got := models(); len(got) != 1 || got[0] != "gpt-4o" {
		t.Fatalf("request models %v", got)
	}

	a.useModelChat("c1", "conn-anthropic", "claude-sonnet-4.5")
	if a.active().Config.Model != "claude-sonnet-4.5" || a.active().Config.ConnectionID != "conn-anthropic" {
		t.Fatalf("composer %+v", a.active().Config)
	}
	held := assistantAt(t, a.active(), 0)
	assertStamp(t, held, "Work OpenAI", "OpenAI", "conn-openai", "gpt-4o", false)

	tt := openTester(a)
	if _, ok := tt.Find("Work OpenAI · gpt-4o"); !ok {
		t.Fatalf("first label followed the composer: %q", tt.Texts())
	}
	if _, ok := tt.Find("Anthropic · claude-sonnet-4.5"); ok {
		t.Fatal("first reply drew the model selected after it was sent")
	}
	if !tt.HasText("claude-sonnet-4.5") {
		t.Fatal("title kept the old model")
	}

	a.send("Second question")
	second := assistantAt(t, a.active(), 1)
	assertStamp(t, second, "Anthropic", "Anthropic", "conn-anthropic", "claude-sonnet-4.5", false)
	assertStamp(t, assistantAt(t, a.active(), 0), "Work OpenAI", "OpenAI", "conn-openai", "gpt-4o", false)
	if got := models(); len(got) != 2 || got[1] != "claude-sonnet-4.5" {
		t.Fatalf("request models %v", got)
	}

	tt = openTester(a)
	if _, ok := tt.Find("Work OpenAI · gpt-4o"); !ok {
		t.Fatalf("first label changed after the second send: %q", tt.Texts())
	}
	if _, ok := tt.Find("Anthropic · claude-sonnet-4.5"); !ok {
		t.Fatalf("second label missing: %q", tt.Texts())
	}
	if !strings.Contains(a.subtleModel(a.active()), "claude-sonnet-4.5") {
		t.Fatalf("subtitle %q", a.subtleModel(a.active()))
	}
}

func TestMessageModelPersistsAcrossReload(t *testing.T) {
	srv, _ := replyServer(t)
	a := modelChatApp(t, srv.URL+"/v1")
	a.send("First question")
	a.useModelChat("c1", "conn-anthropic", "claude-sonnet-4.5")
	a.send("Second question")

	file := filepath.Join(filepath.Dir(a.path), chatsDir, "c1.jsonl")
	recs := assistantRecords(t, file)
	if len(recs) != 2 {
		t.Fatalf("records %+v", recs)
	}
	if recs[0].Model != "gpt-4o" || recs[0].ProviderName != "Work OpenAI" || recs[0].ProviderID != "conn-openai" || recs[0].ProviderKind != "OpenAI" {
		t.Fatalf("first record %+v", recs[0])
	}
	if recs[1].Model != "claude-sonnet-4.5" || recs[1].ProviderName != "Anthropic" || recs[1].ProviderID != "conn-anthropic" || recs[1].ProviderKind != "Anthropic" {
		t.Fatalf("second record %+v", recs[1])
	}

	b := openApp(a.path, newMemKeys())
	b.instant = true
	b.ensureChat("c1")
	b.data.ActiveID = "c1"
	conv := b.conversation("c1")
	if conv == nil {
		t.Fatal("reload missed the chat")
	}
	assertStamp(t, assistantAt(t, conv, 0), "Work OpenAI", "OpenAI", "conn-openai", "gpt-4o", false)
	assertStamp(t, assistantAt(t, conv, 1), "Anthropic", "Anthropic", "conn-anthropic", "claude-sonnet-4.5", false)
	if conv.Config.Model != "claude-sonnet-4.5" {
		t.Fatalf("reloaded chat model %q", conv.Config.Model)
	}
	tt := openTester(b)
	if _, ok := tt.Find("Work OpenAI · gpt-4o"); !ok {
		t.Fatalf("first label missing after reload: %q", tt.Texts())
	}
	if _, ok := tt.Find("Anthropic · claude-sonnet-4.5"); !ok {
		t.Fatalf("second label missing after reload: %q", tt.Texts())
	}
}

func TestMessageModelSurvivesRename(t *testing.T) {
	srv, _ := replyServer(t)
	a := modelChatApp(t, srv.URL+"/v1")
	a.send("First question")
	a.viewConnection("conn-openai")
	a.provEdit.name = "Renamed AI"
	a.saveProviderName(time.Now())
	if c := a.connByID("conn-openai"); c == nil || c.Name != "Renamed AI" {
		t.Fatalf("connection %+v", a.connByID("conn-openai"))
	}
	assertStamp(t, assistantAt(t, a.active(), 0), "Work OpenAI", "OpenAI", "conn-openai", "gpt-4o", false)

	file := filepath.Join(filepath.Dir(a.path), chatsDir, "c1.jsonl")
	recs := assistantRecords(t, file)
	if len(recs) != 1 || recs[0].ProviderName != "Work OpenAI" || recs[0].Model != "gpt-4o" || recs[0].ProviderKind != "OpenAI" {
		t.Fatalf("record after rename %+v", recs)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "Renamed AI") {
		t.Fatalf("rename rewrote the message:\n%s", raw)
	}
	tt := openTester(a)
	if _, ok := tt.Find("Work OpenAI · gpt-4o"); !ok {
		t.Fatalf("label followed the rename: %q", tt.Texts())
	}
	if tt.HasText("Renamed AI") {
		t.Fatalf("renamed provider landed on the message: %q", tt.Texts())
	}
}

func TestMessageModelSurvivesDelete(t *testing.T) {
	srv, _ := replyServer(t)
	a := modelChatApp(t, srv.URL+"/v1")
	a.send("First question")
	a.deleteProvider("conn-openai", time.Now())
	if a.connByID("conn-openai") != nil {
		t.Fatal("provider still present")
	}
	if a.active().Config.Model != "claude-sonnet-4.5" || a.active().Config.ConnectionID != "conn-anthropic" {
		t.Fatalf("chat did not switch: %+v", a.active().Config)
	}
	assertStamp(t, assistantAt(t, a.active(), 0), "Work OpenAI", "OpenAI", "conn-openai", "gpt-4o", false)

	file := filepath.Join(filepath.Dir(a.path), chatsDir, "c1.jsonl")
	recs := assistantRecords(t, file)
	if len(recs) != 1 || recs[0].ProviderName != "Work OpenAI" || recs[0].Model != "gpt-4o" || recs[0].ProviderKind != "OpenAI" || recs[0].ProviderID != "conn-openai" {
		t.Fatalf("record after delete %+v", recs)
	}
	tt := openTester(a)
	showReply(t, tt, "reply 1")
	label := mustFind(t, tt, "Work OpenAI · gpt-4o")
	assertKindDot(t, tt.Image(), label, a.look.ProviderOpenai, a.look.ProviderAnthropic)
	assertKindDot(t, tt.Image(), label, a.look.ProviderOpenai, a.look.ProviderCustom)
	if !strings.Contains(a.subtleModel(a.active()), "claude-sonnet-4.5") {
		t.Fatalf("subtitle %q", a.subtleModel(a.active()))
	}
}

func TestMessageModelDotUsesStoredKind(t *testing.T) {
	srv, _ := replyServer(t)
	base := srv.URL + "/v1"
	a := newTestApp(t)
	a.data.Connections = []Connection{
		{
			ID: "named-openai", Name: "OpenAI", Provider: "Custom", Custom: true, BaseURL: base,
			ModelsSet: true, Models: []ProviderModel{{ID: "gpt-4o", Enabled: true}},
		},
		{
			ID: "conn-anthropic", Name: "Anthropic", Provider: "Anthropic", BaseURL: base,
			ModelsSet: true, Models: []ProviderModel{{ID: "claude-sonnet-4.5", Enabled: true}},
		},
	}
	if err := a.keys.Set("named-openai", "sk-custom"); err != nil {
		t.Fatal(err)
	}
	if err := a.keys.Set("conn-anthropic", "sk-anthropic"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 20, 15, 4, 0, 0, time.UTC)
	a.data.Defaults = Config{Provider: "Anthropic", Model: "claude-sonnet-4.5", ConnectionID: "conn-anthropic", BaseURL: base}
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Custom dot", CreatedAt: now, UpdatedAt: now, Loaded: true,
		Config: Config{Provider: "OpenAI", Model: "gpt-4o", ConnectionID: "named-openai", BaseURL: base},
	}}
	a.data.ActiveID = "c1"
	a.send("Hello")
	assertStamp(t, assistantAt(t, a.active(), 0), "OpenAI", "Custom", "named-openai", "gpt-4o", true)

	a.deleteProvider("named-openai", time.Now())
	assertStamp(t, assistantAt(t, a.active(), 0), "OpenAI", "Custom", "named-openai", "gpt-4o", true)
	if a.active().Config.ConnectionID != "conn-anthropic" {
		t.Fatalf("chat config %+v", a.active().Config)
	}
	tt := openTester(a)
	showReply(t, tt, "reply 1")
	label := mustFind(t, tt, "OpenAI · gpt-4o")
	assertKindDot(t, tt.Image(), label, a.look.ProviderCustom, a.look.ProviderOpenai)
}

func TestLegacyMessageHasNoModelLabel(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, chatsDir), 0o700); err != nil {
		t.Fatal(err)
	}
	chat := filepath.Join(dir, chatsDir, "old.jsonl")
	raw := strings.Join([]string{
		`{"type":"chat","schema":5,"id":"old","title":"Old chat","createdAt":"2026-10-20T12:00:00Z","settings":{"provider":"OpenAI","model":"gpt-4o-mini","connectionId":"conn-openai"}}`,
		`{"type":"message","id":"u","role":"user","content":"hi","createdAt":"2026-10-20T12:00:00Z","status":"complete"}`,
		`{"type":"message","id":"a","role":"assistant","content":"old reply","createdAt":"2026-10-20T12:00:01Z","status":"complete","usage":{"tokens":12}}`,
		`{"type":"message","id":"b","role":"assistant","content":"named reply","createdAt":"2026-10-20T12:00:02Z","status":"complete","model":"stored-only"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(chat, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := readChat(chat, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Config.Model != "gpt-4o-mini" {
		t.Fatalf("session model %q", loaded.Config.Model)
	}
	legacy := assistantAt(t, &loaded, 0)
	if legacy.Model != "" || legacy.ProviderName != "" || legacy.ProviderID != "" || legacy.ProviderKind != "" || replyModelLabel(&legacy) != "" {
		t.Fatalf("legacy stamp %+v label %q", legacy, replyModelLabel(&legacy))
	}
	if assistantMeta(&legacy) != "12 tokens" {
		t.Fatalf("legacy footer %q", assistantMeta(&legacy))
	}
	stored := assistantAt(t, &loaded, 1)
	if stored.Model != "stored-only" || stored.ProviderName != "" || replyModelLabel(&stored) != "stored-only" {
		t.Fatalf("model-only record %+v", stored)
	}

	a := newTestApp(t)
	a.data.Conversations = []Conversation{loaded}
	a.data.ActiveID = "old"
	a.active().Config.Model = "claude-sonnet-4.5"
	a.active().Config.Provider = "Anthropic"
	tt := openTester(a)
	if _, ok := tt.Find("12 tokens"); !ok {
		t.Fatalf("legacy footer picked up a model: %q", tt.Texts())
	}
	if _, ok := tt.Find("stored-only"); !ok {
		t.Fatalf("stored model missing: %q", tt.Texts())
	}
	for _, text := range tt.Texts() {
		if strings.Contains(text, "gpt-4o-mini  ·") || strings.Contains(text, "claude-sonnet-4.5  ·") || strings.Contains(text, "Anthropic ·") {
			t.Fatalf("legacy footer followed the session: %q", text)
		}
	}
	if assistantAt(t, a.active(), 0).Model != "" || assistantAt(t, a.active(), 1).Model != "stored-only" {
		t.Fatalf("switch rewrote stamps %+v", a.active().Messages)
	}
}

func TestRegenerateStampsModelUsed(t *testing.T) {
	srv, models := replyServer(t)
	a := modelChatApp(t, srv.URL+"/v1")
	a.send("First question")
	a.useModelChat("c1", "conn-anthropic", "claude-sonnet-4.5")
	a.send("Second question")
	a.useModelChat("c1", "conn-openai", "o3-mini")
	before := assistantAt(t, a.active(), 0)
	a.regenerate()
	assertStamp(t, assistantAt(t, a.active(), 0), "Work OpenAI", "OpenAI", "conn-openai", "gpt-4o", false)
	if assistantAt(t, a.active(), 0).Content != before.Content || assistantAt(t, a.active(), 0).ID != before.ID {
		t.Fatal("regenerate rewrote the earlier reply")
	}
	assertStamp(t, assistantAt(t, a.active(), 1), "Work OpenAI", "OpenAI", "conn-openai", "o3-mini", false)
	if got := models(); len(got) != 3 || got[2] != "o3-mini" {
		t.Fatalf("regenerate request %v", got)
	}
	tt := openTester(a)
	if _, ok := tt.Find("Work OpenAI · gpt-4o"); !ok {
		t.Fatalf("earlier label changed: %q", tt.Texts())
	}
	if _, ok := tt.Find("Work OpenAI · o3-mini"); !ok {
		t.Fatalf("regenerated label %q", tt.Texts())
	}
	if _, ok := tt.Find("Anthropic · claude-sonnet-4.5"); ok {
		t.Fatal("regenerated reply kept the previous model")
	}
}

func TestEditResendStampsModelUsed(t *testing.T) {
	srv, models := replyServer(t)
	a := modelChatApp(t, srv.URL+"/v1")
	a.send("First question")
	a.useModelChat("c1", "conn-anthropic", "claude-sonnet-4.5")
	a.send("Second question")
	a.useModelChat("c1", "conn-openai", "o3-mini")
	secondUser := ""
	for _, m := range a.active().Messages {
		if m.Role == roleUser {
			secondUser = m.ID
		}
	}
	a.editMessage("c1", secondUser)
	a.edit.text = "Second question, revised"
	a.submitEdit()
	assertStamp(t, assistantAt(t, a.active(), 0), "Work OpenAI", "OpenAI", "conn-openai", "gpt-4o", false)
	assertStamp(t, assistantAt(t, a.active(), 1), "Work OpenAI", "OpenAI", "conn-openai", "o3-mini", false)
	if got := models(); len(got) != 3 || got[2] != "o3-mini" {
		t.Fatalf("edit request %v", got)
	}
	if a.active().Messages[2].Content != "Second question, revised" {
		t.Fatalf("edited user %+v", a.active().Messages[2])
	}
	tt := openTester(a)
	if _, ok := tt.Find("Work OpenAI · gpt-4o"); !ok {
		t.Fatalf("earlier label changed: %q", tt.Texts())
	}
	if _, ok := tt.Find("Work OpenAI · o3-mini"); !ok {
		t.Fatalf("resent label %q", tt.Texts())
	}
}

func TestMessageModelCrops(t *testing.T) {
	a := messageModelScene(t)
	dir := "/cursor/stores/self/artifacts/msg-model"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{false, true} {
		tt := ui.NewTester(a.view, 1120, 760)
		if dark {
			tt.SetDark(true)
		}
		tt.SetScale(2)
		showReply(t, tt, "First reply, sent as gpt-4o.")
		// Find boxes are DIPs. The frame is device pixels.
		first := scaleRect(mustFind(t, tt, "Work OpenAI · gpt-4o"), 2)
		second := scaleRect(mustFind(t, tt, "Anthropic · claude-sonnet-4.5"), 2)
		assertKindDot(t, tt.Image(), first, a.look.ProviderOpenai, a.look.ProviderCustom)
		assertKindDot(t, tt.Image(), second, a.look.ProviderAnthropic, a.look.ProviderOpenai)
		if !tt.HasText("claude-sonnet-4.5 · api.anthropic.com") {
			t.Fatalf("subtitle %q", tt.Texts())
		}
		writePNG(t, filepath.Join(dir, "after-"+modeName(dark)+".png"), tt.Image())
	}
}

func messageModelScene(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	now := time.Date(2026, 10, 20, 15, 4, 0, 0, time.UTC)
	a.data.Connections = []Connection{
		{ID: "conn-openai", Name: "Work OpenAI", Provider: "OpenAI", BaseURL: "https://api.openai.com/v1", ModelsSet: true, Models: []ProviderModel{{ID: "gpt-4o", Enabled: true}}},
		{ID: "conn-anthropic", Name: "Anthropic", Provider: "Anthropic", BaseURL: "https://api.anthropic.com/v1", ModelsSet: true, Models: []ProviderModel{{ID: "claude-sonnet-4.5", Enabled: true}}},
	}
	if err := a.keys.Set("conn-openai", "sk-openai"); err != nil {
		t.Fatal(err)
	}
	if err := a.keys.Set("conn-anthropic", "sk-anthropic"); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Provider: "Anthropic", Model: "claude-sonnet-4.5", ConnectionID: "conn-anthropic", BaseURL: "https://api.anthropic.com/v1"}
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Model labels", CreatedAt: now, UpdatedAt: now, Loaded: true,
		Config: cfg,
		Messages: []Message{
			{ID: "u1", Role: roleUser, Content: "First question", Done: true, CreatedAt: now},
			{
				ID: "a1", Role: roleAssistant, Content: "First reply, sent as gpt-4o.", Done: true, CreatedAt: now.Add(time.Minute),
				Model: "gpt-4o", ProviderID: "conn-openai", ProviderKind: "OpenAI", ProviderName: "Work OpenAI",
			},
			{ID: "u2", Role: roleUser, Content: "Second question", Done: true, CreatedAt: now.Add(2 * time.Minute)},
			{
				ID: "a2", Role: roleAssistant, Content: "Second reply, sent as claude.", Done: true, CreatedAt: now.Add(3 * time.Minute),
				Model: "claude-sonnet-4.5", ProviderID: "conn-anthropic", ProviderKind: "Anthropic", ProviderName: "Anthropic",
			},
		},
	}}
	a.data.ActiveID = "c1"
	return a
}

func scaleRect(r ui.Rect, scale float32) ui.Rect {
	return ui.Rect{X: r.X * scale, Y: r.Y * scale, W: r.W * scale, H: r.H * scale}
}

func showReply(t *testing.T, tt *ui.Tester, content string) {
	t.Helper()
	r := mustFind(t, tt, content)
	tt.Move(r.X+8, r.Y+r.H/2)
}

func assistantRecords(t *testing.T, path string) []messageRec {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []messageRec
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		var rec messageRec
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		if rec.Role == roleAssistant {
			out = append(out, rec)
		}
	}
	return out
}
