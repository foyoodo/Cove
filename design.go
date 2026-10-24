package main

import (
	"time"

	"cove/themes"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"
)

// Lengths are DIPs. Motion durations and curves are tokens.json motion.
const (
	space1 = 4
	space2 = 8
	space3 = 12
	space4 = 16
	space5 = 20
	space6 = 24
	space8 = 32

	radiusRow      = 6
	radiusCode     = 8
	radiusMenu     = 8 // radius/menu; same number as radius/code, its own token
	radiusGroup    = 10
	radiusCard     = 12 // hint strip, settings, dialogs
	radiusBubble   = 14
	radiusComposer = 14
	// radiusContentCard is the main chat card. At the 8pt narrow inset it
	// is concentric with the composer: 14 + 8. The 1pt border's inner
	// corner, which clips the fade and the find bar, is one point less.
	radiusContentCard = 22
	radiusContentClip = 21
	radiusPill        = 999

	readingWidth  = 720
	bubbleMax     = 520
	rowHeight     = 28
	iconHit       = 28
	iconSm        = 24
	sendSize      = 26
	composerToolH = 24 // tool row inside the 73pt composer border box
	scrollbarHold = 12 // frames the overlay thumb stays up after a scroll
	chipH         = 22
	cardW         = 300
	cardH         = 60
	cardGap       = 10
	// suggestionFit is the narrowest suggestion card that still shows the
	// short subtitle on one line: 12pt of "Short paragraphs, the hard part
	// named." plus the 28pt icon, the 10pt gap, and 12pt padding each side.
	// Two columns narrower than this stack, so the line is not clipped.
	suggestionFit    = 284
	sidebarW         = 240 // default user width (cove.sidebar.width)
	sidebarMin       = 200
	sidebarMax       = 360
	sidebarNarrow    = 200 // settings window; also the floor of the main sidebar
	sidebarReserve   = 520 // window minus this is the widest sidebar that keeps the card ≥ 512
	sidebarGrip      = 8   // hit strip centered on the card's left edge
	composerInsetMin = 8   // left, right, and bottom when the card is 736 or narrower
	composerInsetMax = 14  // bottom inset once the card is wide enough to centre 720
	composerFade     = 28  // fade height is composer height + inset + 28
	composerExtra    = 16  // list bottom padding is composer height + inset + 16
	jumpGap          = 10
	jumpH            = 26
	composerMinH     = 73

	motionHoverButton = 120 * time.Millisecond
	motionHoverRow    = 120 * time.Millisecond
	motionPressIn     = 60 * time.Millisecond
	motionActions     = 120 * time.Millisecond
	motionThink       = 200 * time.Millisecond
	motionThinkFade   = 160 * time.Millisecond
	motionEnter       = 220 * time.Millisecond
	motionDots        = 1200 * time.Millisecond
	motionCaret       = 1000 * time.Millisecond
	motionTideBar     = 1600 * time.Millisecond
	motionScrim       = 180 * time.Millisecond
	motionModal       = 220 * time.Millisecond
	motionModalOut    = 160 * time.Millisecond
	motionSwitch      = 160 * time.Millisecond
	// motionTheme matches the background crossfade. Both start on one clock.
	motionTheme     = gradFade
	motionTooltip   = 100 * time.Millisecond
	motionCopied    = 1500 * time.Millisecond
	motionTitleFade = 200 * time.Millisecond
	motionTitleSpin = 800 * time.Millisecond
	motionTitleFail = 4 * time.Second

	messageEnterY = 4
	modalScale    = 0.98
	dotsMin       = 0.3
	tideMin       = 0.35
	tideMax       = 0.9
)

// composerInsets is SPEC §10/§16. side is max(8, (cardWidth−720)/2) and
// bottom is min(14, side). At 736 the two expressions meet at 8, so the
// inset moves continuously and does not jump. cardWidth is the card's
// inner width, inside the 1pt border, which is where the composer is placed.
func composerInsets(cardWidth float32) (side, bottom float32) {
	side = (cardWidth - readingWidth) / 2
	if side < composerInsetMin {
		side = composerInsetMin
	}
	bottom = side
	if bottom > composerInsetMax {
		bottom = composerInsetMax
	}
	return side, bottom
}

func (a *App) composerInsets(c *ui.Context) (side, bottom float32) {
	return composerInsets(a.cardContentWidth(c))
}

