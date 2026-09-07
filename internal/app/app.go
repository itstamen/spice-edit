// =============================================================================
// File: internal/app/app.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Package app is the editor's top-level glue: it owns the tcell screen,
// the file tree, the open tabs, and the event loop. The drawing is split
// into four panels (sidebar / tab bar / editor body / status bar) and the
// mouse dispatcher routes presses, drags, and wheel events to whichever
// panel the cursor is over.
//
// The editor is mouse-first by design — there are no Ctrl-keyed shortcuts
// because they collide with terminal flow control (Ctrl-S/Q) and tmux/zellij
// prefixes. Instead, every action lives behind a click on the ≡ icon at
// the top-left of the tab bar, which opens a centered modal of actions.
package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/clipboard"
	"github.com/cloudmanic/spice-edit/internal/commandpaste"
	"github.com/cloudmanic/spice-edit/internal/customactions"
	"github.com/cloudmanic/spice-edit/internal/editor"
	"github.com/cloudmanic/spice-edit/internal/filetree"
	"github.com/cloudmanic/spice-edit/internal/finder"
	"github.com/cloudmanic/spice-edit/internal/icons"
	"github.com/cloudmanic/spice-edit/internal/spiceconfig"
	"github.com/cloudmanic/spice-edit/internal/termgraphics"
	"github.com/cloudmanic/spice-edit/internal/termtext"
	"github.com/cloudmanic/spice-edit/internal/theme"
	"github.com/cloudmanic/spice-edit/internal/version"
)

// Layout, behavior, and feel constants. Constants instead of config —
// the editor is opinionated by design.
const (
	defaultSidebarWidth = 30
	minSidebarWidth     = 18
	minEditorAfterDrag  = 40
	minWidth            = 50
	minHeight           = 24
	statusFlashFor      = 3 * time.Second
	doubleClickMs       = 500 * time.Millisecond
	doubleEscMs         = 500 * time.Millisecond
	wheelLines          = 3
	wheelCols           = 6 // horizontal step per WheelLeft/WheelRight event

	// modifierStickyWindow is how long a previously-seen Shift modifier
	// state is allowed to persist forward onto the next wheel event.
	// Some terminals (Zellij + macOS Terminal among them) emit the
	// Shift state as a separate ButtonNone+Shift event right before
	// firing the WheelUp/WheelDown without the modifier — so without
	// this carry-forward, shift+wheel reads as plain wheel. 250ms is
	// long enough to bridge the gap and short enough that releasing
	// Shift before scrolling reliably reverts to vertical scroll.
	modifierStickyWindow = 250 * time.Millisecond

	// treeRefreshInterval is how often the background goroutine kicks off
	// a file-tree reload so the sidebar stays in sync with on-disk changes
	// made by other tools (git, mv, another tmux pane, etc.). 10s feels
	// "fresh enough" while costing only a handful of ReadDir syscalls.
	treeRefreshInterval = 10 * time.Second

	// menuButtonWidth is how many cells the ≡ icon occupies at the top-left
	// of the tab bar. Tabs render starting just after it.
	menuButtonWidth = 4

	// modalWidth is the action modal's column count. Sized to comfortably
	// fit the longest dynamic label — "Rename folder (subdir/)" with a
	// folder name up to maxLabelSuffix runes — plus the leading "▸ "
	// chevron and one cell of right padding. Very long custom-action
	// labels will still clip but won't break layout. Height is computed
	// dynamically from the visible groups — see menuLayout.
	modalWidth = 48

	// maxLabelSuffix is the rune budget that newFileLabel /
	// renameFolderLabel / deleteFolderLabel use when truncating their
	// "(in subdir/)" / "(subdir/)" suffix. Pinned alongside modalWidth
	// so the two stay in lockstep — bumping the modal without bumping
	// the suffix budget leaves dead space, and shrinking the modal
	// without shrinking the suffix budget reintroduces the overflow
	// bug where folder names bled into the editor underneath.
	maxLabelSuffix = 30

	// autoScrollTick is how often the auto-scroll goroutine emits a tick
	// while the user is drag-selecting with the cursor parked outside the
	// editor's vertical edges. ~16 ticks/sec feels responsive without
	// overshooting on small files.
	autoScrollTick = 60 * time.Millisecond
)

// osEnv adapts the process environment to termgraphics.Environment.
type osEnv struct{}

// Getenv returns one process environment value.
func (osEnv) Getenv(key string) string { return os.Getenv(key) }

// autoScrollEvent is the custom tcell event our auto-scroll goroutine
// posts at autoScrollTick intervals while the user is drag-selecting past
// the top or bottom edge of the editor pane.
type autoScrollEvent struct {
	when time.Time
}

// When satisfies the tcell.Event interface.
func (e *autoScrollEvent) When() time.Time { return e.when }

// treeRefreshEvent is the custom tcell event the background tree-refresh
// goroutine posts every treeRefreshInterval. The main loop reacts by
// asking the file tree to re-read every loaded directory.
type treeRefreshEvent struct {
	when time.Time
}

// When satisfies the tcell.Event interface.
func (e *treeRefreshEvent) When() time.Time { return e.when }

// customActionDoneEvent is posted by runCustomAction when its background
// shell-out finishes. Carries the label and any error so the main loop
// can flash a sensible status message — running scp / ssh inline would
// freeze the UI for the duration of the network round-trip.
type customActionDoneEvent struct {
	when   time.Time
	label  string
	err    error
	output []byte // combined stdout+stderr from the action's shell run
}

// When satisfies the tcell.Event interface.
func (e *customActionDoneEvent) When() time.Time { return e.when }

// tabRect remembers where each tab was drawn so click handling can hit-test
// against the actual rendered geometry rather than re-deriving it.
type tabRect struct {
	Index    int
	X, Width int
	CloseX   int // Cell column of the × close button.
}

// imageViewKey identifies one fitted graphics image view. A resized terminal
// produces a distinct key so a payload fallback can be retried at the new
// target size.
type imageViewKey struct {
	ID          uint64
	Revision    uint64
	PixelWidth  int
	PixelHeight int
}

// clickRecord tracks the last mouse-press location and time so we can
// detect double-clicks (and select the word under the cursor).
type clickRecord struct {
	x, y int
	when time.Time
}

// menuItemDef describes one row in the action modal: the label shown to
// the user, the y-offset it lives at inside the modal, the action it runs
// when clicked, and a predicate that returns true when the action is
// applicable in the current context (so we can dim non-applicable rows).
//
// labelFor is an optional dynamic-label hook: when non-nil, drawMenu calls
// it instead of using the static label string. Used by toggle-style rows
// whose label depends on app state ("Show / Hide file explorer").
type menuItemDef struct {
	label    string
	relY     int
	shortcut string
	action   func(*App)
	enabled  func(*App) bool
	labelFor func(*App) string
	// visible, when non-nil, decides whether the item appears in the
	// menu at all (returning false drops the row entirely — not the
	// same as enabled, which renders the row greyed out). Used to
	// hide the sidebar toggle in single-file mode, where there's no
	// tree to show or hide.
	visible func(*App) bool
}

// builtinMenuGroups returns the editor's built-in action groups in
// display order. Custom actions loaded from
// ~/.config/spiceedit/actions.json get prepended as their own group
// in menuLayout — they're not included here so toggling them on or
// off doesn't require touching this table.
//
// Each group is rendered as a contiguous block; menuLayout interleaves
// dividers between groups and recomputes every relY. The relY field is
// left zero here on purpose — it gets stamped at layout time.
func builtinMenuGroups() [][]menuItemDef {
	return [][]menuItemDef{
		// Tab actions
		{
			{label: "Save", shortcut: "Esc s", action: (*App).menuSave, enabled: (*App).hasSavableTab},
			{label: "Save & close tab", action: (*App).menuSaveAndClose, enabled: (*App).hasSavableTab},
			{label: "Close tab", shortcut: "Esc w", action: (*App).menuClose, enabled: (*App).hasTab},
			{label: "Close other tabs", action: (*App).menuCloseOtherTabs, enabled: (*App).hasOtherTabs},
			{label: "Close all tabs", action: (*App).menuCloseAllTabs, enabled: (*App).hasTab},
		},
		// History
		{
			{label: "Undo", shortcut: "Esc u", action: (*App).menuUndo, enabled: (*App).hasUndo},
			{label: "Redo", shortcut: "Esc r", action: (*App).menuRedo, enabled: (*App).hasRedo},
			{label: "Revert file", action: (*App).menuRevert, enabled: (*App).hasRevert},
		},
		// Search
		{
			{label: "Find in file", shortcut: "Esc f", action: (*App).menuFind, enabled: (*App).hasFindable},
			{label: "Find file in project", shortcut: "Esc p", action: (*App).menuFindFile, enabled: (*App).hasFinder},
		},
		// File actions
		{
			{shortcut: "Esc n", action: (*App).menuNewFile, enabled: alwaysTrue, labelFor: (*App).newFileLabel},
			{label: "New folder", action: (*App).menuNewFolder, enabled: alwaysTrue},
			{label: "Open with default app", shortcut: "Esc o", action: (*App).menuOpenWithDefaultApp, enabled: (*App).hasFileActionTarget},
			{label: "Show in file manager", shortcut: "Esc e", action: (*App).menuShowInFileManager, enabled: alwaysTrue},
			{label: "Rename file", action: (*App).menuRename, enabled: (*App).hasFileActionTarget},
			{label: "Delete file", action: (*App).menuDelete, enabled: (*App).hasFileActionTarget},
			{action: (*App).menuRenameFolder, enabled: (*App).hasActiveSubfolder, labelFor: (*App).renameFolderLabel},
			{action: (*App).menuDeleteFolder, enabled: (*App).hasActiveSubfolder, labelFor: (*App).deleteFolderLabel},
			{label: "Copy relative path", action: (*App).menuCopyRelativePath, enabled: (*App).hasPathActionTarget},
			{label: "Copy absolute path", action: (*App).menuCopyAbsolutePath, enabled: (*App).hasPathActionTarget},
		},
		// Clipboard
		{
			{label: "Copy selection", shortcut: "Esc c", action: (*App).menuCopy, enabled: (*App).hasSelection},
			{label: "Cut selection", shortcut: "Esc x", action: (*App).menuCut, enabled: (*App).hasSelection},
			{label: "Paste", shortcut: "Esc v", action: (*App).menuPaste, enabled: (*App).hasPasteTarget},
			{label: "Select all", shortcut: "Esc a", action: (*App).menuSelectAll, enabled: (*App).hasEditableTextTab},
			{label: "Delete current line", shortcut: "Esc d", action: (*App).menuDeleteCurrentLine, enabled: (*App).hasEditableTextTab},
			{label: "Toggle line comment", shortcut: "Esc /", action: (*App).menuToggleLineComment, enabled: (*App).hasCommentableTab},
		},
		// View toggle
		{
			{shortcut: "Esc z", action: (*App).menuToggleSoftWrap, enabled: alwaysTrue, labelFor: (*App).softWrapToggleLabel},
			{shortcut: "Esc t", action: (*App).menuToggleSidebar, enabled: alwaysTrue, labelFor: (*App).sidebarToggleLabel, visible: (*App).hasTree},
		},
		// Quit
		{
			{label: "Quit editor", shortcut: "Esc q", action: (*App).menuQuit, enabled: alwaysTrue},
		},
	}
}

// alwaysTrue is the default predicate for actions that are always applicable
// (currently just Quit — which has no preconditions).
func alwaysTrue(*App) bool { return true }

// menuLayout flattens the visible menu groups into a single ordered
// slice of items with relY positions assigned, plus the divider rows
// and the modal's total cell height. Custom actions (when configured)
// get spliced in as their own group right before the Quit row, so
// they sit at the bottom of the menu where the user reaches for
// "what do I do with this file" actions. Recomputed on every call —
// cheap, and lets the layout react when actions.json is reloaded
// mid-session.
func (a *App) menuLayout() (items []menuItemDef, dividers []int, modalHeight int) {
	groups := append([][]menuItemDef{}, builtinMenuGroups()...)
	if len(a.customActions) > 0 {
		ca := make([]menuItemDef, 0, len(a.customActions))
		for i := range a.customActions {
			i := i // capture
			// Custom actions are user-defined shell — we don't try to
			// guess from the command string whether it needs $FILE.
			// "Upgrade SpiceEdit" obviously doesn't; "Open on
			// computer" obviously does. Both should be runnable from
			// the menu; if a $FILE-dependent command is invoked with
			// no tab open it'll fail with a real error and our info
			// modal surfaces it. Better that than getting the
			// heuristic wrong half the time.
			ca = append(ca, menuItemDef{
				label:   a.customActions[i].Label,
				action:  func(app *App) { app.runCustomAction(i) },
				enabled: alwaysTrue,
			})
		}
		// Splice in just before the final group (Quit). builtinMenuGroups
		// guarantees Quit is last; if anyone reorders that, the test
		// pinning custom-actions placement catches it.
		quit := groups[len(groups)-1]
		groups = append(groups[:len(groups)-1], ca, quit)
	}

	// Drop items whose visibility predicate (if any) says they don't
	// belong here right now — e.g. single-file mode hides the sidebar
	// toggle because there's no tree to toggle. A group emptied by
	// filtering vanishes too, so we don't leave a hanging divider
	// between two surviving groups.
	visibleGroups := make([][]menuItemDef, 0, len(groups))
	for _, g := range groups {
		kept := make([]menuItemDef, 0, len(g))
		for _, it := range g {
			if it.visible != nil && !it.visible(a) {
				continue
			}
			kept = append(kept, it)
		}
		if len(kept) > 0 {
			visibleGroups = append(visibleGroups, kept)
		}
	}
	groups = visibleGroups

	// Title at relY 1, divider under it at relY 2, first item at relY 3.
	dividers = []int{2}
	y := 3
	for gi, g := range groups {
		for _, it := range g {
			it.relY = y
			items = append(items, it)
			y++
		}
		if gi < len(groups)-1 {
			dividers = append(dividers, y)
			y++
		}
	}
	// y now points at the bottom border row; height is one beyond.
	modalHeight = y + 1
	return items, dividers, modalHeight
}

// hasTree is the menu visibility predicate for tree-dependent rows.
// True when the file tree was built at startup; false in single-file
// mode, where we deliberately skipped tree construction to avoid
// indexing the working directory.
func (a *App) hasTree() bool {
	return a.tree != nil
}

