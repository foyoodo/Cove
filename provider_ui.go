package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

func (a *App) providerSettings(c *ui.Context) {
	switch a.settingsPage {
	case settingsAdd:
		a.addPresetPage(c)
	case settingsCustom:
		a.customProviderPage(c)
	case settingsPending:
		a.providerDetail(c)
		a.draftActions(c)
	case settingsDetail:
		a.providerDetail(c)
	default:
		a.providerList(c)
	}
}

func (a *App) providerList(c *ui.Context) {
	p := a.look
	if a.providerNotice != "" {
		note := a.providerNotice
		ui.Row(c).FillWidth().Padding(10, 12).Gap(8).Radius(12).Background(p.AccentSoft).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconInfo).Size(14, 14).TextColor(p.TextSecondary).Shrink(0)
			ui.Text(c, note).FontSize(12).TextColor(p.TextPrimary).Grow(1).MinWidth(0)
			if ui.ButtonBase(c).Height(22).Padding(0, 8).Radius(radiusRow).Center().Label("OK").Cursor(ui.CursorPointer).Children(func() {
				ui.Text(c, "OK").FontSize(12).FontWeight(500).TextColor(p.TextLink)
			}).Clicked() {
				a.providerNotice = ""
			}
		})
	}
	if len(a.data.Connections) == 0 {
		a.providerEmpty(c)
		return
	}
	a.settingsGroup(c, "Providers", func() {
		for i := range a.data.Connections {
			conn := &a.data.Connections[i]
			keyless := !conn.Custom && keylessProvider(conn.Provider)
			kind, label := a.providerStatus(conn, keyless)
			a.providerRow(c, conn, kind, label, i == 0)
		}
		a.addProviderRow(c, false)
	})
	a.settingsFoot(c, "Status comes from the saved key. Cove doesn’t contact a provider until you test it or send a message.")
}

func (a *App) providerEmpty(c *ui.Context) {
	p := a.look
	ui.Column(c).FillWidth().Grow(1).Center().AlignItems(ui.Center).Gap(0).Padding(24, 20).Children(func() {
		ui.Icon(c, iconServer).Size(28, 28).TextColor(p.TextTertiary)
		ui.Text(c, "No providers yet").FontSize(17).FontWeight(600).Padding(10, 0, 0, 0)
		ui.Text(c, "Add a provider to start chatting.").FontSize(13).TextColor(p.TextSecondary).Padding(6, 0, 0, 0)
		btn := ui.ButtonBase(c).Height(28).Padding(0, 12).Radius(radiusRow).Center().Margin(16, 0, 0, 0).Label("Add Provider…").Cursor(ui.CursorPointer).Background(p.AccentDefault)
		btn.Children(func() {
			controlText(c, "Add Provider…", 13, 0, 500, p.TextOnAccent)
		})
		if btn.Clicked() {
			a.leaveProviderPage(c.Now())
			a.addForm = addDraft{}
			a.settingsPage = settingsAdd
		}
	})
}

func (a *App) providerRow(c *ui.Context, conn *Connection, kind, status string, first bool) {
	p := a.look
	name := connDisplay(conn)
	ui.Column(c).FillWidth().Children(func() {
		if !first {
			ui.Box(c).Height(1).FillWidth().Margin(0, 0, 0, 12).Background(p.SeparatorDefault)
		}
		row := ui.ButtonBase(c).FillWidth().MinHeight(36).Padding(4, 10, 4, 12).Gap(8).AlignItems(ui.Center).Label(name).Cursor(ui.CursorPointer)
		if row.Pressed() {
			row.Background(p.FillPressed)
		} else if row.Hovered() {
			row.Background(p.FillHover)
		}
		row.Children(func() {
			dot := a.connectionColor(conn)
			if kind == "denied" {
				dot = p.StatusWarning
			}
			ui.Box(c).Size(8, 8).Radius(4).Background(dot).Shrink(0)
			ui.Text(c, name).FontSize(13).SingleLine().Ellipsis("…").MinWidth(0)
			if conn.Custom {
				ui.Text(c, "Custom").FontSize(11).TextColor(p.TextSecondary).Padding(1, 5).Radius(4).Background(p.FillQuaternary).Shrink(0)
			}
			ui.Spacer(c).Grow(1)
			a.statusLabel(c, kind, status)
			ui.Icon(c, iconChevronRight).Size(12, 12).TextColor(p.TextTertiary).Shrink(0)
		})
		if row.Clicked() {
			a.openConnection(conn.ID, c.Now())
		}
	})
}

