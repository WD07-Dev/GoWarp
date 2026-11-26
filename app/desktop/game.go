package desktop

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"golang.org/x/image/font"

	"gowarp/app/assets"
	"gowarp/app/host"
	"gowarp/app/ui"
	"gowarp/core/media"
	"gowarp/core/project"
	"gowarp/core/vm"
	"gowarp/core/window"
)

type game struct {
	mainWindow                                     window.View
	windowReady, windowDirty, windowCloseRequested bool
	windowButtonsState                             windowButtonState

	native                      *nativeViews
	editor                      editorInput
	stageNoAntialias            bool
	displayLogged               bool
	nextPresentation            time.Time
	presentationSize            image.Point
	presentationWaiting         bool
	urlErrors                   chan error
	updateTime                  time.Duration
	frameTicks                  int
	updateBudget                time.Duration
	updateBudgetUntil           time.Time
	tickCost                    time.Duration
	lastHostFrame               time.Time
	lastHostUpdateEnd           time.Time
	hostDrawTime                time.Duration
	hostDrawCalls               int
	lastSlowHost                time.Time
	lastSlowUpdate              time.Time
	lastSlowAudio               time.Time
	lastSlowDraw                time.Time
	stepDiagnostics             vm.StepDiagnostics
	options                     Options
	path                        string
	project                     *project.Project
	events                      []vm.InputEvent
	loader                      *projectLoader
	loading                     loadingView
	drawn, started              bool
	unfocused                   bool
	focusDraws                  int
	failure                     error
	waitingStart                bool
	outsideWidth, outsideHeight int
	windowDPI                   float64
	displayFace, stageFace      font.Face
	displayScale, stageScale    float64
	ctx                         context.Context
	controller                  *controller
	audio                       *audioOutput
	face                        font.Face
	canvas                      *ebiten.Image
	stageCanvas                 *ebiten.Image
	backgroundFrame             *ebiten.Image
	gpu                         stageGPU
	texts                       gpuTextCache
	controlsCanvas              *ebiten.Image
	updates                     int
	limitReached, captured      bool
	drawErr                     error
	windowButtonsApplied        bool
	hostFullscreen              bool
	projectFullscreen           bool
	windowFocused               bool
	windowMinimized             bool
	windowFocusRead             bool
	windowFullscreen            bool
	windowFullscreenRead        bool
}

