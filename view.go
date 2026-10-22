package main

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"cove/themes"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// starter is one empty-state card: a short title, a second line, and the
// prompt a click drops into the composer.
type starter struct {
	Title, Detail, Prompt string
	Icon                  *ui.SVG
}

var starters = []starter{
	{
		Title:  "Explain a tricky idea",
		Detail: "Short paragraphs, the hard part named.",
		Prompt: "Explain a tricky idea in a few short paragraphs.",
		Icon:   iconBulb,
	},
	{
		Title:  "Outline a plan",
		Detail: "Steps first, then the tradeoffs.",
		Prompt: "Help me outline a plan, and name the tradeoffs.",
		Icon:   iconList,
	},
	{
		Title:  "Review a draft",
		Detail: "Gaps and edits, line by line.",
		Prompt: "Review what I have and point out what I might be missing.",
		Icon:   iconPen,
	},
	{
		Title:  "Compare two approaches",
		Detail: "A table, then a recommendation.",
		Prompt: "Compare two approaches in a short table, then recommend one.",
		Icon:   iconCode,
	},
}

// titleBarHeight is the hidden inset title bar. The sidebar's top row and
// the chat toolbar are this tall, so the traffic lights sit in that band
// instead of a separate chrome strip. Headless tests report no title-bar
// inset; the row is still this tall so the layout matches the window.
const titleBarHeight = 52

// sideRow is one sidebar line: a date heading, or a conversation by index
// into the store.
type sideRow struct {
	header string
	index  int
}

func (a *App) view(c *ui.Context) {
	a.inRender = true
	defer func() { a.inRender = false }()
	// modelMenuSeen is set by the composer menu while its popup is open.
	// A pass that builds no open menu drops the chat recorded for it, so
	// the next chat's checks cannot inherit the previous one. Early
	// returns (settings, state boards) count as closed.
	defer func() {
		if !a.modelMenuSeen {
			a.modelMenuChat = ""
		}
		a.modelMenuSeen = false
	}()
	a.trackWindowFocus(c)
	a.applyLook(c)
	a.md.OnLink = func(url string) { c.OpenURL(url) }
	if a.instant {
		a.md.Reveal = &markdown.Reveal{Unit: markdown.AtOnce, Fade: -1}
	}
	a.pollFind()
	a.finder.frameNow = c.Now()
	if a.finder.flashPending {
		a.finder.flashAt = c.Now()
		a.finder.flashPending = false
	}
	a.syncFindQuery(c.Now())
	if !a.instant && a.finder.open && a.finder.draft != a.finder.appliedDraft {
		wait := findDebounce - c.Now().Sub(a.finder.dirtyAt)
		if wait > 0 {
			c.After(wait)
		}
	}
	a.syncNewSessionMenu()
	a.syncFindMenu()
	c.Root().HandleInput(a.noteQuotePointer)
	a.shortcuts(c)
	a.settleComposerFocus()
	if a.announce != "" {
		c.Announce(a.announce)
		a.announce = ""
	}
	for id, until := range a.copied {
		if c.Now().Before(until) {
			c.After(until.Sub(c.Now()))
		} else {
			delete(a.copied, id)
		}
	}

	// gorex paints bgBottom under paintBackground. The gradient covers the
	// whole window, title bar included. Nothing is a vibrancy material.
	look := a.look
	c.Root().Background(look.WindowBaseBottom).Draw(func(p *ui.Painter, r ui.Rect) {
		a.paintBackdrop(p, r)
	})
	if a.shotStates {
		a.statesBoard(c)
		a.veilScrollbar(c)
		a.finishQuotePointer()
		return
	}
	if a.shotPalette {
		a.paletteBoard(c)
		a.veilScrollbar(c)
		a.finishQuotePointer()
		return
	}
	winW, _ := c.Size()
	if a.settingsOpen && a.prefs == nil {
		a.settingsBody(c)
		a.confirmDialog(c)
		a.veilScrollbar(c)
		a.settleComposerFocus()
		a.finishQuotePointer()
		return
	}
	if !a.openedFocus {
		a.openedFocus = true
		if conv := a.active(); conv != nil {
			a.ensureChat(conv.ID)
		}
		if conv := a.active(); untouched(conv) {
			a.requestComposerFocus(conv.ID)
		}
	}
	a.settleComposerFocus()
	// The grip is an absolute sibling of the panes, built first so a drag
	// updates the width before this frame lays them out. It sits above the
	// card, the composer veil, the card shadow, and the title-bar
	// window-drag rows. The title bar itself is not part of the strip.
	a.sidebarGrip(c)
	shown := sidebarShown(a.sidebarWidth, winW)
	ui.Row(c).Fill().Children(func() {
		ui.Column(c).Width(shown).FillHeight().Clip().Children(func() { a.sidebar(c) })
		ui.Column(c).Grow(1).MinWidth(0).FillHeight().Clip().Children(func() { a.chatPane(c) })
	})

	a.finishQuotePointer()
	a.quoteBarView(c)
	a.trafficPlaceholders(c)
	a.renameModal(c)
	a.settingsModal(c)
	a.confirmDialog(c)
	a.requestModal(c)
	a.veilScrollbar(c)
	a.noteKeyboardOwner()
	a.noteComposerCaret()
	// A request the composer did not take (no field, or a sheet opened
	// over it in this frame) does not fire on a later frame.
	a.focusComposerID = ""
}

// paintWindowBackground lives in gradient.go. It draws one cached bitmap
// of the OKLab ramp, not a stack of GPU gradients.

func windowVertical(look Theme) ui.LinearGradient {
	return ui.LinearGradient{
		Angle: 180,
		Stops: []ui.GradientStop{
			{Color: look.WindowBaseTop, At: 0},
			{Color: look.WindowBaseMid, At: 0.38},
			{Color: look.WindowBaseLow, At: 0.70},
			{Color: look.WindowBaseBottom, At: 1},
		},
	}
}

func sidebarFor(winW float32) float32 {
	return sidebarShown(sidebarW, winW)
}

func (a *App) shortcuts(c *ui.Context) {
	a.suppressEditEscape = false
	a.finder.enterHandled = false
	if a.acksOpen {
		if c.Shortcut(0, ui.KeyEscape) {
			a.acksOpen = false
		}
		return
	}
	if a.confirmKind != "" {
		if c.Shortcut(0, ui.KeyEscape) || c.Shortcut(0, ui.KeyEnter) {
			a.dismissConfirm()
			a.suppressEditEscape = true
		}
		if c.Shortcut(ui.Cmd, ui.KeyBackspace) {
			a.acceptConfirm()
		}
		return
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyN) {
		if a.CanInsertBreak() {
			a.InsertSessionBreak()
			if a.composerField.Valid() {
				a.composerField.Focus()
			}
		}
	} else if c.Shortcut(ui.Cmd, ui.KeyN) {
		a.newChat()
	}
	if c.Shortcut(ui.Cmd|ui.Alt, ui.KeyComma) {
		a.openSettings()
	} else if c.Shortcut(ui.Cmd, ui.KeyComma) {
		a.openDefaults()
	}
	if c.Shortcut(ui.Cmd, ui.KeyBackspace) {
		a.askDelete()
	}
	if c.Shortcut(ui.Cmd, ui.KeyF) {
		a.openFind()
	}
	if a.finder.open {
		switch {
		case c.Shortcut(ui.Cmd|ui.Shift, ui.KeyG):
			a.findStep(-1, c.Now())
		case c.Shortcut(ui.Cmd, ui.KeyG):
			a.findStep(1, c.Now())
		}
		if c.Shortcut(0, ui.KeyEscape) {
			a.closeFind()
			a.suppressEditEscape = true
		}
	}
	if a.finder.open && a.finder.focused {
		switch {
		case c.Shortcut(ui.Shift, ui.KeyEnter):
			a.finder.enterHandled = true
			a.findStep(-1, c.Now())
		case c.Shortcut(0, ui.KeyEnter):
			a.findCommitEnter()
		case c.Shortcut(0, ui.KeyUp):
			a.findMove(-1)
		case c.Shortcut(0, ui.KeyDown):
			a.findMove(1)
		}
	} else if c.Shortcut(0, ui.KeyUp) {
		if conv := a.active(); conv != nil {
			if id := latestUserID(conv); id != "" {
				a.editMessage(conv.ID, id)
			}
		}
	}
	if c.Shortcut(ui.Cmd, ui.KeyR) {
		a.regenerate()
	}
	if c.Shortcut(ui.Cmd, ui.KeyPeriod) {
		a.stop()
	}
	if c.Shortcut(ui.Cmd, ui.KeyL) {
		a.addSelectionToComposer()
	}
	if a.quoteBarOpen && c.Shortcut(0, ui.KeyEscape) {
		a.dismissQuoteBar()
		a.suppressEditEscape = true
	}
	if a.edit.convID != "" && c.Shortcut(0, ui.KeyEscape) {
		a.cancelEdit()
		a.suppressEditEscape = true
	}
}

func (a *App) sidebar(c *ui.Context) {
	p := a.look
	a.rebuildSide(c.Now())
	// No tint and no separator. The sidebar is the window background.
	side := ui.Column(c).Fill()
	a.fadeColors(side)
	side.Children(func() {
		a.sidebarTitle(c)
		ui.Column(c).Padding(0, 10, 6, 10).Children(func() {
			a.searchField(c)
		})
		rows := a.sideRows
		a.side.Key = func(i int) any {
			if i < 0 || i >= len(a.sideRows) {
				return i
			}
			row := a.sideRows[i]
			if row.header != "" {
				return "h:" + row.header
			}
			if row.index < 0 || row.index >= len(a.data.Conversations) {
				return i
			}
			return a.data.Conversations[row.index].ID
		}
		a.side.Label = func(i int) string {
			if i < 0 || i >= len(a.sideRows) {
				return ""
			}
			row := a.sideRows[i]
			if row.header != "" {
				return row.header
			}
			if row.index < 0 || row.index >= len(a.data.Conversations) {
				return ""
			}
			return a.data.Conversations[row.index].Title
		}
		a.side.Header = func(i int) bool {
			return i >= 0 && i < len(a.sideRows) && a.sideRows[i].header != ""
		}
		ui.List(c, &a.side, len(rows), func(i int) {
			if i < 0 || i >= len(a.sideRows) {
				return
			}
			a.sidebarRow(c, a.sideRows[i])
		}).Grow(1).Padding(0, 10, 8, 10).Gap(1).Children(func() {
			if len(a.data.Conversations) == 0 {
				ui.Text(c, "No chats yet").TextColor(p.TextSecondary).Padding(space3, space2)
			} else if len(a.sideRows) == 0 {
				ui.Text(c, "No matching chats").TextColor(p.TextSecondary).Padding(space3, space2)
			}
		})
		ui.Column(c).Padding(6, 10, 10, 10).Children(func() {
			gear := ui.ButtonBase(c).Height(rowHeight).FillWidth().Gap(8).Padding(0, 10).Radius(radiusRow).AlignItems(ui.Center).Label("Settings").Cursor(ui.CursorPointer)
			if gear.Hovered() {
				gear.Background(p.FillHover)
			}
			a.fadeColors(gear)
			if gear.Clicked() {
				a.openDefaults()
			}
			gear.Children(func() {
				ui.Icon(c, iconGear).Size(16, 16).TextColor(p.TextSecondary)
				ui.Text(c, "Settings").FontSize(13).TextColor(p.TextSecondary).Grow(1)
				shortcut := controlLine(c, "⌘,", 11, 0, 400, p.TextSecondary)
				if dy := textBaselineDelta(13, 400, 11, 400); dy != 0 {
					shortcut.Translate(0, dy)
				}
			})
		})
	})
}

func (a *App) rebuildSide(now time.Time) {
	q := strings.ToLower(strings.TrimSpace(a.query))
	rows := make([]sideRow, 0, len(a.data.Conversations)*2)
	last := ""
	for i := range a.data.Conversations {
		conv := &a.data.Conversations[i]
		if q != "" {
			blob := strings.ToLower(conv.Title + " " + conv.Config.Model)
			if !strings.Contains(blob, q) {
				continue
			}
		}
		when := conv.UpdatedAt
		if when.IsZero() {
			when = conv.CreatedAt
		}
		bucket := dateBucket(when, now)
		if bucket != last {
			rows = append(rows, sideRow{header: bucket, index: -1})
			last = bucket
		}
		rows = append(rows, sideRow{index: i})
	}
	a.sideRows = rows
}

// showsDraft reports a chat the sidebar should mark Draft. An index row
// uses the draft flag; a loaded chat uses its message slice.
func showsDraft(c *Conversation) bool {
	if c == nil {
		return false
	}
	if c.Messages != nil || c.Loaded {
		return len(c.Messages) == 0
	}
	return c.DraftOnly
}

func (a *App) sidebarRow(c *ui.Context, row sideRow) {
	p := a.look
	if row.header != "" {
		line := ui.Row(c).FillWidth().Padding(12, 8, 4, 8)
		line.Children(func() {
			ui.Text(c, row.header).FontSize(11).FontWeight(600).TextColor(p.TextSecondary).SingleLine()
		})
		return
	}
	if row.index < 0 || row.index >= len(a.data.Conversations) {
		return
	}
	conv := &a.data.Conversations[row.index]
	id := conv.ID
	selected := id == a.data.ActiveID
	line := ui.Row(c).Height(rowHeight).FillWidth().Gap(space2).Padding(0, space2).Radius(radiusRow).AlignItems(ui.Center)
	switch {
	case selected:
		line.Background(p.SelectionRow)
	case line.Hovered():
		line.Background(p.FillHover)
	}
	// While a theme switch is running, suppressFade is set and this hover
	// transition stays off, so the row tracks the same mix as the preview
	// instead of chasing it on the 120ms hover clock.
	if !a.instant && !a.reduceMotion && !a.suppressFade {
		line.Transition(ui.ElementTransition{Colors: true, Duration: motionHoverRow, Ease: easeStandard})
	}
	if line.Clicked() {
		a.selectChat(id)
	}
	line.Cursor(ui.CursorPointer)
	line.ContextMenu(func(m *ui.Menu) {
		if m.Item("Rename").Chosen() {
			a.selectChat(id)
			a.beginRename()
		}
		if m.Item("Delete").Chosen() {
			a.selectChat(id)
			a.askDelete()
		}
	})
	when := conv.UpdatedAt
	if when.IsZero() {
		when = conv.CreatedAt
	}
	line.Children(func() {
		dot := ui.Box(c).Size(6, 6).Radius(3).Shrink(0)
		if a.isStreaming(id) {
			dot.Background(p.ThemeMark).Opacity(a.pulse(c, 0, motionDots, dotsMin, 1))
		} else {
			dot.Background(a.configColor(conv.Config))
		}
		title := ui.Text(c, sidebarTitle(conv)).FontSize(13).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		if selected {
			title.FontWeight(500)
		}
		// Every row shows the time. Streaming replaces it with "Generating".
		// The model name stays in the title bar, not in this meta slot.
		meta := sidebarWhen(c.Now(), when)
		if showsDraft(conv) {
			meta = "Draft"
		}
		if a.isStreaming(id) {
			meta = "Generating"
		}
		if meta != "" {
			ui.Text(c, meta).FontSize(11).TextColor(p.TextSecondary).Shrink(0).SingleLine()
		}
	})
}

