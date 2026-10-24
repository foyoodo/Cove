package main

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

// Quote card geometry from tokens layout.quote. No new color tokens.
const (
	quoteBarH         = 28
	quoteBarGap       = 8
	quoteBarPad       = 4
	quoteBarRadius    = 8
	quoteBarEdge      = 12
	quoteBarDelay     = 150 * time.Millisecond
	quoteBarEnter     = 120 * time.Millisecond
	quoteBarExit      = 80 * time.Millisecond
	quoteCardGap      = 6
	quoteCardMax      = 180
	quoteCardMaxShort = 118
	quoteFlashDur     = 600 * time.Millisecond
	quoteLimitDur     = 3 * time.Second
	quoteBarGuess     = 168
	// layout.quote.close. iconInk is the drawn × bounds in pt, not an
	// icon-box size. Changing it is the whole size adjustment.
	quoteCloseSize   = 20
	quoteCloseInk    = 8
	quoteCloseStroke = 1.5
	quoteCloseInset  = 4
)

// quoteCloseMarkup draws the × so its round-capped stroke fills an
// iconInk square. One viewBox unit is one pt when the icon is sized to
// iconInk, so the stroke is quoteCloseStroke pt.
func quoteCloseMarkup() string {
	ink := float64(quoteCloseInk)
	stroke := float64(quoteCloseStroke)
	m := stroke / 2
	end := ink - m
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %g %g" fill="none" stroke="currentColor" stroke-width="%g" stroke-linecap="round" stroke-linejoin="round"><path d="M%g %g L%g %g"/><path d="M%g %g L%g %g"/></svg>`,
		ink, ink, stroke, m, m, end, end, m, end, end, m)
}

var iconQuoteClose = ui.MustParseSVG([]byte(quoteCloseMarkup()))

// quoteMsgLabel marks the column whose selectable text belongs to id.
func quoteMsgLabel(id string) string {
	return "qmsg:" + id
}

// noteQuotePointer records a primary press so a click that is not the bar
// can dismiss it. Returning false leaves the event to selection and buttons.
func (a *App) noteQuotePointer(ev ui.InputEvent) bool {
	if ev.Kind == ui.InputPointerDown && ev.Button == 0 {
		a.quotePointerDown = true
		a.quotePointerX = ev.X
		a.quotePointerY = ev.Y
		// A press while the window is not key is the click that activates
		// it. That target keeps the keyboard; reactivation does not move it.
		if !a.windowWasFocused {
			a.activateByClick = true
		}
	}
	return false
}

// clearHistorySelection drops a selection that is no longer on screen,
// such as an empty chat or the welcome pane.
func (a *App) clearHistorySelection() {
	a.histSel = HistorySelection{}
	a.quoteBarOpen = false
	a.quoteBarHold = false
	a.quoteDragging = false
	a.quoteBarWait = time.Time{}
}

// captureHistorySelection reads the shared transcript selection. visible is
// the area the bar may occupy: the transcript, below the find bar and
// above the composer.
func (a *App) captureHistorySelection(c *ui.Context, list ui.Element, visible ui.Rect) {
	anchors, dragging := indexQuoteNodes(list)
	parts := list.SelectedTexts()
	sel := assembleSelection(parts, anchors)
	if lines := selectionLineRects(list, parts); len(lines) > 0 {
		sel.First = firstLineSpan(lines)
		sel.LastBottom = lastLineBottom(lines)
		sel.Rect = unionLines(lines)
	} else {
		// No glyph span: do not place on the paragraph box.
		sel.Rect = ui.Rect{}
	}
	if a.quotePhrase != "" {
		a.quotePhraseAt = phraseLineRects(list, a.quotePhrase)
	}
	inView := selectionInView(sel.Rect, visible) && selectionMapped(parts, anchors)
	text := strings.TrimSpace(sel.Text) != ""
	a.quoteDragging = dragging
	if dragging {
		// A drag or a double-click is a new selection. Mouse-up shows
		// the bar whether or not this text was quoted before. The only
		// highlight that skips the bar is the one left on screen when
		// the last quote was added or the bar was dismissed.
		a.quoteBarHold = false
	}
	if !inView {
		sel = HistorySelection{}
		text = false
	}
	a.histSel = sel
	held := a.quoteBarHold && text
	if dragging || !text || held || sel.First.W <= 0 {
		a.quoteBarOpen = false
		if dragging || !text {
			a.quoteBarWait = time.Time{}
		}
		return
	}
	now := c.Now()
	if a.quoteBarWait.IsZero() || a.quoteBarWaitText != sel.Text {
		a.quoteBarWait = now
		a.quoteBarWaitText = sel.Text
	}
	delay := quoteBarDelay
	if a.instant {
		delay = 0
	}
	ready := !now.Before(a.quoteBarWait.Add(delay))
	if !ready {
		a.quoteBarOpen = false
		c.After(a.quoteBarWait.Add(delay).Sub(now))
		return
	}
	compact := a.cardContentWidth(c) < 560
	width := a.quoteBarW
	if width < 40 {
		width = quoteBarWidth(compact, windowScale(c))
	}
	if x, y, below, ok := placeQuoteBar(sel.First, sel.LastBottom, visible, width); ok {
		a.quoteBarOpen = true
		a.quoteBarBelow = below
		a.quoteBarX, a.quoteBarY = x, y
		a.quoteBarRect = ui.Rect{X: x, Y: y, W: width, H: quoteBarH}
		if a.quoteBarShown.IsZero() {
			a.quoteBarShown = now
		}
	} else {
		a.quoteBarOpen = false
	}
}

