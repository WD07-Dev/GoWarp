package packager

import (
	"context"
	"strings"
	"time"
	"unicode"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"gowarp/app/clipboard"
	"gowarp/app/host"
	"gowarp/app/i18n"
)

type clipboardResult struct {
	target                 *string
	original, text, action string
	cursor                 int
	selected               bool
	err                    error
}

func (a *application) clipboardShortcut() bool {
	modifier := ebiten.IsKeyPressed(ebiten.KeyControl)
	if host.ClipboardUsesMeta {
		modifier = ebiten.IsKeyPressed(ebiten.KeyMeta)
	}
	if !modifier {
		return false
	}
	switch {
	case inpututil.IsKeyJustPressed(ebiten.KeyA):
		a.selectAll = true
		a.dirty = true
	case inpututil.IsKeyJustPressed(ebiten.KeyC):
		a.startClipboard("copy")
	case inpututil.IsKeyJustPressed(ebiten.KeyX):
		a.startClipboard("cut")
	case inpututil.IsKeyJustPressed(ebiten.KeyV):
		a.startClipboard("paste")
	default:
		return false
	}
	return true
}

func (a *application) startClipboard(action string) {
	if a.clipboard != nil || a.editor == nil {
		return
	}
	if action != "paste" && !a.selectAll {
		return
	}
	r := clipboardResult{
		target: a.editor, original: *a.editor, cursor: a.cursor,
		selected: a.selectAll, action: action,
	}
	results := make(chan clipboardResult, 1)
	a.clipboard = results
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Second)
	go func() {
		defer cancel()
		if action == "paste" {
			r.text, r.err = clipboard.Read(ctx)
		} else {
			r.err = clipboard.Write(ctx, r.original)
		}
		results <- r
	}()
}

func (a *application) pollClipboard() {
	select {
	case r := <-a.clipboard:
		a.clipboard = nil
		a.applyClipboard(r)
	default:
	}
}

func (a *application) applyClipboard(r clipboardResult) {
	if r.err != nil {
		a.status = i18n.Tr("error.clipboard", r.err)
		a.dirty = true
		return
	}
	if a.busy || a.editor != r.target || *r.target != r.original ||
		a.cursor != r.cursor || a.selectAll != r.selected {
		return
	}
	switch r.action {
	case "paste":
		a.paste(r.text)
	case "cut":
		*a.editor = ""
		a.cursor = 0
		a.selectAll = false
		a.dirty = true
	}
}

func (a *application) paste(text string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	chars := []rune(strings.Map(func(r rune) rune {
		switch r {
		case '\r', '\n', '\t':
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text))
	if len(chars) == 0 {
		return
	}
	value := []rune(*a.editor)
	cursor := max(0, min(a.cursor, len(value)))
	if a.selectAll {
		value = nil
		cursor = 0
	}
	available := max(0, 4096-len(value))
	chars = chars[:min(len(chars), available)]
	if len(chars) == 0 {
		return
	}
	*a.editor = string(value[:cursor]) + string(chars) + string(value[cursor:])
	a.cursor = cursor + len(chars)
	a.normalizePathInput()
	a.selectAll = false
	a.dirty = true
}
