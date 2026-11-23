// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	e "gowarp/core/extension"
	"math"
	"strconv"
	"strings"
)

func New() e.Extension {
	return e.Extension{ID: "SPlooksExpanded", Blocks: []e.Block{
		{Opcode: "SPlooksExpanded_tintSprite", Kind: "command", Inputs: []string{"TARGET", "COLOR"}, Handler: tintSprite},
		{Opcode: "SPlooksExpanded_menu_TARGETS", Kind: "menu", Handler: func(c *e.Context) (e.Result, error) { return e.Result{Value: c.Field("TARGETS")}, nil }},
	}}
}

func tintSprite(c *e.Context) (e.Result, error) {
	target := c.String("TARGET")
	if target != "_myself_" && target != "_stage_" {
		value, err := c.Call("target.list")
		if err != nil {
			return e.Result{}, err
		}
		found := false
		if targets, ok := value.([]any); ok {
			for _, entry := range targets {
				if record, ok := entry.(map[string]any); ok && e.String(record["name"]) == target && e.Number(record["cloneId"]) == 0 && !e.Bool(record["isStage"]) {
					target = e.String(record["id"])
					found = true
					break
				}
			}
		}
		if !found {
			return e.Result{}, nil
		}
	}
	hex := strings.TrimPrefix(c.String("COLOR"), "#")
	color := [3]float64{}
	for i := range color {
		start := min(i*2, len(hex))
		end := min(start+2, len(hex))
		// The source uses parseInt on each pair, accepting a leading hex digit.
		part := hex[start:end]
		valid := 0
		for _, r := range part {
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				break
			}
			valid++
		}
		if valid == 0 {
			color[i] = math.NaN()
		} else {
			n, _ := strconv.ParseUint(part[:valid], 16, 8)
			color[i] = float64(n) / 255
		}
	}
	// The original shader uses only tint RGB; the optional alpha is ignored.
	_, err := c.Call("graphics.tint", color, target)
	return e.Result{}, err
}
