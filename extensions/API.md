# Go extension API

[Extension guide](README.md) - [Examples and credits](EXAMPLES.md)

Import the SDK as `e "gowarp/core/extension"`. Its public types are defined in [core/extension/api.go](../core/extension/api.go). This reference describes the current implementation; it does not expose TurboWarp's JavaScript VM objects or browser APIs.

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

Each `e.Block` has `Opcode`, `Kind`, `Inputs`, and `Handler`. Supported kinds are `command`, `reporter`, `boolean`, `menu`, `branch`, `terminal`, and `hat`. Every kind except `hat` requires a handler. A completed `terminal` block ends its current script.

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


### Mapping JavaScript inputs and menus to Go

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


### Example: fetching text without blocking the VM

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

## Lifecycle events

| Event | When it runs |
| --- | --- |
| `load` | During extension registration. |
| `flag` | Initial extension startup and green-flag restarts. |
| `tick` | Once per VM tick, after the frame and VM time advance. |
| `cancel` | When all running scripts are canceled. |
| `stop` | When the runtime's player-stop operation is called. |
| `clone-created` | Immediately after clone creation; the current target is the new clone. |
| `clone-deleted` | During clone deletion; selected read operations remain available for cleanup. |
| `dispose` | When extensions are closed, including cleanup after partial initialization. Every extension's cleanup is attempted even if another handler returns an error. |

Lifecycle handlers cannot return `Yield`, `YieldTick`, or `Again`. Start a registered hat when ongoing work is needed. Cleanup must tolerate resources that were never initialized. Deleted-target handles cannot be used for later mutations.

## Host calls

Use an operation string, not a Go method:

```go
_, err := c.Call("target.set", "x", c.Number("X"))
return e.Result{}, err
```

In the tables below, `ref?` means an optional target reference. Omit it or use `"_myself_"` for the current target, `"_stage_"` for the stage, or a sprite name for an original sprite. IDs returned by `target.list` and `target.clone` identify individual targets, including clones.

### Runtime, events, and input

| Operation and arguments | Result or action |
| --- | --- |
| `runtime.time()` | VM step time in seconds. Stable within a step. |
| `runtime.monotonicTime()` | Current monotonic time in seconds; uses deterministic VM time outside realtime execution. |
| `runtime.frame()`, `runtime.fps()` | Frame count or configured VM FPS. |
| `runtime.timer()`, `runtime.resetTimer()` | Read or reset the Scratch timer. |
| `runtime.greenFlag()` | Restart the project. |
| `project.storage(id)` | The extension's serialized `extensionStorage` JSON. |
| `events.broadcast(name)` | Start broadcast receivers; return the number of receivers started. |
| `events.startHats(opcode, restart)` | Start registered extension hats; return the number started. |
| `input.keys()` | Copy of the host's held-key names. |
| `input.key(key)` | Whether the key is pressed. |
| `input.mouse()` | Map with `x`, `y`, and `down`. |

Use VM time for ordinary animation and waits. Use monotonic time only when matching a source extension that samples elapsed wall time. A yield is a scheduling decision, not a fixed duration in seconds.

### Targets, variables, and lists

| Operation and arguments | Result or action |
| --- | --- |
| `target.list()` | Target records with `id`, `name`, `source`, `cloneId`, and `isStage`. |
| `target.get(property, ref?)` | Read a target property. |
| `target.set(property, value, ref?)` | Set a supported property. |
| `target.touching(selector, ref?)` | Touching-object test; requires costume geometry. |
| `target.clone(ref?)` | Create a clone and return its ID, or nil when creation is refused. |
| `target.delete(ref?)` | Delete a clone. |
| `target.costumes(ref?)` | Costume metadata records. |
| `target.costumeIndex(name, ref?)` | Exact costume name lookup; returns a zero-based index or -1. |
| `target.costumeSize(index, ref?)` | Width and height in Scratch costume metadata units; index starts at zero. |
| `variable.get(id, ref?)` | Read a variable. |
| `variable.set(id, value, ref?)`, `variable.change(id, amount, ref?)` | Write or increment a variable. |
| `variable.create(id, name, value, ref?)`, `variable.delete(id, ref?)` | Create or remove a variable. |
| `list.get(id, ref?)`, `list.length(id, ref?)` | Copy of list contents or its length. |
| `list.create(id, name, ref?)`, `list.remove(id, ref?)` | Create or remove a list. |
| `list.set(id, values, ref?)` | Replace contents with a `[]any`. |
| `list.append(id, value, ref?)`, `list.clear(id, ref?)` | Append an item or clear contents. |
| `list.item(id, index, ref?)`, `list.delete(id, index, ref?)` | Read or delete an item. |
| `list.insert(id, index, value, ref?)`, `list.replace(id, index, value, ref?)` | Insert or replace an item. |

