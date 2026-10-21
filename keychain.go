package main

import (
	"errors"
	"sync"

	"github.com/egoist/mygo"
)

// macOS Keychain status codes that mean the user, or the system, refused
// this process. They are the codes Security.framework returns from
// SecItemCopyMatching. mygo wraps them in an error string; any read error
// other than "not found" is remembered the same way, so a denied or failed
// read is not tried again until the user asks.
const (
	errSecAuthFailed            int32 = -25293
	errSecUserCanceled          int32 = -128
	errSecInteractionNotAllowed int32 = -25308
)

// keyCall is one in-flight read. Concurrent callers of the same id wait
// on done and share the result, so the credential store is asked once.
type keyCall struct {
	done  chan struct{}
	value string
	err   error
}

// cachedKey is one account after the credential store has been asked, or
// after a Set or Delete. A failure is kept for the life of the process.
type cachedKey struct {
	value  string
	found  bool
	failed bool
	err    error
}

func (e cachedKey) get() (string, error) {
	if e.failed {
		return "", e.err
	}
	if !e.found {
		return "", mygo.ErrSecretNotFound
	}
	return e.value, nil
}

// keyStore is the only cache in front of the login Keychain (or the file
// fallback). Each account is read at most once per process. A later read
// is served from memory. Set writes through only when the cached value
// differs. Delete drops the cached secret and remembers the absence so
// the store is not asked again. A denied or failed read is remembered
// until Retry or a later Set.
//
// An unsigned or ad-hoc signed build gets a new code signature every
// time it is linked. The login Keychain records that signature on the
// item's ACL. The next build is a different binary, so macOS may ask
// once to let the new binary read items the previous build created.
// No attribute this process can set, including kSecAttrAccessGroup,
// sticks across those signatures: an access group needs a real
// keychain-access-groups entitlement, and an ad-hoc signature has no
// stable designated requirement. A Developer ID or distribution
// certificate is what keeps the identity. This store only stops the
// prompts that come from asking again inside one process.
type keyStore struct {
	inner  keyring
	mu     sync.Mutex
	m      map[string]cachedKey
	flight map[string]*keyCall
}

func newKeyStore(inner keyring) keyring {
	if inner == nil {
		return nil
	}
	if s, ok := inner.(*keyStore); ok {
		return s
	}
	return &keyStore{inner: inner, m: map[string]cachedKey{}, flight: map[string]*keyCall{}}
}

// peek reports a cached secret without reading the store. known is false
// when this id has not been fetched yet. A frame uses it so painting does
// not touch the credential store.
func (s *keyStore) peek(id string) (value string, known bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[id]
	if !ok || e.failed {
		return "", ok && e.failed
	}
	if !e.found {
		return "", true
	}
	return e.value, true
}

// failure is the remembered read error, or nil when this id is unread,
// missing, or cached.
func (s *keyStore) failure(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[id]
	if !ok || !e.failed {
		return nil
	}
	return e.err
}

// Retry forgets a remembered failure and reads once. It is the explicit
// user action. A cached success is left alone.
func (s *keyStore) Retry(id string) (string, error) {
	s.mu.Lock()
	if e, ok := s.m[id]; ok && e.failed {
		delete(s.m, id)
	}
	s.mu.Unlock()
	return s.Get(id)
}

func (s *keyStore) Get(id string) (string, error) {
	s.mu.Lock()
	if e, ok := s.m[id]; ok {
		s.mu.Unlock()
		return e.get()
	}
	if call, ok := s.flight[id]; ok {
		s.mu.Unlock()
		<-call.done
		return call.value, call.err
	}
	call := &keyCall{done: make(chan struct{})}
	s.flight[id] = call
	s.mu.Unlock()

	val, err := s.inner.Get(id)
	s.mu.Lock()
	s.remember(id, val, err)
	e := s.m[id]
	call.value, call.err = e.get()
	delete(s.flight, id)
	close(call.done)
	s.mu.Unlock()
	return call.value, call.err
}

func (s *keyStore) remember(id, val string, err error) {
	switch {
	case err == nil:
		s.m[id] = cachedKey{value: val, found: true}
	case errors.Is(err, mygo.ErrSecretNotFound):
		s.m[id] = cachedKey{}
	default:
		s.m[id] = cachedKey{failed: true, err: err}
	}
}

func (s *keyStore) Set(id, secret string) error {
	s.mu.Lock()
	if e, ok := s.m[id]; ok && e.found && !e.failed && e.value == secret {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	if err := s.inner.Set(id, secret); err != nil {
		return err
	}
	s.mu.Lock()
	s.m[id] = cachedKey{value: secret, found: true}
	s.mu.Unlock()
	return nil
}

func (s *keyStore) Delete(id string) error {
	err := s.inner.Delete(id)
	if err != nil && !errors.Is(err, mygo.ErrSecretNotFound) {
		return err
	}
	s.mu.Lock()
	s.m[id] = cachedKey{}
	s.mu.Unlock()
	return nil
}
