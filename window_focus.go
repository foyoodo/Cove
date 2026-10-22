package main

import (
	"unicode/utf8"
	"unsafe"

	"github.com/egoist/mygo/ui"
)

// appWindowFocused reads the surface's keyboard focus. On darwin that bit
// is set from SurfaceFocus, which windowDidBecomeKey: sends when this
// window becomes key (NSWindowDidBecomeKeyNotification): ⌘Tab back to
// Cove, activating the app, or a Dock click that makes the main window
// key. The Settings window is a different surface and does not share it.
//
//go:linkname contextRuntime github.com/egoist/mygo/ui.(*Context).runtime
func contextRuntime(c *ui.Context) unsafe.Pointer

//go:linkname engineWindowFocus github.com/egoist/mygo/ui.(*engine).windowFocus
func engineWindowFocus(rt unsafe.Pointer) bool

func appWindowFocused(c *ui.Context) bool {
	rt := contextRuntime(c)
	if rt == nil {
		return true
	}
	return engineWindowFocus(rt)
}

const (
	activateComposer = iota
	activateEdit
	activateFind
	activateRename
	activateSheet
	activateRounds
)

// trackWindowFocus remembers who had the keyboard when the main window
// resigns key, and on the next time it becomes key moves the composer
// back there unless an edit was in progress or a click is delivering
// its own target.
func (a *App) trackWindowFocus(c *ui.Context) {
	focused := appWindowFocused(c)
	if !a.seenWindowFocus {
		a.seenWindowFocus = true
		a.windowWasFocused = focused
		return
	}
	if !focused && a.windowWasFocused {
		a.rememberActivateFocus()
	}
	if focused && !a.windowWasFocused {
		a.applyWindowActivate()
		a.pendingActivate = false
	} else if focused {
		a.pendingActivate = false
	}
	a.windowWasFocused = focused
}

// rememberActivateFocus snapshots the editing surface that should keep
// the keyboard, and relies on the composer caret already recorded while
// the window was key.
func (a *App) rememberActivateFocus() {
	a.activateKeep = activateComposer
	switch {
	case a.edit.convID != "":
		a.activateKeep = activateEdit
	case a.finder.open && a.finder.focused:
		a.activateKeep = activateFind
	case a.renameOpen:
		a.activateKeep = activateRename
	case a.modalOpen || a.confirmKind != "" || a.requestOpen || a.acksOpen:
		a.activateKeep = activateSheet
	case a.roundsScope != "":
		a.activateKeep = activateRounds
	}
}

// applyWindowActivate runs on the rising edge. A click that made the
// window key already chose a target, so it is left alone. An edit,
// the find field, rename, a sheet or confirm, or the Max context
// number keeps the focus it had. Anything else, including a selection
// in the message list, returns to the composer without moving the
// list or clearing that selection.
//
// An in-window popover is not one of those edits. It closes when the
// window resigns key, and becoming key again does not open it.
func (a *App) applyWindowActivate() {
	if a.activateByClick {
		a.activateByClick = false
		return
	}
	switch a.activateKeep {
	case activateEdit, activateFind, activateRename, activateSheet, activateRounds:
		return
	}
	a.restoreComposerCaret = true
	a.focusComposer()
}

// noteComposerCaret records the composer's selection while the window
// is key, so reactivation can put the caret back. With no record, the
// caret goes to the end of the draft.
func (a *App) noteComposerCaret() {
	if !a.windowWasFocused || !a.composerField.Valid() || !a.composerField.Focused() {
		return
	}
	anchor, caret := a.composerField.TextAnchorCaret()
	a.composerAnchor, a.composerCaretPos = anchor, caret
	a.composerCaretSet = true
}

func composerEnd(draft string) (int, int) {
	n := utf8.RuneCountInString(draft)
	return n, n
}
