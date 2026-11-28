package vm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"

	"gowarp/core/project"
)

func nativeRepeatCount(value float64) float64 {
	return math.Floor(value + .5)
}

func (f *nativeFrame) pushControl(th *thread, c continuation, id string) bool {
	if len(th.stack) >= 1024 {
		f.err = fmt.Errorf("control nesting limit exceeded at %q", id)
		return false
	}
	th.stack = append(th.stack, c)
	return true
}

type nativeProcedure struct {
	run         func(context.Context, *Runtime, *thread) error
	runValues   func(context.Context, *Runtime, *thread, []calculationValue) error
	depth, work int
}

type nativeRegion struct {
	run       func(context.Context, *Runtime, *thread) error
	depth     int
	work      int
	resumable bool
}

/*
Only a project-specific build installs these. Ordinary runtime templates
retain the interpreter, and another project's blocks never use this code.
*/
var nativeProjectKey string
var nativeProcedures map[int]map[string]*nativeProcedure
var nativeRegions map[int]map[string]map[string]*nativeRegion
var nativeScripts map[int]map[string]map[string]*nativeRegion

type nativeInput func(context.Context, *Runtime, *thread, procedureParameters) error

var nativeCallInputs map[int]map[string]nativeInput
var nativeVariableIDs map[int][]string

func nativeArgument(th *thread, name string) calculationValue {
	if value, ok := th.params[name]; ok {
		return value
	}
	return calculationValue{numeric: true}
}

func (r *Runtime) variableBinding(t *target, id string) int {
	if !r.native {
		return 0
	}
	return slices.Index(nativeVariableIDs[t.sourceIndex], id) + 1
}

func (r *Runtime) boundVariable(t *target, binding int) *Variable {
	if r.native && binding > 0 && binding <= len(t.nativeVariables) {
		return t.nativeVariables[binding-1]
	}
	return nil
}

func (r *Runtime) bindNativeVariables(t *target) {
	ids := nativeVariableIDs[t.sourceIndex]
	if len(t.nativeVariables) != len(ids) {
		t.nativeVariables = make([]*Variable, len(ids))
	}
	for i, id := range ids {
		v := t.variables[id]
		if v == nil && r.stage != nil {
			v = r.stage.variables[id]
		}
		t.nativeVariables[i] = v
	}
}

func (r *Runtime) refreshNativeVariables() {
	r.invalidateTargetQueries()
	if r.native {
		for _, t := range r.targets {
			r.bindNativeVariables(t)
		}
	}
}

