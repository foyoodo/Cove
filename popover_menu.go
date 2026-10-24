package main

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// In-window popover menu (SPEC §11.3). Every menu that opens from a
// control inside the window uses this. Right-click context menus and the
// macOS menu bar stay on the system menu.
//
// Lengths match tokens.json layout.menu and radius/menu. The shadow is
// shadow/layer/overlay, whose second layer is the 0.5pt overlay border,
// so the panel does not draw a border of its own.

const (
	menuPad          float32 = 5
	menuLead         float32 = 6
	menuCheck        float32 = 18
	menuIcon         float32 = 16
	menuIconGap      float32 = 6
	menuTrail        float32 = 8
	menuRowH         float32 = 24
	menuHeaderH      float32 = 22
	menuHeaderGap    float32 = 4
	menuSepThick     float32 = 1
	menuSepMargin    float32 = 5
	menuSepInset     float32 = 10 // from the outer edge: padding 5 + 5
	menuMinW         float32 = 180
	menuMaxW         float32 = 300
	menuGap          float32 = 4
	menuWindowMargin float32 = 8
	menuMaxH         float32 = 360
	menuSearchH      float32 = 28
	menuSearchAt             = 10
	menuFadeIn               = 120 * time.Millisecond
	menuFadeOut              = 80 * time.Millisecond
	menuTypeWindow           = time.Second
	menuHighlightR   float32 = 6
	menuDot          float32 = 6
	menuCheckGlyph   float32 = 12
	menuSearchIcon   float32 = 13
)

// menuSepH is the separator's block: 5 above, 1 line, 5 below.
const menuSepH = menuSepMargin*2 + menuSepThick

// menuTextX is where a row's label starts, from the menu's left edge.
const menuTextX = menuPad + menuLead + menuCheck

// modelMenuMaxH is the name the scroll tests already use.
const modelMenuMaxH = menuMaxH

const (
	popItem = iota
	popHeader
	popSep
)

// popEntry is one row of a popover. Headers and separators are not
// selectable. A disabled item is drawn and skipped by the keyboard.
type popEntry struct {
	Kind      int
	ID        string
	Text      string
	Group     string
	Secondary string
	Dot       ui.Color
	Icon      *ui.SVG
	Checkable bool
	Checked   bool
	Disabled  bool
	MarkKind  string
	MarkProv  string
	MarkModel string
	MarkOn    bool
	Body      func(c *ui.Context, fg ui.Color)
}

// popSpec is one open menu. Items scroll. Footer stays pinned under the
// scroll, with a separator above it. Search, when set, is the model menu's
// field and is shown by the caller only once there are enough models.
type popSpec struct {
	Name          string
	TestLabel     string
	Items         []popEntry
	Footer        []popEntry
	FooterSepMark string
	Search        bool
	SearchPrompt  string
	OnSelect      func(id string)
	// ComposerFocus moves the caret back to the composer after a choice.
	// Esc, a click outside, blur, and resize return to the anchor instead.
	ComposerFocus bool
}

type popGeom struct {
	sel int
	r   ui.Rect
}

type popoverState struct {
	open    bool
	wasOpen bool
	// owner is the window that drew this popup. seen is the frame epoch
	// of that draw. A popup that misses a frame releases its focus record.
	owner          uintptr
	seen           uint64
	query          string
	sig            string
	hi             int
	needScroll     bool
	scroll         ui.ScrollState
	page           int
	winW, winH     float32
	px, py         float32
	havePointer    bool
	pointerPending bool
	geoms          []popGeom
	typeBuf        string
	typeAt         time.Time
	focusComposer  bool
	focusMoved     bool
	// pushed records that opening this menu took a focus snapshot.
	pushed    bool
	activate  bool
	closing   bool
	animAt    time.Time
	announced bool
	currentID string
	// keyboardOpen is set when Space, Return, or Down on the focused
	// anchor opens the menu. A pointer click leaves it false.
	keyboardOpen bool
	// pendingKey is the keyboard open, noticed before the frame builds.
	pendingKey bool
	// opened is set after the first paint chooses the initial highlight,
	// so a later filter change is not treated as a fresh open.
	opened bool
	// selN is how many selectable rows the last frame built, so a key
	// that arrives before the next build still knows where it can land.
	selN int
}

