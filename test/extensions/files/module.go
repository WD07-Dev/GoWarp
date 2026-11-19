// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"fmt"
	e "gowarp/core/extension"
)

func picker(encoding string) e.Handler {
	return func(c *e.Context) (e.Result, error) {
		result, ok := c.Local["result"].(<-chan string)
		if !ok {
			as := encoding
			if as == "" {
				as = c.String("as")
			}
			value, err := c.Call("files.pick", as)
			if err != nil {
				return e.Result{}, err
			}
			result, ok = value.(<-chan string)
			if !ok {
				return e.Result{}, fmt.Errorf("files.pick returned an invalid result")
			}
			c.Local["result"] = result
		}
		select {
		case value := <-result:
			return e.Result{Value: value}, nil
		default:
			return e.Result{YieldTick: true, Again: true}, nil
		}
	}
}

func cancel(c *e.Context) (e.Result, error) {
	_, err := c.Call("files.cancel")
	return e.Result{}, err
}

func New() e.Extension {
	return e.Extension{
		ID: "files",
		Blocks: []e.Block{
			{Opcode: "files_showPicker", Kind: "reporter", Handler: picker("text")},
			{Opcode: "files_showPickerAs", Kind: "reporter", Inputs: []string{"as"}, Handler: picker("")},
			{Opcode: "files_menu_encoding", Kind: "menu", Handler: func(c *e.Context) (e.Result, error) {
				return e.Result{Value: c.Field("encoding")}, nil
			}},
		},
		Events: map[string]e.Handler{"cancel": cancel, "stop": cancel, "flag": cancel, "dispose": cancel},
	}
}
