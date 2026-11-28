const {test} = require("node:test");
const assert = require("node:assert/strict");
const {EventEmitter} = require("node:events");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

// Exercise the actual editor extension without starting a browser or Go compiler.
function editor(t, language = "en") {
    const nodes = [];
    const listeners = new Map();
    let displayed = 0;
    let finishZip;
    let zipStarted;
    const zipReady = new Promise(resolve => { zipStarted = resolve; });
    let holdZip = false;
    let zipOptions;
    let generated = 0;
    class Element {
        constructor(tag) {
            this.tagName = tag;
            this.children = [];
            this.dataset = {};
            this.value = "";
            nodes.push(this);
        }
        appendChild(node) { this.children.push(node); node.parentElement = this; }
        setAttribute() {}
        focus() {}
        remove() { this.removed = true; }
        showModal() { this.open = true; }
        close() { this.open = false; }
        getBoundingClientRect() { return {width: 480, height: 360}; }
        getContext() { return {putImageData() { displayed++; }}; }
    }
    const bar = new Element("div");
    const document = {
        title: "Preview test", documentElement: {lang: "en"},
        head: new Element("head"), body: new Element("body"),
        createElement: tag => new Element(tag),
        querySelector: () => bar,
        getElementById: id => nodes.find(node => node.id === id && !node.removed),
        addEventListener(name, callback) { listeners.set(name, callback); },
        removeEventListener(name) { listeners.delete(name); }
    };
    const project = {
        targets: [{isStage: true, name: "Stage", variables: {}, lists: {}, blocks: {
            first: {opcode: "motion_setx", topLevel: true, inputs: {X: [1, [4, "10"]]}, x: 10, y: 10},
            second: {opcode: "motion_sety", topLevel: true, inputs: {Y: [1, [4, "20"]]}, x: 60, y: 10}
        }, comments: {}}], extensions: []
    };
    const runtime = new EventEmitter();
    const monitors = new Map();
    runtime.getMonitorState = () => monitors;
    runtime.requestUpdateMonitor = delta => {
        if (!monitors.has(delta.id)) return false;
        Object.assign(monitors.get(delta.id), delta);
        return true;
    };
    runtime.requestAddMonitor = monitor => {
        if (!runtime.requestUpdateMonitor(monitor)) monitors.set(monitor.id, {...monitor});
    };
    runtime.requestRemoveMonitor = id => monitors.delete(id);
    const extensionURLs = {gowarpEditor: "data:text/javascript,editor-source"};
    runtime.extensionManager = {getExtensionURLs: () => extensionURLs};
    const stage = new Element("div");
    const canvas = new Element("canvas");
    stage.appendChild(canvas);
    const order = ["first", "second"];
    const target = {id: "stage", isOriginal: true, blocks: {getScripts: () => order}};
    Object.assign(runtime, {
        targets: [target], stageWidth: 480, stageHeight: 360, turboMode: false, interpolationEnabled: false,
        renderer: {canvas},
        frameLoop: {
            framerate: 30, running: true,
            stop() {
                this.running = false;
            },
            start() {
                this.running = true;
            }
        },
        toggleScript() {},
        greenFlag() {},
        stopAll() {},
        getEditingTarget: () => target
    });
    const scratchVM = new EventEmitter();
    scratchVM.runtime = runtime;
    scratchVM.toJSON = (_, options) => {
        const saved = JSON.parse(JSON.stringify(project));
        if (options?.allowOptimization !== false) {
            for (const target of saved.targets) {
                target.blocks = Object.fromEntries(Object.entries(target.blocks).map(([id, block]) => ["saved_" + id, block]));
            }
        }
        return JSON.stringify(saved);
    };
    class Zip {
        constructor() { this.files = {}; }
        file(name, data, options) { this.files[name] = data; }
        async generateAsync(options) {
            generated++;
            zipOptions = options;
            if (holdZip) await new Promise(resolve => { finishZip = resolve; zipStarted(); });
            return new Blob([JSON.stringify(this.files)]);
        }
    }
    scratchVM.exports = {JSZip: Zip};
    scratchVM.serializeAssets = () => [{fileName: "costume.svg", fileContent: "<svg/>"}];
    scratchVM._saveProjectZip = () => {
        const zip = new Zip();
        zip.file("project.json", scratchVM.toJSON());
        zip.file("costume.svg", "<svg/>");
        return zip;
    };
    scratchVM.saveProjectSb3 = () => scratchVM._saveProjectZip().generateAsync();
    let uploadedFiles;
    let uploads = 0;
    const uploadSizes = [];
    let snapshot = "";
    let phase = "ready";
    let connected = false;
    let settingsRevision = "initial";
    let multiWindow = false;
    let metadata;
    let runSettings;
    let offline = false;
    let extension;
    const commands = [];
    const sockets = [];
    let stops = 0;
    class Socket {
        static OPEN = 1;
        constructor() {
            sockets.push(this);
            this.readyState = 1;
            this.bufferedAmount = 0;
            queueMicrotask(() => this.onopen?.());
        }
        send(data) {
            const command = JSON.parse(data);
            if (command.token) return;
            commands.push(command);
            if (command.action === "edit") {
                queueMicrotask(() => this.onmessage({data: JSON.stringify({id: command.id})}));
            }
        }
        close() { this.readyState = 3; }
    }
    const window = {devicePixelRatio: 1, addEventListener() {}, removeEventListener() {}};
    const context = {
        window, document, navigator: {language: "en"}, URL, Request, AbortController, TextEncoder, Blob,
        ArrayBuffer, DataView, Uint8ClampedArray, ImageData: class {},
        setTimeout, clearTimeout, requestAnimationFrame: () => 0, cancelAnimationFrame() {},
        MutationObserver: class { observe() {} disconnect() {} },
        WebSocket: Socket,
        Scratch: {vm: scratchVM, translate: {language}, BlockType: {BUTTON: "button", REPORTER: "reporter"},
        extensions: {unsandboxed: true, register(value) { extension = value; }}},
        fetch: async (url, options) => {
            if (offline) throw new Error("Connection refused");
            if (url.endsWith("/connect")) {
                connected = true;
                metadata = JSON.parse(await options.body.text());
            }
            if (url.endsWith("/disconnect")) { connected = false; phase = "ready"; }
            if (url.endsWith("/run")) {
                uploadSizes.push(options.body.size);
                const settings = JSON.parse(decodeURIComponent(options.headers["X-GoWarp-Options"]));
                runSettings = settings;
                if (settings.snapshot) {
                    assert.equal(settings.snapshot, snapshot);
                    assert.equal(options.body.size, 0);
                } else {
                    uploadedFiles = JSON.parse(await options.body.text());
                    snapshot = "snapshot-" + (uploads + 1);
                }
                uploads++;
                phase = "running";
            }
            if (url.endsWith("/stop")) { stops++; phase = "ready"; }
            return {ok: true, json: async () => ({phase, snapshot, connected, settingsRevision, multiWindow})};
        }
    };
    const source = fs.readFileSync(path.resolve(__dirname, "../../extensions/editor/gowarp-editor.js"), "utf8");
    const originalToJSON = scratchVM.toJSON;
    vm.runInNewContext(source, context);
    t.after(() => window.__gowarpEditor.dispose());
    return {
        project, runtime, scratchVM, order, commands, uploads: () => uploads,
        extensionURLs, originalToJSON, dispose: () => window.__gowarpEditor.dispose(),
        uploadSizes,
        metadata: () => metadata,
        runSettings: () => runSettings,
        settingsChanged: () => { settingsRevision += "-changed"; },
        setMultiWindow: value => { multiWindow = value; },
        displayMode: () => nodes.find(node => node.tagName === "select"),
        dialog: () => nodes.find(node => node.tagName === "dialog"),
        address: () => nodes.find(node => node.tagName === "input"),
        open: () => extension.open(),
        offline: value => { offline = value; },
        pixelRatio: value => { window.devicePixelRatio = value; },
        disconnect: () => nodes.find(node => node.textContent === "Disconnect").onclick(),
        uploadedFiles: () => uploadedFiles,
        loading: () => document.getElementById("gowarp-preview-loading"),
        holdZip: (value = true) => { holdZip = value; },
        finishZip: () => finishZip(),
        zipReady,
        generated: () => generated,
        zipOptions: () => zipOptions,
        runButton: () => document.getElementById("gowarp-editor-button"),
        stopButton: () => document.getElementById("gowarp-editor-stop"),
        stops: () => stops, displayed: () => displayed,
        embedded: () => { nodes.find(node => node.tagName === "select").value = "embedded"; },
        hidden: value => { document.hidden = value; listeners.get("visibilitychange")(); },
        frame: (width = 1, height = 1) => {
            const data = new ArrayBuffer(9 + width * height * 4);
            const header = new DataView(data);
            header.setUint8(0, 1);
            header.setUint32(1, width, true);
            header.setUint32(5, height, true);
            sockets.at(-1).onmessage({data});
        },
        reply: reply => sockets.at(-1).onmessage({data: JSON.stringify(reply)}),
        run: () => document.getElementById("gowarp-editor-button").onclick(),
        connect: async () => {
            nodes.find(node => node.tagName === "input").value = "http://127.0.0.1:12345/#" + "a".repeat(64);
            await nodes.find(node => node.textContent === "Connect").onclick();
        }
    };
}
    
