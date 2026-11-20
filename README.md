# GoWarp

<img src="./gowarp-icon.png" alt="GoWarp Logo" width="128">

A Go-based packager and runtime that turns Scratch and TurboWarp projects into standalone executables.

## Background

GoWarp began with a request to optimize [ULB: Nyctalopia](https://gamejolt.com/games/EP_LastBreath/1013526), a game built with TurboWarp. I initially tried modifying the TurboWarp VM, but felt that this approach would not be enough to address its memory usage. That led me to build a new VM, and GoWarp grew from there.

## What can it do?

Load an SB3 file, adjust its runtime settings, preview it, and export it as a game. Block execution, graphics, and audio are handled by the Go runtime, so exported games do not require a browser or Electron.

GoWarp aims to provide a lightweight alternative to TurboWarp Packager. It is still in development, and compatibility and performance may vary between projects.

## Supported platforms

| Platform | Architecture | Status |
| --- | --- | --- |
| Windows | AMD64 (x86-64) | Supported |
| Linux | AMD64 (x86-64) | Supported |

Each distribution ZIP includes runtimes for both platforms, allowing you to choose the export target in the editor.

## Getting started

1. Extract the distribution ZIP for your operating system and launch GoWarp.
2. Select a project with **Open SB3 file**, or drag an SB3 file into the window.
3. Adjust the runtime, window, and player settings using the menu on the left.
4. Use **Preview (F5)** to check the result.
5. Choose the target platform under Export, then click **Create executable**.

GoWarp is a tool for running and packaging games. To edit blocks, costumes, or sounds, make your changes in Scratch or TurboWarp and import the updated SB3 file.

### Saved projects and SB3 files

- **Open SB3 file** loads the Scratch or TurboWarp project to run.
- **Save project** saves GoWarp's runtime and export settings to a `*.gowarp.json` file.
- **Open project** loads previously saved GoWarp settings.

The settings file does not replace the SB3 file. Keep both the settings file and the original SB3 to continue working on your project.

### Available settings

| Category | Settings |
| --- | --- |
| Runtime | FPS, turbo mode, interpolation, high-quality pen, removal of fencing and miscellaneous limits |
| Window | Window size, antialiasing, fullscreen, resizing, disabling minimize/maximize, scaling mode, background color |
| Player | Title, icon, username, automatic start, player controls, mute |
| Cloud variables | Disabled, local storage, WebSocket connection, per-variable storage modes |
| Loading screen | Text, background color, progress visibility and color, logo, background image |

Loading logos and backgrounds accept PNG, JPEG and animated GIF files up to 8 MiB each.
GIFs support up to 120 frames, with a maximum canvas of 512×512 for logos and 1280×720 for backgrounds.
The combined decoded image buffers are limited to 64 MiB.

For WebSocket cloud variables, choose a server type in the connection settings:

- **TurboWarp:** enter a project ID and contact information.
- **Custom:** enter a WebSocket server URL and an optional project ID. A blank project ID uses the app ID.

Supported TurboWarp settings saved in the SB3 are imported when you open the project. Before exporting, check that the FPS and runtime settings match what the game expects.

## Go requirements

| Task | Go installation |
| --- | --- |
| Previewing or exporting a project | Required on the creator's PC |
| Running an exported game | Not required |
| Building GoWarp from source | Required |

Install Go, make sure `go` is available on `PATH`, then restart GoWarp.

GoWarp compiles supported event scripts, custom blocks and calculations into resumable Go code. Waits and loop yields retain their Scratch scheduling behavior; unsupported operations use the shared VM. The first compilation may require an internet connection to download dependencies.

The `runtimes/` folder next to the GoWarp executable is required for previewing and exporting. Keep the directory structure from the distribution ZIP intact.

An exported game is a single executable containing the project and runtime. You do not need to distribute the GoWarp editor or the `runtimes/` folder with it. Any external URLs or online services used by the project must still be accessible.

## Runtime diagnostics

Exported games do not collect diagnostic logs or profiles by default. To record a diagnostic run, launch the game with `--debug`:

```powershell
.\game.exe --debug
```

CPU profiling runs for the diagnostic session without an FPS threshold. Close the game normally to finalize the CPU profile and save an allocation profile using Go’s default memory sampling. Allocation samples include loading as well as gameplay; no extra garbage collection is forced.

Files are saved as `<app-id>.log`, `<app-id>.cpu.pprof`, and `<app-id>.allocs.pprof` in `%LOCALAPPDATA%\gowarp\logs` on Windows or `$XDG_CACHE_HOME/gowarp/logs` (default `~/.cache/gowarp/logs`) on Linux. Each diagnostic run overwrites these files; ordinary runs leave existing files untouched. Profiling adds overhead, so omit the argument for normal play.

Slow VM entries also report graphics query waits, queue waits, and display-thread dispatch time when the parallel graphics path is used. These are elapsed times, include scheduling delays, and overlap with VM timing; they are not GPU execution times and must not be added together. Use `go tool pprof -sample_index=alloc_space <app-id>.allocs.pprof` to inspect cumulative allocation bytes rather than live memory.

## Extension support

GoWarp does not run ordinary JavaScript extensions or automatically translate them into Go. A custom extension must include a GoWarp-compatible implementation inside an `@gowarp-go { ... }` block in a JavaScript comment. External module requirements belong in a separate `@gowarp-config { ... }` block.

A single file can contain both a JavaScript implementation for TurboWarp and a Go implementation for GoWarp. During preview or export, GoWarp extracts the Go implementation and compiles it together with the runtime.

Use **Extensions → Edit extension sources** to edit an SB3's JavaScript and Go headers inside GoWarp. **Save SB3** writes the changed sources directly to that SB3.

- [How extensions run and how to use them](extensions/README.md)
- [Go extension API](extensions/API.md)
- [Examples and original authors](extensions/EXAMPLES.md)

Extension Go code runs without a sandbox, so only use extensions you trust.

## Credits

- Development: **wheedo07**
- Feedback: **[ULB: Nyctalopia team](https://gamejolt.com/games/EP_LastBreath/1013526)** - game testing, compatibility checks, and optimization feedback

## License

GoWarp is distributed under the [Apache License 2.0](LICENSE). See the full license for details.

Third-party libraries and fonts are covered by their respective licenses. See [go.mod](go.mod) for the dependency list and [fonts/LICENSE.txt](app/assets/embed/fonts/LICENSE.txt) for the bundled font license.