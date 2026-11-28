package vm

import (
	_ "embed"
	"fmt"
	"go/format"
	"maps"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/template"

	"gowarp/core/project"
)

//go:embed templates/native.go.tmpl
var nativeTemplateSource string

var nativeTemplates = sync.OnceValue(func() *template.Template {
	return template.Must(template.New("native").Option("missingkey=error").Parse(nativeTemplateSource))
})

func nativeCode(name string, data map[string]any) string {
	var out strings.Builder
	if err := nativeTemplates().ExecuteTemplate(&out, name, data); err != nil {
		panic(err)
	}
	return out.String()
}

const nativeWorkLimit = 8192

type nativeFunction struct {
	name, code, body string
	region           string
	inputs           bool
	script           bool
	resumable        bool
	source           int
	procedure        *procedure
	parameters       []nativeParameter
	variables        []string
	lists            []string
	effects          nativeEffects
	work, depth      int
}

type nativeGenerator struct {
	functions []*nativeFunction
	variables map[int][]string
	next      int
	known     map[*procedure]*nativeFunction
	active    map[*procedure]bool
}

type nativeEmitter struct {
	g           *nativeGenerator
	t           *target
	p           *procedure
	variables   []string
	lists       []string
	path        map[string]bool
	numbers     map[string]bool // Slots initialized as numbers on every incoming path.
	work, depth int
	stop        string
	region      bool
	callInputs  bool
	resumable   bool
	flowLabels  map[string]string
	loopBody    string
	arguments   []*nativeArgumentUse
	inlineArgs  []nativeExpression
	inlineDepth int
	effects     nativeEffects
	barriers    []nativeEffects
}

type nativeExpression struct {
	value, numeric, boolean string
	variableValue           string
	comparison              string
	textComparison          bool
	namedIndex              bool
	numberResult            bool
	argument                *nativeArgumentUse
}

type nativeParameter struct {
	Name                string
	Index               int
	Numeric, NumberUsed bool
}

type nativeArgumentUse struct {
	index        int
	numeric, raw bool
}

type nativeEffects struct {
	variables map[string]bool // Membership means read; true means written.
	external  bool
}

func (effects *nativeEffects) access(id string, write bool) {
	if effects.variables == nil {
		effects.variables = make(map[string]bool)
	}
	effects.variables[id] = effects.variables[id] || write
}

func (x nativeExpression) raw() string {
	if x.argument != nil {
		x.argument.raw = true
	}
	return x.value
}

func (x nativeExpression) num() string {
	if x.argument != nil {
		x.argument.numeric = true
		return fmt.Sprintf("n%d", x.argument.index)
	}
	if x.numeric != "" {
		return "nativeNumber(" + x.numeric + ")"
	}
	return "number(" + x.value + ")"
}

func (x nativeExpression) comparable() string {
	if x.comparison != "" {
		return x.comparison
	}
	if x.numberResult {
		return "nativeComparison{number:" + x.numeric + ",numeric:true}"
	}
	return "nativeComparison{raw:" + x.raw() + "}"
}

func (x nativeExpression) truth() string {
	if x.boolean != "" {
		return x.boolean
	}
	return "truth(" + x.raw() + ")"
}

func nativeNumeric(raw string) nativeExpression {
	return nativeExpression{value: "numberValue(" + raw + ")", numeric: raw, numberResult: true}
}

func nativeBoolean(raw string) nativeExpression {
	return nativeExpression{value: raw, boolean: raw}
}

func nativeConstant(value any) (nativeExpression, bool) {
	var code, numericCode, comparison string
	namedIndex := false
	textComparison := false
	switch value := value.(type) {
	case nil:
		code = "nil"
	case string:
		code = strconv.Quote(value)
		namedIndex = value == "last" || value == "random" || value == "any"
		// Preserve the original text; precompute only its numeric representation.
		if n := number(value); finite(n) {
			numericCode = "nativeLiteral(" + strconv.FormatFloat(n, 'g', -1, 64) + ")"
			if n == 0 && math.Signbit(n) {
				numericCode = "-nativeLiteral(0)"
			}
		}
		n := numeric(value)
		textComparison = math.IsNaN(n) || trim(value) == ""
		if finite(n) && !textComparison {
			comparison = "nativeComparison{raw:" + code + ",number:" + numericCode + ",numeric:true}"
		}
	case bool:
		return nativeBoolean(strconv.FormatBool(value)), true
	case float64:
		if !finite(value) {
			return nativeExpression{}, false
		}
		code = "nativeLiteral(" + strconv.FormatFloat(value, 'g', -1, 64) + ")"
		if value == 0 && math.Signbit(value) {
			code = "number(\"-0\")"
		}
		numericCode = code
	default:
		return nativeExpression{}, false
	}
	_, numberResult := value.(float64)
	return nativeExpression{
		value:          code,
		numeric:        numericCode,
		comparison:     comparison,
		textComparison: textComparison,
		namedIndex:     namedIndex,
		numberResult:   numberResult,
	}, true
}

