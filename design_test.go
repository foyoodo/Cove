package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"cove/themes"

	"github.com/egoist/mygo/ui"
)

func TestNoColorLiteralsOutsideThemes(t *testing.T) {
	hex := regexp.MustCompile(`#[0-9A-Fa-f]{3,8}`)
	var hits []string
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "themes", ".git", "build", "agent-tools", "docs", "third_party":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "//go:build ignore") {
			return nil
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, "ui.Hex(") || strings.Contains(line, "ui.RGBA(") || strings.Contains(line, "ui.RGB(") || hex.MatchString(line) {
				hits = append(hits, path+":"+itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) > 0 {
		t.Fatalf("color literals outside themes/:\n%s", strings.Join(hits, "\n"))
	}
}

func TestDialogScrimIsNeutralBlack(t *testing.T) {
	if got := themes.DialogScrim(false); got != ui.Hex("#0000000F") {
		t.Fatalf("light scrim %+v", got)
	}
	if got := themes.DialogScrim(true); got != ui.Hex("#0000001F") {
		t.Fatalf("dark scrim %+v", got)
	}
}

func TestVeilAlphaCurve(t *testing.T) {
	r := ui.Rect{Y: 100, H: 200}
	h := float32(composerMinH)
	cardBottom := r.Y + r.H
	compTop := cardBottom - float32(composerInsetMax) - h
	fadeTop := compTop - float32(composerFade)
	if a := veilAlphaAt(r, h, composerInsetMax, fadeTop); a != 0 {
		t.Fatalf("fade top %v", a)
	}
	if a := veilAlphaAt(r, h, composerInsetMax, compTop); a < 0.49 || a > 0.51 {
		t.Fatalf("composer top %v", a)
	}
	if a := veilAlphaAt(r, h, composerInsetMax, cardBottom-10); a < 0.49 || a > 0.51 {
		t.Fatalf("knee %v", a)
	}
	if a := veilAlphaAt(r, h, composerInsetMax, cardBottom); a < 0.69 || a > 0.71 {
		t.Fatalf("card bottom %v", a)
	}
}

func TestAccentFollowsDesktop(t *testing.T) {
	a := newTestApp(t)
	tt := openTester(a)
	tt.Frame()
	if a.look.AccentDefault != themes.AccentFallback() {
		t.Fatalf("missing accent should fall back to #007AFF, got %+v", a.look.AccentDefault)
	}
	probe := themes.ProbeAccent()
	tt.SetPreferences(ui.Preferences{Accent: probe})
	tt.Frame()
	if a.look.AccentDefault != probe {
		t.Fatalf("desktop accent was ignored: got %+v want %+v", a.look.AccentDefault, probe)
	}
	mark := themes.Palette(themes.ThemeWarm, false).SelectionRow
	if a.look.ButtonPrimaryBg != probe || a.look.SelectionRow != mark {
		t.Fatalf("accent should recolor the button and leave the theme selection: button %+v selection %+v", a.look.ButtonPrimaryBg, a.look.SelectionRow)
	}
	tt.SetDark(true)
	tt.Frame()
	if a.look.AccentDefault != probe {
		t.Fatalf("dark mode dropped the desktop accent: %+v", a.look.AccentDefault)
	}
	if a.look.ThemeMark != themes.Palette(themes.ThemeWarm, true).ThemeMark {
		t.Fatalf("theme mark followed the accent: %+v", a.look.ThemeMark)
	}
}

// A missing desktop accent follows the system blue for the appearance.
// Light stays the light blue. Dark is the dark system blue, not the light
// one carried over. A color the desktop did report is kept in both modes.
func TestAccentFallbackFollowsAppearance(t *testing.T) {
	darkBlue, ok := themes.NamedAccent("blue", true)
	if !ok {
		t.Fatal("no dark blue accent")
	}
	a := newTestApp(t)
	tt := openTester(a)
	a.data.Appearance = appearanceDark
	tt.SetDark(true)
	tt.Frame()
	if a.look.AccentDefault != darkBlue {
		t.Fatalf("dark fallback accent %+v, want dark system blue %+v", a.look.AccentDefault, darkBlue)
	}
	if a.look.AccentDefault == themes.AccentFallback() {
		t.Fatal("dark mode kept the light system blue")
	}
	a.data.Appearance = appearanceLight
	tt.SetDark(false)
	tt.Frame()
	if a.look.AccentDefault != themes.AccentFallback() {
		t.Fatalf("light fallback accent %+v", a.look.AccentDefault)
	}
}

func TestAppearanceModePersistsAndOverridesSystem(t *testing.T) {
	a := newTestApp(t)
	tt := openTester(a)
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Appearance"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Appearance changes apply immediately — no need to save.") {
		t.Fatalf("appearance note missing: %q", tt.Texts())
	}
	if err := tt.Click("Dark"); err != nil {
		t.Fatal(err)
	}
	if a.data.Appearance != appearanceDark || !a.dark {
		t.Fatalf("dark mode appearance %q dark %v", a.data.Appearance, a.dark)
	}
	got, err := Load(a.path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Appearance != appearanceDark {
		t.Fatalf("appearance not saved: %q", got.Appearance)
	}
	tt.SetDark(false)
	if !a.dark {
		t.Fatal("system light overrode an explicit dark appearance")
	}
	if err := tt.Click("System"); err != nil {
		t.Fatal(err)
	}
	if a.data.Appearance != appearanceSystem || a.dark {
		t.Fatalf("system mode appearance %q dark %v", a.data.Appearance, a.dark)
	}
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	if err := tt.Click("Light"); err != nil {
		t.Fatal(err)
	}
	tt.SetDark(true)
	if a.dark || a.data.Appearance != appearanceLight {
		t.Fatalf("reduce-motion light mode appearance %q dark %v", a.data.Appearance, a.dark)
	}
}

func TestSettingsTabsKeepDrafts(t *testing.T) {
	a := newTestApp(t)
	connectBuiltin(a, "Anthropic", "sk-anthropic")
	a.newChat()
	a.active().Config.Model = "chat-model"
	a.data.Defaults.Model = "default-model"
	tt := openTester(a)
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Chat Settings…"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Model"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("draft-chat")
	if !a.draftDirty(0) {
		t.Fatal("this chat tab did not go dirty")
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if a.active().Config.Model != "chat-model" || a.data.Defaults.Model != "default-model" {
		t.Fatalf("cancel wrote chat %q defaults %q", a.active().Config.Model, a.data.Defaults.Model)
	}
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("default-model"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(" claude-sonnet-4.5"); err != nil {
		t.Fatal(err)
	}
	if a.data.Defaults.Model != "claude-sonnet-4.5" || a.data.Defaults.Provider != "Anthropic" {
		t.Fatalf("settings did not apply: %+v", a.data.Defaults)
	}
	if a.active().Config.Model != "chat-model" {
		t.Fatal("app settings rewrote the open chat")
	}
}
