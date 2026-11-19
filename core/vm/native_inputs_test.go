package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"

	"gowarp/core/project"
)

func addNativeInputBlocks(t testing.TB, blocks project.Blocks) {
	t.Helper()
	mutation := map[string]json.RawMessage{}
	for key, value := range map[string]string{
		"proccode": "inputWait", "argumentids": `["a","b","c","d"]`,
		"argumentnames": `["a","b","c","d"]`, "argumentdefaults": `[0,0,0,0]`, "warp": "true",
	} {
		mutation[key], _ = json.Marshal(value)
	}
	blocks["inputDef"] = &project.Block{Opcode: "procedures_definition", TopLevel: true, Next: "inputWait", Inputs: map[string][]any{"custom_block": {float64(1), "inputProto"}}}
	blocks["inputProto"] = &project.Block{Opcode: "procedures_prototype", Mutation: mutation}
	blocks["inputWait"] = &project.Block{Opcode: "control_wait", Inputs: literalInputs(map[string]any{"DURATION": .02})}
	blocks["inputCall"] = &project.Block{Opcode: "procedures_call", Mutation: mutation, Inputs: map[string][]any{}}
	for _, id := range []string{"a", "b", "c", "d"} {
		blocks["inputCall"].Inputs[id] = []any{float64(2), "inputSum"}
	}
	blocks["inputSum"] = &project.Block{Opcode: "operator_add", Inputs: map[string][]any{"NUM1": {float64(2), "inputProduct"}, "NUM2": {float64(2), "inputArgument"}}}
	blocks["inputProduct"] = &project.Block{Opcode: "operator_multiply", Inputs: map[string][]any{"NUM1": {float64(1), []any{float64(12), "x", "x"}}, "NUM2": {float64(1), []any{float64(4), "1.25"}}}}
	blocks["inputArgument"] = &project.Block{Opcode: "argument_reporter_string_number", Fields: map[string][]any{"VALUE": {"parent", nil}}}
	blocks["inputHat"] = &project.Block{Opcode: "event_whenbroadcastreceived", TopLevel: true, Next: "inputCall"}
	blocks["inputRawCall"] = &project.Block{Opcode: "procedures_call", Mutation: mutation, Inputs: map[string][]any{
		"a": {float64(1), []any{float64(10), "007"}},
		"b": {float64(2), "inputArgument"},
		"c": {float64(2), "inputSum"},
	}}
	blocks["inputRepeatedCall"] = &project.Block{Opcode: "procedures_call", Mutation: mutation, Inputs: map[string][]any{}}
	for i, name := range []string{"a", "b", "c", "d"} {
		id := "inputRepeated" + name
		blocks[id] = &project.Block{Opcode: "operator_add", Inputs: map[string][]any{
			"NUM1": {float64(2), "inputSquared"},
			"NUM2": {float64(1), []any{float64(4), fmt.Sprint(i)}},
		}}
		blocks["inputRepeatedCall"].Inputs[name] = []any{float64(2), id}
	}
	blocks["inputSquared"] = &project.Block{Opcode: "operator_multiply", Inputs: map[string][]any{
		"NUM1": {float64(2), "inputArgument"}, "NUM2": {float64(2), "inputArgument"},
	}}
	blocks["inputSpecialDivide"] = &project.Block{Opcode: "operator_divide", Inputs: map[string][]any{
		"NUM1": {float64(1), []any{float64(12), "x", "x"}},
		"NUM2": {float64(1), []any{float64(12), "y", "y"}},
	}}
	blocks["inputSpecialCall"] = &project.Block{Opcode: "procedures_call", Mutation: mutation, Inputs: map[string][]any{
		"a": {float64(2), "inputSpecialDivide"},
		"b": {float64(1), []any{float64(10), "007"}},
		"c": {float64(2), "inputArgument"},
		"d": {float64(2), "inputSpecialDivide"},
	}}
	blocks["inputOtherArgument"] = &project.Block{Opcode: "argument_reporter_string_number", Fields: map[string][]any{"VALUE": {"other", nil}}}
	blocks["inputPairedCall"] = &project.Block{Opcode: "procedures_call", Mutation: mutation, Inputs: map[string][]any{
		"a": {float64(2), "inputSum"}, "b": {float64(2), "inputOtherArgument"},
	}}
	blocks["inputRandom"] = &project.Block{Opcode: "data_itemoflist", Fields: map[string][]any{"LIST": {"items", "items"}}, Inputs: literalInputs(map[string]any{"INDEX": "random"})}
	blocks["inputRandomCall"] = &project.Block{Opcode: "procedures_call", Mutation: mutation, Inputs: map[string][]any{
		"a": {float64(2), "inputSum"}, "b": {float64(2), "inputRandom"},
	}}
	aliasMutation := make(map[string]json.RawMessage, len(mutation))
	for key, value := range mutation {
		aliasMutation[key] = value
	}
	aliasMutation["proccode"] = json.RawMessage(`"inputAlias"`)
	aliasMutation["argumentnames"], _ = json.Marshal(`["a","a","c","d"]`)
	blocks["inputAliasDef"] = &project.Block{Opcode: "procedures_definition", TopLevel: true, Next: "inputWait", Inputs: map[string][]any{"custom_block": {float64(1), "inputAliasProto"}}}
	blocks["inputAliasProto"] = &project.Block{Opcode: "procedures_prototype", Mutation: aliasMutation}
	blocks["inputAliasCall"] = &project.Block{Opcode: "procedures_call", Mutation: aliasMutation, Inputs: map[string][]any{
		"a": {float64(2), "inputSum"}, "b": {float64(1), []any{float64(10), "007"}},
		"c": {float64(2), "inputSum"}, "d": {float64(2), "inputSum"},
	}}
}

