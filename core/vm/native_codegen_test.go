package vm

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"gowarp/core/project"
)

var nativeLiteralCases = []struct {
	text string
	want float64
}{
	{"007", 7}, {" \ufeff3 ", 3}, {"", 0}, {"false", 0},
	{"NaN", 0}, {"0x10", 16}, {"0b11", 3}, {"-0", math.Copysign(0, -1)},
	{"Infinity", math.Inf(1)}, {"-Infinity", math.Inf(-1)},
	{"1e309", math.Inf(1)}, {"-1e309", math.Inf(-1)},
	{"1_2", 0}, {"1e", 0}, {"Inf", 0}, {"0x1p2", 0}, {"1e-324", 0},
}

type nativeCancelContext struct {
	context.Context
	cancel context.CancelFunc
	steps  *int
}

func (c nativeCancelContext) Err() error {
	if *c.steps >= 1001 {
		c.cancel()
	}
	return c.Context.Err()
}

/*
Compile generated source into a separate test binary so these tests exercise
the real generated functions without installing fixture code in normal builds.
*/
func TestNativeGeneratedParity(t *testing.T) {
	p := nativeCodegenFixture(t)
	before, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	source, err := GenerateNative(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(source) == 0 {
		t.Fatal("eligible warp procedures generated no source")
	}
	again, err := GenerateNative(p)
	if err != nil || !bytes.Equal(source, again) {
		t.Fatalf("generation is not deterministic: %v", err)
	}
	after, err := json.Marshal(p)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("generation changed project blocks: %v", err)
	}
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("project root not found")
		}
		root = parent
	}
	dir := t.TempDir()
	generated := filepath.Join(dir, "native_generated.txt")
	if err := os.WriteFile(generated, source, 0600); err != nil {
		t.Fatal(err)
	}
	overlay, err := json.Marshal(map[string]any{"Replace": map[string]string{
		filepath.Join(root, "core", "vm", "native_project_generated.go"): generated,
	}})
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-overlay="+overlayPath,
		filepath.Join(root, "core", "vm"), "-run", "^TestNativeGeneratedParityChild$", "-count=1")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWARP_NATIVE_PARITY_CHILD=1")
	if testing.Verbose() {
		cmd.Args = append(cmd.Args, "-v")
	}
	if bench := flag.Lookup("test.bench").Value.String(); bench != "" {
		cmd.Args = append(cmd.Args, "-bench", bench, "-benchtime", flag.Lookup("test.benchtime").Value.String())
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated runtime parity: %v\n%s", err, output)
	} else if testing.Verbose() {
		t.Log(string(output))
	}
}

func TestNativeGenerationCompilesOrdinaryProcedures(t *testing.T) {
	p := nativeCodegenFixture(t)
	for _, b := range p.Targets[1].Blocks {
		if b.Opcode == "procedures_prototype" {
			b.Mutation["warp"] = json.RawMessage(`false`)
		}
	}
	source, err := GenerateNative(p)
	if err != nil || len(source) == 0 {
		t.Fatalf("ordinary procedures did not generate source: %d bytes, error %v", len(source), err)
	}
}

