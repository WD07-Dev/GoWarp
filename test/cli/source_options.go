package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gowarp/app/compiler"
	"gowarp/core/vm"
)

type sourceOptions struct {
	stage, script, dump, trace, uart string
	scale                            float64
	paced, verbose, sprites          bool
	at                               int
	width, height                    float64
}

func sourceFlags(flags *flag.FlagSet) *sourceOptions {
	o := &sourceOptions{at: -1}
	flags.StringVar(&o.stage, "stage", "", "override logical stage size (WxH)")
	flags.Float64Var(&o.scale, "scale", 0, "initial native window scale (default 2)")
	flags.StringVar(&o.script, "input", "", "scripted input in seconds: space@1-2;click:0,-50@3")
	flags.StringVar(&o.dump, "dump", "", "write sorted variables/lists as text")
	flags.StringVar(&o.trace, "trace-list", "", "trace additions to the named list to stderr")
	flags.StringVar(&o.uart, "uart", "", "bridge Terminal buffers through DIR/in and DIR/out")
	flags.BoolVar(&o.paced, "paced", false, "pace headless frames using a cancellable wall-clock wait")
	flags.BoolVar(&o.verbose, "verbose", false, "write runtime diagnostics to stderr")
	flags.BoolVar(&o.sprites, "sprites", false, "write sprite state diagnostics to stderr")
	flags.IntVar(&o.at, "at", -1, "capture screenshot at this native update (alias for --frames)")
	return o
}

func (o *sourceOptions) validate(window bool) error {
	if o.stage != "" {
		size := strings.Split(strings.ToLower(o.stage), "x")
		if len(size) != 2 {
			return fmt.Errorf("stage must be WxH")
		}
		var err error
		o.width, err = strconv.ParseFloat(size[0], 64)
		if err != nil {
			return fmt.Errorf("invalid stage width")
		}
		o.height, err = strconv.ParseFloat(size[1], 64)
		if err != nil {
			return fmt.Errorf("invalid stage height")
		}
		if !finiteOption(o.width) || !finiteOption(o.height) || o.width < 1 || o.height < 1 || o.width > 4096 || o.height > 4096 {
			return fmt.Errorf("stage dimensions must be 1..4096")
		}
	}
	if !finiteOption(o.scale) || o.scale < 0 || o.scale > 16 {
		return fmt.Errorf("scale must be finite in (0,16]")
	}
	if !window && o.scale != 0 {
		return fmt.Errorf("--scale requires window mode")
	}
	if window && o.paced {
		return fmt.Errorf("--paced applies to headless mode; native windows are already paced")
	}
	_, err := parseScript(o.script)
	return err
}

func finiteOption(n float64) bool {
	return !math.IsNaN(n) && !math.IsInf(n, 0)
}

func (o *sourceOptions) defaults() []string {
	var args []string
	for _, p := range []struct {
		name, value string
	}{
		{"stage", o.stage},
		{"input", o.script},
		{"dump", o.dump},
		{"trace-list", o.trace},
		{"uart", o.uart},
	} {
		if p.value != "" {
			args = append(args, "--"+p.name, p.value)
		}
	}
	if o.scale != 0 {
		args = append(args, "--scale", strconv.FormatFloat(o.scale, 'g', -1, 64))
	}
	for _, p := range []struct {
		name  string
		value bool
	}{
		{"paced", o.paced},
		{"verbose", o.verbose},
		{"sprites", o.sprites},
	} {
		if p.value {
			args = append(args, "--"+p.name)
		}
	}
	return args
}

type scriptEvent struct {
	key              string
	click            bool
	x, y, start, end float64
	started          bool
}

