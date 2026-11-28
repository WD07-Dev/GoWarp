package desktop

import (
	"context"
	"fmt"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"gowarp/app/assets"
	"gowarp/app/compiler"
	"gowarp/app/runtimehost"
	"gowarp/core/media"
	"gowarp/core/project"
	"gowarp/core/vm"
	"gowarp/core/window"
)

type loadingResult struct {
	controller *controller
	err        error
}

type loadingImages struct {
	background, logo *assets.LoadingImage
}

// CPU preparation hands ownership to the native event loop through the result channel.
type projectLoader struct {
	cancel   context.CancelFunc
	done     chan struct{}
	result   chan loadingResult
	progress chan media.LoadProgress
	images   chan loadingImages
}

func startProjectLoader(ctx context.Context, path string, p *project.Project, options Options, events []vm.InputEvent) *projectLoader {
	ctx, cancel := context.WithCancel(ctx)
	l := &projectLoader{
		cancel: cancel, done: make(chan struct{}), result: make(chan loadingResult, 1),
		progress: make(chan media.LoadProgress, 1), images: make(chan loadingImages, 1),
	}
	go func() {
		defer close(l.done)
		c, err := l.prepare(ctx, path, p, options, events)
		l.result <- loadingResult{controller: c, err: err}
	}()
	return l
}

func (l *projectLoader) report(progress media.LoadProgress) {
	// Keep the latest report without letting a minimized window block loading.
	select {
	case <-l.progress:
	default:
	}
	select {
	case l.progress <- progress:
	default:
	}
}

func (l *projectLoader) prepare(ctx context.Context, path string, p *project.Project, options Options, events []vm.InputEvent) (*controller, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s := options.Settings; s != nil {
		if err := s.Loading.Validate(); err != nil {
			return nil, err
		}
		var pictures loadingImages
		var err error
		if len(s.Loading.Image) > 0 {
			pictures.background, err = assets.DecodeLoadingImage(s.Loading.Image)
			if err != nil {
				return nil, fmt.Errorf("loading image: %w", err)
			}
		}
		if len(s.Loading.Logo) > 0 {
			pictures.logo, err = assets.DecodeLoadingImage(s.Loading.Logo)
			if err != nil {
				return nil, fmt.Errorf("loading logo: %w", err)
			}
		}
		l.images <- pictures
	}
	l.report(media.LoadProgress{Label: "Opening project"})
	if p == nil {
		var err error
		if options.Prepare != nil {
			p, err = options.Prepare(ctx)
		} else {
			p, err = project.Load(path)
		}
		if err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("project is empty")
	}
	if err := compiler.ValidateOutputs(path, p, options.Screenshot); err != nil {
		return nil, err
	}
	prepareDisplay := options.Settings != nil && !options.Replay && options.Screenshot == ""
	lib, err := media.LoadRuntime(ctx, path, p, func(progress media.LoadProgress) {
		if prepareDisplay {
			progress.Total *= 2
		}
		l.report(progress)
	})
	if err != nil {
		return nil, err
	}
	var c *controller
	success := false
	defer func() {
		if !success {
			if c != nil {
				c.vm.CloseExtensions()
			}
			lib.Close()
		}
	}()
	if options.Settings != nil {
		if err := lib.SetAntialias(options.Settings.Canvas.Antialias); err != nil {
			return nil, err
		}
	}
	// Replays and captures need deterministic pixels at each recorded frame.
	if prepareDisplay {
		if err := lib.PrepareCostumePlayback(func(progress media.LoadProgress) {
			progress.Done += progress.Total
			progress.Total *= 2
			l.report(progress)
		}); err != nil {
			return nil, err
		}
		if err := lib.EnableDisplayPreparation(path, p); err != nil {
			return nil, err
		}
	}
	l.report(media.LoadProgress{Done: 1, Total: 1, Label: "Preparing to start"})
	c = newController(p, lib, options)
	if c.initErr != nil {
		return nil, c.initErr
	}
	if err := runtimehost.ConfigureSave(c.vm, path); err != nil {
		return nil, err
	}
	if options.Configure != nil {
		if err := options.Configure(c.vm); err != nil {
			return nil, err
		}
	}
	if err := c.vm.SetExtensions(options.Extensions); err != nil {
		return nil, err
	}
	if err := c.vm.QueueEvents(events); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if options.Settings != nil && !options.Replay {
		c.vm.UseRealtimeClock()
		if options.Screenshot == "" {
			c.cadenceNow = time.Now
		}
	}
	success = true
	return c, nil
}

