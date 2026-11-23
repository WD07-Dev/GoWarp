package vm

import (
	"gowarp/core/project"
	"math"
	"testing"
)

func TestGraphicsTintIsCopiedAndClearsWithEffects(t *testing.T) {
	r := motionRuntime(t, `{}`)
	sprite := r.targets[1]
	c := &extensionContext{r: r, th: &thread{target: sprite}}
	original := [3]float64{0.25, 0.5, 1}
	if _, err := c.Call("graphics.tint", original); err != nil {
		t.Fatal(err)
	}
	r.createClone(sprite)
	clone := r.targets[len(r.targets)-1]
	if clone.state.Effects.Tint == nil || *clone.state.Effects.Tint != original {
		t.Fatal("clone did not inherit tint")
	}
	if _, err := c.Call("graphics.tint", [3]float64{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if *clone.state.Effects.Tint != original {
		t.Fatal("parent update mutated clone tint")
	}
	if _, err := r.looksStatement(c.th, &project.Block{Opcode: "looks_cleargraphiceffects"}, nil); err != nil {
		t.Fatal(err)
	}
	if sprite.state.Effects.Tint != nil {
		t.Fatal("clear effects retained tint")
	}
	if _, err := c.Call("graphics.tint", [3]float64{math.NaN(), -1, 2}, "_stage_"); err != nil {
		t.Fatal(err)
	}
	if got := *r.stage.state.Effects.Tint; got != [3]float64{0, 0, 1} {
		t.Fatal("invalid tint normalization", got)
	}
	if _, err := c.Call("graphics.tint", [3]float64{1, 1, 1}, "_stage_"); err != nil {
		t.Fatal(err)
	}
	if r.stage.state.Effects.Tint != nil {
		t.Fatal("white tint should restore neutral fast path")
	}
	r.stopAll()
	if clone.state.Effects.Tint != nil {
		t.Fatal("stop retained tint")
	}
}
