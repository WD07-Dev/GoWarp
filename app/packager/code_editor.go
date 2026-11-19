package packager

import (
	"sort"
	"strings"
	"unicode"
)

type codeSnapshot struct {
	text           string
	cursor, anchor int
}

type codeEditor struct {
	codeSnapshot
	chars      []rune
	lines      []int
	top, left  int
	undo, redo []codeSnapshot
	revision   uint64
}

func newCodeEditor(text string) *codeEditor {
	e := &codeEditor{codeSnapshot: codeSnapshot{text: normalizeCodeNewlines(text)}}
	e.index()
	return e
}

func normalizeCodeNewlines(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
}

func (e *codeEditor) index() {
	e.chars = []rune(e.text)
	e.lines = []int{0}
	for i, r := range e.chars {
		if r == '\n' {
			e.lines = append(e.lines, i+1)
		}
	}
}

func (e *codeEditor) selection() (int, int) {
	return min(e.cursor, e.anchor), max(e.cursor, e.anchor)
}

func (e *codeEditor) selected() string {
	start, end := e.selection()
	return string(e.chars[start:end])
}

func (e *codeEditor) move(position int, selecting bool) {
	e.cursor = max(0, min(position, len(e.chars)))
	if !selecting {
		e.anchor = e.cursor
	}
}

func (e *codeEditor) row() int {
	return sort.Search(len(e.lines), func(i int) bool { return e.lines[i] > e.cursor }) - 1
}

func (e *codeEditor) lineEnd(row int) int {
	if row+1 < len(e.lines) {
		return e.lines[row+1] - 1
	}
	return len(e.chars)
}

func (e *codeEditor) vertical(delta int, selecting bool) {
	row := e.row()
	column := e.cursor - e.lines[row]
	row = max(0, min(row+delta, len(e.lines)-1))
	e.move(min(e.lines[row]+column, e.lineEnd(row)), selecting)
}

func codeHistory(history []codeSnapshot, state codeSnapshot) []codeSnapshot {
	history = append(history, state)
	bytes := 0
	start := len(history)
	for start > 0 && len(history)-start < 64 {
		size := len(history[start-1].text)
		if bytes+size > 8<<20 && start != len(history) {
			break
		}
		bytes += size
		start--
	}
	if start > 0 {
		history = append([]codeSnapshot(nil), history[start:]...)
	}
	return history
}

func (e *codeEditor) insert(text string) {
	text = normalizeCodeNewlines(text)
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, text)
	e.replaceSelection(text)
}

func (e *codeEditor) replaceSelection(text string) {
	start, end := e.selection()
	next := string(e.chars[:start]) + text + string(e.chars[end:])
	if next == e.text {
		return
	}
	e.undo = codeHistory(e.undo, e.codeSnapshot)
	e.redo = nil
	e.text = next
	e.index()
	e.move(start+len([]rune(text)), false)
	e.revision++
}

func (e *codeEditor) newline() {
	start := e.lines[e.row()]
	end := start
	for end < e.cursor && (e.chars[end] == '\t' || e.chars[end] == ' ') {
		end++
	}
	e.insert("\n" + string(e.chars[start:end]))
}

func (e *codeEditor) erase(back bool) {
	if e.cursor == e.anchor {
		if back {
			e.anchor = max(0, e.cursor-1)
		} else {
			e.anchor = min(len(e.chars), e.cursor+1)
		}
	}
	e.insert("")
}

func (e *codeEditor) undoEdit(redo bool) {
	from, to := &e.undo, &e.redo
	if redo {
		from, to = to, from
	}
	if len(*from) == 0 {
		return
	}
	*to = codeHistory(*to, e.codeSnapshot)
	e.codeSnapshot = (*from)[len(*from)-1]
	(*from)[len(*from)-1] = codeSnapshot{}
	*from = (*from)[:len(*from)-1]
	e.index()
	e.revision++
}