func (g *game) updateLoading() error {
	/*
		Draw must return once before expensive work can begin, allowing the
		first loading frame to reach the native window.
	*/
	if !g.drawn {
		return nil
	}
	if g.loader == nil {
		g.loader = startProjectLoader(g.ctx, g.path, g.project, g.options, g.events)
	}
	select {
	case pictures := <-g.loader.images:
		g.loading.images = pictures
	default:
	}
	select {
	case progress := <-g.loader.progress:
		g.loading.progress = progress
	default:
	}
	select {
	case result := <-g.loader.result:
		if result.err != nil {
			return result.err
		}
		g.controller = result.controller
		g.controller.vm.SetWindowHandler(g.windowCall)
		g.audio = newAudioOutput(g.controller.assets, g.options.Mute)
		if s := g.options.Settings; s != nil {
			graphics, err := g.gpu.enablePen(g.controller.renderer)
			if err != nil {
				return err
			}
			started := time.Now()
			warmup := ebiten.NewImage(7, 1)
			if err := g.gpu.drawShaderWarmup(warmup); err != nil {
				warmup.Deallocate()
				return err
			}
			var pixels [28]byte
			/*
				Readback flushes the driver commands, including lazy shader
				compilation, before the first VM tick or music playback.
			*/
			warmup.ReadPixels(pixels[:])
			warmup.Deallocate()
			projectShaders := g.gpu.canvas.warmupPrograms(g.controller.vm.StageShaderSources())
			if g.options.Diagnostic != nil {
				g.options.Diagnostic(fmt.Sprintf("graphics warmup elapsed=%s shaders=7 project_shaders=%d", time.Since(started), projectShaders))
			}
			graphics.keepHistory = g.controller.vm.UsesColorSensing()
			g.controller.graphics = newGraphicsPipeline(deferredPen{graphics}, graphics.replayCommands)
			g.controller.vm.SetGraphics(g.controller.graphics)
			g.waitingStart = !s.Player.AutoStart && g.options.Editor == nil
			if err := g.restoreGameWindowSize(); err != nil {
				return err
			}
		} else if w, h := g.controller.renderer.Size(); w != 480 || h != 360 {
			scale := g.options.Scale
			if scale <= 0 {
				scale = 2
			}
			ebiten.SetWindowSize(int(float64(w)*scale), int(float64(h+ToolbarHeight+AnswerHeight)*scale))
		}
		ebiten.SetTPS(g.updateTPS())
		g.releaseLoadingImages()
	default:
	}
	return nil
}

func (g *game) restoreGameWindowSize() error {
	s := g.options.Settings
	if s == nil || s.Platform.Fullscreen || g.options.Editor != nil || s.Loading.Width == 0 && s.Loading.Height == 0 {
		return nil
	}
	if n := g.native; n != nil {
		if n.gameWindowSize.X == 0 {
			return nil
		}
		views := n.backend.Views()
		for i := range views {
			v := &views[i]
			if v.ID != "main" {
				continue
			}
			v.X += (v.Width - n.gameWindowSize.X) / 2
			v.Y += (v.Height - n.gameWindowSize.Y) / 2
			v.Width, v.Height = n.gameWindowSize.X, n.gameWindowSize.Y
		}
		layout, err := window.Pack(views)
		if err != nil {
			return err
		}
		n.layout, n.dirty = layout, true
		return g.syncNativeViews()
	}
	width, height := playerWindowSize(s)
	oldWidth, oldHeight := ebiten.WindowSize()
	x, y := ebiten.WindowPosition()
	ebiten.SetWindowSize(width, height)
	ebiten.SetWindowPosition(x+(oldWidth-width)/2, y+(oldHeight-height)/2)
	g.initMainWindow()
	return nil
}

func (g *game) close() {
	if g.frameTiming != nil && g.options.Diagnostic != nil {
		g.frameTiming.flush(time.Now(), "exit", g.options.Diagnostic)
	}
	if g.controller != nil && g.controller.graphics != nil {
		g.controller.graphics.close()
	}
	g.gpu.Close()
	g.texts.clear()
	if g.stageCanvas != nil {
		g.stageCanvas.Deallocate()
	}
	if g.backgroundFrame != nil {
		g.backgroundFrame.Deallocate()
	}
	if g.displayFace != nil {
		g.displayFace.Close()
	}
	if g.stageFace != nil {
		g.stageFace.Close()
	}
	if g.loader != nil {
		g.loader.cancel()
		<-g.loader.done
	}
	if g.audio != nil {
		g.audio.close()
	}
	if g.controller != nil {
		g.controller.vm.CloseExtensions()
		g.controller.assets.Close()
	} else if g.loader != nil {
		select {
		case result := <-g.loader.result:
			if result.controller != nil {
				result.controller.vm.CloseExtensions()
				result.controller.assets.Close()
			}
		default:
		}
	}
	g.releaseLoadingImages()
}
