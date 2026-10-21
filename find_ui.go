package main

import (
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown/parser"

	"cove/themes"
)

func (a *App) findAllowed() bool {
	if a.shotStates || a.shotPalette {
		return false
	}
	if a.settingsOpen && (a.prefs == nil || a.win == nil || !a.win.IsFocused()) {
		return false
	}
	conv := a.active()
	return conv != nil && len(conv.Messages) > 0
}

func (a *App) syncFindMenu() {
	on := a.findAllowed()
	if a.findItem != nil && a.findItem.IsEnabled() != on {
		a.findItem.SetEnabled(on)
	}
	step := a.finder.open && on
	if a.findNextItem != nil && a.findNextItem.IsEnabled() != step {
		a.findNextItem.SetEnabled(step)
	}
	if a.findPrevItem != nil && a.findPrevItem.IsEnabled() != step {
		a.findPrevItem.SetEnabled(step)
	}
}

func (a *App) openFind() {
	if !a.findAllowed() {
		return
	}
	if a.edit.convID != "" {
		a.cancelEdit()
	}
	a.focusComposerID = ""
	a.finder.wantFocus = true
	if a.finder.open {
		return
	}
	a.finder.open = true
	a.finder.cur = -1
	a.finder.sel = 0
	a.finder.listOpen = false
	a.finder.settled = true
}

func (a *App) closeFind() {
	if !a.finder.open {
		return
	}
	a.finder.open = false
	a.finder.listOpen = false
	a.finder.query = ""
	a.finder.draft = ""
	a.finder.appliedDraft = ""
	a.finder.dirtyDraft = ""
	a.finder.hits = nil
	a.finder.capped = false
	a.finder.cur = -1
	a.finder.sel = 0
	a.finder.lift = 0
	a.finder.jumpMsg = ""
	a.finder.settled = true
	a.finder.flashID = ""
	a.finder.flashPending = false
	a.finder.markFresh = false
	a.finder.wantFocus = false
	a.focusComposer()
}

func (a *App) syncFindQuery(now time.Time) {
	if !a.finder.open {
		return
	}
	if a.finder.draft == a.finder.appliedDraft {
		return
	}
	if a.finder.dirtyDraft != a.finder.draft {
		a.finder.dirtyDraft = a.finder.draft
		a.finder.dirtyAt = now
	}
	if !a.instant && now.Sub(a.finder.dirtyAt) < findDebounce {
		return
	}
	a.finder.appliedDraft = a.finder.draft
	a.finder.query = strings.TrimSpace(a.finder.draft)
	a.finder.listOpen = a.finder.query != ""
	a.finder.cur = -1
	a.finder.sel = 0
	a.finder.jumpMsg = ""
	a.finder.settled = true
	a.recomputeHits(false)
}

func (a *App) findMove(d int) {
	n := len(a.finder.hits)
	if n == 0 {
		return
	}
	a.finder.listOpen = true
	if a.finder.cur >= 0 {
		a.finder.sel = a.finder.cur
		a.finder.cur = -1
	}
	a.finder.sel = (a.finder.sel + d%n + n) % n
	a.finder.selMoved = true
}

func (a *App) findCommitEnter() {
	if a.finder.enterHandled {
		return
	}
	a.finder.enterHandled = true
	if a.finder.listOpen && len(a.finder.hits) > 0 {
		a.jumpFind(a.finder.sel, time.Time{})
		return
	}
	a.findStep(1, time.Time{})
}

func (a *App) findStep(d int, now time.Time) {
	n := len(a.finder.hits)
	if n == 0 || !a.finder.open {
		return
	}
	var k int
	if a.finder.cur < 0 {
		if d > 0 {
			k = a.finder.sel
		} else {
			k = a.finder.sel - 1
		}
	} else {
		k = a.finder.cur + d
	}
	a.jumpFind(k, now)
}

func (a *App) jumpFind(k int, now time.Time) {
	n := len(a.finder.hits)
	if n == 0 {
		return
	}
	k %= n
	if k < 0 {
		k += n
	}
	h := a.finder.hits[k]
	a.finder.cur = k
	a.finder.sel = k
	a.finder.listOpen = false
	a.finder.jumpMsg = h.MsgID
	a.finder.jumpStart = h.Start
	a.finder.settled = false
	a.finder.nudges = 0
	a.finder.markFresh = false
	a.finder.lift = 0
	if a.reduceMotion {
		a.finder.flashID = ""
		return
	}
	a.finder.flashID = h.MsgID
	if now.IsZero() {
		a.finder.flashPending = true
	} else {
		a.finder.flashAt = now
		a.finder.flashPending = false
	}
}

func (a *App) findCount() string {
	if strings.TrimSpace(a.finder.draft) == "" {
		return ""
	}
	if a.finder.query == "" {
		return ""
	}
	n := len(a.finder.hits)
	if n == 0 {
		if !a.finder.ready {
			return ""
		}
		return "No results"
	}
	i := a.finder.cur
	if i < 0 {
		i = a.finder.sel
	}
	if i < 0 {
		i = 0
	}
	total := itoa(n)
	if a.finder.capped {
		total = "1000+"
	}
	return itoa(i+1) + " of " + total
}

