import type { CostumeServices, PaintContext, PaintElement } from './type';
import { createLifecycle } from '../shared/lifecycle';
import { tr } from '../translate';

const svgNS = 'http://www.w3.org/2000/svg';
export function createCostumeTools(context: PaintContext, services: CostumeServices) {
    const { document, window, editor } = context;
    let panel: PaintElement | null = null;
    let life = createLifecycle();
    let job: AbortController | null = null;
    let undo: { target: any; renderer: any; changes: { costume: any; before: any; after: any }[] } | null = null;

    function releaseUndo() {
        if(!undo) return;
        for(const change of undo.changes) {
            if(change.costume.skinId !== change.before.skinId) undo.renderer.destroySkin(change.before.skinId);
        }
        undo = null;
    }

    function image(url: string, signal: AbortSignal): Promise<HTMLImageElement> {
        return new Promise((resolve, reject) => {
            const img = new window.Image();
            function done(error?: Error) {
                img.onload = img.onerror = null;
                signal.removeEventListener('abort', abort);
                if(error) { img.src = ''; reject(error); } else resolve(img);
            }
            function abort() {
                done(new DOMException('Aborted', 'AbortError'));
            }
            img.onload = () => done();
            img.onerror = () => done(new Error('Image unavailable'));
            signal.addEventListener('abort', abort, { once: true });
            if(signal.aborted) abort();
            else img.src = url;
        });
    }

    function svg(text: string): SVGSVGElement {
        const doc = new window.DOMParser().parseFromString(text, 'image/svg+xml');
        if(doc.querySelector('parsererror') || doc.documentElement.localName !== 'svg') throw new Error('Invalid SVG');
        return doc.documentElement as unknown as SVGSVGElement;
    }

    function positionedSVG(node: SVGSVGElement, x: number, y: number): SVGGElement {
        const group = node.ownerDocument.createElementNS(svgNS, 'g');
        const content = node.ownerDocument.createElementNS(svgNS, 'g');
        const viewBox = node.viewBox.baseVal;
        const width = Number(node.getAttribute('width'));
        const height = Number(node.getAttribute('height'));
        const clip = node.ownerDocument.createElementNS(svgNS, 'clipPath');
        clip.id = 'gwt-clip-' + window.crypto.getRandomValues(new Uint32Array(4)).join('-');
        clip.setAttribute('clipPathUnits', 'userSpaceOnUse');
        const rect = node.ownerDocument.createElementNS(svgNS, 'rect');
        for(const [key, value] of Object.entries({x, y, width, height})) rect.setAttribute(key, String(value));
        clip.appendChild(rect);
        group.setAttribute('clip-path', `url(#${clip.id})`);
        for(const attr of Array.from(node.attributes)) {
            if(['x', 'y', 'width', 'height', 'viewBox', 'preserveAspectRatio', 'xmlns'].includes(attr.name)) continue;
            content.setAttributeNS(attr.namespaceURI, attr.name, attr.value);
        }
        content.setAttribute('transform', `translate(${x} ${y}) ${node.getAttribute('transform') || ''} scale(${width / viewBox.width} ${height / viewBox.height}) translate(${-viewBox.x} ${-viewBox.y})`);
        content.append(...Array.from(node.childNodes));
        group.append(clip, content);
        return group;
    }

    function capture(): SVGSVGElement {
        const selected = new Set(services.selectedPaths());
        const layer = services.paintingLayer();
        const center = editor.paper()?.project.layers.find((item: any) => item.data?.isBackgroundGuideLayer)?.children[0]?.position;
        if(!selected.size || !layer || !center) throw new Error(tr('costumesSelectFirst'));
        function cloneSelection(item: any): any {
            if(selected.has(item)) return item.clone({insert: false});
            const children = new Map<any, any>();
            for(const child of item.children || []) {
                if(child.clipMask) continue;
                const clone = cloneSelection(child);
                if(clone) children.set(child, clone);
            }
            if(!children.size) return null;
            const clone = item.clone({insert: false});
            clone.removeChildren();
            for(const child of item.children || []) {
                const kept = child.clipMask ? child.clone({insert: false}) : children.get(child);
                if(kept) clone.addChild(kept);
            }
            return clone;
        }
        const drawing = cloneSelection(layer);
        try {
            const bounds = drawing.strokeBounds;
            if(!(bounds.width > 0 && bounds.height > 0)) throw new Error(tr('costumesSelectFirst'));
            const root = svg('<svg xmlns="' + svgNS + '"/>');
            root.setAttribute('viewBox', [bounds.x, bounds.y, bounds.width, bounds.height].join(' '));
            root.setAttribute('x', String((bounds.x - center.x) / 2));
            root.setAttribute('y', String((bounds.y - center.y) / 2));
            root.setAttribute('width', String(bounds.width / 2));
            root.setAttribute('height', String(bounds.height / 2));
            root.appendChild(drawing.exportSVG({asString: false}));
            const canvas = editor.paintCanvas();
            let fiber = canvas ? editor.fiberOf(canvas) : null;
            for(let i = 0; fiber && i < 45; i++, fiber = fiber.return) {
                const props = fiber.stateNode?.props || fiber.memoizedProps;
                if(typeof props?.fontInlineFn === 'function') {
                    return svg(props.fontInlineFn(new window.XMLSerializer().serializeToString(root)));
                }
            }
            return root;
        } finally {
            drawing?.remove();
        }
    }

    function refresh(target: any, vm: any) {
        for(const clone of target.sprite.clones) clone.updateAllDrawableProperties();
        vm.emitTargetsUpdate();
    }

    function showPanel(anchor: PaintElement) {
        job?.abort();
        life.dispose();
        panel = services.toggleFor(anchor, 'costumes', 'gwt-costumes');
        if(!panel) return;
        life = createLifecycle();
        const target = services.target();
        const vm = services.vm();
        const costumes: any[] = target?.getCostumes() || [];
        const source = costumes[target?.currentCostume];
        const heading = document.createElement('h3');
        heading.textContent = tr('costumes');
        panel.appendChild(heading);
        const status = document.createElement('p');
        status.setAttribute('role', 'status');
        panel.appendChild(status);
        let stamp: SVGSVGElement;
        try {
            stamp = capture();
            status.textContent = tr('costumesHint');
        } catch(error) {
            status.textContent = error.message;
        }
        const controls = document.createElement('div');
        controls.className = 'gwt-animation-controls';
        panel.appendChild(controls);
        const choices = document.createElement('div');
        choices.className = 'gwt-costume-list';
        const checks: {costume: any; input: HTMLInputElement}[] = [];
        function button(parent: PaintElement, key: string, action: () => void): HTMLButtonElement {
            const b = document.createElement('button') as unknown as HTMLButtonElement;
            b.type = 'button'; b.textContent = tr(key);
            life.listen(b, 'click', action);
            parent.appendChild(b);
            return b;
        }
        button(controls, 'costumesAll', () => checks.forEach(({input}) => { input.checked = true; }));
        button(controls, 'costumesNone', () => checks.forEach(({input}) => { input.checked = false; }));
        for(const costume of costumes) {
            if(costume === source) continue;
            const label = document.createElement('label');
            const input = document.createElement('input') as unknown as HTMLInputElement;
            input.type = 'checkbox'; input.checked = true;
            label.appendChild(input);
            label.appendChild(document.createTextNode(costume.name));
            choices.appendChild(label);
            checks.push({costume, input});
        }
        panel.appendChild(choices);
        const actions = document.createElement('div');
        actions.className = 'gwt-animation-controls';
        panel.appendChild(actions);
        const apply = button(actions, 'apply', () => { void run(false); });
        const revert = button(actions, 'costumesUndo', () => { void run(true); });
        apply.disabled = !stamp || !checks.length;
        revert.disabled = undo?.target !== target;
        const opened = panel;

        async function run(reverting: boolean) {
            if(job) return;
            const renderer = vm?.runtime?.renderer;
            const storage = vm?.runtime?.storage;
            const changes: {costume: any; before: any; after: any}[] = [];
            const controller = new AbortController();
            job = controller;
            const {signal} = controller;
            apply.disabled = revert.disabled = true;
            function valid() {
                signal.throwIfAborted();
                if(!opened.isConnected || services.target() !== target) throw new Error(tr('costumesChanged'));
            }
            try {
                valid();
                if(reverting) {
                    if(undo?.target !== target || undo.changes.some(({costume, after}) => {
                        return !target.getCostumes().includes(costume) || costume.asset !== after.asset || costume.skinId !== after.skinId ||
                        costume.rotationCenterX !== after.rotationCenterX || costume.rotationCenterY !== after.rotationCenterY
                    })) {
                        throw new Error(tr('costumesChanged'));
                    }
                    for(const {costume, before, after} of undo.changes) {
                        Object.assign(costume, before);
                        renderer.destroySkin(after.skinId);
                    }
                    undo = null;
                    refresh(target, vm);
                    status.textContent = tr('costumesUndone');
                    return;
                }
                const picked = checks.filter(({input}) => input.checked).map(({costume}) => ({
                    costume,
                    before: Object.fromEntries(['asset', 'assetId', 'md5', 'dataFormat', 'bitmapResolution', 'skinId', 'rotationCenterX', 'rotationCenterY', 'size', 'broken'].map((key: string) => [key, costume[key]]))
                }));
                if(!picked.length) return;
                for(const {costume, before} of picked) {
                    valid();
                    const original = await image(before.asset.encodeDataURI(), signal);
                    let overlay: HTMLImageElement | null = null;
                    try {
                        const resolution = before.dataFormat === 'svg' ? 1 : before.bitmapResolution || 1;
                        const base = before.dataFormat === 'svg' ? svg(new TextDecoder().decode(before.asset.data)) : null;
                        const viewBox = base?.hasAttribute('viewBox') ? base.viewBox.baseVal : null;
                        const originalWidth = viewBox?.width ?? original.naturalWidth;
                        const originalHeight = viewBox?.height ?? original.naturalHeight;
                        const cx = before.rotationCenterX == null ? originalWidth / 2 : before.rotationCenterX - (viewBox?.x ?? 0);
                        const cy = before.rotationCenterY == null ? originalHeight / 2 : before.rotationCenterY - (viewBox?.y ?? 0);
                        const x = Number(stamp.getAttribute('x')), y = Number(stamp.getAttribute('y'));
                        const w = Number(stamp.getAttribute('width')), h = Number(stamp.getAttribute('height'));
                        const left = Math.min(0, Math.floor(x * resolution + cx));
                        const top = Math.min(0, Math.floor(y * resolution + cy));
                        const width = Math.max(originalWidth, Math.ceil((x + w) * resolution + cx)) - left;
                        const height = Math.max(originalHeight, Math.ceil((y + h) * resolution + cy)) - top;
                        if(width > 8192 || height > 8192 || width * height > 16777216) throw new Error(tr('costumesTooLarge'));
                        const root = svg('<svg xmlns="' + svgNS + '"/>');
                        root.setAttribute('width', String(width / resolution));
                        root.setAttribute('height', String(height / resolution));
                        root.setAttribute('viewBox', [0, 0, width / resolution, height / resolution].join(' '));
                        const art = stamp.cloneNode(true) as SVGSVGElement;
                        const prefix = 'gwt-' + window.crypto.getRandomValues(new Uint32Array(4)).join('-') + '-';
                        const ids = new Map<string, string>();
                        for(const node of art.querySelectorAll('[id]')) ids.set(node.id, prefix + node.id);
                        for(const node of [art, ...art.querySelectorAll('*')]) {
                            for(const attr of Array.from(node.attributes)) {
                                let value = attr.value;
                                if(attr.name === 'id') value = ids.get(value) || value;
                                else for(const [id, renamed] of ids) {
                                    value = value.split('url(#' + id + ')').join('url(#' + renamed + ')');
                                    if(value === '#' + id) value = '#' + renamed;
                                }
                                node.setAttributeNS(attr.namespaceURI, attr.name, value);
                            }
                        }
                        const placedArt = positionedSVG(art, x + (cx - left) / resolution, y + (cy - top) / resolution);
                        let asset: any, skinId: number;
                        if(before.dataFormat === 'svg') {
                            base.setAttribute('width', String(originalWidth));
                            base.setAttribute('height', String(originalHeight));
                            if(!viewBox) base.setAttribute('viewBox', `0 0 ${originalWidth} ${originalHeight}`);
                            root.append(positionedSVG(base, -left, -top), placedArt);
                            const text = new window.XMLSerializer().serializeToString(root);
                            asset = storage.createAsset(storage.AssetType.ImageVector, storage.DataFormat.SVG, new TextEncoder().encode(text), null, true);
                            skinId = renderer.createSVGSkin(text, [cx - left, cy - top]);
                        } else {
                            root.appendChild(placedArt);
                            root.setAttribute('width', String(width));
                            root.setAttribute('height', String(height));
                            overlay = await image('data:image/svg+xml;charset=utf-8,' + encodeURIComponent(new window.XMLSerializer().serializeToString(root)), signal);
                            const canvas = document.createElement('canvas') as unknown as HTMLCanvasElement;
                            canvas.width = width; canvas.height = height;
                            const ctx = canvas.getContext('2d');
                            ctx.drawImage(original, -left, -top);
                            ctx.drawImage(overlay, 0, 0, width, height);
                            const blob = await new Promise<Blob>((resolve, reject) => canvas.toBlob((value: Blob | null) => value ? resolve(value) : reject(new Error('PNG encoding failed')), 'image/png'));
                            asset = storage.createAsset(storage.AssetType.ImageBitmap, storage.DataFormat.PNG, new Uint8Array(await blob.arrayBuffer()), null, true);
                            valid();
                            skinId = renderer.createBitmapSkin(canvas, resolution, [(cx - left) / resolution, (cy - top) / resolution]);
                            canvas.width = canvas.height = 1;
                        }
                        const after: Record<string, any> = {
                            ...before, asset, assetId: asset.assetId, md5: asset.assetId + '.' + asset.dataFormat,
                            dataFormat: asset.dataFormat, bitmapResolution: resolution, skinId,
                            rotationCenterX: cx - left, rotationCenterY: cy - top, size: [width, height]
                        };
                        delete after.broken;
                        changes.push({ costume, before, after });
                    } finally {
                        original.src = '';
                        if(overlay) overlay.src = '';
                    }
                    status.textContent = tr('costumesProgress', {current: changes.length, total: picked.length});
                    await new Promise<void>((resolve) => window.setTimeout(resolve, 0));
                }
                valid();
                if(changes.some(({costume, before}) => !target.getCostumes().includes(costume) ||
                    costume.asset !== before.asset || costume.skinId !== before.skinId ||
                    costume.rotationCenterX !== before.rotationCenterX || costume.rotationCenterY !== before.rotationCenterY)) {
                    throw new Error(tr('costumesChanged'));
                }
                releaseUndo();
                for(const {costume, after} of changes) {
                    delete costume.broken;
                    Object.assign(costume, after);
                }
                undo = { target, renderer, changes: changes.splice(0) };
                refresh(target, vm);
                status.textContent = tr('costumesDone', {count: undo.changes.length});
            } catch(error) {
                if(!signal.aborted) status.textContent = tr('costumesError') + ' ' + error.message;
            } finally {
                for(const {after} of changes) renderer.destroySkin(after.skinId);
                job = null;
                apply.disabled = !stamp || !checks.length;
                revert.disabled = undo?.target !== target;
            }
        }
    }

    return {
        showPanel,
        sync() {
            if(panel && !panel.isConnected) { job?.abort(); life.dispose(); panel = null; }
            if(undo && services.target() !== undo.target) releaseUndo();
        },
        dispose() {
            job?.abort(); life.dispose(); releaseUndo();
        }
    };
}