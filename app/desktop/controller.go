package desktop

import (
	"context"
	"image"
	"math"
	"time"

	"gowarp/app/config"
	"gowarp/app/editorbridge"
	"gowarp/core/extension"
	"gowarp/core/media"
	"gowarp/core/project"
	"gowarp/core/render"
	"gowarp/core/vm"
)

const ToolbarHeight = 36

const AnswerHeight = 52

type Options struct {
	Editor      *editorbridge.Peer
	Extensions  extension.Bundle
	Prepare     func(context.Context) (*project.Project, error)
	OnStart     func()
	Diagnostic  func(string)
	Status      func() string
	Settings    *config.Settings
	Scale       float64
	Configure   func(*vm.Runtime) error
	BeforeTick  func(*vm.Runtime) error
	AfterTick   func(*vm.Runtime) error
	FPS         float64
	Turbo, Mute bool
	Replay      bool // Ignore physical project input while replaying an input file.
	Frames      int
	Screenshot  string
}

type Input struct {
	Answer                          *string
	MouseX, MouseY                  float64
	MouseDown, MousePressed         bool
	WheelY                          float64
	Escape, Delete, SelectAll       bool
	Pressed, Released               []string
	Text                            string
	Composition                     string
	Enter, Backspace, Restart, Stop bool
}

type controller struct {
	paintedStage    *image.RGBA
	paintedScene    []vm.DisplayTarget
	paintedView     vm.View
	paintedFrame    int
	paintedSize     image.Point
	previous        []vm.DisplayTarget
	current         []vm.DisplayTarget
	interpolation   interpolationBuffer
	previousView    vm.View
	initErr         error
	credit          float64
	cadenceNow      func() time.Time
	cadenceTime     time.Time
	cadenceFPS      float64
	didTick         bool
	deferTick       bool
	tickCount       int
	tickLimit       int
	tickDeadline    time.Time
	flushAudio      func() error
	pending         []vm.InputEvent
	pendingMouse    vm.InputEvent
	answerSubmitted bool
	vm              *vm.Runtime
	renderer        *render.Renderer
	graphics        *graphicsPipeline
	assets          *media.Library
	options         Options
	monitors        monitorUI
	answer          string
	composition     string
	question        *vm.Question
	drag            *vm.DisplayTarget
	dragX, dragY    float64
}

func newController(p *project.Project, lib *media.Library, options Options) *controller {
	c := &controller{
		vm:       vm.New(p),
		renderer: render.New(lib),
		assets:   lib,
		options:  options,
	}
	durations := make([][]float64, len(lib.Sounds))
	for i, sounds := range lib.Sounds {
		for _, s := range sounds {
			durations[i] = append(durations[i], s.Duration())
		}
	}
	c.initErr = c.vm.Run(context.Background(), 0, options.FPS, options.Turbo)
	c.vm.SetWindowed(true)
	c.vm.SetFilePicker(pickFile)
	c.vm.ConfigureAudio(durations)
	c.vm.SetGraphics(c.renderer)
	if settings := options.Settings; settings != nil {
		if err := settings.Validate(); err != nil {
			c.initErr = err
			return c
		}
		background := c.vm.View().Color
		if settings.Platform.MultiWindow {
			background = "transparent"
		}
		c.initErr = c.vm.Configure(
			settings.FPS, settings.Turbo, settings.Interpolation,
			settings.Width, settings.Height,
			settings.Player.Username, background,
		)
		highQualityPen := c.vm.Options().HighQualityPen
		if settings.HighQualityPen != nil {
			highQualityPen = *settings.HighQualityPen
		}
		c.vm.ConfigureRendering(highQualityPen, false)
		c.vm.ConfigureAntialias(settings.Canvas.Antialias)
		limits := c.vm.Options()
		if settings.RemoveFencing != nil {
			limits.Fencing = !*settings.RemoveFencing
		}
		if settings.RemoveMiscLimits != nil {
			limits.MiscLimits = !*settings.RemoveMiscLimits
		}
		c.vm.ConfigureLimits(limits.Fencing, limits.MiscLimits)
		if !settings.Player.AutoStart {
			c.vm.Stop()
		}
	}
	return c
}