func (a *App) chatPane(c *ui.Context) {
	if id := a.data.ActiveID; id != "" {
		a.ensureChat(id)
	}
	conv := a.active()
	if a.finder.open {
		a.scheduleFind(conv)
	}
	draft := ""
	var quotes []Quote
	if conv != nil {
		draft = conv.Draft
		quotes = conv.DraftQuotes
	}
	maxQuoteH := float32(quoteCardMax)
	if a.cardContentWidth(c) < 560 {
		maxQuoteH = quoteCardMaxShort
	}
	a.composerH = composerHeight(draft, quotes, maxQuoteH, a.quoteTextWidth(c, quoteCardComposer))
	if conv != nil && (conv.SwitchNote != "" || a.addProviderNote == conv.ID || a.quoteLimitActive(c, conv)) {
		a.composerH += 34
	}
	pane := ui.Column(c).Fill()
	pane.Children(func() {
		if a.loadError != "" {
			a.banner(c, a.loadError)
			if a.saveBlocked && a.sheetButton(c, "Start fresh", false).Clicked() {
				a.allowSave()
			}
		}
		if a.saveError != "" {
			a.banner(c, "Could not save chats. "+a.saveError)
		}
		a.titleBar(c, conv)
		// Flush under the title bar, 8pt from the right and the bottom.
		// radius/contentCard is 22. The 1pt border's inner corner is 21,
		// and that is what clips the fade and the find bar. The left edge
		// meets the sidebar. The shadow stays inside the 8pt margin.
		p := a.look
		card := ui.Box(c).Grow(1).Margin(0, 8, 8, 0).Radius(radiusContentCard).
			Background(p.LayerContent).Border(1, p.LayerContentBorder).
			Clip().Label("content card")
		contentCardShadow(card, p)
		card.Children(func() {
			clip := ui.Box(c).Absolute().Left(0).Right(0).Top(0).Bottom(0).Radius(radiusContentClip).Clip()
			clip.Children(func() {
				if conv == nil {
					a.welcome(c)
					a.clearHistorySelection()
				} else if len(conv.Messages) == 0 {
					a.emptyTranscript(c, conv)
					a.clearHistorySelection()
				} else {
					a.transcript(c, conv)
				}
				if conv == nil {
					return
				}
				if a.veilProbe != nil {
					ui.Box(c).Absolute().Left(0).Right(0).Top(0).Bottom(0).Background(*a.veilProbe).Label("veil probe")
				}
				a.composerFade(c)
				if a.edit.convID == "" {
					a.jumpButton(c, conv)
				}
				a.composer(c, conv)
				a.findChrome(c, conv)
				if a.edit.convID == conv.ID {
					// Clicks on the disabled composer are outside the editor.
					_, bottom := a.composerInsets(c)
					lock := ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(a.composerH + bottom).Label("composer lock")
					if lock.Clicked() {
						a.cancelEdit()
					}
				}
			})
		})
	})
}

// composerFade is the click-through veil across the whole content card,
// from 28pt above the composer to the card bottom. Each point is the
// content card composited on the window there, and the strength is the
// SPEC curve: clear at the top, 50% at the composer's top edge, still 50%
// until 10pt above the card bottom, then 70% at the card bottom. Strength
// does not change across the row, so text beside the composer is
// suppressed exactly as much as text behind it at that y. There is no
// second plate under the composer. The composer's own fill and shadow
// are painted later, on top.
func (a *App) composerFade(c *ui.Context) {
	winW, winH := c.Size()
	from, to, fading, waiting, u, swapping, swapU := a.veilEnds(c.Now())
	phase := veilPaint{to: to, fading: fading, waiting: waiting, u: u, swapping: swapping, swapU: swapU, note: a.noteVeil, base: a.veilStack}
	if a.instant || a.reduceMotion {
		phase.note = nil
	}
	_, bottom := a.composerInsets(c)
	h := a.composerH + bottom + float32(composerFade)
	ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(h).PassThrough().Label("composer fade").Draw(func(painter *ui.Painter, r ui.Rect) {
		paintCardVeil(painter, r, winW, winH, from, veilFade, a.composerH, func(y float32) float32 {
			return veilAlphaAt(r, a.composerH, bottom, y)
		}, phase)
	})
}

// veilAlphaAt is the fade's opacity at window y: 0 at 28pt above the
// composer, 0.50 at the composer's top, 0.50 until 10pt above the card
// bottom, then 0.70 at the card bottom.
func veilAlphaAt(r ui.Rect, composerH, bottom, y float32) float32 {
	cardBottom := r.Y + r.H
	compBottom := cardBottom - bottom
	compTop := compBottom - composerH
	fadeTop := compTop - float32(composerFade)
	knee := cardBottom - 10
	switch {
	case y <= fadeTop || composerFade <= 0:
		return 0
	case y < compTop:
		return 0.50 * ((y - fadeTop) / float32(composerFade))
	case y < knee:
		return 0.50
	case y < cardBottom:
		span := cardBottom - knee
		if span < 1 {
			return 0.70
		}
		return 0.50 + 0.20*((y-knee)/span)
	default:
		return 0.70
	}
}

func setAlpha(c ui.Color, a float32) ui.Color {
	if a <= 0 {
		c.A = 0
		return c
	}
	if a >= 1 {
		c.A = 255
		return c
	}
	c.A = uint8(a*255 + 0.5)
	return c
}

// veilScrollbar hides the overlay thumb while a list is idle, including
// every settled screenshot. MyGo paints that thumb whenever the list is
// hovered, in the theme color, so an idle frame sets the color clear.
// A scroll that changes which rows are visible, or moves the settings
// sheet, uses a translucent thumb for a short run of frames.
func (a *App) veilScrollbar(c *ui.Context) {
	th := c.Theme()
	if th == nil {
		return
	}
	cf, cl := a.chat.Visible()
	a.noteListScroll(&a.barReady, &a.barFirst, &a.barLast, cf, cl)
	sf, sl := a.side.Visible()
	a.noteListScroll(&a.sideBarReady, &a.sideBarFirst, &a.sideBarLast, sf, sl)
	if !a.formBarReady {
		a.formBarReady = true
		a.formSeenY = a.formScroll.Y
	} else if a.formScroll.Y != a.formSeenY {
		a.barFrames = scrollbarHold
		a.formSeenY = a.formScroll.Y
	}
	if !a.emptyBarReady {
		a.emptyBarReady = true
		a.emptySeenY = a.emptyScroll.Y
	} else if a.emptyScroll.Y != a.emptySeenY {
		a.barFrames = scrollbarHold
		a.emptySeenY = a.emptyScroll.Y
	}
	if a.barFrames > 0 {
		a.barFrames--
		th.Scrollbar = themes.ScrollbarThumb(a.look.Dark)
		if a.barFrames > 0 {
			c.AnimationFrame()
		}
		return
	}
	th.Scrollbar = ui.Transparent
}

// noteListScroll records which rows a list showed. The first reading, and
// the jump from "nothing laid out" (0, -1) to the real rows, is not a scroll.
func (a *App) noteListScroll(ready *bool, first, last *int, curFirst, curLast int) {
	if !*ready {
		*ready = true
		*first, *last = curFirst, curLast
		return
	}
	if curFirst == *first && curLast == *last {
		return
	}
	if !(*first == 0 && *last < 0) {
		a.barFrames = scrollbarHold
	}
	*first, *last = curFirst, curLast
}

func (a *App) jumpButton(c *ui.Context, conv *Conversation) {
	if conv == nil || len(conv.Messages) == 0 || a.chat.AtEnd() || a.edit.convID != "" {
		return
	}
	p := a.look
	_, bottom := a.composerInsets(c)
	ui.Row(c).Absolute().Left(0).Right(0).Bottom(a.composerH + bottom + float32(jumpGap)).Height(jumpH).Justify(ui.Center).PassThrough().Children(func() {
		// The shadow is a sibling plate drawn first, so it sits behind the
		// pill. MyGo's Shadow would quantize it into bands.
		plate := ui.Box(c)
		plate.Draw(func(painter *ui.Painter, r ui.Rect) {
			paintDropShadow(painter, r, radiusPill, shadowJump, p.ShadowMd)
		})
		plate.Children(func() {
			btn := ui.ButtonBase(c).Height(jumpH).MinHeight(jumpH).Gap(5).Padding(0, 10, 0, 8).Radius(radiusPill).Background(p.LayerOverlay).Border(0.5, p.LayerOverlayBorder).AlignItems(ui.Center).Label("Jump to latest").Cursor(ui.CursorPointer)
			if btn.Hovered() {
				btn.Background(p.StopGlyph)
			}
			btn.Children(func() {
				ui.Icon(c, iconDown).Size(13, 13).TextColor(p.TextSecondary)
				controlText(c, "Jump to latest", 12, 0, 500, p.TextPrimary)
			})
			if btn.Clicked() {
				a.chat.ScrollToEnd()
			}
		})
	})
}

func (a *App) banner(c *ui.Context, text string) {
	p := a.look
	ui.Text(c, text).Selectable().TextColor(p.StatusDanger).Padding(space2, space4).Background(p.StatusDanger.Alpha(0.08))
}

func (a *App) welcome(c *ui.Context) {
	p := a.look
	_, winH := c.Size()
	// MinHeight keeps the column centered in the card when the grid is
	// short, and lets it grow so a stacked grid scrolls instead of being
	// cut off by the card.
	ui.Scroll(c).Fill().TrackScroll(&a.emptyScroll).Children(func() {
		ui.Column(c).FillWidth().MinHeight(winH - titleBarHeight - 8).Center().Padding(space8).Gap(space4).AlignItems(ui.Center).Children(func() {
			a.appIcon(c, 64)
			ui.Text(c, "Cove").FontSize(28).FontWeight(600)
			ui.Text(c, "A native chat for any OpenAI-compatible model.").TextColor(p.TextSecondary).TextAlign(ui.Center).MaxWidth(420).FillWidth().MinWidth(0)
			a.starterGrid(c, space8)
		})
	})
}

func sidebarTitle(conv *Conversation) string {
	if conv == nil {
		return ""
	}
	if len(conv.Messages) == 0 {
		if strings.TrimSpace(conv.Draft) != "" {
			return draftTitle(conv.Draft)
		}
		return newChatTitle
	}
	return conv.Title
}

// mark is the assistant avatar on each message, and the small sample of
// that avatar on the palette and states boards. It follows the theme:
// a rounded square (r6 at 22, r12 from 40 up) with the chat glyph in the
// on-accent color. The empty-state hero and Settings › About use appIcon.
func (a *App) mark(c *ui.Context, size float32) {
	p := a.look
	radius := float32(6)
	if size >= 40 {
		radius = 12
	}
	ui.Row(c).Size(size, size).Radius(radius).Background(p.ThemeMark).Center().Children(func() {
		ui.Icon(c, iconMark).Size(size*0.62, size*0.62).TextColor(p.TextOnAccent)
	})
}

func (a *App) sidebarTitle(c *ui.Context) {
	// The hidden-inset traffic lights own the left of this band (about
	// x 12–72). Compose stays at the trailing edge. There is no sidebar
	// collapse control: nothing in the app has ever hidden the sidebar.
	ui.Row(c).Height(titleBarHeight).FillWidth().Padding(0, 10).AlignItems(ui.Center).DragWindow().Children(func() {
		ui.Spacer(c)
		btn := a.iconButtonSize(c, "new-chat", newChatTitle, iconCompose, iconHit)
		a.tip(c, btn, newChatTitle+" (⌘N)")
		if btn.Clicked() {
			a.newChat()
		}
	})
}

func (a *App) trafficPlaceholders(c *ui.Context) {
	bar := c.TitleBar()
	if bar.Left > 0 || bar.Height > 0 {
		return
	}
	// Stand-ins for the real buttons, which occupy roughly x 12–72.
	// 12pt circles at a 24pt stride: 12–24, 36–48, 60–72.
	colors := themes.TrafficLights()
	if !a.look.Focused {
		gray := themes.TrafficInactive(a.look.Dark)
		colors = []ui.Color{gray, gray, gray}
	}
	for i, col := range colors {
		ui.Box(c).Absolute().Left(float32(20+i*20)).Top(20).Size(12, 12).Radius(6).Background(col)
	}
}

// titleBar is the chat pane's toolbar, drawn under the hidden title bar.
// Rare actions live in the More menu. Regenerate and Settings stay icon
// buttons so they remain one click away.
func (a *App) titleBar(c *ui.Context, conv *Conversation) {
	p := a.look
	inset := c.TitleBar()
	right := float32(12)
	if inset.Right > 0 {
		right = inset.Right + 8
	}
	row := ui.Row(c).Height(titleBarHeight).FillWidth().Padding(0, right, 0, 16).Gap(2).AlignItems(ui.Center).DragWindow()
	row.Children(func() {
		if conv == nil {
			ui.Spacer(c)
			return
		}
		ui.Column(c).Gap(2).Grow(1).MinWidth(0).Clip().Children(func() {
			title := ui.Text(c, conv.Title).FontSize(14).FontWeight(600).SingleLine().Ellipsis("…").FillWidth().MinWidth(0)
			if !p.Focused {
				title.TextColor(p.TextSecondary)
			}
			ui.Row(c).Gap(6).AlignItems(ui.Center).FillWidth().Children(func() {
				ui.Box(c).Size(6, 6).Radius(3).Background(a.configColor(conv.Config)).Shrink(0)
				ui.Text(c, a.subtleModel(conv)).FontSize(11).TextColor(p.TextSecondary).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
			})
		})
		regen := a.iconButton(c, "toolbar-regen", "Regenerate", iconRefresh).Disabled(!a.canRegenerate())
		if !a.canRegenerate() {
			regen.Opacity(0.4)
		}
		if regen.Clicked() {
			a.regenerate()
		}
		more := a.iconButton(c, "toolbar-more", "More", iconEllipsis)
		a.moreMenu(c, more)
	})
}

