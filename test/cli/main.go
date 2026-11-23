package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gowarp/app/compiler"
	"gowarp/app/desktop"
	"gowarp/app/runtimehost"
	"gowarp/core/extension"
	"gowarp/core/project"
	"gowarp/core/vm"
)

var nativeRunner = desktop.Run

// Main runs the shared CLI on the calling main goroutine.
func Main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	code := runExecutable(ctx, nil, os.Args[1:], os.Stdout, os.Stderr)
	cancel()
	os.Exit(code)
}

func runExecutable(ctx context.Context, embedded *compiler.Embedded, args []string, stdout, stderr io.Writer) int {
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	attached, err := compiler.ReadEmbedded(executable)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if attached != nil {
		embedded = attached
	}
	if embedded == nil {
		return run(ctx, args, stdout, stderr)
	}
	directory, err := os.MkdirTemp("", "gowarp-project-*")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, filepath.Base(embedded.Name))
	if err = os.WriteFile(path, embedded.Project, 0600); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	effective := append([]string{path}, embeddedDefaults(embedded.Args, args)...)
	effective = append(effective, args...)
	return run(ctx, effective, stdout, stderr)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, bundles ...extension.Bundle) int {
	flags := flag.NewFlagSet("gowarp", flag.ContinueOnError)
	flags.SetOutput(stderr)
	pack := flags.Bool("pack", false, "package this executable with the project (no Go toolchain needed)")
	output := flags.String("output", "", "standalone executable output path")
	flags.StringVar(output, "o", "", "standalone executable output path")
	window := flags.Bool("window", false, "run in a native window")
	windowFrames := flags.Int("frames", 0, "native updates before closing (0 stays open)")
	mute := flags.Bool("mute", false, "mute native audio while preserving waits")
	screenshot := flags.String("screenshot", "", "save the final native frame as PNG; requires --frames")
	inputPath := flags.String("inputs", "", "JSON input event file; replaces physical project input in window mode")
	frames := flags.Int("headless", -1, "number of virtual frames (0 loads initial state)")
	fps := flags.Float64("fps", 30, "virtual frames per second")
	turbo := flags.Bool("turbo", false, "run loops without normal frame yields")
	sourceOptions := sourceFlags(flags)
	flags.Usage = func() {
		fmt.Fprintln(stdout, "Usage: gowarp project.sb3 (--headless N | --window) [--fps N] [--turbo] [--inputs events.json]\nWindow options: --mute --frames N --screenshot output.png --scale N\nRuntime options: --stage WxH --input \"space@1-2;click:0,-50@3\" --dump state.txt --paced --verbose --sprites --trace-list NAME --uart DIR\nPackage: project.sb3 --pack [-o output] [runtime options]")
	}
	var paths []string
	/*
		flag stops at the first positional argument. Resume parsing after it
		to allow options before or after the project path.
	*/
	for len(args) > 0 {
		if err := flags.Parse(args); err != nil {
			if err == flag.ErrHelp {
				return 0
			}
			return 2
		}
		args = flags.Args()
		if len(args) > 0 {
			paths = append(paths, args[0])
			args = args[1:]
		}
	}
	building := *pack
	if !building && *output != "" {
		fmt.Fprintln(stderr, "output requires --pack")
		return 2
	}
	atSet := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "at" {
			atSet = true
		}
	})
	if atSet {
		if *screenshot == "" || sourceOptions.at <= 0 || (*windowFrames != 0 && *windowFrames != sourceOptions.at) {
			fmt.Fprintln(stderr, "--at requires a screenshot and a positive update matching --frames when both are supplied")
			return 2
		}
		*windowFrames = sourceOptions.at
	}
	headlessSet := false
	scaleSet := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "scale" {
			scaleSet = true
		}
		if f.Name == "headless" {
			headlessSet = true
		}
	})
	effectiveWindow := *window || (building && !headlessSet)
	if err := sourceOptions.validate(effectiveWindow); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if scaleSet && sourceOptions.scale == 0 {
		fmt.Fprintln(stderr, "scale must be positive")
		return 2
	}
	invalidMode := (headlessSet && *frames < 0) || (*window && headlessSet) || (!building && !*window && *frames < 0) || (*windowFrames < 0) || (!effectiveWindow && (*windowFrames != 0 || *screenshot != "" || *mute)) || (*screenshot != "" && *windowFrames == 0)
	if len(paths) != 1 || invalidMode || *fps <= 0 || math.IsNaN(*fps) || math.IsInf(*fps, 0) || (effectiveWindow && (*fps != math.Trunc(*fps) || *fps > 1000)) {
		fmt.Fprintln(stderr, "provide one .sb3 path and either --headless N or --window; window fps must be an integer in 1..1000; screenshot requires positive --frames")
		flags.Usage()
		return 2
	}
	path, err := filepath.Abs(paths[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := compiler.ValidateOutputs(path, nil, sourceOptions.dump, *screenshot); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if building {
		defaults := []string{}
		if headlessSet {
			defaults = append(defaults, "--headless", strconv.Itoa(*frames))
		} else {
			defaults = append(defaults, "--window")
		}
		defaults = append(defaults, "--fps", strconv.FormatFloat(*fps, 'g', -1, 64))
		if *turbo {
			defaults = append(defaults, "--turbo")
		}
		if *mute {
			defaults = append(defaults, "--mute")
		}
		if *windowFrames != 0 {
			defaults = append(defaults, "--frames", strconv.Itoa(*windowFrames))
		}
		if *screenshot != "" {
			defaults = append(defaults, "--screenshot", *screenshot)
		}
		if *inputPath != "" {
			defaults = append(defaults, "--inputs", *inputPath)
		}
		defaults = append(defaults, sourceOptions.defaults()...)
		result, buildErr := compiler.Build(ctx, compiler.Options{
			Project: path,
			Output:  *output,
			Args:    defaults,
		})
		if buildErr != nil {
			fmt.Fprintln(stderr, buildErr)
			return 1
		}
		fmt.Fprintln(stdout, result)
		return 0
	}
	p, err := project.Load(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var events []vm.InputEvent
	if *inputPath != "" {
		events, err = readInputEvents(*inputPath)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	session, err := newSourceSession(sourceOptions, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	started := time.Now()
	runtime := vm.New(p)
	var bundle extension.Bundle
	for _, supplied := range bundles {
		bundle = append(bundle, supplied...)
	}
	if *window {
		runtime, err = nativeRunner(ctx, path, p, desktop.Options{
			Extensions: bundle,
			FPS:        *fps,
			Turbo:      *turbo,
			Mute:       *mute,
			Replay:     *inputPath != "" || sourceOptions.script != "",
			Frames:     *windowFrames,
			Screenshot: *screenshot,
			Scale:      sourceOptions.scale,
			Configure:  session.configure,
			BeforeTick: session.before,
			AfterTick:  session.after,
		}, events)
	} else {
		if len(bundles) > 0 {
			if err = runtime.SetExtensions(bundle); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
		}
		err = runtimehost.Headless(runtime, path, p)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		err = session.configure(runtime)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		err = runtime.QueueEvents(events)
		if err == nil {
			err = session.headless(ctx, runtime, *frames, *fps, *turbo)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if sourceOptions.dump != "" {
		if err := writeDump(runtime, sourceOptions.dump, path); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	session.diagnostics(runtime, time.Since(started))
	result := struct {
		Frames   int              `json:"frames"`
		Done     bool             `json:"done"`
		Targets  []vm.TargetState `json:"targets"`
		Question *vm.Question     `json:"question,omitempty"`
	}{runtime.Frames(), runtime.Done(), runtime.Snapshot(), runtime.Question()}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func readInputEvents(path string) ([]vm.InputEvent, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(absolute)
	if err != nil {
		return nil, fmt.Errorf("input events: %w", err)
	}
	defer file.Close()
	const maxInput = 4 << 20
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maxInput {
		return nil, fmt.Errorf("input events exceed 4 MiB")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxInput+1))
	decoder.DisallowUnknownFields()
	var events []vm.InputEvent
	if err := decoder.Decode(&events); err != nil {
		return nil, fmt.Errorf("input events: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("input events must contain one JSON array")
	}
	return events, nil
}

/*
A caller can switch a standalone game's default execution mode. Other flags
keep normal last-value-wins parsing, including defaults such as fps and turbo.
*/
func embeddedDefaults(defaults, overrides []string) []string {
	mode := ""
	for _, arg := range overrides {
		if arg == "--headless" || strings.HasPrefix(arg, "--headless=") {
			mode = "headless"
		}
		if arg == "--window" || arg == "--window=true" {
			mode = "window"
		}
	}
	if mode == "" {
		return defaults
	}
	var result []string
	for i := 0; i < len(defaults); i++ {
		name, _, hasValue := strings.Cut(defaults[i], "=")
		drop := mode == "headless" && (name == "--window" || name == "--frames" || name == "--screenshot" || name == "--mute" || name == "--scale") || mode == "window" && (name == "--headless" || name == "--paced")
		if drop {
			if (name == "--headless" || name == "--frames" || name == "--screenshot" || name == "--scale") && !hasValue && i+1 < len(defaults) {
				i++
			}
			continue
		}
		result = append(result, defaults[i])
	}
	return result
}
