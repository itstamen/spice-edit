# CJK Terminal Cell Width Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Render and interact with CJK and combining Unicode correctly across every SpiceEdit terminal UI surface.

**Architecture:** Add internal/termtext as the shared source of grapheme ranges, terminal-cell widths, bounded tcell drawing, visual-coordinate conversion, and truncation. Keep all editor and App state rune-indexed. Migrate the editor, file tree, text inputs, dynamic App labels, and Finder row styling to those shared rules.

**Tech Stack:** Go 1.24, tcell v2.13.9, uniseg v0.4.7, Go testing, SimulationScreen.

## Global Constraints

- Keep Buffer, cursor, selection, find matches, and undo history rune-indexed.
- Do not make graphemes indivisible editing operations.
- Add uniseg as a direct dependency.
- Never draw a partial wide grapheme.
- Do not add Ctrl shortcuts.
- Add same-package tests for each non-trivial source file.

---

## File Structure

| File | Responsibility |
| --- | --- |
| internal/termtext/termtext.go | Grapheme iteration, widths, visual positions, drawing, truncation. |
| internal/termtext/termtext_test.go | Cluster, clipping, and SimulationScreen tests. |
| internal/editor/indent.go | Tab-aware grapheme visual-column calculations. |
| internal/editor/tab.go | Grapheme-safe editor render, selection, find, and scrolling. |
| internal/filetree/filetree.go | Cell-width-aware tree row segments. |
| internal/app/textinput.go | Shared prompt, form, find, and Finder input renderer. |
| internal/app/{app,modals,formmodal,find,finder}.go | Dynamic UI text, geometry, and Finder result rows. |

### Task 1: Build the shared terminal text package

**Files:**

- Modify: go.mod
- Create: internal/termtext/termtext.go
- Create: internal/termtext/termtext_test.go

**Interfaces:**

~~~go
type Cluster struct {
    Text               string
    StartRune, EndRune int // EndRune is exclusive.
    Width              int
}

func Clusters(text string) []Cluster
func StringWidth(text string) int
func VisualCol(text string, runeIndex int) int
func RuneIndexAtVisual(text string, visualCol int) int
func RuneIndexAtOrAfterVisual(text string, visualCol int) int
func DrawCluster(scr tcell.Screen, x, y, remaining int, c Cluster, style tcell.Style) (used int, drawn bool)
func DrawString(scr tcell.Screen, x, y, remaining int, text string, style tcell.Style) int
func Truncate(text string, maxCells int) string
~~~

- [ ] **Step 1: Write failing cluster and draw tests.**

~~~go
func TestClusters_ReportRangesAndWidths(t *testing.T) {
    got := Clusters("中e\u0301か\u3099")
    want := []Cluster{
        {Text: "中", StartRune: 0, EndRune: 1, Width: 2},
        {Text: "e\u0301", StartRune: 1, EndRune: 3, Width: 1},
        {Text: "か\u3099", StartRune: 3, EndRune: 5, Width: 2},
    }
    if !reflect.DeepEqual(got, want) {
        t.Fatalf("clusters = %#v, want %#v", got, want)
    }
}

func TestDrawString_DoesNotSplitWideCluster(t *testing.T) {
    scr := newScreen(t, 3, 1)
    if used := DrawString(scr, 0, 0, 3, "中a", tcell.StyleDefault); used != 3 {
        t.Fatalf("used = %d, want 3", used)
    }
    text, _, width := scr.Get(0, 0)
    if text != "中" || width != 2 {
        t.Fatalf("cell = (%q, %d), want (中, 2)", text, width)
    }
}
~~~

- [ ] **Step 2: Confirm the test fails before the package exists.**

Run: go test ./internal/termtext

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Implement uniseg-backed clusters and tcell-backed drawing.**

~~~go
func Clusters(text string) []Cluster {
    out := make([]Cluster, 0)
    state, start := -1, 0
    for text != "" {
        part, rest, width, next := uniseg.FirstGraphemeClusterInString(text, state)
        if part == "" {
            break
        }
        end := start + utf8.RuneCountInString(part)
        out = append(out, Cluster{Text: part, StartRune: start, EndRune: end, Width: width})
        text, state, start = rest, next, end
    }
    return out
}

func DrawCluster(scr tcell.Screen, x, y, remaining int, c Cluster, style tcell.Style) (int, bool) {
    if c.Width > remaining {
        return 0, false
    }
    _, used := scr.Put(x, y, c.Text, style)
    return used, true
}
~~~

Implement coordinate conversion by walking Clusters. VisualCol returns the starting cell for an interior rune. RuneIndexAtVisual returns the containing cluster start. RuneIndexAtOrAfterVisual returns a valid next cluster boundary for scroll positioning. Truncate reserves one cell for an ellipsis and only keeps whole clusters.

