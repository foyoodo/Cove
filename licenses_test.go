package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestThirdPartyNoticesIncludeMyGo(t *testing.T) {
	notes := thirdPartyNotices()
	if len(notes) < 5 {
		t.Fatalf("notices: %+v", notes)
	}
	var sawMyGo, sawGo bool
	for _, n := range notes {
		if n.Name == "MyGo" && n.Module == "github.com/egoist/mygo" && strings.Contains(n.License, "MIT License") {
			sawMyGo = true
		}
		if n.Name == "Go" && strings.Contains(n.License, "The Go Authors") {
			sawGo = true
		}
		if n.License == "" || n.Version == "" {
			t.Fatalf("empty notice %+v", n)
		}
	}
	if !sawMyGo || !sawGo {
		t.Fatalf("missing MyGo or Go: %+v", notes)
	}
}

func TestAcknowledgementsSheet(t *testing.T) {
	a := newTestApp(t)
	a.settingsOpen = true
	tt := ui.NewTester(a.view, 780, 560)
	a.formScroll.Y = 1e9
	tt.SetSize(780, 900)
	if err := tt.Click("View acknowledgements"); err != nil {
		t.Fatal(err)
	}
	if !a.acksOpen || !tt.HasText("MyGo") || !tt.HasText("github.com/egoist/mygo v0.5.5") {
		t.Fatalf("sheet: open %v texts %q", a.acksOpen, tt.Texts())
	}
	if err := tt.Click("Close"); err != nil {
		t.Fatal(err)
	}
	if a.acksOpen {
		t.Fatal("close left the sheet up")
	}
}
