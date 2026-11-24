package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"golang.org/x/image/font/basicfont"

	"gowarp/core/extension"
	"gowarp/core/media"
	"gowarp/core/project"
	"gowarp/core/vm"
)

func TestMonitorDoubleClickAndValueDrag(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"variables":{"v":["score",123]}}],"monitors":[{"id":"v","opcode":"data_variable","mode":"large","x":10,"y":10,"visible":true}]}`), &p); err != nil {
		t.Fatal(err)
	}
	c := newController(&p, &media.Library{}, Options{FPS: 30})
	c.paint(basicfont.Face7x13)
	click := Input{MouseX: 20, MouseY: ToolbarHeight + 20, MousePressed: true, MouseDown: true}
	c.monitors.update(c.vm, &click, 480, 360)
	c.monitors.update(c.vm, &Input{MouseX: 20, MouseY: ToolbarHeight + 20}, 480, 360)
	c.monitors.clickAt = time.Now()
	click.MousePressed, click.MouseDown = true, true
	c.monitors.update(c.vm, &click, 480, 360)
	if c.vm.Monitors()[0].Mode != "slider" || c.monitors.edit != nil {
		t.Fatal("double click must change mode rather than edit the value")
	}
	c.vm.NextMonitorMode(0)
	c.vm.NextMonitorMode(0) // Back to the value-only display.
	c.paint(basicfont.Face7x13)
	c.monitors.clickAt = time.Time{}
	click.MousePressed, click.MouseDown = true, true
	c.monitors.update(c.vm, &click, 480, 360)
	c.monitors.update(c.vm, &Input{MouseX: 80, MouseY: ToolbarHeight + 60, MouseDown: true}, 480, 360)
	m := c.vm.Monitors()[0]
	if m.X != 70 || m.Y != 50 || m.Value != "123" || c.monitors.edit != nil {
		t.Fatalf("value drag changed the wrong state: %+v", m)
	}
	if !c.monitors.clickAt.IsZero() {
		t.Fatal("drag must cancel the double-click sequence")
	}
}

func TestListMonitorResizeHandle(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"lists":{"l":["items",[1,2]]}}],"monitors":[{"id":"l","opcode":"data_listcontents","mode":"list","visible":true,"x":10,"y":10,"width":140,"height":110}]}`), &p); err != nil {
		t.Fatal(err)
	}
	c := newController(&p, &media.Library{}, Options{FPS: 30})
	c.paint(basicfont.Face7x13)
	c.monitors.update(c.vm, &Input{MouseX: 147, MouseY: ToolbarHeight + 117, MousePressed: true, MouseDown: true}, 480, 360)
	if c.monitors.drag == nil || c.monitors.drag.hit.kind != "resize" {
		t.Fatal("resize handle missing or covered by another control")
	}
	c.monitors.update(c.vm, &Input{MouseX: 207, MouseY: ToolbarHeight + 167, MouseDown: true}, 480, 360)
	if got := c.vm.Monitors()[0]; got.Width != 200 || got.Height != 160 || got.X != 10 || got.Y != 10 {
		t.Fatalf("resize changed wrong dimensions: %+v", got)
	}
	c.monitors.update(c.vm, &Input{MouseX: 5000, MouseY: 5000, MouseDown: true}, 480, 360)
	if got := c.vm.Monitors()[0]; got.Width != 480 || got.Height != 360 {
		t.Fatalf("resize exceeded stage dimensions: %+v", got)
	}
	c.monitors.update(c.vm, &Input{MouseX: -500, MouseY: -500, MouseDown: true}, 480, 360)
	if got := c.vm.Monitors()[0]; got.Width != 100 || got.Height != 60 || got.Mode != "list" {
		t.Fatalf("resize below minimum: %+v", got)
	}
}

