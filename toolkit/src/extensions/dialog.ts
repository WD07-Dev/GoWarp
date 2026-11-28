import { Project, Review, ExtensionRow, toolkitExtensionId } from './type';
import { ExtensionPolicy } from './security';
import { tr } from '../translate';

export function createExtensionDialog(vm: typeof globalThis.Scratch.vm, policy: ExtensionPolicy) {
    const { approvals, install, clear } = policy;
    function node<K extends keyof HTMLElementTagNameMap>(tag: K, text: string = '', className: string = ''): HTMLElementTagNameMap[K] {
        const e = document.createElement(tag);
        if(text) e.textContent = text;
        if(className) e.className = className;
        return e;
    }
    let dialog: HTMLDialogElement;
    let dialogClose: (() => void) | undefined;

    const entriesFor = (project: Project) => Object.entries(project.extensionURLs || {}).filter(([id, url]: [string, string]) => id !== toolkitExtensionId && typeof url === 'string' && /^(https?:\/\/|data:)/i.test(url));
    function currentProject() {
        const project: Project = JSON.parse(vm.toJSON(undefined, {allowOptimization: false}));
        const used = new Set(project.extensions || []);
        const loaded = vm.extensionManager.getExtensionURLs();
        for(const id of used) {
            if(typeof loaded[id] === 'string') (project.extensionURLs ||= {})[id] ||= loaded[id];
        }
        return project;
    }
    function open(review?: Review) {
        if(dialog && dialog.isConnected) {
            if(!review) return;
            if(dialogClose) dialogClose();
        }
        const panel = node('dialog');
        panel.id = 'gowarp-toolkit-dialog';
        panel.setAttribute('aria-labelledby','gowarp-toolkit-title');
        const header = node('div','','em-header');
        const title = node('h2',tr('toolkitName')); title.id='gowarp-toolkit-title';
        const session = node('span','','em-muted');
        header.append(title,session);
        const help = node('p', review ? tr('extensions.reviewIncoming') : tr('extensions.reviewCurrent'));
        const warning = node('p',tr('extensions.warning'),'em-warning');
        const summary = node('div','','em-summary');
        const projectLabel = node('strong',tr('extensions.title'));
        const selection = node('span','','em-muted'); summary.append(projectLabel,selection);
        const status = node('p','','em-status'); status.setAttribute('role','status');
        const list = node('div','','em-list');
        const controls = node('div','','em-toolbar');
        const footer = node('div','','em-footer');
        const rows: ExtensionRow[]=[];
        function button(text: string, handler: (event: MouseEvent) => void) {
            const b = node('button',text);
            b.type='button';
            b.addEventListener('click', handler);
            return b;
        }
        let closed=false;
        const close = (proceed: boolean=false) => {
            if(closed) return;
            closed=true;
            panel.close();
            panel.remove();
            dialog=null;
            dialogClose=null;
            if(review) {
                if(proceed===true) review.resolve();
                else review.reject(new Error(tr('extensions.cancelled')));
            }
        };
        dialogClose = () => close(false);
        const approve = button(review ? tr('extensions.approveAndOpen') : tr('extensions.approve'), () => {
            const chosen = rows.filter((r: ExtensionRow) => r.check.checked);
            if(!chosen.length && !review) {
                status.textContent = tr('extensions.selectRequired');
                return;
            }
            if(chosen.some((r: ExtensionRow) => r.mode.value==='unsandboxed') && !window.confirm(tr('extensions.confirmUnsandboxed')))return;
            try {
                install();
            } catch(e) {
                status.textContent = e.message;
                return;
            }
            if(review) {
                for(const r of rows) if(!r.check.checked && !chosen.some((c: ExtensionRow)=>c.url===r.url)) approvals.delete(r.url);
            }
            for(const r of chosen) approvals.set(r.url,r.mode.value);
            if(review) {
                close(true);
                return;
            }
            status.textContent = tr('extensions.approved', {count: chosen.length});
            refresh();
        });
        approve.dataset.primary='';
        function setSelection(checked: boolean) {
            for(const row of rows) row.check.checked = checked;
            refresh();
        }
        function setSandboxMode(mode: 'iframe' | 'unsandboxed') {
            for(const row of rows) {
                if(row.check.checked) row.mode.value = mode;
            }
            refresh();
        }
        const selectAll = button(tr('extensions.selectAll'), () => setSelection(true));
        const selectNone = button(tr('extensions.selectNone'), () => setSelection(false));
        const sandboxed = button(tr('extensions.setSandboxed'), () => setSandboxMode('iframe'));
        const unsandboxed = button(tr('extensions.setUnsandboxed'), () => setSandboxMode('unsandboxed'));
        controls.append(selectAll, selectNone, sandboxed, unsandboxed);
        if(!review) controls.append(button(tr('extensions.refresh'),()=>showCurrentProject()));
        const clearButton = button(tr('extensions.clearApprovals'),()=>{clear();rows.forEach((r: ExtensionRow)=>{r.check.checked=false;r.mode.value='default';});status.textContent=tr('extensions.policyRestored');refresh();});
        footer.append(clearButton, node('span','','em-spacer'), button(review ? tr('cancel') : tr('close'), ()=> close(false)), approve);
        function refresh() {
            const count = rows.filter((r: ExtensionRow) => r.check.checked).length;
            session.textContent = tr('extensions.sessionApproved', {count: approvals.size});
            selection.textContent = tr('extensions.selected', {count, total: rows.length});
            selectAll.disabled = !rows.length||count===rows.length;
            selectNone.disabled = !count;
            sandboxed.disabled = unsandboxed.disabled=!count;
            clearButton.disabled = !approvals.size;
            approve.disabled = !review&&!count;
            approve.textContent = review?(count?tr('extensions.approveAndOpen'):tr('extensions.openDefault')):tr('extensions.approve');
        }
        function showProject(project: Project, name: string) {
            list.replaceChildren();rows.length=0;
            projectLabel.textContent=name;
            for(const [id,url] of entriesFor(project)){
                const item = node('div','','em-item');
                const label = node('label');
                const check = node('input');
                check.type = 'checkbox';
                check.checked = approvals.has(url);
                check.addEventListener('change', refresh);
                label.append(check,node('span',id));
                const detail = node('div', url.startsWith('data:') ? tr('extensions.embedded', {
                    count: url.length.toLocaleString()
                }) : url, 'em-detail');
                const select = node('select');
                for(const [value,text] of [['default',tr('extensions.defaultPolicy')],['iframe',tr('extensions.sandboxed')],['unsandboxed',tr('extensions.unsandboxed')]]) {
                    const option = node('option',text);
                    option.value = value;
                    select.append(option);
                }
                select.value = approvals.get(url) || 'default';
                select.setAttribute('aria-label',tr('extensions.sandboxMode', {id}));
                item.append(label,select,detail);list.append(item);rows.push({check,mode:select,url});
            }
            if(!rows.length) list.append(node('div',tr('extensions.empty'),'em-empty'));
            status.textContent= review ? tr('extensions.unselectedPolicy') : tr('extensions.futureLoads');
            refresh();
        }
        function showCurrentProject() {
            try {
                showProject(currentProject(),tr('extensions.currentProject'));
            }catch(e) {
                list.replaceChildren(); rows.length=0;
                projectLabel.textContent=tr('extensions.currentProject');
                list.append(node('div',tr('extensions.readFailed'),'em-empty'));
                status.textContent=String(e.message || e); refresh();
            }
        }
        panel.append(header,help,warning,summary,controls,list,status,footer);
        document.body.append(panel);dialog=panel;
        panel.addEventListener('cancel', (e: Event) => {
            e.preventDefault();
            close(false);
        });
        panel.addEventListener('close', () => close(false));
        if(review)showProject(review.project,tr('extensions.incomingProject'));
        else showCurrentProject();
        panel.showModal();
        (rows[0]?.check||approve).focus();
    }
    return { open, close: (): void => dialogClose?.() };
}