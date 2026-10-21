package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"cove/themes"
)

const newChatTitle = "New Chat"

// Config is the model setup for one conversation, or the default new
// conversations inherit.
type Config struct {
	Provider     string `json:"provider"`
	SystemPrompt string `json:"systemPrompt"`
	Model        string `json:"model"`
	BaseURL      string `json:"baseUrl"`
	// ConnectionID is the shared connection whose API key lives in the
	// credential store. APIKey is legacy JSON and a form field; Save strips
	// it so the conversations file does not keep secrets.
	ConnectionID string   `json:"connectionId,omitempty"`
	APIKey       string   `json:"apiKey,omitempty"`
	Temperature  *float64 `json:"temperature,omitempty"`
	MaxTokens    int      `json:"maxTokens"`
	// Reasoning is the effort for this chat: "", "off", "minimal", "low",
	// "medium", or "high". Empty means Default, which sends no extra field.
	Reasoning string `json:"reasoning,omitempty"`
	// ReasoningStyle is how a Custom provider wants that effort encoded
	// (OpenAI, OpenRouter, xAI, DeepSeek, Ollama, or None). Named providers
	// ignore it and use their own encoding.
	ReasoningStyle string `json:"reasoningStyle,omitempty"`
	// ExtraBody is a JSON object merged onto the chat request after Cove's
	// own fields, so the user's keys win. Empty sends the usual body.
	ExtraBody string `json:"extraBody,omitempty"`
	// LegacyModels is the comma-separated model list from schema 4 and
	// earlier. Migration copies it onto the selected provider and clears it.
	LegacyModels string `json:"models,omitempty"`
}

// Quote is text lifted from the transcript into the composer, and the
// copy stored on the user message that was sent with it. Text is the
// selection itself: a code block stays raw, and a drag across several
// messages is one string in reading order. SourceMessageID is the first
// message that drag touched. Src and Time are the card's source line
// ("Cove" / "You" and the message clock). Multi omits that line. Code
// keeps the selection in a monospace face and sends it inside a fence.
type Quote struct {
	Text            string    `json:"text"`
	SourceMessageID string    `json:"msgId,omitempty"`
	Src             string    `json:"src,omitempty"`
	Time            time.Time `json:"time,omitempty"`
	Code            bool      `json:"code,omitempty"`
	Lang            string    `json:"lang,omitempty"`
	Multi           bool      `json:"multi,omitempty"`
}

