package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/width"
)

const (
	titleSourceTemp   = "temp"
	titleSourceAuto   = "auto"
	titleSourceManual = "manual"

	titleSameAsChat = "Same as Chat"
	titleModelSame  = "\x1esame"

	titleToggleLabel     = "Automatically generate chat titles"
	titleToggleHint      = "Sends the first message and reply to the title model."
	titleModelLabel      = "Title model"
	titleModelHint       = "Same as Chat uses each chat’s own model."
	titleUnavailable     = "Unavailable"
	titleUnavailableHint = "Unavailable — titles use each chat’s model."
	titleFailText        = "Couldn’t generate title"
	titleRegenLabel      = "Regenerate Title"
	titleGenerating      = "Generating title"

	titleStoredMax    = 80
	titleWidthMax     = 30
	titleReplyRunes   = 1000
	titleManualRounds = 3
	titleTokensNormal = 32
	titleTokensReason = 256
	titleTimeout      = 15 * time.Second
)

// titleSystemPrompt asks for a title in the conversation's language.
const titleSystemPrompt = "Write a short title for this conversation. Use the same language as the conversation. Reply with the title only: no quotes, no trailing period, no emoji, and no line breaks. Keep it within 30 characters (about 15 Chinese characters)."

// titleComplete is the non-streaming call a title request uses.
// Tests replace it so a title never hits the network.
var titleComplete = completeChat

// silenceAutoTitles is set by TestMain. Ordinary tests post a chat
// completion and then read that request; a title request on the same
// server would replace it. Production leaves this false, so a new
// install still generates titles. Title tests turn AutoTitles back on.
var silenceAutoTitles bool

// titleJob is one in-flight title request. gen drops a callback that
// arrives after the job was cancelled.
type titleJob struct {
	gen    int
	manual bool
	bar    bool
	cancel context.CancelFunc
}

// titleFade is a cross-fade from an old title to the one now stored.
// row and bar say which surfaces still have to play it. An off-screen
// row is left false and swaps immediately.
type titleFade struct {
	from  string
	start time.Time
	row   bool
	bar   bool
}

// previewMenu is a test-only drawing of the sidebar's native menu.
// The headless renderer records that menu but does not paint it.
type previewMenu struct {
	X, Y  float32
	Items []previewItem
}

type previewItem struct {
	Label    string
	Disabled bool
	Hot      bool
	Sep      bool
}

func (a *App) maybeAutoTitle(convID string) {
	if !a.data.AutoTitles {
		return
	}
	conv := a.conversation(convID)
	if conv == nil || conv.TitleGenerated || conv.TitleSource == titleSourceManual {
		return
	}
	if !isFirstReply(conv) {
		return
	}
	// Record the attempt before the request leaves. A quit while it is
	// in flight does not try again on the next launch.
	conv.TitleGenerated = true
	a.saveSync()
	a.startTitle(conv, false, false)
}

// regenerateTitle asks for a new title for id. dimBar is set for the
// More menu, which is already showing that chat. A sidebar menu passes
// false: only that row changes, and the current chat stays put.
func (a *App) regenerateTitle(id string, dimBar bool) {
	a.ensureChat(id)
	conv := a.conversation(id)
	if !a.canRegenerateTitle(conv) {
		return
	}
	a.startTitle(conv, true, dimBar)
}

func (a *App) canRegenerateTitle(conv *Conversation) bool {
	if conv == nil || a.isStreaming(conv.ID) || a.titleBusy(conv.ID) {
		return false
	}
	if !hasUserMessage(conv) {
		return false
	}
	_, model := a.composerModelParts(conv.Config)
	return model != "No model"
}

func (a *App) titleBusy(id string) bool {
	_, ok := a.titleJobs[id]
	return ok
}

func (a *App) titleDimmed(id string, bar bool) bool {
	job := a.titleJobs[id]
	if job == nil || !job.manual {
		return false
	}
	if bar {
		return job.bar
	}
	return true
}

