// Command bgdiff compares background pixels in two renders.
//
// Content is masked out. A pixel is content when it lies inside one of the
// rectangles in -mask (x,y,w,h in pixels, semicolon-separated) or when the
// optional mask image is black there. Every other pixel is background and
// must match. The tool prints the number of differing background pixels and
// the maximum per-channel difference, and exits 1 when any background pixel
// differs.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strconv"
	"strings"
)

func main() {
	aPath := flag.String("a", "", "first PNG")
	bPath := flag.String("b", "", "second PNG")
	maskPath := flag.String("mask", "", "optional mask PNG; black pixels are content and are ignored")
	rects := flag.String("ignore", "", "content rectangles x,y,w,h in pixels, separated by semicolons")
	flag.Parse()
	if *aPath == "" || *bPath == "" {
		fmt.Fprintln(os.Stderr, "usage: bgdiff -a before.png -b after.png [-ignore x,y,w,h;...] [-mask mask.png]")
		os.Exit(2)
	}
	a := load(*aPath)
	b := load(*bPath)
	if !a.Bounds().Eq(b.Bounds()) {
		fmt.Fprintf(os.Stderr, "size %v vs %v\n", a.Bounds(), b.Bounds())
		os.Exit(2)
	}
	var ignore []image.Rectangle
	if *rects != "" {
		for _, part := range strings.Split(*rects, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			ignore = append(ignore, parseRect(part))
		}
	}
	var mask *image.Gray
	if *maskPath != "" {
		mask = loadGray(*maskPath)
		if !mask.Bounds().Eq(a.Bounds()) {
			fmt.Fprintf(os.Stderr, "mask size %v vs %v\n", mask.Bounds(), a.Bounds())
			os.Exit(2)
		}
	}
	diff, maxD, compared := 0, 0, 0
	bounds := a.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if masked(x, y, ignore, mask) {
				continue
			}
			compared++
			pa := a.RGBAAt(x, y)
			pb := b.RGBAAt(x, y)
			d := channelDelta(pa, pb)
			if d == 0 {
				continue
			}
			diff++
			if d > maxD {
				maxD = d
			}
		}
	}
	fmt.Printf("compared %d background pixels, differ %d, max channel delta %d\n", compared, diff, maxD)
	if diff != 0 {
		os.Exit(1)
	}
}

func masked(x, y int, ignore []image.Rectangle, mask *image.Gray) bool {
	p := image.Point{x, y}
	for _, r := range ignore {
		if p.In(r) {
			return true
		}
	}
	if mask != nil && mask.GrayAt(x, y).Y < 128 {
		return true
	}
	return false
}

func channelDelta(a, b color.RGBA) int {
	d := abs(int(a.R) - int(b.R))
	if g := abs(int(a.G) - int(b.G)); g > d {
		d = g
	}
	if bl := abs(int(a.B) - int(b.B)); bl > d {
		d = bl
	}
	return d
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func parseRect(s string) image.Rectangle {
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		fmt.Fprintf(os.Stderr, "rectangle %q wants x,y,w,h\n", s)
		os.Exit(2)
	}
	n := make([]int, 4)
	for i, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			fmt.Fprintf(os.Stderr, "rectangle %q: %v\n", s, err)
			os.Exit(2)
		}
		n[i] = v
	}
	return image.Rect(n[0], n[1], n[0]+n[2], n[1]+n[3])
}

func load(path string) *image.RGBA {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if r, ok := img.(*image.RGBA); ok {
		return r
	}
	b := img.Bounds()
	out := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			out.Set(x, y, img.At(x, y))
		}
	}
	return out
}

func loadGray(path string) *image.Gray {
	src := load(path)
	b := src.Bounds()
	out := image.NewGray(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := src.RGBAAt(x, y)
			out.SetGray(x, y, color.Gray{Y: c.R})
		}
	}
	return out
}
