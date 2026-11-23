package desktop

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/image/font/basicfont"

	"gowarp/core/media"
	"gowarp/core/project"
	"gowarp/core/vm"
)

func TestPaintMonitorsAboveStageAndWithinViewport(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"variables":{"v":["score",123]}}],"monitors":[{"id":"v","opcode":"data_variable","mode":"large","x":10,"y":10,"visible":true},{"id":"v","opcode":"data_variable","mode":"large","x":100,"y":-20,"visible":true},{"id":"v","opcode":"data_variable","mode":"large","x":150,"y":350,"visible":true},{"id":"v","opcode":"data_variable","mode":"large","x":200,"y":10,"visible":false}]}`), &p); err != nil {
		t.Fatal(err)
	}
	c := newController(&p, &media.Library{}, Options{FPS: 30})
	frame := c.paint(basicfont.Face7x13)
	orange := color.RGBA{255, 140, 26, 255}
	if got := frame.RGBAAt(11, ToolbarHeight+11); got != orange {
		t.Fatalf("visible monitor not painted: %v", got)
	}
	if got := frame.RGBAAt(201, ToolbarHeight+11); got == orange {
		t.Fatal("hidden monitor painted")
	}
	if got := frame.RGBAAt(101, ToolbarHeight-1); got == orange {
		t.Fatal("monitor overwrote toolbar")
	}
	if got := frame.RGBAAt(151, ToolbarHeight+360); got == orange {
		t.Fatal("monitor overwrote answer area")
	}
}

func TestMonitorNormalAndListReadoutsUpdate(t *testing.T) {
	for _, list := range []bool{false, true} {
		m := vm.MonitorState{
			Visible: true,
			List:    list,
			Label:   "score",
			Value:   "1",
			Items:   []string{"1"},
			X:       10,
			Y:       10,
			Width:   140,
			Height:  80,
		}
		before := image.NewRGBA(image.Rect(0, 0, 480, 360))
		drawMonitors(before, basicfont.Face7x13, []vm.MonitorState{m}, 0)
		m.Value, m.Items = "2", []string{"2"}
		after := image.NewRGBA(before.Bounds())
		drawMonitors(after, basicfont.Face7x13, []vm.MonitorState{m}, 0)
		if bytes.Equal(before.Pix, after.Pix) {
			t.Fatalf("list=%v readout ignores changed value", list)
		}
		if before.RGBAAt(10, 10).A == 0 {
			t.Fatalf("list=%v panel missing", list)
		}
	}
}

func TestMonitorTextBoundsZeroWidthCharacters(t *testing.T) {
	got := monitorText(strings.Repeat("\u0301", 2000))
	if utf8.RuneCountInString(got) != 1025 || !strings.HasSuffix(got, "…") {
		t.Fatal("monitor text is unbounded")
	}
}
