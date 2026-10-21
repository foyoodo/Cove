package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestJSONLRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.json")
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	temp := 0.2
	chatTemp := 0.7
	data := defaultData()
	data.Defaults.Model = "kept-model"
	data.Defaults.Temperature = &temp
	data.Defaults.MaxTokens = 128
	data.Defaults.APIKey = "sk-should-not-land"
	data.SidebarWidth = 300
	data.ActiveID = "c1"
	data.Conversations = []Conversation{
		{
			ID: "c1", Title: "Alpha", CreatedAt: now, UpdatedAt: now.Add(time.Minute),
			Config: Config{
				Provider: "OpenAI", Model: "demo", BaseURL: "https://example.com/v1",
				APIKey: "sk-chat", Temperature: &chatTemp, MaxTokens: 64,
			},
			Draft: "next",
			Messages: []Message{
				{ID: "u1", Role: roleUser, Content: "Hello **there**", Done: true, CreatedAt: now},
				{
					ID: "a1", Role: roleAssistant, Content: "Hi", Reasoning: "because",
					Done: true, Tokens: 12, ElapsedMS: 340, CreatedAt: now.Add(time.Second),
					Model: "stamped-model", ProviderID: "conn-demo", ProviderKind: "OpenAI", ProviderName: "North",
				},
				{ID: "e1", Role: roleAssistant, Content: "", Error: "nope", Done: true, CreatedAt: now.Add(2 * time.Second)},
			},
		},
		{
			ID: "d1", Title: "unsent note", CreatedAt: now, UpdatedAt: now.Add(2 * time.Minute),
			Config:   DefaultConfig(),
			Draft:    "unsent note",
			Messages: []Message{},
		},
	}
	if err := Save(path, &data); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{settingsFile, indexFile, filepath.Join(chatsDir, "c1.jsonl"), filepath.Join(chatsDir, "d1.jsonl")} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal(name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, chatsDir, "blank.jsonl")); !os.IsNotExist(err) {
		t.Fatal("unexpected blank chat file")
	}
	raw, err := os.ReadFile(filepath.Join(dir, chatsDir, "c1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("lines %d\n%s", len(lines), raw)
	}
	var header map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatal(err)
	}
	if header["type"] != "chat" || header["schema"] != float64(5) || header["id"] != "c1" || header["title"] != "Alpha" {
		t.Fatalf("header %#v", header)
	}
	var msg map[string]any
	if err := json.Unmarshal([]byte(lines[2]), &msg); err != nil {
		t.Fatal(err)
	}
	if msg["type"] != "message" || msg["status"] != "complete" || msg["reasoning"] != "because" || msg["model"] != "stamped-model" {
		t.Fatalf("assistant %#v", msg)
	}
	if msg["providerId"] != "conn-demo" || msg["providerKind"] != "OpenAI" || msg["providerName"] != "North" {
		t.Fatalf("snapshot %#v", msg)
	}
	usage, _ := msg["usage"].(map[string]any)
	if usage["tokens"] != float64(12) || usage["elapsedMs"] != float64(340) {
		t.Fatalf("usage %#v", msg["usage"])
	}
	var errMsg map[string]any
	if err := json.Unmarshal([]byte(lines[3]), &errMsg); err != nil {
		t.Fatal(err)
	}
	if errMsg["status"] != "error" {
		t.Fatalf("error status %#v", errMsg)
	}
	if _, ok := errMsg["model"]; ok {
		t.Fatalf("unstamped reply gained a model: %#v", errMsg)
	}
	idxRaw, err := os.ReadFile(filepath.Join(dir, indexFile))
	if err != nil {
		t.Fatal(err)
	}
	var idx indexDoc
	if err := json.Unmarshal(idxRaw, &idx); err != nil {
		t.Fatal(err)
	}
	if idx.Schema != 5 || len(idx.Chats) != 2 {
		t.Fatalf("index %+v", idx)
	}
	byID := map[string]indexEntry{}
	for _, e := range idx.Chats {
		byID[e.ID] = e
	}
	if byID["c1"].MessageCount != 3 || byID["c1"].Model != "demo" || byID["c1"].Draft || byID["c1"].DraftText != "next" {
		t.Fatalf("c1 index %+v", byID["c1"])
	}
	if !byID["d1"].Draft || byID["d1"].MessageCount != 0 || byID["d1"].DraftText != "unsent note" {
		t.Fatalf("draft index %+v", byID["d1"])
	}
	if bytes.Contains(storeBytes(t, dir), []byte("sk-")) {
		t.Fatal("api key landed in the store")
	}

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Defaults.Model != "kept-model" || got.Defaults.MaxTokens != 128 || got.Defaults.Temperature == nil || *got.Defaults.Temperature != 0.2 {
		t.Fatalf("defaults %+v", got.Defaults)
	}
	if got.Defaults.APIKey != "" || got.ActiveID != "c1" || got.SidebarWidth != 300 || got.Version != storeVersion {
		t.Fatalf("settings %+v", got)
	}
	c1 := findConv(got.Conversations, "c1")
	if c1.Title != "Alpha" || c1.Draft != "next" || c1.Config.MaxTokens != 64 || c1.Config.Temperature == nil || *c1.Config.Temperature != 0.7 {
		t.Fatalf("chat %+v", c1)
	}
	if c1.Config.APIKey != "" || len(c1.Messages) != 3 || c1.Messages[0].Content != "Hello **there**" || c1.Messages[1].Reasoning != "because" || c1.Messages[1].Tokens != 12 || c1.Messages[2].Error != "nope" {
		t.Fatalf("messages %+v", c1.Messages)
	}
	stamped := c1.Messages[1]
	if stamped.Model != "stamped-model" || stamped.ProviderID != "conn-demo" || stamped.ProviderKind != "OpenAI" || stamped.ProviderName != "North" {
		t.Fatalf("reloaded snapshot %+v", stamped)
	}
	if c1.Messages[2].Model != "" || c1.Messages[2].ProviderName != "" || c1.Messages[2].ProviderID != "" {
		t.Fatalf("unstamped reply picked up a model: %+v", c1.Messages[2])
	}
	d1 := findConv(got.Conversations, "d1")
	if d1.Draft != "unsent note" || len(d1.Messages) != 0 {
		t.Fatalf("draft %+v", d1)
	}

	// A field this build does not know is ignored, and the chat still loads.
	extra := append(append([]byte{}, raw...), []byte(`{"type":"message","id":"future","role":"user","content":"kept","status":"complete","widget":true}`+"\n")...)
	if err := os.WriteFile(filepath.Join(dir, chatsDir, "c1.jsonl"), extra, 0o600); err != nil {
		t.Fatal(err)
	}
	again, err := readChat(filepath.Join(dir, chatsDir, "c1.jsonl"), data.Defaults)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Messages) != 4 || again.Messages[3].Content != "kept" {
		t.Fatalf("unknown field dropped the message: %+v", again.Messages)
	}
}

