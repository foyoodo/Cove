package main

import (
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"sync"

	"github.com/egoist/mygo"
)

// keyring stores API keys outside conversations.json.
type keyring interface {
	Get(id string) (string, error)
	Set(id string, secret string) error
	Delete(id string) error
}

// systemKeyring is the login Keychain on macOS, the Secret Service on
// Linux, and Credential Manager on Windows, via mygo.Secrets.
type systemKeyring struct{}

func (systemKeyring) Get(id string) (string, error) {
	b, err := mygo.Secrets.Get(secretAccount(id))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (systemKeyring) Set(id, secret string) error {
	return mygo.Secrets.Set(secretAccount(id), []byte(secret))
}

func (systemKeyring) Delete(id string) error {
	return mygo.Secrets.Delete(secretAccount(id))
}

func secretAccount(id string) string { return "conn/" + id }

// fileKeyring is the fallback where the credential store is missing
// (Linux without a Secret Service). It is a private file next to the
// conversations, not inside them.
type fileKeyring struct {
	path string
	mu   sync.Mutex
}

func (f *fileKeyring) load() (map[string]string, error) {
	b, err := os.ReadFile(f.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	m := map[string]string{}
	if len(b) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func (f *fileKeyring) store(m map[string]string) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}

func (f *fileKeyring) Get(id string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	if err != nil {
		return "", err
	}
	s, ok := m[id]
	if !ok {
		return "", mygo.ErrSecretNotFound
	}
	return s, nil
}

func (f *fileKeyring) Set(id, secret string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	if err != nil {
		return err
	}
	m[id] = secret
	return f.store(m)
}

func (f *fileKeyring) Delete(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	if err != nil {
		return err
	}
	delete(m, id)
	return f.store(m)
}

// memKeyring is the in-memory store tests use so they do not touch the
// login keychain.
type memKeyring struct {
	mu sync.Mutex
	m  map[string]string
}

func newMemKeys() *memKeyring { return &memKeyring{m: map[string]string{}} }

func (m *memKeyring) Get(id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.m[id]
	if !ok {
		return "", mygo.ErrSecretNotFound
	}
	return s, nil
}

func (m *memKeyring) Set(id, secret string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.m[id] = secret
	return nil
}

func (m *memKeyring) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.m, id)
	return nil
}

// snapshot is a copy of the stored secrets, for tests that require the
// keychain to stay byte-for-byte unchanged.
func (m *memKeyring) snapshot() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]string, len(m.m))
	for k, v := range m.m {
		out[k] = v
	}
	return out
}

var (
	keyProbeOnce sync.Once
	keyProbeErr  error
)

// credentialStore works when Get of a missing account returns not-found
// rather than ErrUnsupported.
func credentialStoreWorks() bool {
	keyProbeOnce.Do(func() {
		_, err := mygo.Secrets.Get("cove-probe")
		if errors.Is(err, mygo.ErrSecretNotFound) {
			return
		}
		keyProbeErr = err
	})
	return keyProbeErr == nil
}

// lazyRing chooses the credential store on the first read, write, or
// delete. Launch does not touch it. On macOS the login Keychain is used
// directly: probing with a dummy account would be a SecItemCopyMatching
// before any provider is used. Other systems are probed once, lazily.
type lazyRing struct {
	path  string
	once  sync.Once
	inner keyring
}

func (l *lazyRing) ring() keyring {
	l.once.Do(func() {
		l.inner = pickKeyring(l.path)
	})
	return l.inner
}

func (l *lazyRing) Get(id string) (string, error) { return l.ring().Get(id) }
func (l *lazyRing) Set(id, secret string) error   { return l.ring().Set(id, secret) }
func (l *lazyRing) Delete(id string) error        { return l.ring().Delete(id) }

func pickKeyring(convPath string) keyring {
	if runtime.GOOS == "darwin" || credentialStoreWorks() {
		return systemKeyring{}
	}
	return &fileKeyring{path: convPath + ".keys"}
}
