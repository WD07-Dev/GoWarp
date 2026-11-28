// Package vm executes Scratch projects for headless and desktop hosts.
package vm

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"gowarp/core/extension"
	"gowarp/core/project"
)

const stepsPerFrame = 10000

type Variable struct {
	Name          string `json:"name"`
	Value         any    `json:"value"`
	queryRevision *uint64
}

type TargetState struct {
	RenderLayer    string
	Camera         string
	Transform      *RenderTransform
	Clipbox        *Bounds
	BlendMode      string
	MaskMode       bool
	RenderQuality  float64
	Text           *TextSkin
	StretchX       float64             `json:"stretchX"`
	StretchY       float64             `json:"stretchY"`
	Pen            PenState            `json:"pen"`
	Pitch          float64             `json:"pitch"`
	Pan            float64             `json:"pan"`
	CurrentCostume int                 `json:"currentCostume"`
	CostumeName    string              `json:"costumeName"`
	LayerOrder     int                 `json:"layerOrder"`
	Bubble         string              `json:"bubble,omitempty"`
	Think          bool                `json:"think,omitempty"`
	Effects        GraphicEffects      `json:"effects"`
	Volume         float64             `json:"volume"`
	Draggable      bool                `json:"draggable"`
	Size           float64             `json:"size"`
	Visible        bool                `json:"visible"`
	RotationStyle  string              `json:"rotationStyle"`
	CloneID        int                 `json:"cloneId,omitempty"`
	Name           string              `json:"name"`
	IsStage        bool                `json:"isStage"`
	Variables      map[string]Variable `json:"variables"`
	Lists          map[string]List     `json:"lists"`
	X              float64             `json:"x"`
	Y              float64             `json:"y"`
	Direction      float64             `json:"direction"`
}

type target struct {
	penLayer        string
	variables       map[string]*Variable
	nativeVariables []*Variable
	edgeInputs      map[string]*thread
	owner           *Runtime
	sourceIndex     int
	edges           map[string]bool
	bubbleOwner     *thread
	dead            bool
	hats            []string
	edgeHats        []string
	broadcastHats   map[string][]string
	hatThreads      map[string]*thread
	procedures      map[string]*procedure
	source          *project.Target
	state           TargetState
}

type continuation struct {
	nextLink, bodyLink *executionBlock
	regions            map[string]*nativeRegion
	extension          *extensionInvocation
	forEach            bool
	index              float64
	header, exit       string
	switchBlock        bool
	caseEnd            bool
	call               bool
	params             procedureParameters
	warp               bool
	procedure          string
	next, body         string
	remaining          float64
	loop               bool
}

type thread struct {
	stackClick       bool
	editorReport     func(string, error)
	code             *executionCode
	cursor           *executionBlock
	regions          map[string]*nativeRegion
	extensionCall    *extensionInvocation
	extensionHost    *extensionContext
	sliceSteps       *int
	sliceExecuted    int
	sliceLimit       int
	sliceStarted     time.Time
	inputRequest     *inputRequest
	expression       *expressionState
	reporters        []*reporterFrame
	reporterBoundary *int
	reporterSteps    *int
	reporterBudget   int
	result           any
	soundWait        *soundVoice
	asking           bool
	bubbleEnd        bool
	glide            *glideState
	hat              string
	slot             int
	replaced         bool
	waiting          []*thread
	params           procedureParameters
	warp             bool
	nativeYield      bool
	procedure        string
	target           *target
	pc               string
	stack            []continuation
	wake             float64
	wakeFrame        int
	done             bool
}

