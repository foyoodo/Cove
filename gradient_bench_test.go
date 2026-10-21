package main

import (
	"testing"

	"cove/themes"
)

func BenchmarkGradientGen(b *testing.B) {
	look := themeLook(themes.ThemeTide, false)
	b.ReportAllocs()
	for b.Loop() {
		img := renderGradient(look, 2880, 1800, true)
		if img == nil || len(img.Pix) == 0 {
			b.Fatal("empty")
		}
	}
}
