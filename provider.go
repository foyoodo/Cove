package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

const (
	probeOK      = "ok"
	probeInvalid = "invalid"

	settingsList    = ""
	settingsDetail  = "detail"
	settingsAdd     = "add"
	settingsCustom  = "custom"
	settingsPending = "pending"
)

// providerEditor is one provider's own base URL, API key, and model list.
// It is not StoreData.Defaults. A field is written when the user commits
// it, and only that field. Opening the page only fills this struct.
type providerEditor struct {
	name    string
	connID  string
	custom  bool
	keyless bool
	baseURL string
	apiKey  string
	showKey bool
	models  []ProviderModel

	loadedBase string
	loadedKey  string

	baseFocused bool
	keyFocused  bool
	nameOn      bool

	testing     bool
	testKind    string
	testLine    string
	refreshing  bool
	refreshNote string
	adding      bool
	newModel    string
	newFocused  bool

	// draft is a preset that has not been added yet. Nothing is written
	// until Add. preset is the built-in name (OpenAI), which is not the
	// display name when the user already has one.
	draft     bool
	preset    string
	nameError string
	baseError string
	format    string
	// listed is set once a custom draft has taken a model list. The first
	// list is enabled. A later Refresh adds new ids off, with a New badge.
	listed bool
}

// addDraft is the Add Provider form. It is discarded unless the user
// confirms Add Provider.
type addDraft struct {
	name, base, key, format string
	nameOn, baseOn, keyOn   bool
}

func presetFormat(name string) string {
	if p, ok := presetByName(name); ok && p.Format != "" {
		return normalizeFormat(p.Format)
	}
	if strings.EqualFold(strings.TrimSpace(name), "Anthropic") {
		return formatAnthropic
	}
	return formatOpenAI
}

func normalizeFormat(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "anthropic", "anthropic-compatible":
		return formatAnthropic
	default:
		return formatOpenAI
	}
}

func formatLabel(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "anthropic", "anthropic-compatible":
		return "Anthropic-compatible"
	default:
		return "OpenAI-compatible"
	}
}

func connFormat(c *Connection) string {
	if c == nil {
		return formatOpenAI
	}
	if strings.TrimSpace(c.Format) != "" {
		return normalizeFormat(c.Format)
	}
	if c.Custom {
		return formatOpenAI
	}
	return presetFormat(c.Provider)
}

func builtinProviderNames() []string {
	var out []string
	for _, p := range presets {
		if p.Name == "Custom" || p.BaseURL == "" {
			continue
		}
		out = append(out, p.Name)
	}
	return out
}

func keylessProvider(name string) bool {
	return name == "Ollama"
}

func officialBase(provider string) string {
	p, ok := presetByName(provider)
	if !ok {
		return ""
	}
	return p.BaseURL
}

func sameEndpoint(a, b string) bool {
	return strings.TrimRight(strings.TrimSpace(a), "/") == strings.TrimRight(strings.TrimSpace(b), "/")
}

// effectiveBase is the URL a request should use. An empty stored value
// means the official endpoint. A custom provider has no official URL.
func effectiveBase(provider, stored string) string {
	stored = strings.TrimSpace(stored)
	if stored != "" {
		return stored
	}
	return officialBase(provider)
}

// storedBase drops the official address so it is never written.
func storedBase(provider, typed string) string {
	typed = strings.TrimSpace(typed)
	off := officialBase(provider)
	if off != "" && sameEndpoint(typed, off) {
		return ""
	}
	return typed
}

func catalogIDs(name string) []string {
	p, ok := presetByName(name)
	if !ok {
		return nil
	}
	if len(p.Catalog) > 0 {
		return append([]string(nil), p.Catalog...)
	}
	if strings.TrimSpace(p.Model) == "" {
		return nil
	}
	return []string{p.Model}
}

// builtinModels is the unsaved catalog. The preset's usual model is on.
func builtinModels(name string) []ProviderModel {
	p, ok := presetByName(name)
	if !ok {
		return nil
	}
	ids := catalogIDs(name)
	out := make([]ProviderModel, 0, len(ids))
	for _, id := range ids {
		out = append(out, ProviderModel{ID: id, Enabled: id == p.Model})
	}
	return out
}

func (a *App) builtinConn(name string) *Connection {
	for i := range a.data.Connections {
		c := &a.data.Connections[i]
		if c.Custom {
			continue
		}
		if c.Provider == name || c.Name == name {
			return c
		}
	}
	return nil
}

