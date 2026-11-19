package vm

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"strings"

	"gowarp/core/project"
)

const listLimit = 200000
const listTextIndexLimit = 8192

type listTextIndex struct {
	values   map[string]int
	overflow bool
	valid    bool
}

type listSharing struct {
	shared bool
}

type List struct {
	Name      string `json:"name"`
	Items     []any  `json:"items"`
	textIndex *listTextIndex
	sharing   *listSharing
}

// Keep the prepared inputs, never their values or the owning sprite's list.
type listWrite struct {
	id          string
	index, item *numericExpression
}

func (r *Runtime) prepareListWrite(t *target, b *project.Block) *listWrite {
	if write, found := r.listWrites[b]; found {
		return write
	}
	if r.listWrites == nil {
		r.listWrites = make(map[*project.Block]*listWrite)
	}
	var write *listWrite
	item := r.numericInput(t, b, "ITEM", 0)
	if item != nil && item.height <= 128 {
		if b.Opcode == "data_addtolist" {
			write = &listWrite{id: field(b, "LIST", 1), item: item}
		} else if index := r.numericInput(t, b, "INDEX", 0); index != nil && index.height <= 128 && index.blocks+item.blocks <= stepsPerFrame {
			write = &listWrite{id: field(b, "LIST", 1), index: index, item: item}
		}
	}
	r.listWrites[b] = write
	return write
}

func (w *listWrite) run(ctx context.Context, r *Runtime, th *thread) error {
	owner, list, err := r.list(th.target, w.id)
	if err != nil {
		return err
	}
	budget := stepsPerFrame
	if w.index == nil {
		item, err := w.item.readConditional(ctx, r, th, &budget)
		if err != nil {
			return err
		}
		if len(list.Items) < listLimit {
			list.append(item)
		}
		owner.state.Lists[w.id] = list
		return nil
	}
	var index any
	if w.index.numeric {
		index, err = w.index.evaluate(ctx, r, th)
		budget -= w.index.blocks
	} else {
		index, err = w.index.readConditional(ctx, r, th, &budget)
	}
	if err != nil {
		return err
	}
	item, err := w.item.readConditional(ctx, r, th, &budget)
	if err != nil {
		return err
	}
	/*
		Evaluate ITEM even for an invalid index, and resolve named/random
		indexes afterwards, matching the interpreter's input order.
	*/
	if i := listIndex(index, len(list.Items)); i >= 0 {
		list.replace(i, item)
	}
	owner.state.Lists[w.id] = list
	return nil
}

/*
A marker is shared by temporary list values too: an input reporter can
create a clone after its caller has already resolved the list.
*/
func (list *List) writable(extra int) {
	if list.sharing == nil || !list.sharing.shared {
		return
	}
	items := make([]any, len(list.Items), len(list.Items)+extra)
	copy(items, list.Items)
	list.Items = items
	list.sharing = &listSharing{}
	list.textIndex = nil
}

func (list *List) clear() {
	list.textIndex = nil
	/*
		Drawing scripts repeatedly refill small scratch lists. Release large
		buffers, and clear references before reusing the small ones.
	*/
	if cap(list.Items) == 0 || cap(list.Items) > 256 || (list.sharing != nil && list.sharing.shared) {
		list.Items = []any{}
		list.sharing = nil
		return
	}
	clear(list.Items)
	list.Items = list.Items[:0]
}

/*
Finite numbers cannot match a nonnumeric string under Scratch comparison.
Omitting them keeps coordinate updates from rebuilding the identifier index.
*/
func listTextKey(value any) (string, bool) {
	if n, ok := value.(float64); ok && finite(n) {
		return "", false
	}
	return strings.ToLower(text(value)), true
}

func (list *List) append(item any) {
	list.writable(1)
	index := len(list.Items)
	list.Items = append(list.Items, item)
	if list.textIndex != nil && list.textIndex.valid {
		list.textIndex.add(item, index+1)
	}
}

func (list *List) insert(index int, item any) {
	list.writable(1)
	list.invalidateTextIndex()
	list.Items = append(list.Items, nil)
	copy(list.Items[index+1:], list.Items[index:])
	list.Items[index] = item
}