func (a *App) addProviderRow(c *ui.Context, first bool) {
	p := a.look
	ui.Column(c).FillWidth().Children(func() {
		if !first {
			ui.Box(c).Height(1).FillWidth().Margin(0, 0, 0, 12).Background(p.SeparatorDefault)
		}
		row := ui.ButtonBase(c).FillWidth().MinHeight(36).Padding(4, 10, 4, 12).Gap(8).AlignItems(ui.Center).Label("Add Provider…").Cursor(ui.CursorPointer)
		if row.Pressed() {
			row.Background(p.FillPressed)
		} else if row.Hovered() {
			row.Background(p.FillHover)
		}
		row.Children(func() {
			ui.Icon(c, iconPlus).Size(12, 12).TextColor(p.AccentDefault).Shrink(0)
			controlText(c, "Add Provider…", 13, 0, 400, p.AccentDefault).Grow(1)
		})
		if row.Clicked() {
			a.leaveProviderPage(c.Now())
			a.addForm = addDraft{}
			a.settingsPage = settingsAdd
		}
	})
}

func (a *App) statusLabel(c *ui.Context, kind, status string) {
	p := a.look
	fg := p.TextSecondary
	switch kind {
	case "none":
		fg = p.TextTertiary
	case "invalid":
		fg = p.StatusDanger
	case "denied":
		fg = p.StatusWarning
	}
	ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(0).Children(func() {
		switch kind {
		case "ok":
			ui.Icon(c, iconCheck).Size(12, 12).TextColor(p.StatusSuccess).Shrink(0)
		case "invalid":
			ui.Icon(c, iconAlert).Size(12, 12).TextColor(p.StatusDanger).Shrink(0)
		case "denied":
			ui.Icon(c, iconAlert).Size(12, 12).TextColor(p.StatusWarning).Shrink(0)
		}
		ui.Text(c, status).FontSize(12).TextColor(fg).SingleLine()
	})
}

