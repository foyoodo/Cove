package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestModelMenuScrollsInShortWindow is the 90-model composer menu in the
// short window from the report: the chip is on the bottom edge, the list
// used to collapse to its last row, and every model has to stay reachable.
func TestModelMenuScrollsInShortWindow(t *testing.T) {
	const (
		winW = 900
		winH = 400
	)
	a := manyModelApp(t, "gamma-08")
	tt := ui.NewTester(a.view, winW, winH)
	tt.Frame()
	if err := tt.Click("Model gamma-08"); err != nil {
		t.Fatal(err)
	}
	if !modelMenuOpen(tt) {
		t.Fatal("menu did not open")
	}
	menu := mustFind(t, tt, "model menu")
	chip := mustFind(t, tt, "Model gamma-08")
	if menu.H < 80 {
		t.Fatalf("menu collapsed to %.1f, the one-row failure", menu.H)
	}
	if menu.H > modelMenuMaxH+4 {
		t.Fatalf("menu height %.1f exceeds the cap %.0f", menu.H, modelMenuMaxH)
	}
	above := chip.Y - 8
	if menu.H > above+2 {
		t.Fatalf("menu %.1f taller than the %.1f above the chip", menu.H, above)
	}
	if menu.Y < -1 || menu.Y+menu.H > float32(winH)+1 {
		t.Fatalf("menu outside the window %+v", menu)
	}
	if menu.Y+menu.H > chip.Y+1 {
		t.Fatalf("menu opened over the chip instead of above it: menu %.1f..%.1f chip %.1f", menu.Y, menu.Y+menu.H, chip.Y)
	}
	if menu.W < 220 || menu.W > 430 {
		t.Fatalf("menu width %.1f", menu.W)
	}

	rows := menuRows(t, tt)
	models := map[string]bool{}
	var sawOff, selectedVisible, footer bool
	visible := 0
	for _, r := range rows {
		if strings.HasSuffix(r.label, "-off") {
			sawOff = true
		}
		if r.on && !r.separator && r.label != "" && r.label != "Models & Providers…" && r.label != "Add a provider…" {
			models[r.label] = false
			if rowOnScreen(menu, r.rect) {
				visible++
				models[r.label] = true
			}
		}
		if r.label == "gamma-08" && r.checked && rowOnScreen(menu, r.rect) {
			selectedVisible = true
		}
		if r.label == "Models & Providers…" {
			footer = true
		}
	}
	if sawOff {
		t.Fatal("disabled model is in the menu")
	}
	if len(models) < 90 {
		t.Fatalf("menu lists %d models, want at least 90", len(models))
	}
	if !selectedVisible {
		t.Fatal("selected model is not visible when the menu opens")
	}
	if !footer {
		t.Fatal("Models & Providers… is not in the menu")
	}
	if visible >= len(models) {
		t.Fatal("every model fit; the list is not actually capped")
	}
	if models["alpha-00"] {
		t.Fatal("menu opened scrolled to the start, past the selected model")
	}
	if models["zeta-15"] {
		t.Fatal("menu opened scrolled to the end")
	}

	// Wheel to the start, then through to the end. Every model must
	// actually appear inside the popup, and the trailing action too.
	// The pointer is parked on the menu first: a scroll that also moves
	// the pointer highlights the row under it and scrolls that row back.
	scrollMenu(t, tt, menu, -20000)
	if !modelVisible(t, tt, "alpha-00") {
		t.Fatal("scrolling up did not reveal the first model")
	}
	for step := 0; step < 40; step++ {
		menu = mustFind(t, tt, "model menu")
		for _, r := range menuRows(t, tt) {
			if _, ok := models[r.label]; ok && rowOnScreen(menu, r.rect) {
				models[r.label] = true
			}
		}
		before := menuSignature(t, tt)
		scrollMenu(t, tt, menu, 160)
		if menuSignature(t, tt) == before {
			break
		}
	}
	var missing []string
	for id, saw := range models {
		if !saw {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("models never scrolled into view: %v", missing)
	}
	if !modelVisible(t, tt, "Models & Providers…") {
		t.Fatal("Models & Providers… is not reachable at the end of the list")
	}
	if !modelVisible(t, tt, "zeta-15") {
		t.Fatal("last model is not reachable")
	}

	chooseGroupedModel(t, tt, "Zeta", "zeta-15")
	if got := a.active().Config; got.Model != "zeta-15" || got.ConnectionID != "conn-zeta" || got.Provider != "Zeta" {
		t.Fatalf("choice from the end of the list %+v", got)
	}
	if modelMenuOpen(tt) {
		t.Fatal("menu stayed open after a choice")
	}

	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		shot := manyModelApp(t, "gamma-08")
		if dark {
			shot.data.Appearance = appearanceDark
		} else {
			shot.data.Appearance = appearanceLight
		}
		view := ui.NewTester(shot.view, winW, winH)
		view.SetScale(2)
		view.SetDark(dark)
		view.Frame()
		if err := view.Click("Model gamma-08"); err != nil {
			t.Fatal(err)
		}
		if !modelMenuOpen(view) {
			t.Fatal("screenshot menu did not open")
		}
		dir := testArtifactDir(t, "model-menu")
		writePNG(t, filepath.Join(dir, "after-short-"+mode+"@2x.png"), view.Image())
	}
}

