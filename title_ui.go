package main

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

func (a *App) syncWindowTitle() {
	title := "Cove"
	if conv := a.active(); conv != nil {
		if t := strings.TrimSpace(conv.Title); t != "" {
			title = t
		}
	}
	if title == a.windowTitle {
		return
	}
	a.windowTitle = title
	if a.win != nil {
		a.win.SetTitle(title)
	}
}

func (a *App) noteTitleClock(c *ui.Context) {
	now := c.Now()
	for id, start := range a.titleFails {
		elapsed := now.Sub(start)
		if elapsed < 0 {
			elapsed = 0
		}
		switch {
		case elapsed < motionTitleFail:
			c.After(motionTitleFail - elapsed)
		case elapsed < motionTitleFail+motionActions:
			c.AnimationFrame()
		default:
			delete(a.titleFails, id)
		}
	}
	for id, fade := range a.titleFades {
		if a.instant || a.reduceMotion {
			delete(a.titleFades, id)
			continue
		}
		elapsed := now.Sub(fade.start)
		if elapsed >= motionTitleFade {
			delete(a.titleFades, id)
			continue
		}
		c.AnimationFrame()
	}
}

func (a *App) titleFailAge(now time.Time, id string) (show bool, fade float32) {
	start, ok := a.titleFails[id]
	if !ok {
		return false, 0
	}
	elapsed := now.Sub(start)
	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed < motionTitleFail {
		return true, 0
	}
	back := elapsed - motionTitleFail
	if back >= motionActions {
		return false, 1
	}
	if motionActions <= 0 {
		return false, 1
	}
	return true, float32(back) / float32(motionActions)
}

func sideListIndex(rows []sideRow, convs []Conversation, id string) int {
	for i, row := range rows {
		if row.index < 0 || row.index >= len(convs) {
			continue
		}
		if convs[row.index].ID == id {
			return i
		}
	}
	return -1
}

func (a *App) sideMeta(c *ui.Context, conv *Conversation, when time.Time) {
	if conv == nil {
		return
	}
	p := a.look
	id := conv.ID
	if a.isStreaming(id) {
		ui.Text(c, "Generating").FontSize(11).TextColor(p.TextSecondary).Shrink(0).SingleLine()
		return
	}
	if a.titleDimmed(id, false) {
		a.titleSpinner(c, id)
		return
	}
	meta := sidebarWhen(c.Now(), when)
	if showsDraft(conv) {
		meta = "Draft"
	}
	show, u := a.titleFailAge(c.Now(), id)
	if show && u < 1 {
		if u <= 0 || meta == "" {
			a.titleWarn(c)
			return
		}
		hold := ui.Box(c).Shrink(0)
		hold.Children(func() {
			a.titleWarn(c).Opacity(1 - u)
			ui.Text(c, meta).FontSize(11).TextColor(p.TextSecondary).Shrink(0).SingleLine().Opacity(u).Absolute().Right(0).Top(0)
		})
		return
	}
	if meta != "" {
		ui.Text(c, meta).FontSize(11).TextColor(p.TextSecondary).Shrink(0).SingleLine()
	}
}

func (a *App) sideAccessLabel(i int, now time.Time) string {
	if i < 0 || i >= len(a.sideRows) {
		return ""
	}
	row := a.sideRows[i]
	if row.header != "" {
		return row.header
	}
	if row.index < 0 || row.index >= len(a.data.Conversations) {
		return ""
	}
	conv := &a.data.Conversations[row.index]
	title := sidebarTitle(conv)
	if a.titleDimmed(conv.ID, false) {
		return title + ", " + titleGenerating
	}
	when := conv.UpdatedAt
	if when.IsZero() {
		when = conv.CreatedAt
	}
	meta := sidebarWhen(now, when)
	if showsDraft(conv) {
		meta = "Draft"
	}
	if a.isStreaming(conv.ID) {
		meta = "Generating"
	}
	if meta == "" {
		return title
	}
	return title + ", " + meta
}

