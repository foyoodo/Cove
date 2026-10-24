package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"cove/themes"

	"github.com/egoist/mygo/ui"
)

type settingsNav struct {
	id, label string
	icon      *ui.SVG
}

var settingsSections = []settingsNav{
	{"general", "General", iconGear},
	{"models", "Models & Providers", iconServer},
	{"appearance", "Appearance", iconPalette},
	{"shortcuts", "Shortcuts", iconKeyboard},
	{"advanced", "Advanced", iconWrench},
}

func (a *App) ensurePref() {
	if a.prefReady {
		return
	}
	a.pref = a.snapshotFrom(a.data.Defaults)
	a.timeoutText = strconv.Itoa(a.data.TimeoutSec)
	a.prefReady = true
}

func (a *App) commitPref(now time.Time) {
	saved := a.captureForm()
	show := a.showKey
	a.showKey = a.prefShowKey
	a.applySnapshot(a.pref)
	ok := a.saveForm(true)
	a.pref = a.captureForm()
	a.prefShowKey = a.showKey
	a.modelPick = modelChoiceID(a.pref.cfg.ConnectionID, a.pref.cfg.Model)
	if ok {
		a.appearanceSavedUntil = now.Add(motionCopied)
		a.formError = ""
	}
	a.applySnapshot(saved)
	a.showKey = show
}

func (a *App) setTheme(id string, now time.Time) {
	id = themes.NormalizeTheme(id)
	if a.data.Theme == id {
		return
	}
	a.data.Theme = id
	a.appearanceSavedUntil = now.Add(motionCopied)
	a.save()
	// The click that got here is inside a frame, so that frame cannot
	// ask for another one. Post one for both windows now. The palette
	// starts on the frame this delivers; it does not wait for the bitmap.
	a.invalidateWindows()
	a.logSwitchRequested()
}

func (a *App) resetAllSettings() {
	a.data.Defaults = DefaultConfig()
	a.data.Appearance = appearanceSystem
	a.data.Theme = themes.ThemeDefault
	a.data.SendWith = ""
	a.data.ShowReasoning = true
	a.data.RestoreLast = true
	a.data.KeepLastRequest = true
	a.data.TimeoutSec = 0
	a.data.AutoTitles = true
	a.data.TitleModel = nil
	a.pref = a.snapshotFrom(a.data.Defaults)
	a.timeoutText = "0"
	a.prefReady = true
	a.formError = ""
	a.appearanceSavedUntil = time.Now().Add(motionCopied)
	a.modelPick = ""
	a.save()
	a.invalidateWindows()
}

func (a *App) settingsWindowRoot(c *ui.Context) {
	a.inRender = true
	defer func() { a.inRender = false }()
	a.applyLook(c)
	look := a.look
	c.Root().Background(look.WindowBaseBottom).Draw(func(p *ui.Painter, r ui.Rect) {
		a.paintSettingsBackdrop(p, r)
	})
	a.settingsBody(c)
	a.confirmDialog(c)
	a.veilScrollbar(c)
	if a.prefs != nil {
		a.prefs.SetTitle(a.settingsHeading())
	}
}

func (a *App) settingsBody(c *ui.Context) {
	a.ensurePref()
	p := a.look
	ui.Row(c).Fill().Children(func() {
		ui.Column(c).Width(sidebarNarrow).FillHeight().Clip().Children(func() {
			ui.Row(c).Height(titleBarHeight).FillWidth()
			ui.Column(c).Padding(0, 10, 8, 10).Gap(8).Grow(1).Children(func() {
				a.settingsSearch(c)
				for _, s := range settingsSections {
					if !a.settingsSectionVisible(s) {
						continue
					}
					sec := s
					row := ui.ButtonBase(c).Height(rowHeight).FillWidth().Gap(8).Padding(0, 8).Radius(radiusRow).AlignItems(ui.Center).Label(sec.label).Cursor(ui.CursorPointer)
					fg := p.TextSecondary
					if a.settingsSection == sec.id {
						row.Background(p.SelectionRow)
						fg = p.TextPrimary
					} else if row.Hovered() {
						row.Background(p.FillHover)
					}
					a.fadeColors(row)
					row.Children(func() {
						ui.Icon(c, sec.icon).Size(16, 16).TextColor(fg)
						ui.Text(c, sec.label).FontSize(13).TextColor(fg).SingleLine().Grow(1).MinWidth(0)
					})
					if row.Clicked() {
						a.leaveProviderPage(c.Now())
						a.settingsSection = sec.id
					}
				}
			})
		})
		// The 52pt title bar sits on the window gradient. The card is the
		// sibling under it, flush to that bar, inset 8 on the right and
		// bottom. Grow in this column is height, so a tall window cannot
		// leave the card floating.
		ui.Column(c).Grow(1).MinWidth(0).FillHeight().Children(func() {
			a.settingsTitle(c)
			ui.Column(c).Grow(1).FillWidth().Padding(0, 8, 8, 0).Children(func() {
				card := ui.Column(c).Fill().Radius(radiusCard).Background(p.LayerContent).Border(1, p.LayerContentBorder).Clip().Label("settings card")
				contentCardShadow(card, p)
				a.fadeColors(card)
				card.Children(func() {
					ui.Scroll(c).Fill().TrackScroll(&a.formScroll).Children(func() {
						_, winH := c.Size()
						cardH := winH - titleBarHeight - 8
						emptyProviders := a.settingsSection == "models" && a.settingsPage == settingsList && len(a.data.Connections) == 0
						col := ui.Column(c).FillWidth().MaxWidth(520).AlignSelf(ui.Center).Padding(16, 20, 24, 20).Gap(18)
						if emptyProviders {
							col = ui.Column(c).FillWidth().MinHeight(cardH).Center().AlignItems(ui.Center).Padding(0, 20)
						}
						col.Children(func() {
							switch a.settingsSection {
							case "models":
								a.providerSettings(c)
							case "appearance":
								a.settingsAppearance(c)
							case "shortcuts":
								a.settingsShortcuts(c)
							case "advanced":
								a.settingsAdvanced(c)
							default:
								a.settingsGeneral(c)
							}
						})
					})
				})
			})
		})
	})
	a.trafficPlaceholders(c)
	a.acknowledgementsSheet(c)
}