func (list *List) remove(index int) {
	list.writable(0)
	list.invalidateTextIndex()
	copy(list.Items[index:], list.Items[index+1:])
	list.Items[len(list.Items)-1] = nil
	list.Items = list.Items[:len(list.Items)-1]
}

func (index *listTextIndex) add(item any, position int) {
	key, ok := listTextKey(item)
	if !ok {
		return
	}
	if _, exists := index.values[key]; exists {
		return
	}
	if len(index.values) >= listTextIndexLimit {
		index.overflow = true
		return
	}
	index.values[key] = position
}

func (list *List) replace(index int, item any) {
	list.writable(0)
	if list.textIndex != nil {
		oldKey, oldOK := listTextKey(list.Items[index])
		newKey, newOK := listTextKey(item)
		if oldOK != newOK || oldKey != newKey {
			list.invalidateTextIndex()
		}
	}
	list.Items[index] = item
}

// Release old keys while keeping the bounded table for the next search.
func (list *List) invalidateTextIndex() {
	if index := list.textIndex; index != nil && index.valid {
		clear(index.values)
		index.valid = false
	}
}

func (list *List) findText(ctx context.Context, value any) (int, bool, error) {
	s, ok := value.(string)
	if !ok || len(list.Items) < 32 {
		return 0, false, nil
	}
	/*
		Numeric queries still use compare: "02", 2 and true have Scratch's
		coercion rules. Blank strings compare as text, not as numeric zero.
	*/
	if !math.IsNaN(numeric(s)) && trim(s) != "" {
		return 0, false, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, true, err
	}
	if list.textIndex == nil || !list.textIndex.valid {
		index := list.textIndex
		if index == nil {
			index = &listTextIndex{values: make(map[string]int)}
		} else {
			clear(index.values)
			index.overflow = false
		}
		for i, item := range list.Items {
			if i%1024 == 0 {
				if err := ctx.Err(); err != nil {
					clear(index.values)
					return 0, true, err
				}
			}
			index.add(item, i+1)
		}
		index.valid = true
		list.textIndex = index
	}
	// Bound cached names, not numeric slots. When full, retain hits and scan uncached queries.
	index, found := list.textIndex.values[strings.ToLower(s)]
	return index, found || !list.textIndex.overflow, nil
}

func (list *List) find(ctx context.Context, value any) (int, error) {
	if len(list.Items) == 0 {
		return 0, nil
	}
	if len(list.Items) >= 32 {
		if index, handled, err := list.findText(ctx, value); handled || err != nil {
			return index, err
		}
	}
	queryString, isString := value.(string)
	queryText, textReady := queryString, isString
	var queryNumber float64
	queryReady := false
	for i, item := range list.Items {
		if i%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
		}
		if s, ok := item.(string); ok && isString && s == queryString {
			return i + 1, nil
		}
		if !queryReady {
			queryNumber = numeric(value)
			if queryNumber == 0 && isString && trim(queryString) == "" {
				queryNumber = math.NaN()
			}
			queryReady = true
		}
		itemNumber := numeric(item)
		if itemNumber == 0 {
			if s, ok := item.(string); ok && trim(s) == "" {
				itemNumber = math.NaN()
			}
		}
		if !math.IsNaN(itemNumber) && !math.IsNaN(queryNumber) {
			if itemNumber == queryNumber {
				return i + 1, nil
			}
			continue
		}
		if !textReady {
			queryText, textReady = text(value), true
		}
		if compareText(text(item), queryText) == 0 {
			return i + 1, nil
		}
	}
	return 0, nil
}

func (r *Runtime) list(t *target, id string) (*target, List, error) {
	if list, ok := t.state.Lists[id]; ok {
		if list.sharing == nil {
			list.sharing = &listSharing{}
			t.state.Lists[id] = list
		}
		return t, list, nil
	}
	if r.stage != nil {
		if list, ok := r.stage.state.Lists[id]; ok {
			if list.sharing == nil {
				list.sharing = &listSharing{}
				r.stage.state.Lists[id] = list
			}
			return r.stage, list, nil
		}
	}
	return nil, List{}, fmt.Errorf("unknown list %q", id)
}

