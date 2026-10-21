package themes

import "github.com/egoist/mygo/ui"

// Traffic lights are the macOS close, minimize, and zoom colors. They are
// window chrome, drawn only when the host has no title-bar inset. They are
// not theme tokens; they live here so the rest of the app has no color literals.
func TrafficLights() []ui.Color {
	return []ui.Color{
		ui.Hex("#FF5F57"),
		ui.Hex("#FEBC2E"),
		ui.Hex("#28C840"),
	}
}

// DialogScrim is neutral black at 6% in light and 12% in dark. It does not
// come from a theme palette, so a Warm window does not tint the overlay.
func DialogScrim(dark bool) ui.Color {
	if dark {
		return ui.Hex("#0000001F")
	}
	return ui.Hex("#0000000F")
}

// TrafficInactive is the gray stand-in for the window controls when the
// window is not focused. The live controls are the system's; screenshots
// paint these.
func TrafficInactive(dark bool) ui.Color {
	if dark {
		return ui.Hex("#4A4A4F")
	}
	return ui.Hex("#D1D1D6")
}

// WindowBackground is the opaque color behind the first frame, light then
// dark. The view paints the gradient on top of it.
func WindowBackground() string {
	// The first frame is Warm, the default preset. The view paints the
	// live gradient over this as soon as it draws.
	return "light-dark(" + hex(Palette(ThemeWarm, false).WindowBaseMid) + ", " + hex(Palette(ThemeWarm, true).WindowBaseMid) + ")"
}

func hex(c ui.Color) string {
	return "#" + byteHex(c.R) + byteHex(c.G) + byteHex(c.B)
}

func byteHex(v uint8) string {
	const digits = "0123456789ABCDEF"
	return string([]byte{digits[v>>4], digits[v&0x0F]})
}

// ProbeAccent is a color that is not the fallback blue, used to prove the
// desktop accent is followed.
func ProbeAccent() ui.Color { return ui.Hex("#FF00AA") }
