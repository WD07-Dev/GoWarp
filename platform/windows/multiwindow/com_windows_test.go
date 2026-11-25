//go:build windows && amd64

package multiwindow

import (
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestCOMQueryPassesPointers(t *testing.T) {
	var table [1]uintptr
	object := &comObject{vtable: unsafe.Pointer(&table[0])}
	returned := &comObject{}
	called := false
	table[0] = syscall.NewCallback(func(self *comObject, iid *windows.GUID, result **comObject) uintptr {
		called = self == object && *iid == iidFactory2
		*result = returned
		return 0
	})
	got, err := query(object, &iidFactory2)
	if err != nil || !called || got != returned {
		t.Fatalf("QueryInterface: called=%t result=%p err=%v", called, got, err)
	}
}

func TestPresentTestForwardsPointerAndResult(t *testing.T) {
	source := &comObject{}
	b := &bridge{}
	b.source.Store(source)
	called := false
	b.present.original = syscall.NewCallback(func(swap *comObject, interval, flags uintptr) uintptr {
		called = swap == source && interval == 2 && flags == 1
		return 0x087a0001 // DXGI_STATUS_OCCLUDED
	})
	if got := b.onPresent(source, 2, 1); got != 0x087a0001 || !called {
		t.Fatalf("Present: result=%#x called=%t", got, called)
	}
	if b.busy.Load() || b.presents.Load() != 0 || b.err() != nil {
		t.Fatal("DXGI_PRESENT_TEST changed rendering state")
	}
}
