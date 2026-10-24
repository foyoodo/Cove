package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// convStream is one in-flight reply. gen drops a callback that arrives
// after Stop or a newer reply on the same chat.
type convStream struct {
	gen    int
	cancel context.CancelFunc
}

// App is the state the window shows. The view reads it on the main thread.
// A streaming reply writes it back through ui, which is Window.Update when
// a window exists and a direct call in tests.
type App struct {
	win  *mygo.Window
	path string

	data StoreData

	sidebarWidth float32
	side         ui.ListState
	chat         ui.ListState
	md           markdown.Options

	settingsOpen bool
	defaultsOpen bool
	renameOpen   bool
	requestOpen  bool
	renameText   string
	deleteID     string

	// confirm is the custom dialog: delete, resend, or reset.
	confirmKind string

	prefs       *mygo.Window
	formDefault bool
	// settingsOpen embeds the settings window in the main view when there
	// is no real window (tests and headless shots). A live app opens prefs.
	settingsSection string
	// settingsPage is the Models & Providers page: list, detail, or add.
	settingsPage string
	// modelMenuChat is the chat whose model menu is open. The choice is
	// delivered on a later frame, which may already be showing another chat.
	// It is recorded when the menu opens and read only for that choice.
	// Check marks follow that chat, so a menu dismissed without a choice
	// cannot leave the next chat's menu showing the previous one.
	modelMenuChat string
	// modelMenuSeen is set during the frame while that popup is open.
	modelMenuSeen bool
	// popovers are the in-window menus (model, effort, More, settings).
	// Right-click menus and the menu bar stay on the system menu.
	popovers map[string]*popoverState
	// addProviderNote is the chat showing "Add a provider to start chatting."
	addProviderNote string
	settingsQuery   string
	providerNotice  string
	addForm         addDraft
	acksOpen        bool
	modelPick       string
	timeoutText     string
	pref            settingsDraft
	prefReady       bool
	prefShowKey     bool
	// provEdit is the provider panel. It is not the app defaults: choosing
	// a provider only changes which connection the panel shows.
	provEdit providerEditor

	form       Config
	formMax    float64
	formTemp   float64
	formTempOn bool
	formEffort string
	formStyle  string
	// formRounds is the chat sheet's Max context. 0 is unlimited.
	// It is written onto the chat when the sheet saves.
	formRounds int
	showKey    bool
	formError  string
	query      string
	sideRows   []sideRow

	notice      string
	loadError   string
	saveError   string
	announce    string
	copied      map[string]time.Time
	composerGen map[string]int
	// edit is a user turn loaded into the composer. The thread stays as it
	// is until the edit is submitted. draft is the composer text to restore
	// if the edit is abandoned.
	edit         messageEdit
	dark         bool
	instant      bool
	reduceMotion bool
	look         Theme
	lookTarget   Theme
	haveLook     bool
	blendFrom    Theme
	blendStart   time.Time
	// fadeLive is the background transition that starts on the click,
	// with the control mix. backdropFadeAt is that same instant: 150ms
	// toward the undithered preview, filtered as it scales up. swapAt
	// is the crossfade from that preview onto the dithered full-resolution
	// bitmap. swapDur is at most 150ms, and it is the time still left on
	// the clock when the bitmap is already in flight. veilStandIn keeps
	// the tinted previous veil until the real one can snap in.
	fadeLive       bool
	backdropFadeAt time.Time
	swapAt         time.Time
	swapDur        time.Duration
	veilStandIn    bool
	suppressFade   bool
	previewBM      *ui.Bitmap
	fullBM         *ui.Bitmap
	fadeFromBM     *ui.Bitmap
	// previewed is set once this switch has drawn the new theme's preview.
	// After that, the bitmap captured at the start is not drawn again when
	// the full-resolution fade takes over: that fade goes over the preview,
	// and putting the old bitmap back is the flash.
	previewed bool
	// baseStack is the picture on screen when this switch started, when
	// that picture is more than one opaque bitmap. A switch that arrives
	// mid-fade keeps those layers under the new one, so the base is the
	// current blend and not an earlier theme.
	baseStack []fadeSlice
	// baseDrawn is set for the frame that painted baseStack.
	baseDrawn    bool
	winPW, winPH int
	veilFrom     Theme
	veilTo       Theme
	// veilStack is the veil composite on screen when this switch started,
	// when that picture is more than one layer. The next fade keeps it as
	// the base instead of an earlier theme or the incoming veil alone.
	veilStack []veilSlice
	// veilProbe, when set, paints an opaque color over the transcript and
	// under the veil so a test can read the veil's strength. Nil in the app.
	veilProbe *ui.Color
	veilGeoms []veilGeom
	veilFresh bool
	veilNoted bool
	veilWinW  int
	veilWinH  int
	// suppressEditEscape is set when the window shortcut already handled
	// Escape or Return for a confirm dialog, so the editor does not
	// also cancel or resend in the same frame.
	suppressEditEscape bool

	modalOpen            bool
	settingsTab          int
	editingTab           int
	chatDraft            settingsDraft
	defaultsDraft        settingsDraft
	appearanceSavedUntil time.Time
	shotStates           bool
	shotPalette          bool
	shotInactive         bool
	shotPinEdit          bool
	shotPinBreak         bool
	newSessionItem       *mygo.MenuItem
	composerField        ui.Element
	searchInput          ui.Element
	roundsText           string
	roundsScope          string
	roundsFocus          string
	roundsNow            time.Time
	lastLimitGlobal      int
	lastLimitChat        int
	shotAccent           ui.Color
	composerH            float32
	// focusComposerID asks the next frame to put the caret in that chat's
	// composer. It is cleared if a sheet, dialog, or the Settings window
	// already has the keyboard.
	focusComposerID string
	// Window reactivation. seenWindowFocus skips the first frame.
	// activateKeep is who should still have the keyboard after the
	// main window becomes key again. The composer caret is recorded
	// while the window is key; with no record it goes to the end.
	seenWindowFocus      bool
	windowWasFocused     bool
	pendingActivate      bool
	activateByClick      bool
	activateKeep         int
	composerCaretSet     bool
	composerAnchor       int
	composerCaretPos     int
	restoreComposerCaret bool
	// restoreComposer and restoreSearch record who had the keyboard when a
	// confirm dialog opened. Dismiss returns it there. A click in Search
	// is not overwritten with the composer.
	restoreComposer bool
	restoreSearch   bool
	// focusSearch asks the next build of the search field to take the
	// keyboard. A dialog's Escape runs before that field is built.
	focusSearch bool
	// keyboardOwner is who had the keyboard at the end of the last frame:
	// "search", "composer", "find", or empty. Shortcuts run before fields
	// are built, so a dialog cannot ask the live element.
	keyboardOwner string
	// histSel is the transcript selection captured on the previous build.
	// ⌘L reads it at the start of the next frame, before the list is rebuilt.
	histSel HistorySelection
	// quoteBarOpen is the placeholder selection bar. It opens on mouse-up
	// over a non-empty selection and closes on Esc, a click elsewhere, or
	// the selection leaving the transcript viewport.
	quoteBarOpen         bool
	quoteBarBelow        bool
	quoteBarX, quoteBarY float32
	quoteBarRect         ui.Rect
	quoteBarW            float32
	quoteBarShown        time.Time
	quoteBarWait         time.Time
	quoteBarWaitText     string
	// quoteBarHold keeps the bar down for the highlight left behind by
	// the last dismiss. A new drag or double-click clears it. It does
	// not remember the selected text.
	quoteBarHold                 bool
	quoteDragging                bool
	quotePointerDown             bool
	quotePointerX, quotePointerY float32
	quoteFlashAt                 time.Time
	quoteFlashIndex              int
	quoteFlashPending            bool
	quoteLimitUntil              time.Time
	quoteLimitConv               string
	quoteLimitPending            bool
	quoteExpanded                map[string]bool
	quoteScroll                  ui.ScrollState
	quoteFollowEnd               bool
	// quotePhrase is a test probe: the next transcript build writes the
	// glyph spans of that phrase into quotePhraseAt.
	quotePhrase   string
	quotePhraseAt []ui.Rect
	// openedFocus is the one-time pass for a launch that lands on an empty draft.
	openedFocus bool
	// streamCaret draws the tide caret on the paragraph still being written.
	streamCaret bool
	// streamLines are the finished lines of the paragraph still being
	// written. They cover the source up to streamCut. A token only wraps
	// the tail after that, so a long reply does not reshape the lines
	// already on screen.
	streamLines []streamLine
	streamText  string
	streamCut   int
	streamAvail float32

	keys keyring
	// assumedKeys are providers loaded from disk that are treated as
	// having a saved key so the model menu can list them. The secret
	// itself is not read until that provider is used. A confirmed miss
	// removes the id.
	assumedKeys map[string]bool
	saveBlocked bool
	// openChats is the LRU of chats whose messages are in memory. The
	// front is the most recently used.
	openChats []string
	// inRender is true while a view function is building a frame.
	// keyRefresh marks the settings-open frame, which may load the two
	// drafts. Tests refuse any other credential access while painting.
	inRender   bool
	keyRefresh bool
	dirty      bool
	saveWait   bool
	// saveMu serializes writes. saveGen drops a snapshot that was cloned
	// before a newer one, so quitting cannot be overwritten by the save
	// that created an empty chat.
	saveMu      sync.Mutex
	saveGen     uint64
	streams     map[string]*convStream
	formScroll  ui.ScrollState
	emptyScroll ui.ScrollState

	// Overlay scrollbars stay hidden while a list is only hovered. A
	// change of which rows are in view shows a translucent thumb for a
	// few frames, then it goes away again. Static shots are idle.
	barFrames         int
	barReady          bool
	barFirst, barLast int
	// thinkSettled is the open flag a thinking block last rested at.
	// thinkBusy is set while its height is still moving. thinkSeen is
	// which blocks this frame built. Together they keep the transcript
	// from gliding on a collapse or an expand, and an off-screen block
	// does not pin that glide.
	thinkSettled              map[string]bool
	thinkBusy                 map[string]bool
	thinkSeen                 []string
	sideBarReady              bool
	sideBarFirst, sideBarLast int
	formBarReady              bool
	formSeenY                 float32
	emptyBarReady             bool
	emptySeenY                float32

	// finder is ⌘F within the open chat. The index is built off the main
	// thread; the view only reads it.
	finder       findState
	finderMu     sync.Mutex
	findPending  *findBuild
	findItem     *mygo.MenuItem
	findNextItem *mygo.MenuItem
	findPrevItem *mygo.MenuItem

	// hScrolls keeps the horizontal offset of each code block and table.
	// hScrollMsg is the message whose markdown is being built.
	hScrolls   map[string]*hScrollSlot
	hScrollMsg string

	// Title generation. Jobs run off the frame. Fades and failures are
	// drawn on the next frame. sideKeep holds sidebar rows that matched
	// the current search so a new title does not drop them.
	titleJobs   map[string]*titleJob
	titleGens   map[string]int
	titleFades  map[string]titleFade
	titleFails  map[string]time.Time
	windowTitle string
	sideQuery   string
	sideKeep    map[string]bool
	sideClock   time.Time
	previewMenu *previewMenu
}

