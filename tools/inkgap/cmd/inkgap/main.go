// Command inkgap prints sub-pixel ink gaps in a delivered PNG.
//
//	go run ./tools/inkgap/cmd/inkgap -scale 2 composer-light-after.png
//
// Scale is the device pixels per point the bitmap was rendered at.
// Each band is one line of text. Gaps are points between half-coverage
// edges of successive glyphs, from the anti-aliased ramp in the file.
package main

import (
	"flag"
	"fmt"
	"image/png"
	"os"

	"cove/tools/inkgap"
)

func main() {
	scale := flag.Float64("scale", 2, "device pixels per point")
	flag.Parse()
	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: inkgap -scale 2 <image.png>")
		os.Exit(2)
	}
	f, err := os.Open(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	bands := inkgap.Measure(img, *scale)
	if len(bands) == 0 {
		fmt.Fprintln(os.Stderr, "no text ink")
		os.Exit(1)
	}
	for i, b := range bands {
		fmt.Printf("band %d y %d-%d glyphs %d\n", i, b.Y0, b.Y1, len(b.Edges))
		for g, e := range b.Edges {
			fmt.Printf("  glyph %d left %.2f right %.2f\n", g, e.Left, e.Right)
		}
		for g, gap := range b.Gaps {
			fmt.Printf("  gap %d  %.3f pt\n", g, gap)
		}
	}
	fmt.Println("focus")
	for i, gap := range inkgap.FocusGaps(img, *scale) {
		fmt.Printf("  gap %d  %.3f pt\n", i, gap)
	}
}
