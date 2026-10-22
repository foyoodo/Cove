package themes

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// AccentFallback is the light system blue used when the desktop accent
// cannot be read (Preferences.Accent with A == 0). Dark mode uses the
// dark system blue; Resolve selects it. A Mac that can read
// NSColor.controlAccentColor already supplies the mode-correct color.
func AccentFallback() ui.Color { return ui.Hex("#007AFF") }

// Inactive gray the accent fades toward: light #A1A1A6, dark #6E6E73.
func inactiveGray(dark bool) ui.Color {
	if dark {
		return ui.Hex("#6E6E73")
	}
	return ui.Hex("#A1A1A6")
}

// ScrollbarThumb is the overlay thumb while it is showing: black at 32%
// in light, white at 35% in dark. Idle frames clear it.
func ScrollbarThumb(dark bool) ui.Color {
	if dark {
		return ui.RGBA(255, 255, 255, 0.35)
	}
	return ui.RGBA(0, 0, 0, 0.32)
}

// ComposerFlat is the opaque stand-in for the composer, used by the stop
// glyph. It is not a theme color token.
func ComposerFlat(dark bool) ui.Color {
	if dark {
		return ui.Hex("#2A2A2F")
	}
	return ui.Hex("#FFFEFE")
}

// NamedAccent is a macOS accent the design previews. The source color is
// what NSColor.controlAccentColor is in that mode; the rest is derived.
func NamedAccent(id string, dark bool) (ui.Color, bool) {
	src, ok := accentSources[id]
	if !ok {
		return ui.Color{}, false
	}
	if dark {
		return src.dark, true
	}
	return src.light, true
}

type accentPair struct{ light, dark ui.Color }

var accentSources = map[string]accentPair{
	"blue":     {ui.Hex("#007AFF"), ui.Hex("#0A84FF")},
	"purple":   {ui.Hex("#953D96"), ui.Hex("#A550A7")},
	"pink":     {ui.Hex("#F74F9E"), ui.Hex("#F74F9E")},
	"orange":   {ui.Hex("#F7821B"), ui.Hex("#F7821B")},
	"green":    {ui.Hex("#62BA46"), ui.Hex("#62BA46")},
	"graphite": {ui.Hex("#8C8C8C"), ui.Hex("#8C8C8C")},
}

// AccentIDs is the preview order of the system accents.
func AccentIDs() []string {
	return []string{"blue", "purple", "pink", "orange", "green", "graphite"}
}

// Resolve builds the palette for one frame. themeID picks the gradient and
// the mark color. accent is the desktop accent; A == 0 means it could not
// be read, and the system blue for this mode is used. Selection stays on
// the mark.
// When focused is false, the inactive set replaces the accent, the primary
// button, and the selected row (neutral gray).
func Resolve(themeID string, dark, focused bool, accent ui.Color) Colors {
	base := Palette(themeID, dark)
	if accent.A == 0 {
		// Unreadable accent (headless tester, or a desktop that reports
		// none). The system blue is the light fallback in light mode and
		// the dark system blue in dark mode.
		accent, _ = NamedAccent("blue", dark)
	}
	accent.A = 255
	applyAccent(&base, accent, dark)
	if !focused {
		applyInactive(&base, dark)
	}
	return base
}

func applyAccent(c *Colors, accent ui.Color, dark bool) {
	hoverT, pressT := float32(0.08), float32(0.16)
	soft, ring, selText := float32(0.16), float32(0.45), float32(0.24)
	toward := ui.Hex("#000000")
	if dark {
		hoverT, pressT = 0.12, 0.10
		soft, ring, selText = 0.28, 0.55, 0.40
		toward = ui.Hex("#FFFFFF")
	}
	c.AccentDefault = accent
	c.AccentHover = accent.Mix(toward, hoverT)
	// Pressed is toward black in both modes.
	c.AccentPressed = accent.Mix(ui.Hex("#000000"), pressT)
	c.AccentSoft = withAlpha(accent, soft)
	c.AccentRing = withAlpha(accent, ring)
	// selection/row is the theme mark at 16% / 20%, already on the palette.
	c.SelectionText = withAlpha(accent, selText)
	c.TextLink = linkColor(accent, dark, *c)
	c.ButtonPrimaryBg = accent
	c.ButtonPrimaryText = ui.Hex("#FFFFFF")
}