// NewApp loads conversations from path. A missing file starts empty. A
// corrupt file is moved aside and is not overwritten.
func NewApp(path string) *App {
	return openApp(path, nil)
}

func openApp(path string, keys keyring) *App {
	if keys == nil {
		keys = &lazyRing{path: path}
	}
	// Every path, including tests, reads through the one memory cache.
	keys = newKeyStore(keys)
	a := &App{
		path:         path,
		sidebarWidth: sidebarW,
		editingTab:   -1,
		copied:       map[string]time.Time{},
		composerGen:  map[string]int{},
		keys:         keys,
		streams:      map[string]*convStream{},
	}
	a.md.Reveal = &markdown.Reveal{CatchUp: 280 * time.Millisecond}
	data, notice, blocked := openStore(path)
	a.data = data
	a.loadError = notice
	a.saveBlocked = blocked
	if data.SidebarWidth > 0 {
		a.sidebarWidth = clampSidebar(data.SidebarWidth)
	}
	a.assumeStoredKeys()
	a.migrateSecrets()
	a.adoptProviders()
	if silenceAutoTitles {
		a.data.AutoTitles = false
	}
	return a
}

// assumeStoredKeys lets the model menu list providers that were saved
// before this process started, without reading their secrets. The first
// send, test, or settings open for that provider is what reads the
// Keychain. A provider added later in this process is not assumed; its
// key is known only after a Set or a read.
func (a *App) assumeStoredKeys() {
	a.assumedKeys = map[string]bool{}
	for i := range a.data.Connections {
		c := &a.data.Connections[i]
		if !c.Custom && keylessProvider(c.Provider) {
			continue
		}
		a.assumedKeys[c.ID] = true
	}
}

func (a *App) save() {
	a.persist(false)
}

// saveSync fsyncs the current snapshot before it returns. Send uses it so
// the user message is durable before the request starts.
func (a *App) saveSync() {
	if a.path == "" || a.saveBlocked {
		return
	}
	a.writeSnapshot(true)
}

