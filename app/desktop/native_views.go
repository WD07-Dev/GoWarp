package desktop

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"slices"

	"github.com/hajimehoshi/ebiten/v2"

	"gowarp/app/config"
	"gowarp/app/host"
	"gowarp/core/extension"
	"gowarp/core/window"
)

type nativeViews struct {
	backend                window.Backend
	layout                 window.Layout
	dirty, prepared, drawn bool
	iconsReady             bool
	gameWindowSize         image.Point
	input                  window.Input
}

/*
Settings use Ebitengine’s startup-monitor DIP scale. Explicit views and
runtime Window API calls use physical pixels.
*/
func nativeWindowViews(s *config.Settings, scale float64) ([]window.View, image.Point) {
	if scale <= 0 {
		scale = 1
	}
	pixels := func(size int) int { return max(1, int(math.Round(float64(size)*scale))) }
	views := slices.Clone(s.Platform.Views)
	var gameWindowSize image.Point
	if len(views) == 0 {
		w, h := s.DisplaySize()
		w, h = pixels(w), pixels(h)
		views = []window.View{{ID: "main", Title: s.Player.Title, X: 100, Y: 100, Width: w, Height: h,
			StageWidth: float64(s.Width), StageHeight: float64(s.Height)}}
	}
	for i := range views {
		if views[i].ID != "main" {
			continue
		}
		v := &views[i]
		v.Border, v.Resizable = s.Platform.Border, s.Platform.Resizable
		v.Opacity = nil
		v.Topmost = s.Platform.Topmost
		v.DisableMinimize, v.DisableMaximize = s.Platform.DisableMinimize, s.Platform.DisableMaximize
		v.DisableClose, v.Fullscreen = s.Platform.DisableClose, s.Platform.Fullscreen
		if !v.Fullscreen && (s.Loading.Width > 0 || s.Loading.Height > 0) {
			gameWindowSize = image.Pt(v.Width, v.Height)
			if s.Loading.Width > 0 {
				v.Width = pixels(s.Loading.Width)
			}
			if s.Loading.Height > 0 {
				v.Height = pixels(s.Loading.Height)
			}
		}
	}
	return views, gameWindowSize
}

func (g *game) openNativeViews() error {
	s := g.options.Settings
	if s == nil || !s.Platform.MultiWindow {
		return nil
	}
	if g.options.Screenshot != "" {
		return fmt.Errorf("native multiwindow does not support screenshot readback")
	}
	if g.options.Editor != nil {
		return fmt.Errorf("native multiwindow requires a standalone player")
	}
	views, gameWindowSize := nativeWindowViews(s, ebiten.Monitor().DeviceScaleFactor())
	layout, err := window.Pack(views)
	if err != nil {
		return err
	}
	backend, title, err := host.OpenNativeWindows(views, !s.Platform.Fullscreen)
	if err != nil {
		return err
	}
	layout, err = window.Pack(backend.Views())
	if err != nil {
		backend.Close()
		return err
	}
	g.native = &nativeViews{backend: backend, layout: layout, gameWindowSize: gameWindowSize}
	ebiten.SetFullscreen(false)
	ebiten.SetWindowTitle(title)
	ebiten.SetWindowDecorated(false)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeDisabled)
	ebiten.SetWindowMousePassthrough(true)
	ebiten.SetWindowVisible(true)
	g.sizeNativeSource()
	return nil
}

func (g *game) sizeNativeSource() {
	size := g.native.layout.Size
	scale := ebiten.Monitor().DeviceScaleFactor()
	if scale <= 0 {
		scale = 1
	}
	w, h := max(2, int(math.Ceil(float64(size.X)/scale))), max(2, int(math.Ceil(float64(size.Y)/scale)))
	if currentW, currentH := ebiten.WindowSize(); currentW != w || currentH != h {
		ebiten.SetWindowSize(w, h)
	}
}