func TestMonitorEditingConsumesGameInputAndUpdatesData(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"variables":{"v":["score",123]},"lists":{"l":["items",["one","two"]]}}],"monitors":[{"id":"v","opcode":"data_variable","mode":"large","x":10,"y":10,"visible":true},{"id":"l","opcode":"data_listcontents","x":10,"y":60,"width":160,"height":120,"visible":true}]}`), &p); err != nil {
		t.Fatal(err)
	}
	c := newController(&p, &media.Library{}, Options{FPS: 30})
	c.vm.Stop()
	c.paint(basicfont.Face7x13)
	if err := c.advance(context.Background(), Input{MouseX: 20, MouseY: ToolbarHeight + 20, MousePressed: true, MouseDown: true}); err != nil {
		t.Fatal(err)
	}
	if c.monitors.edit == nil {
		t.Fatal("click did not open value editor")
	}
	in := Input{Text: "42", Enter: true, Pressed: []string{"4", "2", "enter"}}
	c.monitors.update(c.vm, &in, 480, 360)
	if got := c.vm.Monitors()[0].Value; got != "42" {
		t.Fatalf("edited variable = %q", got)
	}
	if len(in.Pressed) != 0 || in.Enter || in.Text != "" {
		t.Fatal("editor input leaked into project")
	}
	c.paint(basicfont.Face7x13)
	c.monitors.update(c.vm, &Input{MouseX: 60, MouseY: ToolbarHeight + 90, MousePressed: true}, 480, 360)
	c.monitors.update(c.vm, &Input{Text: "changed", Enter: true}, 480, 360)
	if got := c.vm.Monitors()[1].Items[0]; got != "changed" {
		t.Fatalf("edited list item = %q", got)
	}
}

func TestHiddenMonitorDoesNotCaptureClicks(t *testing.T) {
	r := vm.New(&project.Project{Targets: []*project.Target{{IsStage: true}}})
	u := monitorUI{hits: []monitorHit{{index: 0, kind: "value", rect: image.Rect(0, 0, 100, 30)}}}
	in := Input{MouseX: 10, MouseY: ToolbarHeight + 10, MousePressed: true, MouseDown: true}
	u.update(r, &in, 480, 360)
	if !in.MousePressed || !in.MouseDown || u.edit != nil {
		t.Fatal("stale monitor captured game input")
	}
}

func TestMonitorEditCancelsWhenProjectChangesItem(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"lists":{"l":["items",["old"]]}}],"monitors":[{"id":"l","opcode":"data_listcontents","visible":true}]}`), &p); err != nil {
		t.Fatal(err)
	}
	r := vm.New(&p)
	u := monitorUI{edit: &monitorEdit{index: 0, item: 0, original: "old", text: "edited", dirty: true}}
	r.EditMonitorList(0, "replace", 0, "new")
	u.update(r, &Input{Enter: true}, 480, 360)
	if r.Monitors()[0].Items[0] != "new" || u.edit != nil {
		t.Fatal("editor overwrote a project update")
	}
}

func TestMonitorButtonsUseCurrentListState(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"lists":{"l":["items",["new"]]}}],"monitors":[{"id":"l","opcode":"data_listcontents","visible":true}]}`), &p); err != nil {
		t.Fatal(err)
	}
	r := vm.New(&p)
	u := monitorUI{hits: []monitorHit{{index: 0, item: 0, kind: "delete", value: "old", rect: image.Rect(0, 0, 20, 20)}}}
	in := Input{MouseX: 5, MouseY: ToolbarHeight + 5, MousePressed: true}
	u.update(r, &in, 480, 360)
	if r.MonitorListLength(0) != 1 {
		t.Fatal("stale row button deleted a changed item")
	}
	u.hits[0].kind = "add"
	u.hits[0].count = 0 // The game added an item since the last draw.
	in.MousePressed = true
	u.update(r, &in, 480, 360)
	if u.edit == nil || u.edit.item != 1 || r.MonitorListLength(0) != 2 {
		t.Fatal("add button did not select the newly appended item")
	}
}

func TestMonitorListScrollAddAndDelete(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"lists":{"l":["items",[0,1,2,3,4,5,6,7,8,9]]}}],"monitors":[{"id":"l","opcode":"data_listcontents","visible":true,"x":10,"y":10,"width":140,"height":110}]}`), &p); err != nil {
		t.Fatal(err)
	}
	c := newController(&p, &media.Library{}, Options{FPS: 30})
	c.paint(basicfont.Face7x13)
	c.monitors.update(c.vm, &Input{MouseX: 50, MouseY: ToolbarHeight + 50, WheelY: -100}, 480, 360)
	page := c.vm.MonitorWindow(c.monitors.offsets, 14)[0]
	if page.Offset != 7 || len(page.Items) != 3 || page.Items[2] != "9" {
		t.Fatalf("cannot scroll to last row: %+v", page)
	}
	c.paint(basicfont.Face7x13)
	c.monitors.update(c.vm, &Input{MouseX: 18, MouseY: ToolbarHeight + 105, MousePressed: true}, 480, 360)
	c.monitors.update(c.vm, &Input{Text: "last", Enter: true}, 480, 360)
	c.paint(basicfont.Face7x13)
	page = c.vm.MonitorWindow(c.monitors.offsets, 14)[0]
	if page.Count != 11 || page.Items[len(page.Items)-1] != "last" {
		t.Fatalf("added row is not editable/visible: %+v", page)
	}
	c.monitors.update(c.vm, &Input{MouseX: 133, MouseY: ToolbarHeight + 86, MousePressed: true}, 480, 360)
	if c.vm.Monitors()[0].Count != 10 {
		t.Fatal("delete button did not remove row")
	}
}