func (a *App) providerDetail(c *ui.Context) {
	p := a.look
	ed := &a.provEdit
	a.settingsGroup(c, "Connection", func() {
		a.settingsLine(c, "Name", "Shown in the model menu", true, false, func(el *ui.Element) {
			in := a.flexField(c, &ed.name, "Provider name", "")
			in.Label("Provider name")
			if ed.nameError != "" {
				in.Border(1, p.StatusDanger)
			}
			commitLine(in, &ed.nameOn, c.Now(), a.saveProviderName)
			*el = in
		})
		if ed.nameError != "" {
			a.fieldError(c, ed.nameError)
		}
		if ed.custom {
			a.settingsLine(c, "API format", "How Cove talks to this server", false, false, func(el *ui.Element) {
				*el = a.formatPopup(c, &ed.format, func(id string) {
					a.saveProviderFormat(id, c.Now())
				})
			})
		}
		if !ed.keyless && !ed.custom {
			a.apiKeyLine(c, "API key", "Stored in the macOS Keychain", false)
		}
		hint := "Leave empty for the official endpoint"
		placeholder := officialBase(a.editProviderKind())
		if ed.custom {
			hint = "Must serve /chat/completions"
			if normalizeFormat(ed.format) == formatAnthropic {
				hint = "Must serve /messages"
			}
			placeholder = "https://api.example.com/v1"
		}
		a.settingsLine(c, "Base URL", hint, false, false, func(el *ui.Element) {
			in := a.flexField(c, &ed.baseURL, placeholder, "")
			if ed.baseError != "" {
				in.Border(1, p.StatusDanger)
			}
			if ed.draft && ed.custom {
				commitLine(in, &ed.baseFocused, c.Now(), func(time.Time) {
					ed.baseError = httpURLError(ed.baseURL)
				})
			} else {
				commitLine(in, &ed.baseFocused, c.Now(), a.saveProviderBase)
			}
			*el = in
		})
		if ed.baseError != "" {
			a.fieldError(c, ed.baseError)
		}
		if ed.custom {
			a.apiKeyLine(c, "API key", "Optional · Stored in the macOS Keychain", false)
		}
		a.testRow(c)
	})
	shown := 0
	for _, m := range ed.models {
		if m.Enabled {
			shown++
		}
	}
	ui.Column(c).Gap(6).FillWidth().Children(func() {
		ui.Row(c).FillWidth().MinHeight(22).Gap(8).AlignItems(ui.Center).Padding(0, 0, 0, 2).Children(func() {
			ui.Text(c, "Models").FontSize(13).FontWeight(600).Shrink(0)
			ui.Spacer(c).Grow(1)
			// No list yet: an untested custom draft has nothing to count.
			// A fetch that returns no models keeps the add-model row
			// instead of a "0 of 0 shown" counter.
			if len(ed.models) > 0 {
				ui.Text(c, fmt.Sprintf("%d of %d shown", shown, len(ed.models))).FontSize(12).TextColor(p.TextSecondary).Shrink(0).SingleLine()
			}
			label := "Refresh"
			if ed.refreshing {
				label = "Refreshing…"
			}
			btn := a.smallButton(c, label, iconRefresh)
			if ed.refreshing {
				btn.Disabled(true).Opacity(0.45)
			}
			if btn.Clicked() {
				a.refreshProviderModels(c.Now())
			}
		})
		if ed.refreshNote != "" {
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconInfo).Size(13, 13).TextColor(p.TextSecondary).Shrink(0)
				ui.Text(c, ed.refreshNote).FontSize(12).TextColor(p.TextSecondary).Grow(1).MinWidth(0)
			})
		}
		card := ui.Column(c).Radius(radiusGroup).Background(p.LayerGroup).Clip().FillWidth()
		a.fadeColors(card)
		card.Children(func() {
			for i := range ed.models {
				m := ed.models[i]
				a.modelToggleRow(c, m, i == 0)
			}
			if ed.adding {
				a.addModelEditor(c, len(ed.models) == 0)
			} else {
				a.addModelRow(c, len(ed.models) == 0)
			}
		})
	})
	foot := "Only switched-on models appear in the model menu. A later Refresh adds new models switched off."
	if ed.custom && ed.testKind == "ok" {
		foot = "Fetched models are on. Switch off any you don’t want in the model menu."
	}
	a.settingsFoot(c, foot)
	if !ed.draft && ed.connID != "" {
		a.settingsGroup(c, "", func() {
			a.settingsLine(c, "Delete provider", "Removes its key from the Keychain. Chats keep their messages.", true, false, func(*ui.Element) {
				b := ui.ButtonBase(c).Height(22).Padding(0, 2).Center().Label("Delete Provider…").Cursor(ui.CursorPointer)
				b.Children(func() {
					controlText(c, "Delete Provider…", 13, 0, 400, p.StatusDanger)
				})
				if b.Clicked() {
					a.beginConfirm(confirmDeleteProvider)
					a.deleteID = ed.connID
				}
			})
		})
	}
	if a.formError != "" {
		ui.Text(c, a.formError).FontSize(12).TextColor(p.StatusDanger)
	}
}

