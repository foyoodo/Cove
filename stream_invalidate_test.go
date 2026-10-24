package main

import (
	"testing"

	"cove/themes"

	"github.com/egoist/mygo/ui"
)

// The first scroll of a reply is the 1,500-token structured stream. A
// first pass whose p95 is inside the cap and whose max is one slow frame
// is sampled again once. Both caps still apply to that second pass.

func TestStreamAfterLaunch(t *testing.T) {
	a, tt, fp, builds := openStream(t, 2880, 1800, 1, 0, true)
	assertFirstPass(t, "after launch", func() []streamSample {
		return measureStream(t, a, tt, fp, builds, streamTokens(1500))
	})
}

func TestStreamAfterTheme(t *testing.T) {
	a, tt, fp, builds := openStream(t, 2880, 1800, 1, 0, false)
	a.setTheme(themes.ThemeTide, tt.Now())
	tt.Frame()
	awaitGradients()
	awaitVeils()
	tt.Frame()
	assertFirstPass(t, "after theme", func() []streamSample {
		return measureStream(t, a, tt, fp, builds, streamTokens(1500))
	})
}

func settleNewSize(tt *ui.Tester) {
	tt.Frame()
	for i := 0; i < 4; i++ {
		tt.Advance(gradSettle)
		tt.Frame()
		awaitGradients()
		awaitVeils()
	}
}

func TestStreamAfterResize(t *testing.T) {
	a, tt, fp, builds := openStream(t, 1440, 900, 1, 0, false)
	tt.SetSize(2880, 1800)
	settleNewSize(tt)
	assertFirstPass(t, "after resize", func() []streamSample {
		return measureStream(t, a, tt, fp, builds, streamTokens(1500))
	})
}

func TestStreamAfterWidthSwitch(t *testing.T) {
	a, tt, fp, builds := openStream(t, 720, 900, 1, 0, false)
	tt.SetSize(2880, 1800)
	settleNewSize(tt)
	assertFirstPass(t, "after 720 to full screen", func() []streamSample {
		return measureStream(t, a, tt, fp, builds, streamTokens(1500))
	})
}