func (a *App) settingsSectionVisible(s settingsNav) bool {
	q := strings.ToLower(strings.TrimSpace(a.settingsQuery))
	if q == "" {
		return true
	}
	blob := strings.ToLower(s.label + " " + settingsBlob(s.id))
	return strings.Contains(blob, q)
}

func settingsBlob(id string) string {
	switch id {
	case "general":
		return "model provider reasoning temperature max tokens max context rounds system prompt send show reasoning restore about version updates acknowledgements chat titles title model automatically generate"
	case "models":
		return "base url api key models providers openai anthropic xai ollama add provider test connection refresh"
	case "appearance":
		return "mode system light dark theme warm tide graphite mist sage lavender accent"
	case "shortcuts":
		return "keyboard shortcut new chat new session search delete send regenerate stop settings"
	default:
		return "extra body timeout last request history reset finder"
	}
}

func (a *App) settingsSearch(c *ui.Context) {
	p := a.look
	row := ui.Row(c).Height(rowHeight).FillWidth().Gap(8).Padding(0, 8).Radius(radiusRow).Background(p.FillQuaternary).AlignItems(ui.Center)
	row.Children(func() {
		ui.Icon(c, iconSearch).Size(14, 14).TextColor(p.TextSecondary)
		in := ui.TextInputBase(c, &a.settingsQuery).Placeholder("Search").Label("Search settings").Grow(1).MinWidth(0).FontSize(13)
		in.Border(0, ui.Transparent).Background(ui.Transparent).FocusRing(false)
		paintPlaceholder(c, in, p.TextSecondary)
	})
}

func (a *App) settingsTitle(c *ui.Context) {
	p := a.look
	ui.Row(c).Height(titleBarHeight).FillWidth().Padding(0, 16).Gap(6).AlignItems(ui.Center).DragWindow().Children(func() {
		ui.Row(c).Gap(0).Shrink(0).Children(func() {
			back := a.navButton(c, "Back", iconChevronLeft, a.settingsSection == "models" && a.settingsPage != settingsList)
			if back.Clicked() {
				a.leaveProviderPage(c.Now())
			}
			a.navButton(c, "Forward", iconChevronRight, false)
		})
		ui.Text(c, a.settingsHeading()).FontSize(13).FontWeight(600).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
		if !a.appearanceSavedUntil.IsZero() && c.Now().Before(a.appearanceSavedUntil) {
			ui.Text(c, "✓ Saved").FontSize(12).FontWeight(500).TextColor(p.StatusSuccess).Shrink(0)
			c.After(a.appearanceSavedUntil.Sub(c.Now()))
		}
	})
}

func (a *App) settingsHeading() string {
	if a.settingsSection == "models" {
		switch a.settingsPage {
		case settingsAdd:
			return "Add Provider"
		case settingsCustom:
			return "Custom Provider"
		case settingsPending:
			if a.provEdit.custom {
				return "Custom Provider"
			}
			if a.provEdit.name != "" {
				return a.provEdit.name
			}
			return "Add Provider"
		case settingsDetail:
			if a.provEdit.name != "" {
				return a.provEdit.name
			}
		}
	}
	return settingsWindowTitle(a.settingsSection)
}

// navButton is one of the title bar's ‹ › controls. Forward stays disabled.
// Back is enabled on a provider detail page or Add Provider.
func (a *App) navButton(c *ui.Context, label string, glyph *ui.SVG, enabled bool) ui.Element {
	p := a.look
	b := ui.ButtonBase(c).Size(28, 28).Radius(radiusRow).Center().Label(label)
	if !enabled {
		b.Disabled(true).Opacity(0.35)
	} else {
		b.Cursor(ui.CursorPointer)
		if b.Pressed() {
			b.Background(p.FillPressed)
		} else if b.Hovered() {
			b.Background(p.FillHover)
		}
	}
	b.Children(func() {
		ui.Icon(c, glyph).Size(16, 16).TextColor(p.TextSecondary)
	})
	return b
}

