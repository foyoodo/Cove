package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestCustomDraftHidesZeroCounter(t *testing.T) {
	draft := customFormApp(t, false)
	tt := openTester(draft)
	if tt.HasText("0 of 0 shown") {
		t.Fatalf("untested draft showed a counter: %q", tt.Texts())
	}
	if !tt.HasText("Models") || !tt.HasText("Refresh") || !tt.HasText("Add model…") {
		t.Fatalf("models section %q", tt.Texts())
	}

	empty := customFormApp(t, false)
	empty.finishProviderTest("Together AI", "", nil, 200, false, nil, 12, time.Now())
	et := openTester(empty)
	if et.HasText("0 of 0 shown") {
		t.Fatalf("empty fetch showed a counter: %q", et.Texts())
	}
	if !et.HasText("Connected · 0 models · 12 ms") || !et.HasText("Add model…") {
		t.Fatalf("empty fetch %q", et.Texts())
	}

	listed := customFormApp(t, true)
	lt := openTester(listed)
	if !lt.HasText("2 of 2 shown") {
		t.Fatalf("fetched list hid the counter: %q", lt.Texts())
	}

	dir := "/cursor/stores/self/artifacts/zero-counter"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{false, true} {
		a := customFormApp(t, false)
		view := ui.NewTester(a.view, 1120, 760)
		if dark {
			view.SetDark(true)
		}
		view.SetScale(2)
		a.formScroll.Y = 1e9
		view.Frame()
		if view.HasText("0 of 0 shown") {
			t.Fatal("crop still shows the empty counter")
		}
		if _, ok := view.Find("Add model…"); !ok {
			t.Fatalf("empty state missing: %q", view.Texts())
		}
		writePNG(t, filepath.Join(dir, "draft-"+modeName(dark)+".png"), view.Image())
	}
}