func GenerateNative(p *project.Project) ([]byte, error) {
	g := &nativeGenerator{
		known:  make(map[*procedure]*nativeFunction),
		active: make(map[*procedure]bool),
	}
	for source, src := range p.Targets {
		t := &target{source: src, sourceIndex: source}
		if prepareProcedures(t) != nil {
			continue
		}
		codes := make([]string, 0, len(t.procedures))
		for code := range t.procedures {
			codes = append(codes, code)
		}
		sort.Strings(codes)
		for _, code := range codes {
			if !t.procedures[code].warp {
				g.compileFlow(t, code)
			} else if g.compile(t, code) == nil && !g.compileFlow(t, code) {
				g.compileRegions(t, code)
			}
		}
		var hats []string
		for id, b := range src.Blocks {
			if b != nil && b.TopLevel && b.Opcode != "procedures_definition" && b.Next != "" {
				hats = append(hats, id)
			}
		}
		sort.Strings(hats)
		for _, id := range hats {
			g.compileScript(t, &procedure{body: src.Blocks[id].Next}, id, true)
		}
		g.compileCallInputs(t)
	}
	if len(g.functions) == 0 {
		return nil, nil
	}
	var registrations strings.Builder
	bodies := make([]string, 0, len(g.functions))
	for _, function := range g.functions {
		registrations.WriteString(nativeCode("registration", map[string]any{
			"Name":       function.name,
			"Code":       function.code,
			"Source":     function.source,
			"Region":     function.region,
			"Depth":      function.depth,
			"Work":       function.work,
			"Parameters": function.parameters,
			"Inputs":     function.inputs,
			"Script":     function.script,
			"Resumable":  function.resumable,
			"Variables":  function.variables,
			"Lists":      function.lists,
		}))
		bodies = append(bodies, function.body)
	}
	code := nativeCode("module", map[string]any{
		"Variables":     g.variables,
		"ProjectKey":    nativeKey(p),
		"Registrations": registrations.String(),
		"Bodies":        bodies,
	})
	return format.Source([]byte(code))
}

func (g *nativeGenerator) compile(t *target, code string) *nativeFunction {
	p := t.procedures[code]
	if p == nil || g.active[p] {
		return nil
	}
	if function, found := g.known[p]; found {
		return function
	}
	g.known[p] = nil
	g.active[p] = true
	defer delete(g.active, p)
	function := &nativeFunction{
		name:      fmt.Sprintf("nativeCalc%d", g.next),
		code:      code,
		source:    t.sourceIndex,
		procedure: p,
	}
	g.next++
	e := &nativeEmitter{
		g:     g,
		t:     t,
		p:     p,
		path:  make(map[string]bool),
		depth: 1,
	}
	body, ok := e.statements(p.body, 1)
	if !ok || e.work > nativeWorkLimit || e.depth > 32 {
		return nil
	}
	function.parameters, function.effects = e.parameters(), e.effects
	function.body = e.functionBody(function.name, body)
	function.variables, function.lists = e.variables, e.lists
	function.work, function.depth = e.work+1, e.depth
	g.functions = append(g.functions, function)
	g.known[p] = function
	return function
}

func (e *nativeEmitter) functionBody(name, body string) string {
	if e.g.variables == nil {
		e.g.variables = make(map[int][]string)
	}
	ids := e.g.variables[e.t.sourceIndex]
	bindings := make([]int, len(e.variables))
	for i, id := range e.variables {
		index := 0
		for index < len(ids) && ids[index] != id {
			index++
		}
		if index == len(ids) {
			ids = append(ids, id)
		}
		bindings[i] = index + 1
	}
	e.g.variables[e.t.sourceIndex] = ids
	graphicsSync := nativeCode("graphics-sync", nil)
	body = strings.ReplaceAll(body, "/*nativeFlush*/", graphicsSync)
	body = strings.ReplaceAll(body, "/*nativeFlowFlush*/", nativeCode("flush", nil))
	for i, effects := range e.barriers {
		body = strings.ReplaceAll(body, fmt.Sprintf("/*nativeCall%d*/", i), e.sync(effects))
	}
	return nativeCode("function", map[string]any{
		"Name":       name,
		"Parameters": e.parameters(),
		"Region":     e.region,
		"Inputs":     e.callInputs,
		"External":   e.effects.external,
		"Variables":  e.variables,
		"Bindings":   bindings,
		"Lists":      e.lists,
		"LoopBody":   e.loopBody,
		"Work":       e.work,
		"Body":       body,
	})
}

