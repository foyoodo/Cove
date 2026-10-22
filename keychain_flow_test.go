package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func waitIdle(t *testing.T, a *App) {
	t.Helper()
	id := ""
	if conv := a.active(); conv != nil {
		id = conv.ID
	}
	deadline := time.Now().Add(3 * time.Second)
	for a.isStreaming(id) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if a.isStreaming(id) {
		t.Fatal("reply did not finish")
	}
}

func TestKeychainReadsOnceAndSkipsUnchangedWrites(t *testing.T) {
	inner := newCountingKeyring()
	inner.seed("alpha", "k-alpha")
	store := newKeyStore(inner).(*keyStore)
	for i := 0; i < 20; i++ {
		got, err := store.Get("alpha")
		if err != nil || got != "k-alpha" {
			t.Fatalf("read %d: %q %v", i, got, err)
		}
	}
	if inner.Gets("alpha") != 1 {
		t.Fatalf("backend reads %d", inner.Gets("alpha"))
	}
	if err := store.Set("alpha", "k-alpha"); err != nil {
		t.Fatal(err)
	}
	if sets, _ := inner.writes(); sets != 0 {
		t.Fatalf("unchanged write called the backend %d times", sets)
	}
	if err := store.Set("alpha", "k-new"); err != nil {
		t.Fatal(err)
	}
	if sets, _ := inner.writes(); sets != 1 {
		t.Fatalf("changed write called the backend %d times", sets)
	}
	if got, _ := store.Get("alpha"); got != "k-new" || inner.Gets("alpha") != 1 {
		t.Fatalf("set did not stay in memory, reads %d value %q", inner.Gets("alpha"), got)
	}
	if err := store.Delete("alpha"); err != nil {
		t.Fatal(err)
	}
	if _, dels := inner.writes(); dels != 1 {
		t.Fatalf("deletes %d", dels)
	}
	if _, err := store.Get("alpha"); err == nil {
		t.Fatal("deleted secret still resolved")
	}
	if inner.Gets("alpha") != 1 {
		t.Fatal("delete was followed by another read")
	}
	if inner.has("alpha") {
		t.Fatal("backend still holds the deleted secret")
	}
}