func TestJSONLAppendOnSendRewriteOnEdit(t *testing.T) {
	a := newTestApp(t)
	now := time.Date(2026, 10, 18, 15, 0, 0, 0, time.UTC)
	conv := Conversation{
		ID: "c1", Title: "Notes", CreatedAt: now, UpdatedAt: now,
		Config: DefaultConfig(),
		Messages: []Message{
			{ID: "u0", Role: roleUser, Content: "first", Done: true, CreatedAt: now},
			{ID: "a0", Role: roleAssistant, Content: "reply", Done: true, CreatedAt: now.Add(time.Second)},
		},
	}
	a.data.Conversations = []Conversation{conv}
	a.data.ActiveID = "c1"
	a.save()
	file := filepath.Join(filepath.Dir(a.path), chatsDir, "c1.jsonl")
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	ino := fileInode(t, file)
	if strings.Contains(string(before), "second") {
		t.Fatal("planted file already has the next send")
	}

	a.send("second question")
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if fileInode(t, file) != ino {
		t.Fatal("a normal send replaced the chat file")
	}
	if !bytes.HasPrefix(after, before) {
		t.Fatalf("send rewrote the file\nbefore %s\nafter  %s", before, after)
	}
	if !bytes.Contains(after, []byte("second question")) {
		t.Fatalf("user message was not appended\n%s", after)
	}
	loaded, err := readChat(file, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range loaded.Messages {
		if m.Role == roleAssistant && !m.Done && m.Error == "" {
			t.Fatal("an unfinished assistant line was written")
		}
	}
	assertNoTempFiles(t, filepath.Dir(a.path))

	a.editMessage("c1", "u0")
	a.edit.text = "revised first"
	a.submitEdit()
	rewritten, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(rewritten, []byte(`"id":"a0"`)) || bytes.Contains(rewritten, []byte("reply")) {
		t.Fatalf("edit-resend left the later reply\n%s", rewritten)
	}
	if !bytes.Contains(rewritten, []byte("revised first")) {
		t.Fatalf("edited text missing\n%s", rewritten)
	}
	if _, err := readChat(file, DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	assertNoTempFiles(t, filepath.Dir(a.path))
	got, err := Load(a.path)
	if err != nil {
		t.Fatal(err)
	}
	c := findConv(got.Conversations, "c1")
	if len(c.Messages) == 0 || c.Messages[0].Content != "revised first" {
		t.Fatalf("reloaded %+v", c.Messages)
	}
	for _, m := range c.Messages {
		if m.ID == "a0" {
			t.Fatal("later reply survived reload")
		}
	}
}

func TestJSONLSendFsyncsUserBeforeReply(t *testing.T) {
	a := newTestApp(t)
	now := time.Date(2026, 10, 18, 16, 0, 0, 0, time.UTC)
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Notes", CreatedAt: now, UpdatedAt: now, Config: DefaultConfig(),
		Messages: []Message{{ID: "u0", Role: roleUser, Content: "already there", Done: true, CreatedAt: now}},
	}}
	a.data.ActiveID = "c1"
	a.save()
	file := filepath.Join(filepath.Dir(a.path), chatsDir, "c1.jsonl")
	var atSend []byte
	prev := beforeReplyHook
	beforeReplyHook = func() {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		atSend = append([]byte(nil), b...)
	}
	defer func() { beforeReplyHook = prev }()
	a.send("hello from the composer")
	if !bytes.Contains(atSend, []byte("hello from the composer")) {
		t.Fatalf("user message was not on disk before the reply:\n%s", atSend)
	}
	if bytes.Contains(atSend, []byte(`"role":"assistant"`)) {
		t.Fatalf("assistant was written before the request:\n%s", atSend)
	}
	final, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(final, []byte("hello from the composer")) || !bytes.Contains(final, []byte(`"status":"error"`)) {
		t.Fatalf("finished send\n%s", final)
	}
}

func TestJSONLStopSavesIncomplete(t *testing.T) {
	a := newTestApp(t)
	now := time.Date(2026, 10, 18, 16, 30, 0, 0, time.UTC)
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Live", CreatedAt: now, UpdatedAt: now, Config: DefaultConfig(),
		Messages: []Message{
			{ID: "u", Role: roleUser, Content: "Hi", Done: true, CreatedAt: now},
			{ID: "a", Role: roleAssistant, Content: "Partial reply", Done: false, CreatedAt: now.Add(time.Second)},
		},
	}}
	a.data.ActiveID = "c1"
	a.save()
	file := filepath.Join(filepath.Dir(a.path), chatsDir, "c1.jsonl")
	mid, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(mid, []byte("Partial reply")) {
		t.Fatalf("a streaming reply was written:\n%s", mid)
	}
	_, cancel := context.WithCancel(context.Background())
	a.streams["c1"] = &convStream{gen: 1, cancel: cancel}
	a.stopChat("c1")
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(`"status":"incomplete"`)) || !bytes.Contains(body, []byte("Partial reply")) {
		t.Fatalf("stopped reply\n%s", body)
	}
	loaded, err := readChat(file, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Messages) != 2 || loaded.Messages[1].Content != "Partial reply" || !loaded.Messages[1].Incomplete || loaded.Messages[1].Error != "" || !loaded.Messages[1].Done {
		t.Fatalf("reloaded %+v", loaded.Messages)
	}
}

func TestJSONLIndexWriteIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.json")
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	data := defaultData()
	data.Conversations = []Conversation{{
		ID: "c1", Title: "Kept", CreatedAt: now, UpdatedAt: now, Config: DefaultConfig(),
		Messages: []Message{{ID: "m", Role: roleUser, Content: "hi", Done: true, CreatedAt: now}},
	}}
	if err := Save(path, &data); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(dir, indexFile)
	before, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	prev := atomicHook
	atomicHook = func(target, stage string) error {
		if stage == "rename" && target == indexPath {
			return errors.New("injected")
		}
		return nil
	}
	defer func() { atomicHook = prev }()
	if err := writeIndex(dir, []indexEntry{{ID: "nope", Title: "Nope", Schema: chatSchema}}); err == nil {
		t.Fatal("index write succeeded")
	}
	after, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("a failed index write replaced the file\n%s", after)
	}
	var doc indexDoc
	if err := json.Unmarshal(after, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Chats) != 1 || doc.Chats[0].ID != "c1" || doc.Chats[0].Title != "Kept" {
		t.Fatalf("index %+v", doc.Chats)
	}
	assertNoTempFiles(t, dir)
}

func TestJSONLTruncatedFinalLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.json")
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	data := defaultData()
	data.Conversations = []Conversation{{
		ID: "c1", Title: "Kept", CreatedAt: now, UpdatedAt: now, Config: DefaultConfig(),
		Messages: []Message{
			{ID: "u1", Role: roleUser, Content: "keep me", Done: true, CreatedAt: now},
			{ID: "a1", Role: roleAssistant, Content: "and me", Done: true, CreatedAt: now},
		},
	}}
	if err := Save(path, &data); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, chatsDir, "c1.jsonl")
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{\"type\":\"message\",\"id\":\"cut\",\"role\":\"user\",\"content\":\"nope\""); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	var logs []string
	prev := storeLog
	storeLog = func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	}
	defer func() { storeLog = prev }()
	got, err := readChat(file, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 2 || got.Messages[0].Content != "keep me" || got.Messages[1].Content != "and me" {
		t.Fatalf("chat dropped: %+v", got.Messages)
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "skipped truncated final line") {
		t.Fatalf("logs %q", logs)
	}

	// A bad line that is not last is skipped too. Lines around it stay.
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	middle := []byte(lines[0] + "\n" + lines[1] + "\n{not-json\n" + lines[2] + "\n")
	if err := os.WriteFile(file, middle, 0o600); err != nil {
		t.Fatal(err)
	}
	logs = nil
	got, err = readChat(file, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("middle corruption dropped the chat: %+v", got.Messages)
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "skipped corrupt line") {
		t.Fatalf("logs %q", logs)
	}
}

func TestJSONLRebuildIndex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.json")
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	data := defaultData()
	data.Defaults.Model = "kept-model"
	data.ActiveID = "c2"
	data.Conversations = []Conversation{
		{ID: "c1", Title: "One", CreatedAt: now, UpdatedAt: now, Config: Config{Provider: "OpenAI", Model: "m1", BaseURL: "https://api.openai.com/v1"}, Messages: []Message{{ID: "m", Role: roleUser, Content: "one", Done: true, CreatedAt: now}}},
		{ID: "c2", Title: "Two", CreatedAt: now, UpdatedAt: now.Add(time.Minute), Config: Config{Provider: "OpenAI", Model: "m2", BaseURL: "https://api.openai.com/v1"}, Draft: "draft text", Messages: []Message{}},
	}
	if err := Save(path, &data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, indexFile), []byte("{not an index"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := openApp(path, newMemKeys())
	if a.saveBlocked {
		t.Fatal(a.loadError)
	}
	if !strings.Contains(a.loadError, "Rebuilt") {
		t.Fatalf("notice %q", a.loadError)
	}
	if len(a.data.Conversations) != 2 {
		t.Fatalf("rebuilt %+v", a.data.Conversations)
	}
	for _, c := range a.data.Conversations {
		if c.Messages != nil || c.Loaded {
			t.Fatalf("rebuild loaded messages for %s", c.ID)
		}
	}
	one := a.conversation("c1")
	if one == nil || one.Title != "One" || one.MessageCount != 1 || one.Config.Model != "m1" {
		t.Fatalf("c1 %+v", one)
	}
	two := a.conversation("c2")
	if two == nil || two.Title != "draft text" || two.Draft != "draft text" || !two.DraftOnly {
		t.Fatalf("c2 %+v", two)
	}
	if a.data.Defaults.Model != "kept-model" || a.data.ActiveID != "c2" {
		t.Fatalf("settings model %q active %q", a.data.Defaults.Model, a.data.ActiveID)
	}
	a.ensureChat("c1")
	if a.conversation("c1").Messages[0].Content != "one" {
		t.Fatalf("lazy message %+v", a.conversation("c1").Messages)
	}
	if _, err := os.Stat(filepath.Join(dir, indexFile)); err != nil {
		t.Fatal(err)
	}
}

func TestJSONLSaveKeepsUnloadedChats(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.json")
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	data := defaultData()
	data.Conversations = []Conversation{
		{ID: "c1", Title: "One", CreatedAt: now, UpdatedAt: now, Config: DefaultConfig(), Messages: []Message{{ID: "m", Role: roleUser, Content: "keep one", Done: true, CreatedAt: now}}},
		{ID: "c2", Title: "Two", CreatedAt: now, UpdatedAt: now.Add(time.Second), Config: DefaultConfig(), Draft: "note", Messages: []Message{{ID: "n", Role: roleUser, Content: "keep two", Done: true, CreatedAt: now}}},
	}
	if err := Save(path, &data); err != nil {
		t.Fatal(err)
	}
	a := openApp(path, newMemKeys())
	if a.conversation("c1").Messages != nil || a.conversation("c2").Messages != nil {
		t.Fatal("open loaded messages")
	}
	a.data.SidebarWidth = 222
	a.save()
	for _, id := range []string{"c1", "c2"} {
		got, err := readChat(filepath.Join(dir, chatsDir, id+".jsonl"), DefaultConfig())
		if err != nil {
			t.Fatal(id, err)
		}
		if len(got.Messages) != 1 || !strings.HasPrefix(got.Messages[0].Content, "keep") {
			t.Fatalf("%s rewritten or dropped: %+v", id, got.Messages)
		}
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SidebarWidth != 222 || len(loaded.Conversations) != 2 {
		t.Fatalf("reload %+v width %v", loaded.Conversations, loaded.SidebarWidth)
	}
	if findConv(loaded.Conversations, "c2").Draft != "note" {
		t.Fatalf("draft dropped: %+v", findConv(loaded.Conversations, "c2"))
	}
}

func TestJSONLLazyLoadSkipsChatFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.json")
	now := time.Now().UTC().Truncate(time.Second)
	data := defaultData()
	data.Conversations = []Conversation{
		{ID: "c1", Title: "One", CreatedAt: now, UpdatedAt: now, Config: DefaultConfig(), Messages: []Message{{ID: "m", Role: roleUser, Content: "secret body", Done: true, CreatedAt: now}}},
		{ID: "c2", Title: "Two", CreatedAt: now, UpdatedAt: now.Add(time.Second), Config: DefaultConfig(), Messages: []Message{{ID: "n", Role: roleUser, Content: "other", Done: true, CreatedAt: now}}},
	}
	data.ActiveID = "c2"
	if err := Save(path, &data); err != nil {
		t.Fatal(err)
	}
	var reads []string
	prev := chatReadHook
	chatReadHook = func(p string) { reads = append(reads, p) }
	defer func() { chatReadHook = prev }()

	a := openApp(path, newMemKeys())
	if len(reads) != 0 {
		t.Fatalf("startup read chat files: %v", reads)
	}
	if a.conversation("c1") == nil || a.conversation("c1").Messages != nil || a.conversation("c1").Title != "One" {
		t.Fatalf("sidebar row %+v", a.conversation("c1"))
	}
	if a.conversation("c2").Config.Model == "" {
		t.Fatal("index row lost the model")
	}
	a.ensureChat("c1")
	if len(reads) != 1 || !strings.HasSuffix(reads[0], "c1.jsonl") {
		t.Fatalf("reads %v", reads)
	}
	if a.conversation("c1").Messages[0].Content != "secret body" {
		t.Fatalf("loaded %+v", a.conversation("c1").Messages)
	}
	if a.conversation("c2").Messages != nil {
		t.Fatal("opening one chat read the other")
	}
}

