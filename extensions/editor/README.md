# TurboWarp → GoWarp Preview

[Back to the extension guide](../README.md)

[gowarp-editor.js](gowarp-editor.js) adds **GoWarp Run** and **GoWarp Stop** to TurboWarp's top menu, allowing projects to be compiled and run locally using GoWarp. Go is required.

## Setup

1. Start GoWarp and click **Connect TurboWarp**.
2. Click **Enable / copy address**.
3. Load `gowarp-editor.js` in TurboWarp as a custom extension with **Run extension without sandbox** enabled.
4. Click **GoWarp Run**, paste the address, and click **Connect**.
5. Choose a display mode, close the settings, and click **GoWarp Run** again.

The extension is saved with the SB3 even when no extension blocks are used. Connection addresses and tokens are never saved.

Once connected, the packager shows **TurboWarp Preview** and imports the editor project's settings. Change execution, stage/window and other settings in the packager; they apply on the **next GoWarp Run**, without interrupting the current game. A settings change restarts the player on that next run, while unchanged projects still reuse their uploaded snapshot.

Disconnect before opening a different SB3 or project settings file. File opening, extension editing and exporting are disabled during preview mode. Disconnecting restores the previously open packager settings. **GoWarp Stop** closes the game but keeps the editor connected.

Keep GoWarp open while connected. Allow local network access if your browser asks, and never share your connection address.

## Display modes

- **Native window** (default): runs the game in a separate native window.
- **Embedded preview** (experimental): streams the native game's frames directly into the TurboWarp stage, with keyboard and mouse input forwarded to GoWarp.

**Multi-window** forces Native window mode and disables Embedded preview. This also applies when the mode is requested by an older or modified editor helper.

Embedded preview supports up to 60 streamed frames per second with a maximum image edge of 960 pixels. Frames are transmitted as uncompressed RGBA over authenticated loopback WebSockets.

TurboWarp's VM frame loop is paused during preview to avoid duplicate execution. Audio plays locally, and the native window must remain open. Use Native window mode for accurate performance measurements.

## Blocks and controls

While connected, TurboWarp's block interactions are routed to GoWarp:

- Clicking a block stack starts or stops its execution.
- Clicking a reporter displays its result in TurboWarp.
- The green flag restarts scripts, and the stop button stops scripts without closing the preview.
- **GoWarp Stop** closes the preview or cancels compilation.

Block edits are applied to the running VM without uploading the SB3 or recompiling. Variables, lists, clones and unaffected scripts are preserved.

Live edits use the VM execution path instead of compiled Go scripts. Restart the preview to compile the updated project.

Changes to assets, sprites, variable/list definitions, extensions or runtime settings require a rebuild.

## Compilation and project settings

GoWarp imports supported TurboWarp settings on connection. Each run includes unsaved project changes and assets, using the settings currently shown in the packager. Compiler caching and snapshot reuse reduce unnecessary uploads and compilation.

Preview launches use the cached native runtime with a separate project and settings file. They skip executable packaging, icon insertion and project recompression. **GoWarp Stop** still closes the process; another run reuses the compiler cache but loads game resources again. Use the green flag and Scratch's stop button to restart scripts in the open preview without reloading resources.

Runtime extensions require GoWarp-compatible implementations. The editor helper uses `/* @gowarp-ignore */` and does not require a `@gowarp-go` section.

Only one preview can run per connection. Uploads are limited to 512 MiB. Temporary files are cleaned up when the preview or connection closes.

**Security:** Only run projects and Go extension code you trust. Extensions execute native code with your account's permissions.