package main

import (
	"strings"
	"time"
	"unsafe"

	"cove/themes"

	"github.com/egoist/mygo/ui"
)

// Focus restoration records where the keyboard was before a menu, dialog,
// or sheet opened, and puts it back when that surface closes. The snapshot
// is the place from the frame before the click, so a pointer click does
// not leave the keyboard on the button that opened the surface.

type focusKind uint8

const (
	focusNone focusKind = iota
	focusComposer
	focusSearch
	focusFind
	focusSidebar
	focusControl
	focusMessage
	focusEdit
	focusSettingsNav
)

const (
	focusSurfMain = iota
	focusSurfSettings
)

type focusPlace struct {
	kind    focusKind
	id      string
	elem    uint64
	anchor  int
	caret   int
	caretOK bool
	visible bool
}

type tabKind uint8

const (
	tabKindElem tabKind = iota
	tabKindSearch
	tabKindList
	tabKindComposer
	tabKindFind
	tabKindEdit
	tabKindSettings
)

type tabHit struct {
	kind tabKind
	elem uint64
	name string
}

const focusRingWidth float32 = 3

//go:linkname engineFocusedID github.com/egoist/mygo/ui.(*engine).focusedID
func engineFocusedID(rt unsafe.Pointer) uint64

//go:linkname engineFocusShows github.com/egoist/mygo/ui.(*engine).focusShows
func engineFocusShows(rt unsafe.Pointer) bool

//go:linkname engineFocusLive github.com/egoist/mygo/ui.(*engine).focusLive
func engineFocusLive(rt unsafe.Pointer, id uint64, visible bool) bool

//go:linkname engineSetFocusShows github.com/egoist/mygo/ui.(*engine).setFocusShows
func engineSetFocusShows(rt unsafe.Pointer, on bool)

//go:linkname engineFocusID github.com/egoist/mygo/ui.(*engine).focusID
func engineFocusID(rt unsafe.Pointer, id uint64, visible bool)

//go:linkname engineSetSelectionHandoff github.com/egoist/mygo/ui.(*engine).setSelectionHandoff
func engineSetSelectionHandoff(rt unsafe.Pointer, fn func() bool)

//go:linkname engineFocusedSelectable github.com/egoist/mygo/ui.(*engine).focusedSelectable
func engineFocusedSelectable(rt unsafe.Pointer) bool

func (a *App) focusSurf() int {
	if a.buildingSettings || (a.settingsOpen && a.prefs == nil) {
		return focusSurfSettings
	}
	return focusSurfMain
}

func (a *App) pushFocusReturn() {
	if a.returnDepth == 0 {
		a.returnTo = a.settled
		a.returnSurf = a.focusSurf()
	}
	a.returnDepth++
	a.returnPending = false
}

func (a *App) popFocusReturn() {
	if a.returnDepth == 0 {
		return
	}
	a.returnDepth--
	if a.returnDepth == 0 {
		a.returnPending = true
	}
}

func (a *App) abandonFocusReturn() {
	a.returnDepth = 0
	a.returnPending = false
}

// dropPendingFocusReturn leaves a nested surface's snapshot in place and
// skips the restore queued for this frame. A control that focuses itself
// on the way out of a menu uses this.
func (a *App) dropPendingFocusReturn() {
	a.returnPending = false
}

func (a *App) finishFocusReturn(c *ui.Context, surf int) {
	if a.searchCancel && a.returnDepth == 0 {
		a.searchCancel = false
		origin, set := a.searchOrigin, a.searchOriginSet
		a.searchOriginSet = false
		if set {
			a.restorePlace(c, origin)
		} else {
			a.focusComposer()
		}
	}
	if !a.returnPending || a.returnDepth != 0 || a.returnSurf != surf {
		return
	}
	a.returnPending = false
	a.restorePlace(c, a.returnTo)
}