// App is the editor's top-level state holder and event-loop owner.
type App struct {
	screen tcell.Screen
	theme  theme.Theme

	// graphics owns the terminal-specific image layer. It is deliberately
	// separate from tcell's cell buffer, which remains responsible for the
	// universal half-block fallback.
	graphics             termgraphics.Presenter
	showFrame            func()
	eventNow             func() time.Time
	graphicsDisabled     bool
	graphicsFallback     map[imageViewKey]struct{}
	graphicsHinted       bool
	payloadHints         map[imageViewKey]struct{}
	graphicsVisible      bool
	graphicsView         imageViewKey
	hasGraphicsView      bool
	graphicsPlacement    termgraphics.Placement
	hasGraphicsPlacement bool
	graphicsRect         termgraphics.CellRect
	graphicsCells        termgraphics.CellPixels
	hasGraphicsGeometry  bool
	graphicsUsesTTY      bool
	graphicsTTY          tcell.Tty

	rootDir string
	// rootAnchor is the canonical project root used as the trusted starting
	// point for descriptor-relative folder creation. Descendants are still
	// traversed without following symlinks.
	rootAnchor string
	tree       *filetree.Tree
	tabs       []*editor.Tab
	activeTab  int

	// activeFolder is the directory the editor is currently "working
	// in" — the default target for New File from the main menu. It
	// updates whenever the user clicks a folder in the tree, opens a
	// file (parent dir wins), or right-clicks a folder. See
	// setActiveFolder for the single write path so the file tree's
	// matching highlight stays in sync.
	activeFolder string

	width, height int

	// sidebarShown controls whether the file explorer panel is visible.
	// When false the editor and tab bar fill the whole window.
	sidebarShown bool

	// softWrap controls whether text tabs wrap long logical lines to the editor
	// width. It is a session-level view preference, deliberately not persisted.
	softWrap bool

	// sidebarWidth is the live width of the file-explorer block (file tree
	// + 1-cell splitter on its right edge), in screen cells. The user can
	// drag the splitter to change it within [minSidebarWidth, width-minEditorAfterDrag].
	sidebarWidth int

	// sidebarPath* owns the path information panel below the file tree. The
	// interaction fields are kept here so the drawing and future input routing
	// share one compact, rune-indexed state bundle.
	spiceConfigPath          string
	sidebarPathCollapsed     bool
	sidebarPathType          sidebarPathType
	sidebarPathPrefix        []rune
	sidebarPathPrefixCursor  int
	sidebarPathPrefixScroll  int
	sidebarPathPrefixFocused bool
	sidebarPathLastCopied    string
	sidebarPathHasCopied     bool

	clipBuf            string
	copyToSystem       func(string) error
	readFromSystem     func() (string, error)
	commandPasteRecent func(time.Time) bool
	closeCommandPaste  func()
	pasting            bool
	pasteAuthorized    bool
	pasteBuf           strings.Builder
	statusMsg          string
	statusUntil        time.Time
	openPath           pathOpener
	revealPath         pathRevealer
	dragMode           string // "editor", "sidebar", "tree", or "path" while a mouse press is held.
	lastClick          clickRecord
	lastTabRects       []tabRect

	// lastShiftAt is the wall-clock time we last saw any mouse event
	// carrying the Shift modifier. Some terminals (notably Zellij over
	// macOS Terminal) report modifier state in a separate ButtonNone
	// event right before the wheel event, instead of folding the
	// modifier into the wheel event itself. We treat a wheel event as
	// shifted when one of those modifier-state events arrived within
	// modifierStickyWindow. See handleMouse.
	lastShiftAt time.Time

	menuOpen       bool
	hoveredMenuRow int       // index into menuItems of the row under the mouse, or -1.
	lastEscape     time.Time // timestamp of the previous Esc press, for double-tap detection.

	// Prompt modal — single-line text input with OK / Cancel. Used by
	// Rename and New File. See modals.go for render + event handling.
	promptOpen     bool
	promptTitle    string
	promptHint     string
	promptValue    []rune
	promptCursor   int
	promptScroll   int
	promptCallback func(*App, string)

	// Confirm modal — Yes / No, used by Delete. confirmHover is 0 for No
	// (the safe default) or 1 for Yes.
	confirmOpen     bool
	confirmTitle    string
	confirmMessage  string
	confirmHover    int
	confirmCallback func(*App)

	// confirmInfo flips the confirm modal into a single-button "OK"
	// flavour used for reporting things back to the user — like the
	// full stderr from a failed custom action — that don't need a
	// Yes/No decision. confirmMessageLines, when non-nil, supersedes
	// confirmMessage so the renderer can draw a multi-line body for
	// scp / ssh diagnostics that naturally wrap.
	confirmInfo         bool
	confirmMessageLines []string
	confirmInfoScroll   int

	// Save/Discard/Cancel modal — used when closing a dirty tab or
	// quitting with unsaved changes. dirtyHover indexes the button row:
	// 0 = Cancel (safe default for an accidental Enter), 1 = Discard,
	// 2 = Save. Save and Discard run the corresponding callbacks; Cancel
	// just dismisses.
	dirtyOpen            bool
	dirtyTitle           string
	dirtyMessage         string
	dirtyHover           int
	dirtySaveCallback    func(*App)
	dirtyDiscardCallback func(*App)

	// Form modal — multi-field input collected before a custom action's
	// shell command runs. See formmodal.go for layout, focus traversal,
	// and the env-var injection that turns submitted values into KEY=
	// pairs the spawned shell can read. Mutually exclusive with every
	// other modal, like prompt/confirm.
	formOpen     bool
	formTitle    string
	formPrompts  []customactions.Prompt
	formValues   map[string]string // canonical store keyed by Prompt.Key
	formText     [][]rune          // per-prompt rune slice (text rows only; nil for selects)
	formCursor   []int             // caret position into formText[i]; index for selects
	formScroll   []int             // horizontal scroll offset for text rows
	formFocus    int               // which prompt row owns the keyboard
	formCallback func(*App, map[string]string)

	// Right-click context menu over the file tree or an editor tab.
	contextOpen  bool
	contextX     int
	contextY     int
	contextItems []contextItem
	contextHover int

	// Find bar — opened with Esc-f or the "Find in file" menu entry. The
	// bar is a 1-row strip pinned above the status bar; while it's open
	// it owns the keyboard. The active tab carries the query and match
	// list (see editor.Tab.SetFindQuery), so each tab remembers its own
	// search across closes / reopens.
	findOpen   bool
	findValue  []rune
	findCursor int
	findScroll int

	// Auto-scroll while drag-selecting past the editor's top/bottom edge.
	// lastDragX/Y is the most recent mouse position so the auto-scroll
	// tick can extend the selection at the user's column even though the
	// mouse hasn't moved.
	autoScrollStop chan struct{}
	autoScrollDir  int // -1 up, 0 idle, +1 down
	lastDragX      int
	lastDragY      int

	// treeRefreshStop signals the background tree-refresh goroutine to exit.
	treeRefreshStop chan struct{}

	// gitBranch is the current branch name for the project root (or a
	// short commit SHA when HEAD is detached). Empty when the root isn't
	// a git repo. Updated on the same 10-second tick as refreshGitStatus.
	gitBranch string

	// customActions is the list of user-configured shell-out actions
	// loaded from ~/.config/spiceedit/actions.json at startup. When
	// non-empty they prepend a new group to the action menu — see
	// menuLayout. nil / empty when the user hasn't configured any.
	customActions []customactions.Action

	// finder + finder modal state — project-wide file search ("Esc p"
	// or ≡ → Find file). The Finder owns the cached index and a
	// background-build goroutine; the rest of these fields are
	// transient UI state for the modal itself.
	finder         *finder.Finder
	finderOpen     bool
	finderQuery    []rune
	finderCursor   int
	finderScroll   int
	finderSelected int
	finderResults  []finder.Result

	// confirmCancelHook runs when the active confirm modal is dismissed
	// without a Yes — i.e. the user picked No, hit Esc, or clicked
	// outside. Set after openConfirm by flows that want to react to the
	// negative answer (today: format-trust deny, format-install
	// decline). closeAllModals clears it so a stale hook can't fire on
	// an unrelated future modal.
	confirmCancelHook func(*App)

	quit bool
}

// New initialises the screen and mouse, builds the file tree at rootDir,
// and returns an App ready to Run.
func New(rootDir string) (*App, error) {
	scr, err := tcell.NewScreen()
	if err != nil {
		return nil, err
	}
	if err := scr.Init(); err != nil {
		return nil, err
	}
	scr.EnableMouse(tcell.MouseButtonEvents | tcell.MouseDragEvents | tcell.MouseMotionEvents)
	scr.EnablePaste()

	th := theme.Default()
	scr.SetStyle(tcell.StyleDefault.Background(th.BG).Foreground(th.Text))
	scr.Clear()

	tree, err := filetree.New(rootDir)
	if err != nil {
		scr.Fini()
		return nil, err
	}
	rootAnchor, err := filepath.EvalSymlinks(tree.Root.Path)
	if err != nil {
		scr.Fini()
		return nil, err
	}

	a := &App{
		screen:          scr,
		copyToSystem:    clipboard.CopyToSystem,
		readFromSystem:  clipboard.ReadSystem,
		theme:           th,
		rootDir:         rootDir,
		rootAnchor:      rootAnchor,
		tree:            tree,
		hoveredMenuRow:  -1,
		sidebarShown:    true,
		sidebarWidth:    defaultSidebarWidth,
		spiceConfigPath: spiceconfig.DefaultPath(),
	}
	a.showFrame = a.screen.Show
	a.initializeImageGraphics()
	a.setActiveFolder(tree.Root.Path)
	a.loadSpiceConfig()
	a.refreshGitStatus()
	a.loadCustomActions()
	a.flash("Welcome — click a file to open · click  ≡  for the menu")
	a.initializeCommandPaste(commandpaste.Start)
	a.startTreeRefresh()
	// Kick off the project file index in the background so that by
	// the time the user hits Esc-p (or ≡ → Find file) the modal can
	// open with results already in hand. On a 50k-file repo this
	// takes ~150ms; the user pays it once at startup instead of
	// when they're trying to navigate.
	a.finder = finder.New(rootDir)
	scr2 := a.screen
	a.finder.Rebuild(func() {
		_ = scr2.PostEvent(&finderRebuiltEvent{when: time.Now()})
	})
	return a, nil
}

// NewSingleFile is the lean alternative to New for the "spiceedit
// somefile.md" invocation: no file tree, no project finder index,
// no background tree-refresh goroutine, sidebar hidden. The user
// asked for one file — we don't pay the cost of walking and watching
// the surrounding directory tree just to render a file they wanted
// to look at in isolation. The tree-toggle row in the action menu
// is filtered out via the hasTree visibility predicate so the user
// can't accidentally try to show a sidebar that doesn't exist.
//
// rootDir is still recorded (set to the file's parent) so file-level
// actions that need a base directory — Save As, New File, the
// relative/absolute path helpers — have somewhere to anchor.
func NewSingleFile(filePath string) (*App, error) {
	scr, err := tcell.NewScreen()
	if err != nil {
		return nil, err
	}
	if err := scr.Init(); err != nil {
		return nil, err
	}
	scr.EnableMouse(tcell.MouseButtonEvents | tcell.MouseDragEvents | tcell.MouseMotionEvents)
	scr.EnablePaste()

	th := theme.Default()
	scr.SetStyle(tcell.StyleDefault.Background(th.BG).Foreground(th.Text))
	scr.Clear()

	rootDir := filepath.Dir(filePath)
	if rootDir == "" {
		rootDir = "."
	}
	rootAnchor, err := filepath.EvalSymlinks(rootDir)
	if err != nil {
		scr.Fini()
		return nil, err
	}

	a := &App{
		screen:          scr,
		copyToSystem:    clipboard.CopyToSystem,
		readFromSystem:  clipboard.ReadSystem,
		theme:           th,
		rootDir:         rootDir,
		rootAnchor:      rootAnchor,
		tree:            nil,
		hoveredMenuRow:  -1,
		sidebarShown:    false,
		sidebarWidth:    defaultSidebarWidth,
		spiceConfigPath: spiceconfig.DefaultPath(),
	}
	a.showFrame = a.screen.Show
	a.initializeImageGraphics()
	a.setActiveFolder(rootDir)
	a.loadSpiceConfig()
	a.loadCustomActions()
	// openFile loads the file's git gutter markers itself (a file-scoped
	// `git diff`), so single-file mode shows change bars on open without
	// the whole-repo status or tree walk that New performs.
	a.openFile(filePath)
	a.initializeCommandPaste(commandpaste.Start)
	return a, nil
}

// initializeCommandPaste starts the supplied Command+V monitor and installs
// no-op hooks when terminal paste cannot be authorized. Production
// constructors supply commandpaste.Start; tests can inject startup failures.
func (a *App) initializeCommandPaste(start func() (commandpaste.Monitor, error)) {
	monitor, err := start()
	if err != nil {
		a.commandPasteRecent = func(time.Time) bool { return false }
		a.closeCommandPaste = func() {}
		if errors.Is(err, commandpaste.ErrPermissionDenied) {
			a.flash("Command+V paste requires Input Monitoring permission")
		} else {
			a.flash("Command+V paste unavailable")
		}
		return
	}

	a.commandPasteRecent = monitor.Recent
	a.closeCommandPaste = monitor.Close
}

// loadCustomActions reads the user's actions.json (if any) and stores
// the parsed list on the App. Failures are surfaced as a status flash
// so a typo in the config file isn't silently swallowed, but they
// don't block startup — the editor still opens with no custom actions
// in the menu.
func (a *App) loadCustomActions() {
	path := customactions.DefaultPath()
	actions, err := customactions.Load(path)
	if err != nil {
		a.flash("custom actions: " + err.Error())
		return
	}
	a.customActions = actions
}

// loadSpiceConfig reads ~/.config/spiceedit/config.json (if any),
// resolves the Nerd Fonts auto/on/off mode to a concrete bool via
// icons.Resolve, and stamps the result onto the file tree so the
// next render starts drawing glyphs (or doesn't). A malformed
// config flashes a status message but never blocks startup — the
// editor falls back to Defaults() and keeps going.
func (a *App) loadSpiceConfig() {
	cfg, err := spiceconfig.Load(a.spiceConfigPath)
	a.sidebarPathCollapsed = cfg.SidebarPathCollapsed
	if err != nil {
		a.flash("config: " + err.Error())
	}
	if a.tree != nil {
		a.tree.IconsEnabled = icons.Resolve(cfg.Icons)
	}
}

// refreshTree calls tree.Refresh when the file tree exists, and is a
// no-op in single-file mode. File operations (create / rename / delete)
// call this after touching the disk so callers don't have to nil-check
// every site. The git-status and finder refreshes that usually
// accompany it already guard themselves internally.
func (a *App) refreshTree() {
	if a.tree == nil {
		return
	}
	a.tree.Refresh()
}

// refreshGitStatus re-runs `git status --porcelain` against the project
// root and stamps the resulting dirty-paths sets onto the file tree, so
// changed files render in the Modified color on the next draw. It's
// cheap (a couple of forks) but not free — we only call it from the
// 10-second tree-refresh tick and right after our own file operations,
// not on every keystroke. A non-git project leaves the tree's dirty
// maps empty, which the renderer treats as "everything clean".
func (a *App) refreshGitStatus() {
	if a.tree == nil {
		// Single-file mode has no tree to stamp dirty-path sets onto,
		// but the open tab can still show per-line gutter markers —
		// those come from a single `git diff` on the file itself and
		// don't need the (deliberately skipped) directory walk. Skip
		// the whole-repo `git status` and just refresh the gutter.
		a.refreshGitLineChanges()
		return
	}
	st := loadGitStatus(a.rootDir)
	if !st.IsRepo {
		a.tree.DirtyFiles = nil
		a.tree.DirtyFolders = nil
		a.gitBranch = ""
		a.refreshGitLineChanges()
		return
	}
	dirtyFiles := rebaseGitPaths(st.DirtyFiles, a.tree.Root.Path)
	a.tree.DirtyFiles = dirtyFiles
	a.tree.DirtyFolders = dirtyFolderSet(dirtyFiles, a.tree.Root.Path)
	a.gitBranch = st.Branch
	a.refreshGitLineChanges()
}

// refreshGitLineChanges refreshes gutter markers for every open text tab.
func (a *App) refreshGitLineChanges() {
	for _, tab := range a.tabs {
		if tab == nil || tab.Path == "" || tab.IsReadOnly() {
			continue
		}
		tab.GitLines = loadGitLineChanges(a.rootDir, tab.Path)
	}
}

// startTreeRefresh launches a goroutine that posts a treeRefreshEvent every
// treeRefreshInterval. The main event loop reacts by calling tree.Refresh,
// which keeps the sidebar in sync with on-disk changes from outside the
// editor (git, mv, another tmux pane, etc.).
func (a *App) startTreeRefresh() {
	a.treeRefreshStop = make(chan struct{})
	stop := a.treeRefreshStop
	scr := a.screen
	go func() {
		ticker := time.NewTicker(treeRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case t := <-ticker.C:
				_ = scr.PostEvent(&treeRefreshEvent{when: t})
			}
		}
	}()
}

// stopTreeRefresh signals the background tree-refresh goroutine to exit.
// Safe to call multiple times.
func (a *App) stopTreeRefresh() {
	if a.treeRefreshStop != nil {
		close(a.treeRefreshStop)
		a.treeRefreshStop = nil
	}
}

// Close releases the terminal back to the user. Always call this before exit.
func (a *App) Close() {
	a.stopTreeRefresh()
	a.stopAutoScroll()
	a.clearImageGraphics()
	if a.closeCommandPaste != nil {
		a.closeCommandPaste()
		a.closeCommandPaste = nil
	}
	if a.screen != nil {
		a.screen.Fini()
	}
}

const (
	maxEventBurst         = 64
	maxEventBurstDuration = 8 * time.Millisecond
)

type pendingEventSource interface {
	PollEvent() tcell.Event
	HasPendingEvent() bool
}

// eventTime returns the wall clock used to bound one input burst. Tests can
// freeze it so the event-count limit remains deterministic.
func (a *App) eventTime() time.Time {
	if a.eventNow != nil {
		return a.eventNow()
	}
	return time.Now()
}

// Run is the editor's main event loop. Compatible queued input is applied in
// bounded bursts so rapid navigation does not render every intermediate state.
func (a *App) Run() error {
	a.width, a.height = a.screen.Size()
	a.flushFrame()

	var deferred tcell.Event
	for !a.quit {
		ev := deferred
		deferred = nil
		if ev == nil {
			ev = a.screen.PollEvent()
		}
		if ev == nil {
			break
		}
		deadline := a.eventTime().Add(maxEventBurstDuration)
		deferred, _ = a.processEventBurst(ev, a.screen, deadline)
		a.flushFrame()
	}
	return nil
}