func nativeKey(p *project.Project) string {
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	for _, target := range p.Targets {
		if err := encoder.Encode(target.Blocks); err != nil {
			return ""
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// A compiled call chain shares data until it returns to the VM or an observer.
type nativeRegisters struct {
	variables []nativeVariable
	lists     []nativeList
}

func (registers *nativeRegisters) sync(f *nativeFrame) {
	for i := range registers.variables {
		slot := &registers.variables[i]
		f.flushVariable(slot)
		*slot = nativeVariable{binding: slot.binding}
	}
	for i := range registers.lists {
		slot := &registers.lists[i]
		f.flushList(slot, slot.id)
		*slot = nativeList{id: slot.id}
	}
}

type nativeFrame struct {
	ctx   context.Context
	r     *Runtime
	t     *target
	steps *int
	err   error
	empty List
}

type nativeVariable struct {
	value     *Variable
	binding   int // One-based index; zero is used by interpreted calculations.
	dirty     bool
	numeric   bool
	converted bool
	number    float64
}

type nativeList struct {
	id    string
	owner *target
	value List
	dirty bool
}

func (f *nativeFrame) step() bool {
	*f.steps++
	if f.err == nil {
		f.err = f.ctx.Err()
	}
	return f.err == nil
}

// Continue compiled callees on the same Scratch stack; unsupported blocks return to the VM.
func (f *nativeFrame) call(th *thread, b *project.Block) bool {
	base := len(th.stack)
	_, f.err = f.r.call(f.ctx, th, b, nil, false)
	if f.err != nil {
		return false
	}
	if len(th.stack) == base {
		return true
	}
	for !th.done && !f.r.stopped && !th.nativeYield && th.canStep() {
		if th.pc == "" {
			i := len(th.stack) - 1
			if i < base || !th.stack[i].call {
				return false
			}
			if !f.step() {
				return false
			}
			c := th.stack[i]
			th.releaseParams(th.params)
			th.params, th.warp, th.procedure = c.params, c.warp, c.procedure
			th.regions = c.regions
			th.pc, th.cursor = c.next, c.nextLink
			th.stack[i].params = nil
			th.stack = th.stack[:i]
			th.result = ""
			if i == base {
				return true
			}
			continue
		}
		region := th.regions[th.pc]
		if region == nil || len(th.stack)+region.depth >= 1024 ||
			(region.work != 0 && th.sliceLimit-*th.sliceSteps < region.work) {
			return false
		}
		if err := region.run(f.ctx, f.r, th); err != nil {
			f.err = err
			return false
		}
	}
	return false
}

/*
Bind lazily: an untaken branch may refer to nonexistent data. Bindings belong
to this call, so clones and subsequent extension mutations remain visible.
*/
func (f *nativeFrame) variable(slot *nativeVariable, id string) any {
	f.bindVariable(slot, id)
	if f.err != nil {
		return nil
	}
	if slot.numeric {
		// Reuse one immutable box for raw reads and the eventual frame flush.
		f.flushVariable(slot)
	}
	return slot.value.Value
}

// Copy numeric slots without materializing their intermediate values.
func (f *nativeFrame) variableValue(slot *nativeVariable, id string) calculationValue {
	f.bindVariable(slot, id)
	if f.err != nil {
		return calculationValue{}
	}
	if slot.numeric {
		return calculationValue{number: slot.number, numeric: true}
	}
	return calculationValue{value: slot.value.Value}
}

func (f *nativeFrame) setValue(slot *nativeVariable, id string, value calculationValue) {
	if value.numeric {
		f.setNumber(slot, id, value.number)
	} else {
		f.set(slot, id, value.value)
	}
}

// Comparisons retain the original text for Scratch's nonnumeric fallback.
type nativeComparison struct {
	raw     any
	number  float64
	numeric bool
}

func (f *nativeFrame) comparison(slot *nativeVariable, id string) nativeComparison {
	if slot.numeric && f.err == nil {
		return nativeComparison{number: slot.number, numeric: true}
	}
	return f.readComparison(slot, id)
}

func (f *nativeFrame) readComparison(slot *nativeVariable, id string) nativeComparison {
	f.bindVariable(slot, id)
	if f.err != nil {
		return nativeComparison{}
	}
	switch value := slot.value.Value.(type) {
	case float64:
		slot.number, slot.numeric = value, true
		return nativeComparison{number: value, numeric: true}
	case nil:
		return nativeComparison{}
	}
	if !slot.converted {
		slot.number = numeric(slot.value.Value)
		slot.converted = true
	}
	n := slot.number
	if n == 0 {
		if s, ok := slot.value.Value.(string); ok && trim(s) == "" {
			n = math.NaN()
		}
	}
	return nativeComparison{raw: slot.value.Value, number: n, numeric: true}
}

func (v nativeComparison) comparisonNumber() float64 {
	if v.numeric {
		return v.number
	}
	n := numeric(v.raw)
	if n == 0 {
		if s, ok := v.raw.(string); ok && trim(s) == "" {
			return math.NaN()
		}
	}
	return n
}

func (v nativeComparison) text() string {
	if v.numeric && v.raw == nil {
		return text(v.number)
	}
	return text(v.raw)
}

func nativeCompare(a, b nativeComparison) float64 {
	if a.numeric && b.numeric && !math.IsNaN(a.number) && !math.IsNaN(b.number) {
		if a.number == b.number {
			return 0
		}
		return a.number - b.number
	}
	return nativeCompareCoerced(a, b)
}

func nativeCompareCoerced(a, b nativeComparison) float64 {
	an, bn := a.number, b.number
	if !a.numeric {
		an = a.comparisonNumber()
	}
	if !b.numeric {
		bn = b.comparisonNumber()
	}
	if !math.IsNaN(an) && !math.IsNaN(bn) {
		if an == bn {
			return 0
		}
		return an - bn
	}
	return compareText(a.text(), b.text())
}

func (f *nativeFrame) bindVariable(slot *nativeVariable, id string) {
	if f.err != nil {
		return
	}
	if slot.value == nil {
		if slot.binding > 0 && slot.binding <= len(f.t.nativeVariables) {
			slot.value = f.t.nativeVariables[slot.binding-1]
			if slot.value == nil {
				f.err = fmt.Errorf("unknown variable %q", id)
			}
		} else {
			_, slot.value, f.err = f.r.variable(f.t, id)
		}
	}
}

func (f *nativeFrame) number(slot *nativeVariable, id string) float64 {
	if slot.numeric && slot.number == slot.number && f.err == nil {
		return slot.number
	}
	return f.readNumber(slot, id)
}

func (f *nativeFrame) readNumber(slot *nativeVariable, id string) float64 {
	f.bindVariable(slot, id)
	if f.err != nil {
		return 0
	}
	if !slot.numeric && !slot.converted {
		slot.number = numeric(slot.value.Value)
		slot.converted = true
		_, slot.numeric = slot.value.Value.(float64)
	}
	return nativeNumber(slot.number)
}

func (f *nativeFrame) set(slot *nativeVariable, id string, value any) {
	f.bindVariable(slot, id)
	if f.err == nil {
		slot.value.set(value)
		slot.dirty = true
		if n, ok := value.(float64); ok {
			slot.number, slot.numeric, slot.converted = n, true, true
		} else {
			slot.numeric, slot.converted = false, false
		}
	}
}

func (f *nativeFrame) setNumber(slot *nativeVariable, id string, value float64) {
	f.bindVariable(slot, id)
	if f.err == nil {
		slot.number, slot.numeric, slot.dirty = value, true, true
	}
}

func (f *nativeFrame) bindList(slot *nativeList, id string) *nativeList {
	f.list(slot, id)
	return slot
}

func (f *nativeFrame) list(slot *nativeList, id string) *List {
	if f.err != nil {
		return &f.empty
	}
	if slot.owner == nil {
		slot.id = id
		slot.owner, slot.value, f.err = f.r.list(f.t, id)
	}
	return &slot.value
}

func (f *nativeFrame) findListItem(slot *nativeList, value any) float64 {
	if f.err != nil {
		return 0
	}
	previousIndex := slot.value.textIndex
	index, err := slot.value.find(f.ctx, value)
	f.err = err
	if slot.value.textIndex != previousIndex {
		slot.dirty = true
	}
	return float64(index)
}

func (f *nativeFrame) flushVariable(slot *nativeVariable) {
	if slot.value != nil && slot.dirty {
		if slot.numeric {
			previous, numeric := slot.value.Value.(float64)
			if !numeric || math.Float64bits(previous) != math.Float64bits(slot.number) {
				slot.value.set(numberValue(slot.number))
			}
		}
		slot.dirty = false
	}
}

func (f *nativeFrame) flushList(slot *nativeList, id string) {
	if slot.owner != nil && slot.dirty {
		slot.owner.state.Lists[id] = slot.value
		slot.dirty = false
	}
}

func nativeItem(list *List, index any) any {
	i := listIndex(index, len(list.Items))
	if i < 0 {
		return ""
	}
	return list.Items[i]
}

func nativeListIndex(index float64, size int) int {
	index = math.Floor(index)
	if !(index >= 1 && index <= float64(size)) {
		return -1
	}
	return int(index) - 1
}

func nativeNumericItem(list *List, index float64) any {
	i := nativeListIndex(index, len(list.Items))
	if i < 0 {
		return ""
	}
	return list.Items[i]
}

func nativeDelete(list *List, index any) {
	if index == "all" {
		list.clear()
	} else if i := listIndex(index, len(list.Items)); i >= 0 {
		list.remove(i)
	}
}

/*
A function call makes even literal operands runtime floats in generated Go;
division by zero and overflow must follow Scratch rather than Go constants.
*/
func nativeLiteral(value float64) float64 {
	return value
}

func nativeLetter(index float64, value any) string {
	return utf16Letter(text(value), index-1)
}

func nativeNumber(value float64) float64 {
	if math.IsNaN(value) {
		return 0
	}
	return value
}

func nativeModulo(a, b float64) float64 {
	m := math.Mod(a, b)
	if m/b < 0 {
		m += b
	}
	return m
}

func nativeRound(a float64) float64 {
	n := math.Floor(a)
	if a-n >= 0.5 {
		n++
	}
	if n == 0 {
		return math.Copysign(0, a)
	}
	return n
}

func nativeContains(a, b any) bool {
	return strings.Contains(strings.ToLower(text(a)), strings.ToLower(text(b)))
}