func (a *App) popoverState(key string) *popoverState {
	if a.popovers == nil {
		a.popovers = map[string]*popoverState{}
	}
	st, ok := a.popovers[key]
	if !ok {
		st = &popoverState{hi: -1}
		a.popovers[key] = st
	}
	return st
}

func (st *popoverState) reset(c *ui.Context, keyboard bool) {
	w, h := c.Size()
	*st = popoverState{
		open:         true,
		wasOpen:      true,
		hi:           -1,
		needScroll:   true,
		animAt:       c.Now(),
		winW:         w,
		winH:         h,
		keyboardOpen: keyboard,
	}
}

// bindPopover wires anchor to the menu named key. A click opens it. While
// it is open the menu is drawn on the window layer, above the card, and
// keys move its highlight.
func (a *App) bindPopover(c *ui.Context, key string, anchor ui.Element, spec popSpec) {
	if !anchor.Valid() {
		return
	}
	st := a.popoverState(key)
	anchor.Expanded(st.open)
	// Space, Return, and Down open from the keyboard while the anchor is
	// focused. They are taken here so the button's own click does not also
	// run, and so a menu with no current item can highlight its first row.
	// A pointer click still arrives as Clicked.
	anchor.HandleInput(func(ev ui.InputEvent) bool {
		if st.open || ev.Kind != ui.InputKeyDown || ev.Mods != 0 {
			return false
		}
		switch ev.Key {
		case ui.KeyEnter, ui.KeySpace, ui.KeyDown:
			st.pendingKey = true
			return true
		}
		return false
	})
	if !st.open && (anchor.Clicked() || st.pendingKey) {
		keyboard := st.pendingKey
		st.pendingKey = false
		// Only this window's menus. Closing another window's menu here
		// would pop the ledger of the window that is key now and leave
		// the owner's counter stuck.
		for k, other := range a.popovers {
			if k == key || other == nil || !other.open {
				continue
			}
			if other.owner != 0 && other.owner != a.windowToken {
				continue
			}
			a.releasePopover(other, false)
			other.open = false
			other.closing = false
		}
		a.pushFocusReturn()
		st.reset(c, keyboard)
		st.pushed = true
		a.notePopover(st)
		// A pointer click does not leave a ring, and KeepFocus leaves the
		// keyboard in the field underneath. The open menu still has to
		// take letters and arrows. A keyboard open already focused the
		// anchor with its ring, and a search field focuses itself next.
		if !keyboard && !spec.Search {
			anchor.Focus()
			a.showFocusRing(c, false)
		}
	}
	if st.open {
		a.paintPopover(c, anchor, st, spec)
	}
	// Settings menus are tab stops only with Full Keyboard Access. A click
	// still opens the menu without moving the keyboard.
	if a.buildingSettings {
		a.quietControl(anchor, radiusRow)
		a.noteSettingsStop(anchor, false)
	}
	st.wasOpen = st.open
}

func (a *App) releasePopover(st *popoverState, keyboard bool) {
	if st == nil || !st.pushed {
		return
	}
	st.pushed = false
	if st.owner != 0 && st.owner != a.windowToken {
		a.popOwnedLedger(st.owner, keyboard)
		return
	}
	a.popFocusReturnShown(keyboard)
}

func (a *App) dismissPopover(st *popoverState, now time.Time, composer bool, keyboard bool) {
	a.releasePopover(st, keyboard)
	st.focusComposer = composer
	if !st.focusMoved {
		st.focusMoved = true
	}
	if a.instant {
		st.open = false
		st.closing = false
		return
	}
	if !st.closing {
		st.closing = true
		st.animAt = now
	}
}

func (a *App) commitPopover(st *popoverState, spec popSpec, id string, keyboard bool) {
	a.releasePopover(st, keyboard)
	// Choosing a composer chip's item returns to the composer. Settings
	// and Add open another surface, which keeps the pre-menu place.
	if spec.ComposerFocus && id != modelMenuSettings && id != modelMenuAdd {
		a.abandonFocusReturn()
		a.focusComposer()
	}
	st.focusComposer = spec.ComposerFocus
	st.focusMoved = true
	st.open = false
	st.closing = false
	if spec.OnSelect != nil {
		spec.OnSelect(id)
	}
}