// Compile calculation regions while leaving loops and yielding to the interpreter.
func (g *nativeGenerator) compileRegions(t *target, code string) {
	p := t.procedures[code]
	loopBodies := make(map[string]bool)
	for _, block := range t.source.Blocks {
		if block != nil && block.Opcode == "control_repeat" {
			if body, err := substack(block, "SUBSTACK"); err == nil && body != "" {
				loopBodies[body] = true
			}
		}
	}
	seen, covered := make(map[string]bool), make(map[string]bool)
	pending := []string{p.body}
	for len(pending) > 0 {
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		b := t.source.Blocks[id]
		if b == nil || seen[id] {
			continue
		}
		seen[id] = true
		pending = append(pending, b.Next)
		for _, name := range []string{"SUBSTACK2", "SUBSTACK"} {
			if child, err := substack(b, name); err == nil && child != "" {
				pending = append(pending, child)
			}
		}
		var ids []string
		path := make(map[string]bool)
		for next := id; next != "" && !covered[next] && len(ids) < 32; {
			block := t.source.Blocks[next]
			if block == nil || !nativeRegionStatement(block.Opcode) {
				break
			}
			if path[next] {
				ids = nil
				break
			}
			path[next] = true
			ids = append(ids, next)
			next = block.Next
		}
		for len(ids) > 0 {
			stop := t.source.Blocks[ids[len(ids)-1]].Next
			if path[stop] {
				break
			}
			e := &nativeEmitter{
				g:      g,
				t:      t,
				p:      p,
				path:   make(map[string]bool),
				depth:  1,
				stop:   stop,
				region: true,
			}
			if loopBodies[id] && stop == "" {
				e.loopBody = id
			}
			body, ok := e.statements(id, 1)
			if ok && e.work < 7 && e.loopBody == "" {
				break
			}
			if ok && e.work <= nativeWorkLimit {
				name := fmt.Sprintf("nativeCalc%d", g.next)
				g.next++
				work := 0
				if e.loopBody != "" {
					work = e.work
				}
				g.functions = append(g.functions, &nativeFunction{
					name:       name,
					code:       code,
					source:     t.sourceIndex,
					procedure:  p,
					parameters: e.parameters(),
					variables:  e.variables, lists: e.lists,
					effects: e.effects,
					region:  id,
					body:    e.functionBody(name, body),
					depth:   e.depth,
					work:    work,
				})
				for _, compiled := range ids {
					covered[compiled] = true
				}
				break
			}
			ids = ids[:len(ids)-1]
		}
	}
}

func nativeRegionStatement(opcode string) bool {
	switch opcode {
	case "data_setvariableto", "data_changevariableby",
		"data_addtolist", "data_deletealloflist", "data_deleteoflist", "data_replaceitemoflist",
		"control_if", "control_if_else", "procedures_call",
		"motion_gotoxy", "pen_penUp", "pen_penDown",
		"motion_setx", "motion_sety", "motion_changexby", "motion_changeyby",
		"pen_setPenColorParamTo", "pen_changePenColorParamBy",
		"pen_setPenSizeTo", "pen_changePenSizeBy", "pen_setPenColorToColor", "pen_stamp",
		"motion_movesteps", "motion_turnleft", "motion_turnright", "motion_pointindirection",
		"looks_setsizeto", "looks_switchcostumeto", "looks_seteffectto", "looks_cleargraphiceffects":
		return true
	default:
		return false
	}
}

func (e *nativeEmitter) slot(id string, list bool) string {
	if !list {
		e.effects.access(id, false)
	}
	ids, prefix := &e.variables, "v"
	if list {
		ids, prefix = &e.lists, "l"
	}
	for i, existing := range *ids {
		if existing == id {
			return fmt.Sprintf("%s%d", prefix, i)
		}
	}
	index := len(*ids)
	*ids = append(*ids, id)
	return fmt.Sprintf("%s%d", prefix, index)
}