// Curves from tokens.json motion.easing.
var (
	easeStandard   = ui.CubicBezier(0.2, 0, 0, 1)
	easeEmphasized = ui.CubicBezier(0.3, 0, 0, 1)
	easeEnter      = ui.CubicBezier(0.2, 0.8, 0.2, 1)
	easeExit       = ui.CubicBezier(0.4, 0, 1, 1)
	easeInOut      = ui.CubicBezier(0.45, 0, 0.55, 1)
	easeLinear     = ui.CubicBezier(0, 0, 1, 1)
)

const (
	appearanceSystem = "system"
	appearanceLight  = "light"
	appearanceDark   = "dark"
)

// Theme is the resolved appearance for one frame. Color fields map 1:1 to
// tokens.json names (see themes.Colors), after the desktop accent and the
// window focus state have been applied. Components read this; they do not
// pick colors of their own.
type Theme struct {
	themes.Colors
	Dark      bool
	Focused   bool
	StopGlyph ui.Color
}

func (t Theme) uiTheme() *ui.Theme {
	return &ui.Theme{
		Dark:           t.Dark,
		Background:     t.WindowBaseBottom,
		Surface:        t.ControlBg,
		SurfaceHover:   t.FillHover,
		SurfacePressed: t.FillPressed,
		Border:         t.SeparatorDefault,
		Text:           t.TextPrimary,
		TextMuted:      t.TextSecondary,
		Accent:         t.AccentDefault,
		AccentHover:    t.AccentHover,
		AccentPressed:  t.AccentPressed,
		AccentText:     t.TextOnAccent,
		Danger:         t.StatusDanger,
		Warning:        t.StatusWarning,
		Success:        t.StatusSuccess,
		Selection:      t.SelectionText,
		Focus:          t.AccentRing,
		Inverse:        t.InverseBg,
		InverseText:    t.TextOnInverse,
		// Translucent while a scroll is showing it. veilScrollbar clears
		// it on idle frames, so a hovered list does not leave a solid bar.
		Scrollbar: themes.ScrollbarThumb(t.Dark),
		Radius:    radiusRow,
		Spacing:   4,
		FontSize:  13,
		// Empty family is the system UI font: SF Pro on macOS. Inter is
		// only the Figma stand-in and is not bundled.
	}
}

func (t Theme) markdownTheme() *markdown.Theme {
	th := &markdown.Theme{
		FontSize:       14,
		LineHeight:     21.0 / 14.0,
		CodeLineHeight: 18.0 / 12.0,
		Text:           t.TextPrimary,
		Muted:          t.TextSecondary,
		Link:           t.TextLink,
		Strong:         t.TextPrimary,
		StrongWeight:   600,
		Code:           t.CodeInlineText,
		CodeBackground: t.CodeInlineBg,
		CodeFont:       "ui-monospace",
		CodeSize:       12.5 / 14.0,
		// .md gap is 10px. List items are 3px apart. Both are ems of the 14pt body.
		BlockSpacing:        10.0 / 14.0,
		ItemSpacing:         3.0 / 14.0,
		ListIndent:          1.15,
		QuoteBar:            t.TextTertiary,
		QuoteText:           t.TextPrimary,
		QuoteBarWidth:       3,
		Rule:                t.SeparatorDefault,
		CodeBlockBackground: t.CodeBg,
		CodeBlockBorder:     t.CodeBorder,
		CodeBlockRadius:     radiusCode,
		CodeBlockPadding:    12.0 / 14.0,
		CodeColors:          t.codeColors(),
		TableBorder:         t.TableRule,
		TableHeader:         t.TableHeader,
		TableStripe:         ui.Transparent,
		TableCellPadding:    [2]float32{12.0 / 14.0, 0},
		TableMaxColumn:      28,
		Mark:                t.StatusWarningSoft,
		Checkbox:            t.AccentDefault,
	}
	// Heading sizes are absolute in the token file. The markdown theme
	// stores them as a multiple of the 14pt body.
	sizes := [6]float32{20.0 / 14.0, 17.0 / 14.0, 15.0 / 14.0, 14.0 / 14.0, 13.0 / 14.0, 12.0 / 14.0}
	for i := range th.Headings {
		th.Headings[i] = markdown.HeadingStyle{
			// h3 margin is 4px 0 0 on top of the 10px block gap: 14px before, 10px after.
			// Heading margins replace BlockSpacing, so Top is the full 14px.
			Size: sizes[i], Weight: 600, Color: t.TextPrimary, Top: 14.0 / 14.0, Bottom: 10.0 / 14.0,
		}
	}
	th.Headings[5].Color = t.TextSecondary
	return th
}

