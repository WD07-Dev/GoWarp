import {tr} from '../translate';
import type {PaintContext} from './context';

interface LayerServices {
    paintingLayer: () => any;
    selectedPaths: () => any[];
    commit: () => boolean;
    eachSelected: (fn: any, live?: any) => boolean;
    icon: (paths: any) => string;
    paintEditorOpen: () => boolean;
    colorSig: (color: any) => any;
}

export function createLayers(context: PaintContext, services: LayerServices) {
    const {paintState, paper, isBitmapFormat} = context.editor;
    const {document, window} = context;
    const {listen} = context.lifecycle;
    const {applyTheme, readTheme} = context.theme;
    const {paintingLayer, selectedPaths, commit, eachSelected, icon, paintEditorOpen, colorSig} = services;

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
        if (cls === 'Group') return tr('group');
        if (cls === 'PointText' || cls === 'Text') return tr('text');
        if (cls === 'Raster') return tr('raster');
        if (cls === 'CompoundPath') return tr('compound');
        return tr('path');
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
            tr(item.visible ? 'hide' : 'show'));
        listen(eye, 'click', function () {
            toggleObject(item, 'visible');
            markObjects();
        });
        var lock = miniButton(item.locked ? OBJ_GLYPH.lock : OBJ_GLYPH.unlock,
            tr(item.locked ? 'unlock' : 'lock'));
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
        rail.setAttribute('dir', tr().isRtl ? 'rtl' : 'ltr');
        rail.setAttribute('aria-label', tr('objects'));

        var spine = document.createElement('div');
        spine.className = 'gwt-spine';
        spine.setAttribute('aria-hidden', 'true');
        rail.appendChild(spine);

        var head = document.createElement('div');
        head.className = 'gwt-rhead';
        var title = document.createElement('span');
        title.textContent = tr('objects');
        var count = document.createElement('span');
        count.className = 'gwt-count';
        head.appendChild(title);
        head.appendChild(count);
        var pin = miniButton(OBJ_GLYPH.pin, tr('pin'));
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
                ? tr('delete') : tr(row[1]));
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
            var b = miniButton(row[2], tr(row[1]));
            b.dataset.align = String(row[0]);
            listen(b, 'click', function () {
                alignObjects(row[0]);
                markObjects();
            });
            aligns.push(b);
        });
        rail.appendChild(railRow(aligns));

        var last = [];
        ([[-90, 'rotateLeft', OBJ_GLYPH.rotL], [90, 'rotateRight', OBJ_GLYPH.rotR]] as const).forEach(function (row) {
            var b = miniButton(row[2], tr(row[1]));
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
        nameInput.setAttribute('aria-label', tr('name'));
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
        opRange.setAttribute('aria-label', tr('opacity'));
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
    return {
        markObjects,
        syncRail,
        destroyRail,
        get railEl() { return railEl; },
    };
}