func (a *App) findCenter(c *ui.Context) float32 {
	_, winH := c.Size()
	_, bottom := a.composerInsets(c)
	cardH := winH - titleBarHeight - 8
	compTop := cardH - bottom - a.composerH
	if compTop < findBarH {
		compTop = findBarH
	}
	return titleBarHeight + (findBarH+compTop)/2
}

func (a *App) placeFind(c *ui.Context, conv *Conversation) {
	if !a.finder.open || a.finder.jumpMsg == "" || a.finder.settled || conv == nil {
		return
	}
	row := transcriptRowFor(conv, a.finder.jumpMsg)
	if row < 0 {
		a.finder.settled = true
		return
	}
	a.chat.FollowEnd = false
	want := a.findCenter(c)
	if !a.finder.markFresh {
		a.finder.nudges++
		if a.finder.nudges >= 8 {
			a.finder.settled = true
			return
		}
		a.chat.ScrollTo(row, ui.Center)
		return
	}
	got := a.finder.markY + a.finder.markH/2
	err := got - want
	a.finder.markFresh = false
	if err < 4 && err > -4 {
		a.finder.settled = true
		return
	}
	if a.finder.nudges >= 6 {
		a.finder.settled = true
		return
	}
	a.finder.nudges++
	if err < 0 {
		a.finder.lift += -err
		a.chat.ScrollTo(row, ui.Start)
		return
	}
	if a.finder.lift > 0 {
		next := a.finder.lift - err
		if next < 0 {
			next = 0
		}
		a.finder.lift = next
		a.chat.ScrollTo(row, ui.Start)
		return
	}
	a.chat.ScrollTo(row, ui.Center)
}

func transcriptRowFor(conv *Conversation, id string) int {
	rows := transcriptItems(conv)
	for i, row := range rows {
		if row.msg >= 0 && row.msg < len(conv.Messages) && conv.Messages[row.msg].ID == id {
			return i
		}
	}
	return -1
}

func (a *App) findChrome(c *ui.Context, conv *Conversation) {
	if conv == nil || !a.finder.open {
		return
	}
	if a.finder.listOpen {
		dismiss := ui.Box(c).Absolute().Left(0).Right(0).Top(findBarH).Bottom(0).Label("find dismiss")
		if dismiss.Clicked() {
			a.finder.listOpen = false
		}
	}
	a.findBar(c, conv)
}

// findBarPlate is the find bar's own surface: contentFlat at 85% over the
// opaque card, then layer/composer, the same stack as the composer. Both
// layers are translucent on their own; compositing them here makes the
// 32pt bar opaque so transcript text scrolling underneath cannot show through.
func findBarPlate(flat, composer ui.Color) ui.Color {
	backing := flat.Alpha(0.85).Over(flat)
	return composer.Over(backing)
}

