// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"encoding/json"
	e "gowarp/core/extension"
	"slices"
	"strconv"
	"strings"
)

func New() e.Extension {
	return e.Extension{ID: "enderKeysPlusV2", State: map[string]any{}, Events: map[string]e.Handler{"tick": func(c *e.Context) (e.Result, error) {
		_, err := c.Call("events.pollHats", "enderKeysPlusV2_eventKeysPressed", "level")
		return e.Result{}, err
	}}, Blocks: []e.Block{
		{Opcode: "enderKeysPlusV2_menu_keys", Kind: "reporter", Handler: func(c *e.Context) (e.Result, error) { return e.Result{Value: c.Field("keys")}, nil }},
		{Opcode: "enderKeysPlusV2_isKeyPressed", Kind: "boolean", Inputs: []string{"key"}, Handler: func(c *e.Context) (e.Result, error) {
			keys, err := heldKeys(c)
			return e.Result{Value: keyPressed(keys, c.String("key"))}, err
		}},
		{Opcode: "enderKeysPlusV2_eventKeysPressed", Kind: "hat", Inputs: []string{"keys"}, Handler: func(c *e.Context) (e.Result, error) {
			keys, err := heldKeys(c)
			if err != nil {
				return e.Result{}, err
			}
			previous, _ := c.Local["keys"].([]string)
			c.Local["keys"] = keys
			ordered := c.Field("ordered") == "together & in order"
			wanted := parseKeys(c.Args["keys"])
			trigger := false
			switch c.Field("trigger") {
			case "while":
				trigger = keysPressed(keys, wanted, ordered)
			case "when":
				for _, key := range keys {
					if !slices.Contains(previous, key) {
						trigger = keysPressed(keys, wanted, ordered)
						break
					}
				}
			case "after":
				for _, key := range previous {
					if !slices.Contains(keys, key) {
						trigger = keysPressed(previous, wanted, ordered)
						break
					}
				}
			}
			return e.Result{Value: trigger}, nil
		}},
		{Opcode: "enderKeysPlusV2_currentKeyPressed", Kind: "reporter", Inputs: []string{}, Handler: func(c *e.Context) (e.Result, error) {
			v, err := c.Call("input.keys")
			if err != nil {
				return e.Result{}, err
			}
			keys := v.([]string)
			if len(keys) == 0 {
				return e.Result{Value: "None"}, nil
			}
			return e.Result{Value: keys[len(keys)-1]}, nil
		}},
	}}
}

func heldKeys(c *e.Context) ([]string, error) {
	value, err := c.Call("input.keys")
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, key := range value.([]string) {
		if mapped, ok := keyNames[key]; ok {
			key = mapped
		} else if strings.HasPrefix(key, "f") {
			if n, err := strconv.Atoi(key[1:]); err == nil && n >= 1 && n <= 12 {
				key = strings.ToUpper(key)
			}
		} else if strings.HasPrefix(key, "numpad") {
			suffix := strings.TrimPrefix(key, "numpad")
			suffix = strings.TrimPrefix(suffix, "digit")
			key = "numpad: " + suffix
		}
		out = append(out, key)
	}
	return out, nil
}

var keyNames = map[string]string{
	"shiftleft": "left shift", "shiftright": "right shift", "controlleft": "left control", "controlright": "right control", "altleft": "left alt", "altright": "right alt", "metaleft": "left windows key", "metaright": "right windows key", "capslock": "caps lock", "numlock": "num lock", "scrolllock": "scroll lock", "pageup": "page up", "pagedown": "page down", "printscreen": "print screen",
}