// finishQuotePointer dismisses the bar when the press was outside it and
// was not the start of a selection drag.
func (a *App) finishQuotePointer() {
	if a.quotePointerDown && a.quoteBarOpen && !a.quoteDragging && !pointInRect(a.quotePointerX, a.quotePointerY, a.quoteBarRect) {
		a.dismissQuoteBar()
	}
	a.quotePointerDown = false
}

func (a *App) dismissQuoteBar() {
	a.quoteBarOpen = false
	// Keep the bar down for the highlight already on screen. The next
	// drag or double-click clears this, including a reselection of the
	// same words.
	a.quoteBarHold = true
	a.quoteBarShown = time.Time{}
}

// addSelectionToComposer is ⌘L and the Add to Chat button. A non-empty
// transcript selection becomes one quote and the composer takes the caret.
// With no selection the composer only takes the caret. Inline edit ignores it.
func (a *App) addSelectionToComposer() {
	conv := a.active()
	if conv == nil || a.edit.convID != "" || a.confirmKind != "" || a.modalOpen || a.settingsOpen {
		return
	}
	sel := a.histSel
	if strings.TrimSpace(sel.Text) != "" {
		q := a.quoteFromSelection(conv, sel)
		if i := quoteDupIndex(conv.DraftQuotes, q.Text); i >= 0 {
			a.quoteFlashIndex = i
			a.quoteFlashPending = true
		} else if len(conv.DraftQuotes) >= quoteMax {
			a.quoteLimitConv = conv.ID
			a.quoteLimitPending = true
		} else if next, ok := addQuote(conv.DraftQuotes, q); ok {
			conv.DraftQuotes = next
			a.quoteFlashIndex = -1
			a.quoteFollowEnd = true
			a.save()
		}
	}
	a.dismissQuoteBar()
	a.focusComposer()
}

func (a *App) quoteFromSelection(conv *Conversation, sel HistorySelection) Quote {
	q := Quote{
		Text: sel.Text, SourceMessageID: sel.SourceMessageID,
		Multi: sel.Multi, Code: sel.Code, Lang: sel.Lang,
	}
	if sel.Multi || conv == nil {
		return q
	}
	for i := range conv.Messages {
		m := &conv.Messages[i]
		if m.ID != sel.SourceMessageID {
			continue
		}
		if m.Role == roleUser {
			q.Src = "You"
		} else {
			q.Src = "Cove"
		}
		q.Time = m.CreatedAt
		break
	}
	return q
}

func (a *App) removeDraftQuote(i int) {
	conv := a.active()
	if conv == nil {
		return
	}
	next := removeQuote(conv.DraftQuotes, i)
	if len(next) == len(conv.DraftQuotes) {
		return
	}
	conv.DraftQuotes = next
	if a.quoteFlashIndex == i {
		a.quoteFlashIndex = -1
	}
	a.save()
}