// Return a zero-based index, or -1 for Scratch's invalid index.
func listIndex(value any, size int) int {
	if s, ok := value.(string); ok {
		switch s {
		case "last":
			return size - 1
		case "random", "any":
			if size == 0 {
				return -1
			}
			return rand.Intn(size)
		}
	}
	n := math.Floor(number(value))
	if n < 1 || n > float64(size) {
		return -1
	}
	return int(n) - 1
}

func listContents(ctx context.Context, list List) (string, error) {
	parts := make([]string, len(list.Items))
	single := true
	for i, item := range list.Items {
		if i%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return "", err
			}
		}
		parts[i] = text(item)
		s, ok := item.(string)
		if !ok || utf16Length(s) != 1 {
			single = false
		}
	}
	separator := " "
	if single {
		separator = ""
	}
	return strings.Join(parts, separator), nil
}

func (r *Runtime) listStatement(t *target, b *project.Block, input func(string) (any, error)) error {
	id := field(b, "LIST", 1)
	owner, list, err := r.list(t, id)
	if err != nil {
		return err
	}
	switch b.Opcode {
	case "data_addtolist":
		item, err := input("ITEM")
		if err != nil {
			return err
		}
		if r.listObserver != nil {
			r.listObserver(ListAppend{
				Target: t.state.Name,
				List:   list.Name,
				Text:   text(item),
				Number: number(item),
				Note:   r.listNotes[b],
			})
		}
		if len(list.Items) < listLimit {
			list.append(item)
		}
	case "data_deletealloflist":
		list.clear()
	case "data_deleteoflist":
		value, err := input("INDEX")
		if err != nil {
			return err
		}
		if value == "all" {
			list.clear()
		} else if index := listIndex(value, len(list.Items)); index >= 0 {
			list.remove(index)
		}
	case "data_insertatlist", "data_replaceitemoflist":
		value, err := input("INDEX")
		if err != nil {
			return err
		}
		item, err := input("ITEM")
		if err != nil {
			return err
		}
		size := len(list.Items)
		if b.Opcode == "data_insertatlist" {
			size++
		}
		index := listIndex(value, size)
		if index >= 0 {
			if b.Opcode == "data_replaceitemoflist" {
				list.replace(index, item)
			} else if index < listLimit {
				list.insert(index, item)
				if len(list.Items) > listLimit {
					list.Items[len(list.Items)-1] = nil
					list.Items = list.Items[:len(list.Items)-1]
				}
			}
		}
	}
	owner.state.Lists[id] = list
	return nil
}

func (r *Runtime) listReporter(ctx context.Context, t *target, b *project.Block, input func(string) (any, error)) (any, error) {
	id := field(b, "LIST", 1)
	owner, list, err := r.list(t, id)
	if err != nil {
		return nil, err
	}
	switch b.Opcode {
	case "data_lengthoflist":
		return float64(len(list.Items)), nil
	case "data_listcontents":
		return listContents(ctx, list)
	case "data_itemoflist":
		value, err := input("INDEX")
		if err != nil {
			return nil, err
		}
		index := listIndex(value, len(list.Items))
		if index < 0 {
			return "", nil
		}
		return list.Items[index], nil
	case "data_itemnumoflist", "data_listcontainsitem":
		value, err := input("ITEM")
		if err != nil {
			return nil, err
		}
		/*
			A native reporter can mutate this list while evaluating ITEM.
			Never restore the slice or index captured before that call.
		*/
		owner, list, err = r.list(t, id)
		if err != nil {
			return nil, err
		}
		index, err := list.find(ctx, value)
		if err != nil {
			return nil, err
		}
		if list.textIndex != nil {
			owner.state.Lists[id] = list
		}
		if b.Opcode == "data_listcontainsitem" {
			return index != 0, nil
		}
		return float64(index), nil
	}
	return nil, fmt.Errorf("unsupported list reporter %q", b.Opcode)
}
