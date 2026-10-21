# Cove 0.7

Visual source of truth is `themes/tokens.json`. `themes/tide.go` holds the neutral light and dark bases. `themes/accent.go` derives hover, tint, selection, link, and the inactive set from the macOS system accent (`Preferences().Accent`, falling back to `#007AFF`). `Theme` in `design.go` is that palette for one frame. Components read it. They do not contain color literals.

Tide green is only the AI mark: the thinking spark, the thinking and quote bars, the streaming dots and caret, the generating status dot, the avatar glyph, and the app icon.

The window is gorex's warm background: a vertical gradient (`window/baseTop` → `baseMid` → `baseLow` → `baseBottom` at 0 / 38 / 70 / 100%) plus a horizontal tint from x 35% to the right edge. There is no vibrancy and no sidebar separator. The sidebar is that background. The main area is an inset card (`layer/content`, radius 12, 1px border, two `shadow/card` layers) flush under the title bar, 8pt from the right and bottom. Bubbles, code, tables, and rows are translucent overlays. The settings sheet, menus, and the jump-to-latest pill are opaque. The floating composer sits 14pt inside the card (`layer/composer`, 0.95). Its fade and the plate under it sample the card composited onto the background at each point.

Appearance is `system`, `light`, or `dark`, stored on the conversation file. System follows the host appearance MyGo reports at the start of each frame. A live window reads `Window.IsFocused()` each frame; headless shots set `shotInactive`.

Fonts are the system UI font and `ui-monospace`. The app icon is the compiled catalog `icon/compiled/Assets.car` (`CFBundleIconName` = Cove) and `resources/Cove.icns` (`CFBundleIconFile` = Cove). Do not regenerate them.
