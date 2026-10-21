package main

import (
	"strings"
	"unicode/utf8"
	"unsafe"

	"github.com/egoist/mygo/ui"
)

// The offsets below are the MyGo v0.5.5 ui.node and ui.state layouts.
// elementLookup reaches the unexported node so a message label can be
// tied to the paragraphs SelectedTexts returns. Bump these if the pin moves.

const (
	nodeEpoch  = 0
	nodeID     = 40
	nodeKind   = 48
	nodeFlags  = 52
	nodeFirst  = 64
	nodeNext   = 80
	nodeState  = 104
	nodeLabel  = 1264
	nodeSerial = 1732
	nodeText   = 928

	statePressed = 300
	stateEditor  = 960

	// editor.layout is the text.Layout the last frame painted. originX/Y
	// are the content origin inside the element, where that layout sits.
	editorLayout  = 712
	editorOriginX = 772
	editorOriginY = 776

	// text.Layout / Line / Glyph, the geometry Selection paints from.
	layoutLines      = 176
	lineSize         = 152
	lineY            = 4
	lineW            = 8
	lineH            = 12
	lineX            = 0
	lineStart        = 32
	lineEnd          = 40
	lineRTL          = 48
	layoutLineGlyphs = 56
	glyphSize        = 56
	glyphX           = 16
	glyphAdv         = 24
	glyphCluster     = 32
	glyphRunes       = 40
	glyphRTL         = 48

	nodeKindText         = 1
	nodeFlagUnselectable = 1 << 31
)

// elementLookup is ui.Element.lookup. The compiler's symbol is the
// pointer form even though the method takes the element by value, and the
// call has to pass a pointer or the receiver arrives empty.
//
//go:linkname elementLookup github.com/egoist/mygo/ui.(*Element).lookup
func elementLookup(e *ui.Element) unsafe.Pointer

func readPtr(p unsafe.Pointer, off uintptr) unsafe.Pointer {
	if p == nil {
		return nil
	}
	return *(*unsafe.Pointer)(unsafe.Pointer(uintptr(p) + off))
}

func readU64(p unsafe.Pointer, off uintptr) uint64 {
	return *(*uint64)(unsafe.Pointer(uintptr(p) + off))
}

func readU32(p unsafe.Pointer, off uintptr) uint32 {
	return *(*uint32)(unsafe.Pointer(uintptr(p) + off))
}

func readU8(p unsafe.Pointer, off uintptr) byte {
	return *(*byte)(unsafe.Pointer(uintptr(p) + off))
}

func readI32(p unsafe.Pointer, off uintptr) int32 {
	return *(*int32)(unsafe.Pointer(uintptr(p) + off))
}

func readInt(p unsafe.Pointer, off uintptr) int {
	return *(*int)(unsafe.Pointer(uintptr(p) + off))
}

func readF32(p unsafe.Pointer, off uintptr) float32 {
	return *(*float32)(unsafe.Pointer(uintptr(p) + off))
}

func readString(p unsafe.Pointer, off uintptr) string {
	type gs struct {
		p unsafe.Pointer
		n int
	}
	s := *(*gs)(unsafe.Pointer(uintptr(p) + off))
	if s.n <= 0 || s.p == nil {
		return ""
	}
	return unsafe.String((*byte)(s.p), s.n)
}

// retarget points sample's element header at another node from this pass
// so exported Bounds can be read without marking the text clickable.
func retarget(sample ui.Element, serial int32, epoch uint64) ui.Element {
	type hdr struct {
		owner unsafe.Pointer
		slot  uint32
		gen   uint32
	}
	h := *(*hdr)(unsafe.Pointer(&sample))
	if serial <= 0 {
		return ui.Element{}
	}
	h.slot = uint32(serial - 1)
	h.gen = uint32(epoch)
	return *(*ui.Element)(unsafe.Pointer(&h))
}

