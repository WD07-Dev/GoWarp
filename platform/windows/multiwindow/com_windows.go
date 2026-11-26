//go:build windows && amd64

// Package multiwindow shares the Ebitengine DX11 device with native composition windows.
package multiwindow

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iidFactory     = windows.GUID{Data1: 0x7b7166ec, Data2: 0x21c7, Data3: 0x44ae, Data4: [8]byte{0xb2, 0x1a, 0xc9, 0xae, 0x32, 0x1a, 0xe3, 0x69}}
	iidFactory2    = windows.GUID{Data1: 0x50c83a1c, Data2: 0xe072, Data3: 0x4c48, Data4: [8]byte{0x87, 0xb0, 0x36, 0x30, 0xfa, 0x36, 0xa6, 0xd0}}
	iidFactory4    = windows.GUID{Data1: 0x1bc6ea02, Data2: 0xef36, Data3: 0x464f, Data4: [8]byte{0xbf, 0x0c, 0x21, 0xca, 0x39, 0xe5, 0x16, 0x8a}}
	iidDevice11    = windows.GUID{Data1: 0xdb6f6ddb, Data2: 0xac77, Data3: 0x4e88, Data4: [8]byte{0x82, 0x53, 0x81, 0x9d, 0xf9, 0xbb, 0xf1, 0x40}}
	iidDXGIDevice  = windows.GUID{Data1: 0x54ec77fa, Data2: 0x1377, Data3: 0x44e6, Data4: [8]byte{0x8c, 0x32, 0x88, 0xfd, 0x5f, 0x44, 0xc8, 0x4c}}
	iidTexture2D   = windows.GUID{Data1: 0x6f15aaf2, Data2: 0xd208, Data3: 0x4e89, Data4: [8]byte{0x9a, 0xb4, 0x48, 0x95, 0x35, 0xd3, 0x4f, 0x9c}}
	iidComposition = windows.GUID{Data1: 0xc37ea93a, Data2: 0xe7aa, Data3: 0x450d, Data4: [8]byte{0xb1, 0x6f, 0x97, 0x46, 0xcb, 0x04, 0x07, 0xf3}}
)

type comObject struct {
	vtable unsafe.Pointer
}

type patch struct {
	slot        *uintptr
	original    uintptr
	replacement uintptr
}

type swapDesc struct {
	Width, Height, Format                                     uint32
	Stereo                                                    int32
	SampleCount, SampleQuality                                uint32
	Usage, BufferCount, Scaling, SwapEffect, AlphaMode, Flags uint32
}

type textureDesc struct {
	Width, Height, MipLevels, ArraySize, Format uint32
	SampleCount, SampleQuality                  uint32
	Usage, BindFlags, CPUAccessFlags, MiscFlags uint32
}

type box struct {
	Left, Top, Front, Right, Bottom, Back uint32
}

func method(object *comObject, index int) uintptr {
	return *(*uintptr)(unsafe.Add(object.vtable, uintptr(index)*unsafe.Sizeof(uintptr(0))))
}

//go:uintptrescapes
func call(object *comObject, index int, args ...uintptr) uintptr {
	var parameters [12]uintptr
	if len(args) >= len(parameters) {
		panic("too many COM arguments")
	}
	parameters[0] = uintptr(unsafe.Pointer(object))
	copy(parameters[1:], args)
	result, _, _ := syscall.SyscallN(method(object, index), parameters[:1+len(args)]...)
	runtime.KeepAlive(object)
	return result
}

func hresult(operation string, result uintptr) error {
	if int32(result) < 0 {
		return fmt.Errorf("%s: HRESULT 0x%08x", operation, uint32(result))
	}
	return nil
}

func query(object *comObject, iid *windows.GUID) (*comObject, error) {
	var result *comObject
	hr := call(object, 0, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&result)))
	runtime.KeepAlive(iid)
	return result, hresult("QueryInterface", hr)
}

func release(object *comObject) {
	if object != nil {
		call(object, 2)
	}
}

func (p *patch) install(object *comObject, index int, callback uintptr) error {
	p.slot = (*uintptr)(unsafe.Add(object.vtable, uintptr(index)*unsafe.Sizeof(uintptr(0))))
	p.original = atomic.LoadUintptr(p.slot)
	p.replacement = callback
	return p.exchange(p.original, callback)
}

func (p *patch) exchange(old, next uintptr) error {
	var previous uint32
	address := uintptr(unsafe.Pointer(p.slot))
	if err := windows.VirtualProtect(address, unsafe.Sizeof(uintptr(0)), windows.PAGE_READWRITE, &previous); err != nil {
		return fmt.Errorf("protect DXGI vtable: %w", err)
	}
	changed := atomic.CompareAndSwapUintptr(p.slot, old, next)
	var ignored uint32
	err := windows.VirtualProtect(address, unsafe.Sizeof(uintptr(0)), previous, &ignored)
	if !changed {
		return fmt.Errorf("DXGI vtable changed by another hook; refusing to overwrite it")
	}
	return err
}

func (p *patch) restore() error {
	if p.slot == nil || atomic.LoadUintptr(p.slot) == p.original {
		return nil
	}
	return p.exchange(p.replacement, p.original)
}

func buffer(swap *comObject) (*comObject, error) {
	var texture *comObject
	hr := call(swap, 9, 0, uintptr(unsafe.Pointer(&iidTexture2D)), uintptr(unsafe.Pointer(&texture)))
	return texture, hresult("GetBuffer", hr)
}
