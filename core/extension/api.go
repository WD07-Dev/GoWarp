// Package extension defines the Go extension SDK. Players implement Host.
package extension

import "fmt"

type Host interface {
	Call(operation string, args ...any) (any, error)
}

// TargetQuery selects live targets in creation order without building property maps.
type TargetQuery struct {
	Source     string // Empty selects all targets; otherwise select this target's sprite family.
	ClonesOnly bool
	Variable   string // Optional variable ID compared with Value using Scratch equality.
	Value      any
	First      bool
}

type Context struct {
	Args   map[string]any    // Evaluated block inputs.
	Fields map[string]string // Literal fields, including menus with acceptReporters: false.
	State  map[string]any    // Shared by handlers in this extension instance.
	Local  map[string]any    // Per invocation; preserved when Result.Again resumes the block.
	Host   Host
}

func (c *Context) Number(name string) float64 {
	return Number(c.Args[name])
}

func (c *Context) String(name string) string {
	return String(c.Args[name])
}

func (c *Context) Bool(name string) bool {
	return Bool(c.Args[name])
}

func (c *Context) Field(name string) string {
	return c.Fields[name]
}

func (c *Context) Call(operation string, args ...any) (any, error) {
	if c.Host == nil {
		return nil, fmt.Errorf("extension host is unavailable")
	}
	return c.Host.Call(operation, args...)
}

/*
Yield gives other scripts a turn; the caller can resume in the same tick.
Warp scripts need not yield unless YieldTick is set. Again repeats this
command with its evaluated arguments and Local state retained.
*/
type Result struct {
	Value            any
	Branch           string
	Yield            bool
	Again            bool
	YieldTick        bool
	ReevaluateInputs bool
}

type Handler func(*Context) (Result, error)

type Block struct {
	Text          string
	Opcode        string
	Kind          string
	Inputs        []string
	Handler       Handler
	BorrowContext bool
}

type Extension struct {
	Prepare func(*Project) error
	ID      string
	Blocks  []Block
	Events  map[string]Handler
	State   map[string]any
}

type Factory func() Extension
type Bundle []Factory