// Run owns the native event loop on the main goroutine.
func Run(ctx context.Context, path string, p *project.Project, options Options, events []vm.InputEvent) (*vm.Runtime, error) {
	g := &game{
		ctx: ctx, path: path, project: p, options: options, events: events,
		face: loadFont(),
	}
	if options.Settings != nil {
		g.face.Close()
		g.face = ui.Font(14)
	}
	defer g.face.Close()
	defer g.close()
	defer func() {
		if g.editor.frame != nil {
			g.editor.frame.Deallocate()
		}
	}()
	w, h := 480, 360
	scale := options.Scale
	if scale <= 0 {
		scale = 2
	}
	ebiten.SetWindowSize(int(float64(w)*scale), int(float64(h+ToolbarHeight+AnswerHeight)*scale))
	ebiten.SetWindowTitle("GoWarp — " + filepath.Base(path))
	ebiten.SetWindowIcon(assets.DefaultWindowIcon())
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetWindowDecorated(true)
	ebiten.SetWindowClosingHandled(true)
	if s := options.Settings; s != nil {
		if len(s.Player.Icon) > 0 {
			icons, err := assets.IconImages(s.Player.Icon)
			if err != nil {
				return nil, err
			}
			ebiten.SetWindowIcon(icons)
		}
		ebiten.SetWindowTitle(s.Player.Title)
		width, height := playerWindowSize(s)
		if !s.Platform.Fullscreen && options.Editor == nil {
			width, height = s.Loading.WindowSize(width, height)
		}
		ebiten.SetWindowSize(width, height)
		if !s.Platform.Resizable {
			ebiten.SetWindowResizingMode(ebiten.WindowResizingModeDisabled)
		}
		ebiten.SetFullscreen(s.Platform.Fullscreen)
		ebiten.SetWindowDecorated(s.Platform.Border)
		ebiten.SetWindowFloating(s.Platform.Topmost && !s.Platform.MultiWindow)
		g.hostFullscreen = s.Platform.Fullscreen
	}
	g.initMainWindow()
	if err := g.openNativeViews(); err != nil {
		return nil, err
	}
	ebiten.SetTPS(60)
	ebiten.SetRunnableOnUnfocused(true)
	cleared := ebiten.IsScreenClearedEveryFrame()
	ebiten.SetScreenClearedEveryFrame(false)
	defer ebiten.SetScreenClearedEveryFrame(cleared)
	var err error
	if g.native != nil {
		err = ebiten.RunGameWithOptions(g, &ebiten.RunGameOptions{GraphicsLibrary: ebiten.GraphicsLibraryDirectX, InitUnfocused: true, SkipTaskbar: true})
	} else {
		err = ebiten.RunGame(g)
	}
	if err == ebiten.Termination {
		err = nil
	}
	if g.native != nil {
		err = errors.Join(err, g.native.backend.Close())
	}
	if err == nil && g.failure != nil {
		err = g.failure
	}
	if g.controller != nil {
		return g.controller.vm, err
	}
	if err == nil && options.Settings == nil {
		// Closing during preparation cancels the run; CLI callers require a runtime on success.
		err = context.Canceled
	}
	return nil, err
}

func keyName(key ebiten.Key, text string) string {
	s := key.String()
	// OS text input includes Shift and Caps Lock, even when enabled before focus.
	if len(s) == 1 && s[0] >= 'A' && s[0] <= 'Z' {
		for _, char := range text {
			if strings.EqualFold(s, string(char)) {
				return string(char)
			}
		}
	}
	switch s {
	case "Space":
		return "space"
	case "ArrowLeft":
		return "left arrow"
	case "ArrowRight":
		return "right arrow"
	case "ArrowUp":
		return "up arrow"
	case "ArrowDown":
		return "down arrow"
	}
	return strings.ToLower(strings.TrimPrefix(s, "Digit"))
}

func (g *game) Update() error {
	if g.native == nil && ebiten.IsWindowBeingClosed() && !g.mainWindow.DisableClose {
		return ebiten.Termination
	}

	if err := g.pollNativeViews(); err != nil {
		return err
	}
	if g.options.Diagnostic != nil {
		if !g.lastHostUpdateEnd.IsZero() {
			// Measure the gap between callbacks, excluding the previous Update's work.
			g.reportSlowPhase("host_gap", time.Since(g.lastHostUpdateEnd), 1)
		}
		g.hostDrawTime, g.hostDrawCalls = 0, 0
		defer func() { g.lastHostUpdateEnd = time.Now() }()
	}
	if !g.started {
		g.started = true
		if g.options.Diagnostic != nil {
			var info ebiten.DebugInfo
			ebiten.ReadDebugInfo(&info)
			if info.GraphicsLibrary != ebiten.GraphicsLibraryUnknown {
				g.options.Diagnostic("graphics backend=" + info.GraphicsLibrary.String())
			}
		}
		if g.options.OnStart != nil {
			g.options.OnStart()
		}
	}
	err := g.update()
	if g.native != nil && err != nil {
		return err
	}
	if g.options.Settings != nil && err != nil && err != ebiten.Termination && g.ctx.Err() == nil {
		if g.options.Diagnostic != nil {
			g.options.Diagnostic("player failure: " + err.Error())
		}
		g.failure = err
		return nil
	}
	return err
}

