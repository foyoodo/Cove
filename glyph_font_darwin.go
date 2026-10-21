//go:build darwin

package main

import (
	"unsafe"

	"github.com/ebitengine/purego"
)

var (
	ctCopyFamily     func(font uintptr) uintptr
	cfGetCString     func(str uintptr, buf *byte, n int, enc uint32) bool
	cfRelease        func(uintptr)
	darwinFontNames  = true
)

func init() {
	ct, err := purego.Dlopen("/System/Library/Frameworks/CoreText.framework/CoreText", purego.RTLD_LAZY)
	if err != nil {
		darwinFontNames = false
		return
	}
	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_LAZY)
	if err != nil {
		darwinFontNames = false
		return
	}
	purego.RegisterLibFunc(&ctCopyFamily, ct, "CTFontCopyFamilyName")
	purego.RegisterLibFunc(&cfGetCString, cf, "CFStringGetCString")
	purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
}

// kCFStringEncodingUTF8
const cfStringUTF8 = 0x08000100

func fontFamilyName(native uintptr) string {
	if !darwinFontNames || native == 0 || ctCopyFamily == nil {
		return ""
	}
	str := ctCopyFamily(native)
	if str == 0 {
		return ""
	}
	defer cfRelease(str)
	buf := make([]byte, 256)
	if !cfGetCString(str, &buf[0], len(buf), cfStringUTF8) {
		return ""
	}
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	return unsafe.String(&buf[0], n)
}
