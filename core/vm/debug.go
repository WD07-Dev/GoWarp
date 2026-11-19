package vm

import (
	"fmt"
	"io"
	"os"
	"strings"

	"gowarp/core/project"
)

type debugState struct {
	writer           io.Writer
	windowed, paused bool
}

/*
SetDebugWriter redirects project console messages. Nil restores stderr,
leaving stdout available for the host's structured output.
*/
func (r *Runtime) SetDebugWriter(writer io.Writer) {
	r.debug.writer = writer
}

func (r *Runtime) SetWindowed(windowed bool) {
	r.debug.windowed = windowed
}

func (r *Runtime) Paused() bool {
	return r.debug.paused
}

func (r *Runtime) SetPaused(paused bool) {
	r.debug.paused = paused
}

func (r *Runtime) debugLine(line string) {
	writer := r.debug.writer
	if writer == nil {
		writer = os.Stderr
	}
	_, _ = fmt.Fprintln(writer, line)
}

func (r *Runtime) debugCall(th *thread, b *project.Block, code string, input func(string) (any, error)) (bool, bool, error) {
	code = strings.ReplaceAll(code, "\u200b", "")
	if code == "breakpoint" || code == "tw:debugger;" {
		line := "breakpoint in " + th.target.state.Name
		if r.debug.windowed {
			line += ", paused (F8 resumes)"
			r.debug.paused = true
		}
		r.debugLine(line)
		return r.debug.paused, true, nil
	}
	parts := strings.Split(code, " ")
	if len(parts) != 2 || parts[1] != "%s" || (parts[0] != "log" && parts[0] != "warn" && parts[0] != "error") {
		return false, false, nil
	}
	var value any = ""
	if len(b.Mutation["argumentids"]) > 0 {
		ids, err := procedureStrings(b, "argumentids")
		if err != nil {
			return false, true, err
		}
		if len(ids) > 0 {
			value, err = input(ids[0])
			if err != nil {
				return false, true, err
			}
		}
	}
	line := th.target.state.Name + ": " + text(value)
	if parts[0] != "log" {
		line = parts[0] + ": " + line
	}
	r.debugLine(line)
	return false, true, nil
}
