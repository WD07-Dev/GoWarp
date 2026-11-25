const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const { EventEmitter } = require("node:events");

class Element {
    constructor(tag) {
        this.tag = tag;
        this.attributes = {};
        this.style = {};
        this.children = [];
    }
    setAttribute(key, value) {
        this.attributes[key] = String(value);
    }
    replaceChildren() {
        this.children = [];
    }
    append(...nodes) {
        this.children.push(...nodes);
    }
}
const runtime = Object.assign(new EventEmitter(), {stageWidth: 960, stageHeight: 360});
const overlays = new Set();
const renderer = {
    addOverlay(element, mode) { assert.equal(mode, "scale"); overlays.add(element); return {container: {style: {}}}; },
    removeOverlay(element) { overlays.delete(element); }
};
let extension;
const Scratch = {
    vm: {
        runtime,
        renderer
    },
    extensions: {
        unsandboxed: true,
        register(value) {
            extension = value;
        }
    },
    BlockType: {COMMAND: "command", REPORTER: "reporter"},
    ArgumentType: {STRING: "string", NUMBER: "number", COLOR: "color"},
    Cast: {
        toNumber: value => Number(value) || 0,
        toBoolean: value => value !== "false" && Boolean(value),
        toRgbColorObject(value) { const n = parseInt(value.slice(1), 16); return {r: n >> 16, g: (n >> 8) & 255, b: n & 255}; }
    }
};
vm.runInNewContext(fs.readFileSync(process.argv[2], "utf8"), {
    Scratch, document: {createElementNS(namespace, tag) { return new Element(tag); }}
});
assert.equal(extension.stageSize({DIMENSION: "width"}), 960);
assert.equal(extension.stageSize({DIMENSION: "height"}), 360);
extension.create({ID: "second", X: 600, Y: 100, W: 480, H: 360, BORDER: "false"});
extension.viewport({ID: "second", X: 480, Y: 0, W: 240, H: 180});
extension.boundary({ID: "second", SHOW: "true", COLOR: "#55ccff"});
let rect = extension.overlay.children[2];
assert.equal(rect.attributes.x, "480");
assert.equal(rect.attributes.width, "240");
assert.equal(rect.attributes.stroke, "rgb(85, 204, 255)");
extension.move({ID: "second", X: 1000, Y: 200});
assert.equal(extension.overlay.children[2].attributes.x, "480");
extension.moveViewport({ID: "second", X: -10.5, Y: 30});
const view = JSON.parse(extension.get({ID: "second"}));
assert.equal(view.x, 1000);
assert.equal(view.stageX, -10.5);
assert.equal(view.stageWidth, 240);
assert.equal(extension.overlay.children[2].attributes.x, "-10.5");
assert.throws(() => extension.viewport({ID: "second", X: 0, Y: 0, W: 0, H: 180}));
assert.equal(JSON.parse(extension.get({ID: "second"})).stageX, -10.5);
assert.throws(() => extension.set({ID: "second", JSON: '{"stageWidth":"bad"}'}));
runtime.stageWidth = 1280;
runtime.emit("STAGE_SIZE_CHANGED");
assert.equal(extension.stageSize({DIMENSION: "width"}), 1280);
assert.equal(extension.overlay.attributes.viewBox, "0 0 1280 360");
assert.equal(JSON.parse(extension.get({ID: "main"})).stageWidth, 1280);
assert.equal(JSON.parse(extension.get({ID: "second"})).stageWidth, 240);
extension.boundary({ID: "second", SHOW: "false", COLOR: "#55ccff"});
assert.equal(extension.overlay.children.length, 2);
extension.set({ID: "main", JSON: '{"hidden":true}'});
assert.equal(overlays.size, 0);
extension.boundary({ID: "second", SHOW: "true", COLOR: "#ff0000"});
assert.equal(overlays.size, 1);
assert.equal(extension.overlay.style.pointerEvents, "none");
runtime.emit("PROJECT_LOADED");
assert.equal(extension.list(), '["main"]');
runtime.emit("RUNTIME_DISPOSED");
assert.equal(overlays.size, 0);
assert.equal(runtime.listenerCount("STAGE_SIZE_CHANGED"), 0);
assert.equal(runtime.listenerCount("PROJECT_LOADED"), 0);
console.log("Window boundary preview checks passed");