import { createExtensionPolicy } from './security';
import { createExtensionDialog } from './dialog';
import { toolkitExtensionId } from './type';
import { extensionStyle } from './style';
import { tr } from '../translate';

export function installExtensions(Scratch: typeof globalThis.Scratch): () => void {
    const vm = Scratch.vm;
    if(typeof vm._loadExtensions !== 'function') {
        throw new Error(tr('extensions.noLoadHook'));
    }
    const originalExtensionLoad = vm._loadExtensions;
    const policy = createExtensionPolicy(vm);
    const dialog = createExtensionDialog(vm, policy);
    const style = document.createElement('style');
    style.id = 'gowarp-toolkit-style';
    style.textContent = extensionStyle;
    document.head.appendChild(style);
    const extensionLoad = async function(this: typeof vm, extensionIDs: Iterable<string>, extensionURLs: Map<string, string> = new Map(), ...args: unknown[]) {
        const urls: Record<string, string> = Object.create(null);
        const manager = this.extensionManager;
        if(extensionIDs && typeof extensionIDs[Symbol.iterator] === 'function' && typeof extensionURLs?.get === 'function') {
            for(const id of extensionIDs) {
                if(manager.isExtensionLoaded(id) || manager.isBuiltinExtension(id)) continue;
                const url = extensionURLs.get(id);
                if(typeof url !== 'string' || !/^(https?:\/\/|data:)/i.test(url)) continue;
                if(url.startsWith('https://extensions.turbowarp.org/') || url.startsWith('http://localhost:8000/')) continue;
                urls[id] = url;
            }
        }
        if(Object.values(urls).some((url: string) => !policy.approvals.has(url))) {
            await new Promise<void>((resolve: (value: void | PromiseLike<void>) => void, reject: (reason?: unknown) => void) => dialog.open({project: {extensionURLs: urls}, resolve, reject}));
        }
        return originalExtensionLoad.call(this, extensionIDs, extensionURLs, ...args);
    };
    vm._loadExtensions = extensionLoad;
    class Toolkit {
        getInfo(): Omit<Scratch.Info, 'blocks'> & {blocks: Scratch.ExtensionButton[]} {
            return {id:toolkitExtensionId,name:tr('toolkitName'),color1:'#b52e35',blocks:[
                {blockType:Scratch.BlockType.BUTTON,text:tr('toolkitName'),func:'open'},
            ]};
        }
        open() {
            dialog.open();
        }
    }
    Scratch.extensions.register(new Toolkit());
    return () => {
        dialog.close();
        policy.clear();
        if (vm._loadExtensions === extensionLoad) vm._loadExtensions = originalExtensionLoad;
        style.remove();
    };
}