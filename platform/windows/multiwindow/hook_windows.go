//go:build windows && amd64

package multiwindow

import (
	"errors"
	"fmt"
	"image"
	"runtime"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"

	"gowarp/core/window"
)

var dwmFlush = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmFlush")

type failure struct{ err error }

type bridge struct {
	factory, factory4                  *comObject
	create, createComposition, present patch
	source                             atomic.Pointer[comObject]
	armed                              atomic.Bool
	busy                               atomic.Bool
	failure                            atomic.Pointer[failure]
	presents                           atomic.Uint64
	device, context, composition       *comObject
	outputs                            map[uintptr]*output
	frame                              atomic.Pointer[frame]
	manager                            *windowManager
	applied                            *frame
}

type output struct {
	swap, target, visual *comObject
	tile                 image.Rectangle
	capacity, nextSize   image.Point
	next, last           *comObject
}

func (o *output) release() {
	release(o.last)
	release(o.next)
	release(o.visual)
	release(o.target)
	release(o.swap)
}

func (b *bridge) fail(err error) {
	if err != nil {
		b.failure.CompareAndSwap(nil, &failure{err})
	}
}

func (b *bridge) err() error {
	if value := b.failure.Load(); value != nil {
		return value.err
	}
	return nil
}

type frame struct {
	revision uint64
	layout   window.Layout
	handles  []uintptr
}

func (b *bridge) install() error {
	b.outputs = make(map[uintptr]*output)
	createFactory := windows.NewLazySystemDLL("dxgi.dll").NewProc("CreateDXGIFactory1")
	hr, _, err := createFactory.Call(uintptr(unsafe.Pointer(&iidFactory)), uintptr(unsafe.Pointer(&b.factory)))
	if hr != 0 {
		return fmt.Errorf("CreateDXGIFactory1: 0x%x (%v)", hr, err)
	}
	b.factory4, err = query(b.factory, &iidFactory4)
	if err != nil {
		return err
	}
	legacy := syscall.NewCallback(func(factory, device *comObject, desc *[2]uint32, result **comObject) uintptr {
		hr, _, _ := syscall.SyscallN(b.create.original, uintptr(unsafe.Pointer(factory)), uintptr(unsafe.Pointer(device)), uintptr(unsafe.Pointer(desc)), uintptr(unsafe.Pointer(result)))
		if int32(hr) >= 0 {
			b.capture(desc, result)
		}
		return hr
	})
	composition := syscall.NewCallback(func(factory, device *comObject, desc *[2]uint32, restrictOutput *comObject, result **comObject) uintptr {
		hr, _, _ := syscall.SyscallN(b.createComposition.original, uintptr(unsafe.Pointer(factory)), uintptr(unsafe.Pointer(device)), uintptr(unsafe.Pointer(desc)), uintptr(unsafe.Pointer(restrictOutput)), uintptr(unsafe.Pointer(result)))
		if int32(hr) >= 0 {
			b.capture(desc, result)
		}
		return hr
	})
	if err := b.create.install(b.factory, 10, legacy); err != nil {
		return err
	}
	return b.createComposition.install(b.factory4, 24, composition)
}

func (b *bridge) capture(desc *[2]uint32, result **comObject) {
	if !b.armed.Load() || b.source.Load() != nil || b.err() != nil || desc == nil || result == nil {
		return
	}
	if desc[0] <= 1 && desc[1] <= 1 {
		return
	}
	source := *result
	if source == nil {
		return
	}
	var device *comObject
	hr := call(source, 7, uintptr(unsafe.Pointer(&iidDevice11)), uintptr(unsafe.Pointer(&device)))
	if err := hresult("source is not a D3D11 swap chain", hr); err != nil {
		b.fail(err)
		return
	}
	b.device = device
	call(device, 40, uintptr(unsafe.Pointer(&b.context))) // GetImmediateContext
	if b.context == nil {
		b.fail(fmt.Errorf("D3D11 immediate context is missing"))
		return
	}
	call(source, 1) // Keep the exact source object alive until the hook is removed.
	b.source.Store(source)
	callback := syscall.NewCallback(b.onPresent)
	b.fail(b.present.install(source, 8, callback))
}