// TestModelMenuCapsHeight is the same list in a tall window. The chip is
// still at the bottom, so the popup opens upward, and the cap is the
// fixed maximum rather than the window.
func TestModelMenuCapsHeight(t *testing.T) {
	a := manyModelApp(t, "gamma-08")
	tt := ui.NewTester(a.view, 900, 800)
	tt.Frame()
	if err := tt.Click("Model gamma-08"); err != nil {
		t.Fatal(err)
	}
	menu := mustFind(t, tt, "model menu")
	chip := mustFind(t, tt, "Model gamma-08")
	if menu.H < modelMenuMaxH-16 || menu.H > modelMenuMaxH+4 {
		t.Fatalf("menu height %.1f, want the cap %.0f", menu.H, modelMenuMaxH)
	}
	if menu.Y+menu.H > chip.Y+1 {
		t.Fatalf("tall window opened the menu below the chip: menu %.1f chip %.1f", menu.Y+menu.H, chip.Y)
	}
	if !modelCheckedVisible(t, tt, "gamma-08") {
		t.Fatal("selected model is not visible under the cap")
	}
	scrollMenu(t, tt, menu, 20000)
	if !modelVisible(t, tt, "Models & Providers…") {
		t.Fatal("footer is not reachable under the cap")
	}
}

// TestModelMenuKeyboardKeepsHighlightVisible moves the highlight with
// the arrows. The row it lands on stays inside the popup.
func TestModelMenuKeyboardKeepsHighlightVisible(t *testing.T) {
	tt := ui.NewTester(manyModelApp(t, "alpha-00").view, 900, 400)
	tt.Frame()
	if err := tt.Click("Model alpha-00"); err != nil {
		t.Fatal(err)
	}
	if !tt.Focused("Model alpha-00") {
		t.Fatal("model button did not take the keyboard")
	}
	for i := 0; i < 30; i++ {
		tt.Key(0, ui.KeyDown)
	}
	menu := mustFind(t, tt, "model menu")
	hot, ok := hotRow(t, tt)
	if !ok {
		t.Fatal("no highlighted row after Arrow Down")
	}
	if hot.label == "alpha-00" {
		t.Fatal("Arrow Down left the highlight on the first model")
	}
	if !rowOnScreen(menu, hot.rect) {
		t.Fatalf("highlight %q scrolled out of the menu %+v menu %+v", hot.label, hot.rect, menu)
	}
	for i := 0; i < 30; i++ {
		tt.Key(0, ui.KeyUp)
	}
	menu = mustFind(t, tt, "model menu")
	hot, ok = hotRow(t, tt)
	if !ok || hot.label != "alpha-00" || !rowOnScreen(menu, hot.rect) {
		t.Fatalf("Arrow Up did not bring the first model back into view: %+v", hot)
	}
	// Choosing with the keyboard is a click on the row the arrows reached.
	// Down again and confirm the highlight still tracks.
	for i := 0; i < 8; i++ {
		tt.Key(0, ui.KeyDown)
	}
	menu = mustFind(t, tt, "model menu")
	hot, ok = hotRow(t, tt)
	if !ok || !rowOnScreen(menu, hot.rect) {
		t.Fatalf("highlight left the menu %+v", hot)
	}
}

