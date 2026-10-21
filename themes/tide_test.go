package themes

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestTideMatchesTokens(t *testing.T) {
	raw, err := os.ReadFile("tokens.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	color, _ := doc["color"].(map[string]any)
	themesDoc, _ := color["themes"].(map[string]any)
	tide, _ := themesDoc["tide"].(map[string]any)
	if tide == nil {
		t.Fatal("tokens.json has no color.themes.tide")
	}
	for _, mode := range []string{"light", "dark"} {
		got := Tide(mode == "dark")
		want, _ := tide[mode].(map[string]any)
		if want == nil {
			t.Fatalf("missing %s palette", mode)
		}
		seen := map[string]bool{}
		v := reflect.ValueOf(got)
		typ := v.Type()
		for i := 0; i < typ.NumField(); i++ {
			tag := typ.Field(i).Tag.Get("token")
			if tag == "" {
				t.Fatalf("field %s has no token tag", typ.Field(i).Name)
			}
			seen[tag] = true
			rawHex, ok := want[tag].(string)
			if !ok {
				t.Errorf("%s: field %s token %q is not in tokens.json", mode, typ.Field(i).Name, tag)
				continue
			}
			have := v.Field(i).Interface().(ui.Color)
			expect := ui.Hex(rawHex)
			if have != expect {
				t.Errorf("%s %s: got %v want %s", mode, tag, have, rawHex)
			}
		}
		for key := range want {
			if strings.HasPrefix(key, "$") {
				continue
			}
			if !seen[key] {
				t.Errorf("%s: tokens.json color %q has no Theme field", mode, key)
			}
		}
	}
	if _, ok := doc["$future"]; ok {
		// Future accents stay out of the Tide palette. The test above
		// only walks color.themes.tide, so a future accent cannot satisfy it.
	}
	if TideLight().AccentDefault == TideLight().ThemeMark {
		t.Fatal("light accent and tide mark must differ")
	}
	if TideDark().AccentDefault == TideDark().ThemeMark {
		t.Fatal("dark accent and tide mark must differ")
	}
	warm := Palette(ThemeWarm, false)
	if warm.ThemeMark != ui.Hex("#C2603F") || warm.SelectionRow != ui.Hex("#C2603F29") {
		t.Fatalf("warm light mark %+v selection %+v", warm.ThemeMark, warm.SelectionRow)
	}
	warmDark := Palette(ThemeWarm, true)
	if warmDark.ThemeMark != ui.Hex("#F0997A") || warmDark.SelectionRow != ui.Hex("#F0997A33") {
		t.Fatalf("warm dark mark %+v selection %+v", warmDark.ThemeMark, warmDark.SelectionRow)
	}
	if _, ok := palettes["tide"]; !ok {
		loadTokens()
	}
	for _, id := range []string{ThemeWarm, ThemeTide, ThemeNeutral, ThemeMist, ThemeSage, ThemeLavender} {
		if _, ok := palettes[id]; !ok {
			t.Fatalf("missing preset %s", id)
		}
	}
}