func (a *App) testRow(c *ui.Context) {
	p := a.look
	ed := &a.provEdit
	ui.Column(c).FillWidth().Children(func() {
		ui.Box(c).Height(1).FillWidth().Margin(0, 0, 0, 12).Background(p.SeparatorDefault)
		ui.Row(c).FillWidth().MinHeight(36).Padding(6, 10, 6, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			if ed.testLine != "" {
				fg := p.TextSecondary
				if ed.testKind == "fail" {
					fg = p.StatusDanger
				}
				ui.Row(c).Grow(1).MinWidth(0).Gap(4).AlignItems(ui.Center).Children(func() {
					switch ed.testKind {
					case "ok":
						ui.Icon(c, iconCheck).Size(12, 12).TextColor(p.StatusSuccess).Shrink(0)
					case "fail":
						ui.Icon(c, iconAlert).Size(12, 12).TextColor(p.StatusDanger).Shrink(0)
					default:
						ui.Text(c, "·").FontSize(12).TextColor(p.TextTertiary).Shrink(0)
					}
					ui.Text(c, ed.testLine).FontSize(12).TextColor(fg).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
				})
			} else {
				ui.Spacer(c).Grow(1)
			}
			disabled := ed.testing
			if ed.custom {
				disabled = disabled || httpURLError(ed.baseURL) != ""
			} else if !ed.keyless && strings.TrimSpace(ed.apiKey) == "" {
				disabled = true
			}
			label := "Test Connection"
			btn := a.smallButton(c, label, nil)
			if disabled {
				btn.Disabled(true).Opacity(0.45)
			}
			if btn.Clicked() && !disabled {
				a.runProviderTest(c.Now())
			}
		})
	})
}

func (a *App) modelToggleRow(c *ui.Context, m ProviderModel, first bool) {
	p := a.look
	id := m.ID
	on := m.Enabled
	ui.Column(c).FillWidth().Children(func() {
		if !first {
			ui.Box(c).Height(1).FillWidth().Margin(0, 0, 0, 12).Background(p.SeparatorDefault)
		}
		ui.Row(c).FillWidth().MinHeight(36).Padding(4, 10, 4, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			btn := ui.ButtonBase(c).Grow(1).MinWidth(0).Gap(6).Padding(0).Justify(ui.Start).AlignItems(ui.Center).Label(id).Cursor(ui.CursorPointer)
			btn.Children(func() {
				controlText(c, id, 13, 0, 400, p.TextPrimary).Ellipsis("…").MinWidth(0)
				if m.Fresh {
					controlText(c, "New", 11, 0, 500, p.AccentDefault).Padding(1, 5).Radius(4).Background(p.AccentSoft).Shrink(0)
				}
			})
			if btn.Clicked() {
				a.setModelEnabled(id, !on, c.Now())
			}
			sw := ui.Switch(c, &on).Label("Show "+id).Size(32, 18).Shrink(0)
			if sw.Changed() {
				a.setModelEnabled(id, on, c.Now())
			}
		})
	})
}

func (a *App) addModelRow(c *ui.Context, first bool) {
	p := a.look
	ui.Column(c).FillWidth().Children(func() {
		if !first {
			ui.Box(c).Height(1).FillWidth().Margin(0, 0, 0, 12).Background(p.SeparatorDefault)
		}
		row := ui.ButtonBase(c).FillWidth().MinHeight(36).Padding(4, 10, 4, 12).Gap(8).AlignItems(ui.Center).Justify(ui.Start).Label("Add model…").Cursor(ui.CursorPointer)
		if row.Hovered() {
			row.Background(p.FillHover)
		}
		row.Children(func() {
			ui.Icon(c, iconPlus).Size(12, 12).TextColor(p.AccentDefault).Shrink(0)
			controlText(c, "Add model…", 13, 0, 400, p.AccentDefault)
		})
		if row.Clicked() {
			a.provEdit.adding = true
			a.provEdit.newModel = ""
			a.formError = ""
		}
	})
}