func (a *App) paintPopover(c *ui.Context, anchor ui.Element, st *popoverState, spec popSpec) {
	a.notePopover(st)
	w, h := c.Size()
	if !appWindowFocused(c) || (st.winW > 0 && (w != st.winW || h != st.winH)) {
		a.dismissPopover(st, c.Now(), false, false)
		if !st.open {
			return
		}
	}
	if st.closing {
		if a.popoverFade(c, st) <= 0 {
			st.open = false
			st.closing = false
			return
		}
	}
	items := spec.Items
	visible, empty := filterPopover(items, st.query)
	if empty {
		visible = []popEntry{{
			Kind:     popItem,
			Text:     "No models",
			Disabled: true,
		}}
	}
	sig := popSig(visible)
	if sig != st.sig {
		opening := !st.opened
		st.opened = true
		st.sig = sig
		st.currentID = checkedPopID(items)
		switch {
		case empty:
			st.hi = -1
		case st.currentID != "":
			// A current item keeps the highlight, whether the menu was
			// opened by the pointer or the keyboard.
			st.hi = popHighlight(visible, st.currentID)
		case opening && !st.keyboardOpen:
			// Pointer open, nothing checked: no highlight until hover or arrows.
			st.hi = -1
		default:
			st.hi = 0
		}
		st.keyboardOpen = false
		st.needScroll = st.hi >= 0
	}
	sel := selectable(visible)
	footerSel := selectable(spec.Footer)
	combined := append(append([]popEntry{}, sel...), footerSel...)
	if st.hi >= len(combined) {
		st.hi = len(combined) - 1
	}
	a.popoverKeys(c, anchor, st, combined, spec.Search)
	if !st.open {
		return
	}

	if st.activate {
		st.activate = false
		if st.hi >= 0 && st.hi < len(combined) {
			a.commitPopover(st, spec, combined[st.hi].ID, true)
			return
		}
	}
	if !st.open {
		return
	}

	limit, up := menuLimit(anchor.Bounds(), h)
	search := spec.Search
	chrome := menuPad * 2
	if search {
		chrome += menuSearchH + menuSepH
	}
	if len(spec.Footer) > 0 {
		chrome += menuSepH + menuRowH
	}
	listH := menuListHeight(visible)
	room := limit - chrome
	if room < menuRowH {
		room = menuRowH
	}
	viewH := listH
	if viewH > room {
		viewH = room
	}
	total := chrome + viewH
	width := menuWidth(menuContentWidth(append(append([]popEntry{}, items...), spec.Footer...)), anchor.Bounds().W)
	box := placeMenu(anchor.Bounds(), width, total, w, h, up, limit)
	st.page = menuPageStep(viewH)
	if st.needScroll && st.hi >= 0 && st.hi < len(sel) {
		top, ih := menuItemTop(visible, st.hi)
		st.scroll.Y = menuScrollOffset(top, ih, viewH, listH)
		st.needScroll = false
	} else if st.needScroll {
		st.needScroll = false
	}

	p := a.look
	var searchEl ui.Element
	var hotEl ui.Element
	ui.Overlay(c, func() {
		// Keyed so a tooltip overlay appearing or going does not change this
		// element's id. A keyboard-opened menu shows the anchor's tip; the
		// first press hides that tip, and an unkeyed backdrop would be a
		// new element by the time the button comes up, dropping the click.
		back := ui.Box(c.Key("menu-back-" + spec.Name)).Absolute().Left(0).Top(0).Right(0).Bottom(0)
		if back.Clicked() {
			a.dismissPopover(st, c.Now(), false, false)
		}
		a.popoverPointerMove(back, st, len(combined))
		panel := ui.Column(c.Key(spec.Name + "-panel")).Absolute().Left(box.X).Top(box.Y).Width(box.W).Height(box.H).
			Padding(menuPad).Gap(0).Radius(radiusMenu).Background(p.LayerOverlay).Clip().
			Role(ui.RoleMenu).Label(spec.Name)
		panel.Shadow(0, 8, 24, 0, p.ShadowMd)
		panel.Shadow(0, 0, 0, 0.5, p.LayerOverlayBorder)
		if !a.instant {
			panel.Opacity(a.popoverFade(c, st))
		}
		if panel.Clicked() {
			// Padding, a header, or a separator: the menu stays.
		}
		panel.Children(func() {
			if search {
				searchEl = a.popoverSearch(c, st, spec)
				a.popSep(c, modelMenuMark("sep", "search", "", false, false, false))
			}
			scroll := ui.Scroll(c).FillWidth().Height(viewH).TrackScroll(&st.scroll)
			scroll.Children(func() {
				hotEl = a.popoverRows(c, st, visible, 0, spec)
			})
			if len(spec.Footer) > 0 {
				a.popSep(c, spec.FooterSepMark)
				if h := a.popoverRows(c, st, spec.Footer, len(sel), spec); h.Valid() {
					hotEl = h
				}
			}
		})
		if !st.announced && spec.Name != "" {
			c.Announce(spec.Name)
			st.announced = true
		}
		if hotEl.Valid() {
			if searchEl.Valid() {
				searchEl.ActiveDescendant(hotEl)
			} else {
				anchor.ActiveDescendant(hotEl)
			}
		}
	})
	if !st.open {
		return
	}
}