func TestJSONLMigrateSchema2(t *testing.T) {
	assertSchemaMigration(t, schemaMigrateCase{
		name: "schema2",
		raw: `{
			"version": 2,
			"defaults": {"provider":"OpenAI","model":"kept-model","baseUrl":"https://api.openai.com/v1","systemPrompt":"stay","temperature":0.2,"maxTokens":128},
			"sidebarWidth": 280,
			"activeId": "chat",
			"conversations": [
				{"id":"chat","title":"From v0.6","updatedAt":"2026-01-02T00:00:00Z","config":{"provider":"DeepSeek","model":"deepseek-flash","baseUrl":"https://api.deepseek.com/v1","temperature":0.55,"maxTokens":64},"messages":[{"id":"m","role":"user","content":"hello from v0.6","done":true}],"draft":"add a closer"},
				{"id":"note","title":"New Chat","updatedAt":"2026-01-03T00:00:00Z","config":{"provider":"OpenAI","model":"gpt-4o-mini","baseUrl":"https://api.openai.com/v1"},"messages":[],"draft":"unsent note"},
				{"id":"blank","title":"New Chat","updatedAt":"2026-01-01T00:00:00Z","messages":[],"draft":"   "}
			]
		}`,
		model: "kept-model", temp: 0.2, max: 128,
		appearance: appearanceSystem, theme: "warm",
		show: true, restore: true, keep: true,
		active: "chat", width: 280,
		chats: []wantChat{
			{id: "chat", title: "From v0.6", draft: "add a closer", content: "hello from v0.6", model: "deepseek-flash", temp: 0.55, max: 64},
			{id: "note", title: "unsent note", draft: "unsent note"},
		},
	})
}

func TestJSONLMigrateSchema3(t *testing.T) {
	assertSchemaMigration(t, schemaMigrateCase{
		name: "schema3",
		raw: `{
			"version": 3,
			"appearance": "dark",
			"defaults": {"provider":"OpenAI","model":"kept-model","baseUrl":"https://api.openai.com/v1","systemPrompt":"stay","temperature":0.3,"maxTokens":256},
			"sidebarWidth": 240,
			"activeId": "draft",
			"conversations": [
				{"id":"chat","title":"From v0.7.0","updatedAt":"2026-02-02T00:00:00Z","config":{"provider":"OpenAI","model":"gpt-4o-mini","baseUrl":"https://api.openai.com/v1","temperature":0.1,"maxTokens":32},"messages":[{"id":"m","role":"assistant","content":"kept reply","done":true}]},
				{"id":"draft","title":"Old title","updatedAt":"2026-02-03T00:00:00Z","messages":[],"draft":"finish the outline"},
				{"id":"blank","title":"New Chat","updatedAt":"2026-02-01T00:00:00Z","messages":[],"draft":""}
			]
		}`,
		model: "kept-model", temp: 0.3, max: 256,
		appearance: appearanceDark, theme: "warm",
		show: true, restore: true, keep: true,
		active: "draft", width: 240,
		chats: []wantChat{
			{id: "chat", title: "From v0.7.0", content: "kept reply", model: "gpt-4o-mini", temp: 0.1, max: 32},
			{id: "draft", title: "finish the outline", draft: "finish the outline"},
		},
	})
}

func TestJSONLMigrateSchema4(t *testing.T) {
	assertSchemaMigration(t, schemaMigrateCase{
		name: "schema4",
		raw: `{
			"version": 4,
			"appearance": "light",
			"theme": "sage",
			"showReasoning": false,
			"restoreLast": false,
			"keepLastRequest": true,
			"defaults": {"provider":"Anthropic","model":"claude-sonnet-4.5","baseUrl":"https://api.anthropic.com/v1","systemPrompt":"stay","temperature":0.4,"maxTokens":512},
			"sidebarWidth": 220,
			"activeId": "chat",
			"conversations": [
				{"id":"chat","title":"Sage chat","updatedAt":"2026-03-02T00:00:00Z","config":{"provider":"Anthropic","model":"claude-sonnet-4.5","baseUrl":"https://api.anthropic.com/v1","temperature":0.25,"maxTokens":100},"messages":[{"id":"m","role":"user","content":"keep the sage chat","done":true}],"draft":"still drafting"},
				{"id":"blank","title":"New Chat","updatedAt":"2026-03-01T00:00:00Z","messages":[],"draft":""}
			]
		}`,
		model: "claude-sonnet-4.5", temp: 0.4, max: 512,
		appearance: appearanceLight, theme: "sage",
		show: false, restore: false, keep: true,
		active: "chat", width: 220,
		chats: []wantChat{
			{id: "chat", title: "Sage chat", draft: "still drafting", content: "keep the sage chat", model: "claude-sonnet-4.5", temp: 0.25, max: 100},
		},
	})
}

type wantChat struct {
	id, title, draft, content, model string
	temp                             float64
	max                              int
}

type schemaMigrateCase struct {
	name, raw, model, appearance, theme, active string
	temp                                        float64
	max                                         int
	show, restore, keep                         bool
	width                                       float32
	chats                                       []wantChat
}