func (a *App) quoteLimitActive(c *ui.Context, conv *Conversation) bool {
	if conv == nil || a.quoteLimitConv != conv.ID {
		return false
	}
	if a.quoteLimitPending {
		a.quoteLimitUntil = c.Now().Add(quoteLimitDur)
		a.quoteLimitPending = false
	}
	if a.quoteLimitUntil.IsZero() {
		return false
	}
	if !c.Now().Before(a.quoteLimitUntil) {
		return false
	}
	c.After(a.quoteLimitUntil.Sub(c.Now()))
	return true
}

// quoteBarView is the selection toolbar: one Add to Chat button on the
// menu material, 8pt off the selection, flipped below when the top is tight.
func (a *App) quoteBarView(c *ui.Context) {
	if !a.quoteBarOpen || a.edit.convID != "" || a.confirmKind != "" || a.modalOpen || a.settingsOpen {
		return
	}
	p := a.look
	now := c.Now()
	compact := a.cardContentWidth(c) < 560
	bar := ui.Row(c.Key("quote-bar")).Absolute().Left(a.quoteBarX).Top(a.quoteBarY).Height(quoteBarH).Padding(quoteBarPad).Radius(quoteBarRadius).AlignItems(ui.Center).Background(p.LayerOverlay).Border(0.5, p.LayerOverlayBorder).Shadow(0, 8, 24, 0, p.ShadowMd).Role(ui.RoleToolbar).Label("Selection")
	if !a.instant && !a.quoteBarShown.IsZero() {
		u := float32(now.Sub(a.quoteBarShown)) / float32(quoteBarEnter)
		if u < 1 {
			if u < 0 {
				u = 0
			}
			bar.Opacity(u)
			if !a.reduceMotion {
				dy := (1 - u) * 2
				if a.quoteBarBelow {
					dy = -dy
				}
				bar.Translate(0, dy)
			}
			c.After(quoteBarEnter - now.Sub(a.quoteBarShown))
		}
	}
	bar.Children(func() {
		scale := windowScale(c)
		leftPad, rightPad := quoteButtonPads(compact, scale)
		btn := ui.ButtonBase(c).Height(20).Padding(0, rightPad, 0, leftPad).Gap(0).Radius(4).AlignItems(ui.Center).Label("Add selection to chat").Cursor(ui.CursorPointer)
		if btn.Pressed() {
			btn.Background(p.FillPressed)
		} else if btn.Hovered() {
			btn.Background(p.FillHover)
		}
		btn.Children(func() {
			// 14pt text.quote, ink centered in the box so the row's center
			// line — the cap line of "Add to Chat" — runs through the icon.
			ui.Icon(c, iconQuote).Size(14, 14).TextColor(p.TextSecondary).Label("bar quote")
			ui.Box(c).Width(6).Shrink(0)
			controlText(c, "Add to Chat", 13, 0, 400, p.TextPrimary)
			if !compact {
				// 8pt of ink from the label to ⌘. ⌘L is the shared shortcut
				// line at Label/Small 12pt: 1pt of ink between the two keys,
				// each centered on the cap line. No fixed per-glyph nudge.
				gap := quoteLeadGapAt(scale)
				openInkGap(c, gap)
				hint := controlLineFlow(c, "⌘L", 12, 0, 400, p.TextTertiary)
				if gap < 0 {
					hint.MarginStart(gap)
				}
			}
		})
		if btn.Clicked() {
			a.addSelectionToComposer()
		}
	})
	if b := bar.Bounds(); b.W > 40 && abs32(b.W-a.quoteBarW) > 0.5 {
		a.quoteBarW = b.W
		a.quoteBarRect.W = b.W
	}
}

// quoteBarWidth predicts the bar from the same glyph advances the button
// draws, so the first frame centers on the selection instead of a guess.
func quoteBarWidth(compact bool, scale float32) float32 {
	left, right := quoteButtonPads(compact, scale)
	w := float32(quoteBarPad)*2 + left + right + 14 + 6
	w += opticalOf("Add to Chat", 13, 0, 400).elemW
	if compact {
		return w
	}
	w += quoteLeadGapAt(scale)
	w += shortcutRowWidth("⌘L", 12, 0, 400, scale)
	return w
}