func (g *game) update() error {
	if err := g.ctx.Err(); err != nil {
		return err
	}
	if g.options.Editor != nil {
		select {
		case <-g.options.Editor.Done():
			return ebiten.Termination
		default:
		}
	}
	if g.captured {
		return ebiten.Termination
	}
	g.updateFocus(g.sampleWindowFocus())
	if (inpututil.IsKeyJustPressed(ebiten.KeyEscape) || (g.native != nil && slices.Contains(g.native.input.Keys, "escape"))) && (g.controller == nil || g.controller.monitors.edit == nil) {
		if err := g.handleEscape(); err != nil {
			return err
		}
	}
	if g.failure != nil {
		return nil
	}
	if err := g.applyWindowChanges(); err != nil {
		return err
	}
	if err := g.applyWindowButtons(); err != nil {
		return err
	}
	if g.drawErr != nil {
		return g.drawErr
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyF11) || (g.native != nil && slices.Contains(g.native.input.Keys, "f11")) {
		g.toggleFullscreen()
	}
	if g.controller == nil {
		return g.updateLoading()
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyF8) {
		g.controller.vm.SetPaused(false)
	}
	if g.limitReached {
		if g.native != nil {
			return ebiten.Termination
		}
		return nil
	}
	cursorX, cursorY := ebiten.CursorPosition()
	x, y := g.inputPosition(cursorX, cursorY)
	pressed := inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft)
	_, wheelY := ebiten.Wheel()
	in := Input{
		WheelY:       wheelY,
		Escape:       inpututil.IsKeyJustPressed(ebiten.KeyEscape),
		Delete:       inpututil.IsKeyJustPressed(ebiten.KeyDelete),
		SelectAll:    ebiten.IsKeyPressed(ebiten.KeyControl) && inpututil.IsKeyJustPressed(ebiten.KeyA),
		MouseX:       x,
		MouseY:       y,
		MouseDown:    ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft),
		MousePressed: pressed,
		Text:         string(ebiten.AppendInputChars(nil)),
		Enter:        inpututil.IsKeyJustPressed(ebiten.KeyEnter),
		Backspace:    inpututil.IsKeyJustPressed(ebiten.KeyBackspace),
		Restart:      inpututil.IsKeyJustPressed(ebiten.KeyF5),
	}
	if g.controller.options.Settings != nil {
		stage := g.stageBounds(g.outsideWidth, g.outsideHeight)
		in.MousePressed = pressed && image.Pt(cursorX, cursorY).In(stage)
		_, h := g.controller.renderer.Size()
		if g.controller.vm.Question() != nil && y >= float64(ToolbarHeight+max(0, h-AnswerHeight)) {
			in.MousePressed = false
			in.MouseDown = false
		}
	}
	g.nativeInput(&in)
	if g.native != nil {
		pressed = g.native.input.Pressed
	}
	if g.waitingStart && g.options.Editor == nil {
		if !pressed && !in.Enter && !in.Restart {
			return nil
		}
		g.waitingStart = false
		in = Input{Restart: true}
	}
	if buttonX, toolbar := g.toolbarPosition(cursorX, cursorY); pressed && toolbar {
		if buttonX >= 8 && buttonX < 32 {
			in.Restart = true
		}
		if buttonX >= 42 && buttonX < 66 {
			in.Stop = true
		}
		if g.controller.options.Settings != nil && buttonX >= 76 && buttonX < 102 {
			g.toggleFullscreen()
		}
	}

	if g.native == nil {
		for _, key := range inpututil.AppendJustPressedKeys(nil) {
			if key != ebiten.KeyF5 && key != ebiten.KeyF8 {
				in.Pressed = append(in.Pressed, keyName(key, in.Text))
			}
		}
		for _, key := range inpututil.AppendJustReleasedKeys(nil) {
			in.Released = append(in.Released, keyName(key, ""))
		}
	}
	g.editorInput(&in)
	if err := g.audio.poll(g.controller.vm); err != nil {
		return err
	}
	if err := g.advanceController(in); err != nil {
		return err
	}
	if err := g.applyHostCommands(); err != nil {
		return err
	}
	if err := g.applyWindowChanges(); err != nil {
		return err
	}
	if err := g.controller.assets.Err(); err != nil {
		return err
	}
	if err := g.flushAudio(); err != nil {
		return err
	}
	ebiten.SetTPS(g.updateTPS())
	g.recordUpdate()
	return nil
}

