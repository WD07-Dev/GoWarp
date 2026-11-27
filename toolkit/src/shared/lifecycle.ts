export function createLifecycle() {
    const browser: Window = window;
    const listeners = new AbortController();
    const timers = new Set<number>();
    const intervals = new Set<number>();
    let disposed = false;

    function listen<K extends keyof GlobalEventHandlersEventMap>(
        target: EventTarget,
        name: K,
        callback: (event: GlobalEventHandlersEventMap[K]) => void,
        options: boolean | AddEventListenerOptions = false
    ) {
        target.addEventListener(name, callback as EventListener, {
            ...(typeof options === 'boolean' ? {capture: options} : options),
            signal: listeners.signal
        });
    }

    function later(callback: () => void, delay: number): number {
        const id = browser.setTimeout(() => {
            timers.delete(id);
            if (!disposed) callback();
        }, delay);
        timers.add(id);
        return id;
    }

    function repeat(callback: () => void, delay: number): number {
        const id = browser.setInterval(() => {
            if (!disposed) callback();
        }, delay);
        intervals.add(id);
        return id;
    }

    return {
        listen,
        later,
        repeat,
        dispose() {
            if (disposed) return;
            disposed = true;
            listeners.abort();
            timers.forEach(id => browser.clearTimeout(id));
            intervals.forEach(id => browser.clearInterval(id));
            timers.clear();
            intervals.clear();
        }
    };
}