func (a *App) findBar(c *ui.Context, conv *Conversation) {
	p := a.look
	bar := ui.Row(c).Key("find-bar").Absolute().Left(0).Right(0).Top(0).Height(findBarH).Padding(0, 8).Gap(6).AlignItems(ui.Center).Label("find bar")
	plate := findBarPlate(themes.ContentFlat(a.data.Theme, p.Dark), p.LayerComposer)
	bar.Background(plate).BorderWidth(0, 0, 1, 0).BorderColor(p.SeparatorDefault)
	if !a.instant && !a.reduceMotion {
		bar.Transition(ui.ElementTransition{Enter: &ui.Motion{Y: -findBarH}, Duration: 160 * time.Millisecond, Ease: easeEnter})
	}
	bar.Children(func() {
		// Width 420, not Grow(1): the spacer also grows, and an equal share
		// left the field near 330 at 1120 and 150 at 720. The field keeps
		// 420 until the row runs out of room, then it is the only item
		// that shrinks (down to 120). The spacer takes whatever is left.
		field := ui.Row(c).Height(22).Width(420).MinWidth(120).MaxWidth(420).Gap(6).Padding(0, 8).Radius(radiusRow).Background(p.ControlField).Border(1, p.ControlFieldBorder).AlignItems(ui.Center).Label("find field")
		var input ui.Element
		field.Children(func() {
			ui.Icon(c, iconSearch).Size(13, 13).TextColor(p.TextSecondary).Shrink(0)
			input = ui.TextInputBase(c.Key("find-field"), &a.finder.draft).Placeholder("Find in chat").Label("Find in chat").Grow(1).MinWidth(0).FontSize(13)
			input.OnTextKey(func(k ui.TextKey) bool {
				if k.Key == ui.KeyBackspace && k.Mods == ui.Cmd {
					a.askDelete()
					return true
				}
				switch {
				case k.Key == ui.KeyDown && k.Mods == 0:
					a.findMove(1)
					return true
				case k.Key == ui.KeyUp && k.Mods == 0:
					a.findMove(-1)
					return true
				case k.Key == ui.KeyEnter && k.Mods == ui.Shift:
					a.finder.enterHandled = true
					a.findStep(-1, c.Now())
					return true
				case k.Key == ui.KeyEnter && k.Mods == 0:
					a.findCommitEnter()
					return true
				case k.Key == ui.KeyEscape && k.Mods == 0:
					a.closeFind()
					return true
				}
				return false
			})
			input.Border(0, ui.Transparent).Background(ui.Transparent).FocusRing(false)
			paintPlaceholder(c, input, p.TextTertiary)
			if a.finder.wantFocus {
				input.Focus()
				input.SetTextSelection(0, utf8.RuneCountInString(a.finder.draft))
				a.finder.wantFocus = false
			}
		})
		if input.Valid() && input.Focused() {
			field.Border(1, p.AccentDefault).Shadow(0, 0, 0, 3, p.AccentRing)
		}
		a.finder.focused = input.Valid() && input.Focused()
		if input.Valid() && input.Submitted() && !a.finder.enterHandled {
			if c.Modifiers()&ui.Shift != 0 {
				a.finder.enterHandled = true
				a.findStep(-1, c.Now())
			} else {
				a.findCommitEnter()
			}
		}

		count := a.findCount()
		controlText(c, count, 12, 0, 400, p.TextTertiary).FontFeatures("tnum").TextAlign(ui.End).MinWidth(56).Shrink(0)

		on := len(a.finder.hits) > 0
		a.findStepButton(c, "Previous", iconChevronLeft, -1, on)
		a.findStepButton(c, "Next", iconChevronRight, 1, on)
		ui.Spacer(c)
		done := ui.ButtonBase(c).Height(24).Padding(0, 8).Radius(radiusRow).AlignItems(ui.Center).Label("Done").Cursor(ui.CursorPointer).Shrink(0)
		if done.Hovered() {
			done.Background(p.FillHover)
		}
		done.Children(func() {
			controlText(c, "Done", 13, 0, 400, p.TextSecondary)
		})
		if done.Clicked() {
			a.closeFind()
		}
		a.findList(c, conv)
	})
}

func (a *App) findStepButton(c *ui.Context, name string, glyph *ui.SVG, d int, enabled bool) ui.Element {
	p := a.look
	b := ui.ButtonBase(c).Size(24, 24).Radius(radiusRow).Center().Label(name).Cursor(ui.CursorPointer).Shrink(0).Disabled(!enabled)
	if enabled && b.Hovered() {
		b.Background(p.FillHover)
	}
	b.Children(func() {
		ui.Icon(c, glyph).Size(13, 13).TextColor(p.TextSecondary)
	})
	tip := name + " ⌘G"
	if d < 0 {
		tip = name + " ⇧⌘G"
	}
	a.tip(c, b, tip)
	if b.Clicked() {
		a.findStep(d, c.Now())
	}
	return b
}

func (a *App) findList(c *ui.Context, conv *Conversation) {
	if !a.finder.listOpen || strings.TrimSpace(a.finder.draft) == "" {
		return
	}
	p := a.look
	_, winH := c.Size()
	cardH := winH - titleBarHeight - 8
	_, bottom := a.composerInsets(c)
	room := cardH - findBarH - a.composerH - bottom - 20
	maxH := float32(findRowH*findRows + 8)
	if room < maxH {
		maxH = room
	}
	if maxH < findRowH {
		maxH = findRowH
	}
	w := a.cardContentWidth(c) - 16
	if w > 420 {
		w = 420
	}
	if w < 160 {
		w = 160
	}
	list := ui.Scroll(c).Absolute().Left(8).Top(findBarH+4).Width(w).MaxHeight(maxH).Padding(4).Radius(radiusCode).Background(p.LayerOverlay).Border(0.5, p.LayerOverlayBorder).Shadow(0, 8, 24, 0, p.ShadowMd).Label("find results")
	list.TrackScroll(&a.finder.listScroll)
	hits := a.finder.hits
	list.Children(func() {
		if len(hits) == 0 {
			ui.Text(c, "No results").FontSize(13).TextColor(p.TextSecondary).Padding(8, 10)
			return
		}
		for i := range hits {
			a.findRow(c, conv, i)
		}
	})
}

