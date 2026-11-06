package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

func TestWinLinuxIconChecksums(t *testing.T) {
	check := func(path, wantPrefix string) {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(b)
		sum := hex.EncodeToString(digest[:])
		if len(sum) < len(wantPrefix) || sum[:len(wantPrefix)] != wantPrefix {
			t.Fatalf("%s sha256 %s, want prefix %s", path, sum, wantPrefix)
		}
	}
	check("icon/winlinux/cove.ico", "9c641f8f")
	check("icon/winlinux/cove.png", "ed2378ff")
}
