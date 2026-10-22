package main

import (
	"strconv"

	"github.com/egoist/mygo/ui"
)

// newSessionButton is the 24pt plus.bubble control after Reasoning.
// It stays enabled for the pointer so a disabled button can still show
// its reason. A click does nothing unless a break would actually cut.
func (a *App) newSessionButton(c *ui.Context) ui.Element {
	p := a.look
	ok, why := a.sessionInsertWhy()
	b := ui.ButtonBase(c).Size(24, 24).Radius(radiusRow).Center().Shrink(0).Label("New Session")
	fg := p.TextSecondary
	switch {
	case !ok:
		fg = p.TextTertiary
		b.Cursor(ui.CursorDefault)
	case b.Pressed():
		b.Background(p.FillPressed)
		fg = p.TextPrimary
		b.Cursor(ui.CursorPointer)
	case b.Hovered():
		b.Background(p.FillHover)
		fg = p.TextPrimary
		b.Cursor(ui.CursorPointer)
	default:
		b.Cursor(ui.CursorPointer)
	}
	if ok && !a.instant && !a.reduceMotion {
		b.Transition(ui.ElementTransition{Colors: true, Duration: motionHoverButton, Ease: easeStandard})
	}
	b.Children(func() {
		ui.Icon(c, iconPlusBubble).Size(15, 15).TextColor(fg)
	})
	if b.Clicked() && ok {
		a.InsertSessionBreak()
		if a.composerField.Valid() {
			a.composerField.Focus()
		}
	}
	a.sessionTip(c, b, why)
	return b
}

// sessionTip is "New Session" plus the ⇧⌘N key, above the button.
// why is the secondary reason, empty when the button can cut.
func (a *App) sessionTip(c *ui.Context, el ui.Element, why string) {
	if !el.Valid() {
		return
	}
	p := a.look
	ui.TooltipBase(c, el, func(tip ui.Element) {
		tip.AttachTo(el, ui.AnchorTop, ui.AnchorBottom).Margin(0, 0, 6, 0)
		tip.Padding(3, 7).Radius(4).Background(p.LayerOverlay).Border(0.5, p.LayerOverlayBorder)
		tip.Shadow(0, 8, 24, 0, p.ShadowMd)
		tip.Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				controlText(c, "New Session", 11, 0, 400, p.TextPrimary)
				controlLine(c, "⇧⌘N", 11, 0, 400, p.TextSecondary)
				if why != "" {
					ui.Text(c, "·").FontSize(11).TextColor(p.TextSecondary).SingleLine()
					controlText(c, why, 11, 0, 400, p.TextSecondary)
				}
			})
		})
	})
}

// sessionDivider is one New Session cut in the transcript. An empty
// trailing break reserves a 16pt well on the left and shows a matching
// remove button on hover, so the label stays centered.
func (a *App) sessionDivider(c *ui.Context, conv *Conversation, index int) {
	if conv == nil || index < 0 || index >= len(conv.Breaks) {
		return
	}
	b := conv.Breaks[index]
	p := a.look
	label := SessionLabel(b)
	empty := index == len(conv.Breaks)-1 && b.After >= len(conv.Messages)
	row := ui.Row(c).MaxWidth(readingWidth).FillWidth().MinWidth(0).AlignSelf(ui.Center).Height(20).Gap(10).AlignItems(ui.Center).Label(label).Unselectable()
	a.enterMessage(row, b.ID)
	dim := a.breakAfterEdit(conv, b)
	if dim {
		row.Opacity(0.4)
	}
	row.Children(func() {
		if dim {
			shield := ui.ButtonBase(c).Absolute().Left(0).Right(0).Top(0).Bottom(0).Label("later " + b.ID)
			if shield.Clicked() {
				a.cancelEdit()
			}
		}
		line := func() {
			ui.Box(c).Height(1).Grow(1).Shrink(2).MinWidth(16).Background(p.SeparatorDefault)
		}
		line()
		ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(1).MinWidth(0).Children(func() {
			if empty {
				ui.Box(c).Size(16, 16).Shrink(0)
			}
			ui.Text(c, label).FontSize(11).FixedLineHeight(14).TextColor(p.TextTertiary).SingleLine().Ellipsis("…").MinWidth(0)
			if empty {
				a.sessionRemove(c, row)
			}
		})
		line()
	})
}