// saveSoon schedules a snapshot at least two seconds out. Streaming calls
// this instead of encoding the whole file on every chunk.
func (a *App) saveSoon() {
	a.persist(true)
}

func (a *App) persist(debounce bool) {
	if a.path == "" || a.saveBlocked {
		return
	}
	if debounce && a.win != nil {
		a.dirty = true
		if a.saveWait {
			return
		}
		a.saveWait = true
		time.AfterFunc(2*time.Second, func() {
			a.ui(func() {
				a.saveWait = false
				if !a.dirty {
					return
				}
				a.writeSnapshot(false)
				if a.dirty {
					a.saveSoon()
				}
			})
		})
		return
	}
	a.writeSnapshot(a.win == nil)
}

func (a *App) writeSnapshot(syncWrite bool) {
	if a.path == "" || a.saveBlocked {
		return
	}
	a.saveMu.Lock()
	a.saveGen++
	gen := a.saveGen
	snap := cloneStore(a.data)
	a.dirty = false
	path := a.path
	a.saveMu.Unlock()
	// skipped is a snapshot that lost the race to a newer one. It must not
	// clear an error the newer write reported.
	write := func() (skipped bool, err error) {
		a.saveMu.Lock()
		defer a.saveMu.Unlock()
		if gen != a.saveGen {
			return true, nil
		}
		return false, Save(path, &snap)
	}
	if syncWrite {
		skipped, err := write()
		if skipped {
			return
		}
		if err != nil {
			a.saveError = err.Error()
			return
		}
		a.saveError = ""
		return
	}
	go func() {
		skipped, err := write()
		if skipped {
			return
		}
		if err != nil {
			a.ui(func() { a.saveError = err.Error() })
			return
		}
		a.ui(func() {
			if a.saveError != "" {
				a.saveError = ""
			}
		})
	}()
}

func (a *App) allowSave() {
	a.saveBlocked = false
	a.loadError = ""
	a.save()
}

func (a *App) conversation(id string) *Conversation {
	for i := range a.data.Conversations {
		if a.data.Conversations[i].ID == id {
			return &a.data.Conversations[i]
		}
	}
	return nil
}

const openChatLimit = 8

// ensureChat loads id's JSONL the first time it is opened. Chats created
// in memory already have a message slice and are not read from disk.
func (a *App) ensureChat(id string) {
	conv := a.conversation(id)
	if conv == nil {
		return
	}
	if conv.Loaded || conv.Messages != nil {
		if !conv.Loaded {
			conv.Loaded = true
			conv.MessageCount = len(conv.Messages)
		}
		a.recallChat(id)
		return
	}
	path, err := chatFile(storeRoot(a.path), id)
	if err != nil {
		conv.Loaded = true
		conv.Messages = []Message{}
		return
	}
	loaded, err := readChatConn(path, a.data.Defaults, a.data.Connections)
	if err != nil {
		conv.Loaded = true
		if conv.Messages == nil {
			conv.Messages = []Message{}
		}
		a.recallChat(id)
		return
	}
	draft := conv.Draft
	draftQuotes := append([]Quote(nil), conv.DraftQuotes...)
	fromIndex := conv.DraftFromIndex
	title := conv.Title
	updated := conv.UpdatedAt
	count := conv.MessageCount
	loaded.DraftFromIndex = fromIndex
	if fromIndex {
		loaded.Draft = draft
		loaded.DraftQuotes = draftQuotes
	}
	if title != "" {
		loaded.Title = title
	}
	if !updated.IsZero() {
		loaded.UpdatedAt = updated
	}
	if fromIndex && count > loaded.MessageCount {
		loaded.MessageCount = count
	}
	*conv = loaded
	a.recallChat(id)
	a.evictChats()
}

func (a *App) recallChat(id string) {
	next := make([]string, 0, len(a.openChats)+1)
	next = append(next, id)
	for _, old := range a.openChats {
		if old != id {
			next = append(next, old)
		}
	}
	a.openChats = next
}

func (a *App) evictChats() {
	guard := len(a.openChats)
	for len(a.openChats) > openChatLimit && guard > 0 {
		guard--
		id := a.openChats[len(a.openChats)-1]
		a.openChats = a.openChats[:len(a.openChats)-1]
		if id == a.data.ActiveID || a.isStreaming(id) || a.edit.convID == id {
			a.openChats = append([]string{id}, a.openChats...)
			continue
		}
		conv := a.conversation(id)
		if conv == nil || !conv.Loaded {
			continue
		}
		conv.MessageCount = len(conv.Messages)
		conv.DraftOnly = len(conv.Messages) == 0 && (strings.TrimSpace(conv.Draft) != "" || len(conv.DraftQuotes) > 0)
		conv.Messages = nil
		conv.Loaded = false
	}
}

func (a *App) active() *Conversation {
	return a.conversation(a.data.ActiveID)
}

func (a *App) findMessage(convID, msgID string) *Message {
	conv := a.conversation(convID)
	if conv == nil {
		return nil
	}
	for i := range conv.Messages {
		if conv.Messages[i].ID == msgID {
			return &conv.Messages[i]
		}
	}
	return nil
}

func (a *App) touch(conv *Conversation) {
	conv.UpdatedAt = time.Now()
	sortConversations(a.data.Conversations)
}

// untouched is a new chat with nothing to keep. Whitespace in the composer
// counts as empty. A message, or any non-whitespace draft, makes it a
// normal chat and it stays, draft included. Changing the model or the
// reasoning effort does not. Cove has no attachments; the draft and the
// messages are the whole composer.
func untouched(conv *Conversation) bool {
	if conv == nil {
		return false
	}
	if conv.Messages == nil && !conv.Loaded {
		return false
	}
	if len(conv.Messages) > 0 {
		return false
	}
	return strings.TrimSpace(conv.Draft) == "" && len(conv.DraftQuotes) == 0
}

// discardUntouchedExcept deletes empty new chats. keep is left in place so
// the chat the user just chose is still there to select.
func (a *App) discardUntouchedExcept(keep string) {
	next := make([]Conversation, 0, len(a.data.Conversations))
	for i := range a.data.Conversations {
		c := &a.data.Conversations[i]
		if c.ID != keep && untouched(c) {
			continue
		}
		next = append(next, *c)
	}
	if next == nil {
		next = []Conversation{}
	}
	a.data.Conversations = next
	if a.conversation(a.data.ActiveID) != nil {
		return
	}
	a.data.ActiveID = ""
	if keep != "" && a.conversation(keep) != nil {
		a.data.ActiveID = keep
		return
	}
	if len(next) > 0 {
		a.data.ActiveID = next[0].ID
	}
}