func (a *App) findRow(c *ui.Context, conv *Conversation, i int) {
	p := a.look
	h := a.finder.hits[i]
	doc := docByID(a.finder.docs, h.MsgID)
	who := "Cove"
	var when time.Time
	if doc != nil {
		when = doc.When
		if doc.Role == roleUser {
			who = "You"
		}
	}
	on := i == a.finder.sel
	row := ui.ButtonBase(c).Height(findRowH).FillWidth().Padding(6, 10).Radius(radiusRow).AlignItems(ui.Start).Label("find-row-" + itoa(i)).Cursor(ui.CursorPointer)
	if on {
		row.Background(p.AccentDefault)
	} else if row.Hovered() {
		row.Background(p.FillHover)
	}
	if a.finder.selMoved && on {
		row.ScrollIntoView()
		a.finder.selMoved = false
	}
	title := p.TextPrimary
	meta := p.TextTertiary
	body := p.TextSecondary
	match := p.TextPrimary
	if on {
		title, meta, body, match = p.TextOnAccent, p.TextOnAccent, p.TextOnAccent, p.TextOnAccent
	}
	row.Children(func() {
		ui.Column(c).Gap(1).FillWidth().MinWidth(0).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, who).FontSize(12).FontWeight(600).TextColor(title).SingleLine()
				if stamp := formatStamp(c, when); stamp != "" {
					ui.Text(c, stamp).FontSize(11).TextColor(meta).SingleLine()
				}
			})
			lead, trail := "", ""
			if h.Lead {
				lead = "…"
			}
			if h.Trail {
				trail = "…"
			}
			ui.RichText(c,
				ui.Span{Text: lead + h.Before, Size: 12, Color: body},
				ui.Span{Text: h.Match, Size: 12, Weight: 600, Color: match},
				ui.Span{Text: h.After + trail, Size: 12, Color: body},
			).MaxLines(2).FillWidth().MinWidth(0)
		})
	})
	if row.Clicked() {
		a.jumpFind(i, c.Now())
	}
}

func findHL(dark, current bool) float32 {
	if current {
		return 0.32
	}
	if dark {
		return 0.18
	}
	return 0.16
}

func findFlashA(dark bool) float32 {
	if dark {
		return 0.08
	}
	return 0.14
}

func (a *App) paintFlash(c *ui.Context, el ui.Element, id string) {
	if a.reduceMotion || !el.Valid() || a.finder.flashID != id || a.finder.flashAt.IsZero() {
		return
	}
	start := a.finder.flashAt
	dark := a.look.Dark
	mark := a.look.ThemeMark
	el.DrawOver(func(pn *ui.Painter, r ui.Rect) {
		u := float32(c.Now().Sub(start)) / float32(findFlashDur)
		if u < 0 {
			u = 0
		}
		if u >= 1 {
			return
		}
		left := (1 - u) * (1 - u)
		box := ui.Rect{X: r.X - 8, Y: r.Y - 8, W: r.W + 16, H: r.H + 16}
		pn.Fill(box, mark.Alpha(findFlashA(dark)*left), 10)
	})
	if remain := findFlashDur - c.Now().Sub(start); remain > 0 {
		c.After(remain)
	}
}

func (a *App) userBubbleText(c *ui.Context, m *Message) {
	p := a.look
	if len(m.Quotes) > 0 {
		// Shrink so a long quote stays inside the column. MaxWidth is the
		// slot beside Copy and Edit, and never the 520pt cap when that
		// slot is narrower.
		winW, _ := c.Size()
		bubble := ui.Column(c).Padding(7, 12).Gap(4).Radius(radiusBubble).Background(p.BubbleUser).MaxWidth(quoteBubbleSlot(winW, a.sidebarWidth)).MinWidth(0).Shrink(1).Label("user bubble")
		bubble.Children(func() {
			for i := range m.Quotes {
				a.quoteCard(c, m.Quotes[i], quoteCardBubble, m.ID, i)
			}
			if strings.TrimSpace(m.Content) != "" {
				ui.Text(c, m.Content).Selectable().FontSize(14).LineHeight(21.0/14.0).TextColor(p.BubbleUserText).Margin(2, 0, 0, 0)
			}
		})
		a.paintFlash(c, bubble, m.ID)
		return
	}
	style := func(el ui.Element) ui.Element {
		return el.Selectable().FontSize(14).LineHeight(21.0/14.0).TextColor(p.BubbleUserText).Padding(7, 12).Radius(radiusBubble).Background(p.BubbleUser).MaxWidth(bubbleMax).MinWidth(0)
	}
	doc := docByID(a.finder.docs, m.ID)
	if !a.finder.open || a.finder.query == "" || doc == nil || !runOverlaps(findRun{Text: doc.Text}, a.finder.hits, m.ID) {
		el := style(ui.Text(c, m.Content))
		a.paintFlash(c, el, m.ID)
		return
	}
	run := findRun{Kind: "user", Text: m.Content}
	spans := a.hitSpans([]styled{{text: m.Content}}, run, m.ID, 14)
	// The bubble is inline in the actions row, and DrawOver does not run
	// for inline text. A box around it is what records glyph rects.
	wrap := ui.Box(c).Shrink(0)
	wrap.Children(func() {
		style(ui.RichText(c, spans...))
	})
	a.markRun(wrap, spans, run, m.ID, 12, 7, ui.Font{Size: 14}, 21.0/14.0)
}