func (a *App) startTitle(conv *Conversation, manual, dimBar bool) {
	if conv == nil {
		return
	}
	id := conv.ID
	if a.titleBusy(id) {
		return
	}
	cfg, ok := a.titleConfig(conv, true)
	turns := titleTurns(conv, manual)
	body, err := []byte(nil), error(nil)
	if ok {
		body, err = titleRequestBody(cfg, turns)
		ok = err == nil && len(turns) > 0
	}
	if !ok {
		if manual {
			a.noteTitleFail(id)
			a.announce = titleFailText
		}
		return
	}
	if a.titleGens == nil {
		a.titleGens = map[string]int{}
	}
	a.titleGens[id]++
	gen := a.titleGens[id]
	ctx, cancel := context.WithTimeout(context.Background(), titleTimeout)
	if a.titleJobs == nil {
		a.titleJobs = map[string]*titleJob{}
	}
	a.titleJobs[id] = &titleJob{gen: gen, manual: manual, bar: dimBar, cancel: cancel}
	a.launch(func() {
		text, err := titleComplete(ctx, cfg, body)
		cancel()
		a.ui(func() {
			a.finishTitle(id, gen, manual, text, err)
		})
	})
}

func (a *App) finishTitle(id string, gen int, manual bool, text string, err error) {
	job := a.titleJobs[id]
	if job == nil || job.gen != gen {
		return
	}
	delete(a.titleJobs, id)
	conv := a.conversation(id)
	if conv == nil {
		return
	}
	if a.renameOpen && a.data.ActiveID == id {
		return
	}
	if !manual && conv.TitleSource == titleSourceManual {
		return
	}
	if err != nil {
		if manual {
			a.noteTitleFail(id)
			a.announce = titleFailText
		}
		return
	}
	clean, ok := sanitizeTitle(text)
	if !ok {
		if manual {
			a.noteTitleFail(id)
			a.announce = titleFailText
		}
		return
	}
	from := conv.Title
	if clean == from && conv.TitleSource == titleSourceAuto {
		return
	}
	conv.Title = clean
	conv.TitleSource = titleSourceAuto
	a.save()
	a.beginTitleFade(id, from)
	if manual && clean != from {
		a.announce = "Title changed to " + clean
	}
	a.syncWindowTitle()
}

func (a *App) beginTitleFade(id, from string) {
	if a.instant || a.reduceMotion || strings.TrimSpace(from) == "" {
		return
	}
	conv := a.conversation(id)
	if conv == nil || conv.Title == from {
		return
	}
	row := a.sideRowOnScreen(id)
	bar := id == a.data.ActiveID
	if !row && !bar {
		return
	}
	if a.titleFades == nil {
		a.titleFades = map[string]titleFade{}
	}
	a.titleFades[id] = titleFade{from: from, start: time.Now(), row: row, bar: bar}
}

func (a *App) sideRowOnScreen(id string) bool {
	first, last := a.side.Visible()
	if last < first {
		return true
	}
	for i, row := range a.sideRows {
		if row.header != "" || row.index < 0 || row.index >= len(a.data.Conversations) {
			continue
		}
		if a.data.Conversations[row.index].ID != id {
			continue
		}
		return i >= first && i <= last
	}
	return false
}

func (a *App) noteTitleFail(id string) {
	if a.titleFails == nil {
		a.titleFails = map[string]time.Time{}
	}
	a.titleFails[id] = time.Now()
}

func (a *App) cancelTitle(id string, autoOnly bool) {
	job := a.titleJobs[id]
	if job == nil || (autoOnly && job.manual) {
		return
	}
	if job.cancel != nil {
		job.cancel()
	}
	if a.titleGens == nil {
		a.titleGens = map[string]int{}
	}
	a.titleGens[id]++
	delete(a.titleJobs, id)
}

func (a *App) cancelAutoTitles() {
	for id := range a.titleJobs {
		a.cancelTitle(id, true)
	}
}

func (a *App) cancelAllTitles() {
	for id := range a.titleJobs {
		a.cancelTitle(id, false)
	}
}

func (a *App) dropTitleState(id string) {
	a.cancelTitle(id, false)
	delete(a.titleFades, id)
	delete(a.titleFails, id)
}

