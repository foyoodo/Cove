package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSidebarShownMath(t *testing.T) {
	cases := []struct {
		user, win, want float32
	}{
		{240, 1120, 240},
		{360, 1120, 360},
		{200, 2000, 200},
		{100, 1120, 200},
		{500, 1120, 360},
		{360, 720, 200},
		{240, 720, 200},
		{360, 800, 280},
		{300, 800, 280},
		{260, 800, 260},
		{240, 700, 200},
		{360, 760, 240},
	}
	for _, tc := range cases {
		if got := sidebarShown(tc.user, tc.win); got != tc.want {
			t.Errorf("shown(%v, %v) = %v, want %v", tc.user, tc.win, got, tc.want)
		}
	}
	if got := sidebarDragged(360, 800, -20); got != 260 {
		t.Errorf("narrow from a clamped 360 = %v, want 260", got)
	}
	if got := sidebarDragged(360, 800, 20); got != 360 {
		t.Errorf("widen at the window cap dropped the preference: %v", got)
	}
	if got := sidebarDragged(240, 1120, 40); got != 280 {
		t.Errorf("widen = %v, want 280", got)
	}
	if got := sidebarDragged(240, 1120, 400); got != 360 {
		t.Errorf("clamp max = %v", got)
	}
	if got := sidebarDragged(240, 1120, -400); got != 200 {
		t.Errorf("clamp min = %v", got)
	}
}

func TestSidebarResizeDrag(t *testing.T) {
	a := newTestApp(t)
	a.sidebarWidth = sidebarW
	a.data.SidebarWidth = sidebarW
	const winW, winH = 1120, 760
	tt := ui.NewTester(a.view, winW, winH)
	card := mustFind(t, tt, "content card")
	if !near(card.X, sidebarW) {
		t.Fatalf("card starts at %.1f, want %v", card.X, sidebarW)
	}
	grip := mustFind(t, tt, "sidebar resize")
	assertGrip(t, grip, card.X, winH)
	if grip.W < 1 || grip.H < 1 {
		t.Fatalf("cursor rect missing: %v", grip)
	}

	// The title bar is outside the strip, including with the Mac inset
	// reported. A drag there moves the window and leaves the width alone.
	tt.SetTitleBar(ui.TitleBar{Height: 54, Left: 80})
	grip = mustFind(t, tt, "sidebar resize")
	assertGrip(t, grip, card.X, winH)
	tt.Move(card.X, 20)
	if tt.Cursor() == ui.CursorResizeEW {
		t.Fatal("title bar shows the resize cursor")
	}
	before := a.sidebarWidth
	tt.Press(card.X, 24)
	tt.Move(card.X+80, 24)
	tt.Release(card.X+80, 24)
	if a.sidebarWidth != before || !near(mustFind(t, tt, "content card").X, before) {
		t.Fatalf("title-bar drag resized the sidebar to %.1f", a.sidebarWidth)
	}

	// Just under the title bar the strip wins over the window-drag rows.
	// Mouse down, drag, and up change the width on this frame and save it
	// when the button comes up.
	yTop := float32(titleBarHeight) + 4
	tt.Move(card.X, yTop)
	if tt.Cursor() != ui.CursorResizeEW {
		t.Fatalf("cursor under the title bar %v, want resizeLeftRight", tt.Cursor())
	}
	tt.Press(card.X, yTop)
	tt.Move(card.X+36, yTop)
	if !near(a.sidebarWidth, sidebarW+36) || !near(mustFind(t, tt, "content card").X, sidebarW+36) {
		t.Fatalf("live width %.1f card %.1f", a.sidebarWidth, mustFind(t, tt, "content card").X)
	}
	if a.data.SidebarWidth != sidebarW {
		t.Fatalf("saved during the drag: %v", a.data.SidebarWidth)
	}
	tt.Release(card.X+36, yTop)
	if a.data.SidebarWidth != sidebarW+36 {
		t.Fatalf("mouse-up saved %v, want %v", a.data.SidebarWidth, sidebarW+36)
	}
	a.sidebarWidth = sidebarW
	a.data.SidebarWidth = sidebarW
	tt.Frame()

	// The right half of the strip is over the card and, at the bottom, over
	// the composer veil. It still takes the drag.
	grip = mustFind(t, tt, "sidebar resize")
	x := grip.X + grip.W - 2
	y := float32(winH) - 24
	tt.Press(x, y)
	tt.Move(x, y)
	if tt.Cursor() != ui.CursorResizeEW {
		t.Fatalf("cursor over the veil %v", tt.Cursor())
	}
	tt.Move(x+48, y)
	card = mustFind(t, tt, "content card")
	if !near(card.X, sidebarW+48) || !near(a.sidebarWidth, sidebarW+48) {
		t.Fatalf("live width %.1f card %.1f, want %v", a.sidebarWidth, card.X, sidebarW+48)
	}
	if a.data.SidebarWidth != sidebarW {
		t.Fatalf("saved during the drag: %v", a.data.SidebarWidth)
	}
	if tt.FramePending() {
		t.Fatal("resize drag asked for another frame")
	}
	tt.Release(x+48, y)
	if a.data.SidebarWidth != sidebarW+48 {
		t.Fatalf("mouse-up saved %v, want %v", a.data.SidebarWidth, sidebarW+48)
	}
	if tt.Cursor() != ui.CursorResizeEW {
		t.Fatalf("cursor after release %v", tt.Cursor())
	}

	// Left half, over the sidebar list.
	grip = mustFind(t, tt, "sidebar resize")
	tt.Press(grip.X+2, 200)
	tt.Move(grip.X+2+32, 200)
	if !near(a.sidebarWidth, sidebarW+80) {
		t.Fatalf("left-half drag %.1f", a.sidebarWidth)
	}
	tt.Release(grip.X+2+32, 200)

	restarted := openApp(a.path, newMemKeys())
	if restarted.sidebarWidth != a.sidebarWidth {
		t.Fatalf("restart width %v, want %v", restarted.sidebarWidth, a.sidebarWidth)
	}
	tr := ui.NewTester(restarted.view, winW, winH)
	if !near(mustFind(t, tr, "content card").X, a.sidebarWidth) {
		t.Fatalf("restart card at %.1f", mustFind(t, tr, "content card").X)
	}
}