func (e *nativeEmitter) variable(id string) nativeExpression {
	slot := e.slot(id, false)
	value := fmt.Sprintf("f.variable(%s,%q)", slot, id)
	if e.numbers[id] {
		return nativeExpression{value: value, numeric: slot + ".number", numberResult: true}
	}
	return nativeExpression{
		value:         value,
		variableValue: fmt.Sprintf("f.variableValue(%s,%q)", slot, id),
		numeric:       fmt.Sprintf("f.number(%s,%q)", slot, id),
		comparison:    fmt.Sprintf("f.comparison(%s,%q)", slot, id),
		namedIndex:    true,
	}
}

func (e *nativeEmitter) number(id string) string {
	slot := e.slot(id, false)
	if e.numbers[id] {
		return "nativeNumber(" + slot + ".number)"
	}
	return fmt.Sprintf("f.number(%s,%q)", slot, id)
}

func (e *nativeEmitter) list(id string) string {
	return fmt.Sprintf("f.list(%s,%q)", e.slot(id, true), id)
}

func (e *nativeEmitter) parameters() []nativeParameter {
	parameters := make([]nativeParameter, len(e.p.names))
	for i, name := range e.p.names {
		parameters[i].Name = name
		parameters[i].Index = i
		for j := i + 1; j < len(e.p.names); j++ {
			if e.p.names[j] == name {
				parameters[i].Index = j
			}
		}
		if i < len(e.arguments) && e.arguments[i] != nil {
			use := e.arguments[i]
			parameters[i].Numeric = use.numeric && !use.raw
			parameters[i].NumberUsed = use.numeric
		}
	}
	return parameters
}

func (e *nativeEmitter) callBarrier(effects nativeEffects) string {
	if effects.external {
		clear(e.numbers)
	} else {
		for id, write := range effects.variables {
			if write {
				delete(e.numbers, id)
			}
		}
	}
	e.effects.external = e.effects.external || effects.external
	for id, write := range effects.variables {
		e.effects.access(id, write)
	}
	marker := fmt.Sprintf("/*nativeCall%d*/", len(e.barriers))
	e.barriers = append(e.barriers, effects)
	return marker
}

func (e *nativeEmitter) sync(effects nativeEffects) string {
	if effects.external {
		return nativeCode("graphics-sync", nil)
	}
	return ""
}