// indexQuoteNodes maps selectable paragraph ids under a "qmsg:<id>" label
// to that message, and reports whether one of those paragraphs is pressed.
func indexQuoteNodes(root ui.Element) (map[uint64]quoteAnchor, bool) {
	top := elementLookup(&root)
	if top == nil {
		return nil, false
	}
	out := map[uint64]quoteAnchor{}
	dragging := false
	var walk func(p unsafe.Pointer, msg, lang string)
	walk = func(p unsafe.Pointer, msg, lang string) {
		for p != nil {
			next := readPtr(p, nodeNext)
			childMsg, childLang := msg, lang
			if label := readString(p, nodeLabel); strings.HasPrefix(label, "qmsg:") {
				childMsg = label[len("qmsg:"):]
			} else if strings.HasPrefix(label, "qcode:") {
				childLang = label[len("qcode:"):]
			}
			skip := readU32(p, nodeFlags)&nodeFlagUnselectable != 0
			if !skip && childMsg != "" && readU8(p, nodeKind) == nodeKindText {
				id := readU64(p, nodeID)
				el := retarget(root, readI32(p, nodeSerial), readU64(p, nodeEpoch))
				anc := quoteAnchor{msgID: childMsg, code: childLang != "", lang: childLang}
				if el.Valid() {
					anc.bounds = el.Bounds()
				}
				out[id] = anc
				if st := readPtr(p, nodeState); st != nil && readU8(st, statePressed) != 0 {
					dragging = true
				}
			}
			if !skip {
				walk(readPtr(p, nodeFirst), childMsg, childLang)
			}
			p = next
		}
	}
	walk(top, "", "")
	return out, dragging
}

// selectionLineRects is the highlight the engine paints for parts: one
// rectangle per visual run, in window DIPs. It reads the layout Selection
// uses (glyph advances of the selected clusters) and does not fall back
// to the paragraph box.
func selectionLineRects(root ui.Element, parts []ui.SelectedText) []ui.Rect {
	if len(parts) == 0 {
		return nil
	}
	top := elementLookup(&root)
	if top == nil {
		return nil
	}
	want := map[uint64]ui.SelectedText{}
	for _, p := range parts {
		if p.End > p.Start {
			want[p.ID] = p
		}
	}
	var out []ui.Rect
	var walk func(p unsafe.Pointer)
	walk = func(p unsafe.Pointer) {
		for p != nil {
			if readU8(p, nodeKind) == nodeKindText {
				if part, ok := want[readU64(p, nodeID)]; ok {
					out = append(out, nodeLineRects(root, p, part.Start, part.End)...)
				}
			}
			walk(readPtr(p, nodeFirst))
			p = readPtr(p, nodeNext)
		}
	}
	walk(top)
	return out
}

// phraseLineRects is the painted span of phrase inside root, for a drag
// that has to start on those glyphs.
func phraseLineRects(root ui.Element, phrase string) []ui.Rect {
	if phrase == "" {
		return nil
	}
	top := elementLookup(&root)
	if top == nil {
		return nil
	}
	var hit []ui.Rect
	var walk func(p unsafe.Pointer)
	walk = func(p unsafe.Pointer) {
		for p != nil {
			if hit == nil && readU8(p, nodeKind) == nodeKindText {
				s := readString(p, nodeText)
				if i := strings.Index(s, phrase); i >= 0 {
					start := utf8.RuneCountInString(s[:i])
					end := start + utf8.RuneCountInString(phrase)
					hit = nodeLineRects(root, p, start, end)
				}
			}
			if hit == nil {
				walk(readPtr(p, nodeFirst))
			}
			p = readPtr(p, nodeNext)
		}
	}
	walk(top)
	return hit
}

func nodeLineRects(root ui.Element, node unsafe.Pointer, start, end int) []ui.Rect {
	if start >= end {
		return nil
	}
	st := readPtr(node, nodeState)
	if st == nil {
		return nil
	}
	ed := readPtr(st, stateEditor)
	if ed == nil {
		return nil
	}
	lay := readPtr(ed, editorLayout)
	if lay == nil {
		return nil
	}
	el := retarget(root, readI32(node, nodeSerial), readU64(node, nodeEpoch))
	if !el.Valid() {
		return nil
	}
	box := el.Bounds()
	ox := box.X + readF32(ed, editorOriginX)
	oy := box.Y + readF32(ed, editorOriginY)
	local := layoutSelection(lay, start, end)
	out := make([]ui.Rect, len(local))
	for i, r := range local {
		out[i] = ui.Rect{X: ox + r.X, Y: oy + r.Y, W: r.W, H: r.H}
	}
	return out
}