func (a *App) restorePlace(c *ui.Context, p focusPlace) {
	switch p.kind {
	case focusSidebar:
		if a.data.ActiveID == "" {
			return
		}
		a.wantSidebarFocus = true
		a.wantSidebarRing = p.visible
		if a.side.Focus(c) {
			a.showFocusRing(c, p.visible)
			a.wantSidebarFocus = false
			a.wantSidebarRing = false
		}
		a.sayFocus(focusSidebar, a.data.ActiveID)
		return
	case focusSettingsNav:
		a.focusNavID = p.id
		a.wantSettingsRing = p.visible
		return
	}
	if p.elem != 0 {
		if rt := contextRuntime(c); rt != nil && engineFocusLive(rt, p.elem, p.visible) {
			if p.kind == focusComposer {
				a.sayFocus(focusComposer, p.id)
			}
			return
		}
	}
	switch p.kind {
	case focusComposer, focusEdit:
		a.focusComposer()
		a.sayFocus(focusComposer, a.data.ActiveID)
	case focusSearch:
		a.focusSearch = true
		if a.searchInput.Valid() {
			a.searchInput.Focus()
			a.showFocusRing(c, p.visible)
		}
	case focusFind:
		a.finder.wantFocus = true
	case focusMessage:
		if strings.TrimSpace(a.histSel.Text) == "" {
			a.focusComposer()
		}
	case focusControl:
		a.focusComposer()
	default:
		if a.buildingSettings || (a.settingsOpen && a.prefs == nil) {
			if a.focusNavID == "" {
				a.focusNavID = a.settingsSection
			}
			return
		}
		a.focusComposer()
	}
}

func (a *App) showFocusRing(c *ui.Context, on bool) {
	if rt := contextRuntime(c); rt != nil {
		engineSetFocusShows(rt, on)
	}
}

func (a *App) noteSettledFocus(c *ui.Context) {
	if a.returnDepth > 0 {
		return
	}
	if a.win != nil && !appWindowFocused(c) {
		return
	}
	searchNow := a.searchInput.Valid() && a.searchInput.Focused()
	if searchNow && !a.searchWas {
		a.searchOrigin = a.settled
		a.searchOriginSet = true
	}
	a.searchWas = searchNow
	a.settled = a.captureFocus(c)
}

func (a *App) captureFocus(c *ui.Context) focusPlace {
	rt := contextRuntime(c)
	var elem uint64
	var visible bool
	selectable := false
	if rt != nil {
		elem = engineFocusedID(rt)
		visible = engineFocusShows(rt)
		selectable = engineFocusedSelectable(rt)
	}
	p := focusPlace{elem: elem, visible: visible}
	switch {
	case a.composerField.Valid() && a.composerField.Focused():
		p.kind = focusComposer
		p.id = a.data.ActiveID
		if conv := a.active(); conv != nil {
			anchor, caret := a.composerField.TextAnchorCaret()
			p.anchor, p.caret, p.caretOK = anchor, caret, true
			conv.draftAnchor, conv.draftCaret, conv.draftCaretSet = anchor, caret, true
			conv.draftCaretDraft = conv.Draft
		}
	case a.searchInput.Valid() && a.searchInput.Focused():
		p.kind = focusSearch
	case a.finder.open && a.finder.focused:
		p.kind = focusFind
	case a.side.Focused(c):
		p.kind = focusSidebar
		p.id = a.sideRowID(a.sideIndex)
	case a.settingsNav.Focused(c):
		p.kind = focusSettingsNav
		p.id = a.settingsSection
	case a.edit.convID != "":
		p.kind = focusEdit
		p.id = a.edit.convID
	case selectable && strings.TrimSpace(a.histSel.Text) != "":
		p.kind = focusMessage
		p.id = a.data.ActiveID
	case elem != 0:
		p.kind = focusControl
	}
	return p
}

func (a *App) sideRowID(index int) string {
	if index < 0 || index >= len(a.sideRows) {
		return ""
	}
	row := a.sideRows[index]
	if row.header != "" || row.index < 0 || row.index >= len(a.data.Conversations) {
		return ""
	}
	return a.data.Conversations[row.index].ID
}

func (a *App) syncSideIndex() {
	a.sideIndex = -1
	for i, row := range a.sideRows {
		if row.header != "" || row.index < 0 || row.index >= len(a.data.Conversations) {
			continue
		}
		if a.data.Conversations[row.index].ID == a.data.ActiveID {
			a.sideIndex = i
			return
		}
	}
}

func (a *App) visibleChatIDs() []string {
	if len(a.sideRows) > 0 {
		ids := make([]string, 0, len(a.sideRows))
		for _, row := range a.sideRows {
			if row.header != "" || row.index < 0 || row.index >= len(a.data.Conversations) {
				continue
			}
			ids = append(ids, a.data.Conversations[row.index].ID)
		}
		return ids
	}
	ids := make([]string, len(a.data.Conversations))
	for i := range a.data.Conversations {
		ids[i] = a.data.Conversations[i].ID
	}
	return ids
}