func (g *game) pollNativeViews() error {
	n := g.native
	if n == nil {
		return nil
	}
	if !n.prepared || (n.drawn && !n.iconsReady) {
		var err error
		ebiten.RunOnMainThread(func() { err = n.backend.Prepare() })
		if err != nil {
			return err
		}
		n.iconsReady = n.prepared
		n.prepared = true
	}
	in, err := n.backend.Poll()
	if errors.Is(err, window.ErrClosed) {
		return ebiten.Termination
	}
	if err != nil {
		return err
	}
	n.input = in
	if !n.dirty {
		views := n.backend.Views()
		if slices.Equal(n.layout.Views, views) {
			return nil
		}
		layout, err := window.Pack(views)
		if err != nil {
			return err
		}
		changed := n.layout.Size != layout.Size
		n.layout = layout
		if changed {
			g.sizeNativeSource()
		}
	}
	return nil
}

func (g *game) syncNativeViews() error {
	n := g.native
	if n == nil || !n.dirty {
		return nil
	}
	if err := n.backend.SetViews(n.layout.Views); err != nil {
		return err
	}
	layout, err := window.Pack(n.backend.Views())
	if err != nil {
		return err
	}
	n.layout = layout
	n.dirty = false
	g.sizeNativeSource()
	return nil
}

func (g *game) nativeWindowCall(op string, args []any) (any, error) {
	n := g.native
	views, value, err := editWindows(n.layout.Views, op, args)
	if err != nil || op == "get" || op == "list" {
		return value, err
	}
	layout, err := window.Pack(views)
	if err != nil {
		return nil, err
	}
	n.layout, n.dirty = layout, true
	return value, nil
}

func editWindows(current []window.View, op string, args []any) ([]window.View, any, error) {
	if op == "list" {
		ids := make([]string, len(current))
		for i, v := range current {
			ids[i] = v.ID
		}
		return current, ids, nil
	}
	if len(args) == 0 {
		return current, nil, fmt.Errorf("window %s requires an ID or window description", op)
	}
	id := extension.String(args[0])
	index := slices.IndexFunc(current, func(v window.View) bool { return v.ID == id })
	if op != "create" && index < 0 {
		return current, nil, fmt.Errorf("unknown native window %q", id)
	}
	if op == "get" {
		data, err := json.Marshal(current[index])
		return current, string(data), err
	}
	views := slices.Clone(current)
	if op == "create" {
		var v window.View
		if err := decodeView(id, &v); err != nil {
			return current, nil, err
		}
		views = append(views, v)
	} else {
		v := &views[index]
		switch op {
		case "move":
			if len(args) != 3 {
				return current, nil, fmt.Errorf("move requires ID, x, y")
			}
			x, y := extension.Number(args[1]), extension.Number(args[2])
			if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) || math.Abs(x) > 100000 || math.Abs(y) > 100000 {
				return current, nil, fmt.Errorf("invalid desktop coordinates")
			}
			v.X, v.Y = int(math.Round(x)), int(math.Round(y))
		case "viewport":
			if len(args) != 3 && len(args) != 5 {
				return current, nil, fmt.Errorf("viewport requires ID, x, y and optionally width, height")
			}
			v.StageX, v.StageY = extension.Number(args[1]), extension.Number(args[2])
			if len(args) == 5 {
				v.StageWidth, v.StageHeight = extension.Number(args[3]), extension.Number(args[4])
			}
		case "set":
			if len(args) != 2 {
				return current, nil, fmt.Errorf("set requires ID and a JSON object")
			}
			// JSON decoding must not mutate the live opacity before validation.
			if v.Opacity != nil {
				alpha := *v.Opacity
				v.Opacity = &alpha
			}
			if err := decodeView(extension.String(args[1]), v); err != nil {
				return current, nil, err
			}
			if v.ID != id {
				return current, nil, fmt.Errorf("set cannot change a window ID")
			}
		case "remove":
			views = slices.Delete(views, index, index+1)
		default:
			return current, nil, fmt.Errorf("unknown native window operation %q", op)
		}
	}
	if _, err := window.Pack(views); err != nil {
		return current, nil, err
	}
	return views, nil, nil
}