// fadingTitle draws text, or a cross-fade when this chat's title just
// changed. grow is the sidebar row, which shares its line with the time.
// The title bar passes false and fills the column width.
func (a *App) fadingTitle(c *ui.Context, id, text string, bar bool, size float32, weight int, color ui.Color, grow bool) {
	fade, ok := a.titleFades[id]
	use := ok && ((bar && fade.bar) || (!bar && fade.row))
	if !use || a.instant || a.reduceMotion {
		plainTitle(c, text, size, weight, color, grow)
		return
	}
	u := a.titleFadeU(c, fade.start)
	if u >= 1 {
		if bar {
			fade.bar = false
		} else {
			fade.row = false
		}
		if !fade.bar && !fade.row {
			delete(a.titleFades, id)
		} else {
			a.titleFades[id] = fade
		}
		plainTitle(c, text, size, weight, color, grow)
		return
	}
	box := ui.Box(c).MinWidth(0)
	if grow {
		box.Grow(1)
	} else {
		box.FillWidth()
	}
	box.Children(func() {
		old := plainTitle(c, fade.from, size, weight, color, grow).Opacity(1 - u).Role(ui.RoleNone)
		_ = old
		plainTitle(c, text, size, weight, color, false).FillWidth().Opacity(u).Absolute().Left(0).Right(0).Top(0)
	})
}

func (a *App) titleFadeU(c *ui.Context, start time.Time) float32 {
	elapsed := c.Now().Sub(start)
	if elapsed <= 0 {
		c.AnimationFrame()
		return 0
	}
	if elapsed >= motionTitleFade {
		return 1
	}
	c.AnimationFrame()
	return easeInOut(float32(elapsed) / float32(motionTitleFade))
}

func plainTitle(c *ui.Context, text string, size float32, weight int, color ui.Color, grow bool) ui.Element {
	t := ui.Text(c, text).FontSize(size).TextColor(color).SingleLine().Ellipsis("…").MinWidth(0)
	if weight > 0 {
		t.FontWeight(weight)
	}
	if grow {
		t.Grow(1)
	} else {
		t.FillWidth()
	}
	return t
}

func (a *App) titleSpinner(c *ui.Context, id string) {
	p := a.look
	spin := ui.Icon(c, iconTitleSpin).Size(10, 10).TextColor(p.TextTertiary).Label(titleGenerating).Shrink(0)
	spin.Rotate(spin.Loop("title-spin-"+id, motionTitleSpin, ui.Linear) * 360)
}

func (a *App) titleWarn(c *ui.Context) ui.Element {
	p := a.look
	icon := ui.Icon(c, iconAlert).Size(12, 12).TextColor(p.StatusWarning).Label(titleFailText).Shrink(0)
	return a.tip(c, icon, titleFailText)
}

// paintPreviewMenu draws the sidebar context menu for a headless shot.
// The real menu is the system menu and is not in the framebuffer.
func (a *App) paintPreviewMenu(c *ui.Context) {
	m := a.previewMenu
	if m == nil || len(m.Items) == 0 {
		return
	}
	p := a.look
	bg := ui.Color{R: 255, G: 255, B: 255, A: 250}
	if p.Dark {
		bg = ui.Color{R: 44, G: 44, B: 48, A: 250}
	}
	w := float32(188)
	panel := ui.Column(c).Absolute().Left(m.X).Top(m.Y).Width(w).Radius(8).Background(bg).Padding(4, 0).Label("context menu")
	panel.Border(0.5, p.LayerOverlayBorder)
	panel.Shadow(0, 10, 28, 0, p.ShadowMd)
	panel.Children(func() {
		for _, it := range m.Items {
			if it.Sep {
				ui.Box(c).Height(1).FillWidth().Margin(4, 10).Background(p.SeparatorDefault)
				continue
			}
			row := ui.Row(c).Height(22).FillWidth().Padding(0, 12).AlignItems(ui.Center).Radius(5)
			fg := p.TextPrimary
			if it.Disabled {
				fg = p.TextTertiary
			}
			if it.Hot && !it.Disabled {
				row.Background(ui.Color{R: 0, G: 122, B: 255, A: 255})
				fg = ui.Color{R: 255, G: 255, B: 255, A: 255}
			}
			label := it.Label
			row.Children(func() {
				ui.Text(c, label).FontSize(13).TextColor(fg).SingleLine()
			})
		}
	})
}
