package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/egoist/mygo/ui"
)

// Theme debug is off unless COVE_DEBUG_THEME=1 or the user default
// CoveDebugTheme is true. The flag is read once at startup. While it is
// off, the paint and worker paths only load this flag.

const coveBundleID = "dev.cove.chat"

const (
	themeEventSwitch     = "switch requested"
	themeEventBuildStart = "build started"
	themeEventBuildDone  = "build done"
	themeEventPreview    = "preview shown"
	themeEventFull       = "full-res presented"
	themeEventVeil       = "veil presented"
	themeEventResize     = "resize regen"
	themeEventFrame      = "frame"
)

var themeDebugOn atomic.Bool

var (
	themeDebugMu       sync.Mutex
	themeDebugName     string
	themeDebugMode     string
	themeDebugGen      map[uint64]uint64
	themeDebugLogged   map[string]map[uint64]struct{}
	themeDebugSeen     map[uint64]struct{} // image ids whose texture was uploaded
	themeDebugFile     *os.File
	themeDebugOpened   string
	themeDebugOverride atomic.Value // string, full path; tests set it
	themeDebugErr      io.Writer
	themeDebugWrites   atomic.Int64
)

func init() {
	if os.Getenv("COVE_DEBUG_THEME") == "1" || coveDebugThemeDefault() {
		themeDebugOn.Store(true)
	}
}

// themeDebugDefaultValue parses the text `defaults read` prints for a bool.
func themeDebugDefaultValue(s string) bool {
	switch stringsTrim(s) {
	case "1", "true", "TRUE", "yes", "YES":
		return true
	default:
		return false
	}
}

func stringsTrim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\t' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\n' || s[len(s)-1] == '\t' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func themeDebugPath() string {
	if v := themeDebugOverride.Load(); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, "Library", "Logs", "Cove", "theme-debug.log")
}

// bitmapImageID is the scene.Image id Metal and GL use as the texture key.
// Bitmap's first field is that image. A zero id means there is no bitmap.
func bitmapImageID(b *ui.Bitmap) uint64 {
	if b == nil {
		return 0
	}
	img := *(*unsafe.Pointer)(unsafe.Pointer(b))
	if img == nil {
		return 0
	}
	return *(*uint64)(img)
}

func rememberBitmap(b *ui.Bitmap, gen uint64) {
	if !themeDebugOn.Load() || b == nil {
		return
	}
	id := bitmapImageID(b)
	themeDebugMu.Lock()
	if themeDebugGen == nil {
		themeDebugGen = map[uint64]uint64{}
	}
	themeDebugGen[id] = gen
	themeDebugMu.Unlock()
}

func (a *App) debugAppearance() string {
	mode := normalizeAppearance(a.data.Appearance)
	if mode != appearanceSystem {
		return mode
	}
	dark := a.dark
	if a.haveLook {
		dark = a.lookTarget.Dark
	}
	if dark {
		return "system/dark"
	}
	return "system/light"
}

func (a *App) syncThemeDebug() {
	if !themeDebugOn.Load() {
		return
	}
	themeDebugMu.Lock()
	themeDebugName = a.data.Theme
	themeDebugMode = a.debugAppearance()
	themeDebugMu.Unlock()
}

func (a *App) logSwitchRequested() {
	if !themeDebugOn.Load() {
		return
	}
	a.syncThemeDebug()
	themeLog(themeEventSwitch, a.winPW, a.winPH, 0, 0)
}

func themeLog(event string, w, h int, gen, imageID uint64) {
	if !themeDebugOn.Load() {
		return
	}
	themeDebugMu.Lock()
	theme, mode := themeDebugName, themeDebugMode
	themeDebugMu.Unlock()
	themeLogWrite(event, theme, mode, w, h, gen, imageID)
}

// themeLogDrawn logs event the first time bm is the bitmap drawn for it.
// Later frames that keep the same texture stay quiet.
func themeLogDrawn(event string, bm *ui.Bitmap) {
	if !themeDebugOn.Load() || bm == nil {
		return
	}
	id := bitmapImageID(bm)
	themeDebugMu.Lock()
	if themeDebugLogged == nil {
		themeDebugLogged = map[string]map[uint64]struct{}{}
	}
	seen := themeDebugLogged[event]
	if seen == nil {
		seen = map[uint64]struct{}{}
		themeDebugLogged[event] = seen
	}
	if _, ok := seen[id]; ok {
		themeDebugMu.Unlock()
		return
	}
	seen[id] = struct{}{}
	gen := themeDebugGen[id]
	theme, mode := themeDebugName, themeDebugMode
	themeDebugMu.Unlock()
	w, h := bm.Size()
	themeLogWrite(event, theme, mode, w, h, gen, id)
}

func themeLogBitmap(event string, bm *ui.Bitmap, gen uint64) {
	if !themeDebugOn.Load() || bm == nil {
		return
	}
	w, h := bm.Size()
	themeLog(event, w, h, gen, bitmapImageID(bm))
}

func themeLogWrite(event, theme, appearance string, w, h int, gen, imageID uint64) {
	if theme == "" {
		theme = "-"
	}
	if appearance == "" {
		appearance = "-"
	}
	line := fmt.Sprintf("%s %s theme=%s appearance=%s size=%dx%d gen=%d image=%d\n",
		time.Now().Format("2006-01-02 15:04:05.000"), event, theme, appearance, w, h, gen, imageID)
	themeDebugMu.Lock()
	defer themeDebugMu.Unlock()
	themeDebugWrites.Add(1)
	errw := themeDebugErr
	if errw == nil {
		errw = os.Stderr
	}
	io.WriteString(errw, line)
	path := themeDebugPath()
	if path == "" {
		return
	}
	if themeDebugFile == nil || themeDebugOpened != path {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		if themeDebugFile != nil {
			themeDebugFile.Close()
		}
		themeDebugFile = f
		themeDebugOpened = path
	}
	themeDebugFile.WriteString(line)
}

// themeLogBackdrop records one painted frame's background layers.
// Each layer is alpha/image-id/upload. upload is 1 the first time that
// image id is drawn while the log is on, which is when Metal allocates
// the texture. Later frames that reuse it log upload=0. Off, this only
// loads the flag.
func themeLogBackdrop(w, h int, fromA float32, from *ui.Bitmap, previewA float32, preview *ui.Bitmap, fullA float32, full *ui.Bitmap) {
	if !themeDebugOn.Load() {
		return
	}
	themeDebugMu.Lock()
	if themeDebugSeen == nil {
		themeDebugSeen = map[uint64]struct{}{}
	}
	slot := func(a float32, bm *ui.Bitmap) string {
		if bm == nil || a <= 0 {
			return "0.000/0/upload=0"
		}
		id := bitmapImageID(bm)
		up := 0
		if id != 0 {
			if _, ok := themeDebugSeen[id]; !ok {
				themeDebugSeen[id] = struct{}{}
				up = 1
			}
		}
		return fmt.Sprintf("%.3f/%d/upload=%d", a, id, up)
	}
	event := themeEventFrame + " from=" + slot(fromA, from) + " preview=" + slot(previewA, preview) + " full=" + slot(fullA, full)
	theme, mode := themeDebugName, themeDebugMode
	themeDebugMu.Unlock()
	themeLogWrite(event, theme, mode, w, h, 0, 0)
}