test("saving a project keeps the button-only editor extension", async t => {
    const e = editor(t);
    e.project.extensions = ["pen"];
    e.project.extensionURLs = {other: "https://example.com/other.js"};
    e.project.monitors = [{
        id: "score", opcode: "data_variable", params: {VARIABLE: "score"}, visible: true
    }];
    await e.connect();
    const archive = JSON.parse(await (await e.scratchVM.saveProjectSb3()).text());
    const saved = JSON.parse(archive["project.json"]);
    assert.deepEqual(saved.extensions, ["pen", "gowarpEditor"]);
    assert.deepEqual(saved.extensionURLs, {
        other: "https://example.com/other.js", gowarpEditor: e.extensionURLs.gowarpEditor
    });
    // TurboWarp discovers extensions from blocks and monitors when loading,
    // not from the extensions array alone.
    const marker = saved.monitors.find(m => m.opcode === "gowarpEditor_installed");
    assert.ok(marker, "missing extension reference for TurboWarp's loader");
    assert.equal(marker.visible, false);
    assert.deepEqual(saved.monitors[0], e.project.monitors[0]);
    assert.equal(JSON.stringify(saved).includes("a".repeat(64)), false);
    assert.deepEqual(e.project.extensions, ["pen"]);
    assert.equal(e.project.monitors.length, 1);
    const unoptimized = JSON.parse(e.scratchVM.toJSON(undefined, {allowOptimization: false}));
    assert.deepEqual(unoptimized.targets, e.project.targets);
    e.project.monitors = saved.monitors;
    e.project.extensions = saved.extensions;
    const again = JSON.parse(e.scratchVM.toJSON());
    assert.equal(again.monitors.filter(m => m.opcode === marker.opcode).length, 1);
    assert.equal(again.extensions.filter(id => id === "gowarpEditor").length, 1);
});

