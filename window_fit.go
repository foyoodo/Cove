package main

// windowFitMargin is the gap left on each side of a new Windows or Linux
// window inside the monitor work area. The window is never smaller than
// MinWidth × MinHeight.
const windowFitMargin = 24

// fitWindowSize clamps wantW×wantH to the work area minus margin on each
// side, without going below minW×minH. A missing work area leaves the
// requested size unchanged.
func fitWindowSize(wantW, wantH, minW, minH, workW, workH, margin int) (w, h int) {
	w, h = wantW, wantH
	if workW <= 0 || workH <= 0 {
		return w, h
	}
	maxW := workW - 2*margin
	maxH := workH - 2*margin
	if maxW < minW {
		maxW = minW
	}
	if maxH < minH {
		maxH = minH
	}
	if w > maxW {
		w = maxW
	}
	if h > maxH {
		h = maxH
	}
	if w < minW {
		w = minW
	}
	if h < minH {
		h = minH
	}
	return w, h
}
