package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cove/themes"
	"cove/tools/inkgap"

	"github.com/egoist/mygo/ui"
)

// inkScale is the pixels per point the open measurement pass renders at.
// The 4× pass is the historical one. The 2× pass, what a Retina Mac
// paints, sets this to 2 for the duration of its own frames.
var inkScale = 4

func TestSymbolShiftDependsOnSize(t *testing.T) {
	// A return arrow and a command mark do not share one correction, and
	// the return arrow's own correction changes with the size.
	a := opticalOf("↵", 11, 0, 400)
	b := opticalOf("↵", 13, 0, 400)
	c := opticalOf("⌘", 11, 0, 400)
	if a.dy == b.dy && a.dy == c.dy {
		t.Fatalf("symbol shifts collapsed to one constant %+.3f", a.dy)
	}
}

func TestSplitControlLine(t *testing.T) {
	got := splitControlLine("↵ send · ⇧↵ newline")
	want := []controlPart{{"↵", true, false}, {"send", false, false}, {"·", false, true}, {"⇧↵", true, false}, {"newline", false, false}}
	if len(got) != len(want) {
		t.Fatalf("got %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("part %d %#v want %#v", i, got[i], want[i])
		}
	}
	if controlGap(got[0], got[1]) != 3 || controlGap(got[3], got[4]) != 3 {
		t.Fatal("symbol/text gap")
	}
	if controlGap(got[1], got[2]) != 5 || controlGap(got[2], got[3]) != 5 {
		t.Fatal("separator gap")
	}
	if controlGap(controlPart{"⇧", true, false}, controlPart{"↵", true, false}) != 1 {
		t.Fatal("symbol/symbol gap")
	}
	for _, tc := range []struct {
		in   string
		want []controlPart
	}{
		{"⌘L", []controlPart{{"⌘L", true, false}}},
		{"⇧⌘N", []controlPart{{"⇧⌘N", true, false}}},
		{"Next ⌘G", []controlPart{{"Next", false, false}, {"⌘G", true, false}}},
		{"Previous ⇧⌘G", []controlPart{{"Previous", false, false}, {"⇧⌘G", true, false}}},
		{"⌘N  New Chat", []controlPart{{"⌘N", true, false}, {"New Chat", false, false}}},
		{"↵ Return", []controlPart{{"↵", true, false}, {"Return", false, false}}},
		{"⌘,", []controlPart{{"⌘,", true, false}}},
		{"⌘,  Settings", []controlPart{{"⌘,", true, false}, {"Settings", false, false}}},
		{"⌥⌘,", []controlPart{{"⌥⌘,", true, false}}},
		{"⌘. to stop", []controlPart{{"⌘.", true, false}, {"to stop", false, false}}},
		{"·  ⌘. to stop", []controlPart{{"·", false, true}, {"⌘.", true, false}, {"to stop", false, false}}},
	} {
		got := splitControlLine(tc.in)
		if len(got) != len(tc.want) {
			t.Fatalf("%q got %#v", tc.in, got)
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Fatalf("%q part %d %#v want %#v", tc.in, i, got[i], tc.want[i])
			}
		}
	}
	// SPEC §8: spacer = target − right bearing before − left bearing after.
	arrow := opticalOf("↵", 11, 13, 400)
	word := opticalOf("send", 11, 13, 400)
	spacer := controlSpacer(controlPart{"↵", true, false}, controlPart{"send", false, false}, 11, 13, 400)
	formula := float32(3) - (arrow.elemW - arrow.right) - word.left
	if spacer != formula {
		t.Fatalf("↵ to send spacer %.4f want %.4f", spacer, formula)
	}
	label := opticalOf("Add to Chat", 13, 0, 400)
	cmd := opticalOf("⌘", 12, 0, 400)
	lead := quoteLeadGap()
	leadWant := float32(8) - (label.elemW - label.right) - cmd.left
	if lead != leadWant {
		t.Fatalf("label to ⌘ spacer %.4f want %.4f", lead, leadWant)
	}
	l := opticalOf("L", 12, 0, 400)
	pair := inkSpacer("⌘", "L", 12, 0, 400, 1)
	pairWant := float32(1) - (cmd.elemW - cmd.right) - l.left
	if pair != pairWant {
		t.Fatalf("⌘ to L spacer %.4f want %.4f", pair, pairWant)
	}
	// ⌘→N and ⌘→G read 0.75 and 1.25 at 4×, one pixel. The bearings
	// themselves put the ink gap on 1.
	for _, next := range []string{"N", "G"} {
		a := opticalOf("⌘", 11, 0, 400)
		b := opticalOf(next, 11, 0, 400)
		got := specSpacer("⌘", 11, 0, next, 11, 0, 400, 1) + (a.elemW - a.right) + b.left
		if got < 0.999 || got > 1.001 {
			t.Fatalf("analytic ⌘ to %s gap %.4f", next, got)
		}
	}
}

// checkHintCoverage reads the half-coverage gaps in a delivered composer
// bitmap. On that file the hint is the run after the wide hole; gap 0 is
// ↵→send, 4 is send→·, 5 is ·→⇧, 6 is ⇧→↵, 7 is ↵→newline.
func checkHintCoverage(t *testing.T, img image.Image, scale float64) {
	t.Helper()
	gaps := inkgap.FocusGaps(img, scale)
	if len(gaps) < 8 {
		t.Fatalf("hint coverage gaps %d: %v", len(gaps), gaps)
	}
	want := []struct {
		i    int
		name string
		v    float64
	}{
		{0, "↵ to send", 3},
		{4, "send to ·", 5},
		{5, "· to ⇧", 5},
		{6, "⇧ to ↵", 1},
		{7, "↵ to newline", 3},
	}
	for _, w := range want {
		off := gaps[w.i] - w.v
		if off < -0.25 || off > 0.25 {
			t.Errorf("scale %.0f %s gap %.3f off %+.3f gaps %v", scale, w.name, gaps[w.i], off, gaps)
		}
	}
	side := gaps[4] - gaps[5]
	if side < -0.25 || side > 0.25 {
		t.Errorf("scale %.0f · sides %.3f and %.3f diff %+.3f", scale, gaps[4], gaps[5], side)
	}
}

// TestComposerBackgroundMatchesFinal9 locks the window gradient in the
// 2× composer-light-before crop. The top 14 rows are that gradient, above
// the field, and they were byte-identical to final9 at 48cf8fe.
func TestComposerBackgroundMatchesFinal9(t *testing.T) {
	controlOpticsOff = true
	defer func() { controlOpticsOff = false }()
	a := chipApp(t, "grok-4.7", "high")
	a.data.Theme = themes.ThemeWarm
	a.data.Appearance = appearanceLight
	tt := ui.NewTester(a.view, 1120, 760)
	tt.SetScale(2)
	tt.Frame()
	img := copyRGBA(tt.Image())
	comp := mustFind(t, tt, "composer")
	crop := dipRect(comp.X-8, comp.Y+comp.H-80, comp.W+16, 88, 2, img.Bounds())
	shot := cropImage(img, crop)
	f, err := os.Open("testdata/final9-composer-light-before-bg.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ref, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	rb := ref.Bounds()
	const rows = 14
	if shot.Bounds().Dx() != rb.Dx() || rb.Dy() != rows {
		t.Fatalf("crop %dx%d ref %dx%d", shot.Bounds().Dx(), shot.Bounds().Dy(), rb.Dx(), rb.Dy())
	}
	diff, maxD := 0, 0
	for y := 0; y < rows; y++ {
		for x := 0; x < rb.Dx(); x++ {
			g := shot.RGBAAt(shot.Bounds().Min.X+x, shot.Bounds().Min.Y+y)
			r := color.RGBAModel.Convert(ref.At(rb.Min.X+x, rb.Min.Y+y)).(color.RGBA)
			d := absInt(int(g.R)-int(r.R)) + absInt(int(g.G)-int(r.G)) + absInt(int(g.B)-int(r.B))
			if d > 0 {
				diff++
				if d > maxD {
					maxD = d
				}
			}
		}
	}
	if diff != 0 {
		t.Errorf("composer background %d pixels differ from final9, max channel delta %d", diff, maxD)
	}
}

func TestRetinaGaps(t *testing.T) {
	for _, dark := range []bool{false, true} {
		rows := measureRetinaGaps(t, dark)
		if len(rows) < 8 {
			t.Fatalf("2x rows %d", len(rows))
		}
		for _, line := range rows {
			if strings.Contains(line, "FAIL") {
				t.Errorf("%s", line)
			}
			if !strings.HasPrefix(line, "2x ") {
				t.Errorf("2x row missing prefix: %s", line)
			}
		}
	}
}

func TestControlCapHeight(t *testing.T) {
	var lines []string
	defer func() {
		dir := testArtifactDir(t, "chip-center")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Error(err)
			return
		}
		body := strings.Join(lines, "\n") + "\n"
		if err := os.WriteFile(filepath.Join(dir, "measurements.txt"), []byte(body), 0o644); err != nil {
			t.Error(err)
		}
	}()
	lines = append(lines, analyticKeyGap("N"), analyticKeyGap("G"))
	for _, dark := range []bool{false, true} {
		lines = append(lines, measureRetinaGaps(t, dark)...)
	}
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		lines = append(lines, measureComposer(t, mode, dark, "grok-4.7", "digits")...)
		lines = append(lines, measureComposer(t, mode, dark, "gpt-oss", "lower")...)
		lines = append(lines, measureComposer(t, mode, dark, "深度求索-v4", "han")...)
		lines = append(lines, measureSettings(t, mode, dark)...)
		lines = append(lines, measureFind(t, mode, dark)...)
		lines = append(lines, measureEdit(t, mode, dark)...)
		lines = append(lines, measureBarShortcut(t, mode, dark)...)
		lines = append(lines, measureEmptyShortcuts(t, mode, dark)...)
		lines = append(lines, measureModHint(t, mode, dark)...)
		lines = append(lines, measureStopHint(t, mode, dark)...)
	}
	twoX := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "2x ") {
			twoX++
		}
		if strings.Contains(line, "FAIL") {
			t.Errorf("%s", line)
		}
	}
	if twoX == 0 {
		t.Fatal("no 2x gap rows")
	}
}