// quoteLeadGap is SPEC §8 for the bar: 8pt of ink from the label to ⌘.
// spacer = 8 − right bearing of "Add to Chat" − left bearing of ⌘.
func quoteLeadGap() float32 {
	return specSpacer("Add to Chat", 13, 0, "⌘", 12, 0, 400, 8)
}

// quoteLeadGapAt is quoteLeadGap on the window's pixel grid.
func quoteLeadGapAt(scale float32) float32 {
	gap, _ := gridSpacer("Add to Chat", 13, 0, "⌘", 12, 0, 400, 8, scale, false)
	return gap
}

// quoteButtonPads is 6pt of button padding measured to ink: the quote
// icon on the left, and on the right the hint when it shows, otherwise
// the label.
func quoteButtonPads(compact bool, scale float32) (left, right float32) {
	left = 6 - iconInkLeft(iconQuote, 14)
	if compact {
		box := opticalOf("Add to Chat", 13, 0, 400)
		return left, 6 - (box.elemW - box.right)
	}
	return left, 6 - shortcutInkRightBearing("⌘L", 12, 0, 400, false, scale)
}

func iconInkLeft(glyph *ui.SVG, size float32) float32 {
	iconInkMu.Lock()
	if box, ok := iconInkCache[size]; ok {
		iconInkMu.Unlock()
		return box
	}
	iconInkMu.Unlock()
	const scale float32 = 4
	tt := ui.NewTester(func(c *ui.Context) {
		white := ui.Color{R: 255, G: 255, B: 255, A: 255}
		black := ui.Color{A: 255}
		c.Root().Background(white)
		ui.Row(c).Padding(16).Background(white).Children(func() {
			ui.Icon(c, glyph).Size(size, size).TextColor(black).Label("icon optical")
		})
	}, 160, 80)
	tt.SetScale(scale)
	tt.Frame()
	r, ok := tt.Find("icon optical")
	if !ok || r.W < 1 {
		iconInkCache[size] = 0
		return 0
	}
	img := tt.Image()
	b := img.Bounds()
	x0 := int(r.X * scale)
	x1 := int((r.X + r.W) * scale)
	y0 := int(r.Y * scale)
	y1 := int((r.Y + r.H) * scale)
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	left := x1
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			px := img.RGBAAt(x, y)
			if px.R > 180 && px.G > 180 && px.B > 180 {
				continue
			}
			if x < left {
				left = x
			}
		}
	}
	bearing := float32(0)
	if left < x1 {
		bearing = float32(left)/scale - r.X
	}
	iconInkMu.Lock()
	iconInkCache[size] = bearing
	iconInkMu.Unlock()
	return bearing
}

var (
	iconInkMu    sync.Mutex
	iconInkCache = map[float32]float32{}
)

// composerQuotes is the stack of quote cards above the field.
func (a *App) composerQuotes(c *ui.Context, conv *Conversation) {
	if conv == nil || len(conv.DraftQuotes) == 0 {
		return
	}
	maxH := float32(quoteCardMax)
	if a.cardContentWidth(c) < 560 {
		maxH = quoteCardMaxShort
	}
	if a.quoteFlashPending {
		a.quoteFlashAt = c.Now()
		a.quoteFlashPending = false
	}
	quotes := conv.DraftQuotes
	if a.quoteFollowEnd {
		a.quoteScroll.Y = math.MaxFloat32
		a.quoteFollowEnd = false
	}
	full := float32(0)
	for i, q := range quotes {
		if i > 0 {
			full += quoteCardGap
		}
		full += quoteCardEstimate(q, a.quoteTextWidth(c, quoteCardComposer))
	}
	if full > maxH {
		ui.Scroll(c).FillWidth().MaxHeight(maxH).TrackScroll(&a.quoteScroll).Children(func() {
			ui.Column(c).FillWidth().Gap(quoteCardGap).Children(func() {
				for i := range quotes {
					a.quoteCard(c, quotes[i], quoteCardComposer, conv.ID, i)
				}
			})
		})
		return
	}
	ui.Column(c).FillWidth().Gap(quoteCardGap).Children(func() {
		for i := range quotes {
			a.quoteCard(c, quotes[i], quoteCardComposer, conv.ID, i)
		}
	})
}

const (
	quoteCardComposer = iota
	quoteCardBubble
	quoteCardEdit
)

