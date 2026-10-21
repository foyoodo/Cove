// Colors are filled from tokens.json. Do not hand-write palette hexes.
package themes

import "github.com/egoist/mygo/ui"

// Colors is one mode of a theme. Each field maps 1:1 to a tokens.json
// color name through its token tag. tide/default and tide/soft are gone;
// the mark color is theme/mark and depends on the preset.
type Colors struct {
	AccentDefault         ui.Color `token:"accent/default"`
	AccentHover           ui.Color `token:"accent/hover"`
	AccentPressed         ui.Color `token:"accent/pressed"`
	AccentRing            ui.Color `token:"accent/ring"`
	AccentSoft            ui.Color `token:"accent/soft"`
	BubbleUser            ui.Color `token:"bubble/user"`
	BubbleUserText        ui.Color `token:"bubble/userText"`
	ButtonDestructiveBg   ui.Color `token:"button/destructiveBg"`
	ButtonDestructiveText ui.Color `token:"button/destructiveText"`
	ButtonPrimaryBg       ui.Color `token:"button/primaryBg"`
	ButtonPrimaryText     ui.Color `token:"button/primaryText"`
	CodeBg                ui.Color `token:"code/bg"`
	CodeBorder            ui.Color `token:"code/border"`
	CodeHeader            ui.Color `token:"code/header"`
	CodeInlineBg          ui.Color `token:"code/inlineBg"`
	CodeInlineText        ui.Color `token:"code/inlineText"`
	CodeText              ui.Color `token:"code/text"`
	ControlBg             ui.Color `token:"control/bg"`
	ControlBorder         ui.Color `token:"control/border"`
	ControlField          ui.Color `token:"control/field"`
	ControlFieldBorder    ui.Color `token:"control/fieldBorder"`
	ControlKnob           ui.Color `token:"control/knob"`
	ControlSegSelected    ui.Color `token:"control/segSelected"`
	FillHover             ui.Color `token:"fill/hover"`
	FillPressed           ui.Color `token:"fill/pressed"`
	FillQuaternary        ui.Color `token:"fill/quaternary"`
	InverseBg             ui.Color `token:"inverse/bg"`
	LayerComposer         ui.Color `token:"layer/composer"`
	LayerComposerBorder   ui.Color `token:"layer/composerBorder"`
	LayerContent          ui.Color `token:"layer/content"`
	LayerContentBorder    ui.Color `token:"layer/contentBorder"`
	LayerDialogBorder     ui.Color `token:"layer/dialogBorder"`
	LayerDialogScrim      ui.Color `token:"layer/dialogScrim"`
	LayerGroup            ui.Color `token:"layer/group"`
	LayerOverlay          ui.Color `token:"layer/overlay"`
	LayerOverlayBorder    ui.Color `token:"layer/overlayBorder"`
	LayerScrim            ui.Color `token:"layer/scrim"`
	LayerSheet            ui.Color `token:"layer/sheet"`
	ProviderAnthropic     ui.Color `token:"provider/anthropic"`
	ProviderCustom        ui.Color `token:"provider/custom"`
	ProviderDeepseek      ui.Color `token:"provider/deepseek"`
	ProviderOllama        ui.Color `token:"provider/ollama"`
	ProviderOpenai        ui.Color `token:"provider/openai"`
	ProviderOpenrouter    ui.Color `token:"provider/openrouter"`
	ProviderXai           ui.Color `token:"provider/xai"`
	SelectionRow          ui.Color `token:"selection/row"`
	SelectionRowMuted     ui.Color `token:"selection/rowMuted"`
	SelectionText         ui.Color `token:"selection/text"`
	SeparatorDefault      ui.Color `token:"separator/default"`
	ShadowCard            ui.Color `token:"shadow/card"`
	ShadowLg              ui.Color `token:"shadow/lg"`
	ShadowMd              ui.Color `token:"shadow/md"`
	ShadowSm              ui.Color `token:"shadow/sm"`
	SliderTrack           ui.Color `token:"slider/track"`
	StatusDanger          ui.Color `token:"status/danger"`
	StatusDangerSoft      ui.Color `token:"status/dangerSoft"`
	StatusSuccess         ui.Color `token:"status/success"`
	StatusWarning         ui.Color `token:"status/warning"`
	StatusWarningSoft     ui.Color `token:"status/warningSoft"`
	SwitchOff             ui.Color `token:"switch/off"`
	SyntaxComment         ui.Color `token:"syntax/comment"`
	SyntaxFunction        ui.Color `token:"syntax/function"`
	SyntaxKeyword         ui.Color `token:"syntax/keyword"`
	SyntaxNumber          ui.Color `token:"syntax/number"`
	SyntaxPunct           ui.Color `token:"syntax/punct"`
	SyntaxString          ui.Color `token:"syntax/string"`
	SyntaxType            ui.Color `token:"syntax/type"`
	TableHeader           ui.Color `token:"table/header"`
	TableRule             ui.Color `token:"table/rule"`
	TextLink              ui.Color `token:"text/link"`
	TextOnAccent          ui.Color `token:"text/onAccent"`
	TextOnInverse         ui.Color `token:"text/onInverse"`
	TextPrimary           ui.Color `token:"text/primary"`
	TextSecondary         ui.Color `token:"text/secondary"`
	TextTertiary          ui.Color `token:"text/tertiary"`
	ThemeMark             ui.Color `token:"theme/mark"`
	ThinkText             ui.Color `token:"think/text"`
	TideOnAvatar          ui.Color `token:"tide/onAvatar"`
	WindowBaseBottom      ui.Color `token:"window/baseBottom"`
	WindowBaseLow         ui.Color `token:"window/baseLow"`
	WindowBaseMid         ui.Color `token:"window/baseMid"`
	WindowBaseTint        ui.Color `token:"window/baseTint"`
	WindowBaseTop         ui.Color `token:"window/baseTop"`
	WindowStroke          ui.Color `token:"window/stroke"`
}

// Tide returns the Tide preset. Warm is the default theme; Tide is one of
// the six presets, not the mark color for every theme.
func Tide(dark bool) Colors { return Palette(ThemeTide, dark) }

func TideLight() Colors { return Tide(false) }
func TideDark() Colors  { return Tide(true) }
