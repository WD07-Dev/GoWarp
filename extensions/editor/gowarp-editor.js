// Name: GoWarp Editor
// ID: gowarpEditor
// Description: Run the current TurboWarp project in the local GoWarp runtime.
// By: wheedo07
// License: Apache-2.0

/* @gowarp-ignore */
(function (Scratch) {
    "use strict";

    const korean = (Scratch.translate.language || 'en').toLowerCase().startsWith('ko');
    const tr = (en, ko) => korean ? ko : en;

    if(!Scratch.extensions.unsandboxed) {
        throw new Error(tr("GoWarp Editor must run without sandboxing.", "GoWarp 편집기 연결은 샌드박스 없이 실행해야 합니다."));
    }
    
    const previous = window.__gowarpEditor;
    if(previous) previous.dispose();
    
    // Older browsers reject the loopback enum before sending any request.
    let targetAddressSpace;
    try {
        new Request("http://127.0.0.1", {targetAddressSpace: "loopback"});
        targetAddressSpace = "loopback";
    } catch {}

    const vm = Scratch.vm;
    const messages = {
        ready: tr("Ready", "실행 대기 중"),
        preparing: tr("Preparing project...", "프로젝트 준비 중..."),
        receiving: tr("Sending project...", "프로젝트 전송 중..."),
        building: tr("Compiling preview...", "미리보기 컴파일 중..."),
        running: tr("Running in GoWarp", "GoWarp에서 실행 중"),
        stopping: tr("Stopping...", "중지 중..."),
        error: tr("Preview failed", "미리보기 실패")
    };
    let connection = null;
    let busy = false;
    let phase = "ready";
    let disposed = false;
    let polling = null;
    let upload = null;
    let socket = null;
    let revision = 0;
    let previewRevision = -1;
    let previewSnapshot = null;
    let previewTargets = new Map();
    let initialCommand = null;
    let nextCommand = 0;
    let suspendedLoop = false;
    let overlay = null;
    let loading = null;
    let archiveCache = null;
    let lastUpload = null;
    let serverSnapshot = "";
    let settingsRevision = "";
    let previewSettingsRevision = "";
    let multiWindow = false;
    let activeDisplay = "native";
    let pendingFrames = 0;
    const maxFrameEdge = 1920;
    let mouseRequest = 0;
    let pendingMouse = null;
    const reports = new Map();
    const requests = new Map();
    const runtime = vm.runtime;
    const originalToggle = runtime.toggleScript;
    const originalFlag = runtime.greenFlag;
    const originalStop = runtime.stopAll;
    const originalToJSON = vm.toJSON;
    function toJSON(targetId, options) {
        const json = originalToJSON.call(this, targetId, options);
        if (targetId) return json;
        const url = runtime.extensionManager.getExtensionURLs().gowarpEditor;
        if (typeof url !== "string") return json;
        const project = JSON.parse(json);
        project.extensions ||= [];
        if (!project.extensions.includes("gowarpEditor")) project.extensions.push("gowarpEditor");
        project.extensionURLs ||= {};
        project.extensionURLs.gowarpEditor = url;
        // TurboWarp reloads extensions referenced by monitors, even hidden ones.
        project.monitors ||= [];
        if (!project.monitors.some(m => m.opcode === "gowarpEditor_installed")) {
            project.monitors.push({
                id: "gowarpEditor_installed", opcode: "gowarpEditor_installed",
                mode: "default", params: {}, spriteName: null, value: "",
                width: 0, height: 0, x: 0, y: 0, visible: false
            });
        }
        return JSON.stringify(project);
    }
    vm.toJSON = toJSON;
    const changed = () => { revision++; };
    const projectEvents = ["PROJECT_CHANGED", "PROJECT_LOADED", "TURBO_MODE_ON", "TURBO_MODE_OFF"];
    const settingsEvents = ["FRAMERATE_CHANGED", "INTERPOLATION_CHANGED", "STAGE_SIZE_CHANGED", "RUNTIME_OPTIONS_CHANGED", "COMPILER_OPTIONS_CHANGED"];
    for(const event of projectEvents) vm.on(event, changed);
    for(const event of settingsEvents) runtime.on(event, changed);

    function element(tag, text, parent) {
        const node = document.createElement(tag);
        if (text) node.textContent = text;
        if (parent) parent.appendChild(node);
        return node;
    }
    
    const style = element("style");
    style.textContent = `
    .gowarp-editor-control {display:flex;align-items:center;align-self:center;gap:7px;min-height:32px;margin:0 8px;padding:0 14px;border:1px solid #ff8187;border-radius:6px;background:#c52f3b;color:#fff;font:700 13px sans-serif;cursor:pointer;white-space:nowrap;box-shadow:0 2px 5px #0003;}
    #gowarp-editor-button::before {content:"▶";font-size:11px;}
    #gowarp-editor-stop {margin-left:0;background:#444;border-color:#777;}
    #gowarp-editor-stop::before {content:"■";font-size:11px;}
    .gowarp-editor-control:disabled {opacity:.45;cursor:default;}
    .gowarp-editor-control:hover {background:#e13b48;}
    .gowarp-editor-control:focus-visible {outline:2px solid #fff;outline-offset:2px;}
    #gowarp-editor-dialog {box-sizing:border-box;width:min(600px,90vw);padding:24px;border:1px solid #666;border-radius:10px;background:#202020;color:#eee;font:14px/1.5 sans-serif;box-shadow:0 12px 50px #0008;}
    #gowarp-editor-dialog::backdrop {background:#0008;}
    #gowarp-editor-dialog h2 {margin:0 0 12px;color:#ff4b51;}
    #gowarp-editor-dialog input, #gowarp-editor-dialog select {box-sizing:border-box;width:100%;padding:10px;border:1px solid #666;border-radius:5px;background:#111;color:#eee;font:13px monospace;}
    #gowarp-preview {position:absolute;inset:0;width:100%;height:100%;z-index:100;outline:none;touch-action:none;background:#000;image-rendering:pixelated;}
    #gowarp-preview:focus-visible {outline:2px solid #ff4b51;outline-offset:-2px;}
    #gowarp-preview-loading {position:absolute;inset:0;z-index:101;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:16px;background:#202020ed;color:#eee;font:14px sans-serif;}
    #gowarp-preview-loading strong {color:#ff4b51;font-size:22px;}
    #gowarp-preview-loading progress {width:65%;height:8px;accent-color:#ff4b51;}
    #gowarp-preview-loading button {border:1px solid #666;border-radius:5px;padding:8px 16px;background:#333;color:#fff;cursor:pointer;}
    #gowarp-editor-dialog button {padding:9px 14px;margin:12px 8px 0 0;border:1px solid #666;border-radius:5px;background:#333;color:#fff;cursor:pointer;}
    #gowarp-editor-dialog button:disabled {opacity:.45;cursor:default;}
    #gowarp-editor-dialog [data-run] {background:#a32a31;}
    #gowarp-editor-dialog pre {max-height:180px;overflow:auto;white-space:pre-wrap;overflow-wrap:anywhere;}
    `;
    document.head.appendChild(style);
    
    const dialog = element("dialog");
    dialog.id = "gowarp-editor-dialog";
    element("h2", "GoWarp", dialog);
    element("p", tr(
        "In GoWarp, click Connect TurboWarp -> Enable / copy address. Paste the address below.",
        "GoWarp에서 TurboWarp 연결 -> 연결 켜기 / 주소 복사를 누르고 아래에 붙여넣으세요."
    ), dialog);
    const address = element("input", "", dialog);
    address.type = "text";
    address.placeholder = "http://127.0.0.1:.../#...";
    address.autocomplete = "off";
    address.spellcheck = false;
    address.setAttribute("aria-label", tr("GoWarp connection address", "GoWarp 연결 주소"));
    const displayLabel = element("label", tr("Display mode", "표시 방식"), dialog);
    const displayMode = element("select", "", displayLabel);
    element("option", tr("Native window (default)", "별도 창 (기본)"), displayMode).value = "native";
    element("option", tr("Embedded preview (experimental)", "편집기 안에서 미리보기 (실험적)"), displayMode).value = "embedded";
    element("p", tr(
        "Embedded preview displays up to 60 frames/s, with a maximum edge of 1920 px. Audio plays locally. The native window must stay open.",
        "편집기 안 표시는 최대 60프레임/초, 긴 변 1920px로 전송합니다. 소리는 PC에서 재생하며 네이티브 창은 계속 열려 있어야 합니다."
    ), dialog);
    const controls = element("div", "", dialog);
    const connectButton = element("button", tr("Connect", "연결"), controls);
    const runButton = element("button", tr("Run in GoWarp", "GoWarp에서 실행"), controls);
    runButton.dataset.run = "";
    const stopButton = element("button", tr("Stop", "중지"), controls);
    const disconnectButton = element("button", tr("Disconnect", "연결 해제"), controls);
    const status = element("pre", tr("Not connected", "연결되지 않음"), dialog);
    element("p", tr(
        "Requires Go. The first build can take longer; later builds reuse GoWarp's compiler cache. The game opens in a separate window.",
        "Go가 필요합니다. 첫 빌드는 오래 걸릴 수 있으며 이후에는 컴파일 캐시를 재사용합니다. 게임은 별도 창에서 열립니다."
    ), dialog);
    const closeButton = element("button", tr("Close", "닫기"), dialog);
    closeButton.onclick = () => dialog.close();
    document.body.appendChild(dialog);
    
    function refresh() {
        const active = ["preparing", "receiving", "building", "running", "stopping"].includes(phase);
        runButton.disabled = busy || (active && phase !== "running");
        stopButton.disabled = !connection || phase === "stopping" || (!busy && !active);
        const menuStop = document.getElementById("gowarp-editor-stop");
        if(menuStop) menuStop.disabled = stopButton.disabled;
        connectButton.disabled = Boolean(connection) || busy || active;
        address.disabled = Boolean(connection) || busy || active;
        displayMode.disabled = busy || active || multiWindow;
        displayMode.children[1].disabled = multiWindow;
        disconnectButton.disabled = !connection || busy;
        const button = document.getElementById("gowarp-editor-button");
        if(button) button.title = active ? `${tr("GoWarp Run", "GoWarp 실행")} · ${messages[phase]}` : tr("Run this project in GoWarp", "현재 프로젝트를 GoWarp에서 실행");
        const parent = runtime.renderer?.canvas?.parentElement;
        if(parent && ["preparing", "receiving", "building", "stopping"].includes(phase)) {
            if (!loading) {
                loading = element("div", "", parent);
                loading.id = "gowarp-preview-loading";
                loading.setAttribute("role", "status");
                element("strong", "GoWarp", loading);
                element("span", "", loading);
                element("progress", "", loading).setAttribute("aria-label", tr("Loading preview", "미리보기 로딩 중"));
                element("button", tr("Cancel", "취소"), loading).onclick = stop;
            }
            loading.children[1].textContent = messages[phase];
        }else {
            loading?.remove();
            loading = null;
        }
    }

    function showError(error) {
        phase = "error";
        status.textContent = error.message || String(error);
        refresh();
        restoreEditor();
    }
    
    function parseAddress(value) {
        const url = new URL(value.trim());
        if(url.protocol !== "http:" || url.hostname !== "127.0.0.1" || !url.port || url.username || url.password || url.pathname !== "/" || url.search || !/^#[0-9a-f]{64}$/.test(url.hash)) {
            throw new Error(tr("Copy the full connection address from GoWarp.", "GoWarp에서 연결 주소 전체를 복사하세요."));
        }
        return {base: url.origin, token: url.hash.slice(1)};
    }
    
    async function request(target, path, method = "GET", body, options, controller = new AbortController()) {
        const timeout = setTimeout(() => controller.abort(), body ? 120000 : 8000);
        try {
            const headers = {Authorization: `Bearer ${target.token}`};
            if (body) {
                headers["Content-Type"] = path === "/connect" ? "application/json" : "application/x.scratch.sb3";
                headers["X-GoWarp-Options"] = encodeURIComponent(JSON.stringify(options));
            }
            const response = await fetch(target.base + path, {
                method, headers, body, signal: controller.signal,
                credentials: "omit", cache: "no-store", redirect: "error",
                targetAddressSpace
            });
            if(!response.ok) throw new Error(await response.text());
            return path === "/status" || path === "/run" || path === "/connect" ? await response.json() : null;
        } catch (error) {
            if(error instanceof TypeError) {
                throw new Error(tr(
                    "Cannot reach GoWarp. Keep it open, copy a new address, and allow local network access for TurboWarp if your browser asks.",
                    "GoWarp에 연결할 수 없습니다. GoWarp를 켜고 주소를 다시 복사하세요. 브라우저가 요청하면 TurboWarp의 로컬 네트워크 접근을 허용하세요."
                ));
            }
            throw error;
        } finally {
            clearTimeout(timeout);
        }
    }

    function applyStatus(value) {
        if (!value.connected) {
            resetConnection();
            return;
        }
        serverSnapshot = value.snapshot || "";
        settingsRevision = value.settingsRevision || "";
        multiWindow = Boolean(value.multiWindow);
        const wasRunning = phase === "running";
        phase = value.phase;
        if (value.display) activeDisplay = value.display;
        if (multiWindow) displayMode.value = "native";
        else if (value.display) displayMode.value = value.display;
        status.textContent = value.error || messages[phase] || phase;
        refresh();
        if(phase === "running") {
            if(!wasRunning) runtime.emit("PROJECT_RUN_START");
            suspendEditor();
            openStream();
        }else if(phase === "ready" || phase === "error") {
            restoreEditor();
        }
    }

    async function poll() {
        clearTimeout(polling);
        if(!connection || disposed || upload) return;
        const target = connection;
        try {
            const value = await request(target, "/status");
            if(connection !== target || disposed) return;
            applyStatus(value);
            if (connection) polling = setTimeout(poll, phase === "ready" ? 1000 : 500);
        } catch (error) {
            if (connection === target && !disposed) {
                resetConnection();
                showError(error);
            }
        }
    }

    function projectOptions() {
        return {
            name: document.title.replace(/\s*[-–|]\s*TurboWarp.*$/i, "").slice(0, 80),
            fps: runtime.frameLoop.framerate,
            width: runtime.stageWidth, height: runtime.stageHeight,
            turbo: runtime.turboMode, interpolation: runtime.interpolationEnabled,
            highQualityPen: runtime.renderer?.useHighQualityRender,
            removeFencing: runtime.runtimeOptions?.fencing === undefined ? undefined : !runtime.runtimeOptions.fencing,
            removeMiscLimits: runtime.runtimeOptions?.miscLimits === undefined ? undefined : !runtime.runtimeOptions.miscLimits,
            display: multiWindow ? "native" : displayMode.value
        };
    }

    async function connect() {
        const target = parseAddress(address.value);
        const project = JSON.parse(vm.toJSON(undefined, {allowOptimization: false}));
        const targets = project.targets.map(({isStage, name, variables, comments}) => ({isStage, name, variables, comments}));
        const body = new Blob([JSON.stringify({targets})], {type: "application/json"});
        const value = await request(target, "/connect", "POST", body, projectOptions());
        if (disposed) return;
        connection = target;
        applyStatus(value);
        if (connection) polling = setTimeout(poll, 1000);
    }

    function snapshot() {
        const project = JSON.parse(vm.toJSON(undefined, {allowOptimization: false}));
        const targets = runtime.targets.filter(target => target.isOriginal);
        const graphs = project.targets.map((target, index) => {
            const blocks = target.blocks || {};
            for (const block of Object.values(blocks)) {
                if (Array.isArray(block)) {
                    // Serialized standalone variable/list reporters carry workspace coordinates.
                    if (block[0] === 12 || block[0] === 13) block.length = 3;
                }else {
                    delete block.x;
                    delete block.y;
                    delete block.comment;
                }
            }
            const graph = {target: index, blocks, scriptOrder: [...targets[index].blocks.getScripts()]};
            delete target.blocks;
            delete target.comments;
            return graph;
        });
        delete project.meta;
        return {
            structure: JSON.stringify({
                project, targets: targets.map(target => target.id),
                fps: runtime.frameLoop.framerate, turbo: runtime.turboMode,
                interpolation: runtime.interpolationEnabled, width: runtime.stageWidth,
                height: runtime.stageHeight, display: displayMode.value
            }),
            graphs
        };
    }

    function sendEdit(edits) {
        return new Promise((resolve, reject) => {
            const id = ++nextCommand;
            const timeout = setTimeout(() => {
                requests.delete(id);
                reject(new Error(tr("The player did not apply the block edit.", "플레이어가 블록 수정을 반영하지 못했습니다.")));
            }, 10000);
            requests.set(id, {resolve, reject, timeout});
            if(!send({action: "edit", id, edits})) {
                clearTimeout(timeout);
                requests.delete(id);
                reject(new Error(tr("Player connection is not ready. Try again.", "플레이어 연결 중입니다. 다시 눌러주세요.")));
            }
        });
    }
    
    async function run(clicked = null) {
        if(busy || ["receiving", "building", "stopping"].includes(phase)) return;
        busy = true;
        refresh();
        try {
            if (!connection) { open(); return; }
            if (!connection || disposed) return;
            const connected = connection;
            const state = await request(connected, "/status");
            if (connection !== connected || disposed) return;
            applyStatus(state);
            if (!connection) { open(); return; }
            if(phase === "running") {
                const currentRevision = revision;
                const current = previewRevision === revision ? previewSnapshot : snapshot();
                if(previewSettingsRevision === settingsRevision && previewSnapshot && current.structure === previewSnapshot.structure) {
                    const edits = current.graphs.filter((graph, index) => JSON.stringify(graph) !== JSON.stringify(previewSnapshot.graphs[index]));
                    const size = new TextEncoder().encode(JSON.stringify({action: "edit", edits})).byteLength;
                    if(size < 4 * 1024 * 1024 - 256) {
                        if(edits.length) await sendEdit(edits);
                        previewSnapshot = current;
                        previewRevision = currentRevision;
                        if(!send(clicked || {action: "flag"})) {
                            initialCommand = clicked || {action: "flag"};
                            openStream();
                        }
                        return;
                    }
                }
                await request(connection, "/stop", "POST");

                for(let i=0; i < 40; i++) {
                    const state = await request(connection, "/status");
                    if (state.phase === "ready" || state.phase === "error") {
                        applyStatus(state);
                        break;
                    }
                    await new Promise(resolve => setTimeout(resolve, 100));
                }
                restoreEditor();
            }
            if(disposed) return;
            const target = connection;
            upload = new AbortController();
            phase = "preparing";
            status.textContent = messages[phase];
            refresh();
            await new Promise(resolve => setTimeout(resolve, 0));
            if (disposed || upload.signal.aborted) return;
            originalStop.call(runtime);
            suspendEditor();
            previewSnapshot = snapshot();
            previewRevision = revision;
            previewTargets = new Map(runtime.targets.filter(target => target.isOriginal).map((target, index) => [target.id, index]));
            initialCommand = clicked;
            const options = {...projectOptions(), clickStart: Boolean(clicked)};
            // Keep editor IDs; the normal SB3 exporter renames them.
            const json = vm.toJSON(undefined, {allowOptimization: false});
            const assets = vm.serializeAssets();
            const matches = cached => cached?.json === json && assets.length === cached.assets.length &&
                assets.every((asset, index) => asset.fileName === cached.assets[index].fileName &&
                    asset.fileContent === cached.assets[index].fileContent);
            const reuse = serverSnapshot && lastUpload?.target === target &&
                lastUpload.snapshot === serverSnapshot && matches(lastUpload);
            let project = reuse ? new Blob([]) : matches(archiveCache) ? archiveCache.blob : null;
            if (reuse) options.snapshot = serverSnapshot;
            if (!project) {
                const zip = new vm.exports.JSZip();
                zip.file("project.json", json, {compression: "DEFLATE"});
                for (const asset of assets) {
                    const compression = /\.(svg|wav|ttf|otf)$/i.test(asset.fileName) ? "DEFLATE" : "STORE";
                    zip.file(asset.fileName, asset.fileContent, {compression});
                }
                project = await zip.generateAsync({
                    type: "blob", mimeType: "application/x.scratch.sb3",
                    compressionOptions: {level: 1}
                });
                archiveCache = project.size <= 64 * 1024 * 1024 ? {json, assets, blob: project} : null;
            }
            if(disposed || upload.signal.aborted || connection !== target) return;
            phase = "receiving";
            status.textContent = messages[phase];
            refresh();
            const receipt = await request(target, "/run", "POST", project, options, upload);
            previewSettingsRevision = receipt.settingsRevision || "";
            activeDisplay = receipt.display || options.display;
            displayMode.value = activeDisplay;
            lastUpload = {target, json, assets, snapshot: receipt.snapshot};
            phase = "building";
            status.textContent = messages[phase];
        } catch (error) {
            if (!disposed && !(upload && upload.signal.aborted)) showError(error);
        } finally {
            upload = null;
            busy = false;
            refresh();
            if (connection && ["receiving", "building", "running", "stopping"].includes(phase)) poll();
        }
    }

    async function stop() {
        if(upload) upload.abort();
        if(!connection) return;
        const target = connection;
        phase = "stopping";
        refresh();
        try {
            await request(target, "/stop", "POST");
            if(connection === target && !disposed) poll();
        } catch (error) {
            if(!disposed) showError(error);
        }
    }
    
    function open() {
        if(disposed) return;
        if(!dialog.open) dialog.showModal();
        refresh();
        if (connection) poll();
        else address.focus();
    }
    
    connectButton.onclick = async () => {
        if(busy) return;
        busy = true;
        refresh();
        try { await connect(); } catch (error) { showError(error); }
        finally { busy = false; refresh(); }
    };
    runButton.onclick = () => run();
    stopButton.onclick = stop;
    disconnectButton.onclick = async () => {
        if (upload) upload.abort();
        const target = connection;
        resetConnection();
        address.value = "";
        address.focus();
        try {
            if (target) await request(target, "/disconnect", "POST");
        } catch (error) {
            if (!connection && !busy && !disposed) showError(error);
        }
    };

    function resetConnection() {
        clearTimeout(polling);
        connection = null;
        multiWindow = false;
        phase = "ready";
        status.textContent = tr("Disconnected", "연결 해제됨");
        restoreEditor();
        refresh();
    }
    
    function suspendEditor() {
        if(!suspendedLoop && runtime.frameLoop.running) {
            runtime.frameLoop.stop();
            suspendedLoop = true;
        }
    }
    
    function restoreEditor() {
        pendingFrames = 0;
        if(socket) {
            const old = socket;
            socket = null;
            old.close();
        }
        cancelAnimationFrame(mouseRequest);
        mouseRequest = 0;
        pendingMouse = null;
        overlay?.remove();
        overlay = null;
        for(const report of reports.values()) clearTimeout(report.timeout);
        reports.clear();
        for(const pending of requests.values()) {
            clearTimeout(pending.timeout);
            pending.reject(new Error(tr("Preview connection closed.", "미리보기 연결이 종료되었습니다.")));
        }
        requests.clear();
        if (suspendedLoop) {
            suspendedLoop = false;
            runtime.emit("PROJECT_RUN_STOP");
            runtime.frameLoop.start();
        }
    }
    
    function send(command) {
        if(!socket || socket.readyState !== WebSocket.OPEN) {
            status.textContent = tr("Connecting to the player…", "플레이어 연결 중…");
            return false;
        }
        if(socket.bufferedAmount > 65536) {
            socket.close();
            return false;
        }
        if(command.action === "toggle") {
            command = {...command, id: ++nextCommand};
            const timeout = setTimeout(() => reports.delete(command.id), 30000);
            reports.set(command.id, {block: command.block, target: command.target, timeout});
        }
        socket.send(JSON.stringify(command));
        if (command.action === "flag" || command.action === "toggle") runtime.emit("PROJECT_RUN_START");
        if (command.action === "stop") runtime.emit("PROJECT_RUN_STOP");
        return true;
    }
    
    function openStream() {
        if (socket || !connection || disposed) return;
        const ws = new WebSocket(connection.base.replace(/^http:/, "ws:") + "/preview", "gowarp-preview");
        socket = ws;
        pendingFrames = 0;
        ws.binaryType = "arraybuffer";
        ws.onopen = () => {
            if(socket !== ws) return;
            ws.send(JSON.stringify({token: connection.token}));
            if(activeDisplay === "embedded") {
                attachPreview();
                requestFrame();
            }
            if(initialCommand) {
                send(initialCommand);
                initialCommand = null;
            }
        };
        ws.onmessage = event => {
            if(socket !== ws || disposed) return;
            if(event.data instanceof ArrayBuffer) {
                displayFrame(event.data);
                return;
            }
            let reply;
            try {
                reply = JSON.parse(event.data);
            } catch {
                return;
            }
            const pending = requests.get(reply.id);
            if(pending) {
                requests.delete(reply.id);
                clearTimeout(pending.timeout);
                if (reply.error) pending.reject(new Error(reply.error));
                else pending.resolve();
                return;
            }
            const report = reports.get(reply.id);
            if(report) {
                clearTimeout(report.timeout);
                reports.delete(reply.id);
                if(reply.reporter && !reply.error) {
                    const targetId = [...previewTargets].find(([, index]) => index === report.target)?.[0];
                    runtime.visualReport(runtime.getTargetById(targetId), report.block, reply.value);
                }
            }
            if(reply.error) {
                status.textContent = reply.error;
                open();
            }
        };
        ws.onclose = () => {
            if (socket !== ws || disposed) return;
            socket = null;
            overlay?.remove();
            overlay = null;
            // Status polling reconnects while the same preview remains active.
        };
    }
    
    function attachPreview() {
        const parent = runtime.renderer?.canvas?.parentElement;
        if (!parent || overlay) return;
        overlay = element("canvas", "", parent);
        overlay.id = "gowarp-preview";
        overlay.tabIndex = 0;
        overlay.setAttribute("aria-label", tr("GoWarp game preview", "GoWarp 게임 미리보기"));
        overlay.oncontextmenu = event => event.preventDefault();
        const pointer = event => {
            const rect = overlay.getBoundingClientRect();
            if (!rect.width || !rect.height) return;
            pendingMouse = {
                action: "mouse", x: (event.clientX-rect.left)/rect.width,
                y: (event.clientY-rect.top)/rect.height, down: (event.buttons & 1) !== 0
            };
            if (!mouseRequest) {
                mouseRequest = requestAnimationFrame(() => {
                    mouseRequest = 0;
                    if (pendingMouse) send(pendingMouse);
                    pendingMouse = null;
                });
            }
        };
        overlay.onpointermove = pointer;
        overlay.onpointerdown = event => {
            event.stopPropagation();
            event.preventDefault();
            overlay.focus();
            overlay.setPointerCapture(event.pointerId);
            pointer(event);
            // Preserve a quick press/release occurring within one animation frame.
            send(pendingMouse);
            pendingMouse = null;
        };
        overlay.onpointerup = event => {
            event.stopPropagation();
            pointer(event);
            send(pendingMouse);
            pendingMouse = null;
        };
        overlay.onpointercancel = releaseInput;
        overlay.onblur = releaseInput;
        const key = event => ({
            "ArrowUp": "up arrow", "ArrowDown": "down arrow",
            "ArrowLeft": "left arrow", "ArrowRight": "right arrow", " ": "space"
        }[event.key] || event.key.toLowerCase());
        overlay.onkeydown = event => {
            event.stopPropagation();
            event.preventDefault();
            if (!event.repeat) send({action: "key", key: key(event), down: true});
            if (event.key.length === 1 && !event.ctrlKey && !event.metaKey) send({action: "text", text: event.key});
        };
        overlay.onkeyup = event => {
            event.stopPropagation();
            event.preventDefault();
            send({action: "key", key: key(event), down: false});
        };
        
        
    }
    
    function releaseInput() {
        cancelAnimationFrame(mouseRequest);
        mouseRequest = 0;
        pendingMouse = null;
        if (socket?.readyState === WebSocket.OPEN) send({action: "release-input"});
    }
    
    function visibilityChanged() {
        if (document.hidden) releaseInput();
        else requestFrame();
    }
    
    function requestFrame() {
        if (document.hidden || !overlay || phase !== "running" || activeDisplay !== "embedded" || pendingFrames >= 2) return;
        const bounds = overlay.getBoundingClientRect();
        const ratio = window.devicePixelRatio || 1;
        const width = Math.max(1, Math.round(bounds.width * ratio));
        const height = Math.max(1, Math.round(bounds.height * ratio));
        const scale = Math.min(1, maxFrameEdge / Math.max(width, height));
        const frames = 2 - pendingFrames;
        if (send({
            action: "frame", frames, width: Math.max(1, Math.floor(width * scale)),
            height: Math.max(1, Math.floor(height * scale))
        })) pendingFrames += frames;
    }
    
    window.addEventListener("blur", releaseInput);
    document.addEventListener("visibilitychange", visibilityChanged);
    
    function displayFrame(buffer) {
        if (buffer.byteLength < 9) return;
        const header = new DataView(buffer);
        const width = header.getUint32(1, true), height = header.getUint32(5, true);
        if (header.getUint8(0) !== 1 || !width || !height || width > maxFrameEdge || height > maxFrameEdge ||
        buffer.byteLength !== 9+width*height*4) return;
        pendingFrames = Math.max(0, pendingFrames - 1);
        if (document.hidden) return;
        attachPreview();
        requestFrame();
        if (overlay) {
            if (overlay.width !== width) overlay.width = width;
            if (overlay.height !== height) overlay.height = height;
            overlay.getContext("2d").putImageData(new ImageData(new Uint8ClampedArray(buffer, 9), width, height), 0, 0);
        }
    }
    
    function toggleScript(blockId, options = {}) {
        if (!connection) return originalToggle.call(this, blockId, options);
        const target = options.target || runtime.getEditingTarget();
        const index = phase === "running" && previewRevision === revision
        ? previewTargets.get(target?.id)
        : runtime.targets.filter(item => item.isOriginal).indexOf(target);
        if (index === undefined || index < 0) return;
        const block = window.ScratchBlocks?.getMainWorkspace()?.getBlockById(blockId);
        const command = {action: "toggle", target: index, block: blockId, reporter: Boolean(block?.outputConnection)};
        run(command);
    }
    
    function greenFlag() {
        if (!connection) return originalFlag.call(this);
        run();
    }
    
    function stopScripts() {
        if (!connection) return originalStop.call(this);
        if (phase === "running") send({action: "stop"});
        else if (busy) stop();
    }
    
    runtime.toggleScript = toggleScript;
    runtime.greenFlag = greenFlag;
    runtime.stopAll = stopScripts;
    
    function attachButton() {
        if (disposed || document.getElementById("gowarp-editor-button")) return;
        const bar = document.querySelector('[class*="menu-bar_main-menu"]') ||
        document.querySelector('[class*="menu-bar_menu-bar_"]');
        if (!bar) return;
        const button = element("button", tr("GoWarp Run", "GoWarp 실행"), bar);
        button.id = "gowarp-editor-button";
        button.className = "gowarp-editor-control";
        button.type = "button";
        button.title = tr("Run this project in GoWarp", "현재 프로젝트를 GoWarp에서 실행");
        button.onclick = async () => {
            if (!connection) {
                open();
                return;
            }
            if (busy || ["receiving", "building", "stopping"].includes(phase)) return;
            if (dialog.open) dialog.close();
            await run();
        };
        const menuStop = element("button", tr("GoWarp Stop", "GoWarp 중지"), bar);
        menuStop.id = "gowarp-editor-stop";
        menuStop.className = "gowarp-editor-control";
        menuStop.type = "button";
        menuStop.title = tr("Close the GoWarp preview or cancel its build", "GoWarp 미리보기 종료 / 빌드 취소");
        menuStop.onclick = stop;
        refresh();
    }
    
    const observer = new MutationObserver(attachButton);
    observer.observe(document.body, {childList: true, subtree: true});
    attachButton();
    refresh();
    const dispose = () => {
        if (disposed) return;
        disposed = true;
        if (connection) {
            fetch(connection.base + "/disconnect", {
                method: "POST", headers: {Authorization: `Bearer ${connection.token}`},
                credentials: "omit", keepalive: true, targetAddressSpace
            }).catch(() => {});
            connection = null;
        }
        clearTimeout(polling);
        if (upload) upload.abort();
        observer.disconnect();
        archiveCache = null;
        lastUpload = null;
        loading?.remove();
        loading = null;
        restoreEditor();
        for (const event of projectEvents) vm.removeListener(event, changed);
        for (const event of settingsEvents) runtime.removeListener(event, changed);
        if (runtime.toggleScript === toggleScript) runtime.toggleScript = originalToggle;
        if (runtime.greenFlag === greenFlag) runtime.greenFlag = originalFlag;
        if (runtime.stopAll === stopScripts) runtime.stopAll = originalStop;
        if (vm.toJSON === toJSON) vm.toJSON = originalToJSON;
        document.getElementById("gowarp-editor-button")?.remove();
        document.getElementById("gowarp-editor-stop")?.remove();
        dialog.remove();
        style.remove();
        window.removeEventListener("pagehide", dispose);
        window.removeEventListener("blur", releaseInput);
        document.removeEventListener("visibilitychange", visibilityChanged);
    };
    window.addEventListener("pagehide", dispose);
    window.__gowarpEditor = {dispose};
    
    class GoWarpEditor {
        getInfo() {
            return {
                id: "gowarpEditor", name: "GoWarp", color1: "#b52e35",
                blocks: [
                    {blockType: Scratch.BlockType.BUTTON, text: tr("GoWarp connection settings", "GoWarp 연결 설정"), func: "open"},
                    {opcode: "installed", blockType: Scratch.BlockType.REPORTER, text: "GoWarp", hideFromPalette: true, disableMonitor: true}
                ]
            };
        }
        open() { open(); }
        installed() { return ""; }
    }
    Scratch.extensions.register(new GoWarpEditor());
})(Scratch);