func TestNativeGeneratesCallInputs(t *testing.T) {
	source, err := GenerateNative(nativeCodegenFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), `nativeCallInputs[1]["inputCall"]`) {
		t.Fatal("synchronous arguments of a yielding procedure were not compiled")
	}
	if strings.Contains(string(source), `nativeCallInputs[1]["inputRandomCall"]`) {
		t.Fatal("random input must retain interpreter evaluation and error ordering")
	}
}

func testNativeCallInputs(t *testing.T) {
	t.Run("repeated parameter reads", func(t *testing.T) {
		fast, slow := New(nativeCodegenFixture(t)), New(nativeCodegenFixture(t))
		slow.native = false
		for _, value := range []any{"007", "2.125", "", "NaN", "文字", float64(5), nil, true} {
			var states []map[string]any
			for _, r := range []*Runtime{fast, slow} {
				target := r.targets[1]
				th := &thread{target: target, warp: true, params: procedureParameters{"parent": {value: value}}}
				if _, err := r.call(context.Background(), th, target.source.Blocks["inputRepeatedCall"], nil, false); err != nil {
					t.Fatal(err)
				}
				states = append(states, parameterValues(th.params))
			}
			if !reflect.DeepEqual(states[0], states[1]) {
				t.Fatalf("value=%v: arguments differ: %v", value, states)
			}
		}
	})
	t.Run("numeric arguments survive suspended calls", func(t *testing.T) {
		for _, compiled := range []bool{false, true} {
			r := New(nativeCodegenFixture(t))
			r.native = compiled
			target := r.targets[1]
			call := target.source.Blocks["inputSpecialCall"]
			for _, pair := range [][2]float64{{0, 0}, {1, 0}, {-1, 0}, {math.Copysign(0, -1), 1}, {1.25, 2}} {
				target.variables["x"].Value, target.variables["y"].Value = pair[0], pair[1]
				th := &thread{target: target, warp: true, params: procedureParameters{"parent": {value: false}}}
				if _, err := r.call(context.Background(), th, call, nil, false); err != nil {
					t.Fatal(err)
				}
				if err := r.step(context.Background(), th); err != nil {
					t.Fatal(err)
				}
				if th.wake <= r.elapsed || len(th.stack) == 0 {
					t.Fatal("callee did not suspend with its arguments")
				}
				want := pair[0] / pair[1]
				for _, name := range []string{"a", "d"} {
					value := th.params[name]
					got, ok := value.boxed().(float64)
					if !value.numeric || !ok || math.Float64bits(got) != math.Float64bits(want) {
						t.Fatalf("compiled=%v %s lost raw number: got %v, want %v", compiled, name, value, want)
					}
					if value.num() != nativeNumber(want) {
						t.Fatalf("numeric consumer changed %v", want)
					}
				}
				if th.params["b"].boxed() != "007" || th.params["c"].boxed() != false {
					t.Fatal("raw string or boolean argument changed type")
				}
				th.stopScript()
				if th.params["parent"].boxed() != false {
					t.Fatal("return did not restore caller arguments")
				}
			}
		}
	})
	t.Run("forwarded numeric parameters do not allocate", func(t *testing.T) {
		r := New(nativeCodegenFixture(t))
		target := r.targets[1]
		th := &thread{target: target, params: procedureParameters{"parent": {number: 12.5, numeric: true}}}
		inputs := nativeCallInputs[target.sourceIndex]["inputRawCall"]
		if inputs == nil {
			t.Fatal("call inputs were not compiled")
		}
		params := make(procedureParameters, 4)
		allocations := testing.AllocsPerRun(100, func() {
			if err := inputs(context.Background(), r, th, params); err != nil {
				t.Fatal(err)
			}
		})
		if allocations != 0 || params["b"] != th.params["parent"] {
			t.Fatalf("forwarded number was reboxed: %g allocations, value=%v", allocations, params["b"])
		}
	})
	t.Run("numeric inputs do not allocate", func(t *testing.T) {
		for _, callID := range []string{"inputCall", "inputRepeatedCall"} {
			r := New(nativeCodegenFixture(t))
			target := r.targets[1]
			th := &thread{target: target, params: procedureParameters{"parent": {value: float64(3)}}}
			inputs := nativeCallInputs[target.sourceIndex][callID]
			if inputs == nil {
				t.Fatal("call inputs were not compiled")
			}
			params := make(procedureParameters, 4)
			allocations := testing.AllocsPerRun(100, func() {
				if err := inputs(context.Background(), r, th, params); err != nil {
					t.Fatal(err)
				}
			})
			for i, name := range []string{"a", "b", "c", "d"} {
				want := 6.75
				if callID == "inputRepeatedCall" {
					want = 9 + float64(i)
				}
				if params[name].num() != want {
					t.Fatalf("%s %s = %v, want %v", callID, name, params[name], want)
				}
			}
			if allocations != 0 {
				t.Fatalf("numeric input expressions allocated %g objects per call, want 0", allocations)
			}
			target.variables["x"].Value = float64(5)
			next := make(procedureParameters, 4)
			if err := inputs(context.Background(), r, th, next); err != nil {
				t.Fatal(err)
			}
			for i, name := range []string{"a", "b", "c", "d"} {
				before, after := 6.75, 9.25
				if callID == "inputRepeatedCall" {
					before, after = 9+float64(i), 9+float64(i)
				}
				if next[name].num() != after || params[name].num() != before {
					t.Fatalf("input result leaked between calls: previous=%v next=%v", params, next)
				}
			}
		}
	})
	for _, value := range []any{"007", "", "NaN", float64(5)} {
		fast, slow := New(nativeCodegenFixture(t)), New(nativeCodegenFixture(t))
		slow.native = false
		var states []map[string]any
		for _, r := range []*Runtime{fast, slow} {
			target := r.targets[1]
			target.variables["x"].Value = value
			th := &thread{target: target, warp: true, params: procedureParameters{"parent": {value: float64(3)}}}
			if _, err := r.call(context.Background(), th, target.source.Blocks["inputCall"], nil, false); err != nil {
				t.Fatal(err)
			}
			states = append(states, parameterValues(th.params))
			if th.stack[0].params["parent"].boxed() != float64(3) {
				t.Fatal("argument evaluation changed the caller")
			}
		}
		if !reflect.DeepEqual(states[0], states[1]) {
			t.Fatalf("value=%v: arguments differ: %v", value, states)
		}
	}
	t.Run("raw values and defaults", func(t *testing.T) {
		r := New(nativeCodegenFixture(t))
		target := r.targets[1]
		th := &thread{target: target, warp: true}
		if _, err := r.call(context.Background(), th, target.source.Blocks["inputRawCall"], nil, false); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"a": "007", "b": float64(0), "c": float64(3.75), "d": float64(0)}
		if !reflect.DeepEqual(parameterValues(th.params), want) {
			t.Fatalf("arguments=%v, want %v", th.params, want)
		}
		th = &thread{target: target, warp: true, params: procedureParameters{"parent": {value: "007"}}}
		if _, err := r.call(context.Background(), th, target.source.Blocks["inputRawCall"], nil, false); err != nil {
			t.Fatal(err)
		}
		want = map[string]any{"a": "007", "b": "007", "c": float64(10.75), "d": float64(0)}
		if !reflect.DeepEqual(parameterValues(th.params), want) {
			t.Fatalf("mixed raw/numeric arguments=%v, want %v", th.params, want)
		}
	})
	t.Run("distinct caller parameters", func(t *testing.T) {
		for _, compiled := range []bool{false, true} {
			r := New(nativeCodegenFixture(t))
			r.native = compiled
			target := r.targets[1]
			th := &thread{target: target, warp: true, params: procedureParameters{"parent": {value: "007"}, "other": {value: "009"}}}
			if _, err := r.call(context.Background(), th, target.source.Blocks["inputPairedCall"], nil, false); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{"a": float64(10.75), "b": "009", "c": float64(0), "d": float64(0)}
			if !reflect.DeepEqual(parameterValues(th.params), want) {
				t.Fatalf("compiled=%v: arguments=%v, want %v", compiled, th.params, want)
			}
		}
	})
	t.Run("duplicate parameter names", func(t *testing.T) {
		for _, compiled := range []bool{false, true} {
			r := New(nativeCodegenFixture(t))
			r.native = compiled
			target := r.targets[1]
			th := &thread{target: target, warp: true, params: procedureParameters{"parent": {value: float64(3)}}}
			if _, err := r.call(context.Background(), th, target.source.Blocks["inputAliasCall"], nil, false); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{"a": "007", "c": float64(6.75), "d": float64(6.75)}
			if !reflect.DeepEqual(parameterValues(th.params), want) {
				t.Fatalf("compiled=%v: arguments=%v, want %v", compiled, th.params, want)
			}
		}
	})
	t.Run("wait and clone isolation", func(t *testing.T) {
		fast, slow := New(nativeCodegenFixture(t)), New(nativeCodegenFixture(t))
		slow.native = false
		for _, r := range []*Runtime{fast, slow} {
			source := r.targets[1]
			r.createClone(source)
			clone := r.targets[len(r.targets)-1]
			clone.variables["x"].Value = float64(8)
			for _, target := range []*target{source, clone} {
				th := r.startHat(target, "inputHat", false)
				th.params = procedureParameters{"parent": {value: float64(3)}}
			}
		}
		for tick := range 4 {
			for _, r := range []*Runtime{fast, slow} {
				if err := r.Run(context.Background(), 1, 60, false); err != nil {
					t.Fatal(err)
				}
			}
			if !reflect.DeepEqual(fast.Snapshot(), slow.Snapshot()) || fast.Done() != slow.Done() {
				t.Fatalf("tick %d: changed wait or clone state", tick)
			}
			if tick == 0 {
				if fast.Done() {
					t.Fatal("call skipped the wait")
				}
				for _, th := range fast.threads {
					want := 6.75
					if th.target.state.CloneID != 0 {
						want = 13
					}
					if th.params["d"].boxed() != want {
						t.Fatalf("clone %d arguments=%v", th.target.state.CloneID, th.params)
					}
				}
			}
		}
		if !fast.Done() {
			t.Fatal("wait did not finish")
		}
	})
}

