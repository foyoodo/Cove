package main

import (
	"cove/themes"
	"fmt"
	"hash/crc32"
	"runtime"
	"testing"
)

func TestGradientHash(t *testing.T) {
	want := map[string]uint32{
		"warm false":    0xce46eabb,
		"tide false":    0x511411f9,
		"neutral false": 0xff84b455,
		"warm true":     0x6dd551fa,
		"tide true":     0xa6ad9a5e,
		"neutral true":  0x07bcf818,
	}
	for _, dark := range []bool{false, true} {
		for _, id := range []string{themes.ThemeWarm, themes.ThemeTide, themes.ThemeNeutral} {
			img := renderGradient(themeLook(id, dark), 2880, 1800, true)
			key := id + " " + fmt.Sprint(dark)
			sum := crc32.ChecksumIEEE(img.Pix)
			if sum != want[key] {
				t.Errorf("%s hash %08x want %08x", key, sum, want[key])
			}
		}
	}
	img := renderGradient(themeLook(themes.ThemeWarm, false), 720, 520, true)
	if sum := crc32.ChecksumIEEE(img.Pix); sum != 0xf8252628 {
		t.Errorf("small hash %08x", sum)
	}
}

// TestGradientStripsMatchSerial renders awkward widths on one processor
// and on the host's processor count. One processor stays in the serial
// clamp; several processors split the bitmap into vertical strips. The
// pixels have to be the same, including a run that crosses a strip edge.
func TestGradientStripsMatchSerial(t *testing.T) {
	looks := []struct {
		name string
		look Theme
	}{
		{"warm", themeLook(themes.ThemeWarm, false)},
		{"tide-dark", themeLook(themes.ThemeTide, true)},
		{"neutral", themeLook(themes.ThemeNeutral, false)},
	}
	sizes := [][2]int{{320, 200}, {321, 180}, {479, 90}, {1000, 700}, {2881, 64}, {333, 200}}
	prev := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(prev)
	serial := map[string][]byte{}
	for _, look := range looks {
		for _, sz := range sizes {
			img := renderGradient(look.look, sz[0], sz[1], true)
			key := fmt.Sprintf("%s %dx%d", look.name, sz[0], sz[1])
			serial[key] = append([]byte(nil), img.Pix...)
		}
	}
	runtime.GOMAXPROCS(prev)
	if prev < 2 {
		t.Skip("host has one processor; strips are not used")
	}
	for _, look := range looks {
		for _, sz := range sizes {
			img := renderGradient(look.look, sz[0], sz[1], true)
			key := fmt.Sprintf("%s %dx%d", look.name, sz[0], sz[1])
			want := serial[key]
			if len(img.Pix) != len(want) {
				t.Fatalf("%s len %d want %d", key, len(img.Pix), len(want))
			}
			for i := range want {
				if img.Pix[i] != want[i] {
					t.Fatalf("%s pixel byte %d got %d want %d", key, i, img.Pix[i], want[i])
				}
			}
		}
	}
}
