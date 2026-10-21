package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestFindScreenshots(t *testing.T) {
	dir := filepath.Join("/opt/cursor/artifacts", "find")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	shot := func(dark bool, w, h int, name string, setup func(a *App, tt *ui.Tester)) {
		t.Helper()
		a := revisionChat(t)
		a.menu()
		tt := ui.NewTester(a.view, w, h)
		if dark {
			tt.SetDark(true)
		}
		tt.Frame()
		setup(a, tt)
		writePNG(t, filepath.Join(dir, name), tt.Image())
	}
	openQuery := func(q string) func(*App, *ui.Tester) {
		return func(a *App, tt *ui.Tester) {
			tt.Key(ui.Cmd, ui.KeyF)
			tt.Frame()
			if q != "" {
				tt.Type(q)
				waitFind(t, a)
				tt.Frame()
			}
		}
	}
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		shot(dark, 1120, 760, "find-bar-"+mode+".png", openQuery("context"))
		shot(dark, 1120, 760, "find-bar-scrolled-"+mode+".png", func(a *App, tt *ui.Tester) {
			// Extra turns so the list can move a line up into the bar.
			conv := a.active()
			for i := 0; i < 16; i++ {
				conv.Messages = append(conv.Messages, Message{
					ID: fmt.Sprintf("pad-%s-%d", mode, i), Role: roleUser, Done: true,
					Content:   fmt.Sprintf("Scrolled line %d runs under the find bar and must stay hidden.", i),
					CreatedAt: conv.Messages[0].CreatedAt,
				})
			}
			openQuery("")(a, tt)
			bar, ok := tt.Find("find bar")
			if !ok {
				t.Fatal("find bar missing")
			}
			// Wheel over the transcript, right of the empty-query field, so the
			// list moves and a line sits in the bar's band.
			for i := 0; i < 6; i++ {
				tt.Scroll(bar.X+bar.W-40, bar.Y+180, 0, 220)
				tt.Frame()
			}
		})
		shot(dark, 1120, 760, "find-empty-"+mode+".png", openQuery("zzqx"))
		shot(dark, 720, 520, "find-min-"+mode+".png", openQuery("context"))
		shot(dark, 1120, 760, "find-jump-"+mode+".png", func(a *App, tt *ui.Tester) {
			openQuery("context")(a, tt)
			if len(a.finder.hits) < 3 {
				t.Fatalf("%s hits %d", mode, len(a.finder.hits))
			}
			a.jumpFind(2, tt.Now())
			tt.Frame()
			for i := 0; i < 8 && !a.finder.settled; i++ {
				tt.Frame()
			}
			tt.Advance(700 * time.Millisecond)
			tt.Frame()
		})
		shot(dark, 1120, 760, "find-flash-"+mode+".png", func(a *App, tt *ui.Tester) {
			openQuery("context")(a, tt)
			a.jumpFind(2, tt.Now())
			tt.Frame()
			for i := 0; i < 8 && !a.finder.settled; i++ {
				tt.Frame()
			}
		})
	}
}

// TestFindR2Shots writes the designer review of the field width and the
// single-layer flash peak, light and dark, at 1120 and at the 720 minimum.
func TestFindR2Shots(t *testing.T) {
	dir := "/cursor/stores/self/artifacts/find-r2"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sizes := []struct {
		w, h int
		name string
	}{
		{1120, 760, "1120"},
		{720, 520, "720"},
	}
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		for _, sz := range sizes {
			a := revisionChat(t)
			a.menu()
			tt := ui.NewTester(a.view, sz.w, sz.h)
			if dark {
				tt.SetDark(true)
			}
			tt.Frame()
			tt.Key(ui.Cmd, ui.KeyF)
			tt.Frame()
			tt.Type("context")
			waitFind(t, a)
			tt.Frame()
			writePNG(t, filepath.Join(dir, "find-bar-"+sz.name+"-"+mode+".png"), tt.Image())
			if !jumpFindMsg(t, a, tt, assistantHit(a)) {
				t.Fatalf("%s %s no assistant hit", sz.name, mode)
			}
			writePNG(t, filepath.Join(dir, "find-flash-"+sz.name+"-"+mode+".png"), tt.Image())
		}
	}
}

func assistantHit(a *App) string {
	conv := a.active()
	if conv == nil {
		return ""
	}
	role := map[string]string{}
	for _, m := range conv.Messages {
		role[m.ID] = m.Role
	}
	for _, h := range a.finder.hits {
		if role[h.MsgID] == roleAssistant {
			return h.MsgID
		}
	}
	return ""
}
