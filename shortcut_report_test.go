package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShortcutReport(t *testing.T) {
	section := `
Shortcuts
The menu bar and Settings › Shortcuts list the same commands. Shortcuts follow the menu bar and can’t be changed yet.

Chats
New Chat ⌘N
New Session ⇧⌘N
Search Chats ⌘K
Find in Chat ⌘F
Find Next ⌘G
Find Previous ⇧⌘G
Delete Chat ⌘⌫

Messages
Send ↵
New Line ⇧↵
Add to Chat ⌘L
Regenerate ⌘R
Stop ⌘.
Edit Last Message ↑

App
Settings ⌘,
Chat Settings ⌥⌘,

Edit › Find has Find… ⌘F, Find Next ⌘G, and Find Previous ⇧⌘G. Find Next and Find Previous are disabled until the find bar is open. There is no Use Selection for Find, no Find and Replace, and no whole-word or case-sensitive command. Search Chats stays ⌘K.

Request body
A model choice is the connection id plus the model id. The next chat request sends that model id to that connection’s /chat/completions endpoint with that connection’s key. Tests: TestModelSwitchPersistsAcrossProviders, TestCatalogIDMatchesHeader, TestOptInPresetAndCustomShareURL.
`
	path := filepath.Join(testArtifactDir(t, "perf"), "after.txt")
	raw, err := os.ReadFile(path)
	body := ""
	if err == nil {
		body = string(raw)
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	const heading = "\nShortcuts\n"
	if i := strings.Index(body, heading); i >= 0 {
		body = body[:i]
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body+section), 0o644); err != nil {
		t.Fatal(err)
	}
}