func (a *App) popoverFade(c *ui.Context, st *popoverState) float32 {
	if a.instant {
		return 1
	}
	dur := menuFadeIn
	if st.closing {
		dur = menuFadeOut
	}
	if dur <= 0 {
		return 1
	}
	u := float32(c.Now().Sub(st.animAt)) / float32(dur)
	if u < 0 {
		u = 0
	}
	if u > 1 {
		u = 1
	}
	if u < 1 {
		c.After(16 * time.Millisecond)
	}
	if st.closing {
		return 1 - u
	}
	return u
}

func (a *App) popoverSearch(c *ui.Context, st *popoverState, spec popSpec) ui.Element {
	p := a.look
	prompt := spec.SearchPrompt
	if prompt == "" {
		prompt = "Search models"
	}
	row := ui.Row(c).Height(menuSearchH).FillWidth().Gap(6).Padding(0, 8).Radius(menuHighlightR).
		Background(p.FillQuaternary).AlignItems(ui.Center)
	var field ui.Element
	row.Children(func() {
		ui.Icon(c, iconSearch).Size(menuSearchIcon, menuSearchIcon).TextColor(p.TextTertiary).Shrink(0)
		field = ui.TextInputBase(c.Key(spec.Name+"-search"), &st.query).
			Placeholder(prompt).Label(prompt).FontSize(13).Grow(1).MinWidth(0).FocusRing(false)
		field.Border(0, ui.Transparent).Background(ui.Transparent)
		paintPlaceholder(c, field, p.TextTertiary)
		field.Focus()
		// The window treats Up as Edit Last Message. Registering it here
		// claims the key for the field when the editor does not want it,
		// which is a single-line search box. OnTextKey still takes the
		// arrows the editor would otherwise move the caret with.
		if field.Shortcut(0, ui.KeyUp) {
			st.hi = movePop(st.pageCount(), st.hi, -1)
			st.needScroll = true
		}
		if field.Shortcut(0, ui.KeyDown) {
			st.hi = movePop(st.pageCount(), st.hi, 1)
			st.needScroll = true
		}
		if field.Shortcut(0, ui.KeyPageUp) {
			st.hi = movePop(st.pageCount(), st.hi, -max(st.page, 1))
			st.needScroll = true
		}
		if field.Shortcut(0, ui.KeyPageDown) {
			st.hi = movePop(st.pageCount(), st.hi, max(st.page, 1))
			st.needScroll = true
		}
		field.OnTextKey(func(k ui.TextKey) bool {
			switch k.Key {
			case ui.KeyDown:
				st.hi = movePop(st.pageCount(), st.hi, 1)
				st.needScroll = true
				return true
			case ui.KeyUp:
				st.hi = movePop(st.pageCount(), st.hi, -1)
				st.needScroll = true
				return true
			case ui.KeyPageDown:
				st.hi = movePop(st.pageCount(), st.hi, max(st.page, 1))
				st.needScroll = true
				return true
			case ui.KeyPageUp:
				st.hi = movePop(st.pageCount(), st.hi, -max(st.page, 1))
				st.needScroll = true
				return true
			case ui.KeyEnter:
				st.activate = true
				return true
			case ui.KeyEscape:
				if strings.TrimSpace(k.Text()) != "" {
					k.SetText("")
					st.query = ""
					return true
				}
				a.dismissPopover(st, c.Now(), false, true)
				return true
			default:
				// Home, End, and typed characters stay in the field.
				// Space is text, so it is not delivered here.
				return false
			}
		})
	})
	return field
}