func (a *App) iconButton(c *ui.Context, key, label string, glyph *ui.SVG) ui.Element {
	return a.tip(c, a.iconButtonSize(c, key, label, glyph, iconHit), label)
}

func (a *App) iconButtonSize(c *ui.Context, key, label string, glyph *ui.SVG, size float32) ui.Element {
	p := a.look
	b := ui.ButtonBase(c.Key(key)).Size(size, size).Radius(radiusRow).Center().Label(label).Cursor(ui.CursorPointer)
	switch {
	case b.Pressed():
		b.Background(p.FillPressed)
	case b.Hovered():
		b.Background(p.FillHover)
	}
	if !a.instant && !a.reduceMotion {
		b.Transition(ui.ElementTransition{Colors: true, Duration: motionHoverButton, Ease: easeStandard})
	}
	b.Children(func() {
		color := p.TextSecondary
		if b.Hovered() {
			color = p.TextPrimary
		}
		ui.Icon(c, glyph).Size(16, 16).TextColor(color)
	})
	return b
}

// tip shows s in an overlay tooltip with the 0.5pt overlay border.
// Platform menus are drawn by the system and cannot take that stroke.
func (a *App) tip(c *ui.Context, el ui.Element, s string) ui.Element {
	if s == "" || !el.Valid() {
		return el
	}
	p := a.look
	ui.TooltipBase(c, el, func(tip ui.Element) {
		tip.MaxWidth(360).Padding(3, 7).Radius(4).Background(p.LayerOverlay).Border(0.5, p.LayerOverlayBorder).TextColor(p.TextPrimary)
		tip.Shadow(0, 8, 24, 0, p.ShadowMd)
		tip.Children(func() {
			controlLine(c, s, 11, 0, 400, p.TextPrimary)
		})
	})
	return el
}

func (a *App) providerChip(c *ui.Context, conv *Conversation) {
	p := a.look
	label := shortModel(conv.Config.Model)
	if label == "" || label == "Model" {
		return
	}
	ui.Row(c).Gap(6).Padding(3, 8).Radius(radiusPill).Background(p.FillHover).AlignItems(ui.Center).Shrink(0).Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).Background(a.configColor(conv.Config))
		controlText(c, label, 12, 0, 500, p.TextSecondary)
	})
}

// modelMenuSettings and modelMenuAdd are popup values for the trailing
// action. They are not model ids.
const (
	modelMenuSettings = "\x1esettings"
	modelMenuAdd      = "\x1eadd"
)

func (a *App) moreMenu(c *ui.Context, anchor ui.Element) {
	a.bindPopover(c, "title-more", anchor, popSpec{
		Name: "More",
		Items: []popEntry{
			{Kind: popItem, ID: "settings", Text: "Chat Settings…"},
			{Kind: popItem, ID: "rename", Text: "Rename"},
			{Kind: popItem, ID: "delete", Text: "Delete chat"},
			{Kind: popSep},
			{Kind: popItem, ID: "request", Text: "Last request"},
		},
		OnSelect: func(id string) {
			switch id {
			case "settings":
				a.openSettings()
			case "rename":
				a.beginRename()
			case "delete":
				a.askDelete()
			case "request":
				a.requestOpen = true
			}
		},
	})
}

func (a *App) modelMenu(c *ui.Context, conv *Conversation, slot string) ui.Element {
	p := a.look
	chatID := ""
	screen := Config{}
	if conv != nil {
		chatID = conv.ID
		screen = conv.Config
	}
	b := ui.ButtonBase(c.Key("model-menu-" + slot))
	b.Height(chipH).Gap(5).Padding(0, 5, 0, 7).Radius(radiusRow).Background(p.ControlBg).Border(1, p.ControlBorder).Cursor(ui.CursorPointer).MaxWidth(180).MinWidth(0)
	switch {
	case b.Pressed():
		b.Background(p.FillPressed)
	case b.Hovered():
		b.Background(p.FillHover)
	}
	ownerID := chatID
	cfg := screen
	st := a.popoverState("model-" + slot)
	if st.open && a.modelMenuChat != "" {
		ownerID = a.modelMenuChat
		if shown := a.conversation(ownerID); shown != nil {
			cfg = shown.Config
		}
	}
	if st.open {
		a.modelMenuSeen = true
		if a.modelMenuChat == "" {
			a.modelMenuChat = chatID
		}
	}
	items, footer, search := a.modelMenuEntries(cfg)
	a.bindPopover(c, "model-"+slot, b, popSpec{
		Name:          "Model",
		TestLabel:     "model menu",
		Items:         items,
		Footer:        footer,
		FooterSepMark: modelMenuMark("sep", "", "", false, false, false),
		Search:        search,
		SearchPrompt:  "Search models",
		ComposerFocus: true,
		OnSelect: func(id string) {
			if id == modelMenuSettings || id == modelMenuAdd {
				st.focusComposer = false
			}
			target := chatID
			if a.modelMenuChat != "" {
				target = a.modelMenuChat
			}
			a.applyModelMenuChoice(target, id)
		},
	})
	if st.open {
		a.modelMenuSeen = true
		if a.modelMenuChat == "" {
			a.modelMenuChat = chatID
		}
	}
	provider, model := a.composerModelParts(screen)
	label := model
	if provider != "" {
		label = provider + " · " + model
	}
	dot := a.configColor(screen)
	if model == "No model" {
		dot = ui.Color{}
	}
	b.Label("Model " + label)
	b.Children(func() {
		if dot.A > 0 {
			ui.Box(c).Size(6, 6).Radius(3).Background(dot).Shrink(0)
		}
		// The provider run does not shrink. Only the model name ellipsizes,
		// so a narrow button keeps "OpenCode ·" and cuts deepseek-v4.1.
		ui.Row(c).Gap(0).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Center).Children(func() {
			if provider != "" {
				controlText(c, provider, 12, 0, 500, p.TextPrimary).Shrink(0)
				controlText(c, " ·", 12, 0, 500, p.TextTertiary).Shrink(0)
				controlText(c, " "+model, 12, 0, 500, p.TextPrimary).Ellipsis("…").Grow(1).Shrink(1).MinWidth(0)
				return
			}
			controlText(c, model, 12, 0, 500, p.TextPrimary).Ellipsis("…").Grow(1).Shrink(1).MinWidth(0)
		})
		ui.Icon(c, iconChevron).Size(11, 11).TextColor(p.TextSecondary).Shrink(0)
	})
	return a.tip(c, b, "Model")
}

func (a *App) applyModelMenuChoice(chatID, pick string) {
	switch pick {
	case modelMenuSettings, modelMenuAdd:
		a.openProviderSettings()
	default:
		if connID, model, ok := parseModelChoiceID(pick); ok {
			a.useModelChat(chatID, connID, model)
		}
	}
}

// modelMenuEntries is the composer model menu: provider groups, then a
// pinned Models & Providers… row. Search appears once 10 models are enabled.
func (a *App) modelMenuEntries(cfg Config) (items, footer []popEntry, search bool) {
	opts := a.menuModels()
	if len(opts) == 0 {
		items = []popEntry{{
			Kind: popItem, ID: modelMenuAdd, Text: "Add a provider…",
			MarkKind: "action", MarkModel: "Add a provider…", MarkOn: true,
		}}
		return items, nil, false
	}
	search = len(opts) >= menuSearchAt
	prev := ""
	listed := false
	for _, opt := range opts {
		if opt.connID != prev {
			items = append(items, popEntry{
				Kind: popHeader, Text: opt.provider, Dot: a.menuDot(a.connByID(opt.connID)),
				MarkKind: "header", MarkProv: opt.provider,
			})
			prev = opt.connID
		}
		on := modelChosen(cfg, opt.connID, opt.model)
		if on {
			listed = true
		}
		items = append(items, popEntry{
			Kind: popItem, ID: modelChoiceID(opt.connID, opt.model), Text: opt.model, Group: opt.provider,
			Checkable: true, Checked: on,
			MarkKind: "model", MarkProv: opt.provider, MarkModel: opt.model, MarkOn: true,
		})
	}
	if !listed && strings.TrimSpace(cfg.Model) != "" {
		model := strings.TrimSpace(cfg.Model)
		items = append(items, popEntry{
			Kind: popItem, ID: modelChoiceID(cfg.ConnectionID, model), Text: model + " (unavailable)",
			Checkable: true, Checked: true, Disabled: true,
			MarkKind: "unavailable", MarkModel: model, MarkOn: false,
		})
	}
	footer = []popEntry{{
		Kind: popItem, ID: modelMenuSettings, Text: "Models & Providers…",
		MarkKind: "action", MarkModel: "Models & Providers…", MarkOn: true,
	}}
	return items, footer, search
}

// menuDot is the provider status dot. Keychain denial uses status/warning,
// the same color as the Models & Providers list.
func (a *App) menuDot(conn *Connection) ui.Color {
	if conn == nil {
		return a.look.TextTertiary
	}
	keyless := !conn.Custom && keylessProvider(conn.Provider)
	if !keyless && a.keyBlocked(conn.ID) {
		return a.look.StatusWarning
	}
	return a.connectionColor(conn)
}

// modelMenuMark identifies one popup row for tests. The fields are kind,
// provider, model or action title, enabled, checked, and highlighted.
func modelMenuMark(kind, provider, model string, enabled, checked, hot bool) string {
	bit := func(v bool) string {
		if v {
			return "1"
		}
		return "0"
	}
	return strings.Join([]string{"model-menu-row", kind, provider, model, bit(enabled), bit(checked), bit(hot)}, "\x1f")
}

func (a *App) transcript(c *ui.Context, conv *Conversation) {
	a.chat.Key = func(i int) any {
		rows := transcriptItems(a.active())
		if i < 0 || i >= len(rows) {
			return i
		}
		row := rows[i]
		cur := a.active()
		if row.msg >= 0 && cur != nil && row.msg < len(cur.Messages) {
			return cur.Messages[row.msg].ID
		}
		if row.brk >= 0 && cur != nil && row.brk < len(cur.Breaks) {
			return "session:" + cur.Breaks[row.brk].ID
		}
		return i
	}
	a.chat.FollowEnd = true
	if a.shotPinEdit && a.edit.convID == conv.ID {
		a.chat.FollowEnd = false
		rows := transcriptItems(conv)
		for i, row := range rows {
			if row.msg >= 0 && conv.Messages[row.msg].ID == a.edit.msgID {
				a.chat.ScrollTo(i, ui.Start)
				break
			}
		}
	}
	if a.shotPinBreak {
		a.chat.FollowEnd = false
		rows := transcriptItems(conv)
		for i, row := range rows {
			if row.brk >= 0 {
				a.chat.ScrollTo(i, ui.Center)
				break
			}
		}
	}
	// A thinking block that is opening or closing changes the row height
	// every frame. The end-follow glide would hold the row, then spring
	// back, and the reply under the header would jump when the glide and
	// the collapsed height finally agree. Pin the end (or the anchor)
	// directly while it moves: the scroll target is the height of this
	// frame, which eases to the collapsed height, so the reply stays put
	// when the list is at its end and only slides when the offset has to
	// clamp.
	if a.instant || a.thinkingAnimating(conv) {
		a.chat.FollowAnimation = nil
	} else {
		a.chat.FollowAnimation = &ui.Smooth
	}
	a.thinkSeen = a.thinkSeen[:0]
	// The list fills the card, including the area behind the composer.
	// Padding, not a shorter frame, is what lets the last message rest
	// above the card: composer height + 30.
	a.placeFind(c, conv)
	_, inset := a.composerInsets(c)
	bottom := a.composerH + inset + float32(composerExtra)
	top := float32(24)
	if a.finder.open {
		top += findBarH
	}
	top += a.finder.lift
	rows := transcriptItems(conv)
	list := ui.List(c, &a.chat, len(rows), func(i int) {
		cur := a.active()
		current := transcriptItems(cur)
		if cur == nil || i >= len(current) {
			return
		}
		row := current[i]
		if row.brk >= 0 {
			a.sessionDivider(c, cur, row.brk)
			return
		}
		if row.msg < 0 || row.msg >= len(cur.Messages) {
			return
		}
		a.messageView(c, cur, &cur.Messages[row.msg])
	})
	list.Fill().Selectable().Label("transcript").Padding(top, space6, bottom, space6).Gap(space8).Justify(ui.End).ScrollbarInsets(0, 2, a.composerH+inset, 2)
	a.finishThinkingMotion(conv)
	vis := list.Bounds()
	bottomCut := a.composerH + inset
	if vis.H > top+bottomCut+8 {
		vis.Y += top
		vis.H -= top + bottomCut
	}
	a.captureHistorySelection(c, list, vis)
}

func (a *App) activeMessages() []Message {
	conv := a.active()
	if conv == nil {
		return nil
	}
	return conv.Messages
}

func (a *App) emptyTranscript(c *ui.Context, conv *Conversation) {
	p := a.look
	_, winH := c.Size()
	cardH := winH - titleBarHeight - 8
	short := cardH < 560
	avatar := float32(48)
	if short {
		avatar = 40
	}
	// The scroll keeps the grid inside the card. Bottom padding is the
	// composer height plus 14 plus 16, so the last card stays above it.
	ui.Scroll(c).Fill().TrackScroll(&a.emptyScroll).Children(func() {
		_, inset := a.composerInsets(c)
		ui.Column(c).FillWidth().MinHeight(cardH).Center().Padding(0, space6, a.composerH+inset+float32(composerExtra), space6).AlignItems(ui.Center).Children(func() {
			a.appIcon(c, avatar)
			ui.Text(c, "What should we work on?").FontSize(22).FontWeight(600).LetterSpacing(-0.2).Padding(16, 0, 0, 0).SingleLine().Ellipsis("…")
			ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(6, 0, 0, 0).FillWidth().Justify(ui.Center).Children(func() {
				ui.Box(c).Size(6, 6).Radius(3).Background(a.configColor(conv.Config)).Shrink(0)
				ui.Text(c, a.subtleModel(conv)).FontSize(13).TextColor(p.TextSecondary).SingleLine().Ellipsis("…").MinWidth(0)
			})
			a.starterGrid(c, space6)
			if !short {
				ui.Row(c).Gap(18).Padding(18, 0, 0, 0).Wrap().Justify(ui.Center).FillWidth().Children(func() {
					for _, s := range []string{"⌘N  " + newChatTitle, "⌘,  Settings", "⌘K  Search"} {
						controlLine(c, s, 11, 0, 400, p.TextSecondary)
					}
				})
			}
		})
	})
}

