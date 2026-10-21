package main

import (
	"cove/themes"

	"github.com/egoist/mygo/ui"
)

// paletteBoard is the design's color sheet. The swatch panel is a content
// card on the warm window background. It is only built for the headless
// screenshot that matches palette-light.png and palette-dark.png.
func (a *App) paletteBoard(c *ui.Context) {
	p := a.look
	ui.Column(c).Fill().Padding(28, 24).Gap(16).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Cove v0.7 · Tide").FontSize(15).FontWeight(600)
			ui.Text(c, "Opaque base · translucent layers · sheet and menus solid · accent follows the system · Tide only for AI").FontSize(12).TextColor(p.TextSecondary)
		})
		card := ui.Column(c).FillWidth().Padding(18, 20).Gap(14).Radius(radiusCard).
			Background(p.LayerContent).Border(1, p.LayerContentBorder)
		contentCardShadow(card, p)
		card.Children(func() {
			a.paletteRow(c, "Base (gorex, top → bottom)", func() {
				base := windowVertical(p)
				base.Angle = 90
				ui.Box(c).Size(180, 28).Radius(6).LinearGradient(base)
				ui.Text(c, "opaque, title bar included").FontSize(11).TextColor(p.TextSecondary)
			})
			a.paletteRow(c, "Opaque exception", func() {
				a.swatch(c, "layer/sheet", p.LayerSheet)
				a.swatch(c, "layer/overlay", p.LayerOverlay)
				ui.Text(c, "settings sheet · menus, pop-ups, popovers — solid, nothing shows through").FontSize(11).TextColor(p.TextSecondary)
			})
			a.paletteRow(c, "Layers on base", func() {
				a.swatch(c, "layer/content", p.LayerContent)
				a.swatch(c, "layer/composer", p.LayerComposer)
				a.swatch(c, "bubble/user", p.BubbleUser)
				a.swatch(c, "code/bg", p.CodeBg)
				a.swatch(c, "table/header", p.TableHeader)
				a.swatch(c, "layer/group", p.LayerGroup)
				a.swatch(c, "selection/row", p.SelectionRow)
				a.swatch(c, "selection/rowMuted", p.SelectionRowMuted)
				a.swatch(c, "fill/hover", p.FillHover)
				a.swatch(c, "fill/pressed", p.FillPressed)
			})
			a.paletteRow(c, "Text", func() {
				ui.Text(c, "Primary").FontSize(13)
				ui.Text(c, "Secondary").FontSize(13).TextColor(p.TextSecondary)
				ui.Text(c, "Tertiary").FontSize(13).TextColor(p.TextTertiary)
				ui.Text(c, "Link").FontSize(13).TextColor(p.TextLink)
			})
			a.paletteRow(c, "Theme mark", func() {
				ui.Icon(c, iconSpark).Size(16, 16).TextColor(p.ThemeMark)
				for range 3 {
					ui.Box(c).Size(6, 6).Radius(3).Background(p.ThemeMark)
				}
				ui.Box(c).Size(2, 16).Radius(1).Background(p.ThemeMark)
				a.mark(c, 22)
			})
			ui.Box(c).Height(1).FillWidth().Background(p.SeparatorDefault)
			for _, id := range themes.AccentIDs() {
				name := id
				if len(name) > 0 {
					name = string(name[0]-32) + name[1:]
				}
				a.paletteRow(c, name, func() {
					for _, focused := range []bool{true, false} {
						src, _ := themes.NamedAccent(id, p.Dark)
						look := themes.Resolve(a.data.Theme, p.Dark, focused, src)
						label := "active"
						if !focused {
							label = "inactive"
						}
						ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Text(c, "Save").FontSize(12).FontWeight(500).Padding(4, 10).Radius(radiusRow).Background(look.ButtonPrimaryBg).TextColor(look.ButtonPrimaryText)
							ui.Box(c).Size(32, 18).Radius(9).Background(look.AccentDefault).Padding(1)
							ui.Row(c).Height(28).Width(150).Gap(8).Padding(0, 8).Radius(radiusRow).Background(look.SelectionRow).AlignItems(ui.Center).Children(func() {
								ui.Box(c).Size(6, 6).Radius(3).Background(p.ProviderOpenai)
								ui.Text(c, "Selected chat").FontSize(13)
							})
							ui.Text(c, "Link").FontSize(13).TextColor(look.TextLink)
							ui.Text(c, label).FontSize(11).TextColor(p.TextTertiary)
						})
					}
				})
			}
		})
	})
}

func (a *App) paletteRow(c *ui.Context, label string, fn func()) {
	p := a.look
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(p.TextSecondary).Width(140)
		fn()
	})
}

func (a *App) swatch(c *ui.Context, label string, color ui.Color) {
	p := a.look
	ui.Column(c).Gap(4).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(36, 28).Radius(6).Background(color).Border(1, p.SeparatorDefault)
		ui.Text(c, label).FontSize(9).TextColor(p.TextTertiary)
	})
}