func TestSidebarResizeClamps(t *testing.T) {
	a := newTestApp(t)
	a.sidebarWidth = sidebarW
	a.data.SidebarWidth = sidebarW
	tt := ui.NewTester(a.view, 1120, 760)
	grip := mustFind(t, tt, "sidebar resize")
	x, y := grip.X+grip.W/2, grip.Y+80
	tt.Press(x, y)
	tt.Move(x+500, y)
	tt.Release(x+500, y)
	if a.sidebarWidth != sidebarMax || a.data.SidebarWidth != sidebarMax {
		t.Fatalf("max clamp width %v saved %v", a.sidebarWidth, a.data.SidebarWidth)
	}
	if !near(mustFind(t, tt, "content card").X, sidebarMax) {
		t.Fatalf("card at max %.1f", mustFind(t, tt, "content card").X)
	}

	grip = mustFind(t, tt, "sidebar resize")
	x = grip.X + grip.W/2
	tt.Press(x, y)
	tt.Move(x-500, y)
	tt.Release(x-500, y)
	if a.sidebarWidth != sidebarMin || !near(mustFind(t, tt, "content card").X, sidebarMin) {
		t.Fatalf("min clamp width %v card %.1f", a.sidebarWidth, mustFind(t, tt, "content card").X)
	}
}

func TestSidebarResizeNarrowWindow(t *testing.T) {
	a := newTestApp(t)
	a.sidebarWidth = sidebarMax
	a.data.SidebarWidth = sidebarMax
	tt := ui.NewTester(a.view, 720, 520)
	card := mustFind(t, tt, "content card")
	if !near(card.X, sidebarMin) {
		t.Fatalf("720 sidebar %.1f, want %v", card.X, sidebarMin)
	}
	if !near(card.W, 512) {
		t.Fatalf("720 card width %.1f, want 512", card.W)
	}
	if a.sidebarWidth != sidebarMax {
		t.Fatalf("narrow window rewrote the preference to %v", a.sidebarWidth)
	}
	tt.SetSize(1120, 760)
	card = mustFind(t, tt, "content card")
	if !near(card.X, sidebarMax) {
		t.Fatalf("widened sidebar %.1f, want %v", card.X, sidebarMax)
	}
	// 800pt leaves room for 280, so a 300 preference shows 280 and a 260 one shows 260.
	a.sidebarWidth = 300
	tt.SetSize(800, 600)
	if !near(mustFind(t, tt, "content card").X, 280) {
		t.Fatalf("800pt window shows %.1f, want 280", mustFind(t, tt, "content card").X)
	}
	a.sidebarWidth = 260
	tt.Frame()
	if !near(mustFind(t, tt, "content card").X, 260) {
		t.Fatalf("800pt window shows %.1f, want 260", mustFind(t, tt, "content card").X)
	}
}

