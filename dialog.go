package main

import (
	"fmt"
	"strings"
	"time"

	"cove/themes"

	"github.com/egoist/mygo/ui"
)

const (
	confirmDelete         = "delete"
	confirmReset          = "reset"
	confirmDeleteProvider = "delprov"
)

func (a *App) dismissConfirm(keyboard bool) {
	a.endConfirm(keyboard)
}

// releaseConfirmRecord drops a confirm dialog without restoring the place
// that opened it. applyEdit uses this when it clears the dialog itself.
func (a *App) releaseConfirmRecord() {
	if a.confirmKind == "" {
		return
	}
	a.confirmKind = ""
	a.abandonFocusReturn()
}

// beginConfirm opens a confirm dialog. The caret returns to the composer
// on dismiss unless the user had already moved it to another field.
func (a *App) beginConfirm(kind string) {
	if a.confirmKind == "" {
		a.pushFocusReturn()
	}
	a.confirmKind = kind
}

func (a *App) endConfirm(keyboard bool) {
	a.confirmKind = ""
	a.popFocusReturnShown(keyboard)
}

func (a *App) acceptConfirm(keyboard bool) {
	kind := a.confirmKind
	id := a.deleteID
	fromSide := kind == confirmDelete && a.returnTo.kind == focusSidebar
	a.confirmKind = ""
	switch kind {
	case confirmDelete:
		a.deleteConversation(id)
		if fromSide && a.data.ActiveID != "" {
			// The row that opened the dialog is gone. Stay on the adjacent
			// sidebar row instead of restoring that element. The ring
			// follows the key or click that confirmed.
			a.stayOnSidebar(keyboard)
			return
		}
		// Other deletes land in the new chat's composer. A menu that
		// opened the dialog does not take the keyboard back.
		a.abandonFocusReturn()
		a.focusComposer()
		return
	case confirmReset:
		a.resetAllSettings()
	case confirmDeleteProvider:
		a.deleteCustomProvider(id, time.Now())
	}
	a.popFocusReturnShown(keyboard)
}

func (a *App) confirmCopy() (title, body, action string) {
	switch a.confirmKind {
	case confirmReset:
		return "Reset all settings?", "Providers, defaults and appearance go back to how Cove shipped. Chats are kept.", "Reset"
	case confirmDeleteProvider:
		name := "this provider"
		if c := a.connByID(a.deleteID); c != nil && strings.TrimSpace(c.Name) != "" {
			name = c.Name
		}
		body := "Its API key is removed from the Keychain."
		n := len(a.chatIDsUsing(a.deleteID))
		switch n {
		case 0:
		case 1:
			body = "1 chat uses models from this provider. It’ll switch to your default model. " + body
		default:
			body = fmt.Sprintf("%d chats use models from this provider. They’ll switch to your default model. ", n) + body
		}
		return fmt.Sprintf("Delete “%s”?", name), body, "Delete"
	default:
		n := 0
		title = "Delete this chat?"
		if conv := a.conversation(a.deleteID); conv != nil {
			title = fmt.Sprintf("Delete “%s”?", conv.Title)
			n = len(conv.Messages)
		}
		word := "messages"
		if n == 1 {
			word = "message"
		}
		return title, fmt.Sprintf("This chat and its %d %s will be deleted. This can’t be undone.", n, word), "Delete"
	}
}

// confirmDialog is the shared confirmation surface. The fill is the neutral
// overlay, the same in every theme, and it does not sample the gradient.
// Cancel is focused, so Return and Esc both dismiss. The scrim does not.
func (a *App) confirmDialog(c *ui.Context) {
	if a.confirmKind == "" {
		return
	}
	surface := themes.Palette(themes.ThemeWarm, a.look.Dark)
	title, body, action := a.confirmCopy()
	scrim := ui.Box(c).Absolute().Left(0).Right(0).Top(0).Bottom(0).Background(themes.DialogScrim(a.look.Dark)).Label("dialog scrim")
	scrim.Children(func() {
		ui.Column(c).Fill().Center().Children(func() {
			panel := ui.Column(c).Width(272).Padding(20, 16, 16, 16).Radius(12).Background(surface.LayerOverlay).Border(1, surface.LayerDialogBorder).AlignItems(ui.Center).Label("confirm dialog")
			panel.Shadow(0, 16, 48, -6, surface.ShadowLg).Shadow(0, 2, 8, 0, surface.ShadowMd)
			if !a.instant && !a.reduceMotion {
				panel.Transition(ui.ElementTransition{
					Enter:    &ui.Motion{Scale: 0.96, Opacity: 0},
					Duration: 160 * time.Millisecond,
					Ease:     easeEnter,
				})
			}
			panel.Children(func() {
				a.mark(c, 48)
				ui.Text(c, title).FontSize(13).FontWeight(600).TextAlign(ui.Center).Padding(12, 0, 0, 0)
				ui.Text(c, body).FontSize(12).TextColor(a.look.TextSecondary).TextAlign(ui.Center).Padding(4, 0, 0, 0)
				ui.Row(c).FillWidth().Gap(8).Padding(16, 0, 0, 0).Children(func() {
					cancel := a.dialogButton(c, "Cancel", false, surface)
					cancel.AutoFocus()
					if cancel.Clicked() {
						a.dismissConfirm(false)
					}
					act := a.dialogButton(c, action, true, surface)
					if act.Clicked() {
						a.acceptConfirm(false)
					}
				})
			})
		})
	})
}

func (a *App) dialogButton(c *ui.Context, label string, danger bool, surface themes.Colors) ui.Element {
	p := a.look
	b := ui.ButtonBase(c).Grow(1).Height(28).Radius(radiusRow).Center().Label(label).Cursor(ui.CursorPointer)
	fg := p.TextPrimary
	weight := 400
	if danger {
		b.Background(surface.ButtonDestructiveBg)
		fg = surface.ButtonDestructiveText
		weight = 500
	} else {
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
	// The ring follows whichever button has the keyboard. Cancel starts
	// focused; Tab to Delete moves the ring with it.
	a.focusedRing(b, radiusRow)
	return b
}