Readable target properties: `id`, `x`, `y`, `direction`, `size`, `name`, `cloneId`, `source`, `isStage`, `visible`, `rotationStyle`, `currentCostume`, `volume`, `layer`. Writable properties: `x`, `y`, `direction`, `size`, `visible`, `rotationStyle`, `layer`, `currentCostume`.

Variable and list arguments use IDs, not display names. List indices start at one and support Scratch's named indices where applicable.

### Runtime options, camera, and drawing

| Operation and arguments | Result or action |
| --- | --- |
| `options.get(key)`, `options.set(key, value)` | Runtime options listed below. |
| `camera.get(key)`, `camera.set(key, value)` | Camera/view `x`, `y`, `zoom`, `direction`, `width`, `height`, or `color`. |
| `camera.dimensions(width, height)` | Change stage dimensions. |
| `graphics.penAntialias(enabled)` | Enable or disable pen antialiasing. |
| `graphics.targetID(selector)` | Numeric renderer target ID; distinct from a logical target handle. |
| `graphics.quality(id, value)` | Set target rendering quality, from 1 to 100. |
| `graphics.stamp(selector)` | Stamp the selected target. |
| `graphics.penColor()` | Current target’s pen color as four normalized RGBA values. |
| `graphics.surface.create(name, width, height)` | Create or replace a transparent offscreen render texture. Dimensions are truncated to integers in 1–4096; total offscreen storage is limited to 64M pixels per runtime. |
| `graphics.surface.exists(name)` | Whether a named texture exists. `Scratch Stage` always exists. |
| `graphics.surface.clear(name)` | Clear a named texture's color and depth. |
| `graphics.surface.delete(name)` | Release a named texture. |
| `graphics.surface.reset()` | Release all named textures. |
| `graphics.surface.triangle(destination, extension.SurfaceTriangle)` | Draw three vertices to a named surface or `Scratch Stage`, with explicit texture and sampling options. |
| `graphics.clipbox(x1, y1, x2, y2)` | Set clipping for the current target; no arguments clears it. |
| `graphics.blend(mode?)` | Read or set `default`, `additive`, or `multiply`. |
| `graphics.maskTarget(selector?)`, `graphics.maskMode(enabled?)` | Read or set the mask target and current target's mask mode. |
| `graphics.resetClipping()` | Reset clipping, blending, and mask mode across targets. |
| `graphics.screenSplits(parts, showOriginal)` | Set an immutable `[]extension.ScreenSplit`; each part projects its seam and movement into output pixels. |
| `stretch.get(axis)`, `stretch.set(axis, value)` | Read or set `x`/`y` stretch. |
| `canvas.get(property)`, `canvas.set(property, value)` | Canvas effects and display properties. |
| `graphics.text.get()`, `graphics.text.set(properties)` | Read or update the target's text skin, including explicit layout metrics. |
| `graphics.shader.define(name, source, uniforms)` | Register raw Kage source or an `extension.ShaderSource` with a compiler. Source is validated only when compiled. |
| `graphics.shader.select(name)`, `graphics.shader.uniform(name, key, value)` | Select a shader or update a uniform. |
| `graphics.shader.pipeline(entries)` | Set an ordered `[]any` of shader names or maps with `name` and `uniforms`. |

Runtime option keys: `framerate`, `cloneLimit`, `username`, `turbo`, `interpolation`, `fencing`, `miscLimits`, `highQualityPen`.

