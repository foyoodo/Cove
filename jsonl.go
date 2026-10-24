package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	settingsFile = "settings.json"
	indexFile    = "index.json"
	chatsDir     = "chats"
	layoutFile   = ".cove-layout"
	// chatSchema stays 5. A session break is another JSONL record (type
	// "session") and maxRounds is an optional header field. This
	// reader already skips unknown record types, and encoding/json ignores
	// unknown fields, so a build from before this feature still opens the
	// file. An older chat has neither, which is unlimited context and no
	// break: the same requests as before. Bumping to 6 would rewrite every
	// chat and leave a .bak for a change those files do not need.
	chatSchema = 5

	sessionManual = "manual"
	sessionAuto   = "auto"
)

// atomicHook, when set, runs before an atomic rename. Tests use it to
// fail a write and prove the previous file stays in place.
var atomicHook func(path, stage string) error

// beforeReplyHook runs after the user message is fsynced and before the
// assistant placeholder exists. Tests use it to see that send hits disk first.
var beforeReplyHook func()

// storeLog records a skipped chat line. Tests replace it.
var storeLog = log.Printf

// chatReadHook fires when a chat JSONL is opened. Startup must not call it.
var chatReadHook func(path string)

type settingsDoc struct {
	Version         int          `json:"version"`
	Connections     []Connection `json:"connections,omitempty"`
	Defaults        Config       `json:"defaults"`
	ActiveID        string       `json:"activeId,omitempty"`
	SidebarWidth    float32      `json:"sidebarWidth,omitempty"`
	Appearance      string       `json:"appearance,omitempty"`
	Theme           string       `json:"theme,omitempty"`
	SendWith        string       `json:"sendWith,omitempty"`
	ShowReasoning   bool         `json:"showReasoning"`
	RestoreLast     bool         `json:"restoreLast"`
	KeepLastRequest bool         `json:"keepLastRequest"`
	TimeoutSec      int          `json:"timeoutSec,omitempty"`
	MaxRounds       int          `json:"maxRounds,omitempty"`
	// AutoTitles is a pointer so a file from before this field keeps the
	// default (on). A bool would read a missing key as false.
	AutoTitles *bool       `json:"autoTitles,omitempty"`
	TitleModel *TitleModel `json:"titleModel,omitempty"`
}

type indexDoc struct {
	Schema int          `json:"schema"`
	Chats  []indexEntry `json:"chats"`
}

type indexEntry struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	Provider     string    `json:"provider,omitempty"`
	Model        string    `json:"model,omitempty"`
	ConnectionID string    `json:"connectionId,omitempty"`
	Draft        bool      `json:"draft,omitempty"`
	DraftText    string    `json:"draftText,omitempty"`
	DraftQuotes  []Quote   `json:"draftQuotes,omitempty"`
	MessageCount int       `json:"messageCount"`
	Schema       int       `json:"schema"`
}

type chatHeader struct {
	Type           string          `json:"type"`
	Schema         int             `json:"schema"`
	ID             string          `json:"id"`
	CreatedAt      time.Time       `json:"createdAt"`
	Title          string          `json:"title"`
	UpdatedAt      time.Time       `json:"updatedAt,omitempty"`
	Draft          string          `json:"draft,omitempty"`
	DraftQuotes    []Quote         `json:"draftQuotes,omitempty"`
	Settings       json.RawMessage `json:"settings"`
	MaxRounds      int             `json:"maxRounds,omitempty"`
	SwitchNote     string          `json:"switchNote,omitempty"`
	TitleSource    string          `json:"titleSource,omitempty"`
	TitleGenerated bool            `json:"titleGenerated,omitempty"`
}

// configOverride is the Chat Settings fields that differ from the global
// defaults. Absent fields inherit the defaults. API keys are never included.
type configOverride struct {
	Provider     *string  `json:"provider,omitempty"`
	SystemPrompt *string  `json:"systemPrompt,omitempty"`
	Model        *string  `json:"model,omitempty"`
	BaseURL      *string  `json:"baseUrl,omitempty"`
	ConnectionID *string  `json:"connectionId,omitempty"`
	Temperature  *float64 `json:"temperature,omitempty"`
	tempNull     bool
	MaxTokens    *int    `json:"maxTokens,omitempty"`
	Reasoning    *string `json:"reasoning,omitempty"`
	Style        *string `json:"reasoningStyle,omitempty"`
	ExtraBody    *string `json:"extraBody,omitempty"`
}

