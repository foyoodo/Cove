package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"cove/themes"
)

// Load reads the store. A directory that already has index.json is the
// per-chat layout and comes back with every chat's messages, for tests
// and explicit reloads. A lone conversations.json is the old single file.
// A missing file is a fresh store, not an error.
func Load(path string) (StoreData, error) {
	root := storeRoot(path)
	base := filepath.Base(path)
	// A real file that is not the new index or settings is a legacy blob,
	// including conversations.json.bak. The new layout is the directory.
	if fileExists(path) && base != indexFile && base != settingsFile {
		return readLegacy(path)
	}
	if fileExists(filepath.Join(root, indexFile)) {
		return loadFull(root)
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaultData(), nil
		}
		return StoreData{}, err
	}
	return readLegacy(path)
}

// Save writes the per-chat layout through one writer goroutine. API keys
// are removed before encoding. Chat files are appended or rewritten, then
// the index is written. Until a migration commits, a live conversations.json
// is still the store and Save keeps updating that file.
func Save(path string, data *StoreData) error {
	if legacyStillLive(path) {
		return saveLegacy(path, data)
	}
	return SaveSnapshot(path, data)
}

func legacyStillLive(path string) bool {
	if layoutCommitted(storeRoot(path)) {
		return false
	}
	return fileExists(path)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	cErr := d.Close()
	if err != nil {
		return err
	}
	return cErr
}

// openStore loads the sidebar. A present index is read alone, without
// opening chat files. A missing or corrupt index is rebuilt from chat
// headers. A schema 2/3/4 conversations.json is migrated into the new
// tree, and the old file is kept as conversations.json.bak.
func openStore(path string) (StoreData, string, bool) {
	root := storeRoot(path)
	discardPartialMigration(root, path)
	indexPath := filepath.Join(root, indexFile)
	if fileExists(indexPath) {
		data, err := loadLazy(root)
		if err == nil {
			if bakErr := finishLegacyBak(path); bakErr != nil {
				return data, "Chats are open, but the old file could not be moved aside.", false
			}
			return data, "", false
		}
		storeLog("cove: index.json: %v", err)
		if dirHasChats(root) {
			data, rebErr := rebuildIndex(root)
			if rebErr == nil {
				_ = finishLegacyBak(path)
				return data, "Rebuilt the chat index.", false
			}
		}
	} else if dirHasChats(root) {
		data, err := rebuildIndex(root)
		if err == nil {
			_ = finishLegacyBak(path)
			return data, "Rebuilt the chat index.", false
		}
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if fileExists(path + ".bak") {
			return restoreLegacyBak(path)
		}
		return defaultData(), "", false
	}
	data, err := readLegacy(path)
	if err != nil {
		return quarantineLegacy(path, err)
	}
	if err := migrateLegacy(root, path, &data); err != nil {
		// The old file is unchanged. This launch keeps using it, and the
		// next launch tries the migration again.
		return data, "", false
	}
	return data, "", false
}

func restoreLegacyBak(path string) (StoreData, string, bool) {
	data, err := readLegacy(path + ".bak")
	if err != nil {
		return defaultData(), "Could not read saved chats. Saving is paused until you start fresh.", true
	}
	if err := migrateLegacy(storeRoot(path), path, &data); err != nil {
		return data, "Read the backup, but could not copy it back into place. Saving is paused.", true
	}
	return data, "Restored chats from the backup.", false
}

func quarantineLegacy(path string, cause error) (StoreData, string, bool) {
	dest := filepath.Join(filepath.Dir(path), "conversations.corrupt-"+time.Now().UTC().Format("20060102-150405")+".json")
	if renErr := os.Rename(path, dest); renErr != nil {
		return defaultData(), "Could not read saved chats, and the file could not be moved aside. Saving is paused so it is not overwritten.", true
	}
	if fileExists(path + ".bak") {
		data, err := readLegacy(path + ".bak")
		if err == nil {
			if saveErr := migrateLegacy(storeRoot(path), path, &data); saveErr != nil {
				return data, "Read the backup, but could not copy it back into place. Saving is paused.", true
			}
			return data, "Restored chats from the backup. The unreadable file was moved aside.", false
		}
	}
	_ = cause
	return defaultData(), "Could not read saved chats. The file was moved aside and saving is paused until you start fresh.", true
}

func stripSecrets(data *StoreData) *StoreData {
	if data == nil {
		return nil
	}
	out := cloneStore(*data)
	out.Defaults.APIKey = ""
	for i := range out.Conversations {
		out.Conversations[i].Config.APIKey = ""
	}
	return &out
}