// pageCount is the selectable count remembered for the search field's
// keys, which run before the next frame builds the list. 0 until the
// first paint stores it; a Down before that still lands on the first row.
func (st *popoverState) pageCount() int { return st.selN }

func (a *App) popoverKeys(c *ui.Context, anchor ui.Element, st *popoverState, sel []popEntry, search bool) {
	st.selN = len(sel)
	n := len(sel)
	move := func(delta int) {
		if n == 0 {
			return
		}
		st.hi = movePop(n, st.hi, delta)
		st.needScroll = true
		st.typeBuf = ""
	}
	if anchor.Shortcut(0, ui.KeyDown) {
		move(1)
	}
	if anchor.Shortcut(0, ui.KeyUp) {
		move(-1)
	}
	if anchor.Shortcut(0, ui.KeyHome) {
		move(-n)
	}
	if anchor.Shortcut(0, ui.KeyEnd) {
		move(n)
	}
	step := max(st.page, 1)
	if anchor.Shortcut(0, ui.KeyPageDown) {
		move(step)
	}
	if anchor.Shortcut(0, ui.KeyPageUp) {
		move(-step)
	}
	if anchor.Shortcut(0, ui.KeyEnter) || anchor.Shortcut(0, ui.KeySpace) {
		st.activate = true
	}
	if anchor.Shortcut(0, ui.KeyEscape) {
		a.dismissPopover(st, c.Now(), false, true)
	}
	for k := ui.KeyA; k <= ui.KeyZ; k++ {
		if !anchor.Shortcut(0, k) {
			continue
		}
		now := c.Now()
		if now.Sub(st.typeAt) > menuTypeWindow {
			st.typeBuf = ""
		}
		st.typeBuf += string(rune('a' + (k - ui.KeyA)))
		st.typeAt = now
		if idx := typeaheadPop(sel, st.typeBuf); idx >= 0 {
			st.hi = idx
			st.needScroll = true
		}
	}
}

func (a *App) popoverPointerMove(back ui.Element, st *popoverState, n int) {
	// PointerPosition is local to the full-window back. Row boxes are
	// window DIPs. The first sample is only a baseline: opening keeps the
	// current item highlighted until the pointer actually moves (SPEC
	// §11.3.4). That sample has to be recorded even when the back does
	// not yet contain the pointer, or the move itself is mistaken for
	// the baseline and the highlight never follows.
	lx, ly, over := back.PointerPosition()
	box := back.Bounds()
	x, y := box.X+lx, box.Y+ly
	if !st.havePointer {
		st.px, st.py, st.havePointer = x, y, true
		return
	}
	if x != st.px || y != st.py {
		st.px, st.py = x, y
		st.pointerPending = true
	}
	if !over || !st.pointerPending {
		return
	}
	ready := false
	for _, g := range st.geoms {
		if g.r.W > 0 && g.r.H > 0 {
			ready = true
		}
		if g.sel < 0 || g.sel >= n {
			continue
		}
		if x >= g.r.X && x < g.r.X+g.r.W && y >= g.r.Y && y < g.r.Y+g.r.H {
			if st.hi != g.sel {
				st.hi = g.sel
			}
			st.pointerPending = false
			return
		}
	}
	// The row boxes can land a frame after the pointer. Keep the move
	// until a frame that has them.
	if ready {
		st.pointerPending = false
	}
}

func (a *App) popoverRows(c *ui.Context, st *popoverState, entries []popEntry, selBase int, spec popSpec) ui.Element {
	var hot ui.Element
	sel := selBase
	firstHeader := selBase == 0
	nextGeom := append([]popGeom{}, st.geoms...)
	if selBase == 0 {
		nextGeom = nil
	}
	var drawEntry func(e popEntry)
	drawEntry = func(e popEntry) {
		switch e.Kind {
		case popSep:
			a.popSep(c, popMark(e, false))
		default:
			hotRow := !e.Disabled && sel == st.hi
			row := a.popItem(c, st, spec, e, hotRow)
			if hotRow {
				hot = row
			}
			if !e.Disabled {
				nextGeom = append(nextGeom, popGeom{sel: sel, r: row.Bounds()})
				sel++
			}
		}
	}
	for i := 0; i < len(entries); {
		e := entries[i]
		if e.Kind != popHeader {
			drawEntry(e)
			i++
			continue
		}
		header := e
		i++
		var block []popEntry
		for i < len(entries) && entries[i].Kind != popHeader {
			block = append(block, entries[i])
			i++
		}
		group := ui.Column(c).FillWidth().Gap(0).Role(ui.RoleGroup).Label(header.Text)
		if !firstHeader {
			group.Margin(menuHeaderGap, 0, 0, 0)
		}
		firstHeader = false
		group.Children(func() {
			a.popHeader(c, header)
			for _, item := range block {
				drawEntry(item)
			}
		})
	}
	st.geoms = nextGeom
	return hot
}