func (a *App) settingsGeneral(c *ui.Context) {
	a.settingsGroup(c, "New chats", func() {
		a.settingsLine(c, "Default model", "Enabled models of connected providers", true, false, func(el *ui.Element) {
			*el = a.prefModel(c)
		})
		a.settingsRow(c, "Reasoning", false, false, func(el *ui.Element) {
			*el = a.prefEffort(c)
		})
		a.settingsRow(c, "Temperature", false, false, func(el *ui.Element) {
			*el = a.prefTemperature(c)
		})
		a.settingsLine(c, "Max tokens", "0 omits the limit", false, false, func(el *ui.Element) {
			*el = a.prefStepper(c)
		})
		a.settingsLine(c, "Max context", "Rounds per session · a question and its answer = 1 round", false, false, func(el *ui.Element) {
			*el = a.maxContextControl(c, "global", a.data.MaxRounds, func(n int) {
				before := a.data.MaxRounds
				a.SetDefaultMaxContextRounds(n)
				if a.data.MaxRounds != before {
					a.appearanceSavedUntil = c.Now().Add(motionCopied)
				}
			})
		})
		a.settingsStack(c, "System prompt", "Sent with every new chat", false, func(el *ui.Element) {
			in := a.fieldBox(c, ui.TextArea(c, &a.pref.cfg.SystemPrompt).Placeholder("Optional instructions for every reply").Height(64).FillWidth())
			if in.Changed() {
				a.commitPref(c.Now())
			}
			*el = in
		})
	})
	a.settingsGroup(c, "Chats", func() {
		a.settingsLine(c, "Send with", "The other key inserts a newline", true, false, func(el *ui.Element) {
			*el = a.sendWithControl(c)
		})
		a.settingsLine(c, "Show reasoning while generating", "Collapse it when the reply starts", false, false, func(*ui.Element) {
			sw := ui.Switch(c, &a.data.ShowReasoning).Label("Show reasoning while generating")
			if sw.Changed() {
				a.appearanceSavedUntil = c.Now().Add(motionCopied)
				a.save()
			}
		})
		a.settingsRow(c, "Restore last chat on launch", false, false, func(*ui.Element) {
			sw := ui.Switch(c, &a.data.RestoreLast).Label("Restore last chat on launch")
			if sw.Changed() {
				a.appearanceSavedUntil = c.Now().Add(motionCopied)
				a.save()
			}
		})
	})
	a.settingsTitles(c)
	a.settingsAbout(c)
}

func (a *App) settingsTitles(c *ui.Context) {
	a.settingsGroup(c, "Chat titles", func() {
		a.settingsLine(c, titleToggleLabel, titleToggleHint, true, false, func(*ui.Element) {
			sw := ui.Switch(c, &a.data.AutoTitles).Label(titleToggleLabel).Description(titleToggleHint)
			if sw.Changed() {
				if !a.data.AutoTitles {
					a.cancelAutoTitles()
				}
				a.appearanceSavedUntil = c.Now().Add(motionCopied)
				a.save()
			}
		})
		hint := titleModelHint
		hintColor := ui.Color{}
		ref := a.data.TitleModel
		unavailable := ref != nil && !a.titleModelAvailable(ref)
		if unavailable {
			hint = titleUnavailableHint
			hintColor = a.look.StatusWarning
		}
		a.settingsLineColor(c, titleModelLabel, hint, hintColor, false, false, func(el *ui.Element) {
			*el = a.titleModelButton(c, unavailable)
		})
	})
}

func (a *App) titleModelButton(c *ui.Context, unavailable bool) ui.Element {
	p := a.look
	b := ui.ButtonBase(c.Key("settings-title-model"))
	b.Height(22).MinWidth(160).MaxWidth(300).Gap(6).Padding(0, 7, 0, 8).Radius(radiusRow).Background(p.ControlBg).Border(0.5, p.ControlBorder).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	ref := a.data.TitleModel
	switch {
	case unavailable && ref != nil:
		b.Label("Title model, " + ref.ModelID + ", unavailable. Titles use each chat’s model.")
	case ref == nil:
		b.Label("Title model, " + titleSameAsChat)
	default:
		b.Label("Title model, " + a.titleModelValue(ref))
	}
	b.Children(func() {
		if unavailable && ref != nil {
			ui.Icon(c, iconAlert).Size(12, 12).TextColor(p.StatusWarning).Shrink(0)
			controlText(c, ref.ModelID, 13, 0, 400, p.TextPrimary).Ellipsis("…").Grow(1).Shrink(1).MinWidth(0)
		} else if ref == nil {
			controlText(c, titleSameAsChat, 13, 0, 400, p.TextPrimary).SingleLine()
		} else {
			cfg := a.titleModelConfig(ref)
			ui.Box(c).Size(6, 6).Radius(3).Background(a.configColor(cfg)).Shrink(0)
			prov, model := a.generalModelParts(cfg)
			a.generalModelLabel(c, prov, model, p.TextPrimary)
		}
		ui.Icon(c, iconChevron).Size(11, 11).TextColor(p.TextSecondary).Shrink(0)
	})
	items, footer, search := a.titleModelEntries()
	now := c.Now()
	a.bindPopover(c, "settings-title-model", b, popSpec{
		Name:         "Title model",
		Items:        items,
		Footer:       footer,
		Search:       search,
		SearchPrompt: "Search models",
		OnSelect: func(id string) {
			if id == modelMenuSettings {
				a.openProviderSettings()
				return
			}
			switch id {
			case titleModelSame:
				a.data.TitleModel = nil
			default:
				if connID, model, ok := parseModelChoiceID(id); ok {
					a.data.TitleModel = &TitleModel{ProviderID: connID, ModelID: model}
				}
			}
			a.appearanceSavedUntil = now.Add(motionCopied)
			a.save()
		},
	})
	return b
}