// eventCanShareFrame reports whether an event may skip its intermediate frame
// when adjacent compatible events are already waiting. State-changing command
// boundaries remain one-event frames so prompts and tab changes are visible.
func (a *App) eventCanShareFrame(ev tcell.Event) bool {
	if ev == nil || a.anyModalOpen() || !a.lastEscape.IsZero() {
		return false
	}
	switch e := ev.(type) {
	case *tcell.EventKey:
		switch e.Key() {
		case tcell.KeyPgUp, tcell.KeyPgDn,
			tcell.KeyUp, tcell.KeyDown, tcell.KeyLeft, tcell.KeyRight,
			tcell.KeyHome, tcell.KeyEnd,
			tcell.KeyEnter, tcell.KeyTab,
			tcell.KeyBackspace, tcell.KeyBackspace2, tcell.KeyDelete:
			return true
		case tcell.KeyRune:
			return e.Modifiers()&(tcell.ModCtrl|tcell.ModAlt|tcell.ModMeta) == 0
		}
	case *tcell.EventMouse:
		buttons := e.Buttons()
		wheels := tcell.WheelUp | tcell.WheelDown | tcell.WheelLeft | tcell.WheelRight
		return buttons&wheels != 0 || (buttons == tcell.ButtonNone && a.dragMode == "")
	}
	return false
}

// processEventBurst applies first and then consumes compatible queued events
// until the count or time budget is exhausted. A consumed boundary event is
// returned untouched so Run can present the current frame before handling it.
func (a *App) processEventBurst(first tcell.Event, source pendingEventSource, deadline time.Time) (tcell.Event, int) {
	a.handleEvent(first)
	handled := 1
	if !a.eventCanShareFrame(first) {
		return nil, handled
	}
	for !a.quit && handled < maxEventBurst && a.eventTime().Before(deadline) && source.HasPendingEvent() {
		next := source.PollEvent()
		if next == nil {
			break
		}
		if !a.eventCanShareFrame(next) {
			return next, handled
		}
		a.handleEvent(next)
		handled++
	}
	return nil, handled
}

// initializeImageGraphics selects the one supported graphics presenter for
// this session. Herdr is preferred because its pane adapter owns presentation
// inside the multiplexer; direct KGP requires a real tcell TTY with pixels.
func (a *App) initializeImageGraphics() {
	if a == nil || a.screen == nil {
		return
	}
	if termgraphics.IsHerdr(osEnv{}) {
		a.graphics = termgraphics.NewHerdrPresenter(
			os.Getenv("HERDR_SOCKET_PATH"),
			os.Getenv("HERDR_PANE_ID"),
			nil,
		)
		return
	}
	if !termgraphics.SupportsKitty(osEnv{}) {
		return
	}

	tty, ok := a.screen.Tty()
	if !ok || tty == nil {
		return
	}
	window, err := tty.WindowSize()
	if err != nil {
		return
	}
	if window.Width <= 0 || window.Height <= 0 {
		return
	}
	cellWidth, cellHeight := window.CellDimensions()
	if cellWidth <= 0 || cellHeight <= 0 {
		return
	}

	a.graphics = termgraphics.NewKittyPresenter(tty)
	a.graphicsUsesTTY = true
	a.graphicsTTY = tty
}

// flushFrame completes one visual frame, then synchronizes terminal graphics
// only after tcell made the cell buffer visible. A graphics failure redraws
// the same frame with the half-block fallback before returning to the loop.
func (a *App) flushFrame() {
	a.draw()
	a.showCurrentFrame()
	if a.syncImageGraphics() {
		a.draw()
		a.showCurrentFrame()
	}
}

// showCurrentFrame invokes the testable frame-flush seam or falls back to the
// screen directly for Apps assembled by older helpers.
func (a *App) showCurrentFrame() {
	if a.showFrame != nil {
		a.showFrame()
		return
	}
	if a.screen != nil {
		a.screen.Show()
	}
}

// syncImageGraphics presents the currently visible image, or clears an old
// placement once that image is no longer eligible for terminal graphics.
// It reports whether the tcell frame must be redrawn as a half-block fallback.
func (a *App) syncImageGraphics() bool {
	request, visible := a.visibleImageRequest()
	if !visible {
		return a.clearImageGraphics()
	}

	placement, ok := termgraphics.Fit(request.Image.Source, request.Rect, request.Cells)
	if !ok {
		return a.clearImageGraphics()
	}
	key := imageViewKeyFromPlacement(request, placement)
	if a.graphicsVisible && (!a.hasGraphicsView || a.graphicsView != key ||
		!a.hasGraphicsPlacement || a.graphicsPlacement != placement ||
		!a.hasGraphicsGeometry || a.graphicsRect != request.Rect || a.graphicsCells != request.Cells) {
		if a.clearImageGraphics() {
			return true
		}
		if a.graphicsDisabled {
			return true
		}
	}

	if err := a.graphics.Present(request); err != nil {
		if errors.Is(err, termgraphics.ErrPayloadTooLarge) {
			if a.graphicsFallback == nil {
				a.graphicsFallback = make(map[imageViewKey]struct{})
			}
			a.graphicsFallback[key] = struct{}{}
			a.flashPayloadFallbackOnce(key)
			if a.graphicsVisible {
				a.clearImageGraphics()
			}
			return true
		}
		a.disableImageGraphics(err)
		return true
	}

	a.graphicsVisible = true
	a.graphicsView = key
	a.hasGraphicsView = true
	a.graphicsPlacement = placement
	a.hasGraphicsPlacement = true
	a.graphicsRect = request.Rect
	a.graphicsCells = request.Cells
	a.hasGraphicsGeometry = true
	return false
}

// visibleImageRequest returns the active image only when it can be rendered
// behind a fully visible tcell frame. Overlays and invalid terminal geometry
// intentionally use the universal half-block renderer instead.
func (a *App) visibleImageRequest() (termgraphics.Request, bool) {
	if a == nil || a.graphics == nil || a.graphicsDisabled ||
		a.width < minWidth || a.height < minHeight ||
		a.anyModalOpen() || a.findOpen {
		return termgraphics.Request{}, false
	}

	tab := a.activeTabPtr()
	if tab == nil {
		return termgraphics.Request{}, false
	}
	view, ok := tab.ImageView()
	if !ok {
		return termgraphics.Request{}, false
	}

	x, y, cols, rows := a.editorRect()
	if cols <= 0 || rows <= 0 {
		return termgraphics.Request{}, false
	}

	cells := termgraphics.CellPixels{Width: 1, Height: 1}
	if a.graphicsUsesTTY {
		if a.graphicsTTY == nil {
			return termgraphics.Request{}, false
		}
		window, err := a.graphicsTTY.WindowSize()
		if err != nil {
			return termgraphics.Request{}, false
		}
		if window.Width <= 0 || window.Height <= 0 {
			return termgraphics.Request{}, false
		}
		cellWidth, cellHeight := window.CellDimensions()
		if cellWidth <= 0 || cellHeight <= 0 {
			return termgraphics.Request{}, false
		}
		cells = termgraphics.CellPixels{Width: cellWidth, Height: cellHeight}
	}

	request := termgraphics.Request{
		Image: termgraphics.Image{
			ID:       view.ID,
			Revision: view.Revision,
			Source:   view.Image,
		},
		Rect:  termgraphics.CellRect{X: x, Y: y, Cols: cols, Rows: rows},
		Cells: cells,
	}
	key, ok := imageViewKeyFrom(request)
	if !ok {
		return termgraphics.Request{}, false
	}
	if _, fallback := a.graphicsFallback[key]; fallback {
		return termgraphics.Request{}, false
	}
	return request, true
}

// imageViewKeyFrom derives the per-view fallback key from the exact physical
// geometry the presenter will use for its fitted image.
func imageViewKeyFrom(request termgraphics.Request) (imageViewKey, bool) {
	placement, ok := termgraphics.Fit(request.Image.Source, request.Rect, request.Cells)
	if !ok {
		return imageViewKey{}, false
	}
	return imageViewKeyFromPlacement(request, placement), true
}

// imageViewKeyFromPlacement builds a per-view fallback key after Fit already
// supplied the precise target pixel dimensions for request.
func imageViewKeyFromPlacement(request termgraphics.Request, placement termgraphics.Placement) imageViewKey {
	return imageViewKey{
		ID:          request.Image.ID,
		Revision:    request.Image.Revision,
		PixelWidth:  placement.PixelWidth,
		PixelHeight: placement.PixelHeight,
	}
}

// clearImageGraphics removes an active placement after its tcell replacement
// frame is visible. A failed clear disables the session so no later protocol
// output can race an uncertain terminal state.
func (a *App) clearImageGraphics() bool {
	if a == nil || a.graphics == nil || !a.graphicsVisible {
		return false
	}
	if err := a.graphics.Clear(); err != nil {
		a.disableImageGraphics(err)
		return false
	}
	a.graphicsVisible = false
	a.graphicsView = imageViewKey{}
	a.hasGraphicsView = false
	a.graphicsPlacement = termgraphics.Placement{}
	a.hasGraphicsPlacement = false
	a.graphicsRect = termgraphics.CellRect{}
	a.graphicsCells = termgraphics.CellPixels{}
	a.hasGraphicsGeometry = false
	return false
}

// disableImageGraphics stops all future presenter calls and flashes one
// actionable session-level fallback message.
func (a *App) disableImageGraphics(err error) {
	if a == nil || a.graphicsDisabled {
		return
	}
	a.graphicsDisabled = true
	a.graphicsVisible = false
	a.graphicsView = imageViewKey{}
	a.hasGraphicsView = false
	a.graphicsPlacement = termgraphics.Placement{}
	a.hasGraphicsPlacement = false
	a.graphicsRect = termgraphics.CellRect{}
	a.graphicsCells = termgraphics.CellPixels{}
	a.hasGraphicsGeometry = false
	a.graphics = nil
	a.graphicsTTY = nil
	if a.graphicsHinted {
		return
	}
	a.graphicsHinted = true
	if errors.Is(err, termgraphics.ErrFeatureDisabled) {
		a.flash("Herdr graphics disabled — add [experimental] kitty_graphics = true, run herdr server reload-config, then restart Herdr if needed.")
		return
	}
	a.flash("Image graphics unavailable — using block preview.")
}

// flashPayloadFallbackOnce reports an oversized image only once for the
// precise image revision and target geometry that needs block rendering.
func (a *App) flashPayloadFallbackOnce(key imageViewKey) {
	if a.payloadHints == nil {
		a.payloadHints = make(map[imageViewKey]struct{})
	}
	if _, flashed := a.payloadHints[key]; flashed {
		return
	}
	a.payloadHints[key] = struct{}{}
	a.flash("Image too large for graphics — using block preview.")
}

// handleEvent routes a tcell event to its specific handler.
func (a *App) handleEvent(ev tcell.Event) {
	switch e := ev.(type) {
	case *tcell.EventResize:
		a.width, a.height = a.screen.Size()
		a.screen.Sync()
	case *tcell.EventKey:
		a.handleKey(e)
	case *tcell.EventPaste:
		if e.Start() {
			a.beginPaste()
		} else {
			a.finishPaste()
		}
	case *tcell.EventMouse:
		a.handleMouse(e)
	case *autoScrollEvent:
		a.handleAutoScroll()
	case *treeRefreshEvent:
		a.refreshTreeNow()
	case *customActionDoneEvent:
		a.handleCustomActionDone(e)
	case *defaultOpenDoneEvent:
		a.handleDefaultOpenDone(e)
	case *revealDoneEvent:
		a.handleRevealDone(e)
	case *formatDoneEvent:
		a.handleFormatDone(e)
	case *finderRebuiltEvent:
		// The background indexer just finished. Re-run the visible
		// query so "Indexing…" gives way to real results without
		// the user having to type or wait for the next keystroke.
		if a.finderOpen {
			a.refreshFinderResults()
		}
	}
}

// refreshTreeNow re-runs the same refresh pipeline the 10s timer
// fires: rescan the file tree (preserving expansion state), reconcile
// any open tabs with disk, refresh git status, and invalidate the
// finder index so a freshly-pulled file shows up everywhere at once.
// Called from the periodic event and from runCustomAction's success
// path so a Copy-from-remote action's output is visible immediately
// instead of after the next tick.
func (a *App) refreshTreeNow() {
	a.refreshTree()
	a.reconcileOpenTabsWithDisk()
	a.refreshGitStatus()
	a.invalidateFinder()
}

// handleCustomActionDone surfaces the result of an async custom-action
// run. Success flashes a brief confirmation and forces a sidebar
// refresh so a freshly-pulled file appears in the file tree without
// waiting for the 10-second auto-refresh tick. Failure opens an info
// modal with the captured stderr — the prior 1-line flash truncated
// scp's actual diagnostics, which is exactly the case where the user
// most needs to read them.
func (a *App) handleCustomActionDone(e *customActionDoneEvent) {
	if e.err != nil {
		title := "Action failed: " + e.label
		body := splitErrorOutput(e.err, e.output)
		a.openInfo(title, body)
		return
	}
	a.flash(e.label + " — done")
	a.refreshTreeNow()
}

// splitErrorOutput formats the action's captured output for the info
// modal: an opening line summarising the exit error, then up to a
// handful of lines of trimmed stderr, with the actions.log path as
// the closing line so the user knows where to find the full record.
// Pulled out so handleCustomActionDone reads as the routing decision
// it really is.
func splitErrorOutput(runErr error, out []byte) []string {
	const maxLines = 8
	const maxLineWidth = 78

	body := []string{strings.TrimSpace(runErr.Error())}
	captured := strings.TrimRight(string(out), "\n")
	if captured != "" {
		body = append(body, "")
		count := 0
		for _, ln := range strings.Split(captured, "\n") {
			ln = strings.TrimRight(ln, "\r")
			ln = termtext.Truncate(ln, maxLineWidth)
			body = append(body, ln)
			count++
			if count >= maxLines {
				body = append(body, "… (truncated; see actions.log)")
				break
			}
		}
	}
	if logPath := customactions.LogPath(); logPath != "" {
		body = append(body, "", "Full output: "+logPath)
	}
	return body
}

// reconcileOpenTabsWithDisk runs once per background tick. For every
// open tab with a real path it stats the file, compares the on-disk
// mtime to what the tab last knew, and reacts:
//
//   - File missing  → flash once, mark the tab dirty so the user knows.
//   - Disk newer, tab clean → reload the buffer silently, flash success.
//   - Disk newer, tab dirty → leave the buffer alone, flash a warning
//     that saving will overwrite.
//
// Untitled tabs (Path == "") are skipped because there's no disk file to
// reconcile against.
func (a *App) reconcileOpenTabsWithDisk() {
	for _, tab := range a.tabs {
		if tab.Path == "" || tab.IsExternal() {
			continue
		}
		info, err := os.Stat(tab.Path)
		if os.IsNotExist(err) {
			if !tab.DiskGone {
				tab.DiskGone = true
				tab.Dirty = true
				a.flash(fmt.Sprintf("%s deleted on disk", filepath.Base(tab.Path)))
			}
			continue
		}
		if err != nil {
			// Permission denied or some other transient stat error — leave
			// the tab as-is rather than spamming the user with a flash.
			continue
		}
		if tab.DiskGone {
			// File reappeared. Force the mtime check below to fire so we
			// either reload or warn about a dirty conflict.
			tab.DiskGone = false
			tab.Mtime = time.Time{}
		}
		if !info.ModTime().After(tab.Mtime) {
			continue // unchanged on disk.
		}
		if tab.Dirty {
			a.flash(fmt.Sprintf("%s changed on disk — your edits will overwrite on save",
				filepath.Base(tab.Path)))
			// Update Mtime so we don't re-flash every tick for the same change.
			tab.Mtime = info.ModTime()
			continue
		}
		if err := tab.Reload(); err != nil {
			a.flash(fmt.Sprintf("%s reload failed: %v", filepath.Base(tab.Path), err))
			continue
		}
		a.flash(fmt.Sprintf("%s reloaded from disk", filepath.Base(tab.Path)))
	}
}

// -----------------------------------------------------------------------------
// Layout helpers
// -----------------------------------------------------------------------------

// sidebarW is the effective width of the sidebar block (file tree +
// splitter): zero when hidden, a.sidebarWidth otherwise. Every layout
// helper and click router goes through this so toggling/resizing the
// panel reshapes the entire UI in one place.
func (a *App) sidebarW() int {
	if !a.sidebarShown {
		return 0
	}
	return a.sidebarWidth
}

// sidebarPathHeight returns the currently reserved height of the PATH INFO
// panel. It stays unavailable in tiny layouts even when collapsed so a resize
// cannot make a held header press become a tree press.
func (a *App) sidebarPathHeight() int {
	if !a.sidebarShown || a.tree == nil || a.sidebarWidth <= 1 ||
		a.height-1 < sidebarPathExpandedHeight {
		return 0
	}
	if a.sidebarPathCollapsed {
		return sidebarPathCollapsedHeight
	}
	return sidebarPathExpandedHeight
}