func TestMonitorSliderDragUsesValueRange(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true,"variables":{"v":["score",0]}}],"monitors":[{"id":"v","opcode":"data_variable","mode":"slider","visible":true,"x":10,"y":10,"sliderMin":-10,"sliderMax":10}]}`), &p); err != nil {
		t.Fatal(err)
	}
	c := newController(&p, &media.Library{}, Options{FPS: 30})
	c.paint(basicfont.Face7x13)
	var track image.Rectangle
	for _, hit := range c.monitors.hits {
		if hit.kind == "slider" {
			track = hit.rect
		}
	}
	if track.Empty() {
		t.Fatal("slider was not drawn")
	}
	c.monitors.update(c.vm, &Input{MouseX: float64(track.Min.X), MouseY: float64(ToolbarHeight + track.Min.Y + 1), MousePressed: true, MouseDown: true}, 480, 360)
	if c.vm.Monitors()[0].Value != "-10" {
		t.Fatal("slider minimum not applied")
	}
	c.monitors.update(c.vm, &Input{MouseX: 470, MouseY: ToolbarHeight + 50, MouseDown: true}, 480, 360)
	if c.vm.Monitors()[0].Value != "10" {
		t.Fatal("slider did not capture drag outside track")
	}
}

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

func TestReporterMonitorDragsWithoutEditing(t *testing.T) {
	var p project.Project
	if err := json.Unmarshal([]byte(`{"targets":[{"isStage":true}],"monitors":[{"id":"m","opcode":"sample_value","visible":true,"mode":"large","x":10,"y":10}]}`), &p); err != nil {
		t.Fatal(err)
	}
	c := newController(&p, &media.Library{}, Options{FPS: 60})
	factory := func() extension.Extension {
		return extension.Extension{ID: "sample", Blocks: []extension.Block{
			{Opcode: "sample_value", Kind: "reporter", Handler: func(*extension.Context) (extension.Result, error) {
				return extension.Result{Value: 60.0}, nil
			}},
		}}
	}
	if err := c.vm.SetExtensions(extension.Bundle{factory}); err != nil {
		t.Fatal(err)
	}

	if err := c.vm.Tick(context.Background(), 60, false); err != nil {
		t.Fatal(err)
	}
	c.paint(basicfont.Face7x13)
	c.monitors.update(c.vm, &Input{MouseX: 20, MouseY: ToolbarHeight + 20, MousePressed: true, MouseDown: true}, 480, 360)
	if c.monitors.edit != nil || c.monitors.drag == nil {
		t.Fatal("reporter should drag without a value editor")
	}
	c.monitors.update(c.vm, &Input{MouseX: 40, MouseY: ToolbarHeight + 40, MouseDown: true}, 480, 360)
	if c.vm.Monitors()[0].X != 30 {
		t.Fatal("reporter did not move")
	}
	c.monitors.update(c.vm, &Input{MouseX: 40, MouseY: ToolbarHeight + 40}, 480, 360)
	c.paint(basicfont.Face7x13)
	c.monitors.update(c.vm, &Input{MouseX: 40, MouseY: ToolbarHeight + 40, MousePressed: true, MouseDown: true}, 480, 360)
	c.monitors.update(c.vm, &Input{MouseX: 40, MouseY: ToolbarHeight + 40}, 480, 360)
	c.monitors.clickAt = time.Now()
	c.monitors.update(c.vm, &Input{MouseX: 40, MouseY: ToolbarHeight + 40, MousePressed: true, MouseDown: true}, 480, 360)
	if c.vm.Monitors()[0].Mode != "default" || c.monitors.edit != nil {
		t.Fatal("reporter double-click did not change display mode")
	}
}
