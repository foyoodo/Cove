package main

import (
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	xdraw "golang.org/x/image/draw"
)

// TestNewSessionShots writes the New Session screens next to the design
// boards. Each implementation board uses the design cell rectangles at
// half scale, so a later uniform scale keeps the aspect ratio.
func TestNewSessionShots(t *testing.T) {
	dir := testArtifactDir(t, "new-session")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	designDir := "/cursor/stores/self/design-v07-r4/qa"
	requireDesign(t, designDir)
	for _, dark := range []bool{false, true} {
		mode := modeName(dark)
		composer := nsComposerBoard(t, dark)
		divider := nsDividerBoard(t, dark)
		auto := nsAutoBoard(t, dark)
		empty := nsEmptyBoard(t, dark)
		settings := nsSettingsBoard(t, dark)
		min := nsMinBoard(t, dark)
		boards := []struct {
			name string
			img  image.Image
			dw   int
			dh   int
		}{
			{"composer", composer, 1572, 370},
			{"divider", divider, 1120, 1620},
			{"auto", auto, 2252, 820},
			{"empty", empty, 1120, 1620},
			{"settings", settings, 3384, 820},
			{"min", min, 2184, 580},
		}
		for _, b := range boards {
			if b.img.Bounds().Dx() != b.dw || b.img.Bounds().Dy() != b.dh {
				t.Fatalf("%s board %dx%d, want %dx%d", b.name, b.img.Bounds().Dx(), b.img.Bounds().Dy(), b.dw, b.dh)
			}
			writePNG(t, filepath.Join(dir, b.name+"-"+mode+".png"), b.img)
			design := mustPNG(t, filepath.Join(designDir, "new-session-"+b.name+"-"+mode+".png"))
			writePNG(t, filepath.Join(dir, "compare-"+b.name+"-"+mode+".png"), comparePreserve(design, b.img))
		}
	}
}

func nsComposerBoard(t *testing.T, dark bool) image.Image {
	t.Helper()
	enabled := nsHoverTip(t, revisionChat(t), 1120, 760, dark, "⇧⌘N", "")
	emptyApp := newTestApp(t)
	emptyApp.newChat()
	disabled := nsHoverTip(t, emptyApp, 1120, 760, dark, "Send a message first", "")
	streaming := revisionChat(t)
	streaming.streams = map[string]*convStream{streaming.active().ID: {cancel: func() {}}}
	stream := nsHoverTip(t, streaming, 1120, 760, dark, "Available when the reply finishes", "")

	board := newBoard(1572, 370)
	blit(board, composerCrop(t, enabled, 780, 135, 8), 0, 60)
	blit(board, composerCrop(t, disabled, 780, 135, 8), 0, 235)
	blit(board, composerCrop(t, stream, 780, 310, 12), 792, 60)
	return board
}

func nsDividerBoard(t *testing.T, dark bool) image.Image {
	t.Helper()
	a := revisionChat(t)
	a.active().Breaks = []SessionBreak{{
		ID: "mid", Kind: sessionManual, CreatedAt: time.Now(), After: 2,
	}}
	a.shotPinBreak = true
	tt := nsFrame(t, a, 1120, 760, dark)
	if !tt.HasText("New Session") {
		t.Fatalf("divider: %q", tt.Texts())
	}
	if tt.HasText("Remove New Session") {
		t.Fatal("a divider with later messages offered remove")
	}
	label, ok := tt.Find("New Session")
	if !ok {
		t.Fatal("divider label missing")
	}
	board := newBoard(1120, 1620)
	blit(board, tt.Image(), 0, 60)
	blit(board, hstrip(tt.Image(), label, 105), 0, 860)
	return board
}

func nsAutoBoard(t *testing.T, dark bool) image.Image {
	t.Helper()
	full := nsAutoWindow(t, dark, 1120, 760)
	narrow := nsAutoWindow(t, dark, 720, 760)
	assertInside(t, narrow, 720, "New Session · auto (20-round limit)")
	board := newBoard(2252, 820)
	blit(board, full.Image(), 0, 60)
	blit(board, narrow.Image(), 1132, 60)
	return board
}

