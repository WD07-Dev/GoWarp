import type { AlphaStats, AnchorServices, PaintContext, PaintElement, AnchorMeasurement } from './type';
import { tr } from '../translate';

export function createAnchors(context: PaintContext, services: AnchorServices) {
    const { document, window } = context;
    const { listen } = context.lifecycle;
    const { anchorTarget, findVM, markObjects, keyRow, toggleFor, labelEl } = services;

    var anchorCache: Record<string, AlphaStats> = {};
    var anchorState = {
        probing: false,
        busy: false,
        list: null as AnchorMeasurement[] | null,
        spriteId: '',
        n: 0,
        mode: 'match',
        undo: null,
        result: null
    };

    function alphaStats(img: CanvasImageSource, w: number, h: number, scale: number) {
        var s = Math.min(scale, 1400 / Math.max(w, h, 1));
        var cv = document.createElement('canvas');
        cv.width = Math.max(1, Math.round(w * s));
        cv.height = Math.max(1, Math.round(h * s));
        var ctx = cv.getContext('2d', { willReadFrequently: true });
        ctx.drawImage(img, 0, 0, cv.width, cv.height);
        var d = ctx.getImageData(0, 0, cv.width, cv.height).data;
        var sum = 0, sx = 0, sy = 0, minX = 1e9, minY = 1e9, maxX = -1, maxY = -1;
        for(let y=0; y < cv.height; y++) {
            for(let x=0; x < cv.width; x++) {
                var a = d[(y * cv.width + x) * 4 + 3];
                if(!a) continue;
                sum += a;
                sx += x * a;
                sy += y * a;
                if(x < minX) minX = x;
                if(x > maxX) maxX = x;
                if(y < minY) minY = y;
                if(y > maxY) maxY = y;
            }
        }
        var out: AlphaStats = {
            area: sum,
            w: cv.width / s,
            h: cv.height / s,
            thumb: ''
        };
        if(!sum) return out;
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

    async function costumeRaster(c: any) {
        var raw = c.asset && (c.asset.data !== undefined ? c.asset.data : c.asset);
        if(c.dataFormat === 'svg' || typeof raw === 'string') {
            var txt = typeof raw === 'string' ? raw : new TextDecoder().decode(raw);
            const root = new window.DOMParser().parseFromString(txt, 'image/svg+xml').documentElement as unknown as SVGSVGElement;
            const viewBox = root.hasAttribute('viewBox') ? root.viewBox.baseVal : null;
            return new Promise<AlphaStats>((resolve, reject) => {
                const img = new window.Image();
                img.onload = () => {
                    const stats = alphaStats(img, viewBox?.width || img.naturalWidth, viewBox?.height || img.naturalHeight, 4);
                    if(stats.area && viewBox) {
                        stats.cx += viewBox.x;
                        stats.cy += viewBox.y;
                    }
                    resolve(stats);
                };
                img.onerror = () => reject(new Error('SVG image unavailable'));
                img.src = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(txt);
            });
        }
        const bmp = await createImageBitmap(new Blob([raw], { type: 'image/' + (c.dataFormat || 'png') }));
        try {
            return alphaStats(bmp, bmp.width, bmp.height, 1);
        } finally {
            bmp.close();
        }
    }

    async function measureOne(c: any) {
        var key = (c.md5 || c.assetId || '') + '|' + c.dataFormat + '|' + c.name;
        if(anchorCache[key]) return Promise.resolve(anchorCache[key]);
        const s = await costumeRaster(c);
        anchorCache[key] = s;
        var keys = Object.keys(anchorCache);
        if(keys.length > 48) delete anchorCache[keys[0]];
        return s;
    }

    async function measureAnchor() {
        var t = anchorTarget();
        if(!t) return Promise.resolve(null);
        var costumes = t.getCostumes();
        if(costumes.length < 2) return Promise.resolve({short: true, n: costumes.length});
        anchorState.busy = true;
        try {
            const stats = await Promise.all(costumes.map(measureOne));
            anchorState.list = stats.map(function (s: AlphaStats, i: number) {
                return {
                    index: i, name: costumes[i].name, area: s.area || 0, cx: s.cx || 0, cy: s.cy || 0,
                    w: s.w || 0, h: s.h || 0, thumb: s.thumb || '',
                    pivot: [costumes[i].rotationCenterX, costumes[i].rotationCenterY]
                };
            });
            anchorState.spriteId = t.id;
            anchorState.n = costumes.length;
            anchorState.busy = false;
            return anchorState.list;
        } catch (e) {
            anchorState.busy = false;
            if (window.console) window.console.warn('GoWarp Toolkit anchor:', e);
            return null;
        }
    }

    function anchorPlan(mode: string, list: AnchorMeasurement[]) {
        var live = list.filter(function (s: AnchorMeasurement) { return s.area > 0; });
        if(!live.length) return null;
        var out = list.map(function (s: AnchorMeasurement) { return [s.pivot[0], s.pivot[1]]; });
        if(mode === 'own') {
            list.forEach(function (s: AnchorMeasurement, i: number) { if (s.area > 0) out[i] = [s.cx, s.cy]; });
            return out;
        }
        if(mode === 'body') {
            var w = 0, x = 0, y = 0;
            live.forEach(function (s: AnchorMeasurement) { w += s.area; x += s.cx * s.area; y += s.cy * s.area; });
            list.forEach(function (s: AnchorMeasurement, i: number) { if (s.area > 0) out[i] = [x / w, y / w]; });
            return out;
        }
        var base = live[0];
        var d = [base.pivot[0] - base.cx, base.pivot[1] - base.cy];
        list.forEach(function (s: AnchorMeasurement, i: number) { if (s.area > 0) out[i] = [s.cx + d[0], s.cy + d[1]]; });
        return out;
    }

    function anchorJump(list: AnchorMeasurement[], pivots: number[][]) {
        var pts = [];
        list.forEach(function (s: AnchorMeasurement, i: number) {
            if (s.area > 0) pts.push([s.cx - pivots[i][0], s.cy - pivots[i][1]]);
        });
        var tot = 0, max = 0;
        for(let i=1; i < pts.length; i++) {
            var j = Math.hypot(pts[i][0] - pts[i - 1][0], pts[i][1] - pts[i - 1][1]);
            tot += j;
            if (j > max) max = j;
        }
        return {mean: pts.length > 1 ? tot / (pts.length - 1) : 0, max: max, steps: pts.length - 1};
    }

    function px(v: number) {
        return (Math.round(v * 10) / 10).toFixed(1);
    }

    async function refreshAnchorView(t: any, pivots: number[][]) {
        const vm = findVM();
        const renderer = vm.runtime.renderer;
        const costumes = t.getCostumes();
        const changes: {costume: any; asset: any; before: number; after: number; pivot: number[]}[] = [];
        try {
            for(let i = 0; i < costumes.length; i++) {
                const costume = costumes[i];
                const pivot = pivots[i];
                if(costume.rotationCenterX === pivot[0] && costume.rotationCenterY === pivot[1]) continue;
                const asset = costume.asset;
                const before = costume.skinId;
                let after: number;
                if(costume.dataFormat === 'svg') {
                    after = renderer.createSVGSkin(new TextDecoder().decode(asset.data), pivot);
                } else {
                    const img = new window.Image();
                    img.src = asset.encodeDataURI();
                    await img.decode();
                    const resolution = costume.bitmapResolution || 1;
                    after = renderer.createBitmapSkin(img, resolution, [pivot[0] / resolution, pivot[1] / resolution]);
                }
                changes.push({costume, asset, before, after, pivot});
            }
            if(anchorTarget() !== t || changes.some(({costume, asset, before}) =>
                !t.getCostumes().includes(costume) || costume.asset !== asset || costume.skinId !== before)) {
                throw new Error(tr('costumesChanged'));
            }
        } catch(error) {
            for(const change of changes) renderer.destroySkin(change.after);
            throw error;
        }
        for(const {costume, after, pivot} of changes) {
            costume.rotationCenterX = pivot[0];
            costume.rotationCenterY = pivot[1];
            costume.skinId = after;
        }
        for(const clone of t.sprite.clones) clone.updateAllDrawableProperties();
        vm.emitTargetsUpdate();
        vm.emitProjectChanged();
        for(const change of changes) renderer.destroySkin(change.before);
    }

    async function applyAnchor(mode: string) {
        var t = anchorTarget();
        if(!t || !anchorState.list || anchorState.busy) return null;
        var costumes = t.getCostumes();
        if(t.id !== anchorState.spriteId || costumes.length !== anchorState.n) {
            anchorState.list = null;
            return null;
        }
        anchorState.list.forEach(function (s: AnchorMeasurement, i: number) {
            s.pivot = [costumes[i].rotationCenterX, costumes[i].rotationCenterY];
        });
        var plan = anchorPlan(mode, anchorState.list);
        if(!plan) return null;
        var before = anchorJump(anchorState.list, anchorState.list.map(function (s: AnchorMeasurement) { return s.pivot; }));
        var snapshot = costumes.map(function (c: any) { return [c.rotationCenterX, c.rotationCenterY]; });
        anchorState.busy = true;
        markAnchor();
        try {
            await refreshAnchorView(t, plan.map((pivot: number[]) => pivot.map((value: number) => Math.round(value * 1000) / 1000)));
        } catch(error) {
            window.console.warn('GoWarp Toolkit anchor:', error);
            window.alert(tr('costumesError') + ' ' + error.message);
            return null;
        } finally {
            anchorState.busy = false;
            markAnchor();
        }
        var written = 0;
        anchorState.list.forEach(function (s: AnchorMeasurement, i: number) {
            s.pivot = [costumes[i].rotationCenterX, costumes[i].rotationCenterY];
            if(s.area > 0) written++;
        });
        anchorState.undo = {
            spriteId: t.id,
            n: snapshot.length,
            list: snapshot,
            mode: mode
        };
        anchorState.result = {
            before: before.mean,
            after: anchorJump(anchorState.list, anchorState.list.map((s: AnchorMeasurement) => { return s.pivot; })).mean,
            written: written
        };
        markAnchor();
        return anchorState.result;
    }

    async function undoAnchor() {
        var t = anchorTarget();
        var u = anchorState.undo;
        if(!t || !u || anchorState.busy || u.spriteId !== t.id) return false;
        var costumes = t.getCostumes();
        if(costumes.length !== u.n) return false;
        anchorState.busy = true;
        markAnchor();
        try {
            await refreshAnchorView(t, u.list);
        } catch(error) {
            window.console.warn('GoWarp Toolkit anchor:', error);
            window.alert(tr('costumesError') + ' ' + error.message);
            return false;
        } finally {
            anchorState.busy = false;
            markAnchor();
        }
        anchorState.undo = null;
        anchorState.result = null;
        if(anchorState.list) {
            anchorState.list.forEach(function (s: AnchorMeasurement, i: number) {
                s.pivot = [costumes[i].rotationCenterX, costumes[i].rotationCenterY];
            });
        }
        markAnchor();
        return true;
    }

    function anchorBody(panel: PaintElement) {
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
        [['body', tr('anchorBody')], ['own', tr('anchorOwn')], ['match', tr('anchorFirst')]].forEach((row: string[]) => {
            var b = document.createElement('button');
            b.type = 'button';
            b.dataset.mode = String(row[0]);
            b.textContent = row[1];
            listen(b, 'click', () => {
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
        listen(go, 'click', async () => {
            var r = await applyAnchor(anchorState.mode);
            if(r) {
                markAnchor();
                if (services.railEl) markObjects();
            }
        });
        var back = document.createElement('button');
        back.type = 'button';
        back.className = 'gwt-ghost';
        back.textContent = tr('anchorUndo');
        back.disabled = !anchorState.undo;
        listen(back, 'click', async () => {
            if(await undoAnchor()) markAnchor();
        });
        act.appendChild(go);
        act.appendChild(back);
        panel.appendChild(act);
        panel.appendChild(keyRow(['Alt+D']));
        markAnchor();
    }

    function markAnchor() {
        if(!services.openPanel || services.openPanel._gwtKind !== 'anchor') return;
        var panel = services.openPanel;
        var strong = panel.querySelector('.gwt-jump strong');
        var sub = panel.querySelector('.gwt-sub');
        var list = panel.querySelector('.gwt-alist');
        var go = panel.querySelector('.gwt-primary');
        var back = panel.querySelector('.gwt-ghost');
        Array.prototype.forEach.call(panel.querySelectorAll('[data-mode]'), function (b: HTMLElement) {
            var on = b.dataset.mode === anchorState.mode;
            b.setAttribute('aria-pressed', String(on));
            b.classList.toggle('gwt-on', on);
        });
        var tgt = anchorTarget();
        if(!tgt || anchorState.busy) {
            strong.textContent = anchorState.busy ? '…' : '—';
            if(sub) sub.textContent = tgt ? tr('anchorBusy') : tr('anchorNone');
            if(go) go.disabled = true;
            return;
        }
        var costumes = tgt.getCostumes();
        if(costumes.length < 2 || !anchorState.list || anchorState.spriteId !== tgt.id || anchorState.n !== costumes.length) {
            strong.textContent = '—';
            if(sub) sub.textContent = costumes.length < 2 ? tr('anchorNone') : tr('anchorBusy');
            if(list) { list.textContent = ''; list.dataset.sig = String(''); }
            if(go) go.disabled = true;
            if(back) back.disabled = true;
            if(costumes.length > 1 && !anchorState.probing) {
                anchorState.probing = true;
                measureAnchor().then(() => {
                    anchorState.probing = false;
                    markAnchor();
                }, () => {
                    anchorState.probing = false;
                });
            }
            return;
        }
        anchorState.list.forEach((s: AnchorMeasurement, i: number) => {
            s.pivot = [costumes[i].rotationCenterX, costumes[i].rotationCenterY];
        });
        var now = anchorJump(anchorState.list, anchorState.list.map(function (s: AnchorMeasurement) { return s.pivot; }));
        var plan = anchorPlan(anchorState.mode, anchorState.list);
        var next = plan ? anchorJump(anchorState.list, plan) : now;
        strong.textContent = px(now.mean);
        if(sub) {
            sub.textContent = (anchorState.result ? tr('anchorDone') + ' ' + anchorState.result.written + ' · ' : '') + px(now.mean) + ' → ' + px(next.mean) + ' px';
        }
        if(go) go.disabled = !plan || now.mean < .05;
        if(back) back.disabled = !anchorState.undo;
        if(!list || !plan) return;
        var sig = tr().lang + ';' + anchorState.list.map(function (s: AnchorMeasurement, i: number) {
            return [s.name, s.area <= 0 ? '-' : px(s.pivot[0]) + ',' + px(s.pivot[1]) + '>' + px(plan[i][0]) + ',' + px(plan[i][1])].join('|');
        }).join(';');
        if(list.dataset.sig === sig) return;
        list.dataset.sig = String(sig);
        list.textContent = '';
        anchorState.list.forEach(function (s: AnchorMeasurement, i: number) {
            var row = document.createElement('div');
            row.className = 'gwt-arow';
            var th = document.createElement('i');
            th.className = 'gwt-athumb';
            if(s.thumb) th.style.backgroundImage = 'url(' + s.thumb + ')';
            var nm = document.createElement('span');
            nm.className = 'gwt-aname';
            nm.textContent = s.name;
            var d = document.createElement('span');
            d.className = 'gwt-adelta';
            if(s.area <= 0) {
                d.textContent = tr('anchorSkipped');
            }else {
                var shift = Math.hypot(plan[i][0] - s.pivot[0], plan[i][1] - s.pivot[1]);
                d.textContent = (shift < .05 ? '=' : '→ ') + px(s.pivot[0]) + ',' + px(s.pivot[1]) + (shift < .05 ? '' : ' → ' + px(plan[i][0]) + ',' + px(plan[i][1]));
            }
            row.appendChild(th);
            row.appendChild(nm);
            row.appendChild(d);
            list.appendChild(row);
        });
    }

    function showAnchorPanel(anchor: PaintElement) {
        var panel = toggleFor(anchor, 'anchor', 'gwt-wide');
        if(!panel) return;
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