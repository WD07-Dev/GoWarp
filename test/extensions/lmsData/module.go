// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"bytes"
	"encoding/json"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"strings"

	e "gowarp/core/extension"
)

// The hidden forEach blocks require lmsTempVars2 thread variables, which are
// not exposed by the Go extension API. Ordinary list blocks use public calls.
func New() e.Extension {
	return e.Extension{ID: "lmsData", Blocks: []e.Block{
		{Opcode: "lmsData_deleteItems", Kind: "command", Inputs: []string{"NUM1", "NUM2", "LIST"}, Handler: deleteItems},
		{Opcode: "lmsData_deleteAllOfItem", Kind: "command", Inputs: []string{"ITEM", "LIST"}, Handler: deleteAllOfItem},
		{Opcode: "lmsData_replaceAllOfItem", Kind: "command", Inputs: []string{"ITEM1", "ITEM2", "LIST"}, Handler: replaceAllOfItem},
		{Opcode: "lmsData_repeatList", Kind: "command", Inputs: []string{"LIST1", "NUM", "LIST2"}, Handler: repeatList},
		{Opcode: "lmsData_getListJoin", Kind: "reporter", Inputs: []string{"LIST", "STRING"}, Handler: getListJoin},
		{Opcode: "lmsData_timesItemAppears", Kind: "reporter", Inputs: []string{"ITEM", "LIST"}, Handler: timesItemAppears},
		{Opcode: "lmsData_itemIndex", Kind: "reporter", Inputs: []string{"INDEX", "ITEM", "LIST"}, Handler: itemIndex},
		{Opcode: "lmsData_listIsEmpty", Kind: "boolean", Inputs: []string{"LIST"}, Handler: listIsEmpty},
		{Opcode: "lmsData_itemNumExists", Kind: "boolean", Inputs: []string{"NUM", "LIST"}, Handler: itemNumExists},
		{Opcode: "lmsData_orderIs", Kind: "boolean", Inputs: []string{"LIST"}, Handler: orderIs},
		{Opcode: "lmsData_orderList", Kind: "command", Inputs: []string{"LIST"}, Handler: orderList},
		{Opcode: "lmsData_setListToList", Kind: "command", Inputs: []string{"LIST1", "LIST2"}, Handler: setListToList},
		{Opcode: "lmsData_joinLists", Kind: "command", Inputs: []string{"LIST1", "LIST2"}, Handler: joinLists},
		{Opcode: "lmsData_setListArray", Kind: "command", Inputs: []string{"LIST", "ARRAY"}, Handler: setListArray},
		{Opcode: "lmsData_getListArray", Kind: "reporter", Inputs: []string{"LIST"}, Handler: getListArray},
		{Opcode: "lmsData_menu_lists", Kind: "menu", Handler: func(c *e.Context) (e.Result, error) {
			return e.Result{Value: c.Field("lists")}, nil
		}},
	}}
}

type list struct {
	id, target string
	values     []any
}

func findList(c *e.Context, argument string) (*list, error) {
	// The original extension resolves global names before sprite/clone names.
	for _, target := range []string{"_stage_", "_myself_"} {
		value, err := c.Call("list.list", target)
		if err != nil {
			return nil, err
		}
		for _, item := range value.([]any) {
			record := item.(map[string]any)
			if e.String(record["name"]) != c.String(argument) {
				continue
			}
			id := e.String(record["id"])
			value, err := c.Call("list.get", id, target)
			if err != nil {
				return nil, err
			}
			return &list{id, target, value.([]any)}, nil
		}
	}
	return nil, nil
}

func (l *list) set(c *e.Context, values []any) (e.Result, error) {
	_, err := c.Call("list.set", l.id, values, l.target)
	return e.Result{}, err
}

func listIsEmpty(c *e.Context) (e.Result, error) {
	l, err := findList(c, "LIST")
	return e.Result{Value: l == nil || len(l.values) == 0}, err
}

func orderList(c *e.Context) (e.Result, error) {
	l, err := findList(c, "LIST")
	if err != nil || l == nil {
		return e.Result{}, err
	}
	switch c.Field("ORDER") {
	case "ascending", "descending":
		sort.SliceStable(l.values, func(i, j int) bool { return e.Compare(l.values[i], l.values[j]) < 0 })
		if c.Field("ORDER") == "ascending" {
			break
		}
		fallthrough
	case "reversed":
		for i, j := 0, len(l.values)-1; i < j; i, j = i+1, j-1 {
			l.values[i], l.values[j] = l.values[j], l.values[i]
		}
	case "randomised":
		rand.Shuffle(len(l.values), func(i, j int) { l.values[i], l.values[j] = l.values[j], l.values[i] })
	}
	return l.set(c, l.values)
}