func (a *App) prepareFindPaint(m *Message) {
	a.finder.paintDoc = nil
	a.finder.paintRun = 0
	a.finder.paintMsg = ""
	if !a.finder.open || a.finder.query == "" || m == nil {
		return
	}
	doc := docByID(a.finder.docs, m.ID)
	if doc == nil {
		return
	}
	a.finder.paintDoc = doc
	a.finder.paintMsg = m.ID
	for a.finder.paintRun < len(doc.Runs) && doc.Runs[a.finder.paintRun].Kind == "think" {
		a.finder.paintRun++
	}
}

func (a *App) finishFindPaint() {
	if a.finder.hooked {
		a.md.Block = a.finder.savedBlock
		a.finder.savedBlock = nil
		a.finder.hooked = false
	}
	a.finder.paintDoc = nil
	a.finder.paintMsg = ""
}

func (a *App) installFindHook() {
	if a.finder.paintDoc == nil || a.finder.hooked {
		return
	}
	prev := a.md.Block
	a.finder.savedBlock = prev
	a.finder.hooked = true
	a.md.Block = func(c *ui.Context, d *parser.Document, i int) bool {
		if a.findBlock(c, d, i) {
			return true
		}
		if prev != nil {
			return prev(c, d, i)
		}
		return false
	}
}

func (a *App) findBlock(c *ui.Context, d *parser.Document, i int) bool {
	if a.finder.paintDoc == nil {
		return false
	}
	blk := d.Block(i)
	switch blk.Kind {
	case parser.Paragraph, parser.Heading:
		return a.paintLeaf(c, d, i, blk)
	case parser.CodeBlock:
		return a.paintCode(c, d, i, blk)
	case parser.Table:
		return a.paintTable(c, d, i)
	default:
		return false
	}
}

func (a *App) takeRun(text string) (findRun, bool) {
	doc := a.finder.paintDoc
	if doc == nil || text == "" {
		return findRun{}, false
	}
	runs := doc.Runs
	for i := a.finder.paintRun; i < len(runs); i++ {
		if runs[i].Kind == "think" {
			continue
		}
		if runs[i].Text == text {
			a.finder.paintRun = i + 1
			return runs[i], true
		}
		return findRun{}, false
	}
	return findRun{}, false
}

func (a *App) paintLeaf(c *ui.Context, d *parser.Document, i int, blk *parser.Block) bool {
	parts := blockParts(d, i)
	text := joinStyled(parts)
	if text == "" {
		return false
	}
	run, ok := a.takeRun(text)
	if !ok || !runOverlaps(run, a.finder.hits, a.finder.paintMsg) {
		return false
	}
	size := float32(14)
	weight := 0
	if blk.Kind == parser.Heading && blk.Level >= 1 && blk.Level <= 6 {
		sizes := [6]float32{20, 17, 15, 14, 13, 12}
		size = sizes[blk.Level-1]
		weight = 600
	}
	spans := a.hitSpans(parts, run, a.finder.paintMsg, size)
	el := ui.RichText(c, spans...).FontSize(size).LineHeight(21.0 / 14.0).TextColor(a.look.TextPrimary).FillWidth().MinWidth(0)
	if weight > 0 {
		el.FontWeight(weight)
	}
	a.markRun(el, spans, run, a.finder.paintMsg, 0, 0, ui.Font{Size: size, Weight: weight}, 21.0/14.0)
	return true
}

func (a *App) paintCode(c *ui.Context, d *parser.Document, i int, blk *parser.Block) bool {
	code := strings.TrimRight(d.Lines(i), "\n")
	if code == "" {
		return false
	}
	run, ok := a.takeRun(code)
	if !ok || !runOverlaps(run, a.finder.hits, a.finder.paintMsg) {
		return false
	}
	p := a.look
	lang := blk.Language()
	if lang == "" {
		lang = "text"
	}
	parts := a.codeParts(lang, code)
	if joinStyled(parts) != code {
		parts = []styled{{text: code, st: inlStyle{code: true, color: p.CodeText}}}
	}
	spans := a.hitSpans(parts, run, a.finder.paintMsg, 12.5)
	ui.Column(c).FillWidth().MinWidth(0).Radius(radiusCode).Background(p.CodeBg).Border(1, p.CodeBorder).Clip().AlignSelf(ui.Stretch).Label("code block").Children(func() {
		ui.Row(c).Height(28).Padding(0, 4, 0, 12).Background(p.CodeHeader).AlignItems(ui.Center).Children(func() {
			ui.Text(c, lang).FontSize(11).FontWeight(500).TextColor(p.TextSecondary).Unselectable()
			ui.Spacer(c)
			btn := ui.ButtonBase(c).Padding(2, 6).Radius(5).Gap(4).Label("Copy").Cursor(ui.CursorPointer)
			btn.Children(func() {
				ui.Icon(c, iconCopy).Size(12, 12).TextColor(p.TextSecondary)
				ui.Text(c, "Copy").FontSize(11).TextColor(p.TextSecondary)
			})
			if btn.Clicked() {
				c.WriteClipboard(code)
			}
		})
		slot := a.hScrollSlot(a.hScrollKey("code", i))
		edge := scrollEdge(p.CodeBg, a.cardFlat())
		ui.Box(c).AlignSelf(ui.Stretch).Children(func() {
			ui.ScrollHorizontal(c).AlignSelf(ui.Stretch).TrackScroll(&slot.st).Children(func() {
				el := ui.RichText(c, spans...).Font("ui-monospace").FontSize(12.5).LineHeight(18.0/12.5).TextColor(p.CodeText).Padding(10, 12, 12, 12).NoWrap()
				a.markRun(el, spans, run, a.finder.paintMsg, 12, 10, ui.Font{Family: "ui-monospace", Size: 12.5}, 18.0/12.5)
			})
			hScrollFades(c, slot, "code", edge)
		})
	})
	return true
}

