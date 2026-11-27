import type { PaintTools, EditorDocument, EditorWindow, PaintContext, PaintElement, PaintSettings, PaintColor, Shape } from './type';
import { round, cornerPath, SHAPES, NIBS, PENS } from './shapes';
import { createIconButton, isTextInput } from '../shared/dom';
import { createLifecycle } from '../shared/lifecycle';
import type { ScratchHost } from '../shared/scratch';
import { createFolders } from '../sprites/folders';
import { createEditorAccess } from './editor';
import { paintStyle as CSS } from './style';
import { createAnchors } from './anchors';
import { createLayers } from './layers';
import { createTheme } from './theme';
import { preparePenStroke } from './stroke';
import { tr } from '../translate';

export function installPaintTools(Scratch: ScratchHost): PaintTools {
    const document = globalThis.document as EditorDocument;
    const window = globalThis.window as EditorWindow;
    const lifecycle = createLifecycle();
    const {listen, later, repeat} = lifecycle;
    const editor = createEditorAccess(window);
    const {paintCanvas, fiberOf, paper, paintState, isBitmapFormat, isEraserMode, dispatch, store} = editor;
    const theme = createTheme(document, window);
    const {nativeClass, readTheme, applyTheme} = theme;
    let lastLanguage = tr().lang;
    const context: PaintContext = {document, window, lifecycle, editor, theme};
    let disposed = false;
    let unsubscribe: (() => void) | undefined;
    let sheet: CSSStyleSheet | undefined;
    let styleTag: HTMLStyleElement | undefined;
    var LS_KEY = 'gowarp-toolkit.paint';
    var ART_CENTER = {x: 480, y: 360};

    function clamp(v: number, a: number, b: number) {
        return Math.min(b, Math.max(a, v));
    }
    function load(fallback: PaintSettings): PaintSettings {
        try {
            var raw = window.localStorage.getItem(LS_KEY);
            return raw ? Object.assign({}, fallback, JSON.parse(raw)) : fallback;
        } catch (e) {
            return fallback;
        }
    }
    function save(data: PaintSettings) {
        try {
            window.localStorage.setItem(LS_KEY, JSON.stringify(data));
        } catch (e) {
        }
    }

    function findUpdateImage() {
        var canvas = paintCanvas();
        var fiber = canvas ? fiberOf(canvas) : null;
        for (var i = 0; i < 45 && fiber; i++) {
            var props = fiber.stateNode && fiber.stateNode.props ? fiber.stateNode.props : fiber.memoizedProps;
            if (props && typeof props.onUpdateImage === 'function') return props.onUpdateImage;
            fiber = fiber.return;
        }
        return null;
    }

    function commit() {
        var fn = findUpdateImage();
        if (!fn) return false;
        fn(false);
        return true;
    }

    function findLayer(flag: string) {
        var p = paper();
        if(!p) return null;
        var layers = p.project.layers;
        for(let i=0; i < layers.length; i++) {
            if (layers[i].data && layers[i].data[flag]) return layers[i];
        }
        return null;
    }
    function paintingLayer () {
        return findLayer('isPaintingLayer');
    }
    function rasterItem () {
        var layer = findLayer('isRasterLayer');
        return layer && layer.children.length ? layer.children[0] : null;
    }
    function guideLayer () {
        var p = paper();
        if(!p) return null;
        var layer = findLayer('isGuideLayer');
        if(!layer) {
            layer = new p.Layer();
            layer.data = {isGuideLayer: true};
            var art = paintingLayer();
            if(art) art.activate();
        }
        return layer;
    }

    var CSS_COLOR = /^(#([0-9a-f]{3,4}|[0-9a-f]{6}|[0-9a-f]{8})|rgba?\(|hsla?\(|hwb\()/i;
    function colorOf(style: PaintColor) {
        var p = paper();
        var v = style && style.primary;
        if (!p || typeof v !== 'string') return null;
        v = v.trim();
        if (!CSS_COLOR.test(v)) return null;
        try {
            return new p.Color(v);
        } catch (e) {
            return null;
        }
    }
    var lastPaint = {
        fill: null,
        stroke: null
    };
    function pickColor(style: PaintColor, key: string) {
        var color = colorOf(style);
        if (color) {
            lastPaint[key] = style.primary;
            return color;
        }
        return lastPaint[key] ? colorOf({primary: lastPaint[key]}) : null;
    }
    function currentColors() {
        var ps = paintState();
        var color = ps && ps.color ? ps.color : {};
        return {
            fill: pickColor(color.fillColor, 'fill'),
            stroke: pickColor(color.strokeColor, 'stroke'),
            strokeWidth: color.strokeWidth || 0
        };
    }
    function applyStyle(path: any, useStroke: boolean) {
        var colors = currentColors();
        var mine = gwtPaint('fill', path.bounds, 0);
        var stroke = useStroke && colors.stroke;
        path.fillColor = mine === null ? colors.fill : mine;
        path.strokeColor = stroke ? colors.stroke : null;
        path.strokeWidth = stroke ? Math.max(colors.strokeWidth, 1) : 0;
        path.strokeCap = 'round';
        path.strokeJoin = 'round';
    }

    function usable(v: unknown) {
        return typeof v === 'string' && CSS_COLOR.test(v.trim());
    }
    function transparentLike (p: any, css: string) {
        var c = new p.Color(css);
        c.alpha = 0;
        return c.toCSS();
    }
    function gradientColor(p: any, stops: [number, string][], type: string, bounds: any, minSize: number) {
        var list = stops.map(function (s: [number, string], i: number) {
            var c = s[1];
            if (c === null) {
                var other = stops[i === 0 ? stops.length - 1 : 0][1] || stops[0][1];
                c = transparentLike(p, other);
            }
            return [c, s[0]];
        });
        var size = Math.max(minSize || 0, 1e-2);
        var start, end;
        if (type === 'RADIAL') {
            start = bounds.center;
            end = start.add(new p.Point(Math.max(Math.max(bounds.width, bounds.height) / 2, size / 2), 0));
        } else {
            start = (type === 'VERTICAL' ? bounds.topCenter : bounds.leftCenter).clone();
            end = (type === 'VERTICAL' ? bounds.bottomCenter : bounds.rightCenter).clone();
            var span = type === 'VERTICAL' ? Math.abs(end.y - start.y) : Math.abs(end.x - start.x);
            if (span < size) {
                var diff = (size - span) / 2;
                if (type === 'VERTICAL') { end.y += diff; start.y -= diff; }
                else { end.x += diff; start.x -= diff; }
            }
        }
        return {
            gradient: {stops: list, radial: type === 'RADIAL'},
            origin: start,
            destination: end
        };
    }
    function nativeStyle(key: string) {
        var color = (paintState() || {}).color || {};
        return color[key === 'stroke' ? 'strokeColor' : 'fillColor'] || {};
    }
    function gradType(key: string) {
        var type = nativeStyle(key).gradientType;
        return type === 'RADIAL' || type === 'HORIZONTAL' || type === 'VERTICAL' ? type : null;
    }
    function gradStops (key: string) {
        var g = settings.grad;
        var style = nativeStyle(key);
        var a = usable(style.primary) ? style.primary : (lastPaint[key] || null);
        if (!a) return null;
        var b = usable(style.secondary) ? style.secondary : a;
        var out = [{p: g.offsets[0], c: a}];
        for (var i = 1; i < g.count - 1; i++) out.push({p: g.offsets[i], c: g.extras[i - 1]});
        out.push({p: g.offsets[g.count - 1], c: b});
        return out;
    }
    function gwtGradient (key: string) {
        if (!paper() || !gradType(key)) return null;
        var stops = gradStops(key);
        if (!stops) return null;
        return stops.map(function (s: {p: number; c: string}): [number, string] { return [s.p, usable(s.c) ? s.c : null]; });
    }
    function gwtPaint (key: string, bounds: any, minSize: number, type?: string) {
        var p = paper();
        var stops = gwtGradient(key);
        if (!p || !stops) return null;
        return gradientColor(p, stops, type || gradType(key), bounds, minSize);
    }
    function styleShapePath (path: any) {
        var p = paper();
        var color = (paintState() || {}).color || {};
        var width = color.strokeWidth || 0;
        [['fill', 'fillColor'], ['stroke', 'strokeColor']].forEach(function (pair: string[]) {
            var mine = gwtPaint(pair[0], path.bounds, pair[0] === 'stroke' ? width : 0);
            if (mine !== null) {
                path[pair[1]] = mine;
                return;
            }
            var style = color[pair[1]] || {};
            var type = style.gradientType;
            var one = usable(style.primary) ? style.primary : (lastPaint[pair[0]] || null);
            if (usable(style.primary)) lastPaint[pair[0]] = style.primary;
            var two = usable(style.secondary) ? style.secondary : null;
            if (!one && !two) {
                path[pair[1]] = null;
                return;
            }
            if (type === 'RADIAL' || type === 'HORIZONTAL' || type === 'VERTICAL') {
                path[pair[1]] = gradientColor(p, [[0, one || two], [1, two || one]], type,
                    path.bounds, pair[0] === 'stroke' ? width : 0);
            } else {
                path[pair[1]] = one || two;
            }
        });
        path.strokeWidth = width;
        if (!path.strokeColor) path.strokeWidth = 0;
    }

    function nibSwatch (profile: typeof PENS[number]) {
        var pts = [];
        var i;
        for (i = 0; i <= 16; i++) {
            var u = i / 16;
            pts.push({x: 3 + u * 38, y: 10 - Math.sin(u * Math.PI) * 4.5, p: 0.25 + 0.6 * Math.sin(Math.PI * u)});
        }
        var left = [];
        var right = [];
        for (i = 0; i < pts.length; i++) {
            var prev = pts[Math.max(0, i - 1)];
            var next = pts[Math.min(pts.length - 1, i + 1)];
            var angle = Math.atan2(next.y - prev.y, next.x - prev.x);
            var width = 6 * (profile.min + (1 - profile.min) * Math.pow(clamp(pts[i].p, 0.05, 1), profile.exponent));
            if (profile.chisel) width *= 0.3 + 0.7 * Math.abs(Math.sin(angle - Math.PI / 5));
            width = Math.max(0.4, width * (1 - profile.taper * 0.6 * Math.sin(Math.PI * (i / (pts.length - 1)))));
            var nx = -Math.sin(angle) * width / 2;
            var ny = Math.cos(angle) * width / 2;
            left.push(round(pts[i].x + nx) + ',' + round(pts[i].y + ny));
            right.unshift(round(pts[i].x - nx) + ',' + round(pts[i].y - ny));
        }
        return '<svg viewBox="0 0 44 20"><path d="M' + left.concat(right).join('L') + 'Z" fill="currentColor"/></svg>';
    }

    var settings = load({
        nib: 10, eraser: 40, bitNib: 10, bitEraser: 40,
        shapeSize: 160, shapeRound: 0, pen: 'kalem', penSize: 22, penPressure: true, pngScale: 1,
        grad: {count: 2, offsets: [0, 1], extras: []}
    });

    (function migrateGrad () {
        var old = settings.grad;
        var count = Math.min(4, Math.max(2, Number(old && old.count) || 2));
        var from = old && Array.isArray(old.stops) && old.stops.length === count
            ? old.stops.map(function (s: { p: number; c: string; }) { return s.p; }) : null;
        var offsets = old && Array.isArray(old.offsets) && old.offsets.length === count ? old.offsets : from;
        var extras = old && Array.isArray(old.extras) ? old.extras
            : old && Array.isArray(old.stops) ? old.stops.slice(1, -1).map(function (s: { p: number; c: string; }) { return s.c; }) : [];
        settings.grad = {count: count, offsets: [], extras: []};
        for (var i = 0; i < count; i++) {
            var v = Number(offsets && offsets[i]);
            settings.grad.offsets.push(isFinite(v) ? clamp(v, 0, 1) : i / (count - 1));
        }
        for (i = 0; i < count - 2; i++) {
            var c = extras[i];
            settings.grad.extras.push(usable(c) ? c : '#8899aa');
        }
    })();
    function saveSettings () {
        save(settings);
    }

    function nibAction (size: number) {
        var bitmap = isBitmapFormat();
        var eraser = isEraserMode();
        if (bitmap) {
            return eraser
            ? {type: 'scratch-paint/eraser-mode/CHANGE_BIT_ERASER_SIZE', brushSize: size}
            : {type: 'scratch-paint/brush-mode/CHANGE_BIT_BRUSH_SIZE', brushSize: size};
        }
        return eraser
        ? {type: 'scratch-paint/eraser-mode/CHANGE_ERASER_SIZE', brushSize: size}
        : {type: 'scratch-paint/brush-mode/CHANGE_BRUSH_SIZE', brushSize: size};
    }
    function currentNibSize () {
        var ps = paintState();
        if (!ps) return settings.nib;
        if (isBitmapFormat()) return isEraserMode() ? ps.bitEraserSize : ps.bitBrushSize;
        return isEraserMode() ? (ps.eraserMode || {}).brushSize : (ps.brushMode || {}).brushSize;
    }
    function applyNib (size: number) {
        size = clamp(Math.round(size), 1, 100);
        if (isBitmapFormat()) {
            if (isEraserMode()) settings.bitEraser = size; else settings.bitNib = size;
        } else if (isEraserMode()) settings.eraser = size; else settings.nib = size;
        dispatch(nibAction(size));
        saveSettings();
        refreshNibUi();
    }
    function stepNib (delta: number) {
        var size = currentNibSize();
        applyNib(size + delta * Math.max(1, Math.round(size * 0.18)));
    }

    var openPanel = null;
    var openAnchor = null;
    var toolButtons = [];

    function markTool (btn: HTMLElement, on: boolean, fragment: string) {
        if (!btn || !btn.classList.contains('gwt-tool')) return;
        var cls = nativeClass(fragment);
        if (!cls) return;
        if (on) btn.classList.add(cls); else btn.classList.remove(cls);
    }

    function closePanel () {
        if (openPanel) {
            markTool(openAnchor, false, 'tool-select-base_is-selected');
            openPanel.remove();
        }
        openPanel = null;
        openAnchor = null;
        clearShapePreview();
    }

    function artRect () {
        var c = document.querySelector('canvas[class*="paper-canvas"]');
        if (!c) return null;
        var r = c.getBoundingClientRect();
        if (r.width < 8) return null;
        return r;
    }
    function overlapArea (a: Pick<DOMRect, 'left' | 'top' | 'right' | 'bottom'>, b: Pick<DOMRect, 'left' | 'top' | 'right' | 'bottom'>) {
        if (!a || !b) return 0;
        var w = Math.min(a.right, b.right) - Math.max(a.left, b.left);
        var h = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
        return w > 0 && h > 0 ? w * h : 0;
    }
    function boxOf (left: number, top: number, w: number, h: number) {
        return {left: left, top: top, right: left + w, bottom: top + h};
    }

    function fitPanel (panel: PaintElement) {
        var rect = panel._gwtAnchor.getBoundingClientRect();
        var box = panel.getBoundingClientRect();
        var art = artRect();
        var vw = window.innerWidth, vh = window.innerHeight;
        var fits = function (x: number, y: number) {
            return x >= 8 && y >= 8 && x + box.width <= vw - 8 && y + box.height <= vh - 8;
        };
        var tops = [clamp(rect.top, 8, Math.max(8, vh - box.height - 8)),
            art ? art.bottom + 8 : 0, art ? art.top - box.height - 8 : 0];
        var xs = [rect.right + 10, rect.left - box.width - 10, vw - box.width - 8, 8];
        var cands = [];
        xs.forEach(function (x: number) {
            if (Math.abs(x - rect.left) > box.width + rect.width + 60) return;
            tops.forEach(function (y: number) {
                if (!fits(x, y)) return;
                cands.push({x: x, y: y, d: Math.abs(x - rect.left), hit: overlapArea(boxOf(x, y, box.width, box.height), art)});
            });
        });
        if (!cands.length) cands.push({x: xs[0], y: tops[0], d: 0, hit: 0});
        cands.sort(function (a: {x: number; y: number; d: number; hit: number}, b: {x: number; y: number; d: number; hit: number}) {
            return a.hit !== b.hit ? a.hit - b.hit : a.d - b.d;
        });
        var best = cands[0];
        panel.style.setProperty('--gwt-origin', best.x > rect.left ? 'left center' : 'right center');
        panel.style.left = best.x + 'px';
        panel.style.top = best.y + 'px';
    }

    function toggleFor (anchor: PaintElement, kind: string, extraClass?: string) {
        if (openPanel && openAnchor === anchor) {
            closePanel();
            return null;
        }
        closePanel();

        var panel = document.createElement('div');
        panel.className = 'gwt-panel' + (extraClass ? ' ' + extraClass : '');
        panel.setAttribute('dir', tr().isRtl ? 'rtl' : 'ltr');
        panel._gwtAnchor = anchor;
        panel._gwtKind = kind;
        var theme = readTheme();
        applyTheme(panel, theme);
        if (anchor.parentElement) anchor.parentElement.style.setProperty('--gwt-hl', theme.accent);
        var close = document.createElement('button');
        close.type = 'button';
        close.className = 'gwt-close';
        close.setAttribute('aria-label', tr('close'));
        close.textContent = '✕';
        listen(close, 'click', closePanel);
        panel.appendChild(close);
        var rect = anchor.getBoundingClientRect();
        panel.style.left = rect.right + 10 + 'px';
        panel.style.top = rect.top + 'px';
        document.body.appendChild(panel);
        openPanel = panel;
        openAnchor = anchor;
        markTool(anchor, true, 'tool-select-base_is-selected');
        later(function () {
            if (openPanel === panel) fitPanel(panel);
        }, 0);
        return panel;
    }

    var ICON_FILL = '#575E75';
    function lineIcon (inner: string) {
        return 'data:image/svg+xml;base64,' + window.btoa(
            '<svg xmlns="http://www.w3.org/2000/svg" width="20px" height="20px" viewBox="0 0 20 20">' +
            '<g fill="none" stroke="' + ICON_FILL + '" stroke-width="1.6" stroke-linecap="round" ' +
            'stroke-linejoin="round">' + inner + '</g></svg>');
    }
    var SHAPE_TOOL_ICON = lineIcon('<path d="M6.6 8 11.6 16.2H1.6z"/><circle cx="14.2" cy="6.2" r="3.9"/>');
    var PEN_TOOL_ICON = lineIcon('<path d="M10 2.4c2.7 3 4 5.4 4 7.6a4 4 0 0 1-8 0c0-2.2 1.3-4.6 4-7.6z"/>' + '<path d="M10 7.6v3.4"/><circle cx="10" cy="13.6" r=".9" fill="' + ICON_FILL + '" stroke="none"/>');
    var FX_TOOL_ICON = lineIcon('<circle cx="7.8" cy="10" r="4.9"/><circle cx="12.2" cy="10" r="4.9"/>');
    var ANCHOR_TOOL_ICON = lineIcon('<circle cx="10" cy="10" r="3.5"/>' +
    '<path d="M10 1.9v2.8M10 15.3v2.8M1.9 10h2.8M15.3 10h2.8"/>' + '<circle cx="10" cy="10" r=".95" fill="' + ICON_FILL + '" stroke="none"/>');
    var TOOL_ICON = {shapes: SHAPE_TOOL_ICON, pen: PEN_TOOL_ICON, fx: FX_TOOL_ICON, anchor: ANCHOR_TOOL_ICON};

    function icon (paths: string) {
        return '<svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.5" ' +
        'stroke-linecap="round" stroke-linejoin="round">' + paths + '</svg>';
    }
    var GLYPH = {
        size: icon('<path d="M2.5 10h15"/><path d="M5.5 7 2.5 10l3 3"/><path d="M14.5 7l3 3-3 3"/>'),
        round: icon('<path d="M3 6.5A3.5 3.5 0 0 1 6.5 3h11v14h-11A3.5 3.5 0 0 1 3 13.5z"/>'),
        nib: icon('<path d="M10 3c2.4 2.7 3.6 5 3.6 7.2A3.6 3.6 0 0 1 6.4 10.2C6.4 8 7.6 5.7 10 3z"/>' + '<path d="M10 16.4v1.6"/>'),
        undo: icon('<path d="M4 8.5h8.5a4 4 0 1 1 0 8H7"/><path d="M4 8.5 7 5.5M4 8.5l3 3"/>'),
        wheel: icon('<rect x="6.5" y="2.5" width="7" height="15" rx="3.5"/><path d="M10 5.2v2.6"/>'),
        plus: icon('<path d="M10 4v12M4 10h12"/>'),
        trash: icon('<path d="M4.5 6h11M8 6V4.5h4V6M6 6l.7 10h6.6L14 6"/>')
    };
    function shapeIcon(shape: Shape) {
        return '<svg viewBox="-8 -8 116 116"><path d="' + (shape.d || shape.path) + '" fill="currentColor" fill-rule="' + (shape.fillRule || 'nonzero') + '"/></svg>';
    }

    function addToolButton(grid: HTMLElement, key: string, iconUrl: string, onClick: () => void) {
        var proto = grid.querySelector('[class*="tool-select-base_mod-tool-select"]');
        if(!proto) return null;
        var btn = proto.cloneNode(false) as PaintElement;
        btn.className = btn.className.replace(/tool-select-base_is-selected\S*/g, '').replace(/\s+/g, ' ').trim() + ' gwt-tool';
        if(!btn.hasAttribute('tabindex')) btn.setAttribute('tabindex', '0');
        if(!btn.getAttribute('role')) btn.setAttribute('role', 'button');
        btn._gwtKey = key;
        btn.dataset.gwtKey = String(key);
        btn.setAttribute('title', tr(key));
        btn.setAttribute('aria-label', tr(key));
        var imgClass = nativeClass('tool-select-base_tool-select-icon') || 'gwt-icon';
        var img = document.createElement('img');
        img.alt = '';
        img.draggable = false;
        img.className = imgClass;
        img.src = iconUrl;
        btn.textContent = '';
        btn.appendChild(img);
        listen(btn, 'click', onClick);
        listen(btn, 'keydown', function (e: KeyboardEvent) {
            if(e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                btn.click();
            }
        });
        grid.appendChild(btn);
        toolButtons.push(btn);
        return btn;
    }
    function markArmed (btn: HTMLElement, on: boolean) {
        if (btn && btn.classList.contains('gwt-tool')) btn.classList.toggle('gwt-armed', !!on);
    }
    function refreshToolLabels () {
        toolButtons.forEach(function (btn: PaintElement & {_gwtKey: string}) {
            if(!btn.isConnected) return;
            var armed = btn === pen.button && pen.active && pen.profile;
            var stamp = btn === shaper.button && shaper.shape;
            var text = armed ? tr(pen.profile.nameKey) : stamp ? tr(shaper.shape.nameKey) : tr(btn._gwtKey);
            btn.setAttribute('title', text);
            btn.setAttribute('aria-label', text);
        });
    }

    var SLIDER_TEXT = {shapeSize: 'size', shapeRound: 'round', penSize: 'size'};
    function sliderValueText (key: string, value: number) {
        return key === 'shapeRound' ? String(value) : value + ' px';
    }
    function sliderRow (glyph: string, min: number, max: number, step: number, key: string, onChange: () => void) {
        var wrap = document.createElement('div');
        wrap.className = 'gwt-row';
        var symbol = document.createElement('span');
        symbol.innerHTML = glyph;
        var input = document.createElement('input');
        input.type = 'range';
        input.min = min;
        input.max = max;
        input.step = step;
        input.value = settings[key];
        input.setAttribute('aria-label', tr(SLIDER_TEXT[key] || key));
        var out = document.createElement('output');
        out.textContent = sliderValueText(key, settings[key]);
        listen(input, 'input', function () {
            settings[key] = Number(input.value);
            out.textContent = sliderValueText(key, settings[key]);
            saveSettings();
            if (onChange) onChange();
        });
        wrap.appendChild(symbol);
        wrap.appendChild(input);
        wrap.appendChild(out);
        return wrap;
    }
    function keyRow (chips: string[]) {
        var row = document.createElement('div');
        row.className = 'gwt-keys';
        chips.forEach(function (chip: string) {
            if(chip.charAt(0) === '<') {
                var glyph = document.createElement('span');
                glyph.innerHTML = chip;
                row.appendChild(glyph);
                return;
            }
            var k = document.createElement('kbd');
            k.textContent = chip;
            row.appendChild(k);
        });
        return row;
    }

    function viewCenter () {
        try {
            return paper().project.view.center.clone();
        } catch (e) {
            return {x: ART_CENTER.x, y: ART_CENTER.y};
        }
    }

    function buildShapePath (shape: Shape, where: {x: number; y: number; width?: number; height?: number}) {
        var p = paper();
        var radius = shape.points ? Math.max(settings.shapeRound, shape.minRound || 0) : 0;
        var d = shape.points ? cornerPath(shape.points, radius) : shape.path;
        var path = new p.Path(d);
        path.closed = true;
        if(shape.fillRule) path.fillRule = shape.fillRule;
        var scale = settings.shapeSize / 100;
        path.scale(scale);
        path.applyMatrix = true;
        if(where && typeof where.width === 'number') path.bounds = where;
        else path.position = where || viewCenter();
        styleShapePath(path);
        return path;
    }

    function insertShape (shape: Shape, where: {x: number; y: number; width?: number; height?: number}) {
        var p = paper();
        var layer = paintingLayer();
        var color = (paintState() || {}).color || {};
        var paints = function (style: PaintColor, key: string) {
            style = style || {};
            return gwtGradient(key) || usable(style.primary) || usable(style.secondary) || !!lastPaint[key];
        };
        if(!p || !layer || (!paints(color.fillColor, 'fill') && !paints(color.strokeColor, 'stroke'))) return false;
        var path = buildShapePath(shape, where);
        try {
            return isBitmapFormat() ? bakeIntoRaster(path) : addVectorShape(path, layer);
        } catch (e) {
            path.remove();
            if (window.console) window.console.warn('GoWarp Toolkit şekil: ' + e.message);
            return false;
        }
    }

    function bakeIntoRaster (path: any) {
        var raster = rasterItem();
        if(!raster || !raster.canvas) {
            path.remove();
            return false;
        }
        var temp = path.rasterize(72);
        path.remove();
        var ctx = raster.canvas.getContext('2d');
        ctx.save();
        ctx.drawImage(temp.canvas, Math.round(temp.bounds.x), Math.round(temp.bounds.y));
        ctx.restore();
        temp.remove();
        return commit();
    }

    function addVectorShape (path: any, layer: any) {
        var p = paper();
        layer.activate();
        layer.addChild(path);
        path.selected = true;
        dispatch({type: 'scratch-paint/select/CHANGE_SELECTED_ITEMS', selectedItems: [path], bitmapMode: false});
        p.view.update();
        if (!commit()) {
            path.remove();
            return false;
        }
        return true;
    }

    var shapePreview = null;
    var previewed = null;

    function clearShapePreview () {
        previewed = null;
        if(!shapePreview) return;
        shapePreview.remove();
        shapePreview = null;
        var art = paintingLayer();
        if(art) art.activate();
    }

    function sweepGhosts () {
        if(shaper.shape || pen.active || shapePreview) return;
        var layer = findLayer('isGuideLayer');
        if(!layer) return;
        for(var i = layer.children.length - 1; i >= 0; i--) {
            if (layer.children[i]._gwtGhost) layer.children[i].remove();
        }
    }
    function previewShape (shape: Shape, box?: {x: number; y: number; width?: number; height?: number}) {
        var p = paper();
        var layer = guideLayer();
        if(!p || !layer) return;
        if(shapePreview) {
            shapePreview.remove();
            shapePreview = null;
        }
        var path = buildShapePath(shape, box);
        path.fillColor = null;
        path.strokeColor = new p.Color((openPanel && openPanel._gwtAccent) || '#575e75');
        path.strokeWidth = 1.2 / p.view.zoom;
        path.dashArray = [5 / p.view.zoom, 4 / p.view.zoom];
        path.locked = true;
        path._gwtGhost = true;
        layer.addChild(path);
        var art = paintingLayer();
        if (art) art.activate();
        p.view.update();
        shapePreview = path;
        previewed = shape;
    }
    function refreshPreview () {
        if(!previewed) return;
        previewShape(previewed);
    }

    var shaper = {
        button: null,
        shape: null as Shape | null,
        start: null,
        cursor: function (on: boolean) {
            var canvas = document.querySelector('canvas[class*="paper-canvas"]');
            if (canvas) canvas.style.cursor = on ? 'crosshair' : '';
        },
        boxOf: function (from: {x: number; y: number}, to: {x: number; y: number}) {
            var p = paper();
            var w = Math.abs(to.x - from.x);
            var h = Math.abs(to.y - from.y);
            if (!p || w < 3 || h < 3) return null;
            return new p.Rectangle(Math.min(from.x, to.x), Math.min(from.y, to.y), w, h);
        },
        arm: function (shape: Shape) {
            this.disarm();
            pen.deactivate();
            this.shape = shape;
            this.start = null;
            markArmed(this.button, true);
            listen(document, 'pointerdown', this.onDown, true);
            listen(document, 'pointermove', this.onMove, true);
            this.cursor(true);
            refreshToolLabels();
        },
        disarm: function () {
            document.removeEventListener('pointerdown', this.onDown, true);
            document.removeEventListener('pointermove', this.onMove, true);
            document.removeEventListener('pointerup', this.onUp, true);
            document.removeEventListener('pointercancel', this.onUp, true);
            markArmed(this.button, false);
            this.cursor(false);
            if (!this.shape) return;
            this.shape = null;
            this.start = null;
            clearShapePreview();
            refreshToolLabels();
        },
        onDown: function (e: PointerEvent) {
            if (!shaper.shape || e.button !== 0) return;
            var canvas = e.target && (e.target as Element).closest ? (e.target as Element).closest('canvas[class*="paper-canvas"]') : null;
            if (!canvas) return;
            var point = pen.toProject(e);
            if (!point) return;
            e.stopPropagation();
            e.preventDefault();
            shaper.start = point;
            listen(document, 'pointermove', shaper.onMove, true);
            listen(document, 'pointerup', shaper.onUp, true);
            listen(document, 'pointercancel', shaper.onUp, true);
        },
        onMove: function (e: PointerEvent) {
            if (!shaper.shape) return;
            var point = pen.toProject(e);
            if (!point) return;
            if (!shaper.start) {
                previewShape(shaper.shape, point);
                return;
            }
            previewShape(shaper.shape, shaper.boxOf(shaper.start, point) || shaper.start);
        },
        onUp: function (e: PointerEvent) {
            document.removeEventListener('pointermove', shaper.onMove, true);
            document.removeEventListener('pointerup', shaper.onUp, true);
            document.removeEventListener('pointercancel', shaper.onUp, true);
            if (!shaper.shape || !shaper.start) return;
            var point = pen.toProject(e) || shaper.start;
            var where = shaper.boxOf(shaper.start, point) || point;
            shaper.start = null;
            var placed = insertShape(shaper.shape, where);
            if (placed) shaper.disarm();
        }
    };

    function demoBlend (mode: string) {
        return '<i class="gwt-demo"><b></b><u style="mix-blend-mode:' + mode + '"></u></i>';
    }
    function demoMask (masked: boolean) {
        return '<i class="gwt-demo gwt-maskdemo' + (masked ? ' gwt-masked' : '') +
            '"><b></b><u></u></i>';
    }

    function selectedPaths () {
        var layer = paintingLayer();
        var out = [];
        if (!layer) return out;
        (function walk (node: any) {
            if (node.locked) return;
            if (node !== layer && node.selected) {
                out.push(node);
                return;
            }
            if (node.children) node.children.forEach(walk);
        })(layer);
        return out;
    }
    function topOf (items: any[]) {
        var layer = paintingLayer();
        var top = null;
        if (layer) {
            (function find (node: any) {
                if (node.locked) return;
                if (items.indexOf(node) >= 0) {
                    top = node;
                    return;
                }
                if (node.children) node.children.forEach(find);
            })(layer);
        }
        return top || items[items.length - 1];
    }
    function artItems () {
        var layer = paintingLayer(), out = [];
        if (!layer) return out;
        (function walk (node: any) {
            if (node.guide || !node.visible) return;
            if (node !== layer && (!node.children || !node.children.length)) {
                out.push(node);
                return;
            }
            if (node.children) node.children.forEach(walk);
        })(layer);
        return out;
    }
    function hasArt () {
        var p = paper();
        if (!p) return false;
        return Array.prototype.some.call(p.project.layers, function (layer: any) {
            var data = layer.data || {};
            if (!data.isPaintingLayer && !data.isRasterLayer) return false;
            return Array.prototype.some.call(layer.children, function (item: any) {
                return !item.guide && item.visible;
            });
        });
    }
    function applyGradientToSelection () {
        var p = paper();
        var items = selectedPaths();
        if (!p || !items.length || !gradStops(gradTarget)) return false;
        var stroke = gradTarget === 'stroke';
        var styleKey = stroke ? 'strokeColor' : 'fillColor';
        var type = gradType(gradTarget) || 'VERTICAL';
        items.forEach(function (item: any) {
            item[styleKey] = gwtPaint(gradTarget, item.bounds, stroke ? item.strokeWidth : 0, type);
        });
        p.view.update();
        return commit();
    }

    var BLENDS = ['normal', 'multiply', 'screen', 'overlay', 'darken', 'lighten',
        'color-dodge', 'color-burn', 'hard-light', 'soft-light', 'difference', 'exclusion',
        'hue', 'saturation', 'color', 'luminosity'];

    function eachSelected (fn: (item: any) => void, live?: boolean) {
        var p = paper();
        var items = selectedPaths();
        if (!p || !items.length) return false;
        items.forEach(fn);
        p.view.update();
        return live ? true : commit();
    }
    function applyBlend (mode: string) {
        return eachSelected(function (item: any) {
            item.blendMode = mode;
        });
    }
    function blendOfSelection () {
        var items = selectedPaths();
        return items.length ? String(items[0].blendMode || 'normal') : '';
    }
    function stepBlend (dir: number) {
        var at = BLENDS.indexOf(blendOfSelection());
        if (at < 0) at = 0;
        var next = BLENDS[(at + dir + BLENDS.length) % BLENDS.length];
        if (applyBlend(next)) markFx();
        return next;
    }
    function canMask () {
        return selectedPaths().length > 0 && artItems().length > 1;
    }
    function setMask (on: boolean) {
        var p = paper();
        var items = selectedPaths();
        if (!p || !items.length || (on && !canMask())) return false;
        var top = topOf(items);
        artItems().forEach(function (item: any) {
            if (item.clipMask && items.indexOf(item) < 0) item.clipMask = false;
        });
        items.forEach(function (item: any) {
            item.clipMask = on && item === top;
        });
        p.view.update();
        return commit();
    }
    function isMasked () {
        return selectedPaths().some(function (item: any) {
            return !!item.clipMask;
        });
    }
    function setMiter (limit: number, live?: any) {
        return eachSelected(function (item: any) {
            item.miterLimit = limit;
        }, live);
    }
    function miterOfSelection () {
        var items = selectedPaths();
        return items.length ? Number(items[0].miterLimit) || 10 : 10;
    }

    function artLayers (p: any, on: boolean) {
        var names = [];
        Array.prototype.forEach.call(p.project.layers, function (layer: any) {
            var data = layer.data || {};
            var art = !!(data.isPaintingLayer || data.isRasterLayer);
            if (on) {
                layer.__gwtWas = layer.visible;
                layer.visible = art;
            } else layer.visible = layer.__gwtWas;
            if (art) names.push(layer);
        });
        return names;
    }
    function artBox (p: any) {
        var art = artLayers(p, true);
        var box = null;
        art.forEach(function (layer: any) {
            Array.prototype.forEach.call(layer.children, function (item: any) {
                if (item.guide || !item.visible) return;
                var b = item.bounds;
                if (b && b.width && b.height) box = box ? box.unite(b) : b.clone();
            });
        });
        return box;
    }
    function artSvg () {
        var p = paper();
        if (!p) return null;
        var svg = null;
        try {
            var box = artBox(p);
            if (box) svg = p.project.exportSVG({asString: true, bounds: [box.x, box.y, box.width, box.height]});
        } finally {
            artLayers(p, false);
        }
        return svg;
    }
    function exportPng (scale: number) {
        var svg = artSvg();
        if (!svg) return false;
        var url = window.URL.createObjectURL(new window.Blob([svg], {type: 'image/svg+xml;charset=utf-8'}));
        var img = new window.Image();
        img.onload = function () {
            var side = Math.max(img.width, img.height) * scale;
            var fit = side > 16000 ? 16000 / side : 1;
            var canvas = document.createElement('canvas');
            canvas.width = Math.max(1, Math.round(img.width * scale * fit));
            canvas.height = Math.max(1, Math.round(img.height * scale * fit));
            canvas.getContext('2d').drawImage(img, 0, 0, canvas.width, canvas.height);
            window.URL.revokeObjectURL(url);
            var link = document.createElement('a');
            link.href = canvas.toDataURL('image/png');
            link.download = 'gwt' + (scale > 1 ? '-' + scale + 'x' : '') + '.png';
            document.body.appendChild(link);
            link.click();
            link.remove();
        };
        img.onerror = function () {
            window.URL.revokeObjectURL(url);
            if (window.console) window.console.warn('GoWarp Toolkit PNG: taranamadı');
        };
        img.src = url;
        return true;
    }

    function gradientCss (p: any, stops: {p: number; c: string}[], type: string) {
        var list = stops.map(function (s: { p: number; c: string; }) {
            var c = new p.Color(s.c);
            return c.toCSS() + ' ' + Math.round(s.p * 1000) / 10 + '%';
        }).join(',');
        if (type === 'RADIAL') return 'radial-gradient(circle at 50% 50%,' + list + ')';
        return 'linear-gradient(' + (type === 'VERTICAL' ? '180deg' : '90deg') + ',' + list + ')';
    }

    function labelEl (text: string) {
        var l = document.createElement('div');
        l.className = 'gwt-label';
        l.textContent = text;
        return l;
    }

    var gradStrip = null;
    var gradTarget = 'fill';
    var gradPick = 0;

    function pickerBody () {
        var body = document.querySelector('[class*="Popover-body"]');
        if (!body || !body.querySelector('[class*="color-picker_gradient-picker-row"]')) return null;
        return body;
    }
    function gradBarCss () {
        var p = paper();
        var stops = gradStops(gradTarget);
        if (!p || !stops) return '';
        return gradientCss(p, stops, gradType(gradTarget) || 'VERTICAL');
    }
    function hex6 (css: string) {
        var p = paper();
        try {
            var c = new p.Color(css);
            return '#' + [c.red, c.green, c.blue].map(function (v: number) {
                var n = Math.round(clamp(v, 0, 1) * 255);
                return ('0' + n.toString(16)).slice(-2);
            }).join('');
        } catch (e) {
            return '#8899aa';
        }
    }

    function orderOffsets (dragged: number) {
        var g = settings.grad;
        var rows = g.offsets.map(function (p: number, i: number) {
            return {p: p, i: i, extra: i > 0 && i < g.count - 1 ? i - 1 : -1};
        });
        rows.sort(function (a: { p: number; i: number; extra: number; }, b: { p: number; i: number; extra: number; }) { return a.p - b.p; });
        var extras = [];
        rows.forEach(function (row: { p: number; i: number; extra: number; }) {
            if (row.extra >= 0) extras.push(g.extras[row.extra]);
        });
        g.offsets = rows.map(function (row: { p: number; i: number; extra: number; }) { return row.p; });
        g.extras = extras;
        for(let i=0; i < rows.length; i++) {
            if(rows[i].i === dragged) {
                gradPick = i;
                break;
            }
        }
    }

    function setStopCount (n: number) {
        var g = settings.grad;
        if (n === g.count || n < 2 || n > 4) return;
        var middles = [];
        for (var i = 1; i < g.count - 1; i++) middles.push({p: g.offsets[i], c: g.extras[i - 1]});
        if (n > g.count) {
            while (middles.length < n - 2) {
                var all = [g.offsets[0]].concat(middles.map(function (m: {p: number; c: string}) { return m.p; }), [g.offsets[g.count - 1]]);
                var at = 0, gap = -1;
                for (i=0; i < all.length - 1; i++) {
                    if(all[i + 1] - all[i] > gap) { gap = all[i + 1] - all[i]; at = i; }
                }
                middles.push({p: all[at] + gap / 2, c: '#8899aa'});
            }
            middles.sort(function (a: {p: number; c: string}, b: {p: number; c: string}) { return a.p - b.p; });
        }
        middles = middles.slice(0, n - 2);
        var first = g.offsets[0];
        var last = g.offsets[g.count - 1];
        g.count = n;
        g.offsets = [first].concat(middles.map(function (m: {p: number; c: string}) { return m.p; }), [last]);
        g.extras = middles.map(function (m: {p: number; c: string}) { return m.c; });
        gradPick = clamp(gradPick, 0, n - 1);
        saveSettings();
        buildThumbs();
        applyGradientToSelection();
        paintGradStrip();
    }

    function thumbDown (e: PointerEvent) {
        e.preventDefault();
        e.stopPropagation();
        var g = settings.grad;
        var bar = gradStrip.bar;
        var thumb = e.currentTarget as HTMLElement;
        var i = Number(thumb.dataset.i);
        var rect = bar.getBoundingClientRect();
        gradPick = i;
        paintGradStrip();
        var move = function(ev: PointerEvent) {
            g.offsets[i] = clamp((ev.clientX - rect.left) / rect.width, 0, 1);
            thumb.style.left = (g.offsets[i] * 100) + '%';
            bar.style.background = gradBarCss();
        };
        var up = function() {
            document.removeEventListener('pointermove', move);
            document.removeEventListener('pointerup', up);
            orderOffsets(i);
            saveSettings();
            applyGradientToSelection();
            buildThumbs();
            paintGradStrip();
        };
        listen(document, 'pointermove', move);
        listen(document, 'pointerup', up);
    }

    function buildThumbs () {
        var g = settings.grad;
        var bar = gradStrip.bar;
        bar.textContent = '';
        for(let i=0; i < g.count; i++) {
            var b = document.createElement('b');
            b.dataset.i = String(i);
            b.setAttribute('role', 'slider');
            b.setAttribute('aria-label', tr('stops') + ' ' + (i + 1));
            listen(b, 'pointerdown', thumbDown);
            bar.appendChild(b);
        }
    }

    function paintGradStrip () {
        if(!gradStrip || !gradStrip.el.isConnected) return;
        var g = settings.grad;
        gradStrip.bar.style.background = gradBarCss();
        Array.prototype.forEach.call(gradStrip.bar.childNodes, function (b: HTMLElement, i: number) {
            b.style.left = (g.offsets[i] * 100) + '%';
            b.classList.toggle('gwt-pick', i === gradPick);
        });
        Array.prototype.forEach.call(gradStrip.counts.children, function (b: HTMLElement, i: number) {
            b.setAttribute('aria-pressed', String(g.count === i + 2));
        });
        var middle = gradPick > 0 && gradPick < g.count - 1;
        gradStrip.well.hidden = !middle;
        if (middle) gradStrip.well.value = hex6(g.extras[gradPick - 1]);
        gradStrip.apply.disabled = !selectedPaths().length;
    }

    function buildGradStrip (body: HTMLElement) {
        var wrap = document.createElement('div');
        wrap.className = 'gwt-gwrap';

        var bar = document.createElement('div');
        bar.className = 'gwt-bar gwt-gbar';
        bar.setAttribute('aria-label', tr('gradient'));
        wrap.appendChild(bar);

        var foot = document.createElement('div');
        foot.className = 'gwt-gfoot';
        var counts = document.createElement('div');
        counts.className = 'gwt-seg gwt-gcounts';
        [2, 3, 4].forEach(function (n: number) {
            var b = document.createElement('button');
            b.type = 'button';
            b.textContent = String(n);
            b.setAttribute('aria-label', tr('stops') + ' ' + n);
            b.title = tr('stops') + ' · ' + n;
            listen(b, 'click', function () { setStopCount(n); });
            counts.appendChild(b);
        });
        var well = document.createElement('input');
        well.type = 'color';
        well.className = 'gwt-gwell';
        well.setAttribute('aria-label', tr('stops'));
        listen(well, 'input', function () {
            settings.grad.extras[gradPick - 1] = well.value;
            saveSettings();
            gradStrip.bar.style.background = gradBarCss();
        });
        listen(well, 'change', function () { applyGradientToSelection(); });
        var apply = document.createElement('button');
        apply.type = 'button';
        apply.className = 'gwt-gapply';
        apply.textContent = tr('apply');
        listen(apply, 'click', function () {
            if(applyGradientToSelection()) paintGradStrip();
        });
        foot.appendChild(counts);
        foot.appendChild(well);
        foot.appendChild(apply);
        wrap.appendChild(foot);

        var row = body.querySelector('[class*="color-picker_gradient-picker-row"]');
        row.parentNode.insertBefore(wrap, row.nextSibling);
        gradStrip = {
            el: wrap,
            body: body,
            bar: bar,
            counts: counts,
            well: well,
            apply: apply
        };
        buildThumbs();
    }

    function syncGradStrip () {
        var body = pickerBody();
        if(!body) {
            gradStrip = null;
            return;
        }
        if(gradStrip && gradStrip.el.isConnected && gradStrip.body === body) {
            paintGradStrip();
            return;
        }
        gradStrip = null;
        buildGradStrip(body);
        applyTheme(gradStrip.el, readTheme());
        paintGradStrip();
    }

    function dropGradStrip () {
        if(gradStrip && gradStrip.el.isConnected) gradStrip.el.remove();
        gradStrip = null;
    }

    function watchColorTarget () {
        if (document.__gwtGrad) return;
        document.__gwtGrad = true;
        listen(document, 'pointerdown', function(e: PointerEvent) {
            var t = e.target as Element;
            if(!t || !t.closest) return;
            if(t.closest('[class*="color-button_outline-swatch"]')) gradTarget = 'stroke';
            else if(t.closest('[class*="color-button_color-button-swatch"]')) gradTarget = 'fill';
            else return;
            gradPick = 0;
            later(syncGradStrip, 60);
        }, true);
    }

    function showFxPanel(anchor: PaintElement) {
        var panel = toggleFor(anchor, 'fx', 'gwt-fxbox');
        if(!panel) return;

        panel.appendChild(labelEl(tr('mask')));
        var masks = document.createElement('div');
        masks.className = 'gwt-grid gwt-maskrow';
        [['on', tr('mask')], ['off', tr('maskOff')]].forEach(function (row: string[]) {
            var b = createIconButton(document, 'gwt-cell', row[1], demoMask(row[0] === 'on') + '<span>' + row[1] + '</span>');
            b.dataset.mask = String(row[0]);
            listen(b, 'click', function () {
                setMask(row[0] === 'on');
                markFx();
            });
            masks.appendChild(b);
        });
        panel.appendChild(masks);

        panel.appendChild(labelEl(tr('blend')));
        var grid = document.createElement('div');
        grid.className = 'gwt-grid gwt-blends';
        BLENDS.forEach(function (mode: string) {
            var b = createIconButton(document, 'gwt-cell', mode, demoBlend(mode) + '<span>' + mode.replace(/-/g, ' ') + '</span>');
            b.dataset.blend = String(mode);
            listen(b, 'click', function () {
                applyBlend(mode);
                markFx();
            });
            grid.appendChild(b);
        });
        panel.appendChild(grid);

        var row = document.createElement('div');
        row.className = 'gwt-row';
        row.innerHTML = '<span class="gwt-tag">' + tr('joint') + '</span>' +
        '<i class="gwt-demo gwt-corner"><svg viewBox="0 0 30 24" aria-hidden="true">' +
        '<path d="M3 21 15 3 27 21"/></svg></i>';
        var corner = row.querySelector('path');
        var slider = document.createElement('input');
        slider.type = 'range';
        slider.min = 1;
        slider.max = 10;
        slider.step = 0.1;
        slider.value = miterOfSelection();
        slider.setAttribute('aria-label', tr('joint'));
        var out = document.createElement('output');
        out.textContent = Number(slider.value).toFixed(1);
        listen(slider, 'input', function () {
            out.textContent = Number(slider.value).toFixed(1);
            corner.setAttribute('stroke-miterlimit', slider.value);
            setMiter(Number(slider.value), true);
        });
        listen(slider, 'change', function () {
            setMiter(Number(slider.value));
        });
        corner.setAttribute('stroke-miterlimit', slider.value);
        row.appendChild(slider);
        row.appendChild(out);
        panel.appendChild(row);

        var div = document.createElement('div');
        div.className = 'gwt-div';
        panel.appendChild(div);

        panel.appendChild(labelEl(tr('png')));
        var pngs = document.createElement('div');
        pngs.className = 'gwt-seg';
        pngs.style.marginTop = '.3rem';
        [1, 2, 4].forEach(function (scale: number) {
            var b = document.createElement('button');
            b.type = 'button';
            b.textContent = scale === 1 ? '1×' : scale + '×';
            b.dataset.scale = String(scale);
            b.title = tr('png') + ' ' + scale + '×';
            listen(b, 'click', function () {
                settings.pngScale = scale;
                saveSettings();
                exportPng(scale);
                markFx();
            });
            pngs.appendChild(b);
        });
        panel.appendChild(pngs);
        panel.appendChild(keyRow(['Alt+M', 'Alt+B', 'Alt+J', 'Alt+X']));
        markFx();
    }

    function markFx() {
        if(!openPanel || openPanel._gwtKind !== 'fx') return;
        var some = selectedPaths().length > 0;
        var blend = blendOfSelection();
        var masked = isMasked();
        var maskable = canMask();
        var vector = !isBitmapFormat();
        Array.prototype.forEach.call(openPanel.querySelectorAll('[data-blend]'), function (b: HTMLElement) {
            var on = some && b.dataset.blend === blend;
            b.classList.toggle('gwt-on', on);
            b.setAttribute('aria-pressed', String(on));
        });
        Array.prototype.forEach.call(openPanel.querySelectorAll('[data-mask]'), function (b: HTMLButtonElement) {
            var on = some && (b.dataset.mask === 'on') === masked;
            b.classList.toggle('gwt-on', on);
            b.setAttribute('aria-pressed', String(on));
        });
        var art = hasArt();
        Array.prototype.forEach.call(openPanel.querySelectorAll('[data-scale]'), function (b: HTMLButtonElement) {
            b.setAttribute('aria-pressed', String(Number(b.dataset.scale) === settings.pngScale));
            b.disabled = !art;
            b.title = art ? tr('png') + ' ' + b.dataset.scale + '×' : tr('empty');
        });
        var slider = openPanel.querySelector('.gwt-row input[type=range]');
        if(slider && document.activeElement !== slider) {
            slider.value = miterOfSelection();
            slider.nextSibling.textContent = Number(slider.value).toFixed(1);
            var corner = openPanel.querySelector('.gwt-corner path');
            if(corner) corner.setAttribute('stroke-miterlimit', slider.value);
        }
        Array.prototype.forEach.call(openPanel.querySelectorAll('[data-blend], [data-mask]'), function (b: HTMLButtonElement) {
            var ready = some && (b.dataset.mask !== 'on' || maskable);
            var why = !vector ? tr('vectorOnly') : some ? tr('maskAlone') : tr('nothing');
            b.disabled = !ready;
            b.title = ready ? b.textContent : why;
        });
    }

    function refreshPanels() {
        if(openPanel && document.contains(openPanel._gwtAnchor)) fitPanel(openPanel);
        if(openPanel && openPanel._gwtKind === 'fx') later(markFx, 0);
        if(layers.railEl) later(markObjects, 0);
    }

    function showShapePanel(anchor: PaintElement) {
        var panel = toggleFor(anchor, 'shape', 'gwt-wide');
        if(!panel) return;

        panel.appendChild(sliderRow(GLYPH.size, 40, 400, 4, 'shapeSize', refreshPreview));
        panel.appendChild(sliderRow(GLYPH.round, 0, 40, 1, 'shapeRound', refreshPreview));

        var grid = document.createElement('div');
        grid.className = 'gwt-grid';
        SHAPES.forEach(function(shape: Shape) {
            var b = createIconButton(document, 'gwt-cell', tr(shape.nameKey), shapeIcon(shape));
            b.dataset.shape = String(shape.id);
            listen(b, 'click', function () {
                shaper.arm(shape);
                closePanel();
                previewShape(shape);
            });
            listen(b, 'mouseenter', function () {
                previewShape(shape);
            });
            listen(b, 'focus', function () {
                previewShape(shape);
            });
            listen(b, 'mouseleave', clearShapePreview);
            listen(b, 'blur', clearShapePreview);
            grid.appendChild(b);
        });
        panel.appendChild(grid);
        panel.appendChild(keyRow([GLYPH.undo, 'Ctrl', 'Z']));
    }

    function refreshNibUi () {
        if(!openPanel) return;
        var cells = openPanel.querySelectorAll('.gwt-nib');
        if(!cells.length) return;
        var size = currentNibSize();
        var eraser = isEraserMode();
        for(let i=0; i < cells.length; i++) {
            var on = Number(cells[i].dataset.size) === size;
            cells[i].classList.toggle('gwt-on', on);
            var mark = cells[i].firstChild;
            mark.className = eraser ? 'gwt-ring' : 'gwt-dot';
            var px = clamp(Number(cells[i].dataset.size) * (eraser ? 0.34 : 0.28), 4, 22);
            mark.style.width = px + 'px';
            mark.style.height = px + 'px';
        }
        var slider = openPanel.querySelector('.gwt-nib-slider');
        if(slider && document.activeElement !== slider) {
            slider.value = size;
            slider.nextSibling.textContent = size;
        }
    }

    function showNibPanel (anchor: PaintElement) {
        var panel = toggleFor(anchor, 'nib');
        if(!panel) return;

        var grid = document.createElement('div');
        grid.className = 'gwt-grid gwt-nibs';
        NIBS.forEach(function(nib: typeof NIBS[number]) {
            var b = createIconButton(document, 'gwt-cell', tr(nib.nameKey) + ' · ' + nib.size, '');
            b.className = 'gwt-cell gwt-nib';
            b.dataset.size = String(nib.size);
            var dot = document.createElement('span');
            dot.className = isEraserMode() ? 'gwt-ring' : 'gwt-dot';
            var num = document.createElement('span');
            num.className = 'gwt-nib-num';
            num.textContent = String(nib.size);
            b.appendChild(dot);
            b.appendChild(num);
            listen(b, 'click', function () {
                applyNib(nib.size);
            });
            grid.appendChild(b);
        });
        panel.appendChild(grid);

        var row = document.createElement('div');
        row.className = 'gwt-row';
        row.innerHTML = GLYPH.nib;
        var input = document.createElement('input');
        input.type = 'range';
        input.min = 1;
        input.max = 100;
        input.className = 'gwt-nib-slider';
        input.value = currentNibSize();
        input.setAttribute('aria-label', tr('size'));
        var out = document.createElement('output');
        out.textContent = currentNibSize();
        listen(input, 'input', function() {
            out.textContent = input.value;
            applyNib(Number(input.value));
        });
        row.appendChild(input);
        row.appendChild(out);
        panel.appendChild(row);
        panel.appendChild(keyRow(['[', ']', GLYPH.wheel, 'Alt']));
        refreshNibUi();
    }

    var pen = {
        armedInMode: "",
        active: false,
        profile: null as typeof PENS[number] | null,
        button: null,
        points: [],
        preview: null,

        activate: function (profile: typeof PENS[number]) {
            this.deactivate();
            shaper.disarm();
            if(!paper()) return;
            this.active = true;
            this.profile = profile;
            this.armedInMode = (paintState() || {}).mode;
            listen(document, 'pointerdown', this.onDown, true);
            markArmed(this.button, true);
            refreshToolLabels();
        },
        deactivate: function () {
            document.removeEventListener('pointerdown', this.onDown, true);
            markArmed(this.button, false);
            if(!this.active) return;
            this.active = false;
            this.endStroke();
            this.clearPreview();
            this.points = [];
            refreshToolLabels();
        },
        clearPreview: function () {
            if(this.preview) {
                this.preview.remove();
                this.preview = null;
            }
        },
        widthAt: function (t: number, pressure: number, angle: number) {
            var prof = this.profile;
            var base = settings.penSize;
            var pr = settings.penPressure ? clamp(pressure, 0.05, 1) : 0.6;
            var width = base * (prof.min + (1 - prof.min) * Math.pow(pr, prof.exponent));
            if(prof.chisel) {
                var nib = -Math.PI / 5;
                width *= 0.3 + 0.7 * Math.abs(Math.sin(angle - nib));
            }
            var taper = prof.taper * Math.sin(Math.PI * clamp(t, 0, 1));
            return Math.max(0.6, width * (1 - taper * 0.6));
        },
        toProject: function (e: MouseEvent) {
            var p = paper();
            var canvas = document.querySelector('canvas[class*="paper-canvas"]');
            if(!p || !canvas) return null;
            var rect = canvas.getBoundingClientRect();
            return p.project.view.viewToProject(new p.Point(e.clientX - rect.left, e.clientY - rect.top));
        },
        onDown: function (e: PointerEvent) {
            if(!pen.active) return;
            var canvas = e.target && (e.target as Element).closest ? (e.target as Element).closest('canvas[class*="paper-canvas"]') : null;
            if(!canvas) return;
            closePanel();
            e.stopPropagation();
            e.preventDefault();
            var point = pen.toProject(e);
            if(!point) return;
            pen.points = [{x: point.x, y: point.y, p: e.pressure || 0.5}];
            listen(document, 'pointermove', pen.onMove, true);
            listen(document, 'pointerup', pen.onUp, true);
            listen(document, 'pointercancel', pen.onUp, true);
        },
        onMove: function (e: PointerEvent) {
            if(!pen.active || !pen.points.length) return;
            e.stopPropagation();
            var raw = pen.toProject(e);
            if(!raw) return;
            var last = pen.points[pen.points.length - 1];
            var k = 1 - pen.profile.smoothing;
            var x = last.x + (raw.x - last.x) * k;
            var y = last.y + (raw.y - last.y) * k;
            if(Math.hypot(x - last.x, y - last.y) < 1.2) return;
            pen.points.push({x: x, y: y, p: e.pressure || last.p});
            pen.drawPreview();
        },
        onUp: function (e: PointerEvent) {
            if (e) e.stopPropagation();
            document.removeEventListener('pointermove', pen.onMove, true);
            document.removeEventListener('pointerup', pen.onUp, true);
            document.removeEventListener('pointercancel', pen.onUp, true);
            pen.endStroke();
        },
        outline: function () {
            var pts = this.points;
            if(pts.length < 2) return [];
            var left = [];
            var right = [];
            for(let i=0; i < pts.length; i++) {
                var prev = pts[Math.max(0, i - 1)];
                var next = pts[Math.min(pts.length - 1, i + 1)];
                var dx = next.x - prev.x;
                var dy = next.y - prev.y;
                var angle = Math.atan2(dy, dx);
                var len = Math.hypot(dx, dy) || 1;
                var nx = -dy / len;
                var ny = dx / len;
                var half = this.widthAt(i / (pts.length - 1), pts[i].p, angle) / 2;
                left.push([pts[i].x + nx * half, pts[i].y + ny * half]);
                right.push([pts[i].x - nx * half, pts[i].y - ny * half]);
            }
            return left.concat(right.reverse());
        },
        drawPreview: function () {
            var p = paper();
            var pts = this.outline();
            if(!p || pts.length < 4) return;
            this.clearPreview();
            var layer = guideLayer();
            if(!layer) return;
            var accent = (openPanel && openPanel._gwtAccent) || '#575e75';
            var path = new p.Path(pts);
            path.closed = true;
            path.strokeColor = new p.Color(accent);
            path.strokeWidth = 1 / p.view.zoom;
            path.fillColor = new p.Color(accent).clone({alpha: 0.15});
            path.guide = true;
            path.locked = true;
            path._gwtGhost = true;
            layer.addChild(path);
            this.preview = path;
        },
        endStroke: function () {
            var p = paper();
            var pts = this.outline();
            this.clearPreview();
            if(!p || pts.length < 4) {
                this.points = [];
                return;
            }
            var path = new p.Path(pts);
            path.closed = true;
            path.strokeCap = 'round';
            path.strokeJoin = 'round';
            applyStyle(path, false);
            if(!path.fillColor) {
                var cFall = currentColors();
                if(cFall.stroke) path.fillColor = cFall.stroke;
                else {
                    var sSt = store();
                    var pcol = (sSt && sSt.getState().scratchPaint && sSt.getState().scratchPaint.color) || {};
                    path.fillColor = colorOf(pcol.fillColor) || colorOf(pcol.strokeColor) || (p ? new p.Color('#855cd6') : null);
                }
            }
            if(!path.fillColor) {
                path.remove();
                this.points = [];
                return;
            }
            if(isBitmapFormat()) {
                bakeIntoRaster(path);
                this.points = [];
                return;
            }
            var layer = paintingLayer();
            if(!layer) {
                path.remove();
                this.points = [];
                return;
            }
            layer.activate();
            layer.addChild(path);
            const prepared = preparePenStroke(path);
            path.remove();
            path = prepared.path;
            const previous = prepared.previous;
            const previousIndex = previous?.index;
            previous?.remove();
            layer.addChild(path);
            path.selected = true;
            dispatch({
                type: 'scratch-paint/select/CHANGE_SELECTED_ITEMS',
                selectedItems: [path],
                bitmapMode: false
            });
            p.view.update();
            let saved = false;
            try {
                saved = commit();
            } finally {
                if (!saved) {
                    path.remove();
                    if (previous) layer.insertChild(previousIndex, previous);
                    dispatch({
                        type: 'scratch-paint/select/CHANGE_SELECTED_ITEMS',
                        selectedItems: p.project.selectedItems,
                        bitmapMode: false
                    });
                    p.view.update();
                }
                this.points = [];
            }
        }
    };

    function showPenPanel (anchor: PaintElement) {
        var panel = toggleFor(anchor, 'pen');
        if(!panel) return;

        var grid = document.createElement('div');
        grid.className = 'gwt-grid gwt-pens';
        PENS.forEach(function(profile: typeof PENS[number]) {
            var b = createIconButton(document, 'gwt-cell', tr(profile.nameKey), nibSwatch(profile));
            b.className = 'gwt-cell gwt-pen';
            b.dataset.pen = String(profile.id);
            listen(b, 'click', function () {
                settings.pen = profile.id;
                saveSettings();
                pen.activate(profile);
                markPens();
            });
            grid.appendChild(b);
        });
        panel.appendChild(grid);

        var off = document.createElement('button');
        off.type = 'button';
        off.className = 'gwt-cell gwt-off';
        off.textContent = tr('off');
        listen(off, 'click', function() {
            pen.deactivate();
            markPens();
        });
        panel.appendChild(off);

        panel.appendChild(sliderRow(GLYPH.nib, 2, 120, 1, 'penSize', function () {
            pen.drawPreview();
        }));

        var press = document.createElement('label');
        press.className = 'gwt-row';
        var text = document.createElement('span');
        text.className = 'gwt-name';
        text.textContent = tr('pressure');
        var box = document.createElement('input');
        box.type = 'checkbox';
        box.checked = settings.penPressure;
        listen(box, 'change', function () {
            settings.penPressure = box.checked;
            saveSettings();
        });
        press.appendChild(text);
        press.appendChild(box);
        panel.appendChild(press);

        panel.appendChild(keyRow([GLYPH.undo, 'Ctrl', 'Z']));

        function markPens() {
            var buttons = grid.querySelectorAll<HTMLElement>('.gwt-pen');
            for(let i=0; i < buttons.length; i++) {
                buttons[i].classList.toggle('gwt-on', pen.active && buttons[i].dataset.pen === settings.pen);
            }
        }
        markPens();
    }

    var lastMode = null;
    var lastFormat = null;
    var lastColorSig = null;
    function colorSig (color: any) {
        if(!color) return '-';
        if(color.gradient) {
            var stops = color.gradient.stops || [];
            return 'g|' + color.gradient.type + '|' + stops.map(function (s: any) {
                return Math.round(s.offset * 1000) + '@' + (s.color && s.color.toCSS ? s.color.toCSS(true) : s.color);
            }).join(',');
        }
        if(color.toCSS) return color.toCSS(true);
        var f = color.fillColor || {};
        var s = color.strokeColor || {};
        return [f.gradientType, f.primary, f.secondary, s.gradientType, s.primary, s.secondary].join('|');
    }
    function isBrushFamily (mode: string) {
        return /^(BRUSH|ERASER|BIT_BRUSH|BIT_ERASER)$/.test(mode || '');
    }
    function onStateChange () {
        var p = paintState();
        if(!p) return;
        var sig = colorSig(p.color || {});
        if(sig !== lastColorSig) {
            lastColorSig = sig;
            if(gradStrip) paintGradStrip();
        }
        if(p.mode !== lastMode) {
            var switched = lastMode !== null;
            lastMode = p.mode;
            if(switched) {
                if(pen.active && p.mode !== pen.armedInMode) pen.deactivate();
                if(shaper.shape) shaper.disarm();
                if(openPanel && openPanel._gwtKind === 'nib' && !isBrushFamily(p.mode)) closePanel();
                else if(openPanel && openPanel._gwtKind === 'nib') refreshNibUi();
                if(layers.railEl) markObjects();
            }
        }
        if(p.format !== lastFormat) {
            var changed = lastFormat !== null;
            lastFormat = p.format;
            if(changed && openPanel && openPanel._gwtKind === 'nib') closePanel();
            if(changed) syncRail();
        }
    }

    function findVM () {
        return Scratch.vm;
    }

    function anchorTarget () {
        var vm = findVM();
        var t = vm && vm.editingTarget;
        return (t && !t.isStage && typeof t.getCostumes === 'function') ? t : null;
    }

    function toolButton (key: string) {
        for(let i=0; i < toolButtons.length; i++) {
            if(toolButtons[i]._gwtKey === key && toolButtons[i].isConnected) return toolButtons[i];
        }
        return null;
    }

    function syncToolButtons () {
        var grid = document.querySelector('[class*="paint-editor_mode-selector"]');
        toolButtons = toolButtons.filter(function (b: HTMLElement) { return b.isConnected; });
        ['shapes', 'pen', 'fx', 'anchor'].forEach(function (key: string) {
            var btn = toolButton(key);
            if(btn || !grid) return;
            addToolButton(grid, key, TOOL_ICON[key], function () {
                var self = toolButton(key);
                if(!self) return;
                if(key === 'shapes') showShapePanel(self);
                else if(key === 'pen') showPenPanel(self);
                else if(key === 'anchor') showAnchorPanel(self);
                else showFxPanel(self);
            });
        });
        shaper.button = toolButton('shapes');
        pen.button = toolButton('pen');
        refreshToolLabels();
    }

    function mount () {
        var grid = document.querySelector('[class*="paint-editor_mode-selector"]');
        if(!grid || grid.dataset.gwtMounted || !paper() || !store()) return;
        grid.dataset.gwtMounted = String('1');

        syncToolButtons();
        watchColorTarget();
        syncRail();
        if(pen.active) markArmed(pen.button, true);

        var s = store();
        if(!s.__gwtWatch) {
            s.__gwtWatch = true;
            lastMode = (paintState() || {}).mode;
            lastFormat = (paintState() || {}).format;
            unsubscribe = s.subscribe(onStateChange);
        }

        var buttons = grid.querySelectorAll('[class*="tool-select-base_mod-tool-select"]');
        Array.prototype.forEach.call(buttons, function (btn: HTMLElement) {
            if(btn.classList.contains('gwt-tool')) return;
            listen(btn, 'click', function () {
                later(function () {
                    pen.deactivate();
                    var p = paintState();
                    if (!p || !isBrushFamily(p.mode)) return;
                    showNibPanel(btn);
                }, 60);
            });
        });

        var host = document.querySelector('[class*="paint-editor_canvas-container"]');
        if(host && !host.dataset.gwtWheel) {
            host.dataset.gwtWheel = String('1');
            listen(host, 'wheel', function (e: WheelEvent) {
                if (!e.altKey) return;
                e.preventDefault();
                stepNib(e.deltaY < 0 ? 1 : -1);
            }, {passive: false});
        }
    }

    function paintEditorOpen () {
        return !!document.querySelector('[class*="paint-editor_mode-selector"]');
    }

    function onKeyDown (e: KeyboardEvent) {
        if(isTextInput(e.target)) return;
        if(!paintEditorOpen()) return;
        if(e.key === 'Escape') {
            if(openPanel) closePanel();
            else if(pen.active) pen.deactivate();
            else if(shaper.shape) shaper.disarm();
            return;
        }
        if(e.key === '[') stepNib(-1);
        if(e.key === ']') stepNib(1);
        if(e.altKey && !e.ctrlKey && !e.metaKey) {
            if(e.code === 'KeyM') {
                e.preventDefault();
                if (setMask(!isMasked())) markFx();
            }else if(e.code === 'KeyB') {
                e.preventDefault();
                stepBlend(e.shiftKey ? -1 : 1);
                markFx();
            }else if(e.code === 'KeyJ') {
                e.preventDefault();
                if (setMiter(1.2)) markFx();
            }else if(e.code === 'KeyD') {
                e.preventDefault();
                var anchorBtn = toolButton('anchor');
                if (openPanel && openPanel._gwtKind === 'anchor') {
                    applyAnchor(anchorState.mode);
                    markAnchor();
                } else if (anchorBtn) {
                    showAnchorPanel(anchorBtn);
                }
            }else if(e.code === 'KeyX') {
                e.preventDefault();
                exportPng(settings.pngScale || 1);
            }
        }
    }

    function tick () {
        try {
            ensureStyle();
            sfSync();
            if (!paintEditorOpen()) {
                closePanel();
                pen.deactivate();
                shaper.disarm();
                destroyRail();
                return;
            }
            mount();
            sweepGhosts();
            syncGradStrip();
            syncRail();
            syncToolButtons();
            const language = tr().lang;
            if(lastLanguage !== language) {
                lastLanguage = language;
                refreshToolLabels();
                closePanel();
            }else if(openPanel && !openAnchor.isConnected) {
                closePanel();
            }
            if (openPanel && openPanel._gwtKind === 'fx') markFx();
            else if (openPanel && openPanel._gwtKind === 'anchor') markAnchor();
        } catch (e) {
            if(window.console) window.console.warn('GoWarp Toolkit:', e);
        }
    }

    var styleInjected = false;
    function ensureStyle () {
        if (styleInjected || !document.head) return;
        if (window.CSSStyleSheet && CSSStyleSheet.prototype.replaceSync && document.adoptedStyleSheets !== undefined) {
            try {
                sheet = new CSSStyleSheet();
                sheet.replaceSync(CSS);
                document.adoptedStyleSheets = document.adoptedStyleSheets.concat([sheet]);
                styleInjected = true;
                return;
            } catch (e) {}
        }
        styleTag = document.createElement('style') as HTMLStyleElement;
        var tag = styleTag;
        tag.textContent = CSS;
        document.head.appendChild(tag);
        styleInjected = true;
    }

    const layers = createLayers(context, {
        paintingLayer,
        selectedPaths,
        commit,
        eachSelected,
        icon,
        paintEditorOpen,
        colorSig
    });
    const {markObjects, syncRail, destroyRail} = layers;
    const anchors = createAnchors(context, {
        anchorTarget,
        findVM,
        get railEl() {
            return layers.railEl;
        },
        markObjects,
        keyRow,
        get openPanel() {
            return openPanel;
        },
        toggleFor,
        labelEl
    });
    const {showAnchorPanel, applyAnchor, markAnchor, anchorState} = anchors;
    const folders = createFolders(context, {
        findVM,
        ensureStyle
    });
    const { sfSync, sfTeardown } = folders;

    listen(window, 'keydown', onKeyDown);
    listen(window, 'resize', function () {
        if(openPanel) fitPanel(openPanel);
    });
    listen(window, 'pointerdown', function (e: PointerEvent) {
        if(openPanel && openPanel._gwtKind === 'nib' && /paper-canvas/.test(String(e.target && (e.target as Element).className))) closePanel();
        refreshPanels();
    }, true);
    listen(window, 'keyup', refreshPanels, true);

    tick();
    repeat(tick, 900);
    return {
        dispose() {
            if(disposed) return;
            disposed = true;
            lifecycle.dispose();
            unsubscribe?.();
            if(editor.storeRef) delete editor.storeRef.__gwtWatch;
            delete document.__gwtGrad;
            closePanel();
            pen.deactivate(); shaper.disarm(); destroyRail(); dropGradStrip(); sfTeardown();
            document.querySelectorAll('.gwt-tool, .gwt-toast').forEach((element: Element) => element.remove());
            document.querySelectorAll('[data-gwt-mounted], [data-gwt-wheel]').forEach((element: HTMLElement) => {
                delete element.dataset.gwtMounted; delete element.dataset.gwtWheel;
            });
            styleTag?.remove();
            if(sheet) document.adoptedStyleSheets = document.adoptedStyleSheets.filter((item: CSSStyleSheet) => item !== sheet);
        }
    };
}