func (g *game) sampleWindowFocus() bool {
	if g.native != nil {
		return true
	}
	if g.options.Editor != nil {
		return true
	}
	if !g.windowFocusRead {
		g.windowMinimized = ebiten.IsWindowMinimized()
		g.windowFocused = !g.windowMinimized && ebiten.IsFocused()
		g.windowFocusRead = true
	}
	return g.windowFocused
}

func (g *game) sampleFullscreen() bool {
	if g.native != nil {
		for _, v := range g.native.layout.Views {
			if v.ID == "main" {
				return v.Fullscreen
			}
		}
		return false
	}

	if !g.windowFullscreenRead {
		g.windowFullscreen = ebiten.IsFullscreen()
		g.windowFullscreenRead = true
	}
	return g.windowFullscreen
}

func (g *game) handleEscape() error {
	if g.native != nil {
		for i := range g.native.layout.Views {
			v := &g.native.layout.Views[i]
			if v.ID == g.native.input.Window && v.Fullscreen {
				v.Fullscreen = false
				g.native.dirty = true
				return nil
			}
		}
	}

	if ebiten.IsFullscreen() {
		g.hostFullscreen, g.projectFullscreen = false, false
		ebiten.SetFullscreen(false)
		g.windowFocusRead, g.windowFullscreenRead = false, false
		if g.controller != nil {
			g.controller.vm.SetHostFrame(false, 0)
		}
		return nil
	}
	if g.options.Settings == nil || g.failure != nil {
		return ebiten.Termination
	}
	return nil
}

func (g *game) toggleFullscreen() {
	if g.native != nil {
		id := g.native.input.Window
		if id == "" {
			id = "main"
		}
		for i := range g.native.layout.Views {
			v := &g.native.layout.Views[i]
			if v.ID == id {
				v.Fullscreen = !v.Fullscreen
				g.native.dirty = true
				break
			}
		}
		return
	}
	/*
		  Browser F11 and document.requestFullscreen are independent modes.
				A project's exitFullscreen must not cancel the player's F11 shortcut.
	*/
	g.hostFullscreen = !g.hostFullscreen
	ebiten.SetFullscreen(g.hostFullscreen || g.projectFullscreen)
	g.windowFocusRead, g.windowFullscreenRead = false, false
}

func (g *game) updateTPS() int {
	if g.controller.cadenceNow != nil && !g.controller.options.Replay {
		return ebiten.SyncWithFPS
	}
	return g.controller.hostTPS()
}