// cardContentWidth is the content card's inner width at the width the
// sidebar is showing, not the user's stored width.
func (a *App) cardContentWidth(c *ui.Context) float32 {
	winW, _ := c.Size()
	return paneInnerWidth(winW, sidebarShown(a.sidebarWidth, winW))
}

// starterGrid lays the suggestion cards inside the parent's horizontal
// padding. Columns share that width up to the 300pt design size. When two
// columns would be narrower than suggestionFit, they stack so a subtitle
// is not cut off by the card.
func (a *App) starterGrid(c *ui.Context, padX float32) {
	_, winH := c.Size()
	w := a.cardContentWidth(c)
	cols := 2
	n := len(starters)
	if w < 560 {
		cols = 1
		if n > 3 {
			n = 3
		}
	}
	top := float32(26)
	if winH-titleBarHeight-8 < 560 {
		top = 20
	}
	ui.Column(c).Gap(cardGap).Padding(top, 0, 0, 0).FillWidth().AlignSelf(ui.Stretch).Children(func() {
		rows := (n + cols - 1) / cols
		for row := 0; row < rows; row++ {
			ui.Row(c).Gap(cardGap).FillWidth().Justify(ui.Center).Children(func() {
				for col := 0; col < cols; col++ {
					i := row*cols + col
					if i >= n {
						ui.Box(c).Grow(1).Basis(0).MinWidth(0).MaxWidth(cardW)
						continue
					}
					a.starterCard(c, starters[i])
				}
			})
		}
	})
}

func (a *App) starterCard(c *ui.Context, s starter) {
	p := a.look
	card := ui.ButtonBase(c).Height(cardH).Grow(1).Basis(0).MinWidth(0).MaxWidth(cardW).Padding(0, 12, 0, 12).Radius(radiusGroup).Background(p.FillQuaternary).AlignItems(ui.Center).Gap(10).Cursor(ui.CursorPointer).Clip().Label("starter " + s.Title)
	if card.Hovered() {
		card.Background(p.FillPressed)
	}
	if !a.instant && !a.reduceMotion {
		card.Transition(ui.ElementTransition{Colors: true, Duration: motionHoverRow, Ease: easeStandard})
	}
	a.fadeColors(card)
	card.Children(func() {
		ui.Row(c).Size(28, 28).Radius(radiusRow).Background(p.AccentSoft).Center().Shrink(0).Label("starter icon " + s.Title).Children(func() {
			ui.Icon(c, s.Icon).Size(15, 15).TextColor(p.AccentDefault)
		})
		// The column fills the 60pt card and centers the two lines. The
		// subtitle stays on one line, the same as the short cards, and a
		// longer string ends in an ellipsis instead of being clipped.
		ui.Column(c).Gap(2).Grow(1).MinWidth(0).AlignSelf(ui.Stretch).Justify(ui.Center).AlignItems(ui.Start).Children(func() {
			ui.Text(c, s.Title).FontSize(13).FontWeight(600).TextAlign(ui.Start).SingleLine().Ellipsis("…").FillWidth().MinWidth(0)
			ui.Text(c, s.Detail).FontSize(12).TextColor(p.TextSecondary).TextAlign(ui.Start).SingleLine().Ellipsis("…").FillWidth().MinWidth(0)
		})
	})
	if card.Clicked() {
		if a.active() == nil {
			a.newChat()
		}
		if current := a.active(); current != nil {
			// Fill only. A new field puts the caret at the end of the prompt.
			current.Draft = s.Prompt
			if a.composerGen == nil {
				a.composerGen = map[string]int{}
			}
			a.composerGen[current.ID]++
			a.requestComposerFocus(current.ID)
		}
	}
}

func (a *App) messageView(c *ui.Context, conv *Conversation, m *Message) {
	if conv == nil || m == nil {
		return
	}
	if m.Role == roleUser {
		a.userMessage(c, conv, m)
		return
	}
	a.assistantMessage(c, conv, m)
}

func (a *App) afterEdit(conv *Conversation, id string) bool {
	if conv == nil || a.edit.convID != conv.ID || a.edit.msgID == "" {
		return false
	}
	seen := false
	for i := range conv.Messages {
		if conv.Messages[i].ID == a.edit.msgID {
			seen = true
			continue
		}
		if seen && conv.Messages[i].ID == id {
			return true
		}
	}
	return false
}

func (a *App) readingColumn(c *ui.Context) ui.Element {
	return ui.Column(c).MaxWidth(readingWidth).FillWidth().MinWidth(0).AlignSelf(ui.Center).Gap(space2)
}

func (a *App) userMessage(c *ui.Context, conv *Conversation, m *Message) {
	if a.edit.convID == conv.ID && a.edit.msgID == m.ID {
		a.inlineEditor(c, conv)
		return
	}
	col := a.readingColumn(c).Label(quoteMsgLabel(m.ID))
	a.enterMessage(col, m.ID)
	if a.afterEdit(conv, m.ID) {
		col.Opacity(0.4)
	}
	col.Children(func() {
		// The 720 column holds the hover actions beside the bubble. The
		// bubble itself is capped at 560, padding 14×9, so a full sentence
		// stays on one line.
		row := ui.Row(c).Gap(2).AlignItems(ui.End).FillWidth().Justify(ui.End)
		hovered := row.Hovered()
		row.Children(func() {
			acts := ui.Row(c).Gap(0).AlignItems(ui.Center).Shrink(0)
			if !hovered {
				acts.Opacity(0)
			} else if !a.instant && !a.reduceMotion {
				acts.Transition(ui.ElementTransition{Colors: true, Duration: motionActions, Ease: easeStandard})
			}
			acts.Children(func() {
				if m.Content != "" {
					a.copyButton(c, m.ID, m.Content, iconSm)
				}
				btn := a.iconButtonSize(c, "edit-"+m.ID, "Edit", iconPencil, iconSm)
				a.tip(c, btn, "Edit this message")
				if btn.Clicked() {
					a.editMessage(conv.ID, m.ID)
				}
			})
			a.userBubbleText(c, m)
		})
	})
}

func (a *App) enterMessage(col ui.Element, id string) {
	if a.instant || a.reduceMotion || id == "" {
		return
	}
	col.Key(id).Transition(ui.ElementTransition{
		Enter:    &ui.Motion{Y: messageEnterY, Opacity: 0},
		Duration: motionEnter,
		Ease:     easeEnter,
	})
}

func (a *App) assistantMessage(c *ui.Context, conv *Conversation, m *Message) {
	p := a.look
	live := !m.Done
	col := a.readingColumn(c).Label(quoteMsgLabel(m.ID))
	a.enterMessage(col, m.ID)
	a.paintFlash(c, col, m.ID)
	if a.afterEdit(conv, m.ID) {
		col.Opacity(0.4)
	}
	a.prepareFindPaint(m)
	col.Children(func() {
		if a.afterEdit(conv, m.ID) {
			shield := ui.ButtonBase(c).Absolute().Left(0).Right(0).Top(0).Bottom(0).Label("later " + m.ID)
			if shield.Clicked() {
				a.cancelEdit()
			}
		}
		body := ui.Column(c).Gap(10).Grow(1).FillWidth()
		hovered := body.Hovered()
		body.Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Unselectable().Children(func() {
				a.mark(c, 22)
				ui.Text(c, "Cove").FontSize(13).FontWeight(600).Unselectable()
				if stamp := formatStamp(c, m.CreatedAt); stamp != "" {
					ui.Text(c, stamp).FontSize(11).TextColor(p.TextTertiary).Unselectable()
				}
			})
			if strings.TrimSpace(m.Reasoning) != "" && (m.Done || a.data.ShowReasoning) {
				a.thinkingBlock(c, m, live)
			}
			waiting := live && m.Content == "" && strings.TrimSpace(m.Reasoning) == ""
			if waiting {
				a.streamStatus(c, "Waiting for "+modelLabel(conv)+"…", "")
			} else if m.Content != "" {
				a.hScrollMsg = m.ID
				if m.Done {
					a.installFindHook()
					markdown.View(c, m.Content, &a.md).FillWidth().MinWidth(0)
					a.finishFindPaint()
				} else {
					a.streamCaret = true
					markdown.Stream(c, m.Content, &a.md).FillWidth().MinWidth(0)
					a.streamCaret = false
				}
				a.hScrollMsg = ""
			}
			if live && !waiting {
				extra := "·  ⌘. to stop"
				if m.Tokens > 0 {
					extra = "·  " + formatTokens(m.Tokens) + "  ·  ⌘. to stop"
				}
				a.streamStatus(c, "Generating", extra)
			}
			if m.Error != "" {
				ui.Row(c).Gap(8).Padding(6, 6, 6, 10).Radius(radiusCode).Background(p.StatusWarningSoft).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconAlert).Size(14, 14).TextColor(p.StatusWarning)
					ui.Text(c, m.Error).Selectable().FontSize(12).TextColor(p.TextPrimary).Grow(1)
					if isLastMessage(conv, m.ID) && a.sheetButton(c, "Retry", false).Clicked() {
						a.regenerate()
					}
				})
			}
			if !live {
				show := hovered || isLastMessage(conv, m.ID)
				// 12pt under the body. The column already gaps 10.
				a.messageActions(c, conv, m, show)
			}
		})
	})
	a.finishFindPaint()
}

// thinkingAnimating reports whether a disclosure will change height this
// frame. The click flips ReasoningOpen before the frame is built, and the
// panel's progress catches up over the next frames; either one means the
// list should follow the final height instead of gliding.
func (a *App) thinkingAnimating(conv *Conversation) bool {
	if conv == nil {
		return false
	}
	for i := range conv.Messages {
		m := &conv.Messages[i]
		if strings.TrimSpace(m.Reasoning) == "" {
			continue
		}
		if a.thinkBusy[m.ID] {
			return true
		}
		if settled, ok := a.thinkSettled[m.ID]; ok && settled != m.ReasoningOpen {
			return true
		}
	}
	return false
}

func (a *App) noteThinking(id string, open bool, progress float32) {
	if a.thinkBusy == nil {
		a.thinkBusy = map[string]bool{}
	}
	if a.thinkSettled == nil {
		a.thinkSettled = map[string]bool{}
	}
	a.thinkSeen = append(a.thinkSeen, id)
	busy := progress > 0 && progress < 1
	a.thinkBusy[id] = busy
	// Rest only once progress has arrived at the open flag. The frame the
	// disclosure flips, progress is still the old value.
	if !busy && ((open && progress >= 1) || (!open && progress <= 0)) {
		a.thinkSettled[id] = open
	}
}

// finishThinkingMotion drops the follow-freeze for thinking blocks the
// list did not build. A block that scrolled away mid-animation must not
// keep the transcript from gliding.
func (a *App) finishThinkingMotion(conv *Conversation) {
	if conv == nil {
		return
	}
	seen := map[string]bool{}
	for _, id := range a.thinkSeen {
		seen[id] = true
	}
	for i := range conv.Messages {
		m := &conv.Messages[i]
		if seen[m.ID] || strings.TrimSpace(m.Reasoning) == "" {
			continue
		}
		delete(a.thinkBusy, m.ID)
		if _, ok := a.thinkSettled[m.ID]; ok {
			a.thinkSettled[m.ID] = m.ReasoningOpen
		}
	}
}

func (a *App) thinkingBlock(c *ui.Context, m *Message, live bool) {
	p := a.look
	label := thoughtLabel(m, live)
	// No gap between the header and the panel. A gap lives only while the
	// panel node exists, including the last moment its clip height is
	// already ~0, so removing the node at the end of the close jumped the
	// reply by that gap. The 8pt sits inside the panel instead, and the
	// clip eases it to nothing with the body.
	ui.Column(c).AlignItems(ui.Start).Children(func() {
		parts := ui.CollapsibleBase(c, &m.ReasoningOpen)
		a.noteThinking(m.ID, m.ReasoningOpen, parts.Progress())
		tr := parts.Trigger.Gap(6).Height(26).AlignItems(ui.Center).AlignSelf(ui.Start)
		labelColor := p.TextSecondary
		if live || tr.Hovered() {
			labelColor = p.TextPrimary
		}
		tr.Children(func() {
			ui.Icon(c, iconSpark).Size(13, 13).TextColor(p.ThemeMark)
			controlText(c, label, 12, 0, 500, labelColor)
			chev := ui.Icon(c, iconChevronRight).Size(12, 12).TextColor(p.TextTertiary)
			if m.ReasoningOpen {
				chev.Rotate(90)
			}
		})
		bar := p.ThemeMark
		if live {
			bar = bar.Alpha(a.pulse(c, 0, motionTideBar, tideMin, tideMax))
		} else {
			bar = bar.Alpha(0.5)
		}
		parts.Panel(func() {
			ui.Row(c).Gap(12).AlignItems(ui.Stretch).Children(func() {
				ui.Box(c).Width(3).Radius(1).Background(bar).Shrink(0)
				a.thinkingText(c, m, strings.TrimSpace(m.Reasoning), live)
			})
		}).Padding(16, 0, 0, 0)
	})
}

func (a *App) messageActions(c *ui.Context, conv *Conversation, m *Message, show bool) {
	p := a.look
	row := ui.Row(c).Gap(0).AlignItems(ui.Center).Margin(2, 0, 0, 0).Unselectable()
	if !show {
		// Stay in the tree so tests can still click Copy and Regenerate.
		row.Opacity(0)
	}
	row.Children(func() {
		if m.Content != "" {
			a.copyButton(c, m.ID, m.Content, iconSm)
		}
		if m.Role == roleAssistant && isLastMessage(conv, m.ID) {
			btn := a.iconButtonSize(c, "regen-"+m.ID, "Regenerate", iconRefresh, iconSm)
			btn.Disabled(!a.canRegenerate())
			a.tip(c, btn, "Regenerate")
			if btn.Clicked() {
				a.regenerate()
			}
		}
		if m.Role == roleAssistant {
			if meta := assistantMeta(m); meta != "" {
				ui.Box(c).Width(1).Height(14).Background(p.SeparatorDefault).Shrink(0).Margin(0, 0, 0, 2)
				pad := float32(10)
				if dot, ok := a.replyDot(m); ok {
					ui.Box(c).Size(6, 6).Radius(3).Background(dot).Shrink(0).Margin(0, 0, 0, 8)
					pad = 6
				}
				ui.Text(c, meta).FontSize(11).TextColor(p.TextTertiary).Padding(0, 0, 0, pad).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
			}
		}
	})
}

func latestUserID(conv *Conversation) string {
	if conv == nil {
		return ""
	}
	for i := len(conv.Messages) - 1; i >= 0; i-- {
		if conv.Messages[i].Role == roleUser {
			return conv.Messages[i].ID
		}
	}
	return ""
}