func TestControlCapHeightShots(t *testing.T) {
	dir := testArtifactDir(t, "chip-center")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	shots := []struct {
		name, model, section string
		dark                 bool
		theme                string
		kind                 string
	}{
		{"composer-light", "grok-4.7", "", false, themes.ThemeWarm, "composer"},
		{"composer-dark", "grok-4.7", "", true, themes.ThemeWarm, "composer"},
		{"composer-tide", "grok-4.7", "", false, themes.ThemeTide, "composer"},
		{"composer-cjk", "深度求索-v4", "", true, themes.ThemeWarm, "composer"},
		{"segmented-light", "grok-4.7", "appearance", false, themes.ThemeWarm, "segment"},
		{"segmented-dark", "grok-4.7", "appearance", true, themes.ThemeWarm, "segment"},
		{"sendwith-light", "grok-4.7", "general", false, themes.ThemeWarm, "sendwith"},
		{"sendwith-dark", "grok-4.7", "general", true, themes.ThemeWarm, "sendwith"},
		{"popup-light", "grok-4.7", "general", false, themes.ThemeWarm, "popup"},
		{"popup-dark", "grok-4.7", "general", true, themes.ThemeWarm, "popup"},
		{"shortcuts-light", "grok-4.7", "shortcuts", false, themes.ThemeWarm, "shortcuts"},
		{"shortcuts-dark", "grok-4.7", "shortcuts", true, themes.ThemeWarm, "shortcuts"},
		{"shortcuts-add-light", "grok-4.7", "shortcuts", false, themes.ThemeWarm, "addchat"},
		{"shortcuts-add-dark", "grok-4.7", "shortcuts", true, themes.ThemeWarm, "addchat"},
	}
	for _, sh := range shots {
		for _, before := range []bool{true, false} {
			tag := "after"
			if before {
				tag = "before"
				controlOpticsOff = true
			} else {
				controlOpticsOff = false
			}
			a := chipApp(t, sh.model, "high")
			a.data.Theme = sh.theme
			if sh.dark {
				a.data.Appearance = appearanceDark
			} else {
				a.data.Appearance = appearanceLight
			}
			if sh.section != "" {
				a.settingsOpen = true
				a.settingsSection = sh.section
			}
			tt := ui.NewTester(a.view, 1120, 760)
			tt.SetScale(2)
			tt.SetDark(sh.dark)
			tt.Frame()
			img := copyRGBA(tt.Image())
			var crop image.Rectangle
			switch sh.kind {
			case "composer":
				comp := mustFind(t, tt, "composer")
				crop = dipRect(comp.X-8, comp.Y+comp.H-80, comp.W+16, 88, 2, img.Bounds())
			case "segment":
				seg := mustFind(t, tt, "Appearance mode")
				crop = dipRect(seg.X-24, seg.Y-16, seg.W+48, seg.H+32, 2, img.Bounds())
			case "sendwith":
				seg := mustFind(t, tt, "Send with control")
				crop = dipRect(seg.X-40, seg.Y-20, seg.W+80, seg.H+40, 2, img.Bounds())
			case "popup":
				seg := mustFind(t, tt, "Default model menu")
				crop = dipRect(seg.X-80, seg.Y-28, seg.W+160, seg.H+56, 2, img.Bounds())
			case "addchat":
				label := mustFind(t, tt, "Add to Chat")
				cap := mustFind(t, tt, "keycap L")
				crop = dipRect(label.X-16, label.Y-14, cap.X+cap.W+20-(label.X-16), label.H+28, 2, img.Bounds())
			default:
				label := mustFind(t, tt, "Search Chats")
				cap := mustFind(t, tt, "keycap ⌘")
				crop = dipRect(label.X-12, cap.Y-40, cap.X+72-(label.X-12), 300, 2, img.Bounds())
			}
			shot := cropImage(img, crop)
			if sh.kind == "composer" && !before && (sh.name == "composer-light" || sh.name == "composer-dark") {
				checkHintCoverage(t, shot, 2)
			}
			writePNG(t, filepath.Join(dir, sh.name+"-"+tag+".png"), shot)
		}
	}
	controlOpticsOff = false
}

func measureComposer(t *testing.T, mode string, dark bool, model, kind string) []string {
	t.Helper()
	a := chipApp(t, model, "high")
	if dark {
		a.data.Appearance = appearanceDark
	} else {
		a.data.Appearance = appearanceLight
	}
	tt := ui.NewTester(a.view, 1120, 760)
	tt.SetScale(float32(inkScale))
	tt.SetDark(dark)
	tt.Frame()
	img := tt.Image()
	var lines []string
	chip := mustFind(t, tt, "Model "+model)
	text := mustFind(t, tt, model)
	bg := sampleBG(img, chip, inkScale)
	center := chip.Y + chip.H/2
	switch kind {
	case "digits":
		lay := ui.ShapeText(model, ui.Font{Size: 12, Weight: 500}, 0)
		x := lay.CaretAt(ui.TextCaretPosition{Index: strings.IndexRune(model, '4')}).X
		sub := text
		sub.X += x
		sub.W -= x
		lines = append(lines, reportInk(t, fmt.Sprintf("model-chip %s %s digits", model, mode), img, sub, bg, center, inkScale))
	case "han":
		lines = append(lines, reportInk(t, fmt.Sprintf("model-chip %s %s han", model, mode), img, text, bg, center, inkScale))
	default:
		// The line box is what the chip centers. Cap-height centering shifts
		// the glyphs inside it, so the box's own center is the visual
		// center minus that shift.
		box := opticalOf("H", 12, 0, 500)
		capY := text.Y + text.H/2 - box.dy
		off := capY - center
		status := "PASS"
		if off < -0.25 || off > 0.25 {
			status = "FAIL"
		}
		lines = append(lines, fmt.Sprintf("model-chip %s %s cap-line center=%.2f control=%.2f offset=%+.3f %s", model, mode, capY, center, off, status))
	}
	lines = append(lines, reportZone(t, fmt.Sprintf("model-chip %s %s dot", model, mode), img, ui.Rect{X: chip.X, Y: chip.Y, W: text.X - chip.X, H: chip.H}, bg, center, inkScale))
	lines = append(lines, reportZone(t, fmt.Sprintf("model-chip %s %s chevron", model, mode), img, ui.Rect{X: text.X + text.W, Y: chip.Y, W: chip.X + chip.W - (text.X + text.W), H: chip.H}, bg, center, inkScale))
	highChip := mustFind(t, tt, "Reasoning: High")
	high := mustFind(t, tt, "High")
	hbg := sampleBG(img, highChip, inkScale)
	lay := ui.ShapeText("High", ui.Font{Size: 12, Weight: 400}, 0)
	hw := lay.CaretAt(ui.TextCaretPosition{Index: 1}).X
	cap := high
	cap.W = hw
	lines = append(lines, reportInk(t, "reasoning-chip "+mode+" H", img, cap, hbg, highChip.Y+highChip.H/2, inkScale))
	btn := mustFind(t, tt, "New Session")
	lines = append(lines, reportZone(t, "new-session "+mode+" icon", img, btn, sampleBG(img, btn, inkScale), btn.Y+btn.H/2, inkScale))
	hint := mustFind(t, tt, "↵ send · ⇧↵ newline")
	hbg = sampleBG(img, hint, inkScale)
	hc := hint.Y + hint.H/2
	for _, sym := range []string{"↵", "⇧"} {
		r := mustFind(t, tt, sym)
		lines = append(lines, reportInk(t, "hint "+mode+" "+sym, img, r, hbg, hc, inkScale))
	}
	sendLine := mustFind(t, tt, "send")
	off := (sendLine.Y + sendLine.H/2) - hc
	status := "PASS"
	if off < -0.25 || off > 0.25 {
		status = "FAIL"
	}
	lines = append(lines, fmt.Sprintf("hint %s send cap-line center=%.2f control=%.2f offset=%+.3f %s", mode, sendLine.Y+sendLine.H/2, hc, off, status))
	ret := mustFind(t, tt, "↵")
	send := mustFind(t, tt, "send")
	dot := mustFind(t, tt, "·")
	shift := mustFind(t, tt, "⇧")
	lines = append(lines, reportInkGapRects(t, "hint "+mode+" ↵ to send", img, ret, send, hbg, 3, inkScale))
	sendDot := reportInkGapRects(t, "hint "+mode+" send to ·", img, send, dot, hbg, 5, inkScale)
	dotShift := reportInkGapRects(t, "hint "+mode+" · to ⇧", img, dot, shift, hbg, 5, inkScale)
	lines = append(lines, sendDot, dotShift, separatorBalance(t, "hint "+mode+" ·", sendDot, dotShift))
	if kind == "digits" {
		saveInkCrop(t, "hint-"+mode, img, hint)
	}
	if combo, ok := tt.Find("⇧↵"); ok {
		lines = append(lines, measureCombo(t, "hint "+mode, img, combo, hbg, hc, inkScale, "⇧↵", 11, 13, 400)...)
		if word, wok := tt.Find("newline"); wok {
			lines = append(lines, reportInkGapRects(t, "hint "+mode+" ↵ to newline", img, combo, word, hbg, 3, inkScale))
		} else {
			lines = append(lines, "hint "+mode+" newline missing FAIL")
		}
	} else {
		lines = append(lines, "hint "+mode+" ⇧↵ missing FAIL")
	}
	if kind == "digits" {
		lines = append(lines, measureSessionTip(t, tt, mode)...)
		lines = append(lines, measureVisibleShortcutPairs(t, tt, img, mode)...)
	}
	return lines
}

