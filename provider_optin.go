package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// presetSlug is the stable id of a built-in preset. A second OpenAI is
// openai-2. Chats store this id, not the display name.
func presetSlug(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "provider"
	}
	return b.String()
}

func allocPresetID(conns []Connection, slug string) string {
	used := map[string]bool{}
	for _, c := range conns {
		used[c.ID] = true
	}
	if slug != "" && !used[slug] {
		return slug
	}
	for n := 2; ; n++ {
		id := fmt.Sprintf("%s-%d", slug, n)
		if !used[id] {
			return id
		}
	}
}

func providerNameTaken(conns []Connection, name, exceptID string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, c := range conns {
		if c.ID == exceptID {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(c.Name), name) {
			return true
		}
	}
	return false
}

func uniqueProviderName(conns []Connection, base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "Provider"
	}
	if !providerNameTaken(conns, base, "") {
		return base
	}
	for n := 2; ; n++ {
		cand := fmt.Sprintf("%s %d", base, n)
		if !providerNameTaken(conns, cand, "") {
			return cand
		}
	}
}

func (a *App) providerNameError(name, exceptID string) string {
	if strings.TrimSpace(name) == "" {
		return "Enter a name."
	}
	if providerNameTaken(a.data.Connections, name, exceptID) {
		return "Another provider already uses this name."
	}
	return ""
}

func httpURLError(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "Enter a Base URL."
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return "Enter a full URL that starts with http:// or https://"
	}
	return ""
}

// adoptProviders drops untouched built-in presets from a store that was
// saved before providers were opt-in. A preset stays when it has a key,
// was tested, has a non-official base URL or a saved model list, or is
// named by a chat or the default model. Kept ids are not rewritten.
func (a *App) adoptProviders() {
	if !a.data.pendingOptIn || a.saveBlocked {
		return
	}
	root := ""
	if a.path != "" {
		root = storeRoot(a.path)
	}
	// This upgrade is the one launch that reads keys: a preset that has
	// nothing but a key would otherwise be dropped. A current store has
	// pendingOptIn clear and does not come through here.
	hasKey := func(id string) bool {
		if a.keys == nil {
			return false
		}
		s, err := a.keys.Get(id)
		return err == nil && strings.TrimSpace(s) != ""
	}
	a.data.Connections = keepAdopted(a.data.Connections, referencedProviders(&a.data, root), hasKey)
	a.data.pendingOptIn = false
	// A failed migration is still the old conversations.json. Saving here
	// would rewrite that file. The next launch migrates it and prunes then.
	if a.path != "" && legacyStillLive(a.path) {
		return
	}
	a.save()
}

func keepAdopted(conns []Connection, ref providerRefs, hasKey func(string) bool) []Connection {
	out := make([]Connection, 0, len(conns))
	for _, c := range conns {
		if c.Custom || presetKept(c, ref, hasKey(c.ID)) {
			out = append(out, c)
		}
	}
	return out
}

func presetKept(c Connection, ref providerRefs, hasKey bool) bool {
	if hasKey || strings.TrimSpace(c.Probe) != "" || strings.TrimSpace(c.BaseURL) != "" || c.ModelsSet {
		return true
	}
	if c.ID != "" && ref.ids[c.ID] {
		return true
	}
	if ref.names[strings.ToLower(strings.TrimSpace(c.Name))] || ref.names[strings.ToLower(strings.TrimSpace(c.Provider))] {
		return true
	}
	return false
}

type providerRefs struct {
	ids   map[string]bool
	names map[string]bool
}

func referencedProviders(data *StoreData, root string) providerRefs {
	ref := providerRefs{ids: map[string]bool{}, names: map[string]bool{}}
	var models []string
	add := func(id, name string) {
		if id != "" {
			ref.ids[id] = true
		}
		if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
			ref.names[name] = true
		}
	}
	noteModel := func(model string) {
		if model = strings.TrimSpace(model); model != "" {
			models = append(models, model)
		}
	}
	add(data.Defaults.ConnectionID, data.Defaults.Provider)
	noteModel(data.Defaults.Model)
	for _, c := range data.Conversations {
		add(c.Config.ConnectionID, c.Config.Provider)
		noteModel(c.Config.Model)
	}
	if root == "" {
		keepModels(data.Connections, models, add)
		return ref
	}
	if idx, err := readIndex(root); err == nil {
		for _, e := range idx.Chats {
			add(e.ConnectionID, e.Provider)
			noteModel(e.Model)
		}
	}
	dir := filepath.Join(root, chatsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		keepModels(data.Connections, models, add)
		return ref
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		line := b
		if i := strings.IndexByte(string(b), '\n'); i >= 0 {
			line = b[:i]
		}
		var header struct {
			Settings struct {
				ConnectionID string `json:"connectionId"`
				Provider     string `json:"provider"`
				Model        string `json:"model"`
			} `json:"settings"`
		}
		if json.Unmarshal(line, &header) != nil {
			continue
		}
		add(header.Settings.ConnectionID, header.Settings.Provider)
		noteModel(header.Settings.Model)
	}
	keepModels(data.Connections, models, add)
	return ref
}