func (a *App) connByID(id string) *Connection {
	if id == "" {
		return nil
	}
	for i := range a.data.Connections {
		if a.data.Connections[i].ID == id {
			return &a.data.Connections[i]
		}
	}
	return nil
}

// providerConnection finds a built-in by name, or a custom provider by its
// display name.
func (a *App) providerConnection(name string) *Connection {
	if c := a.builtinConn(name); c != nil {
		return c
	}
	for i := range a.data.Connections {
		c := &a.data.Connections[i]
		if c.Custom && c.Name == name {
			return c
		}
	}
	return nil
}

func (a *App) customConnections() []Connection {
	var out []Connection
	for _, c := range a.data.Connections {
		if c.Custom {
			out = append(out, c)
		}
	}
	return out
}

// connectionHasKey reports a saved key without reading the credential
// store. The menu and the provider list call it for every row. The
// secret is read only when that provider is opened, tested, or used to
// send.
func (a *App) connectionHasKey(c *Connection) bool {
	if c == nil || a.keys == nil {
		return false
	}
	if s := a.keyStore(); s != nil {
		if v, known := s.peek(c.ID); known {
			return strings.TrimSpace(v) != ""
		}
	}
	return a.assumedKeys[c.ID]
}

// providerReady is a provider whose models may appear in pickers: a saved
// key, or Ollama with an address. A failed test takes it back out.
func (a *App) providerReady(c *Connection, keyless bool) bool {
	if c != nil && c.Probe == probeInvalid {
		return false
	}
	if keyless {
		if c == nil {
			return false
		}
		return strings.TrimSpace(c.BaseURL) != "" || c.Probe == probeOK
	}
	return a.connectionHasKey(c)
}

func (a *App) providerStatus(c *Connection, keyless bool) (kind, label string) {
	if c != nil && !keyless && a.keyBlocked(c.ID) {
		return "denied", "Key unavailable"
	}
	if c != nil && c.Probe == probeInvalid {
		return "invalid", "Key invalid"
	}
	ready := a.providerReady(c, keyless)
	if c != nil && c.Probe == probeOK && ready {
		return "ok", "Connected"
	}
	if ready {
		if keyless {
			return "saved", "Configured"
		}
		return "saved", "Key saved"
	}
	return "none", "Not set up"
}

func (a *App) displayModels(name string, c *Connection) []ProviderModel {
	if c != nil && c.ModelsSet {
		return append([]ProviderModel(nil), c.Models...)
	}
	if c != nil && c.Custom {
		return nil
	}
	return builtinModels(name)
}

func enabledIDs(models []ProviderModel) []string {
	var out []string
	for _, m := range models {
		if m.Enabled && strings.TrimSpace(m.ID) != "" {
			out = append(out, m.ID)
		}
	}
	return out
}

func (a *App) enabledModelIDs(name string, c *Connection) []string {
	return enabledIDs(a.displayModels(name, c))
}

// modelInLists reports whether provider's model is still offered.
func (a *App) modelInLists(provider, model string) bool {
	if strings.TrimSpace(model) == "" {
		return false
	}
	c := a.providerConnection(provider)
	if c != nil {
		return a.connectionOffersEnabled(c, model)
	}
	for _, id := range a.enabledModelIDs(provider, nil) {
		if id == model {
			return true
		}
	}
	return false
}

// modelOnConnection reports whether this connection still offers model.
// The connection id is the provider, so two providers that share a base
// URL do not answer for each other.
func (a *App) modelOnConnection(connID, model string) bool {
	c := a.connByID(connID)
	if c == nil {
		return false
	}
	return a.connectionOffersEnabled(c, model)
}

func (a *App) connectionOffersEnabled(c *Connection, model string) bool {
	name := c.Provider
	if c.Custom || name == "" {
		name = c.Name
	}
	for _, id := range a.enabledModelIDs(name, c) {
		if id == model {
			return true
		}
	}
	return false
}