func orderIs(c *e.Context) (e.Result, error) {
	l, err := findList(c, "LIST")
	if err != nil || l == nil {
		return e.Result{Value: false}, err
	}
	for i := 1; i < len(l.values); i++ {
		cmp := e.Compare(l.values[i], l.values[i-1])
		if (cmp > 0 && c.Field("ORDER") == "descending") || (cmp < 0 && c.Field("ORDER") == "ascending") {
			return e.Result{Value: false}, nil
		}
	}
	return e.Result{Value: true}, nil
}

func sliceIndex(n float64, length int) int {
	if math.IsNaN(n) {
		return 0
	}
	n = math.Trunc(n)
	if n < 0 {
		n += float64(length)
	}
	return int(e.Clamp(n, 0, float64(length)))
}

func deleteItems(c *e.Context) (e.Result, error) {
	l, err := findList(c, "LIST")
	if err != nil || l == nil {
		return e.Result{}, err
	}
	a, b := c.Args["NUM1"], c.Args["NUM2"]
	greater := jsNumber(a) > jsNumber(b)
	if sa, ok := a.(string); ok {
		if sb, ok := b.(string); ok {
			greater = sa > sb
		}
	}
	if greater {
		a, b = b, a
	}
	start, end := sliceIndex(jsNumber(a)-1, len(l.values)), sliceIndex(jsNumber(b), len(l.values))
	return l.set(c, append(l.values[:start:start], l.values[end:]...))
}

func strictEqual(a, b any) bool {
	switch a := a.(type) {
	case nil:
		return b == nil
	case string:
		v, ok := b.(string)
		return ok && a == v
	case bool:
		v, ok := b.(bool)
		return ok && a == v
	case int, int64, float32, float64:
		switch b.(type) {
		case int, int64, float32, float64:
			return jsNumber(a) == jsNumber(b)
		}
	}
	return false
}

func jsNumber(v any) float64 {
	if s, ok := v.(string); ok {
		s = strings.Trim(strings.TrimSpace(s), "\ufeff")
		if s == "" {
			return 0
		}
		if s == "Infinity" || s == "+Infinity" {
			return math.Inf(1)
		}
		if s == "-Infinity" {
			return math.Inf(-1)
		}
		if len(s) > 2 && s[0] == '0' {
			base := 0
			switch s[1] {
			case 'x', 'X':
				base = 16
			case 'b', 'B':
				base = 2
			case 'o', 'O':
				base = 8
			}
			if base != 0 {
				n, err := strconv.ParseUint(s[2:], base, 64)
				if err == nil {
					return float64(n)
				}
				return math.NaN()
			}
		}
		for _, r := range s {
			if !strings.ContainsRune("0123456789+-.eE", r) {
				return math.NaN()
			}
		}
		n, err := strconv.ParseFloat(s, 64)
		if err == nil || math.IsInf(n, 0) {
			return n
		}
		return math.NaN()
	}
	switch v := v.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case nil, int, int64, bool:
		return e.Number(v)
	}
	return math.NaN()
}

