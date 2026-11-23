package vm

import (
	"context"
	"fmt"
	"time"
)

/*
StepDiagnostics describes a host update, which may contain multiple VM ticks.
The longest entries are individual script turns, not per-sprite totals.
*/
type StepDiagnostics struct {
	Passes, Turns, Steps int
	Elapsed              time.Duration
	Native               bool
	Longest              [3]ScriptTiming
}

type ScriptTiming struct {
	Target, Hat, Block string
	Elapsed            time.Duration
	Steps              int
}

func (r *Runtime) StartStepDiagnostics() {
	r.stepDiagnostics = StepDiagnostics{Native: r.native}
	r.measureSteps = true
}

func (r *Runtime) FinishStepDiagnostics() StepDiagnostics {
	r.measureSteps = false
	return r.stepDiagnostics
}

func (d *StepDiagnostics) record(script ScriptTiming) {
	d.Turns++
	d.Steps += script.Steps
	for i := range d.Longest {
		if script.Elapsed > d.Longest[i].Elapsed {
			copy(d.Longest[i+1:], d.Longest[i:len(d.Longest)-1])
			d.Longest[i] = script
			break
		}
	}
}

/*
A yield ends one script turn. Redraw stops the next pass, not the current one. Input and clocks
advance once per step.
*/
func (r *Runtime) stepThreads(ctx context.Context) error {
	r.redrawRequested = false
	started := time.Now()
	if r.measureSteps {
		defer func() { r.stepDiagnostics.Elapsed += time.Since(started) }()
	}
	budget := time.Duration(float64(time.Second) * .75 / r.FPS())
	work := 0
	for {
		if r.measureSteps {
			r.stepDiagnostics.Passes++
		}
		executed := 0
		// Include newly started hats, but bound cyclic broadcast chains.
		dispatchLimit := len(r.threads) + stepsPerFrame
		for index := 0; index < len(r.threads) && index < dispatchLimit; index++ {
			if r.stopped || r.Paused() {
				break
			}
			th := r.threads[index]
			if th.done || th.replaced || r.wakePending(th) {
				continue
			}
			var timing ScriptTiming
			var turnStart time.Time
			if r.measureSteps {
				timing = ScriptTiming{Target: th.target.state.Name, Hat: th.hat, Block: th.pc}
				turnStart = time.Now()
			}
			err := r.step(ctx, th)
			if r.measureSteps {
				timing.Elapsed, timing.Steps = time.Since(turnStart), th.sliceExecuted
				r.stepDiagnostics.record(timing)
			}
			if err != nil {
				return fmt.Errorf("target %q: %w", th.target.state.Name, err)
			}
			executed += *th.sliceSteps
		}
		r.compactThreads()
		/*
			A broadcast can replace an already visited slot. Inspect the live
			threads rather than the instances which ran during this pass.
		*/
		active := false
		for _, th := range r.threads {
			if !r.wakePending(th) {
				active = true
				break
			}
		}
		if r.extensionErr != nil {
			return r.extensionErr
		}
		if !active || executed == 0 || r.stopped || r.Paused() || (r.redrawRequested && !r.turbo) {
			return nil
		}
		work += executed
		if r.realtime {
			if time.Since(started) >= budget {
				return nil
			}
		} else if work >= stepsPerFrame {
			/*
				Offline runs and replays must not depend on the machine's speed.
				They use a deterministic work budget with the same yield rules.
			*/
			return nil
		}
	}
}

func (t *target) requestRedraw() {
	if t.owner != nil && (t.state.Visible || t.state.IsStage) {
		t.owner.redrawRequested = true
	}
}
