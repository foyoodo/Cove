package main

import "github.com/egoist/mygo/ui"

// sidebarShown is the width the sidebar takes this frame.
// The user's width stays in 200–360. A narrow window clamps it again so
// the card keeps 512pt: shown = clamp(user, 200, window − 520). Widening
// the window returns to the user's width. Below the 720pt floor the
// sidebar stays at 200 and the card gives up the extra.
func sidebarShown(user, winW float32) float32 {
	user = clampSidebar(user)
	hi := winW - sidebarReserve
	if hi < sidebarMin {
		return sidebarMin
	}
	if user > hi {
		return hi
	}
	return user
}

func clampSidebar(w float32) float32 {
	if w < sidebarMin {
		return sidebarMin
	}
	if w > sidebarMax {
		return sidebarMax
	}
	return w
}

// sidebarDragged moves the visible edge by dx. Widening never throws away
// a preference the window is too narrow to show. Narrowing follows the
// pointer, so the card moves on this drag's frame.
func sidebarDragged(user, winW, dx float32) float32 {
	shown := sidebarShown(user, winW)
	next := shown + dx
	if next > shown && next < user {
		next = user
	}
	return clampSidebar(next)
}

// paneInnerWidth is the content card's inner width: the window, minus the
// sidebar, the 8pt right inset, and the 1pt border on each side.
func paneInnerWidth(winW, side float32) float32 {
	w := winW - side - 8 - 2
	if w < 1 {
		return 1
	}
	return w
}

// sidebarGrip is the invisible 8pt strip centered on the card's left edge
// (4pt each side), from below the title bar to the bottom of the window.
// SPEC §4: no line and no highlight. It is an absolute sibling of the
// panes, committed above them, so it takes the pointer over the card, the
// composer veil, the card shadow, and the title-bar window-drag rows where
// they meet the edge. The title bar itself stays a window drag.
func (a *App) sidebarGrip(c *ui.Context) {
	winW, winH := c.Size()
	if winH <= titleBarHeight || winW < 1 {
		return
	}
	shown := sidebarShown(a.sidebarWidth, winW)
	grip := ui.Box(c.Key("sidebar-resize")).Absolute().
		Left(shown - sidebarGrip/2).Width(sidebarGrip).
		Top(titleBarHeight).Height(winH - titleBarHeight).
		Cursor(ui.CursorResizeEW).Label("sidebar resize")
	// Hover asks for a frame when the pointer enters the strip, so the
	// resize cursor is applied on that move.
	grip.Hovered()
	if dx, _, held := grip.Dragged(); held && dx != 0 {
		a.sidebarWidth = sidebarDragged(a.sidebarWidth, winW, dx)
	}
	if grip.DoubleClicked() {
		a.sidebarWidth = sidebarW
	}
	if _, _, ended := grip.DragEnded(); ended || grip.DoubleClicked() {
		a.commitSidebarWidth()
	}
}

// commitSidebarWidth writes cove.sidebar.width when a drag ends or a
// double-click resets it. The width is not saved on the frames in between.
func (a *App) commitSidebarWidth() {
	a.sidebarWidth = clampSidebar(a.sidebarWidth)
	if a.data.SidebarWidth == a.sidebarWidth {
		return
	}
	a.data.SidebarWidth = a.sidebarWidth
	a.save()
}
