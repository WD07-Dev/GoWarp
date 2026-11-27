import {tr} from '../translate';
import type {ScratchHost} from '../shared/scratch';
import type {PaintTools} from '../paint/type';
import { Project, Review } from './type';

export function installExtensions(Scratch: ScratchHost, paint: PaintTools): () => void {

    if (!Scratch.extensions.unsandboxed) {
        throw new Error(tr('errors.unsandboxed'));
    }
    const vm = Scratch.vm;
    const sm = vm.extensionManager.securityManager;
    const approvals = new Map();
    if (typeof vm._loadExtensions !== 'function') {
        throw new Error(tr('extensions.noLoadHook'));
    }
    const originalLoad = sm.canLoadExtensionFromProject;
    const originalMode = sm.getSandboxMode;
    const load = function (url, ...args) {
        if (approvals.has(url)) return Promise.resolve(true);
        return originalLoad.call(this, url, ...args);
    };
    const mode = function (url, ...args) {
        if (approvals.get(url) === 'unsandboxed') return 'unsandboxed';
        if (approvals.get(url) === 'iframe') return 'iframe';
        return originalMode.call(this, url, ...args);
    };
    function install() {
        if (sm.canLoadExtensionFromProject !== originalLoad && sm.canLoadExtensionFromProject !== load ||
            sm.getSandboxMode !== originalMode && sm.getSandboxMode !== mode) {
            throw new Error(tr('extensions.securityChanged'));
        }
        sm.canLoadExtensionFromProject = load;
        sm.getSandboxMode = mode;
    }
    function clear() {
        approvals.clear();
        if (sm.canLoadExtensionFromProject === load) sm.canLoadExtensionFromProject = originalLoad;
        if (sm.getSandboxMode === mode) sm.getSandboxMode = originalMode;
    }
    function node<K extends keyof HTMLElementTagNameMap>(tag: K, text = '', style?: Partial<CSSStyleDeclaration>): HTMLElementTagNameMap[K] {
        const e = document.createElement(tag);
        if (text) e.textContent = text;
        if (style) Object.assign(e.style, style);
        return e;
    }
    let dialog: HTMLDialogElement;
    let dialogClose: (() => void) | undefined;
    
    const originalExtensionLoad = vm._loadExtensions;
    const extensionId = 'gowarpToolkit';
    const entriesFor = (project: Project) => Object.entries(project.extensionURLs || {}).filter(([id, url]) => id !== extensionId && typeof url === 'string' && /^(https?:\/\/|data:)/i.test(url));
    function currentProject() {
        const project: Project = JSON.parse(vm.toJSON(undefined, {allowOptimization: false}));
        const used = new Set(project.extensions || []);
        const loaded = vm.extensionManager.getExtensionURLs();
        for (const id of used) {
            if (typeof loaded[id] === 'string') (project.extensionURLs ||= {})[id] ||= loaded[id];
        }
        return project;
    }
    const style = node('style');
    style.id = 'gowarp-toolkit-style';
    style.textContent = `
        #gowarp-toolkit-button {display:flex;align-items:center;align-self:center;gap:7px;min-height:32px;margin:0 8px;padding:0 14px;border:1px solid #ff8187;border-radius:6px;background:#c52f3b;color:#fff;font:700 13px sans-serif;cursor:pointer;white-space:nowrap;box-shadow:0 2px 5px #0003;}
        #gowarp-toolkit-button:hover {background:#e13b48;}
        #gowarp-toolkit-dialog {box-sizing:border-box;width:min(640px,92vw);max-height:88vh;overflow:auto;padding:24px;border:1px solid #666;border-radius:10px;background:#202020;color:#eee;color-scheme:dark;font:14px/1.5 sans-serif;box-shadow:0 12px 50px #0008;}
        #gowarp-toolkit-dialog::backdrop {background:#0008;}
        #gowarp-toolkit-dialog h2 {margin:0;color:#ff4b51;font-size:22px;}
        #gowarp-toolkit-dialog p {margin:10px 0;}
        #gowarp-toolkit-dialog .em-header,#gowarp-toolkit-dialog .em-footer,#gowarp-toolkit-dialog .em-toolbar,#gowarp-toolkit-dialog .em-summary {display:flex;align-items:center;gap:8px;flex-wrap:wrap;}
        #gowarp-toolkit-dialog .em-header {justify-content:space-between;}
        #gowarp-toolkit-dialog .em-muted {color:#aaa;font-size:12px;}
        #gowarp-toolkit-dialog .em-warning {border-left:3px solid #ff8187;padding:8px 12px;background:#2c2223;color:#e8c2c3;font-size:12px;}
        #gowarp-toolkit-dialog button {padding:8px 12px;border:1px solid #666;border-radius:5px;background:#333;color:#fff;font:inherit;cursor:pointer;}
        #gowarp-toolkit-dialog button:hover:not(:disabled) {background:#444;}
        #gowarp-toolkit-dialog button:disabled {opacity:.45;cursor:default;}
        #gowarp-toolkit-dialog button[data-primary] {background:#a32a31;border-color:#c85e63;}
        #gowarp-toolkit-dialog button[data-primary]:hover:not(:disabled) {background:#c52f3b;}
        #gowarp-toolkit-dialog button:focus-visible,#gowarp-toolkit-dialog input:focus-visible,#gowarp-toolkit-dialog select:focus-visible,#gowarp-toolkit-button:focus-visible {outline:2px solid #ff8187;outline-offset:2px;}
        #gowarp-toolkit-dialog input[type=checkbox] {accent-color:#c52f3b;width:16px;height:16px;margin:0;flex:none;}
        #gowarp-toolkit-dialog select {box-sizing:border-box;max-width:100%;min-width:0;padding:7px 9px;border:1px solid #666;border-radius:5px;background:#111;color:#eee;font:13px sans-serif;}
        #gowarp-toolkit-dialog .em-summary {margin:18px 0 8px;justify-content:space-between;}
        #gowarp-toolkit-dialog .em-toolbar {padding:10px 0;}
        #gowarp-toolkit-dialog .em-toolbar button {font-size:12px;padding:6px 9px;}
        #gowarp-toolkit-dialog .em-list {display:grid;gap:6px;max-height:34vh;overflow:auto;overscroll-behavior:contain;}
        #gowarp-toolkit-dialog .em-item {border:1px solid #484848;border-radius:6px;padding:10px 12px;background:#292929;display:grid;grid-template-columns:minmax(0,1fr) auto;gap:6px 12px;align-items:center;}
        #gowarp-toolkit-dialog .em-item label {display:flex;align-items:center;gap:8px;min-width:0;font-weight:600;overflow-wrap:anywhere;cursor:pointer;}
        #gowarp-toolkit-dialog .em-detail {grid-column:1/-1;color:#aaa;font:12px/1.5 monospace;overflow-wrap:anywhere;}
        #gowarp-toolkit-dialog .em-empty {padding:20px 10px;border:1px dashed #555;border-radius:6px;text-align:center;color:#aaa;}
        #gowarp-toolkit-dialog .em-status {font-size:12px;color:#ccc;overflow-wrap:anywhere;min-height:18px;}
        #gowarp-toolkit-dialog .em-footer {border-top:1px solid #484848;padding-top:14px;margin-top:14px;}
        #gowarp-toolkit-dialog .em-footer .em-spacer {flex:1;}
        @media(max-width:500px) {#gowarp-toolkit-dialog {padding:16px;} #gowarp-toolkit-dialog .em-item {grid-template-columns:1fr;} #gowarp-toolkit-dialog .em-detail {grid-column:1;}}
    `;
    document.head.appendChild(style);
    function open(review?: Review) {

        if (dialog && dialog.isConnected) {
            if (!review) return;
            if (dialogClose) dialogClose();
        }
        const panel = node('dialog');
        panel.id = 'gowarp-toolkit-dialog';
        panel.setAttribute('aria-labelledby','gowarp-toolkit-title');
        const make = <K extends keyof HTMLElementTagNameMap>(tag: K, text = '', cls = '') => { const e = node(tag,text); if (cls) e.className=cls; return e; };
        const header = make('div','','em-header');
        const title = make('h2',tr('toolkitName')); title.id='gowarp-toolkit-title';
        const session = make('span','','em-muted');
        header.append(title,session);
        const help = make('p', review ? tr('extensions.reviewIncoming') : tr('extensions.reviewCurrent'));
        const warning = make('p',tr('extensions.warning'),'em-warning');
        const summary = make('div','','em-summary');
        const projectLabel=make('strong',tr('extensions.title'));
        const selection=make('span','','em-muted'); summary.append(projectLabel,selection);
        const status=make('p','','em-status'); status.setAttribute('role','status');
        const list=make('div','','em-list');
        const controls=make('div','','em-toolbar');
        const footer=make('div','','em-footer');
        const rows: {check: HTMLInputElement; mode: HTMLSelectElement; url: string}[]=[];
        function button(text,handler) { const b=node('button',text); b.type='button'; b.addEventListener('click',handler); return b; }
        let closed=false;
        const close=(proceed=false)=>{
            if(closed)return; closed=true;
            panel.close(); panel.remove(); dialog=null; dialogClose=null;
            if(review) { if(proceed===true)review.resolve(); else review.reject(new Error(tr('extensions.cancelled'))); }
        };
        dialogClose=()=>close(false);
        const approve=button(review?tr('extensions.approveAndOpen'):tr('extensions.approve'),()=>{
            const chosen=rows.filter(r=>r.check.checked);
            if(!chosen.length&&!review){status.textContent=tr('extensions.selectRequired');return;}
            if(chosen.some(r=>r.mode.value==='unsandboxed')&&!window.confirm(tr('extensions.confirmUnsandboxed')))return;
            try{install();}catch(e){status.textContent=e.message;return;}
            if(review){for(const r of rows)if(!r.check.checked&&!chosen.some(c=>c.url===r.url))approvals.delete(r.url);}
            for(const r of chosen)approvals.set(r.url,r.mode.value);
            if(review){close(true);return;}
            status.textContent=tr('extensions.approved', {count: chosen.length});
            refresh();
        }); approve.dataset.primary='';
        const selectAll=button(tr('extensions.selectAll'),()=>{rows.forEach(r=>r.check.checked=true);refresh();});
        const selectNone=button(tr('extensions.selectNone'),()=>{rows.forEach(r=>r.check.checked=false);refresh();});
        const sandboxed=button(tr('extensions.setSandboxed'),()=>{rows.filter(r=>r.check.checked).forEach(r=>r.mode.value='iframe');refresh();});
        const unsandboxed=button(tr('extensions.setUnsandboxed'),()=>{rows.filter(r=>r.check.checked).forEach(r=>r.mode.value='unsandboxed');refresh();});
        controls.append(selectAll,selectNone,sandboxed,unsandboxed);
        if(!review) controls.append(button(tr('extensions.refresh'),()=>showCurrentProject()));
        const clearButton=button(tr('extensions.clearApprovals'),()=>{clear();rows.forEach(r=>{r.check.checked=false;r.mode.value='default';});status.textContent=tr('extensions.policyRestored');refresh();});
        footer.append(clearButton,make('span','','em-spacer'),button(review?tr('cancel'):tr('close'),()=>close(false)),approve);
        function refresh(){
            const count=rows.filter(r=>r.check.checked).length;
            session.textContent=tr('extensions.sessionApproved', {count: approvals.size});
            selection.textContent=tr('extensions.selected', {count, total: rows.length});
            selectAll.disabled=!rows.length||count===rows.length;
            selectNone.disabled=!count;
            sandboxed.disabled=unsandboxed.disabled=!count;
            clearButton.disabled=!approvals.size;
            approve.disabled=!review&&!count;
            approve.textContent=review?(count?tr('extensions.approveAndOpen'):tr('extensions.openDefault')):tr('extensions.approve');
        }
        function showProject(project: Project,name: string){
            list.replaceChildren();rows.length=0;
            projectLabel.textContent=name;
            for(const [id,url] of entriesFor(project)){
                const item=make('div','','em-item');
                const label=make('label');
                const check=node('input');check.type='checkbox';check.checked=approvals.has(url);check.addEventListener('change',refresh);
                label.append(check,node('span',id));
                const detail=make('div',url.startsWith('data:')?tr('extensions.embedded', {count: url.length.toLocaleString()}):url,'em-detail');
                const select=node('select');
                for(const [value,text] of [['default',tr('extensions.defaultPolicy')],['iframe',tr('extensions.sandboxed')],['unsandboxed',tr('extensions.unsandboxed')]]){const option=node('option',text);option.value=value;select.append(option);}
                select.value=approvals.get(url)||'default';select.setAttribute('aria-label',tr('extensions.sandboxMode', {id}));
                item.append(label,select,detail);list.append(item);rows.push({check,mode:select,url});
            }
            if(!rows.length)list.append(make('div',tr('extensions.empty'),'em-empty'));
            status.textContent=review?tr('extensions.unselectedPolicy'):tr('extensions.futureLoads');
            refresh();
        }
        function showCurrentProject(){
            try { showProject(currentProject(),tr('extensions.currentProject')); }
            catch(e) {
                list.replaceChildren(); rows.length=0;
                projectLabel.textContent=tr('extensions.currentProject');
                list.append(make('div',tr('extensions.readFailed'),'em-empty'));
                status.textContent=String(e.message || e); refresh();
            }
        }
        if (!review) {
            const tools = make('div', '', 'em-toolbar');
            tools.append(
                button(tr('extensions.pixelEditor'), () => { close(); paint.openPixelEditor(); })
            );
            panel.append(header, tools, make('p', tr('extensions.toolsHint')));
        } else panel.append(header);
        panel.append(help,warning,summary,controls,list,status,footer);
        document.body.append(panel);dialog=panel;
        panel.addEventListener('cancel',e=>{e.preventDefault();close(false);});
        panel.addEventListener('close',()=>close(false));
        if(review)showProject(review.project,tr('extensions.incomingProject'));
        else showCurrentProject();
        panel.showModal();
        (rows[0]?.check||approve).focus();
    }
    const extensionLoad = async function (extensionIDs, extensionURLs = new Map(), ...args) {
        const urls = Object.create(null);
        const manager = this.extensionManager;
        if (extensionIDs && typeof extensionIDs[Symbol.iterator] === 'function' &&
            typeof extensionURLs?.get === 'function') {
            for (const id of extensionIDs) {
                if (manager.isExtensionLoaded(id) || manager.isBuiltinExtension(id)) continue;
                const url = extensionURLs.get(id);
                if (typeof url !== 'string' || !/^(https?:\/\/|data:)/i.test(url)) continue;
                if (url.startsWith('https://extensions.turbowarp.org/') || url.startsWith('http://localhost:8000/')) continue;
                urls[id] = url;
            }
        }
        if (Object.values(urls).some(url => !approvals.has(url))) {
            await new Promise<void>((resolve, reject) => open({project: {extensionURLs: urls}, resolve, reject}));
        }
        return originalExtensionLoad.call(this, extensionIDs, extensionURLs, ...args);
    };
    vm._loadExtensions = extensionLoad;
    class Toolkit {
        getInfo() {
            return {id:extensionId,name:tr('toolkitName'),color1:'#b52e35',blocks:[
                {blockType:Scratch.BlockType.BUTTON,text:tr('toolkitName'),func:'open'},
            ]};
        }
        open(){open();}
    }
    Scratch.extensions.register(new Toolkit());
    function attachButton(){
        if(document.getElementById('gowarp-toolkit-button'))return;
        const bar=document.querySelector('[class*="menu-bar_main-menu"]')||document.querySelector('[class*="menu-bar_menu-bar_"]');
        if(!bar)return;
        const launcher=node('button',tr('toolkitName'));
        launcher.id='gowarp-toolkit-button';launcher.type='button';
        launcher.title=tr('extensions.launcherHint');
        launcher.addEventListener('click',()=>open());bar.appendChild(launcher);
    }
    const observer=new MutationObserver(attachButton);
    observer.observe(document.body,{childList:true,subtree:true});attachButton();
    return () => {
        observer.disconnect();
        dialogClose?.();
        clear();
        if (vm._loadExtensions === extensionLoad) vm._loadExtensions = originalExtensionLoad;
        document.getElementById('gowarp-toolkit-button')?.remove();
        style.remove();
    };
}