func (e *nativeEmitter) input(b *project.Block, name string, depth int) (nativeExpression, bool) {
	if e.work >= nativeWorkLimit || depth > 128 {
		return nativeExpression{}, false
	}
	in, present := b.Inputs[name]
	if !present {
		return nativeConstant(float64(0))
	}
	if len(in) < 2 {
		return nativeExpression{}, false
	}
	value := in[1]
	if value == nil && len(in) > 2 {
		value = in[2]
	}
	if constant, ok := constantInput(value); ok {
		return nativeConstant(constant)
	}
	if literal, ok := value.([]any); ok && len(literal) >= 3 && literal[0] == float64(12) {
		id, ok := literal[2].(string)
		return e.variable(id), ok
	}
	id, ok := value.(string)
	if !ok || e.path[id] || depth >= 128 {
		return nativeExpression{}, false
	}
	b = e.t.source.Blocks[id]
	if b == nil {
		return nativeExpression{}, false
	}
	e.path[id] = true
	defer delete(e.path, id)
	e.work++
	input := func(name string) (nativeExpression, bool) {
		return e.input(b, name, depth+1)
	}
	switch b.Opcode {
	case "pen_menu_colorParam":
		if len(b.Inputs) != 0 || len(b.Fields) != 1 {
			return nativeExpression{}, false
		}
		for _, values := range b.Fields {
			if len(values) > 0 {
				return nativeConstant(text(values[0]))
			}
		}
		return nativeExpression{}, false
	case "looks_costume", "sensing_keyoptions":
		if len(b.Fields) > 1 {
			return nativeExpression{}, false
		}
		for _, field := range b.Fields {
			if len(field) > 0 {
				return nativeConstant(field[0])
			}
		}
		return nativeConstant("")

	case "sensing_keypressed":
		key, ok := input("KEY_OPTION")
		return nativeBoolean("r.keyPressed(normalizeKey(text(" + key.raw() + ")))"), ok
	case "sensing_timer":
		return nativeNumeric("(r.elapsed-r.timerSeconds)"), true
	case "motion_xposition":
		return nativeNumeric("limitPrecision(t.state.X)"), true
	case "motion_yposition":
		return nativeNumeric("limitPrecision(t.state.Y)"), true
	case "motion_direction":
		return nativeNumeric("t.state.Direction"), true
	case "data_variable":
		id := field(b, "VARIABLE", 1)
		return e.variable(id), true
	case "data_lengthoflist":
		return nativeNumeric("float64(len(" + e.list(field(b, "LIST", 1)) + ".Items))"), true
	case "data_itemnumoflist", "data_listcontainsitem":
		id := field(b, "LIST", 1)
		slot := e.slot(id, true)
		item, ok := input("ITEM")
		search := fmt.Sprintf("f.findListItem(f.bindList(%s,%q),%s)", slot, id, item.raw())
		if b.Opcode == "data_listcontainsitem" {
			return nativeBoolean("(" + search + " != 0)"), ok
		}
		return nativeNumeric(search), ok
	case "data_itemoflist":
		list := e.list(field(b, "LIST", 1))
		index, ok := input("INDEX")
		if index.numeric != "" && !index.namedIndex {
			return nativeExpression{value: "nativeNumericItem(" + list + "," + index.numeric + ")"}, ok
		}
		return nativeExpression{value: "nativeItem(" + list + "," + index.raw() + ")"}, ok
	case "argument_reporter_string_number", "argument_reporter_boolean":
		name := field(b, "VALUE", 0)
		if e.callInputs {
			if !slices.Contains(e.p.names, name) {
				e.p.names = append(e.p.names, name)
				e.arguments = append(e.arguments, &nativeArgumentUse{index: len(e.arguments)})
			}
		}
		for i := len(e.p.names) - 1; i >= 0; i-- {
			if e.p.names[i] == name {
				if e.inlineArgs != nil {
					return e.inlineArgs[i], true
				}
				if e.arguments == nil {
					e.arguments = make([]*nativeArgumentUse, len(e.p.names))
				}
				if e.arguments[i] == nil {
					e.arguments[i] = &nativeArgumentUse{index: i}
				}
				value := fmt.Sprintf("a%d", i)
				if e.callInputs {
					value += ".boxed()"
				}
				return nativeExpression{value: value, argument: e.arguments[i]}, true
			}
		}
		return nativeExpression{}, false
	case "operator_add", "operator_subtract", "operator_multiply", "operator_divide", "operator_mod":
		left, leftOK := input("NUM1")
		right, rightOK := input("NUM2")
		op := "+"
		switch b.Opcode {
		case "operator_subtract":
			op = "-"
		case "operator_multiply":
			op = "*"
		case "operator_divide":
			op = "/"
		case "operator_mod":
			return nativeNumeric("nativeModulo(" + left.num() + "," + right.num() + ")"), leftOK && rightOK
		}
		raw := "(" + left.num() + op + right.num() + ")"
		return nativeNumeric(raw), leftOK && rightOK
	case "operator_mathop", "operator_round":
		value, ok := input("NUM")
		raw := "nativeRound(" + value.num() + ")"
		if b.Opcode == "operator_mathop" {
			raw = fmt.Sprintf("r.mathOperator(%q,%s)", strings.ToLower(field(b, "OPERATOR", 0)), value.num())
		}
		return nativeNumeric(raw), ok
	case "operator_equals", "operator_lt", "operator_gt":
		left, leftOK := input("OPERAND1")
		right, rightOK := input("OPERAND2")
		op := "=="
		switch b.Opcode {
		case "operator_lt":
			op = "<"
		case "operator_gt":
			op = ">"
		}
		if left.textComparison || right.textComparison {
			return nativeBoolean("(compareText(text(" + left.raw() + "),text(" + right.raw() + "))" + op + "0)"), leftOK && rightOK
		}
		return nativeBoolean("(nativeCompare(" + left.comparable() + "," + right.comparable() + ")" + op + "0)"), leftOK && rightOK
	case "operator_and", "operator_or":
		left, leftOK := input("OPERAND1")
		right, rightOK := input("OPERAND2")
		op := "&&"
		if b.Opcode == "operator_or" {
			op = "||"
		}
		return nativeBoolean("(" + left.truth() + op + right.truth() + ")"), leftOK && rightOK
	case "operator_not":
		value, ok := input("OPERAND")
		return nativeBoolean("(!" + value.truth() + ")"), ok
	case "operator_contains":
		left, leftOK := input("STRING1")
		right, rightOK := input("STRING2")
		return nativeBoolean("nativeContains(" + left.raw() + "," + right.raw() + ")"), leftOK && rightOK
	case "operator_join":
		left, leftOK := input("STRING1")
		right, rightOK := input("STRING2")
		return nativeExpression{value: "(text(" + left.raw() + ")+text(" + right.raw() + "))"}, leftOK && rightOK
	case "operator_length":
		value, ok := input("STRING")
		return nativeNumeric("float64(utf16Length(text(" + value.raw() + ")))"), ok
	case "operator_letter_of":
		index, okIndex := input("LETTER")
		value, okValue := input("STRING")
		return nativeExpression{value: "nativeLetter(" + index.num() + "," + value.raw() + ")"}, okIndex && okValue
	}
	return nativeExpression{}, false
}

