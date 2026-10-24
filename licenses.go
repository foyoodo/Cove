package main

import (
	_ "embed"
	"strings"

	"github.com/egoist/mygo/ui"
)

//go:generate go run ./notices .

//go:embed third_party_notices.txt
var thirdPartyNoticesTxt string

// thirdPartyNotice is one bundled dependency and the license text that
// shipped with its module (or with the Go toolchain, for the standard library).
type thirdPartyNotice struct {
	Name, Module, Version, License string
}

func thirdPartyNotices() []thirdPartyNotice {
	var out []thirdPartyNotice
	rest := thirdPartyNoticesTxt
	for rest != "" {
		if !strings.HasPrefix(rest, "@@NAME ") {
			break
		}
		var n thirdPartyNotice
		n.Name, rest = noticeField(rest, "@@NAME ")
		n.Module, rest = noticeField(rest, "@@MODULE ")
		n.Version, rest = noticeField(rest, "@@VERSION ")
		const body = "@@LICENSE\n"
		if !strings.HasPrefix(rest, body) {
			break
		}
		rest = rest[len(body):]
		end := strings.Index(rest, "\n@@END\n")
		if end < 0 {
			n.License = strings.TrimSpace(rest)
			out = append(out, n)
			break
		}
		n.License = strings.TrimSpace(rest[:end])
		rest = rest[end+len("\n@@END\n"):]
		out = append(out, n)
	}
	return out
}

func noticeField(s, prefix string) (value, rest string) {
	s = s[len(prefix):]
	line, rest, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line), rest
}

// acknowledgementsSheet lists the licenses embedded in this build.
// Esc and Close dismiss it.
func (a *App) acknowledgementsSheet(c *ui.Context) {
	if !a.acksOpen {
		return
	}
	if c.Shortcut(0, ui.KeyEscape) {
		a.closeAcks()
		return
	}
	p := a.look
	_, winH := c.Size()
	maxH := winH - 96
	if maxH > 560 {
		maxH = 560
	}
	if maxH < 280 {
		maxH = 280
	}
	scrim := ui.Box(c).Absolute().Left(0).Right(0).Top(0).Bottom(0).Background(p.LayerScrim).Label("acknowledgements scrim")
	scrim.Children(func() {
		ui.Column(c).Fill().Center().Children(func() {
			panel := ui.Column(c).Width(520).MaxHeight(maxH).Radius(radiusComposer).Background(p.LayerSheet).Border(0.5, p.LayerOverlayBorder).Clip().Label("acknowledgements")
			panel.Shadow(0, 2, 6, 0, p.ShadowSm).Shadow(0, 24, 60, -8, p.ShadowLg)
			panel.Children(func() {
				ui.Column(c).Padding(16, 20, 4, 20).Gap(2).FillWidth().Children(func() {
					ui.Text(c, "Acknowledgements").FontSize(15).FontWeight(600)
					ui.Text(c, "Third-party software bundled with Cove.").FontSize(12).TextColor(p.TextSecondary)
				})
				ui.Scroll(c).FillWidth().Grow(1).Children(func() {
					ui.Column(c).FillWidth().Padding(8, 20, 16, 20).Gap(16).Children(func() {
						for _, n := range thirdPartyNotices() {
							notice := n
							ui.Column(c).FillWidth().Gap(4).Children(func() {
								ui.Text(c, notice.Name).FontSize(13).FontWeight(600)
								ui.Text(c, notice.Module+" "+notice.Version).FontSize(12).TextColor(p.TextSecondary).SingleLine().Ellipsis("…")
								ui.Text(c, notice.License).FontSize(11).TextColor(p.TextSecondary)
							})
						}
					})
				})
				ui.Row(c).Padding(12, 20, 16, 20).Justify(ui.End).BorderWidth(1, 0, 0, 0).BorderColor(p.SeparatorDefault).Children(func() {
					if a.sheetButton(c, "Close", true).Clicked() {
						a.closeAcks()
					}
				})
			})
		})
	})
}
