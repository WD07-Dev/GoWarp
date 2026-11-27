export function createIconButton(
    document: {createElement(tag: 'button'): HTMLElement},
    className: string,
    title: string,
    html: string
): HTMLButtonElement {
    const button = document.createElement('button') as HTMLButtonElement;
    button.type = 'button';
    button.className = className;
    button.title = title;
    button.setAttribute('aria-label', title);
    button.innerHTML = html;
    return button;
}

export function isTextInput(target: EventTarget | null): boolean {
    return !!target && /INPUT|TEXTAREA|SELECT/.test((target as Element).tagName);
}