func (a *App) codeParts(lang, code string) []styled {
	ct := a.look.codeColors()
	var tok editor.Tokenizer
	if lang := editor.LanguageNamed(lang); lang != nil {
		tok = lang.Tokenizer
	}
	lines := strings.Split(code, "\n")
	parts := make([]styled, 0, len(lines)*2)
	var state uint64
	for i, line := range lines {
		if i > 0 {
			parts = append(parts, styled{text: "\n", st: inlStyle{code: true, color: ct.Text}})
		}
		if tok == nil || line == "" {
			if line != "" {
				parts = append(parts, styled{text: line, st: inlStyle{code: true, color: ct.Text}})
			}
			continue
		}
		tokens, next := tok.Tokenize(line, state, nil)
		state = next
		at := 0
		for _, tk := range tokens {
			end := tk.End
			if end > len(line) {
				end = len(line)
			}
			if end <= at {
				continue
			}
			parts = append(parts, styled{text: line[at:end], st: inlStyle{code: true, color: ct.TokenColor(tk.Kind)}})
			at = end
		}
		if at < len(line) {
			parts = append(parts, styled{text: line[at:], st: inlStyle{code: true, color: ct.Text}})
		}
	}
	return parts
}

func (a *App) paintTable(c *ui.Context, d *parser.Document, i int) bool {
	type cell struct {
		text   string
		header bool
		run    findRun
		ok     bool
	}
	var rows [][]cell
	hit := false
	for r := i + 1; r < d.Block(i).Next; r = d.Block(r).Next {
		row := d.Block(r)
		var line []cell
		for k := r + 1; k < d.Block(r).Next; k = d.Block(k).Next {
			if d.Block(k).Kind != parser.TableCell {
				continue
			}
			text := joinStyled(blockParts(d, k))
			item := cell{text: text, header: row.Header}
			if text != "" {
				item.run, item.ok = a.takeRun(text)
				if item.ok && runOverlaps(item.run, a.finder.hits, a.finder.paintMsg) {
					hit = true
				}
			}
			line = append(line, item)
		}
		if len(line) > 0 {
			rows = append(rows, line)
		}
	}
	if !hit {
		return false
	}
	p := a.look
	ui.Column(c).FillWidth().Border(1, p.TableRule).Radius(radiusCode).Clip().Children(func() {
		for ri, line := range rows {
			rowEl := ui.Row(c).FillWidth().AlignItems(ui.Stretch)
			if ri == 0 && line[0].header {
				rowEl.Background(p.TableHeader)
			}
			if ri > 0 {
				rowEl.BorderWidth(1, 0, 0, 0).BorderColor(p.TableRule)
			}
			rowEl.Children(func() {
				for ci, item := range line {
					cell := ui.Column(c).Grow(1).Basis(0).MinWidth(0).Padding(6, 12)
					if ci > 0 {
						cell.BorderWidth(0, 0, 0, 1).BorderColor(p.TableRule)
					}
					cell.Children(func() {
						if !item.ok {
							ui.Text(c, item.text).FontSize(14).TextColor(p.TextPrimary)
							return
						}
						parts := []styled{{text: item.text}}
						if item.header {
							parts[0].st.weight = 600
						}
						spans := a.hitSpans(parts, item.run, a.finder.paintMsg, 14)
						el := ui.RichText(c, spans...).FontSize(14).TextColor(p.TextPrimary).FillWidth().MinWidth(0)
						a.markRun(el, spans, item.run, a.finder.paintMsg, 0, 0, ui.Font{Size: 14, Weight: parts[0].st.weight}, 0)
					})
				}
			})
		}
	})
	return true
}