// measureVisibleShortcutPairs checks symbol pairs on the same frame as the
// composer: sidebar ⌘, and search ⌘K.
func measureVisibleShortcutPairs(t *testing.T, tt *ui.Tester, img *image.RGBA, mode string) []string {
	t.Helper()
	var lines []string
	if row, ok := tt.Find("⌘,"); ok {
		lines = append(lines, reportRowGaps(t, "sidebar "+mode+" ⌘ to ,", img, row, 1)...)
		lines = append(lines, reportSidebarCommaBaseline(t, tt, img, mode)...)
		saveInkCrop(t, "sidebar-"+mode, img, row)
	} else {
		lines = append(lines, "sidebar "+mode+" ⌘, missing FAIL")
	}
	if row, ok := tt.Find("⌘K"); ok {
		bg := sampleBG(img, row, inkScale)
		lines = append(lines, measureCombo(t, "search "+mode, img, row, bg, row.Y+row.H/2, inkScale, "⌘K", 11, 0, 400)...)
	} else {
		lines = append(lines, "search "+mode+" ⌘K missing FAIL")
	}
	return lines
}

// measureRetinaGaps renders the shortcut surfaces at 2×, the scale a
// Retina Mac paints at, and keeps every SPEC §8 ink gap. A row that is
// off by more than 0.25pt is a FAIL, which TestControlCapHeight rejects.
func measureRetinaGaps(t *testing.T, dark bool) []string {
	t.Helper()
	mode := "light"
	if dark {
		mode = "dark"
	}
	prev := inkScale
	inkScale = 2
	defer func() { inkScale = prev }()
	var lines []string
	keep := func(rows []string) {
		for _, row := range rows {
			if strings.Contains(row, "gap=") || strings.Contains(row, "sides ") || strings.Contains(row, "missing") || strings.Contains(row, "clusters=") {
				lines = append(lines, "2x "+row)
			}
		}
	}
	a := chipApp(t, "grok-4.7", "high")
	if dark {
		a.data.Appearance = appearanceDark
	} else {
		a.data.Appearance = appearanceLight
	}
	tt := ui.NewTester(a.view, 1120, 760)
	tt.SetScale(2)
	tt.SetDark(dark)
	tt.Frame()
	img := tt.Image()
	hint := mustFind(t, tt, "↵ send · ⇧↵ newline")
	hbg := sampleBG(img, hint, 2)
	ret := mustFind(t, tt, "↵")
	send := mustFind(t, tt, "send")
	dot := mustFind(t, tt, "·")
	shift := mustFind(t, tt, "⇧")
	sendDot := reportInkGapRects(t, "hint "+mode+" send to ·", img, send, dot, hbg, 5, 2)
	dotShift := reportInkGapRects(t, "hint "+mode+" · to ⇧", img, dot, shift, hbg, 5, 2)
	keep([]string{
		reportInkGapRects(t, "hint "+mode+" ↵ to send", img, ret, send, hbg, 3, 2),
		sendDot,
		dotShift,
		separatorBalance(t, "hint "+mode+" ·", sendDot, dotShift),
	})
	if combo, ok := tt.Find("⇧↵"); ok {
		keep(measureCombo(t, "hint "+mode, img, combo, hbg, hint.Y+hint.H/2, 2, "⇧↵", 11, 13, 400))
		if word, wok := tt.Find("newline"); wok {
			keep([]string{reportInkGapRects(t, "hint "+mode+" ↵ to newline", img, combo, word, hbg, 3, 2)})
		} else {
			keep([]string{"hint " + mode + " newline missing FAIL"})
		}
	} else {
		keep([]string{"hint " + mode + " ⇧↵ missing FAIL"})
	}
	keep(measureSessionTip(t, tt, mode))
	if row, ok := tt.Find("⌘,"); ok {
		keep(reportRowGaps(t, "sidebar "+mode+" ⌘ to ,", img, row, 1))
		saveInkCrop(t, "sidebar-"+mode, img, row)
	} else {
		keep([]string{"sidebar " + mode + " ⌘, missing FAIL"})
	}
	keep(gapOnly(measureModHint(t, mode, dark)))
	keep(retinaEmpty(t, mode, dark))
	keep(retinaStop(t, mode, dark))
	keep(retinaFind(t, mode, dark))
	keep(retinaSendWith(t, mode, dark))
	return lines
}

func gapOnly(rows []string) []string {
	var out []string
	for _, row := range rows {
		if strings.Contains(row, "gap=") || strings.Contains(row, "sides ") || strings.Contains(row, "missing") || strings.Contains(row, "clusters=") {
			out = append(out, row)
		}
	}
	return out
}

func retinaEmpty(t *testing.T, mode string, dark bool) []string {
	t.Helper()
	a := chipApp(t, "grok-4.7", "high")
	if dark {
		a.data.Appearance = appearanceDark
	} else {
		a.data.Appearance = appearanceLight
	}
	a.data.Conversations[0].Messages = nil
	tt := ui.NewTester(a.view, 1120, 760)
	tt.SetScale(2)
	tt.SetDark(dark)
	tt.Frame()
	img := tt.Image()
	var lines []string
	lines = append(lines, measureLabeledPairs(t, tt, img, "empty "+mode, "⌘N  "+newChatTitle, []float32{1, 3})...)
	lines = append(lines, measureLabeledPairs(t, tt, img, "empty "+mode, "⌘K  Search", []float32{1, 3})...)
	lines = append(lines, measureLabeledPairs(t, tt, img, "empty "+mode, "⌘,  Settings", []float32{1, 3})...)
	if row, ok := tt.Find("⌘N  " + newChatTitle); ok {
		saveInkCrop(t, "empty-"+mode, img, ui.Rect{X: row.X - 24, Y: row.Y - 28, W: 420, H: row.H + 56})
	}
	return lines
}

func retinaStop(t *testing.T, mode string, dark bool) []string {
	t.Helper()
	a := chipApp(t, "grok-4.7", "high")
	if dark {
		a.data.Appearance = appearanceDark
	} else {
		a.data.Appearance = appearanceLight
	}
	if a.streams == nil {
		a.streams = map[string]*convStream{}
	}
	a.streams[a.data.ActiveID] = &convStream{cancel: func() {}}
	tt := ui.NewTester(a.view, 1120, 760)
	tt.SetScale(2)
	tt.SetDark(dark)
	tt.Frame()
	img := tt.Image()
	lines := measureLabeledPairs(t, tt, img, "stop-hint "+mode, "⌘. to stop", []float32{1, 3})
	if row, ok := tt.Find("⌘. to stop"); ok {
		saveInkCrop(t, "stop-hint-"+mode, img, row)
	}
	return lines
}

func retinaFind(t *testing.T, mode string, dark bool) []string {
	t.Helper()
	a := chipApp(t, "grok-4.7", "high")
	if dark {
		a.data.Appearance = appearanceDark
	} else {
		a.data.Appearance = appearanceLight
	}
	a.finder.open = true
	a.finder.draft = "Hello"
	a.finder.query = "Hello"
	tt := ui.NewTester(a.view, 1120, 760)
	tt.SetScale(2)
	tt.SetDark(dark)
	tt.Frame()
	var lines []string
	lines = append(lines, measureFindTip(t, tt, mode, "Next", "Next ⌘G")...)
	lines = append(lines, measureFindTip(t, tt, mode, "Previous", "Previous ⇧⌘G")...)
	return lines
}