// viewProvider loads a built-in into the detail page. Nothing is created
// and nothing is saved.
func (a *App) viewProvider(name string) {
	if !knownProvider(name) || name == "Custom" {
		return
	}
	show := a.provEdit.showKey && a.provEdit.name == name
	ed := providerEditor{name: name, showKey: show, keyless: keylessProvider(name), preset: name}
	if c := a.builtinConn(name); c != nil {
		ed.connID = c.ID
		if strings.TrimSpace(c.Name) != "" {
			ed.name = c.Name
		}
		ed.baseURL = storedBase(name, c.BaseURL)
		ed.apiKey = a.resolvedKey(Config{ConnectionID: c.ID})
		ed.models = a.displayModels(name, c)
	} else {
		ed.models = builtinModels(name)
	}
	ed.loadedBase = ed.baseURL
	ed.loadedKey = ed.apiKey
	a.provEdit = ed
}

func (a *App) viewCustom(id string) {
	c := a.connByID(id)
	if c == nil || !c.Custom {
		return
	}
	show := a.provEdit.showKey && a.provEdit.connID == id
	ed := providerEditor{
		name: c.Name, connID: c.ID, custom: true, preset: "Custom", showKey: show,
		baseURL: strings.TrimSpace(c.BaseURL),
		apiKey:  a.resolvedKey(Config{ConnectionID: c.ID}),
		models:  a.displayModels(c.Name, c),
		format:  connFormat(c),
	}
	ed.loadedBase = ed.baseURL
	ed.loadedKey = ed.apiKey
	a.provEdit = ed
}

func (a *App) openBuiltin(name string, now time.Time) {
	if a.settingsPage == settingsDetail {
		a.flushProviderEdits(now)
	}
	a.addForm = addDraft{}
	a.viewProvider(name)
	a.settingsPage = settingsDetail
}

func (a *App) openCustom(id string, now time.Time) {
	if a.settingsPage == settingsDetail {
		a.flushProviderEdits(now)
	}
	a.addForm = addDraft{}
	a.viewCustom(id)
	a.settingsPage = settingsDetail
}

// leaveProviderPage returns to the provider list. A detail page commits
// fields the user changed. Add Provider is discarded.
func (a *App) leaveProviderPage(now time.Time) {
	switch a.settingsPage {
	case settingsPending:
		a.provEdit = providerEditor{}
		a.settingsPage = settingsAdd
		return
	case settingsCustom:
		a.addForm = addDraft{}
		a.formError = ""
		a.settingsPage = settingsAdd
		return
	case settingsDetail:
		a.flushProviderEdits(now)
	case settingsAdd:
		a.addForm = addDraft{}
	}
	if a.settingsPage != "" {
		a.provEdit = providerEditor{}
		a.settingsPage = settingsList
	}
}

// editProviderKind is the preset used for the official URL and the catalog.
// The display name can be "OpenAI 2" while the preset stays "OpenAI".
func (a *App) editProviderKind() string {
	if a.provEdit.custom {
		return "Custom"
	}
	if a.provEdit.preset != "" {
		return a.provEdit.preset
	}
	if c := a.editingConn(); c != nil && c.Provider != "" && !c.Custom {
		return c.Provider
	}
	return a.provEdit.name
}

func (a *App) flushProviderEdits(now time.Time) {
	if a.provEdit.name == "" {
		return
	}
	if a.provEdit.baseURL != a.provEdit.loadedBase {
		a.saveProviderBase(now)
	}
	if a.provEdit.apiKey != a.provEdit.loadedKey {
		a.saveProviderKey(now)
	}
}

func (a *App) editingConn() *Connection {
	if a.provEdit.connID != "" {
		if c := a.connByID(a.provEdit.connID); c != nil {
			return c
		}
	}
	if a.provEdit.custom {
		return nil
	}
	return a.builtinConn(a.provEdit.name)
}

func (a *App) ensureEditingConnection() *Connection {
	if c := a.editingConn(); c != nil {
		a.provEdit.connID = c.ID
		return c
	}
	name := a.provEdit.name
	provider := name
	custom := a.provEdit.custom
	if custom {
		provider = "Custom"
	}
	id := newID()
	a.data.Connections = append(a.data.Connections, Connection{
		ID: id, Name: name, Provider: provider, Custom: custom,
		BaseURL: storedBase(provider, a.provEdit.baseURL),
	})
	a.provEdit.connID = id
	return &a.data.Connections[len(a.data.Connections)-1]
}

func (a *App) clearProbe(c *Connection) {
	if c != nil {
		c.Probe = ""
	}
	a.provEdit.testKind = ""
	a.provEdit.testLine = ""
}