func (a *App) hitSpans(parts []styled, run findRun, msgID string, size float32) []ui.Span {
	p := a.look
	type seg struct {
		s, e    int
		st      inlStyle
		current bool
		on      bool
	}
	var segs []seg
	off := 0
	for _, part := range parts {
		if part.text == "" {
			continue
		}
		segs = append(segs, seg{s: off, e: off + len(part.text), st: part.st})
		off += len(part.text)
	}
	end := run.At + len(run.Text)
	for i := range a.finder.hits {
		h := &a.finder.hits[i]
		if h.MsgID != msgID || h.End <= run.At || h.Start >= end {
			continue
		}
		ls := h.Start - run.At
		le := h.End - run.At
		if ls < 0 {
			ls = 0
		}
		if le > len(run.Text) {
			le = len(run.Text)
		}
		cur := i == a.finder.cur
		var next []seg
		for _, g := range segs {
			if le <= g.s || ls >= g.e {
				next = append(next, g)
				continue
			}
			if ls > g.s {
				next = append(next, seg{g.s, ls, g.st, false, false})
			}
			a0, a1 := g.s, g.e
			if ls > a0 {
				a0 = ls
			}
			if le < a1 {
				a1 = le
			}
			next = append(next, seg{a0, a1, g.st, cur, true})
			if le < g.e {
				next = append(next, seg{le, g.e, g.st, false, false})
			}
		}
		segs = next
	}
	out := make([]ui.Span, 0, len(segs))
	text := run.Text
	for _, g := range segs {
		if g.e <= g.s || g.s < 0 || g.e > len(text) {
			continue
		}
		sp := ui.Span{Text: text[g.s:g.e]}
		if g.st.weight > 0 {
			sp.Weight = g.st.weight
		}
		sp.Italic = g.st.italic
		sp.Strikethrough = g.st.strike
		if g.st.code {
			sp.Font = "ui-monospace"
			sp.Size = 12.5
			if size > 0 && size != 14 {
				sp.Size = size * 12.5 / 14
			}
			if g.st.color.A > 0 {
				sp.Color = g.st.color
			} else {
				sp.Color = p.CodeInlineText
				sp.Background = p.CodeInlineBg
				sp.Radius = 3
			}
		}
		if g.st.link {
			sp.Color = p.TextLink
			sp.Underline = true
		}
		if g.on {
			sp.Background = p.ThemeMark.Alpha(findHL(p.Dark, g.current))
			sp.Radius = 3
		}
		out = append(out, sp)
	}
	if len(out) == 0 {
		out = append(out, ui.Span{Text: text})
	}
	return out
}

// highlightFillRects is the current-match fill, in the text's content
// coordinates. It is the same box span backgrounds paint for a radius:
// the rich layout's glyph run, outset by min(size*0.22, 3) and hugged to
// the em box. The ring is the inner stroke of these rects, so a monospace
// or bold prefix cannot slide the outline off the fill.
func highlightFillRects(spans []ui.Span, font ui.Font, width float32, text string, start, end int, lineHeight float32) []ui.Rect {
	if width < 1 || start >= end || end > len(text) || len(spans) == 0 {
		return nil
	}
	lay := ui.ShapeRichText(spans, font, width)
	sels := lay.SelectionRects(ui.TextInputRange{Start: utf16Off(text, start), End: utf16Off(text, end)})
	size, face := spanFaceAt(spans, font, start)
	m := face.Metrics()
	const radius = float32(3)
	px := size * 0.22
	if px > radius {
		px = radius
	}
	// The paragraph's line height is a multiple of the font size. Shaping
	// here uses the font's own spacing, so a wrapped match's later lines
	// sit too high unless each segment is moved down by the difference.
	// Line 0 is already on the fill. One segment per shaped line.
	shapedH := float32(0)
	elemH := float32(0)
	if len(sels) > 0 {
		shapedH = sels[0].H
	}
	if lineHeight > 0 && size > 0 {
		elemH = lineHeight * size
	}
	var out []ui.Rect
	for _, sel := range sels {
		if sel.W < 0.5 || sel.H < 0.5 {
			continue
		}
		ascent, descent := m.Ascent, m.Descent
		baseline := sel.Y + (sel.H-ascent-descent)/2 + ascent
		if ascent+descent > sel.H && sel.H > 0 {
			baseline = sel.Y + ascent*sel.H/(ascent+descent)
		}
		rect := ui.Rect{
			X: sel.X - px,
			Y: baseline - size*0.98,
			W: sel.W + 2*px,
			H: size * 1.32,
		}
		if elemH > shapedH+0.5 && shapedH > 0.5 {
			line := int(sel.Y/shapedH + 0.05)
			rect.Y += float32(line) * (elemH - shapedH)
		}
		out = append(out, rect)
	}
	return out
}

// spanFaceAt is the font of the span holding byteOff of the joined span
// text. A zero field inherits base, as RichText does.
func spanFaceAt(spans []ui.Span, base ui.Font, byteOff int) (float32, ui.Font) {
	face := base
	if face.Size == 0 {
		face.Size = 14
	}
	off := 0
	for _, sp := range spans {
		n := len(sp.Text)
		if byteOff < off+n || n == 0 && byteOff == off {
			if sp.Size > 0 {
				face.Size = sp.Size
			}
			if sp.Font != "" {
				face.Family = sp.Font
			}
			if sp.Weight > 0 {
				face.Weight = sp.Weight
			}
			if sp.Italic {
				face.Italic = true
			}
			return face.Size, face
		}
		off += n
	}
	return face.Size, face
}

