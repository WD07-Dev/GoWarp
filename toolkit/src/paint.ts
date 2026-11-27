import {paintStyle as CSS} from './paint-style';
import {paintStrings as STRINGS} from './paint-strings';
import type {ScratchHost, PaintTools, EditorDocument, EditorWindow, Shape, PixelEditor, AlphaStats, FolderMenuItem} from './types';

export function installPaintTools(Scratch: ScratchHost): PaintTools {
    const document = globalThis.document as EditorDocument;
    const window = globalThis.window as EditorWindow;
    const listeners = new AbortController();
    const timers = new Set<number>();
    const intervals = new Set<number>();
    let disposed = false;
    let unsubscribe: (() => void) | undefined;
    let sheet: CSSStyleSheet | undefined;
    let styleTag: HTMLStyleElement | undefined;
    function listen<K extends keyof GlobalEventHandlersEventMap>(target: EventTarget, name: K, callback: (event: GlobalEventHandlersEventMap[K]) => void, options: boolean | AddEventListenerOptions = false) {
        target.addEventListener(name, callback as EventListener, {...(typeof options === 'boolean' ? {capture: options} : options), signal: listeners.signal});
    }
    function later(callback: () => void, delay: number) {
        const id = globalThis.setTimeout(() => { timers.delete(id); if (!disposed) callback(); }, delay);
        timers.add(id);
        return id;
    }
    function repeat(callback: () => void, delay: number) {
        const id = globalThis.setInterval(() => { if (!disposed) callback(); }, delay);
        intervals.add(id);
        return id;
    }
    var LS_KEY = 'gowarp-toolkit.paint';
    var ART_CENTER = {x: 480, y: 360};

    function clamp (v, a, b) {
        return Math.min(b, Math.max(a, v));
    }
    function fiberOf (el) {
        var key = Object.keys(el).find(function (k) {
            return k.indexOf('__reactInternalInstance$') === 0 || k.indexOf('__reactFiber$') === 0;
        });
        return key ? el[key] : null;
    }
    function paintCanvas () {
        return document.querySelector('canvas[class*="paper-canvas"]');
    }

    var paperRef = null;
    var storeRef = null;

    function isPaperScope (candidate) {
        if (!candidate || typeof candidate.setup !== 'function') return false;
        var project = candidate.project;
        return !!(project && project.view && project.layers && project.layers.length);
    }
    function findPaperInBundle () {
        var chunks = [window.webpackChunkGUI, window.webpackJsonpGUI];
        for (var c = 0; c < chunks.length; c++) {
            var chunk = chunks[c];
            if (!chunk || typeof chunk.push !== 'function') continue;
            var req = null;
            try {
                chunk.push([[Date.now()], {}, function (r) { req = r; }]);
            } catch (e) { continue; }
            if (!req || !req.m) continue;
            for (var id in req.m) {
                if (!Object.prototype.hasOwnProperty.call(req.m, id)) continue;
                var src = '';
                try { src = String(req.m[id]); } catch (e) { continue; }
                if (src.indexOf('PaperScope') < 0) continue;
                try {
                    var mod = req(id);
                    if (isPaperScope(mod)) return mod;
                } catch (e) {
                }
            }
        }
        return null;
    }
    function paper () {
        if (isPaperScope(paperRef)) return paperRef;
        var candidate = (window.paper && window.paper.project) ? window.paper : findPaperInBundle();
        paperRef = isPaperScope(candidate) ? candidate : null;
        return paperRef;
    }

    function findStoreInTree () {
        var canvas = paintCanvas();
        var fiber = canvas ? fiberOf(canvas) : null;
        for (var i = 0; i < 60 && fiber; i++) {
            var props = fiber.memoizedProps;
            if (props && props.value && props.value.store && typeof props.value.store.getState === 'function') {
                return props.value.store;
            }
            fiber = fiber.return;
        }
        return null;
    }
    function store () {
        if (storeRef && typeof storeRef.getState === 'function') return storeRef;
        var candidate = window.ReduxStore && typeof window.ReduxStore.getState === 'function'
            ? window.ReduxStore : findStoreInTree();
        storeRef = candidate || null;
        return storeRef;
    }
    function paintState () {
        var s = store();
        if (!s) return null;
        var state = s.getState();
        return state && state.scratchPaint ? state.scratchPaint : null;
    }
    function dispatch (action) {
        var s = store();
        if (s) s.dispatch(action);
    }
    function isBitmapFormat () {
        var p = paintState();
        return !!p && /^BITMAP/.test(p.format || '');
    }
    function isEraserMode () {
        var p = paintState();
        return !!p && /ERASER/.test(p.mode || '');
    }
    function load (fallback) {
        try {
            var raw = window.localStorage.getItem(LS_KEY);
            return raw ? Object.assign({}, fallback, JSON.parse(raw)) : fallback;
        } catch (e) {
            return fallback;
        }
    }
    function save(data) {
        try {
            window.localStorage.setItem(LS_KEY, JSON.stringify(data));
        } catch (e) {
        }
    }

    function findUpdateImage () {
        var canvas = paintCanvas();
        var fiber = canvas ? fiberOf(canvas) : null;
        for (var i = 0; i < 45 && fiber; i++) {
            var props = fiber.stateNode && fiber.stateNode.props ? fiber.stateNode.props : fiber.memoizedProps;
            if (props && typeof props.onUpdateImage === 'function') return props.onUpdateImage;
            fiber = fiber.return;
        }
        return null;
    }

    function commit () {
        var fn = findUpdateImage();
        if (!fn) return false;
        fn(false);
        return true;
    }

    function findLayer (flag) {
        var p = paper();
        if (!p) return null;
        var layers = p.project.layers;
        for (var i = 0; i < layers.length; i++) {
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
        if (!p) return null;
        var layer = findLayer('isGuideLayer');
        if (!layer) {
            layer = new p.Layer();
            layer.data = {isGuideLayer: true};
            var art = paintingLayer();
            if (art) art.activate();
        }
        return layer;
    }

    var CSS_COLOR = /^(#([0-9a-f]{3,4}|[0-9a-f]{6}|[0-9a-f]{8})|rgba?\(|hsla?\(|hwb\()/i;
    function colorOf (style) {
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
    var lastPaint = {fill: null, stroke: null};
    function pickColor (style, key) {
        var color = colorOf(style);
        if (color) {
            lastPaint[key] = style.primary;
            return color;
        }
        return lastPaint[key] ? colorOf({primary: lastPaint[key]}) : null;
    }
    function currentColors () {
        var ps = paintState();
        var color = ps && ps.color ? ps.color : {};
        return {
            fill: pickColor(color.fillColor, 'fill'),
            stroke: pickColor(color.strokeColor, 'stroke'),
            strokeWidth: color.strokeWidth || 0
        };
    }
    function applyStyle (path, useStroke) {
        var colors = currentColors();
        var mine = gwtPaint('fill', path.bounds, 0);
        var stroke = useStroke && colors.stroke;
        path.fillColor = mine === null ? colors.fill : mine;
        path.strokeColor = stroke ? colors.stroke : null;
        path.strokeWidth = stroke ? Math.max(colors.strokeWidth, 1) : 0;
        path.strokeCap = 'round';
        path.strokeJoin = 'round';
    }

    function usable (v) {
        return typeof v === 'string' && CSS_COLOR.test(v.trim());
    }
    function transparentLike (p, css) {
        var c = new p.Color(css);
        c.alpha = 0;
        return c.toCSS();
    }
    function gradientColor (p, stops, type, bounds, minSize) {
        var list = stops.map(function (s, i) {
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
    function nativeStyle (key) {
        var color = (paintState() || {}).color || {};
        return color[key === 'stroke' ? 'strokeColor' : 'fillColor'] || {};
    }
    function gradType (key) {
        var type = nativeStyle(key).gradientType;
        return type === 'RADIAL' || type === 'HORIZONTAL' || type === 'VERTICAL' ? type : null;
    }
    function gradStops (key) {
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
    function gwtGradient (key) {
        if (!paper() || !gradType(key)) return null;
        var stops = gradStops(key);
        if (!stops) return null;
        return stops.map(function (s) { return [s.p, usable(s.c) ? s.c : null]; });
    }
    function gwtPaint (key, bounds, minSize, type?: any) {
        var p = paper();
        var stops = gwtGradient(key);
        if (!p || !stops) return null;
        return gradientColor(p, stops, type || gradType(key), bounds, minSize);
    }
    function styleShapePath (path) {
        var p = paper();
        var color = (paintState() || {}).color || {};
        var width = color.strokeWidth || 0;
        [['fill', 'fillColor'], ['stroke', 'strokeColor']].forEach(function (pair) {
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

    function round (v) {
        return Math.round(v * 1000) / 1000;
    }
    function normalizePoints (points) {
        var xs = points.map(function (p) { return p[0]; });
        var ys = points.map(function (p) { return p[1]; });
        var minX = Math.min.apply(null, xs);
        var minY = Math.min.apply(null, ys);
        var w = Math.max.apply(null, xs) - minX || 1;
        var h = Math.max.apply(null, ys) - minY || 1;
        return points.map(function (p) {
            return [round((p[0] - minX) / w * 100), round((p[1] - minY) / h * 100)];
        });
    }
    function polygonPath (points) {
        return points.map(function (p, i) {
            return (i === 0 ? 'M' : 'L') + p[0] + ',' + p[1];
        }).join(' ') + 'Z';
    }
    function cornerPath (points, radius) {
        if (!radius || radius <= 0) return polygonPath(points);
        var count = points.length;
        var path = '';
        for (var i = 0; i < count; i++) {
            var previous = points[(i + count - 1) % count];
            var point = points[i];
            var next = points[(i + 1) % count];
            var entryLength = Math.hypot(point[0] - previous[0], point[1] - previous[1]);
            var exitLength = Math.hypot(next[0] - point[0], next[1] - point[1]);
            var cut = Math.min(radius, entryLength / 2, exitLength / 2);
            var entry = [point[0] + ((previous[0] - point[0]) / (entryLength || 1)) * cut,
                point[1] + ((previous[1] - point[1]) / (entryLength || 1)) * cut];
            var exit = [point[0] + ((next[0] - point[0]) / (exitLength || 1)) * cut,
                point[1] + ((next[1] - point[1]) / (exitLength || 1)) * cut];
            path += (i === 0 ? 'M' : 'L') + round(entry[0]) + ',' + round(entry[1]) + ' ';
            path += 'Q' + point[0] + ',' + point[1] + ' ' + round(exit[0]) + ',' + round(exit[1]) + ' ';
        }
        return path + 'Z';
    }
    function regularPolygon (sides, startDegrees) {
        var points = [];
        for (var i = 0; i < sides; i++) {
            var angle = ((startDegrees + (i * 360 / sides)) * Math.PI) / 180;
            points.push([50 + (50 * Math.cos(angle)), 50 + (50 * Math.sin(angle))]);
        }
        return normalizePoints(points);
    }
    function rotatePoints (points, degrees) {
        var radians = degrees * Math.PI / 180;
        var cos = Math.cos(radians);
        var sin = Math.sin(radians);
        return normalizePoints(points.map(function (p) {
            var x = p[0] - 50;
            var y = p[1] - 50;
            return [50 + (x * cos) - (y * sin), 50 + (x * sin) + (y * cos)];
        }));
    }
    function starPoints (tips, inner) {
        var points = [];
        for (var i = 0; i < tips * 2; i++) {
            var radius = i % 2 === 0 ? 50 : inner;
            var angle = ((-90 + (i * 180 / tips)) * Math.PI) / 180;
            points.push([50 + (radius * Math.cos(angle)), 50 + (radius * Math.sin(angle))]);
        }
        return normalizePoints(points);
    }
    function crescentPoints () {
        var points = [];
        var i;
        for (i = 0; i <= 24; i++) {
            var a = (-90 + (i * 180 / 24)) * Math.PI / 180;
            points.push([50 + (50 * Math.cos(a)), 50 + (50 * Math.sin(a))]);
        }
        for (i = 24; i >= 0; i--) {
            var b = (-90 + (i * 180 / 24)) * Math.PI / 180;
            points.push([50 + (50 * Math.cos(b)) + 26 * Math.sin(b), 50 + (50 * Math.sin(b))]);
        }
        return normalizePoints(points);
    }

    var PLUS = [
        [35, 0], [65, 0], [65, 35], [100, 35], [100, 65], [65, 65],
        [65, 100], [35, 100], [35, 65], [0, 65], [0, 35], [35, 35]
    ];

    var SHAPES: Shape[] = [
        {id: 'triangle', label: 'Üçgen', en: 'Triangle', points: [[50, 0], [100, 100], [0, 100]]},
        {id: 'right-triangle', label: 'Dik üçgen', en: 'Right triangle', points: [[0, 0], [100, 100], [0, 100]]},
        {id: 'rhombus', label: 'Eşkenar dörtgen', en: 'Rhombus', points: [[50, 0], [100, 50], [50, 100], [0, 50]]},
        {id: 'parallelogram', label: 'Paralelkenar', en: 'Parallelogram', points: [[22, 0], [100, 0], [78, 100], [0, 100]]},
        {id: 'trapezoid', label: 'Yamuk', en: 'Trapezoid', points: [[24, 0], [76, 0], [100, 100], [0, 100]]},
        {id: 'pentagon', label: 'Beşgen', en: 'Pentagon', points: regularPolygon(5, -90)},
        {id: 'hexagon', label: 'Altıgen', en: 'Hexagon', points: regularPolygon(6, -90)},
        {id: 'heptagon', label: 'Yedigen', en: 'Heptagon', points: regularPolygon(7, -90)},
        {id: 'octagon', label: 'Sekizgen', en: 'Octagon', points: regularPolygon(8, -67.5)},
        {id: 'star5', label: 'Yıldız', en: 'Star', points: starPoints(5, 20)},
        {id: 'star6', label: 'Altı köşeli yıldız', en: 'Six-pointed star', points: starPoints(6, 28)},
        {id: 'soft-triangle', label: 'Yumuşak üçgen', en: 'Rounded triangle', points: [[50, 0], [100, 100], [0, 100]], minRound: 14},
        {id: 'soft-pentagon', label: 'Yumuşak beşgen', en: 'Rounded pentagon', points: regularPolygon(5, -90), minRound: 12},
        {id: 'soft-hexagon', label: 'Yumuşak altıgen', en: 'Rounded hexagon', points: regularPolygon(6, -90), minRound: 12},
        {id: 'soft-square', label: 'Yumuşak kare', en: 'Rounded square', points: [[0, 0], [100, 0], [100, 100], [0, 100]], minRound: 22},
        {id: 'half-circle', label: 'Yarım daire', en: 'Half circle',
            path: 'M0,100 L0,50 A50,50 0 0 1 100,50 L100,100 Z'},
        {id: 'quarter-circle', label: 'Çeyrek daire', en: 'Quarter circle',
            path: 'M0,100 L0,0 A100,100 0 0 1 100,100 Z'},
        {id: 'ring', label: 'Halka', en: 'Ring', fillRule: 'evenodd',
            path: 'M50,0 A50,50 0 1 0 50,100 A50,50 0 1 0 50,0 Z M50,30 A20,20 0 1 1 50,70 A20,20 0 1 1 50,30 Z'},
        {id: 'drop', label: 'Damla', en: 'Drop',
            path: 'M50,2 C62,20 86,38 86,62 C86,82 70,98 50,98 C30,98 14,82 14,62 C14,38 38,20 50,2 Z'},
        {id: 'crescent', label: 'Hilal', en: 'Crescent', points: crescentPoints()},
        {id: 'arrow-up', label: 'Yukarı ok', en: 'Arrow up',
            points: [[50, 0], [100, 50], [65, 50], [65, 100], [35, 100], [35, 50], [0, 50]]},
        {id: 'arrow-down', label: 'Aşağı ok', en: 'Arrow down',
            points: [[50, 100], [100, 50], [65, 50], [65, 0], [35, 0], [35, 50], [0, 50]]},
        {id: 'arrow-right', label: 'Sağ ok', en: 'Arrow right',
            points: [[0, 35], [50, 35], [50, 0], [100, 50], [50, 100], [50, 65], [0, 65]]},
        {id: 'arrow-left', label: 'Sol ok', en: 'Arrow left',
            points: [[100, 35], [50, 35], [50, 0], [0, 50], [50, 100], [50, 65], [100, 65]]},
        {id: 'arrow-double', label: 'Çift ok', en: 'Double arrow',
            points: [[0, 50], [25, 20], [25, 35], [75, 35], [75, 20], [100, 50], [75, 80], [75, 65], [25, 65], [25, 80]]},
        {id: 'heart', label: 'Kalp', en: 'Heart',
            path: 'M50,90 C20,66 2,48 2,29 C2,13 14,2 28,2 C38,2 46,8 50,17 ' +
                'C54,8 62,2 72,2 C86,2 98,13 98,29 C98,48 80,66 50,90 Z'},
        {id: 'cloud', label: 'Bulut', en: 'Cloud',
            path: 'M26,80 C11,80 2,69 2,55 C2,42 11,32 23,30 C27,15 41,4 57,4 ' +
                'C74,4 88,16 91,32 C97,36 100,43 100,51 C100,67 89,80 73,80 Z'},
        {id: 'bolt', label: 'Şimşek', en: 'Bolt',
            points: [[58, 0], [16, 56], [43, 56], [34, 100], [86, 40], [57, 40], [78, 0]]},
        {id: 'bubble', label: 'Konuşma balonu', en: 'Speech bubble', minRound: 6,
            points: [[12, 4], [88, 4], [96, 12], [96, 62], [88, 70], [42, 70], [26, 88], [28, 70], [12, 70], [4, 62], [4, 12]]},
        {id: 'plus', label: 'Artı', en: 'Plus', points: PLUS},
        {id: 'cross', label: 'Çarpı', en: 'Cross', points: rotatePoints(PLUS, 45)}
    ];
    SHAPES.forEach(function (shape) {
        shape.d = shape.points ? cornerPath(shape.points, shape.minRound || 0) : shape.path;
    });

    var NIBS = [
        {label: 'Kıl uç', en: 'Fine tip', size: 1}, {label: 'İnce uç', en: 'Thin tip', size: 2},
        {label: 'Ucu ince kalem', en: 'Pencil tip', size: 3}, {label: 'Tükenmez', en: 'Ballpoint', size: 5},
        {label: 'Keçe ucu', en: 'Felt tip', size: 8}, {label: 'Boya kalemi', en: 'Crayon', size: 12},
        {label: 'Kalın kalem', en: 'Thick pen', size: 18}, {label: 'Fırça', en: 'Brush', size: 26},
        {label: 'Kalın fırça', en: 'Thick brush', size: 40}, {label: 'Boya rulosu', en: 'Roller', size: 60},
        {label: 'Devasa', en: 'Huge', size: 100}
    ];

    var PENS = [
        {id: 'kalem', label: 'Kalem', en: 'Pen', min: 0.72, exponent: 0.35, taper: 0.3, smoothing: 0.3},
        {id: 'tukenmez', label: 'Tükenmez', en: 'Ballpoint', min: 0.92, exponent: 0.15, taper: 0.1, smoothing: 0.25},
        {id: 'firca', label: 'Fırça', en: 'Brush', min: 0.15, exponent: 1.4, taper: 0.45, smoothing: 0.5},
        {id: 'marker', label: 'Kalın marker', en: 'Marker', min: 0.95, exponent: 0.1, taper: 0.05, smoothing: 0.2},
        {id: 'kaligrafi', label: 'Kaligrafi', en: 'Calligraphy', min: 0.3, exponent: 0.8, taper: 0.25, smoothing: 0.35, chisel: true},
        {id: 'murekkep', label: 'Mürekkep', en: 'Ink', min: 0.05, exponent: 2.1, taper: 0.7, smoothing: 0.45}
    ];

    function nibSwatch (profile) {
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
            ? old.stops.map(function (s) { return s.p; }) : null;
        var offsets = old && Array.isArray(old.offsets) && old.offsets.length === count ? old.offsets : from;
        var extras = old && Array.isArray(old.extras) ? old.extras
            : old && Array.isArray(old.stops) ? old.stops.slice(1, -1).map(function (s) { return s.c; }) : [];
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

    function nibAction (size) {
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
    function applyNib (size) {
        size = clamp(Math.round(size), 1, 100);
        if (isBitmapFormat()) {
            if (isEraserMode()) settings.bitEraser = size; else settings.bitNib = size;
        } else if (isEraserMode()) settings.eraser = size; else settings.nib = size;
        dispatch(nibAction(size));
        saveSettings();
        refreshNibUi();
    }
    function stepNib (delta) {
        var size = currentNibSize();
        applyNib(size + delta * Math.max(1, Math.round(size * 0.18)));
    }

    var lang = 'en';
    var isRtl = false;

    function normalizeLang (code: string) {
        const language = String(code || '').toLowerCase().split(/[-_]/)[0];
        return language in STRINGS ? language : 'en';
    }
    function t (key) {
        var dict = STRINGS[lang] || STRINGS.en;
        return dict[key] || STRINGS.en[key] || key;
    }
    function nameOf (item) {
        return lang === 'tr' ? item.label : item.en;
    }
    function readLocale () {
        var s = store();
        var loc = s ? (s.getState().locales || {}) : {};
        lang = normalizeLang(loc.locale || Scratch.translate.language || document.documentElement.lang || navigator.language);
        isRtl = !!loc.isRtl;
    }

    function parseColor (input) {
        var s = String(input || '').trim();
        if (!s) return null;
        var hex = s.match(/^#([0-9a-f]{3,8})$/i);
        if (hex) {
            var h = hex[1];
            if (h.length === 3 || h.length === 4) {
                h = h.split('').map(function (c) { return c + c; }).join('');
            }
            if (h.length < 6) return null;
            return {r: parseInt(h.slice(0, 2), 16), g: parseInt(h.slice(2, 4), 16), b: parseInt(h.slice(4, 6), 16),
                a: h.length >= 8 ? parseInt(h.slice(6, 8), 16) / 255 : 1};
        }
        var rgb = s.match(/rgba?\(([^)]+)\)/i);
        if (rgb) {
            var parts = rgb[1].split(/[\s,/]+/).map(Number);
            return {r: parts[0] || 0, g: parts[1] || 0, b: parts[2] || 0, a: parts.length > 3 ? parts[3] : 1};
        }
        return null;
    }
    function rgba (c, a) {
        return 'rgba(' + c.r + ',' + c.g + ',' + c.b + ',' + a + ')';
    }
    function brightness (c) {
        return (0.299 * c.r + 0.587 * c.g + 0.114 * c.b) / 255;
    }
    function cssVar (cs, name) {
        var v = cs.getPropertyValue(name);
        return v ? v.trim() : '';
    }
    function paintedColor (node, property) {
        for (var hops = 0; hops < 8 && node; hops++) {
            var value = window.getComputedStyle(node)[property];
            var parsed = parseColor(value);
            if (parsed && parsed.a > 0.05) return value;
            node = node.parentElement;
        }
        return null;
    }
    var hashedClassCache = {};
    function nativeClass (fragment) {
        if (hashedClassCache[fragment]) return hashedClassCache[fragment];
        var host = document.querySelector('[class*="' + fragment + '"]');
        if (host) {
            var live = Array.prototype.filter.call(host.classList, function (c) {
                return c.indexOf(fragment) >= 0;
            })[0];
            if (live) {
                hashedClassCache[fragment] = live;
                return live;
            }
        }
        for (var i = 0; i < document.styleSheets.length; i++) {
            var rules;
            try { rules = document.styleSheets[i].cssRules; } catch (e) { continue; }
            for (var r = 0; r < rules.length; r++) {
                var text = rules[r].cssText || '';
                if (text.indexOf(fragment) < 0) continue;
                var token = text.match(new RegExp('[a-zA-Z0-9_-]*' + fragment + '[a-zA-Z0-9_-]*'));
                if (token) {
                    hashedClassCache[fragment] = token[0];
                    return token[0];
                }
            }
        }
        return null;
    }

    function readTheme () {
        var host = document.querySelector('[class*="paint-editor_mode-selector"]') || document.body;
        var cs = window.getComputedStyle(host);
        var selected = document.querySelector('[class*="tool-select-base_is-selected"]');
        var field = document.querySelector('input[class*="input_input"]');
        var fieldCs = field ? window.getComputedStyle(field) : null;

        var raw = {
            surface: cssVar(cs, '--paint-popover-background') || paintedColor(host, 'backgroundColor'),
            accent: (selected ? window.getComputedStyle(selected).backgroundColor : '') ||
                cssVar(cs, '--paint-looks-secondary'),
            text: cssVar(cs, '--paint-text-primary') || paintedColor(host, 'color'),
            field: cssVar(cs, '--paint-input-background') || (fieldCs ? fieldCs.backgroundColor : ''),
            border: cssVar(cs, '--paint-form-border') || (fieldCs ? fieldCs.borderColor : ''),
            tint: cssVar(cs, '--paint-looks-transparent')
        };
        var surface = parseColor(raw.surface) || {r: 255, g: 255, b: 255, a: 1};
        var accent = parseColor(raw.accent) || {r: 133, g: 92, b: 214, a: 1};
        var dark = brightness(surface) < 0.55;
        var text = parseColor(raw.text) || (dark ? {r: 238, g: 238, b: 238, a: 1} : {r: 87, g: 94, b: 117, a: 1});
        return {
            dark: dark,
            surface: raw.surface || (dark ? '#1e1e1e' : '#ffffff'),
            accent: raw.accent || rgba(accent, 1),
            text: raw.text || (dark ? '#eeeeee' : '#575e75'),
            field: raw.field || (dark ? rgba(accent, 0.12) : rgba(accent, 0.08)),
            border: raw.border || (dark ? 'rgba(255,255,255,.15)' : 'rgba(0,0,0,.12)'),
            hover: rgba(accent, dark ? 0.16 : 0.1),
            active: raw.tint && parseColor(raw.tint) ? raw.tint : rgba(accent, 0.35),
            shadow: dark ? '0 8px 24px rgba(0,0,0,.5)' : '0 8px 24px rgba(148,162,188,.35)'
        };
    }
    function applyTheme (panel, theme) {
        var set = function (name, value) {
            panel.style.setProperty('--gwt-' + name, value);
        };
        panel._gwtAccent = theme.accent;
        set('surface', theme.surface);
        set('accent', theme.accent);
        set('text', theme.text);
        set('field', theme.field);
        set('field-border', theme.border);
        set('hover', theme.hover);
        set('active', theme.active);
        set('shadow', theme.shadow);
        set('origin', panel._gwtFlipped ? 'right center' : 'left center');
        panel.style.colorScheme = theme.dark ? 'dark' : 'light';
    }

    var openPanel = null;
    var openAnchor = null;
    var toolButtons = [];

    function markTool (btn, on, fragment) {
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
    function overlapArea (a, b) {
        if (!a || !b) return 0;
        var w = Math.min(a.right, b.right) - Math.max(a.left, b.left);
        var h = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
        return w > 0 && h > 0 ? w * h : 0;
    }
    function boxOf (left, top, w, h) {
        return {left: left, top: top, right: left + w, bottom: top + h};
    }

    function fitPanel (panel) {
        var rect = panel._gwtAnchor.getBoundingClientRect();
        var box = panel.getBoundingClientRect();
        var art = artRect();
        var vw = window.innerWidth, vh = window.innerHeight;
        var fits = function (x, y) {
            return x >= 8 && y >= 8 && x + box.width <= vw - 8 && y + box.height <= vh - 8;
        };
        var tops = [clamp(rect.top, 8, Math.max(8, vh - box.height - 8)),
            art ? art.bottom + 8 : 0, art ? art.top - box.height - 8 : 0];
        var xs = [rect.right + 10, rect.left - box.width - 10, vw - box.width - 8, 8];
        var cands = [];
        xs.forEach(function (x) {
            if (Math.abs(x - rect.left) > box.width + rect.width + 60) return;
            tops.forEach(function (y) {
                if (!fits(x, y)) return;
                cands.push({x: x, y: y, d: Math.abs(x - rect.left),
                    hit: overlapArea(boxOf(x, y, box.width, box.height), art)});
            });
        });
        if (!cands.length) cands.push({x: xs[0], y: tops[0], d: 0, hit: 0});
        cands.sort(function (a, b) {
            return a.hit !== b.hit ? a.hit - b.hit : a.d - b.d;
        });
        var best = cands[0];
        panel.style.setProperty('--gwt-origin', best.x > rect.left ? 'left center' : 'right center');
        panel.style.left = best.x + 'px';
        panel.style.top = best.y + 'px';
    }

    function toggleFor (anchor, kind, extraClass?: any) {
        if (openPanel && openAnchor === anchor) {
            closePanel();
            return null;
        }
        closePanel();
        readLocale();
        var panel = document.createElement('div');
        panel.className = 'gwt-panel' + (extraClass ? ' ' + extraClass : '');
        panel.setAttribute('dir', isRtl ? 'rtl' : 'ltr');
        panel._gwtAnchor = anchor;
        panel._gwtKind = kind;
        var theme = readTheme();
        applyTheme(panel, theme);
        if (anchor.parentElement) anchor.parentElement.style.setProperty('--gwt-hl', theme.accent);
        var close = document.createElement('button');
        close.type = 'button';
        close.className = 'gwt-close';
        close.setAttribute('aria-label', t('close'));
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
    function lineIcon (inner) {
        return 'data:image/svg+xml;base64,' + window.btoa(
            '<svg xmlns="http://www.w3.org/2000/svg" width="20px" height="20px" viewBox="0 0 20 20">' +
            '<g fill="none" stroke="' + ICON_FILL + '" stroke-width="1.6" stroke-linecap="round" ' +
            'stroke-linejoin="round">' + inner + '</g></svg>');
    }
    var SHAPE_TOOL_ICON = lineIcon('<path d="M6.6 8 11.6 16.2H1.6z"/><circle cx="14.2" cy="6.2" r="3.9"/>');
    var PEN_TOOL_ICON = lineIcon('<path d="M10 2.4c2.7 3 4 5.4 4 7.6a4 4 0 0 1-8 0c0-2.2 1.3-4.6 4-7.6z"/>' +
        '<path d="M10 7.6v3.4"/><circle cx="10" cy="13.6" r=".9" fill="' + ICON_FILL + '" stroke="none"/>');
    var FX_TOOL_ICON = lineIcon('<circle cx="7.8" cy="10" r="4.9"/><circle cx="12.2" cy="10" r="4.9"/>');
    var ANCHOR_TOOL_ICON = lineIcon('<circle cx="10" cy="10" r="3.5"/>' +
        '<path d="M10 1.9v2.8M10 15.3v2.8M1.9 10h2.8M15.3 10h2.8"/>' +
        '<circle cx="10" cy="10" r=".95" fill="' + ICON_FILL + '" stroke="none"/>');
    var BOARD_PATH = '<rect x="2.6" y="3.6" width="8.2" height="12.8" rx="1.2"/>' +
        '<rect x="12.6" y="3.6" width="4.8" height="7.2" rx="1.2"/><path d="M12.6 13.6h4.8"/>';
    var BOARD_TOOL_ICON = lineIcon(BOARD_PATH);
    var TOOL_ICON = {shapes: SHAPE_TOOL_ICON, pen: PEN_TOOL_ICON, fx: FX_TOOL_ICON, anchor: ANCHOR_TOOL_ICON,
        board: BOARD_TOOL_ICON};
    var BOARD_GLYPH = icon(BOARD_PATH);

    function icon (paths) {
        return '<svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.5" ' +
            'stroke-linecap="round" stroke-linejoin="round">' + paths + '</svg>';
    }
    var GLYPH = {
        size: icon('<path d="M2.5 10h15"/><path d="M5.5 7 2.5 10l3 3"/><path d="M14.5 7l3 3-3 3"/>'),
        round: icon('<path d="M3 6.5A3.5 3.5 0 0 1 6.5 3h11v14h-11A3.5 3.5 0 0 1 3 13.5z"/>'),
        nib: icon('<path d="M10 3c2.4 2.7 3.6 5 3.6 7.2A3.6 3.6 0 0 1 6.4 10.2C6.4 8 7.6 5.7 10 3z"/>' +
            '<path d="M10 16.4v1.6"/>'),
        undo: icon('<path d="M4 8.5h8.5a4 4 0 1 1 0 8H7"/><path d="M4 8.5 7 5.5M4 8.5l3 3"/>'),
        wheel: icon('<rect x="6.5" y="2.5" width="7" height="15" rx="3.5"/><path d="M10 5.2v2.6"/>'),
        plus: icon('<path d="M10 4v12M4 10h12"/>'),
        trash: icon('<path d="M4.5 6h11M8 6V4.5h4V6M6 6l.7 10h6.6L14 6"/>')
    };
    function shapeIcon (shape) {
        return '<svg viewBox="-8 -8 116 116"><path d="' + (shape.d || shape.path) + '" fill="currentColor" fill-rule="' +
            (shape.fillRule || 'nonzero') + '"/></svg>';
    }

    function addToolButton (grid, key, iconUrl, onClick) {
        var proto = grid.querySelector('[class*="tool-select-base_mod-tool-select"]');
        if (!proto) return null;
        var btn = proto.cloneNode(false);
        btn.className = btn.className.replace(/tool-select-base_is-selected\S*/g, '').replace(/\s+/g, ' ').trim() +
            ' gwt-tool';
        if (!btn.hasAttribute('tabindex')) btn.setAttribute('tabindex', '0');
        if (!btn.getAttribute('role')) btn.setAttribute('role', 'button');
        btn._gwtKey = key;
        btn.dataset.gwtKey = String(key);
        btn.setAttribute('title', t(key));
        btn.setAttribute('aria-label', t(key));
        var imgClass = nativeClass('tool-select-base_tool-select-icon') || 'gwt-icon';
        var img = document.createElement('img');
        img.alt = '';
        img.draggable = false;
        img.className = imgClass;
        img.src = iconUrl;
        btn.textContent = '';
        btn.appendChild(img);
        listen(btn, 'click', onClick);
        listen(btn, 'keydown', function (e) {
            if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                btn.click();
            }
        });
        grid.appendChild(btn);
        toolButtons.push(btn);
        return btn;
    }
    function markArmed (btn, on) {
        if (btn && btn.classList.contains('gwt-tool')) btn.classList.toggle('gwt-armed', !!on);
    }
    function refreshToolLabels () {
        toolButtons.forEach(function (btn) {
            if (!btn.isConnected) return;
            var armed = btn === pen.button && pen.active && pen.profile;
            var stamp = btn === shaper.button && shaper.shape;
            var text = armed ? nameOf(pen.profile) : stamp ? nameOf(shaper.shape) : t(btn._gwtKey);
            btn.setAttribute('title', text);
            btn.setAttribute('aria-label', text);
        });
    }

    var SLIDER_TEXT = {shapeSize: 'size', shapeRound: 'round', penSize: 'size'};
    function sliderValueText (key, value) {
        return key === 'shapeRound' ? String(value) : value + ' px';
    }
    function sliderRow (glyph, min, max, step, key, onChange) {
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
        input.setAttribute('aria-label', t(SLIDER_TEXT[key] || key));
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
    function keyRow (chips) {
        var row = document.createElement('div');
        row.className = 'gwt-keys';
        chips.forEach(function (chip) {
            if (chip.charAt(0) === '<') {
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

    function buildShapePath (shape, where) {
        var p = paper();
        var radius = shape.points ? Math.max(settings.shapeRound, shape.minRound || 0) : 0;
        var d = shape.points ? cornerPath(shape.points, radius) : shape.path;
        var path = new p.Path(d);
        path.closed = true;
        if (shape.fillRule) path.fillRule = shape.fillRule;
        var scale = settings.shapeSize / 100;
        path.scale(scale);
        path.applyMatrix = true;
        if (where && typeof where.width === 'number') path.bounds = where;
        else path.position = where || viewCenter();
        styleShapePath(path);
        return path;
    }

    function insertShape (shape, where) {
        var p = paper();
        var layer = paintingLayer();
        var color = (paintState() || {}).color || {};
        var paints = function (style, key) {
            style = style || {};
            return gwtGradient(key) || usable(style.primary) || usable(style.secondary) || !!lastPaint[key];
        };
        if (!p || !layer || (!paints(color.fillColor, 'fill') && !paints(color.strokeColor, 'stroke'))) return false;
        var path = buildShapePath(shape, where);
        try {
            return isBitmapFormat() ? bakeIntoRaster(path) : addVectorShape(path, layer);
        } catch (e) {
            path.remove();
            if (window.console) window.console.warn('GoWarp Toolkit şekil: ' + e.message);
            return false;
        }
    }

    function bakeIntoRaster (path) {
        var raster = rasterItem();
        if (!raster || !raster.canvas) {
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

    function addVectorShape (path, layer) {
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
        if (!shapePreview) return;
        shapePreview.remove();
        shapePreview = null;
        var art = paintingLayer();
        if (art) art.activate();
    }

    function sweepGhosts () {
        if (shaper.shape || pen.active || shapePreview) return;
        var layer = findLayer('isGuideLayer');
        if (!layer) return;
        for (var i = layer.children.length - 1; i >= 0; i--) {
            if (layer.children[i]._gwtGhost) layer.children[i].remove();
        }
    }
    function previewShape (shape, box?: any) {
        var p = paper();
        var layer = guideLayer();
        if (!p || !layer) return;
        if (shapePreview) {
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
        if (!previewed) return;
        previewShape(previewed);
    }

    var shaper = {
        button: null,
        shape: null,
        start: null,
        cursor: function (on) {
            var canvas = document.querySelector('canvas[class*="paper-canvas"]');
            if (canvas) canvas.style.cursor = on ? 'crosshair' : '';
        },
        boxOf: function (from, to) {
            var p = paper();
            var w = Math.abs(to.x - from.x);
            var h = Math.abs(to.y - from.y);
            if (!p || w < 3 || h < 3) return null;
            return new p.Rectangle(Math.min(from.x, to.x), Math.min(from.y, to.y), w, h);
        },
        arm: function (shape) {
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
        onDown: function (e) {
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
        onMove: function (e) {
            if (!shaper.shape) return;
            var point = pen.toProject(e);
            if (!point) return;
            if (!shaper.start) {
                previewShape(shaper.shape, point);
                return;
            }
            previewShape(shaper.shape, shaper.boxOf(shaper.start, point) || shaper.start);
        },
        onUp: function (e) {
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

    function cell (label, html) {
        var b = document.createElement('button');
        b.type = 'button';
        b.className = 'gwt-cell';
        b.title = label;
        b.setAttribute('aria-label', label);
        b.innerHTML = html;
        return b;
    }

    function demoBlend (mode) {
        return '<i class="gwt-demo"><b></b><u style="mix-blend-mode:' + mode + '"></u></i>';
    }
    function demoMask (masked) {
        return '<i class="gwt-demo gwt-maskdemo' + (masked ? ' gwt-masked' : '') +
            '"><b></b><u></u></i>';
    }

    function msg (id, fallback) {
        var s = store();
        try {
            return (s.getState().locales.messages || {})[id] || fallback;
        } catch (e) {
            return fallback;
        }
    }
    function selectedPaths () {
        var layer = paintingLayer();
        var out = [];
        if (!layer) return out;
        (function walk (node) {
            if (node.locked) return;
            if (node !== layer && node.selected) {
                out.push(node);
                return;
            }
            if (node.children) node.children.forEach(walk);
        })(layer);
        return out;
    }
    function topOf (items) {
        var layer = paintingLayer();
        var top = null;
        if (layer) {
            (function find (node) {
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
        (function walk (node) {
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
        return Array.prototype.some.call(p.project.layers, function (layer) {
            var data = layer.data || {};
            if (!data.isPaintingLayer && !data.isRasterLayer) return false;
            return Array.prototype.some.call(layer.children, function (item) {
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
        items.forEach(function (item) {
            item[styleKey] = gwtPaint(gradTarget, item.bounds, stroke ? item.strokeWidth : 0, type);
        });
        p.view.update();
        return commit();
    }

    var BLENDS = ['normal', 'multiply', 'screen', 'overlay', 'darken', 'lighten',
        'color-dodge', 'color-burn', 'hard-light', 'soft-light', 'difference', 'exclusion',
        'hue', 'saturation', 'color', 'luminosity'];

    function eachSelected (fn, live?: any) {
        var p = paper();
        var items = selectedPaths();
        if (!p || !items.length) return false;
        items.forEach(fn);
        p.view.update();
        return live ? true : commit();
    }
    function applyBlend (mode) {
        return eachSelected(function (item) {
            item.blendMode = mode;
        });
    }
    function blendOfSelection () {
        var items = selectedPaths();
        return items.length ? String(items[0].blendMode || 'normal') : '';
    }
    function stepBlend (dir) {
        var at = BLENDS.indexOf(blendOfSelection());
        if (at < 0) at = 0;
        var next = BLENDS[(at + dir + BLENDS.length) % BLENDS.length];
        if (applyBlend(next)) markFx();
        return next;
    }
    function canMask () {
        return selectedPaths().length > 0 && artItems().length > 1;
    }
    function setMask (on) {
        var p = paper();
        var items = selectedPaths();
        if (!p || !items.length || (on && !canMask())) return false;
        var top = topOf(items);
        artItems().forEach(function (item) {
            if (item.clipMask && items.indexOf(item) < 0) item.clipMask = false;
        });
        items.forEach(function (item) {
            item.clipMask = on && item === top;
        });
        p.view.update();
        return commit();
    }
    function isMasked () {
        return selectedPaths().some(function (item) {
            return !!item.clipMask;
        });
    }
    function setMiter (limit, live?: any) {
        return eachSelected(function (item) {
            item.miterLimit = limit;
        }, live);
    }
    function miterOfSelection () {
        var items = selectedPaths();
        return items.length ? Number(items[0].miterLimit) || 10 : 10;
    }

    function artLayers (p, on) {
        var names = [];
        Array.prototype.forEach.call(p.project.layers, function (layer) {
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
    function artBox (p) {
        var art = artLayers(p, true);
        var box = null;
        art.forEach(function (layer) {
            Array.prototype.forEach.call(layer.children, function (item) {
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
    function exportPng (scale) {
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

    function gradientCss (p, stops, type) {
        var list = stops.map(function (s) {
            var c = new p.Color(s.c);
            return c.toCSS() + ' ' + Math.round(s.p * 1000) / 10 + '%';
        }).join(',');
        if (type === 'RADIAL') return 'radial-gradient(circle at 50% 50%,' + list + ')';
        return 'linear-gradient(' + (type === 'VERTICAL' ? '180deg' : '90deg') + ',' + list + ')';
    }

    function labelEl (text) {
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
    function hex6 (css) {
        var p = paper();
        try {
            var c = new p.Color(css);
            return '#' + [c.red, c.green, c.blue].map(function (v) {
                var n = Math.round(clamp(v, 0, 1) * 255);
                return ('0' + n.toString(16)).slice(-2);
            }).join('');
        } catch (e) {
            return '#8899aa';
        }
    }

    function orderOffsets (dragged) {
        var g = settings.grad;
        var rows = g.offsets.map(function (p, i) {
            return {p: p, i: i, extra: i > 0 && i < g.count - 1 ? i - 1 : -1};
        });
        rows.sort(function (a, b) { return a.p - b.p; });
        var extras = [];
        rows.forEach(function (row) {
            if (row.extra >= 0) extras.push(g.extras[row.extra]);
        });
        g.offsets = rows.map(function (row) { return row.p; });
        g.extras = extras;
        for (var i = 0; i < rows.length; i++) {
            if (rows[i].i === dragged) { gradPick = i; break; }
        }
    }

    function setStopCount (n) {
        var g = settings.grad;
        if (n === g.count || n < 2 || n > 4) return;
        var middles = [];
        for (var i = 1; i < g.count - 1; i++) middles.push({p: g.offsets[i], c: g.extras[i - 1]});
        if (n > g.count) {
            while (middles.length < n - 2) {
                var all = [g.offsets[0]].concat(middles.map(function (m) { return m.p; }),
                    [g.offsets[g.count - 1]]);
                var at = 0, gap = -1;
                for (i = 0; i < all.length - 1; i++) {
                    if (all[i + 1] - all[i] > gap) { gap = all[i + 1] - all[i]; at = i; }
                }
                middles.push({p: all[at] + gap / 2, c: '#8899aa'});
            }
            middles.sort(function (a, b) { return a.p - b.p; });
        }
        middles = middles.slice(0, n - 2);
        var first = g.offsets[0];
        var last = g.offsets[g.count - 1];
        g.count = n;
        g.offsets = [first].concat(middles.map(function (m) { return m.p; }), [last]);
        g.extras = middles.map(function (m) { return m.c; });
        gradPick = clamp(gradPick, 0, n - 1);
        saveSettings();
        buildThumbs();
        applyGradientToSelection();
        paintGradStrip();
    }

    function thumbDown (e) {
        e.preventDefault();
        e.stopPropagation();
        var g = settings.grad;
        var bar = gradStrip.bar;
        var thumb = e.currentTarget;
        var i = Number(thumb.dataset.i);
        var rect = bar.getBoundingClientRect();
        gradPick = i;
        paintGradStrip();
        var move = function (ev) {
            g.offsets[i] = clamp((ev.clientX - rect.left) / rect.width, 0, 1);
            thumb.style.left = (g.offsets[i] * 100) + '%';
            bar.style.background = gradBarCss();
        };
        var up = function () {
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
        for (var i = 0; i < g.count; i++) {
            var b = document.createElement('b');
            b.dataset.i = String(i);
            b.setAttribute('role', 'slider');
            b.setAttribute('aria-label', t('stops') + ' ' + (i + 1));
            listen(b, 'pointerdown', thumbDown);
            bar.appendChild(b);
        }
    }

    function paintGradStrip () {
        if (!gradStrip || !gradStrip.el.isConnected) return;
        var g = settings.grad;
        gradStrip.bar.style.background = gradBarCss();
        Array.prototype.forEach.call(gradStrip.bar.childNodes, function (b, i) {
            b.style.left = (g.offsets[i] * 100) + '%';
            b.classList.toggle('gwt-pick', i === gradPick);
        });
        Array.prototype.forEach.call(gradStrip.counts.children, function (b, i) {
            b.setAttribute('aria-pressed', String(g.count === i + 2));
        });
        var middle = gradPick > 0 && gradPick < g.count - 1;
        gradStrip.well.hidden = !middle;
        if (middle) gradStrip.well.value = hex6(g.extras[gradPick - 1]);
        gradStrip.apply.disabled = !selectedPaths().length;
    }

    function buildGradStrip (body) {
        var wrap = document.createElement('div');
        wrap.className = 'gwt-gwrap';

        var bar = document.createElement('div');
        bar.className = 'gwt-bar gwt-gbar';
        bar.setAttribute('aria-label', t('gradient'));
        wrap.appendChild(bar);

        var foot = document.createElement('div');
        foot.className = 'gwt-gfoot';
        var counts = document.createElement('div');
        counts.className = 'gwt-seg gwt-gcounts';
        [2, 3, 4].forEach(function (n) {
            var b = document.createElement('button');
            b.type = 'button';
            b.textContent = String(n);
            b.setAttribute('aria-label', t('stops') + ' ' + n);
            b.title = t('stops') + ' · ' + n;
            listen(b, 'click', function () { setStopCount(n); });
            counts.appendChild(b);
        });
        var well = document.createElement('input');
        well.type = 'color';
        well.className = 'gwt-gwell';
        well.setAttribute('aria-label', t('stops'));
        listen(well, 'input', function () {
            settings.grad.extras[gradPick - 1] = well.value;
            saveSettings();
            gradStrip.bar.style.background = gradBarCss();
        });
        listen(well, 'change', function () { applyGradientToSelection(); });
        var apply = document.createElement('button');
        apply.type = 'button';
        apply.className = 'gwt-gapply';
        apply.textContent = t('apply');
        listen(apply, 'click', function () {
            if (applyGradientToSelection()) paintGradStrip();
        });
        foot.appendChild(counts);
        foot.appendChild(well);
        foot.appendChild(apply);
        wrap.appendChild(foot);

        var row = body.querySelector('[class*="color-picker_gradient-picker-row"]');
        row.parentNode.insertBefore(wrap, row.nextSibling);
        gradStrip = {el: wrap, body: body, bar: bar, counts: counts, well: well, apply: apply};
        buildThumbs();
    }

    function syncGradStrip () {
        var body = pickerBody();
        if (!body) {
            gradStrip = null;
            return;
        }
        if (gradStrip && gradStrip.el.isConnected && gradStrip.body === body) {
            paintGradStrip();
            return;
        }
        gradStrip = null;
        buildGradStrip(body);
        applyTheme(gradStrip.el, readTheme());
        paintGradStrip();
    }

    function dropGradStrip () {
        if (gradStrip && gradStrip.el.isConnected) gradStrip.el.remove();
        gradStrip = null;
    }

    function watchColorTarget () {
        if (document.__gwtGrad) return;
        document.__gwtGrad = true;
        listen(document, 'pointerdown', function (e) {
            var t = e.target as Element;
            if (!t || !t.closest) return;
            if (t.closest('[class*="color-button_outline-swatch"]')) gradTarget = 'stroke';
            else if (t.closest('[class*="color-button_color-button-swatch"]')) gradTarget = 'fill';
            else return;
            gradPick = 0;
            later(syncGradStrip, 60);
        }, true);
    }

    function showFxPanel (anchor) {
        var panel = toggleFor(anchor, 'fx', 'gwt-fxbox');
        if (!panel) return;

        panel.appendChild(labelEl(t('mask')));
        var masks = document.createElement('div');
        masks.className = 'gwt-grid gwt-maskrow';
        [['on', t('mask')], ['off', t('maskOff')]].forEach(function (row) {
            var b = cell(row[1], demoMask(row[0] === 'on') + '<span>' + row[1] + '</span>');
            b.dataset.mask = String(row[0]);
            listen(b, 'click', function () {
                setMask(row[0] === 'on');
                markFx();
            });
            masks.appendChild(b);
        });
        panel.appendChild(masks);

        panel.appendChild(labelEl(t('blend')));
        var grid = document.createElement('div');
        grid.className = 'gwt-grid gwt-blends';
        BLENDS.forEach(function (mode) {
            var b = cell(mode, demoBlend(mode) + '<span>' + mode.replace(/-/g, ' ') + '</span>');
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
        row.innerHTML = '<span class="gwt-tag">' + t('joint') + '</span>' +
            '<i class="gwt-demo gwt-corner"><svg viewBox="0 0 30 24" aria-hidden="true">' +
            '<path d="M3 21 15 3 27 21"/></svg></i>';
        var corner = row.querySelector('path');
        var slider = document.createElement('input');
        slider.type = 'range';
        slider.min = 1;
        slider.max = 10;
        slider.step = 0.1;
        slider.value = miterOfSelection();
        slider.setAttribute('aria-label', t('joint'));
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

        panel.appendChild(labelEl(t('png')));
        var pngs = document.createElement('div');
        pngs.className = 'gwt-seg';
        pngs.style.marginTop = '.3rem';
        [1, 2, 4].forEach(function (scale) {
            var b = document.createElement('button');
            b.type = 'button';
            b.textContent = scale === 1 ? '1×' : scale + '×';
            b.dataset.scale = String(scale);
            b.title = t('png') + ' ' + scale + '×';
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

    function markFx () {
        if (!openPanel || openPanel._gwtKind !== 'fx') return;
        var some = selectedPaths().length > 0;
        var blend = blendOfSelection();
        var masked = isMasked();
        var maskable = canMask();
        var vector = !isBitmapFormat();
        Array.prototype.forEach.call(openPanel.querySelectorAll('[data-blend]'), function (b) {
            var on = some && b.dataset.blend === blend;
            b.classList.toggle('gwt-on', on);
            b.setAttribute('aria-pressed', String(on));
        });
        Array.prototype.forEach.call(openPanel.querySelectorAll('[data-mask]'), function (b) {
            var on = some && (b.dataset.mask === 'on') === masked;
            b.classList.toggle('gwt-on', on);
            b.setAttribute('aria-pressed', String(on));
        });
        var art = hasArt();
        Array.prototype.forEach.call(openPanel.querySelectorAll('[data-scale]'), function (b) {
            b.setAttribute('aria-pressed', String(Number(b.dataset.scale) === settings.pngScale));
            b.disabled = !art;
            b.title = art ? t('png') + ' ' + b.dataset.scale + '×' : t('empty');
        });
        var slider = openPanel.querySelector('.gwt-row input[type=range]');
        if (slider && document.activeElement !== slider) {
            slider.value = miterOfSelection();
            slider.nextSibling.textContent = Number(slider.value).toFixed(1);
            var corner = openPanel.querySelector('.gwt-corner path');
            if (corner) corner.setAttribute('stroke-miterlimit', slider.value);
        }
        Array.prototype.forEach.call(openPanel.querySelectorAll('[data-blend], [data-mask]'), function (b) {
            var ready = some && (b.dataset.mask !== 'on' || maskable);
            var why = !vector ? t('vectorOnly') : some ? t('maskAlone') : t('nothing');
            b.disabled = !ready;
            b.title = ready ? b.textContent : why;
        });
    }

    function refreshPanels () {
        if (openPanel && document.contains(openPanel._gwtAnchor)) fitPanel(openPanel);
        if (openPanel && openPanel._gwtKind === 'fx') later(markFx, 0);
        if (railEl) later(markObjects, 0);
    }

    function objectList () {
        var layer = paintingLayer(), out = [];
        if (!layer) return out;
        (function walk (node) {
            if (node.guide) return;                       // the editor's locked 1440x1080 guide shape
            if (node.className === 'CompoundPath') {
                out.push(node);
                return;
            }
            if (node !== layer && (!node.children || !node.children.length)) {
                out.push(node);
                return;
            }
            if (node.children) node.children.forEach(walk);
        })(layer);
        out.reverse();
        return out;
    }
    function kindName (item) {
        var cls = item.className;
        if (cls === 'Group') return t('group');
        if (cls === 'PointText' || cls === 'Text') return t('text');
        if (cls === 'Raster') return t('raster');
        if (cls === 'CompoundPath') return t('compound');
        return t('path');
    }
    function objectName (item, index, total) {
        return item.name || (kindName(item) + ' ' + (total - index));
    }
    function useSelectTool () {
        if (/^SELECT/.test((paintState() || {}).mode || '')) return;
        var native = document.querySelector('[class*="mode-selector"] [class*="mod-tool-select"]');
        if (native && !native.classList.contains('gwt-tool')) native.click();
    }
    function selectObject (item) {
        var p = paper();
        if (!p) return false;
        if (item.locked) return false;
        useSelectTool();
        objectList().forEach(function (it) { it.selected = false; });
        item.selected = true;
        p.view.update();
        return true;
    }
    function toggleSelectObject (item) {
        var p = paper();
        if (!p) return false;
        if (item.locked) return false;
        useSelectTool();
        item.selected = !item.selected;
        p.view.update();
        return true;
    }
    function reorderObjects (dir) {
        var p = paper();
        var items = selectedPaths();
        if (!p || !items.length) return false;
        var list = objectList();
        if (!list.length) return false;
        if (dir === 'front') {
            items.forEach(function (it) { it.bringToFront(); });
        } else if (dir === 'back') {
            var bottomItem = list[list.length - 1];
            if (bottomItem) {
                for (var bIdx = items.length - 1; bIdx >= 0; bIdx--) {
                    var it = items[bIdx];
                    if (it !== bottomItem) {
                        it.insertBelow(bottomItem);
                        bottomItem = it;
                    }
                }
            }
        } else if (dir === 'forward') {
            items.forEach(function (it) {
                var idx = list.indexOf(it);
                if (idx > 0) {
                    var above = list[idx - 1];
                    if (items.indexOf(above) < 0) it.insertAbove(above);
                }
            });
        } else if (dir === 'backward') {
            for (var i = items.length - 1; i >= 0; i--) {
                var it = items[i];
                var idx = list.indexOf(it);
                if (idx >= 0 && idx < list.length - 1) {
                    var below = list[idx + 1];
                    if (items.indexOf(below) < 0) it.insertBelow(below);
                }
            }
        }
        p.view.update();
        commit();
        markObjects();
        return true;
    }
    function toggleObject (item, flag) {
        item[flag] = !item[flag];
        var p = paper();
        if (p) p.view.update();
        return commit();
    }
    function duplicateObjects () {
        var p = paper();
        var items = selectedPaths();
        if (!p || !items.length) return false;
        items.forEach(function (it) { it.selected = false; });
        var copies = items.map(function (item) {
            var copy = item.clone(false);
            copy.insertAbove(item);
            return copy;
        });
        copies.forEach(function (it) { it.selected = true; });
        p.view.update();
        return commit();
    }
    function deleteObjects () {
        var items = selectedPaths();
        if (!items.length) return false;
        items.forEach(function (item) { item.remove(); });
        var p = paper();
        if (p) p.view.update();
        return commit();
    }
    function artboardRect (p) {
        return new p.Rectangle(0, 0, 960, 720);
    }
    function alignObjects (where) {
        var p = paper();
        var items = selectedPaths();
        if (!p || !items.length) return false;
        var box = artboardRect(p);
        var b = null;
        items.forEach(function (item) {
            var r = item.bounds;
            b = b ? b.unite(r) : r.clone();
        });
        if (!b) return false;
        var dx = 0, dy = 0;
        if (where === 'left') dx = box.x - b.x;
        if (where === 'hcenter' || where === 'center') dx = box.x + (box.width - b.width) / 2 - b.x;
        if (where === 'right') dx = box.x + box.width - (b.x + b.width);
        if (where === 'top') dy = box.y - b.y;
        if (where === 'vcenter' || where === 'center') dy = box.y + (box.height - b.height) / 2 - b.y;
        if (where === 'bottom') dy = box.y + box.height - (b.y + b.height);
        items.forEach(function (item) {
            item.position = item.position.add(new p.Point(dx, dy));
        });
        p.view.update();
        return commit();
    }
    function rotateObjects (deg) {
        return eachSelected(function (item) { item.rotate(deg); });
    }
    function setObjectOpacity (value, live?: any) {
        return eachSelected(function (item) {
            item.opacity = value / 100;
        }, live);
    }
    function opacityOfSelection () {
        var items = selectedPaths();
        if (!items.length) return 100;
        var value = items[0].opacity;
        return Math.round((value === undefined ? 1 : value) * 100);
    }

    var OBJ_GLYPH = {
        eye: '<path d="M2.6 10S5.3 5.5 10 5.5 17.4 10 17.4 10 14.7 14.5 10 14.5 2.6 10 2.6 10z"/>' +
            '<circle cx="10" cy="10" r="2.1"/>',
        eyeOff: '<path d="M3.6 3.6 16.4 16.4"/><path d="M7.2 7.1C4.4 8.3 2.6 10 2.6 10s2.7 4.5 7.4 4.5' +
            'c1 0 1.9-.2 2.7-.6"/><path d="M10 5.5c4.7 0 7.4 4.5 7.4 4.5s-.8 1.4-2.3 2.6"/>',
        lock: '<rect x="4.6" y="8.8" width="10.8" height="7.8" rx="1.4"/>' +
            '<path d="M7.2 8.8V6.7a2.8 2.8 0 0 1 5.6 0v2.1"/>',
        unlock: '<rect x="4.6" y="8.8" width="10.8" height="7.8" rx="1.4"/>' +
            '<path d="M7.2 8.8V6.7a2.8 2.8 0 0 1 5.2-1.2"/>',
        left: '<path d="M3.2 2.6v14.8"/><rect x="6.4" y="6.2" width="10" height="7.6" rx="1"/>',
        hcenter: '<path d="M10 2.6v14.8"/><rect x="4.8" y="6.2" width="10.4" height="7.6" rx="1"/>',
        right: '<path d="M16.8 2.6v14.8"/><rect x="3.6" y="6.2" width="10" height="7.6" rx="1"/>',
        top: '<path d="M2.6 3.2h14.8"/><rect x="6.2" y="6.4" width="7.6" height="10" rx="1"/>',
        vcenter: '<path d="M2.6 10h14.8"/><rect x="6.2" y="4.8" width="7.6" height="10.4" rx="1"/>',
        bottom: '<path d="M2.6 16.8h14.8"/><rect x="6.2" y="3.6" width="7.6" height="10" rx="1"/>',
        center: '<circle cx="10" cy="10" r="6.6"/><path d="M10 1.6v3.2M10 15.2v3.2M1.6 10h3.2M15.2 10h3.2"/>',
        rotL: '<path d="M4.8 10a5.2 5.2 0 1 1 5.2 5.2"/><path d="M4.8 6.4v3.6h3.6"/>',
        rotR: '<path d="M15.2 10A5.2 5.2 0 1 0 10 15.2"/><path d="M15.2 6.4v3.6h-3.6"/>',
        front: '<path d="M4 3.5h12M10 7.5v9M6.5 11l3.5-3.5 3.5 3.5"/>',
        forward: '<path d="M10 4.5v11M6.5 8l3.5-3.5 3.5 3.5"/>',
        backward: '<path d="M10 15.5v-11M6.5 12l3.5 3.5 3.5-3.5"/>',
        back: '<path d="M4 16.5h12M10 12.5v-9M6.5 9l3.5 3.5 3.5-3.5"/>',
        dup: '<rect x="3.4" y="3.4" width="8.8" height="8.8" rx="1.4"/>' +
            '<path d="M7.4 13.4v1.6a1.6 1.6 0 0 0 1.6 1.6h4.4a1.6 1.6 0 0 0 1.6-1.6v-4.4a1.6 1.6 0 0 0-1.6-1.6h-1.6"/>',
        del: '<path d="M3.4 5.8h13.2"/><path d="M6.6 5.8V4h6.8v1.8"/>' +
            '<path d="M5 5.8 5.9 16.4h8.2L15 5.8"/><path d="M8.4 8.6v5M11.6 8.6v5"/>',
        pin: '<path d="M7.4 3.4h5.2l-1 4.4 2.4 2.8H6l2.4-2.8z"/><path d="M10 10.6v6"/>'
    };
    function miniButton (inner, title) {
        var b = document.createElement('button');
        b.type = 'button';
        b.className = 'gwt-mini';
        b.title = title;
        b.setAttribute('aria-label', title);
        b.innerHTML = icon(inner);
        return b;
    }
    function objectRow (item, index, total) {
        var row = document.createElement('div');
        row.className = 'gwt-obj' + (item.selected ? ' gwt-on' : '') + (item.visible ? '' : ' gwt-dim');
        var eye = miniButton(item.visible ? OBJ_GLYPH.eye : OBJ_GLYPH.eyeOff,
            t(item.visible ? 'hide' : 'show'));
        listen(eye, 'click', function () {
            toggleObject(item, 'visible');
            markObjects();
        });
        var lock = miniButton(item.locked ? OBJ_GLYPH.lock : OBJ_GLYPH.unlock,
            t(item.locked ? 'unlock' : 'lock'));
        if (item.locked) lock.classList.add('gwt-strong');
        listen(lock, 'click', function () {
            toggleObject(item, 'locked');
            markObjects();
        });
        var name = document.createElement('button');
        name.type = 'button';
        name.className = 'gwt-objname';
        name.textContent = objectName(item, index, total);
        name.title = name.textContent;
        listen(name, 'click', function (e) {
            if (e.shiftKey || e.ctrlKey || e.metaKey) {
                toggleSelectObject(item);
            } else {
                selectObject(item);
            }
            markObjects();
        });
        listen(name, 'dblclick', function (e) {
            e.stopPropagation();
            var input = railEl && railEl.querySelector('.gwt-rname');
            if (input && !input.disabled) {
                input.focus();
                input.select();
            }
        });
        row.appendChild(objectThumb(item, 'gwt-thumb'));
        row.appendChild(name);
        row.appendChild(eye);
        row.appendChild(lock);
        return row;
    }
    function objectColor (item) {
        if (!item) return '';
        var color = item.fillColor || item.strokeColor;
        var css = '';
        if (color && color.gradient && color.gradient.stops) {
            try {
                css = 'linear-gradient(135deg, ' + color.gradient.stops.map(function (s) {
                    return s.color && s.color.toCSS ? s.color.toCSS() : '#855cd6';
                }).join(', ') + ')';
            } catch (e) { css = ''; }
        }
        if (!css) {
            try {
                css = color && color.toCSS ? color.toCSS() : '';
            } catch (e) { css = ''; }
        }
        return css;
    }
    function objectThumb (item, cls) {
        var el = document.createElement('i');
        el.className = cls;
        el.setAttribute('aria-hidden', 'true');
        var one = '';
        try { one = item.exportSVG({asString: true}); } catch (e) { one = ''; }
        if (one) {
            var b = item.bounds;
            var box = '<svg xmlns="http://www.w3.org/2000/svg" width="100%" height="100%"' +
                ' viewBox="' + [b.x, b.y, Math.max(b.width, 1), Math.max(b.height, 1)].join(' ') +
                '" preserveAspectRatio="xMidYMid meet">';
            one = /^<svg/i.test(one) ? one.replace(/^<svg[^>]*>/i, box) : box + one + '</svg>';
            el.style.backgroundImage = 'url("data:image/svg+xml;charset=utf-8,' +
                encodeURIComponent(one) + '")';
        } else {
            var css = objectColor(item);
            if (css) el.style.background = css; else el.classList.add('gwt-swatch-empty');
        }
        el.classList.toggle('gwt-off', !item.visible);
        return el;
    }
    function objectChip (item) {
        return objectThumb(item, 'gwt-chip');
    }

    var railEl = null;

    function railRect () {
        var canvas = document.querySelector('canvas[class*="paper-canvas"]');
        var host = document.querySelector('[class*="paint-editor_canvas-container"]');
        var r = (host && host.getBoundingClientRect()) || (canvas && canvas.getBoundingClientRect());
        return r && r.width > 40 && r.height > 40 ? r : null;
    }

    function railRow (children, cls?: any) {
        var row = document.createElement('div');
        row.className = cls || 'gwt-rtools';
        children.forEach(function (b) { row.appendChild(b); });
        return row;
    }

    function buildRail () {
        var rail = document.createElement('div');
        rail.className = 'gwt-rail';
        rail.setAttribute('dir', isRtl ? 'rtl' : 'ltr');
        rail.setAttribute('aria-label', t('objects'));

        var spine = document.createElement('div');
        spine.className = 'gwt-spine';
        spine.setAttribute('aria-hidden', 'true');
        rail.appendChild(spine);

        var head = document.createElement('div');
        head.className = 'gwt-rhead';
        var title = document.createElement('span');
        title.textContent = t('objects');
        var count = document.createElement('span');
        count.className = 'gwt-count';
        head.appendChild(title);
        head.appendChild(count);
        var pin = miniButton(OBJ_GLYPH.pin, t('pin'));
        pin.setAttribute('aria-pressed', 'false');
        listen(pin, 'click', function () {
            var on = rail.classList.toggle('gwt-pin');
            pin.setAttribute('aria-pressed', on ? 'true' : 'false');
        });
        head.appendChild(pin);
        rail.appendChild(head);

        var list = document.createElement('div');
        list.className = 'gwt-objs';
        rail.appendChild(list);

        var acts = [];
        ([['front', 'bringToFront', OBJ_GLYPH.front, function () { reorderObjects('front'); }],
            ['forward', 'bringForward', OBJ_GLYPH.forward, function () { reorderObjects('forward'); }],
            ['backward', 'sendBackward', OBJ_GLYPH.backward, function () { reorderObjects('backward'); }],
            ['back', 'sendToBack', OBJ_GLYPH.back, function () { reorderObjects('back'); }],
            ['dup', 'duplicate', OBJ_GLYPH.dup, duplicateObjects],
            ['del', 'Delete', OBJ_GLYPH.del, deleteObjects]] as const).forEach(function (row) {
            var b = miniButton(row[2], row[0] === 'del'
                ? msg('paint.paintEditor.delete', 'Delete') : t(row[1]));
            b.dataset.order = String(row[0]);
            listen(b, 'click', function () {
                row[3]();
                markObjects();
            });
            acts.push(b);
        });
        rail.appendChild(railRow(acts));

        var aligns = [];
        [['left', 'alignLeft', OBJ_GLYPH.left], ['hcenter', 'alignCenterH', OBJ_GLYPH.hcenter],
            ['right', 'alignRight', OBJ_GLYPH.right], ['top', 'alignTop', OBJ_GLYPH.top],
            ['vcenter', 'alignCenterV', OBJ_GLYPH.vcenter], ['bottom', 'alignBottom', OBJ_GLYPH.bottom],
            ['center', 'alignCenter', OBJ_GLYPH.center]].forEach(function (row) {
            var b = miniButton(row[2], t(row[1]));
            b.dataset.align = String(row[0]);
            listen(b, 'click', function () {
                alignObjects(row[0]);
                markObjects();
            });
            aligns.push(b);
        });
        rail.appendChild(railRow(aligns));

        var last = [];
        [[-90, 'rotateLeft', OBJ_GLYPH.rotL], [90, 'rotateRight', OBJ_GLYPH.rotR]].forEach(function (row) {
            var b = miniButton(row[2], t(row[1]));
            b.dataset.rotate = String(row[0]);
            listen(b, 'click', function () {
                rotateObjects(row[0]);
                markObjects();
            });
            last.push(b);
        });
        var nameInput = document.createElement('input');
        nameInput.type = 'text';
        nameInput.className = 'gwt-text gwt-rname';
        nameInput.setAttribute('aria-label', t('name'));
        listen(nameInput, 'change', function () {
            var items = selectedPaths();
            if (!items.length) return;
            var value = nameInput.value.replace(/\s+/g, ' ').trim();
            items.forEach(function (item) { item.name = value; });
            if (paper()) paper().view.update();
            commit();
            markObjects();
        });
        listen(nameInput, 'keydown', function (e) {
            if (e.key === 'Enter') {
                e.preventDefault();
                nameInput.blur();
            } else if (e.key === 'Escape') {
                e.preventDefault();
                var s = selectedPaths()[0];
                nameInput.value = s ? (s.name || '') : '';
                nameInput.blur();
            }
        });
        last.push(nameInput);
        rail.appendChild(railRow(last, 'gwt-rtools gwt-rspin'));

        var opRange = document.createElement('input');
        opRange.type = 'range';
        opRange.min = 0;
        opRange.max = 100;
        opRange.step = 1;
        opRange.setAttribute('aria-label', t('opacity'));
        var opOut = document.createElement('output');
        listen(opRange, 'input', function () {
            opOut.textContent = opRange.value + '%';
            if (selectedPaths().length) setObjectOpacity(Number(opRange.value), true);
        });
        listen(opRange, 'change', function () {
            if (selectedPaths().length) setObjectOpacity(Number(opRange.value));
        });
        rail.appendChild(railRow([opRange, opOut], 'gwt-rops'));

        document.body.appendChild(rail);
        return rail;
    }

    function destroyRail () {
        if (!railEl) return;
        railEl.remove();
        railEl = null;
    }

    function syncRail () {
        var r = paintEditorOpen() && paper() && !isBitmapFormat() ? railRect() : null;
        if (!r) {
            destroyRail();
            return;
        }
        if (!railEl) railEl = buildRail();
        applyTheme(railEl, readTheme());
        railEl.style.top = Math.round(r.top) + 'px';
        railEl.style.maxHeight = Math.round(r.height) + 'px';
        railEl.style.right = Math.round(window.innerWidth - r.right) + 'px';
        markObjects();
    }

    function markObjects () {
        if (!railEl || !railEl.isConnected) return;
        var items = objectList();
        var total = items.length;
        var list = railEl.querySelector('.gwt-objs');
        var spine = railEl.querySelector('.gwt-spine');
        if (list) {
            var sig = items.map(function (it) {
                var b = it.bounds;
                return it.id + '|' + (it.name || '') + '|' + (it.visible ? 1 : 0) + '|' +
                    (it.locked ? 1 : 0) + '|' + colorSig(it.fillColor) + '>' + colorSig(it.strokeColor) +
                    '|' + Math.round(b.x) + ' ' + Math.round(b.y) + ' ' +
                    Math.round(b.width) + ' ' + Math.round(b.height);
            }).join('/');
            if (list._gwtSig !== sig) {
                var prevScroll = list.scrollTop;
                list._gwtSig = sig;
                list.textContent = '';
                if (spine) spine.textContent = '';
                items.forEach(function (item, i) {
                    list.appendChild(objectRow(item, i, total));
                    if (spine) spine.appendChild(objectChip(item));
                });
                list.scrollTop = prevScroll;
            }
            Array.prototype.forEach.call(list.children, function (row, i) {
                var it = items[i];
                if (!it) return;
                row.classList.toggle('gwt-on', !!it.selected);
                row.classList.toggle('gwt-dim', !it.visible);
            });
            if (spine) Array.prototype.forEach.call(spine.children, function (chip, i) {
                chip.classList.toggle('gwt-on', !!(items[i] && items[i].selected));
            });
        }
        var count = railEl.querySelector('.gwt-count');
        if (count) count.textContent = String(total);
        var sel = selectedPaths()[0];
        var some = !!sel;
        var nameInput = railEl.querySelector('.gwt-rname');
        if (nameInput) {
            nameInput.disabled = !some;
            if (document.activeElement !== nameInput) nameInput.value = sel ? (sel.name || '') : '';
        }
        var opRange = railEl.querySelector('.gwt-rops input[type=range]');
        if (opRange) {
            opRange.disabled = !some;
            if (document.activeElement !== opRange) {
                opRange.value = opacityOfSelection();
                if (opRange.nextSibling) opRange.nextSibling.textContent = opRange.value + '%';
            }
        }
        Array.prototype.forEach.call(railEl.querySelectorAll('.gwt-rtools .gwt-mini'), function (b) {
            b.disabled = !some;
        });
    }

    function showShapePanel (anchor) {
        var panel = toggleFor(anchor, 'shape', 'gwt-wide');
        if (!panel) return;

        panel.appendChild(sliderRow(GLYPH.size, 40, 400, 4, 'shapeSize', refreshPreview));
        panel.appendChild(sliderRow(GLYPH.round, 0, 40, 1, 'shapeRound', refreshPreview));

        var grid = document.createElement('div');
        grid.className = 'gwt-grid';
        SHAPES.forEach(function (shape) {
            var b = cell(nameOf(shape), shapeIcon(shape));
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
        if (!openPanel) return;
        var cells = openPanel.querySelectorAll('.gwt-nib');
        if (!cells.length) return;
        var size = currentNibSize();
        var eraser = isEraserMode();
        for (var i = 0; i < cells.length; i++) {
            var on = Number(cells[i].dataset.size) === size;
            cells[i].classList.toggle('gwt-on', on);
            var mark = cells[i].firstChild;
            mark.className = eraser ? 'gwt-ring' : 'gwt-dot';
            var px = clamp(Number(cells[i].dataset.size) * (eraser ? 0.34 : 0.28), 4, 22);
            mark.style.width = px + 'px';
            mark.style.height = px + 'px';
        }
        var slider = openPanel.querySelector('.gwt-nib-slider');
        if (slider && document.activeElement !== slider) {
            slider.value = size;
            slider.nextSibling.textContent = size;
        }
    }

    function showNibPanel (anchor) {
        var panel = toggleFor(anchor, 'nib');
        if (!panel) return;

        var grid = document.createElement('div');
        grid.className = 'gwt-grid gwt-nibs';
        NIBS.forEach(function (nib) {
            var b = cell(nameOf(nib) + ' · ' + nib.size, '');
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
        input.setAttribute('aria-label', t('size'));
        var out = document.createElement('output');
        out.textContent = currentNibSize();
        listen(input, 'input', function () {
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
        profile: null,
        button: null,
        points: [],
        preview: null,

        activate: function (profile) {
            this.deactivate();
            shaper.disarm();
            if (!paper()) return;
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
            if (!this.active) return;
            this.active = false;
            this.endStroke();
            this.clearPreview();
            this.points = [];
            refreshToolLabels();
        },
        clearPreview: function () {
            if (this.preview) {
                this.preview.remove();
                this.preview = null;
            }
        },
        widthAt: function (t, pressure, angle) {
            var prof = this.profile;
            var base = settings.penSize;
            var pr = settings.penPressure ? clamp(pressure, 0.05, 1) : 0.6;
            var width = base * (prof.min + (1 - prof.min) * Math.pow(pr, prof.exponent));
            if (prof.chisel) {
                var nib = -Math.PI / 5;
                width *= 0.3 + 0.7 * Math.abs(Math.sin(angle - nib));
            }
            var taper = prof.taper * Math.sin(Math.PI * clamp(t, 0, 1));
            return Math.max(0.6, width * (1 - taper * 0.6));
        },
        toProject: function (e) {
            var p = paper();
            var canvas = document.querySelector('canvas[class*="paper-canvas"]');
            if (!p || !canvas) return null;
            var rect = canvas.getBoundingClientRect();
            return p.project.view.viewToProject(new p.Point(e.clientX - rect.left, e.clientY - rect.top));
        },
        onDown: function (e) {
            if (!pen.active) return;
            var canvas = e.target && (e.target as Element).closest ? (e.target as Element).closest('canvas[class*="paper-canvas"]') : null;
            if (!canvas) return;
            closePanel();
            e.stopPropagation();
            e.preventDefault();
            var point = pen.toProject(e);
            if (!point) return;
            pen.points = [{x: point.x, y: point.y, p: e.pressure || 0.5}];
            listen(document, 'pointermove', pen.onMove, true);
            listen(document, 'pointerup', pen.onUp, true);
            listen(document, 'pointercancel', pen.onUp, true);
        },
        onMove: function (e) {
            if (!pen.active || !pen.points.length) return;
            e.stopPropagation();
            var raw = pen.toProject(e);
            if (!raw) return;
            var last = pen.points[pen.points.length - 1];
            var k = 1 - pen.profile.smoothing;
            var x = last.x + (raw.x - last.x) * k;
            var y = last.y + (raw.y - last.y) * k;
            if (Math.hypot(x - last.x, y - last.y) < 1.2) return;
            pen.points.push({x: x, y: y, p: e.pressure || last.p});
            pen.drawPreview();
        },
        onUp: function (e) {
            if (e) e.stopPropagation();
            document.removeEventListener('pointermove', pen.onMove, true);
            document.removeEventListener('pointerup', pen.onUp, true);
            document.removeEventListener('pointercancel', pen.onUp, true);
            pen.endStroke();
        },
        outline: function () {
            var pts = this.points;
            if (pts.length < 2) return [];
            var left = [];
            var right = [];
            for (var i = 0; i < pts.length; i++) {
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
            if (!p || pts.length < 4) return;
            this.clearPreview();
            var layer = guideLayer();
            if (!layer) return;
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
            if (!p || pts.length < 4) {
                this.points = [];
                return;
            }
            var path = new p.Path(pts);
            path.closed = true;
            path.strokeCap = 'round';
            path.strokeJoin = 'round';
            applyStyle(path, false);
            if (!path.fillColor) {
                var cFall = currentColors();
                if (cFall.stroke) path.fillColor = cFall.stroke;
                else {
                    var sSt = store();
                    var pcol = (sSt && sSt.getState().scratchPaint && sSt.getState().scratchPaint.color) || {};
                    path.fillColor = colorOf(pcol.fillColor) || colorOf(pcol.strokeColor) || (p ? new p.Color('#855cd6') : null);
                }
            }
            if (!path.fillColor) {
                path.remove();
                this.points = [];
                return;
            }
            if (isBitmapFormat()) {
                bakeIntoRaster(path);
                this.points = [];
                return;
            }
            var layer = paintingLayer();
            if (!layer) {
                path.remove();
                this.points = [];
                return;
            }
            layer.activate();
            layer.addChild(path);
            path.selected = true;
            dispatch({type: 'scratch-paint/select/CHANGE_SELECTED_ITEMS', selectedItems: [path], bitmapMode: false});
            p.view.update();
            if (!commit()) path.remove();
            this.points = [];
        }
    };

    function showPenPanel (anchor) {
        var panel = toggleFor(anchor, 'pen');
        if (!panel) return;

        var grid = document.createElement('div');
        grid.className = 'gwt-grid gwt-pens';
        PENS.forEach(function (profile) {
            var b = cell(nameOf(profile), nibSwatch(profile));
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
        off.textContent = t('off');
        listen(off, 'click', function () {
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
        text.textContent = t('pressure');
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

        function markPens () {
            var buttons = grid.querySelectorAll<HTMLElement>('.gwt-pen');
            for (var i = 0; i < buttons.length; i++) {
                buttons[i].classList.toggle('gwt-on', pen.active && buttons[i].dataset.pen === settings.pen);
            }
        }
        markPens();
    }

    var PX_MAX_SIDE = 1024;
    var pxEd = null;
    var pxBtn = null;
    var pageToastEl = null;
    var pageToastTimer = 0;

    function pageToast (text, bad?: any) {
        if (!text) return;
        if (!pageToastEl) {
            pageToastEl = document.createElement('div');
            pageToastEl.className = 'gwt-toast';
            document.body.appendChild(pageToastEl);
        }
        pageToastEl.textContent = text;
        pageToastEl.classList.toggle('gwt-bad', !!bad);
        pageToastEl.classList.add('gwt-on');
        window.clearTimeout(pageToastTimer);
        pageToastTimer = later(function () {
            if (pageToastEl) pageToastEl.classList.remove('gwt-on');
        }, 2400);
    }

    function pxNativeButton () {
        var all = document.querySelectorAll('[class*="paint-editor_bitmap-button"]');
        for (var i = 0; i < all.length; i++) {
            var node = all[i], cls = String(node.className || '');
            if (node.classList.contains('gwt-px-btn') || /-icon/.test(cls)) continue;
            return node.tagName === 'BUTTON' ? node : (node.querySelector('button') || node);
        }
        return null;
    }

    function pxStrokeWidth (d, w, minX, minY, maxX, maxY) {
        var gaps = [];
        var strong = function (i, j) {
            return Math.max(Math.abs(d[i] - d[j]), Math.abs(d[i + 1] - d[j + 1]),
                Math.abs(d[i + 2] - d[j + 2])) >= 64 && d[i + 3] > 20 && d[j + 3] > 20;
        };
        for (var y = minY; y <= maxY; y++) {
            var last = -99;
            for (var x = minX; x < maxX; x++) {
                var i = (y * w + x) * 4;
                if (!strong(i, i + 4)) continue;
                var g = x - last;
                if (g >= 2 && g <= 32) gaps.push(g);
                last = x;
            }
        }
        for (var xx = minX; xx <= maxX; xx++) {
            var lastY = -99;
            for (var yy = minY; yy < maxY; yy++) {
                var j = (yy * w + xx) * 4;
                if (!strong(j, j + w * 4)) continue;
                var gy = yy - lastY;
                if (gy >= 2 && gy <= 32) gaps.push(gy);
                lastY = yy;
            }
        }
        if (gaps.length < 8) return 8;
        gaps.sort(function (a, b) { return a - b; });
        return gaps[Math.floor(gaps.length * 0.25)];
    }

    function snapBitmapPixels () {
        var t0 = anchorTarget();
        var raster = rasterItem();
        if (!t0 || !raster || !raster.canvas) return null;
        var cv = raster.canvas, w = cv.width, h = cv.height;
        if (!w || !h) return null;
        var ctx, img;
        try {
            ctx = cv.getContext('2d', {willReadFrequently: true});
            img = ctx.getImageData(0, 0, w, h);
        } catch (e) {
            return null;
        }
        var d = img.data;
        var minX = w, minY = h, maxX = -1, maxY = -1;
        var freq = {};
        for (var y = 0; y < h; y++) {
            for (var x = 0; x < w; x++) {
                var i0 = (y * w + x) * 4;
                if (!d[i0 + 3]) continue;
                if (x < minX) minX = x;
                if (x > maxX) maxX = x;
                if (y < minY) minY = y;
                if (y > maxY) maxY = y;
                var fk = (d[i0] << 16) | (d[i0 + 1] << 8) | d[i0 + 2];
                freq[fk] = (freq[fk] || 0) + 1;
            }
        }
        if (maxX < 0) return null;

        var stroke = pxStrokeWidth(d, w, minX, minY, maxX, maxY);
        var res = stroke >= 5 ? 2 : 1;
        var ratio = res === 1 ? '1:1' : '2x2';
        var changed = 0;

        if (res === 1) {
            changed = pxFixAlpha(img);
            changed += pxHardColors(img, freq);
        } else {
            minX -= minX % res;
            minY -= minY % res;
            maxX = Math.min(w - 1, maxX + (res - 1 - (maxX % res)));
            maxY = Math.min(h - 1, maxY + (res - 1 - (maxY % res)));
            for (var by = minY; by <= maxY; by += res) {
                for (var bx = minX; bx <= maxX; bx += res) {
                    var buckets = {}, bucketList = [];
                    for (var yy = by; yy < Math.min(by + res, h); yy++) {
                        for (var xx = bx; xx < Math.min(bx + res, w); xx++) {
                            var bi = (yy * w + xx) * 4;
                            if (!d[bi + 3]) continue;
                            var bk = ((d[bi] >> 3) << 10) | ((d[bi + 1] >> 3) << 5) | (d[bi + 2] >> 3);
                            var bucket = buckets[bk];
                            if (!bucket) {
                                bucket = buckets[bk] = {n: 0, i: bi, a: 0};
                                bucketList.push(bucket);
                            }
                            bucket.n++;
                            if (d[bi + 3] > bucket.a) { bucket.a = d[bi + 3]; bucket.i = bi; }
                        }
                    }
                    bucketList.sort(function (a, b) { return b.n - a.n; });
                    var best = bucketList.length ? bucketList[0].i : -1;
                    var bestA = bucketList.length ? bucketList[0].a : -1;
                    if (bucketList.length > 1 && best >= 0) {
                        var second = bucketList[1], total = 0;
                        bucketList.forEach(function (b) { total += b.n; });
                        var lum = function (i) { return d[i] * .299 + d[i + 1] * .587 + d[i + 2] * .114; };
                        var contrast = Math.max(Math.abs(d[best] - d[second.i]),
                            Math.abs(d[best + 1] - d[second.i + 1]), Math.abs(d[best + 2] - d[second.i + 2]));
                        if (second.n <= total * .55 && contrast >= 96 && lum(second.i) + 16 < lum(best)) {
                            best = second.i;
                        }
                    }
                    var solid = bestA > 20;
                    var r = solid ? d[best] : 0, g = solid ? d[best + 1] : 0, b = solid ? d[best + 2] : 0;
                    var alpha = solid ? 255 : 0;
                    for (yy = by; yy < Math.min(by + res, h); yy++) {
                        for (xx = bx; xx < Math.min(bx + res, w); xx++) {
                            var j = (yy * w + xx) * 4;
                            if (d[j] === r && d[j + 1] === g && d[j + 2] === b && d[j + 3] === alpha) continue;
                            d[j] = r; d[j + 1] = g; d[j + 2] = b; d[j + 3] = alpha;
                            changed++;
                        }
                    }
                }
            }
        }
        if (changed) {
            ctx.putImageData(img, 0, 0);
            commit();
        }
        return {changed: changed, ratio: ratio};
    }

    function waitForBitmap (tries) {
        var raster = rasterItem();
        if (!isBitmapFormat() || !raster || !raster.canvas || !raster.canvas.width) {
            if (tries < 80) later(function () { waitForBitmap(tries + 1); }, 100);
            return;
        }
        later(function () {
            if (!isBitmapFormat()) return;
            var snap = snapBitmapPixels();
            if (snap && snap.changed) pageToast(t('bitmapSnapped').replace('{ratio}', snap.ratio));
        }, 160);
    }

    function hookConversion (native) {
        if (!native || native._gwtSnap) return;
        native._gwtSnap = true;
        listen(native, 'click', function () {
            if (isBitmapFormat()) return;
            later(function () { waitForBitmap(0); }, 120);
        });
    }

    function syncPixelButton () {
        var native = pxNativeButton();
        if (!paintEditorOpen() || !native) {
            if (pxBtn) { pxBtn.remove(); pxBtn = null; }
            return;
        }
        if (!pxBtn || !pxBtn.isConnected) {
            pxBtn = native.cloneNode(true);
            pxBtn.classList.add('gwt-px-btn');
            pxBtn.removeAttribute('data-testid');
            pxBtn.removeAttribute('aria-pressed');
            if (!pxBtn.getAttribute('tabindex')) pxBtn.setAttribute('tabindex', '0');
            var img = pxBtn.querySelector('img');
            if (img) img.src = PX_BUTTON_ICON;
            var span = pxBtn.querySelector('span');
            if (!span) {
                span = document.createElement('span');
                pxBtn.appendChild(span);
            }
            listen(pxBtn, 'click', openPixelEditor);
            listen(pxBtn, 'keydown', function (e) {
                if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    openPixelEditor();
                }
            });
            native.parentElement.insertBefore(pxBtn, native);
        }
        hookConversion(native);
        var label = pxBtn.querySelector('span');
        if (label) label.textContent = t('pixelOpen');
        pxBtn.title = t('pixelArt');
        pxBtn.setAttribute('aria-label', t('pixelArt'));
        pxBtn.dataset.gwtPixel = String('1');
    }

    function ensureVectorCostume () {
        if (!isBitmapFormat()) return Promise.resolve(true);
        var native = pxNativeButton();
        if (!native) return Promise.resolve(false);
        (native as HTMLElement).click();
        var tries = 0;
        return new Promise(function (res) {
            var iv = repeat(function () {
                tries++;
                if (!isBitmapFormat()) {
                    window.clearInterval(iv);
                    res(true);
                } else if (tries > 60) {
                    window.clearInterval(iv);
                    res(false);
                }
            }, 100);
        });
    }

    function rasterizeCostume () {
        var target = anchorTarget();
        if (!target) return Promise.resolve(null);
        var costume = target.getCostumes()[target.currentCostume];
        if (!costume) return Promise.resolve(null);
        var raw = costume.asset && (costume.asset.data !== undefined ? costume.asset.data : costume.asset);
        var txt = typeof raw === 'string' ? raw : (raw ? new TextDecoder().decode(raw) : '');
        if (!txt) return Promise.resolve(null);
        var w = Number((/\swidth="([\d.]+)"/.exec(txt) || [])[1]) || 0;
        var h = Number((/\sheight="([\d.]+)"/.exec(txt) || [])[1]) || 0;
        var s = Math.min(1, PX_MAX_SIDE / Math.max(w, h, 1));
        return new Promise(function (res) {
            var img = new Image();
            img.onload = function () {
                var W = Math.max(1, Math.round((w || img.naturalWidth) * s));
                var H = Math.max(1, Math.round((h || img.naturalHeight) * s));
                var cv = document.createElement('canvas');
                cv.width = W;
                cv.height = H;
                var ctx = cv.getContext('2d', {willReadFrequently: true});
                ctx.clearRect(0, 0, W, H);
                ctx.imageSmoothingEnabled = s < 1;
                ctx.drawImage(img, 0, 0, W, H);
                res({canvas: cv, ctx: ctx, W: W, H: H, s: s, target: target, costume: costume,
                    costumeW: w || (costume.size && costume.size[0]) || W,
                    costumeH: h || (costume.size && costume.size[1]) || H});
            };
            img.onerror = function () { res(null); };
            img.src = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(txt);
        });
    }

    function pxBounds (data, w, h) {
        var minX = w, minY = h, maxX = -1, maxY = -1;
        for (var y = 0; y < h; y++) {
            for (var x = 0; x < w; x++) {
                if (!data[(y * w + x) * 4 + 3]) continue;
                if (x < minX) minX = x;
                if (x > maxX) maxX = x;
                if (y < minY) minY = y;
                if (y > maxY) maxY = y;
            }
        }
        return maxX < 0 ? null : {x: minX, y: minY, w: maxX - minX + 1, h: maxY - minY + 1};
    }

    function pxFixAlpha (img) {
        var d = img.data, w = img.width, h = img.height;
        var before = new Uint8Array(w * h);
        var semi = [];
        var p, i;
        for (p = 0; p < w * h; p++) before[p] = d[p * 4 + 3];
        for (p = 0; p < w * h; p++) {
            var a = before[p];
            if (!a) continue;
            if (a <= 20) {
                d[p * 4 + 3] = 0;
            } else {
                d[p * 4 + 3] = 255;
                if (a < 235) semi.push(p);
            }
        }
        for (var y = 0; y < h; y++) {
            for (var x = 0; x < w; x++) {
                var k = y * w + x;
                var pa = before[k];
                if (!pa || pa >= 200) continue;
                var empty = 0;
                if (x === 0 || !before[k - 1]) empty++;
                if (x === w - 1 || !before[k + 1]) empty++;
                if (y === 0 || !before[k - w]) empty++;
                if (y === h - 1 || !before[k + w]) empty++;
                if (empty >= 2) d[k * 4 + 3] = 0;
            }
        }
        var R1 = [-1, 0, 1, -1, 1, -1, 0, 1];
        var C1 = [-1, -1, -1, 0, 0, 1, 1, 1];
        for (i = 0; i < semi.length; i++) {
            p = semi[i];
            if (!d[p * 4 + 3]) continue;
            var sx = p % w, sy = (p - sx) / w;
            var found = -1;
            for (var radius = 1; radius <= 2 && found < 0; radius++) {
                for (var q = 0; q < 8; q++) {
                    var nx = sx + R1[q] * radius, ny = sy + C1[q] * radius;
                    if (nx < 0 || ny < 0 || nx >= w || ny >= h) continue;
                    var nk = ny * w + nx;
                    if (before[nk] >= 235 && d[nk * 4 + 3]) { found = nk; break; }
                }
            }
            if (found >= 0) {
                d[p * 4] = d[found * 4];
                d[p * 4 + 1] = d[found * 4 + 1];
                d[p * 4 + 2] = d[found * 4 + 2];
            }
        }
        var changed = 0;
        for (p = 0; p < w * h; p++) {
            if (before[p] !== d[p * 4 + 3]) changed++;
        }
        return changed;
    }

    function pxNearColor (d, j, r, g, b) {
        return d[j + 3] === 255 && Math.abs(d[j] - r) <= 12 &&
            Math.abs(d[j + 1] - g) <= 12 && Math.abs(d[j + 2] - b) <= 12;
    }

    function pxHardColors (img, freq) {
        var d = img.data, w = img.width, h = img.height, n = w * h;
        var flat = {}, x, y, i, key;
        for (y = 1; y < h - 1; y++) {
            for (x = 1; x < w - 1; x++) {
                i = (y * w + x) * 4;
                if (d[i + 3] !== 255) continue;
                var r = d[i], g = d[i + 1], b = d[i + 2];
                if (!pxNearColor(d, i - 4, r, g, b) || !pxNearColor(d, i + 4, r, g, b) ||
                    !pxNearColor(d, i - w * 4, r, g, b) || !pxNearColor(d, i + w * 4, r, g, b)) continue;
                key = (r << 16) | (g << 8) | b;
                flat[key] = (flat[key] || 0) + 1;
            }
        }
        var list = [];
        for (key in flat) {
            if (flat[key] >= 12 && freq[key]) list.push({c: Number(key), n: freq[key]});
        }
        if (list.length < 2) {
            for (key in freq) if (freq[key] >= 12) list.push({c: Number(key), n: freq[key]});
        }
        if (list.length < 2) return 0;
        list.sort(function (a, b) { return b.n - a.n; });
        list = list.slice(0, 24);
        var pal = [], q;
        for (q = 0; q < list.length; q++) {
            pal.push({r: (list[q].c >> 16) & 255, g: (list[q].c >> 8) & 255, b: list[q].c & 255});
        }
        var changed = 0, cache = {};
        for (var p = 0; p < n; p++) {
            i = p * 4;
            if (!d[i + 3]) continue;
            r = d[i]; g = d[i + 1]; b = d[i + 2];
            key = (r << 16) | (g << 8) | b;
            var choice = cache[key];
            if (choice === undefined) {
                var best = -1, second = -1, d1 = Infinity, d2 = Infinity;
                for (q = 0; q < pal.length; q++) {
                    var P = pal[q];
                    var dd = (r - P.r) * (r - P.r) + (g - P.g) * (g - P.g) + (b - P.b) * (b - P.b);
                    if (dd < d1) { d2 = d1; second = best; d1 = dd; best = q; }
                    else if (dd < d2) { d2 = dd; second = q; }
                }
                var pick = best;
                if (best === 0 && second > 0) {
                    var A = pal[0], B = pal[second];
                    var ux = B.r - A.r, uy = B.g - A.g, uz = B.b - A.b;
                    var len = ux * ux + uy * uy + uz * uz;
                    if (len > 900) {
                        var tt = ((r - A.r) * ux + (g - A.g) * uy + (b - A.b) * uz) / len;
                        var ox = r - (A.r + tt * ux), oy = g - (A.g + tt * uy), oz = b - (A.b + tt * uz);
                        if (tt >= 0.35 && ox * ox + oy * oy + oz * oz < len * 0.05) pick = second;
                    }
                }
                choice = (best < 0 || d1 === 0) ? -1 : pick;
                cache[key] = choice;
            }
            if (choice < 0) continue;
            var C = pal[choice];
            if (r !== C.r || g !== C.g || b !== C.b) {
                d[i] = C.r; d[i + 1] = C.g; d[i + 2] = C.b; d[i + 3] = 255;
                changed++;
            }
        }
        return changed;
    }

    function pxHasArt (img) {
        var d = img.data;
        for (var i = 3; i < d.length; i += 4) if (d[i]) return true;
        return false;
    }

    function pxPalette (img, count) {
        var d = img.data, hits = {}, out = [];
        for (var i = 0; i < d.length; i += 40) {
            if (d[i + 3] < 64) continue;
            var key = (d[i] >> 4) + '.' + (d[i + 1] >> 4) + '.' + (d[i + 2] >> 4);
            var hit = hits[key] || (hits[key] = {n: 0, r: d[i], g: d[i + 1], b: d[i + 2]});
            hit.n++;
        }
        Object.keys(hits).sort(function (a, b) { return hits[b].n - hits[a].n; }).slice(0, count)
            .forEach(function (k) {
                var h = hits[k];
                var hex = '#' + [h.r, h.g, h.b].map(function (v) {
                    var s = v.toString(16);
                    return s.length < 2 ? '0' + s : s;
                }).join('');
                out.push(hex);
            });
        return out;
    }

    var PX_BUTTON_ICON = 'data:image/svg+xml;base64,' + window.btoa(
        '<svg xmlns="http://www.w3.org/2000/svg" width="20px" height="20px" viewBox="0 0 20 20">' +
        '<g fill="none" stroke="#FFFFFF" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">' +
        '<path d="M3 7V3h4M13 3h4v4M17 13v4h-4M7 17H3v-4"/>' +
        '<rect x="7.3" y="7.3" width="5.4" height="5.4" rx=".4"/></g></svg>');

    var PX_ICONS = {
        t_brush: icon('<path d="M4.5 15.6c1.4-4.7 5.4-9.1 11.1-10.1-1.3 5.7-5.7 9.5-11.1 10.1z"/>' +
            '<path d="M4.5 15.6l2.5-2.5"/>'),
        t_pencil: icon('<path d="M4 16l1.7-4.5L13 4.2l2.8 2.8-7.3 7.3z"/><path d="M11.6 5.6l2.8 2.8"/>'),
        t_eraser: icon('<path d="M4.2 13.6l6.1-6.1 4.1 4.1-6.1 6.1H5.6z"/><path d="M9.4 16.1h6.2"/>'),
        t_fill: icon('<path d="M8.4 3.6l6.4 6.4-5.1 5.1a1.9 1.9 0 0 1-2.7 0L3.1 11.2z"/>' +
            '<path d="M4.4 10h9.4"/><path d="M16.2 12.9c.8 1.2 1.2 2 1.2 2.6a1.2 1.2 0 1 1-2.4 0c0-.6.4-1.4 1.2-2.6z"/>'),
        t_line: icon('<path d="M4 16L16 4"/>'),
        t_rect: icon('<rect x="4" y="5.5" width="12" height="9" rx=".6"/>'),
        t_ellipse: icon('<ellipse cx="10" cy="10" rx="6.2" ry="4.6"/>'),
        t_picker: icon('<path d="M13.4 3.4l3.2 3.2-8.8 8.8-3.8.7.7-3.8z"/><path d="M11.8 5l3.2 3.2"/>'),
        t_move: icon('<path d="M10 3v14M3 10h14M7.2 5.8L10 3l2.8 2.8M7.2 14.2L10 17l2.8-2.8"/>' +
            '<path d="M5.8 7.2L3 10l2.8 2.8M14.2 7.2L17 10l-2.8 2.8"/>')
    };
    var PX_TOOLS = [
        ['brush', 't_brush', 'brush'], ['pencil', 't_pencil', 'pencil'], ['eraser', 't_eraser', 'eraser'],
        ['fill', 't_fill', 'fill'], ['line', 't_line', 'line'], ['rect', 't_rect', 'rect'],
        ['ellipse', 't_ellipse', 'ellipse'], ['picker', 't_picker', 'picker'], ['move', 't_move', 'move']
    ];

    function pxSnapshot (ed) {
        return new ImageData(new Uint8ClampedArray(ed.grid.data), ed.w, ed.h);
    }
    function pxPush (ed) {
        ed.undo.push(pxSnapshot(ed));
        if (ed.undo.length > 40) ed.undo.shift();
        ed.redo = [];
        pxUpdateButtons(ed);
    }
    function pxUpdateButtons (ed) {
        if (ed.btnUndo) ed.btnUndo.disabled = !ed.undo.length;
        if (ed.btnRedo) ed.btnRedo.disabled = !ed.redo.length;
    }
    function pxGet (ed, x, y) {
        if (x < 0 || y < 0 || x >= ed.w || y >= ed.h) return null;
        var i = (y * ed.w + x) * 4, d = ed.grid.data;
        return [d[i], d[i + 1], d[i + 2], d[i + 3]];
    }
    function pxSet (ed, x, y, c) {
        if (x < 0 || y < 0 || x >= ed.w || y >= ed.h) return;
        var i = (y * ed.w + x) * 4, d = ed.grid.data;
        d[i] = c[0]; d[i + 1] = c[1]; d[i + 2] = c[2]; d[i + 3] = c[3];
    }
    function pxSame (a, b) {
        return a && b && a[0] === b[0] && a[1] === b[1] && a[2] === b[2] && a[3] === b[3];
    }
    function pxStamp (ed, x, y, size, c) {
        var half = Math.floor((size - 1) / 2);
        for (var dy = 0; dy < size; dy++) {
            for (var dx = 0; dx < size; dx++) pxSet(ed, x - half + dx, y - half + dy, c);
        }
    }
    function pxLine (ed, x0, y0, x1, y1, c) {
        var dx = Math.abs(x1 - x0), sx = x0 < x1 ? 1 : -1;
        var dy = -Math.abs(y1 - y0), sy = y0 < y1 ? 1 : -1;
        var err = dx + dy;
        for (;;) {
            pxStamp(ed, x0, y0, ed.size, c);
            if (x0 === x1 && y0 === y1) break;
            var e2 = 2 * err;
            if (e2 >= dy) { err += dy; x0 += sx; }
            if (e2 <= dx) { err += dx; y0 += sy; }
        }
    }
    function pxFlood (ed, x, y, c) {
        var target = pxGet(ed, x, y);
        if (!target || pxSame(target, c)) return;
        var stack = [[x, y]], seen = new Uint8Array(ed.w * ed.h);
        while (stack.length) {
            var point = stack.pop(), px = point[0], py = point[1];
            if (px < 0 || py < 0 || px >= ed.w || py >= ed.h) continue;
            var k = py * ed.w + px;
            if (seen[k]) continue;
            if (!pxSame(pxGet(ed, px, py), target)) continue;
            seen[k] = 1;
            pxSet(ed, px, py, c);
            stack.push([px + 1, py], [px - 1, py], [px, py + 1], [px, py - 1]);
        }
    }
    function pxEllipse (ed, x0, y0, x1, y1, c, filled) {
        var left = Math.min(x0, x1), right = Math.max(x0, x1);
        var top = Math.min(y0, y1), bottom = Math.max(y0, y1);
        var cx = (left + right) / 2, cy = (top + bottom) / 2;
        var rx = Math.max(0.5, (right - left + 1) / 2), ry = Math.max(0.5, (bottom - top + 1) / 2);
        for (var y = top; y <= bottom; y++) {
            var dy = (y - cy) / ry;
            var span = Math.sqrt(Math.max(0, 1 - dy * dy));
            var xa = Math.round(cx - span * rx), xb = Math.round(cx + span * rx);
            if (filled) {
                for (var x = xa; x <= xb; x++) pxSet(ed, x, y, c);
            } else {
                pxSet(ed, xa, y, c);
                pxSet(ed, xb, y, c);
            }
        }
        if (!filled) {
            for (var ix = left; ix <= right; ix++) {
                var dx = (ix - cx) / rx;
                var sp = Math.sqrt(Math.max(0, 1 - dx * dx));
                pxSet(ed, ix, Math.round(cy - sp * ry), c);
                pxSet(ed, ix, Math.round(cy + sp * ry), c);
            }
        }
    }
    function pxBox (ed, x0, y0, x1, y1, c, filled) {
        var left = Math.min(x0, x1), right = Math.max(x0, x1);
        var top = Math.min(y0, y1), bottom = Math.max(y0, y1);
        for (var y = top; y <= bottom; y++) {
            for (var x = left; x <= right; x++) {
                if (filled || x === left || x === right || y === top || y === bottom) pxSet(ed, x, y, c);
            }
        }
    }

    function pxRender (ed) {
        if (!ed || !ed.root.isConnected) return;
        var zoom = ed.zoom, w = ed.w, h = ed.h;
        var cv = ed.canvas;
        cv.width = Math.max(1, w * zoom);
        cv.height = Math.max(1, h * zoom);
        var ctx = cv.getContext('2d');
        ctx.imageSmoothingEnabled = false;
        ed.tmp.width = w;
        ed.tmp.height = h;
        ed.tmp.getContext('2d').putImageData(ed.grid, 0, 0);
        ctx.clearRect(0, 0, cv.width, cv.height);
        ctx.drawImage(ed.tmp, 0, 0, cv.width, cv.height);
        if (ed.gridOn && zoom >= 4) {
            ctx.beginPath();
            ctx.strokeStyle = ed.dark ? 'rgba(255,255,255,.12)' : 'rgba(0,0,0,.08)';
            ctx.lineWidth = 1;
            for (var x = 0; x <= w; x++) {
                var vx = x * zoom + 0.5;
                ctx.moveTo(vx, 0);
                ctx.lineTo(vx, h * zoom);
            }
            for (var y = 0; y <= h; y++) {
                var vy = y * zoom + 0.5;
                ctx.moveTo(0, vy);
                ctx.lineTo(w * zoom, vy);
            }
            ctx.stroke();
        }
        if (ed.zoomOut) ed.zoomOut.textContent = zoom + '×';
    }

    function pxFit (ed) {
        var stage = ed.stage.getBoundingClientRect();
        var fit = Math.floor(Math.min((stage.width - 30) / ed.w, (stage.height - 30) / ed.h));
        ed.zoom = Math.max(1, Math.min(32, fit || 1));
        pxRender(ed);
    }
    function pxZoom (ed, dir) {
        var steps = [1, 2, 3, 4, 6, 8, 10, 12, 16, 20, 24, 32];
        var i = 0;
        while (i < steps.length - 1 && steps[i] < ed.zoom) i++;
        if (dir > 0) i = Math.min(steps.length - 1, i + 1);
        else i = Math.max(0, i - 1);
        ed.zoom = steps[i];
        pxRender(ed);
    }

    function pxPoint (ed, e) {
        var r = ed.canvas.getBoundingClientRect();
        var x = Math.floor((e.clientX - r.left) / ed.zoom);
        var y = Math.floor((e.clientY - r.top) / ed.zoom);
        if (x < 0 || y < 0 || x >= ed.w || y >= ed.h) return null;
        return {x: x, y: y};
    }

    function pxToolColor (ed, tool) {
        if (tool === 'eraser') return [0, 0, 0, 0];
        return ed.rgba;
    }

    function pxDown (ed, e) {
        if (e.button !== 0 || ed.busy) return;
        var p = pxPoint(ed, e);
        if (!p) return;
        e.preventDefault();
        ed.canvas.setPointerCapture(e.pointerId);
        var tool = ed.tool;
        if (tool === 'picker') {
            var c = pxGet(ed, p.x, p.y);
            if (c && c[3]) {
                ed.color = '#' + [c[0], c[1], c[2]].map(function (v) {
                    var s = v.toString(16);
                    return s.length < 2 ? '0' + s : s;
                }).join('');
                ed.colorInput.value = ed.color;
                ed.rgba = [c[0], c[1], c[2], 255];
                ed.note.textContent = ed.color.toUpperCase();
            }
            return;
        }
        pxPush(ed);
        ed.gesture = {tool: tool, start: p, last: p, base: pxSnapshot(ed)};
        if (tool === 'move') {
            ed.gesture.box = pxBounds(ed.grid.data, ed.w, ed.h);
            ed.note.textContent = t('pixelHelp');
            return;
        }
        var color = pxToolColor(ed, tool);
        if (tool === 'brush' || tool === 'pencil' || tool === 'eraser') {
            pxLine(ed, p.x, p.y, p.x, p.y, color);
        } else if (tool === 'fill') {
            pxFlood(ed, p.x, p.y, color);
        }
        pxRender(ed);
    }

    function pxMove (ed, e) {
        if (!ed.gesture) return;
        var p = pxPoint(ed, e);
        if (!p) return;
        var g = ed.gesture;
        var tool = g.tool;
        if (tool === 'move') {
            if (!g.box) return;
            var dx = p.x - g.start.x, dy = p.y - g.start.y;
            dx = clamp(dx, -g.box.x, ed.w - g.box.x - g.box.w);
            dy = clamp(dy, -g.box.y, ed.h - g.box.y - g.box.h);
            var out = new Uint8ClampedArray(ed.w * ed.h * 4);
            var src = g.base.data;
            for (var y = 0; y < ed.h; y++) {
                for (var x = 0; x < ed.w; x++) {
                    var i = (y * ed.w + x) * 4;
                    if (!src[i + 3]) continue;
                    var tx = x + dx, ty = y + dy;
                    if (tx < 0 || ty < 0 || tx >= ed.w || ty >= ed.h) continue;
                    var j = (ty * ed.w + tx) * 4;
                    out[j] = src[i]; out[j + 1] = src[i + 1]; out[j + 2] = src[i + 2]; out[j + 3] = src[i + 3];
                }
            }
            ed.grid.data.set(out);
            pxRender(ed);
            return;
        }
        var color = pxToolColor(ed, tool);
        if (tool === 'brush' || tool === 'pencil' || tool === 'eraser') {
            pxLine(ed, g.last.x, g.last.y, p.x, p.y, color);
            g.last = p;
        } else if (tool === 'line' || tool === 'rect' || tool === 'ellipse') {
            ed.grid.data.set(g.base.data);
            if (tool === 'line') pxLine(ed, g.start.x, g.start.y, p.x, p.y, color);
            else if (tool === 'rect') pxBox(ed, g.start.x, g.start.y, p.x, p.y, color, ed.filled);
            else pxEllipse(ed, g.start.x, g.start.y, p.x, p.y, color, ed.filled);
            g.last = p;
        }
        pxRender(ed);
    }

    function pxUp (ed) {
        if (!ed.gesture) return;
        var g = ed.gesture;
        ed.gesture = null;
        if (g.tool === 'move') return;
        if (g.tool === 'line' || g.tool === 'rect' || g.tool === 'ellipse') {
            pxRender(ed);
        }
        pxUpdateButtons(ed);
        ed.note.textContent = g.tool === 'brush' || g.tool === 'pencil' ? t('pixelHelp') : ed.note.textContent;
    }

    function pxUndo (ed) {
        if (!ed.undo.length) return;
        ed.redo.push(pxSnapshot(ed));
        ed.grid = ed.undo.pop();
        pxRender(ed);
        pxUpdateButtons(ed);
    }
    function pxRedo (ed) {
        if (!ed.redo.length) return;
        ed.undo.push(pxSnapshot(ed));
        ed.grid = ed.redo.pop();
        pxRender(ed);
        pxUpdateButtons(ed);
    }
    function pxNudge (ed, dx, dy) {
        var box = pxBounds(ed.grid.data, ed.w, ed.h);
        if (!box) return;
        dx = clamp(dx, -box.x, ed.w - box.x - box.w);
        dy = clamp(dy, -box.y, ed.h - box.y - box.h);
        if (!dx && !dy) return;
        pxPush(ed);
        var out = new Uint8ClampedArray(ed.w * ed.h * 4);
        var src = ed.grid.data;
        for (var y = 0; y < ed.h; y++) {
            for (var x = 0; x < ed.w; x++) {
                var i = (y * ed.w + x) * 4;
                if (!src[i + 3]) continue;
                var tx = x + dx, ty = y + dy;
                if (tx < 0 || ty < 0 || tx >= ed.w || ty >= ed.h) continue;
                var j = (ty * ed.w + tx) * 4;
                out[j] = src[i]; out[j + 1] = src[i + 1]; out[j + 2] = src[i + 2]; out[j + 3] = src[i + 3];
            }
        }
        ed.grid.data.set(out);
        pxRender(ed);
    }

    function pxVectorPaths (ed) {
        var d = ed.grid.data, w = ed.w, h = ed.h;
        var groups = {}, last = {}, rects = 0;
        for (var y = 0; y < h; y++) {
            var x = 0;
            while (x < w) {
                var i = (y * w + x) * 4;
                var a = d[i + 3];
                if (!a) { x++; continue; }
                var r = d[i], g = d[i + 1], b = d[i + 2];
                var x2 = x + 1;
                while (x2 < w) {
                    var j = (y * w + x2) * 4;
                    if (d[j + 3] !== a || d[j] !== r || d[j + 1] !== g || d[j + 2] !== b) break;
                    x2++;
                }
                var key = r + ',' + g + ',' + b + ',' + a;
                var group = groups[key] || (groups[key] = {r: r, g: g, b: b, a: a, rects: []});
                var runKey = key + '|' + x + '|' + (x2 - x);
                var open = last[runKey];
                if (open && open.y + open.h === y) {
                    open.h++;
                } else {
                    open = {x: x, y: y, w: x2 - x, h: 1};
                    group.rects.push(open);
                    last[runKey] = open;
                    rects++;
                }
                x = x2;
            }
        }
        var unit = ed.unit, offX = ed.offX, offY = ed.offY, x0 = ed.x0, y0 = ed.y0;
        return {count: rects, paths: Object.keys(groups).map(function (key) {
            var group = groups[key];
            var boxes = group.rects.map(function (rect) {
                return {
                    x: x0 + (offX + rect.x) * unit,
                    y: y0 + (offY + rect.y) * unit,
                    w: rect.w * unit,
                    h: rect.h * unit
                };
            });
            return {boxes: boxes, color: group, runs: group.rects.length};
        }).sort(function (a, b) { return a.runs - b.runs; })};
    }

    function pxClearSelection () {
        try {
            var state = store() && store().getState();
            var sel = state && state.scratchPaint && state.scratchPaint.selectedItems;
            if (sel && sel.length) {
                dispatch({type: 'scratch-paint/select/CHANGE_SELECTED_ITEMS', selectedItems: []});
            }
        } catch (e) { /* the editor owns its selection; failing to clear is not fatal */ }
    }

    function pxApply () {
        var ed = pxEd;
        if (!ed) return;
        if (ed.autoFixInput.checked) pxFixAlpha(ed.grid);
        if (!pxHasArt(ed.grid)) {
            ed.note.textContent = t('pixelEmpty');
            return;
        }
        var p = paper(), layer = paintingLayer();
        if (!p || !layer) {
            ed.note.textContent = t('pxFail');
            return;
        }
        var vector = pxVectorPaths(ed);
        if (!vector.paths.length) {
            ed.note.textContent = t('pixelEmpty');
            return;
        }
        if (vector.count > 30000) {
            ed.note.textContent = t('pxHeavy');
            return;
        }
        ed.busy = true;
        pxClearSelection();
        layer.children.slice().forEach(function (child) {
            if (!child.guide) child.remove();
        });
        var raster = findLayer('isRasterLayer');
        if (raster) raster.removeChildren();
        var added = [];
        vector.paths.forEach(function (g) {
            var kids = g.boxes.map(function (box) {
                return new p.Path({
                    segments: [[box.x, box.y], [box.x + box.w, box.y],
                        [box.x + box.w, box.y + box.h], [box.x, box.y + box.h]],
                    closed: true,
                    insert: false
                });
            });
            var path = new p.CompoundPath({children: kids, insert: false});
            path.fillColor = new p.Color(g.color.r / 255, g.color.g / 255, g.color.b / 255, 1);
            path.strokeColor = null;
            path.strokeWidth = 0;
            path.fillRule = 'nonzero';
            layer.addChild(path);
            added.push(path);
        });
        layer.activate();
        p.view.update();
        if (!commit()) {
            added.forEach(function (a) { a.remove(); });
            ed.busy = false;
            ed.note.textContent = t('pxFail');
            return;
        }
        var target = ed.target;
        closePixelEditor();
        refreshAnchorView(target);
        pageToast(t('pxApplied'));
    }

    function closePixelEditor () {
        if (!pxEd) return;
        window.removeEventListener('keydown', pxEd.onKey, true);
        window.removeEventListener('resize', pxEd.onResize);
        pxEd.root.remove();
        pxEd = null;
    }

    function pxKeyDown (ed, e) {
        if (e.target && /INPUT|TEXTAREA|SELECT/.test(e.target.tagName)) return;
        e.stopPropagation();
        if (e.ctrlKey || e.metaKey) {
            if (e.key.toLowerCase() === 'z') {
                e.preventDefault();
                if (e.shiftKey) pxRedo(ed); else pxUndo(ed);
            }
            return;
        }
        var k = e.key.toLowerCase();
        if (k === 'escape') {
            e.preventDefault();
            closePixelEditor();
            return;
        }
        var map = {b: 'brush', p: 'pencil', e: 'eraser', g: 'fill', l: 'line', r: 'rect',
            o: 'ellipse', i: 'picker', v: 'move'};
        if (map[k]) {
            e.preventDefault();
            ed.setTool(map[k]);
            return;
        }
        if (k === '[' || k === ']') {
            e.preventDefault();
            ed.size = clamp(ed.size + (k === ']' ? 1 : -1), 1, 8);
            ed.sizeInput.value = ed.size;
            ed.sizeOut.textContent = String(ed.size);
            return;
        }
        if (k === '+' || k === '=') { e.preventDefault(); pxZoom(ed, 1); return; }
        if (k === '-') { e.preventDefault(); pxZoom(ed, -1); return; }
        if (e.key.indexOf('Arrow') === 0) {
            e.preventDefault();
            var dx = e.key === 'ArrowLeft' ? -1 : e.key === 'ArrowRight' ? 1 : 0;
            var dy = e.key === 'ArrowUp' ? -1 : e.key === 'ArrowDown' ? 1 : 0;
            pxNudge(ed, dx, dy);
        }
    }

    function buildPixelEditor (src) {
        readLocale();
        var bounds = pxBounds(src.ctx.getImageData(0, 0, src.W, src.H).data, src.W, src.H);
        var offX, offY, w, h, grid;
        if (!bounds) {
            w = 64; h = 64;
            offX = Math.round((src.W - w) / 2);
            offY = Math.round((src.H - h) / 2);
            grid = new ImageData(w, h);
        } else {
            offX = bounds.x; offY = bounds.y; w = bounds.w; h = bounds.h;
            grid = src.ctx.getImageData(bounds.x, bounds.y, w, h);
        }
        var theme = readTheme();
        var unit = 2 / src.s;
        var x0 = 480 - src.costumeW;
        var y0 = 360 - src.costumeH;
        var root = document.createElement('div');
        root.className = 'gwt-px';
        root.setAttribute('dir', isRtl ? 'rtl' : 'ltr');
        applyTheme(root, theme);

        var ed: PixelEditor = {
            root: root, src: src, target: src.target, costume: src.costume && src.costume.name,
            w: w, h: h, offX: offX, offY: offY, unit: unit, x0: x0, y0: y0,
            grid: grid, undo: [], redo: [], zoom: 1, tool: 'brush', color: '#4c97ff',
            rgba: [76, 151, 255, 255], size: 3, filled: true, gridOn: true, dark: theme.dark, gesture: null, busy: false,
            tmp: document.createElement('canvas')
        };

        var head = document.createElement('div');
        head.className = 'gwt-px-head';
        var title = document.createElement('strong');
        title.textContent = t('pixelArt');
        var sub = document.createElement('span');
        sub.className = 'gwt-px-sub';
        sub.textContent = t('pixelSub') + ' · ' + w + '×' + h;
        var undo = document.createElement('button');
        undo.type = 'button';
        undo.className = 'gwt-px-icon';
        undo.title = t('undo');
        undo.setAttribute('aria-label', t('undo'));
        undo.innerHTML = GLYPH.undo;
        listen(undo, 'click', function () { pxUndo(ed); });
        var redo = document.createElement('button');
        redo.type = 'button';
        redo.className = 'gwt-px-icon';
        redo.title = t('redo');
        redo.setAttribute('aria-label', t('redo'));
        redo.innerHTML = icon('<path d="M16 8.5H7.5a4 4 0 1 0 0 8H13"/><path d="M16 8.5 13 5.5M16 8.5l-3 3"/>');
        listen(redo, 'click', function () { pxRedo(ed); });
        var close = document.createElement('button');
        close.type = 'button';
        close.className = 'gwt-px-icon gwt-px-close';
        close.setAttribute('aria-label', t('close'));
        close.title = t('close');
        close.textContent = '✕';
        listen(close, 'click', closePixelEditor);
        head.append(title, sub, undo, redo, close);

        var tools = document.createElement('div');
        tools.className = 'gwt-px-tools';
        var toolButtons = {};
        var setTool = function (id) {
            ed.tool = id;
            Object.keys(toolButtons).forEach(function (key) {
                toolButtons[key].classList.toggle('gwt-on', key === id);
            });
            ed.canvas.style.cursor = id === 'move' ? 'grab' : id === 'picker' ? 'copy' : 'crosshair';
        };
        ed.setTool = setTool;
        PX_TOOLS.forEach(function (row) {
            var b = document.createElement('button');
            b.type = 'button';
            b.className = 'gwt-px-tool';
            b.title = t(row[2]);
            b.setAttribute('aria-label', t(row[2]));
            b.dataset.tool = String(row[0]);
            b.innerHTML = PX_ICONS[row[1]];
            listen(b, 'click', function () { setTool(row[0]); });
            tools.appendChild(b);
            toolButtons[row[0]] = b;
        });

        var stage = document.createElement('div');
        stage.className = 'gwt-px-stage';
        var paperBox = document.createElement('div');
        paperBox.className = 'gwt-px-paper';
        var canvas = document.createElement('canvas');
        canvas.className = 'gwt-px-canvas';
        paperBox.appendChild(canvas);
        stage.appendChild(paperBox);
        listen(canvas, 'pointerdown', function (e) { pxDown(ed, e); });
        listen(canvas, 'pointermove', function (e) { pxMove(ed, e); });
        listen(canvas, 'pointerup', function () { pxUp(ed); });
        listen(canvas, 'pointercancel', function () { pxUp(ed); });
        ed.canvas = canvas;
        ed.stage = stage;

        var side = document.createElement('aside');
        side.className = 'gwt-px-side';
        var palette = pxPalette(grid, 10);
        if (palette.length) {
            ed.color = palette[0];
            ed.rgba = [parseInt(palette[0].slice(1, 3), 16), parseInt(palette[0].slice(3, 5), 16),
                parseInt(palette[0].slice(5, 7), 16), 255];
        }
        var colorLabel = document.createElement('label');
        colorLabel.className = 'gwt-px-field';
        colorLabel.textContent = t('color');
        var colorInput = document.createElement('input');
        colorInput.type = 'color';
        colorInput.value = ed.color;
        listen(colorInput, 'input', function () {
            ed.color = colorInput.value;
            ed.rgba = [parseInt(ed.color.slice(1, 3), 16), parseInt(ed.color.slice(3, 5), 16),
                parseInt(ed.color.slice(5, 7), 16), 255];
        });
        colorLabel.appendChild(colorInput);
        side.appendChild(colorLabel);
        if (palette.length) {
            var swatches = document.createElement('div');
            swatches.className = 'gwt-px-palette';
            palette.forEach(function (hex) {
                var b = document.createElement('button');
                b.type = 'button';
                b.style.background = hex;
                b.title = hex;
                listen(b, 'click', function () {
                    ed.color = hex;
                    colorInput.value = hex;
                    ed.rgba = [parseInt(hex.slice(1, 3), 16), parseInt(hex.slice(3, 5), 16),
                        parseInt(hex.slice(5, 7), 16), 255];
                });
                swatches.appendChild(b);
            });
            side.appendChild(swatches);
        }
        var sizeLabel = document.createElement('label');
        sizeLabel.className = 'gwt-px-field';
        var sizeText = document.createElement('span');
        sizeText.textContent = t('size');
        var sizeInput = document.createElement('input');
        sizeInput.type = 'range';
        sizeInput.min = 1;
        sizeInput.max = 8;
        sizeInput.value = ed.size;
        var sizeOut = document.createElement('output');
        sizeOut.textContent = String(ed.size);
        listen(sizeInput, 'input', function () {
            ed.size = Number(sizeInput.value);
            sizeOut.textContent = String(ed.size);
        });
        sizeLabel.append(sizeText, sizeInput, sizeOut);
        side.appendChild(sizeLabel);
        var fillLabel = document.createElement('label');
        fillLabel.className = 'gwt-px-check';
        var fillInput = document.createElement('input');
        fillInput.type = 'checkbox';
        fillInput.checked = true;
        listen(fillInput, 'change', function () { ed.filled = fillInput.checked; });
        fillLabel.append(fillInput, document.createTextNode(t('filled')));
        side.appendChild(fillLabel);
        var gridLabel = document.createElement('label');
        gridLabel.className = 'gwt-px-check';
        var gridInput = document.createElement('input');
        gridInput.type = 'checkbox';
        gridInput.checked = true;
        listen(gridInput, 'change', function () {
            ed.gridOn = gridInput.checked;
            pxRender(ed);
        });
        gridLabel.append(gridInput, document.createTextNode(t('gridOn')));
        side.appendChild(gridLabel);
        var fixLabel = document.createElement('label');
        fixLabel.className = 'gwt-px-check';
        var fixInput = document.createElement('input');
        fixInput.type = 'checkbox';
        fixInput.checked = true;
        fixLabel.append(fixInput, document.createTextNode(t('autoFix')));
        side.appendChild(fixLabel);
        var help = document.createElement('p');
        help.className = 'gwt-px-help';
        help.textContent = t('pixelHelp');
        side.appendChild(help);

        var foot = document.createElement('div');
        foot.className = 'gwt-px-foot';
        var note = document.createElement('span');
        note.className = 'gwt-px-note';
        var zoomOut = document.createElement('button');
        zoomOut.type = 'button';
        zoomOut.className = 'gwt-px-zoom';
        zoomOut.textContent = '−';
        zoomOut.title = t('zoom');
        listen(zoomOut, 'click', function () { pxZoom(ed, -1); });
        var zoomLabel = document.createElement('span');
        zoomLabel.className = 'gwt-px-zoomval';
        var zoomIn = document.createElement('button');
        zoomIn.type = 'button';
        zoomIn.className = 'gwt-px-zoom';
        zoomIn.textContent = '+';
        zoomIn.title = t('zoom');
        listen(zoomIn, 'click', function () { pxZoom(ed, 1); });
        var fit = document.createElement('button');
        fit.type = 'button';
        fit.className = 'gwt-px-zoom gwt-px-fit';
        fit.textContent = t('fit');
        listen(fit, 'click', function () { pxFit(ed); });
        var cancel = document.createElement('button');
        cancel.type = 'button';
        cancel.className = 'gwt-px-cancel';
        cancel.textContent = t('cancel');
        listen(cancel, 'click', closePixelEditor);
        var apply = document.createElement('button');
        apply.type = 'button';
        apply.className = 'gwt-px-apply';
        apply.textContent = t('applyVector');
        listen(apply, 'click', pxApply);
        foot.append(note, zoomOut, zoomLabel, zoomIn, fit, cancel, apply);

        var win = document.createElement('div');
        win.className = 'gwt-px-win';
        win.append(head, tools, stage, side, foot);
        root.appendChild(win);
        document.body.appendChild(root);
        ed.note = note;
        ed.colorInput = colorInput;
        ed.sizeInput = sizeInput;
        ed.sizeOut = sizeOut;
        ed.autoFixInput = fixInput;
        ed.zoomOut = zoomLabel;
        ed.btnUndo = undo;
        ed.btnRedo = redo;
        setTool('brush');
        pxUpdateButtons(ed);
        pxEd = ed;
        window.requestAnimationFrame(function () {
            if (pxEd === ed) {
                ed.autoFixInput = fixInput;
                if (fixInput.checked) {
                    var fixed = pxFixAlpha(ed.grid);
                    if (fixed) note.textContent = fixed + ' ' + t('autoFixed');
                }
                pxFit(ed);
            }
        });
        ed.onKey = function (e) { pxKeyDown(ed, e); };
        ed.onResize = function () { if (pxEd === ed) pxFit(ed); };
        listen(window, 'keydown', ed.onKey, true);
        listen(window, 'resize', ed.onResize);
    }

    function openPixelEditor () {
        if (pxEd) return;
        var target = anchorTarget();
        if (!target) {
            pageToast(t('pixelNoTarget'));
            return;
        }
        var wasBitmap = isBitmapFormat();
        if (wasBitmap) pageToast(t('converting'));
        ensureVectorCostume().then(function (ok) {
            if (!ok) {
                pageToast(t('pixelNoTarget'), true);
                return;
            }
            rasterizeCostume().then(function (src) {
                if (!src) {
                    pageToast(t('pixelNoTarget'), true);
                    return;
                }
                buildPixelEditor(src);
            });
        });
    }

    var BOARD_KEY = 'gowarp-toolkit.board';
    var boardEl = null;
    var boardBtn = null;
    var boardData = null;
    var boardDrag = null;
    var boardSaveTimer = 0;
    var boardFootNote = null;

    function boardProjectKey () { return sfProjectKey(); }
    function boardDefault () {
        return {cols: [{id: 'c1', name: t('todo')}, {id: 'c2', name: t('doing')}, {id: 'c3', name: t('done')}],
            cards: []};
    }
    function boardLoad () {
        try {
            var raw = localStorage.getItem(BOARD_KEY + ':' + boardProjectKey());
            if (raw) {
                var b = JSON.parse(raw);
                if (b && Array.isArray(b.cols) && b.cols.length && Array.isArray(b.cards)) {
                    b.cols = b.cols.filter(function (c) { return c && typeof c.id === 'string'; })
                        .map(function (c) { return {id: c.id, name: String(c.name || '').slice(0, 40)}; });
                    b.cards = b.cards.filter(function (c) {
                        return c && typeof c.id === 'string' && typeof c.col === 'string';
                    }).map(function (c) {
                        return {id: c.id, col: c.col, title: String(c.title || '').slice(0, 200),
                            note: String(c.note || '').slice(0, 2000)};
                    });
                    if (!b.cols.length) b.cols = boardDefault().cols;
                    return b;
                }
            }
        } catch (e) { /* corrupt board: fall through to a fresh one */ }
        return boardDefault();
    }
    function boardSave () {
        if (!boardData) return;
        window.clearTimeout(boardSaveTimer);
        boardSaveTimer = later(function () {
            try {
                localStorage.setItem(BOARD_KEY + ':' + boardProjectKey(), JSON.stringify(boardData));
            } catch (e) { /* private mode or quota: the board still works this session */ }
        }, 250);
    }
    function boardId () {
        return 'b' + Date.now().toString(36) + Math.random().toString(36).slice(2, 6);
    }
    function boardSay (text) {
        if (boardFootNote) boardFootNote.textContent = text;
    }
    function boardClose () {
        if (boardEl) boardEl.remove();
        boardEl = null;
        boardFootNote = null;
    }
    function boardCard (card, col) {
        var box = document.createElement('div');
        box.className = 'gwt-bcard';
        box.dataset.card = String(card.id);

        var title = document.createElement('textarea');
        title.className = 'gwt-ctitle';
        title.rows = 1;
        title.value = card.title;
        title.placeholder = t('cardTitle');
        title.setAttribute('aria-label', t('cardTitle'));
        listen(title, 'input', function () {
            card.title = title.value;
            title.rows = Math.max(1, Math.min(4, title.value.split('\n').length));
            boardSave();
        });

        var note = document.createElement('textarea');
        note.rows = 2;
        note.value = card.note;
        note.placeholder = t('cardNote');
        note.setAttribute('aria-label', t('cardNote'));
        listen(note, 'input', function () {
            card.note = note.value;
            note.rows = Math.max(2, Math.min(6, note.value.split('\n').length + 1));
            boardSave();
        });

        var del = document.createElement('button');
        del.type = 'button';
        del.className = 'gwt-mini';
        del.title = t('delCard');
        del.setAttribute('aria-label', t('delCard'));
        del.innerHTML = GLYPH.trash;
        listen(del, 'click', function () {
            boardData.cards = boardData.cards.filter(function (c) { return c.id !== card.id; });
            boardSave();
            boardRender();
        });

        var bar = document.createElement('div');
        bar.className = 'gwt-bch gwt-drag';
        bar.draggable = true;
        bar.appendChild(del);
        listen(bar, 'dragstart', function (e) {
            boardDrag = card.id;
            if (e.dataTransfer) {
                e.dataTransfer.effectAllowed = 'move';
                try { e.dataTransfer.setData('text/plain', card.id); } catch (err) { /* firefox needs no data */ }
            }
        });
        listen(bar, 'dragend', function () { boardDrag = null; });
        box.append(bar, title, note);
        return box;
    }
    function boardRender () {
        if (!boardEl || !boardData) return;
        var body = boardEl.querySelector('.gwt-bcols');
        if (!body) return;
        body.textContent = '';
        boardData.cols.forEach(function (col) {
            var column = document.createElement('div');
            column.className = 'gwt-bcol';
            column.dataset.col = String(col.id);

            var head = document.createElement('div');
            head.className = 'gwt-bch';
            var name = document.createElement('input');
            name.className = 'gwt-bname';
            name.value = col.name;
            name.placeholder = t('colName');
            name.setAttribute('aria-label', t('colName'));
            listen(name, 'input', function () {
                col.name = name.value.slice(0, 40);
                boardSave();
            });
            var drop = document.createElement('button');
            drop.type = 'button';
            drop.className = 'gwt-mini';
            drop.title = t('delCol');
            drop.setAttribute('aria-label', t('delCol'));
            drop.innerHTML = GLYPH.trash;
            drop.disabled = boardData.cols.length < 2;
            listen(drop, 'click', function () {
                var rest = boardData.cols.filter(function (c) { return c.id !== col.id; });
                if (!rest.length) return;
                boardData.cards.forEach(function (c) { if (c.col === col.id) c.col = rest[0].id; });
                boardData.cols = rest;
                boardSave();
                boardRender();
            });
            head.append(name, drop);
            column.appendChild(head);

            boardData.cards.filter(function (c) { return c.col === col.id; }).forEach(function (card) {
                column.appendChild(boardCard(card, col));
            });

            var add = document.createElement('button');
            add.type = 'button';
            add.className = 'gwt-badd';
            add.textContent = '+ ' + t('addCard');
            listen(add, 'click', function () {
                boardData.cards.push({id: boardId(), col: col.id, title: '', note: ''});
                boardSave();
                boardRender();
                later(function () {
                    var list = boardEl && boardEl.querySelectorAll('[data-col="' + col.id + '"] .gwt-bcard');
                    var last = list && list[list.length - 1];
                    if (last) last.querySelector('textarea').focus();
                }, 0);
            });
            column.appendChild(add);

            listen(column, 'dragover', function (e) {
                if (!boardDrag) return;
                e.preventDefault();
                column.classList.add('gwt-over');
            });
            listen(column, 'dragleave', function () { column.classList.remove('gwt-over'); });
            listen(column, 'drop', function (e) {
                e.preventDefault();
                column.classList.remove('gwt-over');
                var id = boardDrag || (e.dataTransfer && e.dataTransfer.getData('text/plain'));
                var card = boardData.cards.filter(function (c) { return c.id === id; })[0];
                if (!card || card.col === col.id) return;
                card.col = col.id;
                boardDrag = null;
                boardSave();
                boardRender();
            });

            body.appendChild(column);
        });

    }
    function boardExport () {
        try {
            var blob = new Blob([JSON.stringify({app: 'GoWarp Toolkit', version: 2, board: boardData}, null, 2)],
                {type: 'application/json'});
            var a = document.createElement('a');
            a.href = URL.createObjectURL(blob);
            a.download = 'gowarp-board.json';
            document.body.appendChild(a);
            a.click();
            a.remove();
            later(function () { URL.revokeObjectURL(a.href); }, 2000);
            boardSay(t('boardExported'));
        } catch (e) {
            boardSay(t('boardFail'));
        }
    }
    function boardImport (file) {
        if (!file) return;
        var reader = new FileReader();
        reader.onload = function () {
            try {
                var parsed = JSON.parse(String(reader.result || ''));
                var b = parsed && parsed.board ? parsed.board : parsed;
                if (!b || !Array.isArray(b.cols) || !b.cols.length || !Array.isArray(b.cards)) throw new Error('shape');
                boardData = {
                    cols: b.cols.map(function (c, i) { return {id: String(c.id || 'c' + i), name: String(c.name || '').slice(0, 40)}; }),
                    cards: b.cards.map(function (c, i) {
                        return {id: String(c.id || 'k' + i), col: String(c.col || ''), title: String(c.title || '').slice(0, 200),
                            note: String(c.note || '').slice(0, 2000)};
                    })
                };
                var ids = boardData.cols.map(function (c) { return c.id; });
                boardData.cards.forEach(function (c) { if (ids.indexOf(c.col) < 0) c.col = ids[0]; });
                boardSave();
                boardRender();
                boardSay(t('boardImported'));
            } catch (e) {
                boardSay(t('boardBad'));
            }
        };
        reader.readAsText(file);
    }
    function openBoard () {
        if (boardEl) {
            boardClose();
            return;
        }
        readLocale();
        boardData = boardLoad();
        var panel = document.createElement('div');
        panel.className = 'gwt-board';
        panel.setAttribute('dir', isRtl ? 'rtl' : 'ltr');
        applyTheme(panel, readTheme());

        var head = document.createElement('div');
        head.className = 'gwt-bhead';
        var title = document.createElement('strong');
        title.textContent = t('boardTitle');
        var sub = document.createElement('span');
        sub.className = 'gwt-bsub';
        sub.textContent = t('boardSub');
        var addCol = document.createElement('button');
        addCol.type = 'button';
        addCol.className = 'gwt-mini';
        addCol.title = t('addCol');
        addCol.setAttribute('aria-label', t('addCol'));
        addCol.innerHTML = GLYPH.plus;
        listen(addCol, 'click', function () {
            var col = {id: boardId(), name: t('newCol')};
            boardData.cols.push(col);
            boardSave();
            boardRender();
        });
        var close = document.createElement('button');
        close.type = 'button';
        close.className = 'gwt-close';
        close.setAttribute('aria-label', t('close'));
        close.textContent = '✕';
        listen(close, 'click', boardClose);
        head.append(title, sub, addCol, close);

        var body = document.createElement('div');
        body.className = 'gwt-bcols';

        var foot = document.createElement('div');
        foot.className = 'gwt-bfoot';
        var out = document.createElement('button');
        out.type = 'button';
        out.textContent = t('exportB');
        listen(out, 'click', boardExport);
        var into = document.createElement('button');
        into.type = 'button';
        into.textContent = t('importB');
        var files = document.createElement('input');
        files.type = 'file';
        files.accept = '.json,application/json';
        files.hidden = true;
        listen(into, 'click', function () { files.click(); });
        listen(files, 'change', function () {
            boardImport(files.files && files.files[0]);
            files.value = '';
        });
        var note = document.createElement('span');
        note.className = 'gwt-bsub';
        note.style.padding = '.2rem .1rem';
        boardFootNote = note;
        foot.append(out, into, files, note);

        panel.append(head, body, foot);
        document.body.appendChild(panel);
        boardEl = panel;
        boardRender();
    }

    function syncBoardButton () {
        var host = document.querySelector('[class*="asset-panel_detail-area"]');
        if (!host || !paintEditorOpen()) {
            if (boardBtn) { boardBtn.remove(); boardBtn = null; }
            return;
        }
        if (!boardBtn || !boardBtn.isConnected) {
            boardBtn = document.createElement('button');
            boardBtn.type = 'button';
            boardBtn.className = 'gwt-board-btn';
            boardBtn.innerHTML = BOARD_GLYPH;
            listen(boardBtn, 'click', openBoard);
            document.body.appendChild(boardBtn);
        }
        var theme = readTheme();
        boardBtn.style.color = theme.text;
        var r = host.getBoundingClientRect();
        boardBtn.style.left = Math.round(r.right - 40) + 'px';
        boardBtn.style.top = Math.round(r.top + 10) + 'px';
        boardBtn.title = t('boardTitle');
        boardBtn.setAttribute('aria-label', t('boardTitle'));
    }

    var lastMode = null;
    var lastFormat = null;
    var lastColorSig = null;
    function colorSig (color) {
        if (!color) return '-';
        if (color.gradient) {
            var stops = color.gradient.stops || [];
            return 'g|' + color.gradient.type + '|' + stops.map(function (s) {
                return Math.round(s.offset * 1000) + '@' + (s.color && s.color.toCSS ? s.color.toCSS(true) : s.color);
            }).join(',');
        }
        if (color.toCSS) return color.toCSS(true);
        var f = color.fillColor || {};
        var s = color.strokeColor || {};
        return [f.gradientType, f.primary, f.secondary, s.gradientType, s.primary, s.secondary].join('|');
    }
    function isBrushFamily (mode) {
        return /^(BRUSH|ERASER|BIT_BRUSH|BIT_ERASER)$/.test(mode || '');
    }
    function onStateChange () {
        var p = paintState();
        if (!p) return;
        var sig = colorSig(p.color || {});
        if (sig !== lastColorSig) {
            lastColorSig = sig;
            if (gradStrip) paintGradStrip();
        }
        if (p.mode !== lastMode) {
            var switched = lastMode !== null;
            lastMode = p.mode;
            if (switched) {
                if (pen.active && p.mode !== pen.armedInMode) pen.deactivate();
                if (shaper.shape) shaper.disarm();
                if (openPanel && openPanel._gwtKind === 'nib' && !isBrushFamily(p.mode)) closePanel();
                else if (openPanel && openPanel._gwtKind === 'nib') refreshNibUi();
                if (railEl) markObjects();
            }
        }
        if (p.format !== lastFormat) {
            var changed = lastFormat !== null;
            lastFormat = p.format;
            if (changed && openPanel && openPanel._gwtKind === 'nib') closePanel();
            if (changed) syncRail();
        }
    }

    function findVM () { return Scratch.vm; }
    function anchorTarget () {
        var vm = findVM();
        var t = vm && vm.editingTarget;
        return (t && !t.isStage && typeof t.getCostumes === 'function') ? t : null;
    }

    var anchorCache = {};
    var anchorState = {probing: false, busy: false, list: null, spriteId: '', n: 0, mode: 'match', undo: null, result: null};

    function alphaStats (img, w, h, scale) {
        var s = Math.min(scale, 1400 / Math.max(w, h, 1));
        var cv = document.createElement('canvas');
        cv.width = Math.max(1, Math.round(w * s));
        cv.height = Math.max(1, Math.round(h * s));
        var ctx = cv.getContext('2d', {willReadFrequently: true});
        ctx.drawImage(img, 0, 0, cv.width, cv.height);
        var d = ctx.getImageData(0, 0, cv.width, cv.height).data;
        var sum = 0, sx = 0, sy = 0, minX = 1e9, minY = 1e9, maxX = -1, maxY = -1;
        for (var y = 0; y < cv.height; y++) {
            for (var x = 0; x < cv.width; x++) {
                var a = d[(y * cv.width + x) * 4 + 3];
                if (!a) continue;
                sum += a;
                sx += x * a;
                sy += y * a;
                if (x < minX) minX = x;
                if (x > maxX) maxX = x;
                if (y < minY) minY = y;
                if (y > maxY) maxY = y;
            }
        }
        var out: AlphaStats = {area: sum, w: cv.width / s, h: cv.height / s, thumb: ''};
        if (!sum) return out;
        out.cx = sx / sum / s;
        out.cy = sy / sum / s;
        out.box = [(minX + maxX) / 2 / s, (minY + maxY) / 2 / s];
        var fit = Math.min(1, 56 / Math.max(1, out.w), 56 / Math.max(1, out.h));
        var pv = document.createElement('canvas');
        pv.width = Math.max(1, Math.round(out.w * fit));
        pv.height = Math.max(1, Math.round(out.h * fit));
        pv.getContext('2d').drawImage(img, 0, 0, pv.width, pv.height);
        out.thumb = pv.toDataURL('image/png');
        return out;
    }

    function costumeRaster (c) {
        var raw = c.asset && (c.asset.data !== undefined ? c.asset.data : c.asset);
        if (c.dataFormat === 'svg' || typeof raw === 'string') {
            var txt = typeof raw === 'string' ? raw : new TextDecoder().decode(raw);
            var w = Number((/\swidth="([\d.]+)"/.exec(txt) || [])[1]) || 0;
            var h = Number((/\sheight="([\d.]+)"/.exec(txt) || [])[1]) || 0;
            return new Promise(function (res, rej) {
                var img = new Image();
                img.onload = function () {
                    res(alphaStats(img, w || img.naturalWidth, h || img.naturalHeight, 4));
                };
                img.onerror = function () { rej(new Error('svg raster')); };
                img.src = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(txt);
            });
        }
        var bytes = raw instanceof ArrayBuffer ? raw : (raw && raw.buffer ? raw.buffer : raw);
        return createImageBitmap(new Blob([bytes], {type: 'image/' + (c.dataFormat || 'png')}))
            .then(function (bmp) {
                var s = alphaStats(bmp, bmp.width, bmp.height, 1);
                var res = Math.max(1, Number(c.bitmapResolution) || 1);
                if (s.area && res > 1) {
                    var inUnits = Math.hypot(s.cx / res - c.rotationCenterX, s.cy / res - c.rotationCenterY);
                    var inPixels = Math.hypot(s.cx - c.rotationCenterX, s.cy - c.rotationCenterY);
                    if (inUnits <= inPixels) {
                        s.cx /= res;
                        s.cy /= res;
                        s.w /= res;
                        s.h /= res;
                    }
                }
                return s;
            });
    }

    function measureOne (c) {
        var key = (c.md5 || c.assetId || '') + '|' + c.dataFormat + '|' + c.name;
        if (anchorCache[key]) return Promise.resolve(anchorCache[key]);
        return costumeRaster(c).then(function (s) {
            anchorCache[key] = s;
            var keys = Object.keys(anchorCache);
            if (keys.length > 48) delete anchorCache[keys[0]];
            return s;
        });
    }

    function measureAnchor () {
        var t = anchorTarget();
        if (!t) return Promise.resolve(null);
        var costumes = t.getCostumes();
        if (costumes.length < 2) return Promise.resolve({short: true, n: costumes.length});
        anchorState.busy = true;
        return Promise.all(costumes.map(measureOne)).then(function (stats) {
            anchorState.list = stats.map(function (s, i) {
                return {index: i, name: costumes[i].name, area: s.area || 0, cx: s.cx || 0, cy: s.cy || 0,
                    w: s.w || 0, h: s.h || 0, thumb: s.thumb || '',
                    pivot: [costumes[i].rotationCenterX, costumes[i].rotationCenterY]};
            });
            anchorState.spriteId = t.id;
            anchorState.n = costumes.length;
            anchorState.busy = false;
            return anchorState.list;
        }).catch(function (e) {
            anchorState.busy = false;
            if (window.console) window.console.warn('GoWarp Toolkit anchor:', e);
            return null;
        });
    }

    function anchorPlan (mode, list) {
        var live = list.filter(function (s) { return s.area > 0; });
        if (!live.length) return null;
        var out = list.map(function (s) { return [s.pivot[0], s.pivot[1]]; });
        if (mode === 'own') {
            list.forEach(function (s, i) { if (s.area > 0) out[i] = [s.cx, s.cy]; });
            return out;
        }
        if (mode === 'body') {
            var w = 0, x = 0, y = 0;
            live.forEach(function (s) { w += s.area; x += s.cx * s.area; y += s.cy * s.area; });
            list.forEach(function (s, i) { if (s.area > 0) out[i] = [x / w, y / w]; });
            return out;
        }
        var base = live[0];
        var d = [base.pivot[0] - base.cx, base.pivot[1] - base.cy];
        list.forEach(function (s, i) { if (s.area > 0) out[i] = [s.cx + d[0], s.cy + d[1]]; });
        return out;
    }

    function anchorJump (list, pivots) {
        var pts = [];
        list.forEach(function (s, i) {
            if (s.area > 0) pts.push([s.cx - pivots[i][0], s.cy - pivots[i][1]]);
        });
        var tot = 0, max = 0;
        for (var i = 1; i < pts.length; i++) {
            var j = Math.hypot(pts[i][0] - pts[i - 1][0], pts[i][1] - pts[i - 1][1]);
            tot += j;
            if (j > max) max = j;
        }
        return {mean: pts.length > 1 ? tot / (pts.length - 1) : 0, max: max, steps: pts.length - 1};
    }

    function px (v) {
        return (Math.round(v * 10) / 10).toFixed(1);
    }

    function refreshAnchorView (t) {
        try {
            var idx = t.currentCostume, n = t.getCostumes().length;
            if (n < 2) return;
            t.setCostume((idx + 1) % n);
            t.setCostume(idx);
            t.updateAllDrawableProperties();
            var vm = findVM();
            if (vm && typeof vm.emitTargetsUpdate === 'function') vm.emitTargetsUpdate();
            if (vm && typeof vm.emitProjectChanged === 'function') vm.emitProjectChanged();
        } catch (e) { /* the records are already correct; the view catches up on the next switch */ }
    }

    function applyAnchor (mode) {
        var t = anchorTarget();
        if (!t || !anchorState.list) return null;
        var costumes = t.getCostumes();
        if (costumes.length !== anchorState.n) {
            anchorState.list = null;
            return null;
        }
        anchorState.list.forEach(function (s, i) {
            s.pivot = [costumes[i].rotationCenterX, costumes[i].rotationCenterY];
        });
        var plan = anchorPlan(mode, anchorState.list);
        if (!plan) return null;
        var before = anchorJump(anchorState.list, anchorState.list.map(function (s) { return s.pivot; }));
        var snapshot = costumes.map(function (c) { return [c.rotationCenterX, c.rotationCenterY]; });
        var written = 0;
        anchorState.list.forEach(function (s, i) {
            if (s.area <= 0) return;
            costumes[i].rotationCenterX = Math.round(plan[i][0] * 1000) / 1000;
            costumes[i].rotationCenterY = Math.round(plan[i][1] * 1000) / 1000;
            s.pivot = [costumes[i].rotationCenterX, costumes[i].rotationCenterY];
            written++;
        });
        anchorState.undo = {spriteId: t.id, n: snapshot.length, list: snapshot, mode: mode};
        anchorState.result = {before: before.mean, after: anchorJump(anchorState.list,
            anchorState.list.map(function (s) { return s.pivot; })).mean, written: written};
        refreshAnchorView(t);
        return anchorState.result;
    }

    function undoAnchor () {
        var t = anchorTarget();
        var u = anchorState.undo;
        if (!t || !u || u.spriteId !== t.id) return false;
        var costumes = t.getCostumes();
        if (costumes.length !== u.n) return false;
        costumes.forEach(function (c, i) {
            c.rotationCenterX = u.list[i][0];
            c.rotationCenterY = u.list[i][1];
        });
        anchorState.undo = null;
        anchorState.result = null;
        if (anchorState.list) {
            anchorState.list.forEach(function (s, i) {
                s.pivot = [costumes[i].rotationCenterX, costumes[i].rotationCenterY];
            });
        }
        refreshAnchorView(t);
        return true;
    }

    function anchorBody (panel) {
        var jump = document.createElement('div');
        jump.className = 'gwt-jump';
        jump.innerHTML = '<strong>—</strong><span>px</span>';
        panel.appendChild(jump);
        var sub = document.createElement('div');
        sub.className = 'gwt-sub';
        sub.textContent = t('anchorSub');
        panel.appendChild(sub);

        var seg = document.createElement('div');
        seg.className = 'gwt-seg';
        [['body', t('anchorBody')], ['own', t('anchorOwn')], ['match', t('anchorFirst')]].forEach(function (row) {
            var b = document.createElement('button');
            b.type = 'button';
            b.dataset.mode = String(row[0]);
            b.textContent = row[1];
            listen(b, 'click', function () {
                anchorState.mode = row[0];
                markAnchor();
            });
            seg.appendChild(b);
        });
        panel.appendChild(seg);

        var list = document.createElement('div');
        list.className = 'gwt-alist';
        panel.appendChild(list);

        var act = document.createElement('div');
        act.className = 'gwt-actions';
        var go = document.createElement('button');
        go.type = 'button';
        go.className = 'gwt-primary';
        go.textContent = t('anchorApply');
        listen(go, 'click', function () {
            var r = applyAnchor(anchorState.mode);
            if (r) {
                markAnchor();
                if (railEl) markObjects();
            }
        });
        var back = document.createElement('button');
        back.type = 'button';
        back.className = 'gwt-ghost';
        back.textContent = t('anchorUndo');
        back.disabled = !anchorState.undo;
        listen(back, 'click', function () {
            if (undoAnchor()) markAnchor();
        });
        act.appendChild(go);
        act.appendChild(back);
        panel.appendChild(act);
        panel.appendChild(keyRow(['Alt+D']));
        markAnchor();
    }

    function markAnchor () {
        if (!openPanel || openPanel._gwtKind !== 'anchor') return;
        var panel = openPanel;
        var strong = panel.querySelector('.gwt-jump strong');
        var sub = panel.querySelector('.gwt-sub');
        var list = panel.querySelector('.gwt-alist');
        var go = panel.querySelector('.gwt-primary');
        var back = panel.querySelector('.gwt-ghost');
        Array.prototype.forEach.call(panel.querySelectorAll('[data-mode]'), function (b) {
            var on = b.dataset.mode === anchorState.mode;
            b.setAttribute('aria-pressed', String(on));
            b.classList.toggle('gwt-on', on);
        });
        var tgt = anchorTarget();
        if (!tgt || anchorState.busy) {
            strong.textContent = anchorState.busy ? '…' : '—';
            if (sub) sub.textContent = tgt ? t('anchorBusy') : t('anchorNone');
            if (go) go.disabled = true;
            return;
        }
        var costumes = tgt.getCostumes();
        if (costumes.length < 2 || !anchorState.list || anchorState.n !== costumes.length) {
            strong.textContent = '—';
            if (sub) sub.textContent = costumes.length < 2 ? t('anchorNone') : t('anchorBusy');
            if (list) { list.textContent = ''; list.dataset.sig = String(''); }
            if (go) go.disabled = true;
            if (back) back.disabled = true;
            if (costumes.length > 1 && !anchorState.probing) {
                anchorState.probing = true;
                measureAnchor().then(function () {
                    anchorState.probing = false;
                    markAnchor();
                }, function () {
                    anchorState.probing = false;
                });
            }
            return;
        }
        anchorState.list.forEach(function (s, i) {
            s.pivot = [costumes[i].rotationCenterX, costumes[i].rotationCenterY];
        });
        var now = anchorJump(anchorState.list, anchorState.list.map(function (s) { return s.pivot; }));
        var plan = anchorPlan(anchorState.mode, anchorState.list);
        var next = plan ? anchorJump(anchorState.list, plan) : now;
        strong.textContent = px(now.mean);
        if (sub) {
            sub.textContent = (anchorState.result ? t('anchorDone') + ' ' + anchorState.result.written + ' · ' : '') +
                px(now.mean) + ' → ' + px(next.mean) + ' px';
        }
        if (go) go.disabled = !plan || now.mean < .05;
        if (back) back.disabled = !anchorState.undo;
        if (!list || !plan) return;
        var sig = lang + ';' + anchorState.list.map(function (s, i) {
            return [s.name, s.area <= 0 ? '-' : px(s.pivot[0]) + ',' + px(s.pivot[1]) +
                '>' + px(plan[i][0]) + ',' + px(plan[i][1])].join('|');
        }).join(';');
        if (list.dataset.sig === sig) return;
        list.dataset.sig = String(sig);
        list.textContent = '';
        anchorState.list.forEach(function (s, i) {
            var row = document.createElement('div');
            row.className = 'gwt-arow';
            var th = document.createElement('i');
            th.className = 'gwt-athumb';
            if (s.thumb) th.style.backgroundImage = 'url(' + s.thumb + ')';
            var nm = document.createElement('span');
            nm.className = 'gwt-aname';
            nm.textContent = s.name;
            var d = document.createElement('span');
            d.className = 'gwt-adelta';
            if (s.area <= 0) {
                d.textContent = t('anchorSkipped');
            } else {
                var shift = Math.hypot(plan[i][0] - s.pivot[0], plan[i][1] - s.pivot[1]);
                d.textContent = (shift < .05 ? '=' : '→ ') + px(s.pivot[0]) + ',' + px(s.pivot[1]) +
                    (shift < .05 ? '' : ' → ' + px(plan[i][0]) + ',' + px(plan[i][1]));
            }
            row.appendChild(th);
            row.appendChild(nm);
            row.appendChild(d);
            list.appendChild(row);
        });
    }

    function showAnchorPanel (anchor) {
        var panel = toggleFor(anchor, 'anchor', 'gwt-wide');
        if (!panel) return;
        panel.appendChild(labelEl(t('anchorTitle')));
        anchorBody(panel);
    }

    function toolButton (key) {
        for (var i = 0; i < toolButtons.length; i++) {
            if (toolButtons[i]._gwtKey === key && toolButtons[i].isConnected) return toolButtons[i];
        }
        return null;
    }

    function syncToolButtons () {
        var grid = document.querySelector('[class*="paint-editor_mode-selector"]');
        toolButtons = toolButtons.filter(function (b) { return b.isConnected; });
        ['shapes', 'pen', 'fx', 'anchor'].forEach(function (key) {
            var btn = toolButton(key);
            if (btn || !grid) return;
            addToolButton(grid, key, TOOL_ICON[key], function () {
                var self = toolButton(key);
                if (!self) return;
                if (key === 'shapes') showShapePanel(self);
                else if (key === 'pen') showPenPanel(self);
                else if (key === 'anchor') showAnchorPanel(self);
                else showFxPanel(self);
            });
        });
        shaper.button = toolButton('shapes');
        pen.button = toolButton('pen');
        refreshToolLabels();
    }

    function mount () {
        var grid = document.querySelector('[class*="paint-editor_mode-selector"]');
        if (!grid || grid.dataset.gwtMounted || !paper() || !store()) return;
        grid.dataset.gwtMounted = String('1');
        readLocale();
        syncToolButtons();
        watchColorTarget();
        syncRail();
        if (pen.active) markArmed(pen.button, true);

        var s = store();
        if (!s.__gwtWatch) {
            s.__gwtWatch = true;
            lastMode = (paintState() || {}).mode;
            lastFormat = (paintState() || {}).format;
            unsubscribe = s.subscribe(onStateChange);
        }

        var buttons = grid.querySelectorAll('[class*="tool-select-base_mod-tool-select"]');
        Array.prototype.forEach.call(buttons, function (btn) {
            if (btn.classList.contains('gwt-tool')) return;
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
        if (host && !host.dataset.gwtWheel) {
            host.dataset.gwtWheel = String('1');
            listen(host, 'wheel', function (e) {
                if (!e.altKey) return;
                e.preventDefault();
                stepNib(e.deltaY < 0 ? 1 : -1);
            }, {passive: false});
        }
    }

    function paintEditorOpen () {
        return !!document.querySelector('[class*="paint-editor_mode-selector"]');
    }

    function onKeyDown (e) {
        if (pxEd) return;
        if (e.target && /INPUT|TEXTAREA|SELECT/.test(e.target.tagName)) return;
        if (!paintEditorOpen()) return;
        if (e.key === 'Escape') {
            if (openPanel) closePanel();
            else if (pen.active) pen.deactivate();
            else if (shaper.shape) shaper.disarm();
            return;
        }
        if (e.key === '[') stepNib(-1);
        if (e.key === ']') stepNib(1);
        if (e.altKey && !e.ctrlKey && !e.metaKey) {
            if (e.code === 'KeyM') {
                e.preventDefault();
                if (setMask(!isMasked())) markFx();
            } else if (e.code === 'KeyB') {
                e.preventDefault();
                stepBlend(e.shiftKey ? -1 : 1);
                markFx();
            } else if (e.code === 'KeyJ') {
                e.preventDefault();
                if (setMiter(1.2)) markFx();
            } else if (e.code === 'KeyD') {
                e.preventDefault();
                var anchorBtn = toolButton('anchor');
                if (openPanel && openPanel._gwtKind === 'anchor') {
                    applyAnchor(anchorState.mode);
                    markAnchor();
                } else if (anchorBtn) {
                    showAnchorPanel(anchorBtn);
                }
            } else if (e.code === 'KeyP') {
                e.preventDefault();
                openPixelEditor();
            } else if (e.code === 'KeyX') {
                e.preventDefault();
                exportPng(settings.pngScale || 1);
            }
        }
    }

    var SF_KEY = 'gowarp-toolkit.folders';
    var SF_FOLDER = '<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">' +
        '<path d="M10 4H4a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-8l-2-2z"/></svg>';
    var SF_FOLDER_PLUS = '<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">' +
        '<path d="M10 4H4a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-8l-2-2z' +
        'm.5 5H13v2.5h2.5V14H13v2.5h-2.5V14H8v-1.5h2.5V10z"/></svg>';
    var SF_CHECK = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" ' +
        'stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m5 13 4 4L19 7"/></svg>';

    var sf = {bar: null, hint: null, menu: null, obs: null, obsItems: null, raf: 0, sig: '', shown: '',
        active: '', project: '', data: null, links: {}, theme: '', editing: false, mounted: false, count: 0};

    function sfAll () {
        try {
            var raw = window.localStorage.getItem(SF_KEY);
            var all = raw ? JSON.parse(raw) : {};
            return all && typeof all === 'object' ? all : {};
        } catch (e) {
            return {};
        }
    }
    function sfSave () {
        try {
            var all = sfAll();
            all[sf.project] = sf.data;
            window.localStorage.setItem(SF_KEY, JSON.stringify(all));
        } catch (e) { /* private mode: the folders last for this tab only */ }
    }
    function sfProjectKey () {
        var p = location.pathname;
        var m = p.match(/\/projects\/(\d+)/);
        if (m) return 'scratch:' + m[1];
        m = p.match(/\/(\d{6,})(?:\/|$)/);
        if (m) return location.hostname + ':' + m[1];
        var title = String(document.title || '').replace(/\s+/g, ' ').trim().slice(0, 80);
        return location.hostname + ':' + p + ':' + title;
    }
    function sfLoad () {
        var key = sfProjectKey();
        if (sf.project !== key) {
            sf.project = key;
            var raw = sfAll()[key];
            var data = {folders: [], marks: []};
            if (raw && Array.isArray(raw.folders)) {
                raw.folders.forEach(function (f) {
                    if (f && typeof f.id === 'string' && typeof f.name === 'string') {
                        data.folders.push({id: f.id, name: f.name.slice(0, 24)});
                    }
                });
            }
            if (raw && Array.isArray(raw.marks)) {
                raw.marks.forEach(function (m) {
                    if (m && typeof m.n === 'string' && typeof m.f === 'string') data.marks.push({n: m.n, f: m.f});
                });
            }
            sf.data = data;
            sf.links = {};
            sf.active = '';
            sf.sig = '';
            sf.count = 0;
        }
        return sf.data;
    }
    function sfId () {
        return 'f' + Date.now().toString(36) + Math.random().toString(36).slice(2, 6);
    }

    function sfPane () {
        return document.querySelector('[class*="sprite-selector_sprite-selector"]');
    }
    function sfTiles () {
        var scroll = document.querySelector('[class*="sprite-selector_scroll-wrapper"]');
        if (!scroll) return [];
        return Array.prototype.slice.call(
            scroll.querySelectorAll('[class*="sprite-selector-item_sprite-selector-item"]'));
    }
    function sfTileName (tile) {
        var n = tile && tile.querySelector('[class*="sprite-selector-item_sprite-name"]');
        return n ? (n.textContent || '').trim() : '';
    }
    function sfSprites () {
        var out = [];
        var vm = findVM();
        if (vm && vm.runtime && vm.runtime.targets) {
            vm.runtime.targets.forEach(function (t) {
                if (t && t.isOriginal !== false && !t.isStage && t.sprite && t.sprite.name) {
                    out.push({id: t.id, name: String(t.sprite.name)});
                }
            });
            if (out.length) return out;
        }
        sfTiles().forEach(function (tile) {
            var name = sfTileName(tile);
            if (name) out.push({id: 'dom:' + name, name: name});
        });
        return out;
    }

    function sfLink (sprites) {
        var data = sfLoad();
        var live = {};
        sprites.forEach(function (s) { live[s.id] = true; });
        Object.keys(sf.links).forEach(function (id) {
            if (!live[id]) delete sf.links[id];
        });
        var used = [];
        Object.keys(sf.links).forEach(function (id) { used.push(sf.links[id]); });
        sprites.forEach(function (s) {
            var link = sf.links[s.id];
            if (link) {
                if (link.n !== s.name) { link.n = s.name; sfSave(); }
                return;
            }
            for (var i = 0; i < data.marks.length; i++) {
                var mark = data.marks[i];
                if (used.indexOf(mark) < 0 && mark.n === s.name) {
                    sf.links[s.id] = mark;
                    used.push(mark);
                    return;
                }
            }
        });
    }
    function sfFolderOf (sprite) {
        var link = sf.links[sprite.id];
        if (!link) return '';
        for (var i = 0; i < sf.data.folders.length; i++) {
            if (sf.data.folders[i].id === link.f) return link.f;
        }
        return '';
    }
    function sfFolderName (id) {
        for (var i = 0; i < sf.data.folders.length; i++) {
            if (sf.data.folders[i].id === id) return sf.data.folders[i].name;
        }
        return '';
    }
    function sfNameTaken (name, exceptId?: any) {
        var lower = name.toLowerCase();
        return sfLoad().folders.some(function (f) {
            return f.id !== exceptId && f.name.toLowerCase() === lower;
        });
    }
    function sfAssign (sprite, folderId) {
        var data = sfLoad();
        var link = sf.links[sprite.id];
        if (!link) {
            link = {n: sprite.name, f: ''};
            sf.links[sprite.id] = link;
            data.marks.push(link);
        }
        link.n = sprite.name;
        link.f = folderId || '';
        if (!link.f) {
            var i = data.marks.indexOf(link);
            if (i >= 0) data.marks.splice(i, 1);
        }
        sfSave();
        sf.sig = '';
        sfSync();
    }
    function sfMakeFolder (name) {
        var data = sfLoad();
        if (!name || sfNameTaken(name)) {
            var base = t('sfDefault');
            var n = 1;
            do { name = base + ' ' + n++; } while (sfNameTaken(name));
        }
        var folder = {id: sfId(), name: String(name).slice(0, 24)};
        data.folders.push(folder);
        sfSave();
        return folder;
    }

    function sfPaint (node) {
        var pane = sfPane() || document.body;
        var surface = paintedColor(pane, 'backgroundColor') || '#ffffff';
        var c = parseColor(surface) || {r: 255, g: 255, b: 255, a: 1};
        var dark = brightness(c) < 0.55;
        var selected = pane.querySelector ? pane.querySelector('[class*="sprite-selector-item_is-selected"]') : null;
        var accent = selected ? window.getComputedStyle(selected).borderTopColor : '';
        if (!parseColor(accent) || /^rgba\(0,\s*0,\s*0,\s*0\)$/.test(accent)) {
            accent = cssVar(window.getComputedStyle(pane), '--looks-secondary') || '#4c97ff';
        }
        var ac = parseColor(accent) || {r: 76, g: 151, b: 255, a: 1};
        applyTheme(node, {
            dark: dark,
            surface: surface,
            accent: accent,
            text: dark ? '#eeeeee' : '#575e75',
            field: dark ? 'rgba(255,255,255,.07)' : 'rgba(0,0,0,.05)',
            border: dark ? 'rgba(255,255,255,.16)' : 'rgba(0,0,0,.12)',
            hover: dark ? 'rgba(255,255,255,.12)' : 'rgba(0,0,0,.08)',
            active: rgba(ac, 0.35),
            shadow: dark ? '0 8px 24px rgba(0,0,0,.5)' : '0 8px 24px rgba(148,162,188,.35)'
        });
        node.style.setProperty('--gwt-accent-soft', rgba(ac, dark ? 0.22 : 0.13));
        node.style.colorScheme = dark ? 'dark' : 'light';
    }

    function sfEnsureBar (scroll) {
        if (sf.bar && sf.bar.parentElement === scroll) return;
        if (sf.bar) sf.bar.remove();
        var bar = document.createElement('div');
        bar.className = 'gwt-folders';
        bar.setAttribute('role', 'tablist');
        sf.bar = bar;
        sf.mounted = true;
        sf.sig = '';
        scroll.insertBefore(bar, scroll.firstChild);
        sfPaint(bar);
    }
    function sfEnsureHint (scroll) {
        if (sf.hint && sf.hint.parentElement === scroll) return sf.hint;
        if (sf.hint) sf.hint.remove();
        sf.hint = document.createElement('div');
        sf.hint.className = 'gwt-fempty';
        sf.hint.style.display = 'none';
        scroll.insertBefore(sf.hint, sf.bar && sf.bar.nextSibling ? sf.bar.nextSibling : scroll.firstChild);
        sfPaint(sf.hint);
        return sf.hint;
    }
    function sfChip (folderId, label, count, active, isFolder) {
        var chip = document.createElement('button');
        chip.type = 'button';
        chip.className = 'gwt-fchip';
        chip.dataset.folder = String(folderId);
        chip.setAttribute('role', 'tab');
        chip.setAttribute('aria-selected', String(active));
        if (isFolder) {
            chip.title = t('sfHint');
            chip.innerHTML = SF_FOLDER;
        }
        var name = document.createElement('span');
        name.className = 'gwt-fname';
        name.textContent = label;
        var num = document.createElement('span');
        num.className = 'gwt-fnum';
        num.textContent = count;
        chip.append(name, num);
        listen(chip, 'click', function (event) {
            if (event.detail > 1) return; // the second click of a rename must not re-filter
            sf.active = folderId;
            sfSync();
        });
        if (isFolder) {
            listen(chip, 'dblclick', function (event) {
                event.preventDefault();
                sfRenameChip(chip, folderId);
            });
            listen(chip, 'contextmenu', function (event) {
                event.preventDefault();
                event.stopPropagation();
                sfChipMenu(event.clientX, event.clientY, folderId);
            });
        }
        return chip;
    }
    function sfRenderBar (sprites) {
        if (!sf.bar || sf.editing) return;
        var data = sfLoad();
        if (sf.active && !data.folders.some(function (f) { return f.id === sf.active; })) sf.active = '';
        var counts = {};
        sprites.forEach(function (s) {
            var f = sfFolderOf(s);
            counts[f] = (counts[f] || 0) + 1;
        });
        var sig = JSON.stringify([lang, sprites.length, counts,
            data.folders.map(function (f) { return [f.id, f.name]; })]);
        if (sig === sf.sig) {
            if (sf.shown !== sf.active) {
                sf.shown = sf.active;
                Array.prototype.forEach.call(sf.bar.querySelectorAll('.gwt-fchip[data-folder]'), function (chip) {
                    chip.setAttribute('aria-selected', String((chip.dataset.folder || '') === sf.active));
                });
            }
            return;
        }
        sf.sig = sig;
        sf.shown = sf.active;
        sf.bar.textContent = '';
        sf.bar.appendChild(sfChip('', t('sfAll'), sprites.length, sf.active === '', false));
        data.folders.forEach(function (f) {
            sf.bar.appendChild(sfChip(f.id, f.name, counts[f.id] || 0, sf.active === f.id, true));
        });
        var add = document.createElement('button');
        add.type = 'button';
        add.className = 'gwt-fchip gwt-fnew';
        add.title = t('sfNew');
        add.setAttribute('aria-label', t('sfNew'));
        add.textContent = '+';
        listen(add, 'click', function () { sfNewChip(add); });
        sf.bar.appendChild(add);
    }
    function sfNameInput (holder, initial, commit, cancel?: any) {
        if (sf.editing) return;
        sf.editing = true;
        var input = document.createElement('input');
        input.className = 'gwt-finput';
        input.type = 'text';
        input.maxLength = 24;
        input.value = initial || '';
        input.setAttribute('aria-label', t('sfName'));
        holder.textContent = '';
        holder.appendChild(input);
        input.focus();
        input.select();
        var done = false;
        function finish (ok) {
            if (done) return;
            done = true;
            sf.editing = false;
            var value = input.value.trim();
            if (ok && value) commit(value);
            else if (cancel) cancel();
            sf.sig = '';
            sfSync();
        }
        listen(input, 'keydown', function (event) {
            event.stopPropagation();
            if (event.key === 'Enter') { event.preventDefault(); finish(true); }
            else if (event.key === 'Escape') { event.preventDefault(); finish(false); }
        });
        listen(input, 'blur', function () { finish(true); });
        listen(input, 'pointerdown', function (event) { event.stopPropagation(); });
        listen(input, 'click', function (event) { event.stopPropagation(); });
    }
    function sfNewChip (addBtn) {
        if (!sf.bar || sf.editing) return;
        var holder = document.createElement('span');
        holder.className = 'gwt-fchip gwt-fhold';
        sf.bar.insertBefore(holder, addBtn);
        sfNameInput(holder, '', function (name) {
            if (sfNameTaken(name)) { pageToast(t('sfDup'), true); return; }
            sfMakeFolder(name);
            sf.active = '';
            pageToast(t('sfCreated'));
        }, function () {
            holder.remove();
        });
    }
    function sfRenameChip (chip, folderId) {
        var folder = sfLoad().folders.filter(function (f) { return f.id === folderId; })[0];
        if (!folder) return;
        sfNameInput(chip, folder.name, function (name) {
            if (sfNameTaken(name, folderId)) { pageToast(t('sfDup'), true); return; }
            folder.name = name;
            sfSave();
            pageToast(t('sfRenamed'));
        });
    }
    function sfDeleteFolder (folderId) {
        var data = sfLoad();
        data.folders = data.folders.filter(function (f) { return f.id !== folderId; });
        data.marks = data.marks.filter(function (m) { return m.f !== folderId; });
        Object.keys(sf.links).forEach(function (id) {
            if (sf.links[id].f === folderId) delete sf.links[id];
        });
        if (sf.active === folderId) sf.active = '';
        sfSave();
        sf.sig = '';
        pageToast(t('sfDeleted'));
        sfSync();
    }

    function sfCloseMenu () {
        if (sf.menu) {
            sf.menu.remove();
            sf.menu = null;
        }
    }
    function sfMenu (x, y, rows) {
        sfCloseMenu();
        var menu = document.createElement('div');
        menu.className = 'gwt-fmenu';
        sfPaint(menu);
        rows.forEach(function (row) {
            if (row.sep) {
                menu.appendChild(document.createElement('hr'));
                return;
            }
            if (row.head !== undefined) {
                var head = document.createElement('div');
                head.className = 'gwt-fmhead';
                head.textContent = row.head;
                menu.appendChild(head);
                return;
            }
            var b = document.createElement('button');
            b.type = 'button';
            var mark = document.createElement('span');
            mark.className = 'gwt-fmcheck';
            if (row.checked) mark.innerHTML = SF_CHECK;
            b.appendChild(mark);
            if (row.icon) {
                var ic = document.createElement('span');
                ic.className = 'gwt-fmcheck';
                ic.innerHTML = row.icon;
                b.appendChild(ic);
            }
            var label = document.createElement('span');
            label.className = 'gwt-fmlabel';
            label.textContent = row.label;
            b.appendChild(label);
            listen(b, 'click', function () {
                sfCloseMenu();
                row.run();
            });
            menu.appendChild(b);
        });
        document.body.appendChild(menu);
        var rect = menu.getBoundingClientRect();
        menu.style.left = Math.max(8, Math.min(x, window.innerWidth - rect.width - 8)) + 'px';
        menu.style.top = Math.max(8, Math.min(y, window.innerHeight - rect.height - 8)) + 'px';
        sf.menu = menu;
    }
    function sfChipMenu (x, y, folderId) {
        sfMenu(x, y, [
            {head: sfFolderName(folderId)},
            {label: t('sfRename'), icon: SF_FOLDER, run: function () {
                var chip = sf.bar && sf.bar.querySelector('[data-folder="' + folderId + '"]');
                if (chip) sfRenameChip(chip, folderId);
            }},
            {label: t('sfDelete'), run: function () { sfDeleteFolder(folderId); }}
        ]);
    }
    function sfSpriteMenu (tile, anchor) {
        var name = sfTileName(tile);
        var sprite = sfSprites().filter(function (s) { return s.name === name; })[0];
        if (!sprite) return;
        var current = sfFolderOf(sprite);
        var data = sfLoad();
        var rows: FolderMenuItem[] = [{head: sprite.name}];
        data.folders.forEach(function (f) {
            rows.push({label: f.name, icon: SF_FOLDER, checked: f.id === current, run: function () {
                sfAssign(sprite, f.id);
                pageToast(t('sfMoved').replace('{s}', sprite.name).replace('{f}', f.name));
            }});
        });
        if (data.folders.length) rows.push({sep: true});
        if (current) {
            rows.push({label: t('sfNone'), run: function () { sfAssign(sprite, ''); }});
        }
        rows.push({label: t('sfNew'), icon: SF_FOLDER_PLUS, run: function () {
            var folder = sfMakeFolder('');
            sfAssign(sprite, folder.id);
            pageToast(t('sfCreated'));
        }});
        var r = anchor.getBoundingClientRect();
        sfMenu(r.left, r.bottom + 4, rows);
    }
    function sfDecorate () {
        sfTiles().forEach(function (tile) {
            if (tile.querySelector('.gwt-sf-tile')) return;
            var b = document.createElement('button');
            b.type = 'button';
            b.className = 'gwt-sf-tile';
            b.title = t('sfMove');
            b.setAttribute('aria-label', t('sfMove'));
            b.innerHTML = SF_FOLDER;
            listen(b, 'click', function (event) {
                event.preventDefault();
                event.stopPropagation();
                sfSpriteMenu(tile, b);
            });
            listen(b, 'pointerdown', function (event) { event.stopPropagation(); });
            listen(b, 'dblclick', function (event) { event.stopPropagation(); });
            tile.appendChild(b);
        });
    }
    function sfApplyFilter (sprites) {
        var byName = {};
        sprites.forEach(function (s) { byName[s.name] = s; });
        sfTiles().forEach(function (tile) {
            var sprite = byName[sfTileName(tile)];
            var show = !sf.active || (sprite && sfFolderOf(sprite) === sf.active);
            var wrap = tile.parentElement;
            if (!wrap) return;
            if (show) {
                if (wrap.style.display === 'none') wrap.style.display = '';
            } else if (wrap.style.display !== 'none') {
                wrap.style.display = 'none';
            }
        });
    }
    function sfWatch (items) {
        if (!window.MutationObserver) return;
        if (sf.obs && sf.obsItems === items) return;
        if (sf.obs) sf.obs.disconnect();
        var obs = new MutationObserver(function () {
            if (sf.raf) return;
            sf.raf = window.requestAnimationFrame(function () {
                sf.raf = 0;
                sfSync();
            });
        });
        obs.observe(items, {childList: true, subtree: true, characterData: true});
        sf.obs = obs;
        sf.obsItems = items;
    }
    function sfSync () {
        
        var scroll = document.querySelector('[class*="sprite-selector_scroll-wrapper"]');
        var items = scroll && scroll.querySelector('[class*="sprite-selector_items-wrapper"]');
        if (!scroll || !items) { sfTeardown(); return; }
        ensureStyle();
        readLocale();
        var sprites = sfSprites();
        sfLink(sprites);
        if (sf.active && sf.count && sprites.length > sf.count) sf.active = '';
        sf.count = sprites.length;
        sfEnsureBar(scroll);
        var surface = paintedColor(sfPane() || document.body, 'backgroundColor') || '';
        if (surface !== sf.theme) {
            sf.theme = surface;
            sfPaint(sf.bar);
            if (sf.hint) sfPaint(sf.hint);
            if (sf.menu) sfPaint(sf.menu);
        }
        sfRenderBar(sprites);
        sfDecorate();
        sfApplyFilter(sprites);

        var hint = sfEnsureHint(scroll);
        var inFolder = 0;
        if (sf.active) {
            sprites.forEach(function (s) { if (sfFolderOf(s) === sf.active) inFolder++; });
        }
        if (sf.active && !inFolder && sfFolderName(sf.active)) {
            hint.textContent = t('sfEmpty');
            hint.style.display = '';
        } else {
            hint.style.display = 'none';
        }
        sfWatch(items);
    }
    function sfTeardown () {
        if (!sf.mounted) return;
        sf.mounted = false;
        if (sf.obs) {
            sf.obs.disconnect();
            sf.obs = null;
            sf.obsItems = null;
        }
        if (sf.raf) {
            window.cancelAnimationFrame(sf.raf);
            sf.raf = 0;
        }
        if (sf.bar) { sf.bar.remove(); sf.bar = null; }
        if (sf.hint) { sf.hint.remove(); sf.hint = null; }
        sfCloseMenu();
        sf.editing = false;
        sf.sig = '';
        sf.shown = '';
        sf.active = '';
        sf.count = 0;
        sf.theme = '';
        Array.prototype.forEach.call(document.querySelectorAll('.gwt-sf-tile'), function (b) { b.remove(); });
        var scroll = document.querySelector('[class*="sprite-selector_scroll-wrapper"]');
        if (!scroll) return;
        Array.prototype.forEach.call(scroll.querySelectorAll('[class*="sprite-selector_sprite-wrapper"]'), function (w) {
            if (w.style.display === 'none') w.style.display = '';
        });
    }

    listen(document, 'pointerdown', function (event) {
        if (sf.menu && !sf.menu.contains(event.target)) sfCloseMenu();
    }, true);
    listen(window, 'resize', sfCloseMenu);
    listen(window, 'scroll', sfCloseMenu, true);
    listen(document, 'keydown', function (event) {
        if (event.key === 'Escape') sfCloseMenu();
    }, true);

    function tick () {
        try {
            ensureStyle();
            sfSync();
            if (!paintEditorOpen()) {
                closePanel();
                closePixelEditor();
                pen.deactivate();
                shaper.disarm();
                destroyRail();
                if (boardBtn) { boardBtn.remove(); boardBtn = null; }
                if (pxBtn) { pxBtn.remove(); pxBtn = null; }
                return;
            }
            mount();
            sweepGhosts();
            syncPixelButton();
            if (pxEd) {
                var nowTarget = anchorTarget();
                var nowCostume = nowTarget && nowTarget.getCostumes()[nowTarget.currentCostume];
                if (!nowCostume || nowCostume.name !== pxEd.costume) closePixelEditor();
            }
            syncGradStrip();
            syncRail();
            syncToolButtons();
            syncBoardButton();
            var before = lang;
            readLocale();
            if (before !== lang) {
                refreshToolLabels();
                closePanel();
            } else if (openPanel && !openAnchor.isConnected) {
                closePanel();
            }
            if (openPanel && openPanel._gwtKind === 'fx') markFx();
            else if (openPanel && openPanel._gwtKind === 'anchor') markAnchor();
        } catch (e) {
            if (window.console) window.console.warn('GoWarp Toolkit:', e);
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
            } catch (e) { /* fall through to the <style> tag */ }
        }
        styleTag = document.createElement('style') as HTMLStyleElement;
        var tag = styleTag;
        tag.textContent = CSS;
        document.head.appendChild(tag);
        styleInjected = true;
    }

    listen(window, 'keydown', onKeyDown);
    listen(window, 'resize', function () {
        if (openPanel) fitPanel(openPanel);
    });
    listen(window, 'pointerdown', function (e) {
        if (openPanel && openPanel._gwtKind === 'nib' &&
            /paper-canvas/.test(String(e.target && (e.target as Element).className))) closePanel();
        refreshPanels();
    }, true);
    listen(window, 'keyup', refreshPanels, true);


    readLocale();
    tick();
    repeat(tick, 900);
    return {
        openBoard: () => { readLocale(); openBoard(); },
        openPixelEditor: () => { readLocale(); openPixelEditor(); },
        dispose() {
            if (disposed) return;
            disposed = true;
            listeners.abort();
            timers.forEach(id => globalThis.clearTimeout(id));
            intervals.forEach(id => globalThis.clearInterval(id));
            unsubscribe?.();
            if (storeRef) delete storeRef.__gwtWatch;
            delete document.__gwtGrad;
            closePanel(); closePixelEditor(); boardClose();
            pen.deactivate(); shaper.disarm(); destroyRail(); dropGradStrip(); sfTeardown();
            if (boardData) {
                try { localStorage.setItem(BOARD_KEY + ':' + boardProjectKey(), JSON.stringify(boardData)); } catch {}
            }
            document.querySelectorAll('.gwt-tool, .gwt-toast').forEach((element: Element) => element.remove());
            boardBtn?.remove(); pxBtn?.remove();
            document.querySelectorAll('[data-gwt-mounted], [data-gwt-wheel]').forEach((element: HTMLElement) => {
                delete element.dataset.gwtMounted; delete element.dataset.gwtWheel;
            });
            styleTag?.remove();
            if (sheet) document.adoptedStyleSheets = document.adoptedStyleSheets.filter(item => item !== sheet);
        }
    };
}