// persistForExit drops empty new chats and writes the file. Quit and
// closing the window both use it, so an untouched chat is not left on disk.
func (a *App) persistForExit() {
	a.cancelAllTitles()
	a.flushProviderEdits(time.Now())
	a.cancelEdit()
	a.nameKeptDrafts()
	a.discardUntouchedExcept("")
	a.writeSnapshot(true)
}

// nameKeptDrafts titles a chat that has composer text and no messages yet
// from the first line of that text, so the sidebar row survives a restart.
func (a *App) nameKeptDrafts() {
	for i := range a.data.Conversations {
		c := &a.data.Conversations[i]
		if len(c.Messages) > 0 || strings.TrimSpace(c.Draft) == "" {
			continue
		}
		if c.TitleSource == titleSourceManual {
			continue
		}
		c.Title = draftTitle(c.Draft)
	}
}

func (a *App) newChat() {
	if conv := a.active(); untouched(conv) {
		a.discardUntouchedExcept(conv.ID)
		a.notice = ""
		a.save()
		a.requestComposerFocus(conv.ID)
		return
	}
	a.cancelEdit()
	a.nameKeptDrafts()
	a.discardUntouchedExcept("")
	now := time.Now()
	conv := Conversation{
		ID:        newID(),
		Title:     newChatTitle,
		Config:    a.data.Defaults,
		Messages:  []Message{},
		MaxRounds: clampRounds(a.data.MaxRounds),
		CreatedAt: now,
		UpdatedAt: now,
	}
	a.data.Conversations = append(a.data.Conversations, conv)
	a.data.ActiveID = conv.ID
	sortConversations(a.data.Conversations)
	a.notice = ""
	a.save()
	a.requestComposerFocus(conv.ID)
}

const (
	focusOwnerSearch   = "search"
	focusOwnerComposer = "composer"
	focusOwnerFind     = "find"
)

// noteKeyboardOwner records who has the keyboard once the fields exist.
// A shortcut on the next frame runs before those fields are built.
func (a *App) noteKeyboardOwner() {
	switch {
	case a.searchInput.Valid() && a.searchInput.Focused():
		a.keyboardOwner = focusOwnerSearch
	case a.finder.open && a.finder.focused:
		a.keyboardOwner = focusOwnerFind
	case a.composerField.Valid() && a.composerField.Focused():
		a.keyboardOwner = focusOwnerComposer
	default:
		a.keyboardOwner = ""
	}
}

// focusComposer returns the keyboard to the bottom composer. Send, Stop,
// Regenerate, inline-edit send, closing Find, and Add to Chat (⌘L) call
// it. It runs on the action, not on a timer, and not on streaming frames,
// so a click the user made somewhere else stays there. A sheet that
// already has the keyboard keeps it.
func (a *App) focusComposer() {
	if conv := a.active(); conv != nil {
		a.requestComposerFocus(conv.ID)
	}
	// The send button and Find's Escape run after the composer was built
	// this frame, so the id flag would be cleared before the next build.
	// The text view's key does not change on send, so focusing it now
	// sticks.
	if !a.blocksComposerFocus() && a.composerField.Valid() {
		a.composerField.Focus()
	}
}

// requestComposerFocus moves the caret into the composer for id. A sheet,
// a dialog, or a focused Settings window keeps the keyboard it already has.
func (a *App) requestComposerFocus(id string) {
	if id == "" || a.blocksComposerFocus() {
		return
	}
	a.focusComposerID = id
}

// settleComposerFocus drops a pending caret move when a sheet, dialog, or
// the Settings window is already using the keyboard.
func (a *App) settleComposerFocus() {
	if a.blocksComposerFocus() {
		a.focusComposerID = ""
	}
}

// blocksComposerFocus reports whether the composer must not take the keyboard.
func (a *App) blocksComposerFocus() bool {
	if a.modalOpen || a.renameOpen || a.requestOpen || a.acksOpen || a.confirmKind != "" || a.settingsOpen {
		return true
	}
	if a.prefs != nil && a.prefs.IsFocused() {
		return true
	}
	return false
}

func (a *App) selectChat(id string) {
	if a.conversation(id) == nil {
		return
	}
	a.ensureChat(id)
	conv := a.conversation(id)
	if conv == nil {
		return
	}
	if a.data.ActiveID == id {
		// Clicking the draft row that is already selected is how you get
		// back to the composer. A chat that has messages is left alone.
		if len(conv.Messages) == 0 {
			a.requestComposerFocus(id)
		}
		return
	}
	a.cancelEdit()
	a.nameKeptDrafts()
	a.discardUntouchedExcept(id)
	a.data.ActiveID = id
	a.notice = ""
	a.save()
	if c := a.conversation(id); c != nil && len(c.Messages) == 0 {
		a.requestComposerFocus(id)
	}
}

func (a *App) beginRename() {
	conv := a.active()
	if conv == nil {
		return
	}
	a.renameText = conv.Title
	a.renameOpen = true
}

func (a *App) commitRename() {
	conv := a.active()
	if conv == nil {
		a.renameOpen = false
		return
	}
	title := strings.TrimSpace(a.renameText)
	if title == "" {
		title = newChatTitle
	}
	conv.Title = title
	conv.TitleSource = titleSourceManual
	a.touch(conv)
	a.renameOpen = false
	a.save()
}

func (a *App) askDelete() {
	conv := a.active()
	if conv == nil {
		return
	}
	a.deleteID = conv.ID
	a.ensureChat(conv.ID)
	conv = a.active()
	if conv == nil {
		return
	}
	// A chat with no messages, including a draft, goes away without a dialog.
	if len(conv.Messages) == 0 {
		a.deleteConversation(conv.ID)
		return
	}
	a.beginConfirm(confirmDelete)
}

func (a *App) deleteConversation(id string) {
	a.dropTitleState(id)
	if a.isStreaming(id) {
		a.stopChat(id)
	}
	if a.edit.convID == id {
		a.edit = messageEdit{}
	}
	next := make([]Conversation, 0, len(a.data.Conversations))
	for _, c := range a.data.Conversations {
		if c.ID != id {
			next = append(next, c)
		}
	}
	if next == nil {
		next = []Conversation{}
	}
	a.data.Conversations = next
	if a.data.ActiveID == id {
		a.data.ActiveID = ""
		if len(next) > 0 {
			a.data.ActiveID = next[0].ID
		}
	}
	a.confirmKind = ""
	a.save()
}

func (a *App) openSettings() {
	if a.active() == nil {
		a.openDefaults()
		return
	}
	a.openSettingsTab(0)
}

func (a *App) openDefaults() {
	a.ensurePref()
	if a.settingsSection == "" {
		a.settingsSection = "general"
	}
	// Headless tests have no second window. The main view draws the same
	// settings body. A live window is its own window.
	if a.win == nil {
		a.settingsOpen = true
		return
	}
	a.openSettingsWindow()
}