func looseEqual(a, b any) bool {
	if strictEqual(a, b) {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if _, ok := a.(string); ok {
		if _, ok := b.(string); ok {
			return false
		}
	}
	return jsNumber(a) == jsNumber(b)
}

func deleteAllOfItem(c *e.Context) (e.Result, error) {
	l, err := findList(c, "LIST")
	if err != nil || l == nil {
		return e.Result{}, err
	}
	values := []any{}
	for _, v := range l.values {
		if !strictEqual(v, c.Args["ITEM"]) {
			values = append(values, v)
		}
	}
	return l.set(c, values)
}

func replaceAllOfItem(c *e.Context) (e.Result, error) {
	l, err := findList(c, "LIST")
	if err != nil || l == nil {
		return e.Result{}, err
	}
	for i, v := range l.values {
		if strictEqual(v, c.Args["ITEM1"]) {
			l.values[i] = c.Args["ITEM2"]
		}
	}
	return l.set(c, l.values)
}

func timesItemAppears(c *e.Context) (e.Result, error) {
	l, err := findList(c, "LIST")
	count := 0
	if l != nil {
		for _, v := range l.values {
			if looseEqual(v, c.Args["ITEM"]) {
				count++
			}
		}
	}
	return e.Result{Value: count}, err
}

func itemIndex(c *e.Context) (e.Result, error) {
	l, err := findList(c, "LIST")
	if err != nil || l == nil {
		return e.Result{Value: 0}, err
	}
	indices := []int{}
	for i, v := range l.values {
		if strictEqual(v, c.Args["ITEM"]) {
			indices = append(indices, i+1)
		}
	}
	if len(indices) == 0 {
		return e.Result{Value: 0}, nil
	}
	index := jsNumber(c.Args["INDEX"]) - 1
	switch c.String("INDEX") {
	case "_first_":
		index = 0
	case "_last_":
		index = float64(len(indices) - 1)
	case "_random_":
		index = float64(rand.Intn(len(indices)))
	}
	if index < 0 || index >= float64(len(indices)) || math.IsNaN(index) || math.Trunc(index) != index {
		return e.Result{Value: 0}, nil
	}
	return e.Result{Value: indices[int(index)]}, nil
}

func itemNumExists(c *e.Context) (e.Result, error) {
	l, err := findList(c, "LIST")
	if err != nil || l == nil {
		return e.Result{Value: false}, err
	}
	n := math.Floor(c.Number("NUM"))
	switch c.String("NUM") {
	case "last", "random", "any":
		n = float64(len(l.values))
	case "all":
		n = 0
	}
	return e.Result{Value: n >= 1 && n <= float64(len(l.values))}, nil
}

func getListJoin(c *e.Context) (e.Result, error) {
	l, err := findList(c, "LIST")
	if err != nil || l == nil {
		return e.Result{Value: ""}, err
	}
	parts := make([]string, len(l.values))
	for i, v := range l.values {
		parts[i] = joinString(v)
	}
	return e.Result{Value: strings.Join(parts, c.String("STRING"))}, nil
}

func joinString(v any) string {
	if v == nil {
		return ""
	}
	if values, ok := v.([]any); ok {
		parts := make([]string, len(values))
		for i, v := range values {
			parts[i] = joinString(v)
		}
		return strings.Join(parts, ",")
	}
	if _, ok := v.(map[string]any); ok {
		return "[object Object]"
	}
	return e.String(v)
}

func twoLists(c *e.Context) (*list, *list, error) {
	a, err := findList(c, "LIST1")
	if err != nil || a == nil {
		return a, nil, err
	}
	b, err := findList(c, "LIST2")
	return a, b, err
}

func repeatList(c *e.Context) (e.Result, error) {
	a, b, err := twoLists(c)
	if err != nil || a == nil || b == nil {
		return e.Result{}, err
	}
	for i := 0.0; i < c.Number("NUM"); i++ {
		b.values = append(b.values, a.values...)
	}
	return b.set(c, b.values)
}

func setListToList(c *e.Context) (e.Result, error) {
	a, b, err := twoLists(c)
	if err != nil || a == nil || b == nil {
		return e.Result{}, err
	}
	return a.set(c, b.values)
}

func joinLists(c *e.Context) (e.Result, error) {
	a, b, err := twoLists(c)
	if err != nil || a == nil || b == nil {
		return e.Result{}, err
	}
	return b.set(c, append(b.values, a.values...))
}

func setListArray(c *e.Context) (e.Result, error) {
	l, err := findList(c, "LIST")
	if err != nil || l == nil {
		return e.Result{}, err
	}
	var value any
	if json.Unmarshal([]byte(c.String("ARRAY")), &value) != nil {
		return e.Result{}, nil
	}
	values, ok := value.([]any)
	if !ok {
		return e.Result{}, nil
	}
	return l.set(c, values)
}

func getListArray(c *e.Context) (e.Result, error) {
	l, err := findList(c, "LIST")
	if err != nil || l == nil {
		return e.Result{Value: ""}, err
	}
	if l.values == nil {
		l.values = []any{}
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	err = encoder.Encode(jsonValue(l.values))
	return e.Result{Value: strings.TrimSuffix(out.String(), "\n")}, err
}

// JSON.stringify serializes nonfinite numbers as null, including nested values.
func jsonValue(value any) any {
	switch value := value.(type) {
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil
		}
	case float32:
		return jsonValue(float64(value))
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = jsonValue(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, item := range value {
			out[key] = jsonValue(item)
		}
		return out
	}
	return value
}