func (a *App) popHeader(c *ui.Context, e popEntry) ui.Element {
	p := a.look
	row := ui.Row(c).Height(menuHeaderH).FillWidth().Gap(0).AlignItems(ui.Center).Role(ui.RoleNone)
	row.Children(func() {
		if mark := popMark(e, false); mark != "" {
			ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).PassThrough().Role(ui.RoleNone).Label(mark)
		}
		ui.Box(c).Width(menuLead).Shrink(0)
		slot := ui.Box(c).Width(menuCheck).Height(menuHeaderH).Center().Shrink(0)
		if e.Dot.A > 0 {
			slot.Children(func() {
				ui.Box(c).Size(menuDot, menuDot).Radius(menuDot / 2).Background(e.Dot)
			})
		}
		controlText(c, e.Text, 11, 14, 600, p.TextTertiary).Role(ui.RoleNone).Ellipsis("…").Grow(1).Shrink(1).MinWidth(0)
		ui.Box(c).Width(menuTrail).Shrink(0)
	})
	return row
}

func (a *App) popItem(c *ui.Context, st *popoverState, spec popSpec, e popEntry, hot bool) ui.Element {
	p := a.look
	row := ui.Row(c).Height(menuRowH).FillWidth().Gap(0).AlignItems(ui.Center).Radius(menuHighlightR)
	fg := p.TextPrimary
	sec := p.TextTertiary
	check := p.AccentDefault
	switch {
	case e.Disabled:
		fg, sec, check = p.TextTertiary, p.TextTertiary, p.TextTertiary
		row.Disabled(true)
	case hot:
		row.Background(p.AccentDefault)
		fg, sec, check = p.TextOnAccent, p.TextOnAccent, p.TextOnAccent
	}
	if e.Checkable {
		row.Role(ui.RoleMenuItemCheckBox).Checked(e.Checked)
	} else {
		row.Role(ui.RoleMenuItem)
	}
	if !e.Disabled {
		row.Cursor(ui.CursorPointer)
		if row.Clicked() {
			a.commitPopover(st, spec, e.ID, false)
		}
	}
	row.Children(func() {
		if mark := popMark(e, hot); mark != "" {
			ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).PassThrough().Role(ui.RoleNone).Label(mark)
		}
		ui.Box(c).Width(menuLead).Shrink(0)
		slot := ui.Box(c).Width(menuCheck).Height(menuRowH).Center().Shrink(0)
		if e.Checkable && e.Checked {
			slot.Children(func() {
				ui.Icon(c, iconCheck).Size(menuCheckGlyph, menuCheckGlyph).TextColor(check)
			})
		}
		if e.Icon != nil {
			ui.Icon(c, e.Icon).Size(menuIcon, menuIcon).TextColor(fg).Shrink(0)
			ui.Box(c).Width(menuIconGap).Shrink(0)
		}
		if e.Body != nil {
			e.Body(c, fg)
		} else {
			controlText(c, e.Text, 13, 0, 400, fg).Ellipsis("…").Grow(1).Shrink(1).MinWidth(0)
		}
		if e.Secondary != "" {
			controlText(c, e.Secondary, 13, 0, 400, sec).Shrink(0)
		}
		ui.Box(c).Width(menuTrail).Shrink(0)
	})
	if !e.Disabled && e.Text != "" && menuTextWidth(e.Text, 13, 400) > menuLabelRoom(e) {
		a.tip(c, row, e.Text)
	}
	return row
}

func menuLabelRoom(e popEntry) float32 {
	room := menuMaxW - menuTextX - menuTrail - menuPad
	if e.Icon != nil {
		room -= menuIcon + menuIconGap
	}
	if e.Secondary != "" {
		room -= 8 + menuTextWidth(e.Secondary, 13, 400)
	}
	if room < 8 {
		room = 8
	}
	return room
}

