import type {EditorDocument, EditorWindow} from './type';

export function createEditorAccess(document: EditorDocument, window: EditorWindow) {
    var paperRef = null;
    var storeRef = null;

    function isPaperScope(candidate: any) {
        if(!candidate || typeof candidate.setup !== 'function') return false;
        var project = candidate.project;
        return !!(project && project.view && project.layers && project.layers.length);
    }
    function findPaperInBundle() {
        var chunks = [window.webpackChunkGUI, window.webpackJsonpGUI];
        for(let c=0; c < chunks.length; c++) {
            var chunk = chunks[c];
            if(!chunk || typeof chunk.push !== 'function') continue;
            var req = null;
            try {
                chunk.push([[Date.now()], {}, function (r: any) { req = r; }]);
            } catch (e) {
                continue;
            }
            if(!req || !req.m) continue;
            for(var id in req.m) {
                if(!Object.prototype.hasOwnProperty.call(req.m, id)) continue;
                var src = '';
                try {
                    src = String(req.m[id]);
                } catch (e) {
                    continue;
                }
                if(src.indexOf('PaperScope') < 0) continue;
                try {
                    var mod = req(id);
                    if(isPaperScope(mod)) return mod;
                } catch (e) {
                }
            }
        }
        return null;
    }
    function paper () {
        if(isPaperScope(paperRef)) return paperRef;
        var candidate = (window.paper && window.paper.project) ? window.paper : findPaperInBundle();
        paperRef = isPaperScope(candidate) ? candidate : null;
        return paperRef;
    }

    function findStoreInTree () {
        var canvas = paintCanvas();
        var fiber = canvas ? fiberOf(canvas) : null;
        for(let i=0; i < 60 && fiber; i++) {
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
    function dispatch (action: {type: string; [key: string]: unknown}) {
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
    return {
        paintCanvas,
        fiberOf,
        paper,
        paintState,
        isBitmapFormat,
        isEraserMode,
        dispatch,
        store,
        get storeRef() { return storeRef; },
    };
}

function fiberOf(el: Element) {
    var key = Object.keys(el).find(function (k: string) {
        return k.indexOf('__reactInternalInstance$') === 0 || k.indexOf('__reactFiber$') === 0;
    });
    return key ? (el as Element & Record<string, any>)[key] : null;
}

function paintCanvas() {
    return document.querySelector('canvas[class*="paper-canvas"]');
}