// adjacentChatID is the visually next chat after id (older, further down
// the sidebar), or the previous chat when id is the last row.
func (a *App) adjacentChatID(id string) string {
	ids := a.visibleChatIDs()
	at := -1
	for i, cur := range ids {
		if cur == id {
			at = i
			break
		}
	}
	if at < 0 {
		return ""
	}
	if at+1 < len(ids) {
		return ids[at+1]
	}
	if at > 0 {
		return ids[at-1]
	}
	return ""
}

func (a *App) rememberDraftCaret(conv *Conversation) {
	if conv == nil || !a.composerField.Valid() || conv.ID != a.data.ActiveID {
		return
	}
	anchor, caret := a.composerField.TextAnchorCaret()
	conv.draftAnchor, conv.draftCaret, conv.draftCaretSet = anchor, caret, true
	conv.draftCaretDraft = conv.Draft
}

// placeDraftCaret puts the caret when the composer takes the keyboard.
// A window activate uses the global record and, when that record was
// cleared, the end of the draft. Otherwise the caret remembered for this
// chat is restored.
func (a *App) placeDraftCaret(input ui.Element, conv *Conversation) {
	if conv == nil || !input.Valid() {
		return
	}
	if a.restoreComposerCaret {
		a.restoreComposerCaret = false
		if a.composerCaretSet {
			input.SetTextSelection(a.composerAnchor, a.composerCaretPos)
		} else {
			end, _ := composerEnd(conv.Draft)
			input.SetTextSelection(end, end)
		}
		return
	}
	if conv.draftCaretSet && conv.draftCaretDraft == conv.Draft {
		input.SetTextSelection(conv.draftAnchor, conv.draftCaret)
		return
	}
	end, _ := composerEnd(conv.Draft)
	input.SetTextSelection(end, end)
	conv.draftAnchor, conv.draftCaret, conv.draftCaretSet = end, end, true
	conv.draftCaretDraft = conv.Draft
}

func (a *App) sayFocus(kind focusKind, id string) {
	if a.announce != "" {
		return
	}
	switch kind {
	case focusComposer:
		a.announce = "Message Cove, text field"
	case focusSidebar:
		if conv := a.conversation(id); conv != nil && conv.Title != "" {
			a.announce = conv.Title
		}
	}
}

func (a *App) stayOnSidebar(ring bool) {
	a.abandonFocusReturn()
	a.focusComposerID = ""
	if a.data.ActiveID == "" {
		return
	}
	a.wantSidebarFocus = true
	a.wantSidebarRing = ring
	a.sayFocus(focusSidebar, a.data.ActiveID)
}

// handoffSelectionKey runs while a message selection has the keyboard and
// an unmodified printable key arrives. The composer becomes first
// responder so the host can re-dispatch that same key through the input
// method. The character is not written here.
func (a *App) handoffSelectionKey() bool {
	if a.blocksComposerFocus() || a.edit.convID != "" {
		return false
	}
	if a.settled.kind != focusMessage && strings.TrimSpace(a.histSel.Text) == "" {
		return false
	}
	// The composer element from the last frame is not a live handle between
	// frames. Its id still names the text field, so the original key can
	// go through the input method before the next frame rebuilds it.
	if a.focusRT == nil || !engineFocusLive(a.focusRT, a.composerElem, true) {
		return false
	}
	if conv := a.active(); conv != nil {
		a.focusComposerID = conv.ID
	}
	a.sayFocus(focusComposer, a.data.ActiveID)
	return true
}

func (a *App) bindSelectionHandoff(c *ui.Context) {
	rt := contextRuntime(c)
	if rt == nil {
		return
	}
	a.focusRT = rt
	engineSetSelectionHandoff(rt, a.handoffSelectionKey)
}

// --- sidebar search -------------------------------------------------------

func (a *App) syncSearchPick() {
	ids := a.visibleChatIDs()
	if len(ids) == 0 {
		a.searchPick = ""
		return
	}
	for _, id := range ids {
		if id == a.searchPick {
			return
		}
	}
	for _, id := range ids {
		if id == a.data.ActiveID {
			a.searchPick = id
			return
		}
	}
	a.searchPick = ids[0]
}