func isLastMessage(conv *Conversation, id string) bool {
	return conv != nil && len(conv.Messages) > 0 && conv.Messages[len(conv.Messages)-1].ID == id
}

// copyButton copies the message's raw markdown. The label flips to Copied
// until the tester clock, or the real clock, passes the deadline.
func (a *App) copyButton(c *ui.Context, id, text string, size float32) ui.Element {
	p := a.look
	label := "Copy"
	color := p.TextSecondary
	if until, ok := a.copied[id]; ok && c.Now().Before(until) {
		label = "Copied"
		color = p.StatusSuccess
	}
	var btn ui.Element
	if label == "Copied" {
		btn = ui.ButtonBase(c.Key("copy-"+id)).Height(size).Padding(0, 4).AlignItems(ui.Center).Label("Copied")
		btn.Children(func() {
			controlText(c, "Copied", 12, 0, 500, color)
		})
		a.tip(c, btn, "Copied")
	} else {
		btn = a.iconButtonSize(c, "copy-"+id, "Copy", iconCopy, size)
		a.tip(c, btn, "Copy markdown")
	}
	if btn.Clicked() {
		c.WriteClipboard(text)
		a.copied[id] = c.Now().Add(motionCopied)
	}
	return btn
}

func (a *App) searchField(c *ui.Context) {
	p := a.look
	row := ui.Row(c).Height(rowHeight).FillWidth().Gap(8).Padding(0, 8, 0, 10).Radius(radiusRow).Background(p.FillQuaternary).AlignItems(ui.Center)
	row.Children(func() {
		ui.Icon(c, iconSearch).Size(14, 14).TextColor(p.TextSecondary).Shrink(0)
		field := ui.TextInputBase(c.Key("sidebar-search"), &a.query).Placeholder("Search").Label("Search").Grow(1).MinWidth(0).FontSize(13)
		a.searchInput = field
		// No fill and no border: fill/quaternary is translucent, so painting
		// it again on the input draws a second rectangle that ends before ⌘K.
		// The placeholder uses text/secondary, which is Theme.TextMuted.
		field.Border(0, ui.Transparent).Background(ui.Transparent).FocusRing(false)
		field.OnTextKey(func(k ui.TextKey) bool {
			// Same as the composer: ⌘⌫ is Delete Chat, not delete-to-start.
			if k.Key == ui.KeyBackspace && k.Mods == ui.Cmd {
				a.askDelete()
				return true
			}
			return false
		})
		if a.focusSearch || c.Shortcut(ui.Cmd, ui.KeyK) {
			field.Focus()
			a.focusSearch = false
		}
		if field.Focused() {
			row.Border(1, p.AccentDefault).Shadow(0, 0, 0, 3, p.AccentRing)
		}
		controlLine(c, "⌘K", 11, 0, 400, p.TextSecondary).Shrink(0)
	})
}

// paintPlaceholder colors an empty input's placeholder. MyGo paints that
// string with Theme.TextMuted and has no placeholder-color method. Draw
// runs immediately before the input paints and DrawOver restores the
// theme, so the composer and the settings fields stay on text/secondary.
func paintPlaceholder(c *ui.Context, field ui.Element, color ui.Color) {
	th := c.Theme()
	var saved ui.Color
	var on bool
	field.Draw(func(*ui.Painter, ui.Rect) {
		if th == nil || on {
			return
		}
		saved, on = th.TextMuted, true
		th.TextMuted = color
	}).DrawOver(func(*ui.Painter, ui.Rect) {
		if th == nil || !on {
			return
		}
		th.TextMuted = saved
		on = false
	})
}

func (a *App) thinkingText(c *ui.Context, m *Message, text string, live bool) {
	p := a.look
	if m != nil && !live && a.thinkingFind(c, m, text) {
		return
	}
	body := ui.RichText(c).FontSize(12).LineHeight(20.0 / 12.0).TextColor(p.ThinkText).MaxLines(3).Grow(1).MinWidth(0).Selectable()
	if !live {
		body.Children(func() {
			ui.Text(c, text)
		})
		return
	}
	head, tail, ok := splitLastWord(text)
	fade := p.TextTertiary.Alpha(0.6)
	body.Children(func() {
		if ok {
			ui.Text(c, head+" ")
			ui.Text(c, tail).TextColor(fade)
			return
		}
		ui.Text(c, text).TextColor(fade)
	})
}

func splitLastWord(text string) (head, tail string, ok bool) {
	text = strings.TrimSpace(text)
	i := strings.LastIndex(text, " ")
	if i <= 0 || i >= len(text)-1 {
		return text, "", false
	}
	return text[:i], text[i+1:], true
}

func assistantMeta(m *Message) string {
	if m == nil {
		return ""
	}
	var parts []string
	if label := replyModelLabel(m); label != "" {
		parts = append(parts, label)
	}
	if m.Tokens > 0 {
		parts = append(parts, formatTokens(m.Tokens))
	}
	if m.ElapsedMS > 0 {
		parts = append(parts, formatSeconds(m.ElapsedMS))
	}
	return strings.Join(parts, "  ·  ")
}

// replyModelLabel is the stored provider display name and model id.
// A legacy assistant message with neither draws no model label.
func replyModelLabel(m *Message) string {
	if m == nil {
		return ""
	}
	name := strings.TrimSpace(m.ProviderName)
	model := strings.TrimSpace(m.Model)
	switch {
	case name != "" && model != "":
		return name + " · " + model
	case model != "":
		return model
	default:
		return name
	}
}

// replyDot is the provider dot beside a stored model label. Colour comes
// from the kind recorded on the message, not from a live connection, so
// a rename or a delete leaves it alone. No stored kind draws no dot.
func (a *App) replyDot(m *Message) (ui.Color, bool) {
	if m == nil || replyModelLabel(m) == "" {
		return ui.Color{}, false
	}
	if strings.TrimSpace(m.ProviderKind) == "" && !m.ProviderCustom {
		return ui.Color{}, false
	}
	return a.look.providerColor(m.ProviderKind, m.ProviderCustom), true
}

func formatTokens(n int) string {
	if n >= 1000 {
		tenths := (n + 50) / 100
		return itoa(tenths/10) + "." + itoa(tenths%10) + "k tokens"
	}
	return itoa(n) + " tokens"
}

// composerHeight estimates the floating card from the draft so the
// transcript padding matches it on the same frame. Measuring the card
// after layout and applying it next frame shifts the list under a click
// and the click is lost.
func composerHeight(draft string, quotes []Quote, maxQuoteH, quoteW float32) float32 {
	lines := 1
	if strings.TrimSpace(draft) != "" {
		lines = strings.Count(draft, "\n") + 1
	}
	if lines > 10 {
		lines = 10
	}
	// Border box of one line is 73: 1pt stroke, pad 10, a 21pt line, gap 8,
	// a 24pt tool row, pad 8, 1pt stroke. The tool row is 2pt under the
	// 26pt send button, which overflows it by a point on each side.
	h := float32(1+10+8+8+1+composerToolH) + float32(lines)*21
	if len(quotes) > 0 {
		// The column's 8pt gap sits between the quote stack and the field.
		h += quoteAreaHeight(quotes, maxQuoteH, quoteW) + 8
	}
	if h < composerMinH {
		return composerMinH
	}
	return h
}

func formatSeconds(ms int) string {
	tenths := (ms + 50) / 100
	return itoa(tenths/10) + "." + itoa(tenths%10) + "s"
}

func (a *App) composer(c *ui.Context, conv *Conversation) {
	p := a.look
	streamingHere := a.isStreaming(conv.ID)
	editing := a.edit.convID == conv.ID
	// Keyed so a sibling appearing above it (the jump button, the edit
	// lock) does not change this row's id. An unkeyed id is mixed from
	// the parent and the child index, and a new index is a new text view.
	side, bottom := a.composerInsets(c)
	ui.Row(c.Key("composer")).Absolute().Left(side).Right(side).Bottom(bottom).Justify(ui.Center).PassThrough().Children(func() {
		stack := ui.Column(c).MaxWidth(readingWidth).FillWidth().Gap(8)
		stack.Children(func() {
			a.composerNote(c, conv)
			// Shadow behind the plate. The plate's own fill stays on the shell.
			// A stable key keeps the text view's id when the note above it
			// appears or disappears. The key does not include the chat id:
			// the model menu lives in this plate, and a chat switch must
			// not mint a new button or an open choice is dropped. An
			// unkeyed parent chain shifts and MyGo drops focus
			// (runtime.go, unseen focused element).
			plate := ui.Box(c.Key("composer-plate")).FillWidth()
			plate.Draw(func(painter *ui.Painter, r ui.Rect) {
				paintDropShadow(painter, r, radiusComposer, shadowComposer, p.ShadowMd)
			})
			plate.Children(func() {
				shell := ui.Column(c).FillWidth().Padding(10, 8, 8, 12).Gap(8).Radius(radiusComposer).Background(p.LayerComposer).Border(1, p.LayerComposerBorder).Label("composer")
				a.fadeColors(shell)
				shell.Children(func() {
					body := ui.Column(c).FillWidth().Gap(8)
					if editing {
						body.Opacity(0.5)
					}
					body.Children(func() {
						if a.notice != "" {
							ui.Text(c, a.notice).Selectable().TextColor(p.StatusDanger).FontSize(12).Padding(space1, space1, 0, space1)
						}
						a.composerQuotes(c, conv)
						input := ui.TextArea(c.Key(fmt.Sprintf("draft-%s-%d", conv.ID, a.composerGen[conv.ID])), &conv.Draft).
							Placeholder("Message Cove").
							Label("Message").
							FontSize(14).
							LineHeight(21.0/14.0).
							Lines(1, 10).
							MinHeight(0)
						// The shell is the only fill. Focus shows the caret only:
						// no accent ring. While an inline edit is open the field
						// stays, faded, and does not take keys.
						input.Padding(0).MinHeight(21).Border(0, ui.Transparent).Background(ui.Transparent).FocusRing(false)
						a.composerField = input
						if input.Pressed() {
							a.quoteBarOpen = false
						}
						if editing {
							input.Disabled(true)
						} else if a.focusComposerID == conv.ID {
							// One frame. Calling Focus every frame would pin the caret
							// here and the user could not tab away.
							input.Focus()
							if a.restoreComposerCaret {
								a.restoreComposerCaret = false
								if a.composerCaretSet {
									input.SetTextSelection(a.composerAnchor, a.composerCaretPos)
								} else {
									end, _ := composerEnd(conv.Draft)
									input.SetTextSelection(end, end)
								}
							}
							a.focusComposerID = ""
						}
						paintPlaceholder(c, input, p.TextTertiary)
						input.OnTextKey(func(k ui.TextKey) bool {
							if editing {
								return true
							}
							a.dismissQuoteBar()
							if k.Key == ui.KeyBackspace && k.Mods == 0 && len(conv.DraftQuotes) > 0 {
								if start, end := input.TextSelection(); start == 0 && end == 0 {
									a.removeDraftQuote(len(conv.DraftQuotes) - 1)
									return true
								}
							}
							// The editor otherwise takes ⌘⌫ as delete-to-start
							// (editor.wants) and the window shortcut never runs.
							if k.Key == ui.KeyBackspace && k.Mods == ui.Cmd {
								a.askDelete()
								return true
							}
							sendMods := ui.Modifiers(0)
							if a.data.SendWith == "mod" {
								sendMods = ui.Cmd
							}
							if k.Key != ui.KeyEnter || k.Mods != sendMods {
								return false
							}
							if streamingHere {
								a.notice = "Stop the current reply before sending another."
								return true
							}
							text := k.Text()
							if !a.send(text) {
								// Blocked: the draft stays, and so does the caret.
								return true
							}
							// SetText writes the editor and the bound value.
							// Clearing the bound value alone is not enough:
							// the editor writes its buffer back over it.
							k.SetText("")
							return true
						})
						// 24pt, not the send button's 26: the hint's line box was making
						// this row 2pt tall and the single-line border box 75.
						ui.Row(c).FillWidth().Height(composerToolH).Gap(4).AlignItems(ui.Center).Children(func() {
							a.tip(c, a.modelMenu(c, conv, "composer"), "Model").MinWidth(0)
							a.effortMenu(c, conv, "composer").Shrink(0)
							a.newSessionButton(c)
							ui.Spacer(c)
							if a.cardContentWidth(c) >= 560 {
								hint := "↵ send · ⇧↵ newline"
								if a.data.SendWith == "mod" {
									hint = "⌘↵ send · ↵ newline"
								}
								if streamingHere {
									hint = "⌘. to stop"
								}
								controlLine(c, hint, 11, 13, 400, p.TextTertiary).Padding(0, 8, 0, 0).Ellipsis("…").MinWidth(0)
							}
							if streamingHere {
								if a.roundButton(c, "Stop", iconStop, false, editing).Clicked() && !editing {
									a.stop()
								}
							} else if a.roundButton(c, "Send", iconSend, true, editing || (strings.TrimSpace(conv.Draft) == "" && len(conv.DraftQuotes) == 0)).Clicked() && !editing {
								a.send(conv.Draft)
							}
						})
					})
				})
			})
		})
	})
}

func (a *App) composerNote(c *ui.Context, conv *Conversation) {
	if conv == nil {
		return
	}
	text := ""
	kind := "switch"
	if a.quoteLimitActive(c, conv) {
		text = "Up to 5 quotes per message."
		kind = "limit"
	}
	if text == "" {
		text = conv.SwitchNote
	}
	if text == "" && a.addProviderNote == conv.ID {
		text = "Add a provider to start chatting."
		kind = "add"
	}
	if text == "" {
		return
	}
	p := a.look
	row := ui.Row(c).Height(26).Padding(0, 10, 0, 12).Gap(6).Radius(13).AlignItems(ui.Center).AlignSelf(ui.Center).Background(p.LayerComposer).Border(0.5, p.ControlBorder).MaxWidth(readingWidth).Label("switch note")
	row.Children(func() {
		ui.Icon(c, iconInfo).Size(13, 13).TextColor(p.TextTertiary).Shrink(0)
		controlLine(c, text, 12, 0, 400, p.TextTertiary).Ellipsis("…").Shrink(1).MinWidth(0)
		if kind == "add" {
			link := ui.ButtonBase(c).Height(22).Padding(0, 2).Center().Label("Add Provider…").Cursor(ui.CursorPointer).Shrink(0)
			link.Children(func() {
				controlText(c, "Add Provider…", 12, 0, 500, p.TextLink)
			})
			if link.Clicked() {
				a.openAddProvider()
			}
		}
		if kind == "switch" {
			x := ui.ButtonBase(c).Size(16, 16).Radius(8).Center().Label("Dismiss").Cursor(ui.CursorPointer).Shrink(0)
			x.Children(func() {
				ui.Icon(c, iconX).Size(12, 12).TextColor(p.TextTertiary)
			})
			if x.Clicked() {
				a.dismissSwitchNote(conv)
			}
		}
	})
}