test("editor persistence leaves sprite exports alone and restores serialization on disposal", t => {
    const e = editor(t);
    const options = {allowOptimization: false};
    assert.equal(e.scratchVM.toJSON("stage", options), e.originalToJSON("stage", options));
    e.dispose();
    assert.equal(e.scratchVM.toJSON, e.originalToJSON);
});

test("GoWarp Stop closes the preview from the top menu", async t => {
    const e = editor(t);
    assert.ok(e.stopButton(), "missing top-menu stop button");
    assert.equal(e.stopButton().disabled, true);
    await e.connect();
    await e.run();
    await settle();
    assert.equal(e.stopButton().disabled, false);
    await e.stopButton().onclick();
    await settle();
    assert.equal(e.stops(), 1);
    assert.equal(e.stopButton().disabled, true);
    assert.equal(e.runtime.frameLoop.running, true);
});

test("embedded frames keep two requests in flight, and pause when hidden", async t => {
    const e = editor(t);
    e.embedded();
    await e.connect();
    await e.run();
    await settle();
    const count = () => e.commands.filter(command => command.action === "frame").length;
    assert.equal(count(), 1);
    assert.equal(e.commands.at(-1).frames, 2);
    assert.equal(e.commands.at(-1).width, 480);
    assert.equal(e.commands.at(-1).height, 360);
    e.frame();
    assert.equal(e.displayed(), 1);
    assert.equal(count(), 2, "next request waited for another animation frame");
    assert.equal(e.commands.at(-1).frames, 1, "receiving a frame should refill only one slot");
    e.hidden(true);
    e.frame();
    assert.equal(count(), 2);
    e.frame();
    assert.equal(count(), 2);
    e.hidden(false);
    assert.equal(count(), 3, "visible preview did not resume");
    assert.equal(e.commands.at(-1).frames, 2);
    e.hidden(false);
    assert.equal(count(), 3, "visibility events must not accumulate frame requests");
});

test("embedded preview requests physical pixels on high-DPI screens", async t => {
    const e = editor(t);
    e.pixelRatio(2);
    e.embedded();
    await e.connect();
    await e.run();
    await settle();
    const frame = e.commands.find(command => command.action === "frame");
    assert.equal(frame.width, 960);
    assert.equal(frame.height, 720);
});