func (a *App) moveSearch(d int) {
	ids := a.visibleChatIDs()
	if len(ids) == 0 {
		a.searchPick = ""
		return
	}
	a.syncSearchPick()
	at := 0
	for i, id := range ids {
		if id == a.searchPick {
			at = i
			break
		}
	}
	at += d
	if at < 0 {
		at = 0
	}
	if at >= len(ids) {
		at = len(ids) - 1
	}
	a.searchPick = ids[at]
	for i, row := range a.sideRows {
		if row.header != "" || row.index < 0 || row.index >= len(a.data.Conversations) {
			continue
		}
		if a.data.Conversations[row.index].ID == a.searchPick {
			a.side.ScrollIntoView(i)
			return
		}
	}
}

func (a *App) enterSearchList() {
	a.syncSearchPick()
	if a.searchPick == "" {
		return
	}
	a.activateChat(a.searchPick, false)
	a.wantSidebarFocus = true
	a.wantSidebarRing = true
	a.sayFocus(focusSidebar, a.searchPick)
}

func (a *App) commitSearch() string {
	a.syncSearchPick()
	id := a.searchPick
	a.query = ""
	a.searchPick = ""
	a.searchQuerySeen = ""
	if id == "" {
		return ""
	}
	a.activateChat(id, true)
	return id
}

func (a *App) onSearchKey(k ui.TextKey) bool {
	if k.Key == ui.KeyBackspace && k.Mods == ui.Cmd {
		a.askDelete()
		return true
	}
	if k.Mods != 0 {
		return false
	}
	switch k.Key {
	case ui.KeyEscape:
		if strings.TrimSpace(a.query) != "" {
			a.query = ""
			a.searchPick = ""
			a.searchQuerySeen = ""
			k.SetText("")
			return true
		}
		a.searchCancel = true
		return true
	case ui.KeyEnter:
		a.commitSearch()
		k.SetText("")
		return true
	case ui.KeyDown:
		a.enterSearchList()
		return true
	case ui.KeyUp:
		a.moveSearch(-1)
		return true
	}
	return false
}

func (a *App) noteSearchQuery() {
	q := strings.TrimSpace(a.query)
	if q == a.searchQuerySeen {
		return
	}
	a.searchQuerySeen = q
	a.searchPick = ""
}

// --- settings nav ---------------------------------------------------------

func (a *App) visibleSettingsNav() []settingsNav {
	out := make([]settingsNav, 0, len(settingsSections))
	for _, s := range settingsSections {
		if a.settingsSectionVisible(s) {
			out = append(out, s)
		}
	}
	return out
}

func (a *App) syncSettingsNav(rows []settingsNav) {
	a.settingsNavIndex = -1
	for i, s := range rows {
		if s.id == a.settingsSection {
			a.settingsNavIndex = i
			return
		}
	}
	if len(rows) > 0 {
		a.settingsNavIndex = 0
	}
}

// --- tab order ------------------------------------------------------------

func (a *App) resetTabTargets() {
	a.tabSearch = ui.Element{}
	a.tabList = ui.Element{}
	a.tabComposer = ui.Element{}
	a.tabFind = ui.Element{}
	a.tabEdit = ui.Element{}
	a.tabModel = ui.Element{}
	a.tabEffort = ui.Element{}
	a.tabSession = ui.Element{}
	a.tabSend = ui.Element{}
	a.tabRegen = ui.Element{}
	a.tabMore = ui.Element{}
	a.tabNewChat = ui.Element{}
	a.tabSettings = ui.Element{}
	a.tabQuotes = a.tabQuotes[:0]
	a.tabMessages = a.tabMessages[:0]
}

func (a *App) assembleTabs() {
	var chain []tabHit
	add := func(el ui.Element, kind tabKind, always bool) {
		if !el.Valid() {
			return
		}
		if !always && !a.fullKeyboard {
			return
		}
		chain = append(chain, tabHit{kind: kind, elem: el.ID()})
	}
	addID := func(id uint64, always bool) {
		if id == 0 || (!always && !a.fullKeyboard) {
			return
		}
		chain = append(chain, tabHit{kind: tabKindElem, elem: id})
	}
	add(a.tabSearch, tabKindSearch, true)
	add(a.tabList, tabKindList, true)
	for _, el := range a.tabQuotes {
		add(el, tabKindElem, false)
	}
	if a.tabEdit.Valid() {
		add(a.tabEdit, tabKindEdit, true)
	} else {
		add(a.tabComposer, tabKindComposer, true)
	}
	add(a.tabModel, tabKindElem, false)
	add(a.tabEffort, tabKindElem, false)
	add(a.tabSession, tabKindElem, false)
	add(a.tabSend, tabKindElem, false)
	add(a.tabRegen, tabKindElem, false)
	add(a.tabMore, tabKindElem, false)
	for _, el := range a.tabMessages {
		add(el, tabKindElem, false)
	}
	add(a.tabNewChat, tabKindElem, false)
	add(a.tabSettings, tabKindElem, false)
	add(a.tabFind, tabKindFind, true)
	_ = addID
	a.tabChain = chain
}