// replacesLater is the editor hint. One message is singular.
func replacesLater(n int) string {
	if n == 1 {
		return "Replaces 1 later message"
	}
	return fmt.Sprintf("Replaces %d later messages", n)
}

// inlineEditor replaces one user message with a composer-shaped field.
func (a *App) inlineEditor(c *ui.Context, conv *Conversation) {
	p := a.look
	col := a.readingColumn(c)
	col.Label("message editor")
	col.Children(func() {
		plate := ui.Box(c).FillWidth()
		plate.Draw(func(painter *ui.Painter, r ui.Rect) {
			paintDropShadow(painter, r, radiusComposer, shadowComposer, p.ShadowMd)
		})
		plate.Children(func() {
			shell := ui.Column(c).FillWidth().Padding(10, 8, 8, 12).Gap(8).Radius(radiusComposer).Background(p.LayerComposer).Border(1, p.LayerComposerBorder)
			shell.Children(func() {
				if len(a.edit.quotes) > 0 {
					ui.Column(c).FillWidth().Gap(quoteCardGap).Children(func() {
						for i := range a.edit.quotes {
							a.quoteCard(c, a.edit.quotes[i], quoteCardEdit, a.edit.msgID, i)
						}
					})
				}
				input := ui.TextArea(c.Key("inline-edit-"+a.edit.msgID), &a.edit.text).
					Placeholder("Message Cove").
					Label("Edit message").
					FontSize(14).
					LineHeight(21.0/14.0).
					Lines(1, 10).
					MinHeight(0).
					AutoFocus()
				input.Padding(0).MinHeight(21).Border(0, ui.Transparent).Background(ui.Transparent).FocusRing(false)
				paintPlaceholder(c, input, p.TextTertiary)
				input.OnTextKey(func(k ui.TextKey) bool {
					if k.Key == ui.KeyBackspace && k.Mods == ui.Cmd {
						a.askDelete()
						return true
					}
					if k.Key == ui.KeyEscape && k.Mods == 0 {
						if a.suppressEditEscape || a.confirmKind != "" {
							return true
						}
						a.cancelEdit()
						return true
					}
					// Same send key as the composer. Return sends unless
					// Settings › Send with is Command-Return; Shift+Return
					// stays a newline either way.
					sendMods := ui.Modifiers(0)
					if a.data.SendWith == "mod" {
						sendMods = ui.Cmd
					}
					if k.Key != ui.KeyEnter || k.Mods != sendMods {
						return false
					}
					if a.suppressEditEscape || a.confirmKind != "" {
						return true
					}
					a.edit.text = k.Text()
					a.submitEdit()
					return true
				})
				ui.Row(c).FillWidth().Height(composerToolH).Gap(4).AlignItems(ui.Center).Children(func() {
					a.modelMenu(c, conv, "edit").MinWidth(0)
					a.effortMenu(c, conv, "edit").Shrink(0)
					ui.Spacer(c)
					// Label/Small (12/15) in text/secondary. Only when later
					// messages exist. It gives up width before the model name.
					if n := laterCount(conv, a.edit.msgID); n > 0 {
						ui.Text(c, replacesLater(n)).FontSize(12).FixedLineHeight(15).TextColor(p.TextSecondary).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1000)
					}
					cancel := ui.ButtonBase(c).Height(22).Padding(0, 6).Radius(radiusRow).AlignItems(ui.Center).Label("Cancel").Cursor(ui.CursorPointer).Shrink(0)
					if cancel.Hovered() {
						cancel.Background(p.FillHover)
					}
					cancel.Children(func() {
						controlText(c, "Cancel", 12, 0, 400, p.TextSecondary)
					})
					if cancel.Clicked() {
						a.cancelEdit()
					}
					if a.cardContentWidth(c) >= 560 {
						ui.Row(c).Height(18).Padding(0, 5).Radius(4).Background(p.ControlBg).Border(0.5, p.ControlBorder).Center().Shrink(0).Label("esc keycap").Children(func() {
							controlText(c, "esc", 11, 0, 400, p.TextTertiary)
						})
					}
					send := a.roundButton(c, "Send edit", iconSend, true, strings.TrimSpace(a.edit.text) == "" && len(a.edit.quotes) == 0).Shrink(0)
					if send.Clicked() {
						a.submitEdit()
					}
				})
			})
		})
	})
}

func (a *App) effortMenu(c *ui.Context, conv *Conversation, slot string) ui.Element {
	p := a.look
	unsupported := reasoningUnsupported(conv.Config)
	word := effortLabel(conv.Config.Reasoning)
	if unsupported {
		word = "Default"
	}
	// Plain chip: sparkle and the effort word, no control fill. The
	// accessible name still says Reasoning so the menu is findable.
	b := ui.ButtonBase(c.Key("effort-menu-"+slot)).Height(chipH).Gap(5).Padding(0, 5, 0, 7).Radius(radiusRow).Label("Reasoning: " + word).Cursor(ui.CursorPointer)
	fg := p.TextSecondary
	switch {
	case unsupported:
		b.Opacity(0.45)
	case b.Pressed():
		b.Background(p.FillPressed)
		fg = p.TextPrimary
	case b.Hovered():
		b.Background(p.FillHover)
		fg = p.TextPrimary
	}
	if !unsupported {
		chatID := ""
		if conv != nil {
			chatID = conv.ID
		}
		cur := a.conversation(chatID)
		current := "Default"
		if cur != nil {
			current = effortLabel(cur.Config.Reasoning)
		}
		a.bindPopover(c, "effort-"+slot, b, popSpec{
			Name:          "Reasoning",
			Items:         effortMenuEntries(current),
			ComposerFocus: true,
			OnSelect: func(id string) {
				a.setEffortChat(chatID, id)
			},
		})
	}
	b.Children(func() {
		ui.Icon(c, iconSpark).Size(12, 12).TextColor(fg)
		controlText(c, word, 12, 0, 400, fg)
		ui.Icon(c, iconChevron).Size(11, 11).TextColor(p.TextSecondary)
	})
	if unsupported {
		return a.tip(c, b, "This model doesn't support reasoning effort.")
	}
	return a.tip(c, b, "Reasoning effort for this chat")
}

// effortMenuEntries is Off / Default, a separator, then the effort levels.
// The same order is the Settings segmented control.
func effortMenuEntries(current string) []popEntry {
	var out []popEntry
	for _, name := range effortOptions {
		if name == "Minimal" {
			out = append(out, popEntry{Kind: popSep})
		}
		out = append(out, popEntry{
			Kind: popItem, ID: name, Text: name, Checkable: true, Checked: name == current,
			MarkKind: "effort", MarkModel: name, MarkOn: true,
		})
	}
	return out
}

func (a *App) roundButton(c *ui.Context, label string, glyph *ui.SVG, primary, disabled bool) ui.Element {
	p := a.look
	// KeepFocus: a click must not move first responder off the text view.
	// pointerDown (ui/input.go) would otherwise focus this button.
	b := ui.ButtonBase(c).Size(sendSize, sendSize).Radius(sendSize / 2).Center().Label(label).Disabled(disabled).Cursor(ui.CursorPointer).KeepFocus()
	bg, fg := p.TextPrimary, p.StopGlyph
	if primary {
		bg, fg = p.ButtonPrimaryBg, p.ButtonPrimaryText
		if disabled {
			bg, fg = p.FillPressed, p.TextTertiary
		} else if p.Focused && b.Pressed() {
			bg = p.AccentPressed
		} else if p.Focused && b.Hovered() {
			bg = p.AccentHover
		}
	}
	b.Background(bg)
	b.Children(func() {
		if glyph != nil {
			ui.Icon(c, glyph).Size(14, 14).TextColor(fg)
		}
	})
	return b
}

func (a *App) renameModal(c *ui.Context) {
	ui.Modal(c, &a.renameOpen, func() {
		ui.Column(c).Width(380).Gap(12).Children(func() {
			ui.Text(c, "Rename chat").FontSize(18).Bold()
			in := ui.TextInput(c, &a.renameText).Label("Name").AutoFocus()
			if in.Submitted() {
				a.commitRename()
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if a.sheetButton(c, "Cancel", false).Clicked() {
					a.renameOpen = false
				}
				if a.sheetButton(c, "Save name", true).Clicked() {
					a.commitRename()
				}
			})
		})
	})
}

func (a *App) settingsModal(c *ui.Context) {
	wasOpen := a.modalOpen
	p := a.look
	_, winH := c.Size()
	maxH := winH - 96
	if maxH > 600 {
		maxH = 600
	}
	if maxH < 280 {
		maxH = 280
	}
	ui.DialogBase(c, &a.modalOpen, func(back, panel ui.Element) {
		back.Background(p.LayerScrim)
		back.Justify(ui.Start).AlignItems(ui.Center).Padding(64, 20, 16, 20)
		panel.Padding(0).Gap(0).Width(560).MaxHeight(maxH).Radius(radiusComposer).Background(p.LayerSheet).Border(0.5, p.LayerOverlayBorder).Clip()
		panel.Shadow(0, 2, 6, 0, p.ShadowSm).Shadow(0, 24, 60, -8, p.ShadowLg)
		if !a.instant && !a.reduceMotion {
			panel.Transition(ui.ElementTransition{
				Enter:    &ui.Motion{Scale: modalScale, Opacity: 0},
				Duration: motionModal,
				Ease:     easeEnter,
			})
		}
		a.formDefault = false
		a.settingsHeader(c)
		ui.Scroll(c).FillWidth().Grow(1).TrackScroll(&a.formScroll).Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Stretch).Padding(2, 20, 16, 20).Gap(18).Children(func() {
				a.configForm(c)
			})
		})
		if a.formError != "" {
			ui.Text(c, a.formError).TextColor(p.StatusDanger).Padding(0, 20, 8, 20)
		}
		ui.Row(c).Padding(12, 20, 16, 20).Gap(8).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(p.SeparatorDefault).Children(func() {
			if a.sheetButton(c, "Use Defaults", false).Clicked() {
				a.applySnapshot(a.snapshotFrom(a.data.Defaults))
				a.formRounds = clampRounds(a.data.MaxRounds)
				a.roundsScope = ""
				a.roundsText = ""
				a.captureDraft(0)
			}
			ui.Spacer(c)
			if a.sheetButton(c, "Cancel", false).Clicked() {
				a.dropDrafts()
				a.modalOpen = false
			}
			if a.sheetButton(c, "Save", true).Clicked() && a.saveForm(false) {
				a.dropDrafts()
				a.modalOpen = false
			}
		})
	})
	if wasOpen && !a.modalOpen {
		a.dropDrafts()
	}
}

func (a *App) sheetButton(c *ui.Context, label string, primary bool) ui.Element {
	p := a.look
	b := ui.ButtonBase(c).Height(28).MinWidth(76).Padding(0, 14).Radius(radiusRow).Center().Label(label).Cursor(ui.CursorPointer)
	fg := p.TextPrimary
	weight := 400
	switch {
	case primary && p.Focused:
		b.Background(p.ButtonPrimaryBg)
		fg = p.ButtonPrimaryText
		weight = 500
		if b.Pressed() {
			b.Background(p.AccentPressed)
		} else if b.Hovered() {
			b.Background(p.AccentHover)
		}
	case primary:
		b.Background(p.ButtonPrimaryBg).Border(0.5, p.ControlBorder)
		fg = p.ButtonPrimaryText
	default:
		b.Background(p.ControlBg).Border(0.5, p.ControlBorder).Shadow(0, 0.5, 1, 0, p.ShadowMd)
		if b.Pressed() {
			b.Background(p.FillPressed)
		} else if b.Hovered() {
			b.Background(p.FillHover)
		}
	}
	b.Children(func() {
		controlText(c, label, 13, 0, weight, fg)
	})
	return b
}

func (a *App) settingsHeader(c *ui.Context) {
	p := a.look
	ui.Row(c).Padding(16, 20, 12, 20).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Gap(1).Grow(1).Children(func() {
			ui.Text(c, "Chat Settings").FontSize(15).FontWeight(600)
			ui.Text(c, "Applies to this chat only. App-wide defaults live in Settings (⌘,).").FontSize(12).TextColor(p.TextSecondary)
		})
	})
}

func (a *App) settingsTabs(c *ui.Context) {
	p := a.look
	labels := [2]string{"This Chat", "Defaults"}
	parts := ui.SegmentedBase(c, &a.settingsTab, 2)
	parts.Track.Width(196).Height(22).Padding(1).Gap(1).Radius(radiusRow).Background(p.FillQuaternary).Label("Settings section")
	parts.Track.Children(func() {
		for i, label := range labels {
			seg := parts.Segment(i).Grow(1).Height(20).Radius(5).Gap(5).Center()
			if i == a.settingsTab {
				seg.Background(p.ControlSegSelected).Shadow(0, 0.5, 1.5, 0, p.ShadowMd)
			} else if seg.Hovered() {
				seg.Background(p.FillHover)
			}
			a.fadeColors(seg)
			seg.Children(func() {
				controlText(c, label, 12, 0, 500, p.TextPrimary)
				if a.draftDirty(i) {
					ui.Box(c).Size(6, 6).Radius(3).Background(p.AccentDefault)
				}
			})
		}
	})
}

