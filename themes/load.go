package themes

import (
	_ "embed"
	"encoding/json"
	"reflect"
	"sync"

	"github.com/egoist/mygo/ui"
)

//go:embed tokens.json
var tokensJSON []byte

// Theme IDs match color.themes in tokens.json. Graphite's id is neutral.
const (
	ThemeWarm     = "warm"
	ThemeTide     = "tide"
	ThemeNeutral  = "neutral"
	ThemeMist     = "mist"
	ThemeSage     = "sage"
	ThemeLavender = "lavender"
	ThemeDefault  = ThemeWarm
)

// Preset is one appearance theme: a window gradient plus a mark color.
type Preset struct {
	ID   string
	Name string
}

// Presets is the picker order. Warm is the default.
func Presets() []Preset {
	return []Preset{
		{ThemeWarm, "Warm"},
		{ThemeTide, "Tide"},
		{ThemeNeutral, "Graphite"},
		{ThemeMist, "Mist"},
		{ThemeSage, "Sage"},
		{ThemeLavender, "Lavender"},
	}
}

// NormalizeTheme returns id when it is a preset, otherwise Warm.
func NormalizeTheme(id string) string {
	switch id {
	case ThemeWarm, ThemeTide, ThemeNeutral, ThemeMist, ThemeSage, ThemeLavender:
		return id
	default:
		return ThemeDefault
	}
}

type tokenFile struct {
	Color struct {
		Themes    map[string]json.RawMessage `json:"themes"`
		Composite map[string]struct {
			Light map[string]string `json:"light"`
			Dark  map[string]string `json:"dark"`
		} `json:"composite"`
	} `json:"color"`
	Window struct {
		Base struct {
			Stops [][]json.RawMessage `json:"stops"`
			Tint  struct {
				Start float64 `json:"start"`
				End   float64 `json:"end"`
			} `json:"tint"`
		} `json:"base"`
	} `json:"window"`
}

// GradientGeometry is the window ramp from tokens.json: stop positions
// (0, 0.38, 0.70, 1) and where the horizontal tint fades in.
type GradientGeometry struct {
	Stops     []float64
	Names     []string
	TintStart float64
	TintEnd   float64
}

// WindowGradient reads the stop positions and the tint span. Colors stay
// on the palette; only the positions live here, so a token edit moves
// the ramp without a code change.
func WindowGradient() GradientGeometry {
	loadTokens()
	g := GradientGeometry{TintStart: loaded.Window.Base.Tint.Start, TintEnd: loaded.Window.Base.Tint.End}
	for _, pair := range loaded.Window.Base.Stops {
		if len(pair) < 2 {
			continue
		}
		var name string
		var at float64
		if json.Unmarshal(pair[0], &name) != nil || json.Unmarshal(pair[1], &at) != nil {
			continue
		}
		g.Names = append(g.Names, name)
		g.Stops = append(g.Stops, at)
	}
	if len(g.Stops) < 2 || g.TintEnd <= g.TintStart {
		return GradientGeometry{
			Stops:     []float64{0, 0.38, 0.70, 1},
			Names:     []string{"window/baseTop", "window/baseMid", "window/baseLow", "window/baseBottom"},
			TintStart: 0.35, TintEnd: 1,
		}
	}
	return g
}

var (
	loadOnce sync.Once
	loaded   tokenFile
	palettes = map[string][2]Colors{}
)

func loadTokens() {
	loadOnce.Do(func() {
		if err := json.Unmarshal(tokensJSON, &loaded); err != nil {
			panic("themes: tokens.json: " + err.Error())
		}
		for id, raw := range loaded.Color.Themes {
			var block struct {
				Light map[string]string `json:"light"`
				Dark  map[string]string `json:"dark"`
			}
			if err := json.Unmarshal(raw, &block); err != nil {
				continue
			}
			if block.Light == nil {
				continue
			}
			palettes[id] = [2]Colors{fillColors(block.Light), fillColors(block.Dark)}
		}
	})
}

func fillColors(m map[string]string) Colors {
	var c Colors
	v := reflect.ValueOf(&c).Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("token")
		if tag == "" {
			continue
		}
		v.Field(i).Set(reflect.ValueOf(ui.Hex(m[tag])))
	}
	return c
}

// Palette is one preset in light or dark, before the desktop accent and
// the inactive window overrides.
func Palette(id string, dark bool) Colors {
	loadTokens()
	p, ok := palettes[NormalizeTheme(id)]
	if !ok {
		p = palettes[ThemeDefault]
	}
	if dark {
		return p[1]
	}
	return p[0]
}

// ContentFlat is the flat content color from the composite tokens.
func ContentFlat(id string, dark bool) ui.Color {
	loadTokens()
	block, ok := loaded.Color.Composite[NormalizeTheme(id)]
	if !ok {
		block = loaded.Color.Composite[ThemeDefault]
	}
	hex := ""
	if dark {
		hex = block.Dark["contentFlat"]
	} else {
		hex = block.Light["contentFlat"]
	}
	if hex == "" {
		return Palette(id, dark).LayerContent
	}
	return ui.Hex(hex)
}

// MixColors blends each color field. t is 0 at a and 1 at b.
func MixColors(a, b Colors, t float32) Colors {
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	va := reflect.ValueOf(&a).Elem()
	vb := reflect.ValueOf(b)
	for i := 0; i < va.NumField(); i++ {
		ca, okA := va.Field(i).Interface().(ui.Color)
		cb, okB := vb.Field(i).Interface().(ui.Color)
		if !okA || !okB {
			continue
		}
		va.Field(i).Set(reflect.ValueOf(ca.Mix(cb, t)))
	}
	return a
}