func (a *App) titleModelConfig(ref *TitleModel) Config {
	cfg := Config{ConnectionID: ref.ProviderID, Model: ref.ModelID}
	if c := a.connByID(ref.ProviderID); c != nil {
		cfg.Provider = c.Provider
		if c.Custom {
			cfg.Provider = connDisplay(c)
		}
		cfg.BaseURL = connEffective(c)
	}
	return cfg
}

func (a *App) titleModelValue(ref *TitleModel) string {
	if ref == nil {
		return titleSameAsChat
	}
	prov, model := a.generalModelParts(a.titleModelConfig(ref))
	if prov != "" {
		return prov + " · " + model
	}
	return model
}

func (a *App) titleModelEntries() (items, footer []popEntry, search bool) {
	opts := a.menuModels()
	search = len(opts) >= menuSearchAt
	ref := a.data.TitleModel
	unavailable := ref != nil && !a.titleModelAvailable(ref)
	sameOn := ref == nil
	items = append(items, popEntry{
		Kind: popItem, ID: titleModelSame, Text: titleSameAsChat,
		Checkable: true, Checked: sameOn,
	})
	if unavailable {
		items = append(items, popEntry{
			Kind: popItem, ID: modelChoiceID(ref.ProviderID, ref.ModelID), Text: ref.ModelID,
			Checkable: true, Checked: true, Disabled: true, Secondary: titleUnavailable,
		})
	}
	items = append(items, popEntry{Kind: popSep})
	prev := ""
	for _, opt := range opts {
		if opt.connID != prev {
			items = append(items, popEntry{
				Kind: popHeader, Text: opt.provider, Dot: a.menuDot(a.connByID(opt.connID)),
			})
			prev = opt.connID
		}
		id := modelChoiceID(opt.connID, opt.model)
		on := ref != nil && ref.ProviderID == opt.connID && ref.ModelID == opt.model
		items = append(items, popEntry{
			Kind: popItem, ID: id, Text: opt.provider + " · " + opt.model, Group: opt.provider,
			Checkable: true, Checked: on,
			Body: func(c *ui.Context, fg ui.Color) {
				a.modelPickLabel(c, opt.provider, opt.model, fg)
			},
		})
	}
	footer = []popEntry{{
		Kind: popItem, ID: modelMenuSettings, Text: "Models & Providers…",
	}}
	return items, footer, search
}

func (a *App) settingsAbout(c *ui.Context) {
	p := a.look
	a.settingsGroup(c, "About", func() {
		ui.Row(c).FillWidth().MinHeight(72).Padding(12).Gap(12).AlignItems(ui.Center).Children(func() {
			a.appIcon(c, 48)
			ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, "Cove").FontSize(13).FontWeight(600).SingleLine()
				ui.Text(c, "Version "+appVersion+" · macOS 26").FontSize(12).TextColor(p.TextSecondary).SingleLine()
			})
			if enableCheckForUpdates && a.sheetButton(c, "Check for Updates…", false).Clicked() {
				c.OpenURL(releasesURL)
			}
		})
		a.settingsRow(c, "Acknowledgements", false, false, func(*ui.Element) {
			b := ui.ButtonBase(c).Height(22).Gap(2).Padding(0, 2).AlignItems(ui.Center).Label("View acknowledgements").Cursor(ui.CursorPointer)
			b.Children(func() {
				ui.Text(c, "View").FontSize(12).FontWeight(500).TextColor(p.TextLink)
				ui.Icon(c, iconChevronRight).Size(11, 11).TextColor(p.TextLink)
			})
			if b.Clicked() {
				a.acksOpen = true
			}
		})
	})
}

func (a *App) settingsAppearance(c *ui.Context) {
	p := a.look
	a.settingsGroup(c, "Appearance", func() {
		a.settingsRow(c, "Mode", true, false, func(el *ui.Element) {
			*el = a.appearanceControl(c)
		})
	})
	ui.Text(c, "Appearance changes apply immediately — no need to save.").FontSize(12).TextColor(p.TextSecondary)
	a.settingsGroup(c, "Theme", func() {
		_, winW := c.Size()
		cols := 3
		if winW-sidebarNarrow >= 600 {
			cols = 6
		}
		presets := themes.Presets()
		ui.Column(c).FillWidth().Padding(8, 10).Gap(8).Children(func() {
			for i := 0; i < len(presets); i += cols {
				ui.Row(c).FillWidth().Gap(8).Children(func() {
					for col := 0; col < cols; col++ {
						j := i + col
						if j >= len(presets) {
							ui.Box(c).Grow(1)
							continue
						}
						a.themeSwatch(c, presets[j])
					}
				})
			}
		})
	})
	ui.Row(c).Gap(8).AlignItems(ui.Center).FillWidth().Children(func() {
		ui.Box(c).Size(10, 10).Radius(5).Background(p.AccentDefault)
		ui.Text(c, "Controls and links · Change it in System Settings › Appearance").FontSize(12).TextColor(p.TextSecondary).Grow(1)
	})
}