type sliceHdr struct {
	p    unsafe.Pointer
	n, c int
}

func readSlice(p unsafe.Pointer, off uintptr) sliceHdr {
	return *(*sliceHdr)(unsafe.Pointer(uintptr(p) + off))
}

// layoutSelection is text.Layout.Selection: the rectangles covering runes
// start..end, from each line's glyph advances. on is false, matching the
// highlight painted for selectable text.
func layoutSelection(l unsafe.Pointer, start, end int) []ui.Rect {
	if start > end {
		start, end = end, start
	}
	lines := readSlice(l, layoutLines)
	if lines.n == 0 || lines.p == nil {
		return nil
	}
	var out []ui.Rect
	for li := 0; li < lines.n; li++ {
		line := unsafe.Pointer(uintptr(lines.p) + uintptr(li)*lineSize)
		lstart := readInt(line, lineStart)
		lend := readInt(line, lineEnd)
		if lend < start || lstart >= end {
			continue
		}
		glyphs := readSlice(line, layoutLineGlyphs)
		var pieces []ui.Rect
		y := readF32(line, lineY)
		h := readF32(line, lineH)
		for i := 0; i < glyphs.n; {
			g := unsafe.Pointer(uintptr(glyphs.p) + uintptr(i)*glyphSize)
			x0 := readF32(g, glyphX)
			x1 := x0 + readF32(g, glyphAdv)
			cluster := readInt(g, glyphCluster)
			runes := readInt(g, glyphRunes)
			rtl := readU8(g, glyphRTL) != 0
			i++
			for i < glyphs.n {
				g2 := unsafe.Pointer(uintptr(glyphs.p) + uintptr(i)*glyphSize)
				if readInt(g2, glyphCluster) != cluster || readInt(g2, glyphRunes) != runes {
					break
				}
				gx := readF32(g2, glyphX)
				x0 = min(x0, gx)
				x1 = max(x1, gx+readF32(g2, glyphAdv))
				i++
			}
			a, z := max(start, cluster), min(end, cluster+runes)
			if a >= z || runes <= 0 {
				continue
			}
			width := x1 - x0
			left := float32(a-cluster) / float32(runes)
			right := float32(z-cluster) / float32(runes)
			if rtl {
				left, right = 1-right, 1-left
			}
			r := ui.Rect{X: x0 + left*width, Y: y, W: (right - left) * width, H: h}
			if n := len(pieces); n > 0 && pieces[n-1].X+pieces[n-1].W >= r.X-0.01 && pieces[n-1].X <= r.X+0.01 {
				pieces[n-1].W = max(pieces[n-1].X+pieces[n-1].W, r.X+r.W) - pieces[n-1].X
				continue
			}
			pieces = append(pieces, r)
		}
		if start <= lend && end > lend && li < lines.n-1 {
			next := unsafe.Pointer(uintptr(lines.p) + uintptr(li+1)*lineSize)
			if readInt(next, lineStart) > lend {
				x := readF32(line, lineX) + readF32(line, lineW)
				width := h / 3
				if readU8(line, lineRTL) != 0 {
					x = readF32(line, lineX) - width
				}
				pieces = append(pieces, ui.Rect{X: x, Y: y, W: width, H: h})
			}
		}
		for i := 1; i < len(pieces); i++ {
			for j := i; j > 0 && pieces[j-1].X > pieces[j].X; j-- {
				pieces[j-1], pieces[j] = pieces[j], pieces[j-1]
			}
		}
		for _, r := range pieces {
			if r.W <= 0 {
				continue
			}
			if n := len(out); n > 0 && out[n-1].Y == r.Y && out[n-1].X+out[n-1].W >= r.X-0.01 {
				out[n-1].W = max(out[n-1].X+out[n-1].W, r.X+r.W) - out[n-1].X
			} else {
				out = append(out, r)
			}
		}
	}
	return out
}