// sidebarRect returns the file tree's render rectangle above the path panel.
// It is one column narrower than the sidebar block because the rightmost
// column belongs to the resize splitter. Zero width when the sidebar is hidden.
func (a *App) sidebarRect() (x, y, w, h int) {
	sw := a.sidebarW()
	if sw <= 0 {
		return 0, 0, 0, 0
	}
	h = a.height - 1 - a.sidebarPathHeight()
	if h < 0 {
		h = 0
	}
	return 0, 0, sw - 1, h
}

// sidebarPathRect returns the path panel below the tree. It only exists when
// a visible tree has enough rows to show the complete expanded panel.
func (a *App) sidebarPathRect() (x, y, w, h int) {
	h = a.sidebarPathHeight()
	if h == 0 {
		return 0, 0, 0, 0
	}
	return 0, a.height - 1 - h, a.sidebarWidth - 1, h
}

// splitterX returns the x coordinate of the resize splitter column, or -1
// when the sidebar is hidden (no splitter to draw or click).
func (a *App) splitterX() int {
	if !a.sidebarShown {
		return -1
	}
	return a.sidebarWidth - 1
}

// resizeSidebar applies the user's desired sidebar width while clamping it
// to a sensible range — the file tree stays wide enough to read names and
// the editor keeps at least minEditorAfterDrag columns. Tiny windows that
// can't satisfy both fall back to the minimum and let the editor shrink.
func (a *App) resizeSidebar(target int) {
	if target < minSidebarWidth {
		target = minSidebarWidth
	}
	max := a.width - minEditorAfterDrag
	if max < minSidebarWidth {
		max = minSidebarWidth
	}
	if target > max {
		target = max
	}
	a.sidebarWidth = target
}

// tabBarRect returns the tab bar's screen rectangle (one row tall).
func (a *App) tabBarRect() (x, y, w, h int) {
	sw := a.sidebarW()
	return sw, 0, a.width - sw, 1
}

// editorRect returns the editor body's screen rectangle (everything to the
// right of the sidebar, between the tab bar and the status bar). When the
// find bar is open, one row is taken out of the bottom — the bar is
// pinned directly above the status bar.
func (a *App) editorRect() (x, y, w, h int) {
	sw := a.sidebarW()
	h = a.height - 2
	if a.findOpen {
		h -= findBarHeight
	}
	return sw, 1, a.width - sw, h
}

// statusRect returns the status bar's screen rectangle (full-width bottom row).
func (a *App) statusRect() (x, y, w, h int) {
	return 0, a.height - 1, a.width, 1
}

// editorSize returns just the (width, height) of the editor body. Used by
// keyboard handlers that need to compute page-up / page-down deltas.
func (a *App) editorSize() (int, int) {
	_, _, w, h := a.editorRect()
	return w, h
}

// menuButtonRect returns the on-screen rectangle of the ≡ icon in the tab
// bar. Click hit-tests in tabBarClick consult this directly. When the
// sidebar is hidden the icon shifts left to fill the corner.
func (a *App) menuButtonRect() (x, y, w, h int) {
	return a.sidebarW(), 0, menuButtonWidth, 1
}

// menuModalRect returns the on-screen rectangle of the action modal,
// centered in the window. Height is derived from the current layout
// so adding custom actions grows the modal automatically.
func (a *App) menuModalRect() (x, y, w, h int) {
	w = modalWidth
	_, _, h = a.menuLayout()
	x = (a.width - w) / 2
	y = (a.height - h) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return
}

// -----------------------------------------------------------------------------
// Keyboard
// -----------------------------------------------------------------------------

// handleKey responds to keyboard events. There are intentionally no Ctrl-
// based shortcuts: every action lives behind the ≡ menu so the editor never
// fights the terminal (Ctrl-S/Q flow control) or a tmux/zellij prefix. The
// only "command" key is Esc, which closes the menu and acts as the leader
// for the hotkey table in leader.go (Esc s = Save, Esc u = Undo, etc.).
func (a *App) handleKey(ev *tcell.EventKey) {
	if a.pasting {
		a.appendPastedKey(ev)
		return
	}

	// Secondary modals own the keyboard while they're up. Each handler
	// understands Esc (cancel), Enter (submit / activate), and the keys
	// relevant to its layout (text editing for the prompt, arrow keys for
	// the context menu, etc.).
	if a.promptOpen {
		a.handlePromptKey(ev)
		return
	}
	if a.confirmOpen {
		a.handleConfirmKey(ev)
		return
	}
	if a.dirtyOpen {
		a.handleDirtyKey(ev)
		return
	}
	if a.formOpen {
		a.handleFormKey(ev)
		return
	}
	if a.contextOpen {
		a.handleContextKey(ev)
		return
	}
	if a.findOpen {
		a.handleFindKey(ev)
		return
	}
	if a.finderOpen {
		a.handleFinderKey(ev)
		return
	}
	if a.sidebarPathPrefixFocused {
		a.handleSidebarPathKey(ev)
		return
	}

	if ev.Key() == tcell.KeyEsc {
		// Esc is the editor's only command key. Behavior:
		//   • menu open  → close it
		//   • menu shut  → open it on the SECOND Esc within doubleEscMs;
		//     a SINGLE Esc arms the leader table (see below).
		// A lone Esc that isn't followed by a leader binding within the
		// window is intentionally a no-op so the key still feels harmless
		// to mash.
		if a.menuOpen {
			a.closeMenu()
			a.lastEscape = time.Time{}
			return
		}
		now := time.Now()
		if !a.lastEscape.IsZero() && now.Sub(a.lastEscape) < doubleEscMs {
			a.openMenu()
			a.lastEscape = time.Time{}
			return
		}
		a.lastEscape = now
		return
	}
	// Esc-leader hotkey: if Esc was pressed within doubleEscMs and this
	// key is bound in the leader table, fire the action and consume the
	// keystroke. Unbound keys fall through to normal handling so a stray
	// Esc doesn't swallow the next character the user types.
	if !a.lastEscape.IsZero() && time.Since(a.lastEscape) < doubleEscMs {
		if ev.Key() == tcell.KeyRune {
			if action := leaderActionFor(ev.Rune()); action != nil {
				a.lastEscape = time.Time{}
				action(a)
				return
			}
		}
	}
	// Any other key cancels a pending Esc so a stale half-tap doesn't
	// surprise the user later.
	a.lastEscape = time.Time{}

	// While the menu is open, only the navigation keys do anything —
	// editing keys are blocked, but Down/Up move the highlight and Enter
	// activates the highlighted row.
	if a.menuOpen {
		if ev.Key() == tcell.KeyRune {
			if action := leaderActionFor(ev.Rune()); action != nil {
				a.lastEscape = time.Time{}
				action(a)
				return
			}
		}
		switch ev.Key() {
		case tcell.KeyDown:
			a.menuMoveSelection(1)
		case tcell.KeyUp:
			a.menuMoveSelection(-1)
		case tcell.KeyEnter:
			a.menuActivate()
		}
		return
	}

	tab := a.activeTabPtr()
	if tab == nil {
		return
	}
	// Read-only tabs have no cursor or editing surface. Leader commands have
	// already run above, so dropping remaining keys does not block Esc-o.
	// caret movement. Drop every key here so the user can mash arrow
	// keys without anything mysterious happening behind the splash.
	if tab.IsReadOnly() {
		return
	}
	extend := ev.Modifiers()&tcell.ModShift != 0

	switch ev.Key() {
	case tcell.KeyUp:
		if a.softWrap {
			ew, _ := a.editorSize()
			tab.MoveCursorSoftWrap(-1, ew, extend)
		} else {
			tab.MoveCursor(-1, 0, extend)
		}
	case tcell.KeyDown:
		if a.softWrap {
			ew, _ := a.editorSize()
			tab.MoveCursorSoftWrap(1, ew, extend)
		} else {
			tab.MoveCursor(1, 0, extend)
		}
	case tcell.KeyLeft:
		tab.MoveCursor(0, -1, extend)
	case tcell.KeyRight:
		tab.MoveCursor(0, 1, extend)
	case tcell.KeyHome:
		tab.MoveLineHome(extend)
	case tcell.KeyEnd:
		tab.MoveLineEnd(extend)
	case tcell.KeyPgUp:
		ew, h := a.editorSize()
		if a.softWrap {
			tab.MoveCursorSoftWrap(-h, ew, extend)
		} else {
			tab.MoveCursor(-h, 0, extend)
		}
	case tcell.KeyPgDn:
		ew, h := a.editorSize()
		if a.softWrap {
			tab.MoveCursorSoftWrap(h, ew, extend)
		} else {
			tab.MoveCursor(h, 0, extend)
		}
	case tcell.KeyEnter:
		tab.InsertString("\n")
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		tab.Backspace()
	case tcell.KeyDelete:
		tab.Delete()
	case tcell.KeyTab:
		tab.InsertString(tab.IndentUnit)
	case tcell.KeyRune:
		tab.InsertRune(ev.Rune())
	}
}

// beginPaste starts collecting the key events tcell sends between bracketed
// paste start and end markers, without dispatching them as editor commands.
func (a *App) beginPaste() {
	a.pasting = true
	a.pasteAuthorized = a.commandPasteRecent != nil && a.commandPasteRecent(a.eventTime())
	a.pasteBuf.Reset()
	a.lastEscape = time.Time{}
}

// appendPastedKey converts text-like terminal key events into the paste
// buffer. Navigation and command keys are deliberately ignored.
func (a *App) appendPastedKey(ev *tcell.EventKey) {
	switch ev.Key() {
	case tcell.KeyRune:
		a.pasteBuf.WriteRune(ev.Rune())
	case tcell.KeyEnter, tcell.KeyCtrlM:
		a.pasteBuf.WriteByte('\r')
	case tcell.KeyCtrlJ:
		a.pasteBuf.WriteByte('\n')
	case tcell.KeyTab:
		a.pasteBuf.WriteByte('\t')
	}
}

// finishPaste ends bracketed paste capture and routes only payload text that
// was authorized when the terminal sent its paste-start marker.
func (a *App) finishPaste() {
	captured := a.pasteBuf.String()
	accepted := a.pasteAuthorized
	a.pasting = false
	a.pasteAuthorized = false
	a.pasteBuf.Reset()
	if !accepted || captured == "" {
		return
	}
	a.routePastedText(captured)
}

// -----------------------------------------------------------------------------
// Mouse
// -----------------------------------------------------------------------------

// handleMouse routes a mouse event to whichever panel the cursor is over,
// tracking drag state so a click-drag inside the editor extends the
// selection. When the action menu is open it absorbs all mouse events:
// clicks inside trigger an action, clicks outside dismiss the menu.
func (a *App) handleMouse(ev *tcell.EventMouse) {
	x, y := ev.Position()
	btn := ev.Buttons()

	// Remember when we last saw Shift held down on ANY mouse event.
	// Zellij + macOS Terminal split shift+wheel into two events: a
	// ButtonNone+Shift "modifier state" event, then a WheelDown/Up
	// with no modifier. We bridge them via modifierStickyWindow below.
	if ev.Modifiers()&tcell.ModShift != 0 {
		a.lastShiftAt = time.Now()
	}

	// Secondary modals absorb all mouse input. The order here matches
	// keyboard routing so behavior stays predictable.
	if a.promptOpen {
		a.handlePromptMouse(x, y, btn)
		return
	}
	if a.confirmOpen {
		a.handleConfirmMouse(x, y, btn)
		return
	}
	if a.dirtyOpen {
		a.handleDirtyMouse(x, y, btn)
		return
	}
	if a.formOpen {
		a.handleFormMouse(x, y, btn)
		return
	}
	if a.contextOpen {
		a.handleContextMouse(x, y, btn)
		return
	}
	if a.finderOpen {
		a.handleFinderMouse(x, y, btn)
		return
	}

	if a.menuOpen {
		a.updateMenuHover(x, y)
		a.handleMenuMouse(x, y, btn)
		return
	}

	leftDown := btn&tcell.Button1 != 0
	// A tcell backend may combine Button1 and wheel bits in one event. Consume
	// the complete PATH INFO gesture before wheel routing can reach the newly
	// exposed tree or editor after a header toggle changes the panel geometry.
	if leftDown && a.dragMode == "path" {
		return
	}

	// Right-click handling. File-tree rows and rendered tabs get focused
	// context menus; everywhere else falls through to the main action menu.
	// Note: macOS Terminal + tmux often swallows Button3, which is why core
	// actions also live in the main ≡ menu.
	if btn&tcell.Button3 != 0 {
		if a.sidebarPathContains(x, y) {
			a.sidebarPathPrefixFocused = false
			return
		}
		a.sidebarPathPrefixFocused = false
		if a.tryTreeContextClick(x, y) {
			return
		}
		if a.tryTabContextClick(x, y) {
			return
		}
		a.openMenu()
		return
	}

	// Wheel events take priority — they fire even with no button held.
	// Shift+wheel rotates the vertical wheel into horizontal scrolling
	// (the VS Code convention). Most terminals never emit native
	// WheelLeft/WheelRight, so this is the path that actually fires in
	// practice; the dedicated horizontal-wheel branch below is a bonus
	// for terminals that do.
	//
	// We accept "shift was just seen" within modifierStickyWindow as
	// equivalent to shift-on-this-event, because Zellij and friends
	// strip the modifier from the actual wheel event.
	shift := ev.Modifiers()&tcell.ModShift != 0 ||
		(!a.lastShiftAt.IsZero() && time.Since(a.lastShiftAt) < modifierStickyWindow)
	if btn&tcell.WheelUp != 0 {
		if shift {
			a.scrollAtH(x, y, -wheelCols)
		} else {
			a.scrollAt(x, y, -wheelLines)
		}
		return
	}
	if btn&tcell.WheelDown != 0 {
		if shift {
			a.scrollAtH(x, y, wheelCols)
		} else {
			a.scrollAt(x, y, wheelLines)
		}
		return
	}
	if btn&tcell.WheelLeft != 0 {
		a.scrollAtH(x, y, -wheelCols)
		return
	}
	if btn&tcell.WheelRight != 0 {
		a.scrollAtH(x, y, wheelCols)
		return
	}

	// Drag continuation: while we're mid-drag in the editor, every event
	// with the button held extends the selection — even if the cursor has
	// wandered out of the editor pane.
	if leftDown && a.dragMode == "editor" {
		a.editorDrag(x, y)
		return
	}

	// Sidebar resize drag: keep the splitter glued to the mouse x so the
	// panel reshapes live as the user drags.
	if leftDown && a.dragMode == "sidebar" {
		a.resizeSidebar(x + 1)
		return
	}

	// Tree presses act once on button-down. Terminals report motion with
	// Button1 still set, so consume the rest of the held gesture rather than
	// repeatedly toggling or opening whichever row the pointer crosses.
	if leftDown && a.dragMode == "tree" {
		return
	}

	// Initial press dispatch.
	if leftDown && a.dragMode == "" {
		if a.handleSidebarPathMouse(x, y) {
			a.dragMode = "path"
			return
		}
		a.sidebarPathPrefixFocused = false
		sw := a.sidebarW()
		splitX := a.splitterX()
		treeX, treeY, treeW, treeH := a.sidebarRect()
		switch {
		case splitX >= 0 && x == splitX:
			a.dragMode = "sidebar"
		case sw > 0 && pointInRect(x, y, treeX, treeY, treeW, treeH):
			a.dragMode = "tree"
			a.sidebarClick(x, y)
		case y == 0:
			a.tabBarClick(x, y)
		case y > 0 && y < a.height-1:
			if a.editorPress(x, y) {
				a.dragMode = "editor"
			}
		}
		return
	}

	// Button released — exit any drag mode we were in. Preserve the prior
	// mode long enough to publish a completed editor selection exactly once.
	completedDragMode := a.dragMode
	a.dragMode = ""
	a.stopAutoScroll()
	if completedDragMode == "editor" {
		a.autoCopyMouseSelection()
	}
}

// handleMenuMouse processes mouse events while the action menu is open.
// Left-click outside the modal closes it; left-click on a row runs that
// row's action (if it is currently enabled).
func (a *App) handleMenuMouse(x, y int, btn tcell.ButtonMask) {
	if btn&tcell.Button1 == 0 {
		return
	}
	mx, my, mw, mh := a.menuModalRect()
	if x < mx || x >= mx+mw || y < my || y >= my+mh {
		a.closeMenu()
		return
	}
	relY := y - my
	items, _, _ := a.menuLayout()
	for _, item := range items {
		if item.relY != relY {
			continue
		}
		if item.enabled(a) {
			item.action(a)
		}
		return
	}
}

