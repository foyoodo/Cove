package main

import (
	"github.com/egoist/mygo/ui"
)

// statesBoard is the design's message-states sheet: an even 2×2 of 528×322
// panels. Each panel is the window gradient inside a 1pt separator frame.
// It is only built for the headless screenshot.
func (a *App) statesBoard(c *ui.Context) {
	p := a.look
	ui.Column(c).Fill().Padding(26, 24, 24, 24).Gap(18).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(0, 0, 0, 4).Children(func() {
			ui.Text(c, "Message states").FontSize(17).FontWeight(600)
			ui.Text(c, "Waiting · Incomplete + Retry · Error + Retry · Hover actions · Menu").FontSize(12).TextColor(p.TextSecondary)
		})
		ui.Column(c).Gap(16).Children(func() {
			ui.Row(c).Gap(16).Children(func() {
				a.statePanel(c, "Waiting for the first token", "live · before any text", func() {
					ui.Row(c).Justify(ui.End).Children(func() {
						ui.Text(c, "Summarise the WAL chapter in five bullets.").FontSize(14).LineHeight(21.0/14.0).Padding(7, 12).Radius(radiusBubble).Background(p.BubbleUser).TextColor(p.BubbleUserText).MaxWidth(420)
					})
					a.authorLine(c, "09:58")
					a.streamStatus(c, "Waiting for deepseek-reasoner…", "1.8s")
				})
				a.statePanel(c, "Stopped early · incomplete", "last message only", func() {
					a.authorLine(c, "10:02")
					ui.Text(c, "A write-ahead log appends every change to a separate file before the main database is touched, so readers keep seeing the last committed state while").FontSize(14).LineHeight(21.0 / 14.0)
					ui.Row(c).Gap(8).Padding(6, 6, 6, 10).Radius(radiusCode).Background(p.StatusWarningSoft).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, iconAlert).Size(14, 14).TextColor(p.StatusWarning)
						ui.Text(c, "Reply stopped at 214 tokens — connection reset.").FontSize(12).Grow(1)
						a.sheetButton(c, "Retry", false)
					})
				})
			})
			ui.Row(c).Gap(16).Children(func() {
				a.statePanel(c, "Request failed", "error · last message", func() {
					a.authorLine(c, "10:05")
					ui.Row(c).Gap(10).Padding(10, 12).Radius(radiusCode).Background(p.StatusDangerSoft).AlignItems(ui.Start).Children(func() {
						ui.Icon(c, iconAlert).Size(16, 16).TextColor(p.StatusDanger)
						ui.Column(c).Gap(3).Grow(1).Children(func() {
							ui.Text(c, "401 Unauthorized").FontSize(13).FontWeight(600).TextColor(p.StatusDanger)
							ui.Text(c, "api.openai.com rejected the API key. Check the key in Settings, then retry.").FontSize(12).TextColor(p.TextSecondary)
							ui.Row(c).Gap(8).Padding(8, 0, 0, 0).Children(func() {
								a.sheetButton(c, "Retry", false)
								open := ui.ButtonBase(c).Height(22).Padding(0, 10).Radius(radiusRow).Center().Label("Open Settings…").Cursor(ui.CursorPointer)
								open.Background(p.FillHover)
								open.Children(func() {
									controlText(c, "Open Settings…", 13, 0, 400, p.TextPrimary)
								})
							})
						})
					})
				})
				a.statePanel(c, "Hover, tooltip, copied, menu", "actions fade in 120ms", func() {
					a.authorLine(c, "10:07")
					ui.Text(c, "Keep the cancel func next to the context: defer cancel() on the line after you create it.").FontSize(14).LineHeight(21.0 / 14.0)
					// Same rhythm as the transcript: 24pt actions, centered,
					// the panel's 12pt gap under the body. The tooltip is
					// absolute so it does not stretch the row.
					ui.Row(c).Gap(0).Height(iconSm).AlignItems(ui.Center).Children(func() {
						ui.Row(c).Height(iconSm).Gap(4).Padding(0, 4, 0, 2).AlignItems(ui.Center).Children(func() {
							ui.Icon(c, iconCheck).Size(13, 13).TextColor(p.StatusSuccess)
							controlText(c, "Copied", 12, 0, 500, p.StatusSuccess)
						})
						slot := ui.Box(c).Size(iconSm, iconSm)
						slot.Children(func() {
							regen := a.iconButtonSize(c, "state-regen", "Regenerate", iconRefresh, iconSm)
							regen.Background(p.FillHover)
							a.tip(c, regen, "Regenerate")
							ui.Row(c).Absolute().Bottom(iconSm+4).Left(-34).Gap(6).Padding(3, 7).Radius(4).Background(p.LayerOverlay).Border(0.5, p.LayerOverlayBorder).Shadow(0, 8, 24, 0, p.ShadowMd).AlignItems(ui.Center).Children(func() {
								controlText(c, "Regenerate", 11, 0, 400, p.TextPrimary)
								controlLine(c, "⌘R", 11, 0, 400, p.TextSecondary)
							})
						})
						more := a.iconButtonSize(c, "state-more", "More", iconEllipsis, iconSm)
						a.tip(c, more, "More")
					})
					// The model menu sits on top of the panel. It does not
					// replace the copied row.
					ui.Column(c).Absolute().Right(22).Top(62).MinWidth(220).Padding(5).Gap(0).Radius(radiusCode).Background(p.LayerOverlay).Border(0.5, p.LayerOverlayBorder).Shadow(0, 8, 24, 0, p.ShadowMd).Children(func() {
						a.stateMenuItem(c, "claude-sonnet-4.5", true, false)
						a.stateMenuItem(c, "claude-opus-4", false, true)
						a.stateMenuItem(c, "gpt-4o-mini", false, false)
					})
				})
			})
		})
	})
}

func (a *App) stateMenuItem(c *ui.Context, label string, checked, hover bool) {
	p := a.look
	row := ui.Row(c).Height(22).FillWidth().Gap(8).Padding(0, 8).Radius(4).AlignItems(ui.Center)
	fg := p.TextPrimary
	if hover {
		row.Background(p.AccentDefault)
		fg = p.TextOnAccent
	}
	row.Children(func() {
		slot := ui.Box(c).Size(12, 12)
		if checked {
			slot.Children(func() {
				ui.Icon(c, iconCheck).Size(12, 12).TextColor(fg)
			})
		}
		controlText(c, label, 13, 0, 400, fg)
	})
}

func (a *App) statePanel(c *ui.Context, title, note string, fn func()) {
	p := a.look
	panel := ui.Column(c).Width(528).Height(322).Padding(18, 22).Gap(12).Radius(radiusCard).
		Background(p.LayerContent).Border(1, p.LayerContentBorder)
	contentCardShadow(panel, p)
	panel.Children(func() {
		ui.Row(c).FillWidth().Children(func() {
			ui.Text(c, title).FontSize(13).FontWeight(600)
			ui.Spacer(c)
			ui.Text(c, note).FontSize(11).TextColor(p.TextTertiary)
		})
		ui.Box(c).Height(1).FillWidth().Background(p.SeparatorDefault)
		fn()
	})
}

func (a *App) authorLine(c *ui.Context, stamp string) {
	p := a.look
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		a.mark(c, 22)
		ui.Text(c, "Cove").FontSize(13).FontWeight(600)
		ui.Text(c, stamp).FontSize(11).TextColor(p.TextTertiary)
	})
}