func cloneStore(d StoreData) StoreData {
	out := d
	if d.Connections != nil {
		out.Connections = make([]Connection, len(d.Connections))
		for i := range d.Connections {
			c := d.Connections[i]
			if c.Models != nil {
				c.Models = append([]ProviderModel(nil), c.Models...)
			}
			out.Connections[i] = c
		}
	}
	if d.Conversations != nil {
		out.Conversations = make([]Conversation, len(d.Conversations))
		for i := range d.Conversations {
			c := d.Conversations[i]
			if c.DraftQuotes != nil {
				c.DraftQuotes = append([]Quote(nil), c.DraftQuotes...)
			}
			if c.Messages != nil {
				// make keeps an empty slice empty, not nil. A nil slice is an
				// index row that has not been opened; an empty slice is a
				// loaded chat with no messages, including a draft.
				msgs := make([]Message, len(c.Messages))
				copy(msgs, c.Messages)
				for i := range msgs {
					if msgs[i].Quotes != nil {
						msgs[i].Quotes = append([]Quote(nil), msgs[i].Quotes...)
					}
				}
				c.Messages = msgs
			}
			if c.Breaks != nil {
				cuts := make([]SessionBreak, len(c.Breaks))
				copy(cuts, c.Breaks)
				c.Breaks = cuts
			}
			out.Conversations[i] = c
		}
	}
	return out
}

// copyFile is used by tests that plant a backup. It fsyncs the destination.
func copyFile(dst, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func normalize(data *StoreData) {
	from := data.Version
	if data.Version < 4 {
		data.ShowReasoning = true
		data.RestoreLast = true
		data.KeepLastRequest = true
	}
	data.Theme = themes.NormalizeTheme(data.Theme)
	if data.Connections == nil {
		data.Connections = []Connection{}
	}
	if data.Conversations == nil {
		data.Conversations = []Conversation{}
	}
	if data.Defaults.Model == "" && data.Defaults.BaseURL == "" && data.Defaults.Provider == "" {
		data.Defaults = DefaultConfig()
	}
	migrateProviders(data, from)
	for i := range data.Connections {
		c := &data.Connections[i]
		if strings.TrimSpace(c.Format) == "" {
			c.Format = connFormat(c)
			continue
		}
		c.Format = normalizeFormat(c.Format)
	}
	if from > 0 && from < 7 {
		data.pendingOptIn = true
	}
	if data.Version < storeVersion {
		data.Version = storeVersion
	}
	switch data.Appearance {
	case appearanceSystem, appearanceLight, appearanceDark:
	default:
		data.Appearance = appearanceSystem
	}
	if data.Defaults.ConnectionID == "" {
		data.Defaults = bindChatConnection(data.Defaults, legacyIdentityRaw(data.Defaults), data.Connections)
	}
	if !providerListed(data, data.Defaults.Provider) {
		data.Defaults.Provider = "Custom"
	}
	seen := map[string]bool{}
	kept := make([]Conversation, 0, len(data.Conversations))
	for i := range data.Conversations {
		c := data.Conversations[i]
		if c.ID == "" || seen[c.ID] {
			c.ID = newID()
		}
		if c.Messages == nil {
			c.Messages = []Message{}
		}
		// An empty draft is temporary. Older builds saved one on every
		// launch, so a file can hold several, including under Yesterday.
		// Drop them on load. A draft that has text stays, titled from
		// that text, the same way the sidebar shows it.
		if len(c.Messages) == 0 && strings.TrimSpace(c.Draft) == "" {
			continue
		}
		if len(c.Messages) == 0 {
			c.Title = draftTitle(c.Draft)
		} else if c.Title == "" {
			c.Title = newChatTitle
		}
		if c.Config.ConnectionID == "" {
			c.Config = bindChatConnection(c.Config, legacyIdentityRaw(c.Config), data.Connections)
		}
		if !providerListed(data, c.Config.Provider) {
			c.Config.Provider = "Custom"
		}
		if c.UpdatedAt.IsZero() {
			c.UpdatedAt = c.CreatedAt
		}
		if c.CreatedAt.IsZero() {
			c.CreatedAt = c.UpdatedAt
		}
		if c.CreatedAt.IsZero() {
			c.CreatedAt = time.Now()
			c.UpdatedAt = c.CreatedAt
		}
		ids := map[string]bool{}
		for j := range c.Messages {
			m := &c.Messages[j]
			if m.ID == "" || ids[m.ID] {
				m.ID = newID()
			}
			ids[m.ID] = true
		}
		seen[c.ID] = true
		kept = append(kept, c)
	}
	data.Conversations = kept
	sortConversations(data.Conversations)
	if data.ActiveID != "" && !seen[data.ActiveID] {
		data.ActiveID = ""
	}
	if data.ActiveID == "" && len(data.Conversations) > 0 {
		data.ActiveID = data.Conversations[0].ID
	}
}

func sortConversations(convs []Conversation) {
	slices.SortStableFunc(convs, func(a, b Conversation) int {
		if a.UpdatedAt.Equal(b.UpdatedAt) {
			return 0
		}
		if a.UpdatedAt.After(b.UpdatedAt) {
			return -1
		}
		return 1
	})
}
