package main

import (
	"cove/themes"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
)

const hScrollFade = 24

// hScrollSlot is one code block or table scroller. probed asks for a
// second frame so MaxX from layout is known before the fades are drawn.
type hScrollSlot struct {
	st     ui.ScrollState
	probed bool
}

func (a *App) hScrollKey(kind string, index int) string {
	return a.hScrollMsg + "/" + kind + "/" + itoa(index)
}

func (a *App) hScrollSlot(key string) *hScrollSlot {
	if a.hScrolls == nil {
		a.hScrolls = map[string]*hScrollSlot{}
	}
	slot := a.hScrolls[key]
	if slot == nil {
		slot = &hScrollSlot{}
		a.hScrolls[key] = slot
	}
	return slot
}

// scrollEdge is wash, the block's own background, composited over the
// flat card. A translucent token such as code/bg still fades to the
// surface the text sits on.
func scrollEdge(wash, flat ui.Color) ui.Color {
	if wash.A == 0 {
		return flat
	}
	return wash.Over(flat)
}

func (a *App) cardFlat() ui.Color {
	return themes.ContentFlat(a.data.Theme, a.look.Dark)
}

// tableFadeShader fades scrolled table content to transparent at the
// viewport edges. Backgrounds stay in a layer behind it, so each row
// dissolves into its own surface and the fade has no shape of its own.
var tableFadeShader = shader.MustCompile(`package main

var Scroll float
var ViewW float
var Fade float
var FadeL float
var FadeR float

func ColorEffect(pos vec2, color vec4) vec4 {
	x := pos.x - Scroll
	k := float(1)
	if Fade > 0 && ViewW > 1 {
		if FadeL > 0 && x < Fade {
			k = x / Fade
		}
		if FadeR > 0 {
			r := ViewW - x
			if r < Fade && r/Fade < k {
				k = r / Fade
			}
		}
		if k < 0 {
			k = 0
		}
	}
	return color * k
}
`)

// tableContentFade is the alpha mask for one table's scrolling content.
// Zero when nothing overflows or the viewport is not known yet.
func tableContentFade(c *ui.Context, slot *hScrollSlot) ui.Effect {
	if !slot.probed {
		slot.probed = true
		c.After(0)
	}
	vw := slot.st.Bounds(c).W
	fadeL, fadeR := float32(0), float32(0)
	if vw > 1 && slot.st.MaxX > 1 {
		if slot.st.X > 1 {
			fadeL = 1
		}
		if slot.st.X < slot.st.MaxX-1 {
			fadeR = 1
		}
	}
	if fadeL == 0 && fadeR == 0 {
		return nil
	}
	return tableFadeShader.Effect(shader.Options{Uniforms: map[string]any{
		"Scroll": slot.st.X,
		"ViewW":  vw,
		"Fade":   float32(hScrollFade),
		"FadeL":  fadeL,
		"FadeR":  fadeR,
	}})
}

// hScrollFades paints 24pt edge fades over a horizontal scroller, from
// clear at the inner edge to the block's own background at the outer
// edge. Code blocks use this. Tables do not: one wash would paint over
// rows whose background is not that wash. The fades do not take the pointer.
func hScrollFades(c *ui.Context, slot *hScrollSlot, name string, edge ui.Color) {
	if !slot.probed {
		slot.probed = true
		c.After(0)
	}
	const eps = 1
	st := &slot.st
	if st.MaxX <= eps {
		return
	}
	if st.X > eps {
		paintHFade(c, true, name, edge)
	}
	if st.X < st.MaxX-eps {
		paintHFade(c, false, name, edge)
	}
}

func paintHFade(c *ui.Context, left bool, name string, edge ui.Color) {
	box := ui.Box(c).Absolute().Top(0).Bottom(0).Width(hScrollFade).PassThrough()
	side := "right"
	if left {
		side = "left"
	}
	box.Label("hscroll fade " + side + " " + name)
	if left {
		box.Left(0)
	} else {
		box.Right(0)
	}
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		p.FillGradient(r, hFadeGrad(left, edge), 0)
	})
}

func hFadeGrad(left bool, edge ui.Color) ui.LinearGradient {
	clear := edge
	clear.A = 0
	if left {
		return ui.LinearGradient{From: edge, To: clear, Angle: 90}
	}
	return ui.LinearGradient{From: clear, To: edge, Angle: 90}
}