test("embedded preview keeps detail above 960 pixels and bounds oversized requests", async t => {
    const e = editor(t);
    e.pixelRatio(3);
    e.embedded();
    await e.connect();
    await e.run();
    await settle();
    let frame = e.commands.find(command => command.action === "frame");
    assert.equal(frame.width, 1440);
    assert.equal(frame.height, 1080);
    e.pixelRatio(5);
    e.frame(1440, 1080);
    assert.equal(e.displayed(), 1);
    frame = e.commands.at(-1);
    assert.equal(frame.width, 1920);
    assert.equal(frame.height, 1440);
});

async function settle() {
    for(let i=0; i < 10; i++) await new Promise(resolve => setTimeout(resolve, 0));
}

test("preview shows loading while preparing and cancels before upload", async t => {
    const e = editor(t);
    await e.connect();
    e.holdZip();
    const running = e.run();
    await e.zipReady;
    assert.ok(e.loading(), "stage has no loading display during archive preparation");
    assert.equal(e.loading().children[1].textContent, "Preparing project...");
    await e.stopButton().onclick();
    e.finishZip();
    await running;
    await settle();
    assert.equal(e.uploads(), 0, "cancelled archive was sent");
    assert.equal(e.loading(), undefined);
});

test("preview reuses an unchanged archive after stopping", async t => {
    const e = editor(t);
    await e.connect();
    await e.run();
    await settle();
    assert.equal(e.loading(), undefined, "loading remained after launch");
    assert.equal(e.zipOptions().compressionOptions.level, 1);
    await e.stopButton().onclick();
    await settle();
    await e.run();
    await settle();
    assert.equal(e.uploads(), 2);
    assert.equal(e.uploadSizes[1], 0, "unchanged project was transferred again");
    assert.equal(e.generated(), 1, "unchanged assets were zipped again");
    e.project.targets[0].blocks.first.inputs.X = [1, [4, "99"]];
    e.scratchVM.emit("PROJECT_CHANGED");
    await e.stopButton().onclick();
    await settle();
    await e.run();
    await settle();
    assert.equal(e.generated(), 2, "edited project reused a stale archive");
    assert.ok(e.uploadSizes[2] > 0, "edited project was not uploaded");
});

test("workspace clicks and edits use the same IDs as the uploaded project", async t => {
    const e = editor(t);
    await e.connect();
    e.runtime.toggleScript("first");
    await settle();
    const saved = JSON.parse(e.uploadedFiles()["project.json"]);
    const click = e.commands.find(command => command.action === "toggle");
    assert.ok(saved.targets[click.target].blocks[click.block], "clicked block ID was changed during export");
    assert.equal(e.uploadedFiles()["costume.svg"], "<svg/>");
    e.project.targets[0].blocks.first.inputs.X = [1, [4, "42"]];
    e.scratchVM.emit("PROJECT_CHANGED");
    e.runtime.toggleScript("first");
    await settle();
    const edit = e.commands.find(command => command.action === "edit").edits[0];
    assert.ok(edit.blocks.first, "live edit renamed the workspace block");
    assert.deepEqual(edit.scriptOrder, ["first", "second"]);
    assert.equal(e.uploads(), 1);
});

test("existing clicks, workspace moves and block edits reuse the player", async t => {
    const e = editor(t);
    await e.connect();
    await e.run();
    await settle();
    assert.equal(e.uploads(), 1);
    
    e.runtime.toggleScript("second");
    await settle();
    assert.equal(e.uploads(), 1, "a save notification must not rebuild identical code");
    assert.equal(e.commands.at(-1).block, "second");
    
    e.project.targets[0].blocks.first.x = 123;
    e.scratchVM.emit("PROJECT_CHANGED");
    e.runtime.toggleScript("first");
    await settle();
    assert.equal(e.commands.filter(command => command.action === "edit").length, 0);
    assert.equal(e.uploads(), 1);
    
    e.project.targets[0].blocks.first.inputs.X = [1, [4, "42"]];
    e.project.targets[0].blocks.added = {opcode: "motion_setx", topLevel: true, inputs: {X: [1, [4, "7"]]}};
    e.order.push("added");
    e.scratchVM.emit("PROJECT_CHANGED");
    e.runtime.toggleScript("added");
    await settle();
    assert.equal(e.uploads(), 1, "block edits must not export another SB3");
    assert.deepEqual(e.commands.at(-2).edits[0].blocks.first.inputs.X, [1, [4, "42"]]);
    assert.equal(e.commands.at(-1).block, "added");
    
    e.project.targets[0].costumes = [{name: "new costume", md5ext: "new.svg"}];
    e.scratchVM.emit("PROJECT_CHANGED");
    await e.run();
    await settle();
    assert.equal(e.uploads(), 2, "asset changes must rebuild the preview");
});