func utf16Off(s string, byteOff int) int {
	if byteOff < 0 {
		byteOff = 0
	}
	if byteOff > len(s) {
		byteOff = len(s)
	}
	return len(utf16.Encode([]rune(s[:byteOff])))
}

func (a *App) markRun(el ui.Element, spans []ui.Span, run findRun, msgID string, padX, padY float32, font ui.Font, lineHeight float32) {
	if !el.Valid() {
		return
	}
	var curStart, curEnd int
	found := false
	for i := range a.finder.hits {
		h := &a.finder.hits[i]
		if i != a.finder.cur || h.MsgID != msgID {
			continue
		}
		if h.End <= run.At || h.Start >= run.At+len(run.Text) {
			continue
		}
		curStart = h.Start - run.At
		curEnd = h.End - run.At
		if curStart < 0 {
			curStart = 0
		}
		if curEnd > len(run.Text) {
			curEnd = len(run.Text)
		}
		found = true
		break
	}
	if !found {
		return
	}
	text := run.Text
	stroke := a.look.ThemeMark.Alpha(0.55)
	// User bubbles have no column flash: this DrawOver is their single
	// 14%/8% wash. Assistant runs already flash the whole reply from
	// paintFlash; a second outset here stacked to ~26% light / ~15% dark.
	flashUser := run.Kind == "user"
	el.DrawOver(func(pn *ui.Painter, r ui.Rect) {
		if flashUser && a.finder.flashID == msgID && !a.finder.flashAt.IsZero() && !a.reduceMotion {
			u := float32(a.finder.frameNow.Sub(a.finder.flashAt)) / float32(findFlashDur)
			if u < 0 {
				u = 0
			}
			if u < 1 {
				left := (1 - u) * (1 - u)
				pn.Fill(ui.Rect{X: r.X - 8, Y: r.Y - 8, W: r.W + 16, H: r.H + 16}, a.look.ThemeMark.Alpha(findFlashA(a.look.Dark)*left), 10)
			}
		}
		w := r.W - padX*2
		if w < 1 {
			w = r.W
		}
		// The fill is the span background. These are those rects, so the
		// 1px inner stroke lands on the fill instead of a second layout.
		for _, gr := range highlightFillRects(spans, font, w, text, curStart, curEnd, lineHeight) {
			box := ui.Rect{X: r.X + padX + gr.X, Y: r.Y + padY + gr.Y, W: gr.W, H: gr.H}
			if box.W < 1 || box.H < 1 {
				continue
			}
			a.finder.markX, a.finder.markY, a.finder.markW, a.finder.markH = box.X, box.Y, box.W, box.H
			a.finder.markFresh = true
			pn.Stroke(ui.Rect{X: box.X + 0.5, Y: box.Y + 0.5, W: box.W - 1, H: box.H - 1}, stroke, 3, 1)
		}
	})
}

func (a *App) thinkingFind(c *ui.Context, m *Message, text string) bool {
	if !a.finder.open || a.finder.query == "" || !m.ReasoningOpen {
		return false
	}
	doc := docByID(a.finder.docs, m.ID)
	if doc == nil {
		return false
	}
	var run findRun
	ok := false
	for _, r := range doc.Runs {
		if r.Kind == "think" && r.Text == text {
			run = r
			ok = true
			break
		}
	}
	if !ok || !runOverlaps(run, a.finder.hits, m.ID) {
		return false
	}
	parts := []styled{{text: text}}
	spans := a.hitSpans(parts, run, m.ID, 12)
	el := ui.RichText(c, spans...).FontSize(12).LineHeight(20.0 / 12.0).TextColor(a.look.ThinkText).MaxLines(3).Grow(1).MinWidth(0).Selectable()
	a.markRun(el, spans, run, m.ID, 0, 0, ui.Font{Size: 12}, 20.0/12.0)
	return true
}

// findMenuItems are Edit › Find, Find Next, Find Previous.
func (a *App) findMenuItems(act func(func()) func(*mygo.MenuItem, *mygo.Window)) []*mygo.MenuItem {
	find := &mygo.MenuItem{
		ID: "find", Label: "Find…", Accelerator: "CmdOrCtrl+F",
		Click: act(a.openFind),
	}
	next := &mygo.MenuItem{
		ID: "find-next", Label: "Find Next", Accelerator: "CmdOrCtrl+G", Disabled: true,
		Click: act(func() { a.findStep(1, time.Time{}) }),
	}
	prev := &mygo.MenuItem{
		ID: "find-prev", Label: "Find Previous", Accelerator: "Shift+CmdOrCtrl+G", Disabled: true,
		Click: act(func() { a.findStep(-1, time.Time{}) }),
	}
	a.findItem = find
	a.findNextItem = next
	a.findPrevItem = prev
	return []*mygo.MenuItem{find, next, prev}
}
