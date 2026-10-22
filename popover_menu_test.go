package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"cove/themes"

	"github.com/egoist/mygo/ui"
)

func TestPopoverTokensMatchLayout(t *testing.T) {
	raw, err := os.ReadFile("themes/tokens.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	radius := doc["radius"].(map[string]any)
	if radius["radius/menu"].(float64) != float64(radiusMenu) {
		t.Fatalf("radius/menu %.0f, constant %v", radius["radius/menu"], radiusMenu)
	}
	menu := doc["layout"].(map[string]any)["menu"].(map[string]any)
	num := func(m map[string]any, key string) float64 { return m[key].(float64) }
	checks := []struct {
		got  float32
		want float64
		name string
	}{
		{menuPad, num(menu, "paddingY"), "paddingY"},
		{menuPad, num(menu, "paddingX"), "paddingX"},
		{menuMinW, num(menu, "minWidth"), "minWidth"},
		{menuMaxW, num(menu, "maxWidth"), "maxWidth"},
		{menuRowH, num(menu["row"].(map[string]any), "height"), "row.height"},
		{menuLead, num(menu["row"].(map[string]any), "leading"), "row.leading"},
		{menuCheck, num(menu["row"].(map[string]any), "checkColumn"), "row.checkColumn"},
		{menuIcon, num(menu["row"].(map[string]any), "icon"), "row.icon"},
		{menuIconGap, num(menu["row"].(map[string]any), "iconGap"), "row.iconGap"},
		{menuTrail, num(menu["row"].(map[string]any), "trailing"), "row.trailing"},
		{menuHeaderH, num(menu["groupHeader"].(map[string]any), "height"), "groupHeader.height"},
		{menuHeaderGap, num(menu["groupHeader"].(map[string]any), "gapAbove"), "groupHeader.gapAbove"},
		{menuDot, num(menu["groupHeader"].(map[string]any), "dot"), "groupHeader.dot"},
		{menuSepThick, num(menu["separator"].(map[string]any), "thickness"), "separator.thickness"},
		{menuSepInset, num(menu["separator"].(map[string]any), "inset"), "separator.inset"},
		{menuSepMargin, num(menu["separator"].(map[string]any), "margin"), "separator.margin"},
		{menuGap, num(menu["position"].(map[string]any), "gap"), "position.gap"},
		{menuWindowMargin, num(menu["position"].(map[string]any), "windowMargin"), "position.windowMargin"},
		{menuMaxH, num(menu["position"].(map[string]any), "maxHeight"), "position.maxHeight"},
		{float32(menuFadeIn / time.Millisecond), num(menu["motion"].(map[string]any), "fadeIn"), "motion.fadeIn"},
		{float32(menuFadeOut / time.Millisecond), num(menu["motion"].(map[string]any), "fadeOut"), "motion.fadeOut"},
		{menuSearchH, num(menu["search"].(map[string]any), "height"), "search.height"},
		{menuSearchIcon, num(menu["search"].(map[string]any), "icon"), "search.icon"},
		{float32(menuSearchAt), num(menu["search"].(map[string]any), "threshold"), "search.threshold"},
	}
	for _, c := range checks {
		if float64(c.got) != c.want {
			t.Errorf("%s: constant %v, token %v", c.name, c.got, c.want)
		}
	}
	if menuTextX != 29 {
		t.Fatalf("label starts at %v, want 29", menuTextX)
	}
}

func TestPopoverPlaceAndWidth(t *testing.T) {
	// Chip near the bottom of both reference windows. The menu opens
	// upward, stays 8pt inside the window, and caps at 360.
	for _, win := range [][2]float32{{720, 520}, {1120, 760}} {
		anchor := ui.Rect{X: 280, Y: win[1] - 36, W: 140, H: 22}
		limit, up := menuLimit(anchor, win[1])
		if !up {
			t.Fatalf("win %.0f: composer chip should open upward", win[0])
		}
		if limit > menuMaxH+0.1 {
			t.Fatalf("win %.0f: limit %.1f over 360", win[0], limit)
		}
		box := placeMenu(anchor, menuWidth(400, anchor.W), 500, win[0], win[1], up, limit)
		if box.W < menuMinW-0.1 || box.W > menuMaxW+0.1 {
			t.Fatalf("width %.1f", box.W)
		}
		if box.H > menuMaxH+0.1 || box.H > limit+0.1 {
			t.Fatalf("height %.1f limit %.1f", box.H, limit)
		}
		if box.X < menuWindowMargin-0.1 || box.Y < menuWindowMargin-0.1 ||
			box.X+box.W > win[0]-menuWindowMargin+0.1 || box.Y+box.H > win[1]-menuWindowMargin+0.1 {
			t.Fatalf("win %.0fx%.0f menu %+v leaves the 8pt margin", win[0], win[1], box)
		}
		if box.Y+box.H > anchor.Y-menuGap+1 {
			t.Fatalf("gap to anchor: menu bottom %.1f anchor %.1f", box.Y+box.H, anchor.Y)
		}
	}
	// A control near the top opens downward.
	anchor := ui.Rect{X: 40, Y: 20, W: 200, H: 22}
	_, up := menuLimit(anchor, 520)
	if up {
		t.Fatal("a control at the top should open downward")
	}
	if w := menuWidth(100, 40); w != menuMinW {
		t.Fatalf("narrow content width %v", w)
	}
	if w := menuWidth(220, 40); w != 220 {
		t.Fatalf("content width %v", w)
	}
	if w := menuWidth(100, 240); w != 240 {
		t.Fatalf("anchor width %v", w)
	}
	if w := menuWidth(800, 240); w != menuMaxW {
		t.Fatalf("clamped width %v", w)
	}

	// Left edges align when the menu fits.
	left := ui.Rect{X: 40, Y: 400, W: 120, H: 22}
	box := placeMenu(left, 180, 80, 720, 520, true, 360)
	if box.X != left.X {
		t.Fatalf("left-aligned menu x %.1f, anchor %.1f", box.X, left.X)
	}

	// Anchor near the right edge: align the menu's right edge with the
	// anchor's, the way Settings › Reasoning and the title-bar More button do.
	right := ui.Rect{X: 980, Y: 40, W: 120, H: 22}
	box = placeMenu(right, 180, 160, 1120, 760, false, 360)
	if box.X+box.W != right.X+right.W {
		t.Fatalf("right-aligned menu right %.1f, anchor right %.1f", box.X+box.W, right.X+right.W)
	}
	if box.X < menuWindowMargin {
		t.Fatalf("right-aligned menu crossed the left margin: %.1f", box.X)
	}

	// Neither edge can stay on the anchor and inside the window.
	wide := ui.Rect{X: 4, Y: 40, W: 190, H: 22}
	box = placeMenu(wide, 180, 80, 200, 400, false, 360)
	if box.X != menuWindowMargin {
		t.Fatalf("clamped menu x %.1f, want %v", box.X, menuWindowMargin)
	}
	if box.X+box.W > 200-menuWindowMargin+0.1 {
		t.Fatalf("clamped menu right %.1f", box.X+box.W)
	}
}

func TestPopoverFilterHighlightAndKeys(t *testing.T) {
	entries := []popEntry{
		{Kind: popHeader, Text: "OpenAI"},
		{Kind: popItem, ID: "oa/gpt-4o", Text: "gpt-4o", Group: "OpenAI"},
		{Kind: popItem, ID: "oa/gpt-4o-mini", Text: "gpt-4o-mini", Group: "OpenAI", Checked: true},
		{Kind: popHeader, Text: "Anthropic"},
		{Kind: popItem, ID: "an/claude-sonnet-4.5", Text: "claude-sonnet-4.5", Group: "Anthropic"},
		{Kind: popItem, ID: "an/claude-opus-4", Text: "claude-opus-4", Group: "Anthropic"},
		{Kind: popHeader, Text: "DeepSeek"},
		{Kind: popItem, ID: "ds/deepseek-chat", Text: "deepseek-chat", Group: "DeepSeek"},
	}
	got, empty := filterPopover(entries, "son")
	if empty || popSig(got) != "an/claude-sonnet-4.5\n" {
		t.Fatalf("son filter %+v empty %v", labelsOf(got), empty)
	}
	if popHighlight(got, "oa/gpt-4o-mini") != 0 {
		t.Fatal("current model is not in the son results, highlight should be the first row")
	}
	got, empty = filterPopover(entries, "GPT")
	if empty || popHighlight(got, "oa/gpt-4o-mini") != 1 {
		t.Fatalf("gpt highlight %d labels %v", popHighlight(got, "oa/gpt-4o-mini"), labelsOf(got))
	}
	got, empty = filterPopover(entries, "openai")
	if empty || len(selectable(got)) != 2 || got[0].Text != "OpenAI" {
		t.Fatalf("provider match %+v", labelsOf(got))
	}
	if _, empty = filterPopover(entries, "zzqx"); !empty {
		t.Fatal("unknown query still returned rows")
	}
	if _, empty = filterPopover(entries, ""); empty || len(selectable(filterAll(entries))) != 5 {
		t.Fatal("clearing the query should restore every row")
	}
	// Arrows stop at the ends.
	if movePop(4, 0, -1) != 0 || movePop(4, 3, 1) != 3 || movePop(4, 1, 1) != 2 {
		t.Fatal("arrow movement wrapped or skipped")
	}
	sel := selectable(entries)
	if typeaheadPop(sel, "h") != -1 {
		t.Fatal("no row starts with h")
	}
	eff := selectable(effortMenuEntries("Medium"))
	if i := typeaheadPop(eff, "h"); i < 0 || eff[i].Text != "High" {
		t.Fatalf("h -> %+v", eff)
	}
	if i := typeaheadPop(eff, "me"); i < 0 || eff[i].Text != "Medium" {
		t.Fatalf("me -> %+v", eff)
	}
	if i := typeaheadPop(eff, "m"); i < 0 || eff[i].Text != "Minimal" {
		t.Fatalf("m -> %+v", eff)
	}
}

func filterAll(entries []popEntry) []popEntry {
	out, _ := filterPopover(entries, "")
	return out
}

func labelsOf(entries []popEntry) []string {
	var out []string
	for _, e := range entries {
		out = append(out, e.Text)
	}
	return out
}

func TestPopoverModelMenuLayout(t *testing.T) {
	for _, win := range [][2]int{{720, 520}, {1120, 760}} {
		a := menuProtoApp(t)
		tt := ui.NewTester(a.view, win[0], win[1])
		tt.Frame()
		openModelMenu(t, tt)
		menu := mustFind(t, tt, "Model")
		chip := mustFind(t, tt, "Model claude-sonnet-4.5")
		if menu.H > menuMaxH+1 {
			t.Fatalf("%v menu height %.1f", win, menu.H)
		}
		if menu.W < menuMinW-1 || menu.W > menuMaxW+1 {
			t.Fatalf("%v menu width %.1f", win, menu.W)
		}
		if menu.X < 8-0.5 || menu.Y < 8-0.5 || menu.X+menu.W > float32(win[0])-8+0.5 || menu.Y+menu.H > float32(win[1])-8+0.5 {
			t.Fatalf("%v menu %+v is inside the 8pt margin", win, menu)
		}
		if menu.Y+menu.H > chip.Y-menuGap+1.5 {
			t.Fatalf("%v gap: menu bottom %.1f chip %.1f", win, menu.Y+menu.H, chip.Y)
		}
		if menu.X < chip.X-1 || menu.X > chip.X+1 {
			// The leading edges meet unless the menu was pushed in to
			// keep the window margin. This prototype chip is not on the
			// right edge, so they should match.
			t.Fatalf("%v leading edge menu %.1f chip %.1f", win, menu.X, chip.X)
		}
		rows := menuRows(t, tt)
		var sawModel bool
		for _, r := range rows {
			if r.separator || r.label == "" || strings.HasSuffix(r.label, "…") {
				continue
			}
			if r.on && r.rect.H > 16 && (r.rect.H < menuRowH-0.6 || r.rect.H > menuRowH+0.6) {
				t.Fatalf("%v row %q height %.1f", win, r.label, r.rect.H)
			}
			if !r.on && !r.checked && r.rect.H > 0 && r.rect.H < menuHeaderH-0.6 {
				t.Fatalf("%v header %q height %.1f", win, r.label, r.rect.H)
			}
			if r.label == "gpt-4o" && r.on {
				sawModel = true
				text := narrowText(t, tt, "gpt-4o")
				if text.X < menu.X+menuTextX-1.5 || text.X > menu.X+menuTextX+1.5 {
					t.Fatalf("%v gpt-4o text x %.1f, menu x %.1f, want start %v", win, text.X, menu.X, menuTextX)
				}
			}
			if r.label == "OpenAI" && !r.on {
				text := narrowText(t, tt, "OpenAI")
				if text.X < menu.X+menuTextX-1.5 || text.X > menu.X+menuTextX+1.5 {
					t.Fatalf("%v header text x %.1f menu x %.1f", win, text.X, menu.X)
				}
			}
		}
		if !sawModel {
			t.Fatalf("%v rows %+v", win, rows)
		}
		// The 5pt padding stays put: the first header is below the search
		// block, and the footer is inside the menu, above the bottom pad.
		footer := rowByLabel(rows, "Models & Providers…")
		if footer.rect.Y+footer.rect.H > menu.Y+menu.H-menuPad+1 {
			t.Fatalf("%v footer %.1f touches the bottom of %.1f", win, footer.rect.Y+footer.rect.H, menu.Y+menu.H)
		}
		header := rowByLabel(rows, "OpenAI")
		if header.rect.Y < menu.Y+menuPad+menuSearchH {
			t.Fatalf("%v first header %.1f is under the search field", win, header.rect.Y)
		}
		before := footer.rect.Y
		scrollMenu(t, tt, menu, 400)
		footer = rowByLabel(menuRows(t, tt), "Models & Providers…")
		if footer.rect.Y < before-1 || footer.rect.Y > before+1 {
			t.Fatalf("%v footer scrolled from %.1f to %.1f", win, before, footer.rect.Y)
		}
	}
}

func TestPopoverKeyboardSearchAndClose(t *testing.T) {
	a := menuProtoApp(t)
	a.data.Conversations = append(a.data.Conversations, Conversation{
		ID: "other", Title: "Other chat", CreatedAt: a.data.Conversations[0].CreatedAt, UpdatedAt: a.data.Conversations[0].UpdatedAt,
		Config:   a.data.Conversations[0].Config,
		Messages: []Message{{ID: "u2", Role: roleUser, Content: "second", Done: true}},
	})
	tt := ui.NewTester(a.view, 1120, 760)
	tt.Frame()
	openModelMenu(t, tt)
	rows := menuRows(t, tt)
	hot := hotLabeled(rows)
	if hot.label != "claude-sonnet-4.5" || !hot.checked {
		t.Fatalf("open highlight %+v", hot)
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	hot = hotLabeled(menuRows(t, tt))
	if hot.label == "claude-sonnet-4.5" || hot.checked {
		t.Fatalf("highlight stayed on the checked row %+v", hot)
	}
	// Current model is still only a check once the highlight moves.
	if on, ok := modelChecked(menuRows(t, tt), "Anthropic", "claude-sonnet-4.5"); !ok || !on {
		t.Fatal("check left the current model")
	}
	tt.Type("son")
	rows = menuRows(t, tt)
	hot = hotLabeled(rows)
	if hot.label != "claude-sonnet-4.5" {
		t.Fatalf("current model should stay highlighted %+v rows %v", hot, modelMenuLabels(t, tt))
	}
	if headerRow(rows, "OpenAI") || headerRow(rows, "OpenRouter") {
		t.Fatalf("unmatched groups stayed %v", modelMenuLabels(t, tt))
	}
	if !headerRow(rows, "DeepSeek") {
		t.Fatal("deepseek-reasoner contains son and its group should stay")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Key(0, ui.KeyEscape)
	if modelMenuOpen(tt) {
		t.Fatal("menu stayed open before the effort menu")
	}
	if err := tt.Click("Reasoning: Medium"); err != nil {
		t.Fatalf("%v texts %q", err, tt.Texts())
	}
	tt.Key(0, ui.KeyH)
	if got := hotLabeled(effortMarks(t, tt)); got.label != "High" {
		t.Fatalf("h highlight %+v", got)
	}
	tt.Key(0, ui.KeyEnd)
	tt.Key(0, ui.KeyDown)
	if got := hotLabeled(effortMarks(t, tt)); got.label != "High" {
		t.Fatalf("Down wrapped to %+v", got)
	}
	tt.Key(0, ui.KeyHome)
	tt.Key(0, ui.KeyUp)
	if got := hotLabeled(effortMarks(t, tt)); got.label != "Off" {
		t.Fatalf("Up wrapped to %+v", got)
	}
	tt.Key(0, ui.KeyM)
	tt.Key(0, ui.KeyE)
	if got := hotLabeled(effortMarks(t, tt)); got.label != "Medium" {
		t.Fatalf("me highlight %+v", got)
	}
	tt.Key(0, ui.KeyEscape)
	if effortOpen(tt) {
		t.Fatal("Esc left the effort menu open")
	}

	openModelMenu(t, tt)
	tt.Type("gpt")
	rows = menuRows(t, tt)
	hot = hotLabeled(rows)
	if hot.label != "gpt-4o" {
		t.Fatalf("current model is not in the gpt results, want the first %+v rows %v", hot, modelMenuLabels(t, tt))
	}
	chooseGroupedModel(t, tt, "OpenAI", "gpt-4o-mini")
	openModelMenu(t, tt)
	tt.Type("gpt")
	hot = hotLabeled(menuRows(t, tt))
	if hot.label != "gpt-4o-mini" {
		t.Fatalf("current gpt-4o-mini should be the highlight %+v", hot)
	}
	tt.Key(0, ui.KeyEscape)
	if !modelMenuOpen(tt) {
		t.Fatal("first Esc closed the menu instead of clearing the query")
	}
	if !tt.HasText("claude-sonnet-4.5") || !headerRow(menuRows(t, tt), "Anthropic") {
		t.Fatalf("cleared query did not restore groups %v", modelMenuLabels(t, tt))
	}
	hot = hotLabeled(menuRows(t, tt))
	if hot.label != "gpt-4o-mini" {
		t.Fatalf("cleared highlight %+v", hot)
	}
	tt.Type("zzqx")
	if !tt.HasText("No models") {
		t.Fatalf("empty query text %v", modelMenuLabels(t, tt))
	}
	if !modelVisible(t, tt, "Models & Providers…") {
		t.Fatal("footer hid with the empty filter")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Key(0, ui.KeyEscape)
	if modelMenuOpen(tt) {
		t.Fatal("second Esc left the menu open")
	}

	// Clicking outside closes the menu and does not activate the row under it.
	openModelMenu(t, tt)
	other := mustFind(t, tt, "Other chat")
	before := a.data.ActiveID
	tt.ClickAt(other.X+other.W/2, other.Y+other.H/2)
	if modelMenuOpen(tt) {
		t.Fatal("click outside left the menu open")
	}
	if a.data.ActiveID != before {
		t.Fatalf("outside click switched to %s", a.data.ActiveID)
	}

	openModelMenu(t, tt)
	tt.SetFocused(false)
	tt.Frame()
	if modelMenuOpen(tt) {
		t.Fatal("blur left the menu open")
	}
	tt.SetFocused(true)
	tt.Frame()
	if modelMenuOpen(tt) {
		t.Fatal("refocus reopened the menu")
	}

	openModelMenu(t, tt)
	tt.SetSize(1000, 700)
	tt.Frame()
	if modelMenuOpen(tt) {
		t.Fatal("resize left the menu open")
	}
}

func TestPopoverAccessibility(t *testing.T) {
	a := menuProtoApp(t)
	tt := ui.NewTester(a.view, 1120, 760)
	tt.Frame()
	rt := reflectField(reflect.ValueOf(tt).Elem(), "rt")
	reflectField(rt, "access").SetBool(true)
	openModelMenu(t, tt)
	h := reflectField(reflect.ValueOf(tt).Elem(), "h")
	ann := reflectField(h, "announced")
	var said bool
	var heard []string
	for i := 0; i < ann.Len(); i++ {
		heard = append(heard, ann.Index(i).String())
		if ann.Index(i).String() == "Model" {
			said = true
		}
	}
	if !said {
		t.Fatalf("opening the menu did not announce Model: %v", heard)
	}
	tree := reflectField(h, "access")
	if tree.IsNil() {
		t.Fatal("accessibility tree was not built")
	}
	nodes := tree.Elem().FieldByName("Nodes")
	var menu, group, checked bool
	for i := 0; i < nodes.Len(); i++ {
		n := nodes.Index(i)
		label := n.FieldByName("Label").String()
		role := n.FieldByName("Role").Uint()
		states := n.FieldByName("States").Uint()
		switch {
		case label == "Model" && role == 38:
			menu = true
		case label == "OpenAI" && role == 0:
			group = true
		case label == "claude-sonnet-4.5" && role == 41 && states&4 != 0:
			checked = true
		}
	}
	if !menu || !group || !checked {
		t.Fatalf("menu %v group %v checked %v", menu, group, checked)
	}
}

func TestPopoverOpenHighlight(t *testing.T) {
	a := menuProtoApp(t)
	tt := ui.NewTester(a.view, 1120, 760)
	tt.Frame()

	// Pointer open of a menu with no current item: nothing is highlighted.
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	if hot := hotLabeled(kindMarks(t, tt, "more")); hot.label != "" {
		t.Fatalf("mouse-opened More highlighted %q", hot.label)
	}
	tt.Key(0, ui.KeyDown)
	if hot := hotLabeled(kindMarks(t, tt, "more")); hot.label != "Chat Settings…" {
		t.Fatalf("first Down %+v", hot)
	}
	tt.Key(0, ui.KeyEscape)
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyUp)
	if hot := hotLabeled(kindMarks(t, tt, "more")); hot.label != "Last request" {
		t.Fatalf("first Up %+v", hot)
	}
	// Return while nothing is highlighted does not choose.
	tt.Key(0, ui.KeyEscape)
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyEnter)
	if !moreOpen(tt) {
		t.Fatal("Return with no highlight closed More")
	}
	if a.modalOpen || a.settingsOpen {
		t.Fatal("Return with no highlight selected Chat Settings")
	}
	tt.Key(0, ui.KeyEscape)

	// Keyboard open (anchor focused, then Down or Space) highlights the first row.
	if !tt.Focused("More") {
		t.Fatal("Esc did not return focus to More")
	}
	tt.Key(0, ui.KeyDown)
	if hot := hotLabeled(kindMarks(t, tt, "more")); hot.label != "Chat Settings…" {
		t.Fatalf("Down-opened More %+v", hot)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Key(0, ui.KeySpace)
	if hot := hotLabeled(kindMarks(t, tt, "more")); hot.label != "Chat Settings…" {
		t.Fatalf("Space-opened More %+v", hot)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Key(0, ui.KeyEnter)
	if hot := hotLabeled(kindMarks(t, tt, "more")); hot.label != "Chat Settings…" {
		t.Fatalf("Return-opened More %+v", hot)
	}
	tt.Key(0, ui.KeyEscape)

	// A menu with a current item still opens on that item.
	openModelMenu(t, tt)
	if hot := hotLabeled(menuRows(t, tt)); hot.label != "claude-sonnet-4.5" || !hot.checked {
		t.Fatalf("model menu highlight %+v", hot)
	}
}

func TestPopoverAnchorEdge(t *testing.T) {
	a := menuProtoApp(t)
	tt := ui.NewTester(a.view, 1120, 760)
	tt.Frame()
	more := mustFind(t, tt, "More")
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	menu := tallestLabel(t, tt, "More")
	if menu.H < 40 {
		t.Fatal("More menu did not open")
	}
	if abs32(menu.X+menu.W-(more.X+more.W)) > 1.5 {
		t.Fatalf("More menu right %.1f, button right %.1f (left %.1f vs %.1f)", menu.X+menu.W, more.X+more.W, menu.X, more.X)
	}
	if menu.X+menu.W > float32(1120)-menuWindowMargin-2 {
		t.Fatalf("1120 More menu hugs the window: right %.1f button %.1f", menu.X+menu.W, more.X+more.W)
	}
	tt.Key(0, ui.KeyEscape)

	// On a 720-wide window the Reasoning control sits against the card's
	// right edge, so a left-aligned menu would cross the window margin.
	b := menuProtoApp(t)
	tb := ui.NewTester(b.view, 720, 520)
	tb.Frame()
	b.settingsOpen = true
	b.settingsSection = "general"
	tb.Frame()
	btn := rightmostLabel(t, tb, "Reasoning")
	if err := tb.Click("Medium"); err != nil {
		t.Fatal(err)
	}
	menu = tallestLabel(t, tb, "Reasoning")
	if menu.H < 40 {
		t.Fatal("reasoning menu did not open")
	}
	if abs32(menu.X+menu.W-(btn.X+btn.W)) > 1.5 {
		t.Fatalf("reasoning menu right %.1f, button right %.1f", menu.X+menu.W, btn.X+btn.W)
	}
	if btn.X+btn.W < 676 || btn.X+btn.W > 686 {
		t.Fatalf("reasoning button right %.1f, want about 681", btn.X+btn.W)
	}
	if menu.X+menu.W > float32(720)-menuWindowMargin-2 {
		t.Fatalf("settings menu hugs the window: right %.1f", menu.X+menu.W)
	}

	// At 720 the More button's right edge is the window minus the 12pt
	// title-bar padding (~708). Parking the menu on the window margin
	// would put it at 712.
	c := menuProtoApp(t)
	tc := ui.NewTester(c.view, 720, 520)
	tc.Frame()
	more720 := mustFind(t, tc, "More")
	if err := tc.Click("More"); err != nil {
		t.Fatal(err)
	}
	menu = tallestLabel(t, tc, "More")
	if abs32(menu.X+menu.W-(more720.X+more720.W)) > 1.5 {
		t.Fatalf("720 More menu right %.1f, button right %.1f", menu.X+menu.W, more720.X+more720.W)
	}
	if more720.X+more720.W < 704 || more720.X+more720.W > 712 {
		t.Fatalf("720 More button right %.1f, want about 708", more720.X+more720.W)
	}
	if menu.X+menu.W > float32(720)-menuWindowMargin-2 {
		t.Fatalf("720 More menu hugs the window: right %.1f", menu.X+menu.W)
	}
	// Mouse open, nothing current: no row is highlighted.
	for _, row := range kindMarks(t, tc, "more") {
		if row.hot {
			t.Fatalf("mouse-opened More highlighted %q", row.label)
		}
	}
}

func TestPopoverSeparatorInsets(t *testing.T) {
	openers := []struct {
		name string
		open func(t *testing.T, a *App, tt *ui.Tester)
		menu string
	}{
		{"model", func(t *testing.T, a *App, tt *ui.Tester) { openModelMenu(t, tt) }, "Model"},
		{"effort", func(t *testing.T, a *App, tt *ui.Tester) {
			if err := tt.Click("Reasoning: Medium"); err != nil {
				t.Fatal(err)
			}
		}, "Reasoning"},
		{"more", func(t *testing.T, a *App, tt *ui.Tester) {
			if err := tt.Click("More"); err != nil {
				t.Fatal(err)
			}
		}, "More"},
		{"settings", func(t *testing.T, a *App, tt *ui.Tester) { openSettingsReasoning(t, a, tt) }, "Reasoning"},
	}
	for _, win := range [][2]int{{720, 520}, {1120, 760}} {
		for _, op := range openers {
			a := menuProtoApp(t)
			tt := ui.NewTester(a.view, win[0], win[1])
			tt.Frame()
			op.open(t, a, tt)
			menu := tallestLabel(t, tt, op.menu)
			if menu.H < 40 {
				t.Fatalf("%s %v menu did not open: %+v", op.name, win, menu)
			}
			var seps []ui.Rect
			for _, lab := range menuLabels(t, tt) {
				kind, _, _, _, _, _, ok := parseModelMenuMark(lab.text)
				if ok && kind == "sep" && lab.r.W > 1 && lab.r.H > 0 {
					seps = append(seps, lab.r)
				}
			}
			if len(seps) == 0 {
				t.Fatalf("%s %v has no separator", op.name, win)
			}
			for _, s := range seps {
				left := s.X - menu.X
				right := menu.X + menu.W - (s.X + s.W)
				if abs32(left-menuSepInset) > 1 || abs32(right-menuSepInset) > 1 {
					t.Fatalf("%s %v separator insets left %.2f right %.2f, want %v (line %+v menu %+v)", op.name, win, left, right, menuSepInset, s, menu)
				}
			}
		}
	}
}

func TestPopoverDarkAccent(t *testing.T) {
	want, ok := themes.NamedAccent("blue", true)
	if !ok {
		t.Fatal("no dark blue")
	}
	light := themes.AccentFallback()
	a := menuProtoApp(t)
	a.data.Appearance = appearanceDark
	tt := ui.NewTester(a.view, 1120, 760)
	tt.SetDark(true)
	tt.SetScale(2)
	tt.Frame()
	if a.look.AccentDefault != want {
		t.Fatalf("menu palette accent %+v, want dark system blue %+v", a.look.AccentDefault, want)
	}
	openModelMenu(t, tt)
	hot := hotLabeled(menuRows(t, tt))
	if hot.label == "" || !hot.checked {
		t.Fatalf("open highlight %+v", hot)
	}
	img := tt.Image()
	px := img.RGBAAt(int((hot.rect.X+hot.rect.W/2)*2+0.5), int((hot.rect.Y+hot.rect.H/2)*2+0.5))
	if px.R != want.R || px.G != want.G || px.B != want.B {
		t.Fatalf("dark highlight pixel R%v G%v B%v, want R%v G%v B%v (light blue is R%v G%v B%v)", px.R, px.G, px.B, want.R, want.G, want.B, light.R, light.G, light.B)
	}
	tt.Key(0, ui.KeyDown)
	img = tt.Image()
	var checked menuRow
	for _, r := range menuRows(t, tt) {
		if r.checked && !r.hot {
			checked = r
			break
		}
	}
	if checked.label == "" {
		t.Fatal("checked row is still the highlight")
	}
	menu := mustFind(t, tt, "Model")
	cx := menu.X + menuPad + menuLead + menuCheck/2
	cy := checked.rect.Y + checked.rect.H/2
	best := 1 << 20
	for dy := -6; dy <= 6; dy++ {
		for dx := -6; dx <= 6; dx++ {
			p := img.RGBAAt(int((cx+float32(dx))*2+0.5), int((cy+float32(dy))*2+0.5))
			d := absInt(int(p.R)-int(want.R)) + absInt(int(p.G)-int(want.G)) + absInt(int(p.B)-int(want.B))
			if d < best {
				best = d
			}
		}
	}
	if best > 30 {
		t.Fatalf("checkmark is not the dark system blue (nearest channel distance %d)", best)
	}
}

// A reported system accent is accent/default. Blue is only the fallback
// used when Preferences().Accent cannot be read. The desktop supplies the
// mode's own source color (purple's dark variant in dark mode); the
// palette uses that color as the accent rather than remixing it.
func TestPopoverFollowsSystemAccent(t *testing.T) {
	dir := testArtifactDir(t, "popover-menu-r2")
	for _, dark := range []bool{false, true} {
		purple, ok := themes.NamedAccent("purple", dark)
		if !ok {
			t.Fatal("no purple accent")
		}
		blue, _ := themes.NamedAccent("blue", dark)
		mode := "light"
		if dark {
			mode = "dark"
		}
		for _, win := range [][2]int{{720, 520}, {1120, 760}} {
			a := menuProtoApp(t)
			if dark {
				a.data.Appearance = appearanceDark
			} else {
				a.data.Appearance = appearanceLight
			}
			tt := ui.NewTester(a.view, win[0], win[1])
			tt.SetScale(2)
			tt.SetDark(dark)
			tt.SetPreferences(ui.Preferences{Accent: purple})
			tt.Frame()
			if a.look.AccentDefault != purple {
				t.Fatalf("%s palette accent %+v, want system purple %+v", mode, a.look.AccentDefault, purple)
			}
			if a.look.AccentDefault == blue || a.look.AccentDefault == themes.AccentFallback() {
				t.Fatalf("%s system purple was replaced by blue %+v", mode, a.look.AccentDefault)
			}
			openModelMenu(t, tt)
			hot := hotLabeled(menuRows(t, tt))
			if hot.label == "" || !hot.checked {
				t.Fatalf("%s %v open highlight %+v", mode, win, hot)
			}
			img := tt.Image()
			px := img.RGBAAt(int((hot.rect.X+hot.rect.W/2)*2+0.5), int((hot.rect.Y+hot.rect.H/2)*2+0.5))
			if px.R != purple.R || px.G != purple.G || px.B != purple.B {
				t.Fatalf("%s %v highlight R%v G%v B%v, want R%v G%v B%v", mode, win, px.R, px.G, px.B, purple.R, purple.G, purple.B)
			}
			name := "model-menu-open-purple-" + strconv.Itoa(win[0]) + "x" + strconv.Itoa(win[1]) + "-" + mode + "@2x.png"
			writePNG(t, filepath.Join(dir, name), img)
			tt.Key(0, ui.KeyDown)
			img = tt.Image()
			var checked menuRow
			for _, r := range menuRows(t, tt) {
				if r.checked && !r.hot {
					checked = r
					break
				}
			}
			if checked.label == "" {
				t.Fatalf("%s %v checked row is still the highlight", mode, win)
			}
			menu := mustFind(t, tt, "Model")
			cx := menu.X + menuPad + menuLead + menuCheck/2
			cy := checked.rect.Y + checked.rect.H/2
			best := 1 << 20
			for dy := -6; dy <= 6; dy++ {
				for dx := -6; dx <= 6; dx++ {
					p := img.RGBAAt(int((cx+float32(dx))*2+0.5), int((cy+float32(dy))*2+0.5))
					d := absInt(int(p.R)-int(purple.R)) + absInt(int(p.G)-int(purple.G)) + absInt(int(p.B)-int(purple.B))
					if d < best {
						best = d
					}
				}
			}
			if best > 30 {
				t.Fatalf("%s %v checkmark is not the system purple (nearest channel distance %d)", mode, win, best)
			}
		}
	}
}

func TestPopoverScreenshots(t *testing.T) {
	dir := testArtifactDir(t, "popover-menu-r2")
	shots := []struct {
		name string
		open func(t *testing.T, a *App, tt *ui.Tester)
		app  func(t *testing.T) *App
	}{
		{"model-menu-open", openModelMenuUntouched, menuProtoApp},
		{"model-menu", openProtoHighlight, menuProtoApp},
		{"model-menu-search", openProtoSearch, menuProtoApp},
		{"model-menu-empty", openProtoEmpty, menuProtoApp},
		{"model-menu-scrolled", openScrolledModel, func(t *testing.T) *App { return manyModelApp(t, "gamma-08") }},
		{"effort-menu", openEffortHigh, menuProtoApp},
		{"more-menu", openMoreMenu, menuProtoApp},
		{"settings-menu", openSettingsReasoning, menuProtoApp},
	}
	for _, shot := range shots {
		for _, win := range [][2]int{{720, 520}, {1120, 760}} {
			for _, dark := range []bool{false, true} {
				a := shot.app(t)
				if dark {
					a.data.Appearance = appearanceDark
				} else {
					a.data.Appearance = appearanceLight
				}
				tt := ui.NewTester(a.view, win[0], win[1])
				tt.SetScale(2)
				tt.SetDark(dark)
				tt.Frame()
				shot.open(t, a, tt)
				mode := "light"
				if dark {
					mode = "dark"
				}
				name := shot.name + "-" + strconv.Itoa(win[0]) + "x" + strconv.Itoa(win[1]) + "-" + mode + "@2x.png"
				writePNG(t, filepath.Join(dir, name), tt.Image())
			}
		}
	}
}

func openModelMenu(t *testing.T, tt *ui.Tester) {
	t.Helper()
	labels := []string{
		"Model claude-sonnet-4.5",
		"Model gpt-4o-mini",
		"Model OpenAI · gpt-4o-mini",
		"Model gpt-4o",
		"Model gamma-08",
	}
	var last error
	for _, label := range labels {
		if err := tt.Click(label); err == nil {
			last = nil
			break
		} else {
			last = err
		}
	}
	if last != nil {
		t.Fatal(last)
	}
	if !modelMenuOpen(tt) {
		t.Fatal("model menu did not open")
	}
}

func openModelMenuUntouched(t *testing.T, a *App, tt *ui.Tester) {
	t.Helper()
	openModelMenu(t, tt)
}

func openProtoHighlight(t *testing.T, a *App, tt *ui.Tester) {
	t.Helper()
	openModelMenu(t, tt)
	tt.Key(0, ui.KeyUp)
	tt.Key(0, ui.KeyUp)
}

func openProtoSearch(t *testing.T, a *App, tt *ui.Tester) {
	t.Helper()
	openModelMenu(t, tt)
	tt.Type("son")
}

func openProtoEmpty(t *testing.T, a *App, tt *ui.Tester) {
	t.Helper()
	openModelMenu(t, tt)
	tt.Type("zzqx")
}

func openScrolledModel(t *testing.T, a *App, tt *ui.Tester) {
	t.Helper()
	if err := tt.Click("Model gamma-08"); err != nil {
		t.Fatal(err)
	}
}

func openEffortHigh(t *testing.T, a *App, tt *ui.Tester) {
	t.Helper()
	if err := tt.Click("Reasoning: Medium"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyH)
}

func openMoreMenu(t *testing.T, a *App, tt *ui.Tester) {
	t.Helper()
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
}

func openSettingsReasoning(t *testing.T, a *App, tt *ui.Tester) {
	t.Helper()
	a.settingsOpen = true
	a.settingsSection = "general"
	tt.Frame()
	// The row title is also "Reasoning". The closed control's value is the
	// clickable text.
	if err := tt.Click("Medium"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("High"); !ok {
		t.Fatal("settings reasoning menu did not open")
	}
	tt.Key(0, ui.KeyDown)
}

func menuProtoApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	conns := []Connection{
		protoConn("openai", "OpenAI", "OpenAI", "https://api.openai.com/v1", "gpt-4o", "gpt-4o-mini", "o3-mini"),
		protoConn("anthropic", "Anthropic", "Anthropic", "https://api.anthropic.com/v1", "claude-sonnet-4.5", "claude-opus-4"),
		protoConn("deepseek", "DeepSeek", "DeepSeek", "https://api.deepseek.com/v1", "deepseek-chat", "deepseek-reasoner"),
		protoConn("openrouter", "OpenRouter", "OpenRouter", "https://openrouter.ai/api/v1", "openrouter/auto", "openai/gpt-4o-mini", "anthropic/claude-3.5"),
	}
	a.data.Connections = conns
	for _, c := range conns {
		if err := a.keys.Set(c.ID, "sk-"+c.ID); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 10, 22, 9, 41, 0, 0, time.UTC)
	cfg := Config{
		Provider: "Anthropic", Model: "claude-sonnet-4.5", ConnectionID: "anthropic",
		BaseURL: "https://api.anthropic.com/v1", Reasoning: effortMedium,
		SystemPrompt: DefaultConfig().SystemPrompt,
	}
	a.data.Defaults = cfg
	a.data.Conversations = []Conversation{{
		ID: "c1", Title: "Cancel HTTP on close", CreatedAt: now, UpdatedAt: now,
		Config: cfg,
		Messages: []Message{
			{ID: "u", Role: roleUser, Content: "How do I cancel an in-flight HTTP request when the window closes?", Done: true, CreatedAt: now},
			{ID: "a", Role: roleAssistant, Content: "Tie the request to a context.\n\nBuild the request with NewRequestWithContext and cancel that context when the window closes.", Done: true, CreatedAt: now},
		},
	}}
	a.data.ActiveID = "c1"
	a.save()
	return a
}

func protoConn(id, name, kind, base string, models ...string) Connection {
	c := customConn(id, name, base, models...)
	c.Provider = kind
	c.Custom = false
	return c
}

func effortMarks(t *testing.T, tt *ui.Tester) []menuRow {
	t.Helper()
	return kindMarks(t, tt, "effort")
}

func kindMarks(t *testing.T, tt *ui.Tester, kind string) []menuRow {
	t.Helper()
	var out []menuRow
	for _, lab := range menuLabels(t, tt) {
		got, _, model, enabled, checked, hot, ok := parseModelMenuMark(lab.text)
		if !ok || got != kind {
			continue
		}
		out = append(out, menuRow{label: model, on: enabled, checked: checked, hot: hot, rect: lab.r})
	}
	return out
}

func moreOpen(tt *ui.Tester) bool {
	_, ok := tt.Find("Chat Settings…")
	return ok
}

func tallestLabel(t *testing.T, tt *ui.Tester, s string) ui.Rect {
	t.Helper()
	var best ui.Rect
	var found bool
	for _, lab := range menuLabels(t, tt) {
		if lab.text != s {
			continue
		}
		if !found || lab.r.H > best.H {
			best = lab.r
			found = true
		}
	}
	if !found {
		t.Fatalf("no label %q", s)
	}
	return best
}

func rightmostLabel(t *testing.T, tt *ui.Tester, s string) ui.Rect {
	t.Helper()
	var best ui.Rect
	var found bool
	for _, lab := range menuLabels(t, tt) {
		if lab.text != s || lab.r.W < 1 {
			continue
		}
		if !found || lab.r.X > best.X {
			best = lab.r
			found = true
		}
	}
	if !found {
		t.Fatalf("no label %q", s)
	}
	return best
}

func effortOpen(tt *ui.Tester) bool {
	_, ok := tt.Find("Reasoning")
	if !ok {
		return false
	}
	for _, row := range []string{"Off", "Default", "Minimal", "Low", "Medium", "High"} {
		if _, ok := tt.Find(row); ok {
			return true
		}
	}
	return false
}

func hotLabeled(rows []menuRow) menuRow {
	for _, r := range rows {
		if r.hot && !r.separator {
			return r
		}
	}
	return menuRow{}
}

func narrowText(t *testing.T, tt *ui.Tester, s string) ui.Rect {
	t.Helper()
	var best ui.Rect
	var found bool
	for _, lab := range menuLabels(t, tt) {
		if lab.text != s || lab.r.W < 1 {
			continue
		}
		if !found || lab.r.W < best.W {
			best = lab.r
			found = true
		}
	}
	if !found {
		t.Fatalf("no text %q", s)
	}
	return best
}

func rowByLabel(rows []menuRow, label string) menuRow {
	for _, r := range rows {
		if r.label == label {
			return r
		}
	}
	return menuRow{}
}
