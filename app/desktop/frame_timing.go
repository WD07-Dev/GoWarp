package desktop

import (
	"fmt"
	"strings"
	"time"
)

type timingDistribution struct {
	buckets [1002]uint32
	count   uint32
	total   time.Duration
	max     time.Duration
	over60  uint32
	over30  uint32
	over50  uint32
}

func (d *timingDistribution) add(elapsed time.Duration) {
	elapsed = max(0, elapsed)
	bucket := min(int(elapsed/time.Millisecond), len(d.buckets)-1)
	if bucket < len(d.buckets)-1 && elapsed%time.Millisecond != 0 {
		bucket++
	}
	d.buckets[bucket]++
	d.count++
	d.total += elapsed
	d.max = max(d.max, elapsed)
	if elapsed > time.Second/60 {
		d.over60++
	}
	if elapsed > time.Second/30 {
		d.over30++
	}
	if elapsed > 50*time.Millisecond {
		d.over50++
	}
}

// Percentiles use 1 ms upper bounds; the overflow bucket uses the observed maximum.
func (d *timingDistribution) percentile(percent uint32) time.Duration {
	if d.count == 0 {
		return 0
	}
	rank := (d.count*percent + 99) / 100
	var count uint32
	for i, n := range d.buckets {
		count += n
		if count >= rank {
			if i == len(d.buckets)-1 {
				return d.max
			}
			return min(d.max, time.Duration(i)*time.Millisecond)
		}
	}
	return d.max
}

const (
	timingUpdateInterval = iota
	timingDrawInterval
	timingUpdateWork
	timingDrawWork
	timingHostGap
	timingHostOther
	timingVMBatch
	timingAudio
	timingDrawPrepare
	timingMetricCount
)

var timingNames = [...]string{
	"update_interval", "draw_interval", "update_work", "draw_work",
	"host_gap", "host_other", "vm_batch", "audio", "draw_prepare",
}

type frameTimingState struct {
	focused, minimized, loading bool
	logicFPS                    float64
}

type frameTiming struct {
	state                         frameTimingState
	start, updateStart, updateEnd time.Time
	lastDraw                      time.Time
	drawSinceUpdate               time.Duration
	ticks                         int
	stats                         [timingMetricCount]timingDistribution
}

func (f *frameTiming) beginUpdate(now time.Time, state frameTimingState, emit func(string)) {
	if f.start.IsZero() {
		f.start, f.state = now, state
	} else if f.state != state {
		f.flush(now, "state_change", emit)
		f.state = state
		f.updateStart, f.updateEnd, f.lastDraw = time.Time{}, time.Time{}, time.Time{}
		f.drawSinceUpdate = 0
	}
	if !f.updateStart.IsZero() {
		f.stats[timingUpdateInterval].add(now.Sub(f.updateStart))
	}
	if !f.updateEnd.IsZero() {
		gap := now.Sub(f.updateEnd)
		f.stats[timingHostGap].add(gap)
		// Includes presentation, event handling and scheduling; this is not GPU time.
		f.stats[timingHostOther].add(gap - f.drawSinceUpdate)
	}
	f.updateStart, f.drawSinceUpdate = now, 0
	if now.Sub(f.start) >= 5*time.Second {
		f.flush(now, "periodic", emit)
	}
}

func (f *frameTiming) endUpdate(now time.Time) {
	f.stats[timingUpdateWork].add(now.Sub(f.updateStart))
	f.updateEnd = now
}

func (f *frameTiming) beginDraw(now time.Time) {
	if !f.lastDraw.IsZero() {
		f.stats[timingDrawInterval].add(now.Sub(f.lastDraw))
	}
	f.lastDraw = now
}

func (f *frameTiming) endDraw(now time.Time) {
	elapsed := now.Sub(f.lastDraw)
	f.stats[timingDrawWork].add(elapsed)
	f.drawSinceUpdate += elapsed
}

func (f *frameTiming) flush(now time.Time, reason string, emit func(string)) {
	var message strings.Builder
	for i := range f.stats {
		d := &f.stats[i]
		if d.count == 0 {
			continue
		}
		if message.Len() == 0 {
			fmt.Fprintf(&message, "frame_timing reason=%s span_ms=%.3f focused=%t minimized=%t loading=%t logic_fps=%g vm_ticks=%d", reason, now.Sub(f.start).Seconds()*1000, f.state.focused, f.state.minimized, f.state.loading, f.state.logicFPS, f.ticks)
		}
		fmt.Fprintf(
			&message, " %s={n=%d mean_ms=%.3f p95_ms=%.3f p99_ms=%.3f max_ms=%.3f over_60hz=%d over_30hz=%d over_50ms=%d}",
			timingNames[i], d.count, d.total.Seconds()*1000/float64(d.count),
			d.percentile(95).Seconds()*1000, d.percentile(99).Seconds()*1000, d.max.Seconds()*1000, d.over60, d.over30, d.over50,
		)
	}
	f.stats = [timingMetricCount]timingDistribution{}
	f.ticks, f.start = 0, now
	if message.Len() != 0 {
		emit(message.String())
	}
}