func (g *game) advanceController(in Input) error {
	c := g.controller
	realtime := c.cadenceNow != nil && !c.options.Replay
	embedded := g.options.Editor != nil && g.options.Editor.Embedded
	if c.flushAudio == nil && g.audio != nil {
		c.flushAudio = g.flushAudio
	}
	// Allow one catch-up tick; larger batches delay input/audio and queue unseen drawing.
	c.tickLimit = 1
	budget := time.Second / 60
	if c.options.Settings != nil && !c.options.Replay {
		now := time.Now()
		// Background updates can continue without Draw, so renew the budget by time too.
		if !g.updateBudgetUntil.IsZero() && !now.Before(g.updateBudgetUntil) &&
			(!realtime || g.unfocused || g.windowMinimized || embedded) {
			g.updateTime = 0
			g.frameTicks = 0
			/*
				Keep the measured presentation allowance until Draw is absent for two intervals. Resetting to 16
				ms each frame would starve catch-up at lower display rates.
			*/
			if g.lastHostFrame.IsZero() || now.Sub(g.lastHostFrame) >= 2*g.updateBudget {
				g.updateBudget = 0
			}
		}
		budget = max(budget, g.updateBudget)
		if realtime && embedded && !c.cadenceTime.IsZero() {
			// Browser presentation must not be required to renew the VM allowance.
			interval := c.cadenceNow().Sub(c.cadenceTime)
			budget = max(budget, min(time.Second/30, interval))
		}
		if g.updateBudgetUntil.IsZero() || !now.Before(g.updateBudgetUntil) {
			g.updateBudgetUntil = now.Add(budget)
		}
		remaining := budget - g.updateTime
		c.deferTick = remaining <= 0 || (g.updateTime > 0 && remaining <= g.tickCost)
		c.tickLimit = 2
		if realtime {
			// Normal high-FPS ticks must fit too; the allowance spans all callbacks before Draw.
			c.tickLimit = max(2, int(math.Ceil(float64(budget)/float64(c.vm.TickInterval())))) - g.frameTicks
			c.deferTick = c.deferTick || c.tickLimit <= 0
		}
		c.tickDeadline = time.Now().Add(remaining)
	}
	if c.options.Frames > 0 {
		c.tickLimit = min(c.tickLimit, c.options.Frames-g.updates)
		c.deferTick = c.deferTick || c.tickLimit <= 0
	}
	defer func() {
		c.deferTick = false
		c.tickLimit = 0
		c.tickDeadline = time.Time{}
		g.gpu.measureUploads = false
	}()
	start := time.Now()
	if g.options.Diagnostic != nil {
		c.vm.StartStepDiagnostics()
		g.gpu.measureUploads = true
		g.gpu.uploadCount, g.gpu.uploadBytes, g.gpu.uploadTime, g.gpu.evictions = 0, 0, 0, 0
		g.gpu.prepareTime, g.gpu.pixelTime, g.gpu.textureTime, g.gpu.writeTime = 0, 0, 0, 0
		g.gpu.penSubmitTime = 0
	}
	if c.graphics != nil {
		c.graphics.measure = g.options.Diagnostic != nil
		c.graphics.stats = graphicsPipelineStats{}
	}
	err := c.advance(g.ctx, in)
	if g.options.Diagnostic != nil {
		g.stepDiagnostics = c.vm.FinishStepDiagnostics()
	}
	if c.didTick {
		elapsed := time.Since(start)
		g.reportSlowPhase("vm", elapsed, c.tickCount)
		g.updateTime += elapsed
		g.frameTicks += c.tickCount
		g.tickCost = elapsed / time.Duration(c.tickCount)
	}
	return err
}

func (g *game) flushAudio() error {
	commands := g.controller.vm.DrainAudio()
	if len(commands) == 0 {
		return nil
	}
	var started time.Time
	if g.options.Diagnostic != nil {
		started = time.Now()
	}
	err := g.audio.apply(g.controller.vm, commands)
	if !started.IsZero() {
		g.reportSlowPhase("audio", time.Since(started), 1)
	}
	return err
}

