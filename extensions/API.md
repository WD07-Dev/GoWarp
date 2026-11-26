# Go extension API

[Extension guide](README.md) · [Examples and credits](EXAMPLES.md)

This is the reference for Go code inside `@gowarp-go`. Import the SDK as
`e "gowarp/core/extension"`; all Go examples below use that alias. GoWarp runs
the Go implementation, not the JavaScript implementation, and does not expose
TurboWarp's JS VM objects or browser APIs.

## Contents

| Extension implementation | Topics |
| --- | --- |
| [Minimal extension](#minimal-extension) | A complete JS file with its Go implementation |
| [Registration](#registration) | `Extension`, `Block`, opcodes and constructors |
| [Handler context](#handler-context) | Arguments, state and borrowed storage |
| [Inputs and menus](#mapping-javascript-inputs-and-menus-to-go) | `Args` versus `Fields` |
| [Waiting and continuations](#waiting-and-continuations) | `Result`, yielding and live loop inputs |
| [Lifecycle events](#lifecycle-events) | Startup, ticks, clones and cleanup |
| [Stage reporter monitors](#stage-reporter-monitors) | Reporter values displayed on the stage |
| [Project preparation](#project-preparation) | Deterministic block-graph transforms |
| [Asynchronous example](#fetching-text-without-blocking-the-vm) | HTTP without blocking the VM |

| Host API | Topics |
| --- | --- |
| [Calling conventions](#host-calls) | Arguments, return values and target references |
| [Runtime and options](#runtime-and-options) | Time, FPS and execution settings |
| [Events and input](#events-and-input) | Broadcasts, hats, keyboard and mouse |
| [Project and procedures](#project-and-procedures) | Stored extension data and custom blocks |
| [Targets](#targets) | Sprites, clones, motion and costume metadata |
| [Variables and lists](#variables-and-lists) | IDs, values and list items |
| [Cameras](#cameras) | Whole-stage view and drawable transforms |
| [Canvas and stretch](#canvas-and-stretch) | Display effects and sprite stretching |
| [Drawing](#drawing) | Pen, stamps, clipping, blending and screen splits |
| [Surfaces and triangles](#surfaces-and-triangles) | Offscreen textures and typed vertices |
| [Pen layers](#pen-layers) | Independent pen drawables |
| [Text](#text) | Text skins and layout |
| [Shaders](#shaders) | Kage, compilers and pipelines |
| [Audio](#audio) | Assets, voices and playback control |
| [Files and assets](#files-and-assets) | File selection and costume changes |
| [Desktop host](#desktop-host) | Player commands and host timing |
| [Windows](#windows) | Native windows, properties, coordinates and dragging |

The SDK types are defined in [core/extension/api.go](../core/extension/api.go).
Host namespaces are registered in [extension_api.go](../core/vm/extension_api.go).

## Minimal extension

External Go packages require a pinned module declaration in `@gowarp-config`.
See [External Go libraries](README.md#external-go-libraries) for the syntax,
dependency resolution and platform requirements.

This complete JS file provides the same reporter in TurboWarp and GoWarp:

```js
/*
@gowarp-config {
}

@gowarp-go {
package extension

import e "gowarp/core/extension"

func New() e.Extension {
    return e.Extension{
        ID: "example",
        Blocks: []e.Block{
            {
                Opcode: "example_double",
                Kind: "reporter",
                Inputs: []string{"VALUE"},
                Handler: double,
            },
        },
    }
}

func double(c *e.Context) (e.Result, error) {
    return e.Result{Value: c.Number("VALUE") * 2}, nil
}
}
*/

(function (Scratch) {
    class Example {
        getInfo() {
            return {
                id: "example",
                name: "Example",
                blocks: [{
                    opcode: "double",
                    blockType: Scratch.BlockType.REPORTER,
                    text: "double [VALUE]",
                    arguments: {
                        VALUE: {
                            type: Scratch.ArgumentType.NUMBER,
                            defaultValue: 10
                        }
                    }
                }]
            };
        }

        double(args) {
            return Scratch.Cast.toNumber(args.VALUE) * 2;
        }
    }

    Scratch.extensions.register(new Example());
})(Scratch);
```

The JS block uses `double`; the Go registration uses the full stored opcode, `example_double`.

## Registration

`New()` returns an `e.Extension`:

| Field | Purpose |
| --- | --- |
| `ID string` | Must match the project's extension ID. Must be nonempty, unique, contain no underscore, and not use a built-in namespace. |
| `Blocks []e.Block` | Block registrations. |
| `Events map[string]e.Handler` | Lifecycle handlers. |
| `State map[string]any` | State shared by this extension instance. |
| `Prepare func(*e.Project) error` | Optional deterministic block-graph transform; see [Project preparation](#project-preparation). |

Each `e.Block` registers one stored opcode:

| Field | Purpose |
| --- | --- |
| `Opcode string` | Full stored opcode, such as `example_double`. |
| `Kind string` | `command`, `reporter`, `boolean`, `menu`, `branch`, `terminal`, or `hat`. |
| `Inputs []string` | Evaluated input names, in evaluation order. Excludes field-only dropdowns. |
| `Handler e.Handler` | Required except for hats started without a predicate. |
| `Text string` | Optional [monitor label](#stage-reporter-monitors); `[INPUT]` placeholders use saved parameters. |
| `BorrowContext bool` | Optional storage reuse for [synchronous handlers](#borrowed-contexts). |

A completed `terminal` block ends its current script. Predicate hats used with
`events.pollHats` need a handler that returns a boolean value.

Declare evaluated inputs in `Inputs`, in their intended evaluation order. Register menu reporters as well as ordinary blocks when the project uses them.

Return a fresh instance from each `New()` call. Allocate mutable maps and slices there rather than in package globals. Validation calls `New()` but does not run block handlers or lifecycle handlers; keep constructors limited to registration and state initialization.

## Handler context

A handler has the signature `func(*e.Context) (e.Result, error)`.

| Member | Meaning |
| --- | --- |
| `c.Args` | Evaluated input values, keyed by input name. |
| `c.Fields` | Block field strings, keyed by field name. |
| `c.Number(name)`, `c.String(name)`, `c.Bool(name)` | Read an input using Scratch-compatible conversions. |
| `c.Field(name)` | Read a field string. |
| `c.State` | Shared extension state, including across sprites and clones. |
| `c.Local` | State for this invocation; retained while the invocation is suspended. |
| `c.Call(operation, args...)` | Call a host operation; returns `(any, error)`. |

The SDK also exports `Number`, `String`, `Bool`, `Compare`, and `Clamp` for values outside `c.Args`. Returning a handler error reports an execution error to the player.

### Borrowed contexts

Set `BorrowContext: true` on an `e.Block` to reuse invocation storage for a
synchronous handler. The handler must not retain or return the context or its
`Args`, `Fields`, or `Local` maps, including through closures or goroutines.
Copy anything needed after return into extension-owned storage. Values obtained
from arguments and `State` keep their normal lifetimes. The handler may modify
call maps; the next invocation receives fresh inputs, fields, and empty local
state. Nested invocations use separate storage.

Borrowed handlers must return without `Again`, `Yield`, `YieldTick`, or
`Branch`; violating this contract reports an error. Their input reporters may
still suspend before the handler runs. Leave this option unset for asynchronous
handlers or code that keeps invocation data. Those handlers retain independent
contexts and normal continuation behavior. Lifecycle event contexts are not
borrowed.

## Mapping JavaScript inputs and menus to Go

TurboWarp passes both ordinary inputs and dropdown selections through JS
`args`. In GoWarp, read them according to their SB3 representation:

| JS argument definition | SB3 storage | Go handler access | Include in `e.Block.Inputs`? |
| --- | --- | --- | --- |
| Ordinary string, number, or boolean input | The block's `inputs` | `c.Args[name]`, or `c.String`, `c.Number`, `c.Bool` | Yes |
| Menu with `acceptReporters: false` (or omitted) | The block's `fields` | `c.Field(argumentName)` | No |
| Menu with `acceptReporters: true` | An input referencing a menu shadow or a connected reporter | `c.Args[argumentName]`, or a conversion helper | Yes |

For a reporter-enabled menu, also register `extensionID_menu_menuName` with
`Kind: "menu"`. Its handler returns `e.Result{Value: c.Field(menuName)}`.
The menu shadow stores its selection under the **menu name**; the parent block
reads the evaluated result under the **argument name**. These names may differ.
A field-only dropdown needs no separate menu reporter.

For example, these entries inside JS `getInfo()` define one field dropdown and
one ordinary input:

```js
id: "example",
blocks: [{
    opcode: "setData",
    blockType: Scratch.BlockType.COMMAND,
    text: "set [FIELD] to [VALUE]",
    arguments: {
        FIELD: { type: Scratch.ArgumentType.STRING, menu: "properties" },
        VALUE: { type: Scratch.ArgumentType.STRING, defaultValue: "Hello" }
    }
}],
menus: {
    properties: { acceptReporters: false, items: ["details", "state"] }
}
```

In `New()`, initialize `State: map[string]any{}` and register this block:

```go
e.Block{
    Opcode: "example_setData",
    Kind: "command",
    Inputs: []string{"VALUE"},
    Handler: func(c *e.Context) (e.Result, error) {
        c.State[c.Field("FIELD")] = c.String("VALUE")
        return e.Result{}, nil
    },
}
```

Here JS would read `args.FIELD`, but Go must read `c.Field("FIELD")`, not
`c.String("FIELD")` or `c.Field("properties")`. Conversion helpers read only
`Args`; they do not fall back to `Fields`. Tests should use the same `inputs`
and `fields` layout as the saved SB3, rather than putting every value in `Args`.

## Waiting and continuations

| Result field | Effect |
| --- | --- |
| `Value` | Result of a reporter, boolean, or menu block. |
| `Yield` | Give other scripts an execution opportunity. The script may resume in the same tick; warp execution can suppress ordinary yielding. |
| `YieldTick` | Wait until the next VM tick, including in warp or turbo mode. |
| `Again` | Invoke this handler again with the same evaluated inputs and `Local` state. Combine with a yield for waiting. |
| `ReevaluateInputs` | With `Again`, evaluate inputs again on the next continuation while retaining `Local`. Use for loop conditions changed by a branch. |
| `Branch` | Name of the substack to execute. With `Again`, return to the handler after the substack finishes. |

For example, register this command with `Inputs: []string{"SECONDS"}`:

```go
func waitSeconds(c *e.Context) (e.Result, error) {
    value, err := c.Call("runtime.time")
    if err != nil {
        return e.Result{}, err
    }
    now := e.Number(value)
    deadline, waiting := c.Local["deadline"].(float64)
    if !waiting {
        deadline = now + max(0, c.Number("SECONDS"))
        c.Local["deadline"] = deadline
    }
    if now < deadline {
        return e.Result{Again: true, YieldTick: true}, nil
    }
    return e.Result{}, nil
}
```

Do not block a handler with sleeps, long loops, or synchronous network requests. Independent background work must communicate its result back to a later handler invocation. Do not retain or access `Context`, its maps, or the Host from a worker goroutine.

For a complete asynchronous handler, see [Fetching text without blocking the VM](#fetching-text-without-blocking-the-vm).

## Lifecycle events

| Event | When it runs |
| --- | --- |
| `load` | During extension registration. |
| `flag` | Initial extension startup and green-flag restarts. |
| `before-tick` | Once per VM tick, after input/time synchronization and before scripts run (the equivalent of `BEFORE_EXECUTE`). |
| `tick` | Once per VM tick, after the frame and VM time advance. |
| `cancel` | When all running scripts are canceled. |
| `stop` | When the runtime's player-stop operation is called. |
| `clone-created` | Immediately after clone creation; the current target is the new clone. |
| `clone-deleted` | During clone deletion; selected read operations remain available for cleanup. |
| `dispose` | When extensions are closed, including cleanup after partial initialization. Every extension's cleanup is attempted even if another handler returns an error. |

Lifecycle handlers cannot return `Yield`, `YieldTick`, or `Again`. Start a registered hat when ongoing work is needed. Cleanup must tolerate resources that were never initialized. Deleted-target handles cannot be used for later mutations.

## Stage reporter monitors

Visible reporter and boolean monitors saved in the SB3 are evaluated once per VM
tick, after scripts run. Rendering only reads the cached result. Hidden monitors
are not evaluated. Saved `params` are provided as `c.Fields`. Parameters named in
the block's `Inputs` are also passed to `c.Args` with their saved value types.

An optional `Text` field on `e.Block` supplies the monitor label,
for example `Text: "FPS"` or `Text: "timer [TIMER]"`.
Placeholders use saved parameters. Without `Text`, GoWarp displays the opcode.

Reporter monitors can be moved and double-clicked to switch between normal and
large display. Their values cannot be edited, and they have no slider.
The handler and extension state are shared with ordinary block calls; yielding
reporters keep their pending invocation until it completes.

## Project preparation

An extension can register a deterministic block-graph transformation in its Go
implementation. No special configuration directive is needed:

```go
func New() e.Extension {
    return e.Extension{ID: "myExtension", Prepare: prepare}
}

func prepare(p *e.Project) error {
    // Inspect p.Targets and edit their Blocks on this private copy.
    return nil
}
```

`e.Project` and `e.ProjectBlock` expose saved project data, not live VM state.
Only target `Blocks` may change. Target identity, saved script order, variables,
lists, costumes, sounds, extension metadata and settings must remain unchanged.
New blocks must have valid opcodes and block references. Preparation errors
include the responsible extension ID and do not modify the original project.

Callbacks run in bundle order before native compilation and runtime installation,
and again from the untransformed source graph after authoring APIs change code.
They do not run per frame. A callback must produce the same graph from the same
input, without time, randomness, external I/O or runtime state. Do not retain the
project pointer after returning. Global procedure discovery and expansion are
implemented in the [My Blocks+ example](examples/SPmbpCST.js); the engine does not
interpret that extension's storage schema.

## Host calls

Call operations by string name through `c.Call`; the notation in the tables is
not a set of Go methods:

```go
_, err := c.Call("target.set", "x", c.Number("X"))
return e.Result{}, err
```

- Every call returns `(any, error)`. Propagate errors or handle them explicitly.
- `arg?` marks an optional argument. Omit it to use the documented default;
  it does not imply that passing `nil` has the same meaning.
- `ref?` selects a target: omit it or use `"_myself_"` for the current target,
  `"_stage_"` for the stage, or a sprite name for an original sprite. IDs from
  `target.list` and `target.clone` identify individual targets, including clones.
- Target-dependent operations need a valid current target or an explicit reference.
  A lifecycle event does not always have a sprite context.
- JSON strings, maps, slices and typed SDK values are different argument types.
  For example, `window.set` accepts a JSON **string**, `graphics.text.set` takes
  a `map[string]any`, and `graphics.surface.triangle` requires an `e.SurfaceTriangle`.
- Rendering, audio and desktop operations depend on the corresponding player
  services. Do not assume that a headless VM or browser preview provides them.

All calls belong on the VM thread. Background workers must return data to a
later handler invocation rather than calling the host themselves.

### Runtime and options

| Operation and arguments | Result or action |
| --- | --- |
| `runtime.time()` | VM step time in seconds. Stable within a step. |
| `runtime.monotonicTime()` | Current monotonic time in seconds; uses deterministic VM time outside realtime execution. |
| `runtime.frame()`, `runtime.fps()` | Frame count or configured VM FPS. |
| `runtime.extensions()` | Loaded extension IDs, including pen when the project uses it. |
| `runtime.timer()`, `runtime.resetTimer()` | Read or reset the Scratch timer. |
| `runtime.greenFlag()` | Restart the project. |
| `options.get(key)`, `options.set(key, value)` | Runtime options listed below. |

`runtime.fps()` reports the configured VM FPS, not measured display FPS.
Use VM time for ordinary animation and waits. Use monotonic time only when
matching a source extension that samples elapsed wall time. A yield is a
scheduling decision, not a fixed duration in seconds.

| Option key | Value |
| --- | --- |
| `framerate` | Finite number, at most 1000; nonpositive values use 60 FPS. |
| `cloneLimit` | Integer upper bound on the clone count. |
| `username` | String returned by the username reporter. |
| `turbo`, `interpolation`, `fencing`, `miscLimits`, `highQualityPen` | Boolean. `fencing` and `miscLimits` enable the respective limits. |

### Events and input

| Operation and arguments | Result or action |
| --- | --- |
| `events.broadcast(name)` | Start broadcast receivers; return the number of receivers started. |
| `events.startHats(opcode, restart)` | Start registered extension hats; return the number started. |
| `events.pollHats(opcode, mode)` | Evaluate registered hat predicates and return the number of scripts started. Mode: `"edge"` or `"level"`. |
| `input.keys()` | Copy of the host's held-key names. |
| `input.key(key)` | Whether the key is pressed. |
| `input.mouse()` | `map[string]any` with Scratch coordinates `x`, `y`, and boolean `down`. |

For `events.pollHats`, `edge` starts a script on a false-to-true transition;
`level` starts while true without restarting an already-running instance.
Hat handlers must return a boolean `Value` promptly, without yielding or branching.
Their `Local` storage persists per hat block and target for transition detection.

### Project and procedures

| Operation and arguments | Result or action |
| --- | --- |
| `project.storage(id)` | The extension's serialized `extensionStorage` JSON. |
| `procedures.warp()` | Whether the current script is executing in warp mode. |
| `procedures.getParam(name)` | Read a named argument of the current procedure; returns `""` when absent. |
| `procedures.setParam(name, value)` | Update the current procedure's named argument. Requires a running script. |
| `procedures.snapshot()` | Return saved procedure metadata as `[]any` of `{target, name, extensionStorage, blocks}` maps. |
| `procedures.updateCalls(patches)` | Apply an array of `{target, id, mutation, inputs}` records to saved procedure calls, then prepare the graph again. |

`procedures.snapshot()` includes original procedure prototypes and calls in each
record's `blocks`. In `updateCalls`, `target` is a target **index**, not a target
reference, and `id` is the call block ID. A nonempty update batch replaces the
block graph and invalidates compiled execution; avoid resubmitting unchanged
metadata. Use [Prepare](#project-preparation) for transformations known before
runtime installation.

### Targets

| Operation and arguments | Result or action |
| --- | --- |
| `target.list()` | `[]any` of `map[string]any` target records with `id`, `name`, `source`, `cloneId`, and `isStage`. |
| `target.query(query)` | Accepts `extension.TargetQuery` and returns an owned `[]string` of live target IDs in creation order. `Source` selects the sprite family of a target reference (empty means all targets); `ClonesOnly` excludes originals. A nonempty `Variable` filters by variable ID using Scratch equality against `Value`, including stage fallback. Missing variables do not match. `First` stops at the first match. Results reflect current values, independently of draw order. |
| `target.touchingTargets(ids, ref?)` | Tests the target against an explicit `[]string` of candidate IDs in one collision query. Excludes itself, the stage, and hidden candidates; returns false for no eligible candidates. Unknown IDs and unavailable geometry return errors. |
| `target.get(property, ref?)` | Read a target property. |
| `target.set(property, value, ref?)` | Set a supported property. |
| `target.setXY(x, y, ref?)` | Move both coordinates together, with one pen segment and one fencing operation. |
| `target.touching(selector, ref?)` | Touching-object test; requires costume geometry. |
| `target.touchingTarget(candidateRef, ref?)` | Touching test against exactly one resolved target (including a clone ID); excludes self and requires costume geometry. |
| `target.clone(ref?)` | Create a clone and return its ID, or nil when creation is refused. |
| `target.delete(ref?)` | Delete a clone. |
| `target.costumes(ref?)` | `[]any` of maps with `name`, `rotationCenterX`, `rotationCenterY`, and `format`. |
| `target.sounds(ref?)` | Sound names belonging to the selected target, in sound-editor order. |
| `target.costumeIndex(name, ref?)` | Exact costume name lookup; returns a zero-based index or -1. |
| `target.costumeSize(index, ref?)` | Width and height in Scratch costume metadata units; index starts at zero. |

| Properties | Access |
| --- | --- |
| `x`, `y`, `direction`, `size`, `visible`, `rotationStyle`, `layer`, `currentCostume` | Read and write. |
| `id`, `name`, `cloneId`, `source`, `isStage`, `volume` | Read only. |

`currentCostume` and costume lookup indices are zero-based. Setting a costume
explicitly changes the target; sampling a costume as a triangle texture does not.

### Variables and lists

| Operation and arguments | Result or action |
| --- | --- |
| `variable.list(ref?)` | Sorted `[]any` of `{id, name}` maps owned by this target only; no stage fallback. |
| `variable.exists(id, ref?)` | Whether a variable ID exists on the target or stage. Does not build a variable list; an unknown target still returns an error. |
| `variable.get(id, ref?)` | Read a variable. |
| `variable.set(id, value, ref?)`, `variable.change(id, amount, ref?)` | Write or increment a variable. |
| `variable.create(id, name, value, ref?)`, `variable.delete(id, ref?)` | Create or remove a variable. |
| `list.list(ref?)` | Sorted `[]any` of `{id, name}` maps owned by this target only; no stage fallback. |
| `list.get(id, ref?)`, `list.length(id, ref?)` | Copy of list contents or its length. |
| `list.create(id, name, ref?)`, `list.remove(id, ref?)` | Create or remove a list. |
| `list.set(id, values, ref?)` | Replace contents with a `[]any`. |
| `list.append(id, value, ref?)`, `list.clear(id, ref?)` | Append an item or clear contents. |
| `list.item(id, index, ref?)`, `list.delete(id, index, ref?)` | Read or delete an item. |
| `list.insert(id, index, value, ref?)`, `list.replace(id, index, value, ref?)` | Insert or replace an item. |

Variable and list arguments use **IDs**, not display names. List indices start
at one and support Scratch's named indices where applicable. `list.remove`
removes the list itself; `list.delete` removes an item. Use `list.clear` to empty
an existing list. Discovery with `variable.list`/`list.list` is local to the
selected target; ordinary value lookups can fall back to stage-owned data.

### Cameras

| Operation and arguments | Result or action |
| --- | --- |
| `camera.get(key)`, `camera.set(key, value)` | Camera/view `x`, `y`, `zoom`, `direction`, `width`, `height`, or `color`. |
| `camera.dimensions(width, height)` | Change stage dimensions. |
| `camera.transform(name, x, y, scale, rotation)` | Set an immutable drawable camera transform: subtract `(x,y)`, rotate counterclockwise in degrees, then scale. The empty name is the default camera. |
| `camera.bind(name, target?)` | Bind a sprite, clone, render-layer name, or `_all_`. Sprite clones inherit their source binding. |
| `camera.binding(target?)` | Read a target or layer's camera name. |

Whole-stage `camera.get`/`camera.set` controls the view; `camera.dimensions`
changes the logical stage dimensions. Drawable cameras use named transforms
and bindings without replacing logical sprite coordinates. They are independent
of the whole-stage view. Stage dimensions must be in 1–4096 with at most 16M pixels.

### Canvas and stretch

| Operation and arguments | Result or action |
| --- | --- |
| `canvas.get(property)`, `canvas.set(property, value)` | Canvas effects and display properties. |
| `stretch.get(axis)`, `stretch.set(axis, value)` | Read or set `x`/`y` stretch. |

| Canvas property names | Value type |
| --- | --- |
| `blur`, `contrast`, `saturation`, `color shift`, `brightness`, `invert`, `sepia`, `transparency` | Number. |
| `scale`, `scale X`, `scale Y`, `skew X`, `skew Y`, `offset X`, `offset Y`, `rotation` | Number. `scale` aliases `scale X`. |
| `border radius`, `border width`, `renderWidth`, `renderHeight` | Number. |
| `nearest` | Boolean. |
| `border style`, `border color`, `background` | String. |

Filter values follow CSS-style percentages: 100 is neutral for brightness,
contrast, saturation and scale. Property names are case-sensitive.
See [visual_extensions.go](../core/vm/visual_extensions.go) for the numeric slots.

### Drawing

| Operation and arguments | Result or action |
| --- | --- |
| `graphics.penAntialias(enabled)` | Enable or disable pen antialiasing. |
| `graphics.targetID(selector)` | Numeric renderer target ID; distinct from a logical target handle. |
| `graphics.quality(id, value)` | Set target rendering quality, from 1 to 100. |
| `graphics.stamp(selector)` | Stamp the selected target. |
| `graphics.penColor()` | Current target’s pen color as four normalized RGBA values. |
| `graphics.penSize()` | Current target's pen diameter. |
| `graphics.tint([3]float64{r,g,b}, target?)` | Multiply sprite/stamp RGB before color effects; channels are normalized and clamped to 0–1. White removes the tint. |
| `graphics.clipbox(x1, y1, x2, y2)` | Set clipping for the current target; no arguments clears it. |
| `graphics.blend(mode?)` | Read or set `default`, `additive`, or `multiply`. |
| `graphics.maskTarget(selector?)`, `graphics.maskMode(enabled?)` | Read or set the mask target and current target's mask mode. |
| `graphics.resetClipping()` | Reset clipping, blending, and mask mode across targets. |
| `graphics.screenSplits(parts, showOriginal)` | Set an immutable `[]e.ScreenSplit`; each part projects its seam and movement into output pixels. |

Clipping uses WebGL-style pixel truncation and framebuffer origins. These are
shared drawing conventions, independent of any extension ID.

#### Screen splits

A generic screen split can use normalized seam endpoints and logical display-pixel movement:

```go
_, err := c.Call("graphics.screenSplits", []e.ScreenSplit{
    e.NormalizedSplit{TopX: 0.25, BottomX: 0.75, MoveX: 8},
}, false)
```

Other split policies implement `ScreenSplit.Project(width, height, pixelScale)`,
returning `SplitGeometry` in output pixels. Submitted values must be immutable.
The host copies the slice, while each implementation owns its coordinate rules.

### Surfaces and triangles

| Operation and arguments | Result or action |
| --- | --- |
| `graphics.surface.create(name, width, height)` | Create or replace a transparent offscreen render texture. Dimensions are truncated to integers in 1–4096; total offscreen storage is limited to 64M pixels per runtime. |
| `graphics.surface.exists(name)` | Whether a named texture exists. `Scratch Stage` always exists. |
| `graphics.surface.clear(name)` | Clear a named texture's color and depth. |
| `graphics.surface.delete(name)` | Release a named texture. |
| `graphics.surface.reset()` | Release all named textures. |
| `graphics.surface.triangle(destination, e.SurfaceTriangle)` | Draw three vertices to a named surface or `Scratch Stage`, with explicit texture and sampling options. |

#### Triangle data

`e.SurfaceTriangle` contains `Vertices [3]e.TriangleVertex`,
`Texture e.SurfaceTexture`, and the boolean options `Linear` and
`MultiplyTextureAlpha`. Each vertex has float64 fields `X, Y, U, V, R, G, B, A, Z, W`.
The zero texture value draws a solid triangle. Set `Texture.Kind` to `"surface"`
and `Texture.Surface` to a surface name (including an empty name), or use
`Kind: "costume"`, a zero-based `Costume` index, and an optional `Target` selector.
The host copies vertices and captures a costume snapshot without changing the
target's current costume. NaN numeric fields become zero; infinite vertex fields
or nonpositive `W` skip the draw. Colors and depth are explicit: the host does
not apply pen color or extension-specific depth offsets.

Triangle coordinates are centered on the destination, with positive Y pointing
up. UV coordinates start at the top-left and repeat outside 0–1. Colors and
opacity are explicit multipliers, usually 0–1. Z/W is clip depth in −1…1;
W also controls perspective interpolation of color and UV, without moving XY.
Use Z=-1 and W=1 for ordinary 2D drawing.

Pass exactly two arguments: the destination name and an `e.SurfaceTriangle`
value. For example, this handler draws an opaque red triangle on the stage:

```go
func drawTriangle(c *e.Context) (e.Result, error) {
    triangle := e.SurfaceTriangle{
        Vertices: [3]e.TriangleVertex{
            {X: -50, Y: -50, R: 1, A: 1, Z: -1, W: 1},
            {X: 50, Y: -50, R: 1, A: 1, Z: -1, W: 1},
            {X: 0, Y: 50, R: 1, A: 1, Z: -1, W: 1},
        },
    }
    _, err := c.Call("graphics.surface.triangle", "Scratch Stage", triangle)
    return e.Result{}, err
}
```

For textured drawing, set `triangle.Texture` to an `e.SurfaceTexture` as
described above and set each vertex's `U`/`V` coordinates. Set `R`, `G`, `B`,
and `A` to 1 to preserve the sampled color and opacity. A surface cannot sample
itself; missing textures do not draw. Costume sampling does not switch the
target's costume or hide its text. Sampling options are fields on the triangle:

- `Linear`: use linear filtering instead of nearest-neighbor filtering; defaults to false.
- `MultiplyTextureAlpha`: multiply sampled RGB by texture alpha once more; defaults to false.

The stage cannot be created, deleted, or cleared with surface management calls;
use the regular pen clear block to clear stage drawings.
`target.set("currentCostume", index, ref?)` explicitly changes a costume when
an extension needs that side effect.

### Pen layers

| Operation and arguments | Result or action |
| --- | --- |
| `graphics.layer.create(name)` | Create an independent stage-sized pen drawable. `Scratch Stage` exposes the ordinary pen drawable. |
| `graphics.layer.exists(name)` | Test for a registered layer. |
| `graphics.layer.select(name)` / `graphics.layer.selected()` | Select/read the destination for ordinary pen marks and stage-surface triangles. |
| `graphics.layer.bind(name, target?)` | Select this layer whenever the target draws. Empty name removes the binding. |
| `graphics.layer.clear(name)` / `graphics.layer.delete(name)` | Clear or release a layer. The ordinary pen cannot be deleted. |
| `graphics.layer.visible(name, visible?)` | Read or change drawable visibility. |
| `graphics.layer.quality(name, scale)` | Set GPU raster density between 0.25 and 4; CPU sensing keeps logical stage resolution. |
| `graphics.layer.placement(name, placement)` | Pin `below` or `above` sprites, or use `unlocked` ordering among sprites. |
| `graphics.layer.move(name, side, target?)` | Move an unlocked layer to the `front` or `back` of a target. |

Layers share the asset library but retain independent pen buffers and clears.
Ordinary sprites and layer drawables preserve their order. Aggregate layer
storage is limited to 64M logical pixels. Bind a [drawable camera](#cameras)
to a layer when its drawing needs an independent transform.

### Text

| Operation and arguments | Result or action |
| --- | --- |
| `graphics.text.get()`, `graphics.text.set(properties)` | Read a `map[string]any`, or update the target's text skin using a property map. |

| Text properties | Value type |
| --- | --- |
| `text`, `font`, `color`, `outlineColor` | String. |
| `visible`, `screenWidth` | Boolean. |
| `width`, `outlineWidth`, `fontSize`, `lineHeight`, `lineHeightDivisor`, `paddingY`, `anchorY`, `align` | Number. |

Layout dimensions use costume units; `align` is 0 for left, 0.5 for center, and 1 for right.
`screenWidth` keeps wrapping width independent of the sprite's horizontal scale.
The generic default font size is 16; line height defaults to the font size and its divisor to 1.
A rational line height (`lineHeight / lineHeightDivisor`) can preserve exact baseline rounding.
`anchorY` sets the costume's vertical rotation center before outline padding.

### Shaders

| Operation and arguments | Result or action |
| --- | --- |
| `graphics.shader.define(name, source, uniforms)` | Register raw Kage source or an `e.ShaderSource` with a compiler. Source is validated only when compiled. |
| `graphics.shader.select(name)`, `graphics.shader.uniform(name, key, value)` | Select a shader or update a uniform. |
| `graphics.shader.pipeline(entries)` | Set an ordered `[]any` of shader names or maps with `name` and `uniforms`. |

Generic shader sources use exact Kage uniform names. A custom `ShaderCompiler`
can translate source and supply a `ShaderProgram.NewUniforms` factory. The renderer
calls the factory once per compiled GPU program to create a `ShaderUniformBinding`.
Each binding can reuse its output map and buffers; they are submitted to the GPU
before the next call. Inputs must not be modified, and separate factory calls
must own separate mutable state. A nil factory passes raw uniforms through.
Its immutable compiler ID and source text form the compilation cache key; use
a unique ID distinct from the built-in `kage` ID. Compilation remains lazy,
so an unused invalid shader does not prevent the project from loading.
Redefining a name updates its selected program and matching pipeline passes,
while preserving pass order and per-pass overrides. Views captured before the
redefinition retain their previous programs.

The desktop renderer retains up to 32 recent compiled programs and 4 MiB of
source text. Programs in the current pipeline are protected even when they
exceed those limits. Inactive programs are retired in least-recently-used order
and compiled again if selected later; VM definitions remain registered.

### Audio

| Operation and arguments | Result or action |
| --- | --- |
| `audio.asset(selector, exactName)` | Opaque sound asset handle, or nil. |
| `audio.asset.get(handle, property)` | `duration`, `rate`, `name`, `format`, or `target`. |
| `audio.projectVolume(value?)` | Read project volume as a percentage, or set it when a value is supplied. |
| `audio.effect(effect, ref?)` | Read the selected target's `pitch` or `pan` sound effect. |
| `audio.targetVoice(selector)` | Current target's voice for the selected sound, or nil. |
| `audio.targetPlay(selector, start, loop, end?)` | Play the selected sound from the current target, optionally stopping at an end position in seconds. An end at or before the start produces an empty segment. |
| `audio.play(asset, start, end, volume, loop, rate?, pan?)` | Play an asset segment; return a voice handle. |
| `audio.url(url)` | Start URL playback; return a voice handle. |
| `audio.voice.get(handle, property)` | `active`, `pending`, `paused`, `position`, `duration`, `loop`, or `clock`. |
| `audio.voice.set(handle, property, value)` | Set `loop`, `rate`, `pan`, `volume`, `position`, `paused`, or `clock`. |
| `audio.voice.stop(handle)`, `audio.voice.dispose(handle)` | Stop or release a voice. |
| `audio.contextPaused(paused)` | Suspend or resume all playback. This is a setter, not a getter. |
| `audio.stopTarget(ref?)` | Stop voices owned by that exact sprite or clone and release scripts waiting for them. `"_myfamily_"` includes the sprite and all its clones. |

Times and positions are in seconds. Treat asset and voice handles as opaque
values. The default voice clock is `"output"`; `"step"` reports timing using
the playback-start VM step. Changing the clock affects reported timing, not
the audio output speed.

Context suspension pauses all playback, including sounds started while
suspended, and stops playback-position advancement. `audio.contextPaused` has
no read form: pass `true` to suspend and `false` to resume.

### Files and assets

| Operation and arguments | Result or action |
| --- | --- |
| `files.pick(encoding)` | Receive-only `<-chan string` with selected file contents: `"text"` or a data URL. |
| `files.cancel()` | Cancel pending file selection. |
| `assets.snapshot()` | Stage snapshot as a PNG data URL; requires a renderer. |
| `assets.addCostume(url, name)` | Add and select a costume on the current target; requires a renderer. |
| `assets.deleteCostume(name)` | Remove a costume by name while retaining at least one costume. |

Poll file-picker channels without blocking, using `Local` and
`Again`/`YieldTick`. Cancellation or a read failure produces an empty string.

### Desktop host

| Operation and arguments | Result or action |
| --- | --- |
| `host.fullscreen()`, `host.projectFullscreen()` | Actual fullscreen state or the project's fullscreen request state. |
| `host.frameMilliseconds()` | Host frame duration in milliseconds. |
| `host.window(property)` | Read `screenWidth`, `screenHeight`, `focused`, or `title`. Requires a desktop window; screen dimensions use device-independent pixels. |
| `host.command(kind, value)` | Queue a `title`, `close`, `open-url`, or `fullscreen` request. |

Project fullscreen requests are tracked separately from the player's F11 mode.
`host.window` screen dimensions use device-independent pixels. For native
multiwindow placement, use `window.monitor` instead.

### Windows

The desktop player provides `main` in ordinary single-window mode. Creating
additional windows requires Windows multiwindow mode. Unsupported operations
or properties return an error. These calls control native windows; browser
simulation belongs to the [GoWarp Windows example](examples/gowarpWindows.js).

| Operation and arguments | Result or action |
| --- | --- |
| `window.list()` | Return window IDs as `[]string`; includes `main` in ordinary single-window mode. |
| `window.get(id)` | Return JSON window properties. |
| `window.state(id, state)` | Return a boolean for `open`, `minimized`, `maximized` or `focused`. Reads actual OS state; missing or closed IDs return `false`. Hidden windows remain open. Maximized is distinct from fullscreen. |
| `window.create(json)` | Create an additional window; requires native multiwindow on Windows. |
| `window.monitor(id)` | Return JSON monitor bounds (`x`, `y`, `width`, `height`) and usable work area (`workX`, `workY`, `workWidth`, `workHeight`) in physical desktop pixels. Uses the monitor nearest the named native window. Requires Windows multiwindow mode. |
| `window.center(id, all)` | Center one native window (`all=false`) or the visible window group (`all=true`) in that monitor's work area. Includes borders, preserves client sizes and group spacing. Requires Windows multiwindow mode. |
| `window.startDrag(id)` | Start native mouse dragging, including borderless windows. Requires Windows multiwindow mode. Returns without waiting for the drag to finish. |
| `window.move(id, x, y)` | Move the outer window using the coordinate units below. |
| `window.viewport(id, x, y, width?, height?)` | Move a native window’s displayed stage region (top-left origin, Y downward). Supply both dimensions to resize it, or neither to preserve its size. Requires native multiwindow. |
| `window.set(id, json)` | Change only the supplied properties, including frame, buttons, fullscreen and opacity. |
| `window.remove(id)` | Explicitly remove a window, even when `disableClose` is enabled. |

#### Coordinate systems

| Values | Coordinate system |
| --- | --- |
| Native multiwindow `x`, `y`, `width`, `height` | Physical desktop pixels. `x`/`y` locate the **outer window**; width/height describe the **client area**, excluding borders. |
| Ordinary single-window `x`, `y`, `width`, `height` | Ebitengine's monitor-relative device-independent pixels. |
| `window.monitor` bounds and work area | Physical desktop pixels. Secondary monitors can have negative coordinates. |
| `stageX`, `stageY`, `stageWidth`, `stageHeight` | Logical stage units, measured from the top-left with Y increasing downward. Independent of the window's desktop position. |

#### JSON properties

Use a JSON string with `window.create` or `window.set`. Creation requires `id`,
`width`, `height`, `stageWidth` and `stageHeight`. Omitted booleans default to
false, `frame` defaults to `borderless`, and opacity defaults to 1.
`window.set` preserves omitted properties and cannot change the ID.

| Property | Type and meaning |
| --- | --- |
| `id` | Unique, nonempty string, at most 128 bytes. Immutable after creation. |
| `title` | Window title string. |
| `x`, `y` | Integer outer-window position. Native range: −100000 to 100000. |
| `width`, `height` | Integer client-area dimensions, 1–4096. |
| `stageX`, `stageY` | Numeric origin of the displayed stage region. |
| `stageWidth`, `stageHeight` | Positive numeric region dimensions, at most 16384. |
| `frame` | `bordered`, `bordered-locked`, or `borderless`; see below. |
| `resizable` | Allow user resizing. |
| `disableMinimize`, `disableMaximize`, `disableClose` | Disable the respective user controls. Explicit API removal remains allowed. |
| `fullscreen`, `topmost` | Fullscreen mode or always-on-top placement. |
| `hidden` | Hide the native window without removing it. Requires multiwindow mode. |
| `passthrough` | Pass mouse input through the entire window. |
| `opacity` | Whole-window opacity, 0–1. This is separate from per-pixel background transparency. |

Native multiwindow supports at most 16 windows and a packed render atlas of
at most 4096×4096 pixels. Ordinary single-window mode cannot change the stage
viewport or use `bordered-locked`. Native visibility/state is read through
`window.state`, not written through JSON.

Each native viewport uses the player's screen fit setting. `preserve` centers
its stage region without stretching; `integer` uses integer scaling only while
that window is fullscreen. Letterbox margins use the window background color;
empty stage pixels remain transparent. Antialiasing applies to all views.
Player controls, when enabled, appear only in `main`, above its stage region.
Mouse coordinates account for both the fitted region and the controls. The
player display-FPS limit also applies; viewport changes redraw immediately.

For example, inside a handler:

```go
_, err := c.Call("window.create", `{
    "id": "side", "title": "Side view",
    "x": 800, "y": 200, "width": 320, "height": 240,
    "stageX": 240, "stageY": 0, "stageWidth": 240, "stageHeight": 180,
    "frame": "borderless", "topmost": true
}`)
return e.Result{}, err
```

#### Frames and position preservation

Window frames use a single `frame` property:

| Value | Native title-bar/system-menu movement | `window.startDrag(id)` |
| --- | --- | --- |
| `bordered` | Allowed | Allowed |
| `bordered-locked` | Blocked | Allowed: the project chooses the drag area |
| `borderless` | No title bar | Allowed |

`bordered-locked` blocks ordinary title-bar dragging and the system menu's Move
command. An explicit `window.startDrag` still works, so a project can provide
its own drag handle. The lock is restored when that drag finishes.
`window.move` and `window.center` work in all three frame modes; resizing and
maximize buttons are independent settings.

In Windows multiwindow mode, changing `frame` preserves the client area's
desktop position on each axis whose `x` or `y` is omitted. Explicit coordinates
take precedence, even when equal to the previous coordinates. A later
`window.move` overrides a pending adjustment. Fullscreen transitions retain
their normal placement rules.

#### Native dragging

Call `window.startDrag(id)` on a primary mouse-button press in a project-defined
drag area. The call returns immediately; Windows handles movement until release
while scripts and rendering continue.

- An unknown ID returns an error.
- No drag starts if the button is up or another drag is active.
- Hidden, unpresented, minimized, maximized, fullscreen and mouse-passthrough
  windows cannot start a drag.
- `startDrag` requires Windows multiwindow mode, including for `main`.

#### Centering

Use `window.center("main", true)` to center the visible group together,
preserving spacing. Use `false` to center only the named window.

- Group centering skips hidden, minimized, maximized and fullscreen windows.
- Centering a minimized, maximized or fullscreen window individually returns
  an error; restore it first.
- A group larger than the work area is aligned to its top-left, not shrunk.

#### Browser preview

The [GoWarp Windows example](examples/gowarpWindows.js) simulates windows in the
browser. Its create block defaults to `bordered`; raw `window.create` defaults
to `borderless`.

| Feature | Browser behavior |
| --- | --- |
| Positions and frames | Stored simulated values; browser decorations do not change. |
| Dragging | Validates the ID but does not move the browser or simulated windows. |
| Monitor information | Uses the browser's current screen and available area, converting CSS pixels with `devicePixelRatio`. It cannot determine a simulated window's actual monitor or native border thickness. |
| `open` state | Whether the simulated window exists. |
| `focused` state | Browser focus for the visible `main` view. |
| `minimized` / `maximized` state | Always false; there are no native windows. |

Exported Windows placement is authoritative. Closing the last native window
exits the player, so no script can run afterward to observe that closure.

## Fetching text without blocking the VM

The following is a complete Go implementation to place inside `@gowarp-go`.
The JS extension must use ID `webtext` and expose reporter `fetch` with a string
input named `URL`. It needs no external modules.

```go
package extension

import (
    "context"
    "fmt"
    "io"
    "net/http"
    "time"

    e "gowarp/core/extension"
)

type fetchResult struct {
    text string
    err  error
}

func New() e.Extension {
    client := &http.Client{Timeout: 10 * time.Second}
    var pending context.Context
    var cancel context.CancelFunc

    cancelPending := func(c *e.Context) (e.Result, error) {
        if cancel != nil {
            cancel()
            pending, cancel = nil, nil
        }
        client.CloseIdleConnections()
        return e.Result{}, nil
    }

    fetch := func(c *e.Context) (e.Result, error) {
        done, started := c.Local["done"].(chan fetchResult)
        if !started {
            if pending == nil {
                pending, cancel = context.WithCancel(context.Background())
            }
            url := c.String("URL")
            done = make(chan fetchResult, 1)
            c.Local["done"] = done
            go func(ctx context.Context, url string, out chan<- fetchResult) {
                out <- fetchText(ctx, client, url)
            }(pending, url, done)
        }

        select {
        case result := <-done:
            delete(c.Local, "done")
            return e.Result{Value: result.text}, result.err
        default:
            return e.Result{Again: true, YieldTick: true}, nil
        }
    }

    return e.Extension{
        ID: "webtext",
        Blocks: []e.Block{
            {
                Opcode: "webtext_fetch",
                Kind: "reporter",
                Inputs: []string{"URL"},
                Handler: fetch,
            },
        },
        Events: map[string]e.Handler{
            "flag": cancelPending,
            "cancel": cancelPending,
            "stop": cancelPending,
            "dispose": cancelPending,
        },
    }
}

func fetchText(ctx context.Context, client *http.Client, url string) fetchResult {
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    if err != nil {
        return fetchResult{err: err}
    }
    response, err := client.Do(req)
    if err != nil {
        return fetchResult{err: err}
    }
    defer response.Body.Close()
    if response.StatusCode < 200 || response.StatusCode >= 300 {
        return fetchResult{err: fmt.Errorf("HTTP status %s", response.Status)}
    }
    const maxBytes = 1 << 20
    body, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
    if err != nil {
        return fetchResult{err: err}
    }
    if len(body) > maxBytes {
        return fetchResult{err: fmt.Errorf("response exceeds 1 MiB")}
    }
    return fetchResult{text: string(body)}
}
```

The first invocation copies the URL and starts the request. Later invocations
poll the channel through `Local`; `Again` preserves the invocation and
`YieldTick` lets other scripts run until the next tick. Do not enable
`BorrowContext` for this block.

Only VM handlers access `Local`, `pending`, and `cancel`. The worker receives a
Go context and a copied URL, uses a shared HTTP client, and sends one result.
It never accesses `e.Context`, its maps, or the host API. Each invocation has its
own channel, so concurrent scripts and clones cannot consume each other's results.
The one-element buffer lets the worker finish even if its script stops polling.

Green-flag restarts, cancellation of all scripts, player stop, and disposal
cancel outstanding requests without waiting for workers to exit. Cleanup can run
more than once; a subsequent request creates a new cancellation context. A single
script ending or a clone being deleted does not trigger the global `cancel`
event. Its abandoned request is bounded by the client's ten-second timeout;
this example does not implement per-script cancellation. Late results stay in
the abandoned invocation's channel and cannot overwrite a newer request.

HTTP failures are returned as handler errors, which report an execution error
to the player. If the original JS extension instead returns an empty string or
exposes an error reporter, implement that behavior explicitly. This example
starts one worker per invocation; extensions expecting many simultaneous
requests should also bound concurrency.