func (a *App) themeSwatch(c *ui.Context, preset themes.Preset) {
	p := a.look
	pal := themes.Palette(preset.ID, p.Dark)
	selected := themes.NormalizeTheme(a.data.Theme) == preset.ID
	btn := ui.ButtonBase(c).Grow(1).MinWidth(0).Radius(8).Clip().Cursor(ui.CursorPointer).Label(preset.Name)
	if selected {
		btn.Border(2, p.AccentDefault)
	} else {
		btn.Border(1, p.SeparatorDefault)
	}
	btn.Children(func() {
		ui.Column(c).FillWidth().Children(func() {
			ui.Box(c).FillWidth().Height(36).Gradient(pal.WindowBaseTop, pal.WindowBaseBottom, 180)
			ui.Row(c).FillWidth().Height(18).Padding(0, 6).Gap(4).AlignItems(ui.Center).Background(pal.SelectionRow).Children(func() {
				ui.Box(c).Size(6, 6).Radius(3).Background(pal.ThemeMark)
				ui.Box(c).Height(4).Grow(1).Radius(2).Background(pal.TextTertiary)
			})
		})
		weight := 400
		if selected {
			weight = 600
		}
		ui.Text(c, preset.Name).FontSize(11).FontWeight(weight).Padding(4, 0).TextAlign(ui.Center)
	})
	if btn.Clicked() {
		a.setTheme(preset.ID, c.Now())
	}
}

func (a *App) settingsShortcuts(c *ui.Context) {
	send := []string{"↵"}
	newline := []string{"⇧", "↵"}
	if a.data.SendWith == "mod" {
		send = []string{"⌘", "↵"}
		newline = []string{"↵"}
	}
	type shortcutRow struct {
		label string
		keys  []string
	}
	groups := []struct {
		title string
		rows  []shortcutRow
	}{
		{"Chats", []shortcutRow{
			{newChatTitle, []string{"⌘", "N"}},
			{"New Session", []string{"⇧", "⌘", "N"}},
			{"Search Chats", []string{"⌘", "K"}},
			{"Find in Chat", []string{"⌘", "F"}},
			{"Find Next", []string{"⌘", "G"}},
			{"Find Previous", []string{"⇧", "⌘", "G"}},
			{"Delete Chat", []string{"⌘", "⌫"}},
		}},
		{"Messages", []shortcutRow{
			{"Send", send},
			{"New Line", newline},
			{"Add to Chat", []string{"⌘", "L"}},
			{"Regenerate", []string{"⌘", "R"}},
			{"Stop Generating", []string{"⌘", "."}},
			{"Edit Last Message", []string{"↑"}},
		}},
		{"App", []shortcutRow{
			{"Settings", []string{"⌘", ","}},
			{"Chat Settings", []string{"⌥", "⌘", ","}},
		}},
	}
	for _, g := range groups {
		rows := g.rows
		a.settingsGroup(c, g.title, func() {
			for i, row := range rows {
				keys := row.keys
				first := i == 0
				a.settingsRow(c, row.label, first, false, func(*ui.Element) {
					a.keyCaps(c, keys)
				})
			}
		})
	}
	ui.Text(c, "Shortcuts follow the menu bar and can’t be changed yet.").FontSize(12).TextColor(a.look.TextSecondary)
}

// keyCaps draws one 20pt rounded cap per key.
func (a *App) keyCaps(c *ui.Context, keys []string) {
	p := a.look
	ui.Row(c).Gap(3).AlignItems(ui.Center).Shrink(0).Label("keycaps " + strings.Join(keys, "")).Children(func() {
		for _, key := range keys {
			ui.Row(c).Height(20).MinWidth(20).Padding(0, 4).Radius(5).Background(p.ControlBg).Border(0.5, p.ControlBorder).Center().Label("keycap " + key).Children(func() {
				controlKey(c, key, 12, 500, p.TextPrimary)
			})
		}
	})
}

func (a *App) settingsAdvanced(c *ui.Context) {
	p := a.look
	a.settingsGroup(c, "Request", func() {
		a.settingsStack(c, "Extra body", "JSON merged onto every new chat’s request. Your keys win.", true, func(el *ui.Element) {
			in := a.fieldBox(c, ui.TextArea(c, &a.pref.cfg.ExtraBody).Placeholder(`{"reasoning_effort":"none"}`).Height(76).FillWidth().Font("ui-monospace").FontSize(12))
			if in.Changed() {
				a.commitPref(c.Now())
			}
			*el = in
		})
		a.settingsRow(c, "Timeout", false, false, func(el *ui.Element) {
			ui.Text(c, "seconds, 0 waits").FontSize(12).TextColor(p.TextSecondary)
			in := a.lineField(c, &a.timeoutText, "0", "ui-monospace")
			if in.Changed() {
				if n, err := strconv.Atoi(strings.TrimSpace(a.timeoutText)); err == nil && n >= 0 && n <= 3600 {
					a.data.TimeoutSec = n
					a.appearanceSavedUntil = c.Now().Add(motionCopied)
					a.save()
				}
			}
			*el = in
		})
		a.settingsRow(c, "Keep last request", false, false, func(*ui.Element) {
			sw := ui.Switch(c, &a.data.KeepLastRequest).Label("Keep last request")
			if sw.Changed() {
				a.appearanceSavedUntil = c.Now().Add(motionCopied)
				a.save()
			}
		})
	})
	a.settingsGroup(c, "Data", func() {
		n := len(a.data.Conversations)
		size := "not saved yet"
		if st, err := os.Stat(a.path); err == nil {
			size = byteSize(st.Size())
		}
		a.settingsRow(c, "Chat history", true, false, func(*ui.Element) {
			ui.Text(c, fmt.Sprintf("%d · %s", n, size)).FontSize(12).TextColor(p.TextSecondary).SingleLine()
		})
		a.settingsRow(c, "Show in Finder", false, false, func(*ui.Element) {
			if a.sheetButton(c, "Show in Finder", false).Clicked() && a.path != "" {
				c.OpenURL("file://" + a.path)
			}
		})
		a.settingsRow(c, "Reset all settings", false, false, func(*ui.Element) {
			if a.sheetButton(c, "Reset all settings…", false).Clicked() {
				a.beginConfirm(confirmReset)
			}
		})
	})
	if a.formError != "" {
		ui.Text(c, a.formError).FontSize(12).TextColor(p.StatusDanger)
	}
}

func byteSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}

func (a *App) prefProvider(c *ui.Context) ui.Element {
	p := a.look
	b := ui.ButtonBase(c.Key("settings-provider"))
	b.Height(22).MinWidth(168).Gap(6).Padding(0, 7, 0, 8).Radius(radiusRow).Background(p.ControlBg).Border(0.5, p.ControlBorder).AlignItems(ui.Center).Label("Provider").Cursor(ui.CursorPointer)
	b.Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).Background(a.configColor(a.pref.cfg))
		controlText(c, a.pref.cfg.Provider, 13, 0, 400, p.TextPrimary).Grow(1)
		ui.Icon(c, iconChevron).Size(11, 11).TextColor(p.TextSecondary)
	})
	var items []popEntry
	for _, name := range providerNames() {
		name := name
		dot := p.providerColor(name, name == "Custom")
		items = append(items, popEntry{
			Kind: popItem, ID: name, Text: name, Checkable: true, Checked: name == a.pref.cfg.Provider,
			Body: func(c *ui.Context, fg ui.Color) {
				ui.Box(c).Size(6, 6).Radius(3).Background(dot).Shrink(0)
				controlText(c, name, 13, 0, 400, fg).Grow(1).MinWidth(0)
			},
		})
	}
	now := c.Now()
	a.bindPopover(c, "settings-provider", b, popSpec{
		Name:  "Provider",
		Items: items,
		OnSelect: func(id string) {
			a.pref.cfg.Provider = id
			applyPreset(&a.pref.cfg)
			a.pref = a.snapshotFrom(a.pref.cfg)
			a.commitPref(now)
		},
	})
	return b
}

// modelChoiceID is the General popup's item identity. The connection id
// keeps two providers that share a display name and a model id apart.
// The drawn label stays "Provider · model".
func modelChoiceID(connID, model string) string {
	return connID + "\x1f" + model
}

func parseModelChoiceID(s string) (connID, model string, ok bool) {
	connID, model, ok = strings.Cut(s, "\x1f")
	if !ok || connID == "" || strings.TrimSpace(model) == "" {
		return "", "", false
	}
	return connID, strings.TrimSpace(model), true
}

// modelPickLabel is "Provider · model". The provider name and the dot are
// one run that cannot wrap or shrink, so a short measure cannot turn them
// into an ellipsis. The space after the dot starts the model run: a space
// at the end of the provider run is dropped when the line is drawn. Only
// the model id shrinks, and it ends with an ellipsis.
func (a *App) modelPickLabel(c *ui.Context, provider, model string, fg ui.Color) {
	ui.Row(c).Gap(0).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Center).Children(func() {
		controlText(c, provider+" ·", 13, 0, 400, fg).NoWrap().Shrink(0)
		controlText(c, " "+model, 13, 0, 400, fg).Ellipsis("…").Grow(1).Shrink(1).MinWidth(0)
	})
}

// modelChoice is one row of General's default-model popup. Unavailable is
// the current default after its provider's list dropped it. It stays
// selected until the user picks something else.
type modelChoice struct {
	connID, provider, model string
	unavailable             bool
}

func (a *App) defaultModelChoices() []modelChoice {
	var out []modelChoice
	seen := map[string]bool{}
	add := func(connID, provider, model string, unavailable bool) {
		model = strings.TrimSpace(model)
		if model == "" {
			return
		}
		key := modelChoiceID(connID, model)
		if connID == "" {
			key = "name\x1f" + provider + "\x1f" + model
		}
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, modelChoice{connID: connID, provider: provider, model: model, unavailable: unavailable})
	}
	for _, opt := range a.menuModels() {
		add(opt.connID, opt.provider, opt.model, false)
	}
	curID, curP, curM := a.pref.cfg.ConnectionID, a.pref.cfg.Provider, a.pref.cfg.Model
	curKey := modelChoiceID(curID, curM)
	if curID == "" {
		curKey = "name\x1f" + curP + "\x1f" + curM
	}
	if !seen[curKey] {
		add(curID, curP, curM, true)
	}
	return out
}

// generalModelParts is the closed Default model control. The provider
// prefix is shown only when this model name is enabled on two ready
// providers, the same rule as the composer chip. The model run is the
// stored id, and it is the run that ellipsizes.
func (a *App) generalModelParts(cfg Config) (provider, model string) {
	model = strings.TrimSpace(cfg.Model)
	if model == "" {
		return "", "No model"
	}
	if name := a.providerLabel(cfg); name != "" && a.modelNameShared(model) {
		return name, model
	}
	return "", model
}

func (a *App) generalModelLabel(c *ui.Context, provider, model string, fg ui.Color) {
	p := a.look
	ui.Row(c).Gap(0).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Center).Children(func() {
		if provider != "" {
			controlText(c, provider, 13, 0, 400, fg).NoWrap().Shrink(0)
			controlText(c, " ·", 13, 0, 400, p.TextTertiary).NoWrap().Shrink(0)
			controlText(c, " "+model, 13, 0, 400, fg).Ellipsis("…").Grow(1).Shrink(1).MinWidth(0)
			return
		}
		controlText(c, model, 13, 0, 400, fg).Ellipsis("…").Grow(1).Shrink(1).MinWidth(0)
	})
}

