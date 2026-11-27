import type { PaintContext, PixelEditor, PixelSource, PixelBox, PixelGroup, PixelVectorPath, PixelServices } from './type';
import { tr } from '../translate';

export function createPixelEditor(context: PaintContext, services: PixelServices) {
    const { document, window } = context;
    const { later, listen, repeat } = context.lifecycle;
    const { isBitmapFormat, store, dispatch, paper } = context.editor;
    const { readTheme, applyTheme } = context.theme;
    const { anchorTarget, rasterItem, commit, paintEditorOpen, icon, clamp, paintingLayer, findLayer, refreshAnchorView, GLYPH } = services;

    var PX_MAX_SIDE = 1024;
    var pxEd = null;
    var pxBtn = null;
    var pageToastEl = null;
    var pageToastTimer = 0;

    function pageToast(text: string, bad?: boolean) {
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

    function pxNativeButton() {
        var all = document.querySelectorAll('[class*="paint-editor_bitmap-button"]');
        for (var i = 0; i < all.length; i++) {
            var node = all[i], cls = String(node.className || '');
            if (node.classList.contains('gwt-px-btn') || /-icon/.test(cls)) continue;
            return node.tagName === 'BUTTON' ? node : (node.querySelector('button') || node);
        }
        return null;
    }

    function pxStrokeWidth(d: Uint8ClampedArray, w: number, minX: number, minY: number, maxX: number, maxY: number) {
        var gaps = [];
        var strong = function (i: number, j: number) {
            return Math.max(Math.abs(d[i] - d[j]), Math.abs(d[i + 1] - d[j + 1]), Math.abs(d[i + 2] - d[j + 2])) >= 64 && d[i + 3] > 20 && d[j + 3] > 20;
        };
        for(var y = minY; y <= maxY; y++) {
            var last = -99;
            for (var x = minX; x < maxX; x++) {
                var i = (y * w + x) * 4;
                if (!strong(i, i + 4)) continue;
                var g = x - last;
                if (g >= 2 && g <= 32) gaps.push(g);
                last = x;
            }
        }
        for(var xx = minX; xx <= maxX; xx++) {
            var lastY = -99;
            for(var yy = minY; yy < maxY; yy++) {
                var j = (yy * w + xx) * 4;
                if (!strong(j, j + w * 4)) continue;
                var gy = yy - lastY;
                if (gy >= 2 && gy <= 32) gaps.push(gy);
                lastY = yy;
            }
        }
        if(gaps.length < 8) return 8;
        gaps.sort(function (a: number, b: number) { return a - b; });
        return gaps[Math.floor(gaps.length * 0.25)];
    }

    function snapBitmapPixels () {
        var t0 = anchorTarget();
        var raster = rasterItem();
        if(!t0 || !raster || !raster.canvas) return null;
        var cv = raster.canvas, w = cv.width, h = cv.height;
        if(!w || !h) return null;
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
        for(var y = 0; y < h; y++) {
            for(var x = 0; x < w; x++) {
                var i0 = (y * w + x) * 4;
                if(!d[i0 + 3]) continue;
                if(x < minX) minX = x;
                if(x > maxX) maxX = x;
                if(y < minY) minY = y;
                if(y > maxY) maxY = y;
                var fk = (d[i0] << 16) | (d[i0 + 1] << 8) | d[i0 + 2];
                freq[fk] = (freq[fk] || 0) + 1;
            }
        }
        if (maxX < 0) return null;

        var stroke = pxStrokeWidth(d, w, minX, minY, maxX, maxY);
        var res = stroke >= 5 ? 2 : 1;
        var ratio = res === 1 ? '1:1' : '2x2';
        var changed = 0;

        if(res === 1) {
            changed = pxFixAlpha(img);
            changed += pxHardColors(img, freq);
        }else {
            minX -= minX % res;
            minY -= minY % res;
            maxX = Math.min(w - 1, maxX + (res - 1 - (maxX % res)));
            maxY = Math.min(h - 1, maxY + (res - 1 - (maxY % res)));
            for(var by = minY; by <= maxY; by += res) {
                for(var bx = minX; bx <= maxX; bx += res) {
                    var buckets = {}, bucketList = [];
                    for(var yy = by; yy < Math.min(by + res, h); yy++) {
                        for(var xx = bx; xx < Math.min(bx + res, w); xx++) {
                            var bi = (yy * w + xx) * 4;
                            if(!d[bi + 3]) continue;
                            var bk = ((d[bi] >> 3) << 10) | ((d[bi + 1] >> 3) << 5) | (d[bi + 2] >> 3);
                            var bucket = buckets[bk];
                            if(!bucket) {
                                bucket = buckets[bk] = {n: 0, i: bi, a: 0};
                                bucketList.push(bucket);
                            }
                            bucket.n++;
                            if (d[bi + 3] > bucket.a) { bucket.a = d[bi + 3]; bucket.i = bi; }
                        }
                    }
                    bucketList.sort(function (a: {n: number; i: number; a: number}, b: {n: number; i: number; a: number}) { return b.n - a.n; });
                    var best = bucketList.length ? bucketList[0].i : -1;
                    var bestA = bucketList.length ? bucketList[0].a : -1;
                    if (bucketList.length > 1 && best >= 0) {
                        var second = bucketList[1], total = 0;
                        bucketList.forEach(function (b: {n: number; i: number; a: number}) { total += b.n; });
                        var lum = function (i: number) { return d[i] * .299 + d[i + 1] * .587 + d[i + 2] * .114; };
                        var contrast = Math.max(Math.abs(d[best] - d[second.i]),
                            Math.abs(d[best + 1] - d[second.i + 1]), Math.abs(d[best + 2] - d[second.i + 2]));
                        if (second.n <= total * .55 && contrast >= 96 && lum(second.i) + 16 < lum(best)) {
                            best = second.i;
                        }
                    }
                    var solid = bestA > 20;
                    var r = solid ? d[best] : 0, g = solid ? d[best + 1] : 0, b = solid ? d[best + 2] : 0;
                    var alpha = solid ? 255 : 0;
                    for(yy = by; yy < Math.min(by + res, h); yy++) {
                        for(xx = bx; xx < Math.min(bx + res, w); xx++) {
                            var j = (yy * w + xx) * 4;
                            if (d[j] === r && d[j + 1] === g && d[j + 2] === b && d[j + 3] === alpha) continue;
                            d[j] = r; d[j + 1] = g; d[j + 2] = b; d[j + 3] = alpha;
                            changed++;
                        }
                    }
                }
            }
        }
        if(changed) {
            ctx.putImageData(img, 0, 0);
            commit();
        }
        return {changed: changed, ratio: ratio};
    }

    function waitForBitmap (tries: number) {
        var raster = rasterItem();
        if(!isBitmapFormat() || !raster || !raster.canvas || !raster.canvas.width) {
            if (tries < 80) later(function () { waitForBitmap(tries + 1); }, 100);
            return;
        }
        later(() => {
            if(!isBitmapFormat()) return;
            var snap = snapBitmapPixels();
            if(snap && snap.changed) pageToast(tr('bitmapSnapped').replace('{ratio}', snap.ratio));
        }, 160);
    }

    function hookConversion(native: Element & {_gwtSnap?: boolean}) {
        if(!native || native._gwtSnap) return;
        native._gwtSnap = true;
        listen(native, 'click', function () {
            if (isBitmapFormat()) return;
            later(function () { waitForBitmap(0); }, 120);
        });
    }

    function syncPixelButton() {
        var native = pxNativeButton();
        if(!paintEditorOpen() || !native) {
            if (pxBtn) { pxBtn.remove(); pxBtn = null; }
            return;
        }
        if(!pxBtn || !pxBtn.isConnected) {
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
            listen(pxBtn, 'keydown', function (e: KeyboardEvent) {
                if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    openPixelEditor();
                }
            });
            native.parentElement.insertBefore(pxBtn, native);
        }
        hookConversion(native);
        var label = pxBtn.querySelector('span');
        if(label) label.textContent = tr('pixelOpen');
        pxBtn.title = tr('pixelArt');
        pxBtn.setAttribute('aria-label', tr('pixelArt'));
        pxBtn.dataset.gwtPixel = String('1');
    }

    function ensureVectorCostume () {
        if(!isBitmapFormat()) return Promise.resolve(true);
        var native = pxNativeButton();
        if(!native) return Promise.resolve(false);
        (native as HTMLElement).click();
        var tries = 0;
        return new Promise<boolean>(function (res: (value: boolean | PromiseLike<boolean>) => void) {
            var iv = repeat(function () {
                tries++;
                if(!isBitmapFormat()) {
                    window.clearInterval(iv);
                    res(true);
                }else if(tries > 60) {
                    window.clearInterval(iv);
                    res(false);
                }
            }, 100);
        });
    }

    function rasterizeCostume() {
        var target = anchorTarget();
        if(!target) return Promise.resolve(null);
        var costume = target.getCostumes()[target.currentCostume];
        if(!costume) return Promise.resolve(null);
        var raw = costume.asset && (costume.asset.data !== undefined ? costume.asset.data : costume.asset);
        var txt = typeof raw === 'string' ? raw : (raw ? new TextDecoder().decode(raw) : '');
        if(!txt) return Promise.resolve(null);
        var w = Number((/\swidth="([\d.]+)"/.exec(txt) || [])[1]) || 0;
        var h = Number((/\sheight="([\d.]+)"/.exec(txt) || [])[1]) || 0;
        var s = Math.min(1, PX_MAX_SIDE / Math.max(w, h, 1));
        return new Promise<PixelSource>(function (res: (value: PixelSource | PromiseLike<PixelSource>) => void) {
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
                res({
                    canvas: cv, ctx: ctx, W: W, H: H, s: s, target: target, costume: costume,
                    costumeW: w || (costume.size && costume.size[0]) || W,
                    costumeH: h || (costume.size && costume.size[1]) || H
                });
            };
            img.onerror = () => {
                res(null);
            };
            img.src = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(txt);
        });
    }

    function pxBounds(data: Uint8ClampedArray, w: number, h: number) {
        var minX = w, minY = h, maxX = -1, maxY = -1;
        for(var y = 0; y < h; y++) {
            for(var x = 0; x < w; x++) {
                if(!data[(y * w + x) * 4 + 3]) continue;
                if(x < minX) minX = x;
                if(x > maxX) maxX = x;
                if(y < minY) minY = y;
                if(y > maxY) maxY = y;
            }
        }
        return maxX < 0 ? null : {x: minX, y: minY, w: maxX - minX + 1, h: maxY - minY + 1};
    }

    function pxFixAlpha(img: ImageData) {
        var d = img.data, w = img.width, h = img.height;
        var before = new Uint8Array(w * h);
        var semi = [];
        var p, i;
        for(p = 0; p < w * h; p++) before[p] = d[p * 4 + 3];
        for(p = 0; p < w * h; p++) {
            var a = before[p];
            if(!a) continue;
            if(a <= 20) {
                d[p * 4 + 3] = 0;
            }else {
                d[p * 4 + 3] = 255;
                if (a < 235) semi.push(p);
            }
        }
        for(var y = 0; y < h; y++) {
            for(var x = 0; x < w; x++) {
                var k = y * w + x;
                var pa = before[k];
                if(!pa || pa >= 200) continue;
                var empty = 0;
                if(x === 0 || !before[k - 1]) empty++;
                if(x === w - 1 || !before[k + 1]) empty++;
                if(y === 0 || !before[k - w]) empty++;
                if(y === h - 1 || !before[k + w]) empty++;
                if(empty >= 2) d[k * 4 + 3] = 0;
            }
        }
        var R1 = [-1, 0, 1, -1, 1, -1, 0, 1];
        var C1 = [-1, -1, -1, 0, 0, 1, 1, 1];
        for(i = 0; i < semi.length; i++) {
            p = semi[i];
            if(!d[p * 4 + 3]) continue;
            var sx = p % w, sy = (p - sx) / w;
            var found = -1;
            for(var radius = 1; radius <= 2 && found < 0; radius++) {
                for(var q = 0; q < 8; q++) {
                    var nx = sx + R1[q] * radius, ny = sy + C1[q] * radius;
                    if (nx < 0 || ny < 0 || nx >= w || ny >= h) continue;
                    var nk = ny * w + nx;
                    if (before[nk] >= 235 && d[nk * 4 + 3]) { found = nk; break; }
                }
            }
            if(found >= 0) {
                d[p * 4] = d[found * 4];
                d[p * 4 + 1] = d[found * 4 + 1];
                d[p * 4 + 2] = d[found * 4 + 2];
            }
        }
        var changed = 0;
        for(p = 0; p < w * h; p++) {
            if (before[p] !== d[p * 4 + 3]) changed++;
        }
        return changed;
    }

    function pxNearColor(d: Uint8ClampedArray, j: number, r: number, g: number, b: number) {
        return d[j + 3] === 255 && Math.abs(d[j] - r) <= 12 && Math.abs(d[j + 1] - g) <= 12 && Math.abs(d[j + 2] - b) <= 12;
    }

    function pxHardColors(img: ImageData, freq: Record<number, number>) {
        var d = img.data, w = img.width, h = img.height, n = w * h;
        var flat = {}, x, y, i, key;
        for(y = 1; y < h - 1; y++) {
            for(x = 1; x < w - 1; x++) {
                i = (y * w + x) * 4;
                if(d[i + 3] !== 255) continue;
                var r = d[i], g = d[i + 1], b = d[i + 2];
                if(!pxNearColor(d, i - 4, r, g, b) || !pxNearColor(d, i + 4, r, g, b) || !pxNearColor(d, i - w * 4, r, g, b) || !pxNearColor(d, i + w * 4, r, g, b)) continue;
                key = (r << 16) | (g << 8) | b;
                flat[key] = (flat[key] || 0) + 1;
            }
        }
        var list = [];
        for(key in flat) {
            if (flat[key] >= 12 && freq[key]) list.push({c: Number(key), n: freq[key]});
        }
        if(list.length < 2) {
            for (key in freq) if (freq[key] >= 12) list.push({c: Number(key), n: freq[key]});
        }
        if(list.length < 2) return 0;
        list.sort(function (a: {c: number; n: number}, b: {c: number; n: number}) { return b.n - a.n; });
        list = list.slice(0, 24);
        var pal = [], q;
        for(q = 0; q < list.length; q++) {
            pal.push({r: (list[q].c >> 16) & 255, g: (list[q].c >> 8) & 255, b: list[q].c & 255});
        }
        var changed = 0, cache = {};
        for(var p = 0; p < n; p++) {
            i = p * 4;
            if (!d[i + 3]) continue;
            r = d[i]; g = d[i + 1]; b = d[i + 2];
            key = (r << 16) | (g << 8) | b;
            var choice = cache[key];
            if(choice === undefined) {
                var best = -1, second = -1, d1 = Infinity, d2 = Infinity;
                for(q = 0; q < pal.length; q++) {
                    var P = pal[q];
                    var dd = (r - P.r) * (r - P.r) + (g - P.g) * (g - P.g) + (b - P.b) * (b - P.b);
                    if (dd < d1) { d2 = d1; second = best; d1 = dd; best = q; }
                    else if (dd < d2) { d2 = dd; second = q; }
                }
                var pick = best;
                if(best === 0 && second > 0) {
                    var A = pal[0], B = pal[second];
                    var ux = B.r - A.r, uy = B.g - A.g, uz = B.b - A.b;
                    var len = ux * ux + uy * uy + uz * uz;
                    if (len > 900) {
                        var tt = ((r - A.r) * ux + (g - A.g) * uy + (b - A.b) * uz) / len;
                        var ox = r - (A.r + tt * ux), oy = g - (A.g + tt * uy), oz = b - (A.b + tt * uz);
                        if(tt >= 0.35 && ox * ox + oy * oy + oz * oz < len * 0.05) pick = second;
                    }
                }
                choice = (best < 0 || d1 === 0) ? -1 : pick;
                cache[key] = choice;
            }
            if(choice < 0) continue;
            var C = pal[choice];
            if(r !== C.r || g !== C.g || b !== C.b) {
                d[i] = C.r; d[i + 1] = C.g; d[i + 2] = C.b; d[i + 3] = 255;
                changed++;
            }
        }
        return changed;
    }

    function pxHasArt(img: ImageData) {
        var d = img.data;
        for (var i = 3; i < d.length; i += 4) if (d[i]) return true;
        return false;
    }

    function pxPalette(img: ImageData, count: number) {
        var d = img.data, hits = {}, out = [];
        for(let i=0; i < d.length; i += 40) {
            if (d[i + 3] < 64) continue;
            var key = (d[i] >> 4) + '.' + (d[i + 1] >> 4) + '.' + (d[i + 2] >> 4);
            var hit = hits[key] || (hits[key] = {n: 0, r: d[i], g: d[i + 1], b: d[i + 2]});
            hit.n++;
        }
        Object.keys(hits).sort(function (a: string, b: string) { return hits[b].n - hits[a].n; }).slice(0, count)
        .forEach((k: string) => {
            var h = hits[k];
            var hex = '#' + [h.r, h.g, h.b].map(function (v: number) {
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
        '<rect x="7.3" y="7.3" width="5.4" height="5.4" rx=".4"/></g></svg>'
    );
        
    var PX_ICONS = {
        t_brush: icon('<path d="M4.5 15.6c1.4-4.7 5.4-9.1 11.1-10.1-1.3 5.7-5.7 9.5-11.1 10.1z"/>' + '<path d="M4.5 15.6l2.5-2.5"/>'),
        t_pencil: icon('<path d="M4 16l1.7-4.5L13 4.2l2.8 2.8-7.3 7.3z"/><path d="M11.6 5.6l2.8 2.8"/>'),
        t_eraser: icon('<path d="M4.2 13.6l6.1-6.1 4.1 4.1-6.1 6.1H5.6z"/><path d="M9.4 16.1h6.2"/>'),
        t_fill: icon('<path d="M8.4 3.6l6.4 6.4-5.1 5.1a1.9 1.9 0 0 1-2.7 0L3.1 11.2z"/>' + '<path d="M4.4 10h9.4"/><path d="M16.2 12.9c.8 1.2 1.2 2 1.2 2.6a1.2 1.2 0 1 1-2.4 0c0-.6.4-1.4 1.2-2.6z"/>'),
        t_line: icon('<path d="M4 16L16 4"/>'),
        t_rect: icon('<rect x="4" y="5.5" width="12" height="9" rx=".6"/>'),
        t_ellipse: icon('<ellipse cx="10" cy="10" rx="6.2" ry="4.6"/>'),
        t_picker: icon('<path d="M13.4 3.4l3.2 3.2-8.8 8.8-3.8.7.7-3.8z"/><path d="M11.8 5l3.2 3.2"/>'),
        t_move: icon('<path d="M10 3v14M3 10h14M7.2 5.8L10 3l2.8 2.8M7.2 14.2L10 17l2.8-2.8"/>' + '<path d="M5.8 7.2L3 10l2.8 2.8M14.2 7.2L17 10l-2.8 2.8"/>')
    };
    var PX_TOOLS = [
        ['brush', 't_brush', 'brush'], ['pencil', 't_pencil', 'pencil'], ['eraser', 't_eraser', 'eraser'],
        ['fill', 't_fill', 'fill'], ['line', 't_line', 'line'], ['rect', 't_rect', 'rect'],
        ['ellipse', 't_ellipse', 'ellipse'], ['picker', 't_picker', 'picker'], ['move', 't_move', 'move']
    ];

    function pxSnapshot(ed: PixelEditor) {
        return new ImageData(new Uint8ClampedArray(ed.grid.data), ed.w, ed.h);
    }
    function pxPush(ed: PixelEditor) {
        ed.undo.push(pxSnapshot(ed));
        if(ed.undo.length > 40) ed.undo.shift();
        ed.redo = [];
        pxUpdateButtons(ed);
    }
    function pxUpdateButtons(ed: PixelEditor) {
        if(ed.btnUndo) ed.btnUndo.disabled = !ed.undo.length;
        if(ed.btnRedo) ed.btnRedo.disabled = !ed.redo.length;
    }
    function pxGet(ed: PixelEditor, x: number, y: number) {
        if(x < 0 || y < 0 || x >= ed.w || y >= ed.h) return null;
        var i = (y * ed.w + x) * 4, d = ed.grid.data;
        return [d[i], d[i + 1], d[i + 2], d[i + 3]];
    }
    function pxSet(ed: PixelEditor, x: number, y: number, c: number[]) {
        if(x < 0 || y < 0 || x >= ed.w || y >= ed.h) return;
        var i = (y * ed.w + x) * 4, d = ed.grid.data;
        d[i] = c[0]; d[i + 1] = c[1]; d[i + 2] = c[2]; d[i + 3] = c[3];
    }
    function pxSame(a: number[], b: number[]) {
        return a && b && a[0] === b[0] && a[1] === b[1] && a[2] === b[2] && a[3] === b[3];
    }
    function pxStamp(ed: PixelEditor, x: number, y: number, size: number, c: number[]) {
        var half = Math.floor((size - 1) / 2);
        for(var dy = 0; dy < size; dy++) {
            for(var dx = 0; dx < size; dx++) pxSet(ed, x - half + dx, y - half + dy, c);
        }
    }
    function pxLine (ed: PixelEditor, x0: number, y0: number, x1: number, y1: number, c: number[]) {
        var dx = Math.abs(x1 - x0), sx = x0 < x1 ? 1 : -1;
        var dy = -Math.abs(y1 - y0), sy = y0 < y1 ? 1 : -1;
        var err = dx + dy;
        for(;;) {
            pxStamp(ed, x0, y0, ed.size, c);
            if (x0 === x1 && y0 === y1) break;
            var e2 = 2 * err;
            if (e2 >= dy) { err += dy; x0 += sx; }
            if (e2 <= dx) { err += dx; y0 += sy; }
        }
    }
    function pxFlood(ed: PixelEditor, x: number, y: number, c: number[]) {
        var target = pxGet(ed, x, y);
        if(!target || pxSame(target, c)) return;
        var stack = [[x, y]], seen = new Uint8Array(ed.w * ed.h);
        while(stack.length) {
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
    function pxEllipse(ed: PixelEditor, x0: number, y0: number, x1: number, y1: number, c: number[], filled: boolean) {
        var left = Math.min(x0, x1), right = Math.max(x0, x1);
        var top = Math.min(y0, y1), bottom = Math.max(y0, y1);
        var cx = (left + right) / 2, cy = (top + bottom) / 2;
        var rx = Math.max(0.5, (right - left + 1) / 2), ry = Math.max(0.5, (bottom - top + 1) / 2);
        for(var y = top; y <= bottom; y++) {
            var dy = (y - cy) / ry;
            var span = Math.sqrt(Math.max(0, 1 - dy * dy));
            var xa = Math.round(cx - span * rx), xb = Math.round(cx + span * rx);
            if(filled) {
                for (var x = xa; x <= xb; x++) pxSet(ed, x, y, c);
            }else {
                pxSet(ed, xa, y, c);
                pxSet(ed, xb, y, c);
            }
        }
        if(!filled) {
            for(var ix = left; ix <= right; ix++) {
                var dx = (ix - cx) / rx;
                var sp = Math.sqrt(Math.max(0, 1 - dx * dx));
                pxSet(ed, ix, Math.round(cy - sp * ry), c);
                pxSet(ed, ix, Math.round(cy + sp * ry), c);
            }
        }
    }
    function pxBox(ed: PixelEditor, x0: number, y0: number, x1: number, y1: number, c: number[], filled: boolean) {
        var left = Math.min(x0, x1), right = Math.max(x0, x1);
        var top = Math.min(y0, y1), bottom = Math.max(y0, y1);
        for(var y = top; y <= bottom; y++) {
            for(var x = left; x <= right; x++) {
                if(filled || x === left || x === right || y === top || y === bottom) pxSet(ed, x, y, c);
            }
        }
    }

    function pxRender(ed: PixelEditor) {
        if(!ed || !ed.root.isConnected) return;
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
        if(ed.gridOn && zoom >= 4) {
            ctx.beginPath();
            ctx.strokeStyle = ed.dark ? 'rgba(255,255,255,.12)' : 'rgba(0,0,0,.08)';
            ctx.lineWidth = 1;
            for(var x = 0; x <= w; x++) {
                var vx = x * zoom + 0.5;
                ctx.moveTo(vx, 0);
                ctx.lineTo(vx, h * zoom);
            }
            for(var y = 0; y <= h; y++) {
                var vy = y * zoom + 0.5;
                ctx.moveTo(0, vy);
                ctx.lineTo(w * zoom, vy);
            }
            ctx.stroke();
        }
        if(ed.zoomOut) ed.zoomOut.textContent = zoom + '×';
    }

    function pxFit(ed: PixelEditor) {
        var stage = ed.stage.getBoundingClientRect();
        var fit = Math.floor(Math.min((stage.width - 30) / ed.w, (stage.height - 30) / ed.h));
        ed.zoom = Math.max(1, Math.min(32, fit || 1));
        pxRender(ed);
    }
    function pxZoom(ed: PixelEditor, dir: number) {
        var steps = [1, 2, 3, 4, 6, 8, 10, 12, 16, 20, 24, 32];
        var i = 0;
        while(i < steps.length - 2 && steps[i] < ed.zoom) i++;
        if(dir > 0) i = Math.min(steps.length - 1, i + 1);
        else i = Math.max(0, i - 1);
        ed.zoom = steps[i];
        pxRender(ed);
    }

    function pxPoint(ed: PixelEditor, e: MouseEvent) {
        var r = ed.canvas.getBoundingClientRect();
        var x = Math.floor((e.clientX - r.left) / ed.zoom);
        var y = Math.floor((e.clientY - r.top) / ed.zoom);
        if(x < 0 || y < 0 || x >= ed.w || y >= ed.h) return null;
        return {x: x, y: y};
    }

    function pxToolColor(ed: PixelEditor, tool: string) {
        if(tool === 'eraser') return [0, 0, 0, 0];
        return ed.rgba;
    }

    function pxDown(ed: PixelEditor, e: PointerEvent) {
        if(e.button !== 0 || ed.busy) return;
        var p = pxPoint(ed, e);
        if(!p) return;
        e.preventDefault();
        ed.canvas.setPointerCapture(e.pointerId);
        var tool = ed.tool;
        if(tool === 'picker') {
            var c = pxGet(ed, p.x, p.y);
            if(c && c[3]) {
                ed.color = '#' + [c[0], c[1], c[2]].map(function (v: number) {
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
        if(tool === 'move') {
            ed.gesture.box = pxBounds(ed.grid.data, ed.w, ed.h);
            ed.note.textContent = tr('pixelHelp');
            return;
        }
        var color = pxToolColor(ed, tool);
        if(tool === 'brush' || tool === 'pencil' || tool === 'eraser') {
            pxLine(ed, p.x, p.y, p.x, p.y, color);
        }else if(tool === 'fill') {
            pxFlood(ed, p.x, p.y, color);
        }
        pxRender(ed);
    }

    function pxMove(ed: PixelEditor, e: PointerEvent) {
        if(!ed.gesture) return;
        var p = pxPoint(ed, e);
        if(!p) return;
        var g = ed.gesture;
        var tool = g.tool;
        if(tool === 'move') {
            if(!g.box) return;
            var dx = p.x - g.start.x, dy = p.y - g.start.y;
            dx = clamp(dx, -g.box.x, ed.w - g.box.x - g.box.w);
            dy = clamp(dy, -g.box.y, ed.h - g.box.y - g.box.h);
            var out = new Uint8ClampedArray(ed.w * ed.h * 4);
            var src = g.base.data;
            for(let y = 0; y < ed.h; y++) {
                for(let x = 0; x < ed.w; x++) {
                    var i = (y * ed.w + x) * 4;
                    if(!src[i + 3]) continue;
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
        if(tool === 'brush' || tool === 'pencil' || tool === 'eraser') {
            pxLine(ed, g.last.x, g.last.y, p.x, p.y, color);
            g.last = p;
        }else if(tool === 'line' || tool === 'rect' || tool === 'ellipse') {
            ed.grid.data.set(g.base.data);
            if(tool === 'line') pxLine(ed, g.start.x, g.start.y, p.x, p.y, color);
            else if(tool === 'rect') pxBox(ed, g.start.x, g.start.y, p.x, p.y, color, ed.filled);
            else pxEllipse(ed, g.start.x, g.start.y, p.x, p.y, color, ed.filled);
            g.last = p;
        }
        pxRender(ed);
    }

    function pxUp(ed: PixelEditor) {
        if(!ed.gesture) return;
        var g = ed.gesture;
        ed.gesture = null;
        if(g.tool === 'move') return;
        if(g.tool === 'line' || g.tool === 'rect' || g.tool === 'ellipse') {
            pxRender(ed);
        }
        pxUpdateButtons(ed);
        ed.note.textContent = g.tool === 'brush' || g.tool === 'pencil' ? tr('pixelHelp') : ed.note.textContent;
    }

    function pxUndo(ed: PixelEditor) {
        if(!ed.undo.length) return;
        ed.redo.push(pxSnapshot(ed));
        ed.grid = ed.undo.pop();
        pxRender(ed);
        pxUpdateButtons(ed);
    }
    function pxRedo(ed: PixelEditor) {
        if(!ed.redo.length) return;
        ed.undo.push(pxSnapshot(ed));
        ed.grid = ed.redo.pop();
        pxRender(ed);
        pxUpdateButtons(ed);
    }
    function pxNudge(ed: PixelEditor, dx: number, dy: number) {
        var box = pxBounds(ed.grid.data, ed.w, ed.h);
        if(!box) return;
        dx = clamp(dx, -box.x, ed.w - box.x - box.w);
        dy = clamp(dy, -box.y, ed.h - box.y - box.h);
        if(!dx && !dy) return;
        pxPush(ed);
        var out = new Uint8ClampedArray(ed.w * ed.h * 4);
        var src = ed.grid.data;
        for(let y=0; y < ed.h; y++) {
            for(let x=0; x < ed.w; x++) {
                var i = (y * ed.w + x) * 4;
                if(!src[i + 3]) continue;
                var tx = x + dx, ty = y + dy;
                if(tx < 0 || ty < 0 || tx >= ed.w || ty >= ed.h) continue;
                var j = (ty * ed.w + tx) * 4;
                out[j] = src[i]; out[j + 1] = src[i + 1]; out[j + 2] = src[i + 2]; out[j + 3] = src[i + 3];
            }
        }
        ed.grid.data.set(out);
        pxRender(ed);
    }

    function pxVectorPaths (ed: PixelEditor) {
        var d = ed.grid.data, w = ed.w, h = ed.h;
        var groups: Record<string, PixelGroup> = {}, last: Record<string, PixelBox> = {}, rects = 0;
        for(let y=0; y < h; y++) {
            var x = 0;
            while(x < w) {
                var i = (y * w + x) * 4;
                var a = d[i + 3];
                if(!a) {
                    x++;
                    continue;
                }
                var r = d[i], g = d[i + 1], b = d[i + 2];
                var x2 = x + 1;
                while(x2 < w) {
                    var j = (y * w + x2) * 4;
                    if (d[j + 3] !== a || d[j] !== r || d[j + 1] !== g || d[j + 2] !== b) break;
                    x2++;
                }
                var key = r + ',' + g + ',' + b + ',' + a;
                var group = groups[key] || (groups[key] = {r: r, g: g, b: b, a: a, rects: []});
                var runKey = key + '|' + x + '|' + (x2 - x);
                var open = last[runKey];
                if(open && open.y + open.h === y) {
                    open.h++;
                }else {
                    open = {x: x, y: y, w: x2 - x, h: 1};
                    group.rects.push(open);
                    last[runKey] = open;
                    rects++;
                }
                x = x2;
            }
        }
        var unit = ed.unit, offX = ed.offX, offY = ed.offY, x0 = ed.x0, y0 = ed.y0;
        return {
            count: rects, paths: Object.keys(groups).map((key: string) => {
            var group = groups[key];
            var boxes = group.rects.map(function (rect: PixelBox) {
                return {
                    x: x0 + (offX + rect.x) * unit,
                    y: y0 + (offY + rect.y) * unit,
                    w: rect.w * unit,
                    h: rect.h * unit
                };
            });
            return {boxes: boxes, color: group, runs: group.rects.length};
        }).sort(function (a: PixelVectorPath, b: PixelVectorPath) { return a.runs - b.runs; })};
    }

    function pxClearSelection () {
        try {
            var state = store() && store().getState();
            var sel = state && state.scratchPaint && state.scratchPaint.selectedItems;
            if (sel && sel.length) {
                dispatch({type: 'scratch-paint/select/CHANGE_SELECTED_ITEMS', selectedItems: []});
            }
        } catch (e) {}
    }

    function pxApply () {
        var ed = pxEd;
        if(!ed) return;
        if(ed.autoFixInput.checked) pxFixAlpha(ed.grid);
        if(!pxHasArt(ed.grid)) {
            ed.note.textContent = tr('pixelEmpty');
            return;
        }
        var p = paper(), layer = paintingLayer();
        if(!p || !layer) {
            ed.note.textContent = tr('pxFail');
            return;
        }
        var vector = pxVectorPaths(ed);
        if(!vector.paths.length) {
            ed.note.textContent = tr('pixelEmpty');
            return;
        }
        if(vector.count > 30000) {
            ed.note.textContent = tr('pxHeavy');
            return;
        }
        ed.busy = true;
        pxClearSelection();
        layer.children.slice().forEach(function (child: any) {
            if(!child.guide) child.remove();
        });
        var raster = findLayer('isRasterLayer');
        if(raster) raster.removeChildren();
        var added = [];
        vector.paths.forEach(function (g: PixelVectorPath) {
            var kids = g.boxes.map(function (box: PixelBox) {
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
        if(!commit()) {
            added.forEach(function (a: any) { a.remove(); });
            ed.busy = false;
            ed.note.textContent = tr('pxFail');
            return;
        }
        var target = ed.target;
        closePixelEditor();
        refreshAnchorView(target);
        pageToast(tr('pxApplied'));
    }

    function closePixelEditor () {
        if(!pxEd) return;
        window.removeEventListener('keydown', pxEd.onKey, true);
        window.removeEventListener('resize', pxEd.onResize);
        pxEd.root.remove();
        pxEd = null;
    }

    function pxKeyDown (ed: PixelEditor, e: KeyboardEvent) {
        if(e.target && /INPUT|TEXTAREA|SELECT/.test((e.target as Element).tagName)) return;
        e.stopPropagation();
        if(e.ctrlKey || e.metaKey) {
            if(e.key.toLowerCase() === 'z') {
                e.preventDefault();
                if(e.shiftKey) pxRedo(ed); else pxUndo(ed);
            }
            return;
        }
        var k = e.key.toLowerCase();
        if(k === 'escape') {
            e.preventDefault();
            closePixelEditor();
            return;
        }
        var map = {b: 'brush', p: 'pencil', e: 'eraser', g: 'fill', l: 'line', r: 'rect', o: 'ellipse', i: 'picker', v: 'move'};
        if(map[k]) {
            e.preventDefault();
            ed.setTool(map[k]);
            return;
        }
        if(k === '[' || k === ']') {
            e.preventDefault();
            ed.size = clamp(ed.size + (k === ']' ? 1 : -1), 1, 8);
            ed.sizeInput.value = ed.size;
            ed.sizeOut.textContent = String(ed.size);
            return;
        }
        if(k === '+' || k === '=') {
            e.preventDefault();
            pxZoom(ed, 1);
            return;
        }
        if(k === '-') {
            e.preventDefault();
            pxZoom(ed, -1);
            return;
        }
        if(e.key.indexOf('Arrow') === 0) {
            e.preventDefault();
            var dx = e.key === 'ArrowLeft' ? -1 : e.key === 'ArrowRight' ? 1 : 0;
            var dy = e.key === 'ArrowUp' ? -1 : e.key === 'ArrowDown' ? 1 : 0;
            pxNudge(ed, dx, dy);
        }
    }

    function buildPixelEditor(src: PixelSource) {
        var bounds = pxBounds(src.ctx.getImageData(0, 0, src.W, src.H).data, src.W, src.H);
        var offX, offY, w, h, grid;
        if(!bounds) {
            w = 64; h = 64;
            offX = Math.round((src.W - w) / 2);
            offY = Math.round((src.H - h) / 2);
            grid = new ImageData(w, h);
        }else {
            offX = bounds.x; offY = bounds.y; w = bounds.w; h = bounds.h;
            grid = src.ctx.getImageData(bounds.x, bounds.y, w, h);
        }
        var theme = readTheme();
        var unit = 2 / src.s;
        var x0 = 480 - src.costumeW;
        var y0 = 360 - src.costumeH;
        var root = document.createElement('div');
        root.className = 'gwt-px';
        root.setAttribute('dir', tr().isRtl ? 'rtl' : 'ltr');
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
        title.textContent = tr('pixelArt');
        var sub = document.createElement('span');
        sub.className = 'gwt-px-sub';
        sub.textContent = tr('pixelSub') + ' · ' + w + '×' + h;
        var undo = document.createElement('button');
        undo.type = 'button';
        undo.className = 'gwt-px-icon';
        undo.title = tr('undo');
        undo.setAttribute('aria-label', tr('undo'));
        undo.innerHTML = GLYPH.undo;
        listen(undo, 'click', function () { pxUndo(ed); });
        var redo = document.createElement('button');
        redo.type = 'button';
        redo.className = 'gwt-px-icon';
        redo.title = tr('redo');
        redo.setAttribute('aria-label', tr('redo'));
        redo.innerHTML = icon('<path d="M16 8.5H7.5a4 4 0 1 0 0 8H13"/><path d="M16 8.5 13 5.5M16 8.5l-3 3"/>');
        listen(redo, 'click', function () { pxRedo(ed); });
        var close = document.createElement('button');
        close.type = 'button';
        close.className = 'gwt-px-icon gwt-px-close';
        close.setAttribute('aria-label', tr('close'));
        close.title = tr('close');
        close.textContent = '✕';
        listen(close, 'click', closePixelEditor);
        head.append(title, sub, undo, redo, close);

        var tools = document.createElement('div');
        tools.className = 'gwt-px-tools';
        var toolButtons = {};
        var setTool = function (id: string) {
            ed.tool = id;
            Object.keys(toolButtons).forEach(function (key: string) {
                toolButtons[key].classList.toggle('gwt-on', key === id);
            });
            ed.canvas.style.cursor = id === 'move' ? 'grab' : id === 'picker' ? 'copy' : 'crosshair';
        };
        ed.setTool = setTool;
        PX_TOOLS.forEach(function (row: string[]) {
            var b = document.createElement('button');
            b.type = 'button';
            b.className = 'gwt-px-tool';
            b.title = tr(row[2]);
            b.setAttribute('aria-label', tr(row[2]));
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
        listen(canvas, 'pointerdown', function (e: PointerEvent) { pxDown(ed, e); });
        listen(canvas, 'pointermove', function (e: PointerEvent) { pxMove(ed, e); });
        listen(canvas, 'pointerup', function () { pxUp(ed); });
        listen(canvas, 'pointercancel', function () { pxUp(ed); });
        ed.canvas = canvas;
        ed.stage = stage;

        var side = document.createElement('aside');
        side.className = 'gwt-px-side';
        var palette = pxPalette(grid, 10);
        if (palette.length) {
            ed.color = palette[0];
            ed.rgba = [parseInt(palette[0].slice(1, 3), 16), parseInt(palette[0].slice(3, 5), 16), parseInt(palette[0].slice(5, 7), 16), 255];
        }
        var colorLabel = document.createElement('label');
        colorLabel.className = 'gwt-px-field';
        colorLabel.textContent = tr('color');
        var colorInput = document.createElement('input');
        colorInput.type = 'color';
        colorInput.value = ed.color;
        listen(colorInput, 'input', function () {
            ed.color = colorInput.value;
            ed.rgba = [parseInt(ed.color.slice(1, 3), 16), parseInt(ed.color.slice(3, 5), 16), parseInt(ed.color.slice(5, 7), 16), 255];
        });
        colorLabel.appendChild(colorInput);
        side.appendChild(colorLabel);
        if (palette.length) {
            var swatches = document.createElement('div');
            swatches.className = 'gwt-px-palette';
            palette.forEach((hex: string) => {
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
        sizeText.textContent = tr('size');
        var sizeInput = document.createElement('input');
        sizeInput.type = 'range';
        sizeInput.min = 1;
        sizeInput.max = 8;
        sizeInput.value = ed.size;
        var sizeOut = document.createElement('output');
        sizeOut.textContent = String(ed.size);
        listen(sizeInput, 'input', () => {
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
        listen(fillInput, 'change', () => {
            ed.filled = fillInput.checked;
        });
        fillLabel.append(fillInput, document.createTextNode(tr('filled')));
        side.appendChild(fillLabel);
        var gridLabel = document.createElement('label');
        gridLabel.className = 'gwt-px-check';
        var gridInput = document.createElement('input');
        gridInput.type = 'checkbox';
        gridInput.checked = true;
        listen(gridInput, 'change', () => {
            ed.gridOn = gridInput.checked;
            pxRender(ed);
        });
        gridLabel.append(gridInput, document.createTextNode(tr('gridOn')));
        side.appendChild(gridLabel);
        var fixLabel = document.createElement('label');
        fixLabel.className = 'gwt-px-check';
        var fixInput = document.createElement('input');
        fixInput.type = 'checkbox';
        fixInput.checked = true;
        fixLabel.append(fixInput, document.createTextNode(tr('autoFix')));
        side.appendChild(fixLabel);
        var help = document.createElement('p');
        help.className = 'gwt-px-help';
        help.textContent = tr('pixelHelp');
        side.appendChild(help);

        var foot = document.createElement('div');
        foot.className = 'gwt-px-foot';
        var note = document.createElement('span');
        note.className = 'gwt-px-note';
        var zoomOut = document.createElement('button');
        zoomOut.type = 'button';
        zoomOut.className = 'gwt-px-zoom';
        zoomOut.textContent = '−';
        zoomOut.title = tr('zoom');
        listen(zoomOut, 'click', function () { pxZoom(ed, -1); });
        var zoomLabel = document.createElement('span');
        zoomLabel.className = 'gwt-px-zoomval';
        var zoomIn = document.createElement('button');
        zoomIn.type = 'button';
        zoomIn.className = 'gwt-px-zoom';
        zoomIn.textContent = '+';
        zoomIn.title = tr('zoom');
        listen(zoomIn, 'click', function () { pxZoom(ed, 1); });
        var fit = document.createElement('button');
        fit.type = 'button';
        fit.className = 'gwt-px-zoom gwt-px-fit';
        fit.textContent = tr('fit');
        listen(fit, 'click', function () { pxFit(ed); });
        var cancel = document.createElement('button');
        cancel.type = 'button';
        cancel.className = 'gwt-px-cancel';
        cancel.textContent = tr('cancel');
        listen(cancel, 'click', closePixelEditor);
        var apply = document.createElement('button');
        apply.type = 'button';
        apply.className = 'gwt-px-apply';
        apply.textContent = tr('applyVector');
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
        window.requestAnimationFrame(() => {
            if(pxEd === ed) {
                ed.autoFixInput = fixInput;
                if(fixInput.checked) {
                    var fixed = pxFixAlpha(ed.grid);
                    if(fixed) note.textContent = fixed + ' ' + tr('autoFixed');
                }
                pxFit(ed);
            }
        });
        ed.onKey = (e: KeyboardEvent) => {
            pxKeyDown(ed, e);
        };
        ed.onResize = () => {
            if (pxEd === ed) pxFit(ed);
        };
        listen(window, 'keydown', ed.onKey, true);
        listen(window, 'resize', ed.onResize);
    }

    function openPixelEditor () {
        if(pxEd) return;
        var target = anchorTarget();
        if(!target) {
            pageToast(tr('pixelNoTarget'));
            return;
        }
        var wasBitmap = isBitmapFormat();
        if(wasBitmap) pageToast(tr('converting'));
        ensureVectorCostume().then((ok: boolean) => {
            if (!ok) {
                pageToast(tr('pixelNoTarget'), true);
                return;
            }
            rasterizeCostume().then((src: PixelSource) => {
                if (!src) {
                    pageToast(tr('pixelNoTarget'), true);
                    return;
                }
                buildPixelEditor(src);
            });
        });
    }
    return {
        openPixelEditor,
        pageToast,
        closePixelEditor,
        syncPixelButton,
        get pxEd() { return pxEd; },
        removeButton() { pxBtn?.remove(); pxBtn = null; },
    };
}