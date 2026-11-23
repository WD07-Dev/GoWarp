package packager

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"gowarp/app/assets"
	"gowarp/app/clipboard"
	"gowarp/app/compiler"
	"gowarp/app/i18n"
	"gowarp/app/ui"
	"gowarp/core/extension"
)

type extensionEditor struct {
	document       *compiler.ExtensionDocument
	buffers        []*codeEditor
	selected       int
	listTop        int
	focused        bool
	dragging       bool
	confirmClose   bool
	quitAfterClose bool
	message        string
	paste          chan codeClipboard
}

type codeClipboard struct {
	buffer         *codeEditor
	revision       uint64
	cursor, anchor int
	text, action   string
	err            error
}

type extensionImport struct {
	editor   *extensionEditor
	buffer   *codeEditor
	revision uint64
	source   string
	path     string
}

func readExtensionFile(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("extension source must be a regular file")
	}
	const limit = 16 << 20
	if info.Size() > limit {
		return "", fmt.Errorf("extension source exceeds 16 MiB")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if len(data) > limit {
		return "", fmt.Errorf("extension source exceeds 16 MiB")
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("extension source must be UTF-8 text")
	}
	return string(data), nil
}

func (a *application) importExtensionFile(path string) {
	e := a.extensions
	if a.busy || e == nil {
		return
	}
	buffer := e.current()
	if buffer == nil {
		return
	}
	imported := &extensionImport{
		editor:   e,
		buffer:   buffer,
		revision: buffer.revision,
		path:     path,
	}
	a.job(i18n.Tr("extensions.importing"), func(ctx context.Context) result {
		source, err := readExtensionFile(ctx, path)
		if err != nil {
			return result{err: err}
		}
		imported.source = source
		return result{importedExtension: imported}
	})
}

func (a *application) applyExtensionImport(imported *extensionImport) {
	if imported == nil || a.extensions != imported.editor ||
		a.extensions.current() != imported.buffer || imported.buffer.revision != imported.revision {
		return
	}
	buffer := imported.buffer
	buffer.move(0, false)
	buffer.move(len(buffer.chars), true)
	buffer.replaceSelection(normalizeCodeNewlines(imported.source))
	buffer.move(0, false)
	buffer.top, buffer.left = 0, 0
	a.extensions.focused = true
	a.extensions.message = i18n.Tr("extensions.imported", filepath.Base(imported.path))
	a.dirty = true
}

func newExtensionEditor(document *compiler.ExtensionDocument) *extensionEditor {
	e := &extensionEditor{document: document, focused: true}
	for _, source := range document.Sources {
		e.buffers = append(e.buffers, newCodeEditor(source.Source))
	}
	return e
}

func (e *extensionEditor) current() *codeEditor {
	if len(e.buffers) == 0 {
		return nil
	}
	return e.buffers[e.selected]
}

func (e *extensionEditor) edits() map[string]string {
	edits := make(map[string]string)
	for i, buffer := range e.buffers {
		if buffer.text != normalizeCodeNewlines(e.document.Sources[i].Source) {
			edits[e.document.Sources[i].ID] = buffer.text
		}
	}
	return edits
}

func (e *extensionEditor) addGoHeader() {
	buffer := e.current()
	if buffer == nil {
		return
	}
	if strings.Contains(buffer.text, "@gowarp-go") {
		e.message = i18n.Tr("extensions.header_exists")
		return
	}
	id := strings.ReplaceAll(strconv.Quote(e.document.Sources[e.selected].ID), "/", "\\x2f")
	header := strings.ReplaceAll(assets.ExtensionGoTemplate, "{{ID}}", id)
	buffer.move(0, false)
	buffer.insert(header + "\n")
	buffer.move(0, false)
	buffer.top, buffer.left = 0, 0
	e.message = i18n.Tr("extensions.header_added")
}

func (a *application) openExtensionEditor() {
	path := ""
	for _, field := range a.form.fields {
		if field.key == "project" {
			path = field.value
			break
		}
	}
	if strings.TrimSpace(path) == "" {
		a.errorText = i18n.Tr("extensions.open_sb3")
		a.dirty = true
		return
	}
	a.job(i18n.Tr("extensions.loading"), func(ctx context.Context) result {
		doc, err := compiler.OpenExtensionDocument(ctx, path)
		if err != nil {
			return result{err: err}
		}
		return result{extensions: newExtensionEditor(doc)}
	})
}