func (b *bridge) onPresent(swap *comObject, interval, flags uintptr) uintptr {
	if swap != b.source.Load() || !b.busy.CompareAndSwap(false, true) {
		hr, _, _ := syscall.SyscallN(b.present.original, uintptr(unsafe.Pointer(swap)), interval, flags)
		return hr
	}
	defer b.busy.Store(false)
	// DXGI_PRESENT_TEST does not submit a new frame.
	if flags&1 == 0 && b.err() == nil {
		b.fail(b.copyFrame(swap))
		b.presents.Add(1)
	}
	hr, _, _ := syscall.SyscallN(b.present.original, uintptr(unsafe.Pointer(swap)), interval, flags)
	return hr
}

func (b *bridge) syncOutputs(source *comObject, f *frame) error {
	if b.applied == f {
		return nil
	}
	var factory *comObject
	if err := hresult("swap chain GetParent", call(source, 6, uintptr(unsafe.Pointer(&iidFactory2)), uintptr(unsafe.Pointer(&factory)))); err != nil {
		return err
	}
	defer release(factory)
	if b.composition == nil {
		device, err := query(b.device, &iidDXGIDevice)
		if err != nil {
			return err
		}
		defer release(device)
		hr, _, _ := windows.NewLazySystemDLL("dcomp.dll").NewProc("DCompositionCreateDevice").Call(uintptr(unsafe.Pointer(device)), uintptr(unsafe.Pointer(&iidComposition)), uintptr(unsafe.Pointer(&b.composition)))
		if err := hresult("DCompositionCreateDevice", hr); err != nil {
			return err
		}
	}
	for i, hwnd := range f.handles {
		tile := f.layout.Tiles[i]
		o := b.outputs[hwnd]
		if o == nil {
			o = &output{}
			b.outputs[hwnd] = o
			if err := hresult("CreateTargetForHwnd", call(b.composition, 6, hwnd, 1, uintptr(unsafe.Pointer(&o.target)))); err != nil {
				return err
			}
			if err := hresult("CreateVisual", call(b.composition, 7, uintptr(unsafe.Pointer(&o.visual)))); err != nil {
				return err
			}
		}
		if o.swap == nil || tile.Dx() > o.capacity.X || tile.Dy() > o.capacity.Y {
			o.nextSize = image.Pt(max(tile.Dx(), o.capacity.X), max(tile.Dy(), o.capacity.Y))
			desc := swapDesc{Width: uint32(o.nextSize.X), Height: uint32(o.nextSize.Y), Format: 87, SampleCount: 1, Usage: 0x20, BufferCount: 2, SwapEffect: 3, AlphaMode: 1}
			if err := hresult("CreateSwapChainForComposition", call(factory, 24, uintptr(unsafe.Pointer(b.device)), uintptr(unsafe.Pointer(&desc)), 0, uintptr(unsafe.Pointer(&o.next)))); err != nil {
				return err
			}
		}
		o.tile = tile
	}
	return nil
}