func (a *App) quoteCard(c *ui.Context, q Quote, kind int, msgID string, index int) {
	p := a.look
	bg := p.FillQuaternary
	if kind == quoteCardComposer && !a.reduceMotion && index == a.quoteFlashIndex && !a.quoteFlashAt.IsZero() {
		u := float32(c.Now().Sub(a.quoteFlashAt)) / float32(quoteFlashDur)
		if u >= 0 && u < 1 {
			peak := u * 2
			if u > 0.5 {
				peak = (1 - u) * 2
			}
			bg = mixByteColor(p.FillQuaternary, p.FillPressed, peak)
			c.After(quoteFlashDur - c.Now().Sub(a.quoteFlashAt))
		}
	}
	expanded := kind == quoteCardBubble && a.quoteExpanded != nil && a.quoteExpanded[quoteExpandKey(msgID, index)]
	card := ui.Column(c).FillWidth().MinWidth(0).Padding(5, 4, 6, 12).Gap(0).Radius(radiusRow).Background(bg).Role(ui.RoleGroup).Label(quoteAccessLabel(q))
	if kind == quoteCardBubble {
		// Click, or Space / Enter while focused, toggles the full text.
		card.Cursor(ui.CursorPointer).Focusable()
	}
	card.Children(func() {
		ui.Box(c).Absolute().Left(4).Top(5).Bottom(6).Width(2).Radius(1).Background(p.AccentDefault)
		if line := quoteSourceLine(q); line != "" {
			// Height locks the row at 14 even when the font's own em box is taller.
			ui.Text(c, line).FontSize(11).FixedLineHeight(14).Height(14).TextColor(p.TextTertiary).SingleLine().Ellipsis("…")
		}
		body := ui.Text(c, q.Text).FontSize(12).LineHeight(15.0 / 12.0).TextColor(p.TextSecondary).FillWidth().MinWidth(0)
		if q.Code {
			body.Font("ui-monospace")
		}
		if !expanded {
			// The row is 15pt. How many of them depends on the wrap at this
			// card's width, not on a character count.
			n := quoteLayoutLines(q, a.quoteTextWidth(c, kind), false)
			body.Height(float32(n) * 15).MaxLines(2).Ellipsis("…")
		}
		if kind == quoteCardBubble {
			return
		}
		btn := ui.ButtonBase(c).Absolute().Right(quoteCloseInset).Top(quoteCloseInset).Size(quoteCloseSize, quoteCloseSize).Radius(quoteCloseSize / 2).Center().Label("Remove quote").Cursor(ui.CursorPointer)
		a.noteQuoteTab(btn)
		a.takeTab(c, btn)
		if btn.Hovered() {
			btn.Background(p.FillHover)
		}
		btn.Children(func() {
			ui.Icon(c, iconQuoteClose).Size(quoteCloseInk, quoteCloseInk).TextColor(p.TextSecondary).Label("quote-close-icon")
		})
		if btn.Clicked() {
			if kind == quoteCardEdit {
				a.removeEditQuote(index)
			} else {
				a.removeDraftQuote(index)
			}
		}
	})
	if kind == quoteCardBubble && card.Clicked() {
		if a.quoteExpanded == nil {
			a.quoteExpanded = map[string]bool{}
		}
		key := quoteExpandKey(msgID, index)
		a.quoteExpanded[key] = !a.quoteExpanded[key]
	}
}

func quoteExpandKey(msgID string, index int) string {
	return msgID + "#" + fmt.Sprint(index)
}

func quoteSourceLine(q Quote) string {
	if q.Multi || q.Src == "" {
		return ""
	}
	if q.Time.IsZero() {
		return q.Src
	}
	return q.Src + " · " + q.Time.Format("15:04")
}

// quoteAccessLabel is the VoiceOver name: "Quote from Cove: " plus the
// first 80 runes of the quote, whitespace folded.
func quoteAccessLabel(q Quote) string {
	who := q.Src
	if who == "" {
		who = "the selection"
	}
	text := strings.Join(strings.Fields(q.Text), " ")
	rs := []rune(text)
	if len(rs) > 80 {
		text = string(rs[:80])
	}
	return "Quote from " + who + ": " + text
}