func (e *nativeEmitter) statements(id string, depth int) (string, bool) {
	if depth > 32 {
		return "", false
	}
	e.depth = max(e.depth, depth)
	var out strings.Builder
	seen := make(map[string]bool)
	for id != "" && id != e.stop {
		if seen[id] || e.path[id] || e.work > nativeWorkLimit {
			return "", false
		}
		seen[id] = true
		b := e.t.source.Blocks[id]
		if b == nil || (e.region && !nativeRegionStatement(b.Opcode)) {
			return "", false
		}
		e.path[id] = true
		e.work++
		var statement string
		switch b.Opcode {
		case "pen_setPenSizeTo", "pen_changePenSizeBy":
			value, ok := e.input(b, "SIZE", 0)
			if !ok {
				return "", false
			}
			statement = nativeCode("pen-size", map[string]any{
				"Value": value.num(), "Change": b.Opcode == "pen_changePenSizeBy",
			})
		case "pen_setPenColorParamTo", "pen_changePenColorParamBy":
			param, paramOK := e.input(b, "COLOR_PARAM", 0)
			value, valueOK := e.input(b, "VALUE", 0)
			if !paramOK || !valueOK {
				return "", false
			}
			statement = nativeCode("pen-parameter", map[string]any{
				"Param": param.raw(), "Value": value.num(), "Change": b.Opcode == "pen_changePenColorParamBy",
			})
		case "motion_gotoxy", "pen_penUp", "pen_penDown",
			"motion_setx", "motion_sety", "motion_changexby", "motion_changeyby",
			"pen_setPenColorToColor", "pen_stamp",
			"motion_movesteps", "motion_turnleft", "motion_turnright", "motion_pointindirection",
			"looks_setsizeto", "looks_switchcostumeto", "looks_seteffectto", "looks_cleargraphiceffects":
			var names []string
			effect := ""
			switch b.Opcode {
			case "motion_gotoxy":
				names = []string{"X", "Y"}
			case "motion_setx":
				names = []string{"X"}
			case "motion_sety":
				names = []string{"Y"}
			case "motion_changexby":
				names = []string{"DX"}
			case "motion_changeyby":
				names = []string{"DY"}
			case "pen_setPenColorToColor":
				names = []string{"COLOR"}
			case "motion_movesteps":
				names = []string{"STEPS"}
			case "motion_turnleft", "motion_turnright":
				names = []string{"DEGREES"}
			case "motion_pointindirection":
				names = []string{"DIRECTION"}
			case "looks_setsizeto":
				names = []string{"SIZE"}
			case "looks_switchcostumeto":
				names = []string{"COSTUME"}
			case "looks_seteffectto":
				switch strings.ToLower(field(b, "EFFECT", 0)) {
				case "color":
					effect = "Color"
				case "fisheye":
					effect = "Fisheye"
				case "whirl":
					effect = "Whirl"
				case "pixelate":
					effect = "Pixelate"
				case "mosaic":
					effect = "Mosaic"
				case "brightness":
					effect = "Brightness"
				case "ghost":
					effect = "Ghost"
				}
				if effect != "" {
					names = []string{"VALUE"}
				}
			}
			observe := b.Opcode == "motion_gotoxy" || b.Opcode == "motion_movesteps" ||
				b.Opcode == "motion_setx" || b.Opcode == "motion_sety" ||
				b.Opcode == "motion_changexby" || b.Opcode == "motion_changeyby" ||
				b.Opcode == "looks_setsizeto" || b.Opcode == "looks_switchcostumeto" ||
				b.Opcode == "pen_penDown" || b.Opcode == "pen_stamp"
			// Graphics callbacks can invalidate numeric slots before input evaluation.
			if observe && b.Opcode != "motion_gotoxy" {
				clear(e.numbers)
			}
			inputs := make(map[string]string, len(names))
			for _, name := range names {
				value, ok := e.input(b, name, 0)
				if !ok {
					return "", false
				}
				if name == "COLOR" || name == "COSTUME" {
					inputs[name] = value.raw()
				} else {
					inputs[name] = value.num()
				}
			}
			if observe {
				clear(e.numbers)
			}
			e.effects.external = e.effects.external || observe
			statement = nativeCode("drawing", map[string]any{
				"Opcode": b.Opcode, "Inputs": inputs, "Effect": effect, "Observe": observe,
			})
		case "data_setvariableto", "data_changevariableby":
			value, ok := e.input(b, "VALUE", 0)
			if !ok {
				return "", false
			}
			name := field(b, "VARIABLE", 1)
			slot := e.slot(name, false)
			e.effects.access(name, true)
			current, expression := "", ""
			numeric := value.numberResult
			if b.Opcode == "data_changevariableby" {
				current, expression, numeric = e.number(name), value.num(), true
			} else if numeric {
				expression = value.numeric
			} else if value.variableValue != "" {
				expression = value.variableValue
			} else {
				expression = value.raw()
			}
			statement = nativeCode("assignment", map[string]any{
				"Slot": slot, "ID": name, "Numeric": numeric, "Bound": e.numbers[name],
				"Value": expression, "Current": current, "Copy": !numeric && value.variableValue != "",
			})
			if numeric {
				if e.numbers == nil {
					e.numbers = make(map[string]bool)
				}
				e.numbers[name] = true
			} else {
				delete(e.numbers, name)
			}
		case "data_addtolist", "data_deletealloflist", "data_deleteoflist", "data_replaceitemoflist":
			name := field(b, "LIST", 1)
			slot := e.slot(name, true)
			list := e.list(name)
			indexCode, itemCode := "", ""
			indexFunction := "listIndex"
			if b.Opcode == "data_deleteoflist" || b.Opcode == "data_replaceitemoflist" {
				index, ok := e.input(b, "INDEX", 0)
				if !ok {
					return "", false
				}
				value := index.raw()
				if b.Opcode == "data_replaceitemoflist" && index.numeric != "" && !index.namedIndex {
					value, indexFunction = index.numeric, "nativeListIndex"
				}
				indexCode = value
			}
			if b.Opcode == "data_addtolist" || b.Opcode == "data_replaceitemoflist" {
				item, ok := e.input(b, "ITEM", 0)
				if !ok {
					return "", false
				}
				itemCode = item.raw()
			}
			statement = nativeCode("list-write", map[string]any{
				"List": list, "Slot": slot, "Index": indexCode, "Item": itemCode,
				"Opcode": b.Opcode, "IndexFunction": indexFunction,
			})
		case "control_if", "control_if_else":
			condition, ok := e.input(b, "CONDITION", 0)
			if !ok {
				return "", false
			}
			incoming := maps.Clone(e.numbers)
			var outcomes []map[string]bool
			var branches []map[string]any
			for i, name := range []string{"SUBSTACK", "SUBSTACK2"} {
				if i == 1 && b.Opcode == "control_if" {
					break
				}
				e.numbers = maps.Clone(incoming)
				body, err := substack(b, name)
				code, supported := e.statements(body, depth+1)
				outcomes = append(outcomes, e.numbers)
				if err != nil || !supported {
					return "", false
				}
				branches = append(branches, map[string]any{"Code": code, "Step": body != ""})
				if body != "" {
					e.work++
				}
			}
			if len(outcomes) == 1 {
				outcomes = append(outcomes, incoming)
			}
			e.numbers = maps.Clone(outcomes[0])
			for id := range e.numbers {
				if !outcomes[1][id] {
					delete(e.numbers, id)
				}
			}
			statement = nativeCode("branch", map[string]any{
				"Condition": condition.truth(), "Branches": branches,
				"Region": e.region, "Next": b.Next,
			})
		case "control_repeat":
			in := b.Inputs["TIMES"]
			if len(in) < 2 {
				return "", false
			}
			raw := in[1]
			if raw == nil && len(in) > 2 {
				raw = in[2]
			}
			value, constant := constantInput(raw)
			count := math.Floor(number(value) + .5)
			if !constant || !finite(count) || count > 64 {
				return "", false
			}
			count = max(0, count)
			body, err := substack(b, "SUBSTACK")
			before := e.work
			clear(e.numbers)
			code, supported := e.statements(body, depth+1)
			clear(e.numbers)
			if err != nil || !supported || e.work > nativeWorkLimit {
				return "", false
			}
			extra := (e.work-before)*max(0, int(count)-1) + int(count)
			if extra > nativeWorkLimit-e.work {
				return "", false
			}
			e.work += extra
			statement = nativeCode("repeat", map[string]any{"Count": int(count), "Body": code})
		case "procedures_call":
			code, err := mutationText(b, "proccode")
			if err != nil {
				return "", false
			}
			if e.t.procedures[code] == nil {
				debugCode := strings.ReplaceAll(code, "\u200b", "")
				if debugCode == "breakpoint" || debugCode == "tw:debugger;" ||
					strings.HasPrefix(debugCode, "log ") || strings.HasPrefix(debugCode, "warn ") || strings.HasPrefix(debugCode, "error ") {
					return "", false
				}
				break
			}
			callee := e.g.compile(e.t, code)
			if callee == nil {
				return "", false
			}
			ids := callee.procedure.ids
			if len(b.Mutation["argumentids"]) > 0 {
				ids, err = procedureStrings(b, "argumentids")
			}
			if err != nil || len(ids) > len(callee.procedure.names) {
				return "", false
			}
			duplicated := false
			unique := make(map[string]bool)
			for _, id := range ids {
				if unique[id] {
					duplicated = true
				}
				unique[id] = true
			}
			arguments := make([]string, len(callee.procedure.names))
			var declarations strings.Builder
			seenInputs := make(map[string]string)
			for i := range arguments {
				value, ok := nativeConstant(callee.procedure.defaults[i])
				if i < len(ids) {
					if _, present := b.Inputs[ids[i]]; present {
						if prior, seen := seenInputs[ids[i]]; seen {
							arguments[i] = prior
							if callee.parameters[i].Numeric {
								arguments[i] = "number(" + prior + ")"
							}
							continue
						}
						value, ok = e.input(b, ids[i], 0)
					}
				}
				if !ok {
					return "", false
				}
				arguments[i] = fmt.Sprintf("arg%d_%d", e.inlineDepth, i)
				numeric := callee.parameters[i].Numeric && !duplicated
				expression := ""
				if numeric {
					expression = value.num()
				} else {
					expression = value.raw()
				}
				declarations.WriteString(nativeCode("argument", map[string]any{
					"Name": arguments[i], "Value": expression, "Numeric": numeric,
				}))
				if i < len(ids) {
					if _, present := b.Inputs[ids[i]]; present {
						seenInputs[ids[i]] = arguments[i]
					}
				}
				if duplicated && callee.parameters[i].Numeric {
					arguments[i] = "number(" + arguments[i] + ")"
				}
			}
			if callee.work <= 32 && !callee.effects.external && !duplicated {
				args := make([]nativeExpression, len(arguments))
				for i, value := range arguments {
					args[i] = nativeExpression{value: value}
					if callee.parameters[i].Numeric {
						args[i] = nativeNumeric(value)
					}
				}
				previousProcedure, previousArgs := e.p, e.inlineArgs
				previousStop, previousRegion := e.stop, e.region
				e.p, e.inlineArgs, e.stop, e.region = callee.procedure, args, "", false
				e.inlineDepth++
				body, ok := e.statements(callee.procedure.body, depth+1)
				e.inlineDepth--
				e.p, e.inlineArgs, e.stop, e.region = previousProcedure, previousArgs, previousStop, previousRegion
				if !ok {
					return "", false
				}
				e.work++ // Returning from the custom block is still a VM step.
				statement = nativeCode("inline-call", map[string]any{
					"Declarations": declarations.String(), "Arguments": arguments, "Body": body,
				})
			} else {
				slots := make([]string, len(callee.variables))
				for i, id := range callee.variables {
					slots[i] = e.slot(id, false)
				}
				listSlots := make([]string, len(callee.lists))
				for i, id := range callee.lists {
					listSlots[i] = e.slot(id, true)
				}
				statement = nativeCode("call", map[string]any{
					"Declarations": declarations.String(), "Name": callee.name, "Arguments": arguments,
					"Barrier": e.callBarrier(callee.effects), "Variables": slots, "Lists": listSlots,
				})
				e.work += callee.work
			}
			e.depth = max(e.depth, depth+callee.depth)
		default:
			return "", false
		}
		out.WriteString(nativeCode("statement", map[string]any{
			"Region": e.region, "Next": b.Next, "Body": statement,
		}))
		delete(e.path, id)
		id = b.Next
	}
	return out.String(), true
}
