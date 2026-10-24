package main

import (
	"image/color"
	"strings"
	"testing"
	"time"

	"cove/themes"

	"github.com/egoist/mygo/ui"
)

func TestSettingsMenuCloseReleasesFocus(t *testing.T) {
	a, tt := focusChats(t)
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Default model menu"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	st := a.popovers["settings-model"]
	if st == nil || !st.open || a.returnDepth == 0 {
		t.Fatalf("menu open=%v depth=%d", st != nil && st.open, a.returnDepth)
	}
	a.settingsOpen = false
	tt.Frame()
	if st.open {
		t.Fatal("settings menu stayed open after the window closed")
	}
	if a.returnDepth != 0 {
		t.Fatalf("focus counter stuck at %d", a.returnDepth)
	}
	for i := 0; i < 4 && a.settled.kind != focusSidebar; i++ {
		tt.Key(ui.Shift, ui.KeyTab)
	}
	requireSidebar(t, a, "Focus")
	tt.Key(ui.Cmd, ui.KeyBackspace)
	tt.Frame()
	if a.confirmKind != confirmDelete {
		t.Fatal("delete dialog did not open")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.confirmKind != "" || a.returnDepth != 0 {
		t.Fatalf("dialog %q depth %d", a.confirmKind, a.returnDepth)
	}
	requireSidebar(t, a, "Focus")
}

func TestTabWhileMenuStaysOpen(t *testing.T) {
	a, tt := focusChats(t)
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.popupOpen() || a.returnDepth == 0 {
		t.Fatalf("more open=%v depth=%d", a.popupOpen(), a.returnDepth)
	}
	depth := a.returnDepth
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if !a.popupOpen() || a.returnDepth != depth {
		t.Fatalf("tab left the menu open=%v depth=%d", a.popupOpen(), a.returnDepth)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.popupOpen() || a.returnDepth != 0 {
		t.Fatalf("esc open=%v depth=%d", a.popupOpen(), a.returnDepth)
	}
	requireComposer(t, a, tt)
}

func TestSelectionFillStaysPale(t *testing.T) {
	a, tt := focusChats(t)
	tt.Key(ui.Shift, ui.KeyTab)
	requireSidebar(t, a, "Focus")
	row := sidebarTitleRect(t, tt, "Focus")
	center := sampleAt(tt, row.X+row.W/2, row.Y+row.H/2)
	accent := color.RGBA{R: a.look.AccentDefault.R, G: a.look.AccentDefault.G, B: a.look.AccentDefault.B, A: 255}
	if rgbaDelta(center, accent) < 40 {
		t.Fatalf("sidebar row is solid accent %v", center)
	}
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("General"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	nav := sidebarTitleRect(t, tt, "General")
	navCenter := sampleAt(tt, nav.X+nav.W/2, nav.Y+nav.H/2)
	accent = color.RGBA{R: a.look.AccentDefault.R, G: a.look.AccentDefault.G, B: a.look.AccentDefault.B, A: 255}
	if rgbaDelta(navCenter, accent) < 40 {
		t.Fatalf("settings nav row is solid accent %v", navCenter)
	}
}

func sidebarTitleRect(t *testing.T, tt *ui.Tester, title string) ui.Rect {
	t.Helper()
	var best ui.Rect
	ok := false
	for _, r := range tt.FindAll(title) {
		if r.X > 280 || r.H < 16 || r.H > 40 {
			continue
		}
		if !ok || r.Y < best.Y {
			best, ok = r, true
		}
	}
	if !ok {
		t.Fatalf("no sidebar rect for %q", title)
	}
	return best
}

func TestSettingsFocusDoesNotLeak(t *testing.T) {
	a, tt := focusChats(t)
	requireComposer(t, a, tt)
	main := a.ensureLedger(a.windowToken)
	if main == nil {
		t.Fatal("no main ledger")
	}
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !focusUntil(t, tt, "Search settings", 4) {
		t.Fatal("settings search")
	}
	if a.windowToken == 0 || a.ledgers[a.windowToken] == main && a.settingsToken == a.windowToken {
		t.Fatal("settings wrote the main window record")
	}
	a.settingsOpen = false
	tt.Frame()
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if tt.Focused("More") {
		t.Fatal("esc kept More; restore target was a settings element")
	}
	requireComposer(t, a, tt)
}

func TestFocusLedgerPerWindow(t *testing.T) {
	a := newTestApp(t)
	a.settled = focusPlace{kind: focusSettingsNav, id: "general", elem: 42}
	a.returnDepth = 1
	a.windowToken = 1
	a.cur = a.ensureLedger(1)
	a.storeLedger()
	a.clearLiveLedger()
	a.settled = focusPlace{kind: focusComposer, id: "c", elem: 7}
	a.windowToken = 2
	a.cur = a.ensureLedger(2)
	a.storeLedger()
	a.cur = a.ledgers[1]
	a.windowToken = 1
	a.loadLedger()
	if a.settled.elem != 42 || a.settled.kind != focusSettingsNav || a.returnDepth != 1 {
		t.Fatalf("settings record %+v depth %d", a.settled, a.returnDepth)
	}
	a.cur = a.ledgers[2]
	a.loadLedger()
	if a.settled.elem != 7 || a.settled.kind != focusComposer || a.returnDepth != 0 {
		t.Fatalf("main record %+v depth %d", a.settled, a.returnDepth)
	}
}

func TestTemperatureSliderPrecedesSwitch(t *testing.T) {
	a, tt := focusChats(t)
	temp := 1.2
	a.data.Defaults.Temperature = &temp
	a.prefReady = false
	tt.Frame()
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	enableFullKeyboard(tt)
	if !focusUntil(t, tt, "General", 6) {
		t.Fatal("settings nav")
	}
	sawSlider := false
	for i := 0; i < 12; i++ {
		tt.Key(0, ui.KeyTab)
		if tt.Focused("Temperature") {
			sawSlider = true
		}
		if tt.Focused("Send temperature") {
			if !sawSlider {
				t.Fatal("Send temperature came before the Temperature slider")
			}
			return
		}
	}
	t.Fatal("did not reach Send temperature")
}

func TestTabSkipsDisabledControls(t *testing.T) {
	a, tt := focusChats(t)
	a.active().Messages = nil
	a.active().Draft = ""
	tt.Frame()
	enableFullKeyboard(tt)
	focusDraft(t, tt)
	for i := 0; i < 8; i++ {
		tt.Key(0, ui.KeyTab)
		if tt.Focused("Send") || tt.Focused("Regenerate") {
			t.Fatal("tab landed on a disabled control")
		}
		if tt.Focused("More") {
			return
		}
	}
	t.Fatal("did not reach More")
}

func TestPopupOpenIsThisWindow(t *testing.T) {
	a := newTestApp(t)
	a.windowToken = 1
	a.popovers = map[string]*popoverState{
		"title-more":     {open: true, owner: 2},
		"settings-model": {open: true, owner: 1},
	}
	if !a.popupOpen() {
		t.Fatal("this window's menu was ignored")
	}
	a.popovers["settings-model"].open = false
	if a.popupOpen() {
		t.Fatal("another window's menu swallowed Tab")
	}
}

func TestDropSettingsWindowDeletesLedger(t *testing.T) {
	a := newTestApp(t)
	const token uintptr = 0x51
	a.settingsToken = token
	a.windowToken = token
	a.cur = a.ensureLedger(token)
	a.returnDepth = 3
	a.storeLedger()
	a.dropSettingsWindow()
	if _, ok := a.ledgers[token]; ok {
		t.Fatal("settings ledger was kept")
	}
	if a.settingsToken != 0 || a.windowToken != 0 || a.cur != nil {
		t.Fatal("drop left the settings window bound")
	}
}

func TestSettingsTabSkipsButtons(t *testing.T) {
	_, tt := focusChats(t)
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !focusUntil(t, tt, "Search settings", 3) {
		t.Fatal("search")
	}
	tt.Key(0, ui.KeyTab)
	if !tt.Focused("General") && !tt.Focused("Settings") {
		t.Fatal("tab did not reach the settings list")
	}
	tt.Key(0, ui.KeyTab)
	if tt.Focused("Default model menu") || tt.Focused("Back") || tt.Focused("Show reasoning while generating") || tt.Focused("Increase") {
		t.Fatal("Full Keyboard Access off visited a button")
	}
	if !tt.Focused("Max tokens") {
		t.Fatal("tab did not reach the number field")
	}
}

func TestCrossWindowMenuDoesNotStickLedger(t *testing.T) {
	a, tt := focusChats(t)
	requireComposer(t, a, tt)
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	main := a.windowToken
	mainLedger := a.ledgers[main]
	more := a.popovers["title-more"]
	if mainLedger == nil || more == nil || !more.open || !more.pushed || mainLedger.depth == 0 {
		t.Fatalf("more open=%v pushed=%v depth=%d", more != nil && more.open, more != nil && more.pushed, depthOf(mainLedger))
	}
	depth := mainLedger.depth
	// The Settings window is already open. A click in it must not release
	// the main window's menu against the settings ledger. Headless draws
	// that window in this view; opening it by clicking Settings would hit
	// the menu that is up.
	a.settingsOpen = true
	tt.Frame()
	if err := tt.Click("Default model menu"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !more.open || !more.pushed {
		t.Fatal("settings click closed the other window's menu")
	}
	if mainLedger.depth != depth {
		t.Fatalf("settings click moved the main counter from %d to %d", depth, mainLedger.depth)
	}
	a.settingsOpen = false
	tt.Frame()
	// The other window becoming key closes this window's menu and pops
	// its own counter. Headless settings share the surface, so resign
	// the window the way a click in that other window does.
	tt.SetFocused(false)
	if depthOf(a.ledgers[main]) != 0 {
		t.Fatalf("resign left the main counter at %d", depthOf(a.ledgers[main]))
	}
	if more.open {
		t.Fatal("resign left More open")
	}
	tt.SetFocused(true)
	for i := 0; i < 4 && a.settled.kind != focusSidebar; i++ {
		tt.Key(ui.Shift, ui.KeyTab)
	}
	requireSidebar(t, a, "Focus")
	tt.Key(ui.Cmd, ui.KeyBackspace)
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.returnDepth != 0 {
		t.Fatalf("delete-cancel left the counter at %d", a.returnDepth)
	}
	requireSidebar(t, a, "Focus")
}

func depthOf(lg *focusLedger) int {
	if lg == nil {
		return 0
	}
	return lg.depth
}

func TestPointerPressHidesRing(t *testing.T) {
	a, tt := focusChats(t)
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	enableFullKeyboard(tt)
	if !focusUntil(t, tt, "General", 8) {
		t.Fatal("settings nav is not a tab stop")
	}
	if !a.settingsNavKey {
		t.Fatal("tab did not show the settings nav ring")
	}
	row := navRowRect(t, tt, "General")
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	if a.settingsNavKey {
		t.Fatal("click left the ring on the settings row")
	}

	a.settingsOpen = false
	tt.Frame()
	enableFullKeyboard(tt)
	if !focusUntil(t, tt, "Copy", 24) {
		t.Fatal("did not reach Copy")
	}
	if !a.focusRT.FocusShows() {
		t.Fatal("tab did not show a ring on Copy")
	}
	if err := tt.Click("Copy"); err != nil {
		t.Fatal(err)
	}
	if a.focusRT.FocusShows() {
		t.Fatal("click left the ring on Copy")
	}
}

func TestFullKeyboardAccessAppliesOnActivation(t *testing.T) {
	_, tt := focusChats(t)
	focusDraft(t, tt)
	tt.Key(0, ui.KeyTab)
	if tt.Focused("More") || tt.Focused("Send") || tt.Focused("Model gpt-4o-mini") {
		t.Fatal("Full Keyboard Access off visited a button")
	}
	tt.StagePreferences(ui.Preferences{FullKeyboardAccess: true, TextScale: 1})
	tt.Frame()
	focusDraft(t, tt)
	tt.Key(0, ui.KeyTab)
	if tt.Focused("More") || tt.Focused("Send") || tt.Focused("Model gpt-4o-mini") {
		t.Fatal("staged Full Keyboard Access applied before the window was activated")
	}
	tt.SetFocused(false)
	tt.SetFocused(true)
	focusDraft(t, tt)
	if !focusUntil(t, tt, "More", 12) {
		t.Fatal("activating the window did not apply Full Keyboard Access")
	}
}

func TestSettingsPopupRestoresControl(t *testing.T) {
	a, tt := focusChats(t)
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	enableFullKeyboard(tt)
	if !focusUntil(t, tt, "Default model menu", 12) {
		t.Fatal("did not reach Default model menu")
	}
	tt.Key(0, ui.KeySpace)
	tt.Frame()
	st := a.popovers["settings-model"]
	if st == nil || !st.open {
		t.Fatal("menu did not open")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.popupOpen() || a.returnDepth != 0 {
		t.Fatalf("open %v depth %d", a.popupOpen(), a.returnDepth)
	}
	if !tt.Focused("Default model menu") {
		t.Fatal("keyboard menu did not return to Default model menu")
	}
}

func TestIMEHandoffSkipsCaret(t *testing.T) {
	a, tt := focusChats(t)
	tt.Type("hello")
	tt.Frame()
	conv := a.active()
	if conv.Draft != "hello" {
		t.Fatalf("draft %q", conv.Draft)
	}
	// The live caret stays at the end. Remembering rune 1 makes a
	// placeDraftCaret on the handoff frame observable: the next
	// noteComposerCaret would record 1 instead of the end.
	conv.draftCaret = 1
	conv.draftAnchor = 1
	conv.draftCaretSet = true
	conv.draftCaretDraft = conv.Draft
	conv.Messages[len(conv.Messages)-1].Content = "answer text here"
	tt.Frame()
	dragSelect(t, tt, "answer text here", "answer text here")
	tt.Frame()
	if a.quoteBarOpen {
		tt.Key(0, ui.KeyEscape)
	}
	if strings.TrimSpace(a.histSel.Text) == "" || a.settled.kind != focusMessage {
		t.Fatalf("selection %q kind %d", a.histSel.Text, a.settled.kind)
	}
	tt.Key(0, ui.KeyN)
	if a.active().Draft != "hello" {
		t.Fatalf("handoff wrote %q", a.active().Draft)
	}
	if a.imeHandoff {
		t.Fatal("handoff flag was not consumed on the next frame")
	}
	active, off := tt.TextInputActive()
	if !active || off {
		t.Fatalf("composer input method active %v off %v", active, off)
	}
	if a.active().draftCaret == 1 {
		t.Fatal("handoff frame restored the remembered mid-text caret")
	}
	if a.focusRT.FocusShows() {
		t.Fatal("selection handoff left the keyboard ring on")
	}
}

func TestRenameReturnsToSidebarOrComposer(t *testing.T) {
	a, tt := focusChats(t)
	enableFullKeyboard(tt)
	if !focusUntil(t, tt, "More", 16) {
		t.Fatal("did not reach More")
	}
	tt.Key(0, ui.KeySpace)
	tt.Frame()
	if err := tt.Click("Rename"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if tt.Focused("More") {
		t.Fatal("rename returned to More")
	}
	requireComposer(t, a, tt)

	tt.Key(ui.Cmd, ui.KeyK)
	tt.Frame()
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Rename"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if tt.Focused("Search") || a.settled.kind == focusSearch {
		t.Fatal("rename returned to search")
	}
	requireComposer(t, a, tt)

	tt.Key(ui.Shift, ui.KeyTab)
	requireSidebar(t, a, "Focus")
	tt.RightClick("Focus")
	tt.Frame()
	if err := tt.ChooseMenuItem("Rename"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	requireSidebar(t, a, "Focus")
}

func TestDialogRingFollowsFocus(t *testing.T) {
	a, tt := focusChats(t)
	tt.Key(ui.Shift, ui.KeyTab)
	requireSidebar(t, a, "Focus")
	tt.Key(ui.Cmd, ui.KeyBackspace)
	tt.Frame()
	if !tt.Focused("Cancel") {
		t.Fatal("Cancel")
	}
	tt.Key(0, ui.KeyTab)
	if !tt.Focused("Delete") {
		t.Fatal("Tab did not move to Delete")
	}
	if tt.Focused("Cancel") {
		t.Fatal("Cancel kept focus")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	requireSidebar(t, a, "Focus")
}

func TestUnknownAccentContrast(t *testing.T) {
	red := ui.Color{R: 255, G: 59, B: 48, A: 255}
	yellow := ui.Color{R: 255, G: 204, A: 255}
	for _, dark := range []bool{false, true} {
		for _, accent := range []ui.Color{red, yellow} {
			got := contrastRing(accent, dark)
			for _, id := range []string{"pink", "orange", "purple", "blue", "green"} {
				named := themes.FocusContrastRing(id, dark)
				if got == named {
					t.Fatalf("accent %v dark %v snapped to %s %v", accent, dark, id, named)
				}
			}
			if got.A != 255 {
				t.Fatalf("contrast ring alpha %d", got.A)
			}
		}
	}
	blue, _ := themes.NamedAccent("blue", false)
	if contrastRing(blue, false) != themes.FocusContrastRing("blue", false) {
		t.Fatal("named blue did not use the contrast table")
	}
}

func TestRingFadeDoesNotRestart(t *testing.T) {
	a := newTestApp(t)
	a.instant = false
	a.reduceMotion = false
	a.ringSeen = 10
	a.ringAt = time.Unix(100, 0)
	a.ringAlpha(20)
	if !a.ringAt.Equal(time.Unix(100, 0)) || a.ringID != 20 {
		t.Fatalf("move restarted the fade at %v id %d", a.ringAt, a.ringID)
	}
	a.reduceMotion = true
	if a.ringAlpha(20) != 1 {
		t.Fatal("reduce motion did not show the ring at once")
	}
	a.look.Focused = false
	if a.ringColor().A != 0 {
		t.Fatal("inactive window kept a ring color")
	}
}

func TestInactiveWindowHidesRing(t *testing.T) {
	a, tt := focusChats(t)
	tt.Key(ui.Shift, ui.KeyTab)
	requireSidebar(t, a, "Focus")
	row := sidebarTitleRect(t, tt, "Focus")
	on := sampleAt(tt, row.X+4, row.Y+1)
	center := sampleAt(tt, row.X+row.W/2, row.Y+row.H/2)
	tt.SetFocused(false)
	tt.Frame()
	off := sampleAt(tt, row.X+4, row.Y+1)
	offCenter := sampleAt(tt, row.X+row.W/2, row.Y+row.H/2)
	if rgbaDelta(off, offCenter) > 18 && rgbaDelta(on, center) > 8 {
		t.Fatalf("ring stayed on an inactive window edge %v center %v", off, offCenter)
	}
}

func TestSidebarHomeEnd(t *testing.T) {
	a, tt := focusChats(t)
	tt.Key(ui.Shift, ui.KeyTab)
	requireSidebar(t, a, "Focus")
	tt.Key(0, ui.KeyUp)
	requireSidebar(t, a, "Focus")
	tt.Key(0, ui.KeyEnd)
	requireSidebar(t, a, "Beta")
	tt.Key(0, ui.KeyDown)
	requireSidebar(t, a, "Beta")
	tt.Key(0, ui.KeyHome)
	requireSidebar(t, a, "Focus")
}

func TestSidebarDeleteCancelRestores(t *testing.T) {
	a, tt := focusChats(t)
	tt.Key(ui.Shift, ui.KeyTab)
	requireSidebar(t, a, "Focus")
	tt.Key(ui.Cmd, ui.KeyBackspace)
	tt.Frame()
	if !tt.Focused("Cancel") {
		t.Fatal("Cancel")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	requireSidebar(t, a, "Focus")
}

func TestFailedSendKeepsFocus(t *testing.T) {
	a, tt := focusChats(t)
	a.data.Connections = nil
	conv := a.active()
	conv.Config.Model = ""
	conv.Config.ConnectionID = ""
	tt.Type("hi")
	tt.Key(0, ui.KeyEnter)
	if a.active().Draft != "hi" {
		t.Fatalf("failed send cleared the draft %q", a.active().Draft)
	}
	requireComposer(t, a, tt)
}

func TestTitleChangeDoesNotStealFocus(t *testing.T) {
	a, tt := focusChats(t)
	requireComposer(t, a, tt)
	a.active().Title = "Generated title"
	tt.Frame()
	requireComposer(t, a, tt)
	tt.Key(ui.Shift, ui.KeyTab)
	requireSidebar(t, a, "Generated title")
	a.active().Title = "Generated again"
	tt.Frame()
	requireSidebar(t, a, "Generated again")
}

func TestTypingDuringStreaming(t *testing.T) {
	a, tt := focusChats(t)
	armStream(t, a)
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	if !a.isStreaming("c1") {
		t.Fatal("Esc stopped the stream")
	}
	tt.Type("still here")
	if a.active().Draft != "still here" {
		t.Fatalf("draft %q", a.active().Draft)
	}
	requireComposer(t, a, tt)
}

func TestTwentyChatSwitches(t *testing.T) {
	a, tt := focusChats(t)
	tt.Type("hello world")
	for i := 0; i < 3; i++ {
		tt.Key(0, ui.KeyLeft)
	}
	for i := 0; i < 20; i++ {
		title := "Alpha"
		if i%2 == 1 {
			title = "Focus"
		}
		if err := tt.Click(title); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		requireComposer(t, a, tt)
	}
	tt.Type("X")
	if a.active().Draft != "hello woXrld" {
		t.Fatalf("caret after 20 switches: %q", a.active().Draft)
	}
}

func TestGeneratingTitleAnnounced(t *testing.T) {
	a, tt := focusChats(t)
	a.titleJobs = map[string]*titleJob{"c2": {gen: 1, manual: true}}
	tt.Frame()
	tt.Key(ui.Shift, ui.KeyTab)
	requireSidebar(t, a, "Focus")
	if announced(tt, titleGenerating) {
		t.Fatal("a row that is not generating announced a title")
	}
	tt.Key(0, ui.KeyDown)
	requireSidebar(t, a, "Alpha")
	if !announced(tt, titleGenerating) {
		t.Fatal("moving onto a generating title was silent")
	}
}

func clickSmall(t *testing.T, tt *ui.Tester, label string, maxH, maxW float32) {
	t.Helper()
	var best ui.Rect
	ok := false
	for _, r := range tt.FindAll(label) {
		if maxH > 0 && r.H > maxH {
			continue
		}
		if maxW > 0 && r.W > maxW {
			continue
		}
		if !ok || r.W*r.H < best.W*best.H {
			best, ok = r, true
		}
	}
	if !ok {
		t.Fatalf("no small %q", label)
	}
	tt.ClickAt(best.X+best.W/2, best.Y+best.H/2)
}

func announced(tt *ui.Tester, text string) bool {
	for _, s := range tt.Announcements() {
		if s == text {
			return true
		}
	}
	return false
}

func TestTitleSwitchClickKeepsFocus(t *testing.T) {
	a, tt := focusChats(t)
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	scrollTitlesIntoView(t, a, tt)
	before := a.data.AutoTitles
	clickSmall(t, tt, titleToggleLabel, 22, 40)
	if tt.Focused(titleToggleLabel) {
		t.Fatal("clicking the title switch took focus")
	}
	if a.data.AutoTitles == before {
		t.Fatal("title switch did not toggle")
	}
}

func TestTitleModelMenuRestoresButton(t *testing.T) {
	a, tt := focusChats(t)
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	enableFullKeyboard(tt)
	label := "Title model, " + titleSameAsChat
	if !focusUntil(t, tt, label, 24) {
		t.Fatal("did not reach the title model button")
	}
	tt.Key(0, ui.KeySpace)
	tt.Frame()
	st := a.popovers["settings-title-model"]
	if st == nil || !st.open {
		t.Fatal("title model menu did not open")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.popupOpen() || a.returnDepth != 0 {
		t.Fatalf("open %v depth %d", a.popupOpen(), a.returnDepth)
	}
	if !tt.Focused(label) {
		t.Fatal("title model menu did not return to its button")
	}
}

func TestRegenerateTitleKeepsPlace(t *testing.T) {
	a, tt := focusChats(t)
	enableFullKeyboard(tt)
	if !focusUntil(t, tt, "More", 16) {
		t.Fatal("did not reach More")
	}
	tt.Key(0, ui.KeySpace)
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if st := a.popovers["title-more"]; st != nil && st.open {
		t.Fatalf("menu stayed open, highlight %d", st.hi)
	}
	if !tt.Focused("More") {
		t.Fatal("keyboard Regenerate Title did not return to More")
	}
	if a.settled.kind == focusComposer {
		t.Fatal("keyboard Regenerate Title moved to the composer")
	}
	active := a.data.ActiveID

	focusDraft(t, tt)
	requireComposer(t, a, tt)
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click(titleRegenLabel); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	requireComposer(t, a, tt)
	if a.data.ActiveID != active {
		t.Fatalf("More regenerate switched to %s", a.data.ActiveID)
	}

	tt.Key(ui.Shift, ui.KeyTab)
	requireSidebar(t, a, "Focus")
	tt.RightClick("Alpha")
	tt.Frame()
	if err := tt.ChooseMenuItem(titleRegenLabel); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.data.ActiveID != active {
		t.Fatalf("sidebar regenerate switched to %s", a.data.ActiveID)
	}
	requireSidebar(t, a, "Focus")
}

func TestTabFromDisabledRegenerateGoesToMore(t *testing.T) {
	a, tt := focusChats(t)
	enableFullKeyboard(tt)
	focusDraft(t, tt)
	if !focusUntil(t, tt, "Regenerate", 12) {
		t.Fatal("did not reach Regenerate")
	}
	armStream(t, a)
	tt.Frame()
	if a.canRegenerate() {
		t.Fatal("streaming left Regenerate enabled")
	}
	tt.Key(0, ui.KeyTab)
	if tt.Focused("Search") || tt.Focused("Message") {
		t.Fatal("Tab from disabled Regenerate jumped to the start of the chain")
	}
	if !tt.Focused("More") {
		t.Fatal("Tab from disabled Regenerate did not land on More")
	}
	tt.Key(ui.Shift, ui.KeyTab)
	if tt.Focused("Search") || tt.Focused("Regenerate") {
		t.Fatal("Shift-Tab from More wrapped to Search or landed on disabled Regenerate")
	}
	if !tt.Focused("Stop") {
		t.Fatal("Shift-Tab did not skip disabled Regenerate and land on Stop")
	}
}

func TestTabFromDisabledSettingsSliderGoesForward(t *testing.T) {
	a, tt := focusChats(t)
	temp := 1.2
	a.data.Defaults.Temperature = &temp
	a.prefReady = false
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	enableFullKeyboard(tt)
	if !focusUntil(t, tt, "Temperature", 16) {
		t.Fatal("did not reach the Temperature slider")
	}
	a.pref.tempOn = false
	tt.Frame()
	tt.Key(0, ui.KeyTab)
	if tt.Focused("Search settings") || tt.Focused("General") {
		t.Fatal("Tab from the disabled slider jumped to the start of Settings")
	}
	if !tt.Focused("Send temperature") {
		t.Fatal("Tab from the disabled slider did not land on Send temperature")
	}
}

func TestSearchEscapeAfterSettingsKeepsSidebar(t *testing.T) {
	a, tt := focusChats(t)
	tt.Key(ui.Shift, ui.KeyTab)
	requireSidebar(t, a, "Focus")
	tt.Key(ui.Cmd, ui.KeyK)
	if a.settled.kind != focusSearch || !a.searchOriginSet || a.searchOrigin.kind != focusSidebar {
		t.Fatalf("search origin kind %d set %v settled %d", a.searchOrigin.kind, a.searchOriginSet, a.settled.kind)
	}
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.settingsOpen {
		t.Fatal("settings did not open")
	}
	a.settingsOpen = false
	tt.Frame()
	if !a.searchOriginSet || a.searchOrigin.kind != focusSidebar {
		t.Fatalf("closing Settings wiped the search origin set=%v kind=%d", a.searchOriginSet, a.searchOrigin.kind)
	}
	if !tt.Focused("Search") {
		if err := tt.Click("Search"); err != nil {
			t.Fatal(err)
		}
	}
	tt.Key(0, ui.KeyEscape)
	requireSidebar(t, a, "Focus")
}

func TestUnsupportedReasoningIsNotATabStop(t *testing.T) {
	_, tt := focusChats(t)
	chip, ok := tt.Find("Reasoning: Default")
	if !ok {
		t.Fatal("unsupported reasoning chip was removed")
	}
	// A keyboard tip from the same instant wins the tie. Step the clock
	// so this hover is the later one, then rest past the tooltip delay.
	tt.Advance(time.Millisecond)
	tt.Move(chip.X+chip.W/2, chip.Y+chip.H/2)
	tt.Advance(1500 * time.Millisecond)
	tt.Frame()
	if !tt.HasText("This model doesn't support reasoning effort.") {
		t.Fatalf("unsupported reasoning tooltip missing: %q", tt.Texts())
	}
	tt.Move(8, 8)

	enableFullKeyboard(tt)
	focusDraft(t, tt)
	if !focusUntil(t, tt, "Model gpt-4o-mini", 8) {
		t.Fatal("model chip")
	}
	tt.Key(0, ui.KeyTab)
	if tt.Focused("Reasoning: Default") {
		t.Fatal("unsupported reasoning chip took Tab")
	}
	if !tt.Focused("New Session") {
		t.Fatal("Tab did not skip the unsupported reasoning chip")
	}
}

func TestCodeBlockHeaderCorners(t *testing.T) {
	a, tt := focusChats(t)
	a.active().Messages[1].Content = "ans\n\n```\nfmt.Println(1)\n```\n"
	tt.Frame()
	block, ok := tt.Find("code block")
	if !ok || block.W < 40 || block.H < 40 {
		t.Fatalf("code block %+v", block)
	}
	corner := sampleAt(tt, block.X+1, block.Y+1)
	outside := sampleAt(tt, block.X+1, block.Y-4)
	header := sampleAt(tt, block.X+radiusCode+12, block.Y+8)
	right := sampleAt(tt, block.X+block.W-2, block.Y+1)
	rightOut := sampleAt(tt, block.X+block.W+3, block.Y+1)
	if rgbaDelta(corner, header) <= rgbaDelta(corner, outside) {
		t.Fatalf("top-left corner %v matches the header %v more than the outside %v", corner, header, outside)
	}
	if rgbaDelta(right, header) <= rgbaDelta(right, rightOut) {
		t.Fatalf("top-right corner %v matches the header %v more than the outside %v", right, header, rightOut)
	}

	enableFullKeyboard(tt)
	if !focusUntil(t, tt, "Copy code", 24) {
		t.Fatal("code-block Copy is not a tab stop")
	}
	btn, ok := tt.FocusedRect("Copy code")
	if !ok {
		t.Fatal("Copy code has no focused rect")
	}
	ring := sampleAt(tt, btn.X-2, btn.Y+btn.H/2)
	away := sampleAt(tt, btn.X-12, btn.Y+btn.H/2)
	if rgbaDelta(ring, away) < 8 {
		t.Fatalf("Copy ring %v matches the header %v at %+v", ring, away, btn)
	}
	if btn.X-2 < block.X || btn.X+btn.W+3 > block.X+block.W {
		t.Fatalf("Copy ring extends outside the code block: button %+v block %+v", btn, block)
	}
}

func TestSettingsMenuCloseStaysInSettings(t *testing.T) {
	a, _ := focusChats(t)
	a.settingsSection = "general"
	tt := ui.NewTester(a.settingsWindowRoot, 780, 560)
	tt.Frame()
	if err := tt.Click("Default model menu"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	st := a.popovers["settings-model"]
	if st == nil || !st.open {
		t.Fatal("menu did not open")
	}
	tt.ClickAt(400, 24)
	tt.Frame()
	if a.popupOpen() {
		t.Fatal("outside click left the settings menu open")
	}
	if a.focusComposerID != "" || a.settled.kind == focusComposer || tt.Focused("Message") {
		t.Fatalf("menu close asked for the composer id=%q kind=%d", a.focusComposerID, a.settled.kind)
	}
	if a.settled.kind != focusSettingsNav && !tt.Focused("General") && !tt.Focused("Settings") {
		t.Fatalf("settings focus kind %d", a.settled.kind)
	}
	if a.settled.visible {
		t.Fatal("mouse-closed settings menu left a focus ring")
	}
}

func TestKeyboardMenuOutsideClickCloses(t *testing.T) {
	a, tt := focusChats(t)
	enableFullKeyboard(tt)
	focusDraft(t, tt)
	if !focusUntil(t, tt, "More", 16) {
		t.Fatal("Tab did not reach More")
	}
	before := a.data.ActiveID
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if !moreOpen(tt) {
		t.Fatal("Down did not open More")
	}
	tt.ClickAt(500, 400)
	if moreOpen(tt) {
		t.Fatal("first outside click left the keyboard-opened menu open")
	}
	if !tt.Focused("More") {
		t.Fatal("outside click did not restore More")
	}
	if a.settled.visible {
		t.Fatal("outside click left a focus ring")
	}

	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if !moreOpen(tt) {
		t.Fatal("Down did not reopen More")
	}
	other := mustFind(t, tt, "Alpha")
	tt.ClickAt(other.X+other.W/2, other.Y+other.H/2)
	if moreOpen(tt) {
		t.Fatal("first click on a sidebar row left the menu open")
	}
	if a.data.ActiveID != before {
		t.Fatalf("outside click switched to %s", a.data.ActiveID)
	}
	if a.settled.visible {
		t.Fatal("row click left a focus ring")
	}
}