func (a *App) popSep(c *ui.Context, mark string) {
	p := a.look
	// FillWidth resolves to 100% of the content box and does not subtract
	// horizontal margin, so a margin only shifts the full-width line.
	// Spacers shorten it: line width = content width − 2×(inset − pad),
	// which is 10pt from each outer edge.
	inset := menuSepInset - menuPad
	ui.Row(c).FillWidth().Height(menuSepThick).MarginY(menuSepMargin).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Width(inset).Shrink(0)
		line := ui.Box(c).Height(menuSepThick).Grow(1).MinWidth(0).Background(p.SeparatorDefault).Role(ui.RoleNone)
		if mark != "" {
			line.Label(mark)
		}
		ui.Box(c).Width(inset).Shrink(0)
	})
}

func popMark(e popEntry, hot bool) string {
	if e.MarkKind == "" {
		return ""
	}
	return modelMenuMark(e.MarkKind, e.MarkProv, e.MarkModel, e.MarkOn, e.Checked, hot)
}

// menuContentWidth is the width the rows need, before the min/max clamp.
func menuContentWidth(entries []popEntry) float32 {
	var maxW float32
	for _, e := range entries {
		var w float32
		switch e.Kind {
		case popSep:
			continue
		case popHeader:
			w = menuTextX + menuTextWidth(e.Text, 11, 600) + menuTrail + menuPad
		default:
			w = menuTextX
			if e.Icon != nil {
				w += menuIcon + menuIconGap
			}
			label := e.Text
			w += menuTextWidth(label, 13, 400)
			if e.Secondary != "" {
				w += 8 + menuTextWidth(e.Secondary, 13, 400)
			}
			w += menuTrail + menuPad
		}
		if w > maxW {
			maxW = w
		}
	}
	return maxW
}

// menuWidth clamps content to max(anchor, 180) … 300.
func menuWidth(content, anchor float32) float32 {
	minW := anchor
	if minW < menuMinW {
		minW = menuMinW
	}
	if content < minW {
		content = minW
	}
	if content > menuMaxW {
		content = menuMaxW
	}
	return content
}

func menuTextWidth(s string, size float32, weight int) float32 {
	if s == "" {
		return 0
	}
	w, _ := ui.ShapeText(s, ui.Font{Size: size, Weight: weight}, 0).Size()
	return w
}

func menuListHeight(entries []popEntry) float32 {
	var h float32
	first := true
	for _, e := range entries {
		gap, eh := entryMetrics(e, first)
		if e.Kind == popHeader {
			first = false
		}
		h += gap + eh
	}
	return h
}

func entryMetrics(e popEntry, firstHeader bool) (gap, h float32) {
	switch e.Kind {
	case popHeader:
		if !firstHeader {
			gap = menuHeaderGap
		}
		return gap, menuHeaderH
	case popSep:
		return 0, menuSepH
	default:
		return 0, menuRowH
	}
}

func menuItemTop(entries []popEntry, selIndex int) (top, h float32) {
	seen := 0
	first := true
	var y float32
	for _, e := range entries {
		gap, eh := entryMetrics(e, first)
		if e.Kind == popHeader {
			first = false
		}
		if e.Kind == popItem && !e.Disabled {
			if seen == selIndex {
				return y + gap, eh
			}
			seen++
		}
		y += gap + eh
	}
	return 0, menuRowH
}

func menuScrollOffset(itemTop, itemH, viewH, contentH float32) float32 {
	if contentH <= viewH || viewH <= 0 {
		return 0
	}
	y := itemTop + itemH/2 - viewH/2
	maxY := contentH - viewH
	if y < 0 {
		y = 0
	}
	if y > maxY {
		y = maxY
	}
	return y
}

func menuPageStep(viewH float32) int {
	n := int(viewH / menuRowH)
	if n < 1 {
		return 1
	}
	return n
}

func menuLimit(anchor ui.Rect, winH float32) (limit float32, up bool) {
	above := anchor.Y - menuGap
	below := winH - (anchor.Y + anchor.H) - menuGap
	if above < 0 {
		above = 0
	}
	if below < 0 {
		below = 0
	}
	up = above > below
	avail := below
	if up {
		avail = above
	}
	limit = avail - menuWindowMargin
	if limit > menuMaxH {
		limit = menuMaxH
	}
	if limit < menuRowH+menuPad*2 {
		limit = menuRowH + menuPad*2
	}
	return limit, up
}