func BenchmarkNativeCalculationLoop(b *testing.B) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		b.Skip("run with TestNativeGeneratedParity to compile the fixture")
	}
	for _, compiled := range []bool{false, true} {
		b.Run(fmt.Sprintf("compiled=%v", compiled), func(b *testing.B) {
			r := New(nativeCodegenFixture(b))
			r.native = compiled
			target := r.targets[1]
			if err := prepareProcedures(target); err != nil {
				b.Fatal(err)
			}
			code := r.executionCode(target.source)
			th := &thread{target: target, warp: true, regions: target.procedures["dynamicBatch"].regions, stack: make([]continuation, 1)}
			b.ReportAllocs()
			for b.Loop() {
				*target.variables["score"] = Variable{Value: float64(0.25)}
				*target.variables["x"] = Variable{Value: float64(1000)}
				th.pc, th.done = "batchIncrement", false
				th.stack = th.stack[:1]
				th.stack[0] = continuation{body: th.pc, bodyLink: code.block(th.pc), remaining: 1000, loop: true}
				if err := r.step(context.Background(), th); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkNativeProcedureLoop(b *testing.B) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		b.Skip("run with TestNativeGeneratedParity to compile the fixture")
	}
	for _, batch := range []bool{false, true} {
		b.Run(fmt.Sprintf("batch=%v", batch), func(b *testing.B) {
			r := New(nativeCodegenFixture(b))
			target := r.targets[1]
			if err := prepareProcedures(target); err != nil {
				b.Fatal(err)
			}
			if target.procedures["pureStep"].native == nil {
				b.Fatal("helper must be compiled in both modes")
			}
			code := r.executionCode(target.source)
			th := &thread{target: target, warp: true, stack: make([]continuation, 1)}
			if batch {
				th.regions = target.procedures["callBatch"].regions
				if th.regions["callBatchStep"] == nil {
					b.Fatal("procedure loop was not compiled")
				}
			}
			b.ReportAllocs()
			for b.Loop() {
				*target.variables["score"] = Variable{Value: float64(0.25)}
				*target.variables["x"] = Variable{Value: float64(1000)}
				th.pc, th.done = "callBatchStep", false
				th.stack = th.stack[:1]
				th.stack[0] = continuation{body: th.pc, bodyLink: code.block(th.pc), remaining: 1000, loop: true}
				if err := r.step(context.Background(), th); err != nil {
					b.Fatal(err)
				}
			}
			if target.variables["score"].Value != float64(125.25) || target.variables["x"].Value != float64(0) {
				b.Fatal("procedure loop changed its result")
			}
		})
	}
}

func TestNativeGeneratedParityChild(t *testing.T) {
	if os.Getenv("GOWARP_NATIVE_PARITY_CHILD") != "1" {
		t.Skip("requires generated source overlay")
	}
	t.Run("event scripts", testNativeScripts)
	t.Run("script tick timing", testNativeScriptTicks)
	t.Run("script extension fallback", testNativeScriptFallback)
	t.Run("script budgets", testNativeScriptBudgets)
	t.Run("script clones", testNativeScriptClones)
	t.Run("native sensing", testNativeSensing)
	t.Run("connected control flow", testNativeFlow)
	t.Run("conditional repeat", testNativeConditionalRepeat)
	t.Run("connected calls", testNativeConnectedCalls)
	t.Run("shared variable calls", testNativeSharedVariableLoop)
	t.Run("shared numeric registers", testNativeSharedRegisters)
	t.Run("nested inline arguments", testNativeNestedInlineArguments)
	t.Run("compiled call inputs", testNativeCallInputs)
	t.Run("connected flow resume", testNativeFlowResume)
	t.Run("calculation flow resume", testNativeFlowCalculationResume)
	t.Run("connected flow cancellation", testNativeFlowCancellation)
	t.Run("connected flow stack limit", testNativeFlowStackLimit)
	t.Run("connected flow branch cancellation", testNativeFlowBranchCancellation)
	t.Run("connected flow stop", testNativeFlowStop)
	t.Run("native call caches", testNativeCallCaches)
	t.Run("numeric dataflow", testNativeDataflow)
	t.Run("variable copies", testNativeVariableCopies)
	t.Run("list searches", testNativeListSearch)
	t.Run("shared list calls", testNativeSharedLists)
	t.Run("shared list observers", testNativeSharedListObservers)
	t.Run("shared list clones", testNativeSharedListClones)
	t.Run("shared list cancellation", testNativeSharedListCancellation)
	t.Run("numeric comparison allocations", testNativeComparisonAllocations)
	t.Run("comparison values", testNativeComparisons)
	t.Run("typed arguments", testNativeNumericArguments)
	t.Run("native entry arguments", testNativeEntryArguments)
	t.Run("native numeric entry values", testNativeEntryNumericValues)
	t.Run("native numeric entry allocations", testNativeEntryNumericAllocations)
	t.Run("native entry duplicate names", testNativeEntryDuplicates)
	t.Run("graphics cache boundary", testNativeObservedCache)
	t.Run("graphics calculation caches", testNativeGraphicsCaches)
	t.Run("pen menus", testNativePenMenus)
	t.Run("pen color parameters", testNativePenParameters)
	t.Run("pen size values", testNativePenSize)
	t.Run("pen size allocations", testNativePenSizeAllocations)
	t.Run("pen parameter loop", testNativePenParameterLoop)
	t.Run("pen parameter error", testNativePenParameterError)
	t.Run("drawing", testNativeDrawing)
	t.Run("axis motion", testNativeAxisMotion)
	t.Run("drawing dispatch", testNativeDrawingDispatch)
	t.Run("drawing inputs", testNativeDrawingInputs)
	t.Run("drawing debug calls", testNativeDrawingDebug)
	t.Run("stamp allocations", testNativeStampAllocations)
	t.Run("constant conversion", func(t *testing.T) {
		for _, useNative := range []bool{true, false} {
			r := New(nativeCodegenFixture(t))
			r.native = useNative
			th := r.startHat(r.targets[1], "constantsHat", false)
			if err := r.Run(context.Background(), 1, 30, false); err != nil {
				t.Fatal(err)
			}
			if !th.done || (r.targets[1].procedures["constants"].native != nil) != useNative {
				t.Fatal("constant procedure did not use the requested execution path")
			}
			items := r.targets[1].state.Lists["items"].Items
			if len(items) != 1+2*len(nativeLiteralCases) {
				t.Fatalf("constant outputs: %v", items)
			}
			for i, tc := range nativeLiteralCases {
				if items[1+2*i] != tc.text {
					t.Fatalf("literal %q changed to %v", tc.text, items[1+2*i])
				}
				got, ok := items[2+2*i].(float64)
				if !ok || got != tc.want || math.Signbit(got) != math.Signbit(tc.want) {
					t.Fatalf("native=%v: numeric %q = %v, want %v", useNative, tc.text, items[2+2*i], tc.want)
				}
			}
		}
	})
	t.Run("constant conversion allocations", func(t *testing.T) {
		r := New(nativeCodegenFixture(t))
		target := r.targets[1]
		if err := prepareProcedures(target); err != nil {
			t.Fatal(err)
		}
		proc := target.procedures["constantCost"].native
		if proc == nil {
			t.Fatal("constant calculation was not compiled")
		}
		steps := 0
		th := &thread{target: target, sliceSteps: &steps}
		allocations := testing.AllocsPerRun(100, func() {
			if err := proc.run(context.Background(), r, th); err != nil {
				t.Fatal(err)
			}
		})
		if got := target.variables["score"].Value; got != float64(7) {
			t.Fatalf("constant calculation = %v, want 7", got)
		}
		if allocations != 0 {
			t.Fatalf("constant numeric conversion allocated %g objects per call", allocations)
		}
	})
	t.Run("numeric list indexes", func(t *testing.T) {
		for _, useNative := range []bool{false, true} {
			r := New(nativeCodegenFixture(t))
			r.native = useNative
			target := r.targets[1]
			items := make([]any, 2048)
			items[1500] = "coordinate"
			target.state.Lists["items"] = List{Items: items}
			*target.variables["x"] = Variable{Value: float64(1500)}
			r.startHat(target, "indexReadHat", false)
			run(t, r, 1, false)
			if target.variables["score"].Value != "coordinate" || items[1500] != "updated" {
				t.Fatalf("native=%v: numeric list indexing changed results", useNative)
			}
			if !useNative {
				continue
			}
			proc := target.procedures["indexRead"].native
			if proc == nil {
				t.Fatal("indexed calculation was not compiled")
			}
			steps := 0
			th := &thread{target: target, sliceSteps: &steps}
			allocations := testing.AllocsPerRun(100, func() {
				if err := proc.run(context.Background(), r, th); err != nil {
					t.Fatal(err)
				}
			})
			if allocations != 0 {
				t.Fatalf("numeric list indexes allocated %g objects per call", allocations)
			}
		}
	})
	t.Run("variable list indexes keep named values", func(t *testing.T) {
		for _, index := range []any{"last", "random", "any", "1", true, float64(1), "missing"} {
			fast, slow := New(nativeCodegenFixture(t)), New(nativeCodegenFixture(t))
			slow.native = false
			for _, r := range []*Runtime{fast, slow} {
				*r.targets[1].variables["x"] = Variable{Value: index}
				r.startHat(r.targets[1], "namedIndexHat", false)
				if err := r.Run(context.Background(), 1, 30, false); err != nil {
					t.Fatal(err)
				}
			}
			if !reflect.DeepEqual(fast.Snapshot(), slow.Snapshot()) {
				t.Fatalf("index=%v: compiled list lookup changed its meaning", index)
			}
		}
	})
	t.Run("conditional calculation regions", func(t *testing.T) {
		for _, x := range []float64{3, -3} {
			fast, slow := New(nativeCodegenFixture(t)), New(nativeCodegenFixture(t))
			slow.native = false
			for _, r := range []*Runtime{fast, slow} {
				*r.targets[1].variables["x"] = Variable{Value: x}
				r.startHat(r.targets[1], "regionBranchHat", false)
			}
			for frame := 0; frame < 3; frame++ {
				for _, r := range []*Runtime{fast, slow} {
					if err := r.Run(context.Background(), 1, 60, false); err != nil {
						t.Fatal(err)
					}
				}
				if !reflect.DeepEqual(fast.Snapshot(), slow.Snapshot()) || fast.Done() != slow.Done() {
					t.Fatalf("x=%g frame=%d: compiled branch changed state or wait timing", x, frame)
				}
			}
			if fast.targets[1].procedures["regionBranch"].regions["regionChoice"] == nil {
				t.Fatal("conditional calculation region was not compiled")
			}
		}
	})
	t.Run("dynamic repeat batching", func(t *testing.T) {
		for _, fixture := range []struct{ procedure, body string }{
			{"dynamicBatch", "batchIncrement"}, {"callBatch", "callBatchStep"},
		} {
			for _, count := range []float64{-2, 0, 3.5, 1000} {
				fast, slow := New(nativeCodegenFixture(t)), New(nativeCodegenFixture(t))
				slow.native = false
				for _, r := range []*Runtime{fast, slow} {
					*r.targets[1].variables["x"] = Variable{Value: count}
					r.startHat(r.targets[1], fixture.procedure+"Hat", false)
				}
				if err := prepareProcedures(fast.targets[1]); err != nil {
					t.Fatal(err)
				}
				region := fast.targets[1].procedures[fixture.procedure].regions[fixture.body]
				if region == nil {
					t.Fatal("dynamic repeat body was not compiled")
				}
				calls := 0
				runRegion := region.run
				region.run = func(ctx context.Context, r *Runtime, th *thread) error {
					calls++
					return runRegion(ctx, r, th)
				}
				for tick := 0; tick < 8; tick++ {
					for _, r := range []*Runtime{fast, slow} {
						if err := r.Run(context.Background(), 1, 30, false); err != nil {
							t.Fatal(err)
						}
					}
					if !reflect.DeepEqual(fast.Snapshot(), slow.Snapshot()) || fast.Done() != slow.Done() {
						t.Fatalf("count=%g tick=%d: loop changed values or wait timing", count, tick)
					}
				}
				region.run = runRegion
				wantCalls := 0
				if count > 0 {
					wantCalls = 1
				}
				if calls != wantCalls {
					t.Fatalf("count=%g: loop dispatched %d native regions, want %d", count, calls, wantCalls)
				}
			}
		}
	})
	t.Run("numeric loop allocations", func(t *testing.T) {
		r := New(nativeCodegenFixture(t))
		target := r.targets[1]
		if err := prepareProcedures(target); err != nil {
			t.Fatal(err)
		}
		code := r.executionCode(target.source)
		th := &thread{target: target, warp: true, regions: target.procedures["dynamicBatch"].regions, stack: make([]continuation, 1)}
		runLoop := func() {
			*target.variables["score"] = Variable{Value: float64(0.25)}
			*target.variables["x"] = Variable{Value: float64(1000)}
			th.pc, th.done = "batchIncrement", false
			th.stack = th.stack[:1]
			th.stack[0] = continuation{body: th.pc, bodyLink: code.block(th.pc), remaining: 1000, loop: true}
			if err := r.step(context.Background(), th); err != nil {
				t.Fatal(err)
			}
		}
		allocations := testing.AllocsPerRun(10, runLoop)
		if target.variables["score"].Value != float64(125.25) || target.variables["x"].Value != float64(0) {
			t.Fatal("numeric loop changed its result")
		}
		t.Logf("1000 calculation iterations: %g allocations", allocations)
		if allocations > 16 {
			t.Fatalf("numeric loop allocated intermediate values: %g", allocations)
		}
	})
	t.Run("batched loop slice resume", func(t *testing.T) {
		for _, fixture := range []struct{ procedure, body string }{
			{"dynamic", "dynamicAdd"},
			{"dynamicBatch", "batchIncrement"},
			{"callBatch", "callBatchStep"}, {"drawingLoop", "drawingSet"}, {"stampLoop", "stampCall"},
			{"directDrawing", "directMove"},
			{"axisMotion", "axisSetX"},
		} {
			t.Run(fixture.procedure, func(t *testing.T) {
				fast, slow := New(nativeCodegenFixture(t)), New(nativeCodegenFixture(t))
				slow.native = false
				var threads []*thread
				for _, r := range []*Runtime{fast, slow} {
					if fixture.procedure == "drawingLoop" || fixture.procedure == "stampLoop" {
						r.options.Fencing = false
						r.graphics = &surfaceRecorder{}
					}
					target := r.targets[1]
					if err := prepareProcedures(target); err != nil {
						t.Fatal(err)
					}
					code := r.executionCode(target.source)
					steps := 0
					th := &thread{target: target, pc: fixture.body, warp: true,
						regions:    target.procedures[fixture.procedure].regions,
						sliceSteps: &steps, sliceLimit: stepsPerFrame, sliceStarted: time.Now().Add(-time.Second),
						stack: []continuation{{body: fixture.body, bodyLink: code.block(fixture.body), remaining: 6000, loop: true}},
					}
					threads = append(threads, th)
					if err := r.stepBody(context.Background(), th); err != nil {
						t.Fatal(err)
					}
				}
				if !reflect.DeepEqual(fast.Snapshot(), slow.Snapshot()) ||
					threads[0].pc != threads[1].pc || threads[0].stack[0].remaining != threads[1].stack[0].remaining ||
					*threads[0].sliceSteps != *threads[1].sliceSteps {
					t.Fatalf("compiled loop changed its suspended continuation: pc=%q/%q remaining=%v/%v steps=%d/%d statesEqual=%v", threads[0].pc, threads[1].pc, threads[0].stack[0].remaining, threads[1].stack[0].remaining, *threads[0].sliceSteps, *threads[1].sliceSteps, reflect.DeepEqual(fast.Snapshot(), slow.Snapshot()))
				}
				for i, r := range []*Runtime{fast, slow} {
					if err := r.step(context.Background(), threads[i]); err != nil {
						t.Fatal(err)
					}
				}
				if !reflect.DeepEqual(fast.Snapshot(), slow.Snapshot()) || !threads[0].done || !threads[1].done {
					t.Fatal("compiled loop replayed or skipped work after resuming")
				}
				if (fixture.procedure == "drawingLoop" || fixture.procedure == "stampLoop") && !reflect.DeepEqual(fast.graphics.(*surfaceRecorder).commands, slow.graphics.(*surfaceRecorder).commands) {
					t.Fatal("compiled loop replayed or skipped pen commands after resuming")
				}
			})
		}
	})
	t.Run("batched loop cancellation flushes completed iterations", func(t *testing.T) {
		fast, slow := New(nativeCodegenFixture(t)), New(nativeCodegenFixture(t))
		slow.native = false
		var threads []*thread
		for _, r := range []*Runtime{fast, slow} {
			target := r.targets[1]
			if err := prepareProcedures(target); err != nil {
				t.Fatal(err)
			}
			code := r.executionCode(target.source)
			steps := 0
			th := &thread{target: target, pc: "dynamicAdd", warp: true,
				regions:    target.procedures["dynamic"].regions,
				sliceSteps: &steps, sliceLimit: stepsPerFrame, sliceStarted: time.Now(),
				stack: []continuation{{body: "dynamicAdd", bodyLink: code.block("dynamicAdd"), remaining: 6000, loop: true}},
			}
			threads = append(threads, th)
			ctx, cancel := context.WithCancel(context.Background())
			err := r.stepBody(nativeCancelContext{Context: ctx, cancel: cancel, steps: &steps}, th)
			cancel()
			if err != context.Canceled {
				t.Fatalf("expected cancellation inside loop, got %v", err)
			}
		}
		if !reflect.DeepEqual(fast.Snapshot(), slow.Snapshot()) ||
			threads[0].pc != threads[1].pc || threads[0].stack[0].remaining != threads[1].stack[0].remaining ||
			*threads[0].sliceSteps != *threads[1].sliceSteps {
			t.Fatal("canceled compiled loop lost completed work or advanced its continuation")
		}
	})
	for _, tc := range []struct {
		code       string
		want       float64
		wantItems  []any
		wantError  bool
		wantNative bool
	}{
		{"calculate", 17, []any{"007", float64(17)}, false, true},
		{"late", 7, []any{"seed", "007", float64(2), "007", float64(4), "007", float64(6)}, false, true},
		{"lazy", 5, []any{"seed"}, false, true},
		{"error", 9, []any{"seed", "kept"}, true, true},
		{"missingList", 0, []any{"seed"}, true, true},
		{"lists", 1, []any{float64(9)}, false, true},
		{"math", 4, []any{"seed", "Infinity"}, false, true},
		{"strings", 5, []any{"seed", "A😀B3", "B", "�", ""}, false, true},
		{"wait", 3, []any{"seed"}, false, false},
		{"dynamic", 2, []any{"seed"}, false, false},
		{"dynamicNested", 12, []any{"seed"}, false, false},
		{"until", 4, []any{"seed"}, false, false},
		{"recursive", 0, []any{"seed"}, false, false},
		{"bounded", 64, []any{"seed"}, false, true},
		{"shadowRepeat", 3, []any{"seed"}, false, true},
		{"oversize", 65, []any{"seed"}, false, false},
		{"unsupported", 0, []any{"seed"}, false, false},
		{"regionLoop", 6, []any{"seed", float64(2), float64(5)}, false, false},
		{"regionError", 9, []any{"seed", "kept"}, true, false},
		{"regionCount", 361, []any{float64(361)}, false, false},
		{"singleRegion", 361, []any{"seed"}, false, false},
		{"regionNested", 4, []any{float64(4)}, false, false},
	} {
		t.Run(tc.code, func(t *testing.T) {
			native, interpreted := New(nativeCodegenFixture(t)), New(nativeCodegenFixture(t))
			if !native.native || !interpreted.native {
				t.Fatal("generated project key did not activate native execution")
			}
			interpreted.native = false
			for _, r := range []*Runtime{native, interpreted} {
				if err := prepareProcedures(r.targets[1]); err != nil {
					t.Fatal(err)
				}
			}
			if got := native.targets[1].procedures[tc.code].native != nil; got != tc.wantNative {
				t.Fatalf("native selection = %v, want %v", got, tc.wantNative)
			}
			regionCalls := 0
			if tc.code == "regionLoop" || tc.code == "regionError" || tc.code == "regionCount" || tc.code == "regionNested" || tc.code == "singleRegion" {
				start := "regionAdd"
				if tc.code == "regionError" {
					start = "regionErrorSet"
				}
				if tc.code == "regionCount" || tc.code == "regionNested" || tc.code == "singleRegion" {
					start = "regionIncrement"
				}
				if tc.code == "singleRegion" {
					start = "singleIncrement"
				}
				if native.targets[1].procedures[tc.code].regions[native.targets[1].procedures[tc.code].body] != nil {
					start = native.targets[1].procedures[tc.code].body
				}
				region := native.targets[1].procedures[tc.code].regions[start]
				if region == nil {
					t.Fatal("calculation region inside an interpreted procedure was not compiled")
				}
				run := region.run
				region.run = func(ctx context.Context, r *Runtime, th *thread) error {
					regionCalls++
					return run(ctx, r, th)
				}
				defer func() { region.run = run }()
			}
			nth := native.startHat(native.targets[1], tc.code+"Hat", false)
			ith := interpreted.startHat(interpreted.targets[1], tc.code+"Hat", false)
			var nativeErr error
			for tick := 0; tick < 8; tick++ {
				nativeErr = native.Run(context.Background(), 1, 30, false)
				interpreterErr := interpreted.Run(context.Background(), 1, 30, false)
				if fmt.Sprint(nativeErr) != fmt.Sprint(interpreterErr) {
					t.Fatalf("tick %d errors: native %v, interpreter %v", tick, nativeErr, interpreterErr)
				}
				if !reflect.DeepEqual(native.Snapshot(), interpreted.Snapshot()) {
					t.Fatalf("tick %d state differs:\nnative: %+v\ninterpreted: %+v", tick, native.Snapshot(), interpreted.Snapshot())
				}
				if native.Done() != interpreted.Done() || native.Frames() != interpreted.Frames() {
					t.Fatalf("tick %d scheduler state differs", tick)
				}
				if *nth.sliceSteps != *ith.sliceSteps {
					t.Fatalf("tick %d instruction counts: native %d, interpreter %d", tick, *nth.sliceSteps, *ith.sliceSteps)
				}
				if nativeErr != nil || native.Done() {
					break
				}
			}
			if (nativeErr != nil) != tc.wantError || (!tc.wantError && !native.Done()) {
				t.Fatalf("unexpected completion: done %v, error %v", native.Done(), nativeErr)
			}
			if !tc.wantNative && (nth.pc != ith.pc || nth.warp != ith.warp || nth.procedure != ith.procedure) {
				t.Fatal("compiled region changed the continuation after completion or error")
			}
			if (tc.code == "regionLoop" || tc.code == "regionError" || tc.code == "regionCount" || tc.code == "regionNested" || tc.code == "singleRegion") && regionCalls == 0 {
				t.Fatal("runtime did not execute the compiled calculation region")
			}
			state := native.Snapshot()[1]
			if got := number(state.Variables["score"].Value); got != tc.want {
				t.Fatalf("score = %v, want %v", got, tc.want)
			}
			if !reflect.DeepEqual(state.Lists["items"].Items, tc.wantItems) {
				t.Fatalf("list = %#v, want %#v", state.Lists["items"].Items, tc.wantItems)
			}
		})
	}
	t.Run("region cancellation", func(t *testing.T) {
		r := New(nativeCodegenFixture(t))
		if err := prepareProcedures(r.targets[1]); err != nil {
			t.Fatal(err)
		}
		region := r.targets[1].procedures["regionCount"].regions["regionIncrement"]
		if region == nil {
			t.Fatal("calculation region missing")
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		steps := 0
		th := &thread{target: r.targets[1], pc: "regionIncrement", sliceSteps: &steps}
		before := r.Snapshot()
		if err := region.run(ctx, r, th); err != context.Canceled {
			t.Fatalf("cancellation error: %v", err)
		}
		if th.pc != "regionIncrement" || !reflect.DeepEqual(before, r.Snapshot()) {
			t.Fatal("canceled region advanced or changed data")
		}
	})
	t.Run("special floats", func(t *testing.T) {
		want := []float64{math.NaN(), math.Inf(1), 1, math.Copysign(0, -1)}
		for _, useNative := range []bool{true, false} {
			r := New(nativeCodegenFixture(t))
			r.native = useNative
			th := r.startHat(r.targets[1], "specialHat", false)
			if err := r.Run(context.Background(), 1, 30, false); err != nil {
				t.Fatal(err)
			}
			if !th.done || (r.targets[1].procedures["special"].native != nil) != useNative {
				t.Fatal("special-float procedure did not use the requested execution path")
			}
			// Snapshot renders nonfinite numbers as text; inspect the actual values.
			items := r.targets[1].state.Lists["items"].Items
			if len(items) != len(want) {
				t.Fatalf("native=%v: got %v", useNative, items)
			}
			for i, expected := range want {
				got, ok := items[i].(float64)
				if !ok || (math.IsNaN(expected) && !math.IsNaN(got)) ||
					(!math.IsNaN(expected) && (got != expected || math.Signbit(got) != math.Signbit(expected))) {
					t.Fatalf("native=%v item %d = %#v, want %v (negative=%v)", useNative, i, items[i], expected, math.Signbit(expected))
				}
			}
		}
	})
	t.Run("list capacity", func(t *testing.T) {
		for _, useNative := range []bool{true, false} {
			r := New(nativeCodegenFixture(t))
			r.native = useNative
			r.targets[1].state.Lists["items"] = List{Items: make([]any, listLimit)}
			r.startHat(r.targets[1], "mathHat", false)
			run(t, r, 1, false)
			items := r.targets[1].state.Lists["items"].Items
			if len(items) != listLimit || items[listLimit-1] != nil {
				t.Fatalf("native=%v appended beyond list capacity", useNative)
			}
		}
	})
	t.Run("nesting limit fallback", func(t *testing.T) {
		r := New(nativeCodegenFixture(t))
		steps := 0
		th := &thread{target: r.targets[1], stack: make([]continuation, 1023), sliceSteps: &steps}
		call := r.targets[1].source.Blocks["lazyEntry"]
		input := func(string) (any, error) { return nil, nil }
		if _, err := r.call(context.Background(), th, call, input, false); err != nil {
			t.Fatal(err)
		}
		if len(th.stack) != 1024 || th.pc != "lazyIf" || number(r.targets[1].variables["score"].Value) != 0 {
			t.Fatal("native procedure bypassed the interpreter nesting guard")
		}
		if _, err := r.call(context.Background(), th, call, input, false); err == nil {
			t.Fatal("procedure call exceeded nesting limit")
		}
	})
	t.Run("clone bindings", func(t *testing.T) {
		r := New(nativeCodegenFixture(t))
		r.createClone(r.targets[1])
		r.startHat(r.targets[1], "lazyHat", false)
		r.startHat(r.targets[2], "calculateHat", false)
		if err := r.Run(context.Background(), 1, 30, false); err != nil {
			t.Fatal(err)
		}
		states := r.Snapshot()
		if number(states[1].Variables["score"].Value) != 5 || number(states[2].Variables["score"].Value) != 17 ||
			!reflect.DeepEqual(states[1].Lists["items"].Items, []any{"seed"}) || len(states[2].Lists["items"].Items) != 2 {
			t.Fatalf("native clone bindings shared data: %+v", states)
		}
	})
	t.Run("project mismatch", func(t *testing.T) {
		p := nativeCodegenFixture(t)
		p.Targets[1].Blocks["lazySet"].Inputs["VALUE"] = []any{float64(1), []any{float64(4), "99"}}
		r := New(p)
		if r.native {
			t.Fatal("modified project activated stale generated code")
		}
		r.startHat(r.targets[1], "lazyHat", false)
		run(t, r, 1, false)
		if got := number(r.Snapshot()[1].Variables["score"].Value); got != 99 {
			t.Fatalf("fallback used stale blocks: %v", got)
		}
	})
}

func nativeCodegenFixture(t testing.TB) *project.Project {
	t.Helper()
	var blocks project.Blocks
	if err := json.Unmarshal([]byte(`{
 "stringBody":{"opcode":"data_setvariableto","next":"stringAppend","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"stringLength"]}},
 "stringJoin":{"opcode":"operator_join","inputs":{"STRING1":[1,[10,"A😀B"]],"STRING2":[1,[12,"x","x"]]}},
 "stringLength":{"opcode":"operator_length","inputs":{"STRING":[2,"stringJoin"]}},
 "stringAppend":{"opcode":"data_addtolist","next":"stringLetterAppend","fields":{"LIST":["items","items"]},"inputs":{"ITEM":[2,"stringJoin"]}},
 "stringLetterAppend":{"opcode":"data_addtolist","next":"stringSurrogateAppend","fields":{"LIST":["items","items"]},"inputs":{"ITEM":[2,"stringLetter"]}},
 "stringLetter":{"opcode":"operator_letter_of","inputs":{"LETTER":[2,"stringIndex"],"STRING":[2,"stringJoin"]}},
 "stringIndex":{"opcode":"operator_add","inputs":{"NUM1":[1,[12,"x","x"]],"NUM2":[1,[4,"1"]]}},
 "stringSurrogateAppend":{"opcode":"data_addtolist","next":"stringMissingAppend","fields":{"LIST":["items","items"]},"inputs":{"ITEM":[2,"stringSurrogate"]}},
 "stringSurrogate":{"opcode":"operator_letter_of","inputs":{"LETTER":[1,[4,"2"]],"STRING":[2,"stringJoin"]}},
 "stringMissingAppend":{"opcode":"data_addtolist","fields":{"LIST":["items","items"]},"inputs":{"ITEM":[2,"stringMissing"]}},
 "stringMissing":{"opcode":"operator_letter_of","inputs":{"LETTER":[1,[4,"99"]],"STRING":[2,"stringJoin"]}},
 "calculateClear":{"opcode":"data_deletealloflist","next":"calculateSet","fields":{"LIST":["items","items"]}},
 "calculateSet":{"opcode":"data_setvariableto","next":"calculateRepeat","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"arg"]}},
 "arg":{"opcode":"argument_reporter_string_number","fields":{"VALUE":["n",null]}},
 "calculateRepeat":{"opcode":"control_repeat","next":"innerCall","inputs":{"TIMES":[1,[4,"3"]],"SUBSTACK":[2,"calculateAdd"]}},
 "calculateAdd":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"arg"]}},
 "innerCall":{"opcode":"procedures_call","next":"calculateAfter","mutation":{"proccode":"inner","argumentids":"[]"}},
 "innerAdd":{"opcode":"data_changevariableby","next":"innerList","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[12,"score","score"]]}},
 "innerList":{"opcode":"data_addtolist","fields":{"LIST":["items","items"]},"inputs":{"ITEM":[1,[10,"007"]]}},
 "calculateAfter":{"opcode":"data_changevariableby","next":"calculateAppend","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"round"]}},
 "round":{"opcode":"operator_round","inputs":{"NUM":[1,[4,"-0.5"]]}},
 "calculateAppend":{"opcode":"data_addtolist","next":"calculateReplace","fields":{"LIST":["items","items"]},"inputs":{"ITEM":[1,[12,"score","score"]]}},
 "calculateReplace":{"opcode":"data_replaceitemoflist","next":"calculateFinal","fields":{"LIST":["items","items"]},"inputs":{"INDEX":[1,[10,"last"]],"ITEM":[2,"sum"]}},
 "sum":{"opcode":"operator_add","inputs":{"NUM1":[2,"last"],"NUM2":[1,[4,"1"]]}},
 "last":{"opcode":"data_itemoflist","fields":{"LIST":["items","items"]},"inputs":{"INDEX":[1,[10,"last"]]}},
 "calculateFinal":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"last"]}},
 "lateBody":{"opcode":"control_repeat","inputs":{"TIMES":[1,[4,"3"]],"SUBSTACK":[2,"lateCall"]}},
 "lateCall":{"opcode":"procedures_call","next":"lateAdd","mutation":{"proccode":"inner","argumentids":"[]"}},
 "lateAdd":{"opcode":"data_changevariableby","next":"lateAppend","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}},
 "lateAppend":{"opcode":"data_addtolist","fields":{"LIST":["items","items"]},"inputs":{"ITEM":[2,"length"]}},
 "lazyIf":{"opcode":"control_if_else","inputs":{"CONDITION":[2,"shortAnd"],"SUBSTACK":[2,"missingSet"],"SUBSTACK2":[2,"lazySet"]}},
 "shortAnd":{"opcode":"operator_and","inputs":{"OPERAND1":[2,"false"],"OPERAND2":[1,[12,"missing","missing"]]}},
 "false":{"opcode":"operator_equals","inputs":{"OPERAND1":[1,[4,"0"]],"OPERAND2":[1,[4,"1"]]}},
 "missingSet":{"opcode":"data_setvariableto","fields":{"VARIABLE":["missing","missing"]},"inputs":{"VALUE":[1,[4,"7"]]}},
 "lazySet":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"5"]]}},
 "errorSet":{"opcode":"data_setvariableto","next":"errorList","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"9"]]}},
 "errorList":{"opcode":"data_addtolist","next":"missingSet","fields":{"LIST":["items","items"]},"inputs":{"ITEM":[1,[10,"kept"]]}},
 "missingListBody":{"opcode":"data_addtolist","fields":{"LIST":["missingList","missingList"]},"inputs":{"ITEM":[1,[12,"missingVariable","missingVariable"]]}},
 "listsClear":{"opcode":"data_deletealloflist","next":"listsAdd","fields":{"LIST":["items","items"]}},
 "listsAdd":{"opcode":"data_addtolist","next":"listsAdd2","fields":{"LIST":["items","items"]},"inputs":{"ITEM":[1,[4,"1"]]}},
 "listsAdd2":{"opcode":"data_addtolist","next":"listsDelete","fields":{"LIST":["items","items"]},"inputs":{"ITEM":[1,[4,"2"]]}},
 "listsDelete":{"opcode":"data_deleteoflist","next":"listsReplace","fields":{"LIST":["items","items"]},"inputs":{"INDEX":[1,[10,"last"]]}},
 "listsReplace":{"opcode":"data_replaceitemoflist","next":"listsLength","fields":{"LIST":["items","items"]},"inputs":{"INDEX":[1,[4,"1"]],"ITEM":[2,"square"]}},
 "square":{"opcode":"operator_multiply","inputs":{"NUM1":[1,[4,"3"]],"NUM2":[1,[4,"3"]]}},
 "listsLength":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"length"]}},
 "length":{"opcode":"data_lengthoflist","fields":{"LIST":["items","items"]}},
 "mathSet":{"opcode":"data_setvariableto","next":"mathAppend","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"mathSum"]}},
 "mathSum":{"opcode":"operator_add","inputs":{"NUM1":[2,"sqrt"],"NUM2":[2,"mod"]}},
 "sqrt":{"opcode":"operator_mathop","fields":{"OPERATOR":["sqrt",null]},"inputs":{"NUM":[1,[4,"9"]]}},
 "mod":{"opcode":"operator_mod","inputs":{"NUM1":[1,[4,"-5"]],"NUM2":[1,[4,"3"]]}},
 "mathAppend":{"opcode":"data_addtolist","fields":{"LIST":["items","items"]},"inputs":{"ITEM":[2,"division"]}},
 "division":{"opcode":"operator_divide","inputs":{"NUM1":[1,[4,"1"]],"NUM2":[1,[4,"0"]]}},
 "specialClear":{"opcode":"data_deletealloflist","next":"specialNaN","fields":{"LIST":["items","items"]}},
 "specialNaN":{"opcode":"data_addtolist","next":"specialInf","fields":{"LIST":["items","items"]},"inputs":{"ITEM":[2,"nan"]}},
 "nan":{"opcode":"operator_divide","inputs":{"NUM1":[1,[4,"0"]],"NUM2":[1,[4,"0"]]}},
 "specialInf":{"opcode":"data_addtolist","next":"specialCoerced","fields":{"LIST":["items","items"]},"inputs":{"ITEM":[2,"division"]}},
 "specialCoerced":{"opcode":"data_addtolist","next":"specialZero","fields":{"LIST":["items","items"]},"inputs":{"ITEM":[2,"coerced"]}},
 "coerced":{"opcode":"operator_add","inputs":{"NUM1":[2,"nan"],"NUM2":[1,[4,"1"]]}},
 "specialZero":{"opcode":"data_addtolist","fields":{"LIST":["items","items"]},"inputs":{"ITEM":[2,"round"]}},
 "waitBody":{"opcode":"control_wait","next":"waitSet","inputs":{"DURATION":[1,[4,"0"]]}},
 "waitSet":{"opcode":"data_setvariableto","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"3"]]}},
 "dynamicBody":{"opcode":"control_repeat","inputs":{"TIMES":[2,"arg"],"SUBSTACK":[2,"dynamicAdd"]}},
 "dynamicAdd":{"opcode":"data_changevariableby","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[1,[4,"1"]]}},
 "recursiveBody":{"opcode":"control_if","inputs":{"CONDITION":[2,"false"],"SUBSTACK":[2,"recursiveCall"]}},
 "recursiveCall":{"opcode":"procedures_call","mutation":{"proccode":"recursive","argumentids":"[]"}},
 "boundedBody":{"opcode":"control_repeat","inputs":{"TIMES":[1,[4,"64"]],"SUBSTACK":[2,"dynamicAdd"]}},
 "shadowRepeatBody":{"opcode":"control_repeat","inputs":{"TIMES":[3,null,[4,"3"]],"SUBSTACK":[2,"dynamicAdd"]}},
 "oversizeBody":{"opcode":"control_repeat","inputs":{"TIMES":[1,[4,"65"]],"SUBSTACK":[2,"dynamicAdd"]}},
 "unsupportedBody":{"opcode":"looks_show"},
 "regionLoopBody":{"opcode":"control_repeat","inputs":{"TIMES":[2,"arg"],"SUBSTACK":[2,"regionAdd"]}},
 "regionAdd":{"opcode":"data_changevariableby","next":"regionAppend","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"regionArgument"]}},
 "regionAppend":{"opcode":"data_addtolist","next":"regionWait","fields":{"LIST":["items","items"]},"inputs":{"ITEM":[1,[12,"score","score"]]}},
 "regionWait":{"opcode":"control_wait","next":"dynamicAdd","inputs":{"DURATION":[1,[4,"0"]]}},
 "regionErrorBody":{"opcode":"looks_show","next":"regionErrorSet"},
 "regionErrorSet":{"opcode":"data_setvariableto","next":"errorList","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"regionNine"]}},
 "regionCountBody":{"opcode":"control_repeat","inputs":{"TIMES":[1,[4,"361"]],"SUBSTACK":[2,"regionIncrement"]}},
 "regionIncrement":{"opcode":"data_changevariableby","next":"regionStore","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"regionDelta"]}},
 "regionStore":{"opcode":"data_replaceitemoflist","fields":{"LIST":["items","items"]},"inputs":{"INDEX":[1,[4,"1"]],"ITEM":[1,[12,"score","score"]]}},
 "regionDelta":{"opcode":"operator_subtract","inputs":{"NUM1":[2,"regionLength"],"NUM2":[1,[4,"4"]]}},
 "regionLength":{"opcode":"operator_mathop","fields":{"OPERATOR":["sqrt",null]},"inputs":{"NUM":[2,"regionSquares"]}},
 "regionSquares":{"opcode":"operator_add","inputs":{"NUM1":[2,"regionXX"],"NUM2":[2,"regionYY"]}},
 "regionXX":{"opcode":"operator_multiply","inputs":{"NUM1":[1,[12,"x","x"]],"NUM2":[1,[12,"x","x"]]}},
 "regionYY":{"opcode":"operator_multiply","inputs":{"NUM1":[1,[12,"y","y"]],"NUM2":[1,[12,"y","y"]]}},
 "regionNine":{"opcode":"operator_add","inputs":{"NUM1":[2,"regionLength"],"NUM2":[1,[4,"4"]]}},
 "regionArgument":{"opcode":"operator_add","inputs":{"NUM1":[2,"arg"],"NUM2":[2,"regionZero"]}},
 "regionZero":{"opcode":"operator_subtract","inputs":{"NUM1":[2,"regionDelta"],"NUM2":[1,[4,"1"]]}},
 "regionNestedBody":{"opcode":"data_changevariableby","next":"regionNestedStore","fields":{"VARIABLE":["score","score"]},"inputs":{"VALUE":[2,"regionDelta"]}},
 "regionNestedStore":{"opcode":"data_replaceitemoflist","next":"regionNestedCall","fields":{"LIST":["items","items"]},"inputs":{"INDEX":[1,[4,"1"]],"ITEM":[1,[12,"score","score"]]}},
 "regionNestedCall":{"opcode":"procedures_call","next":"regionIncrement","mutation":{"proccode":"wait","argumentids":"[]"}}
}`), &blocks); err != nil {
		t.Fatal(err)
	}
	addNativeDrawingBlocks(t, blocks)
	addNativeCacheBlocks(t, blocks)
	addNativeSharedListBlocks(blocks)
	addNativeDataflowBlocks(t, blocks)
	addNativeListBlocks(t, blocks)
	addNativeSensingBlocks(t, blocks)
	addNativeFlowBlocks(t, blocks)
	addNativeScriptBlocks(t, blocks)
	addNativeInputBlocks(t, blocks)
	addNativeComparisonBlocks(blocks)
	blocks["singleBody"] = &project.Block{Opcode: "control_repeat", Inputs: map[string][]any{"TIMES": {float64(1), []any{float64(4), "361"}}, "SUBSTACK": {float64(2), "singleIncrement"}}}
	blocks["batchBody"] = &project.Block{Opcode: "control_repeat", Next: "batchWait", Inputs: map[string][]any{"TIMES": {float64(1), []any{float64(12), "x", "x"}}, "SUBSTACK": {float64(2), "batchIncrement"}}}
	blocks["batchIncrement"] = &project.Block{Opcode: "data_changevariableby", Next: "batchDecrement", Fields: map[string][]any{"VARIABLE": {"score", "score"}}, Inputs: literalInputs(map[string]any{"VALUE": "0.125"})}
	blocks["batchDecrement"] = &project.Block{Opcode: "data_changevariableby", Fields: map[string][]any{"VARIABLE": {"x", "x"}}, Inputs: literalInputs(map[string]any{"VALUE": "-1"})}
	blocks["batchWait"] = &project.Block{Opcode: "control_wait", Next: "batchDone", Inputs: literalInputs(map[string]any{"DURATION": "0.05"})}
	blocks["batchDone"] = &project.Block{Opcode: "data_addtolist", Fields: map[string][]any{"LIST": {"items", "items"}}, Inputs: literalInputs(map[string]any{"ITEM": "done"})}
	blocks["callBatchBody"] = &project.Block{Opcode: "control_repeat", Next: "batchWait", Inputs: map[string][]any{"TIMES": {float64(1), []any{float64(12), "x", "x"}}, "SUBSTACK": {float64(2), "callBatchStep"}}}
	blocks["callBatchStep"] = &project.Block{Opcode: "procedures_call", Next: "batchDecrement", Mutation: map[string]json.RawMessage{"proccode": json.RawMessage(`"pureStep"`)}, Inputs: literalInputs(map[string]any{"nID": "0.125"})}
	blocks["pureStepBody"] = &project.Block{Opcode: "data_changevariableby", Next: "regionStore", Fields: map[string][]any{"VARIABLE": {"score", "score"}}, Inputs: map[string][]any{"VALUE": {float64(2), "arg"}}}
	blocks["nestedOuter"] = &project.Block{Opcode: "control_repeat", Inputs: map[string][]any{"TIMES": {float64(1), []any{float64(12), "x", "x"}}, "SUBSTACK": {float64(2), "nestedInner"}}}
	blocks["nestedInner"] = &project.Block{Opcode: "control_repeat", Inputs: map[string][]any{"TIMES": {float64(1), []any{float64(12), "y", "y"}}, "SUBSTACK": {float64(2), "dynamicAdd"}}}
	blocks["untilBody"] = &project.Block{Opcode: "control_repeat_until", Inputs: map[string][]any{"CONDITION": {float64(2), "untilEnd"}, "SUBSTACK": {float64(2), "dynamicAdd"}}}
	blocks["untilEnd"] = &project.Block{Opcode: "operator_gt", Inputs: map[string][]any{"OPERAND1": {float64(1), []any{float64(12), "score", "score"}}, "OPERAND2": {float64(1), []any{float64(4), "3"}}}}
	blocks["singleIncrement"] = &project.Block{Opcode: "data_changevariableby", Fields: map[string][]any{"VARIABLE": {"score", "score"}}, Inputs: map[string][]any{"VALUE": {float64(2), "singleValue"}}}
	blocks["singleValue"] = &project.Block{Opcode: "operator_multiply", Inputs: map[string][]any{"NUM1": {float64(2), "regionDelta"}, "NUM2": {float64(2), "regionDelta"}}}
	blocks["constantCostBody"] = &project.Block{Opcode: "data_setvariableto", Fields: map[string][]any{"VARIABLE": {"score", "score"}}, Inputs: map[string][]any{"VALUE": {float64(2), "constantCostSum"}}}
	blocks["constantCostSum"] = &project.Block{Opcode: "operator_add", Inputs: literalInputs(map[string]any{"NUM1": "1e", "NUM2": "007"})}
	blocks["indexReadBody"] = &project.Block{Opcode: "data_setvariableto", Next: "indexWrite", Fields: map[string][]any{"VARIABLE": {"score", "score"}}, Inputs: map[string][]any{"VALUE": {float64(2), "indexItem"}}}
	blocks["namedIndexBody"] = &project.Block{Opcode: "data_setvariableto", Fields: map[string][]any{"VARIABLE": {"score", "score"}}, Inputs: map[string][]any{"VALUE": {float64(2), "namedIndexItem"}}}
	blocks["namedIndexItem"] = &project.Block{Opcode: "data_itemoflist", Fields: map[string][]any{"LIST": {"items", "items"}}, Inputs: map[string][]any{"INDEX": {float64(1), []any{float64(12), "x", "x"}}}}
	blocks["indexItem"] = &project.Block{Opcode: "data_itemoflist", Fields: map[string][]any{"LIST": {"items", "items"}}, Inputs: map[string][]any{"INDEX": {float64(2), "indexSum"}}}
	blocks["indexSum"] = &project.Block{Opcode: "operator_add", Inputs: map[string][]any{"NUM1": {float64(1), []any{float64(12), "x", "x"}}, "NUM2": {float64(1), []any{float64(4), "1"}}}}
	blocks["indexWrite"] = &project.Block{Opcode: "data_replaceitemoflist", Fields: map[string][]any{"LIST": {"items", "items"}}, Inputs: map[string][]any{"INDEX": {float64(2), "indexSum"}, "ITEM": {float64(1), []any{float64(10), "updated"}}}}
	blocks["regionBranchBody"] = &project.Block{Opcode: "control_repeat", Next: "regionBranchWait", Inputs: map[string][]any{"TIMES": {float64(1), []any{float64(4), "361"}}, "SUBSTACK": {float64(2), "regionChoice"}}}
	blocks["regionChoice"] = &project.Block{Opcode: "control_if_else", Next: "regionStore", Inputs: map[string][]any{"CONDITION": {float64(2), "regionPositive"}, "SUBSTACK": {float64(2), "regionYes"}, "SUBSTACK2": {float64(2), "regionNo"}}}
	blocks["regionPositive"] = &project.Block{Opcode: "operator_gt", Inputs: map[string][]any{"OPERAND1": {float64(1), []any{float64(12), "x", "x"}}, "OPERAND2": {float64(1), []any{float64(4), "0"}}}}
	blocks["regionYes"] = &project.Block{Opcode: "data_changevariableby", Fields: map[string][]any{"VARIABLE": {"score", "score"}}, Inputs: map[string][]any{"VALUE": {float64(2), "regionDelta"}}}
	blocks["regionNo"] = &project.Block{Opcode: "data_changevariableby", Next: "regionPureCall", Fields: map[string][]any{"VARIABLE": {"score", "score"}}, Inputs: map[string][]any{"VALUE": {float64(2), "regionNine"}}}
	blocks["regionPureCall"] = &project.Block{Opcode: "procedures_call", Mutation: map[string]json.RawMessage{"proccode": json.RawMessage(`"inner"`), "argumentids": json.RawMessage(`"[]"`)}}
	blocks["regionBranchWait"] = &project.Block{Opcode: "control_wait", Inputs: literalInputs(map[string]any{"DURATION": "0"})}
	for i, tc := range nativeLiteralCases {
		id := fmt.Sprintf("constant%d", i)
		next := ""
		if i+1 < len(nativeLiteralCases) {
			next = fmt.Sprintf("constant%d", i+1)
		}
		blocks[id] = &project.Block{Opcode: "data_addtolist", Next: id + "Number", Fields: map[string][]any{"LIST": {"items", "items"}}, Inputs: literalInputs(map[string]any{"ITEM": tc.text})}
		blocks[id+"Number"] = &project.Block{Opcode: "data_addtolist", Next: next, Fields: map[string][]any{"LIST": {"items", "items"}}, Inputs: map[string][]any{"ITEM": {float64(2), id + "Multiply"}}}
		blocks[id+"Multiply"] = &project.Block{Opcode: "operator_multiply", Inputs: literalInputs(map[string]any{"NUM1": tc.text, "NUM2": "1"})}
	}
	for _, proc := range []struct {
		code, body string
		argument   bool
	}{
		{"sensingRead", "sensingReadBody", false}, {"sensingLoop", "sensingLoopBody", false},
		{"flow", "flowBody", false}, {"flowStep", "flowStepBody", true},
		{"conditional", "conditionalBody", false},
		{"connectedCalls", "connectedCallsBody", false}, {"suspendingStep", "suspendingStepBody", true},
		{"flowWhile", "flowWhileBody", false}, {"flowEmpty", "flowEmptyBody", false}, {"flowYield", "flowYieldBody", false},
		{"flowBreak", "flowBreakBody", false}, {"flowEach", "flowEachBody", false}, {"flowCycle", "flowCycleBody", false},
		{"flowBranch", "flowBranchBody", false}, {"flowStop", "flowStopBody", false},
		{"sensingMenu", "sensingMenuBody", false}, {"sensingEmptyMenu", "sensingEmptyMenuBody", false},
		{"penMenuNumber", "penMenuNumberStore", false}, {"penMenuInput", "penMenuInputStore", false},
		{"penMenuMissing", "penMenuMissingStore", false}, {"penMenuEmpty", "penMenuEmptyStore", false},
		{"penParamError", "penParamError", false},
		{"penParams", "penParamSet", true}, {"penParamBatch", "penParamLoop", false},
		{"penSizes", "penSizeSet", false}, {"penSizeBatch", "penSizeLoop", false}, {"penSizeError", "penSizeError", false},
		{"observed", "observedChange", false}, {"graphicsCallLoop", "graphicsCallLoopBody", false},
		{"duplicateNames", "mixedArgSet", true},
		{"mixedArg", "mixedArgSet", true}, {"mixedPair", "pairSet", true}, {"pairCaller", "pairCallerBody", false},
		{"comparisonCases", "comparisonPrepare", false},
		{"copyLoop", "copyLoopBody", true},
		{"typedChain", "typedStart", true}, {"typedCondition", "typedConditionStart", true},
		{"typedBoth", "typedBothStart", true}, {"typedLoop", "typedLoopStart", true}, {"typedEmpty", "typedEmpty", true},
		{"comparisonLoop", "comparisonLoopBody", false},
		{"textComparisonLoop", "textComparisonLoopBody", false},
		{"inlineOuter", "inlineOuterBody", true}, {"inlineMiddle", "inlineMiddleBody", true},
		{"inlineLoop", "inlineLoopBody", false}, {"inlineStep", "inlineStepBody", true},
		{"cacheLoop", "cacheLoopBody", false}, {"numericPen", "numericPenIf", true},
		{"sharedLoop", "sharedLoopBody", false}, {"sharedHelper", "sharedHelper0", false},
		{"sharedOnly", "sharedOnlyBody", false},
		{"directDrawing", "directDrawingLoop", false}, {"directEffects", "directGhost", false},
		{"axisMotion", "axisLoop", false}, {"axisReporters", "axisReadDirection", false}, {"axisError", "axisError", false},
		{"moveInvalid", "moveInvalid", false}, {"leftInvalid", "leftInvalid", false},
		{"rightInvalid", "rightInvalid", false}, {"directionInvalid", "directionInvalid", false},
		{"sizeInvalid", "sizeInvalid", false}, {"costumeInvalid", "costumeInvalid", false},
		{"colorInvalid", "colorInvalid", false}, {"effectInvalid", "effectInvalid", false},
		{"drawingDebug", "drawingLog", false}, {"stamp", "stampSize", false}, {"stampLoop", "stampLoopBody", false},
		{"drawingError", "drawingError", false}, {"drawing", "drawingSet", false}, {"drawingLoop", "drawingLoopBody", false},
		{"calculate", "calculateClear", true}, {"inner", "innerAdd", false},
		{"late", "lateBody", false},
		{"lazy", "lazyIf", false}, {"error", "errorSet", false},
		{"missingList", "missingListBody", false}, {"special", "specialClear", false},
		{"listSearch", "listSearchSet", false}, {"missingSearch", "missingSearchSet", false},
		{"sharedLists", "sharedListsRepeat", false}, {"sharedListHelper", "sharedListClear", false},
		{"sharedListObserver", "sharedListObserverStart", false},
		{"lists", "listsClear", false}, {"wait", "waitBody", false},
		{"math", "mathSet", false},
		{"strings", "stringBody", false},
		{"dynamic", "dynamicBody", true}, {"recursive", "recursiveBody", false},
		{"dynamicBatch", "batchBody", false},
		{"callBatch", "callBatchBody", false}, {"pureStep", "pureStepBody", true},
		{"dynamicNested", "nestedOuter", false}, {"until", "untilBody", false},
		{"bounded", "boundedBody", false}, {"oversize", "oversizeBody", false},
		{"shadowRepeat", "shadowRepeatBody", false},
		{"unsupported", "unsupportedBody", false},
		{"regionLoop", "regionLoopBody", true}, {"regionError", "regionErrorBody", false},
		{"regionCount", "regionCountBody", false},
		{"singleRegion", "singleBody", false},
		{"regionNested", "regionNestedBody", false},
		{"constants", "constant0", false}, {"constantCost", "constantCostBody", false},
		{"indexRead", "indexReadBody", false},
		{"namedIndex", "namedIndexBody", false},
		{"regionBranch", "regionBranchBody", false},
	} {
		ids, names, defaults := `[]`, `[]`, `[]`
		if proc.argument {
			ids, names, defaults = `["nID"]`, `["n"]`, `[2]`
		}
		mutation := make(map[string]json.RawMessage)
		for key, value := range map[string]string{"proccode": proc.code, "argumentids": ids, "argumentnames": names, "argumentdefaults": defaults, "warp": "true"} {
			mutation[key], _ = json.Marshal(value)
		}
		blocks[proc.code+"Def"] = &project.Block{Opcode: "procedures_definition", TopLevel: true, Next: proc.body, Inputs: map[string][]any{"custom_block": {float64(1), proc.code + "Proto"}}}
		blocks[proc.code+"Proto"] = &project.Block{Opcode: "procedures_prototype", Mutation: mutation}
		blocks[proc.code+"Hat"] = &project.Block{Opcode: "event_whenbroadcastreceived", TopLevel: true, Next: proc.code + "Entry"}
		blocks[proc.code+"Entry"] = &project.Block{Opcode: "procedures_call", Mutation: map[string]json.RawMessage{"proccode": mutation["proccode"], "argumentids": mutation["argumentids"]}}
	}
	blocks["inlineOuterEntry"].Inputs = map[string][]any{"nID": {float64(1), []any{float64(12), "x", "x"}}}
	blocks["penParamsEntry"].Inputs = map[string][]any{"nID": {float64(1), []any{float64(12), "score", "score"}}}
	for _, name := range []string{"numericPen", "mixedArg"} {
		blocks[name+"Entry"].Inputs = map[string][]any{"nID": {float64(1), []any{float64(12), "x", "x"}}}
	}
	for key, value := range map[string]string{"argumentids": `["nID","labelID"]`, "argumentnames": `["n","label"]`, "argumentdefaults": `[2,""]`} {
		blocks["mixedPairProto"].Mutation[key], _ = json.Marshal(value)
	}
	for key, value := range map[string]string{"argumentids": `["nID","secondID"]`, "argumentnames": `["n","n"]`, "argumentdefaults": `[2,"009"]`} {
		blocks["duplicateNamesProto"].Mutation[key], _ = json.Marshal(value)
	}
	blocks["duplicateNamesEntry"].Mutation["argumentids"], _ = json.Marshal(`["nID","secondID"]`)
	blocks["duplicateNamesEntry"].Inputs = map[string][]any{"nID": {float64(1), []any{float64(12), "x", "x"}}, "secondID": {float64(1), []any{float64(12), "y", "y"}}}
	return &project.Project{Targets: []*project.Target{
		{IsStage: true, Name: "Stage", Blocks: project.Blocks{}},
		{Name: "Sprite", Blocks: blocks, Costumes: []project.Costume{{Name: "first"}, {Name: "second"}}, Variables: map[string][]any{"waitDuration": {"waitDuration", float64(0)}, "score": {"score", float64(0)}, "x": {"x", float64(3)}, "y": {"y", float64(4)}}, Lists: map[string][]any{"items": {"items", []any{"seed"}}}},
	}}
}
