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
	"fmt"
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
		{Opcode: "gowarpWindows_monitor", Text: "monitor [PROPERTY] of window [ID]", Kind: "reporter", Inputs: []string{"ID"}, Handler: func(c *e.Context) (e.Result, error) {
			v, err := c.Call("window.monitor", c.String("ID"))
			if err != nil {
				return e.Result{}, err
			}
			var monitor map[string]float64
			if err := json.Unmarshal([]byte(e.String(v)), &monitor); err != nil {
				return e.Result{}, err
			}
			value, ok := monitor[c.Field("PROPERTY")]
			if !ok {
				return e.Result{}, fmt.Errorf("unknown monitor property %q", c.Field("PROPERTY"))
			}
			return e.Result{Value: value}, nil
		}},
		{Opcode: "gowarpWindows_center", Kind: "command", Inputs: []string{"ID"}, Handler: func(c *e.Context) (e.Result, error) {
			scope := c.Field("SCOPE")
			if scope != "window" && scope != "all" {
				return e.Result{}, fmt.Errorf("unknown centering scope %q", scope)
			}
			_, err := c.Call("window.center", c.String("ID"), scope == "all")
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
			return setProperty(c, "opacity", c.Number("PERCENT")/100)
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
    const tr = (en, ko) => (Scratch.translate.language || "en").toLowerCase().startsWith("ko") ? ko : en;
    const str = (value) => ({type: Scratch.ArgumentType.STRING, defaultValue: value});
    const num = (value) => ({type: Scratch.ArgumentType.NUMBER, defaultValue: value});
    if (!Scratch.extensions.unsandboxed) throw new Error(tr("Window API must run unsandboxed.", "창 API는 샌드박스 없이 실행해야 합니다."));
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
            if (!view) throw new Error(tr(`Unknown window: ${id}`, `알 수 없는 창: ${id}`));
            return view;
        }
        validate(view) {
            for (const key of ["x", "y", "width", "height", "stageX", "stageY", "stageWidth", "stageHeight", "opacity"]) {
                if (typeof view[key] !== "number" || !Number.isFinite(view[key])) throw new Error(tr(`Invalid window ${key}`, `올바르지 않은 창 속성: ${key}`));
            }
            if (!view.id || view.id.length > 128 || /\0/.test(view.id + view.title) ||
            !Number.isInteger(view.width) || !Number.isInteger(view.height) ||
            view.width < 1 || view.height < 1 || view.width > 4096 || view.height > 4096 ||
            view.stageWidth <= 0 || view.stageHeight <= 0 || view.stageWidth > 16384 || view.stageHeight > 16384 ||
            view.opacity < 0 || view.opacity > 1 || Math.abs(view.x) > 100000 || Math.abs(view.y) > 100000) {
                throw new Error(tr("Invalid window dimensions, position or opacity", "창의 크기, 위치 또는 불투명도가 올바르지 않습니다."));
            }
        }
        patch(id, patch) {
            const old = this.view(id);
            if (!patch || typeof patch !== "object" || Array.isArray(patch)) throw new Error(tr("Window properties must be an object", "창 속성은 객체여야 합니다."));
            for (const key of Object.keys(patch)) {
                if (!Object.hasOwn(old, key) || typeof patch[key] !== typeof old[key]) throw new Error(tr(`Invalid window property: ${key}`, `올바르지 않은 창 속성: ${key}`));
            }
            const view = {...old, ...patch};
            if (view.id !== old.id) throw new Error(tr("Cannot change a window ID", "창 ID는 변경할 수 없습니다."));
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
                id: "gowarpWindows", name: tr("Window API", "창 API"), color1: "#e64b50",
                blocks: [
                    {
                        opcode: "viewport", blockType: Scratch.BlockType.COMMAND,
                        text: tr("set window [ID] stage area x [X] y [Y] width [W] height [H]", "창 [ID]의 무대 영역을 x [X] y [Y] 너비 [W] 높이 [H]로 정하기"),
                        arguments: {ID: str("main"), X: num(0), Y: num(0), W: num(480), H: num(360)}
                    },
                    {
                        opcode: "moveViewport", blockType: Scratch.BlockType.COMMAND,
                        text: tr("move window [ID] stage area to x [X] y [Y]", "창 [ID]의 무대 영역을 x [X] y [Y]로 옮기기"),
                        arguments: {ID: str("main"), X: num(0), Y: num(0)}
                    },
                    {
                        opcode: "stageSize", blockType: Scratch.BlockType.REPORTER, text: tr("stage [DIMENSION]", "무대 [DIMENSION]"),
                        arguments: {DIMENSION: {type: Scratch.ArgumentType.STRING, menu: "dimensions", defaultValue: "width"}}
                    },
                    {
                        opcode: "boundary", blockType: Scratch.BlockType.COMMAND,
                        text: tr("editor boundary [ID] visible [SHOW] color [COLOR]", "편집기 경계 [ID] 표시 [SHOW] 색상 [COLOR]"),
                        arguments: {ID: str("main"), SHOW: {type: Scratch.ArgumentType.STRING, menu: "booleans", defaultValue: "true"},
                        COLOR: {type: Scratch.ArgumentType.COLOR, defaultValue: "#ff5555"}}
                    },
                    {
                        opcode: "create", blockType: Scratch.BlockType.COMMAND,
                        text: tr("create window [ID] x [X] y [Y] width [W] height [H] border [BORDER]", "창 [ID] 만들기 x [X] y [Y] 너비 [W] 높이 [H] 테두리 [BORDER]"),
                        arguments: {ID: str("view"), X: num(100), Y: num(100), W: num(480), H: num(360), BORDER: {type: Scratch.ArgumentType.STRING, menu: "booleans", defaultValue: "true"}}
                    },
                    {
                        opcode: "monitor", blockType: Scratch.BlockType.REPORTER,
                        text: tr("monitor [PROPERTY] of window [ID]", "창 [ID]가 있는 모니터의 [PROPERTY]"),
                        arguments: {ID: str("main"), PROPERTY: {type: Scratch.ArgumentType.STRING, menu: "monitorProperties", defaultValue: "workWidth"}}
                    },
                    {
                        opcode: "center", blockType: Scratch.BlockType.COMMAND,
                        text: tr("center [SCOPE] on window [ID]'s monitor", "창 [ID]가 있는 모니터 가운데에 [SCOPE] 배치하기"),
                        arguments: {ID: str("main"), SCOPE: {type: Scratch.ArgumentType.STRING, menu: "centerScopes", defaultValue: "window"}}
                    },
                    {
                        opcode: "move", blockType: Scratch.BlockType.COMMAND, text: tr("move window [ID] to x [X] y [Y]", "창 [ID]을(를) x [X] y [Y]로 옮기기"),
                        arguments: {ID: str("main"), X: num(100), Y: num(100)}
                    },
                    {
                        opcode: "set", blockType: Scratch.BlockType.COMMAND, text: tr("set window [ID] properties [JSON]", "창 [ID]의 속성을 [JSON]로 정하기"),
                        arguments: {ID: str("main"), JSON: str('{"border":false,"opacity":0.8}')}
                    },
                    {
                        opcode: "setBoolean", blockType: Scratch.BlockType.COMMAND,
                        text: tr("set window [ID] [PROPERTY] to [VALUE]", "창 [ID]의 [PROPERTY]을(를) [VALUE]로 정하기"),
                        arguments: {ID: str("main"), PROPERTY: {type: Scratch.ArgumentType.STRING, menu: "properties", defaultValue: "border"},
                        VALUE: {type: Scratch.ArgumentType.STRING, menu: "booleans", defaultValue: "true"}}
                    },
                    {
                        opcode: "topmost", blockType: Scratch.BlockType.COMMAND,
                        text: tr("set window [ID] always on top to [VALUE]", "창 [ID]의 항상 맨 위를 [VALUE]로 정하기"),
                        arguments: {ID: str("main"), VALUE: {type: Scratch.ArgumentType.STRING, menu: "booleans", defaultValue: "true"}}
                    },
                    {
                        opcode: "opacity", blockType: Scratch.BlockType.COMMAND, text: tr("set window [ID] opacity to [PERCENT] %", "창 [ID]의 불투명도를 [PERCENT] %로 정하기"),
                        arguments: {ID: str("main"), PERCENT: num(100)}
                    },
                    {
                        opcode: "list", blockType: Scratch.BlockType.REPORTER, text: tr("window IDs", "창 ID 목록"), disableMonitor: true
                    },
                    {
                        opcode: "quit", blockType: Scratch.BlockType.COMMAND, text: tr("close player", "플레이어 종료하기")
                    },
                    {
                        opcode: "remove", blockType: Scratch.BlockType.COMMAND, text: tr("remove window [ID]", "창 [ID] 삭제하기"),
                        arguments: {ID: str("view")}
                    },
                    {
                        opcode: "state", blockType: Scratch.BlockType.BOOLEAN, text: tr("window [ID] is [STATE]?", "창 [ID]이(가) [STATE] 상태인가?"),
                        arguments: {ID: str("main"), STATE: {type: Scratch.ArgumentType.STRING, menu: "states", defaultValue: "open"}}
                    },
                    {
                        opcode: "get", blockType: Scratch.BlockType.REPORTER, text: tr("window [ID] properties", "창 [ID]의 속성"),
                        disableMonitor: true, arguments: {ID: str("main")}
                    }
                ],
                menus: {
                    centerScopes: {acceptReporters: false, items: [
                        {text: tr("this window", "이 창"), value: "window"},
                        {text: tr("all windows together", "모든 창을 함께"), value: "all"}
                    ]},
                    monitorProperties: {acceptReporters: false, items: [
                        {text: tr("x", "x좌표"), value: "x"},
                        {text: tr("y", "y좌표"), value: "y"},
                        {text: tr("width", "너비"), value: "width"},
                        {text: tr("height", "높이"), value: "height"},
                        {text: tr("work area x", "작업 영역 x좌표"), value: "workX"},
                        {text: tr("work area y", "작업 영역 y좌표"), value: "workY"},
                        {text: tr("work area width", "작업 영역 너비"), value: "workWidth"},
                        {text: tr("work area height", "작업 영역 높이"), value: "workHeight"}
                    ]},
                    states: {acceptReporters: false, items: [
                        {text: tr("open", "열림"), value: "open"},
                        {text: tr("minimized", "최소화"), value: "minimized"},
                        {text: tr("maximized", "최대화"), value: "maximized"},
                        {text: tr("focused", "포커스됨"), value: "focused"}
                    ]},
                    dimensions: {acceptReporters: false, items: [
                        {text: tr("width", "너비"), value: "width"},
                        {text: tr("height", "높이"), value: "height"}
                    ]},
                    booleans: {acceptReporters: false, items: [
                        {text: tr("true", "켜기"), value: "true"},
                        {text: tr("false", "끄기"), value: "false"}
                    ]},
                    properties: {acceptReporters: false, items: [
                        {text: tr("border", "테두리 표시"), value: "border"},
                        {text: tr("resizable", "크기 변경 허용"), value: "resizable"},
                        {text: tr("disableMinimize", "최소화 버튼 비활성화"), value: "disableMinimize"},
                        {text: tr("disableMaximize", "최대화 버튼 비활성화"), value: "disableMaximize"},
                        {text: tr("disableClose", "닫기 버튼 비활성화"), value: "disableClose"},
                        {text: tr("fullscreen", "전체화면"), value: "fullscreen"}
                    ]}
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
            if (this.views.has(id)) throw new Error(tr(`Duplicate window: ${id}`, `이미 있는 창: ${id}`));
            if (this.views.size >= 16) throw new Error(tr("At most 16 windows are supported", "창은 최대 16개까지 지원합니다."));
            const view = {...this.defaultView(id, number(W), number(H)), x: Math.round(number(X)), y: Math.round(number(Y)), border: Scratch.Cast.toBoolean(BORDER)};
            this.validate(view);
            this.views.set(id, view);
            this.draw();
        }
        monitorInfo(id) {
            this.view(id);
            // The browser can only describe its current screen, not simulated native windows.
            const screen = globalThis.screen;
            if (!screen) throw new Error(tr("Monitor information is unavailable", "모니터 정보를 가져올 수 없습니다."));
            const dpi = globalThis.devicePixelRatio || 1;
            const px = value => Math.round(value * dpi);
            return {
                x: px(screen.left ?? 0), y: px(screen.top ?? 0), width: px(screen.width), height: px(screen.height),
                workX: px(screen.availLeft ?? screen.left ?? 0), workY: px(screen.availTop ?? screen.top ?? 0),
                workWidth: px(screen.availWidth), workHeight: px(screen.availHeight)
            };
        }
        monitor({ID, PROPERTY}) {
            const monitor = this.monitorInfo(ID);
            if (!Object.hasOwn(monitor, PROPERTY)) throw new Error(tr(`Unknown monitor property: ${PROPERTY}`, `알 수 없는 모니터 속성: ${PROPERTY}`));
            return monitor[PROPERTY];
        }
        center({ID, SCOPE}) {
            if (!["window", "all"].includes(SCOPE)) throw new Error(tr(`Unknown centering scope: ${SCOPE}`, `알 수 없는 배치 범위: ${SCOPE}`));
            const monitor = this.monitorInfo(ID);
            const anchor = this.view(ID);
            if (SCOPE === "window" && anchor.fullscreen) throw new Error(tr("Restore the window before centering", "가운데 배치하기 전에 창을 일반 상태로 복원하세요."));
            const views = SCOPE === "all" ? [...this.views.values()].filter(v => !v.hidden && !v.fullscreen) : [anchor];
            if (!views.length) return;
            const left = Math.min(...views.map(v => v.x)), top = Math.min(...views.map(v => v.y));
            const right = Math.max(...views.map(v => v.x + v.width)), bottom = Math.max(...views.map(v => v.y + v.height));
            const dx = monitor.workX + Math.max(0, Math.floor((monitor.workWidth - (right - left)) / 2)) - left;
            const dy = monitor.workY + Math.max(0, Math.floor((monitor.workHeight - (bottom - top)) / 2)) - top;
            const moved = views.map(v => ({...v, x: v.x + dx, y: v.y + dy}));
            for (const v of moved) this.validate(v);
            for (const v of moved) this.views.set(v.id, v);
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
            if (!["open", "minimized", "maximized", "focused"].includes(STATE)) throw new Error(tr(`Unknown window state: ${STATE}`, `알 수 없는 창 상태: ${STATE}`));
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