func assertSchemaMigration(t *testing.T, tc schemaMigrateCase) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.json")
	raw := []byte(tc.raw)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	a := openApp(path, newMemKeys())
	if a.saveBlocked {
		t.Fatal(a.loadError)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("legacy file still live: %v", err)
	}
	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bak, raw) {
		t.Fatal("bak is not the original file")
	}
	assertMigrated(t, a.data, tc)
	for _, id := range []string{"blank"} {
		if a.conversation(id) != nil {
			t.Fatalf("empty draft %s was written", id)
		}
		if _, err := os.Stat(filepath.Join(dir, chatsDir, id+".jsonl")); !os.IsNotExist(err) {
			t.Fatalf("empty draft file %s: %v", id, err)
		}
	}
	chatBytes := map[string][]byte{}
	for _, c := range tc.chats {
		b, err := os.ReadFile(filepath.Join(dir, chatsDir, c.id+".jsonl"))
		if err != nil {
			t.Fatal(c.id, err)
		}
		chatBytes[c.id] = b
		if !bytes.Contains(b, []byte(`"schema":5`)) {
			t.Fatalf("%s header %s", c.id, b)
		}
	}

	again := openApp(path, newMemKeys())
	if again.saveBlocked {
		t.Fatal(again.loadError)
	}
	bak2, err := os.ReadFile(path + ".bak")
	if err != nil || !bytes.Equal(bak2, raw) {
		t.Fatal("second launch rewrote the backup")
	}
	for id, b := range chatBytes {
		got, err := os.ReadFile(filepath.Join(dir, chatsDir, id+".jsonl"))
		if err != nil || !bytes.Equal(got, b) {
			t.Fatalf("second launch rewrote %s", id)
		}
	}
	for _, c := range again.data.Conversations {
		if c.Messages != nil || c.Loaded {
			t.Fatalf("second launch read %s", c.ID)
		}
	}
	again.ensureChat(tc.chats[0].id)
	if again.data.Defaults.Model != tc.model || again.data.Defaults.MaxTokens != tc.max || again.data.Defaults.Temperature == nil || *again.data.Defaults.Temperature != tc.temp {
		t.Fatalf("second launch defaults %+v", again.data.Defaults)
	}
	opened := again.conversation(tc.chats[0].id)
	if opened.Messages[0].Content != tc.chats[0].content {
		t.Fatalf("second launch message %+v", opened.Messages)
	}
	if opened.Config.Temperature == nil || *opened.Config.Temperature != tc.chats[0].temp || opened.Config.MaxTokens != tc.chats[0].max {
		t.Fatalf("second launch chat config %+v", opened.Config)
	}
}

func assertMigrated(t *testing.T, data StoreData, tc schemaMigrateCase) {
	t.Helper()
	if data.Defaults.Model != tc.model || data.Defaults.MaxTokens != tc.max || data.Defaults.Temperature == nil || *data.Defaults.Temperature != tc.temp {
		t.Fatalf("defaults model %q temp %v max %d", data.Defaults.Model, data.Defaults.Temperature, data.Defaults.MaxTokens)
	}
	if data.Appearance != tc.appearance || data.Theme != tc.theme {
		t.Fatalf("appearance %q theme %q", data.Appearance, data.Theme)
	}
	if data.ShowReasoning != tc.show || data.RestoreLast != tc.restore || data.KeepLastRequest != tc.keep {
		t.Fatalf("prefs show %v restore %v keep %v", data.ShowReasoning, data.RestoreLast, data.KeepLastRequest)
	}
	if data.ActiveID != tc.active || data.SidebarWidth != tc.width {
		t.Fatalf("active %q width %v", data.ActiveID, data.SidebarWidth)
	}
	if len(data.Conversations) != len(tc.chats) {
		t.Fatalf("chats %+v", data.Conversations)
	}
	for _, want := range tc.chats {
		got := findConv(data.Conversations, want.id)
		if got.ID == "" {
			t.Fatalf("missing %s in %+v", want.id, data.Conversations)
		}
		if got.Title != want.title || got.Draft != want.draft {
			t.Fatalf("%s title %q draft %q", want.id, got.Title, got.Draft)
		}
		if want.content == "" {
			if len(got.Messages) != 0 {
				t.Fatalf("%s messages %+v", want.id, got.Messages)
			}
			continue
		}
		if len(got.Messages) != 1 || got.Messages[0].Content != want.content {
			t.Fatalf("%s messages %+v", want.id, got.Messages)
		}
		if want.temp != 0 && (got.Config.Temperature == nil || *got.Config.Temperature != want.temp) {
			t.Fatalf("%s temperature %+v", want.id, got.Config.Temperature)
		}
		if got.Config.MaxTokens != want.max || (want.model != "" && got.Config.Model != want.model) {
			t.Fatalf("%s config %+v", want.id, got.Config)
		}
	}
}

func TestJSONLMigrationLeavesExistingChats(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.json")
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	data := defaultData()
	data.Defaults.Model = "kept-model"
	data.Defaults.MaxTokens = 77
	temp := 0.2
	data.Defaults.Temperature = &temp
	data.Conversations = []Conversation{{
		ID: "c1", Title: "On disk", CreatedAt: now, UpdatedAt: now, Config: DefaultConfig(),
		Messages: []Message{{ID: "m", Role: roleUser, Content: "from jsonl", Done: true, CreatedAt: now}},
	}}
	if err := Save(path, &data); err != nil {
		t.Fatal(err)
	}
	chatBefore, err := os.ReadFile(filepath.Join(dir, chatsDir, "c1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"version":4,"defaults":{"provider":"OpenAI","model":"other","baseUrl":"https://api.openai.com/v1","maxTokens":1},"conversations":[{"id":"other","title":"Legacy","messages":[{"id":"x","role":"user","content":"nope","done":true}]}]}`)
	if err := os.Remove(filepath.Join(dir, indexFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	a := openApp(path, newMemKeys())
	if a.conversation("other") != nil || a.conversation("c1") == nil || a.conversation("c1").Title != "On disk" {
		t.Fatalf("rebuild used the legacy file: %+v", a.data.Conversations)
	}
	if a.data.Defaults.Model != "kept-model" || a.data.Defaults.MaxTokens != 77 {
		t.Fatalf("defaults replaced %+v", a.data.Defaults)
	}
	got, err := os.ReadFile(filepath.Join(dir, chatsDir, "c1.jsonl"))
	if err != nil || !bytes.Equal(got, chatBefore) {
		t.Fatal("rebuild rewrote the chat file")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("legacy file was not moved aside")
	}
}

func TestJSONLConcurrentAppendRetitle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.json")
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	data := defaultData()
	data.Conversations = []Conversation{{
		ID: "c1", Title: "Start", CreatedAt: now, UpdatedAt: now, Config: DefaultConfig(),
		Messages: []Message{{ID: "m0", Role: roleUser, Content: "origin", Done: true, CreatedAt: now}},
	}}
	if err := Save(path, &data); err != nil {
		t.Fatal(err)
	}
	const n = 24
	var wg sync.WaitGroup
	errCh := make(chan error, n*2)
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			msg := Message{ID: fmt.Sprintf("m%d", i+1), Role: roleUser, Content: fmt.Sprintf("body %d", i+1), CreatedAt: now}
			if err := AppendFinal(dir, "c1", msg); err != nil {
				errCh <- err
			}
		}(i)
		go func(i int) {
			defer wg.Done()
			if err := Retitle(dir, "c1", fmt.Sprintf("title-%d", i)); err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
	file := filepath.Join(dir, chatsDir, "c1.jsonl")
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	if len(lines) != n+2 {
		t.Fatalf("lines %d\n%s", len(lines), body)
	}
	seen := map[string]int{}
	var header chatHeader
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatal(err)
	}
	if header.Type != "chat" || header.Schema != 5 || header.ID != "c1" {
		t.Fatalf("header %+v", header)
	}
	titleOK := header.Title == "Start"
	for i := 0; i < n; i++ {
		if header.Title == fmt.Sprintf("title-%d", i) {
			titleOK = true
		}
	}
	if !titleOK {
		t.Fatalf("title %q", header.Title)
	}
	for _, line := range lines[1:] {
		var rec messageRec
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line %s: %v", line, err)
		}
		if rec.Type != "message" || rec.Status != "complete" {
			t.Fatalf("record %+v", rec)
		}
		seen[rec.ID]++
	}
	if seen["m0"] != 1 {
		t.Fatalf("origin count %d", seen["m0"])
	}
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("m%d", i)
		if seen[id] != 1 {
			t.Fatalf("%s count %d", id, seen[id])
		}
	}
	idx, err := readIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Chats) != 1 || idx.Chats[0].Title != header.Title || idx.Chats[0].MessageCount != n+1 {
		t.Fatalf("index %+v header title %q", idx.Chats, header.Title)
	}
	assertNoTempFiles(t, dir)
}

