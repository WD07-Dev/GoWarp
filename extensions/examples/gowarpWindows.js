// Name: Window API
// ID: gowarpWindows
// By: wheedo07
// License: Apache-2.0

/*
@gowarp-config {
}
@gowarp-go {
package extension

import (
	"encoding/json"
	e "gowarp/core/extension"
)

func New() e.Extension {
	return e.Extension{ID: "gowarpWindows", Blocks: []e.Block{
		{Opcode: "gowarpWindows_viewport", Kind: "command", Inputs: []string{"ID", "X", "Y", "W", "H"}, Handler: func(c *e.Context) (e.Result, error) {
			_, err := c.Call("window.viewport", c.String("ID"), c.Number("X"), c.Number("Y"), c.Number("W"), c.Number("H"))
			return e.Result{}, err
		}},
		{Opcode: "gowarpWindows_moveViewport", Kind: "command", Inputs: []string{"ID", "X", "Y"}, Handler: func(c *e.Context) (e.Result, error) {
			_, err := c.Call("window.viewport", c.String("ID"), c.Number("X"), c.Number("Y"))
			return e.Result{}, err
		}},
		{Opcode: "gowarpWindows_stageSize", Text: "stage [DIMENSION]", Kind: "reporter", Handler: func(c *e.Context) (e.Result, error) {
			v, err := c.Call("camera.get", c.Field("DIMENSION"))
			return e.Result{Value: v}, err
		}},
		{Opcode: "gowarpWindows_boundary", Kind: "command", Inputs: []string{"ID", "COLOR"}, Handler: func(c *e.Context) (e.Result, error) {
			// Boundary guides belong to the browser editor, not the exported game.
			return e.Result{}, nil
		}},
		{Opcode: "gowarpWindows_create", Kind: "command", Inputs: []string{"ID", "X", "Y", "W", "H"}, Handler: func(c *e.Context) (e.Result, error) {
			id := c.String("ID")
			width, height := c.Number("W"), c.Number("H")
			data, err := json.Marshal(map[string]any{
				"id": id, "title": id,
				"x": c.Number("X"), "y": c.Number("Y"),
				"width": width, "height": height,
				"stageWidth": width, "stageHeight": height,
				"border": e.Bool(c.Field("BORDER")), "resizable": true, "opacity": 1,
			})
			if err != nil {
				return e.Result{}, err
			}
			_, err = c.Call("window.create", string(data))
			return e.Result{}, err
		}},
		{Opcode: "gowarpWindows_move", Kind: "command", Inputs: []string{"ID", "X", "Y"}, Handler: func(c *e.Context) (e.Result, error) {
			_, err := c.Call("window.move", c.String("ID"), c.Number("X"), c.Number("Y"))
			return e.Result{}, err
		}},
		{Opcode: "gowarpWindows_set", Kind: "command", Inputs: []string{"ID", "JSON"}, Handler: func(c *e.Context) (e.Result, error) {
			_, err := c.Call("window.set", c.String("ID"), c.String("JSON"))
			return e.Result{}, err
		}},
		{Opcode: "gowarpWindows_setBoolean", Kind: "command", Inputs: []string{"ID"}, Handler: func(c *e.Context) (e.Result, error) {
			return setProperty(c, c.Field("PROPERTY"), e.Bool(c.Field("VALUE")))
		}},
		{Opcode: "gowarpWindows_topmost", Kind: "command", Inputs: []string{"ID"}, Handler: func(c *e.Context) (e.Result, error) {
			return setProperty(c, "topmost", e.Bool(c.Field("VALUE")))
		}},
		{Opcode: "gowarpWindows_opacity", Kind: "command", Inputs: []string{"ID", "PERCENT"}, Handler: func(c *e.Context) (e.Result, error) {
			return setProperty(c, "opacity", c.Number("PERCENT") / 100)
		}},
		{Opcode: "gowarpWindows_list", Text: "window IDs", Kind: "reporter", Handler: func(c *e.Context) (e.Result, error) {
			v, err := c.Call("window.list")
			if err != nil {
				return e.Result{}, err
			}
			data, err := json.Marshal(v)
			return e.Result{Value: string(data)}, err
		}},
		{Opcode: "gowarpWindows_quit", Kind: "command", Handler: func(c *e.Context) (e.Result, error) {
			_, err := c.Call("host.command", "close")
			return e.Result{}, err
		}},
		{Opcode: "gowarpWindows_remove", Kind: "command", Inputs: []string{"ID"}, Handler: func(c *e.Context) (e.Result, error) {
			_, err := c.Call("window.remove", c.String("ID"))
			return e.Result{}, err
		}},
		{Opcode: "gowarpWindows_state", Text: "window [ID] is [STATE]?", Kind: "boolean", Inputs: []string{"ID"}, Handler: func(c *e.Context) (e.Result, error) {
			v, err := c.Call("window.state", c.String("ID"), c.Field("STATE"))
			return e.Result{Value: v}, err
		}},
		{Opcode: "gowarpWindows_get", Text: "window [ID] properties", Kind: "reporter", Inputs: []string{"ID"}, Handler: func(c *e.Context) (e.Result, error) {
			v, err := c.Call("window.get", c.String("ID"))
			return e.Result{Value: v}, err
		}},
	}}
}

func setProperty(c *e.Context, property string, value any) (e.Result, error) {
	data, err := json.Marshal(map[string]any{property: value})
	if err != nil {
		return e.Result{}, err
	}
	_, err = c.Call("window.set", c.String("ID"), string(data))
	return e.Result{}, err
}

}
*/