func nsAutoWindow(t *testing.T, dark bool, w, h int) *ui.Tester {
	t.Helper()
	a := revisionChat(t)
	a.active().Breaks = []SessionBreak{{
		ID: "auto", Kind: sessionAuto, Limit: 20, CreatedAt: time.Now(), After: 2,
	}}
	a.shotPinBreak = true
	tt := nsFrame(t, a, w, h, dark)
	if !tt.HasText("New Session · auto (20-round limit)") {
		t.Fatalf("auto %dx%d: %q", w, h, tt.Texts())
	}
	return tt
}

func nsEmptyBoard(t *testing.T, dark bool) image.Image {
	t.Helper()
	a := revisionChat(t)
	conv := a.active()
	conv.Breaks = []SessionBreak{{
		ID: "tail", Kind: sessionManual, CreatedAt: time.Now(), After: len(conv.Messages),
	}}
	tt := nsFrame(t, a, 1120, 760, dark)
	label, ok := tt.Find("New Session")
	if !ok {
		t.Fatalf("empty divider: %q", tt.Texts())
	}
	remove, ok := tt.Find("Remove New Session")
	if !ok {
		t.Fatal("trailing divider has no remove button")
	}
	before := cropFixed(tt.Image(), int(remove.X)-2, int(remove.Y)-2, int(remove.W)+4, int(remove.H)+4)
	tt.Move(label.X+label.W/2, label.Y+label.H/2)
	tt.Frame()
	remove, ok = tt.Find("Remove New Session")
	if !ok {
		t.Fatal("remove button left on hover")
	}
	after := cropFixed(tt.Image(), int(remove.X)-2, int(remove.Y)-2, int(remove.W)+4, int(remove.H)+4)
	if imageSame(before, after) {
		t.Fatal("hover did not draw the remove control")
	}
	label, _ = tt.Find("New Session")
	board := newBoard(1120, 1620)
	blit(board, tt.Image(), 0, 60)
	blit(board, hstrip(tt.Image(), label, 52), 0, 860)
	return board
}

func nsSettingsBoard(t *testing.T, dark bool) image.Image {
	t.Helper()
	unlimited := nsGeneral(t, dark, 0, 780, 760)
	if unlimited.HasText("rounds") {
		t.Fatal("unlimited general row showed the number")
	}
	limited := nsGeneral(t, dark, 20, 780, 760)
	if !limited.HasText("Limit to") || !limited.HasText("rounds") || !limited.HasText("Rounds per session") {
		t.Fatalf("general limit: %q", limited.Texts())
	}
	sheet := nsSheet(t, dark, 20, 1120, 760)
	board := newBoard(3384, 820)
	blit(board, unlimited.Image(), 0, 60)
	blit(board, limited.Image(), 1132, 60)
	blit(board, sheet.Image(), 2264, 60)
	return board
}

func nsMinBoard(t *testing.T, dark bool) image.Image {
	t.Helper()
	dir := testArtifactDir(t, "new-session")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mode := modeName(dark)
	main := nsMinMain(t, dark)
	general := nsGeneral(t, dark, 1, 720, 520)
	if !general.HasText("round") || !general.HasText("Limit to") {
		t.Fatalf("min general: %q", general.Texts())
	}
	assertInside(t, general, 720, "Max context", "Limit to", "round")
	sheet := nsSheet(t, dark, 100, 720, 520)
	assertInside(t, sheet, 720, "Max context", "Limit to", "rounds")
	writePNG(t, filepath.Join(dir, "min-main-"+mode+".png"), main.Image())
	writePNG(t, filepath.Join(dir, "min-general-"+mode+".png"), general.Image())
	writePNG(t, filepath.Join(dir, "min-sheet-"+mode+".png"), sheet.Image())
	board := newBoard(2184, 580)
	blit(board, main.Image(), 0, 60)
	blit(board, general.Image(), 732, 60)
	blit(board, sheet.Image(), 1464, 60)
	return board
}

func nsMinMain(t *testing.T, dark bool) *ui.Tester {
	t.Helper()
	a := revisionChat(t)
	a.active().Breaks = []SessionBreak{{
		ID: "one", Kind: sessionAuto, Limit: 1, CreatedAt: time.Now(), After: 2,
	}}
	a.shotPinBreak = true
	tt := nsFrame(t, a, 720, 520, dark)
	if !tt.HasText("New Session · auto (1-round limit)") {
		t.Fatalf("min main: %q", tt.Texts())
	}
	assertInside(t, tt, 720, "New Session · auto (1-round limit)")
	return tt
}