func applyInactive(c *Colors, dark bool) {
	gray := inactiveGray(dark)
	faded := c.AccentDefault.Mix(gray, 0.55)
	c.AccentDefault = faded
	c.AccentHover = faded
	c.AccentPressed = faded
	// Dialog surfaces stay the active neutral overlay. Only sheets, menus,
	// and the composer pick up the inactive overlay tokens.
	if dark {
		c.AccentSoft = ui.Hex("#FFFFFF12")
		c.SelectionRow = ui.Hex("#FFFFFF12")
		c.SelectionText = ui.Hex("#FFFFFF26")
		c.ButtonPrimaryBg = ui.Hex("#FFFFFF24")
		c.ButtonPrimaryText = ui.Hex("#F2F2F7")
		c.LayerSheet = ui.Hex("#29292C")
		c.LayerOverlay = ui.Hex("#2C2C30")
		c.LayerComposer = ui.Hex("#2B2B30F2")
	} else {
		c.AccentSoft = ui.Hex("#0000000F")
		c.SelectionRow = ui.Hex("#0000000F")
		c.SelectionText = ui.Hex("#0000001A")
		c.ButtonPrimaryBg = ui.Hex("#FFFFFFD9")
		c.ButtonPrimaryText = ui.Hex("#1D1D1F")
		c.LayerSheet = ui.Hex("#F7F7F8")
		c.LayerOverlay = ui.Hex("#FCFCFC")
		c.LayerComposer = ui.Hex("#FFFFFFF2")
	}
	c.AccentRing = ui.Hex("#00000000")
	// Link fades from the contrast-adjusted active link, not the raw accent.
	c.TextLink = c.TextLink.Mix(gray, 0.35)
}

func withAlpha(c ui.Color, a float32) ui.Color {
	if a <= 0 {
		c.A = 0
		return c
	}
	if a >= 1 {
		c.A = 255
		return c
	}
	c.A = uint8(a*255 + 0.5)
	return c
}

// linkColor walks the accent toward black (light) or white (dark) in 2%
// steps until it clears 4.5:1 on every theme's content card, at both ends
// of that theme's window gradient including the horizontal tint. The worst
// theme decides, so a link stays readable after a theme change.
func linkColor(accent ui.Color, dark bool, _ Colors) ui.Color {
	toward := ui.Hex("#000000")
	if dark {
		toward = ui.Hex("#FFFFFF")
	}
	link := accent
	link.A = 255
	for i := 0; i <= 50; i++ {
		if i > 0 {
			link = accent.Mix(toward, float32(i)*0.02)
			link.A = 255
		}
		if linkClearsThemes(link, dark) {
			return link
		}
	}
	return link
}

func linkClearsThemes(link ui.Color, dark bool) bool {
	for _, preset := range Presets() {
		pal := Palette(preset.ID, dark)
		top := pal.LayerContent.Over(pal.WindowBaseTint.Over(pal.WindowBaseTop))
		bottom := pal.LayerContent.Over(pal.WindowBaseTint.Over(pal.WindowBaseBottom))
		if contrast(link, top) < 4.5 || contrast(link, bottom) < 4.5 {
			return false
		}
	}
	return true
}

func contrast(fg, bg ui.Color) float64 {
	l1, l2 := relLum(fg), relLum(bg)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

func relLum(c ui.Color) float64 {
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}

func lin(v uint8) float64 {
	s := float64(v) / 255
	if s <= 0.04045 {
		return s / 12.92
	}
	return math.Pow((s+0.055)/1.055, 2.4)
}
