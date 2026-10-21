package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// A round is one user message plus the assistant reply that follows it.
// The system prompt is not a round. maxRounds counts completed
// rounds in the current session only: messages after the last break.

// LimitRoundsInitial is the number the UI shows the first time the user
// picks "Limit to". It is not written until they set a value.
const LimitRoundsInitial = 20

func clampRounds(n int) int {
	if n < 0 {
		return 0
	}
	if n > 100 {
		return 100
	}
	return n
}

// sessionStart is the index of the first message that still belongs in
// the next request. It is the position of the last break.
func sessionStart(c *Conversation) int {
	if c == nil || len(c.Breaks) == 0 {
		return 0
	}
	start := c.Breaks[len(c.Breaks)-1].After
	if start < 0 {
		return 0
	}
	if start > len(c.Messages) {
		return len(c.Messages)
	}
	return start
}

func sessionMessages(c *Conversation) []Message {
	if c == nil {
		return nil
	}
	return c.Messages[sessionStart(c):]
}

// completedRounds counts user messages in msgs that already have a finished
// assistant reply before the next user message.
func completedRounds(msgs []Message) int {
	n := 0
	for i := 0; i < len(msgs); i++ {
		if msgs[i].Role != roleUser {
			continue
		}
		for j := i + 1; j < len(msgs); j++ {
			if msgs[j].Role == roleUser {
				break
			}
			if msgs[j].Role == roleAssistant && messageKept(msgs[j]) {
				n++
				break
			}
		}
	}
	return n
}

func dropBreaksAfter(c *Conversation, msgIndex int) {
	if c == nil || len(c.Breaks) == 0 {
		return
	}
	kept := make([]SessionBreak, 0, len(c.Breaks))
	for _, b := range c.Breaks {
		if b.After <= msgIndex {
			kept = append(kept, b)
		}
	}
	if len(kept) == 0 {
		c.Breaks = nil
		return
	}
	c.Breaks = kept
}

func (a *App) loadedActive() *Conversation {
	conv := a.active()
	if conv == nil {
		return nil
	}
	a.ensureChat(conv.ID)
	return a.conversation(conv.ID)
}

// CanInsertBreak reports whether a manual break would cut a session that
// still has messages. A second break with nothing after the previous one
// is refused.
func (a *App) CanInsertBreak() bool {
	conv := a.loadedActive()
	if conv == nil || a.isStreaming(conv.ID) {
		return false
	}
	return len(sessionMessages(conv)) > 0
}

// InsertSessionBreak appends a manual break after the current messages.
// It is a no-op when the current session is already empty, and it returns
// whether a break was written.
func (a *App) InsertSessionBreak() bool {
	if !a.CanInsertBreak() {
		return false
	}
	conv := a.loadedActive()
	if conv == nil {
		return false
	}
	conv.Breaks = append(conv.Breaks, SessionBreak{
		ID: newID(), Kind: sessionManual, CreatedAt: time.Now(), After: len(conv.Messages),
	})
	a.touch(conv)
	a.save()
	return true
}

// DeleteTrailingBreak removes a break that has no messages after it.
// A break that already has a later turn stays.
func (a *App) DeleteTrailingBreak() bool {
	conv := a.loadedActive()
	if conv == nil || len(conv.Breaks) == 0 || a.isStreaming(conv.ID) {
		return false
	}
	last := len(conv.Breaks) - 1
	if conv.Breaks[last].After != len(conv.Messages) {
		return false
	}
	conv.Breaks = conv.Breaks[:last]
	if len(conv.Breaks) == 0 {
		conv.Breaks = nil
	}
	a.touch(conv)
	a.save()
	return true
}

// SetMaxContextRounds stores this chat's limit under the header field
// maxRounds. 0 is unlimited and is omitted from the file. Values outside
// 0–100 are clamped. The global default for new chats is separate.
func (a *App) SetMaxContextRounds(n int) {
	conv := a.loadedActive()
	if conv == nil {
		return
	}
	n = clampRounds(n)
	if conv.MaxRounds == n {
		return
	}
	conv.MaxRounds = n
	a.touch(conv)
	a.save()
}

// SetDefaultMaxContextRounds is the General › New chats limit, stored as
// maxRounds in settings.json. Chats already stored keep the value they
// were created with. Picking "Limit to" does not call this; the UI's
// first shown number is LimitRoundsInitial, and it stays unstored until set.
func (a *App) SetDefaultMaxContextRounds(n int) {
	n = clampRounds(n)
	if a.data.MaxRounds == n {
		return
	}
	a.data.MaxRounds = n
	a.save()
}

// maybeAutoBreak inserts an auto break before the outgoing user message
// when the current session has already reached maxRounds completed rounds.
// Rounds are counted from the last break. 0 and a missing limit never break.
func (a *App) maybeAutoBreak(conv *Conversation) {
	if conv == nil {
		return
	}
	n := conv.MaxRounds
	if n <= 0 {
		return
	}
	if completedRounds(sessionMessages(conv)) < n {
		return
	}
	conv.Breaks = append(conv.Breaks, SessionBreak{
		ID: newID(), Kind: sessionAuto, Limit: n, CreatedAt: time.Now(),
		After: len(conv.Messages),
	})
}

// sessionInsertWhy reports whether New Session can cut the active chat,
// and the tooltip reason when it cannot. Streaming wins over an empty session.
func (a *App) sessionInsertWhy() (ok bool, why string) {
	conv := a.loadedActive()
	if conv != nil && a.isStreaming(conv.ID) {
		return false, "Available when the reply finishes"
	}
	if !a.CanInsertBreak() {
		return false, "Send a message first"
	}
	return true, ""
}

// parseSessionLimit clamps a typed Max context value onto 1–100.
// Empty, 0, and anything that is not a number become 1.
func parseSessionLimit(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 {
		return 1
	}
	if n > 100 {
		return 100
	}
	return n
}

// transcriptItem is one row of the message list. msg is a message index,
// or -1 when the row is the break at brk.
type transcriptItem struct {
	msg int
	brk int
}

// transcriptItems walks messages and breaks in file order. A break with
// After == i is the row before message i. Trailing breaks come last.
func transcriptItems(conv *Conversation) []transcriptItem {
	if conv == nil {
		return nil
	}
	rows := make([]transcriptItem, 0, len(conv.Messages)+len(conv.Breaks))
	b := 0
	for i := range conv.Messages {
		for b < len(conv.Breaks) && conv.Breaks[b].After == i {
			rows = append(rows, transcriptItem{msg: -1, brk: b})
			b++
		}
		rows = append(rows, transcriptItem{msg: i, brk: -1})
	}
	for b < len(conv.Breaks) {
		rows = append(rows, transcriptItem{msg: -1, brk: b})
		b++
	}
	return rows
}

// SessionLabel is the transcript text for a break. A manual break is
// "New Session". An auto break names the limit that cut it, including
// the singular "1-round".
func SessionLabel(b SessionBreak) string {
	if b.Kind != sessionAuto {
		return "New Session"
	}
	n := b.Limit
	if n < 1 {
		n = 1
	}
	return fmt.Sprintf("New Session · auto (%d-round limit)", n)
}