func nsGeneral(t *testing.T, dark bool, rounds, w, h int) *ui.Tester {
	t.Helper()
	a := revisionChat(t)
	a.data.MaxRounds = rounds
	a.settingsOpen = true
	a.settingsSection = "general"
	tt := nsFrame(t, a, w, h, dark)
	nsReveal(t, a, tt, "Max context")
	return tt
}

func nsSheet(t *testing.T, dark bool, rounds, w, h int) *ui.Tester {
	t.Helper()
	a := revisionChat(t)
	a.active().MaxRounds = rounds
	a.openSettingsTab(0)
	if a.formRounds != rounds {
		t.Fatalf("sheet opened at %d, want %d", a.formRounds, rounds)
	}
	tt := nsFrame(t, a, w, h, dark)
	nsReveal(t, a, tt, "Max context")
	if !tt.HasText("Limit to") || !tt.HasText("Question + answer = 1 round") {
		t.Fatalf("sheet %dx%d: %q", w, h, tt.Texts())
	}
	return tt
}

type nsTip struct {
	img image.Image
	btn ui.Rect
	tip ui.Rect
}

func nsHoverTip(t *testing.T, a *App, w, h int, dark bool, want, forbid string) nsTip {
	t.Helper()
	tt := nsFrame(t, a, w, h, dark)
	btn, ok := tt.Find("New Session")
	if !ok {
		t.Fatalf("button missing: %q", tt.Texts())
	}
	tt.Move(btn.X+btn.W/2, btn.Y+btn.H/2)
	tt.Advance(700 * time.Millisecond)
	tt.Frame()
	if want != "" && !tt.HasText(want) {
		t.Fatalf("tooltip missing %q: %q", want, tt.Texts())
	}
	if forbid != "" && tt.HasText(forbid) {
		t.Fatalf("tooltip showed %q: %q", forbid, tt.Texts())
	}
	btn, ok = tt.Find("New Session")
	if !ok {
		t.Fatal("button left after the tooltip")
	}
	tip, ok := tt.Find(want)
	if !ok {
		t.Fatalf("tooltip rect missing %q", want)
	}
	return nsTip{img: tt.Image(), btn: btn, tip: tip}
}

func nsFrame(t *testing.T, a *App, w, h int, dark bool) *ui.Tester {
	t.Helper()
	tt := ui.NewTester(a.view, w, h)
	if dark {
		tt.SetDark(true)
	} else {
		tt.Frame()
	}
	return tt
}

func nsReveal(t *testing.T, a *App, tt *ui.Tester, label string) {
	t.Helper()
	if _, ok := tt.Find(label); ok {
		return
	}
	for _, y := range []float32{120, 280, 480, 760, 1200} {
		a.formScroll.Y = y
		tt.Frame()
		if _, ok := tt.Find(label); ok {
			return
		}
	}
	t.Fatalf("could not reveal %s: %q", label, tt.Texts())
}

// composerCrop frames the New Session button and its tooltip. The button
// sits pad px above the bottom edge, and the crop starts left of the button
// so the model control and the send key stay in frame.
func composerCrop(t *testing.T, shot nsTip, w, h, pad int) image.Image {
	t.Helper()
	x := int(shot.btn.X) - 340
	y := int(shot.btn.Y+shot.btn.H) + pad - h
	img := cropFixed(shot.img, x, y, w, h)
	ox, oy := clampedOrigin(shot.img.Bounds(), x, y, w, h)
	if !rectIn(ox, oy, w, h, shot.btn) || !rectIn(ox, oy, w, h, shot.tip) {
		t.Fatalf("composer crop %d,%d %dx%d missed button %.0f,%.0f or tip %.0f,%.0f %.0fx%.0f",
			ox, oy, w, h, shot.btn.X, shot.btn.Y, shot.tip.X, shot.tip.Y, shot.tip.W, shot.tip.H)
	}
	return img
}

