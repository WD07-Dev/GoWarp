import { installExtensions } from './extensions/manager';
import { installPaintTools } from './paint';
import { tr } from './translate';

if(!Scratch.extensions.unsandboxed) {
    throw new Error(tr('errors.unsandboxed'));
}

const key = Symbol.for('gowarp-toolkit.session');
const vm = Scratch.vm;
if(!vm[key]) {
    const paint = installPaintTools(Scratch);
    let removeExtensions: () => void;
    try {
        removeExtensions = installExtensions(Scratch);
    } catch (error) {
        paint.dispose();
        throw error;
    }
    const dispose = () => {
        if(!vm[key]) return;
        removeExtensions();
        paint.dispose();
        window.removeEventListener('pagehide', dispose);
        delete vm[key];
    };
    vm[key] = { dispose };
    window.addEventListener('pagehide', dispose, {once: true});
}