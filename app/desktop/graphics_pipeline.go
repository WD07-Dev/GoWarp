package desktop

import (
	"context"
	"fmt"
	"image/color"
	"runtime"
	"time"

	"gowarp/core/project"
	"gowarp/core/vm"
)

const graphicsBatchSize = 128

type graphicsCommand struct {
	recordedPen
	prepare       bool
	source, index int
}

type graphicsQueryKind uint8

const (
	querySetView graphicsQueryKind = iota + 1
	queryBounds
	queryCostumeSize
	queryTouching
	queryTouchingColor
	queryPen
)

// The worker waits for ack before reading the result or reusing this storage.
type graphicsQuery struct {
	target        vm.DisplayTarget
	scene         []vm.DisplayTarget
	view          vm.View
	pen           vm.PenCommand
	name          string
	x, y          float64
	tight         bool
	color         color.NRGBA
	mask          *color.NRGBA
	bounds        vm.Bounds
	ok            bool
	width, height float64
}

type graphicsWork struct {
	commands   []graphicsCommand
	call       func()
	query      graphicsQueryKind
	finished   bool
	err        error
	panicValue any
}

type graphicsTick struct {
	ctx context.Context
	run func(context.Context) error
}

type graphicsPipelineStats struct {
	ParallelTicks, Batches, Queries int
	QueryWait, QueueWait, DrawTime  time.Duration
}

/*
The VM owns pending; the display thread owns graphics. A batch changes owners
through work/free, and synchronous queries act as ordering barriers.
*/
type graphicsPipeline struct {
	measure            bool
	stats              graphicsPipelineStats
	graphics           vm.Graphics
	drain              func()
	ticks              chan graphicsTick
	work               chan graphicsWork
	free               chan []graphicsCommand
	ack                chan struct{}
	done               chan struct{}
	abort              chan struct{}
	active             bool
	query              graphicsQuery
	pending            []graphicsCommand
	penCount, barriers int
}

func newGraphicsPipeline(g vm.Graphics, drain func()) *graphicsPipeline {
	return &graphicsPipeline{graphics: g, drain: drain}
}

func (p *graphicsPipeline) start() {
	const batches = pendingPenLimit / graphicsBatchSize
	p.ticks = make(chan graphicsTick)
	p.work = make(chan graphicsWork, batches)
	p.free = make(chan []graphicsCommand, batches)
	p.ack = make(chan struct{})
	p.done = make(chan struct{})
	// Preserve the recorder's existing limit before making the VM wait.
	commands := make([]graphicsCommand, pendingPenLimit)
	for i := 0; i < len(commands); i += graphicsBatchSize {
		p.free <- commands[i : i : i+graphicsBatchSize]
	}
	go func() {
		defer close(p.done)
		for tick := range p.ticks {
			result := p.execute(tick)
			p.work <- result
		}
	}()
}

func (p *graphicsPipeline) execute(tick graphicsTick) (result graphicsWork) {
	result.finished = true
	defer func() { result.panicValue = recover() }()
	result.err = tick.run(tick.ctx)
	p.flush()
	return result
}

func (p *graphicsPipeline) close() {
	if p.ticks == nil {
		return
	}
	close(p.ticks)
	<-p.done
}

func (p *graphicsPipeline) tick(ctx context.Context, tick func(context.Context) error) error {
	// Small frames and query-heavy scripts cannot amortize cross-thread handoffs.
	parallel := runtime.GOMAXPROCS(0) > 1 && p.penCount >= 512 && p.barriers <= p.penCount/64
	p.penCount, p.barriers = 0, 0
	if parallel {
		return p.run(ctx, tick)
	}
	return tick(ctx)
}