func TestJSONLStartup500(t *testing.T) {
	const n = 500
	const per = 20
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	body := strings.Repeat("word ", 80) // 400 bytes
	mono := defaultData()
	mono.Defaults.Model = "kept-model"
	temp := 0.2
	mono.Defaults.Temperature = &temp
	mono.Defaults.MaxTokens = 128
	mono.Conversations = make([]Conversation, n)
	for i := 0; i < n; i++ {
		msgs := make([]Message, per)
		for j := 0; j < per; j++ {
			role := roleUser
			if j%2 == 1 {
				role = roleAssistant
			}
			msgs[j] = Message{ID: fmt.Sprintf("m-%d-%d", i, j), Role: role, Content: body, Done: true, CreatedAt: now}
		}
		mono.Conversations[i] = Conversation{
			ID: fmt.Sprintf("c%03d", i), Title: fmt.Sprintf("Chat %d", i),
			CreatedAt: now, UpdatedAt: now.Add(time.Duration(i) * time.Second),
			Config: DefaultConfig(), Messages: msgs,
		}
	}
	mono.ActiveID = "c000"
	blob, err := json.Marshal(mono)
	if err != nil {
		t.Fatal(err)
	}
	monoPath := filepath.Join(t.TempDir(), "conversations.json")
	if err := os.WriteFile(monoPath, blob, 0o600); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.json")
	if err := Save(path, &mono); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	raw, err := os.ReadFile(monoPath)
	if err != nil {
		t.Fatal(err)
	}
	var decoded StoreData
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	before := time.Since(start)
	if len(decoded.Conversations) != n || len(decoded.Conversations[0].Messages) != per {
		t.Fatalf("monolith %+v", len(decoded.Conversations))
	}

	var reads []string
	prev := chatReadHook
	chatReadHook = func(p string) { reads = append(reads, p) }
	defer func() { chatReadHook = prev }()
	start = time.Now()
	lazy, err := loadLazy(dir)
	if err != nil {
		t.Fatal(err)
	}
	after := time.Since(start)
	if len(reads) != 0 {
		t.Fatalf("index load read %d chat files", len(reads))
	}
	if len(lazy.Conversations) != n || lazy.Conversations[0].Messages != nil {
		t.Fatalf("lazy chats %d loaded %v", len(lazy.Conversations), lazy.Conversations[0].Messages != nil)
	}
	if lazy.Defaults.Model != "kept-model" || lazy.Defaults.MaxTokens != 128 || lazy.Defaults.Temperature == nil || *lazy.Defaults.Temperature != 0.2 {
		t.Fatalf("lazy defaults %+v", lazy.Defaults)
	}
	t.Logf("startup 500 chats x %d messages: monolith read+unmarshal %s (%d bytes), index-only %s, chat files read %d",
		per, before, len(blob), after, len(reads))
}