func TestKeychainConcurrentFirstRead(t *testing.T) {
	inner := newCountingKeyring()
	inner.seed("alpha", "k-alpha")
	inner.delay = 30 * time.Millisecond
	store := newKeyStore(inner).(*keyStore)
	const n = 12
	var wg sync.WaitGroup
	start := make(chan struct{})
	errc := make(chan error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			<-start
			got, err := store.Get("alpha")
			if err != nil || got != "k-alpha" {
				errc <- fmt.Errorf("%q %v", got, err)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errc)
	for err := range errc {
		t.Error(err)
	}
	if inner.Gets("alpha") != 1 {
		t.Fatalf("concurrent first reads called the backend %d times", inner.Gets("alpha"))
	}
}

func TestKeychainDenialIsStickyUntilRetry(t *testing.T) {
	inner := newCountingKeyring()
	inner.seed("alpha", "k-alpha")
	inner.deny = fmt.Errorf("keychain: read: user canceled (OSStatus %d)", errSecUserCanceled)
	a := openApp(t.TempDir()+"/conversations.json", inner)
	a.data.Connections = []Connection{{
		ID: "alpha", Name: "Alpha", Provider: "OpenAI", BaseURL: "https://api.openai.com/v1",
	}}
	cfg := Config{ConnectionID: "alpha"}
	if got := a.resolvedKey(cfg); got != "" || !a.keyBlocked("alpha") {
		t.Fatalf("denial resolved %q blocked %v", got, a.keyBlocked("alpha"))
	}
	for i := 0; i < 5; i++ {
		_ = a.resolvedKey(cfg)
	}
	if inner.Gets("alpha") != 1 {
		t.Fatalf("denial was retried automatically, reads %d", inner.Gets("alpha"))
	}
	if keyAccessMessage() != "Couldn’t read the key from the Keychain." {
		t.Fatalf("notice %q", keyAccessMessage())
	}
	a.retryKey("alpha")
	if inner.Gets("alpha") != 2 || !a.keyBlocked("alpha") {
		t.Fatalf("explicit retry reads %d blocked %v", inner.Gets("alpha"), a.keyBlocked("alpha"))
	}
	inner.mu.Lock()
	inner.deny = fmt.Errorf("keychain: read: auth failed (OSStatus %d)", errSecAuthFailed)
	inner.mu.Unlock()
	a.retryKey("alpha")
	if inner.Gets("alpha") != 3 {
		t.Fatalf("second explicit retry reads %d", inner.Gets("alpha"))
	}
	inner.mu.Lock()
	inner.deny = nil
	inner.mu.Unlock()
	a.retryKey("alpha")
	if inner.Gets("alpha") != 4 {
		t.Fatalf("successful retry reads %d", inner.Gets("alpha"))
	}
	if got := a.resolvedKey(cfg); got != "k-alpha" || a.keyBlocked("alpha") || inner.Gets("alpha") != 4 {
		t.Fatalf("after retry value %q blocked %v reads %d", got, a.keyBlocked("alpha"), inner.Gets("alpha"))
	}
	// Typing the key again is the other explicit recovery. It writes once
	// and does not read.
	if err := a.keys.Set("alpha", "k-typed"); err != nil {
		t.Fatal(err)
	}
	if sets, _ := inner.writes(); sets != 1 {
		t.Fatalf("re-entered key writes %d", sets)
	}
	if got := a.resolvedKey(cfg); got != "k-typed" || inner.Gets("alpha") != 4 {
		t.Fatalf("typed key %q reads %d", got, inner.Gets("alpha"))
	}
}

func TestKeychainDenialNoticeRetriesOnce(t *testing.T) {
	inner := newCountingKeyring()
	inner.seed("openai", "sk-openai")
	inner.deny = fmt.Errorf("keychain: read: interaction not allowed (OSStatus %d)", errSecInteractionNotAllowed)
	a := openApp(t.TempDir()+"/conversations.json", inner)
	a.instant = true
	a.data.Connections = []Connection{{
		ID: "openai", Name: "OpenAI", Provider: "OpenAI", BaseURL: "https://api.openai.com/v1",
	}}
	tt := openTester(a)
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Models & Providers"); err != nil {
		t.Fatal(err)
	}
	if inner.Gets("openai") != 0 {
		t.Fatalf("provider list read the key %d times", inner.Gets("openai"))
	}
	if err := tt.Click("OpenAI"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Couldn’t read the key from the Keychain.") || !tt.HasText("Try Again") {
		t.Fatalf("notice missing in %q", tt.Texts())
	}
	if inner.Gets("openai") != 1 {
		t.Fatalf("opening the provider read %d times", inner.Gets("openai"))
	}
	renderFrames(tt, 3)
	if inner.Gets("openai") != 1 {
		t.Fatal("drawing the notice read the key again")
	}
	if err := tt.Click("Try Again"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if inner.Gets("openai") != 2 {
		t.Fatalf("Try Again read %d times", inner.Gets("openai"))
	}
	renderFrames(tt, 3)
	if inner.Gets("openai") != 2 || !a.keyBlocked("openai") {
		t.Fatalf("after Try Again reads %d blocked %v", inner.Gets("openai"), a.keyBlocked("openai"))
	}
}

func TestKeychainDeniedCrops(t *testing.T) {
	dir := testArtifactDir(t, "final14b")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Settings window default is 780×560; the minimum is 720×520.
	sizes := []struct {
		name       string
		w, h       int
		saveDetail bool
		saveMin    bool
	}{
		{name: "", w: 780, h: 560, saveDetail: true},
		{name: "min-", w: 720, h: 520, saveMin: true},
	}
	for _, sz := range sizes {
		for _, dark := range []bool{false, true} {
			mode := "light"
			if dark {
				mode = "dark"
			}
			tag := sz.name + mode
			inner := newCountingKeyring()
			inner.seed("openai", "sk-openai")
			inner.deny = fmt.Errorf("keychain: read: auth failed (OSStatus %d)", errSecAuthFailed)
			a := openApp(t.TempDir()+"/conversations.json", inner)
			a.instant = true
			if dark {
				a.data.Appearance = appearanceDark
			}
			a.data.Connections = []Connection{{
				ID: "openai", Name: "OpenAI", Provider: "OpenAI", BaseURL: "https://api.openai.com/v1",
			}}
			tt := openTester(a)
			tt.SetScale(2)
			tt.SetDark(dark)
			tt.SetSize(sz.w, sz.h)
			if err := tt.Click("Settings"); err != nil {
				t.Fatal(err)
			}
			if err := tt.Click("Models & Providers"); err != nil {
				t.Fatal(err)
			}
			if err := tt.Click("OpenAI"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if !tt.HasText("Couldn’t read the key from the Keychain.") || !tt.HasText("Try Again") {
				t.Fatalf("%s detail missing the notice: %q", tag, tt.Texts())
			}
			if inner.Gets("openai") != 1 {
				t.Fatalf("%s detail reads %d", tag, inner.Gets("openai"))
			}
			assertDeniedNotice(t, tt, tag)
			if sz.saveDetail {
				saveDeniedDetail(t, tt, dir, "detail-"+mode)
			}
			if sz.saveMin && !dark {
				saveDeniedDetail(t, tt, dir, "detail-min-light")
			}
			if sz.saveDetail && !dark {
				btn, ok := tt.Find("Try Again")
				if !ok {
					t.Fatal("Try Again missing")
				}
				tt.Move(btn.X+btn.W/2, btn.Y+btn.H/2)
				tt.Frame()
				if inner.Gets("openai") != 1 {
					t.Fatalf("hover read the key: %d", inner.Gets("openai"))
				}
				assertDeniedNotice(t, tt, tag+"-hover")
				saveDeniedDetail(t, tt, dir, "detail-light-hover")
				tt.Move(0, 0)
				tt.Frame()
			}
			if err := tt.Click("Back"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if !tt.HasText("Key unavailable") {
				t.Fatalf("%s list missing Key unavailable: %q", tag, tt.Texts())
			}
			if inner.Gets("openai") != 1 {
				t.Fatalf("list read the key again: %d", inner.Gets("openai"))
			}
		}
	}
}

// assertDeniedNotice checks the notice stays in the API key field's
// column, 6pt under the field, with the row's 7pt bottom padding under it.
func assertDeniedNotice(t *testing.T, tt *ui.Tester, mode string) {
	t.Helper()
	field, ok := tt.Find("API key field")
	if !ok {
		t.Fatalf("%s API key field missing", mode)
	}
	msg, ok := tt.Find("Couldn’t read the key from the Keychain.")
	if !ok {
		t.Fatalf("%s notice missing", mode)
	}
	btn, ok := tt.Find("Try Again")
	if !ok {
		t.Fatalf("%s Try Again missing", mode)
	}
	row, ok := tt.Find("API key row")
	if !ok {
		t.Fatalf("%s API key row missing", mode)
	}
	if msg.X < field.X-0.5 || msg.X > field.X+0.5 {
		t.Fatalf("%s notice left %.2f, field left %.2f", mode, msg.X, field.X)
	}
	if btn.X < field.X-0.5 {
		t.Fatalf("%s Try Again left %.2f, field left %.2f", mode, btn.X, field.X)
	}
	right := field.X + field.W
	if msg.X+msg.W > right+0.5 {
		t.Fatalf("%s notice right %.2f past field %.2f", mode, msg.X+msg.W, right)
	}
	if btn.X+btn.W > right+0.5 {
		t.Fatalf("%s Try Again right %.2f past field %.2f", mode, btn.X+btn.W, right)
	}
	if btn.W < 48 {
		t.Fatalf("%s Try Again width %.2f", mode, btn.W)
	}
	gap := msg.Y - (field.Y + field.H)
	if gap < 5.5 || gap > 6.5 {
		t.Fatalf("%s gap under the field %.2f", mode, gap)
	}
	// The two do not fit on one line of the field, so Try Again wraps.
	if msg.W+8+btn.W > field.W+0.5 && btn.Y < msg.Y+msg.H-1 {
		t.Fatalf("%s Try Again did not wrap: msg %.1f×%.1f btn y %.1f field %.1f", mode, msg.W, msg.H, btn.Y, field.W)
	}
	noticeBottom := msg.Y + msg.H
	if btn.Y+btn.H > noticeBottom {
		noticeBottom = btn.Y + btn.H
	}
	pad := row.Y + row.H - noticeBottom
	if pad < 6.5 || pad > 7.5 {
		t.Fatalf("%s padding under the notice %.2f", mode, pad)
	}
	name, ok := tt.Find("Provider name")
	if !ok {
		t.Fatalf("%s name field missing", mode)
	}
	if name.X+name.W < right-0.5 || name.X+name.W > right+0.5 {
		t.Fatalf("%s field right %.2f, name field right %.2f", mode, right, name.X+name.W)
	}
}

func saveDeniedDetail(t *testing.T, tt *ui.Tester, dir, name string) {
	t.Helper()
	r, ok := tt.Find("API key row")
	if !ok {
		t.Fatal("API key row missing")
	}
	// Show the divider above the row and the one under the notice.
	r.Y -= 18
	r.H += 18 + 22
	f, err := os.Create(filepath.Join(dir, name+"-2x.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, cropDIP(tt.Image(), r, 2, 8)); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteProviderRemovesKeychainItem(t *testing.T) {
	inner := newCountingKeyring()
	inner.seed("gone", "secret")
	a := openApp(t.TempDir()+"/conversations.json", inner)
	a.instant = true
	a.data.Connections = []Connection{{
		ID: "gone", Name: "Gone", Provider: "OpenAI", BaseURL: "https://api.openai.com/v1",
	}}
	a.deleteProvider("gone", time.Now())
	if _, dels := inner.writes(); dels != 1 {
		t.Fatalf("provider delete removed %d items", dels)
	}
	if inner.has("gone") {
		t.Fatal("keychain item still present")
	}
	if a.connByID("gone") != nil {
		t.Fatal("provider still listed")
	}
	if got := a.resolvedKey(Config{ConnectionID: "gone"}); got != "" || inner.Gets("gone") != 0 {
		t.Fatalf("deleted id resolved %q with %d reads", got, inner.Gets("gone"))
	}
}

func TestKeychainLazyPerProviderAndSessionFlow(t *testing.T) {
	inner := newCountingKeyring()
	dir := t.TempDir()
	path := dir + "/conversations.json"
	seed := openApp(path, inner)
	seed.instant = true
	seed.data.Connections = []Connection{
		{ID: "alpha", Name: "Alpha", Provider: "OpenAI", BaseURL: "http://127.0.0.1:1/v1", Models: []ProviderModel{{ID: "gpt-4o", Enabled: true}}, ModelsSet: true},
		{ID: "beta", Name: "Beta", Provider: "OpenAI", BaseURL: "http://127.0.0.1:1/v1", Models: []ProviderModel{{ID: "gpt-4o-mini", Enabled: true}}, ModelsSet: true},
	}
	seed.data.Defaults.ConnectionID = "alpha"
	seed.data.Defaults.Model = "gpt-4o"
	seed.data.Defaults.BaseURL = "http://127.0.0.1:1/v1"
	seed.data.Defaults.Provider = "Alpha"
	seed.data.Defaults.APIKey = ""
	seed.newChat()
	conv := seed.active()
	conv.Config.ConnectionID = "alpha"
	conv.Config.Model = "gpt-4o"
	conv.Config.Provider = "Alpha"
	conv.Config.BaseURL = "http://127.0.0.1:1/v1"
	conv.Config.APIKey = ""
	inner.seed("alpha", "k-alpha")
	inner.seed("beta", "k-beta")
	seed.saveSync()

	a := openApp(path, inner)
	a.instant = true
	if a.active() == nil {
		a.newChat()
	}
	if inner.Gets("alpha") != 0 || inner.Gets("beta") != 0 {
		t.Fatalf("launch read alpha %d beta %d", inner.Gets("alpha"), inner.Gets("beta"))
	}
	tt := openTester(a)
	renderFrames(tt, 4)
	if inner.Gets("alpha") != 0 || inner.Gets("beta") != 0 {
		t.Fatalf("render read alpha %d beta %d", inner.Gets("alpha"), inner.Gets("beta"))
	}
	if !a.send("hello") {
		conv = a.active()
		id, model := "", ""
		if conv != nil {
			id, model = conv.Config.ConnectionID, conv.Config.Model
		}
		t.Fatalf("send did not start active=%v conn=%s model=%s", conv != nil, id, model)
	}
	waitIdle(t, a)
	if inner.Gets("alpha") != 1 || inner.Gets("beta") != 0 {
		t.Fatalf("send read alpha %d beta %d", inner.Gets("alpha"), inner.Gets("beta"))
	}
	a.openSettingsTab(0)
	renderFrames(tt, 3)
	if inner.Gets("alpha") != 1 || inner.Gets("beta") != 0 {
		t.Fatalf("settings read alpha %d beta %d", inner.Gets("alpha"), inner.Gets("beta"))
	}
	a.useModel("beta", "gpt-4o-mini")
	if inner.Gets("beta") != 0 {
		t.Fatalf("switching providers read beta %d", inner.Gets("beta"))
	}
	if !a.send("again") {
		t.Fatal("second send did not start")
	}
	waitIdle(t, a)
	if inner.Gets("alpha") != 1 || inner.Gets("beta") != 1 {
		t.Fatalf("after switch alpha %d beta %d", inner.Gets("alpha"), inner.Gets("beta"))
	}
	if !a.send("third") {
		t.Fatal("third send did not start")
	}
	waitIdle(t, a)
	a.openSettingsTab(0)
	renderFrames(tt, 2)
	if inner.Gets("alpha") != 1 || inner.Gets("beta") != 1 {
		t.Fatalf("session kept reading alpha %d beta %d", inner.Gets("alpha"), inner.Gets("beta"))
	}
	sets, dels := inner.writes()
	if sets != 0 || dels != 0 {
		t.Fatalf("flow wrote sets %d deletes %d", sets, dels)
	}
}