type Runtime struct {
	targetQueries                     *targetQueryCache
	freeParams                        [8]procedureParameters
	nativeArguments                   []calculationValue
	executionCodes                    map[*project.Target]*executionCode
	stepDiagnostics                   StepDiagnostics
	measureSteps                      bool
	redrawRequested                   bool
	native                            bool
	costumeNames                      map[*project.Target]map[string]int
	trigResults                       [3][16]trigResult
	numericExpressions                map[*project.Block]*numericExpression
	calculations                      map[*project.Block]*calculation
	preparedInputs                    map[*project.Block]*preparedInput
	listWrites                        map[*project.Block]*listWrite
	variableWrites                    map[*project.Block]*variableWrite
	coordinateMoves                   map[*project.Block]*coordinateMove
	project                           *project.Project
	sourceProject                     *project.Project
	extensions                        []*extension.Extension
	extensionBlocks                   map[string]*registeredBlock
	extensionErr                      error
	procedureCodeCache                map[*project.Block]string
	procedureArgumentCache            map[*project.Block]*procedureInputs
	controlInputs                     map[*project.Block]*numericExpression
	sourceCount                       int
	logic                             logicState
	visual                            visualState
	audioExtensions                   *audioExtensionState
	host                              hostExtensionState
	listObserver                      func(ListAppend)
	listNotes                         map[*project.Block]string
	debug                             debugState
	data                              dataState
	graphics                          Graphics
	variableObservations              []*variableObservation
	graphicsDataIndependent           bool
	options                           RuntimeOptions
	elapsed, timerSeconds, initialFPS float64
	elapsedError                      float64
	realtime                          bool
	realtimeStart                     time.Time
	realtimeBase                      float64
	audioElapsed                      float64
	fpsOverride, turboOverride        bool
	audioEnabled                      bool
	audioCommands                     []AudioCommand
	audioDurations                    [][]float64
	nextVoice                         int
	// Event order follows source layering/cloning; targets retains stable snapshot order.
	executable                []*target
	voices                    map[*project.Target]map[int]*soundVoice
	counter                   float64
	events                    []InputEvent
	keys                      map[string]bool
	mouseX, mouseY            float64
	mouseDown                 bool
	lastKey, answer, username string
	loudness                  float64
	askThread                 *thread
	askPrompt                 string
	askID                     uint64
	now                       func() time.Time
	drawList                  []*target
	drawOrderDirty            bool
	sensingScene              []DisplayTarget
	cloneCount                int
	nextCloneID               int
	broadcastNames            []string
	targets                   []*target
	stage                     *target
	threads                   []*thread
	frame                     int
	fps                       float64
	turbo                     bool
	stopped                   bool
}

// New copies initial state from a project validated by project.Load.
func New(p *project.Project) *Runtime {
	r := &Runtime{
		project:       p,
		sourceProject: p,
		sourceCount:   len(p.Targets),
		keys:          map[string]bool{},
		now:           time.Now,
		fps:           30,
	}
	r.options = defaultRuntimeOptions()
	for sourceIndex, src := range p.Targets {
		t := &target{
			owner:       r,
			variables:   make(map[string]*Variable, len(src.Variables)),
			sourceIndex: sourceIndex,
			source:      src,
			state: TargetState{
				Name:          src.Name,
				IsStage:       src.IsStage,
				Lists:         map[string]List{},
				X:             src.X,
				Y:             src.Y,
				Direction:     src.Direction,
				Size:          src.Size,
				Visible:       src.Visible,
				RotationStyle: src.RotationStyle,
			},
		}
		for id, v := range src.Variables {
			t.variables[id] = &Variable{Name: v[0].(string), Value: v[1]}
		}
		for id, list := range src.Lists {
			items := append([]any{}, list[1].([]any)...)
			t.state.Lists[id] = List{Name: list[0].(string), Items: items}
		}
		t.state.StretchX, t.state.StretchY = 100, 100
		t.state.Pen = PenState{
			Size:       1,
			Hue:        66.66,
			Saturation: 100,
			Brightness: 100,
			Shade:      50,
		}
		t.state.CurrentCostume = src.CurrentCostume
		if t.state.CurrentCostume < 0 {
			t.state.CurrentCostume = 0
		}
		if n := len(src.Costumes); n > 0 && t.state.CurrentCostume >= n {
			t.state.CurrentCostume = n - 1
		}
		t.state.CostumeName = costumeName(t)
		t.state.LayerOrder = src.LayerOrder
		t.state.Volume = src.Volume
		t.state.Draggable = src.Draggable
		r.targets = append(r.targets, t)
		r.executable = append(r.executable, t)
		r.drawList = append(r.drawList, t)
		for _, name := range src.Broadcasts {
			r.broadcastNames = append(r.broadcastNames, name)
		}
		if src.IsStage {
			r.stage = t
		}
		t.refreshHats()
	}
	sort.SliceStable(r.drawList, func(i, j int) bool {
		if r.drawList[i].state.IsStage != r.drawList[j].state.IsStage {
			return r.drawList[i].state.IsStage
		}
		return r.drawList[i].state.LayerOrder < r.drawList[j].state.LayerOrder
	})
	r.updateLayers()
	/*
		Scratch initializes execution order from the saved layers as well as
		drawing order. Keep target/asset indices in their original pane order.
	*/
	copy(r.executable, r.drawList)
	r.native = nativeProjectKey != "" && nativeKey(p) == nativeProjectKey
	for i := len(r.executable) - 1; i >= 0; i-- {
		r.startHatsFor(r.executable[i], "event_whenflagclicked", "", true)
	}
	r.InitData(p)
	r.refreshNativeVariables()
	r.configureProject(p)
	return r
}