// openSettingsTab opens the shared settings modal. 0 is This chat, 1 is
// Defaults. Each tab keeps its draft until Save or Cancel.
func (a *App) openSettingsTab(tab int) {
	if !a.modalOpen {
		a.dropDrafts()
	}
	a.keyRefresh = true
	defer func() { a.keyRefresh = false }()
	a.ensureDrafts()
	if a.modalOpen && a.editingTab >= 0 && a.editingTab != tab {
		a.captureDraft(a.editingTab)
	}
	a.settingsTab = tab
	a.formDefault = tab == 1
	a.restoreDraft(tab)
	a.editingTab = tab
	a.modalOpen = true
	a.settingsOpen = false
	a.defaultsOpen = false
}

func (a *App) loadForm(cfg Config) {
	if !knownProvider(cfg.Provider) {
		cfg.Provider = "Custom"
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		cfg.APIKey = a.resolvedKey(cfg)
	}
	a.form = cfg
	a.formEffort = effortLabel(cfg.Reasoning)
	a.formStyle = "None"
	for _, name := range reasoningStyles {
		if name == cfg.ReasoningStyle {
			a.formStyle = name
			break
		}
	}
	a.formTempOn = cfg.Temperature != nil
	a.formTemp = 0.7
	if cfg.Temperature != nil {
		a.formTemp = *cfg.Temperature
	}
	if a.formTemp < 0 {
		a.formTemp = 0
	}
	if a.formTemp > 2 {
		a.formTemp = 2
	}
	a.formMax = float64(cfg.MaxTokens)
	a.showKey = false
	a.formError = ""
}

func (a *App) saveForm(asDefault bool) bool {
	a.form.Model = strings.TrimSpace(a.form.Model)
	a.form.BaseURL = strings.TrimSpace(a.form.BaseURL)
	a.form.APIKey = strings.TrimSpace(a.form.APIKey)
	a.form.ExtraBody = strings.TrimSpace(a.form.ExtraBody)
	a.form.MaxTokens = int(a.formMax)
	if a.form.MaxTokens < 0 {
		a.form.MaxTokens = 0
	}
	a.form.Reasoning = effortValue(a.formEffort)
	if a.form.Provider == "Custom" {
		if a.formStyle == "None" {
			a.form.ReasoningStyle = ""
		} else {
			a.form.ReasoningStyle = a.formStyle
		}
	}
	if a.form.Model == "" {
		a.formError = "Enter a model id."
		return false
	}
	if _, err := completionsURL(a.form.BaseURL); err != nil {
		a.formError = err.Error()
		return false
	}
	if err := mergeExtraBody(map[string]any{}, a.form.ExtraBody); err != nil {
		a.formError = err.Error()
		return false
	}
	if a.formTempOn {
		v := a.formTemp
		a.form.Temperature = &v
	} else {
		a.form.Temperature = nil
	}
	key := a.form.APIKey
	if err := a.bindConnection(&a.form, key); err != nil {
		a.formError = err.Error()
		return false
	}
	a.commitRoundsDraft()
	stored := a.form
	stored.APIKey = ""
	if asDefault {
		a.data.Defaults = stored
	} else if conv := a.active(); conv != nil {
		conv.Config = stored
		conv.MaxRounds = clampRounds(a.formRounds)
		a.touch(conv)
	}
	a.form.APIKey = key
	a.formError = ""
	a.save()
	return true
}

func (a *App) bindConnection(cfg *Config, key string) error {
	key = strings.TrimSpace(key)
	if cfg.ConnectionID == "" || a.connection(cfg.ConnectionID) == nil {
		cfg.ConnectionID = newID()
		a.data.Connections = append(a.data.Connections, Connection{
			ID: cfg.ConnectionID, Name: cfg.Provider, Provider: cfg.Provider, BaseURL: cfg.BaseURL,
		})
	} else {
		for i := range a.data.Connections {
			if a.data.Connections[i].ID == cfg.ConnectionID {
				a.data.Connections[i].Provider = cfg.Provider
				a.data.Connections[i].BaseURL = cfg.BaseURL
				if a.data.Connections[i].Name == "" {
					a.data.Connections[i].Name = cfg.Provider
				}
			}
		}
	}
	if a.keys == nil {
		return errors.New("No credential store is available.")
	}
	if key == "" {
		delete(a.assumedKeys, cfg.ConnectionID)
		return a.keys.Delete(cfg.ConnectionID)
	}
	if err := a.keys.Set(cfg.ConnectionID, key); err != nil {
		return err
	}
	if a.assumedKeys == nil {
		a.assumedKeys = map[string]bool{}
	}
	a.assumedKeys[cfg.ConnectionID] = true
	return nil
}

func (a *App) connection(id string) *Connection {
	for i := range a.data.Connections {
		if a.data.Connections[i].ID == id {
			return &a.data.Connections[i]
		}
	}
	return nil
}

func (a *App) keyStore() *keyStore {
	s, _ := a.keys.(*keyStore)
	return s
}

// resolvedKey is the secret for cfg. The first call reads the credential
// store. Every later call, including ones made while painting, is memory.
// The model menu and the provider list do not call this; they use the
// cache or the keys assumed from the saved provider list.
func (a *App) resolvedKey(cfg Config) string {
	if s := strings.TrimSpace(cfg.APIKey); s != "" {
		return s
	}
	id := cfg.ConnectionID
	if id == "" || a.keys == nil {
		return ""
	}
	s, err := a.keys.Get(id)
	if err != nil {
		return ""
	}
	if strings.TrimSpace(s) == "" {
		delete(a.assumedKeys, id)
		return ""
	}
	if a.assumedKeys == nil {
		a.assumedKeys = map[string]bool{}
	}
	a.assumedKeys[id] = true
	return s
}

// keyBlocked reports a remembered Keychain failure for id. It does not
// read the store.
func (a *App) keyBlocked(id string) bool {
	s := a.keyStore()
	return id != "" && s != nil && s.failure(id) != nil
}

// retryKey is the explicit retry. It reads that one account once more
// and, when the read works, fills the open settings fields.
func (a *App) retryKey(id string) {
	s := a.keyStore()
	if s == nil || id == "" {
		return
	}
	val, err := s.Retry(id)
	if err != nil {
		return
	}
	if strings.TrimSpace(val) == "" {
		delete(a.assumedKeys, id)
	} else {
		if a.assumedKeys == nil {
			a.assumedKeys = map[string]bool{}
		}
		a.assumedKeys[id] = true
	}
	if a.provEdit.connID == id {
		a.provEdit.apiKey = val
		a.provEdit.loadedKey = val
	}
	if a.form.ConnectionID == id {
		a.form.APIKey = val
	}
	if a.chatDraft.loaded && a.chatDraft.cfg.ConnectionID == id {
		a.chatDraft.cfg.APIKey = val
	}
	if a.defaultsDraft.loaded && a.defaultsDraft.cfg.ConnectionID == id {
		a.defaultsDraft.cfg.APIKey = val
	}
}

