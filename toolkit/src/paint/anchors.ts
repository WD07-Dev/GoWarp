import type { AlphaStats, AnchorServices, PaintContext } from './type';
import { tr } from '../translate';

export function createAnchors(context: PaintContext, services: AnchorServices) {
    const {document, window} = context;
    const {listen} = context.lifecycle;
    const {anchorTarget, findVM, markObjects, keyRow, toggleFor, labelEl} = services;

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
        sub.textContent = tr('anchorSub');
        panel.appendChild(sub);

        var seg = document.createElement('div');
        seg.className = 'gwt-seg';
        [['body', tr('anchorBody')], ['own', tr('anchorOwn')], ['match', tr('anchorFirst')]].forEach(function (row) {
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
        go.textContent = tr('anchorApply');
        listen(go, 'click', function () {
            var r = applyAnchor(anchorState.mode);
            if (r) {
                markAnchor();
                if (services.railEl) markObjects();
            }
        });
        var back = document.createElement('button');
        back.type = 'button';
        back.className = 'gwt-ghost';
        back.textContent = tr('anchorUndo');
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
        if (!services.openPanel || services.openPanel._gwtKind !== 'anchor') return;
        var panel = services.openPanel;
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
            if (sub) sub.textContent = tgt ? tr('anchorBusy') : tr('anchorNone');
            if (go) go.disabled = true;
            return;
        }
        var costumes = tgt.getCostumes();
        if (costumes.length < 2 || !anchorState.list || anchorState.n !== costumes.length) {
            strong.textContent = '—';
            if (sub) sub.textContent = costumes.length < 2 ? tr('anchorNone') : tr('anchorBusy');
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
            sub.textContent = (anchorState.result ? tr('anchorDone') + ' ' + anchorState.result.written + ' · ' : '') +
                px(now.mean) + ' → ' + px(next.mean) + ' px';
        }
        if (go) go.disabled = !plan || now.mean < .05;
        if (back) back.disabled = !anchorState.undo;
        if (!list || !plan) return;
        var sig = tr().lang + ';' + anchorState.list.map(function (s, i) {
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
                d.textContent = tr('anchorSkipped');
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
        panel.appendChild(labelEl(tr('anchorTitle')));
        anchorBody(panel);
    }
    return {
        refreshAnchorView,
        showAnchorPanel,
        applyAnchor,
        markAnchor,
        anchorState,
    };
}