func (a *application) saveExtensions() {
	e := a.extensions
	if e == nil || a.busy {
		return
	}
	edits := e.edits()
	if len(edits) == 0 {
		e.message = i18n.Tr("extensions.no_changes")
		return
	}
	doc := *e.document
	doc.Sources = append([]compiler.EditableExtension(nil), doc.Sources...)
	a.job(i18n.Tr("extensions.saving"), func(ctx context.Context) result {
		if err := doc.Save(ctx, edits); err != nil {
			return result{err: err}
		}
		return result{savedExtensions: &doc, committed: true, status: i18n.Tr("extensions.saved")}
	})
}

func (a *application) closeExtensionEditor() {
	if len(a.extensions.edits()) != 0 {
		a.extensions.confirmClose = true
	} else {
		a.extensions = nil
	}
	a.hits = nil
	a.keyboardFocus = -1
	a.dirty = true
}

func (a *application) requestWindowClose() bool {
	if a.extensions != nil && len(a.extensions.edits()) != 0 {
		a.extensions.confirmClose = true
		a.extensions.quitAfterClose = true
		a.dirty = true
		return false
	}
	return true
}

func (a *application) discardExtensions() {
	a.quit = a.extensions.quitAfterClose
	a.extensions = nil
	a.hits = nil
}

func (a *application) extensionCodeRect() image.Rectangle {
	return image.Rect(226, 112, a.width-30, a.height-100)
}

func (a *application) renderExtensionEditor() {
	e := a.extensions
	a.hits = nil
	a.fill(image.Rect(0, 0, a.width, 80), color.RGBA{24, 24, 24, 255})
	a.label(30, 40, i18n.Tr("extensions.title"), ui.Text)
	a.button(a.width-150, 20, 120, i18n.Tr("extensions.back"), a.closeExtensionEditor)
	a.label(30, 72, a.fitText(e.document.Path, a.width-60), ui.Muted)
	a.fill(image.Rect(30, 88, 214, a.height-100), ui.Field)
	rows := max(1, (a.height-188)/32)
	e.listTop = min(e.listTop, max(0, len(e.buffers)-rows))
	for i := e.listTop; i < min(len(e.buffers), e.listTop+rows); i++ {
		r := image.Rect(30, 88+(i-e.listTop)*32, 214, 120+(i-e.listTop)*32)
		if i == e.selected {
			a.fill(r, color.RGBA{70, 42, 46, 255})
		}
		name := e.document.Sources[i].ID
		if e.buffers[i].text != normalizeCodeNewlines(e.document.Sources[i].Source) {
			name += " *"
		}
		a.label(38, r.Min.Y+22, a.fitText(name, 168), ui.Text)
		a.hits = append(a.hits, hit{rect: r, action: func() {
			e.selected, e.focused, e.dragging = i, true, false
			e.message = ""
		}})
	}
	if buffer := e.current(); buffer != nil {
		a.label(226, 103, a.fitText(e.document.Sources[e.selected].ID+".js", a.width-256), ui.Muted)
		a.renderCode(buffer, a.extensionCodeRect())
		a.button(30, a.height-88, 184, i18n.Tr("extensions.import"), func() { a.choose("extension") })
		a.button(226, a.height-88, 146, i18n.Tr("extensions.add_go"), e.addGoHeader)
		a.button(382, a.height-88, 172, i18n.Tr("extensions.check"), func() {
			_, err := extension.ExtractGo(e.document.Sources[e.selected].ID+".js", []byte(buffer.text))
			if err != nil {
				a.errorText = err.Error()
			} else {
				e.message = i18n.Tr("extensions.valid")
			}
		})
		a.coloredButton(a.width-176, a.height-88, 146, i18n.Tr("extensions.save"), color.RGBA{40, 88, 65, 255}, a.saveExtensions)
		message := e.message
		if message == "" {
			message = e.document.Sources[e.selected].Error
		}
		a.label(30, a.height-29, a.fitText(message, a.width-60), ui.Muted)
	} else {
		a.label(226, 132, i18n.Tr("extensions.empty"), ui.Muted)
	}
	if e.confirmClose {
		a.hits = nil
		a.modalFrame(image.Rect(90, 200, a.width-90, 378))
		a.label(114, 240, i18n.Tr("extensions.unsaved"), ui.Text)
		a.label(114, 272, i18n.Tr("extensions.discard_hint"), ui.Muted)
		a.button(114, 310, 190, i18n.Tr("extensions.keep_editing"), func() {
			e.confirmClose = false
			e.quitAfterClose = false
		})
		a.coloredButton(a.width-304, 310, 190, i18n.Tr("extensions.discard"), ui.Accent, a.discardExtensions)
	}
}

const codeLineHeight = 22

func (a *application) codeRuneWidth(r rune) int {
	if r == '\t' {
		return a.textWidth("    ")
	}
	return max(1, a.textWidth(string(r)))
}