(function (Scratch) {
    "use strict";
    const str = (value) => ({type: Scratch.ArgumentType.STRING, defaultValue: value});
    const num = (value) => ({type: Scratch.ArgumentType.NUMBER, defaultValue: value});
    if (!Scratch.extensions.unsandboxed) throw new Error("Window API must run unsandboxed.");
    const runtime = Scratch.vm.runtime;
    const renderer = Scratch.vm.renderer;
    const number = (value) => Scratch.Cast.toNumber(value);
    const svgElement = (tag, attributes = {}) => {
        const node = document.createElementNS("http://www.w3.org/2000/svg", tag);
        for (const [key, value] of Object.entries(attributes)) node.setAttribute(key, String(value));
        return node;
    };
    class GoWarpWindows {
        constructor() {
            this.views = new Map();
            this.guides = new Map();
            this.overlay = null;
            this.autoMain = true;
            this.reset = () => {
                this.views.clear();
                this.guides.clear();
                this.autoMain = true;
                this.views.set("main", this.defaultView("main", runtime.stageWidth, runtime.stageHeight));
                this.draw();
            };
            this.stageChanged = () => {
                const main = this.views.get("main");
                if (main && this.autoMain) {
                    main.stageWidth = runtime.stageWidth;
                    main.stageHeight = runtime.stageHeight;
                }
                this.draw();
            };
            this.dispose = () => {
                this.removeOverlay();
                runtime.off("STAGE_SIZE_CHANGED", this.stageChanged);
                runtime.off("PROJECT_LOADED", this.reset);
                runtime.off("RUNTIME_DISPOSED", this.dispose);
            };
            runtime.on("STAGE_SIZE_CHANGED", this.stageChanged);
            runtime.on("PROJECT_LOADED", this.reset);
            runtime.on("RUNTIME_DISPOSED", this.dispose);
            this.reset();
        }
        defaultView(id, width, height) {
            return {
                id, title: id, x: 0, y: 0, width, height,
                stageX: 0, stageY: 0, stageWidth: width, stageHeight: height,
                border: true, resizable: true, disableMinimize: false, disableMaximize: false,
                disableClose: false, fullscreen: false, topmost: false, passthrough: false, hidden: false, opacity: 1
            };
        }
        view(id) {
            const view = this.views.get(String(id));
            if (!view) throw new Error(`Unknown window: ${id}`);
            return view;
        }
        validate(view) {
            for (const key of ["x", "y", "width", "height", "stageX", "stageY", "stageWidth", "stageHeight", "opacity"]) {
                if (typeof view[key] !== "number" || !Number.isFinite(view[key])) throw new Error(`Invalid window ${key}`);
            }
            if (!view.id || view.id.length > 128 || /\0/.test(view.id + view.title) ||
            !Number.isInteger(view.width) || !Number.isInteger(view.height) ||
            view.width < 1 || view.height < 1 || view.width > 4096 || view.height > 4096 ||
            view.stageWidth <= 0 || view.stageHeight <= 0 || view.stageWidth > 16384 || view.stageHeight > 16384 ||
            view.opacity < 0 || view.opacity > 1 || Math.abs(view.x) > 100000 || Math.abs(view.y) > 100000) {
                throw new Error("Invalid window dimensions, position or opacity");
            }
        }
        patch(id, patch) {
            const old = this.view(id);
            if (!patch || typeof patch !== "object" || Array.isArray(patch)) throw new Error("Window properties must be an object");
            for (const key of Object.keys(patch)) {
                if (!Object.hasOwn(old, key) || typeof patch[key] !== typeof old[key]) throw new Error(`Invalid window property: ${key}`);
            }
            const view = {...old, ...patch};
            if (view.id !== old.id) throw new Error("Cannot change a window ID");
            this.validate(view);
            this.views.set(view.id, view);
            if (view.id === "main" && ["stageX", "stageY", "stageWidth", "stageHeight"].some(key => Object.hasOwn(patch, key))) this.autoMain = false;
            this.draw();
        }
        removeOverlay() {
            if (this.overlay) renderer.removeOverlay(this.overlay);
            this.overlay = null;
        }
        draw() {
            const visible = [...this.views.values()].filter(view => !view.hidden && this.guides.get(view.id)?.show !== false);
            if (!visible.length) { this.removeOverlay(); return; }
            if (!this.overlay) {
                this.overlay = svgElement("svg");
                Object.assign(this.overlay.style, {width: "100%", height: "100%", pointerEvents: "none", overflow: "hidden"});
                this.overlay.setAttribute("aria-hidden", "true");
                const overlay = renderer.addOverlay(this.overlay, "scale");
                overlay.container.style.pointerEvents = "none";
            }
            this.overlay.setAttribute("viewBox", `0 0 ${runtime.stageWidth} ${runtime.stageHeight}`);
            this.overlay.replaceChildren();
            for (const view of visible) {
                const color = this.guides.get(view.id)?.color || "#ff5555";
                const rect = svgElement("rect", {
                    x: view.stageX, y: view.stageY, width: view.stageWidth, height: view.stageHeight,
                    fill: "none", stroke: color, "stroke-width": 2, "vector-effect": "non-scaling-stroke"
                });
                const label = svgElement("text", {x: view.stageX + 5, y: view.stageY + 16, fill: color, "font-size": 13, "font-family": "sans-serif"});
                label.textContent = view.id;
                this.overlay.append(rect, label);
            }
        }
        getInfo() {
            return {
                id: "gowarpWindows", name: "Window API", color1: "#e64b50",
                blocks: [
                    {
                        opcode: "viewport", blockType: Scratch.BlockType.COMMAND,
                        text: "set window [ID] stage area x [X] y [Y] width [W] height [H]",
                        arguments: {ID: str("main"), X: num(0), Y: num(0), W: num(480), H: num(360)}
                    },
                    {
                        opcode: "moveViewport", blockType: Scratch.BlockType.COMMAND,
                        text: "move window [ID] stage area to x [X] y [Y]",
                        arguments: {ID: str("main"), X: num(0), Y: num(0)}
                    },
                    {
                        opcode: "stageSize", blockType: Scratch.BlockType.REPORTER, text: "stage [DIMENSION]",
                        arguments: {DIMENSION: {type: Scratch.ArgumentType.STRING, menu: "dimensions", defaultValue: "width"}}
                    },
                    {
                        opcode: "boundary", blockType: Scratch.BlockType.COMMAND,
                        text: "editor boundary [ID] visible [SHOW] color [COLOR]",
                        arguments: {ID: str("main"), SHOW: {type: Scratch.ArgumentType.STRING, menu: "booleans", defaultValue: "true"},
                        COLOR: {type: Scratch.ArgumentType.COLOR, defaultValue: "#ff5555"}}
                    },
                    {
                        opcode: "create", blockType: Scratch.BlockType.COMMAND,
                        text: "create window [ID] x [X] y [Y] width [W] height [H] border [BORDER]",
                        arguments: {ID: str("view"), X: num(100), Y: num(100), W: num(480), H: num(360), BORDER: {type: Scratch.ArgumentType.STRING, menu: "booleans", defaultValue: "true"}}
                    },
                    {
                        opcode: "move", blockType: Scratch.BlockType.COMMAND, text: "move window [ID] to x [X] y [Y]",
                        arguments: {ID: str("main"), X: num(100), Y: num(100)}
                    },
                    {
                        opcode: "set", blockType: Scratch.BlockType.COMMAND, text: "set window [ID] properties [JSON]",
                        arguments: {ID: str("main"), JSON: str('{"border":false,"opacity":0.8}')}
                    },
                    {
                        opcode: "setBoolean", blockType: Scratch.BlockType.COMMAND,
                        text: "set window [ID] [PROPERTY] to [VALUE]",
                        arguments: {ID: str("main"), PROPERTY: {type: Scratch.ArgumentType.STRING, menu: "properties", defaultValue: "border"},
                        VALUE: {type: Scratch.ArgumentType.STRING, menu: "booleans", defaultValue: "true"}}
                    },
                    {
                        opcode: "topmost", blockType: Scratch.BlockType.COMMAND,
                        text: "set window [ID] always on top to [VALUE]",
                        arguments: {ID: str("main"), VALUE: {type: Scratch.ArgumentType.STRING, menu: "booleans", defaultValue: "true"}}
                    },
                    {
                        opcode: "opacity", blockType: Scratch.BlockType.COMMAND, text: "set window [ID] opacity to [PERCENT] %",
                        arguments: {ID: str("main"), PERCENT: num(100)}
                    },
                    {
                        opcode: "list", blockType: Scratch.BlockType.REPORTER, text: "window IDs", disableMonitor: true
                    },
                    {
                        opcode: "quit", blockType: Scratch.BlockType.COMMAND, text: "close player"
                    },
                    {
                        opcode: "remove", blockType: Scratch.BlockType.COMMAND, text: "remove window [ID]",
                        arguments: {ID: str("view")}
                    },
                    {
                        opcode: "state", blockType: Scratch.BlockType.BOOLEAN, text: "window [ID] is [STATE]?",
                        arguments: {ID: str("main"), STATE: {type: Scratch.ArgumentType.STRING, menu: "states", defaultValue: "open"}}
                    },
                    {
                        opcode: "get", blockType: Scratch.BlockType.REPORTER, text: "window [ID] properties",
                        disableMonitor: true, arguments: {ID: str("main")}
                    }
                ],
                menus: {
                    states: {acceptReporters: false, items: ["open", "minimized", "maximized", "focused"]},
                    dimensions: {acceptReporters: false, items: ["width", "height"]},
                    booleans: {acceptReporters: false, items: ["true", "false"]},
                    properties: {acceptReporters: false, items: ["border", "resizable", "disableMinimize", "disableMaximize", "disableClose", "fullscreen"]}
                }
            };
        }
        viewport({ID, X, Y, W, H}) {
            this.patch(ID, {stageX: number(X), stageY: number(Y), stageWidth: number(W), stageHeight: number(H)});
        }
        moveViewport({ID, X, Y}) { this.patch(ID, {stageX: number(X), stageY: number(Y)}); }
        stageSize({DIMENSION}) { return DIMENSION === "height" ? runtime.stageHeight : runtime.stageWidth; }
        boundary({ID, SHOW, COLOR}) {
            const view = this.view(ID);
            const color = Scratch.Cast.toRgbColorObject(COLOR);
            this.guides.set(view.id, {show: Scratch.Cast.toBoolean(SHOW), color: `rgb(${color.r}, ${color.g}, ${color.b})`});
            this.draw();
        }
        create({ID, X, Y, W, H, BORDER}) {
            const id = String(ID);
            if (this.views.has(id)) throw new Error(`Duplicate window: ${id}`);
            if (this.views.size >= 16) throw new Error("At most 16 windows are supported");
            const view = {...this.defaultView(id, number(W), number(H)), x: Math.round(number(X)), y: Math.round(number(Y)), border: Scratch.Cast.toBoolean(BORDER)};
            this.validate(view);
            this.views.set(id, view);
            this.draw();
        }
        move({ID, X, Y}) { this.patch(ID, {x: Math.round(number(X)), y: Math.round(number(Y))}); }
        set({ID, JSON: properties}) { this.patch(ID, JSON.parse(String(properties))); }
        setBoolean({ID, PROPERTY, VALUE}) { this.patch(ID, {[PROPERTY]: Scratch.Cast.toBoolean(VALUE)}); }
        topmost({ID, VALUE}) { this.patch(ID, {topmost: Scratch.Cast.toBoolean(VALUE)}); }
        opacity({ID, PERCENT}) { this.patch(ID, {opacity: number(PERCENT) / 100}); }
        list() { return JSON.stringify([...this.views.keys()]); }
        quit() { this.views.clear(); this.guides.clear(); this.removeOverlay(); }
        remove({ID}) { this.view(ID); this.views.delete(String(ID)); this.guides.delete(String(ID)); this.draw(); }
        state({ID, STATE}) {
            if (!["open", "minimized", "maximized", "focused"].includes(STATE)) throw new Error(`Unknown window state: ${STATE}`);
            const view = this.views.get(String(ID));
            if (!view) return false;
            if (STATE === "open") return true;
            if (STATE === "focused") return view.id === "main" && !view.hidden && document.hasFocus();
            return false;
        }
        get({ID}) { return JSON.stringify(this.view(ID)); }
    }
    Scratch.extensions.register(new GoWarpWindows());
})(Scratch);