// saveProviderBase writes this provider's base URL. The official address
// is stored as empty. App defaults and chats stay as they are.
func (a *App) saveProviderBase(now time.Time) {
	if a.provEdit.draft {
		return
	}
	name := a.provEdit.name
	if name == "" {
		return
	}
	provider := a.editProviderKind()
	url := storedBase(provider, a.provEdit.baseURL)
	if a.provEdit.custom && url == "" {
		a.formError = "A custom provider needs a base URL."
		a.provEdit.baseURL = a.provEdit.loadedBase
		return
	}
	a.provEdit.baseURL = url
	c := a.editingConn()
	if c == nil {
		if url == "" {
			a.provEdit.loadedBase = ""
			return
		}
		c = a.ensureEditingConnection()
	}
	if c.BaseURL == url && a.provEdit.loadedBase == url {
		return
	}
	c.BaseURL = url
	a.clearProbe(c)
	a.provEdit.loadedBase = url
	a.noteSaved(now)
	a.save()
}

func (a *App) saveProviderFormat(format string, now time.Time) {
	format = normalizeFormat(format)
	a.provEdit.format = format
	if a.provEdit.draft {
		return
	}
	c := a.editingConn()
	if c == nil || c.Format == format {
		return
	}
	c.Format = format
	a.noteSaved(now)
	a.save()
}

// saveProviderKey writes this provider's keychain item. The conversations
// file changes only when a connection has to be created to hold the key,
// or when a previous test result is cleared.
func (a *App) saveProviderKey(now time.Time) {
	if a.provEdit.draft || a.provEdit.name == "" || a.provEdit.keyless {
		return
	}
	key := strings.TrimSpace(a.provEdit.apiKey)
	a.provEdit.apiKey = key
	c := a.editingConn()
	stored := ""
	if c != nil {
		stored = a.resolvedKey(Config{ConnectionID: c.ID})
	}
	if key == stored {
		a.provEdit.loadedKey = key
		return
	}
	created := c == nil
	if c == nil {
		c = a.ensureEditingConnection()
	}
	if a.keys == nil {
		a.formError = "No credential store is available."
		return
	}
	var err error
	if key == "" {
		err = a.keys.Delete(c.ID)
	} else {
		err = a.keys.Set(c.ID, key)
	}
	if err != nil {
		a.formError = err.Error()
		return
	}
	if a.assumedKeys == nil {
		a.assumedKeys = map[string]bool{}
	}
	if key == "" {
		delete(a.assumedKeys, c.ID)
	} else {
		a.assumedKeys[c.ID] = true
	}
	a.clearProbe(c)
	a.provEdit.loadedKey = key
	a.noteSaved(now)
	if created || c.Probe == "" {
		a.save()
	}
}

func (a *App) setModelEnabled(id string, on bool, now time.Time) {
	if a.provEdit.name == "" || id == "" {
		return
	}
	found := false
	for i := range a.provEdit.models {
		if a.provEdit.models[i].ID != id {
			continue
		}
		a.provEdit.models[i].Enabled = on
		a.provEdit.models[i].Fresh = false
		found = true
	}
	if !found {
		return
	}
	a.writeEditorModels(now)
}

func (a *App) addEditorModel(now time.Time) {
	id := strings.TrimSpace(a.provEdit.newModel)
	if id == "" {
		return
	}
	for _, m := range a.provEdit.models {
		if m.ID == id {
			a.formError = "That model is already in the list."
			return
		}
	}
	a.provEdit.models = append(a.provEdit.models, ProviderModel{ID: id, Enabled: true})
	a.provEdit.adding = false
	a.provEdit.newModel = ""
	a.writeEditorModels(now)
}

func (a *App) writeEditorModels(now time.Time) {
	if a.provEdit.draft {
		a.formError = ""
		return
	}
	c := a.ensureEditingConnection()
	c.Models = append([]ProviderModel(nil), a.provEdit.models...)
	c.ModelsSet = true
	a.formError = ""
	a.noteSaved(now)
	a.save()
}

// applyDefaultModel sets the app-wide default from the General popup.
// The connection id is the provider. Temperature, max tokens, reasoning,
// and the system prompt stay.
func (a *App) applyDefaultModel(connID, model string, now time.Time) {
	model = strings.TrimSpace(model)
	c := a.connByID(connID)
	if c == nil || model == "" {
		return
	}
	if a.pref.cfg.ConnectionID == c.ID && a.pref.cfg.Model == model {
		return
	}
	cfg := a.pref.cfg
	cfg.Provider = connDisplay(c)
	cfg.Model = model
	cfg.APIKey = ""
	cfg.ConnectionID = c.ID
	cfg.BaseURL = connEffective(c)
	a.pref = a.snapshotFrom(cfg)
	a.commitPref(now)
}