func (p *graphicsPipeline) run(ctx context.Context, tick func(context.Context) error) error {
	if !p.ScriptDataIndependent() {
		return tick(ctx)
	}
	if p.measure {
		p.stats.ParallelTicks++
	}
	if p.ticks == nil {
		p.start()
	}
	if graphics, ok := p.graphics.(interface {
		beginParallelTick()
		endParallelTick()
	}); ok {
		graphics.beginParallelTick()
		defer graphics.endParallelTick()
	}
	ctx, cancel := context.WithCancel(ctx)
	p.abort = make(chan struct{})
	p.active = true
	finished := false
	defer func() {
		cancel()
		close(p.abort)
		if !finished {
			// A display panic must not leave the VM blocked on a query or full queue.
			for work := range p.work {
				if work.commands != nil {
					p.release(work.commands)
				}
				if work.finished {
					break
				}
			}
		}
		if p.pending != nil {
			p.release(p.pending)
			p.pending = nil
		}
		p.query = graphicsQuery{}
		p.active = false
	}()
	p.ticks <- graphicsTick{ctx: ctx, run: tick}
	for {
		work := <-p.work
		if work.finished {
			finished = true
			if work.panicValue != nil {
				panic(work.panicValue)
			}
			return work.err
		}
		var started time.Time
		if p.measure {
			started = time.Now()
		}
		if work.commands != nil {
			if p.measure {
				p.stats.Batches++
			}
			func() {
				defer p.release(work.commands)
				for i := range work.commands {
					if entry := work.commands[i]; entry.prepare {
						p.prepareCostume(entry.source, entry.index)
						continue
					}
					cmd := work.commands[i].command
					p.graphics.Pen(cmd)
				}
				if p.drain != nil {
					p.drain()
				}
			}()
		} else {
			if work.query != 0 {
				p.answer(work.query)
			} else {
				work.call()
			}
		}
		if p.measure {
			p.stats.DrawTime += time.Since(started)
		}
		if work.commands == nil {
			p.ack <- struct{}{}
		}
	}
}

func (p *graphicsPipeline) release(commands []graphicsCommand) {
	clear(commands)
	p.free <- commands[:0]
}

func (p *graphicsPipeline) send(work graphicsWork) {
	var started time.Time
	if p.measure {
		started = time.Now()
	}
	select {
	case p.work <- work:
		if p.measure {
			p.stats.QueueWait += time.Since(started)
		}
	case <-p.abort:
		if work.commands != nil {
			p.release(work.commands)
		}
		panic(context.Canceled)
	}
}

func (p *graphicsPipeline) flush() {
	if len(p.pending) == 0 {
		return
	}
	commands := p.pending
	p.pending = nil
	p.send(graphicsWork{commands: commands})
}

func (p *graphicsPipeline) invoke(call func()) {
	p.barriers++
	if !p.active {
		call()
		return
	}
	p.exchange(graphicsWork{call: call})
}

func (p *graphicsPipeline) exchange(work graphicsWork) {
	p.flush()
	p.send(work)
	var started time.Time
	if p.measure {
		p.stats.Queries++
		started = time.Now()
	}
	select {
	case <-p.ack:
		if p.measure {
			p.stats.QueryWait += time.Since(started)
		}
	case <-p.abort:
		panic(context.Canceled)
	}
}

func (p *graphicsPipeline) ask(kind graphicsQueryKind) {
	p.barriers++
	p.exchange(graphicsWork{query: kind})
}

func (p *graphicsPipeline) answer(kind graphicsQueryKind) {
	q := &p.query
	switch kind {
	case querySetView:
		p.graphics.SetView(q.view)
	case queryBounds:
		q.bounds, q.ok = p.graphics.Bounds(q.target, q.tight)
	case queryCostumeSize:
		q.width, q.height = p.graphics.CostumeSize(q.target)
	case queryTouching:
		q.ok = p.graphics.Touching(q.scene, q.target, q.name, q.x, q.y)
	case queryTouchingColor:
		q.ok = p.graphics.TouchingColor(q.scene, q.target, q.color, q.mask)
	case queryPen:
		p.graphics.Pen(q.pen)
	}
}

func (p *graphicsPipeline) ScriptDataIndependent() bool {
	g, ok := p.graphics.(interface{ ScriptDataIndependent() bool })
	return ok && g.ScriptDataIndependent()
}

func (p *graphicsPipeline) Pen(cmd vm.PenCommand) {
	batchable := (cmd.Kind == "stamp" && len(cmd.Points) == 0) || cmd.Kind == "line"
	if batchable {
		p.penCount++
	} else if !p.active {
		p.barriers++
	}
	if !p.active {
		p.graphics.Pen(cmd)
		return
	}
	if !batchable {
		p.query.pen = cmd
		p.ask(queryPen)
		p.query = graphicsQuery{}
		return
	}
	entry := graphicsCommand{recordedPen: recordedPen{command: cmd}}
	entry.command.Target.State.Variables = nil
	entry.command.Target.State.Lists = nil
	entry.command.Points = nil
	p.append(entry)
}

