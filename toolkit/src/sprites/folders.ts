import { FolderMenuItem, FolderServices, SpriteInfo, FolderData } from './type';
import { PaintContext, PaintElement } from '../paint/type';
import { createIconButton } from '../shared/dom';
import { tr } from '../translate';

export function createFolders(context: PaintContext, services: FolderServices) {
    const { window, document } = context;
    const { paintedColor, parseColor, brightness, cssVar, applyTheme, rgba } = context.theme;
    const { listen, later } = context.lifecycle;
    const { findVM, ensureStyle } = services;

    var pageToastEl = null;
    var pageToastTimer = 0;

    function pageToast(text: string, bad?: boolean) {
        if (!text) return;
        if (!pageToastEl) {
            pageToastEl = document.createElement('div');
            pageToastEl.className = 'gwt-toast';
            document.body.appendChild(pageToastEl);
        }
        pageToastEl.textContent = text;
        pageToastEl.classList.toggle('gwt-bad', !!bad);
        pageToastEl.classList.add('gwt-on');
        window.clearTimeout(pageToastTimer);
        pageToastTimer = later(function () {
            if (pageToastEl) pageToastEl.classList.remove('gwt-on');
        }, 2400);
    }

    var SF_FOLDER = '<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">' + '<path d="M10 4H4a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-8l-2-2z"/></svg>';
    var SF_FOLDER_PLUS = '<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">' + '<path d="M10 4H4a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-8l-2-2z' + 'm.5 5H13v2.5h2.5V14H13v2.5h-2.5V14H8v-1.5h2.5V10z"/></svg>';
    var SF_CHECK = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" ' + 'stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m5 13 4 4L19 7"/></svg>';

    var sf = {
        bar: null,
        hint: null,
        menu: null,
        obs: null,
        obsItems: null,
        raf: 0,
        sig: '',
        shown: '',
        active: '',
        project: null,
        data: null as FolderData | null,
        sortable: null,
        openFolders: null as string[] | null,
        hidden: new Map<HTMLElement, string>(),
        theme: '',
        editing: false,
        mounted: false,
        count: 0
    };

    function sfLoad() {
        const vm = findVM();
        const project = vm?.runtime?.targets?.find((target: any) => target.isStage);
        if (sf.project !== project) {
            sf.project = project;
            sf.active = '';
            sf.sig = '';
            sf.count = 0;
        }
        const names = new Set(sfSprites().map(sfFolderOf).filter(Boolean));
        sf.data = {folders: Array.from(names, (name: string) => ({id: name, name}))};
        return sf.data;
    }
    function sfPane () {
        return document.querySelector('[class*="sprite-selector_sprite-selector"]');
    }
    function sfTiles (): HTMLElement[] {
        var scroll = document.querySelector('[class*="sprite-selector_scroll-wrapper"]');
        if (!scroll) return [];
        return Array.prototype.slice.call(scroll.querySelectorAll('[class*="sprite-selector-item_sprite-selector-item"]'));
    }
    function sfTileName (tile: Element) {
        var n = tile && tile.querySelector('[class*="sprite-selector-item_sprite-name"]');
        return n ? (n.textContent || '') : '';
    }
    function sfSprites (): SpriteInfo[] {
        var out = [];
        var vm = findVM();
        if(vm && vm.runtime && vm.runtime.targets) {
            vm.runtime.targets.forEach(function (t: any) {
                if(t && t.isOriginal !== false && !t.isStage && t.sprite && t.sprite.name) {
                    out.push({id: t.id, name: String(t.sprite.name)});
                }
            });
        }
        return out;
    }

    function sfFolderOf(sprite: SpriteInfo): string {
        const divider = sprite.name.indexOf('//');
        return divider > 0 ? sprite.name.slice(0, divider) : '';
    }
    function sfTileSprite(tile: Element, sprites: SpriteInfo[]): SpriteInfo | undefined {
        if (tile.classList.contains('sa-folders-folder')) return;
        let fiber = context.editor.fiberOf(tile);
        for (let i = 0; fiber && i < 30; i++, fiber = fiber.return) {
            const props = fiber.stateNode?.props || fiber.memoizedProps;
            if (props?.dragType === 'SPRITE' && typeof props.id === 'string') {
                return sprites.find((sprite: SpriteInfo) => sprite.id === props.id);
            }
        }
        const name = sfTileName(tile);
        return sprites.find((sprite: SpriteInfo) => sprite.name === name);
    }
    function sfOpenFolders() {
        const tile = sfTiles()[0];
        let fiber = tile && context.editor.fiberOf(tile);
        for (let i = 0; fiber && i < 40; i++, fiber = fiber.return) {
            const sortable = fiber.stateNode;
            if (!Array.isArray(sortable?.state?.folders) ||
                !Array.isArray(sortable.props?.items) ||
                typeof sortable.handleAddSortable !== 'function') continue;
            if (sf.sortable !== sortable) {
                sf.sortable = sortable;
                sf.openFolders = [...sortable.state.folders];
            }
            const folders = sf.data.folders.map((folder: {id: string}) => folder.id);
            if (folders.some((name: string) => !sortable.state.folders.includes(name))) {
                sortable.setState({folders});
            }
            return;
        }
    }
    function sfFolderName(id: string) {
        for(let i=0; i < sf.data.folders.length; i++) {
            if(sf.data.folders[i].id === id) return sf.data.folders[i].name;
        }
        return '';
    }
    function sfNameTaken(name: string, exceptId?: string) {
        var lower = name.toLowerCase();
        return sfLoad().folders.some(function (f: { id: string; name: string; }) {
            return f.id !== exceptId && f.name.toLowerCase() === lower;
        });
    }
    function sfRenameSprites(sprites: SpriteInfo[], folderName: string) {
        const vm = findVM();
        if (!vm || typeof vm.renameSprite !== 'function') return;
        for (const sprite of sprites) {
            const folder = sfFolderOf(sprite);
            const base = folder ? sprite.name.slice(folder.length + 2) : sprite.name;
            let name = folderName ? folderName + '//' + base : base;
            if (!name || ['_mouse_', '_stage_', '_edge_', '_myself_', '_random_'].includes(name)) name += '2';
            vm.renameSprite(sprite.id, name);
        }
        vm.emitWorkspaceUpdate?.();
        vm.emitProjectChanged?.();
        sf.sig = '';
        sfLoad();
    }
    function sfAssign(sprite: SpriteInfo, folderId: string) {
        sfRenameSprites([sprite], folderId);
        sfSync();
    }
    function sfValidName(name: string): boolean {
        if (!name || name.includes('//') || name.endsWith('/')) {
            pageToast(tr('sfInvalidName'), true);
            return false;
        }
        return true;
    }
    function sfMakeFolder(name: string, sprite: SpriteInfo) {
        if (!name) {
            const base = tr('sfDefault');
            let n = 1;
            do { name = base + ' ' + n++; } while (sfNameTaken(name));
        }
        if (!sfValidName(name)) return null;
        if (sfNameTaken(name)) {
            pageToast(tr('sfDup'), true);
            return null;
        }
        sfRenameSprites([sprite], name);
        return {id: name, name};
    }
    function sfRenameFolder(folderId: string, name: string) {
        if (!sfValidName(name)) return;
        if (sfNameTaken(name, folderId)) { pageToast(tr('sfDup'), true); return; }
        sfRenameSprites(sfSprites().filter((sprite: SpriteInfo) => sfFolderOf(sprite) === folderId), name);
        if (sf.active === folderId) sf.active = name;
        pageToast(tr('sfRenamed'));
    }

    function sfPaint(node: PaintElement) {
        var pane = sfPane() || document.body;
        var surface = paintedColor(pane, 'backgroundColor') || '#ffffff';
        var c = parseColor(surface) || {r: 255, g: 255, b: 255, a: 1};
        var dark = brightness(c) < 0.55;
        var selected = pane.querySelector ? pane.querySelector('[class*="sprite-selector-item_is-selected"]') : null;
        var accent = selected ? window.getComputedStyle(selected).borderTopColor : '';
        if(!parseColor(accent) || /^rgba\(0,\s*0,\s*0,\s*0\)$/.test(accent)) {
            accent = cssVar(window.getComputedStyle(pane), '--looks-secondary') || '#4c97ff';
        }
        var ac = parseColor(accent) || {r: 76, g: 151, b: 255, a: 1};
        applyTheme(node, {
            dark: dark,
            surface: surface,
            accent: accent,
            text: dark ? '#eeeeee' : '#575e75',
            field: dark ? 'rgba(255,255,255,.07)' : 'rgba(0,0,0,.05)',
            border: dark ? 'rgba(255,255,255,.16)' : 'rgba(0,0,0,.12)',
            hover: dark ? 'rgba(255,255,255,.12)' : 'rgba(0,0,0,.08)',
            active: rgba(ac, 0.35),
            shadow: dark ? '0 8px 24px rgba(0,0,0,.5)' : '0 8px 24px rgba(148,162,188,.35)'
        });
        node.style.setProperty('--gwt-accent-soft', rgba(ac, dark ? 0.22 : 0.13));
        node.style.colorScheme = dark ? 'dark' : 'light';
    }

    function sfEnsureBar(scroll: Element) {
        if (sf.bar && sf.bar.parentElement === scroll) return;
        if (sf.bar) sf.bar.remove();
        var bar = document.createElement('div');
        bar.className = 'gwt-folders';
        bar.setAttribute('role', 'tablist');
        sf.bar = bar;
        sf.mounted = true;
        sf.sig = '';
        scroll.insertBefore(bar, scroll.firstChild);
        sfPaint(bar);
    }
    function sfEnsureHint(scroll: Element) {
        if(sf.hint && sf.hint.parentElement === scroll) return sf.hint;
        if(sf.hint) sf.hint.remove();
        sf.hint = document.createElement('div');
        sf.hint.className = 'gwt-fempty';
        sf.hint.style.display = 'none';
        scroll.insertBefore(sf.hint, sf.bar && sf.bar.nextSibling ? sf.bar.nextSibling : scroll.firstChild);
        sfPaint(sf.hint);
        return sf.hint;
    }
    function sfChip(folderId: string, label: string, count: number, active: boolean, isFolder: boolean) {
        var chip = document.createElement('button');
        chip.type = 'button';
        chip.className = 'gwt-fchip';
        chip.dataset.folder = String(folderId);
        chip.setAttribute('role', 'tab');
        chip.setAttribute('aria-selected', String(active));
        if(isFolder) {
            chip.title = tr('sfHint');
            chip.innerHTML = SF_FOLDER;
        }
        var name = document.createElement('span');
        name.className = 'gwt-fname';
        name.textContent = label;
        var num = document.createElement('span');
        num.className = 'gwt-fnum';
        num.textContent = String(count);
        chip.append(name, num);
        listen(chip, 'click', function (event: PointerEvent) {
            if(event.detail > 1) return;
            sf.active = folderId;
            sfSync();
        });
        if(isFolder) {
            listen(chip, 'dblclick', function (event: MouseEvent) {
                event.preventDefault();
                sfRenameChip(chip, folderId);
            });
            listen(chip, 'contextmenu', function (event: PointerEvent) {
                event.preventDefault();
                event.stopPropagation();
                sfChipMenu(event.clientX, event.clientY, folderId);
            });
        }
        return chip;
    }
    function sfRenderBar(sprites: SpriteInfo[]) {
        if(!sf.bar || sf.editing) return;
        var data = sfLoad();
        if(sf.active && !data.folders.some(function (f: { id: string; name: string; }) { return f.id === sf.active; })) sf.active = '';
        var counts = {};
        sprites.forEach(function (s: SpriteInfo) {
            var f = sfFolderOf(s);
            counts[f] = (counts[f] || 0) + 1;
        });
        var sig = JSON.stringify([tr().lang, sprites.length, counts, data.folders.map((f: { id: string; name: string; }) => [f.id, f.name])]);
        if(sig === sf.sig) {
            if(sf.shown !== sf.active) {
                sf.shown = sf.active;
                Array.prototype.forEach.call(sf.bar.querySelectorAll('.gwt-fchip[data-folder]'), function (chip: HTMLElement) {
                    chip.setAttribute('aria-selected', String((chip.dataset.folder || '') === sf.active));
                });
            }
            return;
        }
        sf.sig = sig;
        sf.shown = sf.active;
        sf.bar.textContent = '';
        sf.bar.appendChild(sfChip('', tr('sfAll'), sprites.length, sf.active === '', false));
        data.folders.forEach(function (f: { id: string; name: string; }) {
            sf.bar.appendChild(sfChip(f.id, f.name, counts[f.id] || 0, sf.active === f.id, true));
        });
        var add = document.createElement('button');
        add.type = 'button';
        add.className = 'gwt-fchip gwt-fnew';
        add.title = tr('sfNewSelected');
        add.setAttribute('aria-label', tr('sfNew'));
        add.textContent = '+';
        listen(add, 'click', function () { sfNewChip(add); });
        sf.bar.appendChild(add);
    }
    function sfNameInput(holder: HTMLElement, initial: string, commit: (name: string) => void, cancel?: () => void) {
        if(sf.editing) return;
        sf.editing = true;
        var input = document.createElement('input');
        input.className = 'gwt-finput';
        input.type = 'text';
        input.maxLength = 24;
        input.value = initial || '';
        input.setAttribute('aria-label', tr('sfName'));
        holder.textContent = '';
        holder.appendChild(input);
        input.focus();
        input.select();
        var done = false;
        function finish (ok: boolean) {
            if (done) return;
            done = true;
            sf.editing = false;
            var value = input.value.trim();
            if (ok && value) commit(value);
            else if (cancel) cancel();
            sf.sig = '';
            sfSync();
        }
        listen(input, 'keydown', function (event: KeyboardEvent) {
            event.stopPropagation();
            if(event.key === 'Enter') { event.preventDefault(); finish(true); }
            else if(event.key === 'Escape') { event.preventDefault(); finish(false); }
        });
        listen(input, 'blur', function () { finish(true); });
        listen(input, 'pointerdown', function (event: PointerEvent) { event.stopPropagation(); });
        listen(input, 'click', function (event: PointerEvent) { event.stopPropagation(); });
    }
    function sfNewChip(addBtn: HTMLElement) {
        if(!sf.bar || sf.editing) return;
        const sprite = sfSprites().find((item: SpriteInfo) => item.id === findVM()?.editingTarget?.id);
        if (!sprite) { pageToast(tr('sfSelectSprite'), true); return; }
        var holder = document.createElement('span');
        holder.className = 'gwt-fchip gwt-fhold';
        sf.bar.insertBefore(holder, addBtn);
        sfNameInput(holder, '', function (name: string) {
            const folder = sfMakeFolder(name, sprite);
            if (!folder) return;
            sf.active = folder.id;
            pageToast(tr('sfCreated'));
        }, function () {
            holder.remove();
        });
    }
    function sfRenameChip(chip: HTMLElement, folderId: string) {
        var folder = sfLoad().folders.filter(function (f: { id: string; name: string; }) { return f.id === folderId; })[0];
        if(!folder) return;
        sfNameInput(chip, folder.name, function (name: string) {
            sfRenameFolder(folderId, name);
        });
    }
    function sfDeleteFolder(folderId: string) {
        sfRenameSprites(sfSprites().filter((sprite: SpriteInfo) => sfFolderOf(sprite) === folderId), '');
        if (sf.active === folderId) sf.active = '';
        pageToast(tr('sfDeleted'));
        sfSync();
    }

    function sfCloseMenu() {
        if(sf.menu) {
            sf.menu.remove();
            sf.menu = null;
        }
    }
    function sfMenu(x: number, y: number, rows: FolderMenuItem[]) {
        sfCloseMenu();
        var menu = document.createElement('div');
        menu.className = 'gwt-fmenu';
        sfPaint(menu);
        rows.forEach(function (row: FolderMenuItem) {
            if (row.sep) {
                menu.appendChild(document.createElement('hr'));
                return;
            }
            if (row.head !== undefined) {
                var head = document.createElement('div');
                head.className = 'gwt-fmhead';
                head.textContent = row.head;
                menu.appendChild(head);
                return;
            }
            var b = document.createElement('button');
            b.type = 'button';
            var mark = document.createElement('span');
            mark.className = 'gwt-fmcheck';
            if (row.checked) mark.innerHTML = SF_CHECK;
            b.appendChild(mark);
            if (row.icon) {
                var ic = document.createElement('span');
                ic.className = 'gwt-fmcheck';
                ic.innerHTML = row.icon;
                b.appendChild(ic);
            }
            var label = document.createElement('span');
            label.className = 'gwt-fmlabel';
            label.textContent = row.label;
            b.appendChild(label);
            listen(b, 'click', function () {
                sfCloseMenu();
                row.run();
            });
            menu.appendChild(b);
        });
        document.body.appendChild(menu);
        var rect = menu.getBoundingClientRect();
        menu.style.left = Math.max(8, Math.min(x, window.innerWidth - rect.width - 8)) + 'px';
        menu.style.top = Math.max(8, Math.min(y, window.innerHeight - rect.height - 8)) + 'px';
        sf.menu = menu;
    }
    function sfChipMenu(x: number, y: number, folderId: string) {
        sfMenu(x, y, [
            {head: sfFolderName(folderId)},
            {label: tr('sfRename'), icon: SF_FOLDER, run: function () {
                var chip = sf.bar && Array.from((sf.bar as HTMLElement).querySelectorAll<HTMLElement>('.gwt-fchip')).find(
                    (item: HTMLElement) => item.dataset.folder === folderId
                );
                if (chip) sfRenameChip(chip, folderId);
            }},
            {label: tr('sfDelete'), run: function () { sfDeleteFolder(folderId); }}
        ]);
    }
    function sfSpriteMenu(tile: Element, anchor: Element) {
        var sprite = sfTileSprite(tile, sfSprites());
        if(!sprite) return;
        var current = sfFolderOf(sprite);
        var data = sfLoad();
        var rows: FolderMenuItem[] = [{head: sprite.name}];
        data.folders.forEach(function (f: { id: string; name: string; }) {
            rows.push({label: f.name, icon: SF_FOLDER, checked: f.id === current, run: function () {
                sfAssign(sprite, f.id);
                pageToast(tr('sfMoved').replace('{s}', sprite.name).replace('{f}', f.name));
            }});
        });
        if(data.folders.length) rows.push({sep: true});
        if(current) {
            rows.push({label: tr('sfNone'), run: function () { sfAssign(sprite, ''); }});
        }
        rows.push({label: tr('sfNew'), icon: SF_FOLDER_PLUS, run: function () {
            var folder = sfMakeFolder('', sprite);
            if (!folder) return;
            sfSync();
            pageToast(tr('sfCreated'));
        }});
        var r = anchor.getBoundingClientRect();
        sfMenu(r.left, r.bottom + 4, rows);
    }
    function sfDecorate() {
        sfTiles().forEach(function (tile: HTMLElement) {
            if(tile.classList.contains('sa-folders-folder') || tile.querySelector('.gwt-sf-tile')) return;
            var b = createIconButton(document, 'gwt-sf-tile', tr('sfMove'), SF_FOLDER);
            listen(b, 'click', function (event: PointerEvent) {
                event.preventDefault();
                event.stopPropagation();
                sfSpriteMenu(tile, b);
            });
            listen(b, 'pointerdown', function (event: PointerEvent) { event.stopPropagation(); });
            listen(b, 'dblclick', function (event: MouseEvent) { event.stopPropagation(); });
            tile.appendChild(b);
        });
    }
    function sfApplyFilter(sprites: SpriteInfo[]) {
        for (const [wrap] of sf.hidden) {
            if (!wrap.isConnected) sf.hidden.delete(wrap);
        }
        sfTiles().forEach(function (tile: HTMLElement) {
            const sprite = sfTileSprite(tile, sprites);
            const show = !tile.classList.contains('sa-folders-folder') &&
                (!sf.active || (sprite && sfFolderOf(sprite) === sf.active));
            const wrap = tile.parentElement;
            if (!wrap) return;
            if (!show) {
                if (!sf.hidden.has(wrap)) sf.hidden.set(wrap, wrap.style.display);
                if (wrap.style.display !== 'none') wrap.style.display = 'none';
            } else if (sf.hidden.has(wrap)) {
                wrap.style.display = sf.hidden.get(wrap);
                sf.hidden.delete(wrap);
            }
        });
    }
    function sfWatch(items: Node) {
        if(!window.MutationObserver) return;
        if(sf.obs && sf.obsItems === items) return;
        if(sf.obs) sf.obs.disconnect();
        var obs = new MutationObserver(function () {
            if(sf.raf) return;
            sf.raf = window.requestAnimationFrame(function () {
                sf.raf = 0;
                sfSync();
            });
        });
        obs.observe(items, {childList: true, subtree: true, characterData: true});
        sf.obs = obs;
        sf.obsItems = items;
    }
    function sfSync() {
        var scroll = document.querySelector('[class*="sprite-selector_scroll-wrapper"]');
        var items = scroll && scroll.querySelector('[class*="sprite-selector_items-wrapper"]');
        if(!scroll || !items) { sfTeardown(); return; }
        ensureStyle();

        var sprites = sfSprites();
        sfLoad();
        sfOpenFolders();
        if(sf.active && sf.count && sprites.length > sf.count) sf.active = '';
        sf.count = sprites.length;
        sfEnsureBar(scroll);
        var surface = paintedColor(sfPane() || document.body, 'backgroundColor') || '';
        if(surface !== sf.theme) {
            sf.theme = surface;
            sfPaint(sf.bar);
            if(sf.hint) sfPaint(sf.hint);
            if(sf.menu) sfPaint(sf.menu);
        }
        sfRenderBar(sprites);
        sfDecorate();
        sfApplyFilter(sprites);

        var hint = sfEnsureHint(scroll);
        var inFolder = 0;
        if(sf.active) {
            sprites.forEach(function (s: SpriteInfo) { if (sfFolderOf(s) === sf.active) inFolder++; });
        }
        if(sf.active && !inFolder && sfFolderName(sf.active)) {
            hint.textContent = tr('sfEmpty');
            hint.style.display = '';
        }else {
            hint.style.display = 'none';
        }
        sfWatch(items);
    }
    function sfTeardown() {
        if(!sf.mounted) return;
        sf.mounted = false;
        if(sf.obs) {
            sf.obs.disconnect();
            sf.obs = null;
            sf.obsItems = null;
        }
        if(sf.raf) {
            window.cancelAnimationFrame(sf.raf);
            sf.raf = 0;
        }
        if(sf.bar) {
            sf.bar.remove();
            sf.bar = null;
        }
        if(sf.hint) {
            sf.hint.remove();
            sf.hint = null;
        }
        sfCloseMenu();
        sf.editing = false;
        sf.sig = '';
        sf.shown = '';
        sf.active = '';
        sf.count = 0;
        sf.theme = '';
        Array.prototype.forEach.call(document.querySelectorAll('.gwt-sf-tile'), function (b: HTMLElement) { b.remove(); });
        for (const [wrap, display] of sf.hidden) wrap.style.display = display;
        sf.hidden.clear();
        if (sf.sortable?.ref?.isConnected) sf.sortable.setState({folders: sf.openFolders});
        sf.sortable = null;
        sf.openFolders = null;
    }

    listen(document, 'pointerdown', function (event: PointerEvent) {
        if(sf.menu && !sf.menu.contains(event.target)) sfCloseMenu();
    }, true);
    listen(window, 'resize', sfCloseMenu);
    listen(window, 'scroll', sfCloseMenu, true);
    listen(document, 'keydown', function (event: KeyboardEvent) {
        if (event.key === 'Escape') sfCloseMenu();
    }, true);
    return {
        sfSync,
        sfTeardown,
    };
}