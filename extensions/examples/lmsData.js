/*
@gowarp-config {
}

@gowarp-go {
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
}
*/
// Name: List Tools
// ID: lmsListTools
// Description: An assortment of new ways to interact with lists.
// By: LilyMakesThings <https://scratch.mit.edu/users/LilyMakesThings/>
// License: MIT AND LGPL-3.0

// (It's getting harder and harder to think of original descriptions now)

/* generated l10n code */Scratch.translate.setup({"de":{"_List Tools":"Listenwerkzeuge"},"fi":{"_# of times [ITEM] appears in [LIST]":"[ITEM] -arvoisten kohteiden määrä listassa [LIST]","_List Tools":"Listatyökalut","_[LIST] as array":"[LIST] taulukkona","_[LIST] is empty?":"onko [LIST] tyhjä?","_ascending":"nouseva","_concatenate [LIST1] onto [LIST2]":"liitä [LIST1] listaan [LIST2]","_delete all [ITEM] in [LIST]":"poista jokainen [ITEM] listasta [LIST]","_delete items [NUM1] to [NUM2] of [LIST]":"poista kohteet [NUM1] – [NUM2] listasta [LIST]","_descending":"laskeva","_first":"ensimmäinen","_for each item # [VAR] in [LIST]":"toista listan [LIST] pituuden verran ja laske muuttujalla [VAR]","_for each item value [VAR] in [LIST]":"toista listan [LIST] [VAR] -arvoisten kohteiden määrän verran","_index # [INDEX] of item [ITEM] in [LIST]":"[INDEX]. [ITEM] -arvoinen kohde listassa [LIST]","_item [NUM] exists in [LIST]?":"onko listassa [LIST] kohde [NUM]?","_last":"viimeinen","_list [LIST] joined by [STRING]":"lista [LIST] erotettuna merkillä [STRING]","_order of [LIST] is [ORDER]?":"onko listan [LIST] järjestys [ORDER]?","_random":"satunnainen","_randomized":"satunnainen","_repeat [LIST1] [NUM] times in [LIST2]":"toista [LIST1] [NUM] kertaa listassa [LIST2]","_replace all [ITEM1] with [ITEM2] in [LIST]":"korvaa jokainen [ITEM1] listassa [LIST] arvolla [ITEM2]","_reversed":"käänteinen","_set [LIST] to array [ARRAY]":"aseta listan [LIST] sisällöksi taulukko [ARRAY] ","_set items of [LIST1] to [LIST2]":"aseta listan [LIST1] kohteiksi [LIST2]","_set order of [LIST] to [ORDER]":"aseta listan [LIST] järjestykseksi [ORDER]"},"it":{"_# of times [ITEM] appears in [LIST]":"numero di volte che [ITEM] appare in [LIST]","_List Tools":"Strumenti Avanzati per Liste","_[LIST] as array":"[LIST] come elenco","_[LIST] is empty?":"[LIST] è vuota","_ascending":"ascendente","_concatenate [LIST1] onto [LIST2]":"concatena [LIST1] a [LIST2]","_delete all [ITEM] in [LIST]":"rimuovi tutti gli elementi [ITEM] in [LIST]","_delete items [NUM1] to [NUM2] of [LIST]":"rimuovi elementi da [NUM1] a [NUM2] di [LIST]","_descending":"discendente","_first":"primo","_for each item # [VAR] in [LIST]":"per ciascuna posizione di [LIST] in [VAR]","_for each item value [VAR] in [LIST]":"per ciascun valore della variabile [VAR] in [LIST]","_index # [INDEX] of item [ITEM] in [LIST]":"elemento in posizione [INDEX] di elemento [ITEM] in [LIST]","_item [NUM] exists in [LIST]?":"esiste elemento [NUM] in [LIST]","_last":"ultimo","_list [LIST] joined by [STRING]":"unisci elementi di [LIST] con [STRING]","_order of [LIST] is [ORDER]?":"ordine di [LIST] è [ORDER]","_random":"casuale","_randomized":"casuale","_repeat [LIST1] [NUM] times in [LIST2]":"ripeti [LIST1] per [NUM] volte in [LIST2]","_replace all [ITEM1] with [ITEM2] in [LIST]":"sostituisci tutti gli elementi [ITEM1] con [ITEM2] in [LIST]","_reversed":"inverso","_set [LIST] to array [ARRAY]":"imposta valori [LIST] da elenco [ARRAY]","_set items of [LIST1] to [LIST2]":"rimpiazza elementi di [LIST1] con [LIST2]","_set order of [LIST] to [ORDER]":"riordina [LIST] in modo [ORDER]"},"ja":{"_# of times [ITEM] appears in [LIST]":"[LIST]で[ITEM]が出てきた回数","_List Tools":"リストツール","_[LIST] as array":"[LIST]のarray","_[LIST] is empty?":"[LIST]が空","_ascending":"数が増加","_concatenate [LIST1] onto [LIST2]":"[LIST1]の中身を[LIST2]に追加する","_delete all [ITEM] in [LIST]":"[LIST]の[ITEM]をすべて削除する","_delete items [NUM1] to [NUM2] of [LIST]":"[LIST]の[NUM1]番から[NUM2]番までを削除する","_descending":"数が減少","_first":"最初の","_index # [INDEX] of item [ITEM] in [LIST]":"[LIST]の[ITEM]が[INDEX]番目に出てきた場所","_item [NUM] exists in [LIST]?":"[LIST]に[NUM]番目が存在する","_last":"最後の","_list [LIST] joined by [STRING]":"[STRING]でつなげた[LIST]","_order of [LIST] is [ORDER]?":"[LIST]が[ORDER]","_random":"ランダム","_randomized":"ランダム","_repeat [LIST1] [NUM] times in [LIST2]":"[LIST1]を[LIST2]で[NUM]回繰り返す","_replace all [ITEM1] with [ITEM2] in [LIST]":"[LIST]のすべての[ITEM1]を[ITEM2]で置き換える","_reversed":"順番が逆","_set [LIST] to array [ARRAY]":"[LIST]をarray[ARRAY]にセットする","_set items of [LIST1] to [LIST2]":"[LIST1]の中身を[LIST2]にする","_set order of [LIST] to [ORDER]":"[LIST]を[ORDER]にする"},"ko":{"_# of times [ITEM] appears in [LIST]":"[LIST]에서 [ITEM]의 개수 ","_List Tools":"리스트 도구","_[LIST] as array":"[LIST]을(를) 배열로","_[LIST] is empty?":"[LIST]이(가) 비었는가?","_ascending":"오름차순","_concatenate [LIST1] onto [LIST2]":"[LIST2]에서 [LIST1]을(를) 덛붙이기","_delete all [ITEM] in [LIST]":"[LIST]의 모든 [ITEM] 삭제하기","_delete items [NUM1] to [NUM2] of [LIST]":"[LIST]의 [NUM1]번째부터 [NUM2]번째까지 삭제하기","_descending":"내림차순","_first":"첫째","_for each item # [VAR] in [LIST]":"[LIST]의 각 항목의 번째를 [VAR](으)로 순회하기","_for each item value [VAR] in [LIST]":"[LIST]의 각 항목의 값을 [VAR](으)로 순회하기","_item [NUM] exists in [LIST]?":"[LIST]에서 [NUM]이(가) 존재하는가?","_last":"마지막째","_list [LIST] joined by [STRING]":"[LIST]을(를) [STRING](으)로 이어붙임","_order of [LIST] is [ORDER]?":"[LIST]이(가) [ORDER]인가?","_random":"무작위","_randomized":"무작위","_replace all [ITEM1] with [ITEM2] in [LIST]":"[LIST]의 모든 [ITEM1]을(를) [ITEM2](으)로 바꾸기","_reversed":"역방향","_set [LIST] to array [ARRAY]":"[LIST]을(를) 배열 [ARRAY](으)로 설정하기","_set items of [LIST1] to [LIST2]":"[LIST1]을(를) [LIST2]의 항목으로 정하기","_set order of [LIST] to [ORDER]":"[LIST]을(를) [ORDER](으)로 정렬하기"},"nb":{"_List Tools":"Liste verktøy"},"nl":{"_# of times [ITEM] appears in [LIST]":"aantal [ITEM] in [LIST]","_List Tools":"Lijsttools","_[LIST] as array":"[LIST] als array","_[LIST] is empty?":"[LIST] is leeg?","_ascending":"oplopend","_concatenate [LIST1] onto [LIST2]":"voeg [LIST1] toe aan [LIST2]","_delete all [ITEM] in [LIST]":"verwijder alle [ITEM] in [LIST]","_delete items [NUM1] to [NUM2] of [LIST]":"verwijder items [NUM1] t/m [NUM2] van [LIST]","_descending":"aflopend","_index # [INDEX] of item [ITEM] in [LIST]":"index van herhaling # [INDEX] van item [ITEM] in [LIST]","_item [NUM] exists in [LIST]?":"item # [NUM] bestaat in [LIST]?","_list [LIST] joined by [STRING]":"lijst [LIST] samengevoegd met [STRING]","_order of [LIST] is [ORDER]?":"volgorde van [LIST] is [ORDER]?","_random":"willekeurig","_randomized":"willekeurig","_repeat [LIST1] [NUM] times in [LIST2]":"herhaal [LIST1] [NUM] keer in [LIST2]","_replace all [ITEM1] with [ITEM2] in [LIST]":"vervang alle [ITEM1] door [ITEM2] in [LIST]","_reversed":"omgekeerd","_set [LIST] to array [ARRAY]":"maak [LIST] array [ARRAY]","_set items of [LIST1] to [LIST2]":"maak [LIST1] [LIST2]","_set order of [LIST] to [ORDER]":"maak volgorde van [LIST] [ORDER]"},"ru":{"_# of times [ITEM] appears in [LIST]":"количество [ITEM] в списке [LIST]","_List Tools":"Инструменты Списка","_[LIST] as array":"[LIST] как массив","_[LIST] is empty?":"[LIST] пустой?","_ascending":"нарастающей","_concatenate [LIST1] onto [LIST2]":"объединить список [LIST1] с [LIST2]","_delete all [ITEM] in [LIST]":"удалить все [ITEM] списка [LIST]","_delete items [NUM1] to [NUM2] of [LIST]":"удалить строчки из [NUM1] к [NUM2] списка [LIST]","_descending":"опускающей","_first":"первый","_for each item # [VAR] in [LIST]":"для каждого номера # [VAR] в [LIST]","_for each item value [VAR] in [LIST]":"для каждого номера значение [VAR] в списке [LIST]","_index # [INDEX] of item [ITEM] in [LIST]":"индекс # [INDEX] элемента [ITEM] списка [LIST]","_item [NUM] exists in [LIST]?":"номер [NUM] существует в [LIST]?","_last":"последний","_list [LIST] joined by [STRING]":"список [LIST]к которому присоединяется [STRING]","_order of [LIST] is [ORDER]?":"сортирован список [LIST] по [ORDER]?","_random":"случайный","_randomized":"рандомизированный","_repeat [LIST1] [NUM] times in [LIST2]":"повторить [LIST1] [NUM] раз в [LIST2]","_replace all [ITEM1] with [ITEM2] in [LIST]":"заменить всё [ITEM1] на [ITEM2] списка [LIST]","_reversed":"перевёрнутый","_set [LIST] to array [ARRAY]":"задать список [LIST] на[ARRAY]","_set items of [LIST1] to [LIST2]":"заменить список [LIST1] на [LIST2]","_set order of [LIST] to [ORDER]":"задать сортировку списка [LIST] на [ORDER]"},"tr":{"_List Tools":"Liste Araçları"},"uk":{"_# of times [ITEM] appears in [LIST]":"кількість [ITEM] у [LIST]","_List Tools":"Інструменти для Списків","_[LIST] as array":"[LIST] як масив","_[LIST] is empty?":"[LIST] порожній?","_ascending":"зростання","_concatenate [LIST1] onto [LIST2]":"доєднати [LIST1] до [LIST2]","_delete all [ITEM] in [LIST]":"вилучити усі [ITEM] з [LIST]","_delete items [NUM1] to [NUM2] of [LIST]":"вилучити з [NUM1] до [NUM2] з [LIST]","_descending":"спадання","_first":"перший","_item [NUM] exists in [LIST]?":"[NUM] -ий елемент існує у [LIST]?","_last":"останій","_list [LIST] joined by [STRING]":"список [LIST] з'єднаний через [STRING]","_order of [LIST] is [ORDER]?":"порядок у [LIST] є [ORDER]?","_random":"випадкове","_randomized":"випадковий","_repeat [LIST1] [NUM] times in [LIST2]":"повторити [LIST1] [NUM] разів у [LIST2] ","_replace all [ITEM1] with [ITEM2] in [LIST]":"замінити усі [ITEM1] на [ITEM2] у [LIST]","_reversed":"задом наперед","_set [LIST] to array [ARRAY]":"замінити елементи [LIST] на масив [ARRAY]","_set items of [LIST1] to [LIST2]":"замінити елементи у [LIST1] на [LIST2]","_set order of [LIST] to [ORDER]":"сортувати [LIST] у порядку [ORDER]"},"zh-cn":{"_# of times [ITEM] appears in [LIST]":"[ITEM]在[LIST]中出现的次数","_List Tools":"列表工具","_[LIST] as array":"[LIST]作为数组","_[LIST] is empty?":"[LIST]是空的吗？","_ascending":"正序","_concatenate [LIST1] onto [LIST2]":"将[LIST1]的内容加到[LIST2]的末尾","_delete all [ITEM] in [LIST]":"删除[LIST]中的所有[ITEM]","_delete items [NUM1] to [NUM2] of [LIST]":"删除[LIST]的第[NUM1]到[NUM2]项","_descending":"倒序","_first":"第一个","_for each item # [VAR] in [LIST]":"遍历[LIST]中的每一项的编号[VAR]","_for each item value [VAR] in [LIST]":"遍历[LIST]中的每个内容[VAR]","_index # [INDEX] of item [ITEM] in [LIST]":"[LIST]中第[INDEX]个[ITEM]的编号","_item [NUM] exists in [LIST]?":"[LIST]存在第[NUM]项？","_last":"最后一个","_list [LIST] joined by [STRING]":"使用[STRING]连接列表[LIST]","_order of [LIST] is [ORDER]?":"[LIST]的顺序为[ORDER]？","_random":"随机","_randomized":"打乱","_repeat [LIST1] [NUM] times in [LIST2]":"将[LIST1]重复[NUM]次后加入[LIST2]","_replace all [ITEM1] with [ITEM2] in [LIST]":"将[LIST]中的所有[ITEM1]替换为[ITEM2]","_reversed":"反转","_set [LIST] to array [ARRAY]":"将[LIST]设为数组[ARRAY]","_set items of [LIST1] to [LIST2]":"将[LIST1]中的内容设为[LIST2]","_set order of [LIST] to [ORDER]":"将[LIST]的顺序设为[ORDER]"}});/* end generated l10n code */(function (Scratch) {
  "use strict";

  /* -- SETUP -- */
  const vm = Scratch.vm;
  const runtime = vm.runtime;

  const getVarObjectFromName = function (name, util, type) {
    const stageTarget = runtime.getTargetForStage();
    const target = util.target;
    let listObject = Object.create(null);

    listObject = stageTarget.lookupVariableByNameAndType(name, type);
    if (listObject) return listObject;
    listObject = target.lookupVariableByNameAndType(name, type);
    if (listObject) return listObject;
  };

  class Data {
    getInfo() {
      return {
        id: "lmsData",
        name: Scratch.translate("List Tools"),
        color1: "#ff661a",
        color2: "#f2590d",
        color3: "#e64d00",
        blocks: [
          {
            opcode: "deleteItems",
            blockType: Scratch.BlockType.COMMAND,
            text: Scratch.translate("delete items [NUM1] to [NUM2] of [LIST]"),
            arguments: {
              NUM1: {
                type: Scratch.ArgumentType.NUMBER,
                defaultValue: "1",
              },
              NUM2: {
                type: Scratch.ArgumentType.NUMBER,
                defaultValue: "3",
              },
              LIST: {
                type: Scratch.ArgumentType.STRING,
                menu: "lists",
              },
            },
          },
          {
            opcode: "deleteAllOfItem",
            blockType: Scratch.BlockType.COMMAND,
            text: Scratch.translate("delete all [ITEM] in [LIST]"),
            arguments: {
              ITEM: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "thing",
              },
              LIST: {
                type: Scratch.ArgumentType.STRING,
                menu: "lists",
              },
            },
          },
          {
            opcode: "replaceAllOfItem",
            blockType: Scratch.BlockType.COMMAND,
            text: Scratch.translate(
              "replace all [ITEM1] with [ITEM2] in [LIST]"
            ),
            arguments: {
              ITEM1: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "apple",
              },
              ITEM2: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "banana",
              },
              LIST: {
                type: Scratch.ArgumentType.STRING,
                menu: "lists",
              },
            },
          },
          {
            opcode: "repeatList",
            blockType: Scratch.BlockType.COMMAND,
            text: Scratch.translate("repeat [LIST1] [NUM] times in [LIST2]"),
            arguments: {
              LIST1: {
                type: Scratch.ArgumentType.STRING,
                menu: "lists",
              },
              LIST2: {
                type: Scratch.ArgumentType.STRING,
                menu: "lists",
              },
              NUM: {
                type: Scratch.ArgumentType.NUMBER,
                defaultValue: "3",
              },
            },
          },

          "---",

          {
            opcode: "getListJoin",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("list [LIST] joined by [STRING]"),
            disableMonitor: true,
            arguments: {
              LIST: {
                type: Scratch.ArgumentType.STRING,
                menu: "lists",
              },
              STRING: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: ",",
              },
            },
          },
          {
            opcode: "timesItemAppears",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("# of times [ITEM] appears in [LIST]"),
            disableMonitor: true,
            arguments: {
              ITEM: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "thing",
              },
              LIST: {
                type: Scratch.ArgumentType.STRING,
                menu: "lists",
              },
            },
          },
          {
            opcode: "itemIndex",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("index # [INDEX] of item [ITEM] in [LIST]"),
            disableMonitor: true,
            arguments: {
              INDEX: {
                type: Scratch.ArgumentType.NUMBER,
                defaultValue: "1",
              },
              ITEM: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "thing",
              },
              LIST: {
                type: Scratch.ArgumentType.STRING,
                menu: "lists",
              },
            },
          },

          "---",

          {
            opcode: "listIsEmpty",
            blockType: Scratch.BlockType.BOOLEAN,
            text: Scratch.translate("[LIST] is empty?"),
            disableMonitor: true,
            arguments: {
              LIST: {
                type: Scratch.ArgumentType.STRING,
                menu: "lists",
              },
            },
          },
          {
            opcode: "itemNumExists",
            blockType: Scratch.BlockType.BOOLEAN,
            text: Scratch.translate("item [NUM] exists in [LIST]?"),
            disableMonitor: true,
            arguments: {
              NUM: {
                type: Scratch.ArgumentType.NUMBER,
                defaultValue: "1",
              },
              LIST: {
                type: Scratch.ArgumentType.STRING,
                menu: "lists",
              },
            },
          },
          {
            opcode: "orderIs",
            blockType: Scratch.BlockType.BOOLEAN,
            text: Scratch.translate("order of [LIST] is [ORDER]?"),
            disableMonitor: true,
            arguments: {
              LIST: {
                type: Scratch.ArgumentType.STRING,
                menu: "lists",
              },
              ORDER: {
                type: Scratch.ArgumentType.STRING,
                menu: "orderTypeSort",
              },
            },
          },

          "---",

          {
            opcode: "orderList",
            blockType: Scratch.BlockType.COMMAND,
            text: Scratch.translate("set order of [LIST] to [ORDER]"),
            disableMonitor: true,
            arguments: {
              LIST: {
                type: Scratch.ArgumentType.STRING,
                menu: "lists",
              },
              ORDER: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "reversed",
                menu: "orderType",
              },
            },
          },
          {
            opcode: "setListToList",
            blockType: Scratch.BlockType.COMMAND,
            text: Scratch.translate("set items of [LIST1] to [LIST2]"),
            arguments: {
              LIST1: {
                type: Scratch.ArgumentType.STRING,
                menu: "lists",
              },
              LIST2: {
                type: Scratch.ArgumentType.STRING,
                menu: "lists",
              },
            },
          },
          {
            opcode: "joinLists",
            blockType: Scratch.BlockType.COMMAND,
            text: Scratch.translate("concatenate [LIST1] onto [LIST2]"),
            arguments: {
              LIST1: {
                type: Scratch.ArgumentType.STRING,
                menu: "lists",
              },
              LIST2: {
                type: Scratch.ArgumentType.STRING,
                menu: "lists",
              },
            },
          },

          "---",

          {
            opcode: "forEachListItem",
            blockType: Scratch.BlockType.LOOP,
            text: Scratch.translate("for each item value [VAR] in [LIST]"),
            hideFromPalette:
              !runtime.extensionManager.isExtensionLoaded("lmsTempVars2"),
            arguments: {
              VAR: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "thread variable",
              },
              LIST: {
                type: Scratch.ArgumentType.STRING,
                menu: "lists",
              },
            },
          },
          {
            opcode: "forEachListItemNum",
            blockType: Scratch.BlockType.LOOP,
            text: Scratch.translate("for each item # [VAR] in [LIST]"),
            hideFromPalette:
              !runtime.extensionManager.isExtensionLoaded("lmsTempVars2"),
            arguments: {
              VAR: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "thread variable",
              },
              LIST: {
                type: Scratch.ArgumentType.STRING,
                menu: "lists",
              },
            },
          },

          "---",

          {
            opcode: "setListArray",
            blockType: Scratch.BlockType.COMMAND,
            text: Scratch.translate("set [LIST] to array [ARRAY]"),
            disableMonitor: true,
            arguments: {
              LIST: {
                type: Scratch.ArgumentType.STRING,
                menu: "lists",
              },
              ARRAY: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: '["apple","banana"]',
              },
            },
          },
          {
            opcode: "getListArray",
            blockType: Scratch.BlockType.REPORTER,
            text: Scratch.translate("[LIST] as array"),
            disableMonitor: true,
            arguments: {
              LIST: {
                type: Scratch.ArgumentType.STRING,
                menu: "lists",
              },
            },
          },
        ],
        menus: {
          operator: {
            acceptReporters: false,
            items: [
              {
                text: "=",
                value: "=",
              },
              {
                text: ">",
                value: ">",
              },
              {
                text: "<",
                value: "<",
              },
            ],
          },
          orderType: {
            acceptReporters: false,
            items: [
              {
                text: Scratch.translate("reversed"),
                value: "reversed",
              },
              {
                text: Scratch.translate("ascending"),
                value: "ascending",
              },
              {
                text: Scratch.translate("descending"),
                value: "descending",
              },
              {
                text: Scratch.translate("randomized"),
                value: "randomised",
              },
            ],
          },
          orderTypeSort: {
            acceptReporters: false,
            items: [
              {
                text: Scratch.translate("ascending"),
                value: "ascending",
              },
              {
                text: Scratch.translate("descending"),
                value: "descending",
              },
            ],
          },
          indexType: {
            acceptReporters: false,
            items: [
              {
                text: Scratch.translate("first"),
                value: "first",
              },
              {
                text: Scratch.translate("last"),
                value: "last",
              },
              {
                text: Scratch.translate("random"),
                value: "random",
              },
            ],
          },
          lists: {
            acceptReporters: true,
            items: "_getLists",
          },
        },
      };
    }

    deleteItems(args, util) {
      const list = getVarObjectFromName(
        Scratch.Cast.toString(args.LIST),
        util,
        "list"
      );
      if (!list) return false;
      const listLength = list.value.length;
      let num1 = 0;
      let num2 = 0;
      if (!list) return;
      if (args.NUM1 > args.NUM2) {
        num1 = args.NUM2 - 1;
        num2 = args.NUM1 - 1;
      } else {
        num1 = args.NUM1 - 1;
        num2 = args.NUM2 - 1;
      }
      const listPart1 = list.value.slice(0, num1);
      const listPart2 = list.value.slice(num2 + 1, listLength);
      list.value = listPart1.concat(listPart2);
    }

    deleteAllOfItem(args, util) {
      const list = getVarObjectFromName(
        Scratch.Cast.toString(args.LIST),
        util,
        "list"
      );
      if (!list) return;
      const newList = list.value.filter(function (model) {
        return model !== args.ITEM;
      });
      list.value = newList;
    }

    replaceAllOfItem(args, util) {
      const list = getVarObjectFromName(
        Scratch.Cast.toString(args.LIST),
        util,
        "list"
      );
      if (!list) return;
      const listLength = list.value.length;
      const item1 = args.ITEM1;
      const item2 = args.ITEM2;
      let newList = [];
      for (let i = 0; i < listLength; i++) {
        if (list.value[i] === item1) {
          newList.push(item2);
        } else {
          newList.push(list.value[i]);
        }
      }
      list.value = newList;
    }

    repeatList(args, util) {
      const list1 = getVarObjectFromName(
        Scratch.Cast.toString(args.LIST1),
        util,
        "list"
      );
      if (!list1) return;
      const list2 = getVarObjectFromName(
        Scratch.Cast.toString(args.LIST2),
        util,
        "list"
      );
      if (!list2) return;
      const currentVal = list1.value;
      for (let i = 0; i < args.NUM; i++) {
        list2.value = list2.value.concat(currentVal);
      }
    }

    getListJoin(args, util) {
      const list = getVarObjectFromName(
        Scratch.Cast.toString(args.LIST),
        util,
        "list"
      );
      if (!list) return "";
      return list.value.join(args.STRING);
    }

    timesItemAppears(args, util) {
      const list = getVarObjectFromName(
        Scratch.Cast.toString(args.LIST),
        util,
        "list"
      );
      if (!list) return 0;
      return list.value.filter((model) => model == args.ITEM).length;
    }

    itemIndex(args, util) {
      const list = getVarObjectFromName(
        Scratch.Cast.toString(args.LIST),
        util,
        "list"
      );
      if (!list) return 0;
      let indexes = [];
      for (let index = 0; index < list.value.length; index++) {
        if (list.value[index] === args.ITEM) {
          indexes.push(index);
        }
      }

      switch (args.INDEX) {
        case "_first_":
          return Scratch.Cast.toNumber(indexes[0] + 1);
        case "_last_":
          return Scratch.Cast.toNumber(indexes[indexes.length - 1] + 1);
        case "_random_":
          return Scratch.Cast.toNumber(
            indexes[Math.floor(Math.random() * indexes.length)] + 1
          );
        default:
          return Scratch.Cast.toNumber(indexes[args.INDEX - 1] + 1);
      }
    }

    listIsEmpty(args, util) {
      const list = getVarObjectFromName(
        Scratch.Cast.toString(args.LIST),
        util,
        "list"
      );
      if (!list) return true;
      if (list.value.length > 0) return false;
      return true;
    }

    itemNumExists(args, util) {
      const list = getVarObjectFromName(
        Scratch.Cast.toString(args.LIST),
        util,
        "list"
      );
      if (!list) return false;
      const listIndex = Scratch.Cast.toListIndex(
        args.NUM,
        list.value.length,
        false
      );
      if (listIndex === Scratch.Cast.LIST_INVALID) return false;
      return true;
    }

    orderIs(args, util) {
      const list = getVarObjectFromName(
        Scratch.Cast.toString(args.LIST),
        util,
        "list"
      );
      if (!list) return false;

      for (let i = 0; i < list.value.length - 1; i++) {
        const compare = Scratch.Cast.compare(list.value[i + 1], list.value[i]);
        if (compare > 0 && args.ORDER === "descending") return false;
        if (compare < 0 && args.ORDER === "ascending") return false;
      }
      return true;
    }

    orderList(args, util) {
      const list = getVarObjectFromName(
        Scratch.Cast.toString(args.LIST),
        util,
        "list"
      );
      if (!list) return;
      if (args.ORDER === "reversed") {
        list.value.reverse();
      } else if (args.ORDER === "randomised") {
        const randomised = list.value
          .map((value) => ({ value, sort: Math.random() }))
          .sort((a, b) => a.sort - b.sort)
          .map(({ value }) => value);
        list.value = randomised;
      } else if (args.ORDER === "ascending") {
        list.value.sort(Scratch.Cast.compare);
      } else if (args.ORDER === "descending") {
        list.value.sort(Scratch.Cast.compare).reverse();
      }
      list._monitorUpToDate = false;
    }

    setListToList(args, util) {
      const list1 = getVarObjectFromName(
        Scratch.Cast.toString(args.LIST1),
        util,
        "list"
      );
      if (!list1) return;
      const list2 = getVarObjectFromName(
        Scratch.Cast.toString(args.LIST2),
        util,
        "list"
      );
      if (!list2) return;

      list1.value = [...list2.value];
    }

    joinLists(args, util) {
      const list1 = getVarObjectFromName(
        Scratch.Cast.toString(args.LIST1),
        util,
        "list"
      );
      if (!list1) return;
      const list2 = getVarObjectFromName(
        Scratch.Cast.toString(args.LIST2),
        util,
        "list"
      );
      if (!list2) return;
      list2.value = list2.value.concat(list1.value);
    }

    forEachListItem(args, util) {
      const list = getVarObjectFromName(
        Scratch.Cast.toString(args.LIST),
        util,
        "list"
      );
      if (!list) return false;
      const listLength = list.value.length;

      const thread = util.thread;
      if (!thread.variables) thread.variables = {};
      const vars = thread.variables;

      if (typeof util.stackFrame.index === "undefined") {
        util.stackFrame.index = 0;
      }

      if (util.stackFrame.index < listLength) {
        let itemIndex = util.stackFrame.index;
        vars[args.VAR] = list.value[itemIndex];
        util.stackFrame.index++;
        return true;
      }
    }

    forEachListItemNum(args, util) {
      const list = getVarObjectFromName(
        Scratch.Cast.toString(args.LIST),
        util,
        "list"
      );
      if (!list) return false;
      const listLength = list.value.length;

      const thread = util.thread;
      if (!thread.variables) thread.variables = {};
      const vars = thread.variables;

      if (typeof util.stackFrame.index === "undefined") {
        util.stackFrame.index = 0;
      }

      if (util.stackFrame.index < listLength) {
        util.stackFrame.index++;
        vars[args.VAR] = util.stackFrame.index;
        return true;
      }
    }

    setListArray(args, util) {
      const list = getVarObjectFromName(
        Scratch.Cast.toString(args.LIST),
        util,
        "list"
      );
      if (!list) return;

      let array;
      try {
        array = JSON.parse(args.ARRAY);
      } catch (error) {
        return;
      }

      if (!Array.isArray(array)) return;
      const newArray = array;
      list.value = newArray;
      list._monitorUpToDate = false;
    }

    getListArray(args, util) {
      const list = getVarObjectFromName(
        Scratch.Cast.toString(args.LIST),
        util,
        "list"
      );
      if (!list) return "";
      return JSON.stringify(list.value);
    }

    _getLists() {
      // @ts-expect-error - Blockly not typed yet
      const lists =
        typeof Blockly === "undefined"
          ? []
          : Blockly.getMainWorkspace()
              .getVariableMap()
              .getVariablesOfType("list")
              .map((model) => model.name);
      if (lists.length > 0) {
        return lists;
      } else {
        return [""];
      }
    }
  }
  Scratch.extensions.register(new Data());
})(Scratch);
