package ui

import "unsafe"

// FocusToken is one window's focus state. It stays valid across frames,
// so a key handler can move the keyboard onto an element built last frame.
type FocusToken struct {
	rt *engine
}

// Token is stable for the window. Zero means there is no engine yet.
func (t FocusToken) Token() uintptr {
	if t.rt == nil {
		return 0
	}
	return uintptr(unsafe.Pointer(t.rt))
}

// Valid reports a token tied to a live engine.
func (t FocusToken) Valid() bool { return t.rt != nil }

// FocusToken returns the window being built.
func (c *Context) FocusToken() FocusToken {
	ctx := c.build()
	if ctx == nil || ctx.rt == nil {
		return FocusToken{}
	}
	return FocusToken{rt: ctx.rt}
}

// WindowFocused reports whether this window is the key window.
func (t FocusToken) WindowFocused() bool {
	if t.rt == nil {
		return true
	}
	return t.rt.windowFocus()
}

// FocusedID is the element that has the keyboard, or 0.
func (t FocusToken) FocusedID() uint64 {
	if t.rt == nil {
		return 0
	}
	return t.rt.focusedID()
}

// FocusShows reports the keyboard focus ring.
func (t FocusToken) FocusShows() bool {
	if t.rt == nil {
		return false
	}
	return t.rt.focusShows()
}

// FocusedSelectable reports a non-editable selection holding the keyboard.
func (t FocusToken) FocusedSelectable() bool {
	if t.rt == nil {
		return false
	}
	return t.rt.focusedSelectable()
}

// FocusIfLive moves the keyboard to id when that element was built this
// frame. visible keeps the keyboard focus ring.
func (t FocusToken) FocusIfLive(id uint64, visible bool) bool {
	if t.rt == nil {
		return false
	}
	return t.rt.focusLive(id, visible)
}

// FocusID moves the keyboard to id even when that element has not been
// built this frame yet.
func (t FocusToken) FocusID(id uint64, visible bool) {
	if t.rt == nil {
		return
	}
	t.rt.focusID(id, visible)
}

// SetFocusVisible shows or hides the keyboard focus ring.
func (t FocusToken) SetFocusVisible(on bool) {
	if t.rt == nil {
		return
	}
	t.rt.setFocusShows(on)
}

// SetSelectionHandoff installs the callback for an unmodified printable
// key typed while a non-editable selection has the keyboard. The callback
// moves the keyboard onto a text field. The key is not inserted here.
func (t FocusToken) SetSelectionHandoff(fn func() bool) {
	if t.rt == nil {
		return
	}
	t.rt.setSelectionHandoff(fn)
}

// OmitTab keeps the element clickable and out of the tab order.
func (e Element) OmitTab() Element {
	n := e.lookup()
	if n == nil {
		return e
	}
	n.flags &^= flagFocusable
	return e
}

// DismissedByKey reports that Escape closed the dialog this frame.
// A click on the backdrop leaves it false.
func (e Element) DismissedByKey() bool {
	n := e.lookup()
	return n != nil && n.dismissedByKey
}
