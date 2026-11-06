package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/egoist/mygo"
)

func TestFileKeyringFallbackMode0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.json.keys")
	k := &fileKeyring{path: path}
	if err := k.Set("conn-test", "sk-secret"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := k.Get("conn-test")
	if err != nil {
		t.Fatal(err)
	}
	if got != "sk-secret" {
		t.Fatalf("Get = %q", got)
	}
	if runtime.GOOS == "windows" {
		return
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("keys file mode %o, want 0600", perm)
	}
}

func TestFileKeyringMissingIsNotFound(t *testing.T) {
	k := &fileKeyring{path: filepath.Join(t.TempDir(), "conversations.json.keys")}
	_, err := k.Get("missing")
	if err != mygo.ErrSecretNotFound {
		t.Fatalf("Get missing = %v, want ErrSecretNotFound", err)
	}
}
