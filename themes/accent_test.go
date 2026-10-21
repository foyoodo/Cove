package themes

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestDerivedAccentsMatchTokens(t *testing.T) {
	raw, err := os.ReadFile("tokens.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	color := doc["color"].(map[string]any)
	accents := color["accents"].(map[string]any)
	keys := []string{
		"accent/default", "accent/hover", "accent/pressed", "accent/soft", "accent/ring",
		"selection/text", "text/link", "button/primaryBg", "button/primaryText",
	}
	for _, id := range AccentIDs() {
		block, ok := accents[id].(map[string]any)
		if !ok {
			t.Fatalf("missing accent %s", id)
		}
		src := block["source"].(map[string]any)
		for _, mode := range []struct {
			dark     bool
			focused  bool
			tokenKey string
			srcKey   string
		}{
			{false, true, "light", "light"},
			{true, true, "dark", "dark"},
			{false, false, "inactive-light", "light"},
			{true, false, "inactive-dark", "dark"},
		} {
			want, _ := block[mode.tokenKey].(map[string]any)
			gotAccent, ok := NamedAccent(id, mode.dark)
			if !ok {
				t.Fatal(id)
			}
			if ui.Hex(src[mode.srcKey].(string)) != gotAccent {
				t.Fatalf("%s %s source %v != %s", id, mode.srcKey, gotAccent, src[mode.srcKey])
			}
			got := Resolve(ThemeWarm, mode.dark, mode.focused, gotAccent)
			for _, key := range keys {
				have := colorByToken(got, key)
				rawHex, _ := want[key].(string)
				expect := ui.Hex(rawHex)
				// tokens.json was rounded with Python's half-to-even. MyGo's
				// Color.Mix rounds half up, so a channel can differ by 1.
				if !near(have, expect) {
					t.Errorf("%s %s %s: got #%02X%02X%02X%02X want %s", id, mode.tokenKey, key, have.R, have.G, have.B, have.A, rawHex)
				}
			}
		}
	}
}

func TestInactiveLayersMatchTokens(t *testing.T) {
	raw, err := os.ReadFile("tokens.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	tide := doc["color"].(map[string]any)["themes"].(map[string]any)["tide"].(map[string]any)
	inactive := tide["inactive"].(map[string]any)
	for _, mode := range []struct {
		dark bool
		key  string
	}{{false, "light"}, {true, "dark"}} {
		want := inactive[mode.key].(map[string]any)
		accent, _ := NamedAccent("blue", mode.dark)
		got := Resolve(ThemeTide, mode.dark, false, accent)
		for key, rawHex := range want {
			have := colorByToken(got, key)
			expect := ui.Hex(rawHex.(string))
			if !near(have, expect) {
				t.Errorf("inactive %s %s: got #%02X%02X%02X%02X want %s", mode.key, key, have.R, have.G, have.B, have.A, rawHex)
			}
		}
	}
}

func near(a, b ui.Color) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) <= 1 && d(a.G, b.G) <= 1 && d(a.B, b.B) <= 1 && d(a.A, b.A) <= 1
}

func colorByToken(c Colors, token string) ui.Color {
	switch token {
	case "accent/default":
		return c.AccentDefault
	case "accent/hover":
		return c.AccentHover
	case "accent/pressed":
		return c.AccentPressed
	case "accent/soft":
		return c.AccentSoft
	case "accent/ring":
		return c.AccentRing
	case "selection/row":
		return c.SelectionRow
	case "selection/text":
		return c.SelectionText
	case "text/link":
		return c.TextLink
	case "button/primaryBg":
		return c.ButtonPrimaryBg
	case "button/primaryText":
		return c.ButtonPrimaryText
	case "layer/sheet":
		return c.LayerSheet
	case "layer/overlay":
		return c.LayerOverlay
	case "layer/composer":
		return c.LayerComposer
	default:
		return ui.Color{}
	}
}