func BenchmarkNativeRepeatedInputs(b *testing.B) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		b.Skip("requires generated source overlay")
	}
	for _, value := range []any{float64(2.125), "2.125"} {
		b.Run(fmt.Sprintf("%T", value), func(b *testing.B) {
			r := New(nativeCodegenFixture(b))
			target := r.targets[1]
			th := &thread{target: target, warp: true, params: procedureParameters{"parent": {value: value}}, stack: make([]continuation, 0, 1)}
			call := target.source.Blocks["inputRepeatedCall"]
			b.ReportAllocs()
			for b.Loop() {
				if _, err := r.call(context.Background(), th, call, nil, false); err != nil {
					b.Fatal(err)
				}
				if th.params["d"].num() != float64(7.515625) {
					b.Fatal(th.params)
				}
				th.stopScript()
			}
		})
	}
}

func BenchmarkNativeCallInputs(b *testing.B) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		b.Skip("requires generated source overlay")
	}
	for _, compiled := range []bool{false, true} {
		b.Run(fmt.Sprintf("compiled=%v", compiled), func(b *testing.B) {
			r := New(nativeCodegenFixture(b))
			r.native = compiled
			target := r.targets[1]
			th := &thread{target: target, warp: true, params: procedureParameters{"parent": {value: float64(3)}}, stack: make([]continuation, 0, 1)}
			call := target.source.Blocks["inputCall"]
			b.ReportAllocs()
			for b.Loop() {
				if _, err := r.call(context.Background(), th, call, nil, false); err != nil {
					b.Fatal(err)
				}
				if th.params["d"].num() != float64(6.75) {
					b.Fatal(th.params)
				}
				th.stopScript()
			}
		})
	}
}