func (a *App) addModelEditor(c *ui.Context, first bool) {
	p := a.look
	ui.Column(c).FillWidth().Children(func() {
		if !first {
			ui.Box(c).Height(1).FillWidth().Margin(0, 0, 0, 12).Background(p.SeparatorDefault)
		}
		ui.Row(c).FillWidth().MinHeight(36).Padding(6, 10, 6, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			in := a.flexField(c, &a.provEdit.newModel, "Model ID", "ui-monospace")
			in.Label("Model ID")
			commitLine(in, &a.provEdit.newFocused, c.Now(), func(now time.Time) {
				if strings.TrimSpace(a.provEdit.newModel) != "" {
					a.addEditorModel(now)
				}
			})
			if ui.ButtonBase(c).Height(22).Padding(0, 8).Radius(radiusRow).Center().Label("Cancel").Cursor(ui.CursorPointer).Children(func() {
				controlText(c, "Cancel", 13, 0, 400, p.TextPrimary)
			}).Clicked() {
				a.provEdit.adding = false
				a.provEdit.newModel = ""
			}
			add := a.smallButton(c, "Add", nil)
			if strings.TrimSpace(a.provEdit.newModel) == "" {
				add.Disabled(true).Opacity(0.45)
			}
			if add.Clicked() {
				a.addEditorModel(c.Now())
			}
		})
	})
}

func (a *App) addPresetPage(c *ui.Context) {
	a.settingsGroup(c, "Presets", func() {
		names := builtinProviderNames()
		for i, name := range names {
			a.presetRow(c, name, i == 0)
		}
		a.customPresetRow(c, len(names) == 0)
	})
	a.settingsFoot(c, "You can add the same service more than once — for example a work and a personal account.")
}

func (a *App) presetRow(c *ui.Context, name string, first bool) {
	p := a.look
	ui.Column(c).FillWidth().Children(func() {
		if !first {
			ui.Box(c).Height(1).FillWidth().Margin(0, 0, 0, 12).Background(p.SeparatorDefault)
		}
		row := ui.ButtonBase(c).FillWidth().MinHeight(36).Padding(4, 10, 4, 12).Gap(8).AlignItems(ui.Center).Label(name).Cursor(ui.CursorPointer)
		if row.Pressed() {
			row.Background(p.FillPressed)
		} else if row.Hovered() {
			row.Background(p.FillHover)
		}
		row.Children(func() {
			ui.Box(c).Size(8, 8).Radius(4).Background(p.providerColor(name, false)).Shrink(0)
			ui.Text(c, name).FontSize(13).SingleLine().Shrink(0)
			ui.Spacer(c).Grow(1)
			ui.Text(c, displayHost(officialBase(name))).FontSize(12).TextColor(p.TextSecondary).SingleLine().Ellipsis("…").MinWidth(0)
			ui.Icon(c, iconChevronRight).Size(12, 12).TextColor(p.TextTertiary).Shrink(0)
		})
		if row.Clicked() {
			a.beginPreset(name)
		}
	})
}

func (a *App) customPresetRow(c *ui.Context, first bool) {
	p := a.look
	ui.Column(c).FillWidth().Children(func() {
		if !first {
			ui.Box(c).Height(1).FillWidth().Margin(0, 0, 0, 12).Background(p.SeparatorDefault)
		}
		row := ui.ButtonBase(c).FillWidth().MinHeight(36).Padding(4, 10, 4, 12).Gap(8).AlignItems(ui.Center).Label("Custom…").Cursor(ui.CursorPointer)
		if row.Pressed() {
			row.Background(p.FillPressed)
		} else if row.Hovered() {
			row.Background(p.FillHover)
		}
		row.Children(func() {
			ui.Icon(c, iconPlus).Size(12, 12).TextColor(p.AccentDefault).Shrink(0)
			controlText(c, "Custom…", 13, 0, 400, p.AccentDefault).Grow(1)
			ui.Icon(c, iconChevronRight).Size(12, 12).TextColor(p.TextTertiary).Shrink(0)
		})
		if row.Clicked() {
			a.beginCustom()
		}
	})
}

