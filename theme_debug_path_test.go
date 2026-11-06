package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo"
)

func TestThemeDebugPathUsesPathLogs(t *testing.T) {
	themeDebugOverride.Store("")
	got := themeDebugPath()
	dir, err := mygo.App.Path(mygo.PathLogs)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "theme-debug.log")
	if got != want {
		t.Fatalf("themeDebugPath() = %q, want %q", got, want)
	}
	switch runtime.GOOS {
	case "darwin":
		if !strings.Contains(got, filepath.Join("Library", "Logs")) {
			t.Fatalf("darwin log path %q does not use Library/Logs", got)
		}
	case "windows":
		// UserConfigDir is %AppData% (Roaming). PathLogs is <that>/<app>/logs.
		if !strings.Contains(strings.ToLower(got), `\logs\`) && !strings.Contains(strings.ToLower(got), `/logs/`) {
			t.Fatalf("windows log path %q does not use a logs directory", got)
		}
	default:
		if !strings.Contains(got, string(filepath.Separator)+"logs"+string(filepath.Separator)) {
			t.Fatalf("linux log path %q does not use XDG/config logs", got)
		}
	}
}
