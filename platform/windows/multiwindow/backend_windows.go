//go:build windows && amd64

package multiwindow

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"gowarp/core/window"
)

// SourceTitle identifies the render-only GLFW window within this process.
var SourceTitle = fmt.Sprintf("GoWarp render source %d", os.Getpid())

type Backend struct {
	bridge                bridge
	manager               *windowManager
	frame                 *frame
	views                 []window.View
	prepared              bool
	sourceWindow          uintptr
	start, timeOfProgress time.Time
	presents              uint64
}

func Open(views []window.View, centerMain bool) (*Backend, error) {
	if value := os.Getenv("EBITENGINE_GRAPHICS_LIBRARY"); value != "" && value != "directx" {
		return nil, fmt.Errorf("native multiwindow requires DirectX 11, not %s", value)
	}
	if value := os.Getenv("EBITENGINE_DIRECTX"); strings.Contains(value, "version=") && !strings.Contains(value, "version=11") {
		return nil, fmt.Errorf("native multiwindow requires DirectX 11")
	}

	if _, err := window.Pack(views); err != nil {
		return nil, err
	}
	m, err := startWindows()
	if err != nil {
		return nil, err
	}
	m.centerMain = centerMain
	b := &Backend{manager: m}
	b.bridge.manager = m
	if err = b.SetViews(views); err == nil {
		err = b.bridge.install()
	}
	if err != nil {
		return nil, errors.Join(err, b.Close())
	}
	return b, nil
}

// Prepare must run on Ebitengine's main thread after GLFW creates its HWND.
// Call it again after the first Draw, when Ebitengine has applied queued icons.
func (b *Backend) Prepare() error {
	if b.prepared {
		return b.manager.copySourceIcons(b.sourceWindow)
	}
	hwnd, err := concealSource(SourceTitle)
	if err != nil {
		return err
	}
	b.sourceWindow = hwnd
	b.bridge.armed.Store(true)
	b.prepared = true
	b.start = time.Now()
	b.timeOfProgress = b.start
	return nil
}

func (b *Backend) SetViews(views []window.View) error {
	layout, err := window.Pack(views)
	if err != nil {
		return err
	}
	handles, actual, err := b.manager.setViews(layout.Views)
	if err != nil {
		return err
	}
	layout, err = window.Pack(actual)
	if err != nil {
		return err
	}
	b.views = layout.Views
	if b.frame == nil || !slices.Equal(b.frame.handles, handles) || !slices.Equal(b.frame.layout.Tiles, layout.Tiles) || b.frame.revision != b.manager.revision.Load() {
		b.frame = &frame{layout: layout, handles: handles, revision: b.manager.revision.Load()}
	}
	return nil
}

// Publish is paired with DrawFinalScreen, never with Update: the copy rectangles
// must describe the pixels being submitted to this particular source Present.
func (b *Backend) Publish(ready bool) {
	if ready {
		b.bridge.frame.Store(b.frame)
	} else {
		b.bridge.frame.Store(nil)
	}
}

func (b *Backend) StartDrag(id string) error {
	return b.manager.startDrag(id)
}

func (b *Backend) Monitor(id string) (window.Monitor, error) {
	return b.manager.monitor(id)
}

func (b *Backend) Center(id string, all bool) error {
	views, err := b.manager.center(b.views, id, all)
	if err != nil {
		return err
	}
	layout, err := window.Pack(views)
	if err != nil {
		return err
	}
	b.views = layout.Views
	if b.frame != nil && !slices.Equal(b.frame.layout.Tiles, layout.Tiles) {
		b.frame = &frame{layout: layout, handles: slices.Clone(b.frame.handles), revision: b.frame.revision}
	}
	return nil
}

func (b *Backend) Views() []window.View { return slices.Clone(b.views) }

func (b *Backend) Poll() (window.Input, error) {
	if err := b.bridge.err(); err != nil {
		return window.Input{}, err
	}
	in, closed, current := b.manager.poll()
	views := b.views
	copied := false
	resized := false
	for i, previous := range views {
		for _, v := range current {
			if previous.ID != v.ID {
				continue
			}
			updated := previous
			updated.X, updated.Y = v.X, v.Y
			updated.State = v.State
			updated.Width, updated.Height = v.Width, v.Height
			if updated != previous {
				// Published frames may still reference the previous views.
				if !copied {
					views = slices.Clone(views)
					copied = true
				}
				views[i] = updated
				resized = resized || previous.Width != v.Width || previous.Height != v.Height
			}
			break
		}
	}
	if len(closed) > 0 {
		if !copied {
			views = slices.Clone(views)
		}
		views = slices.DeleteFunc(views, func(v window.View) bool { return slices.Contains(closed, v.ID) })
		if len(views) == 0 {
			return in, window.ErrClosed
		}
		if err := b.SetViews(views); err != nil {
			return in, err
		}
	} else if resized {
		layout, err := window.Pack(views)
		if err != nil {
			return in, err
		}
		b.views = layout.Views
		// Do not post a new SetViews during interactive resizing: that could apply
		// an older snapshot over a newer HWND size. Publish immutable GPU metadata
		// only when DrawFinalScreen submits the corresponding resized source atlas.
		b.frame = &frame{layout: layout, handles: slices.Clone(b.frame.handles), revision: b.frame.revision}
	} else {
		// Position and state changes do not affect atlas resources.
		b.views = views
	}
	count := b.bridge.presents.Load()
	if count != b.presents {
		b.presents = count
		b.timeOfProgress = time.Now()
	}
	if b.prepared && time.Since(b.start) > 5*time.Second && time.Since(b.timeOfProgress) > 5*time.Second {
		return in, fmt.Errorf("native window source presentation stopped")
	}
	return in, nil
}

// Close is called after RunGame returns, when there can be no more Present callbacks.
func (b *Backend) Close() error {
	err := b.bridge.close()
	if b.manager != nil {
		b.manager.close()
	}
	return err
}