func (a *App) draftActions(c *ui.Context) {
	ed := &a.provEdit
	ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Padding(8, 0, 0, 0).Children(func() {
		cancel := a.sheetButton(c, "Cancel", false)
		if cancel.Clicked() {
			a.leaveProviderPage(c.Now())
		}
		ok := a.sheetButton(c, "Add", true)
		ready := a.providerNameError(ed.name, "") == ""
		if ed.custom && httpURLError(ed.baseURL) != "" {
			ready = false
		}
		if !ready {
			ok.Disabled(true).Opacity(0.45)
		}
		if ok.Clicked() && ready {
			a.commitDraft(c.Now())
		}
	})
}

func (a *App) customProviderPage(c *ui.Context) {
	p := a.look
	f := &a.addForm
	a.settingsGroup(c, "OpenAI-compatible provider", func() {
		a.settingsLine(c, "Name", "Shown in the model menu", true, false, func(el *ui.Element) {
			in := a.flexField(c, &f.name, "e.g. Together AI", "")
			in.Label("Provider name")
			*el = in
		})
		a.settingsLine(c, "API format", "How Cove talks to this server", false, false, func(el *ui.Element) {
			*el = a.formatPopup(c, &f.format, func(id string) { f.format = id })
		})
		a.settingsLine(c, "Base URL", "Must serve /chat/completions", false, false, func(el *ui.Element) {
			in := a.flexField(c, &f.base, "https://api.example.com/v1", "")
			*el = in
		})
		a.settingsLine(c, "API key", "Optional · Stored in the macOS Keychain", false, false, func(el *ui.Element) {
			in := a.flexField(c, &f.key, "Optional for local servers", "ui-monospace")
			*el = in
		})
	})
	a.settingsFoot(c, "You can add as many providers as you like. Models are loaded from /models. If that fails, add a model id yourself.")
	ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
		cancel := a.sheetButton(c, "Cancel", false)
		if cancel.Clicked() {
			a.leaveProviderPage(c.Now())
		}
		ok := a.sheetButton(c, "Add Provider", true)
		ready := strings.TrimSpace(f.name) != "" && strings.TrimSpace(f.base) != ""
		if !ready {
			ok.Disabled(true).Opacity(0.45)
		}
		if ok.Clicked() && ready {
			a.confirmAddProvider(c.Now())
		}
	})
	if a.formError != "" {
		ui.Text(c, a.formError).FontSize(12).TextColor(p.StatusDanger)
	}
}

func (a *App) confirmAddProvider(now time.Time) {
	name := strings.TrimSpace(a.addForm.name)
	base := strings.TrimSpace(a.addForm.base)
	key := strings.TrimSpace(a.addForm.key)
	if name == "" || base == "" {
		return
	}
	if _, err := completionsURL(base); err != nil {
		a.formError = err.Error()
		return
	}
	id := newID()
	a.data.Connections = append(a.data.Connections, Connection{
		ID: id, Name: name, Provider: "Custom", Custom: true, BaseURL: base,
		Format: normalizeFormat(a.addForm.format),
	})
	if key != "" {
		if a.keys == nil {
			a.formError = "No credential store is available."
			a.data.Connections = a.data.Connections[:len(a.data.Connections)-1]
			return
		}
		if err := a.keys.Set(id, key); err != nil {
			a.formError = err.Error()
			a.data.Connections = a.data.Connections[:len(a.data.Connections)-1]
			return
		}
	}
	a.addForm = addDraft{}
	a.noteSaved(now)
	a.save()
	a.viewCustom(id)
	a.settingsPage = settingsDetail
}

