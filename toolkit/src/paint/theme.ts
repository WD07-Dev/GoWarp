import type { EditorDocument, EditorWindow, PaintElement, RGBA } from './type';

export function createTheme(document: EditorDocument, window: EditorWindow) {
    function parseColor(input: string) {
        var s = String(input || '').trim();
        if(!s) return null;
        var hex = s.match(/^#([0-9a-f]{3,8})$/i);
        if(hex) {
            var h = hex[1];
            if(h.length === 3 || h.length === 4) {
                h = h.split('').map(function (c: string) { return c + c; }).join('');
            }
            if(h.length < 6) return null;
            return {
                r: parseInt(h.slice(0, 2), 16),
                g: parseInt(h.slice(2, 4), 16),
                b: parseInt(h.slice(4, 6), 16),
                a: h.length >= 8 ? parseInt(h.slice(6, 8), 16) / 255 : 1
            };
        }
        var rgb = s.match(/rgba?\(([^)]+)\)/i);
        if(rgb) {
            var parts = rgb[1].split(/[\s,/]+/).map(Number);
            return {r: parts[0] || 0, g: parts[1] || 0, b: parts[2] || 0, a: parts.length > 3 ? parts[3] : 1};
        }
        return null;
    }
    function rgba(c: RGBA, a: number) {
        return 'rgba(' + c.r + ',' + c.g + ',' + c.b + ',' + a + ')';
    }
    function brightness(c: RGBA) {
        return (0.299 * c.r + 0.587 * c.g + 0.114 * c.b) / 255;
    }
    function cssVar(cs: CSSStyleDeclaration, name: string) {
        var v = cs.getPropertyValue(name);
        return v ? v.trim() : '';
    }
    function paintedColor(node: Element, property: 'color' | 'backgroundColor') {
        for(let hops=0; hops < 8 && node; hops++) {
            var value = window.getComputedStyle(node)[property];
            var parsed = parseColor(value);
            if (parsed && parsed.a > 0.05) return value;
            node = node.parentElement;
        }
        return null;
    }
    var hashedClassCache = {};
    function nativeClass(fragment: string) {
        if(hashedClassCache[fragment]) return hashedClassCache[fragment];
        var host = document.querySelector('[class*="' + fragment + '"]');
        if(host) {
            var live = Array.prototype.filter.call(host.classList, function (c: string) {
                return c.indexOf(fragment) >= 0;
            })[0];
            if (live) {
                hashedClassCache[fragment] = live;
                return live;
            }
        }
        for(let i=0; i < document.styleSheets.length; i++) {
            var rules;
            try {
                rules = document.styleSheets[i].cssRules;
            } catch (e) { continue; }
            for(let r=0; r < rules.length; r++) {
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

    function readTheme() {
        var host = document.querySelector('[class*="paint-editor_mode-selector"]') || document.body;
        var cs = window.getComputedStyle(host);
        var selected = document.querySelector('[class*="tool-select-base_is-selected"]');
        var field = document.querySelector('input[class*="input_input"]');
        var fieldCs = field ? window.getComputedStyle(field) : null;

        var raw = {
            surface: cssVar(cs, '--paint-popover-background') || paintedColor(host, 'backgroundColor'),
            accent: (selected ? window.getComputedStyle(selected).backgroundColor : '') || cssVar(cs, '--paint-looks-secondary'),
            text: cssVar(cs, '--paint-text-primary') || paintedColor(host, 'color'),
            field: cssVar(cs, '--paint-input-background') || (fieldCs ? fieldCs.backgroundColor : ''),
            border: cssVar(cs, '--paint-form-border') || (fieldCs ? fieldCs.borderColor : ''),
            tint: cssVar(cs, '--paint-looks-transparent')
        };
        var surface = parseColor(raw.surface) || {r: 255, g: 255, b: 255, a: 1};
        var accent = parseColor(raw.accent) || {r: 133, g: 92, b: 214, a: 1};
        var dark = brightness(surface) < 0.55;
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
    function applyTheme(panel: PaintElement, theme: ReturnType<typeof readTheme>) {
        var set = function (name: string, value: string) {
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
    return {
        nativeClass,
        readTheme,
        applyTheme,
        paintedColor,
        parseColor,
        brightness,
        cssVar,
        rgba,
    };
}