// titleConfig is the model a title request will call. A saved title model
// that is still available wins. Otherwise the chat's own model is used.
// The saved choice is left as it is either way. readKey fills the secret;
// painting passes false so a frame does not touch the credential store.
func (a *App) titleConfig(conv *Conversation, readKey bool) (Config, bool) {
	if conv == nil {
		return Config{}, false
	}
	if ref := a.data.TitleModel; ref != nil && a.titleModelAvailable(ref) {
		if cfg, ok := a.configForTitleModel(ref, readKey); ok {
			return cfg, true
		}
	}
	return a.chatTitleConfig(conv, readKey)
}

func (a *App) titleModelAvailable(ref *TitleModel) bool {
	if ref == nil || strings.TrimSpace(ref.ModelID) == "" || strings.TrimSpace(ref.ProviderID) == "" {
		return false
	}
	c := a.connByID(ref.ProviderID)
	if c == nil || !a.modelOnConnection(ref.ProviderID, ref.ModelID) {
		return false
	}
	keyless := !c.Custom && keylessProvider(c.Provider)
	if !a.providerReady(c, keyless) {
		return false
	}
	if !keyless && a.keyBlocked(c.ID) {
		return false
	}
	return true
}

func (a *App) configForTitleModel(ref *TitleModel, readKey bool) (Config, bool) {
	c := a.connByID(ref.ProviderID)
	if c == nil {
		return Config{}, false
	}
	cfg := Config{
		Provider:     c.Provider,
		Model:        ref.ModelID,
		BaseURL:      connEffective(c),
		ConnectionID: c.ID,
	}
	if c.Custom {
		cfg.Provider = "Custom"
		if strings.TrimSpace(c.Name) != "" {
			cfg.Provider = c.Name
		}
	}
	cfg.ReasoningStyle = c.Format
	if !readKey {
		return cfg, true
	}
	keyless := !c.Custom && keylessProvider(c.Provider)
	cfg.APIKey = a.resolvedKey(cfg)
	if !keyless && strings.TrimSpace(cfg.APIKey) == "" {
		return Config{}, false
	}
	return cfg, true
}

func (a *App) chatTitleConfig(conv *Conversation, readKey bool) (Config, bool) {
	if _, model := a.composerModelParts(conv.Config); model == "No model" {
		return Config{}, false
	}
	cfg := conv.Config
	cfg.ExtraBody = ""
	cfg.Temperature = nil
	cfg.SystemPrompt = ""
	if c := a.connByID(cfg.ConnectionID); c != nil {
		if strings.TrimSpace(cfg.BaseURL) == "" {
			cfg.BaseURL = connEffective(c)
		}
		keyless := !c.Custom && keylessProvider(c.Provider)
		if !a.providerReady(c, keyless) || (!keyless && a.keyBlocked(c.ID)) {
			return Config{}, false
		}
		if readKey {
			cfg.APIKey = a.resolvedKey(cfg)
			if !keyless && strings.TrimSpace(cfg.APIKey) == "" {
				return Config{}, false
			}
		}
		return cfg, true
	}
	if strings.TrimSpace(cfg.Model) == "" || strings.TrimSpace(cfg.BaseURL) == "" {
		return Config{}, false
	}
	if len(a.data.Connections) == 0 {
		return Config{}, false
	}
	if readKey && strings.TrimSpace(cfg.APIKey) == "" && !allowsEmptyKey(cfg) {
		cfg.APIKey = a.resolvedKey(cfg)
		if strings.TrimSpace(cfg.APIKey) == "" {
			return Config{}, false
		}
	}
	return cfg, true
}

func isFirstReply(conv *Conversation) bool {
	if conv == nil {
		return false
	}
	n := 0
	for _, m := range conv.Messages {
		if m.Role == roleAssistant && m.Done {
			n++
		}
	}
	return n == 1
}

func hasUserMessage(conv *Conversation) bool {
	if conv == nil {
		return false
	}
	if conv.Messages == nil && !conv.Loaded {
		return conv.MessageCount > 0
	}
	for _, m := range conv.Messages {
		if m.Role != roleUser {
			continue
		}
		if strings.TrimSpace(m.Content) != "" || len(m.Quotes) > 0 {
			return true
		}
	}
	return false
}