// prefModel is the General popup that replaces the separate provider menu
// and model field. The trigger uses the duplicate-name rule. Rows stay
// "Provider · model" so two copies of one name stay distinct in the list.
func (a *App) prefModel(c *ui.Context) ui.Element {
	p := a.look
	if a.modelPick == "" {
		a.modelPick = modelChoiceID(a.pref.cfg.ConnectionID, a.pref.cfg.Model)
	}
	b := ui.ButtonBase(c.Key("settings-default-model"))
	b.Height(22).MinWidth(180).MaxWidth(300).Gap(6).Padding(0, 7, 0, 8).Radius(radiusRow).Background(p.ControlBg).Border(0.5, p.ControlBorder).AlignItems(ui.Center).Label("Default model menu").Cursor(ui.CursorPointer)
	missing := !a.modelOnConnection(a.pref.cfg.ConnectionID, a.pref.cfg.Model)
	if a.pref.cfg.ConnectionID == "" {
		missing = !a.modelInLists(a.pref.cfg.Provider, a.pref.cfg.Model)
	}
	b.Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).Background(a.configColor(a.pref.cfg)).Shrink(0)
		prov, model := a.generalModelParts(a.pref.cfg)
		a.generalModelLabel(c, prov, model, p.TextPrimary)
		if missing {
			ui.Text(c, "Unavailable").FontSize(12).TextColor(p.TextTertiary).SingleLine().Shrink(0)
		}
		ui.Icon(c, iconChevron).Size(11, 11).TextColor(p.TextSecondary).Shrink(0)
	})
	opts := a.defaultModelChoices()
	var items []popEntry
	lastID := ""
	for _, opt := range opts {
		prov, model, connID := opt.provider, opt.model, opt.connID
		if connID != lastID {
			lastID = connID
			dot := a.menuDot(a.connByID(connID))
			items = append(items, popEntry{
				Kind: popHeader, Text: prov, Dot: dot,
			})
		}
		id := modelChoiceID(connID, model)
		unavailable := opt.unavailable
		items = append(items, popEntry{
			Kind: popItem, ID: id, Text: prov + " · " + model, Group: prov,
			Checkable: true, Checked: id == a.modelPick, Disabled: unavailable,
			Secondary: map[bool]string{true: "Unavailable"}[unavailable],
			Body: func(c *ui.Context, fg ui.Color) {
				a.modelPickLabel(c, prov, model, fg)
			},
		})
	}
	now := c.Now()
	a.bindPopover(c, "settings-model", b, popSpec{
		Name:  "Default model",
		Items: items,
		OnSelect: func(id string) {
			a.modelPick = id
			connID, model, ok := parseModelChoiceID(id)
			if ok {
				a.applyDefaultModel(connID, model, now)
			}
		},
	})
	return b
}

func (a *App) prefEffort(c *ui.Context) ui.Element {
	p := a.look
	b := ui.ButtonBase(c.Key("settings-reasoning"))
	b.Height(22).MinWidth(120).MaxWidth(300).Gap(6).Padding(0, 7, 0, 9).Radius(radiusRow).Background(p.ControlBg).Border(0.5, p.ControlBorder).AlignItems(ui.Center).Label("Reasoning").Cursor(ui.CursorPointer)
	b.Children(func() {
		controlText(c, a.pref.effort, 13, 0, 400, p.TextPrimary).Grow(1).MinWidth(0)
		ui.Icon(c, iconChevron).Size(11, 11).TextColor(p.TextSecondary).Shrink(0)
	})
	now := c.Now()
	a.bindPopover(c, "settings-effort", b, popSpec{
		Name:  "Reasoning",
		Items: effortMenuEntries(a.pref.effort),
		OnSelect: func(id string) {
			a.pref.effort = id
			a.commitPref(now)
		},
	})
	return b
}

func (a *App) sendWithControl(c *ui.Context) ui.Element {
	p := a.look
	idx := 0
	if a.data.SendWith == "mod" {
		idx = 1
	}
	labels := [2]string{"↵ Return", "⌘↵"}
	parts := ui.SegmentedBase(c, &idx, 2)
	track := parts.Track
	track.Width(176).Height(22).Padding(1).Gap(1).Radius(radiusRow).Background(p.FillQuaternary).Label("Send with control")
	track.Children(func() {
		for i, label := range labels {
			seg := parts.Segment(i).Grow(1).Height(20).Radius(5).Center()
			if i == 0 {
				seg.Label("send segment return")
			} else {
				seg.Label("send segment command")
			}
			if i == idx {
				seg.Background(p.ControlSegSelected)
			}
			seg.Children(func() {
				controlLine(c, label, 12, 0, 400, p.TextPrimary)
			})
		}
	})
	if track.Changed() {
		a.data.SendWith = ""
		if idx == 1 {
			a.data.SendWith = "mod"
		}
		a.appearanceSavedUntil = c.Now().Add(motionCopied)
		a.save()
	}
	return track
}