func (a *application) codeColumnWidth(chars []rune) int {
	width := 0
	for _, r := range chars {
		width += a.codeRuneWidth(r)
	}
	return width
}

func (a *application) renderCode(e *codeEditor, rect image.Rectangle) {
	a.fill(rect, ui.Field)
	canvas := a.pixels
	a.pixels = canvas.SubImage(a.pixelRect(rect)).(*image.RGBA)
	defer func() { a.pixels = canvas }()
	rows := max(1, rect.Dy()/codeLineHeight)
	e.top = max(0, min(e.top, len(e.lines)-1))
	first, last := e.selection()
	for row := e.top; row < min(len(e.lines), e.top+rows); row++ {
		y := rect.Min.Y + (row-e.top)*codeLineHeight
		a.label(rect.Min.X+4, y+17, strconv.Itoa(row+1), ui.Muted)
		x := rect.Min.X + 50 - e.left
		start, end := e.lines[row], e.lineEnd(row)
		for pos := start; pos <= end; pos++ {
			if pos == e.cursor && a.extensions.focused && x >= rect.Min.X+48 {
				a.fill(image.Rect(x, y+2, x+1, y+20), ui.Text)
			}
			if pos == end {
				if first <= pos && pos < last {
					a.fill(image.Rect(max(rect.Min.X+48, x), y, max(rect.Min.X+48, x+6), y+codeLineHeight), color.RGBA{70, 55, 90, 255})
				}
				break
			}
			width := a.codeRuneWidth(e.chars[pos])
			if x+width >= rect.Min.X+48 && x < rect.Max.X {
				if pos >= first && pos < last {
					a.fill(image.Rect(max(rect.Min.X+48, x), y, x+width, y+codeLineHeight), color.RGBA{70, 55, 90, 255})
				}
				if e.chars[pos] != '\t' && x >= rect.Min.X+48 {
					a.label(x, y+17, string(e.chars[pos]), ui.Text)
				}
			}
			x += width
			if x > rect.Max.X {
				break
			}
		}
	}
}

func (a *application) codePosition(e *codeEditor, x, y int) int {
	rect := a.extensionCodeRect()
	row := max(0, min(e.top+(y-rect.Min.Y)/codeLineHeight, len(e.lines)-1))
	column := rect.Min.X + 50 - e.left
	for pos := e.lines[row]; pos < e.lineEnd(row); pos++ {
		width := a.codeRuneWidth(e.chars[pos])
		if x < column+width/2 {
			return pos
		}
		column += width
	}
	return e.lineEnd(row)
}

func (a *application) revealCodeCursor(e *codeEditor) {
	rows := max(1, a.extensionCodeRect().Dy()/codeLineHeight)
	row := e.row()
	e.top = max(0, min(e.top, row))
	if row >= e.top+rows {
		e.top = row - rows + 1
	}
	x := a.codeColumnWidth(e.chars[e.lines[row]:e.cursor])
	width := a.extensionCodeRect().Dx() - 60
	if x < e.left {
		e.left = x
	} else if x > e.left+width {
		e.left = x - width
	}
}

func (a *application) codeClipboard(action string) {
	e := a.extensions
	buffer := e.current()
	if e.paste != nil || buffer == nil {
		return
	}
	r := codeClipboard{
		buffer:   buffer,
		revision: buffer.revision,
		cursor:   buffer.cursor,
		anchor:   buffer.anchor,
		action:   action,
		text:     buffer.selected(),
	}
	if action != "paste" && r.text == "" {
		return
	}
	results := make(chan codeClipboard, 1)
	e.paste = results
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Second)
	go func() {
		defer cancel()
		if action == "paste" {
			r.text, r.err = clipboard.ReadWithLimit(ctx, 16<<20)
		} else {
			r.err = clipboard.WriteWithLimit(ctx, r.text, 16<<20)
		}
		results <- r
	}()
}