func (p *graphicsPipeline) append(entry graphicsCommand) {
	if p.pending == nil {
		var started time.Time
		if p.measure {
			started = time.Now()
		}
		select {
		case p.pending = <-p.free:
			if p.measure {
				p.stats.QueueWait += time.Since(started)
			}
		case <-p.abort:
			panic(context.Canceled)
		}
	}
	p.pending = append(p.pending, entry)
	if len(p.pending) == graphicsBatchSize {
		p.flush()
	}
}

func (p *graphicsPipeline) SetView(view vm.View) {
	if !p.active {
		p.barriers++
		p.graphics.SetView(view)
		return
	}
	p.query.view = view
	p.ask(querySetView)
	p.query = graphicsQuery{}
}

func (p *graphicsPipeline) Bounds(t vm.DisplayTarget, tight bool) (vm.Bounds, bool) {
	if !p.active {
		p.barriers++
		return p.graphics.Bounds(t, tight)
	}
	p.query.target, p.query.tight = t, tight
	p.ask(queryBounds)
	box, ok := p.query.bounds, p.query.ok
	p.query = graphicsQuery{}
	return box, ok
}

func (p *graphicsPipeline) CostumeSize(t vm.DisplayTarget) (float64, float64) {
	if !p.active {
		p.barriers++
		return p.graphics.CostumeSize(t)
	}
	p.query.target = t
	p.ask(queryCostumeSize)
	w, h := p.query.width, p.query.height
	p.query = graphicsQuery{}
	return w, h
}

func (p *graphicsPipeline) Touching(scene []vm.DisplayTarget, t vm.DisplayTarget, name string, x, y float64) bool {
	if !p.active {
		p.barriers++
		return p.graphics.Touching(scene, t, name, x, y)
	}
	p.query.scene, p.query.target, p.query.name = scene, t, name
	p.query.x, p.query.y = x, y
	p.ask(queryTouching)
	hit := p.query.ok
	p.query = graphicsQuery{}
	return hit
}

func (p *graphicsPipeline) TouchingColor(scene []vm.DisplayTarget, t vm.DisplayTarget, c color.NRGBA, mask *color.NRGBA) bool {
	if !p.active {
		p.barriers++
		return p.graphics.TouchingColor(scene, t, c, mask)
	}
	p.query.scene, p.query.target = scene, t
	p.query.color, p.query.mask = c, mask
	p.ask(queryTouchingColor)
	hit := p.query.ok
	p.query = graphicsQuery{}
	return hit
}

func (p *graphicsPipeline) PrepareCostume(source, index int) {
	if p.active {
		p.append(graphicsCommand{prepare: true, source: source, index: index})
	} else {
		p.prepareCostume(source, index)
	}
}

func (p *graphicsPipeline) prepareCostume(source, index int) {
	if g, ok := p.graphics.(interface{ PrepareCostume(int, int) }); ok {
		g.PrepareCostume(source, index)
	}
}

func (p *graphicsPipeline) AddCostume(source int, meta project.Costume, data []byte) (out project.Costume, err error) {
	p.invoke(func() {
		g, ok := p.graphics.(interface {
			AddCostume(int, project.Costume, []byte) (project.Costume, error)
		})
		if !ok {
			err = fmt.Errorf("dynamic costumes require a graphics renderer")
			return
		}
		out, err = g.AddCostume(source, meta, data)
	})
	return
}

func (p *graphicsPipeline) DeleteCostume(source, index int) (err error) {
	p.invoke(func() {
		g, ok := p.graphics.(interface{ DeleteCostume(int, int) error })
		if !ok {
			err = fmt.Errorf("dynamic costumes require a graphics renderer")
			return
		}
		err = g.DeleteCostume(source, index)
	})
	return
}

func (p *graphicsPipeline) Snapshot(scene []vm.DisplayTarget) (out []byte, err error) {
	p.invoke(func() {
		g, ok := p.graphics.(interface {
			Snapshot([]vm.DisplayTarget) ([]byte, error)
		})
		if !ok {
			err = fmt.Errorf("stage snapshot requires a graphics renderer")
			return
		}
		out, err = g.Snapshot(scene)
	})
	return
}