func TestSidebarResizeDoubleClick(t *testing.T) {
	a := newTestApp(t)
	a.sidebarWidth = 320
	a.data.SidebarWidth = 320
	tt := ui.NewTester(a.view, 1120, 760)
	grip := mustFind(t, tt, "sidebar resize")
	x, y := grip.X+grip.W/2, grip.Y+100
	tt.ClickAt(x, y)
	tt.ClickAt(x, y)
	if a.sidebarWidth != sidebarW || a.data.SidebarWidth != sidebarW {
		t.Fatalf("double-click width %v saved %v", a.sidebarWidth, a.data.SidebarWidth)
	}
	if !near(mustFind(t, tt, "content card").X, sidebarW) {
		t.Fatalf("card after reset %.1f", mustFind(t, tt, "content card").X)
	}

	// On a 720pt window the edge stays at 200, but the preference returns
	// to 240, so the next widen does too.
	a.sidebarWidth = sidebarMax
	a.data.SidebarWidth = sidebarMax
	tt.SetSize(720, 520)
	grip = mustFind(t, tt, "sidebar resize")
	tt.ClickAt(grip.X+grip.W/2, grip.Y+40)
	tt.ClickAt(grip.X+grip.W/2, grip.Y+40)
	if a.sidebarWidth != sidebarW {
		t.Fatalf("reset on a narrow window stored %v", a.sidebarWidth)
	}
	if !near(mustFind(t, tt, "content card").X, sidebarMin) {
		t.Fatalf("narrow edge after reset %.1f", mustFind(t, tt, "content card").X)
	}
	tt.SetSize(1120, 760)
	if !near(mustFind(t, tt, "content card").X, sidebarW) {
		t.Fatalf("widen after reset %.1f", mustFind(t, tt, "content card").X)
	}
}

func TestSidebarResizeFrames(t *testing.T) {
	dir := filepath.Join("/opt/cursor/artifacts", "sidebar-resize")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		a := smallLongApp(t)
		a.sidebarWidth = sidebarW
		a.data.SidebarWidth = sidebarW
		tt := ui.NewTester(a.view, 1120, 760)
		if dark {
			tt.SetDark(true)
		}
		saveShot(t, dir, "drag-wide-"+mode+"-0-before.png", tt.Image())
		grip := mustFind(t, tt, "sidebar resize")
		x, y := grip.X+grip.W/2, float32(280)
		tt.Press(x, y)
		for i, dx := range []float32{40, 80, 120} {
			tt.Move(x+dx, y)
			saveShot(t, dir, fmt.Sprintf("drag-wide-%s-%d.png", mode, i+1), tt.Image())
			if !near(mustFind(t, tt, "content card").X, sidebarW+dx) {
				t.Fatalf("%s frame %d card %.1f", mode, i+1, mustFind(t, tt, "content card").X)
			}
		}
		tt.Release(x+120, y)
		saveShot(t, dir, "drag-wide-"+mode+"-4-after.png", tt.Image())

		b := smallLongApp(t)
		b.sidebarWidth = sidebarMax
		tt = ui.NewTester(b.view, 720, 520)
		if dark {
			tt.SetDark(true)
		}
		card := mustFind(t, tt, "content card")
		if !near(card.X, sidebarMin) || !near(card.W, 512) {
			t.Fatalf("%s 720 card %v", mode, card)
		}
		saveShot(t, dir, "window-720-"+mode+".png", tt.Image())
		// The long code line and the third table column sit past the 512pt
		// card. Both are horizontal scrollers, so a wheel toward the end
		// brings the cut-off tail into the card. Positive dx is that direction.
		code := mustFind(t, tt, "code block")
		codeRect := image.Rect(int(code.X)+8, int(code.Y)+28, int(code.X+code.W)-8, int(code.Y+code.H)-8)
		codeBefore := cropRGBA(tt.Image(), codeRect)
		scrollFarRight(tt, code.X+48, code.Y+code.H-16)
		if !imageChanged(codeBefore, cropRGBA(tt.Image(), codeRect)) {
			t.Fatalf("%s code block did not scroll horizontally", mode)
		}
		table := mustFind(t, tt, "table scroller")
		scrollFarRight(tt, table.X+24, table.Y+20)
		head := mustFind(t, tt, "Frees the goroutine")
		if head.W < 100 || head.X < table.X || head.X+head.W > table.X+table.W+1 {
			t.Fatalf("%s table did not scroll the last column into the card: head %v table %v", mode, head, table)
		}
		saveShot(t, dir, "window-720-scrolled-"+mode+".png", tt.Image())
		tt.SetSize(1120, 760)
		if !near(mustFind(t, tt, "content card").X, sidebarMax) {
			t.Fatalf("%s restored %.1f", mode, mustFind(t, tt, "content card").X)
		}
		saveShot(t, dir, "window-wide-restored-"+mode+".png", tt.Image())
	}

	// The strip paints nothing. 228 is window background; 237 is inside the
	// 8pt strip and still left of the card, so a highlight or a rule would
	// show up between them.
	a := newTestApp(t)
	tt := ui.NewTester(a.view, 1120, 760)
	img := tt.Image()
	if d := colorDist(img, 228, 200, 237, 200); d > 12 {
		t.Fatalf("resize strip painted a mark, delta %d", d)
	}
}

