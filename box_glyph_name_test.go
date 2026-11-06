package main

import (
	"fmt"
	"strings"
	"testing"

	"golang.org/x/text/unicode/runenames"
)

func TestBoxGlyphsMatchUnicodeNames(t *testing.T) {
	var mismatches []string
	checked := 0
	for r := rune(boxMin); r <= boxMax; r++ {
		name := runenames.Name(r)
		want, ok := boxGlyphFromUnicodeName(name)
		if !ok {
			t.Errorf("U+%04X %q: could not derive arms from the Unicode name", r, name)
			continue
		}
		got := lookupBox(r)
		checked++
		if got != want {
			mismatches = append(mismatches, fmt.Sprintf(
				"U+%04X %s: got NESW=%s dash=%d v=%v arc=%d diag=%d want NESW=%s dash=%d v=%v arc=%d diag=%d",
				r, name, neswString(got), got.dash, got.dashV, got.arc, got.diag,
				neswString(want), want.dash, want.dashV, want.arc, want.diag,
			))
		}
	}
	if checked < 128 {
		t.Fatalf("derived names for %d/128 glyphs", checked)
	}
	if len(mismatches) > 0 {
		t.Fatalf("%d NESW mismatches:\n%s", len(mismatches), strings.Join(mismatches, "\n"))
	}
}

func TestBoxGlyphsLeftTeeWeights(t *testing.T) {
	cases := []struct {
		r    rune
		nesw string
		name string
	}{
		{0x2525, "L.LH", "BOX DRAWINGS VERTICAL LIGHT AND LEFT HEAVY"},
		{0x2527, "L.HL", "BOX DRAWINGS DOWN HEAVY AND LEFT UP LIGHT"},
		{0x2528, "H.HL", "BOX DRAWINGS VERTICAL HEAVY AND LEFT LIGHT"},
		{0x2529, "H.LH", "BOX DRAWINGS DOWN LIGHT AND LEFT UP HEAVY"},
	}
	for _, tc := range cases {
		got := lookupBox(tc.r)
		if neswString(got) != tc.nesw {
			t.Errorf("U+%04X got %s want %s (%s)", tc.r, neswString(got), tc.nesw, tc.name)
		}
		if runenames.Name(tc.r) != tc.name {
			t.Errorf("U+%04X name %q want %q", tc.r, runenames.Name(tc.r), tc.name)
		}
	}
}

func neswString(g boxGlyph) string {
	let := func(w uint8) byte {
		switch w {
		case 1:
			return 'L'
		case 2:
			return 'H'
		case 3:
			return 'D'
		default:
			return '.'
		}
	}
	return string([]byte{let(g.n), let(g.e), let(g.s), let(g.w)})
}

func boxGlyphFromUnicodeName(name string) (boxGlyph, bool) {
	const prefix = "BOX DRAWINGS "
	if !strings.HasPrefix(name, prefix) {
		return boxGlyph{}, false
	}
	rest := strings.TrimPrefix(name, prefix)
	switch {
	case strings.Contains(rest, "DASH"):
		return dashGlyphFromName(rest)
	case strings.Contains(rest, "ARC"):
		return arcGlyphFromName(rest)
	case strings.Contains(rest, "DIAGONAL"):
		return diagGlyphFromName(rest)
	default:
		return junctionGlyphFromName(rest)
	}
}

func dashGlyphFromName(rest string) (boxGlyph, bool) {
	n := uint8(2)
	switch {
	case strings.Contains(rest, "QUADRUPLE"):
		n = 4
	case strings.Contains(rest, "TRIPLE"):
		n = 3
	case strings.Contains(rest, "DOUBLE"):
		n = 2
	default:
		return boxGlyph{}, false
	}
	heavy := strings.Contains(rest, "HEAVY")
	vert := strings.Contains(rest, "VERTICAL")
	w := uint8(1)
	if heavy {
		w = 2
	}
	g := boxGlyph{dash: n, dashV: vert}
	if vert {
		g.n, g.s = w, w
	} else {
		g.e, g.w = w, w
	}
	return g, true
}

func arcGlyphFromName(rest string) (boxGlyph, bool) {
	down := strings.Contains(rest, "DOWN")
	up := strings.Contains(rest, "UP")
	left := strings.Contains(rest, "LEFT")
	right := strings.Contains(rest, "RIGHT")
	switch {
	case down && right:
		return boxGlyph{arc: 1, e: 1, s: 1}, true
	case down && left:
		return boxGlyph{arc: 2, w: 1, s: 1}, true
	case up && left:
		return boxGlyph{arc: 3, w: 1, n: 1}, true
	case up && right:
		return boxGlyph{arc: 4, e: 1, n: 1}, true
	default:
		return boxGlyph{}, false
	}
}

func diagGlyphFromName(rest string) (boxGlyph, bool) {
	switch {
	case strings.Contains(rest, "CROSS"):
		return boxGlyph{diag: 3}, true
	case strings.Contains(rest, "UPPER RIGHT TO LOWER LEFT"):
		return boxGlyph{diag: 1}, true
	case strings.Contains(rest, "UPPER LEFT TO LOWER RIGHT"):
		return boxGlyph{diag: 2}, true
	default:
		return boxGlyph{}, false
	}
}

func junctionGlyphFromName(rest string) (boxGlyph, bool) {
	var g boxGlyph
	var dirs []int
	var prefix uint8
	ok := false
	apply := func(w uint8) {
		if len(dirs) == 0 {
			prefix = w
			return
		}
		for _, d := range dirs {
			setBoxArm(&g, d, w)
		}
		dirs = dirs[:0]
		prefix = 0
		ok = true
	}
	flush := func() {
		if len(dirs) == 0 {
			return
		}
		w := prefix
		if w == 0 {
			w = 1
		}
		for _, d := range dirs {
			setBoxArm(&g, d, w)
		}
		dirs = dirs[:0]
		ok = true
	}
	for _, word := range strings.Fields(rest) {
		switch word {
		case "AND":
			flush()
		case "LIGHT", "SINGLE":
			apply(1)
		case "HEAVY":
			apply(2)
		case "DOUBLE":
			apply(3)
		case "HORIZONTAL":
			dirs = append(dirs, 1, 3)
		case "VERTICAL":
			dirs = append(dirs, 0, 2)
		case "DOWN":
			dirs = append(dirs, 2)
		case "UP":
			dirs = append(dirs, 0)
		case "LEFT":
			dirs = append(dirs, 3)
		case "RIGHT":
			dirs = append(dirs, 1)
		default:
			return boxGlyph{}, false
		}
	}
	flush()
	return g, ok
}

func setBoxArm(g *boxGlyph, d int, w uint8) {
	switch d {
	case 0:
		g.n = w
	case 1:
		g.e = w
	case 2:
		g.s = w
	case 3:
		g.w = w
	}
}