test("opening the extension does not connect or show settings", async t => {
    const e = editor(t);
    assert.ok(!e.dialog().open);
    assert.equal(e.metadata(), undefined);
    await e.connect();
    assert.equal(e.metadata().targets[0].name, "Stage");
    assert.equal(e.metadata().targets[0].blocks, undefined);
    assert.equal(e.uploads(), 0);
});

test("packager settings restart the next run without reuploading assets", async t => {
    const e = editor(t);
    await e.connect();
    await e.run();
    await new Promise(resolve => setImmediate(resolve));
    e.settingsChanged();
    await e.run();
    assert.equal(e.stops(), 1);
    assert.equal(e.uploads(), 2);
    assert.equal(e.uploadSizes[1], 0);
});

test("multi-window disables embedded preview and forces a native run", async t => {
    const e = editor(t);
    e.embedded();
    e.setMultiWindow(true);
    await e.connect();
    assert.equal(e.displayMode().value, "native");
    assert.equal(e.displayMode().disabled, true);
    await e.run();
    assert.equal(e.runSettings().display, "native");
    await e.disconnect();
    assert.equal(e.displayMode().disabled, false);
    await e.run();
    assert.equal(e.dialog().open, true);
    assert.equal(e.uploads(), 1);
});

test("disconnecting an unreachable packager unlocks the address for reconnecting", async t => {
    const e = editor(t);
    await e.connect();
    await e.run();
    await settle();
    e.offline(true);
    await e.disconnect();
    assert.equal(e.address().disabled, false);
    assert.equal(e.runtime.frameLoop.running, true);
    e.offline(false);
    await e.connect();
    assert.equal(e.address().disabled, true);
    await e.run();
    await settle();
    assert.equal(e.uploads(), 2);
});

test("disconnect releases the cached archive before reconnecting", async t => {
    const e = editor(t);
    await e.connect();
    await e.run();
    await settle();
    assert.equal(e.generated(), 1);
    await e.disconnect();
    await e.connect();
    await e.run();
    await settle();
    assert.equal(e.generated(), 2, "disconnected project assets remained cached");
});

test("disconnect during archive creation cannot repopulate the cache", async t => {
    const e = editor(t);
    await e.connect();
    e.holdZip();
    const running = e.run();
    await e.zipReady;
    await e.disconnect();
    e.finishZip();
    await running;
    assert.equal(e.uploads(), 0);
    e.holdZip(false);
    await e.connect();
    await e.run();
    await settle();
    assert.equal(e.generated(), 2, "cancelled archive remained cached");
});

test("opening settings after the packager closes unlocks the connection address", async t => {
    const e = editor(t);
    await e.connect();
    await e.run();
    await settle();
    e.offline(true);
    e.open();
    await settle();
    assert.equal(e.address().disabled, false);
    assert.equal(e.runtime.frameLoop.running, true);
});

test("preview forwards monitor checkboxes without restarting", async t => {
    const e = editor(t);
    await e.connect();
    await e.run();
    await settle();
    e.runtime.requestAddMonitor({id: "score", opcode: "data_variable", params: {VARIABLE: "score"}, visible: true, spriteName: null});
    await new Promise(resolve => setImmediate(resolve));
    let command = e.commands.findLast(c => c.action === "monitor");
    assert.equal(command?.monitor.visible, true);
    e.runtime.requestUpdateMonitor({id: "score", visible: false});
    await new Promise(resolve => setImmediate(resolve));
    command = e.commands.findLast(c => c.action === "monitor");
    assert.equal(command?.monitor.visible, false);
    assert.equal(e.uploads(), 1);
});

test("embedded questions use the editor answer input and clean up on disconnect", async t => {
    const e = editor(t);
    const questions = [];
    e.runtime.on("QUESTION", text => questions.push(text));
    await e.connect();
    e.embedded();
    await e.run();
    await settle();
    e.reply({action: "question", questionId: 1, question: "이름?"});
    assert.equal(questions.at(-1), "이름?");
    e.runtime.emit("ANSWER", "한글 answer");
    const answer = e.commands.findLast(c => c.action === "answer");
    assert.equal(answer?.text, "한글 answer");
    assert.equal(answer?.questionId, 1);
    e.reply({action: "question", questionId: 2, question: "이름?"});
    await e.disconnect();
    assert.equal(questions.at(-1), null);
    const count = e.commands.length;
    e.runtime.emit("ANSWER", "Scratch answer");
    assert.equal(e.commands.length, count);
});