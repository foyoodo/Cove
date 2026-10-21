package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// pulse is a looping opacity from minA to maxA. delay shifts a dot in a
// row. Reduce Motion holds the bright end so nothing breathes.
func (a *App) pulse(c *ui.Context, delay, period time.Duration, minA, maxA float32) float32 {
	if a.reduceMotion || period <= 0 {
		return maxA
	}
	if !a.instant {
		c.After(32 * time.Millisecond)
	}
	ms := c.Now().UnixMilli() + delay.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	span := period.Milliseconds()
	if span <= 0 {
		return maxA
	}
	t := float64(ms%span) / float64(span)
	wave := float32(0.5 - 0.5*math.Cos(2*math.Pi*t))
	return minA + (maxA-minA)*wave
}

func (a *App) streamStatus(c *ui.Context, label, extra string) {
	p := a.look
	ui.Row(c).Gap(8).AlignItems(ui.Center).Unselectable().Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			for i := range 3 {
				ui.Box(c).Size(6, 6).Radius(3).Background(p.ThemeMark).Opacity(a.pulse(c, time.Duration(i)*200*time.Millisecond, motionDots, dotsMin, 1))
			}
		})
		ui.Text(c, label).FontSize(11).TextColor(p.TextSecondary)
		if extra != "" {
			controlLine(c, extra, 11, 0, 400, p.TextTertiary)
		}
	})
}

func sidebarWhen(now, t time.Time) string {
	if rel := relativeTime(now, t); rel != "" {
		return rel
	}
	if t.IsZero() {
		return ""
	}
	if t.Year() == now.Year() {
		return t.Format("Jan 2")
	}
	return t.Format("Jan 2006")
}