func assertGrip(t *testing.T, grip ui.Rect, edge float32, winH int) {
	t.Helper()
	if !near(grip.W, sidebarGrip) {
		t.Fatalf("grip width %.2f", grip.W)
	}
	if !near(grip.X+grip.W/2, edge) {
		t.Fatalf("grip center %.2f, edge %.2f", grip.X+grip.W/2, edge)
	}
	if !near(grip.Y, titleBarHeight) {
		t.Fatalf("grip top %.2f", grip.Y)
	}
	if !near(grip.H, float32(winH)-titleBarHeight) {
		t.Fatalf("grip height %.2f", grip.H)
	}
}

func scrollFarRight(tt *ui.Tester, x, y float32) {
	for i := 0; i < 6; i++ {
		tt.Scroll(x, y, 400, 0)
	}
}

func imageChanged(a, b *image.RGBA) bool {
	if a.Bounds() != b.Bounds() {
		return true
	}
	return string(a.Pix) != string(b.Pix)
}

func TestSidebarResizeReport(t *testing.T) {
	section := `
Sidebar resize
The drag target left with the separator. v0.7 draws the sidebar, the title bar, and the window as one background and does not paint a divider. The old 1pt line was what a drag grabbed. SPEC §4 defines the replacement: an invisible 8pt strip centered on the main card's left edge, 4pt on each side, the full height of the window below the title bar. Hover and drag show resizeLeftRight. The strip paints no line and no highlight. The range is 200–360, default 240. Mouse-up writes cove.sidebar.width. Double-click sets 240. The width on screen is clamp(userWidth, 200, windowWidth − 520), so the card stays at least 512. At 720 the sidebar is 200 and the card is 512. Widening the window returns to the saved width. The card follows the pointer on the drag frame, with no animation.

The strip is an absolute sibling of the panes and is committed above them, so it wins hit testing over the card, the composer veil, the card shadow, and the title-bar window-drag rows along that edge. A press in the title bar still drags the window.

Resize stopped sticking in 59706c4 (Ship the v0.7 revision 2 design with the queued layout fixes). That commit still called ui.Split, and then assigned sidebarFor at the start of every frame, so a drag was replaced by 200 or 240 before layout. The self-drawn base and the inset card are not that commit. 8fb8e81 (Restore the gorex inset card and warm window background) kept ui.Split and only painted over the split's 1pt line. The split widget itself was removed in 9c83320, which is the commit after the pin, when the 8pt strip took its place.

Frames, light and dark: artifacts/sidebar-resize/drag-wide-{light,dark}-0-before.png through drag-wide-{light,dark}-4-after.png are a drag at y=280 from 240 to 360. window-720-{light,dark}.png is the 720pt window with a 360 preference, sidebar 200 and card 512. window-wide-restored-{light,dark}.png is that saved width after the window widens.

Tests: TestSidebarResizeDrag, TestSidebarResizeClamps, TestSidebarResizeNarrowWindow, TestSidebarResizeDoubleClick, TestSidebarResizeFrames.
`
	path := "/opt/cursor/artifacts/perf/after.txt"
	raw, err := os.ReadFile(path)
	body := ""
	if err == nil {
		body = string(raw)
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	const heading = "\nSidebar resize\n"
	if i := strings.Index(body, heading); i >= 0 {
		rest := body[i+len(heading):]
		cut := len(rest)
		if j := strings.Index(rest, "\nShortcuts\n"); j >= 0 && j < cut {
			cut = j
		}
		body = body[:i] + section + rest[cut:]
	} else {
		body += section
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func near(a, b float32) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 0.6
}

func colorDist(img image.Image, x0, y0, x1, y1 int) int {
	c0 := img.At(x0, y0)
	c1 := img.At(x1, y1)
	return chanDist(c0, c1)
}

func chanDist(a, b color.Color) int {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	return absInt(int(ar>>8)-int(br>>8)) + absInt(int(ag>>8)-int(bg>>8)) + absInt(int(ab>>8)-int(bb>>8))
}