func (a *App) providerKeyField(c *ui.Context) ui.Element {
	p := a.look
	field := ui.Row(c).Height(22).Width(260).MaxWidth(260).MinWidth(120).Gap(4).Padding(0, 2, 0, 7).Radius(radiusRow).Background(p.ControlField).Border(1, p.ControlFieldBorder).AlignItems(ui.Center).Label("API key field")
	var focus ui.Element
	field.Children(func() {
		if a.provEdit.showKey {
			in := ui.TextInputBase(c, &a.provEdit.apiKey).Placeholder("sk-…").Label("API key").Grow(1).MinWidth(0).Font("ui-monospace").FontSize(12)
			in.Border(0, ui.Transparent).Background(ui.Transparent).FocusRing(false)
			commitLine(in, &a.provEdit.keyFocused, c.Now(), a.saveProviderKey)
			focus = in
		} else if masked := maskProviderKey(a.provEdit.apiKey); masked != "" {
			ui.Text(c, masked).Font("ui-monospace").FontSize(12).TextColor(p.TextSecondary).Grow(1).SingleLine()
		} else {
			ui.Text(c, "sk-…").Font("ui-monospace").FontSize(12).TextColor(p.TextTertiary).Grow(1)
		}
		label := "Show key"
		glyph := iconEye
		if a.provEdit.showKey {
			label = "Hide key"
			glyph = iconEyeOff
		}
		btn := ui.ButtonBase(c).Size(18, 18).Radius(4).Center().Label(label).Cursor(ui.CursorPointer)
		btn.Children(func() {
			ui.Icon(c, glyph).Size(13, 13).TextColor(p.TextSecondary)
		})
		if btn.Clicked() {
			if a.provEdit.showKey && a.provEdit.apiKey != a.provEdit.loadedKey {
				a.saveProviderKey(c.Now())
			}
			a.provEdit.showKey = !a.provEdit.showKey
			a.provEdit.keyFocused = false
		}
	})
	if focus.Valid() {
		return focus
	}
	return field
}

func (a *App) prefTemperature(c *ui.Context) ui.Element {
	p := a.look
	const inset = float32(8)
	s := ui.SliderBase(c, &a.pref.temp, 0, 2).Step(0.1).Width(148).Height(16).PaddingX(inset).FocusRing(false).Label("Temperature")
	ui.Textf(c, "%.1f", a.pref.temp).Font("ui-monospace").FontSize(12)
	sw := ui.Switch(c, &a.pref.tempOn).Label("Send temperature").Size(32, 18)
	if sw.Changed() {
		a.commitPref(c.Now())
	}
	if !a.pref.tempOn {
		s.Disabled(true)
	}
	if s.Changed() {
		a.commitPref(c.Now())
	}
	s.Draw(func(painter *ui.Painter, r ui.Rect) {
		innerW := r.W - 2*inset
		if innerW < 1 {
			return
		}
		frac := float32(a.pref.temp / 2)
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
		if !a.pref.tempOn {
			fill = p.TextTertiary
		}
		painter.Fill(track, p.SliderTrack, h/2)
		if frac > 0 {
			painter.Fill(ui.Rect{X: track.X, Y: track.Y, W: track.W * frac, H: h}, fill, h/2)
		}
		kw := float32(16)
		knob := ui.Rect{X: r.X + inset + frac*innerW - kw/2, Y: r.Y, W: kw, H: r.H}
		painter.Fill(knob, p.ControlKnob, kw/2)
		painter.Stroke(knob, p.ControlBorder, kw/2, 0.5)
	})
	return s
}

func (a *App) prefStepper(c *ui.Context) ui.Element {
	p := a.look
	shown := strconv.FormatFloat(a.pref.max, 'f', 0, 64)
	row := ui.Row(c).Gap(4).AlignItems(ui.Center)
	var focus ui.Element
	row.Children(func() {
		ui.Row(c).Width(80).Height(22).Padding(0, 7).Radius(radiusRow).Background(p.ControlField).Border(1, p.ControlFieldBorder).AlignItems(ui.Center).Children(func() {
			in := ui.TextInputBase(c, &shown).Font("ui-monospace").FontSize(12).Grow(1).MinWidth(0).Padding(3, 0).Label("Max tokens")
			in.Border(0, ui.Transparent).Background(ui.Transparent).FocusRing(false)
			focus = in
			if in.Changed() {
				if v, err := strconv.ParseFloat(strings.TrimSpace(shown), 64); err == nil {
					a.pref.max = clampMax(v)
					a.commitPref(c.Now())
				}
			}
		})
		ui.Column(c).Width(13).Height(22).Radius(5).Background(p.ControlBg).Border(0.5, p.ControlBorder).Clip().Children(func() {
			up := ui.ButtonBase(c).Height(11).FillWidth().Center().Label("Increase").Cursor(ui.CursorPointer)
			up.Children(func() { ui.Icon(c, iconChevron).Size(8, 8).Rotate(180).TextColor(p.TextSecondary) })
			if up.Clicked() {
				a.pref.max = clampMax(a.pref.max + 256)
				a.commitPref(c.Now())
			}
			down := ui.ButtonBase(c).Height(11).FillWidth().Center().Label("Decrease").Cursor(ui.CursorPointer)
			down.Children(func() { ui.Icon(c, iconChevron).Size(8, 8).TextColor(p.TextSecondary) })
			if down.Clicked() {
				a.pref.max = clampMax(a.pref.max - 256)
				a.commitPref(c.Now())
			}
		})
	})
	if focus.Valid() {
		return focus
	}
	return row
}

func clampMax(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 200000 {
		return 200000
	}
	return float64(int(v + 0.5))
}