func (g *game) reportSlowPhase(phase string, elapsed time.Duration, ticks int) {
	if g.options.Diagnostic == nil || g.controller == nil {
		return
	}
	c := g.controller
	// Compare a catch-up batch with its total tick allowance.
	threshold := max(time.Second/30, time.Duration(2*float64(time.Second)*float64(max(1, ticks))/c.vm.FPS()))
	if phase == "host_gap" {
		/*
			The host still updates at least 60 times a second even when a
			project deliberately uses a lower simulation rate.
		*/
		threshold = max(time.Second/30, 2*time.Second/time.Duration(c.hostTPS()))
	}
	if elapsed < threshold {
		return
	}
	last := &g.lastSlowUpdate
	switch phase {
	case "audio":
		last = &g.lastSlowAudio
	case "draw_cpu":
		last = &g.lastSlowDraw
	case "host_gap":
		last = &g.lastSlowHost
	}
	now := time.Now()
	if now.Sub(*last) < 5*time.Second {
		return
	}
	*last = now
	message := fmt.Sprintf("slow phase=%s elapsed=%s ticks=%d frame=%d fps=%g pending_ticks=%.2f", phase, elapsed, ticks, c.vm.Frames(), c.vm.FPS(), c.credit)
	message += fmt.Sprintf(" focused=%t minimized=%t actual_tps=%.1f actual_fps=%.1f", g.sampleWindowFocus(), g.windowMinimized, ebiten.ActualTPS(), ebiten.ActualFPS())
	if phase == "host_gap" {
		message += fmt.Sprintf(" draw_calls=%d host_draw_cpu=%s host_other_time=%s", g.hostDrawCalls, g.hostDrawTime, max(0, elapsed-g.hostDrawTime))
	}
	if phase == "vm" {
		if c.graphics != nil && c.graphics.measure {
			graphics := c.graphics.stats
			message += fmt.Sprintf(" graphics_parallel_ticks=%d graphics_batches=%d graphics_queries=%d graphics_query_wait=%s graphics_queue_wait=%s graphics_dispatch=%s",
				graphics.ParallelTicks, graphics.Batches, graphics.Queries, graphics.QueryWait, graphics.QueueWait, graphics.DrawTime)
		}
		stats := g.stepDiagnostics
		message += fmt.Sprintf(" scheduler=%s passes=%d turns=%d steps=%d native=%t", stats.Elapsed, stats.Passes, stats.Turns, stats.Steps, stats.Native)

		for i, script := range stats.Longest {
			if script.Elapsed == 0 {
				break
			}
			// Bound project-provided names without retaining a trace per block.
			message += fmt.Sprintf(" script%d={target=%.120q hat=%.80q block=%.80q elapsed=%s steps=%d}", i+1, script.Target, script.Hat, script.Block, script.Elapsed, script.Steps)
		}
	}
	if phase == "vm" || phase == "draw_cpu" {
		// Uploads can flush pending pen work, so these durations may overlap.
		message += fmt.Sprintf(" texture_uploads=%d upload_bytes=%d upload_time=%s evictions=%d texture_cache_bytes=%d pen_submit=%s", g.gpu.uploadCount, g.gpu.uploadBytes, g.gpu.uploadTime, g.gpu.evictions, g.gpu.bytes, g.gpu.penSubmitTime)
		message += fmt.Sprintf(" prepare_time=%s pixel_time=%s texture_time=%s write_time=%s", g.gpu.prepareTime, g.gpu.pixelTime, g.gpu.textureTime, g.gpu.writeTime)
	}
	g.options.Diagnostic(message)
}

func (g *game) recordUpdate() {
	/*
		--frames and --at count execution opportunities, not skipped host updates.
		Stopped and paused updates retain the original bounded-window behavior.
	*/
	if g.controller.didTick {
		g.updates += g.controller.tickCount
	} else if g.controller.vm.Stopped() || g.controller.vm.Paused() {
		g.updates++
	}
	if limit := g.controller.options.Frames; limit > 0 && g.updates >= limit {
		g.limitReached = true
	}
}

func (g *game) updateFocus(focused bool) {
	if !focused {
		g.unfocused = true
		g.focusDraws = 0
		return
	}
	if g.unfocused {
		g.unfocused = false
		g.focusDraws = host.FocusRestoreDraws
		// Visibility probes must not suspend the VM while audio keeps playing.
		g.updateTime, g.updateBudget = 0, 0
		g.frameTicks = 0
		g.updateBudgetUntil = time.Time{}
	}
}

func (g *game) Draw(screen *ebiten.Image) {
	if g.options.Diagnostic != nil {
		start := time.Now()
		defer func() {
			g.hostDrawTime += time.Since(start)
			g.hostDrawCalls++
		}()
	}
	g.draw(screen, g.sampleWindowFocus())
}

