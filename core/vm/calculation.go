package vm

import (
	"context"

	"gowarp/core/project"
)

const (
	calculationVariableLimit = 16
	calculationListLimit     = 8
	calculationArgumentLimit = 16
	calculationStackLimit    = 32
	calculationCodeLimit     = 256
)

type calculationOpcode uint8

const (
	calcBegin calculationOpcode = iota
	calcLiteral
	calcVariable
	calcArgument
	calcBindList
	calcListItem
	calcListLength
	calcTimer
	calcAdd
	calcSubtract
	calcMultiply
	calcDivide
	calcMod
	calcRound
	calcMath
	calcSet
	calcChange
	calcAppend
	calcReplace
)

type calculationInstruction struct {
	value any
	op    calculationOpcode
	slot  uint8
}

type calculation struct {
	code                        []calculationInstruction
	variables, lists, arguments []string
}

type calculationBuilder struct {
	calculation
	stack int
}

func calculationStatement(opcode string) bool {
	switch opcode {
	case "data_setvariableto", "data_changevariableby", "data_addtolist", "data_replaceitemoflist":
		return true
	}
	return false
}

func (r *Runtime) prepareCalculation(t *target, first *project.Block) *calculation {
	if program, found := r.calculations[first]; found {
		return program
	}
	if r.calculations == nil {
		r.calculations = make(map[*project.Block]*calculation)
	}
	r.calculations[first] = nil
	builder := calculationBuilder{}
	var blocks []*project.Block
	var offsets []int
	for block := first; block != nil && len(blocks) < 32 && calculationStatement(block.Opcode); block = t.source.Blocks[block.Next] {
		if block != first {
			if _, prepared := r.calculations[block]; prepared {
				break
			}
		}
		cyclic := false
		for _, previous := range blocks {
			if previous == block {
				cyclic = true
				break
			}
		}
		if cyclic {
			break
		}
		offset := len(builder.code)
		if !builder.statement(r, t, block) {
			builder.code = builder.code[:offset]
			break
		}
		blocks = append(blocks, block)
		offsets = append(offsets, offset)
	}
	if len(builder.code) < 12 {
		return nil
	}
	/*
		Interior entry points share instructions and metadata; a resumed script
		must not allocate another overlapping copy of the same command group.
	*/
	for i, block := range blocks {
		program := builder.calculation
		program.code = program.code[offsets[i]:]
		r.calculations[block] = &program
	}
	return r.calculations[first]
}

func (b *calculationBuilder) slot(names *[]string, name string, limit int) (uint8, bool) {
	for i, existing := range *names {
		if existing == name {
			return uint8(i), true
		}
	}
	if len(*names) == limit {
		return 0, false
	}
	*names = append(*names, name)
	return uint8(len(*names) - 1), true
}

func (b *calculationBuilder) emit(op calculationOpcode, slot uint8, value any, delta int) bool {
	if len(b.code) == calculationCodeLimit || b.stack+delta > calculationStackLimit || b.stack+delta < 0 {
		return false
	}
	b.code = append(b.code, calculationInstruction{op: op, slot: slot, value: value})
	b.stack += delta
	return true
}

func (b *calculationBuilder) expression(n *numericExpression) bool {
	if n == nil || n.conditional {
		return false
	}
	switch n.op {
	case exprLiteral:
		value := n.literal
		if value == nil {
			value = float64(0)
		}
		return b.emit(calcLiteral, 0, value, 1)
	case exprVariable:
		slot, ok := b.slot(&b.variables, n.name, calculationVariableLimit)
		return ok && b.emit(calcVariable, slot, nil, 1)
	case exprArgument:
		slot, ok := b.slot(&b.arguments, n.name, calculationArgumentLimit)
		return ok && b.emit(calcArgument, slot, nil, 1)
	case exprListItem, exprListLength:
		slot, ok := b.slot(&b.lists, n.name, calculationListLimit)
		if !ok || !b.emit(calcBindList, slot, nil, 0) {
			return false
		}
		if n.op == exprListLength {
			return b.emit(calcListLength, slot, nil, 1)
		}
		return b.expression(n.left) && b.emit(calcListItem, slot, nil, 0)
	case exprTimer:
		return b.emit(calcTimer, 0, nil, 1)
	case exprAdd, exprSubtract, exprMultiply, exprDivide, exprMod:
		if !b.expression(n.left) || !b.expression(n.right) {
			return false
		}
		op := calcAdd + calculationOpcode(n.op-exprAdd)
		return b.emit(op, 0, nil, -1)
	case exprRound, exprMath:
		if !b.expression(n.left) {
			return false
		}
		if n.op == exprRound {
			return b.emit(calcRound, 0, nil, 0)
		}
		return b.emit(calcMath, 0, n.name, 0)
	}
	return false
}

