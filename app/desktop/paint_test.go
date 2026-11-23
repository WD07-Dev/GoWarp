package desktop

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"gowarp/app/config"
	"gowarp/core/media"
	"gowarp/core/project"
	"gowarp/core/vm"
)

func TestBubbleWrappingHasVisibleLimit(t *testing.T) {
	lines := wrapText(basicfont.Face7x13, strings.Repeat("question\n", 200), 170)
	if len(lines) > 10 || lines[len(lines)-1] != "…" {
		t.Fatalf("unbounded display lines: %d", len(lines))
	}
}

type zeroWidthFace struct {
	font.Face
}

func (zeroWidthFace) GlyphAdvance(rune) (fixed.Int26_6, bool) {
	return 0, true
}

func TestBubbleWrappingBoundsZeroWidthCharacters(t *testing.T) {
	lines := wrapText(zeroWidthFace{basicfont.Face7x13}, strings.Repeat("\u0301", 2000), 170)
	if count := utf8.RuneCountInString(strings.Join(lines, "")); count > 1025 {
		t.Fatalf("unbounded zero-width text: %d", count)
	}
}

func TestPlayerReusesStageWhileAnswerEditingRepaints(t *testing.T) {
	c := cadenceController(t, .1, `{"flag":{"opcode":"event_whenflagclicked","topLevel":true,"next":"ask"},"ask":{"opcode":"sensing_askandwait","inputs":{"QUESTION":[1,[10,"Name?"]]}}}`)
	s := config.Default(nil)
	c.options.Settings = &s
	if err := c.vm.Run(context.Background(), 1, .1, false); err != nil {
		t.Fatal(err)
	}
	before := c.paint(basicfont.Face7x13)
	previousScene := &c.paintedScene[0]
	if err := c.advance(context.Background(), Input{Text: "Hello"}); err != nil {
		t.Fatal(err)
	}
	after := c.paint(basicfont.Face7x13)
	if c.vm.Frames() != 1 || previousScene != &c.paintedScene[0] {
		t.Fatal("unchanged stage was rendered again between execution frames")
	}
	if bytes.Equal(before.Pix, after.Pix) {
		t.Fatal("stage reuse hid answer editing")
	}
}

func TestPlayerStageReuseTracksSpriteAndViewChangesBetweenFrames(t *testing.T) {
	p := &project.Project{Targets: []*project.Target{
		{IsStage: true},
		{Name: "Sprite", Visible: true, Size: 100, Direction: 90},
	}}
	im := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			im.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	lib := &media.Library{Costumes: [][]*media.Costume{nil, {{Image: im, Width: 2, Height: 2, CenterX: 1, CenterY: 1}}}}
	s := config.Default(nil)
	c := newController(p, lib, Options{Settings: &s, FPS: 30})
	if got := c.paint(basicfont.Face7x13).RGBAAt(240, 180); got != (color.RGBA{R: 255, A: 255}) {
		t.Fatalf("initial sprite pixel = %v", got)
	}
	c.vm.MoveSprite(1, 0, 30, 0)
	frame := c.paint(basicfont.Face7x13)
	if frame.RGBAAt(240, 180) != (color.RGBA{255, 255, 255, 255}) || frame.RGBAAt(270, 180) != (color.RGBA{R: 255, A: 255}) {
		t.Fatal("stage reuse hid a sprite move between ticks")
	}
	if err := c.vm.Configure(30, false, false, 480, 360, "player", "#112233"); err != nil {
		t.Fatal(err)
	}
	if got := c.paint(basicfont.Face7x13).RGBAAt(10, 10); got != (color.RGBA{0x11, 0x22, 0x33, 255}) {
		t.Fatalf("stage reuse hid view color change: %v", got)
	}
	if c.vm.Frames() != 0 {
		t.Fatal("test unexpectedly advanced project frames")
	}
}

func TestPlayerStageReuseInvalidatesBeforeFailedTick(t *testing.T) {
	c := cadenceController(t, 30, `{}`)
	s := config.Default(nil)
	c.options.Settings = &s
	c.paint(basicfont.Face7x13)
	failure := errors.New("tick failed after drawing")
	c.options.BeforeTick = func(*vm.Runtime) error {
		c.renderer.Pen(vm.PenCommand{Kind: "line", Line: [4]float64{0, 0, 0, 0}, Size: 10, Color: color.NRGBA{R: 255, A: 255}})
		return failure
	}
	if err := c.tick(context.Background()); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if got := c.paint(basicfont.Face7x13).RGBAAt(240, 180); got != (color.RGBA{R: 255, A: 255}) {
		t.Fatalf("failed tick left stale pen pixels: %v", got)
	}
	if c.vm.Frames() != 0 {
		t.Fatal("failed hook advanced project frames")
	}
}

func TestPlayerInterpolationBypassesStageReuse(t *testing.T) {
	c := cadenceController(t, 30, `{}`)
	s := config.Default(nil)
	c.options.Settings = &s
	c.paint(basicfont.Face7x13)
	if err := c.vm.Configure(30, false, true, 480, 360, "player", "#ffffff"); err != nil {
		t.Fatal(err)
	}
	c.paint(basicfont.Face7x13)
	if c.paintedStage != nil {
		t.Fatal("interpolated stage was cached")
	}
}