// formatPopup is the API format menu. The closed control shows the current
// format; the two choices are not side-by-side buttons.
func (a *App) formatPopup(c *ui.Context, current *string, set func(string)) ui.Element {
	p := a.look
	if strings.TrimSpace(*current) == "" {
		*current = formatOpenAI
	}
	sel := ui.SelectBase(c, current)
	b := sel.Trigger
	b.Height(22).MinWidth(180).MaxWidth(260).Gap(6).Padding(0, 7, 0, 8).Radius(radiusRow).Background(p.ControlBg).Border(0.5, p.ControlBorder).AlignItems(ui.Center).Label("API format").Cursor(ui.CursorPointer)
	b.Children(func() {
		controlText(c, formatLabel(*current), 13, 0, 400, p.TextPrimary).Grow(1).MinWidth(0)
		ui.Icon(c, iconChevron).Size(11, 11).TextColor(p.TextSecondary).Shrink(0)
	})
	sel.Popup(func(panel ui.Element) {
		panel.Padding(5).Radius(radiusCode).Background(p.LayerOverlay).Border(0.5, p.LayerOverlayBorder).MinWidth(200)
		for _, id := range []string{formatOpenAI, formatAnthropic} {
			id := id
			item := sel.Item(id).Height(22).FillWidth().Padding(0, 8).Radius(4).AlignItems(ui.Center)
			fg := p.TextPrimary
			if item.Highlighted() {
				item.Background(p.AccentDefault)
				fg = p.TextOnAccent
			}
			item.Children(func() {
				controlText(c, formatLabel(id), 13, 0, 400, fg)
			})
		}
	})
	if b.Changed() {
		set(*current)
	}
	return b
}

func (a *App) settingsFoot(c *ui.Context, text string) {
	p := a.look
	ui.Row(c).FillWidth().Gap(6).Padding(0, 2).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconInfo).Size(13, 13).TextColor(p.TextSecondary).Shrink(0)
		ui.Text(c, text).FontSize(12).TextColor(p.TextSecondary).Grow(1).MinWidth(0)
	})
}

func (a *App) smallButton(c *ui.Context, label string, glyph *ui.SVG) ui.Element {
	p := a.look
	b := ui.ButtonBase(c).Height(22).Padding(0, 8).Gap(4).Radius(radiusRow).Center().Label(label).Cursor(ui.CursorPointer).Shrink(0)
	b.Background(p.ControlBg).Border(0.5, p.ControlBorder)
	if b.Pressed() {
		b.Background(p.FillPressed)
	} else if b.Hovered() {
		b.Background(p.FillHover)
	}
	b.Children(func() {
		if glyph != nil {
			ui.Icon(c, glyph).Size(12, 12).TextColor(p.TextPrimary).Shrink(0)
		}
		controlText(c, label, 13, 0, 400, p.TextPrimary)
	})
	return b
}

// apiKeyLine is the API key settings row. While the key is denied, the
// notice is part of the row, in the field's column: 6pt under the field,
// then the notice, then the same 7pt bottom padding as the other hint
// rows. The sentence stays inside the field's width and wraps, with Try
// Again on the next line when the two do not fit together.
func (a *App) apiKeyLine(c *ui.Context, label, hint string, first bool) {
	if !a.keyBlocked(a.provEdit.connID) {
		a.settingsLine(c, label, hint, first, false, func(el *ui.Element) {
			*el = a.providerKeyField(c)
		})
		return
	}
	p := a.look
	ui.Column(c).FillWidth().Children(func() {
		if !first {
			ui.Box(c).Height(1).FillWidth().Margin(0, 0, 0, 12).Background(p.SeparatorDefault)
		}
		// 7pt padding matches a hint row. The 34pt band is that row's
		// content box (48 − 14), so the field stays where the other
		// fields sit and 6pt remains under it.
		row := ui.Row(c).FillWidth().Padding(7, 10, 7, 12).Gap(12).AlignItems(ui.Start).Label("API key row")
		row.Children(func() {
			var field ui.Element
			lab := ui.ButtonBase(c).Grow(1).MinWidth(0).Height(34).Padding(0).Justify(ui.Start).AlignItems(ui.Center).Cursor(ui.CursorDefault)
			lab.Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
					ui.Text(c, label).FontSize(13).TextAlign(ui.Start).SingleLine().Ellipsis("…")
					ui.Text(c, hint).FontSize(12).TextColor(p.TextSecondary).TextAlign(ui.Start).SingleLine().Ellipsis("…")
				})
			})
			ui.Column(c).Width(260).MaxWidth(260).MinWidth(120).Shrink(0).Children(func() {
				ui.Row(c).Height(34).FillWidth().AlignItems(ui.Center).Children(func() {
					field = a.providerKeyField(c).FillWidth()
				})
				a.keyAccessNotice(c, a.provEdit.connID)
			})
			if lab.Clicked() && field.Valid() {
				field.Focus()
			}
		})
	})
}