func (b *calculationBuilder) statement(r *Runtime, t *target, block *project.Block) bool {
	if !b.emit(calcBegin, 0, block.Next, 0) {
		return false
	}
	if block.Opcode == "data_setvariableto" || block.Opcode == "data_changevariableby" {
		write := r.prepareVariableWrite(t, block)
		if write == nil || !b.expression(write.value) {
			return false
		}
		slot, ok := b.slot(&b.variables, write.id, calculationVariableLimit)
		op := calcSet
		if block.Opcode == "data_changevariableby" {
			op = calcChange
		}
		return ok && b.emit(op, slot, nil, -1)
	}
	write := r.prepareListWrite(t, block)
	if write == nil {
		return false
	}
	slot, ok := b.slot(&b.lists, write.id, calculationListLimit)
	if !ok || !b.emit(calcBindList, slot, nil, 0) {
		return false
	}
	if write.index != nil && !b.expression(write.index) {
		return false
	}
	if !b.expression(write.item) {
		return false
	}
	if block.Opcode == "data_addtolist" {
		return b.emit(calcAppend, slot, nil, -1)
	}
	return b.emit(calcReplace, slot, nil, -2)
}

type calculationValue struct {
	value   any
	number  float64
	numeric bool
}

func (v calculationValue) num() float64 {
	if v.numeric {
		return nativeNumber(v.number)
	}
	return number(v.value)
}

func (v calculationValue) boxed() any {
	if v.numeric {
		return numberValue(v.number)
	}
	return v.value
}

func (v calculationValue) index(size int) int {
	if v.numeric {
		return nativeListIndex(v.number, size)
	}
	return listIndex(v.value, size)
}

type calculationFrame struct {
	nativeFrame
	variables [calculationVariableLimit]nativeVariable
	values    [calculationVariableLimit]calculationValue
	lists     [calculationListLimit]nativeList
	stack     [calculationStackLimit]calculationValue
}

func (f *calculationFrame) bindVariable(slot uint8, id string) {
	if f.variables[slot].value == nil {
		value := f.variable(&f.variables[slot], id)
		f.values[slot] = calculationValue{value: value}
	}
}

func (p *calculation) run(ctx context.Context, r *Runtime, th *thread) error {
	f := calculationFrame{ctx: ctx, r: r, t: th.target}
	var arguments [calculationArgumentLimit]calculationValue
	for i, name := range p.arguments {
		arguments[i] = nativeArgument(th, name)
	}
	sp, statements := 0, 0
	for _, instruction := range p.code {
		slot := instruction.slot
		switch instruction.op {
		case calcBegin:
			if statements > 0 {
				if !th.canStep() {
					goto finished
				}
				*th.sliceSteps++
			}
			statements++
			if f.err = ctx.Err(); f.err != nil {
				goto finished
			}
			th.pc = instruction.value.(string)
		case calcLiteral:
			f.stack[sp] = calculationValue{value: instruction.value}
			sp++
		case calcVariable:
			f.bindVariable(slot, p.variables[slot])
			f.stack[sp] = f.values[slot]
			sp++
		case calcArgument:
			f.stack[sp] = arguments[slot]
			sp++
		case calcBindList:
			f.list(&f.lists[slot], p.lists[slot])
		case calcListItem:
			items := f.lists[slot].value.Items
			index := f.stack[sp-1].index(len(items))
			var value any = ""
			if index >= 0 {
				value = items[index]
			}
			f.stack[sp-1] = calculationValue{value: value}
		case calcListLength:
			f.stack[sp] = calculationValue{number: float64(len(f.lists[slot].value.Items)), numeric: true}
			sp++
		case calcTimer:
			f.stack[sp] = calculationValue{number: r.elapsed - r.timerSeconds, numeric: true}
			sp++
		case calcAdd, calcSubtract, calcMultiply, calcDivide, calcMod:
			a, b := f.stack[sp-2].num(), f.stack[sp-1].num()
			var value float64
			switch instruction.op {
			case calcAdd:
				value = a + b
			case calcSubtract:
				value = a - b
			case calcMultiply:
				value = a * b
			case calcDivide:
				value = a / b
			case calcMod:
				value = nativeModulo(a, b)
			}
			sp--
			f.stack[sp-1] = calculationValue{number: value, numeric: true}
		case calcRound, calcMath:
			value := f.stack[sp-1].num()
			if instruction.op == calcRound {
				value = nativeRound(value)
			} else {
				value = r.mathOperator(instruction.value.(string), value)
			}
			f.stack[sp-1] = calculationValue{number: value, numeric: true}
		case calcSet, calcChange:
			f.bindVariable(slot, p.variables[slot])
			if f.err != nil {
				goto finished
			}
			sp--
			value := f.stack[sp]
			if instruction.op == calcChange {
				value = calculationValue{number: f.values[slot].num() + value.num(), numeric: true}
			}
			f.values[slot], f.variables[slot].dirty = value, true
		case calcAppend:
			sp--
			list := &f.lists[slot]
			if len(list.value.Items) < listLimit {
				list.value.append(f.stack[sp].boxed())
				list.dirty = true
			}
		case calcReplace:
			sp -= 2
			list := &f.lists[slot]
			if index := f.stack[sp].index(len(list.value.Items)); index >= 0 {
				list.value.replace(index, f.stack[sp+1].boxed())
				list.dirty = true
			}
		}
		if f.err != nil {
			break
		}
	}
finished:
	// A later error or quota boundary must expose every completed write.
	for i := range p.variables {
		if f.variables[i].dirty {
			f.variables[i].value.Value = f.values[i].boxed()
			f.flushVariable(&f.variables[i])
		}
	}
	for i, id := range p.lists {
		f.flushList(&f.lists[i], id)
	}
	return f.err
}
