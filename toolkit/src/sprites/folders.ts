import { FolderMenuItem, FolderServices, SpriteInfo, FolderData } from './type';
import { PaintContext, PaintElement } from '../paint/type';
import { createIconButton } from '../shared/dom';
import { tr } from '../translate';

export function createFolders(context: PaintContext, services: FolderServices) {
    const { window, document } = context;
    const { paintedColor, parseColor, brightness, cssVar, applyTheme, rgba } = context.theme;
    const { listen } = context.lifecycle;
    const { findVM, pageToast, ensureStyle } = services;

    var SF_KEY = 'gowarp-toolkit.folders';
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
        project: '',
        data: null as FolderData | null,
        links: {},
        theme: '',
        editing: false,
        mounted: false,
        count: 0
    };

    function sfAll () {
        try {
            var raw = window.localStorage.getItem(SF_KEY);
            var all = raw ? JSON.parse(raw) : {};
            return all && typeof all === 'object' ? all : {};
        } catch (e) {
            return {};
        }
    }
    function sfSave () {
        try {
            var all = sfAll();
            all[sf.project] = sf.data;
            window.localStorage.setItem(SF_KEY, JSON.stringify(all));
        } catch (e) {}
    }
    function sfProjectKey () {
        var p = location.pathname;
        var m = p.match(/\/projects\/(\d+)/);
        if (m) return 'scratch:' + m[1];
        m = p.match(/\/(\d{6,})(?:\/|$)/);
        if (m) return location.hostname + ':' + m[1];
        var title = String(document.title || '').replace(/\s+/g, ' ').trim().slice(0, 80);
        return location.hostname + ':' + p + ':' + title;
    }
    function sfLoad () {
        var key = sfProjectKey();
        if (sf.project !== key) {
            sf.project = key;
            var raw = sfAll()[key];
            var data: FolderData = {folders: [], marks: []};
            if (raw && Array.isArray(raw.folders)) {
                raw.folders.forEach(function (f: {id: string; name: string}) {
                    if (f && typeof f.id === 'string' && typeof f.name === 'string') {
                        data.folders.push({id: f.id, name: f.name.slice(0, 24)});
                    }
                });
            }
            if (raw && Array.isArray(raw.marks)) {
                raw.marks.forEach(function (m: {n: string; f: string}) {
                    if (m && typeof m.n === 'string' && typeof m.f === 'string') data.marks.push({n: m.n, f: m.f});
                });
            }
            sf.data = data;
            sf.links = {};
            sf.active = '';
            sf.sig = '';
            sf.count = 0;
        }
        return sf.data;
    }
    function sfId () {
        return 'f' + Date.now().toString(36) + Math.random().toString(36).slice(2, 6);
    }
    function sfPane () {
        return document.querySelector('[class*="sprite-selector_sprite-selector"]');
    }
    function sfTiles (): HTMLElement[] {
        var scroll = document.querySelector('[class*="sprite-selector_scroll-wrapper"]');
        if (!scroll) return [];
        return Array.prototype.slice.call(
            scroll.querySelectorAll('[class*="sprite-selector-item_sprite-selector-item"]'));
    }
    function sfTileName (tile: Element) {
        var n = tile && tile.querySelector('[class*="sprite-selector-item_sprite-name"]');
        return n ? (n.textContent || '').trim() : '';
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
            if (out.length) return out;
        }
        sfTiles().forEach(function (tile: HTMLElement) {
            var name = sfTileName(tile);
            if (name) out.push({id: 'dom:' + name, name: name});
        });
        return out;
    }

    function sfLink(sprites: SpriteInfo[]) {
        var data = sfLoad();
        var live = {};
        sprites.forEach(function (s: SpriteInfo) { live[s.id] = true; });
        Object.keys(sf.links).forEach(function (id: string) {
            if (!live[id]) delete sf.links[id];
        });
        var used = [];
        Object.keys(sf.links).forEach(function (id: string) { used.push(sf.links[id]); });
        sprites.forEach(function (s: SpriteInfo) {
            var link = sf.links[s.id];
            if(link) {
                if(link.n !== s.name) {
                    link.n = s.name; sfSave();
                }
                return;
            }
            for(let i=0; i < data.marks.length; i++) {
                var mark = data.marks[i];
                if (used.indexOf(mark) < 0 && mark.n === s.name) {
                    sf.links[s.id] = mark;
                    used.push(mark);
                    return;
                }
            }
        });
    }
    function sfFolderOf(sprite: SpriteInfo): string {
        var link = sf.links[sprite.id];
        if(!link) return '';
        for(let i=0; i < sf.data.folders.length; i++) {
            if(sf.data.folders[i].id === link.f) return link.f;
        }
        return '';
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
    function sfAssign(sprite: SpriteInfo, folderId: string) {
        var data = sfLoad();
        var link = sf.links[sprite.id];
        if(!link) {
            link = {n: sprite.name, f: ''};
            sf.links[sprite.id] = link;
            data.marks.push(link);
        }
        link.n = sprite.name;
        link.f = folderId || '';
        if(!link.f) {
            var i = data.marks.indexOf(link);
            if(i >= 0) data.marks.splice(i, 1);
        }
        sfSave();
        sf.sig = '';
        sfSync();
    }
    function sfMakeFolder(name: string) {
        var data = sfLoad();
        if(!name || sfNameTaken(name)) {
            var base = tr('sfDefault');
            var n = 1;
            do {
                name = base + ' ' + n++;
            } while (sfNameTaken(name));
        }
        var folder = {id: sfId(), name: String(name).slice(0, 24)};
        data.folders.push(folder);
        sfSave();
        return folder;
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
        add.title = tr('sfNew');
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
            if (event.key === 'Enter') { event.preventDefault(); finish(true); }
            else if (event.key === 'Escape') { event.preventDefault(); finish(false); }
        });
        listen(input, 'blur', function () { finish(true); });
        listen(input, 'pointerdown', function (event: PointerEvent) { event.stopPropagation(); });
        listen(input, 'click', function (event: PointerEvent) { event.stopPropagation(); });
    }
    function sfNewChip(addBtn: HTMLElement) {
        if(!sf.bar || sf.editing) return;
        var holder = document.createElement('span');
        holder.className = 'gwt-fchip gwt-fhold';
        sf.bar.insertBefore(holder, addBtn);
        sfNameInput(holder, '', function (name: string) {
            if (sfNameTaken(name)) { pageToast(tr('sfDup'), true); return; }
            sfMakeFolder(name);
            sf.active = '';
            pageToast(tr('sfCreated'));
        }, function () {
            holder.remove();
        });
    }
    function sfRenameChip(chip: HTMLElement, folderId: string) {
        var folder = sfLoad().folders.filter(function (f: { id: string; name: string; }) { return f.id === folderId; })[0];
        if(!folder) return;
        sfNameInput(chip, folder.name, function (name: string) {
            if (sfNameTaken(name, folderId)) { pageToast(tr('sfDup'), true); return; }
            folder.name = name;
            sfSave();
            pageToast(tr('sfRenamed'));
        });
    }
    function sfDeleteFolder (folderId: string) {
        var data = sfLoad();
        data.folders = data.folders.filter(function (f: { id: string; name: string; }) { return f.id !== folderId; });
        data.marks = data.marks.filter(function (m: { n: string; f: string; }) { return m.f !== folderId; });
        Object.keys(sf.links).forEach(function (id: string) {
            if (sf.links[id].f === folderId) delete sf.links[id];
        });
        if (sf.active === folderId) sf.active = '';
        sfSave();
        sf.sig = '';
        pageToast(tr('sfDeleted'));
        sfSync();
    }

    function sfCloseMenu () {
        if (sf.menu) {
            sf.menu.remove();
            sf.menu = null;
        }
    }
    function sfMenu (x: number, y: number, rows: FolderMenuItem[]) {
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
    function sfChipMenu (x: number, y: number, folderId: string) {
        sfMenu(x, y, [
            {head: sfFolderName(folderId)},
            {label: tr('sfRename'), icon: SF_FOLDER, run: function () {
                var chip = sf.bar && sf.bar.querySelector('[data-folder="' + folderId + '"]');
                if (chip) sfRenameChip(chip, folderId);
            }},
            {label: tr('sfDelete'), run: function () { sfDeleteFolder(folderId); }}
        ]);
    }
    function sfSpriteMenu (tile: Element, anchor: Element) {
        var name = sfTileName(tile);
        var sprite = sfSprites().filter(function (s: SpriteInfo) { return s.name === name; })[0];
        if (!sprite) return;
        var current = sfFolderOf(sprite);
        var data = sfLoad();
        var rows: FolderMenuItem[] = [{head: sprite.name}];
        data.folders.forEach(function (f: { id: string; name: string; }) {
            rows.push({label: f.name, icon: SF_FOLDER, checked: f.id === current, run: function () {
                sfAssign(sprite, f.id);
                pageToast(tr('sfMoved').replace('{s}', sprite.name).replace('{f}', f.name));
            }});
        });
        if (data.folders.length) rows.push({sep: true});
        if (current) {
            rows.push({label: tr('sfNone'), run: function () { sfAssign(sprite, ''); }});
        }
        rows.push({label: tr('sfNew'), icon: SF_FOLDER_PLUS, run: function () {
            var folder = sfMakeFolder('');
            sfAssign(sprite, folder.id);
            pageToast(tr('sfCreated'));
        }});
        var r = anchor.getBoundingClientRect();
        sfMenu(r.left, r.bottom + 4, rows);
    }
    function sfDecorate () {
        sfTiles().forEach(function (tile: HTMLElement) {
            if (tile.querySelector('.gwt-sf-tile')) return;
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
    function sfApplyFilter (sprites: SpriteInfo[]) {
        var byName = {};
        sprites.forEach(function (s: SpriteInfo) { byName[s.name] = s; });
        sfTiles().forEach(function (tile: HTMLElement) {
            var sprite = byName[sfTileName(tile)];
            var show = !sf.active || (sprite && sfFolderOf(sprite) === sf.active);
            var wrap = tile.parentElement;
            if (!wrap) return;
            if (show) {
                if (wrap.style.display === 'none') wrap.style.display = '';
            } else if (wrap.style.display !== 'none') {
                wrap.style.display = 'none';
            }
        });
    }
    function sfWatch (items: Node) {
        if (!window.MutationObserver) return;
        if (sf.obs && sf.obsItems === items) return;
        if (sf.obs) sf.obs.disconnect();
        var obs = new MutationObserver(function () {
            if (sf.raf) return;
            sf.raf = window.requestAnimationFrame(function () {
                sf.raf = 0;
                sfSync();
            });
        });
        obs.observe(items, {childList: true, subtree: true, characterData: true});
        sf.obs = obs;
        sf.obsItems = items;
    }
    function sfSync () {
        
        var scroll = document.querySelector('[class*="sprite-selector_scroll-wrapper"]');
        var items = scroll && scroll.querySelector('[class*="sprite-selector_items-wrapper"]');
        if (!scroll || !items) { sfTeardown(); return; }
        ensureStyle();

        var sprites = sfSprites();
        sfLink(sprites);
        if (sf.active && sf.count && sprites.length > sf.count) sf.active = '';
        sf.count = sprites.length;
        sfEnsureBar(scroll);
        var surface = paintedColor(sfPane() || document.body, 'backgroundColor') || '';
        if (surface !== sf.theme) {
            sf.theme = surface;
            sfPaint(sf.bar);
            if (sf.hint) sfPaint(sf.hint);
            if (sf.menu) sfPaint(sf.menu);
        }
        sfRenderBar(sprites);
        sfDecorate();
        sfApplyFilter(sprites);

        var hint = sfEnsureHint(scroll);
        var inFolder = 0;
        if (sf.active) {
            sprites.forEach(function (s: SpriteInfo) { if (sfFolderOf(s) === sf.active) inFolder++; });
        }
        if (sf.active && !inFolder && sfFolderName(sf.active)) {
            hint.textContent = tr('sfEmpty');
            hint.style.display = '';
        } else {
            hint.style.display = 'none';
        }
        sfWatch(items);
    }
    function sfTeardown () {
        if (!sf.mounted) return;
        sf.mounted = false;
        if (sf.obs) {
            sf.obs.disconnect();
            sf.obs = null;
            sf.obsItems = null;
        }
        if (sf.raf) {
            window.cancelAnimationFrame(sf.raf);
            sf.raf = 0;
        }
        if (sf.bar) { sf.bar.remove(); sf.bar = null; }
        if (sf.hint) { sf.hint.remove(); sf.hint = null; }
        sfCloseMenu();
        sf.editing = false;
        sf.sig = '';
        sf.shown = '';
        sf.active = '';
        sf.count = 0;
        sf.theme = '';
        Array.prototype.forEach.call(document.querySelectorAll('.gwt-sf-tile'), function (b: HTMLElement) { b.remove(); });
        var scroll = document.querySelector('[class*="sprite-selector_scroll-wrapper"]');
        if (!scroll) return;
        Array.prototype.forEach.call(scroll.querySelectorAll('[class*="sprite-selector_sprite-wrapper"]'), function (w: HTMLElement) {
            if (w.style.display === 'none') w.style.display = '';
        });
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