func (o configOverride) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	first := true
	add := func(key string, val any) {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		kb, _ := json.Marshal(key)
		vb, _ := json.Marshal(val)
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(vb)
	}
	if o.Provider != nil {
		add("provider", *o.Provider)
	}
	if o.SystemPrompt != nil {
		add("systemPrompt", *o.SystemPrompt)
	}
	if o.Model != nil {
		add("model", *o.Model)
	}
	if o.BaseURL != nil {
		add("baseUrl", *o.BaseURL)
	}
	if o.ConnectionID != nil {
		add("connectionId", *o.ConnectionID)
	}
	if o.tempNull {
		add("temperature", nil)
	} else if o.Temperature != nil {
		add("temperature", *o.Temperature)
	}
	if o.MaxTokens != nil {
		add("maxTokens", *o.MaxTokens)
	}
	if o.Reasoning != nil {
		add("reasoning", *o.Reasoning)
	}
	if o.Style != nil {
		add("reasoningStyle", *o.Style)
	}
	if o.ExtraBody != nil {
		add("extraBody", *o.ExtraBody)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func strPtr(s string) *string { return &s }

func chatOverrides(chat, base Config) configOverride {
	chat.APIKey = ""
	base.APIKey = ""
	var o configOverride
	if chat.Provider != base.Provider {
		o.Provider = strPtr(chat.Provider)
	}
	if chat.SystemPrompt != base.SystemPrompt {
		o.SystemPrompt = strPtr(chat.SystemPrompt)
	}
	// The selection is the connection id plus the model id. A chat on a
	// different connection keeps the model string even when it matches the
	// default, so a shared model id does not fall back to the other provider.
	if chat.Model != base.Model || (chat.ConnectionID != "" && chat.ConnectionID != base.ConnectionID && chat.Model != "") {
		o.Model = strPtr(chat.Model)
	}
	if chat.BaseURL != base.BaseURL {
		o.BaseURL = strPtr(chat.BaseURL)
	}
	if chat.ConnectionID != base.ConnectionID {
		o.ConnectionID = strPtr(chat.ConnectionID)
	}
	if !sameTemp(chat.Temperature, base.Temperature) {
		if chat.Temperature == nil {
			o.tempNull = true
		} else {
			v := *chat.Temperature
			o.Temperature = &v
		}
	}
	if chat.MaxTokens != base.MaxTokens {
		n := chat.MaxTokens
		o.MaxTokens = &n
	}
	if chat.Reasoning != base.Reasoning {
		o.Reasoning = strPtr(chat.Reasoning)
	}
	if chat.ReasoningStyle != base.ReasoningStyle {
		o.Style = strPtr(chat.ReasoningStyle)
	}
	if chat.ExtraBody != base.ExtraBody {
		o.ExtraBody = strPtr(chat.ExtraBody)
	}
	return o
}

func sameTemp(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func cloneConfig(c Config) Config {
	out := c
	out.APIKey = ""
	if c.Temperature != nil {
		v := *c.Temperature
		out.Temperature = &v
	}
	return out
}

// applyOverrides copies defaults and lays this chat's Chat Settings on top.
// A key in the file is ignored.
func applyOverrides(base Config, raw json.RawMessage) Config {
	cfg := cloneConfig(base)
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("{}")) {
		return cfg
	}
	if err := json.Unmarshal(trimmed, &cfg); err != nil {
		storeLog("cove: chat settings: %v", err)
		return cloneConfig(base)
	}
	cfg.APIKey = ""
	return cfg
}

type usageRec struct {
	Tokens    int `json:"tokens,omitempty"`
	ElapsedMS int `json:"elapsedMs,omitempty"`
}

type messageRec struct {
	Type           string    `json:"type"`
	ID             string    `json:"id"`
	Role           string    `json:"role"`
	Content        string    `json:"content"`
	Quotes         []Quote   `json:"quotes,omitempty"`
	Reasoning      string    `json:"reasoning,omitempty"`
	Model          string    `json:"model,omitempty"`
	ProviderID     string    `json:"providerId,omitempty"`
	ProviderKind   string    `json:"providerKind,omitempty"`
	ProviderName   string    `json:"providerName,omitempty"`
	ProviderCustom bool      `json:"providerCustom,omitempty"`
	Usage          *usageRec `json:"usage,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	Status         string    `json:"status"`
	Error          string    `json:"error,omitempty"`
	ThinkingMS     int       `json:"thinkingMs,omitempty"`
	ReasoningOpen  bool      `json:"reasoningOpen,omitempty"`
	ThinkStarted   time.Time `json:"thinkStarted,omitempty"`
}

type msgSig struct {
	ID      string
	Content string
	Status  string
}

type diskChat struct {
	header string
	sigs   []msgSig
}

func storeRoot(path string) string {
	return filepath.Dir(path)
}

func chatFile(root, id string) (string, error) {
	if id == "" || strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return "", fmt.Errorf("bad chat id %q", id)
	}
	return filepath.Join(root, chatsDir, id+".jsonl"), nil
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func headerKey(c Conversation, defaults Config) string {
	raw, _ := json.Marshal(chatOverrides(c.Config, defaults))
	b, _ := json.Marshal(struct {
		Title   string          `json:"title"`
		Created time.Time       `json:"createdAt"`
		Set     json.RawMessage `json:"settings"`
		Rounds  int             `json:"maxRounds,omitempty"`
		Note    string          `json:"switchNote,omitempty"`
		Quotes  []Quote         `json:"draftQuotes,omitempty"`
		Source  string          `json:"titleSource,omitempty"`
		Gen     bool            `json:"titleGenerated,omitempty"`
	}{c.Title, c.CreatedAt, raw, c.MaxRounds, c.SwitchNote, c.DraftQuotes, c.TitleSource, c.TitleGenerated})
	return string(b)
}

func messageStatus(m Message) string {
	if strings.TrimSpace(m.Error) != "" {
		return "error"
	}
	if m.Incomplete || !m.Done {
		return "incomplete"
	}
	return "complete"
}

func messageKept(m Message) bool {
	return m.Role == roleUser || m.Done || m.Incomplete || strings.TrimSpace(m.Error) != ""
}

func finalMessages(c Conversation) []Message {
	if c.Messages == nil {
		return nil
	}
	out := make([]Message, 0, len(c.Messages))
	for _, m := range c.Messages {
		if messageKept(m) {
			out = append(out, m)
		}
	}
	return out
}

func keepOnDisk(c Conversation) bool {
	// An index row has not been opened. Its messages are still in the
	// file, so a nil slice must not look like an empty draft.
	if !c.Loaded && c.Messages == nil {
		return c.MessageCount > 0 || c.DraftOnly || strings.TrimSpace(c.Draft) != "" || len(c.DraftQuotes) > 0
	}
	if len(finalMessages(c)) > 0 {
		return true
	}
	return strings.TrimSpace(c.Draft) != "" || len(c.DraftQuotes) > 0
}

func encodeHeader(c Conversation, defaults Config) ([]byte, error) {
	raw, err := json.Marshal(chatOverrides(c.Config, defaults))
	if err != nil {
		return nil, err
	}
	h := chatHeader{
		Type: "chat", Schema: chatSchema, ID: c.ID, CreatedAt: c.CreatedAt,
		Title: c.Title, UpdatedAt: c.UpdatedAt, Draft: c.Draft, DraftQuotes: c.DraftQuotes, Settings: raw,
		MaxRounds: c.MaxRounds, SwitchNote: c.SwitchNote,
		TitleSource: c.TitleSource, TitleGenerated: c.TitleGenerated,
	}
	b, err := json.Marshal(h)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

type sessionRec struct {
	Type      string    `json:"type"`
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Limit     int       `json:"limit,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

func encodeSession(b SessionBreak) ([]byte, error) {
	rec := sessionRec{Type: "session", ID: b.ID, Kind: b.Kind, CreatedAt: b.CreatedAt}
	if b.Kind == sessionAuto {
		rec.Limit = b.Limit
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func validSession(rec sessionRec) bool {
	if rec.ID == "" {
		return false
	}
	return rec.Kind == sessionManual || rec.Kind == sessionAuto
}

// diskEvent is one JSONL record after the header, in file order.
type diskEvent struct {
	sig  msgSig
	line func(Conversation) ([]byte, error)
}

func messageEvent(m Message) diskEvent {
	// Quotes ride in the signature so a turn whose blocks change is
	// rewritten even when the typed text stays the same.
	content := m.Content
	if len(m.Quotes) > 0 {
		raw, _ := json.Marshal(m.Quotes)
		content += "\x00" + string(raw)
	}
	return diskEvent{
		sig: msgSig{ID: m.ID, Content: content, Status: messageStatus(m)},
		line: func(c Conversation) ([]byte, error) {
			return encodeMessage(c, m)
		},
	}
}

func breakEvent(b SessionBreak) diskEvent {
	limit := "0"
	if b.Kind == sessionAuto {
		limit = strconv.Itoa(b.Limit)
	}
	return diskEvent{
		sig: msgSig{ID: b.ID, Content: b.Kind + ":" + limit, Status: "session"},
		line: func(Conversation) ([]byte, error) {
			return encodeSession(b)
		},
	}
}

// transcriptEvents is the durable log: kept messages, with each session
// break placed before the message it precedes. A trailing break is last.
func transcriptEvents(c Conversation) []diskEvent {
	var out []diskEvent
	bi := 0
	for i := 0; i <= len(c.Messages); i++ {
		for bi < len(c.Breaks) && c.Breaks[bi].After == i {
			out = append(out, breakEvent(c.Breaks[bi]))
			bi++
		}
		if i == len(c.Messages) {
			break
		}
		if messageKept(c.Messages[i]) {
			out = append(out, messageEvent(c.Messages[i]))
		}
	}
	for ; bi < len(c.Breaks); bi++ {
		if c.Breaks[bi].After >= len(c.Messages) {
			out = append(out, breakEvent(c.Breaks[bi]))
		}
	}
	return out
}

func eventSigs(events []diskEvent) []msgSig {
	out := make([]msgSig, len(events))
	for i, e := range events {
		out[i] = e.sig
	}
	return out
}

func encodeMessage(c Conversation, m Message) ([]byte, error) {
	rec := messageRec{
		Type: "message", ID: m.ID, Role: m.Role, Content: m.Content, Quotes: m.Quotes,
		Reasoning: m.Reasoning, CreatedAt: m.CreatedAt, Status: messageStatus(m),
		Error: m.Error, ThinkingMS: m.ThinkingMS, ReasoningOpen: m.ReasoningOpen,
		ThinkStarted: m.ThinkStarted,
	}
	// The snapshot is whatever the reply recorded at send time. A later
	// composer switch must not rewrite this line, and a legacy message
	// with no snapshot stays without one.
	if m.Role == roleAssistant {
		rec.Model = strings.TrimSpace(m.Model)
		rec.ProviderID = strings.TrimSpace(m.ProviderID)
		rec.ProviderKind = strings.TrimSpace(m.ProviderKind)
		rec.ProviderName = strings.TrimSpace(m.ProviderName)
		rec.ProviderCustom = m.ProviderCustom
	}
	if m.Tokens > 0 || m.ElapsedMS > 0 {
		rec.Usage = &usageRec{Tokens: m.Tokens, ElapsedMS: m.ElapsedMS}
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func sameSigs(disk, cur []msgSig) bool {
	if len(disk) > len(cur) {
		return false
	}
	for i := range disk {
		if disk[i] != cur[i] {
			return false
		}
	}
	return true
}

// readChat loads one JSONL file. Header settings are this chat's overrides
// on top of defaults. A truncated or corrupt final line is skipped and
// logged. The chat is kept. It does not attach a connection id; the store
// writer uses that so a header still missing one is rewritten on the next save.
func readChat(path string, defaults Config) (Conversation, error) {
	return readChatConn(path, defaults, nil)
}

// readChatConn is readChat, then binds a header that stored a base URL or
// a bare model and no connection id onto the connection that header names.
func readChatConn(path string, defaults Config, conns []Connection) (Conversation, error) {
	if chatReadHook != nil {
		chatReadHook(path)
	}
	f, err := os.Open(path)
	if err != nil {
		return Conversation{}, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	var lines []string
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	if err := sc.Err(); err != nil {
		return Conversation{}, err
	}
	if len(lines) == 0 {
		return Conversation{}, fmt.Errorf("empty chat file")
	}
	var header chatHeader
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		return Conversation{}, fmt.Errorf("chat header: %w", err)
	}
	if header.Type != "" && header.Type != "chat" {
		return Conversation{}, fmt.Errorf("chat header type %q", header.Type)
	}
	conv := Conversation{
		ID: header.ID, Title: header.Title, CreatedAt: header.CreatedAt,
		UpdatedAt: header.UpdatedAt, Draft: header.Draft, DraftQuotes: header.DraftQuotes,
		Config:         applyOverrides(defaults, header.Settings),
		MaxRounds:      clampRounds(header.MaxRounds),
		SwitchNote:     header.SwitchNote,
		TitleSource:    header.TitleSource,
		TitleGenerated: header.TitleGenerated,
		Messages:       []Message{}, Loaded: true,
	}
	if conv.UpdatedAt.IsZero() {
		conv.UpdatedAt = conv.CreatedAt
	}
	id := conv.ID
	for i, line := range lines[1:] {
		var rec messageRec
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			last := i == len(lines)-2
			if last {
				storeLog("cove: chat %s: skipped truncated final line: %v", id, err)
				continue
			}
			storeLog("cove: chat %s: skipped corrupt line %d: %v", id, i+2, err)
			continue
		}
		if rec.Type == "session" {
			var cut sessionRec
			if err := json.Unmarshal([]byte(line), &cut); err != nil || !validSession(cut) {
				storeLog("cove: chat %s: skipped session line %d", id, i+2)
				continue
			}
			if cut.Kind != sessionAuto {
				cut.Limit = 0
			}
			conv.Breaks = append(conv.Breaks, SessionBreak{
				ID: cut.ID, Kind: cut.Kind, Limit: cut.Limit, CreatedAt: cut.CreatedAt,
				After: len(conv.Messages),
			})
			continue
		}
		if rec.Type != "" && rec.Type != "message" {
			continue
		}
		m := Message{
			ID: rec.ID, Role: rec.Role, Content: rec.Content, Quotes: rec.Quotes, Reasoning: rec.Reasoning,
			CreatedAt: rec.CreatedAt, Error: rec.Error, ThinkingMS: rec.ThinkingMS,
			ReasoningOpen: rec.ReasoningOpen, ThinkStarted: rec.ThinkStarted,
			Model: rec.Model, ProviderID: rec.ProviderID, ProviderKind: rec.ProviderKind,
			ProviderName: rec.ProviderName, ProviderCustom: rec.ProviderCustom,
		}
		if rec.Usage != nil {
			m.Tokens = rec.Usage.Tokens
			m.ElapsedMS = rec.Usage.ElapsedMS
		}
		switch rec.Status {
		case "error":
			m.Done = true
			if m.Error == "" {
				m.Error = "error"
			}
		case "incomplete":
			// A stopped reply is finished from the user's point of view.
			// It must not come back as a live stream.
			m.Done = true
			m.Incomplete = true
		default:
			m.Done = true
		}
		conv.Messages = append(conv.Messages, m)
		if !m.CreatedAt.IsZero() && m.CreatedAt.After(conv.UpdatedAt) {
			conv.UpdatedAt = m.CreatedAt
		}
	}
	conv.MessageCount = len(conv.Messages)
	conv.DraftOnly = len(conv.Messages) == 0 && (strings.TrimSpace(conv.Draft) != "" || len(conv.DraftQuotes) > 0)
	conv.diskFinal = len(finalMessages(conv))
	conv.diskHeader = headerKey(conv, defaults)
	if conns != nil {
		conv.Config = bindChatConnection(conv.Config, header.Settings, conns)
	}
	return conv, nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := syncDir(dir); err != nil {
		return err
	}
	if atomicHook != nil {
		if err := atomicHook(path, "rename"); err != nil {
			return err
		}
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	ok = true
	return syncDir(dir)
}

func writeSettings(root string, data *StoreData) error {
	doc := settingsDoc{
		Version: storeVersion, Connections: data.Connections, Defaults: data.Defaults,
		ActiveID: data.ActiveID, SidebarWidth: data.SidebarWidth, Appearance: data.Appearance,
		Theme: data.Theme, SendWith: data.SendWith, ShowReasoning: data.ShowReasoning,
		RestoreLast: data.RestoreLast, KeepLastRequest: data.KeepLastRequest, TimeoutSec: data.TimeoutSec,
		MaxRounds: data.MaxRounds, AutoTitles: &data.AutoTitles, TitleModel: data.TitleModel,
	}
	doc.Defaults.APIKey = ""
	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	return atomicWrite(filepath.Join(root, settingsFile), buf, 0o600)
}

func writeIndex(root string, entries []indexEntry) error {
	if entries == nil {
		entries = []indexEntry{}
	}
	buf, err := json.MarshalIndent(indexDoc{Schema: chatSchema, Chats: entries}, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	return atomicWrite(filepath.Join(root, indexFile), buf, 0o600)
}

func writeLayoutMarker(root string) error {
	return atomicWrite(filepath.Join(root, layoutFile), []byte("5\n"), 0o600)
}

func layoutCommitted(root string) bool {
	return fileExists(filepath.Join(root, layoutFile))
}

func indexFrom(c Conversation, n int) indexEntry {
	return indexEntry{
		ID: c.ID, Title: c.Title, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
		Provider: c.Config.Provider, Model: c.Config.Model, ConnectionID: c.Config.ConnectionID,
		Draft:     len(finalMessages(c)) == 0 && (strings.TrimSpace(c.Draft) != "" || len(c.DraftQuotes) > 0),
		DraftText: c.Draft, DraftQuotes: c.DraftQuotes, MessageCount: n, Schema: chatSchema,
	}
}

func rewriteChat(root string, c Conversation, defaults Config) error {
	path, err := chatFile(root, c.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var buf bytes.Buffer
	h, err := encodeHeader(c, defaults)
	if err != nil {
		return err
	}
	buf.Write(h)
	for _, ev := range transcriptEvents(c) {
		line, err := ev.line(c)
		if err != nil {
			return err
		}
		buf.Write(line)
	}
	return atomicWrite(path, buf.Bytes(), 0o600)
}

func appendMessages(root string, c Conversation, msgs []Message) error {
	events := make([]diskEvent, len(msgs))
	for i, m := range msgs {
		events[i] = messageEvent(m)
	}
	return appendEvents(root, c, events)
}

func appendEvents(root string, c Conversation, events []diskEvent) error {
	if len(events) == 0 {
		return nil
	}
	path, err := chatFile(root, c.ID)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	for _, ev := range events {
		line, err := ev.line(c)
		if err != nil {
			f.Close()
			return err
		}
		if _, err := f.Write(line); err != nil {
			f.Close()
			return err
		}
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func removeChat(root, id string) error {
	path, err := chatFile(root, id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncDir(filepath.Join(root, chatsDir))
}

func readSettings(root string) (settingsDoc, error) {
	b, err := os.ReadFile(filepath.Join(root, settingsFile))
	if err != nil {
		return settingsDoc{}, err
	}
	var doc settingsDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		return settingsDoc{}, err
	}
	return doc, nil
}

func readIndex(root string) (indexDoc, error) {
	b, err := os.ReadFile(filepath.Join(root, indexFile))
	if err != nil {
		return indexDoc{}, err
	}
	var doc indexDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		return indexDoc{}, err
	}
	if doc.Chats == nil {
		doc.Chats = []indexEntry{}
	}
	return doc, nil
}

func settingsToData(doc settingsDoc) StoreData {
	data := defaultData()
	if doc.Version != 0 {
		data.Version = doc.Version
	}
	data.Connections = doc.Connections
	if doc.Defaults.Model != "" || doc.Defaults.BaseURL != "" || doc.Defaults.Provider != "" {
		data.Defaults = doc.Defaults
	}
	data.ActiveID = doc.ActiveID
	data.SidebarWidth = doc.SidebarWidth
	data.Appearance = doc.Appearance
	data.Theme = doc.Theme
	data.SendWith = doc.SendWith
	data.ShowReasoning = doc.ShowReasoning
	data.RestoreLast = doc.RestoreLast
	data.KeepLastRequest = doc.KeepLastRequest
	data.TimeoutSec = doc.TimeoutSec
	if doc.AutoTitles != nil {
		data.AutoTitles = *doc.AutoTitles
	}
	data.TitleModel = doc.TitleModel
	active := data.ActiveID
	normalize(&data)
	data.ActiveID = active
	data.MaxRounds = clampRounds(doc.MaxRounds)
	data.Conversations = []Conversation{}
	return data
}

func convFromIndex(e indexEntry) Conversation {
	c := Conversation{
		ID: e.ID, Title: e.Title, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
		Config:         Config{Provider: e.Provider, Model: e.Model, ConnectionID: e.ConnectionID},
		Draft:          e.DraftText,
		DraftQuotes:    e.DraftQuotes,
		DraftOnly:      e.Draft,
		DraftFromIndex: true,
		MessageCount:   e.MessageCount,
		Messages:       nil,
		Loaded:         false,
	}
	if c.Title == "" {
		c.Title = newChatTitle
	}
	return c
}

// loadLazy reads settings and the sidebar index. It does not open chat files.
func loadLazy(root string) (StoreData, error) {
	doc, err := readSettings(root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return StoreData{}, err
	}
	data := defaultData()
	if err == nil {
		data = settingsToData(doc)
	}
	idx, err := readIndex(root)
	if err != nil {
		return StoreData{}, err
	}
	convs := make([]Conversation, 0, len(idx.Chats))
	seen := map[string]bool{}
	for _, e := range idx.Chats {
		if e.ID == "" || seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		convs = append(convs, convFromIndex(e))
	}
	sortConversations(convs)
	data.Conversations = convs
	if data.ActiveID != "" && !seen[data.ActiveID] {
		data.ActiveID = ""
	}
	data.Version = storeVersion
	return data, nil
}

// loadFull reads settings and every chat file. Tests use it to round-trip.
func loadFull(root string) (StoreData, error) {
	data, err := loadLazy(root)
	if err != nil {
		return StoreData{}, err
	}
	for i := range data.Conversations {
		c := &data.Conversations[i]
		path, err := chatFile(root, c.ID)
		if err != nil {
			return StoreData{}, err
		}
		loaded, err := readChatConn(path, data.Defaults, data.Connections)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				c.Loaded = true
				c.Messages = []Message{}
				continue
			}
			return StoreData{}, err
		}
		draft := c.Draft
		draftQuotes := append([]Quote(nil), c.DraftQuotes...)
		fromIndex := c.DraftFromIndex
		title := c.Title
		loaded.DraftFromIndex = fromIndex
		if fromIndex {
			loaded.Draft = draft
			loaded.DraftQuotes = draftQuotes
		}
		if title != "" {
			loaded.Title = title
		}
		data.Conversations[i] = loaded
	}
	sortConversations(data.Conversations)
	return data, nil
}

// rebuildIndex scans chat headers and last lines when the index is missing
// or corrupt. A bad final line does not drop the chat.
func rebuildIndex(root string) (StoreData, error) {
	doc, err := readSettings(root)
	data := defaultData()
	if err == nil {
		data = settingsToData(doc)
	}
	dir := filepath.Join(root, chatsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			data.Conversations = []Conversation{}
			return data, writeIndex(root, nil)
		}
		return StoreData{}, err
	}
	var idx []indexEntry
	var convs []Conversation
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		conv, err := readChatConn(path, data.Defaults, data.Connections)
		if err != nil {
			storeLog("cove: skipping %s while rebuilding the index: %v", e.Name(), err)
			continue
		}
		n := len(finalMessages(conv))
		idx = append(idx, indexFrom(conv, n))
		conv.Loaded = false
		conv.Messages = nil
		conv.Breaks = nil
		conv.DraftFromIndex = true
		conv.MessageCount = n
		convs = append(convs, conv)
	}
	if err := writeIndex(root, idx); err != nil {
		return StoreData{}, err
	}
	sortConversations(convs)
	data.Conversations = convs
	data.Version = storeVersion
	seen := map[string]bool{}
	for _, c := range convs {
		seen[c.ID] = true
	}
	if data.ActiveID != "" && !seen[data.ActiveID] {
		data.ActiveID = ""
	}
	return data, nil
}

type writeReq struct {
	kind  int
	snap  StoreData
	id    string
	msg   Message
	title string
	done  chan error
}

const (
	opSnap = iota
	opAppend
	opRetitle
)

type storeWriter struct {
	root     string
	ch       chan writeReq
	disk     map[string]diskChat
	defaults Config
}

func (w *storeWriter) baseConfig() Config {
	if w.defaults.Model != "" || w.defaults.BaseURL != "" || w.defaults.Provider != "" {
		return w.defaults
	}
	if doc, err := readSettings(w.root); err == nil {
		w.defaults = doc.Defaults
	}
	return w.defaults
}

var (
	writerMu sync.Mutex
	writers  = map[string]*storeWriter{}
)

func writerFor(root string) *storeWriter {
	writerMu.Lock()
	defer writerMu.Unlock()
	w := writers[root]
	if w != nil {
		return w
	}
	w = &storeWriter{root: root, ch: make(chan writeReq, 64), disk: map[string]diskChat{}}
	writers[root] = w
	go w.loop()
	return w
}

func (w *storeWriter) loop() {
	for req := range w.ch {
		var err error
		switch req.kind {
		case opAppend:
			err = w.appendOne(req.id, req.msg)
		case opRetitle:
			err = w.retitle(req.id, req.title)
		default:
			err = w.saveSnap(&req.snap)
		}
		req.done <- err
	}
}

func submit(root string, req writeReq) error {
	req.done = make(chan error, 1)
	writerFor(root).ch <- req
	return <-req.done
}

// AppendFinal adds one finished message to a chat file and then the index.
func AppendFinal(root, id string, msg Message) error {
	msg.Done = true
	return submit(root, writeReq{kind: opAppend, id: id, msg: msg})
}

// Retitle rewrites a chat file with a new title, keeping its messages.
func Retitle(root, id, title string) error {
	return submit(root, writeReq{kind: opRetitle, id: id, title: title})
}

func (w *storeWriter) remember(c Conversation) {
	w.disk[c.ID] = diskChat{header: headerKey(c, w.baseConfig()), sigs: eventSigs(transcriptEvents(c))}
}

func (w *storeWriter) learn(id string) (Conversation, diskChat, error) {
	base := w.baseConfig()
	if d, ok := w.disk[id]; ok {
		path, err := chatFile(w.root, id)
		if err != nil {
			return Conversation{}, diskChat{}, err
		}
		conv, err := readChat(path, base)
		if err != nil {
			return Conversation{}, diskChat{}, err
		}
		return conv, d, nil
	}
	path, err := chatFile(w.root, id)
	if err != nil {
		return Conversation{}, diskChat{}, err
	}
	conv, err := readChat(path, base)
	if err != nil {
		return Conversation{}, diskChat{}, err
	}
	d := diskChat{header: headerKey(conv, base), sigs: eventSigs(transcriptEvents(conv))}
	w.disk[id] = d
	return conv, d, nil
}

func (w *storeWriter) appendOne(id string, msg Message) error {
	conv, d, err := w.learn(id)
	if err != nil {
		return err
	}
	conv.Messages = append(conv.Messages, msg)
	conv.UpdatedAt = time.Now()
	if err := appendMessages(w.root, conv, []Message{msg}); err != nil {
		return err
	}
	d.sigs = eventSigs(transcriptEvents(conv))
	d.header = headerKey(conv, w.baseConfig())
	w.disk[id] = d
	return w.putIndex(conv, len(finalMessages(conv)))
}

func (w *storeWriter) retitle(id, title string) error {
	conv, _, err := w.learn(id)
	if err != nil {
		return err
	}
	conv.Title = title
	conv.UpdatedAt = time.Now()
	if err := rewriteChat(w.root, conv, w.baseConfig()); err != nil {
		return err
	}
	w.remember(conv)
	return w.putIndex(conv, len(finalMessages(conv)))
}

func (w *storeWriter) putIndex(c Conversation, n int) error {
	idx, err := readIndex(w.root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	entry := indexFrom(c, n)
	found := false
	for i := range idx.Chats {
		if idx.Chats[i].ID == c.ID {
			idx.Chats[i] = entry
			found = true
			break
		}
	}
	if !found {
		idx.Chats = append(idx.Chats, entry)
	}
	return writeIndex(w.root, idx.Chats)
}

func (w *storeWriter) saveSnap(data *StoreData) error {
	clean := stripSecrets(data)
	w.defaults = clean.Defaults
	if err := os.MkdirAll(w.root, 0o700); err != nil {
		return err
	}
	if err := writeSettings(w.root, clean); err != nil {
		return err
	}
	entries := make([]indexEntry, 0, len(clean.Conversations))
	alive := map[string]bool{}
	for i := range clean.Conversations {
		c := clean.Conversations[i]
		alive[c.ID] = true
		if !keepOnDisk(c) {
			if _, ok := w.disk[c.ID]; ok || chatExists(w.root, c.ID) {
				if err := removeChat(w.root, c.ID); err != nil {
					return err
				}
				delete(w.disk, c.ID)
			}
			continue
		}
		// An index row this process has not opened stays on disk. A chat
		// that only exists in memory (Messages nil, no file yet) is written
		// below, including a draft that has text.
		if !c.Loaded && c.Messages == nil && chatExists(w.root, c.ID) {
			n := c.MessageCount
			title := c.Title
			draft := n == 0 && (strings.TrimSpace(c.Draft) != "" || len(c.DraftQuotes) > 0)
			if n == 0 && strings.TrimSpace(c.Draft) != "" {
				title = draftTitle(c.Draft)
			}
			entries = append(entries, indexEntry{
				ID: c.ID, Title: title, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
				Provider: c.Config.Provider, Model: c.Config.Model, ConnectionID: c.Config.ConnectionID,
				Draft: draft, DraftText: c.Draft, DraftQuotes: c.DraftQuotes, MessageCount: n, Schema: chatSchema,
			})
			continue
		}
		if len(finalMessages(c)) == 0 && strings.TrimSpace(c.Draft) != "" {
			c.Title = draftTitle(c.Draft)
		}
		finals := finalMessages(c)
		events := transcriptEvents(c)
		sigs := eventSigs(events)
		key := headerKey(c, w.defaults)
		d, ok := w.disk[c.ID]
		if !ok && chatExists(w.root, c.ID) {
			existing, err := readChat(mustChat(w.root, c.ID), w.defaults)
			if err == nil {
				d = diskChat{header: headerKey(existing, w.defaults), sigs: eventSigs(transcriptEvents(existing))}
				ok = true
			}
		}
		if ok && key == d.header && sameSigs(d.sigs, sigs) {
			if len(events) > len(d.sigs) {
				if err := appendEvents(w.root, c, events[len(d.sigs):]); err != nil {
					return err
				}
			}
		} else {
			if err := rewriteChat(w.root, c, w.defaults); err != nil {
				return err
			}
		}
		w.disk[c.ID] = diskChat{header: key, sigs: sigs}
		entries = append(entries, indexFrom(c, len(finals)))
	}
	for id := range w.disk {
		if !alive[id] {
			if err := removeChat(w.root, id); err != nil {
				return err
			}
			delete(w.disk, id)
		}
	}
	if old, err := readIndex(w.root); err == nil {
		for _, e := range old.Chats {
			if !alive[e.ID] {
				if err := removeChat(w.root, e.ID); err != nil {
					return err
				}
				delete(w.disk, e.ID)
			}
		}
	}
	// Chats removed from the snapshot but never cached still need to go.
	if entries == nil {
		entries = []indexEntry{}
	}
	if err := writeIndex(w.root, entries); err != nil {
		return err
	}
	return writeLayoutMarker(w.root)
}

func chatExists(root, id string) bool {
	path, err := chatFile(root, id)
	if err != nil {
		return false
	}
	return fileExists(path)
}

func mustChat(root, id string) string {
	path, _ := chatFile(root, id)
	return path
}

// migrateHook, when set, runs before each migration step. A non-nil error
// aborts the step. Tests use it to fail in the middle.
var migrateHook func(step string) error

func runMigrateHook(step string) error {
	if migrateHook == nil {
		return nil
	}
	return migrateHook(step)
}

// migrateLegacy writes the new tree into a temp directory, moves it into
// root, and renames the old file to name.bak. Any failure removes the new
// files and leaves the old file byte-for-byte, so the next launch can retry.
// A second call is a no-op once the new layout is in place and the old file is gone.
func migrateLegacy(root, legacyPath string, data *StoreData) error {
	if layoutAdopted(root) && !fileExists(legacyPath) {
		return finishLegacyBak(legacyPath)
	}
	parent := filepath.Dir(root)
	if parent == "" {
		parent = "."
	}
	cleanMigrateTemps(parent)
	if err := runMigrateHook("snapshot"); err != nil {
		return failMigration(root, legacyPath, "snapshot", err)
	}
	tmp, err := os.MkdirTemp(parent, ".cove-migrate-")
	if err != nil {
		return failMigration(root, legacyPath, "snapshot", err)
	}
	defer os.RemoveAll(tmp)
	w := &storeWriter{root: tmp, disk: map[string]diskChat{}}
	snap := cloneStore(*data)
	for i := range snap.Conversations {
		snap.Conversations[i].Loaded = true
		if snap.Conversations[i].Messages == nil {
			snap.Conversations[i].Messages = []Message{}
		}
	}
	if err := w.saveSnap(&snap); err != nil {
		return failMigration(root, legacyPath, "snapshot", err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return failMigration(root, legacyPath, "snapshot", err)
	}
	steps := []struct {
		name string
		from string
		to   string
	}{
		{"rename-settings", filepath.Join(tmp, settingsFile), filepath.Join(root, settingsFile)},
		{"rename-index", filepath.Join(tmp, indexFile), filepath.Join(root, indexFile)},
		{"rename-chats", filepath.Join(tmp, chatsDir), filepath.Join(root, chatsDir)},
	}
	for _, step := range steps {
		if err := runMigrateHook(step.name); err != nil {
			return failMigration(root, legacyPath, step.name, err)
		}
		if err := os.Rename(step.from, step.to); err != nil {
			return failMigration(root, legacyPath, step.name, err)
		}
	}
	if err := syncDir(root); err != nil {
		return failMigration(root, legacyPath, "rename-chats", err)
	}
	// The marker is the commit. Until it is renamed into place, openStore
	// keeps using the old file and does not show the new tree.
	if err := runMigrateHook("commit"); err != nil {
		return failMigration(root, legacyPath, "commit", err)
	}
	if err := writeLayoutMarker(root); err != nil {
		return failMigration(root, legacyPath, "commit", err)
	}
	if err := runMigrateHook("backup"); err != nil {
		return failMigration(root, legacyPath, "backup", err)
	}
	if err := finishLegacyBak(legacyPath); err != nil {
		return failMigration(root, legacyPath, "backup", err)
	}
	return nil
}

func failMigration(root, legacyPath, step string, err error) error {
	storeLog("cove: migration failed at %s: %v; keeping %s", step, err, filepath.Base(legacyPath))
	removeNewLayout(root)
	return err
}

func layoutAdopted(root string) bool {
	return layoutCommitted(root)
}

func removeNewLayout(root string) {
	_ = os.Remove(filepath.Join(root, settingsFile))
	_ = os.Remove(filepath.Join(root, indexFile))
	_ = os.Remove(filepath.Join(root, layoutFile))
	_ = os.RemoveAll(filepath.Join(root, chatsDir))
}

func cleanMigrateTemps(parent string) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".cove-migrate-") {
			_ = os.RemoveAll(filepath.Join(parent, e.Name()))
		}
	}
}

// discardPartialMigration drops a new tree that was never committed. The
// live conversations.json stays the only store, so a later retry cannot
// show those chats beside the ones in the old file.
func discardPartialMigration(root, legacyPath string) {
	if !fileExists(legacyPath) || layoutCommitted(root) || !partialLayout(root) {
		return
	}
	storeLog("cove: discarding an unfinished migration and keeping %s", filepath.Base(legacyPath))
	removeNewLayout(root)
	cleanMigrateTemps(filepath.Dir(root))
}

func partialLayout(root string) bool {
	if fileExists(filepath.Join(root, settingsFile)) || fileExists(filepath.Join(root, indexFile)) {
		return true
	}
	st, err := os.Stat(filepath.Join(root, chatsDir))
	return err == nil && st.IsDir()
}

// saveLegacy writes the single-file store. It is the fallback while a
// migration has not committed, so a failed migration can still save chats.
func saveLegacy(path string, data *StoreData) error {
	clean := stripSecrets(data)
	buf, err := json.MarshalIndent(clean, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	return atomicWrite(path, buf, 0o600)
}

func finishLegacyBak(legacyPath string) error {
	if !fileExists(legacyPath) {
		return nil
	}
	bak := legacyPath + ".bak"
	if fileExists(bak) {
		return nil
	}
	if err := os.Rename(legacyPath, bak); err != nil {
		return err
	}
	return syncDir(filepath.Dir(legacyPath))
}

func dirHasChats(root string) bool {
	entries, err := os.ReadDir(filepath.Join(root, chatsDir))
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			return true
		}
	}
	return false
}

// SaveSnapshot writes a store through the single writer goroutine.
func SaveSnapshot(path string, data *StoreData) error {
	if path == "" {
		return errors.New("no path to save conversations")
	}
	return submit(storeRoot(path), writeReq{kind: opSnap, snap: cloneStore(*data)})
}

// readLegacy parses a schema 2/3/4 single file. It does not write.
func readLegacy(path string) (StoreData, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return StoreData{}, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return StoreData{}, io.ErrUnexpectedEOF
	}
	var data StoreData
	if err := json.Unmarshal(b, &data); err != nil {
		return StoreData{}, fmt.Errorf("conversations file is not valid JSON: %w", err)
	}
	normalize(&data)
	for i := range data.Conversations {
		c := &data.Conversations[i]
		c.Loaded = true
		c.MessageCount = len(c.Messages)
		c.DraftOnly = len(c.Messages) == 0 && strings.TrimSpace(c.Draft) != ""
	}
	return data, nil
}