func parseScript(spec string) ([]scriptEvent, error) {
	var events []scriptEvent
	if strings.TrimSpace(spec) == "" {
		return events, nil
	}
	for _, item := range strings.Split(spec, ";") {
		parts := strings.Split(item, "@")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid scripted event %q: expected key@seconds[-end] or click:x,y@seconds", item)
		}
		times := strings.Split(parts[1], "-")
		if len(times) > 2 {
			return nil, fmt.Errorf("invalid scripted time %q", parts[1])
		}
		e := scriptEvent{}
		var err error
		e.start, err = strconv.ParseFloat(strings.TrimSpace(times[0]), 64)
		if err != nil || !finiteOption(e.start) || e.start < 0 {
			return nil, fmt.Errorf("invalid scripted start time %q", times[0])
		}
		e.end = e.start
		if len(times) == 2 {
			e.end, err = strconv.ParseFloat(strings.TrimSpace(times[1]), 64)
			if err != nil || !finiteOption(e.end) || e.end < e.start {
				return nil, fmt.Errorf("scripted end must be finite and >= start")
			}
		}
		what := strings.TrimSpace(parts[0])
		if strings.HasPrefix(what, "click:") {
			point := strings.Split(strings.TrimPrefix(what, "click:"), ",")
			if len(point) != 2 {
				return nil, fmt.Errorf("click requires x,y coordinates")
			}
			e.click = true
			e.x, err = strconv.ParseFloat(strings.TrimSpace(point[0]), 64)
			if err != nil || !finiteOption(e.x) {
				return nil, fmt.Errorf("invalid click x coordinate")
			}
			e.y, err = strconv.ParseFloat(strings.TrimSpace(point[1]), 64)
			if err != nil || !finiteOption(e.y) {
				return nil, fmt.Errorf("invalid click y coordinate")
			}
		} else {
			e.key = strings.ToLower(what)
			if e.key == "" {
				return nil, fmt.Errorf("scripted key is empty")
			}
			if e.key == "left" || e.key == "right" || e.key == "up" || e.key == "down" {
				e.key += " arrow"
			}
		}
		events = append(events, e)
	}
	return events, nil
}

type sourceSession struct {
	options        *sourceOptions
	script         []scriptEvent
	keys           map[string]bool
	mouse          bool
	mouseX, mouseY float64
	writer         io.Writer
	uart           *uartBridge
}

func newSourceSession(o *sourceOptions, writer io.Writer) (*sourceSession, error) {
	script, err := parseScript(o.script)
	if err != nil {
		return nil, err
	}
	s := &sourceSession{
		options: o,
		script:  script,
		keys:    map[string]bool{},
		writer:  writer,
	}
	if o.uart != "" {
		path, err := filepath.Abs(o.uart)
		if err != nil {
			return nil, err
		}
		if err = os.MkdirAll(path, 0755); err != nil {
			return nil, err
		}
		s.uart = &uartBridge{directory: path}
	}
	return s, nil
}

func (s *sourceSession) configure(r *vm.Runtime) error {
	r.SetDebugWriter(s.writer)
	if s.options.stage != "" {
		if err := r.SetStageSize(s.options.width, s.options.height); err != nil {
			return err
		}
	}
	if s.options.trace != "" || s.uart != nil {
		r.SetListObserver(func(e vm.ListAppend) {
			if e.List == s.options.trace {
				suffix := ""
				if e.Note != "" {
					suffix = "  // " + e.Note
				}
				fmt.Fprintf(s.writer, "%s: %s += %s%s\n", e.Target, e.List, e.Text, suffix)
			}
			if s.uart != nil && e.Target == "RISCV" && e.List == "Terminal.outputBuffer" {
				value := e.Number
				if !finiteOption(value) {
					value = 0
				}
				s.uart.output = append(s.uart.output, byte(int64(value)))
			}
		})
	}
	return nil
}