func (g *game) draw(screen *ebiten.Image, focused bool) {
	if g.native != nil {
		g.drawNativeViews(screen)
		return
	}
	g.updateFocus(focused)
	if g.controller == nil {
		// Start loading after its first frame, even without focus.
		if focused || !g.drawn {
			g.drawLoading(screen)
			g.streamEditorFrame(screen)
			g.drawn = true
			g.focusDraws = max(0, g.focusDraws-1)
		}
		return
	}
	// Sample host timing even when composition is skipped.
	g.logDisplaySize(screen.Bounds().Size())
	g.sampleHostFrame()
	idlePreview := g.options.Editor != nil && g.options.Editor.Embedded && !g.options.Editor.WantsFrame()
	if (!focused || idlePreview) && !g.limitReached {
		// Finish pen work offscreen instead of accumulating it until focus returns.
		if pen := g.gpu.nativePen; pen != nil {
			pen.replayCommands()
			pen.flushStamps()
			pen.flushLines()
			pen.flushClear([4]*ebiten.Image{})
		}
		if idlePreview {
			return
		}
		if !g.windowMinimized {
			// Keep presentation active without recomposing the scene after losing focus.
			if g.backgroundFrame == nil {
				g.backgroundFrame = ebiten.NewImage(screen.Bounds().Dx(), screen.Bounds().Dy())
				g.backgroundFrame.DrawImage(screen, &ebiten.DrawImageOptions{Blend: ebiten.BlendCopy})
			}
			options := ebiten.DrawImageOptions{Blend: ebiten.BlendCopy}
			options.GeoM.Scale(float64(screen.Bounds().Dx())/float64(g.backgroundFrame.Bounds().Dx()),
				float64(screen.Bounds().Dy())/float64(g.backgroundFrame.Bounds().Dy()))
			screen.DrawImage(g.backgroundFrame, &options)
		}
		return
	}
	if g.backgroundFrame != nil {
		g.backgroundFrame.Deallocate()
		g.backgroundFrame = nil
	}
	if !g.shouldPresent(time.Now(), screen.Bounds().Size()) {
		return
	}
	g.focusDraws = max(0, g.focusDraws-1)
	if g.options.Diagnostic != nil {
		start := time.Now()
		g.gpu.measureUploads = true
		g.gpu.uploadCount, g.gpu.uploadBytes, g.gpu.uploadTime, g.gpu.evictions = 0, 0, 0, 0
		g.gpu.prepareTime, g.gpu.pixelTime, g.gpu.textureTime, g.gpu.writeTime = 0, 0, 0, 0
		g.gpu.penSubmitTime = 0
		defer func() {
			g.gpu.measureUploads = false
			g.reportSlowPhase("draw_cpu", time.Since(start), 1)
		}()
	}
	if g.controller.options.Settings != nil {
		g.drawPlayer(screen)
		g.streamEditorFrame(screen)
		g.captureFrame(screen)
		return
	}
	frame := g.controller.paint(g.face)
	if err := g.controller.assets.Err(); err != nil {
		g.drawErr = err
	}
	if g.canvas == nil || g.canvas.Bounds() != frame.Bounds() {
		if g.canvas != nil {
			g.canvas.Deallocate()
		}
		g.canvas = ebiten.NewImage(frame.Bounds().Dx(), frame.Bounds().Dy())
	}
	g.canvas.WritePixels(frame.Pix)
	screen.Clear()
	screen.DrawImage(g.canvas, nil)
	g.streamEditorFrame(screen)
	g.captureFrame(screen)
}

func (g *game) captureFrame(screen *ebiten.Image) {
	if g.limitReached && !g.captured {
		if path := g.controller.options.Screenshot; path != "" {
			captured := image.NewRGBA(screen.Bounds())
			screen.ReadPixels(captured.Pix)
			g.drawErr = saveScreenshot(path, captured)
		}
		g.captured = true
	}
}

