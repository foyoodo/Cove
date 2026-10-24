package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
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
	titleTokensReason = 256
)

// titleTimeout is how long a title request may run. A test may shorten
// it; the app leaves it at 15s. A deadline keeps the temporary title and
// shows nothing.
var titleTimeout = 15 * time.Second

// titleSystemPrompt asks for a title in the conversation's language.
const titleSystemPrompt = "Write a short title for this conversation. Use the same language as the conversation. Reply with the title only: no quotes, no trailing period, no emoji, and no line breaks. Keep it within 30 characters (about 15 Chinese characters)."

// titleComplete is the non-streaming call a title request uses.
// Tests replace it so a title never hits the network.
var titleComplete = completeChat

// openAppHook and framePaintHook are nil in the app. Tests set them so
// a finished reply does not also post a title, and so a headless shot
// can draw a menu the framebuffer does not paint.
var (
	openAppHook    func(*App)
	framePaintHook func(*App, *ui.Context)
)

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

// protectLegacyTitle marks a chat saved before titleSource existed.
// Those files have a name the user already accepted. An empty source
// plus any message means the one automatic attempt is already done, so
// a later regenerate of that reply cannot overwrite the name.
func protectLegacyTitle(conv *Conversation) {
	if conv == nil || conv.TitleSource != "" || conv.TitleGenerated {
		return
	}
	if len(conv.Messages) == 0 && conv.MessageCount == 0 {
		return
	}
	conv.TitleGenerated = true
}

// markAutoTitle records the one automatic attempt. The caller saves that
// flag with the reply, then starts the request. Only a temporary title
// is eligible: a manual name, and a legacy chat with no source, stay.
func (a *App) markAutoTitle(convID string) bool {
	if !a.data.AutoTitles {
		return false
	}
	conv := a.conversation(convID)
	if conv == nil || conv.TitleGenerated || conv.TitleSource != titleSourceTemp {
		return false
	}
	if !isFirstReply(conv) {
		return false
	}
	conv.TitleGenerated = true
	return true
}

// maybeAutoTitle is the direct path tests use. A live reply goes through
// markAutoTitle and the save it already performs.
func (a *App) maybeAutoTitle(convID string) {
	if !a.markAutoTitle(convID) {
		return
	}
	a.saveSync()
	if conv := a.conversation(convID); conv != nil {
		a.startTitle(conv, false, false)
	}
}

// regenerateTitle asks for a new title for id. dimBar greys the title
// bar along with the sidebar row. That is the More menu, and a sidebar
// menu on the chat already on screen. A sidebar menu on another chat
// leaves the title bar alone and does not switch to that chat.
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
	return a.chatHasModel(conv.Config)
}

// chatHasModel is the same condition as the composer chip reading
// "No model": the connection is gone, there is no provider, or the chat
// has no model id. It does not compare that display string.
func (a *App) chatHasModel(cfg Config) bool {
	if id := strings.TrimSpace(cfg.ConnectionID); id != "" && a.connByID(id) == nil {
		return false
	}
	if len(a.data.Connections) == 0 {
		return false
	}
	model := strings.TrimSpace(cfg.Model)
	return model != "" && shortModel(model) != "Model"
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
	cfg, usingChat, ok := a.titleConfig(conv, false)
	turns := titleTurns(conv, manual)
	body, err := []byte(nil), error(nil)
	omitReasoning := usingChat && normalizeEffort(conv.Config.Reasoning) == ""
	if ok {
		body, err = titleRequestBody(cfg, turns, omitReasoning)
		ok = err == nil && len(turns) > 0
	}
	if !ok {
		if manual {
			a.noteTitleFail(id)
			a.announce = titleFailText
		}
		return
	}
	needKey := titleNeedsKey(cfg)
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
	// The credential read and the HTTP call stay off the UI thread when a
	// window is open. Tests stay synchronous unless they opt into titleAsync.
	run := func() {
		if needKey {
			var val string
			var keyErr error
			if a.keys == nil {
				keyErr = errNoTitleKey
			} else {
				val, keyErr = a.keys.Get(cfg.ConnectionID)
				if keyErr == nil && strings.TrimSpace(val) == "" {
					keyErr = errNoTitleKey
				}
			}
			if keyErr != nil {
				a.ui(func() {
					a.finishTitle(id, gen, manual, "", keyErr)
				})
				cancel()
				return
			}
			cfg.APIKey = val
		}
		text, callErr := completeTitle(ctx, cfg, body)
		cancel()
		a.ui(func() {
			a.finishTitle(id, gen, manual, text, callErr)
		})
	}
	if a.win != nil || a.titleAsync {
		go run()
		// A title that starts while the sidebar row already has the
		// keyboard is announced here. A later focus move announces again
		// from noteSettledFocus. Synchronous tests finish in run and
		// speak the result instead.
		if manual && a.settled.kind == focusSidebar && a.settled.id == id {
			a.sayFocus(focusSidebar, id)
		}
		return
	}
	run()
}