func keyPressed(keys []string, key string) bool {
	if key == "any" {
		return len(keys) > 0
	}
	if strings.HasPrefix(key, "#") {
		for _, member := range tags[key] {
			if slices.Contains(keys, member) {
				return true
			}
		}
		return false
	}
	return slices.Contains(keys, key)
}
func parseKeys(value any) []string {
	var raw []any
	switch value := value.(type) {
	case []string:
		return append([]string{}, value...)
	case []any:
		raw = value
	default:
		text := e.String(value)
		if !strings.HasPrefix(text, "[") || !strings.HasSuffix(text, "]") {
			text = "[" + text + "]"
		}
		if json.Unmarshal([]byte(text), &raw) != nil {
			return nil
		}
	}
	out := make([]string, len(raw))
	for i, value := range raw {
		out[i] = e.String(value)
	}
	return out
}
func keysPressed(keys, wanted []string, ordered bool) bool {
	if len(wanted) == 0 {
		return false
	}
	if ordered {
		selected := []string{}
		for _, key := range keys {
			if slices.Contains(wanted, key) {
				selected = append(selected, key)
			}
		}
		return strings.Join(selected, ",") == strings.Join(wanted, ",")
	}
	for _, key := range wanted {
		if !keyPressed(keys, key) {
			return false
		}
	}
	return true
}

var tags = map[string][]string{
	"#a":                 {"a", "A"},
	"#b":                 {"b", "B"},
	"#c":                 {"c", "C"},
	"#d":                 {"d", "D"},
	"#e":                 {"e", "E"},
	"#f":                 {"f", "F"},
	"#g":                 {"g", "G"},
	"#h":                 {"h", "H"},
	"#i":                 {"i", "I"},
	"#j":                 {"j", "J"},
	"#k":                 {"k", "K"},
	"#l":                 {"l", "L"},
	"#m":                 {"m", "M"},
	"#n":                 {"n", "N"},
	"#o":                 {"o", "O"},
	"#p":                 {"p", "P"},
	"#q":                 {"q", "Q"},
	"#r":                 {"r", "R"},
	"#s":                 {"s", "S"},
	"#t":                 {"t", "T"},
	"#u":                 {"u", "U"},
	"#v":                 {"v", "V"},
	"#w":                 {"w", "W"},
	"#x":                 {"x", "X"},
	"#y":                 {"y", "Y"},
	"#z":                 {"z", "Z"},
	"#letters":           {"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x", "y", "z", "A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M", "N", "O", "P", "Q", "R", "S", "T", "U", "V", "W", "X", "Y", "Z"},
	"#uppercaseLetters":  {"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M", "N", "O", "P", "Q", "R", "S", "T", "U", "V", "W", "X", "Y", "Z"},
	"#lowercaseLetters":  {"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x", "y", "z"},
	"#vowels":            {"a", "e", "i", "o", "u"},
	"#consonants":        {"b", "c", "d", "f", "g", "h", "j", "k", "l", "m", "n", "p", "q", "r", "s", "t", "v", "w", "x", "y", "z"},
	"#numbers":           {"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"},
	"#specialCharacters": {"!", "@", "#", "$", "%", "^", "&", "*", "(", ")", "_", "+", "=", "{", "}", "[", "]", "|", "\\", ":", ";", "'", "\"", "<", ">", ",", ".", "?", "/", "~", "`"},
	"#shift":             {"left shift", "right shift"},
	"#alt":               {"left alt", "right alt"},
	"#control":           {"left control", "right control"},
	"#windowsKey":        {"left windows key", "right windows key"},
	"#arrowKeys":         {"up arrow", "down arrow", "right arrow", "left arrow"},
	"#functionKeys":      {"F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10", "F11", "F12"},
	"#navigationKeys":    {"up arrow", "down arrow", "right arrow", "left arrow", "home", "end", "page up", "page down", "insert", "delete"},
	"#numpad":            {"numpad: divide", "numpad: multiply", "numpad: subtract", "numpad: add", "numpad: 0", "numpad: 1", "numpad: 2", "numpad: 3", "numpad: 4", "numpad: 5", "numpad: 6", "numpad: 7", "numpad: 8", "numpad: 9", "numpad: decimal", "numpad: enter"},
}
