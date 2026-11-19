package vm

import (
	"fmt"
	"sort"
	"strings"
)

// Compile only synchronous argument expressions; the call itself may still yield.
func (g *nativeGenerator) compileCallInputs(t *target) {
	ids := make([]string, 0)
	for id, b := range t.source.Blocks {
		if b.Opcode == "procedures_call" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	planner := &Runtime{}
	for _, id := range ids {
		b := t.source.Blocks[id]
		code, err := mutationText(b, "proccode")
		p := t.procedures[code]
		if err != nil || p == nil || !p.warp || g.known[p] != nil {
			continue
		}
		prepared, err := planner.procedureArguments(t, b, p.ids)
		if err != nil || !prepared.synchronous || len(prepared.arguments) > len(p.names) {
			continue
		}
		e := &nativeEmitter{g: g, t: t, p: &procedure{}, path: make(map[string]bool), callInputs: true}
		var body strings.Builder
		expressions := make(map[string]int)
		supported := true
		for i, name := range p.names {
			value, ok := nativeConstant(p.defaults[i])
			if i < len(prepared.arguments) && prepared.arguments[i].present {
				if !nativeInputDeterministic(prepared.arguments[i].plan) {
					supported = false
					break
				}
				value, ok = e.input(b, prepared.arguments[i].id, 0)
			}
			if !ok {
				supported = false
				break
			}
			var expression string
			if value.argument != nil {
				expression = fmt.Sprintf("a%d", value.argument.index)
			} else if value.numberResult && value.numeric != "" {
				expression = "calculationValue{number: " + value.numeric + ", numeric: true}"
			} else {
				expression = "calculationValue{value: " + value.raw() + "}"
			}
			index, found := expressions[expression]
			if found {
				expression = ""
			} else {
				index = i
				expressions[expression] = index
			}
			body.WriteString(nativeCode("call-input", map[string]any{
				"Name": name, "Value": expression, "Index": index,
			}))
		}
		// Simple literal/variable arguments already have a cheap interpreted path.
		if !supported || e.work < 4 || e.work > nativeWorkLimit {
			continue
		}
		name := fmt.Sprintf("nativeInputs%d", g.next)
		g.next++
		g.functions = append(g.functions, &nativeFunction{
			name: name, source: t.sourceIndex, region: id, inputs: true,
			body: e.functionBody(name, body.String()), variables: e.variables, lists: e.lists,
		})
	}
}

func nativeInputDeterministic(n *numericExpression) bool {
	if n == nil {
		return true
	}
	// Named or dynamic list indexes may consume randomness before a later error.
	if n.op == exprListItem && !n.left.numeric {
		return false
	}
	return nativeInputDeterministic(n.left) && nativeInputDeterministic(n.right)
}