func keyAccessMessage() string {
	return "Couldn’t read the key from the Keychain."
}

func (a *App) migrateSecrets() {
	if a.keys == nil || a.saveBlocked {
		return
	}
	type sig struct{ provider, base, key string }
	ids := map[sig]string{}
	var failed error
	touch := func(cfg *Config) bool {
		key := strings.TrimSpace(cfg.APIKey)
		if key == "" {
			return false
		}
		s := sig{cfg.Provider, cfg.BaseURL, key}
		id, ok := ids[s]
		if !ok {
			id = newID()
			if err := a.keys.Set(id, key); err != nil {
				failed = err
				return false
			}
			ids[s] = id
			a.data.Connections = append(a.data.Connections, Connection{
				ID: id, Name: cfg.Provider, Provider: cfg.Provider, BaseURL: cfg.BaseURL,
			})
		}
		cfg.ConnectionID = id
		cfg.APIKey = ""
		return true
	}
	changed := touch(&a.data.Defaults)
	for i := range a.data.Conversations {
		if touch(&a.data.Conversations[i].Config) {
			changed = true
		}
	}
	if failed != nil {
		a.loadError = "Could not move an API key into the credential store. Saving is paused. " + failed.Error()
		a.saveBlocked = true
		return
	}
	if changed {
		a.save()
	}
}

func (a *App) isStreaming(id string) bool {
	st := a.streams[id]
	return st != nil && st.cancel != nil
}

func (a *App) canRegenerate() bool {
	conv := a.active()
	if conv == nil || a.isStreaming(conv.ID) {
		return false
	}
	for _, m := range conv.Messages {
		if m.Role == roleUser && strings.TrimSpace(m.Content) != "" {
			return true
		}
	}
	return false
}

func (a *App) regenerate() {
	if !a.canRegenerate() {
		return
	}
	conv := a.active()
	if len(conv.Messages) > 0 && conv.Messages[len(conv.Messages)-1].Role == roleAssistant {
		conv.Messages = conv.Messages[:len(conv.Messages)-1]
	}
	a.notice = ""
	a.startReply(conv)
	a.focusComposer()
}

// send posts text as the user and starts a reply. It is safe to call from
// the composer, including from OnTextKey between frames.
// sendNeedsProvider blocks a send that has nowhere to go: the chat's
// connection was removed and nothing replaced it, or there is no model
// and no provider at all. The draft stays in the composer.
func (a *App) sendNeedsProvider(conv *Conversation) bool {
	if conv == nil {
		return false
	}
	if id := strings.TrimSpace(conv.Config.ConnectionID); id != "" && a.connByID(id) == nil {
		return true
	}
	if len(a.data.Connections) == 0 && strings.TrimSpace(conv.Config.Model) == "" {
		return true
	}
	return false
}

func (a *App) send(text string) bool {
	text = strings.TrimSpace(text)
	conv := a.active()
	if conv == nil || (text == "" && len(conv.DraftQuotes) == 0) {
		return false
	}
	if a.isStreaming(conv.ID) {
		a.notice = "Stop the current reply before sending another."
		return false
	}
	if a.edit.convID == conv.ID {
		return false
	}
	if a.sendNeedsProvider(conv) {
		a.addProviderNote = conv.ID
		// A blocked send leaves the draft and any attached quotes in the
		// composer. The caret is not moved.
		return false
	}
	conv.SwitchNote = ""
	a.addProviderNote = ""
	if conv.TitleSource != titleSourceManual && !hasUserMessage(conv) {
		titleSrc := text
		if strings.TrimSpace(titleSrc) == "" && len(conv.DraftQuotes) > 0 {
			titleSrc = conv.DraftQuotes[0].Text
		}
		if title := temporaryTitle(titleSrc); title != newChatTitle {
			conv.Title = title
			conv.TitleSource = titleSourceTemp
		}
	}
	quotes := append([]Quote(nil), conv.DraftQuotes...)
	conv.DraftQuotes = nil
	a.clearDraft(conv)
	a.maybeAutoBreak(conv)
	conv.Messages = append(conv.Messages, Message{
		ID:        newID(),
		Role:      roleUser,
		Content:   text,
		Quotes:    quotes,
		Done:      true,
		CreatedAt: time.Now(),
	})
	a.notice = ""
	a.startReply(conv)
	a.focusComposer()
	return true
}

