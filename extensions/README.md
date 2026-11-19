# GoWarp extensions

GoWarp extensions run Go code embedded in JavaScript comments.
The JavaScript part defines the extension for TurboWarp; GoWarp compiles and runs the Go part.

- [API reference](API.md) — registration, handlers, scheduling, and host operations
- [Examples and original authors](EXAMPLES.md) — existing implementations and credits
- [GoWarp overview](../README.md)

## How execution works

1. GoWarp reads the project's extension IDs and the sources referenced by `extensionURLs`.
2. It finds one `@gowarp-go` header before the JavaScript file's first executable code.
3. It extracts the Go source and compiles it together with the GoWarp runtime.
4. A registration check verifies extension IDs and the blocks used by the project.
5. The runtime dispatches extension blocks to their registered Go handlers.

GoWarp does not evaluate the JavaScript or translate JavaScript into Go. Adding a marker to an existing extension is not enough: its blocks need Go implementations.

Go must be installed on the creator's PC to preview or export projects. The first compilation may download dependencies. Players do not need Go or a JavaScript engine to run the exported game. Compiled runtimes are cached and rebuilt when their inputs change.

## Using an extension in a project

1. Obtain a JS extension that includes both its TurboWarp implementation and an `@gowarp-go` implementation.
2. Load that file or its URL as a custom extension in TurboWarp, add its blocks, and save the project as SB3.
3. Open the SB3 in GoWarp and choose **Preview (F5)**.
4. Once the project works as expected, use **Create executable**.

GoWarp loads the sources referenced by the SB3. It does not scan this directory or automatically replace an existing extension with a similarly named file. A project that still references the original JS-only source needs its source reference updated.

### Editing extension code in GoWarp

1. Open an SB3 and select **Extensions → Edit extension sources** to switch to the extension editor screen.
2. Select an extension on the left. Edit its full JavaScript source, including the Go comment header. Linked HTTP(S) sources are downloaded when the editor opens.
3. Use **Add Go header** for a starter implementation if the source has no header yet. Implement its blocks using the [API reference](API.md); the starter does not translate JS.
4. **Check Go syntax** checks the header without executing it. Type checking and block registration checks happen during preview/export.
5. Choose **Save SB3** (Ctrl+S). This overwrites the current SB3, embedding only changed sources. Blocks, assets and unedited extension references stay intact.
6. Choose **Back** to return to the packager, then preview or export again to compile your changes.

## Writing an extension

Start with the [minimal extension in the API reference](API.md#minimal-extension). Keep the JS and Go extension IDs, opcodes, and input names consistent.

The header must:

- Wrap the Go implementation in `@gowarp-go { ... }`.
- Put dependency settings in `@gowarp-config { ... }`. This block may be empty
  or omitted when no external modules are needed.
- Appear before executable JavaScript. License comments may precede it.
- Declare `package extension` and `func New() extension.Extension` (an import alias is allowed).
- Import Go's standard library, `gowarp/core/extension`, or explicitly versioned
  [external modules](#external-go-libraries).

Do not nest block comments inside the header: a closing `*/`, even inside a Go string, ends the enclosing JavaScript comment.

Handlers run on the VM thread. Return promptly; use the [yield and continuation API](API.md#waiting-and-continuations) for waits. Do not use `time.Sleep` to implement a waiting block.

Extensions run as native code without a sandbox. Only compile extensions you trust. Constructors and Go initialization code may run during registration validation, before the game starts.

## External Go libraries

Declare each external module and its exact version with a `require` line in
`@gowarp-config`, then import its packages inside `@gowarp-go` normally:

```go
/*
@gowarp-config {
    require github.com/KidiXDev/rich-go v0.0.0-20260519131909-c4659679e3fa
}

@gowarp-go {
package extension

import (
    e "gowarp/core/extension"
    rich "github.com/KidiXDev/rich-go/client"
)

// Implement New() and your handlers here.
}
*/
```

The declaration names the **module**, while an import can name one of its
subpackages. Add one `require module/path version` line per module. Use a full release
version such as `v1.2.3`, a prerelease, or a Go pseudo-version; `latest`,
branch names and abbreviated versions are not accepted.

During preview/export, GoWarp resolves dependencies in a temporary copy of the
runtime SDK. Downloads use GoWarp's existing Go cache (on Windows,
`%LOCALAPPDATA%\gowarp\go`). The first build needs access to the module source;
subsequent builds can reuse downloaded modules and compiled runtimes. Changing a
declaration invalidates the runtime cache. Dependencies are compiled into the
exported executable; players do not install Go or run `go get`.

All extensions in a game share one Go module graph. Conflicting versions between
extensions, or dependencies that change GoWarp's pinned runtime libraries, fail
the build. Go resolves transitive dependencies from the libraries' module files;
GoWarp checks the selected versions before compiling. See the
[Go module reference](https://go.dev/ref/mod#minimal-version-selection).
If a library omits a dependency from its module file, add an explicit
`require` entry in `@gowarp-config` for that dependency too. GoWarp rejects an undeclared module
discovered during resolution rather than building with an unpinned latest version.

Libraries must support both the creator's OS (for registration validation) and
the export target. Builds use `CGO_ENABLED=0`, so libraries requiring C compilation
are not supported. Other GoWarp internal packages remain unavailable as direct
extension imports; use the public extension API for host services.

External libraries do not change the handler threading rules: network calls,
IPC and other blocking work must not block the VM thread. Adding dependency
support does not automatically provide a browser API or move an existing host
service into an extension.

## Troubleshooting

| Message or symptom | What to check |
| --- | --- |
| Go is required | Install a compatible Go toolchain and make sure `go` is available on `PATH`. |
| Missing `@gowarp-go` header | The project's referenced source needs a Go implementation, not just a JS implementation. |
| A project opcode has no registered Go extension block | Register the full opcode, including its extension ID prefix. Check menu and monitor reporters too. |
| Unsupported extension host operation | Check the operation name against [API.md](API.md) and use a matching GoWarp build. |
| Import needs a `require` entry | Declare the package's module and exact version in `@gowarp-config`. |
| Go extension dependencies conflict | Use compatible module versions across extensions and the GoWarp runtime. |
| Edited code has no effect | Check the source referenced by the SB3. Embedded data URLs contain a copy of the source. Export the game again after updating it. |

Support is determined by the registered Go handlers. An example port does not imply support for every block or browser feature in the original extension.