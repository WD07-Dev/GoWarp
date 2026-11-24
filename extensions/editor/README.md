# TurboWarp -> GoWarp preview

[Back to the extension guide](../README.md)

[gowarp-editor.js](gowarp-editor.js) adds **GoWarp Run** and **GoWarp Stop** to TurboWarp's top menu.
GoWarp compiles the project and runs it locally. Go is required.

## Connect

1. Start GoWarp and click **Connect TurboWarp** at the bottom. This opens a separate connection screen; **Back** returns to the packager without disconnecting.
2. Click **Enable / copy address**.
3. In TurboWarp, load gowarp-editor.js as a custom extension from **File** with **Run extension without sandbox** enabled.
4. Click **GoWarp Run**, paste the address, and click **Connect**.
5. Choose a display mode, close the settings, and click **GoWarp Run** again to run.

When connected, **GoWarp Run** starts the preview directly using the selected
display mode. Without a connection, it opens the connection settings instead.
The extension palette contains only **GoWarp connection settings**, which always
opens the settings. An expired or unreachable connection also returns you there.

Saving the SB3 also saves this helper, even when none of its blocks are used.
A hidden monitor reference lets TurboWarp load the extension again when opening
the project; it adds nothing to the stage or workspace. The original extension
URL (or embedded source when loaded from a file) is retained. The connection
address and token are not saved, so reconnect after reopening the editor.

Keep GoWarp open while connected. Each new connection has a private token;
do not share the address or store it in a project. The editor keeps it only in
page memory. The bridge accepts https://turbowarp.org, loopback editors and
file-based editors. Allow local network access if your browser asks.

## Display modes

- **Native window** (default): the game renders in its own window. No frames are read back or transmitted to the browser.
- **Embedded preview** (experimental): an additional canvas displays the native game's frames over the existing TurboWarp stage. Click it to use the keyboard; mouse input is also forwarded. Losing focus releases held keys and buttons. The native player's top controls are hidden; use TurboWarp's controls instead.

Embedded preview keeps at most two frame requests in flight, so the player can
prepare the next frame while the previous one is being delivered, up to 60 frames/s.
Images fit the browser stage's physical pixel size, with a
maximum edge of 960 pixels. Only freshly rendered frames are captured; hidden
browser tabs pause requests until visible again. These limits apply to the
streamed image, not VM timing. Without a pending request, the player skips scene
composition while continuing game logic and pen updates.
The embedded player renders at the requested stream resolution rather than
rendering a larger native-window image and shrinking it for every transfer.
Resizing the native window does not raise the render resolution beyond the
browser's request. The logical stage size and mouse coordinates are unchanged.
Frames use uncompressed RGBA over authenticated loopback WebSockets. The runtime
reuses two pixel buffers and sends them off the rendering thread. The relay
reuses up to three receive buffers instead of growing a new buffer for each
image. GPU readback and browser presentation still add work and latency; use the native window for
performance measurements. Audio plays locally in the native runtime, not through
the stream. The native game window must remain open; this is not a headless player.

The original TurboWarp canvas is retained. Its VM frame loop is paused during
native preview to avoid running and drawing the project twice. Closing the
preview or disconnecting restores the browser loop and removes the overlay.
Browser extension code with its own timers is not suspended by this mechanism.

## Blocks and controls

While connected, clicking a block stack routes the click to GoWarp. With an
unchanged project, it starts that stack in the existing native state; clicking
it again stops that stack. Other scripts, variables and clones are preserved.
Reporter clicks display values returned by GoWarp in TurboWarp's reporter bubble.
Clicking a stack before a preview exists builds a preview without running green-flag
scripts, then runs the selected stack.

TurboWarp's green flag restarts scripts in the existing preview when the project
is unchanged. Its stop button stops scripts without closing the native window.
**GoWarp Stop** next to **GoWarp Run**, or **Stop** in the connection dialog,
closes the preview or cancels its build.
**Disconnect** also returns control to the browser runtime.

Block additions, deletions and input changes are sent to the running VM when you
click a stack or Run. No SB3 upload or Go compilation is needed. Workspace-only
position changes and comments do not invalidate the preview. Editing stops scripts
in affected sprites (including their clones); other sprites continue running.
Variables, lists and clones remain in memory. The changed stack then runs when clicked.

After a live block edit, the preview uses the VM execution path instead of stale
compiled Go scripts. Its performance may differ from an exported game. Closing
the preview and running again compiles the current scripts.

Changes to assets, sprites, variables/lists, extensions or runtime settings still
build a new snapshot and restart the preview. Edits larger than 4 MiB also fall
back to a rebuild. Runtime state is not transferred across a rebuild or written
back into the browser project.

## Project settings and cleanup

The snapshot includes unsaved blocks, assets, stage size, FPS, turbo mode and
interpolation. Zero FPS uses 60 FPS. Other supported SB3 settings use GoWarp's
normal import; the packager's unrelated open project does not supply settings.
Runtime extensions need GoWarp implementations. The editor helper is marked
`/* @gowarp-ignore */`: its browser code is excluded from native execution and
needs no `@gowarp-go` section.

The existing compiler cache is reused. When only project blocks change, the
extension validator is reused but still checks and prepares the new project.
The player is reused when the generated Go code is identical, even if project
metadata or initial values changed. Changes that affect generated code still
require compilation.
Changes to extensions, the runtime SDK or the Go toolchain rebuild the validator.
The first compilation still needs to build the runtime and download any missing
dependencies. Unchanged stack clicks and green-flag
restarts and live block edits do not export, upload, recompile or reload assets. Other changes may
require Go compilation and dependency downloads. Only one preview runs per
connection. Uploads are streamed to temporary files and limited to 512 MiB.
GoWarp retains the last completed upload for this connection, so stopping and
running an unchanged project sends only its snapshot ID and settings. A changed
project or a new connection requires another upload. Job files are removed when
the preview ends; the retained snapshot is removed when the connection closes.
Closing GoWarp cancels its build/player and listener.

The stage shows preparation, upload and compilation status with an indeterminate
progress bar and a Cancel button. Local uploads use fast compression. The browser
keeps only the most recent archive, up to 64 MiB, and reuses it if the project and
assets are unchanged.
An SDK unpacked for a build is used directly instead of copying it into a second
temporary directory. Local source checkouts are still copied before compilation.

Only run projects and Go extension code you trust: extensions run as native code
with your account's permissions.
