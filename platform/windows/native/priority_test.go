package native

import (
	"errors"
	"fmt"
	"testing"
)

func TestGamePriorityRaisesNormalAndReadsBack(t *testing.T) {
	current := uint32(0x20)
	reads, writes := 0, 0
	message, err := configureGamePriority(func() (uint32, error) {
		reads++
		return current, nil
	}, func(priority uint32) error {
		writes++
		if priority != 0x8000 {
			t.Fatalf("requested priority = %#x", priority)
		}
		current = priority
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if current != 0x8000 || reads != 2 || writes != 1 {
		t.Fatalf("priority=%#x reads=%d writes=%d", current, reads, writes)
	}
	if message != "process_priority before=0x20 after=0x8000" {
		t.Fatalf("message = %q", message)
	}
}

func TestGamePriorityPreservesOtherClasses(t *testing.T) {
	for _, priority := range []uint32{0x40, 0x4000, 0x8000, 0x80, 0x100, 0x1234} {
		t.Run(fmt.Sprintf("%#x", priority), func(t *testing.T) {
			reads := 0
			_, err := configureGamePriority(func() (uint32, error) {
				reads++
				return priority, nil
			}, func(uint32) error {
				t.Fatal("changed an existing non-Normal priority")
				return nil
			})
			if err != nil || reads != 1 {
				t.Fatalf("reads=%d error=%v", reads, err)
			}
		})
	}
}

func TestGamePriorityReportsObservedClass(t *testing.T) {
	message, err := configureGamePriority(func() (uint32, error) { return 0x20, nil }, func(uint32) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if message != "process_priority before=0x20 after=0x20" {
		t.Fatalf("message = %q", message)
	}
}

func TestGamePriorityErrors(t *testing.T) {
	failure := errors.New("access denied")
	for _, stage := range []string{"read", "set", "verify"} {
		t.Run(stage, func(t *testing.T) {
			reads, writes := 0, 0
			_, err := configureGamePriority(func() (uint32, error) {
				reads++
				if stage == "read" || stage == "verify" && reads == 2 {
					return 0, failure
				}
				return 0x20, nil
			}, func(uint32) error {
				writes++
				if stage == "set" {
					return failure
				}
				return nil
			})
			if !errors.Is(err, failure) {
				t.Fatalf("error=%v", err)
			}
			if stage == "read" && writes != 0 {
				t.Fatal("set priority after failed read")
			}
			if stage == "set" && reads != 1 {
				t.Fatal("read priority after failed set")
			}
		})
	}
}
