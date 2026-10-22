package main

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// countingKeyring records store traffic and panics if the credential
// store is touched while a view is building, outside the one refresh
// that runs as the Settings sheet opens.
type countingKeyring struct {
	mu    sync.Mutex
	gets  map[string]int
	sets  int
	dels  int
	m     map[string]string
	block func() bool
	deny  error
	delay time.Duration
}

func newCountingKeyring() *countingKeyring {
	return &countingKeyring{gets: map[string]int{}, m: map[string]string{}}
}

func (k *countingKeyring) guard(op, id string) {
	if k.block != nil && k.block() {
		panic(fmt.Sprintf("credential store %s during render (%s)", op, id))
	}
}

func (k *countingKeyring) seed(id, secret string) {
	k.mu.Lock()
	k.m[id] = secret
	k.mu.Unlock()
}

func (k *countingKeyring) Gets(id string) int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.gets[id]
}

func (k *countingKeyring) writes() (sets, dels int) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.sets, k.dels
}

func (k *countingKeyring) has(id string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	_, ok := k.m[id]
	return ok
}

func (k *countingKeyring) Get(id string) (string, error) {
	k.guard("Get", id)
	k.mu.Lock()
	k.gets[id]++
	deny := k.deny
	delay := k.delay
	s, ok := k.m[id]
	k.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	if deny != nil {
		return "", deny
	}
	if !ok {
		return "", mygo.ErrSecretNotFound
	}
	return s, nil
}

func (k *countingKeyring) Set(id, secret string) error {
	k.guard("Set", id)
	k.mu.Lock()
	defer k.mu.Unlock()
	k.sets++
	k.m[id] = secret
	return nil
}

func (k *countingKeyring) Delete(id string) error {
	k.guard("Delete", id)
	k.mu.Lock()
	defer k.mu.Unlock()
	k.dels++
	delete(k.m, id)
	return nil
}

func newKeyedApp(t *testing.T) (*App, *countingKeyring) {
	t.Helper()
	keys := newCountingKeyring()
	a := openApp(t.TempDir()+"/conversations.json", keys)
	a.instant = true
	keys.block = func() bool { return a.inRender && !a.keyRefresh }
	a.newChat()
	a.active().Config.ConnectionID = "chat-conn"
	a.active().Config.APIKey = ""
	a.data.Defaults.ConnectionID = "default-conn"
	a.data.Defaults.APIKey = ""
	a.data.Connections = []Connection{
		{ID: "chat-conn", Name: "OpenAI", Provider: "OpenAI", BaseURL: a.active().Config.BaseURL},
		{ID: "default-conn", Name: "OpenAI", Provider: "OpenAI", BaseURL: a.data.Defaults.BaseURL},
	}
	keys.seed("chat-conn", "sk-chat")
	keys.seed("default-conn", "sk-default")
	return a, keys
}

func renderFrames(tt *ui.Tester, n int) {
	for i := 0; i < n; i++ {
		tt.Frame()
	}
}

func TestKeyCacheCountsAndUpdates(t *testing.T) {
	a, keys := newKeyedApp(t)
	a.openSettingsTab(0)
	if keys.Gets("chat-conn") != 1 || keys.Gets("default-conn") != 1 {
		t.Fatalf("open read chat %d defaults %d", keys.Gets("chat-conn"), keys.Gets("default-conn"))
	}
	for i := 0; i < 8; i++ {
		_ = a.draftDirty(0)
		_ = a.draftDirty(1)
	}
	tt := openTester(a)
	renderFrames(tt, 8)
	if keys.Gets("chat-conn") != 1 || keys.Gets("default-conn") != 1 {
		t.Fatalf("frames re-read chat %d defaults %d", keys.Gets("chat-conn"), keys.Gets("default-conn"))
	}
	if a.draftDirty(0) || a.draftDirty(1) {
		t.Fatal("fresh settings looked dirty")
	}

	a.form.APIKey = "sk-new"
	if !a.saveForm(false) {
		t.Fatal(a.formError)
	}
	if sets, _ := keys.writes(); sets != 1 {
		t.Fatalf("Set calls %d", sets)
	}
	if got := a.resolvedKey(a.active().Config); got != "sk-new" {
		t.Fatalf("resolved after Set %q", got)
	}
	if keys.Gets("chat-conn") != 1 {
		t.Fatal("resolvedKey hit the store after Set")
	}
	if a.draftDirty(0) {
		t.Fatal("saved key still looks dirty")
	}

	if err := a.keys.Delete("chat-conn"); err != nil {
		t.Fatal(err)
	}
	if _, dels := keys.writes(); dels != 1 {
		t.Fatalf("Delete calls %d", dels)
	}
	if got := a.resolvedKey(a.active().Config); got != "" {
		t.Fatalf("resolved after Delete %q", got)
	}
	if keys.Gets("chat-conn") != 1 {
		t.Fatal("resolvedKey hit the store after Delete")
	}
	if got := a.resolvedKey(a.active().Config); got != "" || keys.Gets("chat-conn") != 1 {
		t.Fatal("missing key was queried again")
	}

	missing := Config{ConnectionID: "no-such"}
	if a.resolvedKey(missing) != "" || keys.Gets("no-such") != 1 {
		t.Fatalf("first miss gets %d", keys.Gets("no-such"))
	}
	if a.resolvedKey(missing) != "" || keys.Gets("no-such") != 1 {
		t.Fatal("not-found was queried again")
	}
}

func TestRenderDoesNotTouchCredentials(t *testing.T) {
	a, keys := newKeyedApp(t)
	tt := openTester(a)
	renderFrames(tt, 4)
	if keys.Gets("chat-conn") != 0 || keys.Gets("default-conn") != 0 {
		t.Fatalf("main view read the store chat %d defaults %d", keys.Gets("chat-conn"), keys.Gets("default-conn"))
	}
	sets, dels := keys.writes()
	if sets != 0 || dels != 0 {
		t.Fatalf("main view wrote the store sets %d deletes %d", sets, dels)
	}

	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Chat Settings…"); err != nil {
		t.Fatal(err)
	}
	if keys.Gets("chat-conn") != 1 || keys.Gets("default-conn") != 1 {
		t.Fatalf("open read chat %d defaults %d", keys.Gets("chat-conn"), keys.Gets("default-conn"))
	}
	renderFrames(tt, 6)
	if a.draftDirty(0) || a.draftDirty(1) {
		t.Fatal("opened settings looked dirty")
	}

	if err := tt.Click("Model"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("typed-model")
	if !a.draftDirty(0) {
		t.Fatal("this chat tab did not go dirty")
	}
	renderFrames(tt, 4)
	if keys.Gets("chat-conn") != 1 || keys.Gets("default-conn") != 1 {
		t.Fatalf("drawing re-read chat %d defaults %d", keys.Gets("chat-conn"), keys.Gets("default-conn"))
	}
	sets, dels = keys.writes()
	if sets != 0 || dels != 0 {
		t.Fatalf("drawing wrote the store sets %d deletes %d", sets, dels)
	}

	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Chat Settings…"); err != nil {
		t.Fatal(err)
	}
	if keys.Gets("chat-conn") != 1 || keys.Gets("default-conn") != 1 {
		t.Fatalf("reopen read chat %d defaults %d", keys.Gets("chat-conn"), keys.Gets("default-conn"))
	}
	renderFrames(tt, 3)
	if keys.Gets("chat-conn") != 1 || keys.Gets("default-conn") != 1 {
		t.Fatal("frames after reopen touched the store")
	}
}
