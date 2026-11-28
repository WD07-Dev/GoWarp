import type { AnimationServices, CostumeFrame, LoadedFrame, PaintContext, PaintElement } from './type';
import { createLifecycle } from '../shared/lifecycle';
import { createAnimationGIF } from './gif';
import { createGIFCrop } from './crop';
import { tr } from '../translate';

export function frameBounds(costume: CostumeFrame, width: number, height: number) {
    const resolution = costume.dataFormat === 'svg' ? 1 : Math.max(1, costume.bitmapResolution || 1);
    return {
        x: -(costume.rotationCenterX ?? width / 2) / resolution,
        y: -(costume.rotationCenterY ?? height / 2) / resolution,
        width: width / resolution,
        height: height / resolution
    };
}

export function playbackFrame(start: number, end: number, fps: number, elapsed: number): number {
    return start + Math.floor(Math.max(0, elapsed) * fps / 1000) % (end - start + 1);
}

export function createAnimationTools(context: PaintContext, services: AnimationServices) {
    const { document, window, editor } = context;
    const visibility = new AbortController();
    let panelLife = createLifecycle();
    let panel: PaintElement | null = null;
    let preview: HTMLCanvasElement | null = null;
    let overlay: HTMLCanvasElement | null = null;
    let status: HTMLElement | null = null;
    let play: HTMLButtonElement | null = null;
    let startInput: HTMLInputElement | null = null;
    let endInput: HTMLInputElement | null = null;
    let target: any = null;
    let costumeCount = 0;
    let start = 0, end = 0, frame = 0, fps = 12;
    let enabled = false, previous = true, next = true, opacity = 0.25;
    let playing = false, startedAt = 0;
    let raf = 0, disposed = false, imageId = 0;
    let overlayKey = '', previewKey = '';
    let exporting: AbortController | null = null;
    let crop: ReturnType<typeof createGIFCrop> | null = null;
    const cache = new Map<NonNullable<CostumeFrame['asset']>, LoadedFrame>();

    function clearCache() {
        for(const entry of cache.values()) {
            entry.image.onload = entry.image.onerror = null;
            entry.image.src = '';
        }
        cache.clear();
    }
    function load(costume: CostumeFrame): LoadedFrame | undefined {
        const asset = costume?.asset;
        if(!asset || typeof asset.encodeDataURI !== 'function') return;
        let entry = cache.get(asset);
        if(entry) {
            cache.delete(asset);
            cache.set(asset, entry);
            return entry;
        }
        const image = new window.Image();
        entry = { image, state: 'loading', id: ++imageId };
        cache.set(asset, entry);
        image.onload = () => {
            entry.state = 'ready';
            overlayKey = previewKey = '';
            wake();
        };
        image.onerror = () => {
            entry.state = 'error';
            overlayKey = previewKey = '';
            wake();
        };
        try {
            image.src = asset.encodeDataURI();
        } catch {
            entry.state = 'error';
        }
        while(cache.size > 8) {
            const key = cache.keys().next().value;
            const old = cache.get(key);
            old.image.onload = old.image.onerror = null;
            old.image.src = '';
            cache.delete(key);
        }
        return entry;
    }
    function hideOverlay() {
        overlay?.remove();
        overlay = null;
        overlayKey = '';
    }
    function stop() {
        playing = false;
        if(play) {
            play.textContent = tr('animationPlay');
            play.setAttribute('aria-pressed', 'false');
        }
    }
    function closePanel() {
        exporting?.abort();
        exporting = null;
        stop();
        panelLife.dispose();
        panel = null;
        preview = null;
        crop = null;
        status = play = startInput = endInput = null;
        previewKey = '';
    }
    function exportImage(costume: CostumeFrame, signal: AbortSignal): Promise<HTMLImageElement> {
        return new Promise((resolve: (image: HTMLImageElement) => void, reject: (reason: unknown) => void) => {
            const image = new window.Image();
            function cleanup(): void {
                image.onload = image.onerror = null;
                signal.removeEventListener('abort', abort);
            }
            function abort(): void {
                cleanup();
                image.src = '';
                reject(signal.reason);
            }
            if(signal.aborted) {
                abort();
                return;
            }
            signal.addEventListener('abort', abort, {once: true});
            image.onload = () => {
                cleanup();
                resolve(image);
            };
            image.onerror = () => {
                cleanup();
                reject(new Error(tr('animationImageError')));
            };
            try {
                if(!costume.asset) throw new Error(tr('animationImageError'));
                image.src = costume.asset.encodeDataURI();
            } catch(error: unknown) {
                cleanup();
                reject(error);
            }
        });
    }
    async function exportGIF(button: HTMLButtonElement, message: HTMLElement): Promise<void> {
        if(exporting || disposed) return;
        const current = services.target();
        const costumes: CostumeFrame[] = (current?.getCostumes() || []).slice(start, end + 1).map((costume: CostumeFrame) => ({...costume}));
        if(!costumes.length) {
            message.textContent = tr('animationImageError');
            return;
        }
        stop();
        const job = new AbortController();
        exporting = job;
        button.disabled = true;
        message.textContent = tr('animationExportProgress', {current: 0, total: costumes.length});
        const canvas = document.createElement('canvas') as unknown as HTMLCanvasElement;
        try {
            const size = services.stageSize();
            const region = crop?.bounds() || {x: 0, y: 0, width: Math.round(size.width), height: Math.round(size.height)};
            const {width, height} = region;
            const gif = createAnimationGIF(width, height, fps);
            canvas.width = width; canvas.height = height;
            const ctx = canvas.getContext('2d', {willReadFrequently: true});
            if(!ctx) throw new Error(tr('animationImageError'));
            for(let i=0; i < costumes.length; i++) {
                await new Promise<void>((resolve: () => void) => window.setTimeout(resolve, 0));
                job.signal.throwIfAborted();
                const image = await exportImage(costumes[i], job.signal);
                try {
                    job.signal.throwIfAborted();
                    const bounds = frameBounds(costumes[i], image.naturalWidth, image.naturalHeight);
                    ctx.clearRect(0, 0, width, height);
                    ctx.drawImage(image, size.width / 2 + bounds.x - region.x, size.height / 2 + bounds.y - region.y, bounds.width, bounds.height);
                    gif.write(ctx.getImageData(0, 0, width, height).data);
                } finally {
                    image.src = '';
                }
                message.textContent = tr('animationExportProgress', {current: i + 1, total: costumes.length});
            }
            job.signal.throwIfAborted();
            const blob = new window.Blob([gif.finish()], {type: 'image/gif'});
            const url = window.URL.createObjectURL(blob);
            const link = document.createElement('a') as unknown as HTMLAnchorElement;
            link.href = url;
            link.download = String(current.getName?.() || 'animation').replace(/[<>:"/\\|?*\x00-\x1f]/g, '_') + '.gif';
            try {
                document.body.appendChild(link);
                link.click();
            } finally {
                link.remove();
                window.setTimeout(() => window.URL.revokeObjectURL(url), 1000);
            }
            message.textContent = '';
        } catch(error: unknown) {
            if(!job.signal.aborted) {
                message.textContent = tr('animationExportError');
                window.console.warn('GoWarp Toolkit GIF:', error);
            }
        } finally {
            canvas.width = canvas.height = 0;
            button.disabled = false;
            if(job.signal.aborted) message.textContent = '';
            if(exporting === job) exporting = null;
        }
    }
    function selectedCostume(canvas: HTMLCanvasElement, current: number): number {
        let fiber = editor.fiberOf(canvas);
        for(let i=0; fiber && i < 45; i++, fiber = fiber.return) {
            const props = fiber.stateNode?.props || fiber.memoizedProps;
            if(Number.isInteger(props?.selectedCostumeIndex)) return props.selectedCostumeIndex;
        }
        return current;
    }
    function updateRange(count: number) {
        if(end === costumeCount - 1 || !costumeCount) end = count - 1;
        end = Math.max(0, Math.min(end, count - 1));
        start = Math.min(start, end);
        frame = Math.max(start, Math.min(frame, end));
        costumeCount = count;
        if(startInput && endInput) {
            startInput.max = endInput.max = String(count);
            startInput.value = String(start + 1);
            endInput.value = String(end + 1);
        }
    }
    function drawPreview(costumes: CostumeFrame[]) {
        if(!preview || !status) return;
        const costume = costumes[frame];
        const entry = load(costume);
        const size = services.stageSize();
        const dpr = window.devicePixelRatio || 1;
        const key = JSON.stringify([frame, entry?.id, entry?.state, costume?.name, costume?.rotationCenterX, costume?.rotationCenterY, costume?.bitmapResolution, size, dpr, crop?.bounds()]);
        if(key === previewKey) return;
        previewKey = key;
        const width = 320, height = 200;
        preview.width = Math.round(width * dpr);
        preview.height = Math.round(height * dpr);
        const ctx = preview.getContext('2d');
        ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
        if(costume && entry?.state === 'ready') {
            const image = entry.image;
            const bounds = frameBounds(costume, image.naturalWidth, image.naturalHeight);
            const scale = Math.min(width / size.width, height / size.height);
            ctx.drawImage(image, width / 2 + bounds.x * scale, height / 2 + bounds.y * scale, bounds.width * scale, bounds.height * scale);
        }
        crop?.draw(ctx, width, height);
        const detail = !entry || entry.state === 'error' ? tr('animationImageError') : entry.state === 'loading' ? tr('animationLoading') : costume.name || '';
        status.textContent = `${frame + 1} / ${costumes.length} · ${detail}`;
    }
    function drawOnion(canvas: HTMLCanvasElement, costumes: CostumeFrame[], selected: number) {
        if(!enabled || (!previous && !next) || !opacity) {
            hideOverlay();
            return;
        }
        const paper = editor.paper();
        const center = paper?.project?.layers.find((layer: any) => layer.data?.isBackgroundGuideLayer)?.children[0]?.position;
        if(!center || !paper.view) {
            hideOverlay();
            return;
        }
        const frames = [previous ? selected - 1 : -1, next ? selected + 1 : -1].filter((index: number) => index >= 0 && index < costumes.length);
        if(!frames.length) {
            hideOverlay();
            return;
        }
        const entries = frames.map((index: number) => load(costumes[index]));
        const rect = canvas.getBoundingClientRect();
        const matrix = paper.view.matrix;
        const dpr = window.devicePixelRatio || 1;
        const key = JSON.stringify([
            selected, opacity, rect.x, rect.y, rect.width, rect.height, dpr,
            matrix.a, matrix.b, matrix.c, matrix.d, matrix.tx, matrix.ty, center.x, center.y,
            frames.map((index: number, i: number) => [
                entries[i]?.id, entries[i]?.state,
                costumes[index].rotationCenterX, costumes[index].rotationCenterY, costumes[index].bitmapResolution
            ])
        ]);
        if(key === overlayKey) return;
        overlayKey = key;
        if(!overlay) {
            overlay = document.createElement('canvas') as unknown as HTMLCanvasElement;
            overlay.className = 'gwt-onion-overlay';
            overlay.setAttribute('aria-hidden', 'true');
            document.body.appendChild(overlay);
        }
        Object.assign(overlay.style, {left: rect.left + 'px', top: rect.top + 'px', width: rect.width + 'px', height: rect.height + 'px'});
        overlay.width = Math.max(1, Math.round(rect.width * dpr));
        overlay.height = Math.max(1, Math.round(rect.height * dpr));
        const ctx = overlay.getContext('2d');
        ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
        ctx.transform(matrix.a, matrix.b, matrix.c, matrix.d, matrix.tx, matrix.ty);
        ctx.globalAlpha = opacity;
        entries.forEach((entry: LoadedFrame | undefined, i: number) => {
            if(entry?.state !== 'ready') return;
            const image = entry.image;
            const bounds = frameBounds(costumes[frames[i]], image.naturalWidth, image.naturalHeight);
            ctx.drawImage(image, center.x + bounds.x * 2, center.y + bounds.y * 2, bounds.width * 2, bounds.height * 2);
        });
    }
    function tick(now: number) {
        if(panel && !panel.isConnected) closePanel();
        const canvas = editor.paintCanvas() as HTMLCanvasElement | null;
        const current = services.target();
        if(disposed || document.hidden || !canvas || !current) {
            stop();
            hideOverlay();
            return;
        }
        const costumes: CostumeFrame[] = current.getCostumes();
        if(target !== current) {
            exporting?.abort();
            target = current;
            stop(); clearCache();
            costumeCount = 0; start = end = frame = 0;
            overlayKey = previewKey = '';
        }
        if(costumes.length !== costumeCount) {
            stop();
            updateRange(costumes.length);
        }
        if(!costumes.length) {
            hideOverlay();
            return;
        }
        if(playing) frame = playbackFrame(start, end, fps, now - startedAt);
        if(panel) {
            drawPreview(costumes);
            if (playing) load(costumes[frame < end ? frame + 1 : start]);
        }
        drawOnion(canvas, costumes, selectedCostume(canvas, current.currentCostume));
        wake();
    }
    function wake() {
        if(disposed || raf || document.hidden || (!enabled && !panel?.isConnected)) return;
        raf = window.requestAnimationFrame((now: number) => {
            raf = 0;
            tick(now);
        });
    }
    function showPanel(anchor: PaintElement) {
        const opened = services.toggleFor(anchor, 'animation', 'gwt-animation');
        closePanel();
        if(!opened) return;
        panel = opened;
        panelLife = createLifecycle();
        const { listen } = panelLife;
        const heading = document.createElement('h3');
        heading.textContent = tr('animation');
        panel.appendChild(heading);
        preview = document.createElement('canvas') as unknown as HTMLCanvasElement;
        preview.className = 'gwt-animation-preview';
        preview.setAttribute('role', 'img');
        preview.setAttribute('aria-label', tr('animationPreview'));
        panel.appendChild(preview);
        status = document.createElement('div');
        status.className = 'gwt-animation-status';
        panel.appendChild(status);
        const controls = document.createElement('div');
        controls.className = 'gwt-animation-controls';
        function button(text: string, action: () => void): HTMLButtonElement {
            const b = document.createElement('button') as unknown as HTMLButtonElement;
            b.type = 'button'; b.textContent = text;
            listen(b, 'click', action);
            controls.appendChild(b);
            return b;
        }
        button(tr('animationPrevious'), () => {
            stop();
            frame = frame > start ? frame - 1 : end;
            previewKey = '';
        });
        play = button(tr('animationPlay'), () => {
            if(playing) {
                stop();
                return;
            }
            playing = true;
            startedAt = window.performance.now() - (frame - start) * 1000 / fps;
            play.textContent = tr('animationPause');
            play.setAttribute('aria-pressed', 'true');
            wake();
        });
        play.setAttribute('aria-pressed', 'false');
        button(tr('animationNext'), () => {
            stop();
            frame = frame < end ? frame + 1 : start;
            previewKey = '';
        });
        panel.appendChild(controls);
        const range = document.createElement('div'); range.className = 'gwt-animation-range';
        function numberField(key: string, value: number, max: number, change: (value: number) => void): HTMLInputElement {
            const label = document.createElement('label');
            label.textContent = tr(key);
            const input = document.createElement('input') as unknown as HTMLInputElement;
            input.type = 'number';
            input.min = '1';
            input.max = String(max);
            input.step = '1';
            input.value = String(value);
            listen(input, 'change', () => {
                const number = Math.max(1, Math.min(Number(input.max), Math.round(Number(input.value)) || 1));
                input.value = String(number);
                stop();
                change(number);
                previewKey = '';
            });
            listen(input, 'keydown', (event: KeyboardEvent) => event.stopPropagation());
            label.appendChild(input);
            range.appendChild(label);
            return input;
        }
        startInput = numberField('animationFrom', start + 1, Math.max(1, costumeCount), (value: number) => {
            start = value - 1; end = Math.max(start, end); updateRange(costumeCount);
        });
        endInput = numberField('animationTo', end + 1, Math.max(1, costumeCount), (value: number) => {
            end = value - 1; start = Math.min(start, end); updateRange(costumeCount);
        });
        numberField('animationFPS', fps, 60, (value: number) => { fps = value; });
        panel.appendChild(range);
        crop = createGIFCrop(context, preview, services.stageSize, panelLife, () => {
            stop();
            previewKey = '';
            wake();
        });
        panel.appendChild(crop.element);
        const exportControls = document.createElement('div');
        exportControls.className = 'gwt-animation-controls';
        const exportButton = document.createElement('button') as unknown as HTMLButtonElement;
        exportButton.type = 'button';
        exportButton.textContent = tr('animationExportGIF');
        const exportStatus = document.createElement('div');
        exportStatus.className = 'gwt-animation-hint';
        exportStatus.setAttribute('role', 'status');
        listen(exportButton, 'click', () => {
            exportGIF(exportButton, exportStatus);
        });
        exportControls.appendChild(exportButton);
        panel.append(exportControls, exportStatus);
        const onion = document.createElement('fieldset');
        const legend = document.createElement('legend'); legend.textContent = tr('animationOnion'); onion.appendChild(legend);
        function check(key: string, checked: boolean, change: (value: boolean) => void) {
            const label = document.createElement('label'); const input = document.createElement('input') as unknown as HTMLInputElement;
            input.type = 'checkbox'; input.checked = checked;
            const text = document.createElement('span'); text.textContent = tr(key); label.append(input, text);
            listen(input, 'change', () => {
                change(input.checked);
                overlayKey = '';
                wake();
                if(!enabled) hideOverlay();
            });
            onion.appendChild(label);
        }
        check('animationOnionEnabled', enabled, (value: boolean) => {
            enabled = value;
        });
        check('animationPrevious', previous, (value: boolean) => {
            previous = value;
        });
        check('animationNext', next, (value: boolean) => {
            next = value;
        });
        const alphaLabel = document.createElement('label'); alphaLabel.className = 'gwt-animation-alpha';
        alphaLabel.textContent = tr('opacity');
        const alpha = document.createElement('input') as unknown as HTMLInputElement;
        alpha.type = 'range';
        alpha.min = '0';
        alpha.max = '100';
        alpha.value = String(opacity * 100);
        const value = document.createElement('output'); value.textContent = alpha.value + '%';
        listen(alpha, 'input', () => {
            opacity = Number(alpha.value) / 100;
            value.textContent = alpha.value + '%';
            overlayKey = '';
            wake();
        });
        alphaLabel.append(alpha, value);
        onion.appendChild(alphaLabel);
        panel.appendChild(onion);
        const hint = document.createElement('p');
        hint.className = 'gwt-animation-hint';
        hint.textContent = tr('animationHint');
        panel.appendChild(hint);
        previewKey = ''; tick(window.performance.now());
    }
    document.addEventListener('visibilitychange', () => {
        if(document.hidden) {
            stop();
            hideOverlay();
        }else wake();
    }, { signal: visibility.signal });
    return {
        showPanel,
        sync: wake,
        dispose() {
            disposed = true;
            window.cancelAnimationFrame(raf);
            visibility.abort(); closePanel(); hideOverlay(); clearCache();
        }
    };
}