// UnmarshalJSON accepts the phase-1 sourceMessageId key as well as msgId.
func (q *Quote) UnmarshalJSON(b []byte) error {
	var raw struct {
		Text            string    `json:"text"`
		SourceMessageID string    `json:"sourceMessageId"`
		MsgID           string    `json:"msgId"`
		Src             string    `json:"src"`
		Time            time.Time `json:"time"`
		Code            bool      `json:"code"`
		Lang            string    `json:"lang"`
		Multi           bool      `json:"multi"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*q = Quote{
		Text: raw.Text, Src: raw.Src, Time: raw.Time, Code: raw.Code,
		Lang: raw.Lang, Multi: raw.Multi,
	}
	q.SourceMessageID = raw.MsgID
	if q.SourceMessageID == "" {
		q.SourceMessageID = raw.SourceMessageID
	}
	return nil
}

// Message is one turn in a conversation. Content is the raw markdown.
type Message struct {
	ID      string `json:"id"`
	Role    string `json:"role"`
	Content string `json:"content"`
	// Quotes are the blocks attached when this user turn was sent. They
	// are omitted on assistant turns and on messages written before quotes
	// existed. The request prefixes Content with them; the bubble does not
	// bake them into Content.
	Quotes        []Quote `json:"quotes,omitempty"`
	Reasoning     string  `json:"reasoning,omitempty"`
	ReasoningOpen bool    `json:"reasoningOpen,omitempty"`
	// ThinkStarted is when the first reasoning delta arrived. ThinkingMS is
	// how long reasoning ran, kept so a reopened chat can say "Thought for 12s".
	ThinkStarted time.Time `json:"thinkStarted,omitempty"`
	ThinkingMS   int       `json:"thinkingMs,omitempty"`
	Done         bool      `json:"done"`
	// Incomplete is a reply that was stopped before it finished. It is
	// durable, with status "incomplete", and it is not still streaming.
	Incomplete bool   `json:"-"`
	Error      string `json:"error,omitempty"`
	// Tokens and ElapsedMS are optional usage for the action row
	// ("1.2k tokens · 3.4s"). Zero hides that part.
	Tokens    int       `json:"tokens,omitempty"`
	ElapsedMS int       `json:"elapsedMs,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	// Reply snapshot, written when the assistant turn starts. The footer
	// reads these and never the chat's current model. Empty means a legacy
	// turn: the footer draws no model label and no provider dot.
	// ProviderName is the display name at send time, so a later rename or
	// delete does not change the label. ProviderKind and ProviderCustom
	// are the dot colour. ProviderID is the connection id.
	Model          string `json:"model,omitempty"`
	ProviderID     string `json:"providerId,omitempty"`
	ProviderKind   string `json:"providerKind,omitempty"`
	ProviderName   string `json:"providerName,omitempty"`
	ProviderCustom bool   `json:"providerCustom,omitempty"`
}

// SessionBreak is a cut in one chat's context. Messages before it stay in
// the transcript and on disk, and they are left out of later requests.
// Kind is "manual" or "auto". Limit is the maxRounds that triggered
// an auto break; a manual break leaves it 0. After is how many messages
// precede the break in this process, and it is not stored: the JSONL order
// is the position.
type SessionBreak struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Limit     int       `json:"limit,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	After     int       `json:"-"`
}

// Conversation is a chat and the model it talks to.
type Conversation struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Config   Config    `json:"config"`
	Messages []Message `json:"messages"`
	Draft    string    `json:"draft,omitempty"`
	// DraftQuotes travel with Draft: the header and the sidebar index both
	// keep them, so a switch or a restart restores the chips above the field.
	DraftQuotes []Quote `json:"-"`
	// LastRequest is this chat's most recent request body, pretty-printed.
	// It is not saved: the key is a header, and the body is only a debug view.
	LastRequest string    `json:"-"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	// MaxRounds is this chat's limit on completed rounds in the
	// current session. 0 is unlimited. It is fixed when the chat is created
	// (copied from the global default) and when SetMaxContextRounds runs.
	// A missing value on an older file stays unlimited, even if the global
	// default later changes.
	MaxRounds int `json:"-"`
	// Breaks are session cuts, in file order. The transcript does not render
	// them; request assembly stops at the last one.
	Breaks []SessionBreak `json:"-"`
	// Loaded is true once Messages is this process's copy. An index row
	// leaves it false and Messages nil so startup does not read chat files.
	Loaded       bool `json:"-"`
	MessageCount int  `json:"-"`
	DraftOnly    bool `json:"-"`
	// DraftFromIndex means Draft came from the sidebar index, which is
	// written after the chat file and wins over a stale header draft.
	DraftFromIndex bool `json:"-"`
	// diskFinal / diskHeader describe the last snapshot this process wrote
	// or loaded. The store writer keeps its own copy; these ride along on
	// a snapshot so a save can append instead of rewriting.
	diskFinal  int    `json:"-"`
	diskHeader string `json:"-"`
	// SwitchNote is the one-time composer line after this chat's provider
	// was deleted: Switched to <default> — “<name>” was removed.
	// It is cleared by the × or by the next send in this chat.
	SwitchNote string `json:"-"`
}

// storeVersion is the schema written by this build. Older single-file
// stores (v2, v3, v4) still load and migrate. v5 is per-chat JSONL.
// v6 gives each provider its own model list and base URL.
// v7 lists only providers the user added. Untouched presets from a
// v6 file are removed once, after keys are available.
// v3 adds Appearance. v4 adds the theme preset and the app preferences.
// A missing theme is Warm.
const storeVersion = 7

// ProviderModel is one id in a provider's catalog. Enabled models are the
// ones that show up in the model menu. Fresh marks a model Refresh just
// found; those start switched off.
type ProviderModel struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	Fresh   bool   `json:"fresh,omitempty"`
}

// Connection is a provider profile chats share. The API key is not in this
// struct; it is stored under ID in the credential store.
// Models is this provider's list. ModelsSet is true once the user has saved
// that list, including an empty one. Until then the built-in catalog is
// shown and nothing is stored for the list.
// BaseURL is empty when the provider uses its official endpoint. The
// official address is never written here.
// Probe is the last local test: "" (never), "ok", or "invalid".
// Custom is a user-added OpenAI-compatible provider. Name is what the
// list shows; Provider is the built-in name, or "Custom".
type Connection struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Provider  string          `json:"provider"`
	BaseURL   string          `json:"baseUrl,omitempty"`
	Models    []ProviderModel `json:"models,omitempty"`
	ModelsSet bool            `json:"modelsSet,omitempty"`
	Custom    bool            `json:"custom,omitempty"`
	Probe     string          `json:"probe,omitempty"`
	// Format is how a custom provider speaks: "" or "openai" is
	// OpenAI-compatible, "anthropic" is Anthropic-compatible.
	Format string `json:"format,omitempty"`
}

