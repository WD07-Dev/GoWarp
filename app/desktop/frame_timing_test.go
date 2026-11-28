package desktop

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestFrameTimingDistribution(t *testing.T) {
	var d timingDistribution
	for range 94 {
		d.add(16 * time.Millisecond)
	}
	for range 5 {
		d.add(20 * time.Millisecond)
	}
	d.add(140 * time.Millisecond)
	if d.count != 100 || d.over60 != 6 || d.over30 != 1 || d.over50 != 1 {
		t.Fatalf("incorrect counts: %+v", d)
	}
	if d.percentile(95) != 20*time.Millisecond || d.percentile(99) != 20*time.Millisecond || d.max != 140*time.Millisecond {
		t.Fatal("percentiles must not hide the maximum stall")
	}
	d = timingDistribution{}
	d.add(time.Second / 60)
	d.add(time.Second/60 + 1)
	d.add(time.Second / 30)
	d.add(time.Second/30 + 1)
	if d.over60 != 3 || d.over30 != 1 {
		t.Fatal("thresholds must use original durations, not histogram buckets")
	}
	d = timingDistribution{}
	d.add(2300 * time.Millisecond)
	if d.percentile(99) != 2300*time.Millisecond {
		t.Fatal("long stalls must not be clipped to the histogram range")
	}
}

func TestFrameTimingSeparatesWorkAndGaps(t *testing.T) {
	var trace frameTiming
	var messages []string
	emit := func(s string) { messages = append(messages, s) }
	start := time.Unix(100, 0)
	state := frameTimingState{focused: true}
	trace.beginUpdate(start, state, emit)
	trace.endUpdate(start.Add(3 * time.Millisecond))
	trace.beginDraw(start.Add(4 * time.Millisecond))
	trace.endDraw(start.Add(6 * time.Millisecond))
	trace.beginUpdate(start.Add(20*time.Millisecond), state, emit)
	if len(messages) != 0 {
		t.Fatal("must not write a log for every frame")
	}
	if trace.stats[timingUpdateInterval].max != 20*time.Millisecond ||
		trace.stats[timingUpdateWork].max != 3*time.Millisecond ||
		trace.stats[timingHostGap].max != 17*time.Millisecond ||
		trace.stats[timingHostOther].max != 15*time.Millisecond ||
		trace.stats[timingDrawWork].max != 2*time.Millisecond {
		t.Fatal("update, draw and unattributed gap were not separated")
	}
	trace.endUpdate(start.Add(23 * time.Millisecond))
	trace.beginDraw(start.Add(24 * time.Millisecond))
	trace.endDraw(start.Add(26 * time.Millisecond))
	trace.flush(start.Add(time.Second), "exit", emit)
	for _, field := range []string{"frame_timing", "reason=exit", "focused=true", "update_interval={n=1", "draw_interval={n=1", "p95_ms=", "p99_ms=", "over_60hz="} {
		if len(messages) != 1 || !strings.Contains(messages[0], field) {
			t.Fatalf("missing %q in %v", field, messages)
		}
	}
	trace.flush(start.Add(2*time.Second), "exit", emit)
	if len(messages) != 1 {
		t.Fatal("empty summary was written")
	}
}

func TestFrameTimingPeriodicAndStateReset(t *testing.T) {
	var trace frameTiming
	var messages []string
	emit := func(s string) { messages = append(messages, s) }
	start := time.Unix(100, 0)
	state := frameTimingState{focused: true, loading: true}
	trace.beginUpdate(start, state, emit)
	trace.endUpdate(start.Add(time.Millisecond))
	trace.beginUpdate(start.Add(5*time.Second), state, emit)
	if len(messages) != 1 || !strings.Contains(messages[0], "reason=periodic") {
		t.Fatalf("missing periodic summary: %v", messages)
	}
	trace.endUpdate(start.Add(5*time.Second + time.Millisecond))
	trace.beginDraw(start.Add(5*time.Second + 2*time.Millisecond))
	trace.endDraw(start.Add(5*time.Second + 3*time.Millisecond))
	state.loading = false
	trace.beginUpdate(start.Add(6*time.Second), state, emit)
	if len(messages) != 2 || !strings.Contains(messages[1], "loading=true") {
		t.Fatalf("loading samples must be flushed separately: %v", messages)
	}
	if trace.stats[timingUpdateInterval].count != 0 || !trace.lastDraw.IsZero() {
		t.Fatal("state transition polluted gameplay intervals")
	}
	trace.endUpdate(start.Add(6*time.Second + time.Millisecond))
	state.focused, state.minimized = false, true
	trace.beginUpdate(start.Add(7*time.Second), state, emit)
	if len(messages) != 3 || !strings.Contains(messages[2], "focused=true minimized=false loading=false") {
		t.Fatalf("foreground samples mixed with background samples: %v", messages)
	}
}

func TestFrameTimingSampleAllocations(t *testing.T) {
	var d timingDistribution
	if n := testing.AllocsPerRun(100, func() { d.add(17 * time.Millisecond) }); n != 0 {
		t.Fatalf("sampling allocates %g objects", n)
	}
}

func TestFrameTimingRecordsShortPhases(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g := &game{controller: cadenceController(t, 60, `{}`), ctx: ctx}
	if err := g.Update(); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if g.frameTiming != nil {
		t.Fatal("ordinary runs must not allocate timing histograms")
	}
	var messages []string
	g.options.Diagnostic = func(s string) { messages = append(messages, s) }
	if err := g.Update(); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	g.reportSlowPhase("vm", 20*time.Millisecond, 2)
	g.reportSlowPhase("audio", time.Millisecond, 1)
	g.reportSlowPhase("draw_cpu", 10*time.Millisecond, 1)
	if len(messages) != 0 {
		t.Fatalf("short phases should only appear in the periodic summary: %v", messages)
	}
	g.frameTiming.flush(time.Now(), "exit", g.options.Diagnostic)
	for _, field := range []string{
		"logic_fps=60 vm_ticks=2",
		"vm_batch={n=1 mean_ms=20.000",
		"audio={n=1 mean_ms=1.000",
		"draw_prepare={n=1 mean_ms=10.000",
	} {
		if len(messages) != 1 || !strings.Contains(messages[0], field) {
			t.Fatalf("short phase was lost: %q in %v", field, messages)
		}
	}
}
