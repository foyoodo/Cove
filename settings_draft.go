package main

import (
	"strings"
	"time"
)

// settingsDraft is one settings tab's unsaved fields. Appearance is not
// part of a draft: it saves on its own.
type settingsDraft struct {
	loaded bool
	cfg    Config
	max    float64
	temp   float64
	tempOn bool
	effort string
	style  string
	// rounds is this chat's Max context while the sheet is open. 0 is unlimited.
	rounds int
}

func (a *App) ensureDrafts() {
	if !a.chatDraft.loaded {
		cfg := DefaultConfig()
		rounds := 0
		if conv := a.active(); conv != nil {
			cfg = conv.Config
			rounds = conv.MaxRounds
		}
		a.chatDraft = a.snapshotFrom(cfg)
		a.chatDraft.rounds = rounds
	}
	if !a.defaultsDraft.loaded {
		a.defaultsDraft = a.snapshotFrom(a.data.Defaults)
	}
}

func (a *App) dropDrafts() {
	a.chatDraft = settingsDraft{}
	a.defaultsDraft = settingsDraft{}
	a.editingTab = -1
	a.formError = ""
	a.showKey = false
	if a.roundsScope == "chat" {
		a.roundsScope = ""
		a.roundsText = ""
	}
}

func (a *App) snapshotFrom(cfg Config) settingsDraft {
	if !knownProvider(cfg.Provider) {
		cfg.Provider = "Custom"
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		cfg.APIKey = a.resolvedKey(cfg)
	}
	d := settingsDraft{
		loaded: true,
		cfg:    cfg,
		effort: effortLabel(cfg.Reasoning),
		style:  "None",
		temp:   0.7,
		tempOn: cfg.Temperature != nil,
		max:    float64(cfg.MaxTokens),
	}
	for _, name := range reasoningStyles {
		if name == cfg.ReasoningStyle {
			d.style = name
			break
		}
	}
	if cfg.Temperature != nil {
		d.temp = *cfg.Temperature
	}
	if d.temp < 0 {
		d.temp = 0
	}
	if d.temp > 2 {
		d.temp = 2
	}
	return d
}

func (a *App) captureForm() settingsDraft {
	return settingsDraft{
		loaded: true,
		cfg:    a.form,
		max:    a.formMax,
		temp:   a.formTemp,
		tempOn: a.formTempOn,
		effort: a.formEffort,
		style:  a.formStyle,
		rounds: a.formRounds,
	}
}

func (a *App) applySnapshot(d settingsDraft) {
	a.form = d.cfg
	a.formMax = d.max
	a.formTemp = d.temp
	a.formTempOn = d.tempOn
	a.formEffort = d.effort
	a.formStyle = d.style
	a.formRounds = d.rounds
	if a.formEffort == "" {
		a.formEffort = "Default"
	}
	if a.formStyle == "" {
		a.formStyle = "None"
	}
}

func (a *App) captureDraft(tab int) {
	snap := a.captureForm()
	if tab == 1 {
		a.defaultsDraft = snap
		return
	}
	a.chatDraft = snap
}

func (a *App) restoreDraft(tab int) {
	a.ensureDrafts()
	if tab == 1 {
		a.applySnapshot(a.defaultsDraft)
		return
	}
	a.applySnapshot(a.chatDraft)
}

func (a *App) draftDirty(tab int) bool {
	var snap settingsDraft
	switch {
	case tab == a.editingTab:
		snap = a.captureForm()
	case tab == 1:
		snap = a.defaultsDraft
	default:
		snap = a.chatDraft
	}
	if !snap.loaded {
		return false
	}
	saved := a.data.Defaults
	savedRounds := 0
	if tab == 0 {
		conv := a.active()
		if conv == nil {
			return false
		}
		saved = conv.Config
		savedRounds = conv.MaxRounds
	}
	if !sameSettings(snap, saved, a.resolvedKey(saved)) {
		return true
	}
	return tab == 0 && snap.rounds != savedRounds
}

func sameSettings(d settingsDraft, saved Config, key string) bool {
	if d.cfg.Provider != saved.Provider {
		return false
	}
	if strings.TrimSpace(d.cfg.Model) != strings.TrimSpace(saved.Model) {
		return false
	}
	if strings.TrimSpace(d.cfg.BaseURL) != strings.TrimSpace(saved.BaseURL) {
		return false
	}
	if strings.TrimSpace(d.cfg.APIKey) != strings.TrimSpace(key) {
		return false
	}
	if strings.TrimSpace(d.cfg.SystemPrompt) != strings.TrimSpace(saved.SystemPrompt) {
		return false
	}
	if strings.TrimSpace(d.cfg.ExtraBody) != strings.TrimSpace(saved.ExtraBody) {
		return false
	}
	if effortValue(d.effort) != normalizeEffort(saved.Reasoning) {
		return false
	}
	style, savedStyle := "", ""
	if d.cfg.Provider == "Custom" {
		if d.style != "None" {
			style = d.style
		}
		savedStyle = saved.ReasoningStyle
	}
	if style != savedStyle {
		return false
	}
	if d.tempOn != (saved.Temperature != nil) {
		return false
	}
	if d.tempOn && saved.Temperature != nil && *saved.Temperature != d.temp {
		return false
	}
	return int(d.max) == saved.MaxTokens
}

// saveBoth writes both tabs. A tab that was never opened is still loaded
// when the modal is, so both are saved. Appearance is already stored.
func (a *App) saveBoth() bool {
	if a.editingTab >= 0 {
		a.captureDraft(a.editingTab)
	}
	if a.chatDraft.loaded {
		a.applySnapshot(a.chatDraft)
		if conv := a.active(); conv != nil {
			if !a.saveForm(false) {
				a.settingsTab = 0
				a.editingTab = 0
				a.formDefault = false
				return false
			}
		}
	}
	if a.defaultsDraft.loaded {
		a.applySnapshot(a.defaultsDraft)
		if !a.saveForm(true) {
			a.settingsTab = 1
			a.editingTab = 1
			a.formDefault = true
			return false
		}
	}
	return true
}

func (a *App) setAppearance(mode string, now time.Time) {
	mode = normalizeAppearance(mode)
	changed := a.data.Appearance != mode
	a.data.Appearance = mode
	a.appearanceSavedUntil = now.Add(motionCopied)
	a.save()
	if changed {
		a.invalidateWindows()
		a.logSwitchRequested()
	}
}

func appearanceIndex(mode string) int {
	switch normalizeAppearance(mode) {
	case appearanceLight:
		return 1
	case appearanceDark:
		return 2
	default:
		return 0
	}
}

func appearanceFromIndex(i int) string {
	switch i {
	case 1:
		return appearanceLight
	case 2:
		return appearanceDark
	default:
		return appearanceSystem
	}
}

func effortIndex(label string) int {
	for i, name := range effortOptions {
		if name == label {
			return i
		}
	}
	return 1
}