- [ ] **Step 4: Add edge tests, format, tidy, test, and commit.**

Add a VisualCol test for a CJK glyph and a combining cluster, plus a one-cell truncation test for a wide glyph.

Run: gofmt -w internal/termtext/*.go && go mod tidy && go test ./internal/termtext

Expected: PASS.

~~~bash
git add go.mod go.sum internal/termtext
git commit -m "Add Unicode terminal text helpers"
~~~

### Task 2: Convert editor visual math and rendering

**Files:**

- Modify: internal/editor/indent.go
- Modify: internal/editor/indent_test.go
- Modify: internal/editor/tab.go
- Modify: internal/editor/tab_test.go

**Interfaces:**

- Consumes Cluster and DrawCluster from Task 1.
- Preserves RuneVisualWidth, LineVisualCol, RuneColAtVisual, Tab.Render, Tab.EnsureVisible, and Tab.HitTest signatures.
- Adds private runeColAtOrAfterVisual for horizontal scrolling.

- [ ] **Step 1: Add failing CJK column, hit-test, and render tests.**

~~~go
func TestLineVisualCol_UsesWideAndCombiningClusters(t *testing.T) {
    runes := []rune("a中e\u0301b")
    cases := []struct{ col, want int }{
        {0, 0}, {1, 1}, {2, 3}, {3, 3}, {4, 4}, {5, 5},
    }
    for _, tc := range cases {
        if got := LineVisualCol(runes, tc.col); got != tc.want {
            t.Errorf("LineVisualCol(%d) = %d, want %d", tc.col, got, tc.want)
        }
    }
}

func TestTab_Render_CJKDoesNotOverwriteTrailingCells(t *testing.T) {
    scr := newSimScreen(t, 40, 5)
    defer scr.Fini()
    tab, _ := NewTab("")
    tab.Buffer = NewBuffer("中文测试")
    tab.Render(scr, theme.Default(), 0, 0, 40, 5)
    contentX := defaultGutterWidth + 1
    text, _, width := scr.Get(contentX, 0)
    if text != "中" || width != 2 {
        t.Fatalf("first cell = (%q, %d)", text, width)
    }
}
~~~

- [ ] **Step 2: Confirm the tests fail under the one-rune-one-cell implementation.**

Run: go test ./internal/editor -run 'Test(LineVisualCol_UsesWide|Tab_Render_CJK)'

Expected: FAIL.

- [ ] **Step 3: Implement tab-aware grapheme spans.**

Treat a cluster whose text is a tab as a TabStop-dependent span. Use the cluster width for every other grapheme. A click in any cluster cell returns its StartRune. Keep public cursor and ScrollX fields rune-indexed.

- [ ] **Step 4: Replace Tab.Render's per-rune loop.**

~~~go
for _, cluster := range lineClusters(runes) {
    width := cluster.visualWidth(visualCol)
    screenCol := visualCol - scrollVisual
    style := t.clusterStyle(lineIdx, cluster, styles, selStart, selEnd, hasSel, lineBg)
    if cluster.Text == "\t" {
        drawTabCells(scr, contentX+screenCol, cy, width, contentW-screenCol, style)
    } else if screenCol >= 0 && screenCol+width <= contentW {
        termtext.DrawCluster(scr, contentX+screenCol, cy, contentW-screenCol, cluster.Cluster, style)
    }
    visualCol += width
}
~~~

clusterStyle starts with the syntax style for StartRune, then applies selection or find styling when any rune in the range is selected or matched. Keep the prepainted background when the final cluster does not fit.

- [ ] **Step 5: Normalize horizontal visibility and add regressions.**

Use LineVisualCol for cursor and scroll comparison. When scrolling right, call runeColAtOrAfterVisual instead of subtracting cell counts from rune indexes. Add tests that a second CJK cell hit maps to the same rune, a combining-mark interior is never ScrollX, and a find/selection member styles the whole visible cluster.

- [ ] **Step 6: Format, test, and commit.**

Run: gofmt -w internal/editor/{indent,tab}*.go && go test ./internal/editor

Expected: PASS.

~~~bash
git add internal/editor/indent.go internal/editor/indent_test.go internal/editor/tab.go internal/editor/tab_test.go
git commit -m "Render editor text by grapheme width"
~~~

### Task 3: Convert file-tree rendering

**Files:**

- Modify: internal/filetree/filetree.go
- Modify: internal/filetree/filetree_test.go

**Interfaces:**

- Consumes StringWidth and DrawString from Task 1.
- Keeps drawNodeRow and tree hit testing unchanged.

- [ ] **Step 1: Add failing CJK filename and narrow-row tests.**

~~~go
func TestRender_CJKFilenameUsesTwoCellsPerRune(t *testing.T) {
    dir := t.TempDir()
    if err := os.WriteFile(filepath.Join(dir, "中文测试.txt"), []byte("x"), 0644); err != nil {
        t.Fatal(err)
    }
    tr, err := New(dir)
    if err != nil {
        t.Fatal(err)
    }
    cells, width := renderAndCollect(t, tr, 40, 10)
    if findRowY(cells, width, 10, "中") < 0 {
        t.Fatal("CJK filename row not rendered")
    }
}
~~~

Add an icon-enabled offset assertion and a narrow width assertion proving that a final two-cell glyph is omitted rather than split.

- [ ] **Step 2: Confirm current local drawString fails the coordinate assertion.**

Run: go test ./internal/filetree -run TestRender_CJKFilenameUsesTwoCellsPerRune

Expected: FAIL.

- [ ] **Step 3: Draw all row segments through termtext.**

~~~go
prefixW := termtext.DrawString(scr, x, y, w, prefix, rowStyle)
glyphW := termtext.DrawString(scr, x+prefixW, y, w-prefixW, glyph, glyphStyle)
termtext.DrawString(scr, x+prefixW+glyphW, y, w-prefixW-glyphW, "  "+suffix, rowStyle)
~~~

Use the same DrawString path when icons are disabled. Delete the local drawString helper and replace rune-length segment offsets with returned cell widths.

- [ ] **Step 4: Format, test, and commit.**

Run: gofmt -w internal/filetree/filetree*.go && go test ./internal/filetree

Expected: PASS.

~~~bash
git add internal/filetree/filetree.go internal/filetree/filetree_test.go
git commit -m "Render tree names with terminal cell widths"
~~~

### Task 4: Share App text-input drawing and mouse mapping

**Files:**

- Create: internal/app/textinput.go
- Create: internal/app/textinput_test.go
- Modify: internal/app/modals.go
- Modify: internal/app/formmodal.go
- Modify: internal/app/find.go
- Modify: internal/app/finder.go
- Modify: adjacent app tests

**Interfaces:**

~~~go
func drawTextInput(scr tcell.Screen, x, y, width int, text []rune, cursor int, scroll *int, style tcell.Style, showCursor bool)
func textInputCursorAt(text []rune, scroll, localCell int) int
~~~

- [ ] **Step 1: Add failing shared-helper tests.**

~~~go
func TestDrawTextInput_CJKScrollsAndKeepsCaretVisible(t *testing.T) {
    scr := newSimScreen(t, 12, 1)
    text := []rune("ab中文测试")
    scroll := 0
    drawTextInput(scr, 0, 0, 4, text, len(text), &scroll, tcell.StyleDefault, true)
    if scroll == 0 {
        t.Fatal("expected horizontal scrolling")
    }
    if _, _, visible := scr.GetCursor(); !visible {
        t.Fatal("caret should be visible")
    }
}

func TestTextInputCursorAt_SecondCJKCellReturnsClusterStart(t *testing.T) {
    if got := textInputCursorAt([]rune("中a"), 0, 1); got != 0 {
        t.Fatalf("cursor = %d, want 0", got)
    }
}
~~~

- [ ] **Step 2: Confirm the helper tests fail before implementation.**

Run: go test ./internal/app -run 'Test(DrawTextInput_CJK|TextInputCursorAt_SecondCJK)'

Expected: FAIL.

- [ ] **Step 3: Implement the one renderer and integrate all four inputs.**

~~~go
func drawTextInput(scr tcell.Screen, x, y, width int, text []rune, cursor int, scroll *int, style tcell.Style, showCursor bool) {
    value := string(text)
    *scroll = textInputScroll(value, cursor, *scroll, width)
    scrollCell := termtext.VisualCol(value, *scroll)
    for _, cluster := range termtext.Clusters(value) {
        if cluster.EndRune <= *scroll {
            continue
        }
        cell := termtext.VisualCol(value, cluster.StartRune) - scrollCell
        if cell >= 0 && cell+cluster.Width <= width {
            termtext.DrawCluster(scr, x+cell, y, width-cell, cluster, style)
        }
    }
    if showCursor {
        caret := x + termtext.VisualCol(value, cursor) - scrollCell
        if caret >= x && caret <= x+width {
            scr.ShowCursor(caret, y)
        }
    }
}
~~~

Replace adjustPromptScroll, adjustFormScroll, adjustFindScroll, and adjustFinderScroll. Replace prompt and form input mouse column arithmetic with textInputCursorAt. Preserve existing rune-by-rune keyboard editing.

- [ ] **Step 4: Add prompt, form, find, and Finder CJK regressions.**

Prompt and form tests must verify a click on either cell of a wide glyph places the cursor at the cluster start. Find and Finder tests must verify CJK input does not overwrite their right-side counter text and keeps the caret visible after horizontal scroll.

- [ ] **Step 5: Format, test, and commit.**

Run: gofmt -w internal/app/{textinput,modals,formmodal,find,finder}*.go && go test ./internal/app

Expected: PASS.

~~~bash
git add internal/app
git commit -m "Share grapheme-aware text input rendering"
~~~

### Task 5: Convert dynamic App layout and Finder result styling

**Files:**

- Modify: internal/app/app.go
- Modify: internal/app/modals.go
- Modify: internal/app/finder.go
- Modify: internal/app/app_test.go
- Modify: internal/app/modals_test.go
- Modify: internal/app/finder_test.go

**Interfaces:**

- Consumes StringWidth, DrawString, DrawCluster, and Truncate.
- Keeps layoutTabs, drawStatusBar, drawMenu, drawFinderRow, and modal geometry signatures unchanged.

- [ ] **Step 1: Add failing tab, status, modal, and Finder tests.**

~~~go
func TestLayoutTabs_CJKNameUsesCellWidth(t *testing.T) {
    a := newTestApp(t, t.TempDir())
    a.tabs = []*editor.Tab{{Path: "中文.txt"}}
    got := a.layoutTabs()[0]
    if got.Width < 1+2+4+len(".txt")+3 {
        t.Fatalf("tab width too small: %+v", got)
    }
}
~~~

Add a CJK status right-alignment assertion, a CJK centered-confirm assertion, and a Finder Result with Path set to 目录/中文.txt and rune-indexed MatchedIndexes. Assert the CJK cluster uses the hit style.

- [ ] **Step 2: Confirm the focused tests fail before migration.**

Run: go test ./internal/app -run 'Test(LayoutTabs_CJK|DrawFinderRow_CJK|DrawStatusBar_.*CJK|DrawConfirm_.*CJK)'

Expected: FAIL.

- [ ] **Step 3: Replace all dynamic rune-count layout.**

~~~go
nameWidth := termtext.StringWidth(tab.DisplayName())
width := 1 + 2 + iconW + nameWidth + 1 + 1 + 1

rightWidth := termtext.StringWidth(right)
termtext.DrawString(a.screen, sx+sw-rightWidth, sy, rightWidth, right, style)

label = termtext.Truncate(label, shortcutX-(mx+4)-2)
termtext.DrawString(a.screen, mx+4, cy, shortcutX-(mx+4)-2, label, labelStyle)
~~~

Make drawAt, drawStatusText, and drawButton delegate to termtext.DrawString with explicit available widths. Remove runeLen and trimRunes. Replace every dynamic layout use of len([]rune(...)) in the App with StringWidth.

- [ ] **Step 4: Preserve Finder match semantics through cluster rendering.**

~~~go
for _, cluster := range termtext.Clusters(result.Path) {
    style := rowStyle
    if cluster.StartRune <= sepIdx {
        style = mutedOnRow
    }
    if anyMatched(result.MatchedIndexes, cluster.StartRune, cluster.EndRune) {
        style = hitOnRow
    }
    used, ok := termtext.DrawCluster(a.screen, startCol+drawn, rowY, maxCols-drawn, cluster, style)
    if !ok {
        break
    }
    drawn += used
}
~~~

Do not change finder.Score or finder.Result.MatchedIndexes. The renderer converts existing rune indexes to whole-cluster visual styles.

- [ ] **Step 5: Format, test, and commit.**

Run: gofmt -w internal/app/{app,modals,finder}*.go && go test ./internal/app

Expected: PASS.

~~~bash
git add internal/app
git commit -m "Use terminal cell widths throughout app UI"
~~~

### Task 6: Verify all requirements

**Files:**

- Modify only when test failures require a targeted correction.
- Verify: go.mod, go.sum, internal/termtext, internal/editor, internal/filetree, internal/app.

- [ ] **Step 1: Format all touched files.**

Run: gofmt -w internal/termtext/*.go internal/editor/{indent,tab}*.go internal/filetree/filetree*.go internal/app/{app,modals,formmodal,find,finder,textinput}*.go

Expected: no output.

- [ ] **Step 2: Run affected packages.**

Run: go test ./internal/termtext ./internal/editor ./internal/filetree ./internal/app

Expected: PASS.

- [ ] **Step 3: Run the repository gate.**

Run: make test

Expected: go test -race ./... exits 0.

- [ ] **Step 4: Audit source usage and final diff.**

Run: git diff --check && rg -n "runeLen|trimRunes|len\\(\\[\\]rune\\(" internal/app internal/filetree --glob '*.go'

Expected: no whitespace errors and no remaining dynamic UI layout based on rune count. Remaining rune indexes must describe buffer, finder-score, or input state rather than screen cells.

- [ ] **Step 5: Commit only verification corrections.**

~~~bash
git add go.mod go.sum internal
git commit -m "Verify CJK terminal rendering"
~~~

Do not create an empty verification commit.