func retinaSendWith(t *testing.T, mode string, dark bool) []string {
	t.Helper()
	a := chipApp(t, "grok-4.7", "high")
	if dark {
		a.data.Appearance = appearanceDark
	} else {
		a.data.Appearance = appearanceLight
	}
	a.settingsOpen = true
	a.settingsSection = "general"
	tt := ui.NewTester(a.view, 1120, 760)
	tt.SetScale(2)
	tt.SetDark(dark)
	tt.Frame()
	img := tt.Image()
	left := mustFind(t, tt, "↵ Return")
	arrow := mustFind(t, tt, "↵")
	word := mustFind(t, tt, "Return")
	bg := sampleBG(img, ui.Rect{X: left.X - 8, Y: left.Y, W: 6, H: left.H}, 2)
	var lines []string
	lines = append(lines, reportInkGapRects(t, "send-with "+mode+" ↵ to Return", img, arrow, word, bg, 3, 2))
	if right, ok := tt.Find("⌘↵"); ok {
		seg := mustFind(t, tt, "send segment command")
		cbg := sampleBG(img, seg, 2)
		lines = append(lines, measureCombo(t, "send-with "+mode, img, right, cbg, seg.Y+seg.H/2, 2, "⌘↵", 12, 0, 400)...)
	} else {
		lines = append(lines, "send-with "+mode+" ⌘↵ missing FAIL")
	}
	return lines
}

func measureEmptyShortcuts(t *testing.T, mode string, dark bool) []string {
	t.Helper()
	a := chipApp(t, "grok-4.7", "high")
	if dark {
		a.data.Appearance = appearanceDark
	} else {
		a.data.Appearance = appearanceLight
	}
	a.data.Conversations[0].Messages = nil
	tt := ui.NewTester(a.view, 1120, 760)
	tt.SetScale(float32(inkScale))
	tt.SetDark(dark)
	tt.Frame()
	img := tt.Image()
	var lines []string
	lines = append(lines, measureLabeledPairs(t, tt, img, "empty "+mode, "⌘N  "+newChatTitle, []float32{1, 3})...)
	lines = append(lines, measureLabeledPairs(t, tt, img, "empty "+mode, "⌘K  Search", []float32{1, 3})...)
	lines = append(lines, measureLabeledPairs(t, tt, img, "empty "+mode, "⌘,  Settings", []float32{1, 3})...)
	lines = append(lines, reportComboBaseline(t, tt, img, "empty "+mode+" ,", "⌘,  Settings", ",", "S", 11, 0, 400)...)
	if row, ok := tt.Find("⌘N  " + newChatTitle); ok {
		saveInkCrop(t, "empty-"+mode, img, ui.Rect{X: row.X - 24, Y: row.Y - 28, W: 420, H: row.H + 56})
	}
	return lines
}

func measureStopHint(t *testing.T, mode string, dark bool) []string {
	t.Helper()
	a := chipApp(t, "grok-4.7", "high")
	if dark {
		a.data.Appearance = appearanceDark
	} else {
		a.data.Appearance = appearanceLight
	}
	if a.streams == nil {
		a.streams = map[string]*convStream{}
	}
	a.streams[a.data.ActiveID] = &convStream{cancel: func() {}}
	tt := ui.NewTester(a.view, 1120, 760)
	tt.SetScale(float32(inkScale))
	tt.SetDark(dark)
	tt.Frame()
	img := tt.Image()
	lines := measureLabeledPairs(t, tt, img, "stop-hint "+mode, "⌘. to stop", []float32{1, 3})
	lines = append(lines, reportComboBaseline(t, tt, img, "stop-hint "+mode+" .", "⌘. to stop", ".", "t", 11, 13, 400)...)
	if row, ok := tt.Find("⌘. to stop"); ok {
		saveInkCrop(t, "stop-hint-"+mode, img, row)
	}
	return lines
}

func saveInkCrop(t *testing.T, name string, img *image.RGBA, row ui.Rect) {
	t.Helper()
	dir := testArtifactDir(t, "chip-center")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Error(err)
		return
	}
	crop := dipRect(row.X-16, row.Y-10, row.W+32, row.H+20, inkScale, img.Bounds())
	file := name + ".png"
	if inkScale == 2 {
		file = name + "-2x.png"
	}
	writePNG(t, filepath.Join(dir, file), cropImage(img, crop))
}

// measureLabeledPairs reads the ink runs of one shortcut line, left to
// right, and checks each hole against the §8 targets. A line whose second
// run is a word still produces one run per glyph when the holes are the
// 1pt and 3pt gaps, so the targets are those gaps in order.
func measureLabeledPairs(t *testing.T, tt *ui.Tester, img *image.RGBA, name, label string, wants []float32) []string {
	t.Helper()
	row, ok := tt.Find(label)
	if !ok {
		return []string{name + " " + label + " missing FAIL"}
	}
	bg := sampleBG(img, row, inkScale)
	spans := inkClusters(img, row, bg, inkScale)
	if len(spans) < len(wants)+1 {
		return []string{fmt.Sprintf("%s %s clusters=%d want=%d FAIL", name, label, len(spans), len(wants)+1)}
	}
	var lines []string
	for i, want := range wants {
		lines = append(lines, reportGap(t, fmt.Sprintf("%s %s gap %d", name, label, i+1), looseGap(img, spans[i], spans[i+1], bg, inkScale), want))
	}
	return lines
}

func measureModHint(t *testing.T, mode string, dark bool) []string {
	t.Helper()
	a := chipApp(t, "grok-4.7", "high")
	if dark {
		a.data.Appearance = appearanceDark
	} else {
		a.data.Appearance = appearanceLight
	}
	a.data.SendWith = "mod"
	tt := ui.NewTester(a.view, 1120, 760)
	tt.SetScale(float32(inkScale))
	tt.SetDark(dark)
	tt.Frame()
	img := tt.Image()
	hint, ok := tt.Find("⌘↵ send · ↵ newline")
	if !ok {
		return []string{"mod-hint " + mode + " missing FAIL"}
	}
	bg := sampleBG(img, hint, inkScale)
	var lines []string
	if combo, cok := tt.Find("⌘↵"); cok {
		lines = append(lines, measureCombo(t, "mod-hint "+mode, img, combo, bg, hint.Y+hint.H/2, inkScale, "⌘↵", 11, 13, 400)...)
		send, sok := tt.Find("send")
		dot, dok := tt.Find("·")
		if sok {
			lines = append(lines, reportInkGapRects(t, "mod-hint "+mode+" ↵ to send", img, combo, send, bg, 3, inkScale))
		} else {
			lines = append(lines, "mod-hint "+mode+" send missing FAIL")
		}
		if sok && dok {
			sendDot := reportInkGapRects(t, "mod-hint "+mode+" send to ·", img, send, dot, bg, 5, inkScale)
			// The ↵ after the dot is the second return arrow. Its ink is the
			// right edge of the hint up to newline, so measure · against the
			// following glyph by the dot's own right side and the next run.
			next := ui.Rect{X: dot.X + dot.W, Y: hint.Y, W: hint.X + hint.W - (dot.X + dot.W), H: hint.H}
			dotNext := reportInkGapRects(t, "mod-hint "+mode+" · to ↵", img, dot, next, bg, 5, inkScale)
			lines = append(lines, sendDot, dotNext, separatorBalance(t, "mod-hint "+mode+" ·", sendDot, dotNext))
		} else if !dok {
			lines = append(lines, "mod-hint "+mode+" · missing FAIL")
		}
	} else {
		lines = append(lines, "mod-hint "+mode+" ⌘↵ missing FAIL")
	}
	if word, wok := tt.Find("newline"); wok {
		// The lone ↵ is the one before newline. Find returns the first ↵,
		// which is inside ⌘↵, so use the newline's left neighbor by scanning
		// the hint from the word back to the previous glyph.
		prev := ui.Rect{X: hint.X, Y: hint.Y, W: word.X - hint.X, H: hint.H}
		lines = append(lines, reportInkGapRects(t, "mod-hint "+mode+" ↵ to newline", img, prev, word, bg, 3, inkScale))
	} else {
		lines = append(lines, "mod-hint "+mode+" newline missing FAIL")
	}
	saveInkCrop(t, "mod-hint-"+mode, img, hint)
	return lines
}

