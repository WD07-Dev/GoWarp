package desktop

import (
	"image"
	"unicode/utf8"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/exp/textinput"
	"golang.org/x/image/font"
)

type answerInput struct {
	composer    textinput.Composer
	questionID  uint64
	text        string
	composition string
}

func (g *game) updateAnswerInput(in *Input) error {
	a := &g.answerInput
	c := g.controller
	q := c.vm.Question()
	embedded := g.options.Editor != nil && g.options.Editor.Embedded
	// Multiwindow HWNDs receive text through their own Windows message loop.
	if g.native != nil || embedded || c.options.Replay || q == nil || c.answerSubmitted || in.Restart || in.Stop {
		a.composer.Cancel()
		a.questionID = 0
		return nil
	}
	if a.questionID != q.ID {
		a.composer.Cancel()
		a.questionID = q.ID
		a.text, a.composition = "", ""
	}
	in.Text = ""
	if !ebiten.IsFocused() {
		a.composer.Confirm()
		in.Enter, in.Backspace = false, false
		in.Answer = &a.text
		return nil
	}
	if a.composer.OnNewSession == nil {
		a.composer.OnNewSession = func() *textinput.SessionOptions {
			c := g.controller
			w, h := c.renderer.Size()
			x := min(8+font.MeasureString(g.face, a.text).Ceil(), w-22)
			bounds := image.Rect(x, h+ToolbarHeight+AnswerHeight-25, x+1, h+ToolbarHeight+AnswerHeight-9)
			if c.options.Settings != nil {
				stage := g.stageBounds(g.outsideWidth, g.outsideHeight)
				x = stage.Min.X + x*stage.Dx()/max(1, w)
				bounds = image.Rect(x, stage.Max.Y-25*stage.Dy()/max(1, h), x+1, stage.Max.Y-9*stage.Dy()/max(1, h))
			}
			return &textinput.SessionOptions{CaretBounds: bounds, TextBeforeCaret: a.text}
		}
		a.composer.OnComposition = func(composition *textinput.Composition) {
			a.composition = composition.Text()
		}
		a.composer.OnCommit = func(commit *textinput.Commit) {
			before, after := commit.SurroundingText()
			text := before + commit.Text() + after
			if len(text) <= 4096 {
				a.text = text
			}
		}
	}
	handled, err := a.composer.Update()
	if err != nil {
		return err
	}
	if handled {
		in.Enter, in.Backspace = false, false
	} else if in.Backspace {
		a.composer.Confirm()
		_, n := utf8.DecodeLastRuneInString(a.text)
		a.text = a.text[:len(a.text)-n]
		in.Backspace = false
	} else if in.Enter {
		a.composer.Confirm()
	}
	in.Answer = &a.text
	in.Composition = a.composition
	return nil
}