func (a *App) configForm(c *ui.Context) {
	if a.formDefault {
		a.appearanceGroup(c)
	}
	a.settingsGroup(c, "Endpoint", func() {
		prev := a.form.Provider
		a.settingsRow(c, "Provider", true, false, func(el *ui.Element) {
			*el = a.providerSelect(c)
		})
		if a.form.Provider != prev {
			applyPreset(&a.form)
		}
		a.settingsRow(c, "Model", false, false, func(el *ui.Element) {
			*el = a.lineField(c, &a.form.Model, "gpt-4o-mini", "")
		})
		a.settingsRow(c, "Base URL", false, false, func(el *ui.Element) {
			*el = a.lineField(c, &a.form.BaseURL, "https://api.openai.com/v1", "")
		})
		a.settingsRow(c, "API key", false, false, func(el *ui.Element) {
			*el = a.apiKeyField(c)
		})
	})
	hintCfg := a.form
	if hintCfg.Provider == "Custom" {
		hintCfg.ReasoningStyle = a.formStyle
	}
	a.settingsGroup(c, "Generation", func() {
		a.settingsRow(c, "Temperature", true, false, func(el *ui.Element) {
			p := a.look
			slider := a.temperatureSlider(c)
			*el = slider
			val := ui.Textf(c, "%.1f", a.formTemp).Font("ui-monospace").FontSize(12).MinWidth(26)
			if !a.formTempOn {
				val.Opacity(0.4)
			}
			sw := ui.Switch(c, &a.formTempOn).Label("Send temperature").Size(32, 18)
			if !p.Focused {
				sw.Opacity(0.72)
			}
		})
		a.settingsRow(c, "Max tokens", false, false, func(el *ui.Element) {
			ui.Text(c, "0 omits the limit").FontSize(12).TextColor(a.look.TextSecondary)
			*el = a.maxTokenStepper(c)
		})
		a.settingsLine(c, "Max context", "Question + answer = 1 round", false, false, func(el *ui.Element) {
			*el = a.maxContextControl(c, "chat", a.formRounds, func(n int) {
				a.formRounds = n
			})
		})
		a.settingsRow(c, "Reasoning", false, false, func(el *ui.Element) {
			p := a.look
			idx := effortIndex(a.formEffort)
			if reasoningUnsupported(hintCfg) {
				idx = effortIndex("Default")
			}
			// Content-sized segments at the design's 7pt padding. The
			// themed control's button padding is wider than the row.
			parts := ui.SegmentedBase(c, &idx, len(effortOptions))
			track := parts.Track
			track.Height(22).Padding(1).Gap(1).Radius(radiusRow).Background(p.FillQuaternary).Label("Reasoning effort")
			track.Children(func() {
				for i, label := range effortOptions {
					seg := parts.Segment(i).Height(20).Radius(5).Padding(0, 7).Center()
					if i == idx {
						seg.Background(p.ControlSegSelected).Shadow(0, 0.5, 1.5, 0, p.ShadowMd)
					} else if seg.Hovered() {
						seg.Background(p.FillHover)
					}
					weight := 400
					if i == idx {
						weight = 500
					}
					seg.Children(func() {
						controlText(c, label, 12, 0, weight, p.TextPrimary)
					})
				}
			})
			*el = track
			if reasoningUnsupported(hintCfg) {
				track.Disabled(true)
				a.tip(c, track, "This model doesn't support reasoning effort.")
			} else if track.Changed() && idx >= 0 && idx < len(effortOptions) {
				a.formEffort = effortOptions[idx]
			}
		})
		if a.form.Provider == "Custom" {
			a.settingsRow(c, "Reasoning style", false, false, func(el *ui.Element) {
				sel := ui.Select(c, &a.formStyle, reasoningStyles).Height(22).MinWidth(160)
				*el = sel
			})
		}
	})
	a.settingsGroup(c, "Instructions", func() {
		a.settingsStack(c, "System prompt", "Optional instructions for every reply in this chat.", true, func(el *ui.Element) {
			in := a.fieldBox(c, ui.TextArea(c, &a.form.SystemPrompt).Placeholder("Optional instructions for every reply").Height(64).FillWidth())
			*el = in
		})
	})
	a.settingsGroup(c, "Request", func() {
		a.settingsStack(c, "Extra body", "JSON merged onto the request. Your keys win.", true, func(el *ui.Element) {
			in := a.fieldBox(c, ui.TextArea(c, &a.form.ExtraBody).Placeholder(`{"reasoning_effort":"none"}`).Height(76).FillWidth().Font("ui-monospace").FontSize(12))
			*el = in
		})
		if !a.formDefault {
			a.settingsStack(c, "Last request", "The most recent body sent from this chat. The API key stays in the header.", false, func(*ui.Element) {
				a.lastRequestRow(c)
			})
		}
	})
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func (a *App) appearanceGroup(c *ui.Context) {
	p := a.look
	ui.Column(c).Gap(6).FillWidth().Children(func() {
		a.settingsGroup(c, "Appearance", func() {
			a.settingsRow(c, "Mode", true, false, func(el *ui.Element) {
				*el = a.appearanceControl(c)
			})
		})
		ui.Row(c).FillWidth().Gap(6).Padding(0, 2).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconInfo).Size(13, 13).TextColor(p.TextSecondary)
			ui.Text(c, "Appearance changes apply immediately — no need to save.").FontSize(12).TextColor(p.TextSecondary).Grow(1)
			if !a.appearanceSavedUntil.IsZero() && c.Now().Before(a.appearanceSavedUntil) {
				ui.Text(c, "Saved").FontSize(12).FontWeight(500).TextColor(p.StatusSuccess)
				c.After(a.appearanceSavedUntil.Sub(c.Now()))
			}
		})
	})
}

func (a *App) settingsGroup(c *ui.Context, title string, fn func()) {
	p := a.look
	ui.Column(c).Gap(6).FillWidth().Children(func() {
		if title != "" {
			ui.Text(c, title).FontSize(13).FontWeight(600).Padding(0, 0, 0, 2)
		}
		card := ui.Column(c).Radius(radiusGroup).Background(p.LayerGroup).Clip().FillWidth()
		a.fadeColors(card)
		card.Children(fn)
	})
}

// settingsRow is one line of a grouped list: label on the left, a 300pt
// control column on the right, and a separator inset 12pt. top is a
// multi-line control (a text area), which aligns to the top of the row.
// A click on the label focuses the control fn stores.
// settingsStack puts the title and description above a full-width field.
func (a *App) settingsStack(c *ui.Context, label, desc string, first bool, fn func(*ui.Element)) {
	p := a.look
	ui.Column(c).FillWidth().Children(func() {
		if !first {
			ui.Box(c).Height(1).FillWidth().Margin(0, 0, 0, 12).Background(p.SeparatorDefault)
		}
		ui.Column(c).FillWidth().Padding(8, 12).Gap(6).Children(func() {
			ui.Text(c, label).FontSize(13).TextAlign(ui.Start)
			if desc != "" {
				ui.Text(c, desc).FontSize(12).TextColor(p.TextSecondary).TextAlign(ui.Start)
			}
			var el ui.Element
			fn(&el)
		})
	})
}

func (a *App) settingsRow(c *ui.Context, label string, first, top bool, fn func(*ui.Element)) {
	a.settingsLine(c, label, "", first, top, fn)
}

// settingsLine is settingsRow plus an optional one-line secondary
// description under the label. The control is at most 300 wide and only
// as wide as it needs to be, so a long label stays on one line.
func (a *App) settingsLine(c *ui.Context, label, hint string, first, top bool, fn func(*ui.Element)) {
	p := a.look
	ui.Column(c).FillWidth().Children(func() {
		if !first {
			ui.Box(c).Height(1).FillWidth().Margin(0, 0, 0, 12).Background(p.SeparatorDefault)
		}
		row := ui.Row(c).FillWidth().MinHeight(36).Padding(0, 10, 0, 12).Gap(12).AlignItems(ui.Center)
		if hint != "" {
			row.MinHeight(48).Padding(7, 10, 7, 12)
		}
		if top {
			row.AlignItems(ui.Start).Padding(8, 10, 8, 12)
		}
		row.Children(func() {
			var el ui.Element
			// The label sits on the row's leading edge. A growing button
			// centers its text by default, which reads as a form.
			lab := ui.ButtonBase(c).Grow(1).MinWidth(0).MinHeight(20).Padding(0).Justify(ui.Start).AlignItems(ui.Center).Cursor(ui.CursorDefault)
			if top {
				lab.AlignItems(ui.Start).Padding(3, 0, 0, 0)
			}
			lab.Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
					ui.Text(c, label).FontSize(13).TextAlign(ui.Start).SingleLine().Ellipsis("…")
					if hint != "" {
						hintText := ui.Text(c, hint).FontSize(12).TextColor(p.TextSecondary).TextAlign(ui.Start)
						if len(hint) > 48 {
							hintText.FillWidth()
						} else {
							hintText.SingleLine().Ellipsis("…")
						}
					}
				})
			})
			ctl := ui.Row(c).MaxWidth(300).Justify(ui.End).AlignItems(ui.Center).Gap(8).Shrink(0)
			if top {
				ctl.Grow(1).MaxWidth(1e6).AlignItems(ui.Start)
			}
			ctl.Children(func() { fn(&el) })
			if lab.Clicked() && el.Valid() {
				el.Focus()
			}
		})
	})
}

func (a *App) fieldBox(c *ui.Context, el ui.Element) ui.Element {
	p := a.look
	return el.Radius(radiusRow).Background(p.ControlField).Border(1, p.ControlFieldBorder)
}

// lineField is a 22pt settings field whose 13pt text is centered. The
// themed input's 6pt vertical padding sits the glyphs low in that height.
func (a *App) lineField(c *ui.Context, value *string, placeholder, font string) ui.Element {
	p := a.look
	in := ui.TextInputBase(c, value).Placeholder(placeholder).Height(22).Width(280).MaxWidth(300).FontSize(13).LineHeight(16.0/13.0).Padding(3, 7)
	if font != "" {
		in.Font(font).FontSize(12).LineHeight(16.0 / 12.0)
	}
	in.Radius(radiusRow).Background(p.ControlField).Border(1, p.ControlFieldBorder).FocusRing(false)
	paintPlaceholder(c, in, p.TextTertiary)
	return in
}

// providerSelect is the settings popup. The trigger shows the provider's
// color dot; the menu is an overlay with the 0.5pt border.
func (a *App) providerSelect(c *ui.Context) ui.Element {
	p := a.look
	b := ui.ButtonBase(c.Key("chat-provider"))
	b.Height(22).MinWidth(168).Gap(6).Padding(0, 7, 0, 8).Radius(radiusRow).Background(p.ControlBg).Border(0.5, p.ControlBorder).AlignItems(ui.Center).Label("Provider").Cursor(ui.CursorPointer)
	b.Shadow(0, 0.5, 1, 0, p.ShadowMd)
	if b.Pressed() {
		b.Background(p.FillPressed)
	} else if b.Hovered() {
		b.Background(p.FillHover)
	}
	b.Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).Background(a.look.providerColor(a.form.Provider, a.form.Provider == "Custom")).Shrink(0)
		controlText(c, a.form.Provider, 13, 0, 400, p.TextPrimary).Grow(1)
		ui.Icon(c, iconChevron).Size(11, 11).TextColor(p.TextSecondary).Shrink(0)
	})
	var items []popEntry
	for _, name := range providerNames() {
		name := name
		dot := p.providerColor(name, name == "Custom")
		items = append(items, popEntry{
			Kind: popItem, ID: name, Text: name, Checkable: true, Checked: name == a.form.Provider,
			Body: func(c *ui.Context, fg ui.Color) {
				ui.Box(c).Size(6, 6).Radius(3).Background(dot).Shrink(0)
				controlText(c, name, 13, 0, 400, fg)
			},
		})
	}
	a.bindPopover(c, "chat-provider", b, popSpec{
		Name:  "Provider",
		Items: items,
		OnSelect: func(id string) {
			a.form.Provider = id
		},
	})
	return b
}

// maxTokenStepper is an 80pt field, a 4pt gap, and a separate 13×22
// stepper (radius 5) with the up and down chevrons. The value stays in
// range; 0 means the request omits max_tokens.
func (a *App) maxTokenStepper(c *ui.Context) ui.Element {
	p := a.look
	shown := strconv.FormatFloat(a.formMax, 'f', 0, 64)
	var focus ui.Element
	row := ui.Row(c).Gap(4).AlignItems(ui.Center)
	row.Children(func() {
		field := ui.Row(c).Width(80).Height(22).Padding(0, 7).Radius(radiusRow).Background(p.ControlField).Border(1, p.ControlFieldBorder).AlignItems(ui.Center)
		field.Children(func() {
			in := ui.TextInputBase(c, &shown).Font("ui-monospace").FontSize(12).LineHeight(16.0/12.0).Grow(1).MinWidth(0).Padding(3, 0)
			in.Border(0, ui.Transparent).Background(ui.Transparent).FocusRing(false)
			focus = in
			if in.Changed() {
				if v, err := strconv.ParseFloat(strings.TrimSpace(shown), 64); err == nil {
					a.formMax = math.Max(0, math.Min(200000, math.Round(v)))
				}
			}
		})
		// One control: the two halves share the rounded rect.
		ui.Column(c).Width(13).Height(22).Radius(5).Background(p.ControlBg).Border(0.5, p.ControlBorder).Clip().Shrink(0).Children(func() {
			up := ui.ButtonBase(c).Height(11).FillWidth().Center().Label("Increase").Cursor(ui.CursorPointer)
			up.Children(func() {
				ui.Icon(c, iconChevron).Size(8, 8).Rotate(180).TextColor(p.TextSecondary)
			})
			if up.Clicked() {
				a.formMax = math.Min(200000, a.formMax+1)
			}
			down := ui.ButtonBase(c).Height(11).FillWidth().Center().Label("Decrease").Cursor(ui.CursorPointer)
			down.Children(func() {
				ui.Icon(c, iconChevron).Size(8, 8).TextColor(p.TextSecondary)
			})
			if down.Clicked() {
				a.formMax = math.Max(0, a.formMax-1)
			}
		})
	})
	if focus.Valid() {
		return focus
	}
	return row
}

// temperatureSlider draws the 170pt track. Off, the fill is the gray
// track color rather than a faded accent.
func (a *App) temperatureSlider(c *ui.Context) ui.Element {
	p := a.look
	const inset = float32(8)
	s := ui.SliderBase(c, &a.formTemp, 0, 2).Step(0.1).Width(170).Height(16).PaddingX(inset).FocusRing(false).Label("Temperature")
	if !a.formTempOn {
		s.Disabled(true)
	}
	s.Draw(func(painter *ui.Painter, r ui.Rect) {
		innerW := r.W - 2*inset
		if innerW < 1 {
			return
		}
		frac := float32(0)
		if a.formTemp > 0 {
			frac = float32(a.formTemp / 2)
		}
		if frac < 0 {
			frac = 0
		}
		if frac > 1 {
			frac = 1
		}
		h := float32(4)
		cy := r.Y + r.H/2
		track := ui.Rect{X: r.X + inset, Y: cy - h/2, W: innerW, H: h}
		fill := p.AccentDefault
		if !a.formTempOn {
			fill = p.TextTertiary
		}
		painter.Fill(track, p.SliderTrack, h/2)
		if frac > 0 {
			painter.Fill(ui.Rect{X: track.X, Y: track.Y, W: track.W * frac, H: h}, fill, h/2)
		}
		kw := float32(16)
		x := r.X + inset + frac*innerW - kw/2
		knob := ui.Rect{X: x, Y: r.Y, W: kw, H: r.H}
		painter.Shadow(knob, kw/2, 0, 0.5, 1.5, 0, p.ShadowMd)
		painter.Fill(knob, p.ControlKnob, kw/2)
		painter.Stroke(knob, p.ControlBorder, kw/2, 0.5)
	})
	return s
}