// scrollAt scrolls whichever panel the (x, y) cursor is over.
func (a *App) scrollAt(x, y, delta int) {
	if a.sidebarPathContains(x, y) {
		return
	}
	sx, sy, sw, sh := a.sidebarRect()
	if a.tree != nil && pointInRect(x, y, sx, sy, sw, sh) {
		a.tree.Scroll(delta)
		return
	}
	if sw := a.sidebarW(); sw > 0 && x < sw {
		return
	}
	if y > 0 && y < a.height-1 {
		if t := a.activeTabPtr(); t != nil {
			if t.IsReadOnly() {
				return
			}
			ew, eh := a.editorSize()
			if a.softWrap {
				t.ScrollSoftWrap(delta, ew, eh)
			} else {
				t.Scroll(delta)
			}
		}
	}
}

// scrollAtH scrolls the panel under (x, y) horizontally by delta cells.
// The file tree has no useful horizontal axis (each row is a single label),
// so we only honor horizontal wheel events when they fall inside the
// editor pane.
func (a *App) scrollAtH(x, y, delta int) {
	if sw := a.sidebarW(); sw > 0 && x < sw {
		return
	}
	if y > 0 && y < a.height-1 {
		if t := a.activeTabPtr(); t != nil {
			if t.IsReadOnly() {
				return
			}
			if a.softWrap {
				return
			}
			t.ScrollH(delta)
		}
	}
}

// tryTreeContextClick opens the right-click context menu when (x, y) lands
// on a tree row. Returns true if it consumed the event so the caller knows
// not to fall back to the main action menu. Right-clicking a node also
// counts as "I'm working here" — the active folder updates so the main
// menu's New File defaults to a sensible target even after the context
// menu closes.
func (a *App) tryTreeContextClick(x, y int) bool {
	if a.tree == nil {
		return false
	}
	sx, sy, sw, sh := a.sidebarRect()
	if !pointInRect(x, y, sx, sy, sw, sh) {
		return false
	}
	n, ok := a.tree.HitTest(x-sx, y-sy)
	if !ok {
		return false
	}
	a.sidebarPathPrefixFocused = false
	if n.IsDir {
		a.setActiveFolder(n.Path)
	} else {
		a.setActiveFolder(filepath.Dir(n.Path))
	}
	a.tree.SelectedPath = n.Path
	a.copyCurrentSidebarPath(true)
	a.openTreeContext(n, x, y)
	return true
}

// tabAtPoint returns the rendered tab under the screen coordinate. The
// rectangles come from the most recent tab-bar draw so hit testing matches
// the exact cells visible to the user.
func (a *App) tabAtPoint(x, y int) (*editor.Tab, bool) {
	if y != 0 {
		return nil, false
	}
	for _, r := range a.lastTabRects {
		if x >= r.X && x < r.X+r.Width && r.Index >= 0 && r.Index < len(a.tabs) {
			return a.tabs[r.Index], true
		}
	}
	return nil, false
}

// tryTabContextClick activates a hit tab and opens its close-actions menu.
// Activating first keeps the editor body and file-tree highlight aligned with
// the tab whose menu the user is about to operate.
func (a *App) tryTabContextClick(x, y int) bool {
	tab, ok := a.tabAtPoint(x, y)
	if !ok {
		return false
	}
	a.activeTab = a.tabIndex(tab)
	a.syncActiveTreeFile()
	a.openTabContext(tab, x, y)
	return true
}

// sidebarClick toggles a directory or opens a file when the user clicks a
// row in the file tree. Either action also updates the editor's "active
// folder" so the next New File from the main menu defaults to wherever
// the user is currently focused. Clicking the project-root row only
// resets the active folder — it never toggles the root's expansion
// since the root is always shown and there's no useful "collapsed
// root" state.
func (a *App) sidebarClick(x, y int) {
	if a.tree == nil {
		return
	}
	sx, sy, sw, sh := a.sidebarRect()
	if !pointInRect(x, y, sx, sy, sw, sh) {
		return
	}
	n, ok := a.tree.HitTest(x-sx, y-sy)
	if !ok {
		return
	}
	if n == a.tree.Root {
		a.tree.SelectedPath = n.Path
		a.copyCurrentSidebarPath(true)
		a.setActiveFolder(a.rootDir)
		return
	}
	a.tree.SelectedPath = n.Path
	a.copyCurrentSidebarPath(true)
	if n.IsDir {
		a.setActiveFolder(n.Path)
		a.tree.Toggle(n)
		return
	}
	a.setActiveFolder(filepath.Dir(n.Path))
	a.openFileWithExternalPolicy(n.Path, externalOpenDisplayOnly)
}

// setActiveFolder records path as the editor's current working folder and
// mirrors it onto the file tree. With no editor tab, it is also the fallback
// exact tree selection. All writes to a.activeFolder go through here.
func (a *App) setActiveFolder(path string) {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	a.activeFolder = path
	if a.tree != nil {
		a.tree.ActiveFolder = path
		if a.activeTabPtr() == nil {
			a.tree.SelectedPath = path
		}
	}
}

// tabBarClick dispatches clicks in the tab bar: the leftmost menuButtonWidth
// cells open the action menu; remaining cells switch or close tabs based on
// where the click landed within their rendered geometry.
func (a *App) tabBarClick(x, _ int) {
	sw := a.sidebarW()
	if x >= sw && x < sw+menuButtonWidth {
		a.openMenu()
		return
	}
	for _, r := range a.lastTabRects {
		if x >= r.X && x < r.X+r.Width {
			if x == r.CloseX {
				a.requestCloseTab(r.Index)
				return
			}
			a.activeTab = r.Index
			a.syncActiveTreeFile()
			return
		}
	}
}

// syncActiveTreeFile mirrors the active tab path into the file tree.
func (a *App) syncActiveTreeFile() {
	if a.tree == nil {
		return
	}
	tab := a.activeTabPtr()
	if tab == nil || tab.Path == "" {
		a.tree.ActiveFile = ""
		a.tree.SelectedPath = a.activeFolder
		return
	}
	a.tree.ActiveFile = tab.Path
	a.tree.SelectedPath = tab.Path
}

// editorPress handles the initial mouse press inside the editor — placing
// the caret, optionally selecting a word on double-click. It reports whether
// the press landed on editable text so callers can start a drag only then.
func (a *App) editorPress(x, y int) bool {
	tab := a.activeTabPtr()
	if tab == nil {
		return false
	}
	ex, ey, ew, eh := a.editorRect()
	if tab.IsExternal() {
		if tab.ExternalOpenHitTest(x, y, ex, ey, ew, eh) {
			a.openPathWithDefaultApp(tab.Path)
		}
		return false
	}
	if tab.IsReadOnly() {
		return false
	}
	localX := x - ex
	localY := y - ey
	if a.openGitHunkAt(tab, localX, localY, ew, eh) {
		return false
	}
	pos, ok := a.editorHitTest(tab, localX, localY, ew, eh)
	if !ok {
		return false
	}
	textArea := localX >= editorTextStart(tab)

	now := time.Now()
	if a.lastClick.x == x && a.lastClick.y == y && now.Sub(a.lastClick.when) < doubleClickMs {
		a.selectWordAt(tab, pos)
		a.lastClick = clickRecord{} // prevent triple-click from selecting nothing.
		return textArea
	}
	a.lastClick = clickRecord{x: x, y: y, when: now}
	tab.MoveCursorTo(pos, false)
	return textArea
}

// editorTextStart mirrors editor's private gutter-width formula so App can
// distinguish text presses from gutter clicks before it starts a drag.
func editorTextStart(tab *editor.Tab) int {
	gutterWidth := 6
	if width := len(strconv.Itoa(tab.Buffer.LineCount())) + 2; width > gutterWidth {
		gutterWidth = width
	}
	return gutterWidth + 1
}

// openGitHunkAt opens a diff preview when the user clicks a gutter marker.
func (a *App) openGitHunkAt(tab *editor.Tab, localX, localY, w, h int) bool {
	if localX != 0 || localY < 0 {
		return false
	}
	pos, ok := a.editorHitTest(tab, localX, localY, w, h)
	if !ok {
		return false
	}
	line := pos.Line
	if tab.GitLines[line] == editor.GitLineNone {
		return false
	}
	lines := loadGitHunkPreview(a.rootDir, tab.Path, line)
	if len(lines) == 0 {
		a.openInfo("Git change", []string{"No git diff found for this line."})
		return true
	}
	a.openInfo("Git change · "+filepath.Base(tab.Path), lines)
	return true
}

// editorDrag extends the selection during a click-drag inside the editor.
// (x, y) is clamped to the editor rect so dragging into another pane still
// extends the selection sensibly. When the mouse passes above or below the
// editor we engage auto-scroll so the user can select content that's not
// yet on screen — same feel as VS Code or any GUI text editor. Image tabs
// drop the drag entirely.
func (a *App) editorDrag(x, y int) {
	tab := a.activeTabPtr()
	if tab == nil || tab.IsReadOnly() {
		return
	}
	ex, ey, ew, eh := a.editorRect()

	// Remember where the mouse is so the auto-scroll tick can extend the
	// selection at this column even while the mouse stops moving.
	a.lastDragX = x
	a.lastDragY = y

	// Edge detection: outside the editor's vertical bounds turns on
	// auto-scroll; back inside turns it off.
	switch {
	case y < ey:
		a.startAutoScroll(-1)
	case y >= ey+eh:
		a.startAutoScroll(1)
	default:
		a.stopAutoScroll()
	}

	// Clamp the mouse into the editor and extend the selection there.
	localX := x - ex
	localY := y - ey
	if localX < 0 {
		localX = 0
	}
	if localY < 0 {
		localY = 0
	}
	if localX >= ew {
		localX = ew - 1
	}
	if localY >= eh {
		localY = eh - 1
	}
	pos, ok := a.editorHitTest(tab, localX, localY, ew, eh)
	if !ok {
		return
	}
	tab.MoveCursorTo(pos, true)
}

// startAutoScroll begins a timer goroutine that posts autoScrollEvents at
// autoScrollTick intervals so the editor keeps scrolling while the user
// holds the mouse past an edge. dir is -1 (up) or +1 (down). Calling with
// the same direction is a no-op so we don't restart the timer on every
// drag motion event.
func (a *App) startAutoScroll(dir int) {
	if a.autoScrollDir == dir {
		return
	}
	a.stopAutoScroll()
	a.autoScrollDir = dir
	a.autoScrollStop = make(chan struct{})
	stop := a.autoScrollStop
	scr := a.screen
	go func() {
		ticker := time.NewTicker(autoScrollTick)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case t := <-ticker.C:
				_ = scr.PostEvent(&autoScrollEvent{when: t})
			}
		}
	}()
}

// stopAutoScroll signals the auto-scroll goroutine to exit (idempotent).
func (a *App) stopAutoScroll() {
	if a.autoScrollStop != nil {
		close(a.autoScrollStop)
		a.autoScrollStop = nil
	}
	a.autoScrollDir = 0
}

// handleAutoScroll runs once per autoScrollEvent: nudge the viewport in the
// armed direction and extend the selection to the edge row at the user's
// last known mouse column. Bails out (and stops the timer) if anything
// suggests the user is no longer drag-selecting (button released, menu
// opened, no active tab).
func (a *App) handleAutoScroll() {
	if a.autoScrollDir == 0 || a.dragMode != "editor" || a.anyModalOpen() {
		a.stopAutoScroll()
		return
	}
	tab := a.activeTabPtr()
	if tab == nil || tab.IsReadOnly() {
		a.stopAutoScroll()
		return
	}
	ex, _, ew, eh := a.editorRect()
	if a.softWrap {
		tab.ScrollSoftWrap(a.autoScrollDir, ew, eh)
	} else {
		tab.Scroll(a.autoScrollDir)
	}

	localX := a.lastDragX - ex
	if localX < 0 {
		localX = 0
	}
	if localX >= ew {
		localX = ew - 1
	}
	localY := eh - 1
	if a.autoScrollDir < 0 {
		localY = 0
	}
	pos, ok := a.editorHitTest(tab, localX, localY, ew, eh)
	if !ok {
		return
	}
	tab.MoveCursorTo(pos, true)
}

// editorHitTest routes coordinate mapping through the active wrapping mode.
func (a *App) editorHitTest(tab *editor.Tab, localX, localY, w, h int) (editor.Position, bool) {
	if tab == nil || tab.IsReadOnly() {
		return editor.Position{}, false
	}
	if a.softWrap {
		return tab.HitTestSoftWrap(localX, localY, w, h)
	}
	return tab.HitTest(localX, localY, w, h)
}

// selectWordAt selects the word under the buffer position p (or does
// nothing if p sits in whitespace / punctuation).
func (a *App) selectWordAt(tab *editor.Tab, p editor.Position) {
	line := tab.Buffer.LineRunes(p.Line)
	if len(line) == 0 {
		return
	}
	start := p.Col
	if start > len(line) {
		start = len(line)
	}
	for start > 0 && isWordChar(line[start-1]) {
		start--
	}
	end := p.Col
	for end < len(line) && isWordChar(line[end]) {
		end++
	}
	if start == end {
		return
	}
	tab.Anchor = editor.Position{Line: p.Line, Col: start}
	tab.Cursor = editor.Position{Line: p.Line, Col: end}
}

// isWordChar reports whether r is part of a "word" for double-click select.
// Intentionally simple ASCII-ish definition; covers the common cases.
func isWordChar(r rune) bool {
	return r == '_' ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9')
}

// -----------------------------------------------------------------------------
// Tab + clipboard actions
// -----------------------------------------------------------------------------

// activeTabPtr returns the currently active *editor.Tab, or nil when there
// are no tabs open.
func (a *App) activeTabPtr() *editor.Tab {
	if a.activeTab < 0 || a.activeTab >= len(a.tabs) {
		return nil
	}
	return a.tabs[a.activeTab]
}

// flash sets a transient status message that displays for statusFlashFor
// before the status bar reverts to the active file's info.
func (a *App) flash(msg string) {
	a.statusMsg = msg
	a.statusUntil = time.Now().Add(statusFlashFor)
}

// externalOpenPolicy controls whether displaying an external file also hands
// it to the host default application.
type externalOpenPolicy uint8

const (
	externalOpenImmediate externalOpenPolicy = iota
	externalOpenDisplayOnly
)

// OpenFile opens the file at path in a new tab — or switches to it if
// it is already open. Exported so main.go can seed the editor with the
// file the user named on the command line ("spiceedit foo.go"). Thin
// wrapper around openFile so internal callers keep using the lowercase
// name and the public surface stays small.
func (a *App) OpenFile(path string) { a.openFile(path) }

// openFile opens the file at path in a new tab — or switches to it if it is
// already open in another tab. Errors are surfaced as a flash message.
// Whatever the path resolves to, its parent becomes the active folder so
// the next New File from the main menu lands next to it.
func (a *App) openFile(path string) {
	a.openFileWithExternalPolicy(path, externalOpenImmediate)
}

// openFileWithExternalPolicy shares classification and tab handling while
// letting tree clicks display external files without launching another app.
func (a *App) openFileWithExternalPolicy(path string, policy externalOpenPolicy) {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	a.setActiveFolder(filepath.Dir(path))
	if a.tree != nil {
		a.tree.ActiveFile = path
		a.tree.SelectedPath = path
		// Reveal the file's location in the sidebar: expand every ancestor
		// directory and scroll the row into view. Without this, opening a
		// file via the finder (Esc-p) or the command line leaves the tree
		// collapsed at the top, so the active-file highlight is set on a
		// row nobody can see. listH mirrors Render's own list-area height
		// (sidebarH - 2) so the "already visible" guard inside Reveal uses
		// the same viewport the next paint will.
		_, _, _, sh := a.sidebarRect()
		listH := sh - 2
		if listH < 0 {
			listH = 0
		}
		a.tree.Reveal(path, listH)
	}
	for i, t := range a.tabs {
		if t.Path != path {
			continue
		}
		a.activeTab = i
		if t.IsExternal() {
			a.handleExternalFileOpen(t.Path, policy)
			return
		}
		if !t.IsReadOnly() {
			a.reopenTextTab(t)
		}
		return
	}
	t, err := editor.NewTab(path)
	if err != nil {
		a.flash(fmt.Sprintf("Error: %v", err))
		return
	}
	if t.IsExternal() {
		a.displayExternalTab(t)
		a.handleExternalFileOpen(t.Path, policy)
		return
	}
	a.tabs = append(a.tabs, t)
	a.activeTab = len(a.tabs) - 1
	if !t.IsReadOnly() {
		t.GitLines = loadGitLineChanges(a.rootDir, t.Path)
	}
	a.flash(fmt.Sprintf("Opened %s", filepath.Base(path)))
}

// displayExternalTab replaces the one shared external tab in place, or appends
// it when no unsupported file is currently represented in the tab bar.
func (a *App) displayExternalTab(tab *editor.Tab) {
	for i, existing := range a.tabs {
		if existing.IsExternal() {
			a.tabs[i] = tab
			a.activeTab = i
			return
		}
	}
	a.tabs = append(a.tabs, tab)
	a.activeTab = len(a.tabs) - 1
}