func (a *application) updateExtensionEditor() {
	e := a.extensions
	if a.errorText != "" || e.confirmClose {
		if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
			a.errorText = ""
			e.confirmClose = false
			e.quitAfterClose = false
			a.dirty = true
		}
		a.extensionClick()
		return
	}
	buffer := e.current()
	select {
	case r := <-e.paste:
		e.paste = nil
		if r.err != nil {
			e.message = i18n.Tr("error.clipboard", r.err)
		} else if buffer == r.buffer && buffer.revision == r.revision && buffer.cursor == r.cursor && buffer.anchor == r.anchor {
			switch r.action {
			case "paste":
				buffer.insert(r.text)
			case "cut":
				buffer.insert("")
			}
			a.revealCodeCursor(buffer)
		}
		a.dirty = true
	default:
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		a.closeExtensionEditor()
		return
	}
	x, y := ebiten.CursorPosition()
	x, y = a.logicalPoint(x, y)
	shift := ebiten.IsKeyPressed(ebiten.KeyShift)
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		e.focused = buffer != nil && image.Pt(x, y).In(a.extensionCodeRect())
		e.dragging = e.focused
		if e.focused {
			buffer.move(a.codePosition(buffer, x, y), shift)
		} else {
			a.extensionClick()
			return
		}
		a.dirty = true
	}
	if e.dragging {
		if !ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
			e.dragging = false
		} else if buffer != nil {
			buffer.move(a.codePosition(buffer, x, y), true)
			a.revealCodeCursor(buffer)
			a.dirty = true
		}
	}
	wx, wy := ebiten.Wheel()
	if wx != 0 || wy != 0 {
		if x < 214 {
			e.listTop = max(0, e.listTop-int(wy)*3)
		} else if buffer != nil {
			if shift {
				wx += wy
				wy = 0
			}
			buffer.top = max(0, min(len(buffer.lines)-1, buffer.top-int(wy)*3))
			buffer.left = max(0, buffer.left-int(wx)*32)
		}
		a.dirty = true
	}
	ctrl := ebiten.IsKeyPressed(ebiten.KeyControl)
	if ctrl && inpututil.IsKeyJustPressed(ebiten.KeyS) {
		a.saveExtensions()
		return
	}
	if buffer == nil || !e.focused {
		return
	}
	before := buffer.codeSnapshot
	if ctrl {
		switch {
		case inpututil.IsKeyJustPressed(ebiten.KeyA):
			buffer.anchor, buffer.cursor = 0, len(buffer.chars)
		case inpututil.IsKeyJustPressed(ebiten.KeyC):
			a.codeClipboard("copy")
		case inpututil.IsKeyJustPressed(ebiten.KeyX):
			a.codeClipboard("cut")
		case inpututil.IsKeyJustPressed(ebiten.KeyV):
			a.codeClipboard("paste")
		case inpututil.IsKeyJustPressed(ebiten.KeyZ):
			buffer.undoEdit(shift)
		case inpututil.IsKeyJustPressed(ebiten.KeyY):
			buffer.undoEdit(true)
		}
	} else {
		switch {
		case repeated(ebiten.KeyBackspace):
			buffer.erase(true)
		case repeated(ebiten.KeyDelete):
			buffer.erase(false)
		case repeated(ebiten.KeyEnter):
			buffer.newline()
		case repeated(ebiten.KeyTab):
			buffer.insert("\t")
		}
		if chars := ebiten.AppendInputChars(nil); len(chars) != 0 {
			buffer.insert(string(chars))
		}
	}
	switch {
	case repeated(ebiten.KeyArrowLeft):
		start, _ := buffer.selection()
		if buffer.cursor != buffer.anchor && !shift {
			buffer.move(start, false)
		} else {
			buffer.move(buffer.cursor-1, shift)
		}
	case repeated(ebiten.KeyArrowRight):
		_, end := buffer.selection()
		if buffer.cursor != buffer.anchor && !shift {
			buffer.move(end, false)
		} else {
			buffer.move(buffer.cursor+1, shift)
		}
	case repeated(ebiten.KeyArrowUp):
		buffer.vertical(-1, shift)
	case repeated(ebiten.KeyArrowDown):
		buffer.vertical(1, shift)
	case repeated(ebiten.KeyPageUp):
		buffer.vertical(-max(1, a.extensionCodeRect().Dy()/codeLineHeight), shift)
	case repeated(ebiten.KeyPageDown):
		buffer.vertical(max(1, a.extensionCodeRect().Dy()/codeLineHeight), shift)
	case repeated(ebiten.KeyHome):
		pos := buffer.lines[buffer.row()]
		if ctrl {
			pos = 0
		}
		buffer.move(pos, shift)
	case repeated(ebiten.KeyEnd):
		pos := buffer.lineEnd(buffer.row())
		if ctrl {
			pos = len(buffer.chars)
		}
		buffer.move(pos, shift)
	}
	if before != buffer.codeSnapshot {
		a.revealCodeCursor(buffer)
		a.dirty = true
	}
}

func (a *application) extensionClick() {
	if !inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		return
	}
	x, y := ebiten.CursorPosition()
	x, y = a.logicalPoint(x, y)
	for _, h := range a.hits {
		if image.Pt(x, y).In(h.rect) && h.action != nil {
			h.action()
			a.dirty = true
			return
		}
	}
}