func titleTurns(conv *Conversation, manual bool) []apiMessage {
	if conv == nil {
		return nil
	}
	if manual {
		return titleSessionTurns(conv)
	}
	var user, reply string
	sawUser := false
	replySkipped := false
	for _, m := range conv.Messages {
		if m.Role == roleUser && !sawUser {
			user = quoteRequestText(m.Quotes, m.Content)
			sawUser = true
			continue
		}
		if m.Role != roleAssistant || !sawUser || reply != "" || replySkipped {
			continue
		}
		if m.Incomplete || strings.TrimSpace(m.Error) != "" || !m.Done {
			replySkipped = true
			continue
		}
		reply = clipRunes(m.Content, titleReplyRunes)
	}
	return titlePair(user, reply)
}

func titleSessionTurns(conv *Conversation) []apiMessage {
	var out []apiMessage
	rounds := 0
	for _, m := range sessionMessages(conv) {
		switch m.Role {
		case roleUser:
			if rounds >= titleManualRounds {
				return out
			}
			text := strings.TrimSpace(quoteRequestText(m.Quotes, m.Content))
			if text == "" {
				continue
			}
			out = append(out, apiMessage{Role: roleUser, Content: text})
			rounds++
		case roleAssistant:
			if rounds == 0 || m.Incomplete || strings.TrimSpace(m.Error) != "" || !m.Done {
				continue
			}
			text := strings.TrimSpace(clipRunes(m.Content, titleReplyRunes))
			if text == "" {
				continue
			}
			out = append(out, apiMessage{Role: roleAssistant, Content: text})
		}
	}
	return out
}

func titlePair(user, reply string) []apiMessage {
	var out []apiMessage
	if strings.TrimSpace(user) != "" {
		out = append(out, apiMessage{Role: roleUser, Content: user})
	}
	if strings.TrimSpace(reply) != "" {
		out = append(out, apiMessage{Role: roleAssistant, Content: reply})
	}
	return out
}

func clipRunes(s string, n int) string {
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

// titleOutputTokens is 256 for a reasoning model, where the provider may
// count the reasoning itself toward the cap, and 32 otherwise.
func titleOutputTokens(model string) int {
	if titleReasoningModel(model) {
		return titleTokensReason
	}
	return titleTokensNormal
}

func titleReasoningModel(model string) bool {
	id := strings.ToLower(strings.TrimSpace(model))
	leaf := id
	if i := strings.LastIndex(id, "/"); i >= 0 {
		leaf = id[i+1:]
	}
	if strings.Contains(id, "reason") || strings.Contains(id, "thinking") {
		return true
	}
	for _, s := range []string{id, leaf} {
		switch {
		case strings.HasPrefix(s, "o1"), strings.HasPrefix(s, "o3"), strings.HasPrefix(s, "o4"), strings.HasPrefix(s, "gpt-5"):
			return true
		}
	}
	return false
}

// titleRequestBody is a non-streaming chat completion with reasoning at
// its lowest setting and a short output cap. The chat's own temperature
// and extra body are not copied.
func titleRequestBody(cfg Config, turns []apiMessage) ([]byte, error) {
	if len(turns) == 0 {
		return nil, errNoTitleInput
	}
	msgs := make([]apiMessage, 0, len(turns)+1)
	msgs = append(msgs, apiMessage{Role: "system", Content: titleSystemPrompt})
	msgs = append(msgs, turns...)
	payload := map[string]any{
		"model":    strings.TrimSpace(cfg.Model),
		"messages": msgs,
		"stream":   false,
	}
	cap := titleOutputTokens(cfg.Model)
	if cfg.Provider == "OpenAI" {
		payload["max_completion_tokens"] = cap
	} else {
		payload["max_tokens"] = cap
	}
	off := cfg
	off.Reasoning = effortOff
	off.ExtraBody = ""
	for k, v := range reasoningParams(off) {
		payload[k] = v
	}
	return json.Marshal(payload)
}

var errNoTitleInput = errTitle("no title input")

type errTitle string

func (e errTitle) Error() string { return string(e) }

// temporaryTitle is the sidebar title stored when the first message is
// sent: the first content line, at most 80 runes, with an ellipsis when
// it was cut. An empty line stays "New Chat".
func temporaryTitle(text string) string {
	line := strings.Join(strings.Fields(firstContentLine(text)), " ")
	if line == "" {
		return newChatTitle
	}
	r := []rune(line)
	if len(r) <= titleStoredMax {
		return line
	}
	return string(r[:titleStoredMax-1]) + "…"
}

func firstContentLine(text string) string {
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || trim == "" {
			continue
		}
		return stripMarkdownMarks(trim)
	}
	return ""
}