func titleNeedsKey(cfg Config) bool {
	if strings.TrimSpace(cfg.APIKey) != "" || strings.TrimSpace(cfg.ConnectionID) == "" {
		return false
	}
	return !allowsEmptyKey(cfg)
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
func (a *App) titleConfig(conv *Conversation, readKey bool) (cfg Config, usingChat, ok bool) {
	if conv == nil {
		return Config{}, false, false
	}
	if ref := a.data.TitleModel; ref != nil && a.titleModelAvailable(ref) {
		if picked, ready := a.configForTitleModel(ref, readKey); ready {
			return picked, false, true
		}
	}
	cfg, ok = a.chatTitleConfig(conv, readKey)
	return cfg, true, ok
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

// titleRequestBody is a non-streaming chat completion. The output cap is
// 256 for every model: some providers count reasoning toward the cap, and
// the client still cuts the title to 30 display-width units. omitReasoning
// is set when this is the chat's own model and that chat's reasoning is
// Default, which sends no reasoning fields. Otherwise the body carries the
// lowest level that model family accepts.
func titleRequestBody(cfg Config, turns []apiMessage, omitReasoning bool) ([]byte, error) {
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
	if cfg.Provider == "OpenAI" {
		payload["max_completion_tokens"] = titleTokensReason
	} else {
		payload["max_tokens"] = titleTokensReason
	}
	if !omitReasoning {
		for k, v := range titleReasoningParams(cfg) {
			payload[k] = v
		}
	}
	return json.Marshal(payload)
}

// titleReasoningParams is the lowest reasoning a title request may send.
// Nil means the body stays a plain chat completion: the model rejects the
// parameter (gpt-4o, grok-4, grok-3-mini) or the provider has no encoding.
func titleReasoningParams(cfg Config) map[string]any {
	leaf := modelLeaf(cfg.Model)
	switch reasoningStyle(cfg) {
	case "openai", "xai":
		if reasoningUnsupported(cfg) || grokSkipsReasoning(leaf) {
			return nil
		}
		effort, ok := openAITitleEffort(leaf)
		if !ok {
			return nil
		}
		return map[string]any{"reasoning_effort": effort}
	case "openrouter":
		return map[string]any{"reasoning": map[string]any{"effort": "none", "enabled": false}}
	case "deepseek":
		return map[string]any{"thinking": map[string]any{"type": "disabled"}}
	case "ollama":
		return map[string]any{"reasoning_effort": "none", "think": false}
	default:
		return nil
	}
}

func modelLeaf(model string) string {
	id := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(id, "/"); i >= 0 && i < len(id)-1 {
		id = id[i+1:]
	}
	return id
}

// openAITitleEffort is the lowest reasoning_effort the family accepts.
// o-series has no "none". gpt-5 before 5.1 has no "none" and uses minimal.
// gpt-5.1 and newer accept none.
func openAITitleEffort(leaf string) (string, bool) {
	switch {
	case strings.HasPrefix(leaf, "o1"), strings.HasPrefix(leaf, "o3"), strings.HasPrefix(leaf, "o4"):
		return effortLow, true
	case strings.HasPrefix(leaf, "gpt-5."):
		return "none", true
	case strings.HasPrefix(leaf, "gpt-5"):
		return effortMinimal, true
	case strings.HasPrefix(leaf, "gpt-4"), strings.HasPrefix(leaf, "gpt-3.5"), strings.HasPrefix(leaf, "chatgpt-"):
		return "", false
	case grokSkipsReasoning(leaf):
		return "", false
	default:
		return "none", true
	}
}

// grokSkipsReasoning reports Grok models that reject reasoning_effort.
// grok-4.5 and newer are a different family and are not matched here.
func grokSkipsReasoning(leaf string) bool {
	if strings.HasPrefix(leaf, "grok-3-mini") {
		return true
	}
	return strings.HasPrefix(leaf, "grok-4") && !strings.HasPrefix(leaf, "grok-4.")
}

var errNoTitleKey = errTitle("no title key")

// completeTitle posts the body. A 400 is tried once more with every
// reasoning field removed: a relay that rejects an unknown parameter
// still answers a plain chat completion.
func completeTitle(ctx context.Context, cfg Config, body []byte) (string, error) {
	text, err := titleComplete(ctx, cfg, body)
	if statusOf(err) != 400 {
		return text, err
	}
	plain, ok := stripReasoningJSON(body)
	if !ok {
		return text, err
	}
	return titleComplete(ctx, cfg, plain)
}

func statusOf(err error) int {
	var se *StatusError
	if errors.As(err, &se) && se != nil {
		return se.Status
	}
	return 0
}

func stripReasoningJSON(body []byte) ([]byte, bool) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false
	}
	changed := false
	for _, key := range []string{"reasoning_effort", "reasoning", "thinking", "think"} {
		if _, ok := payload[key]; ok {
			delete(payload, key)
			changed = true
		}
	}
	if !changed {
		return nil, false
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, false
	}
	return out, true
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

const titlePunct = ".。．!！?？…:：,，"

// sanitizeTitle cleans a model title. False means the text was empty
// after cleanup and the previous title should stay. Quotes are removed
// only from the ends, so an apostrophe inside a word stays.
func sanitizeTitle(s string) (string, bool) {
	var ok bool
	s, ok = stripThinkBlocks(s)
	if !ok {
		return "", false
	}
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	var b strings.Builder
	for _, r := range s {
		if isEmojiRune(r) {
			continue
		}
		b.WriteRune(r)
	}
	s = trimTitleEdges(strings.Join(strings.Fields(b.String()), " "))
	if s == "" {
		return "", false
	}
	s = trimTitleEdges(truncateTitleWidth(s, titleWidthMax))
	if s == "" {
		return "", false
	}
	return s, true
}

// stripThinkBlocks drops closed reasoning wrappers such as <think>…</think>.
// An opening tag with no closer is a failure: the model did not return a title.
func stripThinkBlocks(s string) (string, bool) {
	tags := []string{"think", "thinking", "reasoning"}
	for {
		lower := strings.ToLower(s)
		openAt := -1
		tag := ""
		for _, name := range tags {
			i := strings.Index(lower, "<"+name+">")
			if i >= 0 && (openAt < 0 || i < openAt) {
				openAt = i
				tag = name
			}
		}
		if openAt < 0 {
			return s, true
		}
		closer := "</" + tag + ">"
		rel := strings.Index(lower[openAt:], closer)
		if rel < 0 {
			return "", false
		}
		s = s[:openAt] + s[openAt+rel+len(closer):]
	}
}

func trimTitleEdges(s string) string {
	for {
		next := strings.TrimSpace(s)
		next = trimQuoteEdges(next)
		next = strings.TrimRight(next, titlePunct)
		next = strings.TrimSpace(next)
		if next == s {
			return s
		}
		s = next
	}
}

func trimQuoteEdges(s string) string {
	r := []rune(s)
	i, j := 0, len(r)
	for i < j && isQuoteRune(r[i]) {
		i++
	}
	for j > i && isQuoteRune(r[j-1]) {
		j--
	}
	return string(r[i:j])
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
	// Joiners and variation selectors are stripped even when they are left
	// behind after the pictograph they were attached to.
	if r == 0x200D || r == 0xFE0E || r == 0xFE0F || r == 0x20E3 {
		return true
	}
	if r == 0x00A9 || r == 0x00AE { // © ®
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
	// U+2300 is Miscellaneous Technical: key marks such as ⌘ ⌥ ⌃ stay,
	// and only the emoji in that block are listed in isTechnicalEmoji.
	if isTechnicalEmoji(r) {
		return true
	}
	switch {
	case r >= 0x25A0 && r <= 0x25FF:
		return true
	case r >= 0x2600 && r <= 0x27BF:
		return true
	case r >= 0x2B00 && r <= 0x2BFF: // ⬆ ⭐
		return true
	case r >= 0x1F300 && r <= 0x1FAFF:
		return true
	case r >= 0xE0020 && r <= 0xE007F:
		return true
	default:
		return false
	}
}

// isTechnicalEmoji reports the pictographs in U+2300–U+23FF. The rest
// of that block is technical signs (⌘ ⌥ ⌃ ⌫ and the like) and stays in
// a title.
func isTechnicalEmoji(r rune) bool {
	switch r {
	case 0x231A, 0x231B, 0x23CF: // ⌚ ⌛ ⏏
		return true
	}
	if r >= 0x23E9 && r <= 0x23F3 || r >= 0x23F8 && r <= 0x23FA {
		return true
	}
	return false
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
