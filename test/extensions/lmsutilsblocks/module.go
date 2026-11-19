package extension

import (
	e "gowarp/core/extension"
	"math"
	"math/rand"
	"strings"
	"unicode/utf16"
)

// This native port covers the utility opcodes used in Dusttrust.
func New() e.Extension {
	x := e.Extension{ID: "lmsutilsblocks"}
	add := func(op string, inputs []string, h e.Handler) {
		x.Blocks = append(x.Blocks, e.Block{Opcode: "lmsutilsblocks_" + op, Kind: "reporter", Inputs: inputs, Handler: h})
	}
	add("menu_trueFalseMenu", nil, func(c *e.Context) (e.Result, error) { return e.Result{Value: c.Field("trueFalseMenu")}, nil })
	add("trueFalseBoolean", []string{"TRUEFALSE"}, func(c *e.Context) (e.Result, error) {
		v := c.String("TRUEFALSE")
		return e.Result{Value: v == "true" || (v == "random" && rand.Float64() > 0.5)}, nil
	})
	add("stringIfElse", []string{"BOOLEAN", "INPUTA", "INPUTB"}, func(c *e.Context) (e.Result, error) {
		if c.Bool("BOOLEAN") {
			return e.Result{Value: c.Args["INPUTA"]}, nil
		}
		return e.Result{Value: c.Args["INPUTB"]}, nil
	})
	add("isClone", nil, func(c *e.Context) (e.Result, error) {
		v, err := c.Call("target.get", "cloneId")
		return e.Result{Value: e.Number(v) != 0}, err
	})
	add("spriteClicked", nil, func(c *e.Context) (e.Result, error) {
		v, err := c.Call("input.mouse")
		if err != nil {
			return e.Result{}, err
		}
		mouse, _ := v.(map[string]any)
		if !e.Bool(mouse["down"]) {
			return e.Result{Value: false}, nil
		}
		v, err = c.Call("target.touching", "_mouse_")
		return e.Result{Value: v}, err
	})
	add("lettersToOf", []string{"STRING", "INPUTA", "INPUTB"}, func(c *e.Context) (e.Result, error) {
		units := utf16.Encode([]rune(c.String("STRING")))
		a := sliceIndex(c.Number("INPUTA")-1, len(units))
		b := sliceIndex(c.Number("INPUTB"), len(units))
		if b < a {
			b = a
		}
		return e.Result{Value: string(utf16.Decode(units[a:b]))}, nil
	})
	add("itemOfFromString", []string{"INPUTA", "INPUTB", "INPUTC"}, func(c *e.Context) (e.Result, error) {
		s, sep := c.String("INPUTB"), c.String("INPUTC")
		parts := strings.Split(s, sep)
		if sep == "" {
			units := utf16.Encode([]rune(s))
			parts = make([]string, len(units))
			for i, u := range units {
				parts[i] = string(utf16.Decode([]uint16{u}))
			}
		}
		n := c.Number("INPUTA") - 1
		v := ""
		if n >= 0 && n < float64(len(parts)) && n == math.Trunc(n) {
			v = parts[int(n)]
		}
		return e.Result{Value: v}, nil
	})
	return x
}
func sliceIndex(n float64, size int) int {
	n = math.Trunc(n)
	if math.IsNaN(n) {
		n = 0
	}
	if n < 0 {
		n += float64(size)
	}
	return int(math.Max(0, math.Min(float64(size), n)))
}