func (a *App) startReply(conv *Conversation) {
	cfg := conv.Config
	cfg.APIKey = a.resolvedKey(cfg)
	// The system prompt is added in the request body. Only the current
	// session — messages after the last break — is sent with it.
	history := append([]Message(nil), sessionMessages(conv)...)
	convID := conv.ID
	a.touch(conv)
	a.chat.ScrollToEnd()
	// touch sorts the sidebar in place, so the pointer passed in may now
	// be a different chat. The user turn is already on this id.
	conv = a.conversation(convID)
	if conv == nil {
		return
	}
	// The user turn is durable before the assistant placeholder exists and
	// before the request is sent. Streaming does not write that placeholder.
	a.saveSync()
	if beforeReplyHook != nil {
		beforeReplyHook()
	}
	msgID := newID()
	msg := Message{
		ID:        msgID,
		Role:      roleAssistant,
		CreatedAt: time.Now(),
	}
	// Stamp the provider and model this request actually uses. Regenerate
	// and edit-and-resend come through here, so the new reply records the
	// current selection and earlier replies keep the stamp they started with.
	a.stampReply(&msg, cfg)
	conv.Messages = append(conv.Messages, msg)

	body, bodyErr := chatRequestBody(cfg, history)
	if bodyErr == nil && a.data.KeepLastRequest {
		conv.LastRequest = prettyJSON(body)
	}
	fail := func(err error) {
		if m := a.findMessage(convID, msgID); m != nil {
			m.Done = true
			m.Error = explainAPIError(err)
		}
		a.noteProviderAuth(convID, err)
		a.saveSync()
		a.maybeAutoTitle(convID)
	}
	if bodyErr != nil {
		fail(bodyErr)
		return
	}
	if err := validateConfig(cfg); err != nil {
		fail(err)
		return
	}

	if a.streams == nil {
		a.streams = map[string]*convStream{}
	}
	st := a.streams[convID]
	if st == nil {
		st = &convStream{}
		a.streams[convID] = st
	}
	if st.cancel != nil {
		st.cancel()
	}
	st.gen++
	gen := st.gen
	ctx, cancel := context.WithCancel(context.Background())
	if a.data.TimeoutSec > 0 {
		var timeoutCancel context.CancelFunc
		ctx, timeoutCancel = context.WithTimeout(ctx, time.Duration(a.data.TimeoutSec)*time.Second)
		parent := cancel
		cancel = func() {
			timeoutCancel()
			parent()
		}
	}
	st.cancel = cancel

	a.launch(func() {
		var mu sync.Mutex
		var content, reasoning strings.Builder
		var dirty bool
		var tickStop chan struct{}
		var tickDone chan struct{}
		if a.win != nil {
			tickStop = make(chan struct{})
			tickDone = make(chan struct{})
			go func() {
				defer close(tickDone)
				t := time.NewTicker(40 * time.Millisecond)
				defer t.Stop()
				for {
					select {
					case <-tickStop:
						return
					case <-t.C:
						mu.Lock()
						if !dirty {
							mu.Unlock()
							continue
						}
						text, thought := content.String(), reasoning.String()
						dirty = false
						mu.Unlock()
						a.ui(func() {
							a.applyStream(convID, msgID, gen, text, thought)
							a.saveSoon()
						})
					}
				}
			}()
		}
		err := StreamChat(ctx, cfg, history, func(d Delta) {
			mu.Lock()
			if d.Content != "" {
				content.WriteString(d.Content)
			}
			if d.Reasoning != "" {
				reasoning.WriteString(d.Reasoning)
			}
			text, thought := content.String(), reasoning.String()
			dirty = true
			mu.Unlock()
			if a.win == nil {
				a.applyStream(convID, msgID, gen, text, thought)
			}
		})
		if tickStop != nil {
			close(tickStop)
			<-tickDone
		}
		if errors.Is(err, context.Canceled) {
			err = nil
		}
		mu.Lock()
		finalText, finalThought := content.String(), reasoning.String()
		mu.Unlock()
		a.ui(func() {
			a.finishReply(convID, msgID, gen, finalText, finalThought, err)
		})
	})
}

// stampReply records the provider and model used for this assistant turn.
// The display name and the kind are copied as plain text. Renaming or
// deleting the provider later does not change this message.
func (a *App) stampReply(m *Message, cfg Config) {
	if m == nil {
		return
	}
	m.Model = strings.TrimSpace(cfg.Model)
	m.ProviderID = strings.TrimSpace(cfg.ConnectionID)
	m.ProviderName = strings.TrimSpace(cfg.Provider)
	m.ProviderKind = ""
	m.ProviderCustom = false
	if c := a.connByID(m.ProviderID); c != nil {
		m.ProviderKind = c.Provider
		m.ProviderCustom = c.Custom
		if name := connDisplay(c); name != "" {
			m.ProviderName = name
		}
	}
}

// finishReply writes the reply that gen started. Stop bumps the generation
// by one and may run before the last batched deltas were posted, so that
// predecessor still flushes the text it already received. A newer reply
// has moved the generation further and is left alone.
func (a *App) finishReply(convID, msgID string, gen int, text, thought string, err error) {
	st := a.streams[convID]
	if st == nil || (st.gen != gen && st.gen != gen+1) {
		return
	}
	stopped := st.gen == gen+1
	if stopped {
		// One flush after Stop. A second callback for this reply no-ops.
		st.gen++
	}
	m := a.findMessage(convID, msgID)
	if m != nil {
		if text != "" {
			m.Content = text
		}
		if thought != "" {
			m.Reasoning = thought
		}
		if err != nil && !stopped {
			m.Error = explainAPIError(err)
			a.noteProviderAuth(convID, err)
		} else if !stopped && strings.TrimSpace(m.Content) == "" && strings.TrimSpace(m.Reasoning) == "" {
			m.Error = "The model returned an empty reply."
		}
		if stopped {
			m.Incomplete = true
		}
		m.Done = true
		if strings.TrimSpace(m.Reasoning) != "" {
			finishThinking(m)
		} else {
			m.ReasoningOpen = false
		}
	}
	if !stopped && st.cancel != nil {
		st.cancel()
		st.cancel = nil
	}
	if !stopped {
		if err != nil {
			a.announce = "The reply failed"
		} else {
			a.announce = "Reply ready"
		}
	}
	a.saveSync()
	a.maybeAutoTitle(convID)
}

// applyStream copies a batched delta onto the message when gen is still current.
func (a *App) applyStream(convID, msgID string, gen int, text, thought string) {
	st := a.streams[convID]
	if st == nil || st.gen != gen {
		return
	}
	m := a.findMessage(convID, msgID)
	if m == nil || m.Done {
		return
	}
	m.Content = text
	m.Reasoning = thought
	if thought != "" {
		if m.ThinkStarted.IsZero() {
			m.ThinkStarted = time.Now()
		}
		m.ReasoningOpen = true
	}
}

func (a *App) stop() {
	if conv := a.active(); conv != nil {
		a.stopChat(conv.ID)
	}
}

func (a *App) stopChat(id string) {
	st := a.streams[id]
	if st == nil || st.cancel == nil {
		return
	}
	st.cancel()
	st.cancel = nil
	// Bump the generation so a late callback does not append more text.
	st.gen++
	if conv := a.conversation(id); conv != nil {
		for i := len(conv.Messages) - 1; i >= 0; i-- {
			m := &conv.Messages[i]
			if m.Role == roleAssistant && !m.Done {
				m.Done = true
				m.Incomplete = true
				if strings.TrimSpace(m.Reasoning) != "" {
					finishThinking(m)
				}
				break
			}
		}
	}
	a.notice = ""
	a.saveSync()
	a.focusComposer()
}

// setEffort stores the composer's quick choice on the chat the menu was
// opened for. The choice is delivered in a later frame, which may already
// be showing another chat.
func (a *App) setEffort(label string) {
	if conv := a.active(); conv != nil {
		a.setEffortChat(conv.ID, label)
	}
}

func (a *App) setEffortChat(chatID, label string) {
	conv := a.conversation(chatID)
	if conv == nil {
		return
	}
	next := effortValue(label)
	if conv.Config.Reasoning == next {
		return
	}
	conv.Config.Reasoning = next
	a.touch(conv)
	a.save()
	// The menu frame is already running, so requestFrame is a no-op.
	// Invalidate posts a frame and, on darwin, wakes the run loop.
	a.invalidateWindows()
}

// useModel points the open chat at one model of one connection.
// The connection id is the provider. A shared base URL, or the same model
// id on another provider, does not retarget the choice. Sampling, the
// system prompt, and reasoning effort stay. Other chats and the app
// defaults stay.
func (a *App) useModel(connID, model string) {
	if conv := a.active(); conv != nil {
		a.useModelChat(conv.ID, connID, model)
	}
}