// reportComboBaseline checks that a punctuation key sits on the same
// baseline as a letter in the same shortcut line. The letter is the first
// rune of the word after the combo; its ink bottom is the baseline.
func reportComboBaseline(t *testing.T, tt *ui.Tester, img *image.RGBA, name, label, punct, letter string, size, lineH float32, weight int) []string {
	t.Helper()
	row, ok := tt.Find(label)
	if !ok {
		return []string{name + " baseline missing FAIL"}
	}
	bg := sampleBG(img, row, inkScale)
	spans := inkClusters(img, row, bg, inkScale)
	if len(spans) < 3 {
		return []string{fmt.Sprintf("%s baseline clusters=%d FAIL", name, len(spans))}
	}
	// Modifier, then the punctuation key, then the word.
	mark := spans[1]
	word := spans[2]
	letterW := ui.ShapeText(letter, ui.Font{Size: size, Weight: weight}, 0).CaretAt(ui.TextCaretPosition{Index: 1}).X
	x0 := word.left
	x1 := x0 + int(letterW*float32(inkScale))
	if x1 > word.right {
		x1 = word.right + 1
	}
	letterBot, lok := inkBottomBetween(img, x0, x1, word.top, word.bot+1, bg)
	if !lok {
		return []string{name + " letter ink missing FAIL"}
	}
	markBot := float32(mark.bot+1) / float32(inkScale)
	markBase := markBot - inkBelowBaseline(punct, size, lineH, weight)
	letterBase := letterBot - inkBelowBaseline(letter, size, lineH, weight)
	return []string{reportBaseline(t, name+" baseline", markBase, letterBase)}
}

func reportSidebarCommaBaseline(t *testing.T, tt *ui.Tester, img *image.RGBA, mode string) []string {
	t.Helper()
	shortcut, ok := tt.Find("⌘,")
	gear, gok := tt.Find("Settings")
	if !ok || !gok {
		return []string{"sidebar " + mode + " , baseline missing FAIL"}
	}
	bg := sampleBG(img, gear, inkScale)
	spans := inkClusters(img, shortcut, bg, inkScale)
	if len(spans) < 2 {
		return []string{fmt.Sprintf("sidebar %s , baseline clusters=%d FAIL", mode, len(spans))}
	}
	commaBot := float32(spans[len(spans)-1].bot+1) / float32(inkScale)
	commaBase := commaBot - inkBelowBaseline(",", 11, 0, 400)
	// "Settings" is the 13pt label in the same button, to the left of ⌘,.
	text := ui.Rect{X: gear.X + 36, Y: gear.Y, W: shortcut.X - gear.X - 44, H: gear.H}
	if text.W < 8 {
		return []string{"sidebar " + mode + " Settings text missing FAIL"}
	}
	sW := ui.ShapeText("S", ui.Font{Size: 13, Weight: 400}, 0).CaretAt(ui.TextCaretPosition{Index: 1}).X
	x0 := int(text.X * float32(inkScale))
	x1 := x0 + int(sW*float32(inkScale))
	y0 := int(text.Y * float32(inkScale))
	y1 := int((text.Y + text.H) * float32(inkScale))
	letterBot, lok := inkBottomBetween(img, x0, x1, y0, y1, bg)
	if !lok {
		return []string{"sidebar " + mode + " S ink missing FAIL"}
	}
	letterBase := letterBot - inkBelowBaseline("S", 13, 0, 400)
	return []string{reportBaseline(t, "sidebar "+mode+" , baseline", commaBase, letterBase)}
}

func measureKeycapPunctBaseline(t *testing.T, tt *ui.Tester, img *image.RGBA, mode string) []string {
	t.Helper()
	lcap, ok := tt.Find("keycap L")
	glyph, gok := tt.Find("L")
	if !ok || !gok {
		return []string{"shortcut-caps " + mode + " L missing FAIL"}
	}
	bg := sampleBG(img, lcap, inkScale)
	lBot, lok := inkBottomRect(img, glyph, bg, inkScale)
	if !lok {
		return []string{"shortcut-caps " + mode + " L ink missing FAIL"}
	}
	// Compare each punctuation key to L inside its own cap, so a different
	// row on the page does not look like a baseline error.
	lBase := lBot - inkBelowBaseline("L", 12, 0, 500) - lcap.Y
	var lines []string
	for _, spec := range []struct{ caps, punct string }{
		{"keycaps ⌘,", ","},
		{"keycaps ⌥⌘,", ","},
		{"keycaps ⌘.", "."},
	} {
		row, rok := tt.Find(spec.caps)
		if !rok {
			lines = append(lines, "shortcut-caps "+mode+" "+spec.caps+" missing FAIL")
			continue
		}
		cap := ui.Rect{X: row.X + row.W - 24, Y: row.Y, W: 24, H: row.H}
		mark := ui.Rect{X: cap.X + 4, Y: cap.Y + 2, W: cap.W - 8, H: cap.H - 4}
		mBot, mok := inkBottomRect(img, mark, bg, inkScale)
		if !mok {
			lines = append(lines, "shortcut-caps "+mode+" "+spec.punct+" ink missing FAIL")
			continue
		}
		mBase := mBot - inkBelowBaseline(spec.punct, 12, 0, 500) - cap.Y
		lines = append(lines, reportBaseline(t, "shortcut-caps "+mode+" "+spec.caps+" "+spec.punct+" baseline", mBase, lBase))
	}
	return lines
}

func reportBaseline(t *testing.T, name string, got, want float32) string {
	t.Helper()
	off := got - want
	status := "PASS"
	if off < -0.25 || off > 0.25 {
		status = "FAIL"
	}
	return fmt.Sprintf("%s baseline=%.2f text=%.2f offset=%+.3f %s", name, got, want, off, status)
}

// inkBelowBaseline is how far a glyph's ink extends below the font
// baseline, in the unshifted line box. Translation moves both together.
func inkBelowBaseline(sample string, size, lineH float32, weight int) float32 {
	box := opticalOf(sample, size, lineH, weight)
	m := ui.Font{Size: size, Weight: weight}.Metrics()
	base := (box.elemH-m.Ascent-m.Descent)/2 + m.Ascent
	return box.bot - base
}

func inkBottomRect(img *image.RGBA, r ui.Rect, bg color.RGBA, scale int) (float32, bool) {
	_, bot, ok := inkBounds(img, r, bg, scale)
	if !ok {
		return 0, false
	}
	return float32(bot+1) / float32(scale), true
}

func inkBottomBetween(img *image.RGBA, x0, x1, y0, y1 int, bg color.RGBA) (float32, bool) {
	if x1 <= x0 || y1 <= y0 {
		return 0, false
	}
	b := img.Bounds()
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	maxD := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if d := colorDelta(img.RGBAAt(x, y), bg); d > maxD {
				maxD = d
			}
		}
	}
	if maxD < 12 {
		return 0, false
	}
	cut := maxD * 2 / 3
	if cut < 16 {
		cut = 16
	}
	bot := y0
	found := false
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if colorDelta(img.RGBAAt(x, y), bg) < cut {
				continue
			}
			found = true
			if y > bot {
				bot = y
			}
		}
	}
	if !found {
		return 0, false
	}
	return float32(bot+1) / float32(inkScale), true
}

// reportRowGaps measures the ink gap between the first two ink runs in a
// shortcut line. Used when the second run is punctuation rather than a
// separate findable glyph.
func reportRowGaps(t *testing.T, name string, img *image.RGBA, row ui.Rect, want float32) []string {
	t.Helper()
	bg := sampleBG(img, row, inkScale)
	spans := inkClusters(img, row, bg, inkScale)
	if len(spans) < 2 {
		return []string{fmt.Sprintf("%s clusters=%d FAIL", name, len(spans))}
	}
	return []string{reportGap(t, name, looseGap(img, spans[0], spans[1], bg, inkScale), want)}
}