func (t Theme) codeColors() *editor.Theme {
	ct := editor.DefaultTheme(t.uiTheme())
	ct.Background = t.CodeBg
	ct.Text = t.CodeText
	ct.Cursor = t.ThemeMark
	set := func(k editor.TokenKind, c ui.Color) { ct.Tokens[k].Color = c }
	set(editor.TokenKeyword, t.SyntaxKeyword)
	set(editor.TokenOperator, t.SyntaxPunct)
	set(editor.TokenPunctuation, t.SyntaxPunct)
	set(editor.TokenString, t.SyntaxString)
	set(editor.TokenEscape, t.SyntaxString)
	set(editor.TokenRegexp, t.SyntaxString)
	set(editor.TokenComment, t.SyntaxComment)
	set(editor.TokenFunction, t.SyntaxFunction)
	set(editor.TokenMeta, t.SyntaxFunction)
	set(editor.TokenNumber, t.SyntaxNumber)
	set(editor.TokenType, t.SyntaxType)
	set(editor.TokenProperty, t.SyntaxType)
	set(editor.TokenConstant, t.SyntaxNumber)
	set(editor.TokenAttribute, t.SyntaxType)
	set(editor.TokenTag, t.SyntaxKeyword)
	set(editor.TokenVariable, t.CodeText)
	set(editor.TokenText, t.CodeText)
	return ct
}

// providerColor is the dot for a preset kind (OpenAI, Anthropic, …).
// A custom connection uses provider/custom even when its display name
// is a preset name. The display name is never a key.
func (t Theme) providerColor(kind string, custom bool) ui.Color {
	if custom {
		return t.ProviderCustom
	}
	switch kind {
	case "OpenAI":
		return t.ProviderOpenai
	case "Anthropic":
		return t.ProviderAnthropic
	case "OpenRouter":
		return t.ProviderOpenrouter
	case "DeepSeek":
		return t.ProviderDeepseek
	case "Ollama":
		return t.ProviderOllama
	case "xAI":
		return t.ProviderXai
	default:
		return t.ProviderCustom
	}
}

// applyLook resolves the palette from the user's appearance mode, the
// system appearance, the desktop accent, and whether the window is
// focused. Tide green is not derived from the accent.
//
// MyGo rebuilds the system theme at the start of every frame from the
// host's dark flag (engine.defaultTheme) and asks for a new frame when
// that flag changes (themeChanged). There is no separate appearance
// subscription. Reading Theme().Dark before SetTheme is the system
// appearance for this frame, so System mode follows a live change on the
// next frame.
//
// ui.Context does not expose window focus. A live window is read with
// Window.IsFocused. Headless tests have no window, so they use shotInactive.
func (a *App) applyLook(c *ui.Context) {
	sysDark := c.Theme().Dark
	a.reduceMotion = c.Preferences().ReduceMotion
	dark := sysDark
	switch normalizeAppearance(a.data.Appearance) {
	case appearanceLight:
		dark = false
	case appearanceDark:
		dark = true
	}
	focused := true
	if a.win != nil {
		focused = a.win.IsFocused()
	} else if a.shotInactive {
		focused = false
	}
	accent := c.Preferences().Accent
	if a.shotAccent.A != 0 {
		accent = a.shotAccent
	}
	target := Theme{
		Colors:    themes.Resolve(a.data.Theme, dark, focused, accent),
		Dark:      dark,
		Focused:   focused,
		StopGlyph: themes.ComposerFlat(dark),
	}
	now := c.Now()
	wasMoving := a.themeMoving()
	stopsChanged := a.haveLook && (a.lookTarget.ThemeMark != target.ThemeMark || a.lookTarget.WindowBaseTop != target.WindowBaseTop || a.lookTarget.Dark != target.Dark)
	colorsChanged := a.haveLook && a.lookTarget.Colors != target.Colors
	focusSame := a.haveLook && a.lookTarget.Focused == target.Focused

	if a.instant || a.reduceMotion {
		a.clearThemeClock()
		a.lookTarget = target
		a.haveLook = true
		a.finishLook(c, target, wasMoving)
		return
	}

	if stopsChanged {
		// The mix and the preview start in this frame. A full-resolution
		// bitmap already in cache fades in on this same clock. One that
		// is still building crossfades over the preview when it lands,
		// for at most 150ms.
		a.beginThemeSwitch(now, target)
	}
	a.lookTarget = target
	a.haveLook = true

	// Accent does not change the window bitmap. Mix it immediately.
	if !stopsChanged && colorsChanged && focusSame && !a.fadeLive && a.blendStart.IsZero() {
		a.blendFrom = a.look
		a.blendStart = now
		a.backdropFadeAt = time.Time{}
	}

	a.advanceThemeClock(now)

	shown := target
	if !a.blendStart.IsZero() {
		if now.Before(a.blendStart.Add(motionTheme)) {
			shown.Colors = themes.MixColors(a.blendFrom.Colors, target.Colors, clockU(now, a.blendStart, motionTheme))
		} else {
			a.blendStart = time.Time{}
		}
	}
	if a.themeMoving() || a.heldBackdrop() {
		c.After(16 * time.Millisecond)
	} else if wasMoving {
		a.backdropFadeAt = time.Time{}
		a.swapAt = time.Time{}
		a.swapDur = 0
		a.previewBM = nil
		a.fullBM = nil
		a.fadeFromBM = nil
		a.previewed = false
		a.baseStack = nil
		a.baseDrawn = false
		a.veilStack = nil
	}
	a.finishLook(c, shown, wasMoving)
}