func (g *game) Layout(outsideWidth, outsideHeight int) (int, int) {
	g.windowFocusRead, g.windowFullscreenRead = false, false
	g.windowDPI = 1
	if peer := g.options.Editor; peer != nil && peer.Embedded {
		outsideWidth, outsideHeight = peer.FrameSize(max(1, outsideWidth), max(1, outsideHeight))
	}
	g.outsideWidth, g.outsideHeight = outsideWidth, outsideHeight
	if g.controller == nil {
		if g.options.Settings != nil {
			return max(1, outsideWidth), max(1, outsideHeight)
		}
		return 480, 360 + ToolbarHeight + AnswerHeight
	}
	w, h := g.controller.renderer.Size()
	if g.controller.options.Settings != nil {
		return max(1, outsideWidth), max(1, outsideHeight)
	}
	return w, h + ToolbarHeight + AnswerHeight
}

func (g *game) LayoutF(width, height float64) (float64, float64) {
	g.windowFocusRead, g.windowFullscreenRead = false, false
	return g.layoutScale(width, height, ebiten.Monitor().DeviceScaleFactor())
}

func (g *game) layoutScale(width, height, scale float64) (float64, float64) {
	if g.native != nil {
		size := g.native.layout.Size
		g.outsideWidth, g.outsideHeight = size.X, size.Y
		g.windowDPI = 1
		if scale > 0 && !math.IsNaN(scale) && !math.IsInf(scale, 0) {
			g.windowDPI = scale
		}
		return float64(size.X), float64(size.Y)
	}
	packaged := g.options.Settings != nil || (g.controller != nil && g.controller.options.Settings != nil)
	if !packaged {
		w, h := g.Layout(int(width), int(height))
		return float64(w), float64(h)
	}
	if scale <= 0 || math.IsNaN(scale) || math.IsInf(scale, 0) {
		scale = 1
	}
	g.windowDPI = scale
	g.outsideWidth = max(1, int(math.Ceil(width*scale)))
	g.outsideHeight = max(1, int(math.Ceil(height*scale)))
	if peer := g.options.Editor; peer != nil && peer.Embedded {
		w, h := peer.FrameSize(g.outsideWidth, g.outsideHeight)
		g.windowDPI *= float64(w) / float64(g.outsideWidth)
		g.outsideWidth, g.outsideHeight = w, h
	}
	return float64(g.outsideWidth), float64(g.outsideHeight)
}

func (g *game) dpi() float64 {
	if g.windowDPI <= 0 {
		return 1
	}
	return g.windowDPI
}

func (g *game) pixels(n int) int {
	return int(math.Round(float64(n) * g.dpi()))
}

func (g *game) scaledFace(scale float64, stage bool) font.Face {
	face, previous := &g.displayFace, &g.displayScale
	if stage {
		face, previous = &g.stageFace, &g.stageScale
	}
	noAntialias := stage && g.controller != nil && g.controller.renderer.DisplayView().NoAntialias
	if *face == nil || *previous != scale || (stage && g.stageNoAntialias != noAntialias) {
		g.texts.clear()
		if *face != nil {
			(*face).Close()
		}
		*face, *previous = ui.Font(14*scale), scale
		if stage {
			g.stageNoAntialias = noAntialias
			if noAntialias {
				*face = media.WithoutFontAntialias(*face)
			}
		}
	}
	return *face
}

func (g *game) inputPosition(x, y int) (float64, float64) {
	if g.controller.options.Settings == nil {
		return float64(x), float64(y)
	}
	w, h := g.controller.renderer.Size()
	stage := g.stageBounds(g.outsideWidth, g.outsideHeight)
	sx := float64(x-stage.Min.X) * float64(w) / float64(stage.Dx())
	sy := float64(y-stage.Min.Y) * float64(h) / float64(stage.Dy())
	return sx, sy + ToolbarHeight
}

func saveScreenshot(path string, img image.Image) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
		return err
	}
	f, err := os.Create(absolute)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return fmt.Errorf("empty screenshot")
	}
	return nil
}
