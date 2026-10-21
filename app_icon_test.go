package main

import (
	"image"
	"testing"

	"cove/themes"

	"github.com/egoist/mygo/ui"
)

func TestReviewDraftSubtitleFitsCard(t *testing.T) {
	if cardH != 60 {
		t.Fatalf("card height %v", cardH)
	}
	for _, s := range starters {
		if s.Title != "Review a draft" {
			continue
		}
		if s.Detail != "Gaps and edits, line by line." {
			t.Fatalf("detail %q", s.Detail)
		}
		return
	}
	t.Fatal("missing Review a draft")
}

func TestSuggestionSubtitlesStayCentered(t *testing.T) {
	a := newTestApp(t)
	a.newChat()
	tt := ui.NewTester(a.view, 720, 520)
	assertSubtitlesCentered(t, tt, "Gaps and edits, line by line.")

	for i := range starters {
		if starters[i].Title == "Review a draft" {
			starters[i].Detail = "What I might be missing, line by line, with suggested edits that run on."
		}
	}
	t.Cleanup(func() {
		for i := range starters {
			if starters[i].Title == "Review a draft" {
				starters[i].Detail = "Gaps and edits, line by line."
			}
		}
	})
	const longDetail = "What I might be missing, line by line, with suggested edits that run on."
	long := ui.NewTester(a.view, 720, 520)
	sub := assertSubtitlesCentered(t, long, longDetail)
	short := mustFind(t, tt, "Gaps and edits, line by line.")
	if abs32(sub.H-short.H) > 2 {
		t.Fatalf("long subtitle grew to %.1f, short is %.1f", sub.H, short.H)
	}
	full, _ := ui.ShapeText(longDetail, ui.Font{Size: 12}, 0).Size()
	if sub.W > full-8 {
		t.Fatalf("long subtitle %.1f was not shortened from %.1f", sub.W, full)
	}
	card := mustFind(t, long, "starter Review a draft")
	if inkIn(long.Image(), card.X+card.W-8, card.Y+8, card.X+card.W-1, card.Y+card.H-8) {
		t.Fatalf("subtitle ink runs into the card edge %v", card)
	}
}

func inkIn(img image.Image, x0, y0, x1, y1 float32) bool {
	b := img.Bounds()
	for y := int(y0); y < int(y1); y++ {
		if y < b.Min.Y || y >= b.Max.Y {
			continue
		}
		for x := int(x0); x < int(x1); x++ {
			if x < b.Min.X || x >= b.Max.X {
				continue
			}
			r, g, b, a := img.At(x, y).RGBA()
			if a > 200*257 && r < 80*257 && g < 80*257 && b < 80*257 {
				return true
			}
		}
	}
	return false
}

func assertSubtitlesCentered(t *testing.T, tt *ui.Tester, reviewDetail string) ui.Rect {
	t.Helper()
	review := mustFind(t, tt, "starter Review a draft")
	outline := mustFind(t, tt, "starter Outline a plan")
	reviewTitle := mustFind(t, tt, "Review a draft")
	outlineTitle := mustFind(t, tt, "Outline a plan")
	detail := mustFind(t, tt, reviewDetail)
	if detail.Y < review.Y || detail.Y+detail.H > review.Y+review.H-1 {
		t.Fatalf("subtitle %v leaves card %v", detail, review)
	}
	if reviewTitle.Y < review.Y || reviewTitle.Y+reviewTitle.H > review.Y+review.H-1 {
		t.Fatalf("title %v leaves card %v", reviewTitle, review)
	}
	if abs32((reviewTitle.Y-review.Y)-(outlineTitle.Y-outline.Y)) > 2 {
		t.Fatalf("review title inset %.1f, outline inset %.1f", reviewTitle.Y-review.Y, outlineTitle.Y-outline.Y)
	}
	if detail.H > reviewTitle.H+6 {
		t.Fatalf("subtitle is more than one line: %.1f title %.1f", detail.H, reviewTitle.H)
	}
	return detail
}

func TestAppIconKeepsOriginalColors(t *testing.T) {
	w, h := coveAppIcon.Size()
	if w != 1024 || h != 1024 {
		t.Fatalf("bitmap %dx%d", w, h)
	}
	a := newTestApp(t)
	a.data.Theme = themes.ThemeWarm
	a.newChat()
	hero := ui.NewTester(a.view, 1120, 760)
	assertBundledIcon(t, hero, "empty hero")

	a.settingsOpen = true
	a.settingsSection = "general"
	about := ui.NewTester(a.view, 780, 900)
	if !about.HasText("Version 0.7.0 · macOS 26") {
		a.formScroll.Y = 1e9
		about.SetSize(780, 900)
	}
	assertBundledIcon(t, about, "about")
	if about.HasText("Check for Updates…") {
		t.Fatal("update button is visible")
	}
}

func assertBundledIcon(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	r, ok := tt.Find("Cove app icon")
	if !ok || r.W < 30 || r.H < 30 {
		t.Fatalf("%s icon %v texts %q", name, r, tt.Texts())
	}
	img := tt.Image()
	if !regionHas(img, r, tideWave) {
		t.Fatalf("%s icon has no tide wave", name)
	}
	if !regionHas(img, r, graphite) {
		t.Fatalf("%s icon has no graphite base", name)
	}
}

func tideWave(r, g, b uint32) bool {
	return g > 180*257 && g > r+60*257 && b > r+40*257 && b > 140*257
}

func graphite(r, g, b uint32) bool {
	return r > 25*257 && r < 70*257 && g > 28*257 && g < 75*257 && b > 30*257 && b < 80*257 && r+8*257 > g && g+8*257 > b
}

func regionHas(img image.Image, r ui.Rect, match func(uint32, uint32, uint32) bool) bool {
	b := img.Bounds()
	x0, y0 := int(r.X+r.W*0.2), int(r.Y+r.H*0.2)
	x1, y1 := int(r.X+r.W*0.8), int(r.Y+r.H*0.8)
	for y := y0; y < y1; y++ {
		if y < b.Min.Y || y >= b.Max.Y {
			continue
		}
		for x := x0; x < x1; x++ {
			if x < b.Min.X || x >= b.Max.X {
				continue
			}
			cr, cg, cb, ca := img.At(x, y).RGBA()
			if ca < 200*257 {
				continue
			}
			if match(cr, cg, cb) {
				return true
			}
		}
	}
	return false
}