func mixByteColor(a, b ui.Color, t float32) ui.Color {
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	lerp := func(x, y uint8) uint8 {
		return uint8(float32(x) + (float32(y)-float32(x))*t + 0.5)
	}
	return ui.Color{R: lerp(a.R, b.R), G: lerp(a.G, b.G), B: lerp(a.B, b.B), A: lerp(a.A, b.A)}
}

func quoteAreaHeight(quotes []Quote, maxH, textW float32) float32 {
	if len(quotes) == 0 {
		return 0
	}
	h := float32(0)
	for i, q := range quotes {
		if i > 0 {
			h += quoteCardGap
		}
		h += quoteCardEstimate(q, textW)
	}
	if maxH > 0 && h > maxH {
		return maxH
	}
	return h
}

// quoteTextWidth is the width the quote body wraps at. kind is a composer
// card, the inline editor, or a card inside a sent user bubble.
func (a *App) quoteTextWidth(c *ui.Context, kind int) float32 {
	winW, _ := c.Size()
	return quoteTextWidth(kind, winW, a.sidebarWidth)
}

// quoteBubbleSlot is the bubble's outer width: the message column minus
// Copy and Edit, and never wider than the 520pt cap.
func quoteBubbleSlot(winW, sidebar float32) float32 {
	pane := paneInnerWidth(winW, sidebarShown(sidebar, winW))
	col := pane - space6*2
	if col > readingWidth {
		col = readingWidth
	}
	actions := float32(iconSm*2 + 2)
	slot := col - actions
	if slot > bubbleMax {
		slot = bubbleMax
	}
	if slot < 80 {
		return 80
	}
	return slot
}

func quoteTextWidth(kind int, winW, sidebar float32) float32 {
	pane := paneInnerWidth(winW, sidebarShown(sidebar, winW))
	var outer float32
	switch kind {
	case quoteCardBubble:
		// The body lays out in the bubble slot, inside 12+12 bubble
		// padding and 12+4 card padding.
		outer = quoteBubbleSlot(winW, sidebar) - (12 + 12 + 12 + 4)
	default:
		side, _ := composerInsets(pane)
		row := pane - side*2
		if row > readingWidth {
			row = readingWidth
		}
		// Shell border 1+1, shell padding 12+8, card padding 12+4.
		outer = row - 2 - 12 - 8 - 12 - 4
	}
	if outer < 40 {
		return 40
	}
	return outer
}

// quoteCardEstimate is the rendered height of a collapsed quote card:
// padding 5+6, a 14pt source line when there is one, and 15pt for each
// body line the text actually wraps to, at most two.
func quoteCardEstimate(q Quote, textW float32) float32 {
	h := float32(5 + 6)
	if quoteSourceLine(q) != "" {
		h += 14
	}
	h += float32(quoteLayoutLines(q, textW, false)) * 15
	return h
}

// quoteLayoutLines is the number of rows the body takes when laid out at
// textW. A collapsed card keeps at most two and ellipsizes the rest.
func quoteLayoutLines(q Quote, textW float32, expanded bool) int {
	n := quoteWrappedLines(q.Text, quoteBodyFont(q), textW)
	if !expanded && n > 2 {
		return 2
	}
	if n < 1 {
		return 1
	}
	return n
}

func quoteBodyFont(q Quote) ui.Font {
	f := ui.Font{Size: 12}
	if q.Code {
		f.Family = "ui-monospace"
	}
	return f
}

func quoteWrappedLines(text string, font ui.Font, width float32) int {
	if strings.TrimRight(text, "\n") == "" {
		return 1
	}
	if width < 1 {
		width = 10000
	}
	n := 0
	for _, para := range strings.Split(text, "\n") {
		if para == "" {
			n++
			continue
		}
		_, h := ui.ShapeText(para, font, width).Size()
		sample := string([]rune(para)[:1])
		_, h1 := ui.ShapeText(sample, font, width).Size()
		if h1 < 1 {
			h1 = h
		}
		lines := int(h/h1 + 0.01)
		if lines < 1 {
			lines = 1
		}
		n += lines
	}
	if n < 1 {
		return 1
	}
	return n
}

func (a *App) removeEditQuote(i int) {
	a.edit.quotes = removeQuote(a.edit.quotes, i)
}