func stripMarkdownMarks(s string) string {
	for {
		s = strings.TrimSpace(s)
		switch {
		case strings.HasPrefix(s, "#"):
			s = strings.TrimLeft(s, "#")
			continue
		case strings.HasPrefix(s, ">"):
			s = strings.TrimPrefix(s, ">")
			continue
		case strings.HasPrefix(s, "- "), strings.HasPrefix(s, "* "):
			s = s[2:]
			continue
		default:
			if rest, ok := stripNumberMark(s); ok {
				s = rest
				continue
			}
			return strings.TrimSpace(s)
		}
	}
}

func stripNumberMark(s string) (string, bool) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(s) || s[i] != '.' {
		return s, false
	}
	return strings.TrimSpace(s[i+1:]), true
}

// sanitizeTitle cleans a model title. False means the text was empty
// after cleanup and the previous title should stay.
func sanitizeTitle(s string) (string, bool) {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	var b strings.Builder
	for _, r := range s {
		if isQuoteRune(r) || isEmojiRune(r) {
			continue
		}
		b.WriteRune(r)
	}
	s = strings.Join(strings.Fields(b.String()), " ")
	s = strings.TrimRight(s, ".。．!！?？…")
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	s = truncateTitleWidth(s, titleWidthMax)
	if strings.TrimSpace(s) == "" {
		return "", false
	}
	return s, true
}

func isQuoteRune(r rune) bool {
	switch r {
	case '"', '\'', '`',
		'\u2018', '\u2019', '\u201A', '\u201B',
		'\u201C', '\u201D', '\u201E', '\u201F',
		'\u00AB', '\u00BB', '\u2039', '\u203A',
		'\u300C', '\u300D', '\u300E', '\u300F',
		'\u301D', '\u301E', '\u301F',
		'\uFF02', '\uFF07':
		return true
	default:
		return false
	}
}

func isEmojiRune(r rune) bool {
	if r == 0x200D || r == 0xFE0E || r == 0xFE0F || r == 0x20E3 {
		return true
	}
	if r >= 0x1F3FB && r <= 0x1F3FF {
		return true
	}
	if r >= 0x1F1E6 && r <= 0x1F1FF {
		return true
	}
	// Emoji live in these blocks. Go's unicode package here has no
	// Extended_Pictographic table, so the blocks stand in for it.
	switch {
	case r >= 0x1F300 && r <= 0x1FAFF:
		return true
	case r >= 0x2600 && r <= 0x27BF:
		return true
	case r >= 0xE0020 && r <= 0xE007F:
		return true
	default:
		return false
	}
}

func runeWidth(r rune) int {
	if r == 0 || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) {
		return 0
	}
	switch width.LookupRune(r).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 2
	default:
		return 1
	}
}

func displayWidth(s string) int {
	n := 0
	for _, r := range s {
		n += runeWidth(r)
	}
	return n
}

func truncateTitleWidth(s string, max int) string {
	if displayWidth(s) <= max {
		return s
	}
	if strings.Contains(s, " ") {
		words := strings.Fields(s)
		var kept string
		for _, w := range words {
			next := w
			if kept != "" {
				next = kept + " " + w
			}
			if displayWidth(next) > max {
				break
			}
			kept = next
		}
		if kept != "" {
			return kept
		}
		return cutWidth(words[0], max)
	}
	return cutWidth(s, max)
}

func cutWidth(s string, max int) string {
	n := 0
	var b strings.Builder
	for _, r := range s {
		w := runeWidth(r)
		if n+w > max {
			break
		}
		b.WriteRune(r)
		n += w
	}
	return b.String()
}