// handleExternalFileOpen launches only for deliberate non-tree open flows.
func (a *App) handleExternalFileOpen(path string, policy externalOpenPolicy) {
	if policy == externalOpenDisplayOnly {
		return
	}
	a.openPathWithDefaultApp(path)
}

// reopenTextTab reloads a clean existing text tab immediately and asks before
// replacing unsaved edits in a dirty tab.
func (a *App) reopenTextTab(tab *editor.Tab) {
	if tab.Dirty {
		name := tab.DisplayName()
		a.openConfirm(
			"Reload file from disk?",
			fmt.Sprintf("Reload %s and discard unsaved changes?", name),
			func(app *App) { app.reloadTextTabFromDisk(tab) },
		)
		return
	}
	a.reloadTextTabFromDisk(tab)
}

// reloadTextTabFromDisk replaces one text buffer with the latest disk contents
// and refreshes the Git gutter only after the read succeeds.
func (a *App) reloadTextTabFromDisk(tab *editor.Tab) {
	name := tab.DisplayName()
	if err := tab.Reload(); err != nil {
		a.flash(fmt.Sprintf("Could not reload %s: %v", name, err))
		return
	}
	tab.GitLines = loadGitLineChanges(a.rootDir, tab.Path)
	a.flash(fmt.Sprintf("Reloaded %s from disk", name))
}

// saveActiveTab writes the active tab's buffer to disk.
func (a *App) saveActiveTab() {
	a.saveTabAt(a.activeTab)
}

// saveTabAt saves the tab at idx. Returns true on success, false on
// any kind of failure (no tab, untitled, IO error). Failures flash a
// status message so the caller doesn't have to. Pulled out from
// saveActiveTab so the dirty-close modal can save a specific tab and
// branch on success — saving and then closing must not eat the user's
// work when the save itself failed.
func (a *App) saveTabAt(idx int) bool {
	if idx < 0 || idx >= len(a.tabs) {
		return false
	}
	tab := a.tabs[idx]
	if tab.Path == "" {
		a.flash("Saving untitled tabs is not supported yet")
		return false
	}
	if err := tab.Save(); err != nil {
		a.flash(fmt.Sprintf("Save failed: %v", err))
		return false
	}
	a.refreshGitStatus()
	a.flash(fmt.Sprintf("Saved %s", filepath.Base(tab.Path)))
	// Format-on-save runs after the disk write succeeds, so a broken
	// formatter never blocks the user's save from landing. The
	// formatter (when configured + trusted) reloads the buffer
	// asynchronously via formatDoneEvent — see format.go.
	a.runFormatOnSave(idx)
	return true
}

// saveAllDirty walks every open tab and saves each dirty one. Returns
// true when every dirty tab saved successfully — used by the quit flow
// to decide whether it's safe to actually exit. The first failure
// short-circuits because there's no point cascading more failed saves
// past one we've already flashed about, and the user needs to react to
// the first error before deciding what to do with the rest.
func (a *App) saveAllDirty() bool {
	for i, tab := range a.tabs {
		if !tab.Dirty {
			continue
		}
		if !a.saveTabAt(i) {
			return false
		}
	}
	return true
}

// dirtyTabCount returns the number of tabs with unsaved changes.
// Used by the quit flow to decide whether to skip the modal entirely.
func (a *App) dirtyTabCount() int {
	n := 0
	for _, tab := range a.tabs {
		if tab.Dirty {
			n++
		}
	}
	return n
}

// tabIndex returns target's current index, or -1 when it is no longer open.
func (a *App) tabIndex(target *editor.Tab) int {
	for i, tab := range a.tabs {
		if tab == target {
			return i
		}
	}
	return -1
}

// tabsExcept snapshots every open tab except target in current tab order.
func (a *App) tabsExcept(target *editor.Tab) []*editor.Tab {
	out := make([]*editor.Tab, 0, len(a.tabs))
	for _, tab := range a.tabs {
		if tab != target {
			out = append(out, tab)
		}
	}
	return out
}

// openTargetTabs deduplicates targets and returns only tabs still open,
// preserving the current tab order for deterministic saves and fallbacks.
func (a *App) openTargetTabs(targets []*editor.Tab) []*editor.Tab {
	wanted := tabPointerSet(targets)
	out := make([]*editor.Tab, 0, len(wanted))
	for _, tab := range a.tabs {
		if _, ok := wanted[tab]; ok {
			out = append(out, tab)
		}
	}
	return out
}

// tabPointerSet builds an identity set for target membership checks.
func tabPointerSet(tabs []*editor.Tab) map[*editor.Tab]struct{} {
	out := make(map[*editor.Tab]struct{}, len(tabs))
	for _, tab := range tabs {
		if tab != nil {
			out[tab] = struct{}{}
		}
	}
	return out
}

// dirtyTabs returns dirty tabs from tabs in the supplied order.
func dirtyTabs(tabs []*editor.Tab) []*editor.Tab {
	out := make([]*editor.Tab, 0, len(tabs))
	for _, tab := range tabs {
		if tab.Dirty {
			out = append(out, tab)
		}
	}
	return out
}

// saveDirtyTabs saves dirty targets in their current tab order. A failure
// stops later saves so the caller can keep the complete close set visible.
func (a *App) saveDirtyTabs(targets []*editor.Tab) bool {
	targeted := tabPointerSet(targets)
	for i, tab := range a.tabs {
		if _, ok := targeted[tab]; !ok || !tab.Dirty {
			continue
		}
		if !a.saveTabAt(i) {
			return false
		}
	}
	return true
}

// requestCloseTabs closes a captured target set. Dirty targets share one
// Save / Discard / Cancel prompt, and Save closes nothing unless all required
// writes succeed.
func (a *App) requestCloseTabs(targets []*editor.Tab) {
	targets = a.openTargetTabs(targets)
	if len(targets) == 0 {
		return
	}
	if len(targets) == 1 {
		a.requestCloseTab(a.tabIndex(targets[0]))
		return
	}

	dirty := dirtyTabs(targets)
	if len(dirty) == 0 {
		a.closeTabs(targets)
		return
	}
	message := fmt.Sprintf("%d files have unsaved changes.", len(dirty))
	if len(dirty) == 1 {
		message = dirty[0].DisplayName() + " has unsaved changes."
	}
	a.openDirtyClose(
		"Unsaved changes",
		message,
		func(app *App) {
			if app.saveDirtyTabs(targets) {
				app.closeTabs(targets)
			}
		},
		func(app *App) { app.closeTabs(targets) },
	)
}

// requestCloseTab closes the tab at idx. A clean tab closes immediately;
// a dirty tab opens the unsaved-changes modal so the user can pick
// Save / Discard / Cancel. The Save path saves the buffer first and only
// closes the tab on success — a save error would otherwise silently lose
// the user's work.
func (a *App) requestCloseTab(idx int) {
	if idx < 0 || idx >= len(a.tabs) {
		return
	}
	target := a.tabs[idx]
	if !target.Dirty {
		a.closeTabs([]*editor.Tab{target})
		return
	}
	a.openDirtyClose(
		"Unsaved changes",
		target.DisplayName()+" has unsaved changes.",
		func(app *App) {
			// Save → close. saveTabAt flashes its own error, in which
			// case we keep the tab around so the user can react.
			idx := app.tabIndex(target)
			if idx >= 0 && app.saveTabAt(idx) {
				app.closeTabs([]*editor.Tab{target})
			}
		},
		func(app *App) { app.closeTabs([]*editor.Tab{target}) },
	)
}

// closeTab removes the tab at idx without any dirty-check.
func (a *App) closeTab(idx int) {
	if idx < 0 || idx >= len(a.tabs) {
		return
	}
	a.closeTabs([]*editor.Tab{a.tabs[idx]})
}

// closeTabs removes a target set in one state transition. A surviving active
// tab stays active by identity; otherwise the nearest right survivor wins,
// falling back to the nearest left survivor.
func (a *App) closeTabs(targets []*editor.Tab) {
	targets = a.openTargetTabs(targets)
	if len(targets) == 0 {
		return
	}
	closing := tabPointerSet(targets)
	active := a.activeTabPtr()
	oldActive := a.activeTab

	var fallback *editor.Tab
	if _, activeCloses := closing[active]; activeCloses {
		for i := oldActive + 1; i < len(a.tabs); i++ {
			if _, closes := closing[a.tabs[i]]; !closes {
				fallback = a.tabs[i]
				break
			}
		}
		if fallback == nil {
			for i := oldActive - 1; i >= 0; i-- {
				if _, closes := closing[a.tabs[i]]; !closes {
					fallback = a.tabs[i]
					break
				}
			}
		}
	}

	survivors := make([]*editor.Tab, 0, len(a.tabs)-len(targets))
	for _, tab := range a.tabs {
		if _, closes := closing[tab]; !closes {
			survivors = append(survivors, tab)
		}
	}
	a.tabs = survivors
	switch {
	case len(a.tabs) == 0:
		a.activeTab = 0
	case active != nil && a.tabIndex(active) >= 0:
		a.activeTab = a.tabIndex(active)
	case fallback != nil:
		a.activeTab = a.tabIndex(fallback)
	default:
		a.activeTab = 0
	}
	a.syncActiveTreeFile()
}

// copySelection puts the active tab's selection on the system clipboard
// (via OSC 52) and into the editor's internal clipboard.
func (a *App) copySelection() {
	tab := a.activeTabPtr()
	if tab == nil || !tab.HasSelection() {
		return
	}
	txt := tab.SelectionText()
	a.clipBuf = txt
	copyToSystem := a.copyToSystem
	if copyToSystem == nil {
		copyToSystem = clipboard.CopyToSystem
	}
	if err := copyToSystem(txt); err != nil {
		a.flash("Copied (system clipboard unavailable)")
		return
	}
	a.flash("Copied")
}

// autoCopyMouseSelection publishes a completed editable mouse selection once.
func (a *App) autoCopyMouseSelection() {
	tab := a.activeTabPtr()
	if tab == nil || tab.IsReadOnly() || !tab.HasSelection() {
		return
	}
	a.copySelection()
}

// cutSelection copies the selection then deletes it.
func (a *App) cutSelection() {
	tab := a.activeTabPtr()
	if tab == nil || !tab.HasSelection() {
		return
	}
	a.copySelection()
	tab.DeleteSelection()
}

// pasteClipboard reads the system clipboard and inserts it at the cursor.
// Explicit paste does not fall back to the editor's internal copy buffer:
// failure is surfaced to the user so they do not paste stale selection text.
func (a *App) pasteClipboard() {
	if !a.hasEditableTextTab() {
		return
	}
	readFromSystem := a.readFromSystem
	if readFromSystem == nil {
		readFromSystem = clipboard.ReadSystem
	}
	text, err := readFromSystem()
	if err != nil {
		a.flash("System clipboard unavailable")
		return
	}
	a.insertPastedText(text)
}

// normalizePastedText maps terminal and clipboard newline variants to LF so
// Buffer.InsertString receives the same text regardless of paste origin.
func normalizePastedText(text string) string {
	return strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(text)
}

// normalizeSingleLinePaste converts every pasted line break to a space so a
// terminal paste cannot submit or activate a single-line modal control.
func normalizeSingleLinePaste(text string) string {
	return strings.ReplaceAll(normalizePastedText(text), "\n", " ")
}

// normalizeSidebarPathPrefixPaste removes terminal line endings while
// preserving meaningful spaces in a path prefix and flattening internal lines.
func normalizeSidebarPathPrefixPaste(text string) string {
	text = normalizePastedText(text)
	text = strings.TrimRight(text, "\n")
	return strings.ReplaceAll(text, "\n", " ")
}