func (a *App) noteSaved(now time.Time) {
	a.formError = ""
	a.appearanceSavedUntil = now.Add(motionCopied)
}

// commitLine saves when Enter is pressed or the field loses focus.
func commitLine(in ui.Element, focused *bool, now time.Time, save func(time.Time)) {
	on := in.Focused()
	if in.Submitted() || (*focused && !on) {
		save(now)
	}
	*focused = on
}

func presetByName(name string) (preset, bool) {
	for _, p := range presets {
		if p.Name == name {
			return p, true
		}
	}
	return preset{}, false
}

func parseModels(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" || seen[part] {
			continue
		}
		seen[part] = true
		out = append(out, part)
	}
	return out
}

func sameModels(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// menuModel is one enabled model of a provider that is set up.
// connID is the provider. Two rows can share a model id and a base URL.
type menuModel struct {
	provider, model, connID string
	custom                  bool
}

// modelChosen is the composer's check mark: the open chat's connection
// and model, not the provider's display name or base URL.
func modelChosen(cfg Config, connID, model string) bool {
	return connID != "" && cfg.ConnectionID == connID && cfg.Model == model
}

// connectionColor is the dot for a saved connection. Kind comes from the
// connection, not from the name drawn in the sidebar or the model chip.
// A missing connection is text/tertiary, not the custom brown.
func (a *App) connectionColor(c *Connection) ui.Color {
	if c == nil {
		return a.look.TextTertiary
	}
	return a.look.providerColor(c.Provider, c.Custom)
}

// configColor is the dot for a chat or the default model. It follows the
// connection id. A display name such as "OpenAI 2" or a custom provider
// named "OpenAI" does not pick the color. No connection uses text/tertiary.
func (a *App) configColor(cfg Config) ui.Color {
	if id := strings.TrimSpace(cfg.ConnectionID); id != "" {
		return a.connectionColor(a.connByID(id))
	}
	return a.look.TextTertiary
}

func connDisplay(c *Connection) string {
	if c == nil {
		return ""
	}
	if name := strings.TrimSpace(c.Name); name != "" {
		return name
	}
	return c.Provider
}

func connEffective(c *Connection) string {
	if c == nil {
		return ""
	}
	kind := c.Provider
	if c.Custom {
		kind = "Custom"
	}
	return effectiveBase(kind, c.BaseURL)
}

func (a *App) menuModels() []menuModel {
	var out []menuModel
	for i := range a.data.Connections {
		c := &a.data.Connections[i]
		keyless := !c.Custom && keylessProvider(c.Provider)
		if !a.providerReady(c, keyless) {
			continue
		}
		name := connDisplay(c)
		catalog := c.Provider
		if c.Custom || catalog == "" {
			catalog = c.Name
		}
		for _, model := range a.enabledModelIDs(catalog, c) {
			out = append(out, menuModel{provider: name, model: model, connID: c.ID, custom: c.Custom})
		}
	}
	return out
}

// firstFallback is the first enabled model of the first set-up provider,
// in list order, skipping a connection that is about to be deleted.
func (a *App) firstFallback(skipID string) (provider, model, connID string, ok bool) {
	for i := range a.data.Connections {
		c := &a.data.Connections[i]
		if c.ID == skipID {
			continue
		}
		keyless := !c.Custom && keylessProvider(c.Provider)
		if !a.providerReady(c, keyless) {
			continue
		}
		catalog := c.Provider
		if c.Custom || catalog == "" {
			catalog = c.Name
		}
		ids := a.enabledModelIDs(catalog, c)
		if len(ids) == 0 {
			continue
		}
		return connDisplay(c), ids[0], c.ID, true
	}
	return "", "", "", false
}

func (a *App) deleteCustomProvider(id string, now time.Time) {
	a.deleteProvider(id, now)
}

// deleteProvider removes any added provider, including a preset. Chats
// that used it take the default model and show a one-time composer note.
// If the default model was on this provider, it falls back to the first
// enabled model of the first ready provider still in the list.
func (a *App) deleteProvider(id string, now time.Time) {
	c := a.connByID(id)
	if c == nil {
		a.settingsPage = settingsList
		return
	}
	name := connDisplay(c)
	wasDefault := a.data.Defaults.ConnectionID == id
	using := a.chatIDsUsing(id)
	if a.keys != nil {
		_ = a.keys.Delete(id)
	}
	delete(a.assumedKeys, id)
	next := a.data.Connections[:0]
	for _, item := range a.data.Connections {
		if item.ID != id {
			next = append(next, item)
		}
	}
	a.data.Connections = next
	a.provEdit = providerEditor{}
	a.settingsPage = settingsList
	a.providerNotice = ""
	if wasDefault {
		if prov, model, conn, ok := a.firstFallback(""); ok {
			cfg := a.data.Defaults
			cfg.Provider = prov
			cfg.Model = model
			cfg.ConnectionID = conn
			cfg.APIKey = ""
			if live := a.connByID(conn); live != nil {
				cfg.Provider = connDisplay(live)
				cfg.BaseURL = connEffective(live)
			}
			a.data.Defaults = cfg
			a.pref = a.snapshotFrom(cfg)
			a.prefReady = true
			a.modelPick = modelChoiceID(conn, model)
			a.providerNotice = name + " was deleted. New chats now use " + model + " (" + prov + ")."
		} else {
			a.data.Defaults.Provider = ""
			a.data.Defaults.Model = ""
			a.data.Defaults.ConnectionID = ""
			a.data.Defaults.BaseURL = ""
			a.pref = a.snapshotFrom(a.data.Defaults)
			a.prefReady = true
			a.modelPick = ""
		}
	}
	if a.data.Defaults.ConnectionID != "" && a.connByID(a.data.Defaults.ConnectionID) != nil {
		note := deletedProviderNote(name, a.data.Defaults.Model)
		for _, chatID := range using {
			a.ensureChat(chatID)
			conv := a.conversation(chatID)
			if conv == nil {
				continue
			}
			conv.Config.ConnectionID = a.data.Defaults.ConnectionID
			conv.Config.Model = a.data.Defaults.Model
			conv.Config.Provider = a.data.Defaults.Provider
			conv.Config.BaseURL = a.data.Defaults.BaseURL
			conv.Config.APIKey = ""
			conv.SwitchNote = note
		}
	}
	a.noteSaved(now)
	a.save()
}

func deletedProviderNote(name, model string) string {
	return "Switched to " + model + " — “" + name + "” was removed."
}

// chatIDsUsing lists chats whose header names this connection, including
// chats that are still only an index row.
func (a *App) chatIDsUsing(id string) []string {
	if id == "" {
		return nil
	}
	var out []string
	for i := range a.data.Conversations {
		c := &a.data.Conversations[i]
		if c.Config.ConnectionID == id {
			out = append(out, c.ID)
			continue
		}
		if c.Loaded || c.Messages != nil {
			continue
		}
		if a.headerConnectionID(c.ID) == id {
			out = append(out, c.ID)
		}
	}
	return out
}

func (a *App) headerConnectionID(id string) string {
	if a.path == "" {
		return ""
	}
	path, err := chatFile(storeRoot(a.path), id)
	if err != nil {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return ""
	}
	var header struct {
		Settings struct {
			ConnectionID string `json:"connectionId"`
		} `json:"settings"`
	}
	if json.Unmarshal(sc.Bytes(), &header) != nil {
		return ""
	}
	return header.Settings.ConnectionID
}

func (a *App) noteProviderAuth(convID string, err error) {
	var se *StatusError
	if !errors.As(err, &se) || (se.Status != 401 && se.Status != 403) {
		return
	}
	conv := a.conversation(convID)
	if conv == nil {
		return
	}
	a.markProviderInvalid(conv.Config)
}

func (a *App) markProviderInvalid(cfg Config) {
	var c *Connection
	if cfg.ConnectionID != "" {
		c = a.connByID(cfg.ConnectionID)
	}
	if c == nil {
		c = a.providerConnection(cfg.Provider)
	}
	if c == nil || c.Probe == probeInvalid {
		return
	}
	c.Probe = probeInvalid
	a.save()
}

func providerListed(data *StoreData, name string) bool {
	if knownProvider(name) {
		return true
	}
	for _, c := range data.Connections {
		if c.Name == name {
			return true
		}
	}
	return false
}

// legacyIdentityRaw is a schema that stored the provider name, model, and
// base URL on the chat itself and had no connection id.
func legacyIdentityRaw(cfg Config) json.RawMessage {
	m := map[string]string{}
	if strings.TrimSpace(cfg.Provider) != "" {
		m["provider"] = cfg.Provider
	}
	if strings.TrimSpace(cfg.Model) != "" {
		m["model"] = cfg.Model
	}
	if strings.TrimSpace(cfg.BaseURL) != "" {
		m["baseUrl"] = cfg.BaseURL
	}
	b, err := json.Marshal(m)
	if err != nil {
		return []byte("{}")
	}
	return b
}

// bindChatConnection attaches a connection id to a chat or the global
// default whose stored settings named a provider, a base URL, or a model
// and no connection id. The model string is not rewritten. A unique name,
// then a model only one connection lists, then a URL only one connection
// uses, is the provider that reference meant. Several matches take the
// first connection in list order, and that choice is logged.
func bindChatConnection(cfg Config, raw json.RawMessage, conns []Connection) Config {
	keys := overrideKeys(raw)
	if keys == nil {
		return cfg
	}
	if _, ok := keys["connectionId"]; ok {
		return cfg
	}
	_, hasModel := keys["model"]
	_, hasURL := keys["baseUrl"]
	_, hasProvider := keys["provider"]
	if !hasModel && !hasURL && !hasProvider {
		return cfg
	}
	id := pickConnection(cfg, conns, hasProvider, hasURL)
	if id == "" || id == cfg.ConnectionID {
		return cfg
	}
	c := connSliceByID(conns, id)
	if c == nil {
		return cfg
	}
	cfg.ConnectionID = c.ID
	if name := connDisplay(c); name != "" {
		cfg.Provider = name
	}
	cfg.BaseURL = connEffective(c)
	return cfg
}

func overrideKeys(raw json.RawMessage) map[string]json.RawMessage {
	trimmed := bytesTrim(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(trimmed, &m) != nil {
		return nil
	}
	return m
}

func bytesTrim(raw json.RawMessage) []byte {
	return []byte(strings.TrimSpace(string(raw)))
}

func connSliceByID(conns []Connection, id string) *Connection {
	if id == "" {
		return nil
	}
	for i := range conns {
		if conns[i].ID == id {
			return &conns[i]
		}
	}
	return nil
}

func connsNamed(conns []Connection, name string) []*Connection {
	name = strings.TrimSpace(name)
	if name == "" || name == "Custom" {
		return nil
	}
	var out []*Connection
	for i := range conns {
		c := &conns[i]
		if c.Name == name || (!c.Custom && c.Provider == name) {
			out = append(out, c)
		}
	}
	return out
}

// connOffers reports whether the connection's catalog contains model,
// including ids the user has not switched on. Enabled-only would miss a
// chat that named a built-in id other than the preset's usual model.
func connOffers(c *Connection, model string) bool {
	model = strings.TrimSpace(model)
	if c == nil || model == "" {
		return false
	}
	if c.ModelsSet {
		for _, m := range c.Models {
			if m.ID == model {
				return true
			}
		}
		return false
	}
	if c.Custom {
		return false
	}
	name := c.Provider
	if name == "" {
		name = c.Name
	}
	for _, id := range catalogIDs(name) {
		if id == model {
			return true
		}
	}
	return false
}

func pickConnection(cfg Config, conns []Connection, named, hasURL bool) string {
	model := strings.TrimSpace(cfg.Model)
	if named {
		matches := connsNamed(conns, cfg.Provider)
		if len(matches) == 1 {
			return matches[0].ID
		}
		if len(matches) > 1 && model != "" {
			listed := connsOffering(matches, model)
			if len(listed) == 1 {
				return listed[0].ID
			}
			if len(listed) > 1 {
				return firstMatch(listed, cfg, hasURL)
			}
		}
		if len(matches) > 1 && model == "" {
			return firstMatch(matches, cfg, hasURL)
		}
	}
	if model != "" {
		var listed []*Connection
		for i := range conns {
			if connOffers(&conns[i], model) {
				listed = append(listed, &conns[i])
			}
		}
		if len(listed) == 1 {
			return listed[0].ID
		}
		if len(listed) > 1 {
			return firstMatch(listed, cfg, hasURL)
		}
	}
	if hasURL {
		var hits []*Connection
		for i := range conns {
			if sameEndpoint(connEffective(&conns[i]), cfg.BaseURL) {
				hits = append(hits, &conns[i])
			}
		}
		if len(hits) == 1 {
			return hits[0].ID
		}
		if len(hits) > 1 {
			return logAmbiguous(hits[0], cfg)
		}
	}
	return ""
}

func connsOffering(conns []*Connection, model string) []*Connection {
	var out []*Connection
	for _, c := range conns {
		if connOffers(c, model) {
			out = append(out, c)
		}
	}
	return out
}

// firstMatch narrows a tied set by base URL when that still leaves someone.
// One survivor is the provider the reference meant. Several take the first
// in list order, and that choice is logged.
func firstMatch(cands []*Connection, cfg Config, hasURL bool) string {
	if hasURL {
		var hits []*Connection
		for _, c := range cands {
			if sameEndpoint(connEffective(c), cfg.BaseURL) {
				hits = append(hits, c)
			}
		}
		if len(hits) == 1 {
			return hits[0].ID
		}
		if len(hits) > 1 {
			cands = hits
		}
	}
	if len(cands) == 1 {
		return cands[0].ID
	}
	if len(cands) == 0 {
		return ""
	}
	return logAmbiguous(cands[0], cfg)
}

func logAmbiguous(c *Connection, cfg Config) string {
	if c == nil {
		return ""
	}
	storeLog("cove: ambiguous model reference model %q base %q; using %s, the first matching provider", cfg.Model, cfg.BaseURL, c.ID)
	return c.ID
}

// migrateProviders copies a schema ≤5 shared base URL and comma-separated
// model list onto the provider that was selected then. The official address
// is not stored. Default model, temperature, and max tokens are untouched.
func migrateProviders(data *StoreData, from int) {
	// v6 already ran this step. A later schema must not create a preset
	// row just because the old shared base URL is still on defaults.
	if from >= 6 {
		data.Defaults.LegacyModels = ""
		return
	}
	selected := data.Defaults.Provider
	if selected == "" {
		selected = "OpenAI"
	}
	if csv := strings.TrimSpace(data.Defaults.LegacyModels); csv != "" {
		if c := ensureMigrated(data, selected); c != nil {
			var models []ProviderModel
			for _, id := range parseModels(csv) {
				models = append(models, ProviderModel{ID: id, Enabled: true})
			}
			c.Models = models
			c.ModelsSet = true
		}
	}
	data.Defaults.LegacyModels = ""
	for i := range data.Conversations {
		data.Conversations[i].Config.LegacyModels = ""
	}
	base := strings.TrimSpace(data.Defaults.BaseURL)
	off := officialBase(selected)
	if base != "" && !sameEndpoint(base, off) {
		if c := ensureMigrated(data, selected); c != nil && (strings.TrimSpace(c.BaseURL) == "" || sameEndpoint(c.BaseURL, off)) {
			c.BaseURL = base
		}
	}
	for i := range data.Connections {
		c := &data.Connections[i]
		kind := c.Provider
		if c.Custom {
			kind = "Custom"
		}
		if off := officialBase(kind); off != "" && sameEndpoint(c.BaseURL, off) {
			c.BaseURL = ""
		}
	}
}

// ensureMigrated returns the connection that receives a schema ≤5 base
// URL and model list. An unknown provider name is left for normalize to
// repair to Custom, so a bad name does not become its own row.
func ensureMigrated(data *StoreData, provider string) *Connection {
	if provider == "Custom" {
		for i := range data.Connections {
			if data.Connections[i].Custom {
				return &data.Connections[i]
			}
		}
		data.Connections = append(data.Connections, Connection{
			ID: newID(), Name: "Custom", Provider: "Custom", Custom: true,
		})
		return &data.Connections[len(data.Connections)-1]
	}
	if !knownProvider(provider) {
		return nil
	}
	for i := range data.Connections {
		c := &data.Connections[i]
		if c.Custom {
			continue
		}
		if c.Provider == provider || c.Name == provider {
			if c.Name == "" {
				c.Name = provider
			}
			if c.Provider == "" {
				c.Provider = provider
			}
			return c
		}
	}
	data.Connections = append(data.Connections, Connection{
		ID: newID(), Name: provider, Provider: provider,
	})
	return &data.Connections[len(data.Connections)-1]
}