// useModelChat retargets one chat. Identity is the connection id plus the
// model id. The display name and the base URL are copied from that
// connection so the next request cannot keep the previous provider's
// endpoint or key. Other chats and the app defaults stay.
func (a *App) useModelChat(chatID, connID, model string) {
	model = strings.TrimSpace(model)
	conv := a.conversation(chatID)
	if conv == nil || model == "" {
		return
	}
	c := a.connByID(connID)
	if c == nil {
		return
	}
	if conv.Config.ConnectionID == c.ID && conv.Config.Model == model {
		return
	}
	cfg := conv.Config
	cfg.ConnectionID = c.ID
	cfg.Model = model
	cfg.APIKey = ""
	cfg.Provider = connDisplay(c)
	cfg.BaseURL = connEffective(c)
	conv.Config = cfg
	a.touch(conv)
	a.save()
	a.invalidateWindows()
}

// usePreset switches the open chat to a provider's usual model and URL.
// The key, sampling, system prompt, and reasoning effort stay.
func (a *App) usePreset(name string) {
	conv := a.active()
	if conv == nil || !knownProvider(name) {
		return
	}
	cfg := conv.Config
	cfg.Provider = name
	applyPreset(&cfg)
	conv.Config = cfg
	a.touch(conv)
	a.save()
}

// messageEdit is one user message open in place. The thread and the
// bottom composer stay as they are until the edit is submitted. draft is
// the composer text from before the edit, restored if anything wrote over it.
type messageEdit struct {
	convID string
	msgID  string
	draft  string
	text   string
	quotes []Quote
}

// editMessage opens an in-place editor. A second Edit is ignored until
// this one is cancelled or sent. Nothing is truncated and nothing is saved.
func (a *App) editMessage(convID, msgID string) {
	if a.edit.convID != "" {
		return
	}
	if a.isStreaming(convID) {
		a.notice = "Stop the current reply before editing."
		return
	}
	conv := a.conversation(convID)
	if conv == nil {
		return
	}
	idx := -1
	for i := range conv.Messages {
		if conv.Messages[i].ID == msgID && conv.Messages[i].Role == roleUser {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	a.edit = messageEdit{
		convID: convID, msgID: msgID, draft: conv.Draft, text: conv.Messages[idx].Content,
		quotes: append([]Quote(nil), conv.Messages[idx].Quotes...),
	}
	a.dismissQuoteBar()
	a.notice = ""
}

// cancelEdit closes the editor. The message and the composer draft are
// the ones from before Edit.
func (a *App) cancelEdit() {
	ed := a.edit
	if ed.convID == "" {
		return
	}
	a.edit = messageEdit{}
	conv := a.conversation(ed.convID)
	if conv == nil {
		return
	}
	conv.Draft = ed.draft
}

// laterCount is how many messages follow msgID.
func laterCount(conv *Conversation, msgID string) int {
	if conv == nil {
		return 0
	}
	for i := range conv.Messages {
		if conv.Messages[i].ID == msgID {
			return len(conv.Messages) - i - 1
		}
	}
	return 0
}

// submitEdit sends the in-place editor immediately. Messages after the
// edited one are dropped and a new reply starts. Nothing is asked first.
// An open delete or reset dialog is left alone.
func (a *App) submitEdit() {
	if a.edit.convID == "" || a.confirmKind != "" {
		return
	}
	text := strings.TrimSpace(a.edit.text)
	if text == "" && len(a.edit.quotes) == 0 {
		return
	}
	conv := a.conversation(a.edit.convID)
	if conv == nil {
		a.cancelEdit()
		return
	}
	if a.isStreaming(conv.ID) {
		a.notice = "Stop the current reply before editing."
		return
	}
	a.applyEdit()
}

// applyEdit writes the editor text onto the user message, drops everything
// after it, and starts a new reply.
func (a *App) applyEdit() {
	conv := a.conversation(a.edit.convID)
	if conv == nil {
		a.edit = messageEdit{}
		a.confirmKind = ""
		return
	}
	text := strings.TrimSpace(a.edit.text)
	msgID := a.edit.msgID
	quotes := append([]Quote(nil), a.edit.quotes...)
	a.edit = messageEdit{}
	a.confirmKind = ""
	if text == "" && len(quotes) == 0 {
		return
	}
	for i := range conv.Messages {
		if conv.Messages[i].ID == msgID && conv.Messages[i].Role == roleUser {
			conv.Messages[i].Content = text
			conv.Messages[i].Quotes = quotes
			conv.Messages = append([]Message(nil), conv.Messages[:i+1]...)
			dropBreaksAfter(conv, i)
			break
		}
	}
	a.notice = ""
	a.startReply(conv)
	a.focusComposer()
}

// finishThinking closes the thinking disclosure and records how long it ran.
func finishThinking(m *Message) {
	if m.ThinkingMS == 0 {
		ms := 1
		if !m.ThinkStarted.IsZero() {
			d := int(time.Since(m.ThinkStarted).Milliseconds())
			if d > ms {
				ms = d
			}
		}
		m.ThinkingMS = ms
	}
	m.ReasoningOpen = false
}

// clearDraft drops the composer text after a send that actually started.
// The text view keeps its key. MyGo adopts the empty string on the next
// build (textinput.go, when the bound value differs and no native edit
// is pending), so the caret stays in that same view. Bumping composerGen
// used to mint a new text view; runtime.go then dropped focus because the
// focused element was no longer in the frame. A blocked send does not
// call this: the draft and any attached quotes stay, and focus is unchanged.
func (a *App) clearDraft(conv *Conversation) {
	conv.Draft = ""
}

func (a *App) launch(fn func()) {
	if a.win == nil {
		fn()
		return
	}
	go fn()
}

func (a *App) ui(fn func()) {
	if a.win != nil {
		a.win.Update(fn)
		return
	}
	fn()
}

func (a *App) refresh() {
	a.invalidateWindows()
}

// invalidateWindows asks every live window for a frame. setTheme writes
// the store from inside the settings frame, and requestFrame is a no-op
// while that frame is already running, so the main window would otherwise
// keep the old picture until something else rebuilt it. A pointer move
// does not: the background does not track the pointer. A cached bitmap
// also never calls wakeGradient. Invalidate posts to the main thread and,
// on darwin, signals the run loop, so the new theme is presented with no
// further input.
func (a *App) invalidateWindows() {
	if a.win != nil {
		a.win.Invalidate()
	}
	if a.prefs != nil {
		a.prefs.Invalidate()
	}
	noteThemeInvalidate()
}