func (a *App) finishLook(c *ui.Context, shown Theme, wasMoving bool) {
	nowMoving := a.themeMoving()
	// The frame that lands on the target still has a one-step color
	// change. Suppress the element chase so it does not add another 150ms.
	a.suppressFade = wasMoving || nowMoving
	c.SetTheme(shown.uiTheme())
	a.look = shown
	a.dark = shown.Dark
	a.md.Theme = shown.markdownTheme()
	// One selection across the transcript, so a drag can cross messages.
	// The list is the Selectable container. Markdown must not open its own.
	a.md.ShareSelection = true
	a.md.CodeBlock = func(ctx *ui.Context, b *markdown.CodeBlockView) {
		a.codeBlock(ctx, b)
	}
	a.md.Block = func(ctx *ui.Context, d *parser.Document, i int) bool {
		return a.renderTable(ctx, d, i) || a.renderStreamCaret(ctx, d, i)
	}
	mainWin, prefsWin := a.win, a.prefs
	setGradientMode(a.instant, !a.instant && !a.reduceMotion, func() {
		if mainWin != nil {
			mainWin.Invalidate()
		}
		if prefsWin != nil {
			prefsWin.Invalidate()
		}
		// Headless tests have no window, so Invalidate does not post a
		// frame. The hook is the same wake the worker just took.
		if v := gradientWakeHook.Load(); v != nil {
			if hook := v.(func()); hook != nil {
				hook()
			}
		}
	})
	a.veilNoted = false
}

func (a *App) clearThemeClock() {
	a.fadeLive = false
	a.blendStart = time.Time{}
	a.backdropFadeAt = time.Time{}
	a.swapAt = time.Time{}
	a.swapDur = 0
	a.veilStandIn = false
	a.previewBM = nil
	a.fullBM = nil
	a.fadeFromBM = nil
	a.previewed = false
	a.baseStack = nil
	a.baseDrawn = false
	a.veilStack = nil
	a.veilFresh = false
}

func (a *App) themeMoving() bool {
	return a.fadeLive || a.veilStandIn || !a.blendStart.IsZero()
}

// planVeil is the veil stack on screen, bottom to top. A switch calls it
// before resetting the clock, so the next base is the current blend.
func (a *App) planVeil(now time.Time) []veilSlice {
	if !a.fadeLive {
		if a.veilStandIn {
			return []veilSlice{{a.veilFrom, 1}}
		}
		return []veilSlice{{a.lookTarget, 1}}
	}
	u := clockU(now, a.backdropFadeAt, motionTheme)
	layers := make([]veilSlice, 0, 4)
	if u < 1 {
		if len(a.veilStack) > 0 {
			layers = append(layers, a.veilStack...)
		} else {
			layers = append(layers, veilSlice{a.veilFrom, 1})
		}
	}
	if u > 0 {
		pa := u
		if len(layers) == 0 {
			pa = 1
		}
		layers = append(layers, veilSlice{a.veilTo, pa})
	}
	return layers
}

// installVeilBase keeps a single opaque veil on the from-theme path.
// A blend stays as the stack that was on screen.
func (a *App) installVeilBase(layers []veilSlice) {
	if len(layers) == 1 && layers[0].alpha >= 0.999 {
		a.veilFrom = layers[0].look
		a.veilStack = nil
		return
	}
	if len(layers) > 0 {
		a.veilFrom = layers[0].look
	}
	a.veilStack = append([]veilSlice(nil), layers...)
}

// fadeSlice is one backdrop bitmap and the alpha it was drawn at.
type fadeSlice struct {
	bm    *ui.Bitmap
	alpha float32
}

// beginThemeSwitch starts the control mix and the background crossfade
// at now. The full-resolution bitmap, when it is not already cached, is
// requested off the frame and swapped in later. The base is the picture
// already on screen, including a blend that has not finished.