// appearanceControl matches the This Chat / Defaults track: gray track,
// white selected segment.
func (a *App) appearanceControl(c *ui.Context) ui.Element {
	p := a.look
	idx := appearanceIndex(a.data.Appearance)
	labels := [3]string{"System", "Light", "Dark"}
	parts := ui.SegmentedBase(c, &idx, len(labels))
	track := parts.Track
	track.Width(216).Height(22).Padding(1).Gap(1).Radius(radiusRow).Background(p.FillQuaternary).Label("Appearance mode")
	track.Children(func() {
		for i, label := range labels {
			seg := parts.Segment(i).Grow(1).Height(20).Radius(5).Center()
			if i == idx {
				seg.Background(p.ControlSegSelected).Shadow(0, 0.5, 1.5, 0, p.ShadowMd)
			} else if seg.Hovered() {
				seg.Background(p.FillHover)
			}
			weight := 400
			if i == idx {
				weight = 500
			}
			seg.Children(func() {
				controlLine(c, label, 12, 0, weight, p.TextPrimary)
			})
		}
	})
	if track.Changed() {
		a.setAppearance(appearanceFromIndex(idx), c.Now())
	}
	return track
}

func (a *App) lastRequestRow(c *ui.Context) {
	p := a.look
	full := oneLine(abbreviateRequest(a.chatLastRequest()))
	preview := ui.Text(c, full).Font("ui-monospace").FontSize(12).TextColor(p.TextSecondary).MaxLines(4).Ellipsis("…").FillWidth().MinWidth(0)
	a.tip(c, preview, full)
	btn := ui.ButtonBase(c).Height(22).Gap(1).Padding(0).AlignItems(ui.Center).Label("View JSON").Cursor(ui.CursorPointer).Shrink(0)
	btn.Children(func() {
		controlText(c, "View JSON", 12, 0, 500, p.TextLink)
		ui.Icon(c, iconChevronRight).Size(11, 11).TextColor(p.TextLink)
	})
	if btn.Clicked() {
		a.requestOpen = true
	}
}

func (a *App) prefsView(c *ui.Context) {
	a.inRender = true
	defer func() { a.inRender = false }()
	a.applyLook(c)
	t := c.Theme()
	title := "Conversation settings"
	subtitle := "These apply to this chat only. New chats keep the defaults."
	saveLabel := "Save settings"
	if a.formDefault {
		title = "Default model"
		subtitle = "New chats start with these settings."
		saveLabel = "Save defaults"
	}
	ui.Column(c).Fill().Background(t.Background).Padding(20, 22, 16, 22).Gap(12).Children(func() {
		ui.Column(c).Gap(4).Children(func() {
			ui.Text(c, title).FontSize(20).Bold()
			ui.Text(c, subtitle).TextColor(t.TextMuted)
		})
		ui.Scroll(c).Grow(1).TrackScroll(&a.formScroll).Children(func() {
			a.configForm(c)
		})
		if a.formError != "" {
			ui.Text(c, a.formError).TextColor(t.Danger)
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if a.sheetButton(c, "Cancel", false).Clicked() {
				a.closePrefs()
			}
			if a.sheetButton(c, saveLabel, true).Clicked() && a.saveForm(a.formDefault) {
				a.refresh()
				a.closePrefs()
			}
		})
	})
}

func (a *App) requestModal(c *ui.Context) {
	t := c.Theme()
	ui.Modal(c, &a.requestOpen, func() {
		ui.Column(c).Width(560).Gap(12).Children(func() {
			ui.Text(c, "Last request").FontSize(18).Bold()
			ui.Text(c, "The full JSON of this chat's most recent request. The API key stays in the header.").TextColor(t.TextMuted)
			ui.Scroll(c).MaxHeight(380).Children(func() {
				ui.Text(c, fullRequestText(a.chatLastRequest())).Selectable().Font("monospace").FontSize(12)
			})
			ui.Row(c).Justify(ui.End).Children(func() {
				if a.sheetButton(c, "Close", false).Clicked() {
					a.requestOpen = false
				}
			})
		})
	})
}

func (a *App) chatLastRequest() string {
	if conv := a.active(); conv != nil {
		return conv.LastRequest
	}
	return ""
}

func fullRequestText(body string) string {
	if strings.TrimSpace(body) == "" {
		return "No request has been sent from this chat yet."
	}
	return body
}

// abbreviateRequest is the settings preview. The messages array is replaced
// with a count so the field stays short; the More menu shows the full body.
func abbreviateRequest(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return "No request has been sent from this chat yet."
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return body
	}
	msgs, ok := payload["messages"].([]any)
	if !ok {
		return body
	}
	payload["messages"] = []any{fmt.Sprintf("… %d messages …", len(msgs))}
	b, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return prettyJSON(b)
}

func reasoningFieldHint(cfg Config) string {
	if reasoningUnsupported(cfg) {
		return "This model doesn't support reasoning effort, so Cove sends nothing and the picker stays on Default."
	}
	return "Default omits the parameter, so a server that does not understand it still answers. " + reasoningHint(cfg)
}

func reasoningHint(cfg Config) string {
	switch reasoningStyle(cfg) {
	case "openai":
		return "OpenAI receives reasoning_effort: none, minimal, low, medium, or high."
	case "xai":
		return "xAI receives reasoning_effort. Off sends none; Grok 4.5 and newer may reject that because those models cannot disable reasoning."
	case "openrouter":
		return "OpenRouter receives a reasoning object with effort and enabled. The reply still includes the reasoning text."
	case "deepseek":
		return "Off sends thinking.type set to disabled and no reasoning_effort, which is what the official API honors. Other levels send thinking.type enabled plus a sibling reasoning_effort. Off does not change the model id. A relay that drops thinking will not see it; put the fields that relay forwards in Extra body."
	case "ollama":
		return "Ollama receives reasoning_effort and the think option. Off sends none and think set to false."
	default:
		return "This provider sends no reasoning field."
	}
}

// composerModelParts is the chip text. provider is set only when this
// model's leaf name is enabled on two or more ready providers. The model
// is the leaf name; the button ellipsizes that run and leaves provider whole.
func (a *App) composerModelParts(cfg Config) (provider, model string) {
	// A removed connection keeps its stored id. With no provider left to
	// send on, the button is "No model" and the reference stays put.
	if id := strings.TrimSpace(cfg.ConnectionID); id != "" && a.connByID(id) == nil {
		return "", "No model"
	}
	if len(a.data.Connections) == 0 {
		return "", "No model"
	}
	model = shortModel(cfg.Model)
	if model == "" || model == "Model" {
		return "", "No model"
	}
	if name := a.providerLabel(cfg); name != "" && a.modelNameShared(cfg.Model) {
		return name, model
	}
	return "", model
}

// composerModelLabel is the model chip's accessible name, without the
// "Model " prefix. A single gpt-4o stays "gpt-4o". Two enabled copies
// read "OpenCode · deepseek-v4.1".
func (a *App) composerModelLabel(cfg Config) string {
	provider, model := a.composerModelParts(cfg)
	if provider == "" {
		return model
	}
	return provider + " · " + model
}

// modelNameShared reports whether the visible model name is enabled on at
// least two ready providers. The match is the name the chip draws, so
// "openai/gpt-4o" and "gpt-4o" count as the same name.
func (a *App) modelNameShared(model string) bool {
	want := shortModel(model)
	if want == "" || want == "Model" {
		return false
	}
	seen := map[string]bool{}
	n := 0
	for i := range a.data.Connections {
		c := &a.data.Connections[i]
		if !a.labelReady(c) || seen[c.ID] {
			continue
		}
		catalog := c.Provider
		if c.Custom || catalog == "" {
			catalog = c.Name
		}
		hit := false
		for _, id := range a.enabledModelIDs(catalog, c) {
			if shortModel(id) == want {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		seen[c.ID] = true
		n++
		if n >= 2 {
			return true
		}
	}
	return false
}

// labelReady is providerReady for the chip and the title. During a frame
// it uses only the key cache, so a paint does not read the credential store.
func (a *App) labelReady(c *Connection) bool {
	if c == nil || c.Probe == probeInvalid {
		return false
	}
	keyless := !c.Custom && keylessProvider(c.Provider)
	if keyless {
		return strings.TrimSpace(c.BaseURL) != "" || c.Probe == probeOK
	}
	if a.inRender {
		cache := a.keyStore()
		if cache != nil {
			if value, known := cache.peek(c.ID); known {
				return strings.TrimSpace(value) != ""
			}
		}
		// A provider loaded from disk is listed before its secret is read.
		return a.assumedKeys[c.ID]
	}
	return a.connectionHasKey(c)
}

func (a *App) subtleModel(conv *Conversation) string {
	if conv == nil {
		return ""
	}
	if a.composerModelLabel(conv.Config) == "No model" {
		if len(a.menuModels()) == 0 {
			return "No provider yet"
		}
		return "No model"
	}
	model := shortModel(conv.Config.Model)
	host := displayHost(conv.Config.BaseURL)
	head := model
	if a.modelNameShared(conv.Config.Model) {
		if name := a.providerLabel(conv.Config); name != "" && model != "" && model != "Model" {
			head = name + " · " + model
		}
	}
	switch {
	case head != "" && head != "Model" && host != "":
		return head + " · " + host
	case host != "":
		return host
	default:
		return head
	}
}

func (a *App) providerLabel(cfg Config) string {
	if c := a.connByID(cfg.ConnectionID); c != nil {
		if name := connDisplay(c); name != "" {
			return name
		}
	}
	return strings.TrimSpace(cfg.Provider)
}

func (a *App) openProviderSettings() {
	a.settingsSection = "models"
	a.settingsPage = settingsList
	a.openDefaults()
}

func modelLabel(conv *Conversation) string {
	if conv == nil {
		return "No model"
	}
	if s := strings.TrimSpace(conv.Config.Model); s != "" {
		return s
	}
	return "No model"
}

// shortModel is the leaf the chip compares and draws. A catalog id
// "org/deepseek-v4.1" and a bare "deepseek-v4.1" are the same name.
// The button's width cuts the name with an ellipsis; cutting it here
// would make two different long names look shared.
func shortModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return "Model"
	}
	if i := strings.LastIndex(model, "/"); i >= 0 && i < len(model)-1 {
		model = model[i+1:]
	}
	return model
}

func formatStamp(c *ui.Context, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	now := c.Now()
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04")
	}
	if t.Year() == now.Year() {
		return t.Format("Jan 2, 15:04")
	}
	return t.Format("Jan 2 2006, 15:04")
}

func relativeTime(now, t time.Time) string {
	if t.IsZero() || now.Before(t) {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	default:
		return ""
	}
}

func (a *App) codeBlock(c *ui.Context, b *markdown.CodeBlockView) {
	p := a.look
	lang := b.Language
	if lang == "" {
		lang = "text"
	}
	slot := a.hScrollSlot(a.hScrollKey("code", b.Index))
	edge := scrollEdge(p.CodeBg, a.cardFlat())
	ui.Column(c).FillWidth().MinWidth(0).Radius(radiusCode).Background(p.CodeBg).Border(1, p.CodeBorder).Clip().AlignSelf(ui.Stretch).Label("code block").Children(func() {
		ui.Row(c).Height(28).Padding(0, 4, 0, 12).Background(p.CodeHeader).AlignItems(ui.Center).Label("code header").Children(func() {
			ui.Text(c, lang).FontSize(11).FontWeight(500).TextColor(p.TextSecondary).Unselectable()
			ui.Spacer(c)
			b.CopyButton(c)
		})
		// Body hardcodes 0.25em (3.5px at 14pt) above the code. The design
		// pre is 10px 12px 12px, and the sides and bottom already come
		// from CodeBlockPadding.
		ui.Column(c).Padding(6.5, 0, 0, 0).AlignSelf(ui.Stretch).Label("qcode:" + lang).Children(func() {
			ui.Box(c).AlignSelf(ui.Stretch).Children(func() {
				b.Body(c).TrackScroll(&slot.st).AlignSelf(ui.Stretch)
				hScrollFades(c, slot, "code", edge)
			})
		})
	})
}

// maskKey is the design's API key mask: 13 characters, 14 bullets, last 4.
func maskKey(key string) string {
	key = strings.TrimSpace(key)
	r := []rune(key)
	if len(r) == 0 {
		return ""
	}
	if len(r) <= 17 {
		return strings.Repeat("•", len(r))
	}
	return string(r[:13]) + strings.Repeat("•", 14) + string(r[len(r)-4:])
}

// apiKeyField is the masked value with the eye button inside the field.
// Show key reveals an editable input; Hide key returns the mask.
func (a *App) apiKeyField(c *ui.Context) ui.Element {
	p := a.look
	field := ui.Row(c).Height(22).FillWidth().Gap(4).Padding(0, 2, 0, 7).Radius(radiusRow).Background(p.ControlField).Border(1, p.ControlFieldBorder).AlignItems(ui.Center)
	var focus ui.Element
	field.Children(func() {
		if a.showKey {
			in := ui.TextInputBase(c, &a.form.APIKey).Placeholder("sk-…").Grow(1).MinWidth(0).Font("ui-monospace").FontSize(12)
			in.Border(0, ui.Transparent).Background(ui.Transparent).FocusRing(false)
			paintPlaceholder(c, in, p.TextTertiary)
			focus = in
		} else if masked := maskKey(a.form.APIKey); masked != "" {
			ui.Text(c, masked).Font("ui-monospace").FontSize(12).TextColor(p.TextSecondary).Grow(1).MinWidth(0).SingleLine()
		} else {
			ui.Text(c, "sk-…").Font("ui-monospace").FontSize(12).TextColor(p.TextTertiary).Grow(1)
		}
		label := "Show key"
		glyph := iconEye
		if a.showKey {
			label = "Hide key"
			glyph = iconEyeOff
		}
		btn := ui.ButtonBase(c).Size(18, 18).Radius(4).Center().Label(label).Cursor(ui.CursorPointer).Shrink(0)
		a.tip(c, btn, label)
		if btn.Hovered() {
			btn.Background(p.FillHover)
		}
		btn.Children(func() {
			ui.Icon(c, glyph).Size(13, 13).TextColor(p.TextSecondary)
		})
		if btn.Clicked() {
			a.showKey = !a.showKey
		}
	})
	if focus.Valid() {
		return focus
	}
	return field
}
