/*
Package extension is the native Go extension SDK. It depends on neither the
Scratch VM nor the desktop host; a player supplies the Host implementation.
*/
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
	Args   map[string]any
	Fields map[string]string
	State  map[string]any
	Local  map[string]any
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
	Value any
	/*
		Branch names a substack input. With Again, the handler is called again
		after that substack returns, retaining Local and evaluated arguments.
	*/
	Branch string
	Yield  bool
	Again  bool
	// YieldTick waits for the next VM tick, including in warp/turbo mode.
	YieldTick bool
	// ReevaluateInputs refreshes inputs on Again while retaining Local state.
	ReevaluateInputs bool
}

type Handler func(*Context) (Result, error)

type Block struct {
	// Text labels stage monitors; [INPUT] placeholders use the saved monitor parameters.
	Text    string
	Opcode  string
	Kind    string
	Inputs  []string
	Handler Handler
	/*
		BorrowContext reuses call storage. The handler must finish synchronously
		without retaining or returning Context, Args, Fields, or Local.
		State and argument values keep their normal lifetimes.
	*/
	BorrowContext bool
}

type Extension struct {
	/*
		Prepare transforms block graphs before compilation and runtime installation.
		It must be deterministic and independent of runtime state or external I/O.
	*/
	Prepare func(*Project) error
	ID      string
	Blocks  []Block
	Events  map[string]Handler
	State   map[string]any
}

type Factory func() Extension
type Bundle []Factory