func (a *App) beginThemeSwitch(now time.Time, target Theme) {
	// The base is whatever this frame is already showing, including a
	// blend that has not finished. A later switch must not start from an
	// earlier theme or from the incoming bitmap with the blend thrown away.
	onScreen := a.planFade(now)
	veilShown := a.planVeil(now)
	fromPal := a.look
	a.veilTo = target
	a.blendFrom = fromPal
	a.blendStart = now
	a.backdropFadeAt = now
	a.fadeLive = true
	a.swapAt = time.Time{}
	a.swapDur = 0
	a.fullBM = nil
	a.previewBM = nil
	a.fadeFromBM = nil
	a.baseStack = nil
	a.previewed = false
	a.baseDrawn = false
	a.installBase(onScreen)
	a.installVeilBase(veilShown)
	a.veilStandIn = false
	a.veilFresh = false
	a.veilGeoms = nil
	bumpVeilJobs()
	a.lookTarget = target
	a.logSwitchRequested()
	// The window that did not handle the click has no timer of its own
	// yet. A cached full-resolution bitmap returns before the worker, so
	// wakeGradient will not invalidate that window either.
	a.invalidateWindows()
	if a.winPW < 1 || a.winPH < 1 {
		return
	}
	if e := cachedGradient(target, a.winPW, a.winPH); e != nil {
		a.fullBM = e.bm
		a.armSwap(now)
		return
	}
	ensureGradient(target, a.winPW, a.winPH)
	a.previewBM = previewBitmap(target, a.winPW, a.winPH)
}

// installBase keeps a single opaque bitmap on the old from-layer path.
// Anything blended stays as the stack that was on screen.
func (a *App) installBase(layers []fadeSlice) {
	if len(layers) == 1 && layers[0].alpha >= 0.999 && layers[0].bm != nil {
		a.fadeFromBM = layers[0].bm
		a.baseStack = nil
		return
	}
	a.fadeFromBM = nil
	a.baseStack = append([]fadeSlice(nil), layers...)
}

// fadeWeights is the theme-clock and full-resolution-swap position.
// The outgoing picture is retired once this switch has drawn its preview
// and either the swap has started or the theme clock has finished.
func (a *App) fadeWeights(now time.Time) (u, swapU float32, swapArmed, retire bool) {
	u = clockU(now, a.backdropFadeAt, motionTheme)
	swapArmed = a.fullBM != nil && !a.swapAt.IsZero()
	if swapArmed {
		swapU = clockU(now, a.swapAt, a.swapDur)
	}
	retire = a.previewed && (swapArmed || u >= 1)
	return
}

// planFade is the backdrop stack this frame would paint, bottom to top.
// A switch calls it before resetting the clock, so the next base is the
// current blend and not an earlier theme or the incoming bitmap alone.
func (a *App) planFade(now time.Time) []fadeSlice {
	if a.winPW < 1 || a.winPH < 1 {
		return nil
	}
	if !a.fadeLive {
		if a.fullBM != nil {
			return []fadeSlice{{a.fullBM, 1}}
		}
		if a.previewBM != nil {
			return []fadeSlice{{a.previewBM, 1}}
		}
		base, over, alpha := displayedGradient(a.winPW, a.winPH, now)
		var layers []fadeSlice
		if base != nil {
			layers = append(layers, fadeSlice{base, 1})
		}
		if over != nil && alpha > 0 {
			layers = append(layers, fadeSlice{over, alpha})
		}
		return layers
	}
	u, swapU, _, retire := a.fadeWeights(now)
	layers := make([]fadeSlice, 0, 4)
	if !retire && swapU < 1 {
		if len(a.baseStack) > 0 {
			layers = append(layers, a.baseStack...)
		} else if a.fadeFromBM != nil {
			layers = append(layers, fadeSlice{a.fadeFromBM, 1})
		}
	}
	if a.previewBM != nil && swapU < 1 {
		pa := u
		if retire || len(layers) == 0 {
			pa = 1
		}
		if pa > 0 {
			layers = append(layers, fadeSlice{a.previewBM, pa})
		}
	}
	if a.fullBM != nil && swapU > 0 {
		layers = append(layers, fadeSlice{a.fullBM, swapU})
	} else if len(layers) == 0 && a.fullBM != nil {
		layers = append(layers, fadeSlice{a.fullBM, 1})
	}
	return layers
}