func rectIn(ox, oy, w, h int, r ui.Rect) bool {
	return r.X >= float32(ox)-1 && r.Y >= float32(oy)-1 &&
		r.X+r.W <= float32(ox+w)+1 && r.Y+r.H <= float32(oy+h)+1
}

func clampedOrigin(b image.Rectangle, x, y, w, h int) (int, int) {
	if x < b.Min.X {
		x = b.Min.X
	}
	if y < b.Min.Y {
		y = b.Min.Y
	}
	if x+w > b.Max.X {
		x = b.Max.X - w
	}
	if y+h > b.Max.Y {
		y = b.Max.Y - h
	}
	if w > b.Dx() {
		x = b.Min.X
	}
	if h > b.Dy() {
		y = b.Min.Y
	}
	return x, y
}

func imageSame(a, b image.Image) bool {
	ab, bb := a.Bounds(), b.Bounds()
	if ab.Dx() != bb.Dx() || ab.Dy() != bb.Dy() {
		return false
	}
	for y := 0; y < ab.Dy(); y++ {
		for x := 0; x < ab.Dx(); x++ {
			ar, ag, abv, aa := a.At(ab.Min.X+x, ab.Min.Y+y).RGBA()
			br, bg, bbv, ba := b.At(bb.Min.X+x, bb.Min.Y+y).RGBA()
			if ar != br || ag != bg || abv != bbv || aa != ba {
				return false
			}
		}
	}
	return true
}

func hstrip(src image.Image, label ui.Rect, h int) image.Image {
	y := int(label.Y+label.H/2) - h/2
	return cropFixed(src, 0, y, src.Bounds().Dx(), h)
}

func cropFixed(src image.Image, x, y, w, h int) image.Image {
	b := src.Bounds()
	if x < b.Min.X {
		x = b.Min.X
	}
	if y < b.Min.Y {
		y = b.Min.Y
	}
	if x+w > b.Max.X {
		x = b.Max.X - w
	}
	if y+h > b.Max.Y {
		y = b.Max.Y - h
	}
	if w > b.Dx() {
		w = b.Dx()
		x = b.Min.X
	}
	if h > b.Dy() {
		h = b.Dy()
		y = b.Min.Y
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), src, image.Pt(x, y), draw.Src)
	return dst
}

func newBoard(w, h int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.NRGBA{R: 30, G: 30, B: 32, A: 255}), image.Point{}, draw.Src)
	return dst
}

func blit(dst *image.NRGBA, src image.Image, x, y int) {
	b := src.Bounds()
	draw.Draw(dst, image.Rect(x, y, x+b.Dx(), y+b.Dy()), src, b.Min, draw.Src)
}

// comparePreserve places the design board beside the implementation.
// The implementation is doubled first (the design files are 2×), then both
// sides scale by the same factor so neither is stretched.
func comparePreserve(design, impl image.Image) image.Image {
	ib := impl.Bounds()
	doubled := image.NewNRGBA(image.Rect(0, 0, ib.Dx()*2, ib.Dy()*2))
	xdraw.NearestNeighbor.Scale(doubled, doubled.Bounds(), impl, ib, draw.Src, nil)
	db := design.Bounds()
	h := db.Dy()
	if h > 980 {
		h = 980
	}
	dw := int(float64(db.Dx()) * float64(h) / float64(db.Dy()))
	iw := int(float64(doubled.Bounds().Dx()) * float64(h) / float64(doubled.Bounds().Dy()))
	left := image.NewNRGBA(image.Rect(0, 0, dw, h))
	right := image.NewNRGBA(image.Rect(0, 0, iw, h))
	xdraw.ApproxBiLinear.Scale(left, left.Bounds(), design, db, draw.Over, nil)
	xdraw.ApproxBiLinear.Scale(right, right.Bounds(), doubled, doubled.Bounds(), draw.Over, nil)
	const gap = 16
	out := image.NewNRGBA(image.Rect(0, 0, dw+gap+iw, h))
	draw.Draw(out, out.Bounds(), image.NewUniform(color.NRGBA{R: 255, G: 255, B: 255, A: 255}), image.Point{}, draw.Src)
	draw.Draw(out, left.Bounds(), left, image.Point{}, draw.Src)
	draw.Draw(out, image.Rect(dw+gap, 0, dw+gap+iw, h), right, image.Point{}, draw.Src)
	return out
}
