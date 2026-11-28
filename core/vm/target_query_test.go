package vm

import (
	"context"
	"fmt"
	"math"
	"slices"
	"testing"

	"gowarp/core/extension"
	"gowarp/core/project"
)

func queryFixture(clones int) (*Runtime, *extensionContext) {
	r := New(&project.Project{Targets: []*project.Target{
		{IsStage: true, Variables: map[string][]any{"global": {"global", "RED"}}},
		{Name: "A", Variables: map[string][]any{"tag": {"tag", "BLOCKED"}}},
	}})
	r.options.CloneLimit = clones + 10
	for range clones {
		r.createClone(r.targets[1])
	}
	return r, &extensionContext{r: r, th: &thread{target: r.targets[1]}}
}

func TestTargetQueryRepeatedMissAllocations(t *testing.T) {
	_, c := queryFixture(300)
	args := []any{extension.TargetQuery{Source: "_myself_", ClonesOnly: true, Variable: "tag", Value: "MISSING", First: true}}
	allocs := testing.AllocsPerRun(100, func() {
		got, err := c.Call("target.query", args...)
		if err != nil || len(got.([]string)) != 0 {
			t.Fatalf("query returned %v, %v", got, err)
		}
	})
	if allocs > 1 {
		t.Fatalf("unchanged lookup allocated %.0f objects per call", allocs)
	}
}

func TestTargetQueryChanges(t *testing.T) {
	r, c := queryFixture(2)
	first, second := logicTargetID(r.targets[2]), logicTargetID(r.targets[3])
	q := extension.TargetQuery{Source: "_myself_", ClonesOnly: true, Variable: "tag", Value: "red", First: true}
	check := func(want ...string) {
		t.Helper()
		got := hostResult(t, c, "target.query", q).([]string)
		if !slices.Equal(got, want) {
			t.Fatalf("query %+v = %v, want %v", q, got, want)
		}
		// The caller owns the returned slice.
		if len(got) > 0 {
			got[0] = "modified"
		}
	}
	check()
	hostResult(t, c, "variable.set", "tag", "RED", second)
	check(second)
	check(second)
	hostResult(t, c, "variable.set", "tag", "red", first)
	check(first)
	hostResult(t, c, "target.delete", first)
	check(second)
	hostResult(t, c, "variable.delete", "tag", second)
	check()
	hostResult(t, c, "variable.create", "tag", "tag", "RED", second)
	check(second)
	hostResult(t, c, "variable.set", "tag", "RED")
	hostResult(t, c, "target.delete", second)
	check()
	third := hostResult(t, c, "target.clone").(string)
	check(third)
	q.Variable = "global"
	check(third)
	hostResult(t, c, "variable.set", "global", "BLUE", "_stage_")
	check()
	hostResult(t, c, "variable.create", "global", "global", "RED", third)
	check(third)
	hostResult(t, c, "variable.delete", "global", third)
	check()
	r.GreenFlag()
	check()
}

func TestTargetQueryScratchComparison(t *testing.T) {
	r, c := queryFixture(1)
	id := logicTargetID(r.targets[2])
	values := []any{nil, false, true, 0.0, 2.0, "02", "", " ", "RED", "red", "아야네", "보호막",
		"İ", "i", "Infinity", math.Inf(1), math.NaN(), []any{1}, map[string]any{"a": 1}}
	for _, value := range values {
		hostResult(t, c, "variable.set", "tag", value, id)
		for _, expected := range values {
			q := extension.TargetQuery{Source: "_myself_", ClonesOnly: true, Variable: "tag", Value: expected}
			got := hostResult(t, c, "target.query", q).([]string)
			if match := extension.Compare(value, expected) == 0; (len(got) == 1) != match {
				t.Fatalf("Compare(%#v, %#v): query=%v, match=%t", value, expected, got, match)
			}
		}
	}
}

func TestTargetQueryNativeWrites(t *testing.T) {
	r, c := queryFixture(1)
	clone := r.targets[2]
	q := extension.TargetQuery{Source: "_myself_", ClonesOnly: true, Variable: "tag", Value: 2.0}
	check := func(want bool) {
		t.Helper()
		got := hostResult(t, c, "target.query", q).([]string)
		if (len(got) == 1) != want {
			t.Fatalf("query returned %v, want match=%t", got, want)
		}
	}
	check(false)
	f := &nativeFrame{r: r, t: clone}
	slot := &nativeVariable{}
	f.setNumber(slot, "tag", 2)
	f.flushVariable(slot)
	check(true)
	f.set(slot, "tag", "other")
	check(false)
	f.set(slot, "tag", "02")
	check(true)
	if f.err != nil {
		t.Fatal(f.err)
	}
}