func TestJSONLMigrationFailureRetries(t *testing.T) {
	raw := []byte(`{"version":4,"defaults":{"provider":"OpenAI","model":"kept-model","baseUrl":"https://api.openai.com/v1","temperature":0.2,"maxTokens":128},"conversations":[{"id":"c1","title":"Kept","messages":[{"id":"m","role":"user","content":"hello","done":true}],"updatedAt":"2026-04-01T00:00:00Z"},{"id":"d1","title":"New Chat","messages":[],"draft":"unsent note","updatedAt":"2026-04-02T00:00:00Z"}]}`)
	t.Run("injected", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "conversations.json")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		var logs []string
		prevLog := storeLog
		storeLog = func(format string, args ...any) {
			logs = append(logs, fmt.Sprintf(format, args...))
		}
		defer func() { storeLog = prevLog }()
		prevHook := migrateHook
		migrateHook = func(step string) error {
			if step == "rename-index" {
				return errors.New("injected")
			}
			return nil
		}
		defer func() { migrateHook = prevHook }()

		a := openApp(path, newMemKeys())
		if a.saveBlocked {
			t.Fatal("a failed migration paused saves")
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, raw) {
			t.Fatalf("legacy file changed:\n%s", got)
		}
		if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
			t.Fatal("backup was written")
		}
		for _, name := range []string{settingsFile, indexFile, chatsDir} {
			if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
				t.Fatalf("%s left behind after a failed migration: %v", name, err)
			}
		}
		if a.conversation("c1") == nil || len(a.conversation("c1").Messages) != 1 || a.conversation("c1").Messages[0].Content != "hello" {
			t.Fatalf("chat lost: %+v", a.data.Conversations)
		}
		if a.conversation("d1") == nil || a.conversation("d1").Draft != "unsent note" {
			t.Fatalf("draft lost: %+v", a.conversation("d1"))
		}
		joined := strings.Join(logs, "\n")
		if !strings.Contains(joined, "migration failed") || !strings.Contains(joined, "injected") || !strings.Contains(joined, "keeping") {
			t.Fatalf("log %q", joined)
		}

		a.conversation("c1").Title = "Renamed"
		a.save()
		if _, err := os.Stat(filepath.Join(dir, indexFile)); !os.IsNotExist(err) {
			t.Fatal("saving after a failed migration switched to the new layout")
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(body, []byte("Renamed")) || !bytes.Contains(body, []byte("hello")) || !bytes.Contains(body, []byte("unsent note")) {
			t.Fatalf("legacy save lost a chat:\n%s", body)
		}

		migrateHook = nil
		b := openApp(path, newMemKeys())
		if b.saveBlocked {
			t.Fatal(b.loadError)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("retry left the old file in place")
		}
		bak, err := os.ReadFile(path + ".bak")
		if err != nil || !bytes.Contains(bak, []byte("Renamed")) {
			t.Fatalf("backup %s %v", bak, err)
		}
		b.ensureChat("c1")
		b.ensureChat("d1")
		if b.conversation("c1").Title != "Renamed" || b.conversation("c1").Messages[0].Content != "hello" {
			t.Fatalf("retry c1 %+v", b.conversation("c1"))
		}
		if b.conversation("d1").Draft != "unsent note" {
			t.Fatalf("retry draft %+v", b.conversation("d1"))
		}
		if b.data.Defaults.Model != "kept-model" || b.data.Defaults.MaxTokens != 128 || b.data.Defaults.Temperature == nil || *b.data.Defaults.Temperature != 0.2 {
			t.Fatalf("defaults %+v", b.data.Defaults)
		}
	})

	t.Run("leftover", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "conversations.json")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		// A crash after settings.json was renamed, and before the chat files.
		if err := os.WriteFile(filepath.Join(dir, settingsFile), []byte(`{"version":5,"defaults":{"provider":"OpenAI","model":"wrong","baseUrl":"https://api.openai.com/v1"}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, indexFile), []byte(`{"schema":5,"chats":[{"id":"intruder","title":"Intruder","schema":5}]}`), 0o600); err != nil {
			t.Fatal(err)
		}
		a := openApp(path, newMemKeys())
		if a.saveBlocked {
			t.Fatal(a.loadError)
		}
		if a.conversation("intruder") != nil {
			t.Fatal("half-written index was kept")
		}
		a.ensureChat("c1")
		a.ensureChat("d1")
		if a.conversation("c1") == nil || a.conversation("c1").Messages[0].Content != "hello" || a.conversation("d1") == nil || a.conversation("d1").Draft != "unsent note" {
			t.Fatalf("legacy chats %+v", a.data.Conversations)
		}
		if a.data.Defaults.Model != "kept-model" {
			t.Fatalf("stray settings won: %+v", a.data.Defaults)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("retry left the old file in place")
		}
	})
}

func TestJSONLFailedMigrationNoDuplicates(t *testing.T) {
	raw := []byte(`{"version":4,"defaults":{"provider":"OpenAI","model":"kept-model","baseUrl":"https://api.openai.com/v1"},"conversations":[{"id":"c1","title":"One","updatedAt":"2026-04-02T00:00:00Z","messages":[{"id":"m1","role":"user","content":"one","done":true}]},{"id":"c2","title":"Two","updatedAt":"2026-04-01T00:00:00Z","messages":[{"id":"m2","role":"user","content":"two","done":true}]}]}`)
	t.Run("retry", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "conversations.json")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		prev := migrateHook
		migrateHook = func(step string) error {
			if step == "commit" {
				return errors.New("injected")
			}
			return nil
		}
		defer func() { migrateHook = prev }()
		a := openApp(path, newMemKeys())
		assertChatIDs(t, a.data.Conversations, "c1", "c2")
		if _, err := os.Stat(filepath.Join(dir, indexFile)); !os.IsNotExist(err) {
			t.Fatal("failed migration left an index beside the old file")
		}
		if _, err := os.Stat(filepath.Join(dir, layoutFile)); !os.IsNotExist(err) {
			t.Fatal("failed migration committed the new tree")
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, raw) {
			t.Fatal("old file changed")
		}
		migrateHook = nil
		b := openApp(path, newMemKeys())
		if b.saveBlocked {
			t.Fatal(b.loadError)
		}
		b.ensureChat("c1")
		b.ensureChat("c2")
		assertChatIDs(t, b.data.Conversations, "c1", "c2")
		if b.conversation("c1").Messages[0].Content != "one" || b.conversation("c2").Messages[0].Content != "two" {
			t.Fatalf("retry lost a chat %+v", b.data.Conversations)
		}
		idx, err := readIndex(dir)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]int{}
		for _, e := range idx.Chats {
			seen[e.ID]++
		}
		if len(seen) != 2 || seen["c1"] != 1 || seen["c2"] != 1 {
			t.Fatalf("index ids %+v", seen)
		}
	})
	t.Run("uncommitted", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "conversations.json")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(dir, chatsDir), 0o700); err != nil {
			t.Fatal(err)
		}
		extra := "{\"type\":\"chat\",\"schema\":5,\"id\":\"extra\",\"title\":\"Extra\",\"settings\":{}}\n{\"type\":\"message\",\"id\":\"x\",\"role\":\"user\",\"content\":\"extra\",\"status\":\"complete\"}\n"
		if err := os.WriteFile(filepath.Join(dir, chatsDir, "extra.jsonl"), []byte(extra), 0o600); err != nil {
			t.Fatal(err)
		}
		idx := []byte(`{"schema":5,"chats":[{"id":"c1","title":"One","schema":5},{"id":"extra","title":"Extra","schema":5}]}`)
		if err := os.WriteFile(filepath.Join(dir, indexFile), idx, 0o600); err != nil {
			t.Fatal(err)
		}
		a := openApp(path, newMemKeys())
		if a.saveBlocked {
			t.Fatal(a.loadError)
		}
		assertChatIDs(t, a.data.Conversations, "c1", "c2")
		if a.conversation("extra") != nil {
			t.Fatal("uncommitted chat was shown beside the old file")
		}
		a.ensureChat("c1")
		a.ensureChat("c2")
		if a.conversation("c1").Messages[0].Content != "one" || a.conversation("c2").Messages[0].Content != "two" {
			t.Fatalf("legacy chats %+v", a.data.Conversations)
		}
		if _, err := os.Stat(filepath.Join(dir, chatsDir, "extra.jsonl")); !os.IsNotExist(err) {
			t.Fatal("extra chat file survived the retry")
		}
	})
}

func assertChatIDs(t *testing.T, convs []Conversation, want ...string) {
	t.Helper()
	got := map[string]int{}
	for _, c := range convs {
		got[c.ID]++
	}
	if len(got) != len(want) {
		t.Fatalf("sidebar ids %+v, want %v", got, want)
	}
	for _, id := range want {
		if got[id] != 1 {
			t.Fatalf("sidebar id %s appears %d times in %+v", id, got[id], got)
		}
	}
}

func TestJSONLHeaderStoresOverridesOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.json")
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	temp := 0.2
	otherTemp := 0.9
	data := defaultData()
	data.Appearance = appearanceDark
	data.Theme = "sage"
	data.Defaults.Model = "kept-model"
	data.Defaults.SystemPrompt = "global prompt"
	data.Defaults.Temperature = &temp
	data.Defaults.MaxTokens = 128
	data.Defaults.APIKey = "sk-global"
	data.Defaults.ConnectionID = "conn-global"
	data.Connections = []Connection{{
		ID: "conn-global", Name: "OpenAI", Provider: "OpenAI", BaseURL: data.Defaults.BaseURL,
	}}
	same := data.Defaults
	same.MaxTokens = 64
	same.APIKey = "sk-chat"
	other := data.Defaults
	other.Model = "other-model"
	other.Temperature = &otherTemp
	other.ConnectionID = "conn-other"
	other.APIKey = "sk-other"
	data.ActiveID = "same"
	data.Conversations = []Conversation{
		{ID: "same", Title: "Same", CreatedAt: now, UpdatedAt: now, Config: same, Messages: []Message{{ID: "m", Role: roleUser, Content: "hi", Done: true, CreatedAt: now}}},
		{ID: "other", Title: "Other", CreatedAt: now, UpdatedAt: now.Add(time.Second), Config: other, Messages: []Message{{ID: "n", Role: roleUser, Content: "yo", Done: true, CreatedAt: now}}},
	}
	if err := Save(path, &data); err != nil {
		t.Fatal(err)
	}
	sameRaw, err := os.ReadFile(filepath.Join(dir, chatsDir, "same.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"sk-", "conn-global", "kept-model", "global prompt", `"appearance"`, `"connections"`, `"sage"`, `"apiKey"`, `"theme"`} {
		if bytes.Contains(sameRaw, []byte(banned)) {
			t.Fatalf("same.jsonl contains %q:\n%s", banned, sameRaw)
		}
	}
	sameHeader := headerSettings(t, sameRaw)
	if string(sameHeader) != `{"maxTokens":64}` {
		t.Fatalf("same settings %s", sameHeader)
	}
	otherRaw, err := os.ReadFile(filepath.Join(dir, chatsDir, "other.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(otherRaw, []byte("sk-")) || bytes.Contains(otherRaw, []byte("connections")) || bytes.Contains(otherRaw, []byte("global prompt")) {
		t.Fatalf("other.jsonl leaked globals:\n%s", otherRaw)
	}
	otherHeader := headerSettings(t, otherRaw)
	if string(otherHeader) != `{"model":"other-model","connectionId":"conn-other","temperature":0.9}` {
		t.Fatalf("other settings %s", otherHeader)
	}
	idx, err := os.ReadFile(filepath.Join(dir, indexFile))
	if err != nil || !bytes.Contains(idx, []byte("kept-model")) || !bytes.Contains(idx, []byte("other-model")) {
		t.Fatalf("index lost the sidebar model: %s", idx)
	}
	settings, err := os.ReadFile(filepath.Join(dir, settingsFile))
	if err != nil || !bytes.Contains(settings, []byte("conn-global")) || !bytes.Contains(settings, []byte("global prompt")) || bytes.Contains(settings, []byte("sk-")) {
		t.Fatalf("settings.json %s", settings)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	s := findConv(got.Conversations, "same")
	if s.Config.Model != "kept-model" || s.Config.MaxTokens != 64 || s.Config.SystemPrompt != "global prompt" || s.Config.ConnectionID != "conn-global" || s.Config.APIKey != "" || s.Config.Temperature == nil || *s.Config.Temperature != 0.2 {
		t.Fatalf("same reloaded %+v", s.Config)
	}
	o := findConv(got.Conversations, "other")
	if o.Config.Model != "other-model" || o.Config.MaxTokens != 128 || o.Config.ConnectionID != "conn-other" || o.Config.APIKey != "" || o.Config.Temperature == nil || *o.Config.Temperature != 0.9 {
		t.Fatalf("other reloaded %+v", o.Config)
	}
}

func TestJSONLRestartMatchesEditResend(t *testing.T) {
	a := newTestApp(t)
	now := time.Date(2026, 10, 18, 15, 4, 0, 0, time.UTC)
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Notes", Draft: "half written", Config: DefaultConfig(),
		CreatedAt: now, UpdatedAt: now,
		Messages: []Message{
			{ID: "u", Role: roleUser, Content: "Please revise this", Done: true, CreatedAt: now},
			{ID: "a", Role: roleAssistant, Content: "A reply", Reasoning: "thought", Done: true, CreatedAt: now.Add(time.Second)},
		},
	}}
	a.data.ActiveID = "c1"
	a.save()

	b := openApp(a.path, newMemKeys())
	b.instant = true
	openTester(b).Frame()
	b.ensureChat("c1")
	if got, want := visibleChat(b.conversation("c1")), visibleChat(a.conversation("c1")); got != want {
		t.Fatalf("restart changed the thread\nbefore %s\nafter  %s", want, got)
	}

	b.editMessage("c1", "u")
	b.edit.text = "Revised question"
	b.submitEdit()
	if b.conversation("c1").Draft != "half written" {
		t.Fatalf("draft %q", b.conversation("c1").Draft)
	}
	shown := visibleChat(b.conversation("c1"))
	if strings.Contains(shown, "A reply") || strings.Contains(shown, "thought") || !strings.Contains(shown, "Revised question") {
		t.Fatalf("edit did not replace the later reply: %s", shown)
	}

	c := openApp(b.path, newMemKeys())
	c.instant = true
	openTester(c).Frame()
	c.ensureChat("c1")
	if got := visibleChat(c.conversation("c1")); got != shown {
		t.Fatalf("restart after edit-resend\nbefore %s\nafter  %s", shown, got)
	}
}

type visibleMsg struct {
	Role, Content, Reasoning, Error string
}

func visibleChat(c *Conversation) string {
	if c == nil {
		return "<nil>"
	}
	msgs := make([]visibleMsg, len(c.Messages))
	for i, m := range c.Messages {
		msgs[i] = visibleMsg{Role: m.Role, Content: m.Content, Reasoning: m.Reasoning, Error: m.Error}
	}
	buf, _ := json.Marshal(struct {
		Title, Draft, Model string
		Messages            []visibleMsg
	}{c.Title, c.Draft, c.Config.Model, msgs})
	return string(buf)
}

func headerSettings(t *testing.T, raw []byte) json.RawMessage {
	t.Helper()
	line := raw
	if i := bytes.IndexByte(raw, '\n'); i >= 0 {
		line = raw[:i]
	}
	var header chatHeader
	if err := json.Unmarshal(line, &header); err != nil {
		t.Fatal(err)
	}
	return header.Settings
}

func findConv(convs []Conversation, id string) Conversation {
	for _, c := range convs {
		if c.ID == id {
			return c
		}
	}
	return Conversation{}
}

func fileInode(t *testing.T, path string) uint64 {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("stat has no inode")
	}
	return sys.Ino
}

func assertNoTempFiles(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".tmp-") || strings.HasPrefix(d.Name(), ".cove-migrate-") {
			t.Fatalf("leftover %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
