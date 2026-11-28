import type { AnimationServices, PaintContext } from './type';
import { createLifecycle } from '../shared/lifecycle';
import { tr } from '../translate';

export function createGIFCrop(context: PaintContext, preview: HTMLCanvasElement,
    stageSize: AnimationServices['stageSize'], life: ReturnType<typeof createLifecycle>, changed: () => void) {
    const { document } = context;
    const fieldset = document.createElement('fieldset');
    fieldset.className = 'gwt-animation-crop';
    const label = document.createElement('label');
    const enabled = document.createElement('input') as unknown as HTMLInputElement;
    enabled.type = 'checkbox';
    label.append(enabled, document.createTextNode(tr('animationCrop')));
    const fields = document.createElement('div');
    fields.className = 'gwt-animation-range';
    const inputs = {} as Record<'x' | 'y' | 'width' | 'height', HTMLInputElement>;
    let stage = { width: 0, height: 0 };
    let area = {x: 0, y: 0, width: 1, height: 1};
    let drag: {id: number; x: number; y: number; before: typeof area} | null = null;
    let viewWidth = 320, viewHeight = 200;

    function sync(force: boolean = false) {
        const size = stageSize();
        const width = Math.max(1, Math.round(size.width)), height = Math.max(1, Math.round(size.height));
        if(stage.width !== width || stage.height !== height) {
            stage = {width, height};
            area = {x: 0, y: 0, width, height};
            drag = null;
        }
        area.x = Math.max(0, Math.min(stage.width - 1, area.x));
        area.y = Math.max(0, Math.min(stage.height - 1, area.y));
        area.width = Math.max(1, Math.min(stage.width - area.x, area.width));
        area.height = Math.max(1, Math.min(stage.height - area.y, area.height));
        for(const key of ['x', 'y', 'width', 'height'] as const) {
            const input = inputs[key];
            if(!input) continue;
            if(force || document.activeElement !== input) input.value = String(area[key]);
            input.disabled = !enabled.checked;
            input.max = String(key === 'x' ? stage.width - 1 : key === 'y' ? stage.height - 1 :
                key === 'width' ? stage.width - area.x : stage.height - area.y);
        }
        preview.classList.toggle('gwt-crop-active', enabled.checked);
    }
    for(const [key, text] of [['x', 'animationCropLeft'], ['y', 'animationCropTop'],
        ['width', 'animationCropWidth'], ['height', 'animationCropHeight']] as const) {
        const row = document.createElement('label');
        row.textContent = tr(text);
        const input = document.createElement('input') as unknown as HTMLInputElement;
        input.type = 'number'; input.step = '1'; input.min = key === 'x' || key === 'y' ? '0' : '1';
        input.dataset.crop = key;
        inputs[key] = input;
        life.listen(input, 'change', () => {
            const value = Number(input.value);
            if(Number.isFinite(value)) area[key] = Math.round(value);
            sync(true); changed();
        });
        life.listen(input, 'keydown', (event: KeyboardEvent) => event.stopPropagation());
        row.appendChild(input); fields.appendChild(row);
    }
    const reset = document.createElement('button') as unknown as HTMLButtonElement;
    reset.type = 'button'; reset.textContent = tr('animationCropReset');
    const controls = document.createElement('div'); controls.className = 'gwt-animation-controls';
    controls.appendChild(reset);
    const hint = document.createElement('p');
    hint.className = 'gwt-animation-hint'; hint.textContent = tr('animationCropHint');
    fieldset.append(label, fields, controls, hint);
    life.listen(enabled, 'change', () => { drag = null; sync(); changed(); });
    life.listen(reset, 'click', () => {
        drag = null;
        area = {x: 0, y: 0, ...stage};
        sync(true); changed();
    });
    function point(event: PointerEvent) {
        const bounds = preview.getBoundingClientRect();
        const scale = Math.min(viewWidth / stage.width, viewHeight / stage.height);
        return {
            x: Math.round(((event.clientX - bounds.left - preview.clientLeft) * viewWidth / preview.clientWidth - (viewWidth - stage.width * scale) / 2) / scale),
            y: Math.round(((event.clientY - bounds.top - preview.clientTop) * viewHeight / preview.clientHeight - (viewHeight - stage.height * scale) / 2) / scale)
        };
    }
    function move(event: PointerEvent) {
        if(!drag || event.pointerId !== drag.id) return;
        const p = point(event);
        const x = Math.max(0, Math.min(stage.width, p.x)), y = Math.max(0, Math.min(stage.height, p.y));
        area = {x: Math.min(drag.x, x), y: Math.min(drag.y, y), width: Math.abs(x - drag.x), height: Math.abs(y - drag.y)};
        sync(true); changed();
    }
    life.listen(preview, 'pointerdown', (event: PointerEvent) => {
        if(!enabled.checked || event.button !== 0 || drag) return;
        sync();
        const p = point(event);
        if(p.x < 0 || p.y < 0 || p.x >= stage.width || p.y >= stage.height) return;
        event.preventDefault();
        drag = {id: event.pointerId, ...p, before: {...area}};
        preview.setPointerCapture(event.pointerId);
        move(event);
    });
    life.listen(preview, 'pointermove', move);
    life.listen(preview, 'pointerup', (event: PointerEvent) => {
        if(drag?.id !== event.pointerId) return;
        move(event); drag = null;
        preview.releasePointerCapture(event.pointerId);
    });
    function cancel(event: PointerEvent) {
        if(drag?.id !== event.pointerId) return;
        area = drag.before; drag = null; sync(); changed();
    }
    life.listen(preview, 'pointercancel', cancel);
    life.listen(preview, 'lostpointercapture', cancel);
    sync();
    return {
        element: fieldset,
        bounds() {
            sync();
            return enabled.checked ? {...area} : {x: 0, y: 0, ...stage};
        },
        draw(ctx: CanvasRenderingContext2D, width: number, height: number) {
            viewWidth = width; viewHeight = height;
            if(!enabled.checked) return;
            const scale = Math.min(width / stage.width, height / stage.height);
            const left = (width - stage.width * scale) / 2, top = (height - stage.height * scale) / 2;
            const x = left + area.x * scale, y = top + area.y * scale;
            const w = area.width * scale, h = area.height * scale;
            ctx.save();
            ctx.fillStyle = '#0008';
            ctx.fillRect(left, top, stage.width * scale, y - top);
            ctx.fillRect(left, y + h, stage.width * scale, top + stage.height * scale - y - h);
            ctx.fillRect(left, y, x - left, h);
            ctx.fillRect(x + w, y, left + stage.width * scale - x - w, h);
            ctx.strokeStyle = '#000'; ctx.lineWidth = 2; ctx.strokeRect(x, y, w, h);
            ctx.strokeStyle = '#fff'; ctx.lineWidth = 1; ctx.setLineDash([4, 4]); ctx.strokeRect(x, y, w, h);
            ctx.restore();
        }
    };
}