func decodeView(text string, v *window.View) error {
	if !bytes.HasPrefix(bytes.TrimSpace([]byte(text)), []byte("{")) {
		return fmt.Errorf("window description must be a JSON object")
	}
	d := json.NewDecoder(bytes.NewBufferString(text))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("unexpected data after window description")
	}
	return nil
}

func (g *game) nativeInput(in *Input) {
	n := g.native
	if n == nil {
		return
	}
	v := n.input
	*in = Input{MouseX: v.X, MouseY: v.Y + ToolbarHeight, MouseDown: v.Down, MousePressed: v.Pressed,
		Text: v.Text, Pressed: v.Keys, Released: v.Released, WheelY: v.Wheel,
		Enter: slices.Contains(v.Keys, "enter"), Backspace: slices.Contains(v.Keys, "backspace"),
		Escape: slices.Contains(v.Keys, "escape"), Delete: slices.Contains(v.Keys, "delete"), Restart: slices.Contains(v.Keys, "f5")}
}

func (g *game) drawNativeViews(dst *ebiten.Image) {
	n := g.native
	n.drawn = false
	if err := g.syncNativeViews(); err != nil {
		g.drawErr = err
		return
	}
	if dst.Bounds().Size() != n.layout.Size {
		return
	}
	if g.controller == nil {
		dst.Clear()
		for i, v := range n.layout.Views {
			if !v.Hidden {
				g.drawLoading(dst.SubImage(n.layout.Tiles[i]).(*ebiten.Image))
			}
		}
		g.drawn = true
		n.drawn = true
		return
	}
	g.sampleHostFrame()
	if !n.input.Visible {
		if pen := g.gpu.nativePen; pen != nil {
			pen.replayCommands()
			pen.flushStamps()
			pen.flushLines()
			pen.flushClear([4]*ebiten.Image{})
		}
		n.drawn = true
		return
	}
	c := g.controller
	w, h := c.renderer.Size()
	if g.stageCanvas == nil || g.stageCanvas.Bounds().Size() != image.Pt(w, h) {
		if g.stageCanvas != nil {
			g.stageCanvas.Deallocate()
		}
		g.stageCanvas = ebiten.NewImage(w, h)
	}
	scene := c.displayScene()
	g.gpu.transparent = true
	if err := g.gpu.Draw(g.stageCanvas, c.renderer, scene); err != nil {
		g.drawErr = err
		return
	}
	painter := newGPUOverlayPainter(g.stageCanvas, g.scaledFace(1, true), w, h, &g.texts)
	c.paintOverlays(painter, scene, 0, h)
	if g.waitingStart {
		g.drawStartOverlay(g.stageCanvas, g.stageCanvas.Bounds())
	}
	dst.Clear()
	for i, v := range n.layout.Views {
		if v.Hidden {
			continue
		}
		tile := n.layout.Tiles[i]
		op := ebiten.DrawImageOptions{Blend: ebiten.BlendCopy}
		if g.options.Settings.Canvas.Antialias {
			op.Filter = ebiten.FilterLinear
		}
		op.GeoM.Translate(-v.StageX, -v.StageY)
		op.GeoM.Scale(float64(v.Width)/v.StageWidth, float64(v.Height)/v.StageHeight)
		op.GeoM.Translate(float64(tile.Min.X), float64(tile.Min.Y))
		a := float32(v.Alpha())
		op.ColorScale.Scale(a, a, a, a)
		dst.SubImage(tile).(*ebiten.Image).DrawImage(g.stageCanvas, &op)
	}
	n.drawn = true
}

func (g *game) DrawFinalScreen(screen ebiten.FinalScreen, offscreen *ebiten.Image, geo ebiten.GeoM) {
	if g.native == nil {
		ebiten.DefaultDrawFinalScreen(screen, offscreen, geo)
		return
	}
	n := g.native
	size := n.layout.Size
	ready := n.drawn && offscreen.Bounds().Size() == size && screen.Bounds().Dx() >= size.X && screen.Bounds().Dy() >= size.Y
	n.backend.Publish(ready)
	if ready {
		screen.DrawImage(offscreen, &ebiten.DrawImageOptions{Blend: ebiten.BlendCopy})
	}
}