// armSwap starts the preview-to-full-resolution crossfade. It lasts the
// time still left on the 150ms clock when that is at least a frame, and
// otherwise the full 150ms. Either way it does not run longer than that.
func (a *App) armSwap(now time.Time) {
	if !a.swapAt.IsZero() {
		return
	}
	dur := motionTheme
	if !a.backdropFadeAt.IsZero() {
		remain := a.backdropFadeAt.Add(motionTheme).Sub(now)
		if remain >= 16*time.Millisecond && remain < dur {
			dur = remain
		}
	}
	a.swapAt = now
	a.swapDur = dur
}

// advanceThemeClock keeps the preview on the 150ms clock and crossfades
// to the full-resolution bitmap once it exists. fadeLive stays set until
// that crossfade finishes, so paint does not fall through to a bitmap a
// stale build left behind.
func (a *App) advanceThemeClock(now time.Time) {
	if a.fadeLive && a.fullBM == nil && a.winPW > 0 && a.winPH > 0 {
		if e := cachedGradient(a.lookTarget, a.winPW, a.winPH); e != nil {
			a.fullBM = e.bm
			a.armSwap(now)
		} else {
			ensureGradient(a.lookTarget, a.winPW, a.winPH)
		}
	}
	swapDone := a.fadeLive && a.fullBM != nil && !a.swapAt.IsZero() && !now.Before(a.swapAt.Add(a.swapDur))
	if swapDone {
		// A miss for this window's size must not drop the bitmap and
		// restart the swap. Restarting draws the outgoing theme again.
		if e := cachedGradient(a.lookTarget, a.winPW, a.winPH); e != nil {
			a.fullBM = e.bm
		}
		if a.fullBM != nil {
			a.fadeLive = false
			a.pinBackdrop()
			if a.veilRequired() && !a.veilsReady() {
				a.veilStandIn = true
			}
		}
	}
	a.advanceVeilSwap()
}

func (a *App) advanceVeilSwap() {
	if !a.veilStandIn {
		return
	}
	if !a.veilRequired() || a.veilsReady() {
		a.veilStandIn = false
	}
}

func (a *App) veilsReady() bool {
	if !a.veilFresh || len(a.veilGeoms) == 0 {
		return false
	}
	for _, g := range a.veilGeoms {
		e := cachedGradient(a.lookTarget, g.winW, g.winH)
		if e == nil || !veilReady(g, e.seq, packColor(a.lookTarget.LayerContent)) {
			return false
		}
	}
	return true
}

func clockU(now, start time.Time, dur time.Duration) float32 {
	if start.IsZero() || dur <= 0 {
		return 0
	}
	u := float32(now.Sub(start)) / float32(dur)
	if u < 0 {
		return 0
	}
	if u > 1 {
		return 1
	}
	return u
}

func (a *App) pinBackdrop() {
	if a.winPW > 0 {
		pinGradientShown(a.lookTarget, a.winPW, a.winPH)
	}
	if a.veilWinW > 0 && (a.veilWinW != a.winPW || a.veilWinH != a.winPH) {
		pinGradientShown(a.lookTarget, a.veilWinW, a.veilWinH)
	}
	// The other window keeps its own size. Pin every size that already
	// has this theme so its next paint does not crossfade from the old one.
	cacheMu.Lock()
	extra := make([][2]int, 0, len(sizes))
	for sz := range sizes {
		extra = append(extra, sz)
	}
	cacheMu.Unlock()
	for _, sz := range extra {
		if cachedGradient(a.lookTarget, sz[0], sz[1]) != nil {
			pinGradientShown(a.lookTarget, sz[0], sz[1])
		}
	}
}

// heldBackdrop is a finished clock whose bitmap is not yet the one
// presentGradient would draw. Dropping the clock's bitmap first paints
// the previous theme.
func (a *App) heldBackdrop() bool {
	if a.fadeLive || (a.fullBM == nil && a.previewBM == nil) || a.winPW < 1 {
		return false
	}
	return !sizeShowsTarget(a)
}

func sizeShowsTarget(a *App) bool {
	e := cachedGradient(a.lookTarget, a.winPW, a.winPH)
	if e == nil {
		return false
	}
	cacheMu.Lock()
	st := sizes[[2]int{a.winPW, a.winPH}]
	cacheMu.Unlock()
	return st != nil && st.shown == e && st.from == nil
}

// veilRequired is true when this frame's chat paints a composer veil.
// Welcome, the settings sheet, and the palette boards do not.
func (a *App) veilRequired() bool {
	if a.shotStates || a.shotPalette {
		return false
	}
	if a.settingsOpen && a.prefs == nil {
		return false
	}
	return a.active() != nil
}