Text properties: `text`, `font`, `color`, `outlineColor`, `width`, `outlineWidth`, `visible`, `fontSize`, `lineHeight`, `lineHeightDivisor`, `paddingY`, `anchorY`, `align`, and `screenWidth`.
Layout dimensions use costume units; `align` is 0 for left, 0.5 for center, and 1 for right.
`screenWidth` keeps wrapping width independent of the sprite's horizontal scale.
The generic default font size is 16; line height defaults to the font size and its divisor to 1.
A rational line height (`lineHeight / lineHeightDivisor`) can preserve exact baseline rounding.
`anchorY` sets the costume's vertical rotation center before outline padding.

Canvas properties include effects, `nearest`, `renderWidth`, `renderHeight`, `border style`, `border color`, and `background`; see [extension_graphics.go](../core/vm/extension_graphics.go).
Filter values follow CSS-style percentages; 100 is neutral for brightness, contrast, saturation, and scale. Clipping uses WebGL-style pixel truncation and framebuffer origins. These are shared drawing conventions, independent of any extension ID.

`extension.SurfaceTriangle` contains `Vertices [3]extension.TriangleVertex`,
`Texture extension.SurfaceTexture`, and the boolean options `Linear` and
`MultiplyTextureAlpha`. Each vertex has float64 fields `X, Y, U, V, R, G, B, A, Z, W`.
The zero texture value draws a solid triangle. Set `Texture.Kind` to `"surface"`
and `Texture.Surface` to a surface name (including an empty name), or use
`Kind: "costume"`, a zero-based `Costume` index, and an optional `Target` selector.
The host copies vertices and captures a costume snapshot without changing the
target's current costume. NaN numeric fields become zero; infinite vertex fields
or nonpositive `W` skip the draw. Colors and depth are explicit: the host does
not apply pen color or extension-specific depth offsets.

A generic screen split can use normalized seam endpoints and logical display-pixel movement:

```go
_, err := c.Call("graphics.screenSplits", []extension.ScreenSplit{
    extension.NormalizedSplit{TopX: 0.25, BottomX: 0.75, MoveX: 8},
}, false)
```

Other split policies implement `ScreenSplit.Project(width, height, pixelScale)`,
returning `SplitGeometry` in output pixels. Submitted values must be immutable.
The host copies the slice, while each implementation owns its coordinate rules.

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

### Audio

| Operation and arguments | Result or action |
| --- | --- |
| `audio.asset(selector, exactName)` | Opaque sound asset handle, or nil. |
| `audio.asset.get(handle, property)` | `duration`, `rate`, `name`, `format`, or `target`. |
| `audio.projectVolume(value)` | Set project volume as a percentage. |
| `audio.targetVoice(selector)` | Current target's voice for the selected sound, or nil. |
| `audio.targetPlay(selector, start, loop)` | Play the selected sound from the current target. |
| `audio.play(asset, start, end, volume, loop, rate?, pan?)` | Play an asset segment; return a voice handle. |
| `audio.url(url)` | Start URL playback; return a voice handle. |
| `audio.voice.get(handle, property)` | `active`, `pending`, `paused`, `position`, `duration`, `loop`, or `clock`. |
| `audio.voice.set(handle, property, value)` | Set `loop`, `rate`, `pan`, `volume`, `position`, `paused`, or `clock`. |
| `audio.voice.stop(handle)`, `audio.voice.dispose(handle)` | Stop or release a voice. |

Times and positions are in seconds. Treat asset and voice handles as opaque values. The default voice clock is `"output"`; `"step"` reports timing using the playback-start VM step. Changing the clock affects reported timing, not the audio output speed.

### Files, assets, and window controls

| Operation and arguments | Result or action |
| --- | --- |
| `files.pick(encoding)` | Receive-only `<-chan string` with selected file contents: `"text"` or a data URL. |
| `files.cancel()` | Cancel pending file selection. |
| `assets.snapshot()` | Stage snapshot as a PNG data URL; requires a renderer. |
| `assets.addCostume(url, name)` | Add and select a costume on the current target; requires a renderer. |
| `assets.deleteCostume(name)` | Remove a costume by name while retaining at least one costume. |
| `host.fullscreen()`, `host.projectFullscreen()` | Actual fullscreen state or the project's fullscreen request state. |
| `host.frameMilliseconds()` | Host frame duration in milliseconds. |
| `host.command(kind, value)` | Queue a `title`, `close`, `open-url`, or `fullscreen` request. |