func TestTargetQueryScriptWrites(t *testing.T) {
	for _, mode := range []string{"interpreter", "prepared", "calculation"} {
		t.Run(mode, func(t *testing.T) {
			r := New(calculationFixture())
			target := r.targets[1]
			c := &extensionContext{r: r, th: &thread{target: target}}
			q := extension.TargetQuery{Source: "_myself_", Variable: "x", Value: 1.25}
			if got := hostResult(t, c, "target.query", q).([]string); len(got) != 1 {
				t.Fatal(got)
			}
			if mode != "calculation" {
				disableCalculations(r)
			}
			if mode == "interpreter" {
				r.variableWrites = make(map[*project.Block]*variableWrite)
				for _, block := range target.source.Blocks {
					r.variableWrites[block] = nil
				}
			}
			if err := r.step(context.Background(), &thread{target: target, pc: "c0"}); err != nil {
				t.Fatal(err)
			}
			if got := hostResult(t, c, "target.query", q).([]string); len(got) != 0 {
				t.Fatalf("query kept the old value after %s writes: %v", mode, got)
			}
			q.Value = target.variables["x"].Value
			if got := hostResult(t, c, "target.query", q).([]string); len(got) != 1 {
				t.Fatalf("query missed the new value: %v", got)
			}
		})
	}
}

func TestTargetQueryExternalWrites(t *testing.T) {
	r := monitorRuntime(t)
	c := &extensionContext{r: r, th: &thread{target: r.stage}}
	q := extension.TargetQuery{Source: "_stage_", Variable: "v", Value: 1.0}
	check := func(want bool) {
		t.Helper()
		got := hostResult(t, c, "target.query", q).([]string)
		if (len(got) == 1) != want {
			t.Fatalf("query returned %v, want match=%t", got, want)
		}
	}
	check(true)
	r.SetMonitorValue(0, "2")
	check(false)
	r.SetMonitorSlider(0, 1)
	check(true)
	r.stage.source.Variables["v"] = []any{"score", 1.0, true}
	r.ApplyCloud("v", "3")
	check(false)
	r.ApplyCloud("v", "1")
	check(true)
	d, finish := r.observeVariables(display(r.stage))
	d.State.Variables["v"] = Variable{Name: "score", Value: 4.0}
	check(false)
	d.State.Variables["v"] = Variable{Name: "score", Value: 1.0}
	finish()
	check(true)
}

func TestTargetQueryCapacityAndSourceOrder(t *testing.T) {
	r := eventProject(t, `[{"isStage":true},{"name":"A","variables":{"tag":["tag","RED"]}},{"name":"B","variables":{"tag":["tag","RED"]}}]`)
	c := &extensionContext{r: r, th: &thread{target: r.targets[1]}}
	a := hostResult(t, c, "target.clone", "A").(string)
	b := hostResult(t, c, "target.clone", "B").(string)
	a2 := hostResult(t, c, "target.clone", "A").(string)
	for i := range 20 {
		hostResult(t, c, "target.query", extension.TargetQuery{Value: i, Variable: "tag"})
	}
	for _, tc := range []struct {
		source string
		want   []string
	}{
		{"", []string{a, b, a2}},
		{"A", []string{a, a2}},
		{"B", []string{b}},
	} {
		q := extension.TargetQuery{Source: tc.source, ClonesOnly: true, Variable: "tag", Value: "red"}
		for range 2 {
			got := hostResult(t, c, "target.query", q).([]string)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("source=%q: %v, want %v", tc.source, got, tc.want)
			}
		}
	}
}

func BenchmarkTargetQuery(b *testing.B) {
	for _, clones := range []int{32, 300, 1000} {
		b.Run(fmt.Sprint(clones), func(b *testing.B) {
			for _, hit := range []bool{false, true} {
				b.Run(fmt.Sprintf("hit=%t", hit), func(b *testing.B) {
					r, c := queryFixture(clones)
					if hit {
						r.targets[len(r.targets)-1].variables["tag"].Value = "MATCH"
					}
					args := []any{extension.TargetQuery{Source: "_myself_", ClonesOnly: true, Variable: "tag", Value: "MATCH", First: true}}
					b.ReportAllocs()
					for b.Loop() {
						got, err := c.Call("target.query", args...)
						if err != nil || (len(got.([]string)) == 1) != hit {
							b.Fatalf("query returned %v, %v", got, err)
						}
					}
				})
			}
		})
	}
}

func BenchmarkTargetQueryChangingFilter(b *testing.B) {
	for _, cached := range []bool{false, true} {
		b.Run(fmt.Sprintf("cached=%t", cached), func(b *testing.B) {
			r, _ := queryFixture(300)
			q := extension.TargetQuery{ClonesOnly: true, Variable: "tag", Value: "일치", First: true}
			last := r.targets[len(r.targets)-1]
			last.variables["tag"].set("일치")
			want := logicTargetID(last)
			changing := r.targets[2].variables["tag"]
			values := [2]string{"불일치", "다른 값"}
			i := 0
			b.ReportAllocs()
			for b.Loop() {
				changing.set(values[i%2])
				i++
				var ids []string
				if cached {
					ids = r.queryTargets(1, q)
				} else {
					for _, target := range r.targets {
						if target.dead || target.sourceIndex != 1 || target.state.CloneID == 0 {
							continue
						}
						if extension.Compare(target.variables["tag"].Value, q.Value) == 0 {
							ids = append(ids, logicTargetID(target))
							break
						}
					}
				}
				if len(ids) != 1 || ids[0] != want {
					b.Fatal(ids)
				}
			}
		})
	}
}