func measureSettings(t *testing.T, mode string, dark bool) []string {
	t.Helper()
	var lines []string
	a := chipApp(t, "grok-4.7", "high")
	if !dark {
		a.data.Appearance = appearanceLight
	}
	a.settingsOpen = true
	a.settingsSection = "general"
	tt := ui.NewTester(a.view, 1120, 760)
	tt.SetScale(float32(inkScale))
	tt.SetDark(dark)
	tt.Frame()
	img := tt.Image()
	left := mustFind(t, tt, "↵ Return")
	seg := mustFind(t, tt, "send segment return")
	arrow := mustFind(t, tt, "↵")
	word := mustFind(t, tt, "Return")
	bg := sampleBG(img, ui.Rect{X: left.X - 8, Y: left.Y, W: 6, H: left.H}, inkScale)
	center := seg.Y + seg.H/2
	layR := ui.ShapeText("Return", ui.Font{Size: 12, Weight: 400}, 0)
	capR := word
	capR.W = layR.CaretAt(ui.TextCaretPosition{Index: 1}).X
	lines = append(lines, reportInk(t, "send-with "+mode+" ↵", img, arrow, bg, center, inkScale))
	lines = append(lines, reportInk(t, "send-with "+mode+" R", img, capR, bg, center, inkScale))
	lines = append(lines, reportInkGapRects(t, "send-with "+mode+" ↵ to Return", img, arrow, word, bg, 3, inkScale))
	if right, ok := tt.Find("⌘↵"); ok {
		seg := mustFind(t, tt, "send segment command")
		cbg := sampleBG(img, seg, inkScale)
		lines = append(lines, measureCombo(t, "send-with "+mode, img, right, cbg, seg.Y+seg.H/2, inkScale, "⌘↵", 12, 0, 400)...)
		lines = append(lines, reportInkX(t, "send-with "+mode+" ⌘↵ horizontal", img, right, cbg, seg.X+seg.W/2, inkScale))
	} else {
		lines = append(lines, "send-with "+mode+" ⌘↵ missing FAIL")
	}
	a.settingsSection = "appearance"
	tt = ui.NewTester(a.view, 1120, 760)
	tt.SetScale(float32(inkScale))
	tt.SetDark(dark)
	tt.Frame()
	img = tt.Image()
	seg = mustFind(t, tt, "Appearance mode")
	bg = sampleBG(img, seg, inkScale)
	center = seg.Y + seg.H/2
	for _, label := range []string{"System", "Light", "Dark"} {
		r := mustFind(t, tt, label)
		lay := ui.ShapeText(label, ui.Font{Size: 12, Weight: 500}, 0)
		cap := r
		cap.W = lay.CaretAt(ui.TextCaretPosition{Index: 1}).X
		segBG := sampleBG(img, ui.Rect{X: r.X - 6, Y: r.Y, W: 4, H: r.H}, inkScale)
		lines = append(lines, reportInk(t, "segment "+mode+" "+label+" cap", img, cap, segBG, center, inkScale))
	}
	a.settingsSection = "general"
	tt = ui.NewTester(a.view, 1120, 760)
	tt.SetScale(float32(inkScale))
	tt.SetDark(dark)
	tt.Frame()
	img = tt.Image()
	pop := mustFind(t, tt, "Default model menu")
	pbg := sampleBG(img, pop, inkScale)
	modelText, ok := tt.Find("gpt-4o-mini")
	if !ok {
		lines = append(lines, "popup "+mode+" model text missing FAIL")
	} else {
		lay := ui.ShapeText("gpt-4o-mini", ui.Font{Size: 13, Weight: 400}, 0)
		sub := modelText
		sub.X += lay.CaretAt(ui.TextCaretPosition{Index: strings.IndexRune("gpt-4o-mini", '4')}).X
		sub.W = modelText.X + modelText.W - sub.X
		lines = append(lines, reportInk(t, "popup "+mode+" digits", img, sub, pbg, pop.Y+pop.H/2, inkScale))
	}
	a.settingsSection = "shortcuts"
	tt = ui.NewTester(a.view, 1120, 760)
	tt.SetScale(float32(inkScale))
	tt.SetDark(dark)
	tt.Frame()
	img = tt.Image()
	for _, key := range []string{"⌫", "⌥", "⇧", "⌘"} {
		cap := mustFind(t, tt, "keycap "+key)
		glyph, ok := tt.Find(key)
		if !ok {
			lines = append(lines, fmt.Sprintf("shortcut %s %s missing FAIL", mode, key))
			continue
		}
		bg := sampleBG(img, cap, inkScale)
		lines = append(lines, reportInk(t, "shortcut-key "+mode+" "+key, img, glyph, bg, cap.Y+cap.H/2, inkScale))
		lines = append(lines, reportInkX(t, "shortcut-key "+mode+" "+key+" horizontal", img, glyph, bg, cap.X+cap.W/2, inkScale))
	}
	lines = append(lines, measureAddToChatRow(t, tt, img, mode)...)
	lines = append(lines, measureKeycapPunctBaseline(t, tt, img, mode)...)
	for _, id := range []string{"keycaps ⌘,", "keycaps ⌥⌘,", "keycaps ⌘.", "keycaps ⌘⌫", "keycaps ↑"} {
		cap, ok := tt.Find(id)
		if !ok {
			lines = append(lines, "shortcut-caps "+mode+" "+id+" missing FAIL")
			continue
		}
		saveInkCrop(t, "shortcut-"+strings.ReplaceAll(id, " ", "-")+"-"+mode, img, cap)
	}
	return lines
}

func measureBarShortcut(t *testing.T, mode string, dark bool) []string {
	t.Helper()
	a := newTestApp(t)
	if dark {
		a.data.Appearance = appearanceDark
	} else {
		a.data.Appearance = appearanceLight
	}
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Notes", Loaded: true, Config: DefaultConfig(),
		Messages: []Message{{ID: "u1", Role: roleUser, Content: "Measure the shortcut hint on the selection bar.", Done: true}},
	}}
	a.data.ActiveID = "c1"
	tt := ui.NewTester(a.view, 1100, 760)
	tt.SetScale(float32(inkScale))
	tt.SetDark(dark)
	tt.Frame()
	dragRects(tt, resolvePhrase(t, a, tt, "shortcut hint"))
	if !a.quoteBarOpen {
		return []string{"bar " + mode + " closed FAIL"}
	}
	return measureQuoteBar(t, tt, copyRGBA(tt.Image()), mode)
}

func measureAddToChatRow(t *testing.T, tt *ui.Tester, img *image.RGBA, mode string) []string {
	t.Helper()
	row, ok := tt.Find("keycaps ⌘L")
	lcap, lok := tt.Find("keycap L")
	if !ok || !lok {
		return []string{"shortcut-row " + mode + " Add to Chat ⌘L missing FAIL"}
	}
	var lines []string
	bg := sampleBG(img, lcap, inkScale)
	if glyph, gok := tt.Find("L"); gok && glyph.X >= lcap.X-0.5 && glyph.X+glyph.W <= lcap.X+lcap.W+0.5 {
		lines = append(lines, reportInk(t, "shortcut-row "+mode+" Add to Chat L", img, glyph, bg, lcap.Y+lcap.H/2, inkScale))
		lines = append(lines, reportInkX(t, "shortcut-row "+mode+" Add to Chat L horizontal", img, glyph, bg, lcap.X+lcap.W/2, inkScale))
	} else {
		lines = append(lines, "shortcut-row "+mode+" Add to Chat L glyph missing FAIL")
	}
	gap := float32(3)
	cmd := ui.Rect{X: row.X, Y: row.Y, W: lcap.X - gap - row.X, H: row.H}
	if cmd.W < 8 {
		return append(lines, "shortcut-row "+mode+" Add to Chat ⌘ cap missing FAIL")
	}
	// Stay inside the cap fill so the border is not counted as the glyph.
	inset := ui.Rect{X: cmd.X + 3, Y: cmd.Y + 1, W: cmd.W - 6, H: cmd.H - 2}
	cbg := sampleBG(img, cmd, inkScale)
	lines = append(lines, reportInk(t, "shortcut-row "+mode+" Add to Chat ⌘", img, inset, cbg, cmd.Y+cmd.H/2, inkScale))
	lines = append(lines, reportInkX(t, "shortcut-row "+mode+" Add to Chat ⌘ horizontal", img, inset, cbg, cmd.X+cmd.W/2, inkScale))
	return lines
}

func measureFind(t *testing.T, mode string, dark bool) []string {
	t.Helper()
	a := chipApp(t, "grok-4.7", "high")
	if !dark {
		a.data.Appearance = appearanceLight
	}
	a.finder.open = true
	a.finder.draft = "Hello"
	a.finder.query = "Hello"
	tt := ui.NewTester(a.view, 1120, 760)
	tt.SetScale(float32(inkScale))
	tt.SetDark(dark)
	tt.Frame()
	img := tt.Image()
	bar := mustFind(t, tt, "find bar")
	field := mustFind(t, tt, "find field")
	var lines []string
	for _, label := range []string{"Done", "No results", "1 of 1"} {
		r, ok := tt.Find(label)
		if !ok {
			continue
		}
		// The bar's bottom border sits inside the row, so the centered
		// controls share the field's center, not the border-box center.
		want := field.Y + field.H/2
		scan := r
		if label == "Done" {
			want = r.Y + r.H/2
		}
		if label == "1 of 1" {
			// The f in "of" rises above the digits. Measure the last digit.
			digit := ui.ShapeText("1", ui.Font{Size: 12, Weight: 400}, 0).CaretAt(ui.TextCaretPosition{Index: 1}).X
			scan.X = r.X + r.W - digit
			scan.W = digit
		}
		lines = append(lines, reportInk(t, "find "+mode+" "+label, img, scan, sampleBG(img, bar, inkScale), want, inkScale))
	}
	if len(lines) == 0 {
		lines = append(lines, "find "+mode+" counter missing FAIL")
	}
	lines = append(lines, measureFindTip(t, tt, mode, "Next", "Next ⌘G")...)
	lines = append(lines, measureFindTip(t, tt, mode, "Previous", "Previous ⇧⌘G")...)
	return lines
}

func restPointer(tt *ui.Tester, r ui.Rect) {
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Advance(700 * time.Millisecond)
	tt.Frame()
}

func measureSessionTip(t *testing.T, tt *ui.Tester, mode string) []string {
	t.Helper()
	btn := mustFind(t, tt, "New Session")
	restPointer(tt, btn)
	img := tt.Image()
	row, ok := tt.Find("⇧⌘N")
	if !ok {
		return []string{"session-tip " + mode + " missing FAIL"}
	}
	bg := sampleBG(img, row, inkScale)
	return measureCombo(t, "session-tip "+mode, img, row, bg, row.Y+row.H/2, inkScale, "⇧⌘N", 11, 0, 400)
}