func (s *sourceSession) before(r *vm.Runtime) error {
	if len(s.script) == 0 {
		return nil
	}
	now := r.Time()
	keys := map[string]bool{}
	mouse := false
	events := []vm.InputEvent{}
	for i := range s.script {
		e := &s.script[i]
		if now+1e-12 < e.start || now > e.end+.05+1e-12 {
			continue
		}
		if e.click {
			s.mouseX, s.mouseY = e.x, e.y
			mouse = true
			events = append(events, vm.InputEvent{
				Frame: r.Frames(),
				Kind:  "mouse",
				X:     e.x,
				Y:     e.y,
				Down:  true,
			})
			if !e.started {
				events = append(events, vm.InputEvent{
					Frame: r.Frames(),
					Kind:  "click-at",
					X:     e.x,
					Y:     e.y,
				})
			}
		} else {
			keys[e.key] = true
			if !e.started {
				events = append(events, vm.InputEvent{
					Frame: r.Frames(),
					Kind:  "key",
					Key:   e.key,
					Down:  true,
				})
			}
		}
		e.started = true
	}
	for key := range s.keys {
		if !keys[key] {
			events = append(events, vm.InputEvent{
				Frame: r.Frames(),
				Kind:  "key",
				Key:   key,
				Down:  false,
			})
		}
	}
	if s.mouse && !mouse {
		events = append(events, vm.InputEvent{
			Frame: r.Frames(),
			Kind:  "mouse",
			X:     s.mouseX,
			Y:     s.mouseY,
			Down:  false,
		})
	}
	s.keys, s.mouse = keys, mouse
	return r.QueueEvents(events)
}

func (s *sourceSession) after(r *vm.Runtime) error {
	if s.uart != nil {
		return s.uart.pump(r)
	}
	return nil
}

func (s *sourceSession) headless(ctx context.Context, r *vm.Runtime, frames int, fps float64, turbo bool) error {
	if !s.options.paced && len(s.script) == 0 && s.uart == nil {
		return r.Run(ctx, frames, fps, turbo)
	}
	for i := 0; i < frames; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if r.Stopped() {
			break
		}
		if len(s.script) == 0 && s.uart == nil && r.Done() {
			break
		}
		start := time.Now()
		if err := s.before(r); err != nil {
			return err
		}
		if err := r.Tick(ctx, fps, turbo); err != nil {
			return err
		}
		if err := s.after(r); err != nil {
			return err
		}
		if s.options.paced {
			delay := time.Until(start.Add(time.Duration(float64(time.Second) / r.FPS())))
			if delay > 0 {
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
			}
		}
	}
	return nil
}

func writeDump(r *vm.Runtime, path, projectPath string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if err := compiler.ValidateOutputs(projectPath, nil, path); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err = r.WriteDump(f); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	_, err = os.Stat(path)
	return err
}

func (s *sourceSession) diagnostics(r *vm.Runtime, elapsed time.Duration) {
	if s.options.verbose {
		fmt.Fprintf(s.writer, "ran %d frames in %s (done=%v)\n", r.Frames(), elapsed, r.Done())
		_ = r.WriteDump(s.writer)
	}
	if s.options.sprites {
		for _, t := range r.Snapshot() {
			fmt.Fprintf(s.writer, "%s clone=%d x=%g y=%g direction=%g size=%g visible=%v costume=%s\n", t.Name, t.CloneID, t.X, t.Y, t.Direction, t.Size, t.Visible, t.CostumeName)
		}
	}
}

type uartBridge struct {
	directory string
	offset    int64
	output    []byte
}

func (u *uartBridge) pump(r *vm.Runtime) error {
	if len(u.output) > 0 {
		f, err := os.OpenFile(filepath.Join(u.directory, "out"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return err
		}
		n, err := f.Write(u.output)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if n != len(u.output) {
			return io.ErrShortWrite
		}
		u.output = nil
	}
	f, err := os.Open(filepath.Join(u.directory, "in"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() < u.offset {
		u.offset = 0
	}
	if _, err = f.Seek(u.offset, io.SeekStart); err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return err
	}
	r.AppendStageBytes("Terminal.inputBuffer", data)
	u.offset += int64(len(data))
	return nil
}