func (c *controller) advance(ctx context.Context, in Input) error {
	c.didTick = false
	c.tickCount = 0
	if c.initErr != nil {
		return c.initErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if in.Restart {
		c.paintedStage = nil
		c.previous = nil
		c.pending = nil
		c.credit = 0
		c.cadenceTime = time.Time{}
		c.answerSubmitted = false
		c.monitors.edit = nil
		c.monitors.drag = nil
		c.vm.GreenFlag()
		c.drag = nil
		c.answer = ""
		c.question = nil
	}
	if in.Stop {
		c.paintedStage = nil
		c.previous = nil
		c.pending = nil
		c.credit = 0
		c.cadenceTime = time.Time{}
		c.answerSubmitted = false
		c.vm.Stop()
		c.drag = nil
		c.answer = ""
		c.question = nil
	}
	if !c.options.Replay {
		w, h := c.renderer.Size()
		c.monitors.update(c.vm, &in, w, h)
	}
	if c.vm.Stopped() || c.vm.Paused() {
		c.credit = 0
		c.cadenceTime = time.Time{}
		if !c.options.Replay {
			for _, key := range in.Pressed {
				c.vm.SetKeyState(key, true)
			}
			for _, key := range in.Released {
				c.vm.SetKeyState(key, false)
			}
		}
		return nil
	}
	if c.options.Replay {
		return c.advanceCadence(ctx)
	}
	frame := c.vm.Frames()
	events := []vm.InputEvent{}
	w, h := c.renderer.Size()
	mouseX := math.Max(0, math.Min(float64(w), in.MouseX))
	mouseY := math.Max(0, math.Min(float64(h), in.MouseY-ToolbarHeight))
	x, y := c.renderer.FromScreen(mouseX, mouseY)
	if math.IsNaN(x) || math.IsInf(x, 0) || math.IsNaN(y) || math.IsInf(y, 0) {
		x, y = 0, 0
	}
	c.pendingMouse = vm.InputEvent{
		Frame: frame,
		Kind:  "mouse",
		X:     x,
		Y:     y,
		Down:  in.MouseDown,
	}
	for _, key := range in.Pressed {
		c.vm.SetKeyState(key, true)
		events = append(events, vm.InputEvent{
			Frame: frame,
			Kind:  "key",
			Key:   key,
			Down:  true,
		})
	}
	for _, key := range in.Released {
		c.vm.SetKeyState(key, false)
		events = append(events, vm.InputEvent{
			Frame: frame,
			Kind:  "key",
			Key:   key,
		})
	}
	if q := c.vm.Question(); q != nil {
		if c.question == nil || *q != *c.question {
			c.answer = ""
			c.answerSubmitted = false
		}
		c.question = q
		c.composition = in.Composition
		if !c.answerSubmitted && in.Backspace {
			runes := []rune(c.answer)
			if len(runes) > 0 {
				c.answer = string(runes[:len(runes)-1])
			}
		}
		if !c.answerSubmitted && len(c.answer)+len(in.Text) <= 4096 {
			c.answer += in.Text
		}
		if !c.answerSubmitted && in.Answer != nil {
			c.answer = *in.Answer
		}
		if !c.answerSubmitted && in.Enter {
			events = append(events, vm.InputEvent{
				Frame:  frame,
				Kind:   "answer",
				Answer: c.answer,
			})
			c.answer = ""
			c.composition = ""
			c.answerSubmitted = true
		}
	} else {
		c.answer = ""
		c.composition = ""
		c.question = nil
		c.answerSubmitted = false
	}
	if in.MousePressed && in.MouseY >= ToolbarHeight && in.MouseY < float64(ToolbarHeight+h) {
		hit, ok := c.renderer.Hit(c.vm.Scene(), x, y)
		if ok {
			events = append(events, vm.InputEvent{
				Frame:   frame,
				Kind:    "click",
				Target:  hit.State.Name,
				CloneID: hit.State.CloneID,
			})
			if hit.State.Draggable {
				c.vm.RaiseSprite(hit.Source, hit.State.CloneID)
				c.drag = &hit
				c.dragX, c.dragY = hit.State.X-x, hit.State.Y-y
			}
		} else {
			events = append(events, vm.InputEvent{
				Frame:  frame,
				Kind:   "click",
				Target: "_stage_",
			})
		}
	}
	if !in.MouseDown {
		c.drag = nil
	} else if c.drag != nil {
		c.vm.MoveSprite(c.drag.Source, c.drag.State.CloneID, x+c.dragX, y+c.dragY)
	}
	c.pending = append(c.pending, events...)
	return c.advanceCadence(ctx)
}

func (c *controller) hostTPS() int {
	fps := c.vm.FPS()
	if c.cadenceNow != nil && !c.options.Replay {
		fps = float64(time.Second) / float64(c.vm.TickInterval())
	}
	if c.options.Settings != nil {
		return int(math.Max(60, math.Ceil(fps)))
	}
	return int(math.Max(1, math.Ceil(fps)))
}

// Real-time recovery coalesces overdue callbacks; replay retains every scheduled tick.
func (c *controller) advanceCadence(ctx context.Context) error {
	minimum := 1 - 1e-12
	increment := c.vm.FPS() / float64(c.hostTPS())
	realtime := c.cadenceNow != nil && !c.options.Replay
	if realtime {
		now := c.cadenceNow()
		increment = 1
		if !c.cadenceTime.IsZero() {
			increment = now.Sub(c.cadenceTime).Seconds() * c.cadenceFPS
		}
		c.cadenceTime = now
		defer func() { c.cadenceFPS = float64(time.Second) / float64(c.vm.TickInterval()) }()
		/*
			Round to the nearest tick like the host, retaining negative credit
			for an early tick. Repeated catch-up callbacks cannot add time.
		*/
		minimum = .5
	}
	c.credit += increment
	if c.credit < minimum {
		return nil
	}
	if c.deferTick {
		return nil
	}
	if realtime {
		/*
			Bound total overdue work, not just each batch. Otherwise a slow
			section leaves seconds of ticks that speed up every later frame.
			Keep the fractional phase so normal playback does not drift.
		*/
		overdue := math.Floor(c.credit+.5) - float64(max(1, c.tickLimit))
		if overdue > 0 {
			c.credit -= overdue
		}
	}
	if !c.options.Replay {
		events := make([]vm.InputEvent, 0, len(c.pending)+1)
		c.pendingMouse.Frame = c.vm.Frames()
		events = append(events, c.pendingMouse)
		for _, event := range c.pending {
			event.Frame = c.vm.Frames()
			events = append(events, event)
		}
		if err := c.vm.QueueEvents(events); err != nil {
			return err
		}
		c.pending = nil
	}
	for c.credit >= minimum {
		c.credit--
		if !realtime {
			c.credit = math.Max(0, c.credit)
		}
		started := time.Now()
		if err := c.tick(ctx); err != nil {
			return err
		}
		if c.vm.Stopped() || c.vm.Paused() {
			c.credit = 0
			c.cadenceTime = time.Time{}
			break
		}
		if c.tickCount >= max(1, c.tickLimit) {
			break
		}
		if !c.tickDeadline.IsZero() && time.Until(c.tickDeadline) <= time.Since(started) {
			break
		}
	}
	return nil
}

func (c *controller) tick(ctx context.Context) error {
	c.paintedStage = nil
	if c.options.Settings != nil && c.vm.Options().Interpolation {
		c.previous = c.vm.SceneInto(c.previous)
		c.previousView = c.vm.View()
	}
	if c.options.BeforeTick != nil {
		if err := c.options.BeforeTick(c.vm); err != nil {
			return err
		}
	}
	var err error
	catchingUp := c.credit >= .5 && c.tickCount+1 < max(1, c.tickLimit)
	if c.graphics != nil && !c.options.Replay && !catchingUp {
		err = c.graphics.tick(ctx, c.tickVM)
	} else {
		err = c.tickVM(ctx)
	}
	if err != nil {
		return err
	}
	c.didTick = true
	c.tickCount++
	c.answerSubmitted = false
	if c.options.AfterTick != nil {
		if err := c.options.AfterTick(c.vm); err != nil {
			return err
		}
	}
	if c.flushAudio != nil {
		return c.flushAudio()
	}
	return nil
}

func (c *controller) tickVM(ctx context.Context) error {
	return c.vm.Tick(ctx, c.options.FPS, c.options.Turbo)
}