func measureFindTip(t *testing.T, tt *ui.Tester, mode, anchor, label string) []string {
	t.Helper()
	btn := mustFind(t, tt, anchor)
	restPointer(tt, btn)
	img := tt.Image()
	row, ok := tt.Find(label)
	if !ok {
		return []string{"find-hint " + mode + " " + label + " missing FAIL"}
	}
	bg := sampleBG(img, row, inkScale)
	center := row.Y + row.H/2
	if combo, ok := tt.Find("⇧⌘G"); ok && combo.X >= row.X-0.5 && combo.X+combo.W <= row.X+row.W+0.5 {
		lines := measureCombo(t, "find-hint "+mode, img, combo, bg, center, inkScale, "⇧⌘G", 11, 0, 400)
		word := ui.Rect{X: row.X, Y: row.Y, W: combo.X - row.X, H: row.H}
		lines = append(lines, reportInkGapRects(t, "find-hint "+mode+" Previous to ⇧", img, word, combo, bg, 3, inkScale))
		return lines
	}
	combo, ok := tt.Find("⌘G")
	if !ok || combo.X < row.X-0.5 || combo.X+combo.W > row.X+row.W+0.5 {
		return []string{"find-hint " + mode + " " + label + " shortcut missing FAIL"}
	}
	lines := measureCombo(t, "find-hint "+mode, img, combo, bg, center, inkScale, "⌘G", 11, 0, 400)
	word := ui.Rect{X: row.X, Y: row.Y, W: combo.X - row.X, H: row.H}
	lines = append(lines, reportInkGapRects(t, "find-hint "+mode+" Next to ⌘", img, word, combo, bg, 3, inkScale))
	return lines
}

func measureEdit(t *testing.T, mode string, dark bool) []string {
	t.Helper()
	a := chipApp(t, "grok-4.7", "high")
	if !dark {
		a.data.Appearance = appearanceLight
	}
	a.edit.convID = "c"
	a.edit.msgID = "u"
	a.edit.text = "Hello"
	tt := ui.NewTester(a.view, 1120, 760)
	tt.SetScale(float32(inkScale))
	tt.SetDark(dark)
	tt.Frame()
	img := tt.Image()
	cap, ok := tt.Find("esc keycap")
	glyph, gok := tt.Find("esc")
	if !ok || !gok {
		return []string{"esc-keycap " + mode + " missing FAIL"}
	}
	// esc is a word. Cap-height centering puts H on the keycap center, so
	// the lowercase ink sits lower by the measured difference.
	h := opticalOf("H", 11, 0, 400)
	word := opticalOf("esc", 11, 0, 400)
	center := cap.Y + cap.H/2
	return []string{reportInk(t, "esc-keycap "+mode+" lowercase", img, glyph, sampleBG(img, cap, inkScale), center+(h.dy-word.dy), inkScale)}
}

func reportInk(t *testing.T, name string, img *image.RGBA, r ui.Rect, bg color.RGBA, want float32, scale int) string {
	t.Helper()
	if bg.A == 0 {
		bg = edgeBG(img, r, scale)
	}
	top, bot, ok := inkBounds(img, r, bg, scale)
	if !ok {
		x := int((r.X + r.W/2) * float32(scale))
		y := int(r.Y*float32(scale)) - 3
		b := img.Bounds()
		if x >= b.Min.X && x < b.Max.X && y >= b.Min.Y && y < b.Max.Y {
			bg = img.RGBAAt(x, y)
			top, bot, ok = inkBounds(img, r, bg, scale)
		}
	}
	if !ok {
		cx := int((r.X + r.W/2) * float32(scale))
		cy := int((r.Y + r.H/2) * float32(scale))
		p := img.RGBAAt(cx, cy)
		return fmt.Sprintf("%s no-ink rect=%v px=%d,%d #%02x%02x%02x bg=#%02x%02x%02x FAIL", name, r, cx, cy, p.R, p.G, p.B, bg.R, bg.G, bg.B)
	}
	got := float32(top+bot+1) / 2 / float32(scale)
	off := got - want
	status := "PASS"
	if off < -0.25 || off > 0.25 {
		status = "FAIL"
	}
	return fmt.Sprintf("%s center=%.2f control=%.2f offset=%+.3f h=%.1f %s", name, got, want, off, r.H, status)
}

func reportZone(t *testing.T, name string, img *image.RGBA, r ui.Rect, bg color.RGBA, want float32, scale int) string {
	t.Helper()
	return reportInk(t, name, img, r, bg, want, scale)
}

func reportInkX(t *testing.T, name string, img *image.RGBA, r ui.Rect, bg color.RGBA, want float32, scale int) string {
	t.Helper()
	left, right, ok := inkBoundsX(img, r, bg, scale)
	if !ok {
		return fmt.Sprintf("%s no-ink rect=%v FAIL", name, r)
	}
	got := float32(left+right+1) / 2 / float32(scale)
	off := got - want
	status := "PASS"
	if off < -0.25 || off > 0.25 {
		status = "FAIL"
	}
	return fmt.Sprintf("%s center=%.2f control=%.2f offset=%+.3f w=%.1f %s", name, got, want, off, r.W, status)
}

// measureCombo checks each symbol in a run (⇧↵, ⌘↵) against the line's
// cap-height center, and the 1pt gap between consecutive symbols. The gap
// is the row's leftover width after each glyph's measured advance.
func measureCombo(t *testing.T, name string, img *image.RGBA, r ui.Rect, bg color.RGBA, wantY float32, scale int, glyphs string, size, lineH float32, weight int) []string {
	t.Helper()
	rs := []rune(glyphs)
	spans := inkClusters(img, r, bg, scale)
	if len(spans) != len(rs) {
		return []string{fmt.Sprintf("%s %s clusters=%d want=%d FAIL", name, glyphs, len(spans), len(rs))}
	}
	var lines []string
	for i, sp := range spans {
		got := float32(sp.top+sp.bot+1) / 2 / float32(scale)
		off := got - wantY
		status := "PASS"
		if off < -0.25 || off > 0.25 {
			status = "FAIL"
		}
		lines = append(lines, fmt.Sprintf("%s %s center=%.2f control=%.2f offset=%+.3f %s", name, string(rs[i]), got, wantY, off, status))
		if i+1 < len(spans) {
			gap := looseGap(img, sp, spans[i+1], bg, scale)
			lines = append(lines, reportGap(t, fmt.Sprintf("%s %s to %s", name, string(rs[i]), string(rs[i+1])), gap, 1))
		}
	}
	return lines
}

type inkCluster struct {
	left, right int
	top, bot    int
}

func inkClusters(img *image.RGBA, r ui.Rect, bg color.RGBA, scale int) []inkCluster {
	x0 := int(r.X) * scale
	x1 := int(r.X+r.W) * scale
	y0 := int(r.Y) * scale
	y1 := int(r.Y+r.H) * scale
	b := img.Bounds()
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	if x1 <= x0 || y1 <= y0 {
		return nil
	}
	maxD := 0
	colD := make([]int, x1-x0)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			d := colorDelta(img.RGBAAt(x, y), bg)
			if d > maxD {
				maxD = d
			}
			if d > colD[x-x0] {
				colD[x-x0] = d
			}
		}
	}
	if maxD < 12 {
		return nil
	}
	cut := maxD * 2 / 3
	if cut < 16 {
		cut = 16
	}
	var spans []inkCluster
	open := false
	var cur inkCluster
	for i, d := range colD {
		if d < cut {
			if open {
				spans = append(spans, cur)
				open = false
			}
			continue
		}
		x := x0 + i
		if !open {
			cur = inkCluster{left: x, right: x, top: y1, bot: y0}
			open = true
		}
		cur.right = x
		for y := y0; y < y1; y++ {
			if colorDelta(img.RGBAAt(x, y), bg) < cut {
				continue
			}
			if y < cur.top {
				cur.top = y
			}
			if y > cur.bot {
				cur.bot = y
			}
		}
	}
	if open {
		spans = append(spans, cur)
	}
	return spans
}

func reportInkGapRects(t *testing.T, name string, img *image.RGBA, left, right ui.Rect, bg color.RGBA, want float32, scale int) string {
	t.Helper()
	if left.X > right.X {
		left, right = right, left
	}
	lScan, rScan := left, right
	if left.X+left.W > right.X {
		split := right.X
		overlapR := left.X + left.W
		if overlapR > right.X+right.W {
			overlapR = right.X + right.W
		}
		split = (right.X + overlapR) / 2
		lScan.W = split - left.X
		rScan.X = split
		rScan.W = right.X + right.W - split
	}
	gap, ok := pairCoverage(img, lScan, rScan, scale)
	if !ok {
		return name + " no-ink FAIL"
	}
	return reportGap(t, name, gap, want)
}