func (a *App) sessionRemove(c *ui.Context, row ui.Element) {
	p := a.look
	x := ui.ButtonBase(c).Size(16, 16).Radius(8).Center().Shrink(0).Label("Remove New Session").Cursor(ui.CursorPointer)
	show := row.Hovered() || x.Hovered() || x.Focused()
	fg := p.TextSecondary
	switch {
	case !show:
		x.Opacity(0)
	case x.Pressed():
		x.Background(p.FillPressed)
		fg = p.TextPrimary
	case x.Hovered():
		x.Background(p.FillHover)
		fg = p.TextPrimary
	}
	if !a.instant && !a.reduceMotion {
		x.Transition(ui.ElementTransition{Colors: true, Duration: motionActions, Ease: easeStandard})
	}
	x.Children(func() {
		ui.Icon(c, iconSessionX).Size(9, 9).TextColor(fg)
	})
	if x.Clicked() && show {
		a.DeleteTrailingBreak()
	}
}

func (a *App) breakAfterEdit(conv *Conversation, b SessionBreak) bool {
	if conv == nil || a.edit.convID != conv.ID || a.edit.msgID == "" {
		return false
	}
	for i := range conv.Messages {
		if conv.Messages[i].ID == a.edit.msgID {
			return b.After > i
		}
	}
	return false
}

func (a *App) syncNewSessionMenu() {
	if a.newSessionItem == nil {
		return
	}
	on := a.CanInsertBreak()
	if a.newSessionItem.IsEnabled() != on {
		a.newSessionItem.SetEnabled(on)
	}
}

func (a *App) rememberLimit(scope string, n int) {
	if n < 1 {
		return
	}
	if scope == "global" {
		a.lastLimitGlobal = n
		return
	}
	a.lastLimitChat = n
}

func (a *App) recalledLimit(scope string) int {
	n := a.lastLimitChat
	if scope == "global" {
		n = a.lastLimitGlobal
	}
	if n < 1 || n > 100 {
		return LimitRoundsInitial
	}
	return n
}

// commitRoundsDraft writes the number being typed. Global saves at once.
// A chat value waits in the sheet until Save.
func (a *App) commitRoundsDraft() {
	if a.roundsScope == "" {
		return
	}
	n := parseSessionLimit(a.roundsText)
	scope := a.roundsScope
	a.roundsScope = ""
	a.roundsText = ""
	a.rememberLimit(scope, n)
	if scope == "global" {
		before := a.data.MaxRounds
		a.SetDefaultMaxContextRounds(n)
		if a.data.MaxRounds != before {
			a.appearanceSavedUntil = a.roundsNow.Add(motionCopied)
		}
		return
	}
	a.formRounds = n
}