func (a *App) focusTab(c *ui.Context, back bool) {
	chain := a.tabChain
	n := len(chain)
	if n == 0 {
		return
	}
	cur := uint64(0)
	if rt := contextRuntime(c); rt != nil {
		cur = engineFocusedID(rt)
	}
	i := -1
	for j, hit := range chain {
		if hit.elem == cur {
			i = j
			break
		}
	}
	next := 0
	switch {
	case i < 0 && back:
		next = n - 1
	case i < 0:
		next = 0
	case back:
		next = (i - 1 + n) % n
	default:
		next = (i + 1) % n
	}
	hit := chain[next]
	switch hit.kind {
	case tabKindSearch:
		a.focusSearch = true
	case tabKindList:
		a.wantSidebarFocus = true
		a.wantSidebarRing = true
	case tabKindComposer, tabKindEdit:
		a.focusComposer()
		a.showFocusRing(c, true)
	case tabKindFind:
		a.finder.wantFocus = true
	case tabKindSettings:
		a.focusNavID = hit.name
		a.wantSettingsRing = true
	default:
		a.tabWant = hit.elem
		if rt := contextRuntime(c); rt != nil {
			engineFocusID(rt, hit.elem, true)
		}
	}
}

func (a *App) takeTab(c *ui.Context, el ui.Element) {
	if !el.Valid() || a.tabWant == 0 || el.ID() != a.tabWant {
		return
	}
	el.Focus()
	a.showFocusRing(c, true)
	a.tabWant = 0
}

func (a *App) noteMessageTab(el ui.Element) {
	if el.Valid() {
		a.tabMessages = append(a.tabMessages, el)
	}
}

func (a *App) noteQuoteTab(el ui.Element) {
	if el.Valid() {
		a.tabQuotes = append(a.tabQuotes, el)
	}
}

// --- rings ----------------------------------------------------------------

func (a *App) beginRings() {
	a.ringSeen = a.ringID
	a.ringID = 0
}

func (a *App) ringColor() ui.Color {
	if !a.look.Focused {
		return ui.Color{}
	}
	if a.paintCtx != nil && a.paintCtx.Preferences().HighContrast {
		return themes.FocusContrastRing(matchAccentID(a.look.AccentDefault, a.look.Dark), a.look.Dark)
	}
	return a.look.AccentRing
}

func matchAccentID(accent ui.Color, dark bool) string {
	best := "blue"
	bestD := 1 << 30
	for _, id := range themes.AccentIDs() {
		src, ok := themes.NamedAccent(id, dark)
		if !ok {
			continue
		}
		d := accentDistance(accent, src)
		if d < bestD {
			best, bestD = id, d
		}
	}
	return best
}

func accentDistance(a, b ui.Color) int {
	dr := int(a.R) - int(b.R)
	dg := int(a.G) - int(b.G)
	db := int(a.B) - int(b.B)
	return dr*dr + dg*dg + db*db
}

func (a *App) ringAlpha(id uint64) float32 {
	if a.ringID == 0 {
		a.ringID = id
		if a.ringSeen != id && a.paintCtx != nil {
			a.ringAt = a.paintCtx.Now()
		}
	}
	if a.instant || a.reduceMotion || (a.paintCtx != nil && a.paintCtx.Preferences().ReduceMotion) {
		return 1
	}
	const dur = 100 * time.Millisecond
	if a.ringAt.IsZero() || a.paintCtx == nil {
		return 1
	}
	elapsed := a.paintCtx.Now().Sub(a.ringAt)
	if elapsed >= dur {
		return 1
	}
	if elapsed < 0 {
		elapsed = 0
	}
	a.paintCtx.After(dur - elapsed)
	u := float32(elapsed) / float32(dur)
	return easeEnter(u)
}

