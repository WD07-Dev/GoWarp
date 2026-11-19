package vm

import (
	"image/color"
	"testing"
)

func TestPenLineAllocations(t *testing.T) {
	for _, operation := range []string{"pen down", "move"} {
		t.Run(operation, func(t *testing.T) {
			recorder := &surfaceRecorder{commands: make([]PenCommand, 0, 1)}
			r := &Runtime{graphics: recorder}
			sprite := &target{owner: r, state: TargetState{Pen: PenState{Down: true, Size: 3, Brightness: 100}}}
			allocs := testing.AllocsPerRun(100, func() {
				recorder.commands = recorder.commands[:0]
				if operation == "pen down" {
					if err := r.penDown(sprite); err != nil {
						t.Fatal(err)
					}
				} else {
					sprite.setXY(sprite.state.X+1, sprite.state.Y+2)
				}
			})
			if len(recorder.commands) != 1 || recorder.commands[0].Kind != "line" {
				t.Fatal("operation did not submit a line")
			}
			if allocs != 0 {
				t.Fatalf("line coordinates allocate per operation: %g", allocs)
			}
		})
	}
}

func TestPenLineRetainedCommands(t *testing.T) {
	recorder := &surfaceRecorder{}
	r := &Runtime{graphics: recorder}
	sprite := &target{owner: r, state: TargetState{X: 1, Y: 2, Pen: PenState{Size: 3, Saturation: 100, Brightness: 100}}}
	if err := r.penDown(sprite); err != nil {
		t.Fatal(err)
	}
	sprite.setXY(4, 6)
	sprite.state.Pen.Size = 7
	sprite.state.Pen.Hue = 50
	sprite.setXY(-3, 8)
	want := [][4]float64{{1, 2, 1, 2}, {1, 2, 4, 6}, {4, 6, -3, 8}}
	if len(recorder.commands) != len(want) {
		t.Fatalf("got %d commands, want %d", len(recorder.commands), len(want))
	}
	for i, command := range recorder.commands {
		if command.Kind != "line" || command.Line != want[i] {
			t.Fatalf("line %d: got %v, want %v", i, command.Line, want[i])
		}
		if command.Target.State.X != want[i][2] || command.Target.State.Y != want[i][3] || !command.Target.State.Pen.Down {
			t.Fatalf("line %d was submitted before updating the sprite", i)
		}
	}
	if recorder.commands[0].Size != 3 || recorder.commands[1].Color != (color.NRGBA{R: 255, A: 255}) ||
		recorder.commands[2].Size != 7 || recorder.commands[2].Color != (color.NRGBA{G: 255, B: 255, A: 255}) {
		t.Fatal("retained line commands lost their pen settings")
	}
	if !r.redrawRequested {
		t.Fatal("drawing did not request a redraw")
	}
}