// Run advances virtual frames without wall-clock sleeps. Subsequent calls resume.
func (r *Runtime) Run(ctx context.Context, frames int, fps float64, turbo bool) error {
	return r.advance(ctx, frames, fps, turbo, false)
}

func (r *Runtime) advance(ctx context.Context, frames int, fps float64, turbo, keepIdle bool) error {
	if len(r.variableObservations) != 0 {
		r.readObservedVariables()
		defer r.writeObservedVariables()
	}
	if frames < 0 || fps <= 0 || !finite(fps) {
		return fmt.Errorf("frames must be nonnegative and fps must be finite and positive")
	}
	if r.initialFPS != 0 && r.initialFPS != fps {
		return fmt.Errorf("cannot change fps during execution")
	}
	r.initialFPS = fps
	if !r.fpsOverride {
		r.fps = fps
	}
	if !r.turboOverride {
		r.turbo = turbo
	}
	for i := 0; i < frames; i++ {
		if r.extensionErr != nil {
			return r.extensionErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if r.stopped || r.Paused() || (!keepIdle && r.Done()) {
			break
		}
		r.syncTime()
		r.cleanQuestion()
		for len(r.events) > 0 && r.events[0].Frame <= r.frame {
			event := r.events[0]
			r.events = r.events[1:]
			r.applyInput(event)
		}
		if err := r.extensionEvent("before-tick", r.stage); err != nil {
			return err
		}
		if err := r.pollEdgeHats(ctx); err != nil {
			return err
		}
		if err := r.stepThreads(ctx); err != nil {
			return err
		}
		if r.Paused() {
			break
		}
		if err := r.updateMonitorReporters(ctx); err != nil {
			return err
		}
		r.advanceTime()
		r.frame++
		if err := r.extensionEvent("tick", r.stage); err != nil {
			return err
		}
		if r.graphics != nil && (r.options.View.Shader != nil || r.splitEffectsView() != nil) {
			r.graphics.SetView(r.View())
		}
	}
	return nil
}

func (r *Runtime) Done() bool {
	if r.stopped {
		return true
	}
	if len(r.events) > 0 {
		return false
	}
	for _, t := range r.targets {
		if len(t.edgeHats) != 0 {
			return false
		}
	}
	for _, th := range r.threads {
		if !th.done && !th.replaced {
			return false
		}
	}
	return true
}

func (r *Runtime) Frames() int {
	return r.frame
}

func (r *Runtime) Snapshot() []TargetState {
	r.readObservedVariables()
	states := make([]TargetState, 0, len(r.targets))
	for _, t := range r.targets {
		s := t.state
		s.Variables = map[string]Variable{}
		for id, cell := range t.variables {
			v := *cell
			if n, ok := v.Value.(float64); ok && !finite(n) {
				v.Value = text(n)
			}
			s.Variables[id] = v
		}
		s.Lists = map[string]List{}
		for id, list := range t.state.Lists {
			list.Items = append([]any{}, list.Items...)
			list.sharing = nil
			list.textIndex = nil
			for i, item := range list.Items {
				if n, ok := item.(float64); ok && !finite(n) {
					list.Items[i] = text(n)
				}
			}
			s.Lists[id] = list
		}
		states = append(states, s)
	}
	return states
}

func (r *Runtime) variable(t *target, id string) (*target, *Variable, error) {
	if v, ok := t.variables[id]; ok {
		return t, v, nil
	}
	if r.stage != nil {
		if v, ok := r.stage.variables[id]; ok {
			return r.stage, v, nil
		}
	}
	return nil, nil, fmt.Errorf("unknown variable %q", id)
}

func field(b *project.Block, name string, index int) string {
	v := b.Fields[name]
	if len(v) <= index {
		return ""
	}
	s, _ := v[index].(string)
	return s
}

func substack(b *project.Block, name string) (string, error) {
	in, ok := b.Inputs[name]
	if !ok {
		return "", nil
	}
	if len(in) < 2 {
		return "", fmt.Errorf("invalid %s input", name)
	}
	if in[1] == nil {
		return "", nil
	}
	s, ok := in[1].(string)
	if !ok {
		return "", fmt.Errorf("invalid %s block reference", name)
	}
	return s, nil
}

func (r *Runtime) stepBody(ctx context.Context, th *thread) error {
	if th.soundWait != nil {
		r.advanceVoice(th.soundWait)
		if th.soundWait.active || th.soundWait.outputPending {
			return nil
		}
		th.soundWait = nil
	}
	if th.asking {
		r.cleanQuestion()
		if r.askThread == th {
			return nil
		}
		th.asking = false
		if th.target.bubbleOwner == th {
			th.target.state.Bubble = ""
			th.target.requestRedraw()
			th.target.bubbleOwner = nil
		}
	}
	if anyRunning(th.waiting) {
		return nil
	}
	th.waiting = nil
	if th.bubbleEnd {
		if th.target.bubbleOwner == th {
			th.target.state.Bubble = ""
			th.target.requestRedraw()
			th.target.bubbleOwner = nil
		}
		th.bubbleEnd = false
	}
	if th.glide != nil && !r.advanceGlide(th) {
		return nil
	}
	for th.canStep() {
		*th.sliceSteps++
		if r.stopped || th.done {
			return nil
		}
		if th.reporterSteps != nil {
			if *th.reporterSteps <= 0 {
				return fmt.Errorf("reporter procedure instruction limit exceeded")
			}
			*th.reporterSteps--
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if request := th.inputRequest; request != nil && len(th.reporters) == 0 {
			if th.expression == nil {
				th.expression = newExpression("")
			}
			budget := stepsPerFrame
			value, err := r.input(ctx, th, request.block, request.name, 0, &budget)
			if err != nil {
				return err
			}
			request.value = value
			th.done = true
			return nil
		}
		if th.pc == "" {
			if len(th.stack) == 0 {
				th.done = true
				return nil
			}
			i := len(th.stack) - 1
			c := &th.stack[i]
			if c.forEach {
				th.pc = c.header
				if !th.warp {
					return nil
				}
				continue
			}
			if c.caseEnd {
				th.stack = th.stack[:i]
				th.breakControl()
				continue
			}
			if c.call {
				th.releaseParams(th.params)
				th.params, th.warp, th.procedure = c.params, c.warp, c.procedure
				th.regions = c.regions
				c.params = nil
			}
			if c.loop && c.remaining > 1 {
				c.remaining--
				th.pc, th.cursor = c.body, c.bodyLink
			} else {
				th.pc, th.cursor = c.next, c.nextLink
				th.stack = th.stack[:i]
			}
			if c.call {
				th.result = ""
				if th.reporterBoundary != nil && len(th.stack) <= *th.reporterBoundary {
					return nil
				}
			}
			if c.extension != nil {
				th.extensionCall = c.extension
				c.extension = nil
				if !th.warp {
					return nil
				}
			}
			if c.loop && !th.warp {
				return nil
			}
			continue
		}
		id := th.pc
		instruction := r.executionBlock(th)
		b := instruction.source
		if b == nil {
			return fmt.Errorf("missing block %q", id)
		}
		th.cursor = instruction.nextBlock()
		if th.regions != nil && r.native && th.reporterSteps == nil && r.listObserver == nil {
			if region := th.regions[id]; region != nil && (th.warp || region.resumable) && len(th.stack)+region.depth < 1024 &&
				(region.work == 0 || th.sliceLimit-(*th.sliceSteps-1) >= region.work) {
				if th.expression != nil && th.expression.root.children != nil {
					th.expression.reset(id)
				}
				// The generated region counts its first statement too.
				*th.sliceSteps--
				if err := region.run(ctx, r, th); err != nil {
					return err
				}
				if th.nativeYield {
					th.nativeYield = false
					return nil
				}
				continue
			}
		}
		if b.Next != "" && th.reporterSteps == nil && r.listObserver == nil && calculationStatement(b.Opcode) {
			if instruction.ready&calculationReady == 0 {
				instruction.calculation = r.prepareCalculation(th.target, b)
				instruction.ready |= calculationReady
			}
			if program := instruction.calculation; program != nil {
				if th.expression != nil && th.expression.root.children != nil {
					th.expression.reset(id)
				}
				if err := program.run(ctx, r, th); err != nil {
					return err
				}
				continue
			}
		}
		/*
			Pure variable inputs use the same prepared expressions as list writes.
			Reporter procedures and extensions retain their resumable input path.
		*/
		if b.Opcode == "data_setvariableto" || b.Opcode == "data_changevariableby" {
			if instruction.ready&writePlanReady == 0 {
				instruction.variableWrite = r.prepareVariableWrite(th.target, b)
				instruction.ready |= writePlanReady
			}
			if write := instruction.variableWrite; write != nil {
				if th.expression != nil && th.expression.root.children != nil {
					th.expression.reset(id)
				}
				th.pc = b.Next
				if err := write.run(ctx, r, th); err != nil {
					return err
				}
				continue
			}
		}
		/*
			Prepared list inputs cannot suspend or call extensions. Execute them
			directly without building/resetting an input continuation each time.
		*/
		if r.listObserver == nil && (b.Opcode == "data_replaceitemoflist" || b.Opcode == "data_addtolist") {
			if instruction.ready&writePlanReady == 0 {
				instruction.listWrite = r.prepareListWrite(th.target, b)
				instruction.ready |= writePlanReady
			}
			if write := instruction.listWrite; write != nil {
				if th.expression != nil && th.expression.root.children != nil {
					th.expression.reset(id)
				}
				th.pc = b.Next
				if err := write.run(ctx, r, th); err != nil {
					return err
				}
				continue
			}
		}
		if b.Opcode == "motion_gotoxy" {
			if instruction.ready&writePlanReady == 0 {
				instruction.move = r.prepareCoordinateMove(th.target, b)
				instruction.ready |= writePlanReady
			}
			if move := instruction.move; move != nil {
				if th.expression != nil && th.expression.root.children != nil {
					th.expression.reset(id)
				}
				th.pc = b.Next
				if err := move.run(ctx, r, th); err != nil {
					return err
				}
				continue
			}
		}
		if th.expression == nil {
			th.expression = newExpression(id)
		} else if !th.expression.resume || th.expression.statement != id {
			th.expression.reset(id)
		}
		th.expression.resume = false
		th.pc = b.Next
		budget := stepsPerFrame
		prepared := r.statementInput(th, instruction)
		input := func(name string) (any, error) {
			if prepared != nil && prepared.name == name {
				if prepared.constant {
					return prepared.value, nil
				}
				return prepared.read(ctx, r, th, name, 0, &budget)
			}
			return r.input(ctx, th, b, name, 0, &budget)
		}
		if !instruction.directDispatch {
			if result, handled, err := r.invokeExtension(ctx, th, b, input, false, 0, &budget); handled {
				if err != nil || result.YieldTick || (result.Yield && (!th.warp || th.reporterSteps != nil)) {
					return err
				}
				continue
			}
			if handled, err := r.graphicsStatement(th, b, input); handled {
				if err != nil {
					return err
				}
				continue
			}
			if handled, err := r.dataStatement(th, b, input); handled {
				if err != nil {
					return err
				}
				continue
			}
		}
		switch b.Opcode {
		case "event_broadcast", "event_broadcastandwait":
			value, err := input("BROADCAST_INPUT")
			if err != nil {
				return err
			}
			started := r.broadcast(text(value))
			if b.Opcode == "event_broadcastandwait" {
				th.waiting = started
				if anyRunning(started) {
					return nil
				}
			}
		case "control_create_clone_of":
			value, err := input("CLONE_OPTION")
			if err != nil {
				return err
			}
			r.createClone(r.cloneSource(th.target, text(value)))
		case "control_delete_this_clone":
			if th.target.state.CloneID != 0 {
				r.deleteClone(th.target)
				th.done = true
				return nil
			}
		case "motion_glidesecstoxy", "motion_glideto", "motion_movesteps", "motion_turnright", "motion_turnleft", "motion_pointindirection", "motion_gotoxy", "motion_setx", "motion_changexby", "motion_sety", "motion_changeyby", "motion_goto", "motion_pointtowards", "motion_setrotationstyle", "looks_setsizeto", "looks_changesizeby", "looks_show", "looks_hide":
			yield, err := r.motionStatement(th, b, input)
			if err != nil {
				return err
			}
			if yield {
				return nil
			}
		case "looks_say", "looks_think", "looks_sayforsecs", "looks_thinkforsecs", "looks_switchcostumeto", "looks_nextcostume", "looks_switchbackdropto", "looks_switchbackdroptoandwait", "looks_nextbackdrop", "looks_seteffectto", "looks_changeeffectby", "looks_cleargraphiceffects", "looks_gotofrontback", "looks_goforwardbackwardlayers":
			yield, err := r.looksStatement(th, b, input)
			if err != nil {
				return err
			}
			if yield {
				return nil
			}
		case "sound_play", "sound_playuntildone", "sound_stopallsounds", "sound_setvolumeto", "sound_changevolumeby", "sound_seteffectto", "sound_changeeffectby", "sound_cleareffects":
			yield, err := r.soundStatement(th, b, input)
			if err != nil {
				return err
			}
			if yield {
				return nil
			}
		case "procedures_return":
			value, err := input("VALUE")
			if err != nil {
				return err
			}
			th.stopScript()
			th.result = value
			if th.done || (th.reporterBoundary != nil && len(th.stack) <= *th.reporterBoundary) {
				return nil
			}
		case "procedures_call":
			yield, err := r.call(ctx, th, b, input, false)
			if err != nil {
				return err
			}
			if yield {
				return nil
			}
		case "data_setvariableto", "data_changevariableby":
			value, err := input("VALUE")
			if err != nil {
				return err
			}
			_, v, err := r.variable(th.target, field(b, "VARIABLE", 1))
			if err != nil {
				return err
			}
			if b.Opcode == "data_changevariableby" {
				value = numberValue(number(v.Value) + number(value))
			}
			v.set(value)
		case "data_addtolist", "data_deleteoflist", "data_deletealloflist", "data_insertatlist", "data_replaceitemoflist":
			if err := r.listStatement(th.target, b, input); err != nil {
				return err
			}
		case "control_clear_counter":
			r.counter = 0
		case "control_incr_counter":
			r.counter++
		case "control_break":
			th.breakControl()
		case "control_for_each":
			if err := r.forEach(th, id, b, input); err != nil {
				return err
			}
		case "control_switch":
			if err := r.switchControl(ctx, th, b, &budget); err != nil {
				return err
			}
		case "control_case", "control_case_fallthrough", "control_default", "control_all_at_once":
			if b.Opcode != "control_all_at_once" && !th.inSwitch() {
				continue
			}
			if b.Opcode == "control_case_fallthrough" {
				continue
			}
			body, link, err := instruction.branch("SUBSTACK")
			if err != nil {
				return err
			}
			if len(th.stack) >= 1024 {
				return fmt.Errorf("control nesting limit exceeded")
			}
			th.stack = append(th.stack, continuation{next: b.Next, nextLink: instruction.nextBlock(), caseEnd: b.Opcode == "control_case"})
			th.pc, th.cursor = body, link
		case "motion_scroll_right", "motion_scroll_up", "motion_align_scene", "looks_hideallsprites", "looks_changestretchby", "looks_setstretchto", "procedures_definition", "procedures_prototype":
			// Explicit legacy no-ops in the reference.
		case "control_repeat", "control_forever":
			count := math.Inf(1)
			if b.Opcode == "control_repeat" {
				v, err := r.controlInput(ctx, th, instruction, "TIMES", input)
				if err != nil {
					return err
				}
				count = math.Floor(number(v) + 0.5)
			}
			if count <= 0 {
				continue
			}
			body, link, err := instruction.branch("SUBSTACK")
			if err != nil {
				return err
			}
			if len(th.stack) >= 1024 {
				return fmt.Errorf("control nesting limit exceeded at %q", id)
			}
			th.stack = append(th.stack, continuation{
				next:      b.Next,
				nextLink:  instruction.nextBlock(),
				bodyLink:  link,
				body:      body,
				remaining: count,
				loop:      true,
				exit:      b.Next,
			})
			th.pc, th.cursor = body, link
		case "control_if", "control_if_else":
			v, err := r.controlInput(ctx, th, instruction, "CONDITION", input)
			if err != nil {
				return err
			}
			name := "SUBSTACK"
			if !truth(v) {
				if b.Opcode == "control_if" {
					continue
				}
				name = "SUBSTACK2"
			}
			body, link, err := instruction.branch(name)
			if err != nil {
				return err
			}
			if body != "" {
				if len(th.stack) >= 1024 {
					return fmt.Errorf("control nesting limit exceeded at %q", id)
				}
				th.stack = append(th.stack, continuation{next: b.Next, nextLink: instruction.nextBlock()})
				th.pc, th.cursor = body, link
			}
		case "control_repeat_until", "control_while":
			value, err := r.controlInput(ctx, th, instruction, "CONDITION", input)
			if err != nil {
				return err
			}
			enter := !truth(value)
			if b.Opcode == "control_while" {
				enter = truth(value)
			}
			if !enter {
				continue
			}
			body, link, err := instruction.branch("SUBSTACK")
			if err != nil {
				return err
			}
			if len(th.stack) >= 1024 {
				return fmt.Errorf("control nesting limit exceeded at %q", id)
			}
			th.stack = append(th.stack, continuation{
				next:      id,
				nextLink:  instruction,
				bodyLink:  link,
				body:      body,
				remaining: 1,
				loop:      true,
				exit:      b.Next,
			})
			th.pc, th.cursor = body, link
		case "control_wait_until":
			value, err := r.controlInput(ctx, th, instruction, "CONDITION", input)
			if err != nil {
				return err
			}
			if !truth(value) {
				th.pc = id
				return nil
			}
		case "sensing_askandwait":
			r.cleanQuestion()
			if r.askThread != nil {
				th.pc = id
				return nil
			}
			value, err := input("QUESTION")
			if err != nil {
				return err
			}
			r.askID++
			r.askThread = th
			r.askPrompt = text(value)
			th.asking = true
			r.redrawRequested = true
			if th.target.state.Visible && !th.target.state.IsStage {
				th.target.state.Bubble = r.askPrompt
				th.target.state.Think = false
				th.target.bubbleOwner = th
			}
			return nil
		case "sensing_setdragmode":
			th.target.state.Draggable = field(b, "DRAG_MODE", 0) == "draggable"
		case "sensing_resettimer":
			r.timerSeconds = r.elapsed
		case "control_wait":
			v, err := input("DURATION")
			if err != nil {
				return err
			}
			duration := math.Max(0, number(v))
			th.wake = r.elapsed + duration
			r.redrawRequested = true
			/*
				Reporter evaluation forces warp internally; its waits still
				suspend the caller even without a positive duration.
			*/
			if th.warp && th.reporterSteps == nil && duration == 0 {
				continue
			}
			/*
				A wait yields the script, not necessarily the tick. Redraw ends
				normal-mode passes; turbo can resume an expired wait this tick.
			*/
			return nil
		case "control_stop":
			switch field(b, "STOP_OPTION", 0) {
			case "all":
				r.stopAll()
				r.host.commands = append(r.host.commands, HostCommand{Kind: "stop-all"})
			case "this script":
				th.stopScript()
				th.result = ""
				if th.reporterBoundary != nil && len(th.stack) <= *th.reporterBoundary {
					return nil
				}
			case "other scripts in sprite":
				for _, other := range r.threads {
					if other != th && other.target == th.target {
						other.done = true
					}
				}
			default:
				return fmt.Errorf("unsupported stop option")
			}
			if r.stopped || th.done {
				return nil
			}
		default:
			return fmt.Errorf("unsupported opcode %q at block %q", b.Opcode, id)
		}
	}
	if th.reporterSteps != nil && *th.reporterSteps <= 0 {
		return fmt.Errorf("reporter procedure instruction limit exceeded")
	}
	/*
		A bounded instruction slice prevents malformed next cycles and turbo loops
		from blocking cancellation or monopolizing the scheduler.
	*/
	return nil
}