type menuBox struct {
	X, Y, W, H float32
	Up         bool
}

func placeMenu(anchor ui.Rect, width, height, winW, winH float32, up bool, limit float32) menuBox {
	if height > limit {
		height = limit
	}
	if width > winW-2*menuWindowMargin {
		width = winW - 2*menuWindowMargin
	}
	if width < 1 {
		width = menuMinW
	}
	// Left edges first. If that would sit closer than 8pt to the window's
	// right edge, align the right edges instead. Clamp to the 8pt margin
	// only when right-aligning still leaves the left edge outside it.
	x := anchor.X
	if x+width > winW-menuWindowMargin {
		x = anchor.X + anchor.W - width
	}
	if x < menuWindowMargin {
		x = menuWindowMargin
	}
	y := anchor.Y + anchor.H + menuGap
	if up {
		y = anchor.Y - menuGap - height
	}
	if y < menuWindowMargin {
		y = menuWindowMargin
	}
	if y+height > winH-menuWindowMargin {
		y = winH - menuWindowMargin - height
	}
	if y < menuWindowMargin {
		y = menuWindowMargin
	}
	return menuBox{X: x, Y: y, W: width, H: height, Up: up}
}

func filterPopover(entries []popEntry, query string) (out []popEntry, empty bool) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return entries, false
	}
	type grp struct {
		header int
		items  []int
	}
	var groups []grp
	cur := grp{header: -1}
	flush := func() {
		if cur.header >= 0 || len(cur.items) > 0 {
			groups = append(groups, cur)
		}
		cur = grp{header: -1}
	}
	for i, e := range entries {
		if e.Kind == popHeader {
			flush()
			cur.header = i
			continue
		}
		cur.items = append(cur.items, i)
	}
	flush()
	for _, g := range groups {
		headerHit := false
		if g.header >= 0 && strings.Contains(strings.ToLower(entries[g.header].Text), q) {
			headerHit = true
		}
		var keep []int
		if headerHit {
			keep = g.items
		} else {
			for _, i := range g.items {
				e := entries[i]
				if e.Kind != popItem {
					continue
				}
				blob := strings.ToLower(e.Text + "\n" + e.Group)
				if strings.Contains(blob, q) {
					keep = append(keep, i)
				}
			}
		}
		if len(keep) == 0 {
			continue
		}
		if g.header >= 0 {
			out = append(out, entries[g.header])
		}
		for _, i := range keep {
			out = append(out, entries[i])
		}
	}
	if len(selectable(out)) == 0 {
		return nil, true
	}
	return out, false
}

func selectable(entries []popEntry) []popEntry {
	var out []popEntry
	for _, e := range entries {
		if e.Kind == popItem && !e.Disabled && e.ID != "" {
			out = append(out, e)
		}
	}
	return out
}

func checkedPopID(entries []popEntry) string {
	for _, e := range entries {
		if e.Kind == popItem && e.Checked && !e.Disabled {
			return e.ID
		}
	}
	return ""
}

func popHighlight(entries []popEntry, currentID string) int {
	sel := selectable(entries)
	if len(sel) == 0 {
		return -1
	}
	if currentID != "" {
		for i, e := range sel {
			if e.ID == currentID {
				return i
			}
		}
	}
	return 0
}

func popSig(entries []popEntry) string {
	var b strings.Builder
	for _, e := range selectable(entries) {
		b.WriteString(e.ID)
		b.WriteByte('\n')
	}
	return b.String()
}

func movePop(n, hi, delta int) int {
	if n <= 0 {
		return -1
	}
	if hi < 0 {
		// Nothing highlighted yet. Down enters on the first enabled row,
		// Up on the last, as a macOS menu does.
		if delta > 0 {
			return min(delta-1, n-1)
		}
		return max(n+delta, 0)
	}
	hi += delta
	if hi < 0 {
		return 0
	}
	if hi >= n {
		return n - 1
	}
	return hi
}

func typeaheadPop(sel []popEntry, prefix string) int {
	p := strings.ToLower(prefix)
	if p == "" {
		return -1
	}
	for i, e := range sel {
		if strings.HasPrefix(strings.ToLower(e.Text), p) {
			return i
		}
	}
	return -1
}