// maxContextControl is the Unlimited / Limit to popup. Limit to adds a
// 44pt number, a stepper, and rounds/round. The first time Limit to is
// chosen the number is 20, unless this control already had one.
func (a *App) maxContextControl(c *ui.Context, scope string, value int, set func(int)) ui.Element {
	p := a.look
	if value > 0 {
		a.rememberLimit(scope, value)
	}
	mode := "Unlimited"
	if value > 0 {
		mode = "Limit to"
	}
	var focus ui.Element
	row := ui.Row(c).Gap(8).AlignItems(ui.Center).Shrink(0)
	row.Children(func() {
		b := ui.ButtonBase(c.Key("max-context-" + scope))
		b.Height(22).MinWidth(104).Gap(6).Padding(0, 7, 0, 9).Radius(radiusRow).Background(p.ControlBg).Border(0.5, p.ControlBorder).AlignItems(ui.Center).Label("Max context").Cursor(ui.CursorPointer)
		b.Shadow(0, 0.5, 1, 0, p.ShadowMd)
		if b.Pressed() {
			b.Background(p.FillPressed)
		} else if b.Hovered() {
			b.Background(p.FillHover)
		}
		b.Children(func() {
			controlText(c, mode, 13, 0, 400, p.TextPrimary).Grow(1)
			ui.Icon(c, iconChevron).Size(11, 11).TextColor(p.TextSecondary).Shrink(0)
		})
		var choices []popEntry
		for _, opt := range []string{"Unlimited", "Limit to"} {
			choices = append(choices, popEntry{
				Kind: popItem, ID: opt, Text: opt, Checkable: true, Checked: opt == mode,
			})
		}
		a.bindPopover(c, "max-context-"+scope, b, popSpec{
			Name:  "Max context",
			Items: choices,
			OnSelect: func(id string) {
				switch {
				case id == "Limit to" && value <= 0:
					value = a.recalledLimit(scope)
					set(value)
					a.roundsFocus = scope
				case id == "Unlimited" && value > 0:
					a.rememberLimit(scope, value)
					a.roundsScope = ""
					a.roundsText = ""
					set(0)
				}
			},
		})
		if value <= 0 {
			return
		}
		shown := strconv.Itoa(value)
		if a.roundsScope == scope {
			shown = a.roundsText
		}
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			field := ui.Row(c).Width(44).Height(22).Padding(0, 6).Radius(radiusRow).Background(p.ControlField).Border(1, p.ControlFieldBorder).AlignItems(ui.Center)
			field.Children(func() {
				in := ui.TextInputBase(c, &shown).Font("ui-monospace").FontSize(12).LineHeight(16.0/12.0).Grow(1).MinWidth(0).Padding(3, 0).Label("Rounds per session")
				in.Border(0, ui.Transparent).Background(ui.Transparent).FocusRing(false)
				focus = in
				if a.roundsFocus == scope {
					in.AutoFocus()
					a.roundsFocus = ""
				}
				a.roundsNow = c.Now()
				if in.Focused() {
					a.roundsScope = scope
					a.roundsText = shown
				} else if a.roundsScope == scope {
					a.roundsText = shown
					a.commitRoundsDraft()
				}
				if in.Submitted() {
					a.roundsScope = scope
					a.roundsText = shown
					a.commitRoundsDraft()
				}
			})
			ui.Column(c).Width(13).Height(22).Radius(5).Background(p.ControlBg).Border(0.5, p.ControlBorder).Clip().Shrink(0).Children(func() {
				up := ui.ButtonBase(c).Height(11).FillWidth().Center().Label("Increase rounds").Cursor(ui.CursorPointer)
				up.Children(func() {
					ui.Icon(c, iconChevron).Size(8, 8).Rotate(180).TextColor(p.TextSecondary)
				})
				if up.Clicked() {
					n := value
					if n < 100 {
						n++
					}
					a.roundsScope = ""
					a.roundsText = ""
					a.rememberLimit(scope, n)
					set(n)
				}
				down := ui.ButtonBase(c).Height(11).FillWidth().Center().Label("Decrease rounds").Cursor(ui.CursorPointer)
				down.Children(func() {
					ui.Icon(c, iconChevron).Size(8, 8).TextColor(p.TextSecondary)
				})
				if down.Clicked() {
					n := value
					if n > 1 {
						n--
					}
					a.roundsScope = ""
					a.roundsText = ""
					a.rememberLimit(scope, n)
					set(n)
				}
			})
		})
		unit := "rounds"
		if value == 1 {
			unit = "round"
		}
		ui.Text(c, unit).FontSize(12).TextColor(p.TextSecondary).SingleLine().Shrink(0)
	})
	if focus.Valid() {
		return focus
	}
	return row
}
