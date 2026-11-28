/// <reference types="@turbowarp/types-tw" />
/// <reference types="@turbowarp/types-tw/types/scratch-vm-extension" />

interface VM {
    _loadExtensions(extensionIDs: Iterable<string>, extensionURLs?: Map<string, string>): Promise<unknown[]>;
    toJSON(targetId?: string, options?: {allowOptimization?: boolean}): string;
}

declare namespace VM {
    interface ExtensionManager {
        getExtensionURLs(): Record<string, string>;
        isBuiltinExtension(extensionID: string): boolean;
    }
}

declare namespace Scratch {
    namespace translate {
        const language: string;
    }

    interface ExtensionButton {
        blockType: 'button';
        text: string;
        func: string;
    }

    namespace extensions {
        function register(extensionObject: {
            getInfo(): Omit<Info, 'blocks'> & {blocks: (Block | ExtensionButton | string)[]};
        }): void;
    }
}