// UnmarshalJSON accepts a schema 5 model list of strings, all enabled, and
// the schema 6 list of objects.
func (c *Connection) UnmarshalJSON(b []byte) error {
	var raw struct {
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Provider  string          `json:"provider"`
		BaseURL   string          `json:"baseUrl"`
		Models    json.RawMessage `json:"models"`
		ModelsSet bool            `json:"modelsSet"`
		Custom    bool            `json:"custom"`
		Probe     string          `json:"probe"`
		Format    string          `json:"format"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*c = Connection{
		ID: raw.ID, Name: raw.Name, Provider: raw.Provider, BaseURL: raw.BaseURL,
		ModelsSet: raw.ModelsSet, Custom: raw.Custom, Probe: raw.Probe, Format: raw.Format,
	}
	if len(raw.Models) == 0 || string(raw.Models) == "null" {
		return nil
	}
	var objs []ProviderModel
	if json.Unmarshal(raw.Models, &objs) == nil && (len(objs) == 0 || objs[0].ID != "") {
		c.Models = objs
		return nil
	}
	var ids []string
	if err := json.Unmarshal(raw.Models, &ids); err != nil {
		return err
	}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		c.Models = append(c.Models, ProviderModel{ID: id, Enabled: true})
	}
	if len(c.Models) > 0 {
		c.ModelsSet = true
	}
	return nil
}

// StoreData is the file written under Application Support.
type StoreData struct {
	Version       int            `json:"version"`
	Connections   []Connection   `json:"connections,omitempty"`
	Defaults      Config         `json:"defaults"`
	Conversations []Conversation `json:"conversations"`
	ActiveID      string         `json:"activeId"`
	// SidebarWidth is the user's sidebar width (cove.sidebar.width), saved
	// when a resize drag ends. 0 means the default, 240. A narrow window
	// may show less; the stored value comes back when the window widens.
	SidebarWidth float32 `json:"sidebarWidth,omitempty"`
	// Appearance is "system", "light", or "dark". Empty means system, which
	// is what a v2 file becomes.
	Appearance string `json:"appearance,omitempty"`
	// Theme is a preset id: warm, tide, neutral, mist, sage, or lavender.
	// Empty means Warm.
	Theme string `json:"theme,omitempty"`
	// SendWith is "" (Return sends) or "mod" (Command-Return sends).
	SendWith string `json:"sendWith,omitempty"`
	// ShowReasoning hides the thinking block while a reply is still
	// streaming. A finished reply still shows it. RestoreLast selects the
	// previous chat on launch. KeepLastRequest stores the debug body.
	ShowReasoning   bool `json:"showReasoning"`
	RestoreLast     bool `json:"restoreLast"`
	KeepLastRequest bool `json:"keepLastRequest"`
	// TimeoutSec is an optional overall deadline for one reply. Zero keeps
	// the streaming client, which has no overall timeout.
	TimeoutSec int `json:"timeoutSec,omitempty"`
	// MaxRounds is the limit copied onto a new chat. 0 is unlimited.
	// Chats already on disk keep their own value.
	MaxRounds int `json:"maxRounds,omitempty"`
	// pendingOptIn is set while loading a store from before providers
	// were opt-in. It is not written. adoptProviders clears it after
	// the one-time prune, once keys can be seen.
	pendingOptIn bool `json:"-"`
}

const (
	roleUser      = "user"
	roleAssistant = "assistant"
)

// API formats Cove already speaks. Gemini is not implemented. Empty and
// "openai" are the OpenAI-compatible list and chat calls. "anthropic"
// lists models with an Anthropic key; chat still uses the completions client.
const (
	formatOpenAI    = "openai"
	formatAnthropic = "anthropic"
)

// preset fills a provider's usual base URL and model. An empty base URL
// means the fields the user typed stay as they are. Catalog is the built-in
// model list; Model is the one switched on until the user saves a list.
// Format is the list-endpoint family copied onto each instance.
type preset struct {
	Name    string
	BaseURL string
	Model   string
	Catalog []string
	Format  string
}

var presets = []preset{
	{Name: "OpenAI", BaseURL: "https://api.openai.com/v1", Model: "gpt-4o-mini", Format: formatOpenAI, Catalog: []string{"gpt-4o", "gpt-4o-mini", "o3-mini", "gpt-4.1"}},
	{Name: "Anthropic", BaseURL: "https://api.anthropic.com/v1", Model: "claude-sonnet-4.5", Format: formatAnthropic, Catalog: []string{"claude-sonnet-4.5", "claude-opus-4", "claude-haiku-4.5"}},
	{Name: "DeepSeek", BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-flash", Format: formatOpenAI, Catalog: []string{"deepseek-chat", "deepseek-reasoner", "deepseek-flash"}},
	{Name: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1", Model: "openai/gpt-4o-mini", Format: formatOpenAI, Catalog: []string{"openai/gpt-4o-mini", "anthropic/claude-sonnet-4.5"}},
	{Name: "xAI", BaseURL: "https://api.x.ai/v1", Model: "grok-4", Format: formatOpenAI, Catalog: []string{"grok-4", "grok-4-fast"}},
	{Name: "Ollama", BaseURL: "http://localhost:11434/v1", Model: "llama3.2", Format: formatOpenAI, Catalog: []string{"llama3.2"}},
	{Name: "Custom", Format: formatOpenAI},
}

// DefaultConfig is what a fresh install offers. The key is empty until
// the user sets one.
func DefaultConfig() Config {
	return Config{
		Provider:     "OpenAI",
		SystemPrompt: "You are a helpful assistant. Be clear and concise.",
		Model:        "gpt-4o-mini",
		BaseURL:      "https://api.openai.com/v1",
	}
}

func f64p(v float64) *float64 { return &v }

func defaultData() StoreData {
	return StoreData{
		Version:         storeVersion,
		Defaults:        DefaultConfig(),
		Conversations:   []Conversation{},
		Appearance:      appearanceSystem,
		Theme:           themes.ThemeDefault,
		ShowReasoning:   true,
		RestoreLast:     true,
		KeepLastRequest: true,
	}
}

func providerNames() []string {
	names := make([]string, len(presets))
	for i, p := range presets {
		names[i] = p.Name
	}
	return names
}

func knownProvider(name string) bool {
	for _, p := range presets {
		if p.Name == name {
			return true
		}
	}
	return false
}

// applyPreset copies a provider's base URL and model onto cfg when the
// user picks one. Custom leaves whatever they typed.
func applyPreset(cfg *Config) {
	for _, p := range presets {
		if p.Name != cfg.Provider || p.BaseURL == "" {
			continue
		}
		cfg.BaseURL = p.BaseURL
		cfg.Model = p.Model
		return
	}
}

func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000")))
	}
	return hex.EncodeToString(b[:])
}

// draftTitle is the sidebar title of a chat that has text but no messages yet.
func draftTitle(draft string) string {
	line := draft
	if i := strings.IndexAny(draft, "\r\n"); i >= 0 {
		line = draft[:i]
	}
	return titleFrom(line)
}

func titleFrom(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return newChatTitle
	}
	const limit = 42
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	r := []rune(text)
	return string(r[:limit]) + "…"
}

// dateBucket groups a chat in the sidebar. now is the clock the view is
// using, so a test can pin it.
func dateBucket(t, now time.Time) string {
	if t.IsZero() {
		return "Older"
	}
	loc := now.Location()
	t = t.In(loc)
	now = now.In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	switch {
	case !t.Before(start):
		return "Today"
	case !t.Before(start.AddDate(0, 0, -1)):
		return "Yesterday"
	case !t.Before(start.AddDate(0, 0, -7)):
		return "Previous 7 Days"
	default:
		return "Older"
	}
}

func hostOf(base string) string {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Host == "" {
		return strings.TrimSpace(base)
	}
	return u.Host
}

// displayHost is the host drawn in the preset list and the title subtitle.
// Ollama's default and any saved 127.0.0.1:11434 address read localhost:11434.
func displayHost(base string) string {
	h := hostOf(base)
	if h == "127.0.0.1:11434" {
		return "localhost:11434"
	}
	return h
}

// allowsEmptyKey reports whether a local server can be called without a
// bearer token, as Ollama does.
func allowsEmptyKey(cfg Config) bool {
	if cfg.Provider == "Ollama" {
		return true
	}
	u, err := url.Parse(strings.TrimSpace(cfg.BaseURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0":
		return true
	default:
		return false
	}
}