func manyModelApp(t *testing.T, selected string) *App {
	t.Helper()
	a := newTestApp(t)
	names := []struct{ name, prefix string }{
		{"Alpha", "alpha"},
		{"Beta", "beta"},
		{"Gamma", "gamma"},
		{"Delta", "delta"},
		{"Epsilon", "epsilon"},
		{"Zeta", "zeta"},
	}
	var conns []Connection
	var selectedConn Connection
	for _, n := range names {
		var models []string
		for i := 0; i < 16; i++ {
			models = append(models, n.prefix+"-"+twoDigits(i))
		}
		models = append(models, n.prefix+"-off")
		c := customConn("conn-"+n.prefix, n.name, "https://"+n.prefix+".example/v1", models...)
		c.Models[len(c.Models)-1].Enabled = false
		conns = append(conns, c)
		if strings.HasPrefix(selected, n.prefix+"-") {
			selectedConn = c
		}
		if err := a.keys.Set(c.ID, n.prefix+"-key"); err != nil {
			t.Fatal(err)
		}
	}
	a.data.Connections = conns
	when := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	cfg := Config{
		Provider: selectedConn.Name, Model: selected, ConnectionID: selectedConn.ID,
		BaseURL: selectedConn.BaseURL, SystemPrompt: DefaultConfig().SystemPrompt,
	}
	a.data.Defaults = cfg
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Relay", CreatedAt: when, UpdatedAt: when,
		Config:   cfg,
		Messages: []Message{{ID: "m", Role: roleUser, Content: "hi", Done: true, CreatedAt: when}},
	}}
	a.data.ActiveID = "c1"
	a.save()
	return a
}

func twoDigits(n int) string {
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

func rowOnScreen(menu, row ui.Rect) bool {
	if row.W < 8 || row.H < 8 {
		return false
	}
	top := row.Y
	if menu.Y > top {
		top = menu.Y
	}
	bot := row.Y + row.H
	if menu.Y+menu.H < bot {
		bot = menu.Y + menu.H
	}
	return bot-top >= 8
}

func scrollMenu(t *testing.T, tt *ui.Tester, menu ui.Rect, dy float32) {
	t.Helper()
	x, y := menu.X+menu.W/2, menu.Y+menu.H/2
	tt.Move(x, y)
	tt.Scroll(x, y, 0, dy)
}

func menuSignature(t *testing.T, tt *ui.Tester) string {
	t.Helper()
	menu := mustFind(t, tt, "model menu")
	var b strings.Builder
	for _, r := range menuRows(t, tt) {
		if rowOnScreen(menu, r.rect) && r.label != "" {
			b.WriteString(r.label)
			b.WriteByte('|')
		}
	}
	return b.String()
}

func modelVisible(t *testing.T, tt *ui.Tester, label string) bool {
	t.Helper()
	menu := mustFind(t, tt, "model menu")
	for _, r := range menuRows(t, tt) {
		if r.label == label && rowOnScreen(menu, r.rect) {
			return true
		}
	}
	return false
}

func modelCheckedVisible(t *testing.T, tt *ui.Tester, model string) bool {
	t.Helper()
	menu := mustFind(t, tt, "model menu")
	for _, r := range menuRows(t, tt) {
		if r.label == model && r.checked && rowOnScreen(menu, r.rect) {
			return true
		}
	}
	return false
}

func hotRow(t *testing.T, tt *ui.Tester) (menuRow, bool) {
	t.Helper()
	for _, r := range menuRows(t, tt) {
		if r.hot && !r.separator {
			return r, true
		}
	}
	return menuRow{}, false
}