func (b *bridge) copyFrame(source *comObject) error {
	f := b.frame.Load()
	if f == nil || (b.manager != nil && f.revision != b.manager.revision.Load()) {
		return nil
	}
	texture, err := buffer(source)
	if err != nil {
		return err
	}
	defer release(texture)
	var desc textureDesc
	call(texture, 10, uintptr(unsafe.Pointer(&desc)))
	if desc.Format != 87 || desc.SampleCount != 1 || int(desc.Width) < f.layout.Size.X || int(desc.Height) < f.layout.Size.Y {
		return fmt.Errorf("unexpected source texture: %+v", desc)
	}
	transition := b.applied != f
	if err := b.syncOutputs(source, f); err != nil {
		return err
	}
	var replaced []*comObject
	defer func() {
		for _, swap := range replaced {
			release(swap)
		}
	}()
	for i, hwnd := range f.handles {
		o := b.outputs[hwnd]
		if o.next == nil && o.last != nil && (f.layout.Views[i].Hidden || win.IsIconic(win.HWND(hwnd))) {
			continue
		}
		swap := o.swap
		if o.next != nil {
			swap = o.next
		}
		destination, err := buffer(swap)
		if err != nil {
			return err
		}
		if transition && o.last != nil && o.last != destination {
			previous := box{Right: uint32(o.capacity.X), Bottom: uint32(o.capacity.Y), Back: 1}
			call(b.context, 46, uintptr(unsafe.Pointer(destination)), 0, 0, 0, 0, uintptr(unsafe.Pointer(o.last)), 0, uintptr(unsafe.Pointer(&previous)))
			runtime.KeepAlive(previous)
		}
		region := box{Left: uint32(o.tile.Min.X), Top: uint32(o.tile.Min.Y), Right: uint32(o.tile.Max.X), Bottom: uint32(o.tile.Max.Y), Back: 1}
		call(b.context, 46, uintptr(unsafe.Pointer(destination)), 0, 0, 0, 0, uintptr(unsafe.Pointer(texture)), 0, uintptr(unsafe.Pointer(&region)))
		runtime.KeepAlive(region)
		if err := hresult("output Present", call(swap, 8, 0, 0)); err != nil {
			release(destination)
			return err
		}
		release(o.last)
		o.last = destination
		if o.next != nil {
			if err := hresult("SetContent", call(o.visual, 15, uintptr(unsafe.Pointer(o.next)))); err != nil {
				return err
			}
			if err := hresult("SetRoot", call(o.target, 3, uintptr(unsafe.Pointer(o.visual)))); err != nil {
				return err
			}
			replaced = append(replaced, o.swap)
			o.swap, o.next, o.capacity = o.next, nil, o.nextSize
		}
	}
	if !transition {
		return nil
	}
	if err := hresult("Commit prepared outputs", call(b.composition, 3)); err != nil {
		return err
	}
	if err := hresult("WaitForCommitCompletion", call(b.composition, 4)); err != nil {
		return err
	}
	if b.manager != nil {
		applied, err := b.manager.presented(f.revision, func() error {
			hr, _, _ := dwmFlush.Call()
			return hresult("DwmFlush", hr)
		})
		if err != nil {
			return err
		}
		if !applied {
			return nil
		}
	}
	active := make(map[uintptr]bool, len(f.handles))
	for _, hwnd := range f.handles {
		active[hwnd] = true
	}
	removed := false
	for hwnd, o := range b.outputs {
		if active[hwnd] {
			continue
		}
		if err := hresult("detach retired output", call(o.target, 3, 0)); err != nil {
			return err
		}
		o.release()
		delete(b.outputs, hwnd)
		removed = true
	}
	if removed {
		if err := hresult("Commit retired outputs", call(b.composition, 3)); err != nil {
			return err
		}
		if err := hresult("WaitForCommitCompletion", call(b.composition, 4)); err != nil {
			return err
		}
	}
	b.applied = f
	if b.manager != nil {
		b.manager.destroyRetiredWindows(b.outputs, f.revision)
	}
	return nil
}

func (b *bridge) close() error {
	err := errors.Join(b.present.restore(), b.createComposition.restore(), b.create.restore())
	for _, o := range b.outputs {
		if o.target != nil {
			call(o.target, 3, 0)
		}
		o.release()
	}
	if b.composition != nil {
		call(b.composition, 3)
	}
	release(b.composition)
	release(b.source.Load())
	release(b.context)
	release(b.device)
	release(b.factory4)
	release(b.factory)
	return err
}