// paintBackdrop draws the window bitmap. A theme change crossfades the
// picture already on screen toward the filtered preview on the shared
// 150ms clock, then crossfades that preview to the dithered full-resolution
// bitmap in at most 150ms. Instant shots still build synchronously. The
// clock itself is armed with Context.After in applyLook; a painter repaint
// would move the bitmap without the controls.
//
// The chat card is translucent, so the gradient shows through it. The
// backdrop is one image of the whole window: splitting that image or
// filling the card with a solid run changes pixels. A 1:1 opaque draw is
// copied, not bilinear-sampled, so a large scroll stays inside a frame.
func (a *App) paintBackdrop(p *ui.Painter, r ui.Rect) {
	a.paintBackdropIn(p, r, a.chatBackdropHole(r))
}

// paintSettingsBackdrop is the settings window. Its card is not the chat
// card, so the whole gradient stays the bitmap.
func (a *App) paintSettingsBackdrop(p *ui.Painter, r ui.Rect) {
	a.paintBackdropIn(p, r, ui.Rect{})
}

// chatBackdropHole is the chat card interior, inset by the corner radius.
// Empty when this frame does not show that card.
func (a *App) chatBackdropHole(r ui.Rect) ui.Rect {
	if a.shotStates || a.shotPalette || a.settingsOpen || r.W < 1 || r.H < 1 {
		return ui.Rect{}
	}
	shown := sidebarShown(a.sidebarWidth, r.W)
	inset := float32(radiusContentCard)
	x := r.X + shown + inset
	y := r.Y + float32(titleBarHeight) + inset
	w := r.W - shown - 8 - 2*inset
	h := r.H - float32(titleBarHeight) - 8 - 2*inset
	if w < 32 || h < 32 {
		return ui.Rect{}
	}
	return ui.Rect{X: x, Y: y, W: w, H: h}
}

func (a *App) paintBackdropIn(p *ui.Painter, r, hole ui.Rect) {
	a.baseDrawn = false
	if r.W <= 0 || r.H <= 0 {
		return
	}
	scale := p.Scale()
	if scale < 1 {
		scale = 1
	}
	_, pw := deviceSpan(r.X, r.X+r.W, scale)
	_, ph := deviceSpan(r.Y, r.Y+r.H, scale)
	if pw > 0 && ph > 0 {
		a.winPW, a.winPH = pw, ph
	}
	if a.instant || a.reduceMotion || gradSync.Load() {
		paintWindowBackground(p, r, hole, a.backdrop())
		return
	}
	if a.fadeLive {
		a.paintFadingBackdrop(p, r, hole, pw, ph)
		return
	}
	if a.heldBackdrop() {
		a.paintHeldBackdrop(p, r, hole, pw, ph)
		return
	}
	paintWindowBackground(p, r, hole, a.backdrop())
}

func (a *App) paintFadingBackdrop(p *ui.Painter, r, hole ui.Rect, pw, ph int) {
	if a.fullBM == nil && pw > 0 && ph > 0 {
		if e := cachedGradient(a.lookTarget, pw, ph); e != nil {
			a.fullBM = e.bm
			// Publishing arms the swap. It does not restart the theme
			// clock and it does not put the outgoing bitmap back.
			if a.swapAt.IsZero() {
				a.armSwap(p.Now())
			}
		} else if a.previewBM == nil {
			a.previewBM = previewBitmap(a.lookTarget, pw, ph)
			ensureGradient(a.lookTarget, pw, ph)
		}
	}
	now := p.Now()
	u := clockU(now, a.backdropFadeAt, motionTheme)
	// planFade is the picture on screen: a frozen blend under the new
	// layer, or one opaque bitmap. The new preview stays at the clock
	// until the full-resolution fade retires that base.
	fromA := float32(0)
	previewA := float32(0)
	fullA := float32(0)
	var fromBM, previewBM, fullBM *ui.Bitmap
	for _, layer := range a.planFade(now) {
		if layer.bm == nil || layer.alpha <= 0 {
			continue
		}
		drawBackdropLayer(p, r, hole, layer)
		switch {
		case layer.bm == a.previewBM:
			previewA = layer.alpha
			previewBM = layer.bm
			themeLogDrawn(themeEventPreview, layer.bm)
			if u > 0 {
				a.previewed = true
			}
		case layer.bm == a.fullBM:
			fullA = layer.alpha
			fullBM = layer.bm
			themeLogDrawn(themeEventFull, layer.bm)
		case layer.bm == a.fadeFromBM && len(a.baseStack) == 0:
			fromA = layer.alpha
			fromBM = layer.bm
		default:
			// A frozen stack is the base. It is not the from-layer, so a
			// later switch cannot treat one slice of the blend as the
			// whole picture.
			a.baseDrawn = true
		}
	}
	themeLogBackdrop(pw, ph, fromA, fromBM, previewA, previewBM, fullA, fullBM)
}

