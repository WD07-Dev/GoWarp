import { GIFEncoder, quantize, applyPalette } from 'gifenc';

export function createAnimationGIF(width: number, height: number, fps: number) {
    if(![width, height].every((value: number) => Number.isInteger(value) && value > 0 && value <= 65535) ||
        !Number.isFinite(fps) || fps < 1 || fps > 60) {
        throw new Error('Invalid GIF size or frame rate');
    }
    const encoder = GIFEncoder();
    let frames = 0, finished = false;
    return {
        write(data: Uint8ClampedArray): void {
            if(finished || data.length !== width * height * 4) throw new Error('Invalid GIF frame');
            const palette = quantize(data, 255);
            const index = applyPalette(data, palette);
            for(let i=0; i < index.length; i++) {
                index[i] = data[i * 4 + 3] < 128 ? 0 : index[i] + 1;
            }
            palette.unshift([0, 0, 0]);
            const delay = (Math.round((frames + 1) * 100 / fps) - Math.round(frames * 100 / fps)) * 10;
            encoder.writeFrame(index, width, height, {
                palette, delay, transparent: true, transparentIndex: 0, dispose: 2, repeat: 0
            });
            frames++;
        },
        finish(): Uint8Array<ArrayBuffer> {
            if(!frames || finished) throw new Error('No unfinished GIF to save');
            encoder.finish();
            finished = true;
            return new Uint8Array(encoder.bytesView());
        }
    };
}