// pairCoverage is the point gap between the half-coverage edges of the ink
// in left and the ink in right. It is the same reading tools/inkgap makes
// from the delivered PNG.
func pairCoverage(img *image.RGBA, left, right ui.Rect, scale int) (float32, bool) {
	if left.X > right.X {
		left, right = right, left
	}
	const pad = 8
	x0 := int(left.X*float32(scale)) - pad
	y0 := int(min32(left.Y, right.Y)*float32(scale)) - pad
	x1 := int((right.X+right.W)*float32(scale)) + pad
	y1 := int(max32(left.Y+left.H, right.Y+right.H)*float32(scale)) + pad
	r := image.Rect(x0, y0, x1, y1).Intersect(img.Bounds())
	if r.Empty() {
		return 0, false
	}
	crop := cropImage(img, r)
	edges := inkgap.FocusEdges(crop, float64(scale))
	if len(edges) < 2 {
		return 0, false
	}
	split := float64((left.X+left.W+right.X)/2*float32(scale)) - float64(r.Min.X)
	var haveL, haveR bool
	var leftRight, rightLeft float64
	for _, e := range edges {
		if e.Right <= split+0.5 {
			leftRight = e.Right
			haveL = true
		}
		if !haveR && e.Left >= split-0.5 {
			rightLeft = e.Left
			haveR = true
		}
	}
	if !haveL || !haveR || rightLeft <= leftRight {
		return 0, false
	}
	return float32(rightLeft-leftRight) / float32(scale), true
}

func min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func max32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// looseGap is the ink-to-ink hole between two glyphs, including the faint
// fringe opticalOf counts.
func looseGap(img *image.RGBA, a, b inkCluster, bg color.RGBA, scale int) float32 {
	// The cluster boxes only choose which glyphs. The distance is the
	// half-coverage gap in the bitmap, via tools/inkgap.
	_ = bg
	left := ui.Rect{
		X: float32(a.left) / float32(scale),
		Y: float32(a.top) / float32(scale),
		W: float32(a.right-a.left+1) / float32(scale),
		H: float32(a.bot-a.top+1) / float32(scale),
	}
	right := ui.Rect{
		X: float32(b.left) / float32(scale),
		Y: float32(b.top) / float32(scale),
		W: float32(b.right-b.left+1) / float32(scale),
		H: float32(b.bot-b.top+1) / float32(scale),
	}
	gap, ok := pairCoverage(img, left, right, scale)
	if !ok {
		return 0
	}
	return gap
}

// expandInk walks from x along dir (+1 or -1) while the column still
// carries the fringe, and stops before limit. It returns the last ink pixel.
func expandInk(img *image.RGBA, x, limit, y0, y1 int, bg color.RGBA, dir int) int {
	b := img.Bounds()
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	maxD := 0
	for y := y0; y < y1; y++ {
		for xx := x; ; xx += dir {
			if dir > 0 && xx >= limit || dir < 0 && xx <= limit {
				break
			}
			if xx < b.Min.X || xx >= b.Max.X {
				break
			}
			if d := colorDelta(img.RGBAAt(xx, y), bg); d > maxD {
				maxD = d
			}
			if dir > 0 && xx+dir >= limit || dir < 0 && xx+dir <= limit {
				break
			}
		}
	}
	if maxD < 12 {
		return x
	}
	cut := maxD * 3 / 10
	if cut < 12 {
		cut = 12
	}
	last := x
	for {
		next := last + dir
		if dir > 0 && next >= limit || dir < 0 && next <= limit {
			break
		}
		if next < b.Min.X || next >= b.Max.X {
			break
		}
		ink := false
		for y := y0; y < y1; y++ {
			if colorDelta(img.RGBAAt(next, y), bg) >= cut {
				ink = true
				break
			}
		}
		if !ink {
			break
		}
		last = next
	}
	return last
}

func inkEdges(img *image.RGBA, r ui.Rect, bg color.RGBA, scale int) (left, right float32, ok bool) {
	l, rt, ok := inkBoundsX(img, r, bg, scale)
	if !ok {
		return 0, 0, false
	}
	s := float32(scale)
	return float32(l) / s, float32(rt+1) / s, true
}

func separatorBalance(t *testing.T, name, left, right string) string {
	t.Helper()
	a, b := gapValue(left), gapValue(right)
	off := a - b
	status := "PASS"
	if off < -0.25 || off > 0.25 {
		status = "FAIL"
	}
	return fmt.Sprintf("%s sides %.2f and %.2f diff=%+.3f %s", name, a, b, off, status)
}

func analyticKeyGap(next string) string {
	a := opticalOf("⌘", 11, 0, 400)
	b := opticalOf(next, 11, 0, 400)
	got := specSpacer("⌘", 11, 0, next, 11, 0, 400, 1) + (a.elemW - a.right) + b.left
	off := got - 1
	status := "PASS"
	if off < -0.001 || off > 0.001 {
		status = "FAIL"
	}
	return fmt.Sprintf("analytic ⌘ to %s gap=%.2f want=1 offset=%+.3f %s", next, got, off, status)
}

func gapValue(line string) float32 {
	i := strings.Index(line, "gap=")
	if i < 0 {
		return 0
	}
	var g float32
	fmt.Sscanf(line[i+4:], "%f", &g)
	return g
}

func reportGap(t *testing.T, name string, got, want float32) string {
	t.Helper()
	off := got - want
	status := "PASS"
	if off < -0.25 || off > 0.25 {
		status = "FAIL"
	}
	return fmt.Sprintf("%s gap=%.2f want=%.0f offset=%+.3f %s", name, got, want, off, status)
}

func edgeBG(img *image.RGBA, r ui.Rect, scale int) color.RGBA {
	b := img.Bounds()
	// Side bearing, mid-line: the line box is wider than the cap, so this
	// pixel is the surface behind the glyphs rather than the glyph itself.
	x := int(r.X*float32(scale)) + 1
	y := int((r.Y + r.H/2) * float32(scale))
	if x < b.Min.X {
		x = b.Min.X
	}
	if x >= b.Max.X {
		x = b.Max.X - 1
	}
	if y < b.Min.Y {
		y = b.Min.Y
	}
	if y >= b.Max.Y {
		y = b.Max.Y - 1
	}
	return img.RGBAAt(x, y)
}

func sampleBG(img *image.RGBA, r ui.Rect, scale int) color.RGBA {
	x := int(r.X+2) * scale
	y := int(r.Y+2) * scale
	b := img.Bounds()
	if x >= b.Max.X {
		x = b.Max.X - 1
	}
	if y >= b.Max.Y {
		y = b.Max.Y - 1
	}
	if x < b.Min.X {
		x = b.Min.X
	}
	if y < b.Min.Y {
		y = b.Min.Y
	}
	return img.RGBAAt(x, y)
}

func inkBoundsX(img *image.RGBA, r ui.Rect, bg color.RGBA, scale int) (left, right int, ok bool) {
	spans := inkClusters(img, r, bg, scale)
	if len(spans) == 0 {
		return 0, 0, false
	}
	return spans[0].left, spans[len(spans)-1].right, true
}

func inkBounds(img *image.RGBA, r ui.Rect, bg color.RGBA, scale int) (top, bot int, ok bool) {
	x0 := int(r.X) * scale
	x1 := int(r.X+r.W) * scale
	y0 := int(r.Y) * scale
	y1 := int(r.Y+r.H) * scale
	b := img.Bounds()
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	maxD := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if d := colorDelta(img.RGBAAt(x, y), bg); d > maxD {
				maxD = d
			}
		}
	}
	if maxD < 12 {
		return 0, 0, false
	}
	cut := maxD * 2 / 3
	if cut < 16 {
		cut = 16
	}
	top, bot = y1, y0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if colorDelta(img.RGBAAt(x, y), bg) < cut {
				continue
			}
			ok = true
			if y < top {
				top = y
			}
			if y > bot {
				bot = y
			}
		}
	}
	return top, bot, ok
}

func colorDelta(a, b color.RGBA) int {
	return absInt(int(a.R)-int(b.R)) + absInt(int(a.G)-int(b.G)) + absInt(int(a.B)-int(b.B))
}

func dipRect(x, y, w, h float32, scale int, b image.Rectangle) image.Rectangle {
	r := image.Rect(int(x)*scale, int(y)*scale, int(x+w)*scale, int(y+h)*scale)
	return r.Intersect(b)
}

func chipApp(t *testing.T, model, effort string) *App {
	t.Helper()
	a := newTestApp(t)
	conn := customConn("c1", "xAI", "https://api.x.ai/v1", model)
	a.data.Connections = []Connection{conn}
	if err := a.keys.Set("c1", "key"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	cfg := DefaultConfig()
	cfg.Provider = "Custom"
	cfg.Model = model
	cfg.ConnectionID = "c1"
	cfg.BaseURL = conn.BaseURL
	cfg.Reasoning = effort
	a.data.Conversations = []Conversation{{
		ID: "c", Title: "Notes", Config: cfg, UpdatedAt: now, CreatedAt: now,
		Messages: []Message{{ID: "u", Role: roleUser, Content: "Hello", Done: true, CreatedAt: now}},
	}}
	a.data.ActiveID = "c"
	a.data.Appearance = appearanceDark
	return a
}