func drawBackdropLayer(p *ui.Painter, r, hole ui.Rect, layer fadeSlice) {
	paintBackdropBitmap(p, layer.bm, nil, r, hole, layer.alpha)
}

// paintHeldBackdrop draws the theme the clock already reached. The size
// map may still be pointing at the previous theme; painting that is the flash.
func (a *App) paintHeldBackdrop(p *ui.Painter, r, hole ui.Rect, pw, ph int) {
	if pw > 0 && ph > 0 {
		if e := cachedGradient(a.lookTarget, pw, ph); e != nil {
			pinGradientShown(a.lookTarget, pw, ph)
			paintBackdropBitmap(p, e.bm, e.img, r, hole, 1)
			themeLogDrawn(themeEventFull, e.bm)
			themeLogBackdrop(pw, ph, 0, nil, 0, nil, 1, e.bm)
			return
		}
		ensureGradient(a.lookTarget, pw, ph)
	}
	if a.fullBM != nil {
		paintBackdropBitmap(p, a.fullBM, nil, r, hole, 1)
		themeLogDrawn(themeEventFull, a.fullBM)
		themeLogBackdrop(pw, ph, 0, nil, 0, nil, 1, a.fullBM)
		return
	}
	if a.previewBM != nil {
		paintBackdropBitmap(p, a.previewBM, nil, r, hole, 1)
		themeLogDrawn(themeEventPreview, a.previewBM)
		themeLogBackdrop(pw, ph, 0, nil, 1, a.previewBM, 0, nil)
	}
}

// veilEnds is the veil's from/to palettes and the shared clock position.
// swapping stays false: a real veil that arrives after the clock snaps
// in on the frame it is ready, instead of starting a second fade.
func (a *App) veilEnds(now time.Time) (from, to Theme, fading, waiting bool, u float32, swapping bool, swapU float32) {
	if a.instant || a.reduceMotion {
		return a.look, a.look, false, false, 0, false, 0
	}
	if a.veilStandIn {
		return a.veilFrom, a.lookTarget, true, false, 1, false, 0
	}
	if a.fadeLive {
		return a.veilFrom, a.veilTo, true, false, clockU(now, a.backdropFadeAt, motionTheme), false, 0
	}
	return a.look, a.look, false, false, 0, false, 0
}

// backdrop is the theme the window bitmap fades to. The bitmap is the
// target palette, never the in-between control mix.
func (a *App) backdrop() Theme {
	if a.haveLook {
		return a.lookTarget
	}
	return a.look
}

func normalizeAppearance(mode string) string {
	switch mode {
	case appearanceLight, appearanceDark, appearanceSystem:
		return mode
	default:
		return appearanceSystem
	}
}

// cardShadowShot selects shadow.layer/content while a test is rendering.
// 0 is the product. 1 draws no card shadow. 2 draws the retired wide
// layer (0,6,22,-2), dark at black 25%, which banded the sidebar and the
// bottom gap.
var cardShadowShot int

// contentCardShadow paints SPEC shadow.layer/content. Dark mode keeps the
// tight layer only: shadow/card is black 20%, and the 1px content stroke
// separates the card. Light mode adds 0,2,8,-4 in the same shadow/card
// token. Both stay inside the 8pt margin, so the sidebar and the
// window bottom keep the bare background.
func contentCardShadow(card ui.Element, p Theme) {
	switch cardShadowShot {
	case 1:
		return
	case 2:
		c := p.ShadowCard
		if p.Dark {
			c = ui.Color{A: 0x40}
		}
		card.Shadow(0, 1, 2, 0, c)
		card.Shadow(0, 6, 22, -2, c)
		return
	}
	card.Shadow(0, 1, 2, 0, p.ShadowCard)
	if p.Dark {
		return
	}
	card.Shadow(0, 2, 8, -4, p.ShadowCard)
}

// fadeColors cross-fades background and border over the theme duration.
// Reduce Motion skips the transition so the switch is instant. MyGo still
// animates colors when a transition is set and Reduce Motion is on, so
// the transition is omitted entirely in that case.
func (a *App) fadeColors(e ui.Element) {
	if a.instant || a.reduceMotion || a.suppressFade {
		return
	}
	e.Transition(ui.ElementTransition{
		Colors:   true,
		Duration: motionTheme,
		Ease:     easeLinear,
	})
}