// insertPastedRunes inserts text at a rune cursor and returns the updated
// value and cursor. It is shared by every single-line text input modal.
func insertPastedRunes(value []rune, cursor int, text string) ([]rune, int) {
	if text == "" {
		return value, cursor
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(value) {
		cursor = len(value)
	}
	pasted := []rune(text)
	next := make([]rune, 0, len(value)+len(pasted))
	next = append(next, value[:cursor]...)
	next = append(next, pasted...)
	next = append(next, value[cursor:]...)
	return next, cursor + len(pasted)
}

// routePastedText sends bracketed paste to the active single-line text state.
// Non-text overlays absorb the payload so the background editor cannot change.
func (a *App) routePastedText(text string) {
	if a.promptOpen {
		text = normalizeSingleLinePaste(text)
		a.promptValue, a.promptCursor = insertPastedRunes(a.promptValue, a.promptCursor, text)
		return
	}
	if a.confirmOpen || a.dirtyOpen {
		return
	}
	if a.formOpen {
		if a.formFocus >= 0 && a.formFocus < len(a.formPrompts) && a.formPrompts[a.formFocus].Type == customactions.PromptText {
			text = normalizeSingleLinePaste(text)
			i := a.formFocus
			a.formText[i], a.formCursor[i] = insertPastedRunes(a.formText[i], a.formCursor[i], text)
			a.formValues[a.formPrompts[i].Key] = string(a.formText[i])
		}
		return
	}
	if a.contextOpen {
		return
	}
	if a.findOpen {
		text = normalizeSingleLinePaste(text)
		a.findValue, a.findCursor = insertPastedRunes(a.findValue, a.findCursor, text)
		a.findApplyQuery()
		return
	}
	if a.finderOpen {
		text = normalizeSingleLinePaste(text)
		a.finderQuery, a.finderCursor = insertPastedRunes(a.finderQuery, a.finderCursor, text)
		a.finderSelected = 0
		a.refreshFinderResults()
		return
	}
	if a.menuOpen {
		return
	}
	if a.sidebarPathPrefixFocused {
		text = normalizeSidebarPathPrefixPaste(text)
		if text == "" {
			return
		}
		a.sidebarPathPrefix, a.sidebarPathPrefixCursor = insertPastedRunes(
			a.sidebarPathPrefix, a.sidebarPathPrefixCursor, text)
		a.copyCurrentSidebarPath(false)
		return
	}
	a.insertPastedText(text)
}

// insertPastedText normalizes text and inserts it as one structural editor
// operation when the active tab is writable.
func (a *App) insertPastedText(text string) {
	tab := a.activeTabPtr()
	if tab == nil || tab.IsReadOnly() {
		return
	}
	text = normalizePastedText(text)
	if text == "" {
		return
	}
	tab.InsertString(text)
}

// -----------------------------------------------------------------------------
// Action menu
// -----------------------------------------------------------------------------

// openMenu shows the action modal. While it is up, the editor doesn't
// receive typed keys, and clicks outside the modal dismiss it. We pre-
// select the first enabled row so Down/Up/Enter keyboard navigation has
// somewhere sensible to start.
func (a *App) openMenu() {
	a.closeAllModals()
	a.menuOpen = true
	a.menuMoveSelection(1)
}

// menuMoveSelection advances hoveredMenuRow to the next (dir=+1) or
// previous (dir=-1) enabled menu item, wrapping around at the ends so the
// list feels continuous. Disabled items and dividers are skipped. If no
// item is currently enabled hoveredMenuRow stays -1.
func (a *App) menuMoveSelection(dir int) {
	items, _, _ := a.menuLayout()
	n := len(items)
	if n == 0 {
		return
	}
	start := a.hoveredMenuRow
	if start < 0 {
		// No current selection — start one step before the first row (for
		// Down) or one past the last (for Up) so the loop lands on the
		// first/last enabled item.
		if dir > 0 {
			start = -1
		} else {
			start = n
		}
	}
	for i := 1; i <= n; i++ {
		idx := ((start+dir*i)%n + n) % n
		if items[idx].enabled(a) {
			a.hoveredMenuRow = idx
			return
		}
	}
	a.hoveredMenuRow = -1
}

// menuActivate runs the currently-highlighted menu item, if any. It's the
// keyboard-Enter equivalent of clicking a row.
func (a *App) menuActivate() {
	items, _, _ := a.menuLayout()
	if a.hoveredMenuRow < 0 || a.hoveredMenuRow >= len(items) {
		return
	}
	item := items[a.hoveredMenuRow]
	if !item.enabled(a) {
		return
	}
	item.action(a)
}

// closeMenu hides the action modal without running any action.
func (a *App) closeMenu() {
	a.menuOpen = false
	a.hoveredMenuRow = -1
}

// updateMenuHover sets hoveredMenuRow to the index of the enabled menu row
// at (x, y), or to -1 when the mouse is over a disabled row, a divider, the
// title, or anywhere outside the modal.
func (a *App) updateMenuHover(x, y int) {
	a.hoveredMenuRow = -1
	mx, my, mw, mh := a.menuModalRect()
	if x < mx || x >= mx+mw || y < my || y >= my+mh {
		return
	}
	relY := y - my
	items, _, _ := a.menuLayout()
	for i, item := range items {
		if item.relY == relY && item.enabled(a) {
			a.hoveredMenuRow = i
			return
		}
	}
}

// hasTab reports whether there is an active tab to act on.
func (a *App) hasTab() bool { return a.activeTabPtr() != nil }

// hasOtherTabs reports whether closing every tab except the active one would
// remove at least one tab.
func (a *App) hasOtherTabs() bool {
	return a.activeTabPtr() != nil && len(a.tabs) > 1
}

// hasSavableTab reports whether the active tab is one we can persist —
// it must exist, have a path on disk, and expose editable text. Used by Save
// and Save & Close.
func (a *App) hasSavableTab() bool {
	t := a.activeTabPtr()
	return t != nil && t.Path != "" && !t.IsReadOnly()
}

// hasFileTab reports whether the active tab is backed by a real file (text or
// image). It deliberately remains an active-tab predicate; main-menu file
// actions use hasFileActionTarget to include explicit tree selections.
func (a *App) hasFileTab() bool {
	t := a.activeTabPtr()
	return t != nil && t.Path != ""
}

// projectRootPath returns the absolute project root used for filesystem
// comparisons. In project mode the tree root is already absolute and remains
// authoritative when App.rootDir retains user input such as ".".
func (a *App) projectRootPath() string {
	root := a.rootDir
	if a.tree != nil && a.tree.Root != nil && a.tree.Root.Path != "" {
		root = a.tree.Root.Path
	}
	if abs, err := filepath.Abs(root); err == nil {
		return abs
	}
	return root
}

// fileActionTarget resolves the file used by the main menu's file actions.
// An explicit tree selection is authoritative: only an existing non-directory
// file may be used, and an invalid selection must not redirect an action to an
// unrelated active tab. Without a selection (or without a tree), preserve the
// active file-backed tab behavior used in single-file mode.
func (a *App) fileActionTarget() (string, bool) {
	if a.tree != nil && a.tree.SelectedPath != "" {
		path := a.tree.SelectedPath
		info, err := os.Lstat(path)
		if err != nil || info.IsDir() {
			return "", false
		}
		return path, true
	}
	tab := a.activeTabPtr()
	if tab == nil || tab.Path == "" {
		return "", false
	}
	return tab.Path, true
}

// hasFileActionTarget reports whether a main-menu file action has a valid
// target. It intentionally differs from hasFileTab, which retains its
// established active-tab-only meaning for callers that need that distinction.
func (a *App) hasFileActionTarget() bool {
	_, ok := a.fileActionTarget()
	return ok
}

// pathActionTarget resolves the filesystem node used by main-menu path-copy
// actions. An explicit tree selection is authoritative and may be any node
// Lstat can see, including directories, roots, and symlinks. When there is no
// selection (or tree), retain the active file-backed tab fallback.
func (a *App) pathActionTarget() (string, bool) {
	if a.tree != nil && a.tree.SelectedPath != "" {
		path := a.tree.SelectedPath
		if _, err := os.Lstat(path); err != nil {
			return "", false
		}
		return path, true
	}
	return a.fileActionTarget()
}

// hasPathActionTarget reports whether a main-menu path-copy action has a
// valid target. It differs from hasFileActionTarget because path copying also
// accepts explicitly selected directories and symlinks.
func (a *App) hasPathActionTarget() bool {
	_, ok := a.pathActionTarget()
	return ok
}

// folderActionTarget resolves the folder used by the main menu's folder
// actions. An explicit tree selection is authoritative just as it is for
// files. Lstat deliberately classifies directory symlinks as file targets so
// deleting one removes the link rather than traversing its destination.
func (a *App) folderActionTarget() (string, bool) {
	folder := a.activeFolder
	if a.tree != nil && a.tree.SelectedPath != "" {
		folder = a.tree.SelectedPath
	}
	if folder == "" {
		return "", false
	}
	absFolder, err := filepath.Abs(folder)
	if err != nil || absFolder == a.projectRootPath() {
		return "", false
	}
	info, err := os.Lstat(absFolder)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return absFolder, true
}

// hasSelection reports whether the active tab has a non-empty selection.
func (a *App) hasSelection() bool {
	t := a.activeTabPtr()
	return t != nil && t.HasSelection()
}

// hasEditableTextTab reports whether the active tab accepts text editing.
func (a *App) hasEditableTextTab() bool {
	tab := a.activeTabPtr()
	return tab != nil && !tab.IsReadOnly()
}

// hasPasteTarget reports whether the active tab accepts explicit paste.
func (a *App) hasPasteTarget() bool {
	return a.hasEditableTextTab()
}

// hasCommentableTab reports whether the active tab is editable text with a
// known single-line comment marker.
func (a *App) hasCommentableTab() bool {
	t := a.activeTabPtr()
	if t == nil || t.IsReadOnly() {
		return false
	}
	_, ok := editor.LineCommentPrefix(t.Path)
	return ok
}

// hasClipboard reports whether the editor's internal clipboard has copied
// selection text.
func (a *App) hasClipboard() bool { return a.clipBuf != "" }

// hasUndo reports whether the active tab has anything to undo. Used to
// enable / disable the Undo row in the action menu.
func (a *App) hasUndo() bool {
	t := a.activeTabPtr()
	return t != nil && t.CanUndo()
}

// hasRedo reports whether the active tab has anything to redo.
func (a *App) hasRedo() bool {
	t := a.activeTabPtr()
	return t != nil && t.CanRedo()
}

// hasRevert reports whether the active tab differs from its on-open
// (or last-reload) baseline — i.e. there is something to revert.
func (a *App) hasRevert() bool {
	t := a.activeTabPtr()
	return t != nil && t.CanRevert()
}

// menuUndo rolls the active tab back one undo step.
func (a *App) menuUndo() {
	a.closeMenu()
	t := a.activeTabPtr()
	if t == nil {
		return
	}
	if !t.Undo() {
		a.flash("Nothing to undo")
	}
}

// menuRedo re-applies the most recently undone step.
func (a *App) menuRedo() {
	a.closeMenu()
	t := a.activeTabPtr()
	if t == nil {
		return
	}
	if !t.Redo() {
		a.flash("Nothing to redo")
	}
}

// menuRevert rewinds the active tab all the way back to the buffer
// state we captured the moment the file was opened (or last reloaded).
// The pre-revert state goes onto the undo stack so an accidental click
// is recoverable with one Undo.
func (a *App) menuRevert() {
	a.closeMenu()
	t := a.activeTabPtr()
	if t == nil {
		return
	}
	if !t.RevertFile() {
		a.flash("File matches its on-open state — nothing to revert")
		return
	}
	a.flash("Reverted to on-open state — Undo to recover")
}

// runCustomAction executes the custom action at idx. When the action
// declares prompts, the form modal opens first and the shell command
// runs only after the user submits — values are exported as KEY=VALUE
// env vars named after each prompt's Key. When prompts is empty the
// command runs immediately (the historical behaviour) and the action
// requires an open tab so $FILE / $FILENAME aren't blank.
//
// Either path runs in a goroutine so a slow scp / ssh can't freeze
// the UI; completion fires a customActionDoneEvent the main loop
// turns into a flash on success or an info modal on failure.
func (a *App) runCustomAction(idx int) {
	a.closeMenu()
	if idx < 0 || idx >= len(a.customActions) {
		return
	}
	act := a.customActions[idx]

	// No "is a file open?" guard: custom actions are user-defined
	// shell, and we don't second-guess what their command line
	// needs. A $FILE-dependent command run without a tab open will
	// fail with a real error and route through the info modal,
	// which is more honest than disabling actions like
	// "brew upgrade ..." that don't touch FILE at all.
	if len(act.Prompts) == 0 {
		a.execCustomAction(act, nil)
		return
	}

	a.openForm(act.Label, act.Prompts, func(app *App, values map[string]string) {
		app.execCustomAction(act, values)
	})
}

// execCustomAction is the actual shell-out. Pulled out of
// runCustomAction so both the prompt-less and prompted paths share
// the env-var, logging, and event-posting wiring without diverging.
func (a *App) execCustomAction(act customactions.Action, promptValues map[string]string) {
	vars := a.captureActionVars()
	env := append(os.Environ(), vars.envSlice()...)
	env = append(env, promptValuesEnv(act.Prompts, promptValues)...)

	a.flash(act.Label + "…")
	scr := a.screen
	go func() {
		started := time.Now()
		cmd := exec.Command("sh", "-c", act.Command)
		cmd.Env = env
		out, runErr := cmd.CombinedOutput()
		duration := time.Since(started)

		// Always log — success or failure — so the user can scroll
		// back through actions.log when something goes sideways.
		// Best-effort: a log-write failure must not eat the action's
		// real error.
		_ = customactions.AppendLog(customactions.LogPath(), customactions.RunRecord{
			Time:     started,
			Duration: duration,
			Label:    act.Label,
			Command:  act.Command,
			File:     vars.File,
			Filename: vars.Filename,
			ExitErr:  runErr,
			Output:   out,
		})

		_ = scr.PostEvent(&customActionDoneEvent{
			when:   time.Now(),
			label:  act.Label,
			err:    runErr,
			output: out,
		})
	}()
}

// menuSave runs the Save action and dismisses the menu.
func (a *App) menuSave() {
	a.closeMenu()
	a.saveActiveTab()
}

// menuSaveAndClose saves the active tab and then closes it. If the save
// fails the close is aborted so we don't lose the user's edits.
func (a *App) menuSaveAndClose() {
	a.closeMenu()
	tab := a.activeTabPtr()
	if tab == nil || tab.Path == "" {
		return
	}
	if err := tab.Save(); err != nil {
		a.flash(fmt.Sprintf("Save failed: %v", err))
		return
	}
	a.refreshGitStatus()
	a.flash(fmt.Sprintf("Saved %s — closed", filepath.Base(tab.Path)))
	a.closeTab(a.activeTab)
}

// menuClose closes the active tab via the same dirty-tab confirmation flow
// used by clicking the × on the tab.
func (a *App) menuClose() {
	a.closeMenu()
	a.requestCloseTab(a.activeTab)
}

// menuCloseOtherTabs closes every tab except the active tab.
func (a *App) menuCloseOtherTabs() {
	a.closeMenu()
	target := a.activeTabPtr()
	if target == nil {
		return
	}
	a.requestCloseTabs(a.tabsExcept(target))
}

// menuCloseAllTabs closes every open tab through the shared batch flow.
func (a *App) menuCloseAllTabs() {
	a.closeMenu()
	a.requestCloseTabs(append([]*editor.Tab(nil), a.tabs...))
}

// menuCopy copies the current selection.
func (a *App) menuCopy() {
	a.closeMenu()
	a.copySelection()
}

// menuCut cuts the current selection.
func (a *App) menuCut() {
	a.closeMenu()
	a.cutSelection()
}

// menuPaste pastes the current system clipboard text at the cursor.
func (a *App) menuPaste() {
	a.closeMenu()
	a.pasteClipboard()
}

// menuSelectAll selects the full active editable text buffer.
func (a *App) menuSelectAll() {
	a.closeMenu()
	tab := a.activeTabPtr()
	if tab == nil || tab.IsReadOnly() {
		return
	}
	tab.SelectAll()
}

// menuDeleteCurrentLine removes the line under the cursor or every line
// touched by the active selection.
func (a *App) menuDeleteCurrentLine() {
	a.closeMenu()
	if tab := a.activeTabPtr(); tab != nil {
		tab.DeleteCurrentLines()
	}
}

// menuToggleLineComment comments or uncomments the active line selection.
func (a *App) menuToggleLineComment() {
	a.closeMenu()
	tab := a.activeTabPtr()
	if tab == nil || tab.IsReadOnly() {
		return
	}
	changed, ok := tab.ToggleLineComment()
	if !ok {
		a.flash("No line comment syntax for this file")
		return
	}
	if !changed {
		a.flash("No non-blank lines to comment")
		return
	}
	a.flash("Toggled line comment")
}

// menuOpenWithDefaultApp opens the resolved file target through the host
// operating system without saving or otherwise mutating editor state.
func (a *App) menuOpenWithDefaultApp() {
	a.closeMenu()
	target, ok := a.fileActionTarget()
	if !ok {
		a.flash("No file to open with default app")
		return
	}
	a.openPathWithDefaultApp(target)
}

// menuShowInFileManager selects the active file in the host file manager or
// opens the project root when no file-backed tab is active.
func (a *App) menuShowInFileManager() {
	a.closeMenu()
	a.showInFileManager()
}

// menuRefreshTree forces an immediate sidebar reload. Currently unwired
// from the menu — the 10s background poller covers the common case — but
// the method is kept so re-adding the menu row (see menuItems) only
// requires uncommenting one line.
func (a *App) menuRefreshTree() {
	a.closeMenu()
	a.refreshTree()
	a.flash("File tree refreshed")
}

// menuToggleSidebar shows or hides the file explorer panel. The editor and
// tab bar reflow to fill the freed cells when the panel is hidden, and
// snap back when it returns.
func (a *App) menuToggleSidebar() {
	a.closeMenu()
	// Single-file mode has no file tree, so there's nothing to show or
	// hide. The menu row is hidden (hasTree), but the Esc-t leader reaches
	// here directly — guard it so the toggle can't flip sidebarShown true
	// and send draw() into a.tree.Render on a nil tree.
	if a.tree == nil {
		a.flash("No file explorer in single-file mode")
		return
	}
	a.sidebarShown = !a.sidebarShown
}

// menuToggleSoftWrap flips the session-level text wrapping preference. When
// enabled, horizontal scroll is reset because wrapped text and horizontal
// offset are mutually exclusive views of the same long logical lines.
func (a *App) menuToggleSoftWrap() {
	a.closeMenu()
	a.softWrap = !a.softWrap
	if a.softWrap {
		for _, tab := range a.tabs {
			if tab != nil && !tab.IsReadOnly() {
				tab.ScrollX = 0
			}
		}
		a.flash("Soft wrap enabled")
		return
	}
	a.flash("Soft wrap disabled")
}

// softWrapToggleLabel returns the dynamic label for the soft-wrap menu row.
func (a *App) softWrapToggleLabel() string {
	if a.softWrap {
		return "Disable soft wrap"
	}
	return "Enable soft wrap"
}

// sidebarToggleLabel returns the label the toggle row should display given
// the current sidebar state. Drawn dynamically by drawMenu.
func (a *App) sidebarToggleLabel() string {
	if a.sidebarShown {
		return "Hide file explorer"
	}
	return "Show file explorer"
}

// menuQuit exits the editor. When any tab has unsaved changes, opens the
// dirty-close modal so the user can pick Save (save all then quit),
// Discard (quit anyway), or Cancel. With no dirty tabs we exit straight
// away.
func (a *App) menuQuit() {
	a.closeMenu()
	dirty := a.dirtyTabCount()
	if dirty == 0 {
		a.quit = true
		return
	}
	var message string
	if dirty == 1 {
		// Find the one dirty tab so we can name it in the modal.
		for _, tab := range a.tabs {
			if tab.Dirty {
				name := filepath.Base(tab.Path)
				if name == "" || name == "." {
					name = "untitled"
				}
				message = name + " has unsaved changes. Save before quitting?"
				break
			}
		}
	} else {
		message = fmt.Sprintf("%d files have unsaved changes. Save all before quitting?", dirty)
	}
	a.openDirtyClose(
		"Unsaved changes",
		message,
		func(app *App) {
			// Only quit if every save succeeded — a half-saved exit
			// would silently lose work on whichever tab failed.
			if app.saveAllDirty() {
				app.quit = true
			}
		},
		func(app *App) { app.quit = true },
	)
}

// -----------------------------------------------------------------------------
// Drawing
// -----------------------------------------------------------------------------

// draw paints the entire screen. Called once per event in the main loop.
// The action modal — if open — is drawn last so it sits on top of everything.
func (a *App) draw() {
	a.screen.Clear()

	if a.width < minWidth || a.height < minHeight {
		a.drawTooSmall()
		return
	}

	if a.sidebarShown && a.tree != nil {
		sx, sy, sw, sh := a.sidebarRect()
		a.tree.Render(a.screen, a.theme, sx, sy, sw, sh)
		a.drawSplitter()
	}

	a.drawTabBar()

	if tab := a.activeTabPtr(); tab != nil {
		ex, ey, ew, eh := a.editorRect()
		if tab.IsImage() {
			if _, visible := a.visibleImageRequest(); visible {
				tab.RenderImageBackground(a.screen, a.theme, ex, ey, ew, eh)
			} else {
				tab.Render(a.screen, a.theme, ex, ey, ew, eh)
			}
		} else {
			tab.RenderSoftWrap(a.screen, a.theme, ex, ey, ew, eh, a.softWrap)
		}
	} else {
		a.drawEmptyEditor()
	}

	// Editor rendering owns the normal caret, so repaint PATH INFO afterward
	// to restore its input caret when that field has focus. Find and modal
	// overlays still draw later and retain final focus precedence.
	if a.sidebarShown && a.tree != nil {
		a.drawSidebarPath()
	}

	if a.findOpen {
		a.drawFindBar()
	}
	a.drawStatusBar()

	// Modal layering, bottom-up. Only one of these is open at a time
	// (closeAllModals enforces it), but the order still matters so a
	// future contributor can't accidentally double-open them.
	if a.menuOpen {
		a.drawMenu()
	}
	if a.contextOpen {
		a.drawContext()
	}
	if a.promptOpen {
		a.drawPrompt()
	}
	if a.confirmOpen {
		a.drawConfirm()
	}
	if a.dirtyOpen {
		a.drawDirtyClose()
	}
	if a.formOpen {
		a.drawForm()
	}
	if a.finderOpen {
		a.drawFinder()
	}
}

// iconsOn reports whether Nerd Font glyphs should render in places
// outside the file tree (e.g. the tab bar). The single source of
// truth is the file tree — App.loadSpiceConfig stamped the resolved
// auto/on/off decision onto t.IconsEnabled there, so consulting the
// tree keeps tabs and tree perfectly in sync (turning icons off via
// config.json hides them everywhere at once).
func (a *App) iconsOn() bool {
	return a.tree != nil && a.tree.IconsEnabled
}

// layoutTabs computes the tabRect geometry for every tab. Tabs are rendered
// to the right of the menu button, in the format:
//
//	" <dirty><icon? ><name> × " — a single space pad, two-cell dirty slot
//	(dot+space, or two spaces), an optional Nerd Font glyph + 1-space
//	separator (only when icons are enabled), the file name, a separator
//	space, the close ×, and a trailing space.
func (a *App) layoutTabs() []tabRect {
	out := make([]tabRect, 0, len(a.tabs))
	cursor := a.sidebarW() + menuButtonWidth
	for i, t := range a.tabs {
		nameWidth := termtext.StringWidth(t.DisplayName())
		iconWidth := 0
		if a.iconsOn() {
			iconWidth = termtext.StringWidth(icons.For(t.DisplayName(), false, false)) + 1
		}
		w := 1 + 2 + iconWidth + nameWidth + 1 + 1 + 1 // pad+dirty+icon?+name+space+×+pad
		out = append(out, tabRect{
			Index:  i,
			X:      cursor,
			Width:  w,
			CloseX: cursor + 1 + 2 + iconWidth + nameWidth + 1,
		})
		cursor += w
	}
	return out
}

// drawTabBar paints the tab bar across the top of the editor area: first
// the menu button (≡), then any open tabs.
func (a *App) drawTabBar() {
	tx, ty, tw, _ := a.tabBarRect()
	barStyle := tcell.StyleDefault.Background(a.theme.SidebarBG).Foreground(a.theme.Muted)
	for cx := tx; cx < tx+tw; cx++ {
		a.screen.SetContent(cx, ty, ' ', nil, barStyle)
	}

	a.drawMenuButton()

	rects := a.layoutTabs()
	a.lastTabRects = rects
	for _, r := range rects {
		active := r.Index == a.activeTab
		bg := a.theme.SidebarBG
		fg := a.theme.Muted
		if active {
			bg = a.theme.BG
			fg = a.theme.Text
		}
		st := tcell.StyleDefault.Background(bg).Foreground(fg)
		if active {
			st = st.Bold(true)
		}
		// Background.
		for cx := r.X; cx < r.X+r.Width; cx++ {
			if cx >= tx+tw {
				break
			}
			a.screen.SetContent(cx, ty, ' ', nil, st)
		}
		tab := a.tabs[r.Index]
		col := r.X + 1
		if tab.Dirty {
			a.screen.SetContent(col, ty, '●', nil, st.Foreground(a.theme.Modified))
		}
		col += 2 // skip dirty slot.
		// Per-language Nerd Font glyph between the dirty dot and the
		// filename — only when icons are enabled. Coloured the same
		// way the file tree glyphs are (icons.ColorFor) so the eye
		// connects "this tab" to "that row in the tree" instantly.
		if a.iconsOn() {
			name := tab.DisplayName()
			glyph := icons.For(name, false, false)
			gfg := icons.ColorFor(name, false, fg)
			gst := tcell.StyleDefault.Background(bg).Foreground(gfg)
			if active {
				gst = gst.Bold(true)
			}
			if col < tx+tw {
				available := tx + tw - col
				col += termtext.DrawString(a.screen, col, ty, available, glyph, gst)
			}
			col++ // separator space after glyph
		}
		if col < tx+tw {
			available := r.CloseX - 1 - col
			if terminalAvailable := tx + tw - col; available > terminalAvailable {
				available = terminalAvailable
			}
			if available > 0 {
				col += termtext.DrawString(a.screen, col, ty, available, tab.DisplayName(), st)
			}
		}
		if r.CloseX < tx+tw {
			closeStyle := st.Foreground(a.theme.Muted)
			if active {
				closeStyle = st.Foreground(a.theme.Subtle)
			}
			a.screen.SetContent(r.CloseX, ty, '×', nil, closeStyle)
		}
	}
}

// drawSplitter paints a 1-column vertical line at the right edge of the
// sidebar. Idle it sits in Subtle grey; while the user is dragging it
// brightens to Accent so the active grab handle is unmistakable.
func (a *App) drawSplitter() {
	x := a.splitterX()
	if x < 0 {
		return
	}
	fg := a.theme.Subtle
	if a.dragMode == "sidebar" {
		fg = a.theme.Accent
	}
	style := tcell.StyleDefault.Background(a.theme.SidebarBG).Foreground(fg)
	for y := 0; y < a.height-1; y++ {
		a.screen.SetContent(x, y, '│', nil, style)
	}
}

// drawMenuButton paints the ≡ icon in the leftmost cells of the tab bar.
// It's deliberately big and accent-coloured so it reads as a button.
func (a *App) drawMenuButton() {
	mx, my, mw, _ := a.menuButtonRect()
	bg := a.theme.SidebarBG
	fg := a.theme.Accent
	if a.menuOpen {
		// Visually press the button while the menu is up.
		bg = a.theme.Accent
		fg = a.theme.BG
	}
	style := tcell.StyleDefault.Background(bg).Foreground(fg).Bold(true)
	for cx := mx; cx < mx+mw; cx++ {
		a.screen.SetContent(cx, my, ' ', nil, style)
	}
	// Center the ≡ glyph in the button's mw cells.
	a.screen.SetContent(mx+mw/2, my, '≡', nil, style)
}

// drawEmptyEditor paints the placeholder shown when no tabs are open.
func (a *App) drawEmptyEditor() {
	ex, ey, ew, eh := a.editorRect()
	bg := a.theme.BG
	muted := tcell.StyleDefault.Background(bg).Foreground(a.theme.Muted)
	bold := tcell.StyleDefault.Background(bg).Foreground(a.theme.Text).Bold(true)
	for cy := ey; cy < ey+eh; cy++ {
		for cx := ex; cx < ex+ew; cx++ {
			a.screen.SetContent(cx, cy, ' ', nil, muted)
		}
	}
	cy := ey + eh/2
	msg1 := "No file open"
	msg2 := "Click a file in the tree, or  ≡  for the menu"
	cx1 := ex + (ew-termtext.StringWidth(msg1))/2
	termtext.DrawString(a.screen, cx1, cy-1, ew, msg1, bold)
	cx2 := ex + (ew-termtext.StringWidth(msg2))/2
	termtext.DrawString(a.screen, cx2, cy+1, ew, msg2, muted)
	a.screen.HideCursor()
}

// drawStatusBar paints the bottom status bar.
func (a *App) drawStatusBar() {
	sx, sy, sw, _ := a.statusRect()
	bg := a.theme.StatusBG
	fg := a.theme.BG
	style := tcell.StyleDefault.Background(bg).Foreground(fg).Bold(true)
	for cx := sx; cx < sx+sw; cx++ {
		a.screen.SetContent(cx, sy, ' ', nil, style)
	}

	// Right-side text: current git branch when we're inside a repo. Drawn
	// first so the left-side text can be clipped against it and the two
	// pieces never overlap on a narrow window.
	var rightWidth int
	if a.gitBranch != "" {
		right := " " + a.gitBranch + " "
		rw := termtext.StringWidth(right)
		if rw < sw {
			drawAt(a.screen, sx+sw-rw, sy, right, style)
			rightWidth = rw
		}
	}

	// Left-side text: status flash, file info, or root dir.
	var left string
	if time.Now().Before(a.statusUntil) && a.statusMsg != "" {
		left = " " + a.statusMsg
	} else if tab := a.activeTabPtr(); tab != nil {
		if tab.IsExternal() {
			left = fmt.Sprintf(" EXTERNAL · %s", filepath.Base(tab.Path))
		} else if tab.IsImage() && tab.Image != nil {
			b := tab.Image.Bounds()
			left = fmt.Sprintf(" %s · %d×%d · %s",
				strings.ToUpper(tab.ImageFmt), b.Dx(), b.Dy(), filepath.Base(tab.Path))
		} else {
			lang := detectLangLabel(tab.Path)
			dirty := ""
			if tab.Dirty {
				dirty = " · ●"
			}
			left = fmt.Sprintf(" %s · Ln %d, Col %d · %d lines%s",
				lang, tab.Cursor.Line+1, tab.Cursor.Col+1, tab.Buffer.LineCount(), dirty)
		}
	} else {
		left = " " + filepath.Base(a.rootDir)
	}
	// One cell of breathing room between left and right text so they
	// don't visually butt up against each other on a tight terminal.
	leftMax := sw - rightWidth
	if rightWidth > 0 {
		leftMax--
	}
	if leftMax < 0 {
		leftMax = 0
	}
	drawStatusText(a.screen, sx, sy, leftMax, left, style)
}

// drawTooSmall paints a centred error message when the terminal window is
// smaller than the editor's minimum supported size.
func (a *App) drawTooSmall() {
	style := tcell.StyleDefault.Background(a.theme.BG).Foreground(a.theme.Error).Bold(true)
	for cy := 0; cy < a.height; cy++ {
		for cx := 0; cx < a.width; cx++ {
			a.screen.SetContent(cx, cy, ' ', nil,
				tcell.StyleDefault.Background(a.theme.BG))
		}
	}
	msg := "Window too small — please resize"
	cy := a.height / 2
	cx := (a.width - termtext.StringWidth(msg)) / 2
	if cx < 0 {
		cx = 0
	}
	termtext.DrawString(a.screen, cx, cy, a.width-cx, msg, style)
	a.screen.HideCursor()
}

// drawMenu renders the action modal centered in the window. The
// item / divider / height layout comes from menuLayout so adding
// custom actions or new built-in groups doesn't require touching this
// function.
func (a *App) drawMenu() {
	mx, my, mw, mh := a.menuModalRect()
	items, dividers, _ := a.menuLayout()

	bg := a.theme.LineHL
	bgStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Text)
	borderStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Subtle)
	titleStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Accent).Bold(true)
	mutedStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Muted)
	chevronStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.AccentSoft)

	// Fill the entire modal rect with the modal bg.
	for cy := my; cy < my+mh; cy++ {
		for cx := mx; cx < mx+mw; cx++ {
			a.screen.SetContent(cx, cy, ' ', nil, bgStyle)
		}
	}

	// Outer border.
	a.screen.SetContent(mx, my, '┌', nil, borderStyle)
	a.screen.SetContent(mx+mw-1, my, '┐', nil, borderStyle)
	a.screen.SetContent(mx, my+mh-1, '└', nil, borderStyle)
	a.screen.SetContent(mx+mw-1, my+mh-1, '┘', nil, borderStyle)
	for cx := mx + 1; cx < mx+mw-1; cx++ {
		a.screen.SetContent(cx, my, '─', nil, borderStyle)
		a.screen.SetContent(cx, my+mh-1, '─', nil, borderStyle)
	}
	for cy := my + 1; cy < my+mh-1; cy++ {
		a.screen.SetContent(mx, cy, '│', nil, borderStyle)
		a.screen.SetContent(mx+mw-1, cy, '│', nil, borderStyle)
	}

	// Horizontal dividers between action groups. The dy list comes from
	// menuLayout — including the always-on row under the title — so it
	// stays in sync with whatever rows are actually being drawn.
	for _, dy := range dividers {
		cy := my + dy
		a.screen.SetContent(mx, cy, '├', nil, borderStyle)
		a.screen.SetContent(mx+mw-1, cy, '┤', nil, borderStyle)
		for cx := mx + 1; cx < mx+mw-1; cx++ {
			a.screen.SetContent(cx, cy, '─', nil, borderStyle)
		}
	}

	// Title row: " Menu" on the left, "esc " on the right.
	drawAt(a.screen, mx+1, my+1, " Menu", titleStyle)
	hint := "esc "
	drawAt(a.screen, mx+mw-1-termtext.StringWidth(hint), my+1, hint, mutedStyle)

	// Version stamp baked into the bottom border, right-aligned. A small
	// pad of dashes is left between the version text and the corner so it
	// reads as part of the frame rather than a label awkwardly butted up
	// against the border.
	verLabel := " v" + version.Version + " "
	verLen := termtext.StringWidth(verLabel)
	verX := mx + mw - 2 - verLen
	if verX > mx+1 {
		drawAt(a.screen, verX, my+mh-1, verLabel, mutedStyle)
	}

	// Action rows. Hovered (enabled) rows get a tinted full-width
	// background so they read like a hovered button in a GUI menu.
	hoverBg := a.theme.Selection
	hoverStyle := tcell.StyleDefault.Background(hoverBg).Foreground(a.theme.Text).Bold(true)
	hoverChevStyle := tcell.StyleDefault.Background(hoverBg).Foreground(a.theme.AccentSoft).Bold(true)
	for i, item := range items {
		cy := my + item.relY
		enabled := item.enabled(a)
		hovered := enabled && i == a.hoveredMenuRow

		var labelStyle, chevStyle, shortcutStyle tcell.Style
		switch {
		case hovered:
			// Paint the row's interior with the hover background first.
			for cx := mx + 1; cx < mx+mw-1; cx++ {
				a.screen.SetContent(cx, cy, ' ', nil, hoverStyle)
			}
			labelStyle = hoverStyle
			chevStyle = hoverChevStyle
			shortcutStyle = tcell.StyleDefault.Background(hoverBg).Foreground(a.theme.Muted).Bold(true)
		case enabled:
			labelStyle = bgStyle
			chevStyle = chevronStyle
			shortcutStyle = mutedStyle
		default:
			labelStyle = mutedStyle
			chevStyle = mutedStyle
			shortcutStyle = mutedStyle
		}
		// Dynamic label (e.g. the file-explorer toggle row) takes precedence
		// over the static one when present.
		label := item.label
		if item.labelFor != nil {
			label = item.labelFor(a)
		}
		drawAt(a.screen, mx+2, cy, "▸", chevStyle)
		if item.shortcut == "" {
			drawAtWidth(a.screen, mx+4, cy, mw-6, label, labelStyle)
			continue
		}
		shortcutX := mx + mw - 2 - textWidth(item.shortcut)
		label = truncateText(label, shortcutX-(mx+4)-2)
		drawAt(a.screen, mx+4, cy, label, labelStyle)
		drawAt(a.screen, shortcutX, cy, item.shortcut, shortcutStyle)
	}

	a.screen.HideCursor()
}

