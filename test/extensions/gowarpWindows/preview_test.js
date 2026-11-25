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
    translate: {language: "en"},
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
    BlockType: {COMMAND: "command", REPORTER: "reporter", BOOLEAN: "boolean"},
    ArgumentType: {STRING: "string", NUMBER: "number", COLOR: "color"},
    Cast: {
        toNumber: value => Number(value) || 0,
        toBoolean: value => value !== "false" && Boolean(value),
        toRgbColorObject(value) { const n = parseInt(value.slice(1), 16); return {r: n >> 16, g: (n >> 8) & 255, b: n & 255}; }
    }
};
let focused = true;
vm.runInNewContext(fs.readFileSync(process.argv[2], "utf8"), {
    Scratch, screen: {left: -1280, top: 0, width: 1280, height: 720, availLeft: -1280, availTop: 30, availWidth: 1280, availHeight: 690}, devicePixelRatio: 1.5, document: {hasFocus: () => focused, createElementNS(namespace, tag) { return new Element(tag); }}
});
const stateBlock = extension.getInfo().blocks.find(block => block.opcode === "state");
assert.equal(stateBlock?.blockType, Scratch.BlockType.BOOLEAN);
assert.equal(extension[stateBlock.opcode]({ID: "main", STATE: stateBlock.arguments.STATE.defaultValue}), true);
assert.equal(extension.state({ID: "missing", STATE: "open"}), false);
assert.equal(extension.state({ID: "main", STATE: "focused"}), true);
focused = false;
assert.equal(extension.state({ID: "main", STATE: "focused"}), false);
focused = true;
assert.throws(() => extension.state({ID: "missing", STATE: "bad"}));
for (const state of ["minimized", "maximized"]) {
    assert.equal(extension.state({ID: "main", STATE: state}), false);
}
assert.equal(extension.stageSize({DIMENSION: "width"}), 960);
assert.equal(extension.stageSize({DIMENSION: "height"}), 360);
extension.create({ID: "second", X: 600, Y: 100, W: 480, H: 360, BORDER: "false"});
assert.equal(extension.state({ID: "second", STATE: "open"}), true);
assert.equal(extension.state({ID: "second", STATE: "focused"}), false);
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
assert.equal(extension.state({ID: "main", STATE: "open"}), true);
assert.equal(extension.state({ID: "main", STATE: "minimized"}), false);
assert.equal(extension.state({ID: "main", STATE: "focused"}), false);
assert.equal(overlays.size, 0);
extension.boundary({ID: "second", SHOW: "true", COLOR: "#ff0000"});
assert.equal(overlays.size, 1);
assert.equal(extension.overlay.style.pointerEvents, "none");
extension.remove({ID: "second"});
assert.equal(extension.state({ID: "second", STATE: "open"}), false);
runtime.emit("PROJECT_LOADED");
assert.equal(extension.list(), '["main"]');
assert.equal(extension.monitor({ID: "main", PROPERTY: "workWidth"}), 1920);
assert.equal(extension.monitor({ID: "main", PROPERTY: "workX"}), -1920);
assert.equal(extension.monitor({ID: "main", PROPERTY: "workY"}), 45);
extension.set({ID:"main", JSON:'{"width":400,"height":300,"x":60,"y":60}'});
extension.create({ID:"second", X:480, Y:60, W:400, H:300, BORDER:"true"});
extension.center({ID:"main", SCOPE:"all"});
assert.equal(JSON.parse(extension.get({ID:"main"})).x,-1370);
assert.equal(JSON.parse(extension.get({ID:"main"})).y,412);
assert.equal(JSON.parse(extension.get({ID:"second"})).x,-950);
extension.center({ID:"second", SCOPE:"window"});
assert.equal(JSON.parse(extension.get({ID:"second"})).x,-1160);
assert.equal(JSON.parse(extension.get({ID:"main"})).x,-1370);
assert.throws(() => extension.monitor({ID:"missing",PROPERTY:"width"}));
assert.throws(() => extension.monitor({ID:"main",PROPERTY:"bad"}));
assert.throws(() => extension.center({ID:"main",SCOPE:"bad"}));
// Translation changes labels, never opcodes, placeholders or saved menu values.
const englishInfo = JSON.parse(JSON.stringify(extension.getInfo()));
Scratch.translate.language = "ko-KR";
const koreanInfo = JSON.parse(JSON.stringify(extension.getInfo()));
assert.equal(koreanInfo.name, "창 API");
for (let i = 0; i < englishInfo.blocks.length; i++) {
    const en = englishInfo.blocks[i], ko = koreanInfo.blocks[i];
    assert.notEqual(ko.text, en.text);
    assert.deepEqual([...ko.text.matchAll(/\[[A-Z]+\]/g)].map(m => m[0]).sort(), [...en.text.matchAll(/\[[A-Z]+\]/g)].map(m => m[0]).sort());
    assert.deepEqual({...ko, text: en.text}, en);
}
for (const [name, menu] of Object.entries(englishInfo.menus)) {
    assert.deepEqual(koreanInfo.menus[name].items.map(item => item.value), menu.items.map(item => item.value));
    for (let i = 0; i < menu.items.length; i++) assert.notEqual(koreanInfo.menus[name].items[i].text, menu.items[i].text);
}
assert.equal(extension.monitor({ID: "main", PROPERTY: "workWidth"}), 1920);
assert.throws(() => extension.view("missing"), /알 수 없는 창/);
Scratch.translate.language = "fr";
assert.equal(extension.getInfo().name, "Window API");

extension.quit();
assert.equal(extension.state({ID: "main", STATE: "open"}), false);
runtime.emit("RUNTIME_DISPOSED");
assert.equal(overlays.size, 0);
assert.equal(runtime.listenerCount("STAGE_SIZE_CHANGED"), 0);
assert.equal(runtime.listenerCount("PROJECT_LOADED"), 0);
console.log("Window boundary preview checks passed");