// keyAccessNotice is the denied-key sentence under the API key field.
// Label/Small in status/warning. Try Again is accent/default, underlined
// only while hovered, and the only read.
func (a *App) keyAccessNotice(c *ui.Context, id string) {
	if id == "" || !a.keyBlocked(id) {
		return
	}
	p := a.look
	ui.Row(c).FillWidth().Wrap().GapX(8).GapY(0).AlignItems(ui.Center).Children(func() {
		ui.Text(c, keyAccessMessage()).FontSize(12).LineHeight(15.0 / 12.0).FontWeight(400).TextColor(p.StatusWarning).Shrink(1).MinWidth(0)
		btn := ui.ButtonBase(c).Height(15).Padding(0).Center().Label("Try Again").Cursor(ui.CursorPointer).Shrink(0)
		hover := btn.Hovered()
		btn.Children(func() {
			label := ui.Text(c, "Try Again").FontSize(12).LineHeight(15.0 / 12.0).FontWeight(400).TextColor(p.AccentDefault).NoWrap()
			if hover {
				label.Underline()
			}
		})
		if btn.Clicked() {
			a.retryKey(id)
		}
	})
}

// fieldError is the danger caption under a field, right-aligned.
func (a *App) fieldError(c *ui.Context, text string) {
	p := a.look
	ui.Row(c).FillWidth().Padding(0, 12, 4, 0).Children(func() {
		ui.Spacer(c).Grow(1)
		ui.Text(c, text).FontSize(12).TextColor(p.StatusDanger).SingleLine()
	})
}

// flexField is a settings text field that shrinks with the row, up to 260pt.
func (a *App) flexField(c *ui.Context, value *string, placeholder, font string) ui.Element {
	p := a.look
	in := ui.TextInputBase(c, value).Placeholder(placeholder).Height(22).Width(260).MaxWidth(260).MinWidth(120).FontSize(13).LineHeight(16.0/13.0).Padding(3, 7)
	if font != "" {
		in.Font(font).FontSize(12).LineHeight(16.0 / 12.0)
	}
	in.Radius(radiusRow).Background(p.ControlField).Border(1, p.ControlFieldBorder).FocusRing(false)
	paintPlaceholder(c, in, p.TextTertiary)
	return in
}

// maskProviderKey is prefix + 8 bullets + the last 4 characters.
func maskProviderKey(key string) string {
	key = strings.TrimSpace(key)
	r := []rune(key)
	if len(r) == 0 {
		return ""
	}
	if len(r) <= 8 {
		return strings.Repeat("•", len(r))
	}
	prefix := 12
	if len(r) < prefix+4 {
		prefix = len(r) - 4
	}
	if prefix < 1 {
		return strings.Repeat("•", 8) + string(r[len(r)-4:])
	}
	return string(r[:prefix]) + strings.Repeat("•", 8) + string(r[len(r)-4:])
}