func scaleAlpha(c ui.Color, a float32) ui.Color {
	if a <= 0 {
		c.A = 0
		return c
	}
	if a >= 1 {
		return c
	}
	c.A = uint8(float32(c.A)*a + 0.5)
	return c
}

func paintOuterFocusRing(p *ui.Painter, r ui.Rect, radius float32, color ui.Color) {
	if color.A == 0 {
		return
	}
	const w = focusRingWidth
	outer := ui.Rect{X: r.X - w, Y: r.Y - w, W: r.W + 2*w, H: r.H + 2*w}
	p.Stroke(outer, color, radius+w, w)
}

func paintInsetFocusRing(p *ui.Painter, r ui.Rect, radius float32, color ui.Color) {
	if color.A == 0 {
		return
	}
	p.Stroke(r, color, radius, 2)
}

func (a *App) strokeOuter(p *ui.Painter, r ui.Rect, radius float32, id uint64) {
	paintOuterFocusRing(p, r, radius, scaleAlpha(a.ringColor(), a.ringAlpha(id)))
}

func (a *App) strokeInset(p *ui.Painter, r ui.Rect, radius float32, id uint64) {
	paintInsetFocusRing(p, r, radius, scaleAlpha(a.ringColor(), a.ringAlpha(id)))
}

// keyboardRingOnly draws the keyboard ring. The send button already
// keeps the pointer from moving the keyboard.
func (a *App) keyboardRingOnly(el ui.Element, radius float32) {
	if !el.Valid() {
		return
	}
	el.FocusRing(false)
	el.DrawOver(func(p *ui.Painter, r ui.Rect) {
		if el.FocusVisible() {
			a.strokeOuter(p, r, radius, el.ID())
		}
	})
}

// quietControl keeps a pointer click from moving the keyboard and draws
// the keyboard focus ring when Tab landed here.
func (a *App) quietControl(el ui.Element, radius float32) {
	if !el.Valid() {
		return
	}
	el.KeepFocus().FocusRing(false)
	el.DrawOver(func(p *ui.Painter, r ui.Rect) {
		if el.FocusVisible() {
			a.strokeOuter(p, r, radius, el.ID())
		}
	})
}

// fieldRing draws the focus ring as soon as a text field has the keyboard,
// including after a click, and leaves the field's own border in place.
func (a *App) fieldRing(el ui.Element, radius float32) {
	if !el.Valid() {
		return
	}
	el.FocusRing(false)
	el.DrawOver(func(p *ui.Painter, r ui.Rect) {
		if el.Focused() {
			a.strokeOuter(p, r, radius, el.ID())
		}
	})
}

// alwaysRing draws the ring while the control exists and the window is
// focused, including when focus was placed programmatically.
func (a *App) alwaysRing(el ui.Element, radius float32) {
	if !el.Valid() {
		return
	}
	el.FocusRing(false)
	el.DrawOver(func(p *ui.Painter, r ui.Rect) {
		if a.look.Focused {
			a.strokeOuter(p, r, radius, el.ID())
		}
	})
}

func (a *App) keyboardInset(el ui.Element, radius float32) {
	if !el.Valid() {
		return
	}
	el.FocusRing(false)
	el.DrawOver(func(p *ui.Painter, r ui.Rect) {
		if el.FocusVisible() {
			a.strokeInset(p, r, radius, el.ID())
		}
	})
}

func (a *App) openRequest() {
	if !a.requestOpen {
		a.pushFocusReturn()
	}
	a.requestOpen = true
}

func (a *App) openAcks() {
	if !a.acksOpen {
		a.pushFocusReturn()
	}
	a.acksOpen = true
}

func (a *App) closeAcks() {
	if !a.acksOpen {
		return
	}
	a.acksOpen = false
	a.popFocusReturn()
}

func (a *App) finishSidebarList(c *ui.Context, list ui.Element) {
	if list.Changed() {
		id := a.sideRowID(a.sideIndex)
		if id != "" && id != a.data.ActiveID {
			a.activateChat(id, false)
			a.sayFocus(focusSidebar, id)
		}
	}
	if a.wantSidebarFocus && a.side.Focus(c) {
		a.showFocusRing(c, a.wantSidebarRing)
		a.wantSidebarFocus = false
		a.wantSidebarRing = false
	}
	if list.Submitted() {
		a.focusComposer()
	}
	a.sideKeyFocus = list.FocusVisible()
	a.tabList = list
	a.takeTab(c, list)
}