Poll file-picker channels without blocking, using `Local` and `Again`/`YieldTick`. Cancellation or a read failure produces an empty string. Project fullscreen requests are tracked separately from the player's F11 mode.

### Project preparation and live loop conditions

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

- `procedures.setParam(name, value)` updates the current procedure's named argument.
- `procedures.snapshot()` and `procedures.updateCalls(...)` support authoring tools
  that synchronize saved call metadata. A snapshot is an array of
  `{target, name, extensionStorage, blocks}` records; `blocks` contains original
  procedure prototypes and calls. `procedures.updateCalls(patches)` accepts an
  array of `{target, id, mutation, inputs}` records, where `target` is the target
  index and `id` is the call block ID. Changed calls invalidate compiled execution;
  unchanged metadata leaves compiled execution active.
- `Result.ReevaluateInputs` refreshes inputs on the next `Again` continuation,
  retaining `Local`. Use this for loop conditions that can change inside a branch.
  Ordinary `Again` continues to retain the initial inputs. Combine with
  `YieldTick` for a tick wait, including inside warp procedures.

### Drawable cameras and pen layers

These operations change rendering without replacing logical sprite coordinates:

| Operation | Behavior |
| --- | --- |
| `camera.transform(name, x, y, scale, rotation)` | Set an immutable drawable camera transform: subtract `(x,y)`, rotate counterclockwise in degrees, then scale. The empty name is the default camera. |
| `camera.bind(name, target?)` | Bind a sprite, clone, render-layer name, or `_all_`. Sprite clones inherit their source binding. |
| `camera.binding(target?)` | Read a target or layer's camera name. |
| `graphics.tint([3]float64{r,g,b}, target?)` | Multiply sprite/stamp RGB before color effects; channels are normalized and clamped to 0–1. White removes the tint. |
| `graphics.penSize()` | Current target's pen diameter. |
| `graphics.layer.create(name)` | Create an independent stage-sized pen drawable. `Scratch Stage` exposes the ordinary pen drawable. |
| `graphics.layer.exists(name)` | Test for a registered layer. |
| `graphics.layer.select(name)` / `graphics.layer.selected()` | Select/read the destination for ordinary pen marks and stage-surface triangles. |
| `graphics.layer.bind(name, target?)` | Select this layer whenever the target draws. Empty name removes the binding. |
| `graphics.layer.clear(name)` / `graphics.layer.delete(name)` | Clear or release a layer. The ordinary pen cannot be deleted. |
| `graphics.layer.visible(name[, visible])` | Read or change drawable visibility. |
| `graphics.layer.quality(name, scale)` | Set GPU raster density between 0.25 and 4; CPU sensing keeps logical stage resolution. |
| `graphics.layer.placement(name, placement)` | Pin `below` or `above` sprites, or use `unlocked` ordering among sprites. |
| `graphics.layer.move(name, side, target?)` | Move an unlocked layer to the `front` or `back` of a target. |

Layers share the asset library, but retain independent pen buffers and clears.
Ordinary sprites and layer drawables preserve their order. Aggregate layer
storage is limited to 64M logical pixels. Drawable cameras are independent of
the existing whole-stage `camera.get`/`camera.set` view API.

### Local data discovery and atomic movement

- `variable.list(target?)` and `list.list(target?)` return sorted arrays of
  `{id, name}` records owned by that target only, without stage fallback.
- `target.setXY(x, y, target?)` moves both coordinates together, preserving a
  single pen segment and one fencing operation.


### Sound context control

- `audio.contextPaused()` reads the context suspension flag;
`audio.contextPaused(paused)` suspends/resumes all playback. Sounds started
during suspension wait too, and playback position does not advance.

- `audio.stopTarget(target?)` stops only voices owned by that exact sprite or
clone and releases scripts waiting for those voices.

- `events.pollHats(opcode, mode)` evaluates a registered hat predicate on each
eligible target. `edge` starts on a false-to-true transition; `level` starts
while true without restarting an already-running instance. Hat handlers must
return a boolean `Value` promptly. Their `Local` storage persists per hat and
target for transition detection.