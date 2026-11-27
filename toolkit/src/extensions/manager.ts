import type {ScratchHost} from '../shared/scratch';
import type {PaintTools} from '../paint/type';
interface Project {
    extensions?: string[];
    extensionURLs?: Record<string, string>;
}
interface Review {
    project: {extensionURLs: Record<string, string>};
    resolve: () => void;
    reject: (error: Error) => void;
}
export function installExtensions(Scratch: ScratchHost, paint: PaintTools): () => void {
    const korean = (Scratch.translate.language || 'en').toLowerCase().startsWith('ko');
    const tr = (en, ko) => korean ? ko : en;

    if (!Scratch.extensions.unsandboxed) {
        throw new Error(tr('GoWarp Toolkit must run without sandbox.', 'GoWarp Toolkit는 샌드박스 없이 실행해야 합니다.'));
    }
    const vm = Scratch.vm;
    const sm = vm.extensionManager.securityManager;
    const approvals = new Map();
    if (typeof vm._loadExtensions !== 'function') {
        throw new Error(tr('This TurboWarp version does not expose the extension-loading hook.', '이 TurboWarp 버전은 확장 로딩 연결 기능을 지원하지 않습니다.'));
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
            throw new Error(tr('Another extension changed the security manager. Reload the editor first.', '다른 확장이 보안 설정을 변경했습니다. 먼저 편집기를 새로고침해 주세요.'));
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
        const title = make('h2',tr('GoWarp Toolkit', 'GoWarp Toolkit')); title.id='gowarp-toolkit-title';
        const session = make('span','','em-muted');
        header.append(title,session);
        const help = make('p', review ? tr('Review the custom extensions in this project before opening it.', '프로젝트를 열기 전에 포함된 사용자 정의 확장을 확인하세요.') : tr('Review the custom extensions in the current project. Approvals apply to future loads in this editor session.', '현재 프로젝트의 사용자 정의 확장을 확인하세요. 승인은 현재 편집기 세션에서 이후에 불러오는 확장에 적용됩니다.'));
        const warning = make('p',tr('Only approve code you trust. Unsandboxed extensions have full editor access. Remote URLs can change. Default mode keeps TurboWarp’s normal sandbox policy. Clearing approvals does not unload running code.', '신뢰하는 코드만 승인하세요. 샌드박스 없이 실행하는 확장은 편집기 전체에 접근할 수 있으며, 원격 URL의 코드는 바뀔 수 있습니다. 기본 모드는 TurboWarp의 기본 샌드박스 정책을 따릅니다. 승인을 초기화해도 이미 실행 중인 코드는 종료되지 않습니다.'),'em-warning');
        const summary = make('div','','em-summary');
        const projectLabel=make('strong',tr('Extensions', '확장'));
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
            if(review) { if(proceed===true)review.resolve(); else review.reject(new Error(tr('Project loading cancelled in GoWarp Toolkit.', 'GoWarp Toolkit에서 프로젝트 불러오기를 취소했습니다.'))); }
        };
        dialogClose=()=>close(false);
        const approve=button(review?tr('Approve selected & open', '선택 항목 승인 후 열기'):tr('Approve selected', '선택 항목 승인'),()=>{
            const chosen=rows.filter(r=>r.check.checked);
            if(!chosen.length&&!review){status.textContent=tr('Select at least one extension.', '확장을 하나 이상 선택하세요.');return;}
            if(chosen.some(r=>r.mode.value==='unsandboxed')&&!window.confirm(tr('Allow the selected unsandboxed extensions to execute with full editor access? Only continue if you trust their code.', '선택한 확장을 샌드박스 없이 실행하여 편집기 전체에 접근하도록 허용할까요? 코드를 신뢰하는 경우에만 계속하세요.')))return;
            try{install();}catch(e){status.textContent=e.message;return;}
            if(review){for(const r of rows)if(!r.check.checked&&!chosen.some(c=>c.url===r.url))approvals.delete(r.url);}
            for(const r of chosen)approvals.set(r.url,r.mode.value);
            if(review){close(true);return;}
            status.textContent=tr(`Approved ${chosen.length} entries for future loads. Already running extensions keep their current sandbox mode.`, `${chosen.length}개 항목을 승인했습니다. 이후 불러올 때 적용되며, 실행 중인 확장의 샌드박스 모드는 유지됩니다.`);
            refresh();
        }); approve.dataset.primary='';
        const selectAll=button(tr('Select all', '전체 선택'),()=>{rows.forEach(r=>r.check.checked=true);refresh();});
        const selectNone=button(tr('Select none', '전체 선택 해제'),()=>{rows.forEach(r=>r.check.checked=false);refresh();});
        const sandboxed=button(tr('Set sandboxed', '샌드박스 사용'),()=>{rows.filter(r=>r.check.checked).forEach(r=>r.mode.value='iframe');refresh();});
        const unsandboxed=button(tr('Set unsandboxed', '샌드박스 사용 안 함'),()=>{rows.filter(r=>r.check.checked).forEach(r=>r.mode.value='unsandboxed');refresh();});
        controls.append(selectAll,selectNone,sandboxed,unsandboxed);
        if(!review) controls.append(button(tr('Refresh', '새로고침'),()=>showCurrentProject()));
        const clearButton=button(tr('Clear approvals', '승인 초기화'),()=>{clear();rows.forEach(r=>{r.check.checked=false;r.mode.value='default';});status.textContent=tr('Default security policy restored for future loads.', '이후 불러오는 확장에 기본 보안 정책을 적용합니다.');refresh();});
        footer.append(clearButton,make('span','','em-spacer'),button(review?tr('Cancel', '취소'):tr('Close', '닫기'),()=>close(false)),approve);
        function refresh(){
            const count=rows.filter(r=>r.check.checked).length;
            session.textContent=tr(`${approvals.size} approved this session`, `이 세션에서 ${approvals.size}개 승인됨`);
            selection.textContent=tr(`${count} / ${rows.length} selected`, `${rows.length}개 중 ${count}개 선택됨`);
            selectAll.disabled=!rows.length||count===rows.length;
            selectNone.disabled=!count;
            sandboxed.disabled=unsandboxed.disabled=!count;
            clearButton.disabled=!approvals.size;
            approve.disabled=!review&&!count;
            approve.textContent=review?(count?tr('Approve selected & open', '선택 항목 승인 후 열기'):tr('Open with default policy', '기본 정책으로 열기')):tr('Approve selected', '선택 항목 승인');
        }
        function showProject(project: Project,name: string){
            list.replaceChildren();rows.length=0;
            projectLabel.textContent=name;
            for(const [id,url] of entriesFor(project)){
                const item=make('div','','em-item');
                const label=make('label');
                const check=node('input');check.type='checkbox';check.checked=approvals.has(url);check.addEventListener('change',refresh);
                label.append(check,node('span',id));
                const detail=make('div',url.startsWith('data:')?tr(`Embedded JavaScript (${url.length.toLocaleString()} URL characters)`, `내장 JavaScript (URL ${url.length.toLocaleString()}자)`):url,'em-detail');
                const select=node('select');
                for(const [value,text] of [['default',tr('Default sandbox policy', '기본 샌드박스 정책')],['iframe',tr('Sandboxed', '샌드박스 사용')],['unsandboxed',tr('Unsandboxed', '샌드박스 사용 안 함')]]){const option=node('option',text);option.value=value;select.append(option);}
                select.value=approvals.get(url)||'default';select.setAttribute('aria-label',tr(`${id} sandbox mode`, `${id} 샌드박스 모드`));
                item.append(label,select,detail);list.append(item);rows.push({check,mode:select,url});
            }
            if(!rows.length)list.append(make('div',tr('No custom extensions found.', '사용자 정의 확장이 없습니다.'),'em-empty'));
            status.textContent=review?tr('Unselected extensions follow TurboWarp’s normal security policy.', '선택하지 않은 확장은 TurboWarp의 기본 보안 정책을 따릅니다.'):tr('Approvals apply to future loads; currently loaded extensions are not restarted.', '승인은 이후 불러오는 확장에 적용됩니다. 이미 불러온 확장은 다시 시작하지 않습니다.');
            refresh();
        }
        function showCurrentProject(){
            try { showProject(currentProject(),tr('Current project', '현재 프로젝트')); }
            catch(e) {
                list.replaceChildren(); rows.length=0;
                projectLabel.textContent=tr('Current project', '현재 프로젝트');
                list.append(make('div',tr('Could not read the current project.', '현재 프로젝트를 읽을 수 없습니다.'),'em-empty'));
                status.textContent=String(e.message || e); refresh();
            }
        }
        if (!review) {
            const tools = make('div', '', 'em-toolbar');
            tools.append(
                button(tr('Pixel editor', '픽셀 편집기'), () => { close(); paint.openPixelEditor(); })
            );
            panel.append(header, tools, make('p', tr('Drawing, layers and alignment tools are in the Costumes tab. Sprite folders appear above the sprite list.', '그리기·레이어·중심 정렬 도구는 모양 탭에, 폴더는 스프라이트 목록 위에 표시됩니다.')));
        } else panel.append(header);
        panel.append(help,warning,summary,controls,list,status,footer);
        document.body.append(panel);dialog=panel;
        panel.addEventListener('cancel',e=>{e.preventDefault();close(false);});
        panel.addEventListener('close',()=>close(false));
        if(review)showProject(review.project,tr('Incoming project', '불러올 프로젝트'));
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
            return {id:extensionId,name:tr('GoWarp Toolkit', 'GoWarp Toolkit'),color1:'#b52e35',blocks:[
                {blockType:Scratch.BlockType.BUTTON,text:tr('GoWarp Toolkit', 'GoWarp Toolkit'),func:'open'},
            ]};
        }
        open(){open();}
    }
    Scratch.extensions.register(new Toolkit());
    function attachButton(){
        if(document.getElementById('gowarp-toolkit-button'))return;
        const bar=document.querySelector('[class*="menu-bar_main-menu"]')||document.querySelector('[class*="menu-bar_menu-bar_"]');
        if(!bar)return;
        const launcher=node('button',tr('GoWarp Toolkit', 'GoWarp Toolkit'));
        launcher.id='gowarp-toolkit-button';launcher.type='button';
        launcher.title=tr('Review and approve project extensions', '프로젝트 확장 확인 및 승인');
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