// keepModels keeps a preset when a stored model id belongs to exactly one
// connection. A shared id is not a provider reference.
func keepModels(conns []Connection, models []string, add func(string, string)) {
	seen := map[string]bool{}
	for _, model := range models {
		if seen[model] {
			continue
		}
		seen[model] = true
		var hits []*Connection
		for i := range conns {
			if connOffers(&conns[i], model) {
				hits = append(hits, &conns[i])
			}
		}
		if len(hits) == 1 {
			add(hits[0].ID, connDisplay(hits[0]))
		}
	}
}

func (a *App) beginPreset(name string) {
	if !knownProvider(name) || name == "Custom" {
		return
	}
	shown := uniqueProviderName(a.data.Connections, name)
	a.formError = ""
	a.provEdit = providerEditor{
		name: shown, draft: true, preset: name, keyless: keylessProvider(name),
		models: builtinModels(name), format: presetFormat(name),
	}
	a.settingsPage = settingsPending
}

// beginCustom opens the Custom Provider draft. Add is what writes it.
// Cancel returns to Add Provider and leaves the list unchanged.
func (a *App) beginCustom() {
	a.formError = ""
	a.addForm = addDraft{}
	a.provEdit = providerEditor{
		draft: true, custom: true, preset: "Custom", format: formatOpenAI,
	}
	a.settingsPage = settingsPending
}

func (a *App) commitDraft(now time.Time) {
	ed := &a.provEdit
	if !ed.draft {
		return
	}
	ed.nameError = a.providerNameError(ed.name, "")
	if ed.custom {
		ed.baseError = httpURLError(ed.baseURL)
	}
	if ed.nameError != "" || ed.baseError != "" {
		return
	}
	name := strings.TrimSpace(ed.name)
	kind := ed.preset
	custom := ed.custom
	var id string
	if custom {
		id = newID()
		kind = "Custom"
	} else {
		id = allocPresetID(a.data.Connections, presetSlug(kind))
	}
	format := presetFormat(kind)
	if custom {
		format = normalizeFormat(ed.format)
	}
	conn := Connection{
		ID: id, Name: name, Provider: kind, Custom: custom,
		BaseURL: storedBase(kind, ed.baseURL), Format: format,
	}
	if len(ed.models) > 0 {
		conn.Models = append([]ProviderModel(nil), ed.models...)
		conn.ModelsSet = true
	}
	a.data.Connections = append(a.data.Connections, conn)
	if key := strings.TrimSpace(ed.apiKey); key != "" && !ed.keyless {
		if a.keys == nil {
			a.data.Connections = a.data.Connections[:len(a.data.Connections)-1]
			a.formError = "No credential store is available."
			return
		}
		if err := a.keys.Set(id, key); err != nil {
			a.data.Connections = a.data.Connections[:len(a.data.Connections)-1]
			a.formError = err.Error()
			return
		}
		if a.assumedKeys == nil {
			a.assumedKeys = map[string]bool{}
		}
		a.assumedKeys[id] = true
	}
	a.formError = ""
	a.noteSaved(now)
	a.save()
	if custom {
		a.viewCustom(id)
	} else {
		a.viewConnection(id)
	}
	a.settingsPage = settingsDetail
}

func (a *App) viewConnection(id string) {
	c := a.connByID(id)
	if c == nil {
		return
	}
	if c.Custom {
		a.viewCustom(id)
		return
	}
	kind := c.Provider
	if kind == "" {
		kind = c.Name
	}
	show := a.provEdit.showKey && a.provEdit.connID == id
	ed := providerEditor{
		name: c.Name, connID: c.ID, preset: kind, showKey: show,
		keyless: keylessProvider(kind),
		baseURL: storedBase(kind, c.BaseURL),
		apiKey:  a.resolvedKey(Config{ConnectionID: c.ID}),
		models:  a.displayModels(kind, c),
		format:  connFormat(c),
	}
	ed.loadedBase = ed.baseURL
	ed.loadedKey = ed.apiKey
	a.provEdit = ed
}

func (a *App) openConnection(id string, now time.Time) {
	if a.settingsPage == settingsDetail {
		a.flushProviderEdits(now)
	}
	c := a.connByID(id)
	if c == nil {
		return
	}
	a.addForm = addDraft{}
	a.formError = ""
	a.viewConnection(id)
	a.settingsPage = settingsDetail
}

func (a *App) saveProviderName(now time.Time) {
	ed := &a.provEdit
	except := ""
	if !ed.draft {
		except = ed.connID
	}
	ed.nameError = a.providerNameError(ed.name, except)
	if ed.draft || ed.nameError != "" {
		return
	}
	c := a.editingConn()
	if c == nil {
		return
	}
	name := strings.TrimSpace(ed.name)
	if c.Name == name {
		return
	}
	c.Name = name
	a.noteSaved(now)
	a.save()
}

func (a *App) dismissSwitchNote(conv *Conversation) {
	if conv == nil || conv.SwitchNote == "" {
		return
	}
	conv.SwitchNote = ""
	a.save()
}

func (a *App) openAddProvider() {
	a.settingsSection = "models"
	a.settingsPage = settingsAdd
	a.addForm = addDraft{}
	a.formError = ""
	a.openDefaults()
}
