import { tr } from '../translate';

export function createExtensionPolicy(vm: typeof globalThis.Scratch.vm) {
    const sm = vm.extensionManager.securityManager;
    const approvals = new Map<string, string>();
    const originalLoad = sm.canLoadExtensionFromProject;
    const originalMode = sm.getSandboxMode;
    const load = function(this: typeof sm, url: string, ...args: unknown[]) {
        if(approvals.has(url)) return Promise.resolve(true);
        return originalLoad.call(this, url, ...args);
    };
    const mode = function(this: typeof sm, url: string, ...args: unknown[]) {
        const approvedMode = approvals.get(url);
        if(approvedMode === 'unsandboxed' || approvedMode === 'iframe') return approvedMode;
        return originalMode.call(this, url, ...args);
    };
    function install() {
        if(sm.canLoadExtensionFromProject !== originalLoad && sm.canLoadExtensionFromProject !== load ||
            sm.getSandboxMode !== originalMode && sm.getSandboxMode !== mode) {
            throw new Error(tr('extensions.securityChanged'));
        }
        sm.canLoadExtensionFromProject = load;
        sm.getSandboxMode = mode;
    }
    function clear() {
        approvals.clear();
        if(sm.canLoadExtensionFromProject === load) sm.canLoadExtensionFromProject = originalLoad;
        if(sm.getSandboxMode === mode) sm.getSandboxMode = originalMode;
    }
    return { approvals, install, clear };
}

export type ExtensionPolicy = ReturnType<typeof createExtensionPolicy>;