// drawStatusText writes s left-aligned into the status bar at (x, y) with a
// max width of maxW cells. Truncates rather than wraps.
func drawStatusText(scr tcell.Screen, x, y, maxW int, s string, st tcell.Style) {
	termtext.DrawString(scr, x, y, maxW, s, st)
}

// drawAt writes s starting at (x, y) without bounds checking. Callers are
// expected to keep the string within the rectangle they're drawing into.
func drawAt(scr tcell.Screen, x, y int, s string, st tcell.Style) {
	termtext.DrawString(scr, x, y, termtext.StringWidth(s), s, st)
}

// drawAtWidth draws a whole-grapheme prefix of s in the explicitly bounded
// cell range beginning at x. Modal and menu callers use it to keep wide text
// from bleeding into borders, hints, or adjacent controls.
func drawAtWidth(scr tcell.Screen, x, y, maxW int, s string, st tcell.Style) int {
	return termtext.DrawString(scr, x, y, maxW, s, st)
}

// textWidth returns the number of terminal cells a string occupies.
func textWidth(s string) int {
	return termtext.StringWidth(s)
}

// truncateText shortens s to max visible cells, reserving the final cell for
// an ellipsis when truncation is needed.
func truncateText(s string, max int) string {
	return termtext.Truncate(s, max)
}

// detectLangLabel returns a short label for the active file's language —
// just the file extension, or "text" when there is no path or extension.
func detectLangLabel(path string) string {
	if path == "" {
		return "text"
	}
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	if ext == "" {
		return "text"
	}
	return ext
}
