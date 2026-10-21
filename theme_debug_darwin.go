//go:build darwin

package main

import "os/exec"

// coveDebugThemeDefault reads the bool set by:
//
//	defaults write dev.cove.chat CoveDebugTheme -bool true
func coveDebugThemeDefault() bool {
	out, err := exec.Command("defaults", "read", coveBundleID, "CoveDebugTheme").Output()
	if err != nil {
		return false
	}
	return themeDebugDefaultValue(string(out))
}
