// Cove is a native chat for OpenAI-compatible models. Replies stream in
// as Markdown, and chats are kept in the user's Application Support
// directory so they survive a restart.
package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"cove/themes"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// appVersion is the User-Agent and matches mygo.json.
const appVersion = "0.7.0"

// enableCheckForUpdates shows the About button that opens releasesURL.
// There is no releases page yet, so the button stays off the screen.
// The URL stays in the binary for when that page exists.
const enableCheckForUpdates = false

// releasesURL is the page Check for Updates opens in the default browser.
// Cove does not phone home. This is the source repository's release list.
const releasesURL = "https://github.com/xianhe-meng/cove/releases"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--dump-glyph-metrics" {
		dir := "glyph-metrics"
		if len(os.Args) > 2 {
			dir = os.Args[2]
		}
		if err := dumpGlyphMetrics(dir); err != nil {
			log.Fatal(err)
		}
		return
	}
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	path, err := storePath()
	if err != nil {
		log.Fatal(err)
	}
	app := NewApp(path)
	mygo.App.OnSecondInstance(func([]string, string) {
		if app.win != nil {
			app.win.Restore()
			app.win.Focus()
		}
	})
	mygo.App.OnWindowAllClosed(func() {
		if runtime.GOOS != "darwin" {
			mygo.App.Quit()
		}
	})
	mygo.App.OnActivate(func(hasVisible bool) {
		if !hasVisible {
			app.openWindow()
		}
	})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		app.persistForExit()
	})
	mygo.App.SetMenu(app.menu())
	mygo.App.WhenReady(func() {
		app.openWindow()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func storePath() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "conversations.json"), nil
}

func (a *App) openWindow() {
	if a.win != nil {
		a.win.Show()
		a.win.Focus()
		return
	}
	lights := mygo.Point{X: 20, Y: 20}
	if !a.data.RestoreLast {
		a.data.ActiveID = ""
	}
	a.win = mygo.NewWindow(mygo.WindowOptions{
		Title:  "Cove",
		Width:  1120,
		Height: 760,
		// The suggestion grid and the composer hold at 720×520. Shorter
		// than that, a single column of cards collides with the composer.
		MinWidth:       720,
		MinHeight:      520,
		StateKey:       "main",
		TitleBarStyle:  mygo.TitleBarHiddenInset,
		TitleBarHeight: titleBarHeight,
		// Opaque from the first frame. The view paints the neutral
		// gradient over this; there is no vibrancy material.
		BackgroundColor:      themes.WindowBackground(),
		TrafficLightPosition: &lights,
		Content:              ui.View(a.view),
	})
	a.win.OnClose(func(*mygo.CloseEvent) {
		a.persistForExit()
	})
	// windowDidBecomeKey: is NSWindowDidBecomeKeyNotification. MyGo
	// also sends SurfaceFocus from there when this surface is first
	// responder; trackWindowFocus restores the composer on that edge.
	// The Settings window is created separately and is not wired here.
	a.win.OnFocus(func() { a.pendingActivate = true })
}

func (a *App) openSettingsWindow() {
	a.ensurePref()
	if a.settingsSection == "" {
		a.settingsSection = "general"
	}
	if a.prefs != nil {
		a.prefs.SetTitle(settingsWindowTitle(a.settingsSection))
		a.prefs.Show()
		a.prefs.Focus()
		a.prefs.Invalidate()
		return
	}
	lights := mygo.Point{X: 20, Y: 20}
	w := mygo.NewWindow(mygo.WindowOptions{
		Title:                settingsWindowTitle(a.settingsSection),
		Width:                780,
		Height:               560,
		MinWidth:             720,
		MinHeight:            520,
		TitleBarStyle:        mygo.TitleBarHiddenInset,
		TitleBarHeight:       titleBarHeight,
		BackgroundColor:      themes.WindowBackground(),
		TrafficLightPosition: &lights,
		Content:              ui.View(a.settingsWindowRoot),
	})
	w.OnClosed(func() {
		a.dropSettingsWindow()
		a.flushProviderEdits(time.Now())
		a.settingsPage = settingsList
		if a.prefs == w {
			a.prefs = nil
		}
	})
	a.prefs = w
}

func settingsWindowTitle(section string) string {
	for _, s := range settingsSections {
		if s.id == section {
			return s.label
		}
	}
	return "Settings"
}

func (a *App) closePrefs() {
	if a.prefs != nil {
		a.prefs.Close()
	}
}

func (a *App) menu() *mygo.Menu {
	act := func(fn func()) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) {
			fn()
			a.refresh()
		}
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Label: "File", Submenu: []*mygo.MenuItem{
			{Label: "New Chat", Accelerator: "CmdOrCtrl+N", Click: act(a.newChat)},
			{Label: "Settings…", Accelerator: "CmdOrCtrl+,", Click: act(a.openDefaults)},
			{Label: "Chat Settings…", Accelerator: "Alt+CmdOrCtrl+,", Click: act(a.openSettings)},
			mygo.Separator(),
			{Role: mygo.RoleClose},
		}},
		{Label: "Edit", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleUndo},
			{Role: mygo.RoleRedo},
			mygo.Separator(),
			{Role: mygo.RoleCut},
			{Role: mygo.RoleCopy},
			{Role: mygo.RolePaste},
			{Role: mygo.RoleSelectAll},
			mygo.Separator(),
			{Label: "Add to Chat", Accelerator: "CmdOrCtrl+L", Click: act(a.addSelectionToComposer)},
			mygo.Separator(),
			{Label: "Find", Submenu: a.findMenuItems(act)},
		}},
		{Label: "Chat", Submenu: []*mygo.MenuItem{
			func() *mygo.MenuItem {
				it := &mygo.MenuItem{
					ID: "new-session", Label: "New Session", Accelerator: "Shift+CmdOrCtrl+N",
					Click: act(func() {
						if a.CanInsertBreak() {
							a.InsertSessionBreak()
							a.focusComposer()
						}
					}),
				}
				a.newSessionItem = it
				return it
			}(),
			mygo.Separator(),
			{Label: "Regenerate", Accelerator: "CmdOrCtrl+R", Click: act(a.regenerate)},
			{Label: "Stop", Accelerator: "CmdOrCtrl+.", Click: act(a.stop)},
			mygo.Separator(),
			{Label: "Last Request", Click: act(func() { a.openRequest() })},
		}},
		{Role: mygo.RoleWindowMenu},
	})
}
